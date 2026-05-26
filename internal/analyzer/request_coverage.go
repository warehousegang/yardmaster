package analyzer

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	yardv1alpha1 "github.com/warehousegang/yardmaster/api/v1alpha1"
)

type RequestCoverageAnalyzer struct{}

func NewRequestCoverageAnalyzer() *RequestCoverageAnalyzer {
	return &RequestCoverageAnalyzer{}
}

func (a *RequestCoverageAnalyzer) AnalyzePod(pod *corev1.Pod) *FindingDraft {
	if pod == nil || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		return nil
	}

	missing := missingRequests(pod)
	if len(missing) == 0 {
		return nil
	}

	return &FindingDraft{
		Spec: yardv1alpha1.DispatchFindingSpec{
			Severity: yardv1alpha1.FindingSeverityInfo,
			Category: "requests",
			Subject:  yardv1alpha1.SubjectFromPod(pod),
			Summary:  "Pod has containers without CPU or memory requests.",
			Detail:   fmt.Sprintf("Missing requests: %s.", strings.Join(missing, ", ")),
			Recommendations: []string{
				"Set CPU and memory requests for each container so scheduling reflects actual capacity needs.",
				"Use recent workload usage metrics to choose requests before relying on this pod for capacity planning.",
			},
		},
	}
}

func missingRequests(pod *corev1.Pod) []string {
	missing := make([]string, 0)
	for _, container := range pod.Spec.InitContainers {
		missing = append(missing, missingRequestsForContainer("init container", container)...)
	}
	for _, container := range pod.Spec.Containers {
		missing = append(missing, missingRequestsForContainer("container", container)...)
	}
	return missing
}

func missingRequestsForContainer(kind string, container corev1.Container) []string {
	var missing []string
	if request, ok := container.Resources.Requests[corev1.ResourceCPU]; !ok || request.IsZero() {
		missing = append(missing, fmt.Sprintf("%s %s cpu", kind, container.Name))
	}
	if request, ok := container.Resources.Requests[corev1.ResourceMemory]; !ok || request.IsZero() {
		missing = append(missing, fmt.Sprintf("%s %s memory", kind, container.Name))
	}
	return missing
}
