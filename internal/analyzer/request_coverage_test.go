package analyzer

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestRequestCoverageFindsMissingCPUAndMemoryRequests(t *testing.T) {
	pod := pendingPod()
	pod.Spec.Containers = []corev1.Container{
		{
			Name: "api",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
			},
		},
		{Name: "worker"},
	}

	finding := NewRequestCoverageAnalyzer().AnalyzePod(&pod)
	if finding == nil {
		t.Fatal("expected finding")
	}

	for _, want := range []string{"container api cpu", "container worker cpu", "container worker memory"} {
		if !strings.Contains(finding.Spec.Detail, want) {
			t.Fatalf("expected detail to include %q, got %q", want, finding.Spec.Detail)
		}
	}
}

func TestRequestCoverageSkipsFullyRequestedPod(t *testing.T) {
	pod := pendingPod()

	finding := NewRequestCoverageAnalyzer().AnalyzePod(&pod)
	if finding != nil {
		t.Fatalf("expected no finding, got %#v", finding)
	}
}

func TestRequestCoverageTreatsZeroRequestsAsMissing(t *testing.T) {
	pod := pendingPod()
	pod.Spec.Containers[0].Resources.Requests = corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("0"),
		corev1.ResourceMemory: resource.MustParse("0"),
	}

	finding := NewRequestCoverageAnalyzer().AnalyzePod(&pod)
	if finding == nil {
		t.Fatal("expected finding")
	}
	if !strings.Contains(finding.Spec.Detail, "container api cpu") || !strings.Contains(finding.Spec.Detail, "container api memory") {
		t.Fatalf("expected zero request detail, got %q", finding.Spec.Detail)
	}
}

func TestRequestCoverageSkipsTerminalPods(t *testing.T) {
	pod := pendingPod()
	pod.Spec.Containers = []corev1.Container{{Name: "api"}}
	pod.Status.Phase = corev1.PodSucceeded

	finding := NewRequestCoverageAnalyzer().AnalyzePod(&pod)
	if finding != nil {
		t.Fatalf("expected no finding, got %#v", finding)
	}
}
