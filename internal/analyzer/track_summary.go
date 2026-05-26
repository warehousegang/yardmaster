package analyzer

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	yardv1alpha1 "github.com/warehousegang/yardmaster/api/v1alpha1"
)

const (
	TrackLabelKarpenterNodePool = "karpenter.sh/nodepool"
	TrackLabelEKSNodeGroup      = "eks.amazonaws.com/nodegroup"
	TrackLabelGKENodePool       = "cloud.google.com/gke-nodepool"
	TrackLabelAzureAgentPool    = "kubernetes.azure.com/agentpool"
)

type TrackSummaryAnalyzer struct{}

type TrackFindingDraft struct {
	TrackName string
	Spec      yardv1alpha1.DispatchFindingSpec
}

type trackAccumulator struct {
	name           string
	source         string
	nodeCount      int
	allocatableCPU resource.Quantity
	allocatableMem resource.Quantity
	requestedCPU   resource.Quantity
	requestedMem   resource.Quantity
	requestingPods int
	nodeNames      map[string]struct{}
}

func NewTrackSummaryAnalyzer() *TrackSummaryAnalyzer {
	return &TrackSummaryAnalyzer{}
}

func (a *TrackSummaryAnalyzer) Analyze(nodes []corev1.Node, pods []corev1.Pod) []TrackFindingDraft {
	tracks := make(map[string]*trackAccumulator)
	nodeToTrack := make(map[string]string)

	for _, node := range nodes {
		if !nodeReady(node) {
			continue
		}
		name, source := trackNameForNode(node)
		track := tracks[name]
		if track == nil {
			track = &trackAccumulator{
				name:           name,
				source:         source,
				allocatableCPU: resource.MustParse("0"),
				allocatableMem: resource.MustParse("0"),
				requestedCPU:   resource.MustParse("0"),
				requestedMem:   resource.MustParse("0"),
				nodeNames:      make(map[string]struct{}),
			}
			tracks[name] = track
		}

		track.nodeCount++
		track.nodeNames[node.Name] = struct{}{}
		track.allocatableCPU = addQuantity(track.allocatableCPU, *node.Status.Allocatable.Cpu())
		track.allocatableMem = addQuantity(track.allocatableMem, *node.Status.Allocatable.Memory())
		nodeToTrack[node.Name] = name
	}

	for _, pod := range pods {
		if pod.Spec.NodeName == "" || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		trackName, ok := nodeToTrack[pod.Spec.NodeName]
		if !ok {
			continue
		}
		track := tracks[trackName]
		requests := podRequests(&pod)
		track.requestedCPU = addQuantity(track.requestedCPU, *requests.Cpu())
		track.requestedMem = addQuantity(track.requestedMem, *requests.Memory())
		track.requestingPods++
	}

	names := make([]string, 0, len(tracks))
	for name := range tracks {
		names = append(names, name)
	}
	sort.Strings(names)

	findings := make([]TrackFindingDraft, 0, len(names))
	for _, name := range names {
		track := tracks[name]
		findings = append(findings, TrackFindingDraft{
			TrackName: name,
			Spec: yardv1alpha1.DispatchFindingSpec{
				Severity: yardv1alpha1.FindingSeverityInfo,
				Category: "tracks",
				Subject: yardv1alpha1.DispatchFindingSubject{
					APIVersion: yardv1alpha1.GroupVersion.String(),
					Kind:       "Track",
					Name:       name,
				},
				Summary: fmt.Sprintf("Track %s has %d ready node(s) and %d scheduled pod(s).", name, track.nodeCount, track.requestingPods),
				Detail:  trackDetail(track),
				Recommendations: []string{
					"Compare requested percentages with workload priority and expected burst capacity.",
					"Investigate placement constraints when one Track is much hotter than the rest.",
				},
			},
		})
	}

	return findings
}

func trackNameForNode(node corev1.Node) (string, string) {
	knownLabels := []struct {
		key    string
		prefix string
	}{
		{TrackLabelKarpenterNodePool, "karpenter"},
		{TrackLabelEKSNodeGroup, "eks"},
		{TrackLabelGKENodePool, "gke"},
		{TrackLabelAzureAgentPool, "aks"},
	}

	for _, label := range knownLabels {
		if value := node.Labels[label.key]; value != "" {
			return label.prefix + "/" + value, label.key
		}
	}

	instanceType := firstLabelValue(node.Labels, "node.kubernetes.io/instance-type", "beta.kubernetes.io/instance-type")
	if instanceType == "" {
		instanceType = "unknown-instance"
	}
	zone := firstLabelValue(node.Labels, "topology.kubernetes.io/zone", "failure-domain.beta.kubernetes.io/zone")
	if zone == "" {
		zone = "unknown-zone"
	}
	os := node.Labels["kubernetes.io/os"]
	if os == "" {
		os = "unknown-os"
	}
	arch := node.Labels["kubernetes.io/arch"]
	if arch == "" {
		arch = "unknown-arch"
	}

	return fmt.Sprintf("shape/%s/%s/%s/%s", instanceType, zone, os, arch), "node-shape"
}

func trackDetail(track *trackAccumulator) string {
	cpuPct := percent(track.requestedCPU, track.allocatableCPU)
	memPct := percent(track.requestedMem, track.allocatableMem)
	return fmt.Sprintf(
		"Grouped by %s. CPU requested %s of %s (%d%%); memory requested %s of %s (%d%%). Nodes: %s.",
		track.source,
		track.requestedCPU.String(),
		track.allocatableCPU.String(),
		cpuPct,
		track.requestedMem.String(),
		track.allocatableMem.String(),
		memPct,
		strings.Join(sortedSet(track.nodeNames), ", "),
	)
}

func nodeReady(node corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func percent(used, total resource.Quantity) int {
	if total.IsZero() {
		return 0
	}
	usedMilli := used.MilliValue()
	totalMilli := total.MilliValue()
	if totalMilli == 0 {
		return 0
	}
	return int((usedMilli * 100) / totalMilli)
}

func firstLabelValue(labels map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := labels[key]; value != "" {
			return value
		}
	}
	return ""
}

func sortedSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
