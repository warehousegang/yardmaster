package controller

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	yardv1alpha1 "github.com/warehousegang/yardmaster/api/v1alpha1"
	"github.com/warehousegang/yardmaster/internal/analyzer"
)

func TestPendingPodReconcilerFindingLifecycle(t *testing.T) {
	ctx := context.Background()
	pod := controllerPendingPod("api-123")
	pod.Spec.NodeSelector = map[string]string{"workload": "api"}
	node := controllerReadyNode("node-1")
	k8sClient := newControllerTestClient(pod, node)
	reconciler := &PendingPodReconciler{
		Client:           k8sClient,
		FindingNamespace: DefaultFindingNamespace,
		Analyzer:         analyzer.NewPendingPodAnalyzer(),
	}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}}
	findingKey := types.NamespacedName{
		Namespace: DefaultFindingNamespace,
		Name:      findingNameForPod(pod.Namespace, pod.Name),
	}

	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("create reconcile failed: %v", err)
	}

	var created yardv1alpha1.DispatchFinding
	if err := k8sClient.Get(ctx, findingKey, &created); err != nil {
		t.Fatalf("expected created finding: %v", err)
	}
	if created.Spec.Category != "scheduling" {
		t.Fatalf("expected scheduling category, got %q", created.Spec.Category)
	}
	if created.Status.FirstSeen.IsZero() || created.Status.LastSeen.IsZero() {
		t.Fatalf("expected timestamps, got %#v", created.Status)
	}
	firstSeen := created.Status.FirstSeen

	var currentPod corev1.Pod
	if err := k8sClient.Get(ctx, request.NamespacedName, &currentPod); err != nil {
		t.Fatal(err)
	}
	currentPod.Spec.NodeSelector = nil
	if err := k8sClient.Update(ctx, &currentPod); err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("update reconcile failed: %v", err)
	}

	var updated yardv1alpha1.DispatchFinding
	if err := k8sClient.Get(ctx, findingKey, &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Spec.Summary == created.Spec.Summary {
		t.Fatalf("expected finding summary to update, still %q", updated.Spec.Summary)
	}
	if !updated.Status.FirstSeen.Equal(&firstSeen) {
		t.Fatalf("expected firstSeen to be preserved: before=%s after=%s", firstSeen, updated.Status.FirstSeen)
	}

	if err := k8sClient.Get(ctx, request.NamespacedName, &currentPod); err != nil {
		t.Fatal(err)
	}
	currentPod.Status.Phase = corev1.PodRunning
	currentPod.Spec.NodeName = node.Name
	if err := k8sClient.Update(ctx, &currentPod); err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("resolution reconcile failed: %v", err)
	}
	assertFindingDeleted(t, ctx, k8sClient, findingKey)
}

func TestPendingPodReconcilerDeletesFindingWhenPodIsDeleted(t *testing.T) {
	ctx := context.Background()
	podNamespace := "default"
	podName := "deleted-pod"
	finding := testFinding(
		findingNameForPod(podNamespace, podName),
		"scheduling",
		yardv1alpha1.DispatchFindingSubject{APIVersion: "v1", Kind: "Pod", Namespace: podNamespace, Name: podName},
	)
	k8sClient := newControllerTestClient(finding)
	reconciler := &PendingPodReconciler{
		Client:           k8sClient,
		FindingNamespace: DefaultFindingNamespace,
		Analyzer:         analyzer.NewPendingPodAnalyzer(),
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: podNamespace, Name: podName},
	}); err != nil {
		t.Fatalf("delete reconcile failed: %v", err)
	}

	assertFindingDeleted(t, ctx, k8sClient, client.ObjectKeyFromObject(finding))
}

func TestRequestCoverageReconcilerCreatesAndResolvesFinding(t *testing.T) {
	ctx := context.Background()
	pod := controllerPendingPod("missing-requests")
	pod.Status.Phase = corev1.PodRunning
	pod.Spec.Containers[0].Resources.Requests = nil
	k8sClient := newControllerTestClient(pod)
	reconciler := &RequestCoverageReconciler{
		Client:            k8sClient,
		FindingNamespace:  DefaultFindingNamespace,
		IgnoredNamespaces: map[string]struct{}{},
		Analyzer:          analyzer.NewRequestCoverageAnalyzer(),
	}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}}
	findingKey := types.NamespacedName{
		Namespace: DefaultFindingNamespace,
		Name:      requestFindingNameForPod(pod.Namespace, pod.Name),
	}

	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("create reconcile failed: %v", err)
	}
	var finding yardv1alpha1.DispatchFinding
	if err := k8sClient.Get(ctx, findingKey, &finding); err != nil {
		t.Fatalf("expected request finding: %v", err)
	}
	if !strings.Contains(finding.Spec.Detail, "container api cpu") {
		t.Fatalf("unexpected detail %q", finding.Spec.Detail)
	}

	var currentPod corev1.Pod
	if err := k8sClient.Get(ctx, request.NamespacedName, &currentPod); err != nil {
		t.Fatal(err)
	}
	currentPod.Spec.Containers[0].Resources.Requests = corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("100m"),
		corev1.ResourceMemory: resource.MustParse("128Mi"),
	}
	if err := k8sClient.Update(ctx, &currentPod); err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("resolution reconcile failed: %v", err)
	}
	assertFindingDeleted(t, ctx, k8sClient, findingKey)
}

func TestTrackSummaryReconcilerCreatesUpdatesAndCleansFindings(t *testing.T) {
	ctx := context.Background()
	node := controllerReadyNode("node-1")
	node.Labels[analyzer.TrackLabelKarpenterNodePool] = "general"
	stale := testFinding(
		"track-stale",
		"tracks",
		yardv1alpha1.DispatchFindingSubject{APIVersion: yardv1alpha1.GroupVersion.String(), Kind: "Track", Name: "stale"},
	)
	k8sClient := newControllerTestClient(node, stale)
	reconciler := &TrackSummaryReconciler{
		Client:           k8sClient,
		FindingNamespace: DefaultFindingNamespace,
		Analyzer:         analyzer.NewTrackSummaryAnalyzer(),
	}
	expectedKey := types.NamespacedName{
		Namespace: DefaultFindingNamespace,
		Name:      trackFindingName("karpenter/general"),
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{}); err != nil {
		t.Fatalf("create reconcile failed: %v", err)
	}
	var created yardv1alpha1.DispatchFinding
	if err := k8sClient.Get(ctx, expectedKey, &created); err != nil {
		t.Fatalf("expected track finding: %v", err)
	}
	assertFindingDeleted(t, ctx, k8sClient, client.ObjectKeyFromObject(stale))

	var currentNode corev1.Node
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(node), &currentNode); err != nil {
		t.Fatal(err)
	}
	currentNode.Status.Allocatable[corev1.ResourceCPU] = resource.MustParse("8")
	if err := k8sClient.Status().Update(ctx, &currentNode); err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.Reconcile(ctx, ctrl.Request{}); err != nil {
		t.Fatalf("update reconcile failed: %v", err)
	}
	var updated yardv1alpha1.DispatchFinding
	if err := k8sClient.Get(ctx, expectedKey, &updated); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated.Spec.Detail, "of 8") {
		t.Fatalf("expected updated capacity detail, got %q", updated.Spec.Detail)
	}

	if err := k8sClient.Delete(ctx, &currentNode); err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.Reconcile(ctx, ctrl.Request{}); err != nil {
		t.Fatalf("cleanup reconcile failed: %v", err)
	}
	assertFindingDeleted(t, ctx, k8sClient, expectedKey)
}

func newControllerTestClient(objects ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(testScheme()).
		WithStatusSubresource(&yardv1alpha1.DispatchFinding{}, &corev1.Pod{}, &corev1.Node{}).
		WithObjects(objects...).
		Build()
}

func controllerPendingPod(name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "api",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodPending},
	}
}

func controllerReadyNode(name string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"kubernetes.io/os":   "linux",
				"kubernetes.io/arch": "arm64",
			},
		},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
			Conditions: []corev1.NodeCondition{{
				Type:   corev1.NodeReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
}

func testFinding(name, category string, subject yardv1alpha1.DispatchFindingSubject) *yardv1alpha1.DispatchFinding {
	return &yardv1alpha1.DispatchFinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: DefaultFindingNamespace,
			Labels:    mergeFindingLabels(nil, category, subject.Kind),
		},
		Spec: yardv1alpha1.DispatchFindingSpec{
			Severity: yardv1alpha1.FindingSeverityInfo,
			Category: category,
			Subject:  subject,
			Summary:  "test finding",
		},
	}
}

func assertFindingDeleted(t *testing.T, ctx context.Context, k8sClient client.Client, key types.NamespacedName) {
	t.Helper()
	var finding yardv1alpha1.DispatchFinding
	err := k8sClient.Get(ctx, key, &finding)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected finding %s to be deleted, got %v", key, err)
	}
}
