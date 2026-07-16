package dashboard

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	yardv1alpha1 "github.com/warehousegang/yardmaster/api/v1alpha1"
)

func TestFetchFindingsSortsFiltersAndCounts(t *testing.T) {
	lastSeen := metav1.NewTime(time.Date(2026, time.July, 16, 12, 30, 0, 0, time.UTC))
	scheduling := dashboardFinding(
		"scheduling-api",
		"yardmaster-system",
		"scheduling",
		yardv1alpha1.FindingSeverityWarning,
		yardv1alpha1.DispatchFindingSubject{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Namespace:  "default",
			Name:       "api",
		},
	)
	scheduling.Spec.Detail = "No ready nodes match."
	scheduling.Spec.Recommendations = []string{"Add compatible capacity."}
	scheduling.Spec.Related = []yardv1alpha1.DispatchFindingSubject{{
		APIVersion: "v1",
		Kind:       "Pod",
		Namespace:  "default",
		Name:       "api-123",
	}}
	scheduling.Status.LastSeen = lastSeen

	requests := dashboardFinding(
		"requests-worker",
		"yardmaster-system",
		"requests",
		yardv1alpha1.FindingSeverityInfo,
		yardv1alpha1.DispatchFindingSubject{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Namespace:  "workers",
			Name:       "worker",
		},
	)
	tracks := dashboardFinding(
		"track-general",
		"yardmaster-system",
		"tracks",
		yardv1alpha1.FindingSeverityCritical,
		yardv1alpha1.DispatchFindingSubject{
			APIVersion: yardv1alpha1.GroupVersion.String(),
			Kind:       "Track",
			Name:       "karpenter/general",
		},
	)
	otherNamespace := dashboardFinding(
		"ignored",
		"other-system",
		"scheduling",
		yardv1alpha1.FindingSeverityWarning,
		yardv1alpha1.DispatchFindingSubject{
			APIVersion: "v1",
			Kind:       "Pod",
			Namespace:  "default",
			Name:       "ignored",
		},
	)

	server := newServer(
		fake.NewClientBuilder().
			WithScheme(dashboardTestScheme()).
			WithObjects(scheduling, requests, tracks, otherNamespace).
			Build(),
		"yardmaster-system",
	)

	response, err := server.fetchFindings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Findings) != 3 {
		t.Fatalf("expected 3 findings, got %d", len(response.Findings))
	}
	if got := []string{
		response.Findings[0].Category,
		response.Findings[1].Category,
		response.Findings[2].Category,
	}; got[0] != "scheduling" || got[1] != "requests" || got[2] != "tracks" {
		t.Fatalf("unexpected category order %v", got)
	}
	if response.Counts != (Counts{Total: 3, Scheduling: 1, Requests: 1, Tracks: 1, Warnings: 2}) {
		t.Fatalf("unexpected counts %#v", response.Counts)
	}

	first := response.Findings[0]
	if first.Subject != "deployment/default/api" {
		t.Fatalf("unexpected subject %q", first.Subject)
	}
	if len(first.Related) != 1 || first.Related[0] != "pod/default/api-123" {
		t.Fatalf("unexpected related subjects %v", first.Related)
	}
	if first.Action != "Investigate placement" || first.Confidence != "high" {
		t.Fatalf("unexpected presentation fields action=%q confidence=%q", first.Action, first.Confidence)
	}
	if first.LastSeen == "" {
		t.Fatal("expected formatted lastSeen")
	}
}

func dashboardFinding(
	name string,
	namespace string,
	category string,
	severity yardv1alpha1.FindingSeverity,
	subject yardv1alpha1.DispatchFindingSubject,
) *yardv1alpha1.DispatchFinding {
	return &yardv1alpha1.DispatchFinding{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: yardv1alpha1.DispatchFindingSpec{
			Severity: severity,
			Category: category,
			Subject:  subject,
			Summary:  name,
		},
	}
}

func dashboardTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(yardv1alpha1.AddToScheme(scheme))
	return scheme
}
