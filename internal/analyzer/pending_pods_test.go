package analyzer

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAnalyzePodExplainsMissingNodeSelector(t *testing.T) {
	pod := pendingPod()
	pod.Spec.NodeSelector = map[string]string{"workload": "api"}

	finding := NewPendingPodAnalyzer().AnalyzePod(&pod, []corev1.Node{readyNode("node-1")}, nil)
	if finding == nil {
		t.Fatal("expected finding")
	}
	if !strings.Contains(finding.Spec.Detail, "workload=api") {
		t.Fatalf("expected missing selector detail, got %q", finding.Spec.Detail)
	}
}

func TestAnalyzePodExplainsUntoleratedTaint(t *testing.T) {
	node := readyNode("node-1")
	node.Spec.Taints = []corev1.Taint{{
		Key:    "workload",
		Value:  "batch",
		Effect: corev1.TaintEffectNoSchedule,
	}}
	pod := pendingPod()

	finding := NewPendingPodAnalyzer().AnalyzePod(&pod, []corev1.Node{node}, nil)
	if finding == nil {
		t.Fatal("expected finding")
	}
	if !strings.Contains(finding.Spec.Detail, "workload=batch:NoSchedule") {
		t.Fatalf("expected taint detail, got %q", finding.Spec.Detail)
	}
}

func TestAnalyzePodExplainsNoReadyNodes(t *testing.T) {
	node := readyNode("node-1")
	node.Status.Conditions = []corev1.NodeCondition{{
		Type:   corev1.NodeReady,
		Status: corev1.ConditionFalse,
	}}

	finding := NewPendingPodAnalyzer().AnalyzePod(ptr(pendingPod()), []corev1.Node{node}, nil)
	if finding == nil {
		t.Fatal("expected finding")
	}
	if !strings.Contains(finding.Spec.Summary, "no ready schedulable nodes") {
		t.Fatalf("expected no ready node summary, got %q", finding.Spec.Summary)
	}
}

func TestAnalyzePodSkipsRunningPod(t *testing.T) {
	pod := pendingPod()
	pod.Status.Phase = corev1.PodRunning

	finding := NewPendingPodAnalyzer().AnalyzePod(&pod, []corev1.Node{readyNode("node-1")}, nil)
	if finding != nil {
		t.Fatalf("expected no finding, got %#v", finding)
	}
}

func pendingPod() corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-123",
			Namespace: "default",
		},
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
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}
}

func readyNode(name string) corev1.Node {
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"kubernetes.io/os": "linux"},
		},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
			Conditions: []corev1.NodeCondition{{
				Type:   corev1.NodeReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
}

func ptr[T any](value T) *T {
	return &value
}
