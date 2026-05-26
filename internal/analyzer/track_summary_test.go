package analyzer

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTrackSummaryGroupsByKnownNodePoolLabels(t *testing.T) {
	node := readyNode("node-1")
	node.Labels[TrackLabelKarpenterNodePool] = "general"
	node.Status.Allocatable = corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("4"),
		corev1.ResourceMemory: resource.MustParse("8Gi"),
	}

	pod := scheduledPod("api", "node-1", "500m", "1Gi")

	findings := NewTrackSummaryAnalyzer().Analyze([]corev1.Node{node}, []corev1.Pod{pod})
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].TrackName != "karpenter/general" {
		t.Fatalf("expected karpenter track, got %q", findings[0].TrackName)
	}
	if !strings.Contains(findings[0].Spec.Detail, "CPU requested 500m of 4") {
		t.Fatalf("expected cpu summary, got %q", findings[0].Spec.Detail)
	}
	if !strings.Contains(findings[0].Spec.Detail, "memory requested 1Gi of 8Gi") {
		t.Fatalf("expected memory summary, got %q", findings[0].Spec.Detail)
	}
}

func TestTrackSummaryFallsBackToNodeShape(t *testing.T) {
	node := readyNode("node-1")
	node.Labels["node.kubernetes.io/instance-type"] = "m7i.large"
	node.Labels["topology.kubernetes.io/zone"] = "us-east-1a"
	node.Labels["kubernetes.io/os"] = "linux"
	node.Labels["kubernetes.io/arch"] = "arm64"

	findings := NewTrackSummaryAnalyzer().Analyze([]corev1.Node{node}, nil)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].TrackName != "shape/m7i.large/us-east-1a/linux/arm64" {
		t.Fatalf("expected shape fallback, got %q", findings[0].TrackName)
	}
}

func TestTrackSummaryIgnoresUnscheduledPods(t *testing.T) {
	node := readyNode("node-1")
	pod := pendingPod()

	findings := NewTrackSummaryAnalyzer().Analyze([]corev1.Node{node}, []corev1.Pod{pod})
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if !strings.Contains(findings[0].Spec.Summary, "0 scheduled pod(s)") {
		t.Fatalf("expected no scheduled pods, got %q", findings[0].Spec.Summary)
	}
}

func scheduledPod(name, nodeName, cpu, memory string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Containers: []corev1.Container{{
				Name: name,
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse(cpu),
						corev1.ResourceMemory: resource.MustParse(memory),
					},
				},
			}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}
}
