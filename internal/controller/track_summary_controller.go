package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
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
	Scheme           *runtime.Scheme
	FindingNamespace string
	Analyzer         *analyzer.TrackSummaryAnalyzer
}

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

func trackSummaryRequest(_ context.Context, _ client.Object) []reconcile.Request {
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: "cluster"}}}
}

func (r *TrackSummaryReconciler) upsertFinding(ctx context.Context, name string, draft analyzer.TrackFindingDraft) error {
	key := types.NamespacedName{Namespace: r.FindingNamespace, Name: name}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		now := metav1.Now()
		var current yardv1alpha1.DispatchFinding
		err := r.Get(ctx, key, &current)
		if apierrors.IsNotFound(err) {
			finding := yardv1alpha1.DispatchFinding{
				TypeMeta: metav1.TypeMeta{
					APIVersion: yardv1alpha1.GroupVersion.String(),
					Kind:       "DispatchFinding",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: r.FindingNamespace,
					Labels: map[string]string{
						"yardmaster.dev/category": "tracks",
						"yardmaster.dev/subject":  "track",
					},
				},
				Spec: draft.Spec,
			}
			if err := r.Create(ctx, &finding); err != nil {
				return err
			}
			finding.Status.FirstSeen = now
			finding.Status.LastSeen = now
			return r.Status().Update(ctx, &finding)
		}
		if err != nil {
			return err
		}

		current.Spec = draft.Spec
		if err := r.Update(ctx, &current); err != nil {
			return err
		}
		current.Status.LastSeen = now
		if current.Status.FirstSeen.IsZero() {
			current.Status.FirstSeen = now
		}
		return r.Status().Update(ctx, &current)
	})
}

func (r *TrackSummaryReconciler) deleteStaleFindings(ctx context.Context, expected map[string]struct{}) error {
	var existing yardv1alpha1.DispatchFindingList
	if err := r.List(ctx, &existing, client.InNamespace(r.FindingNamespace), client.MatchingLabels{"yardmaster.dev/category": "tracks"}); err != nil {
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
