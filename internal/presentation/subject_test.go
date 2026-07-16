package presentation

import (
	"testing"

	yardv1alpha1 "github.com/warehousegang/yardmaster/api/v1alpha1"
)

func TestSubjectLabel(t *testing.T) {
	tests := []struct {
		name    string
		subject yardv1alpha1.DispatchFindingSubject
		want    string
	}{
		{
			name: "namespaced subject",
			subject: yardv1alpha1.DispatchFindingSubject{
				Kind:      "Pod",
				Namespace: "default",
				Name:      "worker",
			},
			want: "pod/default/worker",
		},
		{
			name: "cluster scoped subject",
			subject: yardv1alpha1.DispatchFindingSubject{
				Kind: "Node",
				Name: "node-1",
			},
			want: "node/node-1",
		},
		{
			name: "missing kind",
			subject: yardv1alpha1.DispatchFindingSubject{
				Name: "unknown",
			},
			want: "object/unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SubjectLabel(tt.subject); got != tt.want {
				t.Fatalf("SubjectLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}
