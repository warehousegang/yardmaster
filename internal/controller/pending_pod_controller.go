package controller

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	yardv1alpha1 "github.com/warehousegang/yardmaster/api/v1alpha1"
	"github.com/warehousegang/yardmaster/internal/analyzer"
)

const DefaultFindingNamespace = "yardmaster-system"

type PendingPodReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	FindingNamespace string
	Analyzer         *analyzer.PendingPodAnalyzer
}

func (r *PendingPodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("pod", req.NamespacedName)

	var pod corev1.Pod
	if err := r.Get(ctx, req.NamespacedName, &pod); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, r.deleteFinding(ctx, req.Namespace, req.Name)
		}
		return ctrl.Result{}, err
	}

	var nodes corev1.NodeList
	if err := r.List(ctx, &nodes); err != nil {
		return ctrl.Result{}, err
	}

	var events corev1.EventList
	if err := r.List(ctx, &events, client.InNamespace(pod.Namespace)); err != nil {
		return ctrl.Result{}, err
	}

	finding := r.Analyzer.AnalyzePod(&pod, nodes.Items, events.Items)
	if finding == nil {
		return ctrl.Result{}, r.deleteFinding(ctx, pod.Namespace, pod.Name)
	}

	if err := r.upsertFinding(ctx, &pod, finding); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("recorded pending pod finding")
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *PendingPodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.FindingNamespace == "" {
		r.FindingNamespace = DefaultFindingNamespace
	}
	if r.Analyzer == nil {
		r.Analyzer = analyzer.NewPendingPodAnalyzer()
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Watches(&corev1.Node{}, handler.EnqueueRequestsFromMapFunc(r.requestsForAllPendingPods)).
		Watches(&corev1.Event{}, handler.EnqueueRequestsFromMapFunc(r.requestsForEvent)).
		Complete(r)
}

func (r *PendingPodReconciler) requestsForAllPendingPods(ctx context.Context, _ client.Object) []reconcile.Request {
	var pods corev1.PodList
	if err := r.List(ctx, &pods); err != nil {
		return nil
	}

	requests := make([]reconcile.Request, 0, len(pods.Items))
	for _, pod := range pods.Items {
		if pod.Status.Phase != corev1.PodPending || pod.Spec.NodeName != "" {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name},
		})
	}
	return requests
}

func (r *PendingPodReconciler) requestsForEvent(_ context.Context, object client.Object) []reconcile.Request {
	event, ok := object.(*corev1.Event)
	if !ok || event.InvolvedObject.Kind != "Pod" || event.InvolvedObject.Name == "" {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Namespace: event.InvolvedObject.Namespace,
			Name:      event.InvolvedObject.Name,
		},
	}}
}

func (r *PendingPodReconciler) upsertFinding(ctx context.Context, pod *corev1.Pod, draft *analyzer.FindingDraft) error {
	name := findingNameForPod(pod.Namespace, pod.Name)
	key := types.NamespacedName{Namespace: r.FindingNamespace, Name: name}
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
					"yardmaster.dev/category": "scheduling",
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
}

func (r *PendingPodReconciler) deleteFinding(ctx context.Context, podNamespace, podName string) error {
	key := types.NamespacedName{
		Namespace: r.FindingNamespace,
		Name:      findingNameForPod(podNamespace, podName),
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

func findingNameForPod(namespace, name string) string {
	base := sanitizeDNSLabel(fmt.Sprintf("pod-%s-%s", namespace, name))
	hash := shortHash(namespace + "/" + name)
	maxBase := 63 - len(hash) - 1
	if len(base) > maxBase {
		base = strings.TrimRight(base[:maxBase], "-")
	}
	if base == "" {
		base = "pod"
	}
	return fmt.Sprintf("%s-%s", base, hash)
}

func sanitizeDNSLabel(value string) string {
	value = strings.ToLower(value)
	var builder strings.Builder
	builder.Grow(len(value))
	lastDash := false
	for _, r := range value {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if valid {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteRune('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func shortHash(value string) string {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(value))
	return fmt.Sprintf("%08x", hash.Sum32())
}
