package presentation

import (
	"strings"

	yardv1alpha1 "github.com/warehousegang/yardmaster/api/v1alpha1"
)

func SubjectLabel(subject yardv1alpha1.DispatchFindingSubject) string {
	kind := strings.ToLower(subject.Kind)
	if kind == "" {
		kind = "object"
	}
	if subject.Namespace == "" {
		return kind + "/" + subject.Name
	}
	return kind + "/" + subject.Namespace + "/" + subject.Name
}
