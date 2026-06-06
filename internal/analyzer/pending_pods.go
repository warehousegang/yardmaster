package analyzer

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	yardv1alpha1 "github.com/warehousegang/yardmaster/api/v1alpha1"
)

type PendingPodAnalyzer struct{}

type FindingDraft struct {
	Spec yardv1alpha1.DispatchFindingSpec
}

func NewPendingPodAnalyzer() *PendingPodAnalyzer {
	return &PendingPodAnalyzer{}
}

func (a *PendingPodAnalyzer) AnalyzePod(pod *corev1.Pod, nodes []corev1.Node, events []corev1.Event) *FindingDraft {
	if pod == nil || pod.Status.Phase != corev1.PodPending || pod.Spec.NodeName != "" {
		return nil
	}

	detail := schedulerMessage(pod, events)
	if detail == "" {
		detail = "Pod is pending and has not been assigned to a node."
	}

	readyNodes := readySchedulableNodes(nodes)
	if len(readyNodes) == 0 {
		return findingForPod(pod, "Workload cannot schedule because no ready schedulable nodes are available.", detail, []string{
			"Add or recover ready node capacity.",
			"Check cluster autoscaler or Karpenter events for provisioning failures.",
		})
	}

	if missing := missingNodeSelectorTerms(pod, readyNodes); len(missing) > 0 {
		return findingForPod(pod, "Workload cannot schedule on any ready node because its node selector does not match.", fmt.Sprintf("No ready schedulable nodes match nodeSelector terms: %s.", strings.Join(missing, ", ")), []string{
			"Add compatible node capacity with the required labels.",
			"Relax the nodeSelector if it is no longer required.",
		})
	}

	if taints := blockingTaints(pod, readyNodes); len(taints) > 0 {
		return findingForPod(pod, "Workload cannot schedule on compatible nodes because of untolerated taints.", fmt.Sprintf("Ready schedulable nodes are blocked by untolerated taints: %s.", strings.Join(taints, ", ")), []string{
			"Add a matching toleration if this workload is allowed on those nodes.",
			"Schedule the workload onto a node pool without the blocking taints.",
		})
	}

	if shortfalls := resourceShortfalls(pod, readyNodes); len(shortfalls) > 0 {
		return findingForPod(pod, "Workload cannot schedule because no ready node has enough free requested capacity.", fmt.Sprintf("Requested resources exceed currently available allocatable capacity on ready schedulable nodes: %s.", strings.Join(shortfalls, ", ")), []string{
			"Add node capacity that can fit this pod's requests.",
			"Reduce resource requests if they are higher than the workload needs.",
		})
	}

	return findingForPod(pod, "Workload is pending and Kubernetes reports it as unschedulable.", detail, []string{
		"Inspect scheduler events for affinity, topology, volume, or quota constraints.",
		"Compare the pod placement rules with the labels and taints on ready nodes.",
	})
}

func findingForPod(pod *corev1.Pod, summary, detail string, recommendations []string) *FindingDraft {
	return &FindingDraft{
		Spec: yardv1alpha1.DispatchFindingSpec{
			Severity:        yardv1alpha1.FindingSeverityWarning,
			Category:        "scheduling",
			Subject:         yardv1alpha1.SubjectFromPod(pod),
			Summary:         summary,
			Detail:          detail,
			Recommendations: recommendations,
		},
	}
}

func schedulerMessage(pod *corev1.Pod, events []corev1.Event) string {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionFalse {
			if condition.Message != "" {
				return condition.Message
			}
			if condition.Reason != "" {
				return condition.Reason
			}
		}
	}

	for _, event := range events {
		if event.InvolvedObject.Kind == "Pod" &&
			event.InvolvedObject.Namespace == pod.Namespace &&
			event.InvolvedObject.Name == pod.Name &&
			strings.EqualFold(event.Reason, "FailedScheduling") &&
			event.Message != "" {
			return event.Message
		}
	}

	return ""
}

func readySchedulableNodes(nodes []corev1.Node) []corev1.Node {
	var ready []corev1.Node
	for _, node := range nodes {
		if node.Spec.Unschedulable {
			continue
		}
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
				ready = append(ready, node)
				break
			}
		}
	}
	return ready
}

func missingNodeSelectorTerms(pod *corev1.Pod, nodes []corev1.Node) []string {
	if len(pod.Spec.NodeSelector) == 0 {
		return nil
	}

	missing := make(map[string]struct{})
	for key, want := range pod.Spec.NodeSelector {
		matched := false
		for _, node := range nodes {
			if got, ok := node.Labels[key]; ok && got == want {
				matched = true
				break
			}
		}
		if !matched {
			missing[fmt.Sprintf("%s=%s", key, want)] = struct{}{}
		}
	}

	return sortedKeys(missing)
}

func blockingTaints(pod *corev1.Pod, nodes []corev1.Node) []string {
	blockers := make(map[string]struct{})
	for _, node := range nodes {
		if len(pod.Spec.NodeSelector) > 0 && !nodeMatchesSelector(node, pod.Spec.NodeSelector) {
			continue
		}
		for _, taint := range node.Spec.Taints {
			if taint.Effect != corev1.TaintEffectNoSchedule && taint.Effect != corev1.TaintEffectNoExecute {
				continue
			}
			if !toleratesTaint(pod.Spec.Tolerations, taint) {
				blockers[formatTaint(taint)] = struct{}{}
			}
		}
	}
	return sortedKeys(blockers)
}

func resourceShortfalls(pod *corev1.Pod, nodes []corev1.Node) []string {
	requests := podRequests(pod)
	if requests.Cpu().IsZero() && requests.Memory().IsZero() {
		return nil
	}

	var cpuFits, memoryFits bool
	for _, node := range nodes {
		if len(pod.Spec.NodeSelector) > 0 && !nodeMatchesSelector(node, pod.Spec.NodeSelector) {
			continue
		}
		cpu := node.Status.Allocatable.Cpu()
		memory := node.Status.Allocatable.Memory()
		if cpu != nil && cpu.Cmp(*requests.Cpu()) >= 0 {
			cpuFits = true
		}
		if memory != nil && memory.Cmp(*requests.Memory()) >= 0 {
			memoryFits = true
		}
	}

	var shortfalls []string
	if !requests.Cpu().IsZero() && !cpuFits {
		shortfalls = append(shortfalls, fmt.Sprintf("cpu=%s", requests.Cpu().String()))
	}
	if !requests.Memory().IsZero() && !memoryFits {
		shortfalls = append(shortfalls, fmt.Sprintf("memory=%s", requests.Memory().String()))
	}
	return shortfalls
}

func podRequests(pod *corev1.Pod) corev1.ResourceList {
	total := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("0"),
		corev1.ResourceMemory: resource.MustParse("0"),
	}

	for _, container := range pod.Spec.Containers {
		total[corev1.ResourceCPU] = addQuantity(total[corev1.ResourceCPU], container.Resources.Requests[corev1.ResourceCPU])
		total[corev1.ResourceMemory] = addQuantity(total[corev1.ResourceMemory], container.Resources.Requests[corev1.ResourceMemory])
	}

	for _, container := range pod.Spec.InitContainers {
		if cpu := container.Resources.Requests[corev1.ResourceCPU]; cpu.Cmp(total[corev1.ResourceCPU]) > 0 {
			total[corev1.ResourceCPU] = cpu.DeepCopy()
		}
		if memory := container.Resources.Requests[corev1.ResourceMemory]; memory.Cmp(total[corev1.ResourceMemory]) > 0 {
			total[corev1.ResourceMemory] = memory.DeepCopy()
		}
	}

	return total
}

func addQuantity(left, right resource.Quantity) resource.Quantity {
	out := left.DeepCopy()
	out.Add(right)
	return out
}

func nodeMatchesSelector(node corev1.Node, selector map[string]string) bool {
	for key, want := range selector {
		if got, ok := node.Labels[key]; !ok || got != want {
			return false
		}
	}
	return true
}

func toleratesTaint(tolerations []corev1.Toleration, taint corev1.Taint) bool {
	for _, toleration := range tolerations {
		if toleration.Key != taint.Key {
			continue
		}
		switch toleration.Operator {
		case corev1.TolerationOpExists:
			if toleration.Effect == "" || toleration.Effect == taint.Effect {
				return true
			}
		default:
			if toleration.Value == taint.Value && (toleration.Effect == "" || toleration.Effect == taint.Effect) {
				return true
			}
		}
	}
	return false
}

func formatTaint(taint corev1.Taint) string {
	if taint.Value == "" {
		return fmt.Sprintf("%s:%s", taint.Key, taint.Effect)
	}
	return fmt.Sprintf("%s=%s:%s", taint.Key, taint.Value, taint.Effect)
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
