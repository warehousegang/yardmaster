package dashboard

import (
	"context"
	"sort"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	yardv1alpha1 "github.com/warehousegang/yardmaster/api/v1alpha1"
	yardpresentation "github.com/warehousegang/yardmaster/internal/presentation"
)

type Finding struct {
	Name             string   `json:"name"`
	Severity         string   `json:"severity"`
	Category         string   `json:"category"`
	CategoryTitle    string   `json:"categoryTitle"`
	Subject          string   `json:"subject"`
	SubjectKind      string   `json:"subjectKind"`
	SubjectNamespace string   `json:"subjectNamespace"`
	SubjectName      string   `json:"subjectName"`
	Signal           string   `json:"signal"`
	Action           string   `json:"action"`
	Confidence       string   `json:"confidence"`
	Related          []string `json:"related"`
	Summary          string   `json:"summary"`
	Detail           string   `json:"detail"`
	Recommendations  []string `json:"recommendations"`
	LastSeen         string   `json:"lastSeen"`
}

type FindingsResponse struct {
	Namespace string    `json:"namespace"`
	UpdatedAt string    `json:"updatedAt"`
	Counts    Counts    `json:"counts"`
	Findings  []Finding `json:"findings"`
}

type Counts struct {
	Total      int `json:"total"`
	Scheduling int `json:"scheduling"`
	Requests   int `json:"requests"`
	Tracks     int `json:"tracks"`
	Warnings   int `json:"warnings"`
}

func (s *Server) fetchFindings(ctx context.Context) (FindingsResponse, error) {
	var list yardv1alpha1.DispatchFindingList
	if err := s.client.List(ctx, &list, client.InNamespace(s.findingNamespace)); err != nil {
		return FindingsResponse{}, err
	}

	sort.Slice(list.Items, func(i, j int) bool {
		left, right := list.Items[i], list.Items[j]
		if left.Spec.Category != right.Spec.Category {
			return categoryRank(left.Spec.Category) < categoryRank(right.Spec.Category)
		}
		if left.Spec.Severity != right.Spec.Severity {
			return severityRank(left.Spec.Severity) > severityRank(right.Spec.Severity)
		}
		return subjectLabel(left) < subjectLabel(right)
	})

	response := FindingsResponse{
		Namespace: s.findingNamespace,
		UpdatedAt: time.Now().Format(time.RFC3339),
		Findings:  make([]Finding, 0, len(list.Items)),
	}

	for _, item := range list.Items {
		subject := item.Spec.Subject
		finding := Finding{
			Name:             item.Name,
			Severity:         string(item.Spec.Severity),
			Category:         item.Spec.Category,
			CategoryTitle:    categoryTitle(item.Spec.Category),
			Subject:          subjectLabel(item),
			SubjectKind:      subject.Kind,
			SubjectNamespace: subject.Namespace,
			SubjectName:      subject.Name,
			Signal:           signalLabel(item.Spec.Category),
			Action:           actionLabel(item),
			Confidence:       confidenceLabel(item),
			Related:          relatedLabels(item.Spec.Related),
			Summary:          item.Spec.Summary,
			Detail:           item.Spec.Detail,
			Recommendations:  item.Spec.Recommendations,
			LastSeen:         timeLabel(item.Status.LastSeen.Time),
		}
		response.Findings = append(response.Findings, finding)
		response.Counts.Total++
		switch item.Spec.Category {
		case "scheduling":
			response.Counts.Scheduling++
		case "requests":
			response.Counts.Requests++
		case "tracks":
			response.Counts.Tracks++
		}
		if item.Spec.Severity == yardv1alpha1.FindingSeverityWarning || item.Spec.Severity == yardv1alpha1.FindingSeverityCritical {
			response.Counts.Warnings++
		}
	}

	return response, nil
}

func subjectLabel(finding yardv1alpha1.DispatchFinding) string {
	return yardpresentation.SubjectLabel(finding.Spec.Subject)
}

func relatedLabels(subjects []yardv1alpha1.DispatchFindingSubject) []string {
	if len(subjects) == 0 {
		return nil
	}
	labels := make([]string, 0, len(subjects))
	for _, subject := range subjects {
		labels = append(labels, yardpresentation.SubjectLabel(subject))
	}
	return labels
}

func categoryTitle(category string) string {
	switch category {
	case "scheduling":
		return "Scheduling"
	case "requests":
		return "Requests"
	case "tracks":
		return "Node Pools"
	case "":
		return "General"
	default:
		return strings.ToUpper(category[:1]) + category[1:]
	}
}

func signalLabel(category string) string {
	switch category {
	case "scheduling":
		return "Placement blocker"
	case "requests":
		return "Request coverage"
	case "tracks":
		return "Capacity track"
	default:
		return "Capacity finding"
	}
}

func actionLabel(finding yardv1alpha1.DispatchFinding) string {
	switch finding.Spec.Category {
	case "scheduling":
		return "Investigate placement"
	case "requests":
		return "Set requests"
	case "tracks":
		return "Review track"
	default:
		if len(finding.Spec.Recommendations) > 0 {
			return "Review recommendation"
		}
		return "Review"
	}
}

func confidenceLabel(finding yardv1alpha1.DispatchFinding) string {
	if finding.Spec.Detail != "" && len(finding.Spec.Recommendations) > 0 {
		return "high"
	}
	if finding.Spec.Detail != "" {
		return "medium"
	}
	return "observed"
}

func categoryRank(category string) int {
	switch category {
	case "scheduling":
		return 10
	case "requests":
		return 20
	case "tracks":
		return 30
	default:
		return 100
	}
}

func severityRank(severity yardv1alpha1.FindingSeverity) int {
	switch severity {
	case yardv1alpha1.FindingSeverityCritical:
		return 3
	case yardv1alpha1.FindingSeverityWarning:
		return 2
	default:
		return 1
	}
}

func timeLabel(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Local().Format("15:04:05")
}
