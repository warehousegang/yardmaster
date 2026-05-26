package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type FindingSeverity string

const (
	FindingSeverityInfo    FindingSeverity = "info"
	FindingSeverityWarning FindingSeverity = "warning"
	FindingSeverityCritical FindingSeverity = "critical"
)

type DispatchFindingSubject struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
}

type DispatchFindingSpec struct {
	Severity        FindingSeverity        `json:"severity"`
	Category        string                 `json:"category"`
	Subject         DispatchFindingSubject `json:"subject"`
	Summary         string                 `json:"summary"`
	Detail          string                 `json:"detail,omitempty"`
	Recommendations []string               `json:"recommendations,omitempty"`
}

type DispatchFindingStatus struct {
	FirstSeen metav1.Time `json:"firstSeen,omitempty"`
	LastSeen  metav1.Time `json:"lastSeen,omitempty"`
}

type DispatchFinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DispatchFindingSpec   `json:"spec,omitempty"`
	Status DispatchFindingStatus `json:"status,omitempty"`
}

type DispatchFindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []DispatchFinding `json:"items"`
}

func SubjectFromPod(pod *corev1.Pod) DispatchFindingSubject {
	return DispatchFindingSubject{
		APIVersion: "v1",
		Kind:       "Pod",
		Namespace:  pod.Namespace,
		Name:       pod.Name,
	}
}
