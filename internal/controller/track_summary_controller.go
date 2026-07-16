package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	yardv1alpha1 "github.com/warehousegang/yardmaster/api/v1alpha1"
	"github.com/warehousegang/yardmaster/internal/analyzer"
)

type TrackSummaryReconciler struct {
	client.Client
	FindingNamespace string
	Analyzer         *analyzer.TrackSummaryAnalyzer
}

var karpenterVersions = []string{"v1", "v1beta1"}

func (r *TrackSummaryReconciler) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var nodes corev1.NodeList
	if err := r.List(ctx, &nodes); err != nil {
		return ctrl.Result{}, err
	}

	var pods corev1.PodList
	if err := r.List(ctx, &pods); err != nil {
		return ctrl.Result{}, err
	}

	findings := r.Analyzer.Analyze(nodes.Items, pods.Items)
	karpenterFindings, err := r.karpenterTrackFindings(ctx)
	if err != nil {
		logger.Error(err, "unable to read Karpenter track policy")
	} else {
		findings = append(findings, karpenterFindings...)
	}

	expected := make(map[string]struct{}, len(findings))
	for _, finding := range findings {
		name := trackFindingName(finding.TrackName)
		expected[name] = struct{}{}
		if err := r.upsertFinding(ctx, name, finding); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.deleteStaleFindings(ctx, expected); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("recorded track summary findings", "count", len(findings))
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *TrackSummaryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.FindingNamespace == "" {
		r.FindingNamespace = DefaultFindingNamespace
	}
	if r.Analyzer == nil {
		r.Analyzer = analyzer.NewTrackSummaryAnalyzer()
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named("track-summary").
		For(&corev1.Node{}).
		Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(trackSummaryRequest)).
		Complete(r)
}

func (r *TrackSummaryReconciler) karpenterTrackFindings(ctx context.Context) ([]analyzer.TrackFindingDraft, error) {
	nodePools, version, err := r.listKarpenterObjects(ctx, "NodePoolList")
	if meta.IsNoMatchError(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	nodeClaims, _, err := r.listKarpenterObjects(ctx, "NodeClaimList")
	if err != nil && !meta.IsNoMatchError(err) {
		return nil, err
	}

	var nodeClaimItems []unstructured.Unstructured
	if nodeClaims != nil {
		nodeClaimItems = nodeClaims.Items
	}
	claimsByPool := karpenterClaimsByPool(nodeClaimItems)
	findings := make([]analyzer.TrackFindingDraft, 0, len(nodePools.Items))
	for _, nodePool := range nodePools.Items {
		name := nodePool.GetName()
		trackName := "karpenter-policy/" + name
		activeNodes := nestedStringDefault(nodePool.Object, "0", "status", "nodes")
		ready := karpenterConditionStatus(nodePool, "Ready")
		cpuUsed := nestedStringDefault(nodePool.Object, "0", "status", "resources", "cpu")
		cpuLimit := nestedStringDefault(nodePool.Object, "unbounded", "spec", "limits", "cpu")
		memUsed := nestedStringDefault(nodePool.Object, "0", "status", "resources", "memory")
		memLimit := nestedStringDefault(nodePool.Object, "unbounded", "spec", "limits", "memory")
		nodeLimit := nestedStringDefault(nodePool.Object, "unbounded", "spec", "limits", "nodes")
		nodeClass := karpenterNodeClass(nodePool)
		requirements := karpenterRequirements(nodePool)
		taints := karpenterTaints(nodePool)
		disruption := karpenterDisruption(nodePool)
		claimCount := claimsByPool[name]

		findings = append(findings, analyzer.TrackFindingDraft{
			TrackName: trackName,
			Spec: yardv1alpha1.DispatchFindingSpec{
				Severity: yardv1alpha1.FindingSeverityInfo,
				Category: "tracks",
				Subject: yardv1alpha1.DispatchFindingSubject{
					APIVersion: yardv1alpha1.GroupVersion.String(),
					Kind:       "Track",
					Name:       trackName,
				},
				Summary: fmt.Sprintf("Karpenter NodePool %s is %s with %s active node(s) and %d NodeClaim(s).", name, ready, activeNodes, claimCount),
				Detail: fmt.Sprintf(
					"Grouped by karpenter.sh/nodepool. CPU requested %s of %s (%s); memory requested %s of %s (%s). Nodes: %s. Karpenter policy: apiVersion karpenter.sh/%s; node limit %s; nodeClass %s; requirements %s; taints %s; disruption %s.",
					cpuUsed,
					cpuLimit,
					percentFromStrings(cpuUsed, cpuLimit),
					memUsed,
					memLimit,
					percentFromStrings(memUsed, memLimit),
					nodeNamesForKarpenterTrack(activeNodes, claimCount),
					version,
					nodeLimit,
					nodeClass,
					requirements,
					taints,
					disruption,
				),
				Recommendations: []string{
					"Compare NodePool requirements, taints, and limits with workloads that need elastic capacity.",
					"Inspect Karpenter events when a pending workload should match this NodePool but no NodeClaim appears.",
				},
			},
		})
	}
	return findings, nil
}

func (r *TrackSummaryReconciler) listKarpenterObjects(ctx context.Context, kind string) (*unstructured.UnstructuredList, string, error) {
	var lastErr error
	for _, version := range karpenterVersions {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "karpenter.sh",
			Version: version,
			Kind:    kind,
		})
		if err := r.List(ctx, list); err != nil {
			lastErr = err
			if meta.IsNoMatchError(err) {
				continue
			}
			return nil, version, err
		}
		return list, version, nil
	}
	return nil, "", lastErr
}

func trackSummaryRequest(_ context.Context, _ client.Object) []reconcile.Request {
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: "cluster"}}}
}

func (r *TrackSummaryReconciler) upsertFinding(ctx context.Context, name string, draft analyzer.TrackFindingDraft) error {
	return upsertDispatchFinding(ctx, r.Client, r.FindingNamespace, name, "tracks", draft.Spec)
}

func (r *TrackSummaryReconciler) deleteStaleFindings(ctx context.Context, expected map[string]struct{}) error {
	var existing yardv1alpha1.DispatchFindingList
	if err := r.List(ctx, &existing, client.InNamespace(r.FindingNamespace), client.MatchingLabels{findingCategoryLabel: "tracks"}); err != nil {
		return err
	}
	for _, finding := range existing.Items {
		if _, ok := expected[finding.Name]; ok {
			continue
		}
		if err := r.Delete(ctx, &finding); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func trackFindingName(trackName string) string {
	base := sanitizeDNSLabel("track-" + trackName)
	hash := shortHash("track/" + trackName)
	maxBase := 63 - len(hash) - 1
	if len(base) > maxBase {
		base = strings.TrimRight(base[:maxBase], "-")
	}
	if base == "" {
		base = "track"
	}
	return fmt.Sprintf("%s-%s", base, hash)
}

func karpenterClaimsByPool(items []unstructured.Unstructured) map[string]int {
	claims := make(map[string]int)
	for _, item := range items {
		pool := item.GetLabels()["karpenter.sh/nodepool"]
		if pool == "" {
			pool = item.GetLabels()["karpenter.sh/nodepool-name"]
		}
		if pool == "" {
			continue
		}
		claims[pool]++
	}
	return claims
}

func karpenterConditionStatus(nodePool unstructured.Unstructured, conditionType string) string {
	conditions, ok, _ := unstructured.NestedSlice(nodePool.Object, "status", "conditions")
	if !ok {
		return "Unknown"
	}
	for _, item := range conditions {
		condition, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if fmt.Sprint(condition["type"]) != conditionType {
			continue
		}
		switch fmt.Sprint(condition["status"]) {
		case "True":
			return "Ready"
		case "False":
			return "NotReady"
		default:
			return "Unknown"
		}
	}
	return "Unknown"
}

func karpenterNodeClass(nodePool unstructured.Unstructured) string {
	ref, ok, _ := unstructured.NestedMap(nodePool.Object, "spec", "template", "spec", "nodeClassRef")
	if !ok {
		return "not set"
	}
	kind := fmt.Sprint(ref["kind"])
	name := fmt.Sprint(ref["name"])
	if kind == "" || kind == "<nil>" {
		kind = "NodeClass"
	}
	if name == "" || name == "<nil>" {
		return kind
	}
	return kind + "/" + name
}

func karpenterRequirements(nodePool unstructured.Unstructured) string {
	requirements, ok, _ := unstructured.NestedSlice(nodePool.Object, "spec", "template", "spec", "requirements")
	if !ok || len(requirements) == 0 {
		return "none"
	}
	out := make([]string, 0, len(requirements))
	for _, item := range requirements {
		req, ok := item.(map[string]any)
		if !ok {
			continue
		}
		key := fmt.Sprint(req["key"])
		operator := fmt.Sprint(req["operator"])
		values := stringSliceFromAny(req["values"])
		if len(values) == 0 {
			out = append(out, fmt.Sprintf("%s %s", key, operator))
			continue
		}
		out = append(out, fmt.Sprintf("%s %s [%s]", key, operator, strings.Join(values, ",")))
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, "; ")
}

func karpenterTaints(nodePool unstructured.Unstructured) string {
	taints, ok, _ := unstructured.NestedSlice(nodePool.Object, "spec", "template", "spec", "taints")
	if !ok || len(taints) == 0 {
		return "none"
	}
	out := make([]string, 0, len(taints))
	for _, item := range taints {
		taint, ok := item.(map[string]any)
		if !ok {
			continue
		}
		key := fmt.Sprint(taint["key"])
		value := fmt.Sprint(taint["value"])
		effect := fmt.Sprint(taint["effect"])
		if value == "" || value == "<nil>" {
			out = append(out, fmt.Sprintf("%s:%s", key, effect))
			continue
		}
		out = append(out, fmt.Sprintf("%s=%s:%s", key, value, effect))
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, ", ")
}

func karpenterDisruption(nodePool unstructured.Unstructured) string {
	policy := nestedStringDefault(nodePool.Object, "not set", "spec", "disruption", "consolidationPolicy")
	after := nestedStringDefault(nodePool.Object, "not set", "spec", "disruption", "consolidateAfter")
	return fmt.Sprintf("%s after %s", policy, after)
}

func nestedStringDefault(object map[string]any, fallback string, fields ...string) string {
	value, ok, _ := unstructured.NestedFieldNoCopy(object, fields...)
	if !ok || value == nil {
		return fallback
	}
	if text := fmt.Sprint(value); text != "" && text != "<nil>" {
		return text
	}
	return fallback
}

func stringSliceFromAny(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text := fmt.Sprint(item)
		if text == "" || text == "<nil>" {
			continue
		}
		out = append(out, text)
	}
	return out
}

func percentFromStrings(used, total string) string {
	usedQuantity, err := resource.ParseQuantity(used)
	if err != nil {
		return "n/a"
	}
	totalQuantity, err := resource.ParseQuantity(total)
	if err != nil || totalQuantity.IsZero() {
		return "n/a"
	}
	return fmt.Sprintf("%d%%", int((usedQuantity.MilliValue()*100)/totalQuantity.MilliValue()))
}

func nodeNamesForKarpenterTrack(activeNodes string, claimCount int) string {
	if activeNodes == "0" && claimCount == 0 {
		return "none"
	}
	return fmt.Sprintf("%s active Karpenter node(s), %d NodeClaim(s)", activeNodes, claimCount)
}
