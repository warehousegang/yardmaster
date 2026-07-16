package controller

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/warehousegang/yardmaster/internal/analyzer"
)

const DefaultFindingNamespace = "yardmaster-system"

type PendingPodReconciler struct {
	client.Client
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
	promoteFindingToWorkload(ctx, r.Client, &pod, &finding.Spec)

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
		Named("pending-pod").
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
	return upsertDispatchFinding(ctx, r.Client, r.FindingNamespace, name, "scheduling", draft.Spec)
}

func (r *PendingPodReconciler) deleteFinding(ctx context.Context, podNamespace, podName string) error {
	key := types.NamespacedName{
		Namespace: r.FindingNamespace,
		Name:      findingNameForPod(podNamespace, podName),
	}
	return deleteDispatchFinding(ctx, r.Client, key)
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
