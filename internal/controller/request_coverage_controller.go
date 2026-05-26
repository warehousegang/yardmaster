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
	"sigs.k8s.io/controller-runtime/pkg/log"

	yardv1alpha1 "github.com/warehousegang/yardmaster/api/v1alpha1"
	"github.com/warehousegang/yardmaster/internal/analyzer"
)

type RequestCoverageReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
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
						"yardmaster.dev/category": "requests",
						"yardmaster.dev/subject":  "pod",
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

func (r *RequestCoverageReconciler) deleteFinding(ctx context.Context, podNamespace, podName string) error {
	key := types.NamespacedName{
		Namespace: r.FindingNamespace,
		Name:      requestFindingNameForPod(podNamespace, podName),
	}
	var finding yardv1alpha1.DispatchFinding
	if err := r.Get(ctx, key, &finding); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return r.Delete(ctx, &finding)
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
