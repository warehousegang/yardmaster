package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	yardv1alpha1 "github.com/warehousegang/yardmaster/api/v1alpha1"
)

func TestHandlerServesIndexHealthAndFindings(t *testing.T) {
	finding := dashboardFinding(
		"requests-api",
		"yardmaster-system",
		"requests",
		yardv1alpha1.FindingSeverityInfo,
		yardv1alpha1.DispatchFindingSubject{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Namespace:  "default",
			Name:       "api",
		},
	)
	server := newServer(
		fake.NewClientBuilder().WithScheme(dashboardTestScheme()).WithObjects(finding).Build(),
		"yardmaster-system",
	)
	server.logoPath = ""
	handler := server.Handler()

	index := httptest.NewRecorder()
	handler.ServeHTTP(index, httptest.NewRequest(http.MethodGet, "/", nil))
	if index.Code != http.StatusOK {
		t.Fatalf("expected index 200, got %d", index.Code)
	}
	if !strings.Contains(index.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("unexpected content type %q", index.Header().Get("Content-Type"))
	}
	if !strings.Contains(index.Body.String(), "Namespace: yardmaster-system") {
		t.Fatal("expected namespace in rendered page")
	}

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusNoContent {
		t.Fatalf("expected health 204, got %d", health.Code)
	}

	apiResponse := httptest.NewRecorder()
	handler.ServeHTTP(apiResponse, httptest.NewRequest(http.MethodGet, "/api/findings", nil))
	if apiResponse.Code != http.StatusOK {
		t.Fatalf("expected API 200, got %d: %s", apiResponse.Code, apiResponse.Body.String())
	}
	if !strings.Contains(apiResponse.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("unexpected API content type %q", apiResponse.Header().Get("Content-Type"))
	}
	var response FindingsResponse
	if err := json.NewDecoder(apiResponse.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Counts.Total != 1 || len(response.Findings) != 1 {
		t.Fatalf("unexpected API response %#v", response)
	}

	logo := httptest.NewRecorder()
	handler.ServeHTTP(logo, httptest.NewRequest(http.MethodGet, "/assets/logo", nil))
	if logo.Code != http.StatusNotFound {
		t.Fatalf("expected missing logo 404, got %d", logo.Code)
	}
}

func TestFindingsEndpointReturnsServerErrorWhenKubernetesReadFails(t *testing.T) {
	server := newServer(errorReader{}, "yardmaster-system")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/findings", nil))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), "read failed") {
		t.Fatalf("unexpected error body %q", response.Body.String())
	}
}

type errorReader struct{}

func (errorReader) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return errors.New("read failed")
}

func (errorReader) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return errors.New("read failed")
}
