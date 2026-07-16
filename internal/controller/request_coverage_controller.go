package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/warehousegang/yardmaster/internal/analyzer"
)

type RequestCoverageReconciler struct {
	client.Client
	FindingNamespace  string
	IgnoredNamespaces map[string]struct{}
	Analyzer          *analyzer.RequestCoverageAnalyzer
}

func (r *RequestCoverageReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("pod", req.NamespacedName)

	var pod corev1.Pod
	if err := r.Get(ctx, req.NamespacedName, &pod); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, r.deleteFinding(ctx, req.Namespace, req.Name)
		}
		return ctrl.Result{}, err
	}

	if r.ignoresNamespace(pod.Namespace) {
		return ctrl.Result{}, r.deleteFinding(ctx, pod.Namespace, pod.Name)
	}

	finding := r.Analyzer.AnalyzePod(&pod)
	if finding == nil {
		return ctrl.Result{}, r.deleteFinding(ctx, pod.Namespace, pod.Name)
	}
	promoteFindingToWorkload(ctx, r.Client, &pod, &finding.Spec)

	if err := r.upsertFinding(ctx, &pod, finding); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("recorded request coverage finding")
	return ctrl.Result{RequeueAfter: 15 * time.Minute}, nil
}

func (r *RequestCoverageReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.FindingNamespace == "" {
		r.FindingNamespace = DefaultFindingNamespace
	}
	if r.Analyzer == nil {
		r.Analyzer = analyzer.NewRequestCoverageAnalyzer()
	}
	if r.IgnoredNamespaces == nil {
		r.IgnoredNamespaces = DefaultIgnoredRequestNamespaces()
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named("request-coverage").
		For(&corev1.Pod{}).
		Complete(r)
}

func (r *RequestCoverageReconciler) ignoresNamespace(namespace string) bool {
	_, ok := r.IgnoredNamespaces[namespace]
	return ok
}

func (r *RequestCoverageReconciler) upsertFinding(ctx context.Context, pod *corev1.Pod, draft *analyzer.FindingDraft) error {
	name := requestFindingNameForPod(pod.Namespace, pod.Name)
	return upsertDispatchFinding(ctx, r.Client, r.FindingNamespace, name, "requests", draft.Spec)
}

func (r *RequestCoverageReconciler) deleteFinding(ctx context.Context, podNamespace, podName string) error {
	key := types.NamespacedName{
		Namespace: r.FindingNamespace,
		Name:      requestFindingNameForPod(podNamespace, podName),
	}
	return deleteDispatchFinding(ctx, r.Client, key)
}

func requestFindingNameForPod(namespace, name string) string {
	base := sanitizeDNSLabel(fmt.Sprintf("requests-pod-%s-%s", namespace, name))
	hash := shortHash("requests/" + namespace + "/" + name)
	maxBase := 63 - len(hash) - 1
	if len(base) > maxBase {
		base = strings.TrimRight(base[:maxBase], "-")
	}
	if base == "" {
		base = "requests-pod"
	}
	return fmt.Sprintf("%s-%s", base, hash)
}

func DefaultIgnoredRequestNamespaces() map[string]struct{} {
	return map[string]struct{}{
		"kube-node-lease":    {},
		"kube-public":        {},
		"kube-system":        {},
		"local-path-storage": {},
		"yardmaster-system":  {},
	}
}
