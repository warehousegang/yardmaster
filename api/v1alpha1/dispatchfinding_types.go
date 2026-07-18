package v1alpha1
// found at https://pkg.go.dev/k8s.io
import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type FindingSeverity string

const (
	FindingSeverityInfo     FindingSeverity = "info"
	FindingSeverityWarning  FindingSeverity = "warning"
	FindingSeverityCritical FindingSeverity = "critical"
)

type DispatchFindingSubject struct {
	// +required
	APIVersion string `json:"apiVersion"`
	// +required
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	// +required
	Name string `json:"name"`
}

type DispatchFindingSpec struct {
	// +kubebuilder:validation:Enum=info;warning;critical
	// +required
	Severity FindingSeverity `json:"severity"`
	// +required
	Category string `json:"category"`
	// +required
	Subject DispatchFindingSubject   `json:"subject"`
	Related []DispatchFindingSubject `json:"related,omitempty"`
	// +required
	Summary         string   `json:"summary"`
	Detail          string   `json:"detail,omitempty"`
	Recommendations []string `json:"recommendations,omitempty"`
}

type DispatchFindingStatus struct {
	FirstSeen metav1.Time `json:"firstSeen,omitempty"`
	LastSeen  metav1.Time `json:"lastSeen,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=df
// +kubebuilder:printcolumn:name="Severity",type=string,JSONPath=".spec.severity"
// +kubebuilder:printcolumn:name="Category",type=string,JSONPath=".spec.category"
// +kubebuilder:printcolumn:name="Subject",type=string,JSONPath=".spec.subject.name"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"
type DispatchFinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DispatchFindingSpec   `json:"spec,omitempty"`
	Status DispatchFindingStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
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
