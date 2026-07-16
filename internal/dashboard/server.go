package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	yardv1alpha1 "github.com/warehousegang/yardmaster/api/v1alpha1"
	yardpresentation "github.com/warehousegang/yardmaster/internal/presentation"
)

type Server struct {
	client           client.Client
	findingNamespace string
	logoPath         string
	template         *template.Template
}

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

func New(kubeconfig, findingNamespace string) (*Server, error) {
	restConfig, err := dashboardConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(yardv1alpha1.AddToScheme(scheme))

	k8sClient, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}

	return &Server{
		client:           k8sClient,
		findingNamespace: findingNamespace,
		logoPath:         findLogoPath(),
		template:         template.Must(template.New("dashboard").Parse(pageHTML)),
	}, nil
}

func dashboardConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}

	inCluster, err := rest.InClusterConfig()
	if err == nil {
		return inCluster, nil
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/findings", s.handleFindings)
	mux.HandleFunc("/assets/logo", s.handleLogo)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.template.Execute(w, map[string]string{
		"Namespace": s.findingNamespace,
	})
}

func (s *Server) handleFindings(w http.ResponseWriter, r *http.Request) {
	response, err := s.fetchFindings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (s *Server) handleLogo(w http.ResponseWriter, r *http.Request) {
	if s.logoPath == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, s.logoPath)
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

func findLogoPath() string {
	candidates := []string{
		filepath.Join("assets", "yardmaster-logo.png"),
		filepath.Join("assets", "yardmaster.png"),
		filepath.Join(os.Getenv("HOME"), "Downloads", "yardmaster.png"),
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

const pageHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Yardmaster</title>
  <style>
    :root {
      color-scheme: light;
      --ink: #142033;
      --muted: #617089;
      --soft: #34445c;
      --line: #c8d7eb;
      --line-strong: #91acd0;
      --panel: #ffffff;
      --panel-strong: #f7fbff;
      --wash: #eef4fb;
      --blue: #246bfe;
      --blue-soft: #4f93ff;
      --cyan: #0ea5c8;
      --amber: #e5a64b;
      --amber-dark: #a9651f;
      --critical: #d84263;
      --ok: #22a66f;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      color: var(--ink);
      background:
        radial-gradient(circle at 18% -8%, rgba(79, 147, 255, .22), transparent 30%),
        radial-gradient(circle at 90% 8%, rgba(14, 165, 200, .14), transparent 24%),
        linear-gradient(180deg, #f8fbff 0%, #eef4fb 48%, #e6eef8 100%);
      font: 14px/1.45 ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 24px;
      padding: 18px 28px;
      background: rgba(255, 255, 255, .92);
      border-bottom: 1px solid var(--line);
      position: sticky;
      top: 0;
      z-index: 5;
      backdrop-filter: blur(16px);
    }
    .brand {
      display: flex;
      align-items: center;
      gap: 14px;
      min-width: 0;
    }
    .brand img {
      width: 58px;
      height: 58px;
      object-fit: cover;
      border-radius: 8px;
      border: 1px solid #c5d8f2;
      background: #f6f9ff;
    }
    h1 {
      margin: 0;
      font-size: 25px;
      font-weight: 850;
      letter-spacing: 0;
    }
    .tagline {
      margin-top: 2px;
      color: var(--muted);
      font-size: 13px;
    }
    main {
      max-width: 1500px;
      margin: 0 auto;
      padding: 22px 24px 48px;
    }
    .meta {
      display: flex;
      justify-content: flex-end;
      gap: 18px;
      flex-wrap: wrap;
      color: var(--muted);
      font-size: 13px;
    }
    .error {
      display: none;
      margin-bottom: 14px;
      padding: 12px 14px;
      border: 1px solid #f0a9b9;
      background: #fff1f4;
      color: #8f2440;
      border-radius: 8px;
    }
    .control-deck {
      display: grid;
      grid-template-columns: minmax(260px, 1.3fr) repeat(3, minmax(180px, 1fr));
      gap: 12px;
      margin-bottom: 16px;
    }
    .control-card, .yard-board, .finding, .toolbar {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      box-shadow: 0 18px 44px rgba(41, 72, 110, .12);
    }
    .control-card {
      min-height: 122px;
      padding: 16px;
      display: grid;
      align-content: space-between;
      gap: 14px;
    }
    .control-card.primary {
      background:
        linear-gradient(135deg, #dbeaff, #ffffff),
        var(--panel);
    }
    .control-label {
      color: var(--muted);
      font-size: 12px;
      font-weight: 800;
      letter-spacing: .08em;
      text-transform: uppercase;
    }
    .control-value {
      font-size: 30px;
      font-weight: 850;
      line-height: 1.05;
      overflow-wrap: anywhere;
    }
    .control-note {
      color: var(--soft);
      font-size: 13px;
    }
    .metric-row {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 12px;
      color: var(--soft);
      font-size: 13px;
    }
    .meter {
      height: 8px;
      overflow: hidden;
      border-radius: 999px;
      background: #e7eef8;
      border: 1px solid #d6e1ef;
    }
    .meter span {
      display: block;
      height: 100%;
      width: 0%;
      border-radius: inherit;
      background: linear-gradient(90deg, var(--blue), var(--cyan));
    }
    .toolbar {
      display: grid;
      grid-template-columns: minmax(240px, 1.2fr) repeat(3, minmax(150px, .55fr));
      gap: 10px;
      padding: 12px;
      margin-bottom: 16px;
      align-items: end;
    }
    .field {
      display: grid;
      gap: 6px;
    }
    .field label {
      color: var(--muted);
      font-size: 11px;
      font-weight: 800;
      letter-spacing: .08em;
      text-transform: uppercase;
    }
    input, select {
      width: 100%;
      min-height: 38px;
      color: var(--ink);
      background: #ffffff;
      border: 1px solid #c8d7eb;
      border-radius: 8px;
      padding: 8px 10px;
      font: inherit;
    }
    input::placeholder { color: #8795a8; }
    input:focus, select:focus {
      outline: 2px solid rgba(14, 165, 200, .55);
      outline-offset: 2px;
    }
    .yard-board {
      display: grid;
      grid-template-columns: minmax(0, 1fr) 380px;
      gap: 18px;
      padding: 18px;
      margin-bottom: 22px;
      background:
        linear-gradient(135deg, #ffffff, #edf6ff),
        var(--panel-strong);
    }
    .yard-title {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 14px;
      margin-bottom: 14px;
    }
    .yard-title h2, .section h2 {
      margin: 0;
      font-size: 17px;
    }
    .yard-title span, .section-count {
      color: var(--muted);
      font-size: 13px;
    }
    .yard-map {
      min-height: 560px;
      display: grid;
      gap: 18px;
      align-content: start;
      padding: 18px;
      border: 1px solid #c8d7eb;
      border-radius: 8px;
      background:
        linear-gradient(90deg, rgba(36, 107, 254, .10) 1px, transparent 1px) 0 0 / 48px 48px,
        linear-gradient(180deg, rgba(255, 255, 255, .80), rgba(226, 238, 251, .62)),
        #f3f8fe;
    }
    .yard-empty {
      min-height: 260px;
      display: grid;
      align-content: center;
      justify-items: center;
      gap: 8px;
      text-align: center;
      color: var(--muted);
    }
    .yard-empty strong {
      color: var(--ink);
      font-size: 18px;
    }
    .track-lane {
      min-height: 260px;
      display: grid;
      grid-template-columns: 250px minmax(0, 1fr);
      gap: 22px;
      align-items: stretch;
      border: 1px solid #bed2eb;
      border-radius: 8px;
      background: linear-gradient(90deg, #f8fbff, #e8f2fe);
      position: relative;
      overflow: hidden;
      padding: 18px;
    }
    .track-lane:before, .track-lane:after {
      content: "";
      position: absolute;
      left: 302px;
      right: 22px;
      height: 2px;
      background: rgba(80, 118, 166, .34);
    }
    .track-lane:before { top: 72px; }
    .track-lane:after { bottom: 72px; }
    .track-panel {
      display: grid;
      align-content: start;
      gap: 12px;
      position: relative;
      z-index: 1;
    }
    .track-name {
      width: 100%;
      padding: 10px 12px;
      border: 1px solid #c6d7ed;
      border-radius: 8px;
      color: var(--ink);
      background: rgba(255, 255, 255, .86);
      font-weight: 850;
      overflow-wrap: anywhere;
      cursor: pointer;
    }
    .track-kpis {
      display: grid;
      gap: 9px;
    }
    .track-kpi {
      padding: 9px 10px;
      border: 1px solid #d4e1f0;
      border-radius: 8px;
      background: rgba(255, 255, 255, .72);
    }
    .track-kpi strong {
      display: block;
      font-size: 15px;
    }
    .track-kpi span {
      color: var(--muted);
      font-size: 12px;
    }
    .cargo-row {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(176px, 1fr));
      gap: 14px;
      align-content: start;
      position: relative;
      z-index: 1;
    }
    .cargo {
      appearance: none;
      min-height: 92px;
      padding: 12px;
      border-radius: 7px;
      background:
        linear-gradient(180deg, rgba(250, 177, 88, .98), rgba(167, 91, 24, .98));
      border: 1px solid #bf7b31;
      box-shadow: inset 10px 0 0 rgba(255, 255, 255, .16), 0 12px 24px rgba(114, 76, 34, .18);
      color: #071324;
      text-align: left;
      cursor: pointer;
      display: grid;
      align-content: space-between;
      gap: 10px;
    }
    .cargo-title {
      font-size: 13px;
      line-height: 1.2;
      font-weight: 850;
      overflow-wrap: anywhere;
    }
    .cargo-meta {
      display: flex;
      flex-wrap: wrap;
      gap: 5px;
    }
    .cargo-meta span {
      padding: 2px 6px;
      border-radius: 999px;
      background: rgba(255, 255, 255, .30);
      color: #071324;
      font-size: 11px;
      font-weight: 800;
    }
    .dispatch-panel {
      display: grid;
      align-content: start;
      gap: 12px;
    }
    .dispatch-card {
      width: 100%;
      border: 1px solid #c8d7eb;
      border-radius: 8px;
      padding: 13px;
      background: #ffffff;
      color: inherit;
      text-align: left;
    }
    button.dispatch-card {
      cursor: pointer;
    }
    .dispatch-card strong {
      display: block;
      margin-bottom: 5px;
      overflow-wrap: anywhere;
    }
    .dispatch-card p {
      margin: 0;
      color: var(--muted);
      font-size: 13px;
    }
    .dispatch-clear {
      border-color: #a9d9c4;
      background: #ecfbf4;
    }
    .section {
      margin-top: 22px;
    }
    .section-head {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 12px;
      margin-bottom: 12px;
    }
    .list {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(360px, 1fr));
      gap: 12px;
    }
    .finding {
      min-height: 178px;
      padding: 15px;
      display: grid;
      align-content: space-between;
      gap: 12px;
    }
    .finding[role="button"], .track-name[data-finding] {
      cursor: pointer;
    }
    .cargo:hover, button.dispatch-card:hover, .finding[role="button"]:hover, .track-name[data-finding]:hover {
      border-color: #7ea9e8;
      box-shadow: 0 0 0 1px rgba(36, 107, 254, .16), 0 18px 44px rgba(41, 72, 110, .14);
    }
    .cargo:focus-visible, button.dispatch-card:focus-visible, .finding[role="button"]:focus-visible, .track-name[data-finding]:focus-visible, .drawer-close:focus-visible {
      outline: 2px solid var(--cyan);
      outline-offset: 3px;
    }
    .finding-head {
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      gap: 14px;
    }
    .subject {
      font-weight: 850;
      overflow-wrap: anywhere;
    }
    .summary {
      margin: 5px 0 0;
      color: #2b3b52;
    }
    .detail {
      margin: 0;
      color: var(--muted);
    }
    .related {
      display: flex;
      flex-wrap: wrap;
      gap: 6px;
      margin-top: 8px;
    }
    .related span, .chip {
      max-width: 100%;
      padding: 4px 8px;
      color: #1e4d94;
      background: #eef5ff;
      border: 1px solid #c8d7eb;
      border-radius: 999px;
      font-size: 12px;
      overflow-wrap: anywhere;
    }
    .chips {
      display: flex;
      flex-wrap: wrap;
      gap: 7px;
    }
    .pill {
      display: inline-flex;
      align-items: center;
      height: 24px;
      padding: 0 9px;
      border-radius: 999px;
      font-size: 12px;
      font-weight: 850;
      text-transform: uppercase;
      letter-spacing: .04em;
      white-space: nowrap;
    }
    .warning { color: #071324; background: var(--amber); }
    .critical { color: #fff; background: var(--critical); }
    .info { color: #fff; background: var(--blue); }
    .ok { color: #04140e; background: var(--ok); }
    ul {
      margin: 10px 0 0 18px;
      padding: 0;
      color: var(--muted);
    }
    .empty {
      padding: 44px 18px;
      text-align: center;
      color: var(--muted);
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
    }
    .drawer-backdrop {
      position: fixed;
      inset: 0;
      display: none;
      justify-content: flex-end;
      background: rgba(45, 59, 79, .28);
      z-index: 20;
    }
    .drawer-backdrop.open {
      display: flex;
    }
    .drawer {
      width: min(560px, 100vw);
      height: 100vh;
      overflow: auto;
      padding: 24px;
      background: #ffffff;
      border-left: 1px solid var(--line);
      box-shadow: -24px 0 70px rgba(41, 72, 110, .24);
    }
    .drawer-head {
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      gap: 16px;
      margin-bottom: 20px;
    }
    .drawer-title {
      margin: 0;
      font-size: 21px;
      overflow-wrap: anywhere;
    }
    .drawer-meta {
      display: flex;
      flex-wrap: wrap;
      gap: 7px;
      margin-top: 10px;
    }
    .drawer-meta span {
      padding: 4px 8px;
      border: 1px solid #c8d7eb;
      border-radius: 999px;
      color: #1e4d94;
      background: #eef5ff;
      font-size: 12px;
    }
    .drawer-close {
      appearance: none;
      width: 38px;
      height: 38px;
      flex: 0 0 auto;
      border: 1px solid #c8d7eb;
      border-radius: 8px;
      color: var(--ink);
      background: #eef5ff;
      cursor: pointer;
      font-size: 24px;
      line-height: 1;
    }
    .drawer-section {
      margin-top: 20px;
      padding-top: 18px;
      border-top: 1px solid #dde7f4;
    }
    .drawer-section h3 {
      margin: 0 0 9px;
      font-size: 12px;
      color: var(--muted);
      text-transform: uppercase;
      letter-spacing: .08em;
    }
    .drawer-section p {
      margin: 0;
      color: #2b3b52;
    }
    .drawer-grid {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 10px;
    }
    .drawer-tile {
      padding: 11px;
      border: 1px solid #d4e1f0;
      border-radius: 8px;
      background: #f5f9ff;
    }
    .drawer-tile strong {
      display: block;
      font-size: 18px;
    }
    .drawer-tile span {
      color: var(--muted);
      font-size: 12px;
    }
    @media (max-width: 1100px) {
      .control-deck, .toolbar, .yard-board { grid-template-columns: 1fr; }
      .yard-map { min-height: 420px; }
      .track-lane { grid-template-columns: 1fr; }
      .track-lane:before, .track-lane:after { left: 22px; }
      .list { grid-template-columns: 1fr; }
    }
    @media (max-width: 700px) {
      header { align-items: flex-start; flex-direction: column; }
      main { padding: 16px; }
      .control-deck { grid-template-columns: 1fr; }
      .cargo-row { grid-template-columns: 1fr; }
      .drawer { width: 100vw; }
      .drawer-grid { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>
  <header>
    <div class="brand">
      <img src="/assets/logo" alt="Yardmaster logo">
      <div>
        <h1>Yardmaster</h1>
        <div class="tagline">Capacity interpreter for Kubernetes yards</div>
      </div>
    </div>
    <div class="meta">
      <span>Namespace: {{ .Namespace }}</span>
      <span id="updated">Loading...</span>
    </div>
  </header>
  <main>
    <div id="error" class="error"></div>
    <section id="controlDeck" class="control-deck"></section>
    <section class="toolbar" aria-label="Finding filters">
      <div class="field">
        <label for="search">Search Yard</label>
        <input id="search" type="search" placeholder="workload, namespace, reason, recommendation">
      </div>
      <div class="field">
        <label for="namespaceFilter">Namespace</label>
        <select id="namespaceFilter"></select>
      </div>
      <div class="field">
        <label for="categoryFilter">Signal</label>
        <select id="categoryFilter">
          <option value="all">All signals</option>
          <option value="scheduling">Scheduling</option>
          <option value="requests">Requests</option>
          <option value="tracks">Tracks</option>
        </select>
      </div>
      <div class="field">
        <label for="severityFilter">Severity</label>
        <select id="severityFilter">
          <option value="all">All severities</option>
          <option value="critical">Critical</option>
          <option value="warning">Warning</option>
          <option value="info">Info</option>
        </select>
      </div>
    </section>
    <section class="yard-board">
      <div>
        <div class="yard-title">
          <h2>The Yard</h2>
          <span id="yardCaption">Tracks, Cargo, and placement signals</span>
        </div>
        <div id="yardObjects" class="yard-map"></div>
      </div>
      <aside class="dispatch-panel">
        <div class="yard-title">
          <h2>Dispatch</h2>
          <span id="dispatchCount">0 active</span>
        </div>
        <div id="dispatchObjects"></div>
      </aside>
    </section>
    <div id="content" class="empty">Loading findings...</div>
  </main>
  <div id="drawerBackdrop" class="drawer-backdrop" aria-hidden="true">
    <aside class="drawer" role="dialog" aria-modal="true" aria-labelledby="drawerTitle">
      <div class="drawer-head">
        <div>
          <h2 id="drawerTitle" class="drawer-title"></h2>
          <div id="drawerMeta" class="drawer-meta"></div>
        </div>
        <button id="drawerClose" class="drawer-close" type="button" aria-label="Close detail panel">&times;</button>
      </div>
      <div id="drawerTrackSection" class="drawer-section">
        <h3>Track Snapshot</h3>
        <div id="drawerTrackGrid" class="drawer-grid"></div>
      </div>
      <div class="drawer-section">
        <h3>Summary</h3>
        <p id="drawerSummary"></p>
      </div>
      <div id="drawerDetailSection" class="drawer-section">
        <h3>Reason</h3>
        <p id="drawerDetail"></p>
      </div>
      <div id="drawerRelatedSection" class="drawer-section">
        <h3>Related Objects</h3>
        <div id="drawerRelated" class="related"></div>
      </div>
      <div id="drawerRecommendationSection" class="drawer-section">
        <h3>Recommendations</h3>
        <ul id="drawerRecommendations"></ul>
      </div>
    </aside>
  </div>
  <script>
    const categoryOrder = ["scheduling", "requests", "tracks"];
    let currentFindings = [];
    let filterState = { search: "", namespace: "all", category: "all", severity: "all" };
    const escapeHTML = (value) => String(value || "").replace(/[&<>"']/g, (ch) => ({
      "&": "&amp;", "<": "&lt;", ">": "&gt;", "\"": "&quot;", "'": "&#39;"
    }[ch]));

    async function refresh() {
      const error = document.getElementById("error");
      try {
        const response = await fetch("/api/findings", { cache: "no-store" });
        if (!response.ok) throw new Error(await response.text());
        const data = await response.json();
        error.style.display = "none";
        render(data);
      } catch (err) {
        currentFindings = [];
        error.textContent = "Could not load findings: " + err.message;
        error.style.display = "block";
        document.getElementById("updated").textContent = "Cluster offline";
        renderControls([]);
        renderNamespaceOptions([]);
        renderOfflineYard();
        renderFindings([]);
      }
    }

    function render(data) {
      currentFindings = data.findings || [];
      document.getElementById("updated").textContent = "Updated: " + new Date(data.updatedAt).toLocaleTimeString();
      renderNamespaceOptions(currentFindings);
      renderAll();
    }

    function renderAll() {
      const filtered = filteredFindings();
      renderControls(filtered);
      renderYard(filtered);
      renderFindings(filtered);
    }

    function renderControls(findings) {
      const total = findings.length;
      const scheduling = countBy(findings, "category", "scheduling");
      const requests = countBy(findings, "category", "requests");
      const tracks = countBy(findings, "category", "tracks");
      const warnings = findings.filter((finding) => finding.severity === "warning" || finding.severity === "critical").length;
      const requestPct = total ? Math.round((requests / total) * 100) : 0;
      const health = scheduling > 0 ? "Dispatch attention" : requests > 0 ? "Capacity planning risk" : "Yard clear";
      const healthNote = scheduling > 0 ? scheduling + " placement blocker(s) need review" : requests > 0 ? requests + " workload(s) need request coverage" : "No active findings in the current view";

      document.getElementById("controlDeck").innerHTML =
        "<div class=\"control-card primary\">" +
          "<div><div class=\"control-label\">Yard health</div><div class=\"control-value\">" + escapeHTML(health) + "</div></div>" +
          "<div class=\"control-note\">" + escapeHTML(healthNote) + "</div>" +
        "</div>" +
        renderControlCard("Dispatch", String(scheduling), warnings ? warnings + " warning signal(s)" : "No active scheduling blockers", scheduling ? 100 : 0) +
        renderControlCard("Request coverage", String(requests), requestPct + "% of visible findings", requestPct) +
        renderControlCard("Tracks", String(tracks), total + " total visible finding(s)", tracks ? 100 : 0);
    }

    function renderControlCard(label, value, note, percent) {
      return "<div class=\"control-card\">" +
        "<div><div class=\"control-label\">" + escapeHTML(label) + "</div><div class=\"control-value\">" + escapeHTML(value) + "</div></div>" +
        "<div><div class=\"metric-row\"><span>" + escapeHTML(note) + "</span></div><div class=\"meter\"><span style=\"width:" + clamp(percent, 0, 100) + "%\"></span></div></div>" +
      "</div>";
    }

    function renderNamespaceOptions(findings) {
      const select = document.getElementById("namespaceFilter");
      const namespaces = [...new Set(findings.map((finding) => finding.subjectNamespace).filter(Boolean))].sort();
      const current = select.value || filterState.namespace;
      select.innerHTML = "<option value=\"all\">All namespaces</option>" + namespaces.map((namespace) =>
        "<option value=\"" + escapeHTML(namespace) + "\">" + escapeHTML(namespace) + "</option>"
      ).join("");
      select.value = namespaces.includes(current) ? current : "all";
      filterState.namespace = select.value;
    }

    function renderYard(findings) {
      const tracks = findings.filter((finding) => finding.category === "tracks");
      const cargo = findings.filter((finding) => finding.category === "requests");
      const dispatch = findings.filter((finding) => finding.category === "scheduling");
      const yard = document.getElementById("yardObjects");
      const dispatchBox = document.getElementById("dispatchObjects");
      document.getElementById("dispatchCount").textContent = dispatch.length + " active";
      document.getElementById("yardCaption").textContent = cargo.length + " Cargo object(s) on " + Math.max(tracks.length, 1) + " Track view(s)";

      if (!tracks.length && !cargo.length) {
        yard.innerHTML = "<div class=\"yard-empty\"><strong>No Yard objects in this view</strong><p>Adjust filters or wait for Yardmaster to record Track and Cargo findings.</p></div>";
        dispatchBox.innerHTML = renderDispatchClear();
        return;
      }

      const lanes = tracks.length ? tracks : [{ name: "", subject: "track/unassigned", summary: "Cargo without a matching Track summary.", detail: "" }];
      yard.innerHTML = lanes.map((track, index) => {
        const laneCargo = cargo.filter((_, cargoIndex) => cargoIndex % lanes.length === index);
        const trackAttrs = track.name ? " data-finding=\"" + escapeHTML(track.name) + "\" tabindex=\"0\" role=\"button\"" : "";
        const metrics = parseTrackDetail(track.detail || "");
        const blocks = laneCargo.length ? laneCargo.map(renderCargo).join("") : "<div class=\"yard-empty\"><strong>Track clear</strong><p>No Cargo findings assigned to this Track.</p></div>";
        return "<div class=\"track-lane\">" +
          "<div class=\"track-panel\">" +
            "<div class=\"track-name\"" + trackAttrs + ">" + escapeHTML(track.subject) + "</div>" +
            "<div class=\"track-kpis\">" +
              renderTrackKPI(metrics.nodes || "unknown", "nodes") +
              renderTrackKPI(metrics.cpu || "not reported", "CPU requested") +
              renderTrackKPI(metrics.memory || "not reported", "memory requested") +
              renderTrackKPI(laneCargo.length, "Cargo findings") +
            "</div>" +
          "</div>" +
          "<div class=\"cargo-row\">" + blocks + "</div>" +
        "</div>";
      }).join("");

      dispatchBox.innerHTML = dispatch.length ? dispatch.map(renderDispatch).join("") : renderDispatchClear();
    }

    function renderTrackKPI(value, label) {
      return "<div class=\"track-kpi\"><strong>" + escapeHTML(value) + "</strong><span>" + escapeHTML(label) + "</span></div>";
    }

    function renderDispatch(finding) {
      return "<button class=\"dispatch-card\" type=\"button\" data-finding=\"" + escapeHTML(finding.name) + "\">" +
        "<strong>" + escapeHTML(finding.subject) + "</strong>" +
        "<p>" + escapeHTML(finding.summary) + "</p>" +
        "<div class=\"chips\"><span class=\"chip\">" + escapeHTML(finding.action) + "</span><span class=\"chip\">" + escapeHTML(finding.confidence) + " confidence</span></div>" +
      "</button>";
    }

    function renderDispatchClear() {
      return "<div class=\"dispatch-card dispatch-clear\"><strong>Dispatch clear</strong><p>No active scheduling blockers in the current view.</p></div>";
    }

    function renderOfflineYard() {
      document.getElementById("dispatchCount").textContent = "0 active";
      document.getElementById("yardCaption").textContent = "Waiting for DispatchFinding data";
      document.getElementById("yardObjects").innerHTML =
        "<div class=\"yard-empty\"><strong>Cluster offline</strong><p>The dashboard will fill in once Yardmaster can read DispatchFinding resources.</p></div>";
      document.getElementById("dispatchObjects").innerHTML =
        "<div class=\"dispatch-card\"><strong>Waiting for cluster</strong><p>Dispatch findings appear after Yardmaster can reach the Kubernetes API.</p></div>";
    }

    function renderCargo(finding) {
      return "<button class=\"cargo\" type=\"button\" data-finding=\"" + escapeHTML(finding.name) + "\">" +
        "<span class=\"cargo-title\">" + escapeHTML(shortName(finding.subject)) + "</span>" +
        "<span class=\"cargo-meta\"><span>" + escapeHTML(finding.subjectNamespace || "cluster") + "</span><span>" + escapeHTML(finding.action) + "</span></span>" +
      "</button>";
    }

    function renderFindings(findings) {
      const content = document.getElementById("content");
      if (!findings.length) {
        content.className = "empty";
        content.textContent = "No active findings in this view.";
        return;
      }

      content.className = "";
      const groups = new Map();
      for (const finding of findings) {
        if (!groups.has(finding.category)) groups.set(finding.category, []);
        groups.get(finding.category).push(finding);
      }

      const categories = [...groups.keys()].sort((a, b) => {
        const ai = categoryOrder.indexOf(a);
        const bi = categoryOrder.indexOf(b);
        return (ai === -1 ? 99 : ai) - (bi === -1 ? 99 : bi) || a.localeCompare(b);
      });

      content.innerHTML = categories.map((category) => {
        const grouped = groups.get(category);
        const title = grouped[0].categoryTitle;
        return "<section class=\"section\">" +
          "<div class=\"section-head\"><h2>" + escapeHTML(title) + "</h2><span class=\"section-count\">" + grouped.length + " finding(s)</span></div>" +
          "<div class=\"list\">" + grouped.map(renderFinding).join("") + "</div>" +
        "</section>";
      }).join("");
    }

    function renderFinding(finding) {
      const recs = finding.recommendations || [];
      const detail = finding.detail ? "<p class=\"detail\">" + escapeHTML(finding.detail) + "</p>" : "";
      const related = (finding.related || []).length ?
        "<div class=\"related\">" + finding.related.slice(0, 2).map((item) => "<span>" + escapeHTML(item) + "</span>").join("") + "</div>" : "";
      return "<article class=\"finding\" role=\"button\" tabindex=\"0\" data-finding=\"" + escapeHTML(finding.name) + "\">" +
        "<div class=\"finding-head\">" +
          "<div>" +
            "<div class=\"subject\">" + escapeHTML(finding.subject) + "</div>" +
            "<p class=\"summary\">" + escapeHTML(finding.summary) + "</p>" +
          "</div>" +
          "<span class=\"pill " + escapeHTML(finding.severity) + "\">" + escapeHTML(finding.severity) + "</span>" +
        "</div>" +
        detail +
        related +
        "<div class=\"chips\"><span class=\"chip\">" + escapeHTML(finding.signal) + "</span><span class=\"chip\">" + escapeHTML(finding.action) + "</span><span class=\"chip\">" + escapeHTML(finding.confidence) + " confidence</span></div>" +
      "</article>";
    }

    function openFinding(name) {
      const finding = currentFindings.find((item) => item.name === name);
      if (!finding) return;

      document.getElementById("drawerTitle").textContent = finding.subject;
      document.getElementById("drawerSummary").textContent = finding.summary || "";
      document.getElementById("drawerMeta").innerHTML = [
        finding.categoryTitle || finding.category,
        finding.severity,
        finding.action,
        finding.confidence + " confidence",
        finding.lastSeen ? "last seen " + finding.lastSeen : ""
      ].filter(Boolean).map((item) => "<span>" + escapeHTML(item) + "</span>").join("");

      const metrics = parseTrackDetail(finding.detail || "");
      const trackSection = document.getElementById("drawerTrackSection");
      if (finding.category === "tracks") {
        trackSection.style.display = "block";
        document.getElementById("drawerTrackGrid").innerHTML =
          renderDrawerTile(metrics.nodes || "unknown", "nodes") +
          renderDrawerTile(metrics.cpu || "not reported", "CPU requested") +
          renderDrawerTile(metrics.memory || "not reported", "memory requested") +
          renderDrawerTile(metrics.source || "track grouping", "source");
      } else {
        trackSection.style.display = "none";
        document.getElementById("drawerTrackGrid").innerHTML = "";
      }

      const detailSection = document.getElementById("drawerDetailSection");
      detailSection.style.display = finding.detail ? "block" : "none";
      document.getElementById("drawerDetail").textContent = finding.detail || "";

      const related = finding.related || [];
      const relatedSection = document.getElementById("drawerRelatedSection");
      relatedSection.style.display = related.length ? "block" : "none";
      document.getElementById("drawerRelated").innerHTML = related.map((item) => "<span>" + escapeHTML(item) + "</span>").join("");

      const recs = finding.recommendations || [];
      const recSection = document.getElementById("drawerRecommendationSection");
      recSection.style.display = recs.length ? "block" : "none";
      document.getElementById("drawerRecommendations").innerHTML = recs.map((rec) => "<li>" + escapeHTML(rec) + "</li>").join("");

      const backdrop = document.getElementById("drawerBackdrop");
      backdrop.classList.add("open");
      backdrop.setAttribute("aria-hidden", "false");
      document.getElementById("drawerClose").focus();
    }

    function renderDrawerTile(value, label) {
      return "<div class=\"drawer-tile\"><strong>" + escapeHTML(value) + "</strong><span>" + escapeHTML(label) + "</span></div>";
    }

    function closeDrawer() {
      const backdrop = document.getElementById("drawerBackdrop");
      backdrop.classList.remove("open");
      backdrop.setAttribute("aria-hidden", "true");
    }

    function filteredFindings() {
      const search = filterState.search.trim().toLowerCase();
      return currentFindings.filter((finding) => {
        if (filterState.namespace !== "all" && finding.subjectNamespace !== filterState.namespace) return false;
        if (filterState.category !== "all" && finding.category !== filterState.category) return false;
        if (filterState.severity !== "all" && finding.severity !== filterState.severity) return false;
        if (!search) return true;
        const haystack = [
          finding.subject,
          finding.subjectKind,
          finding.subjectNamespace,
          finding.subjectName,
          finding.summary,
          finding.detail,
          finding.action,
          finding.signal,
          (finding.related || []).join(" "),
          (finding.recommendations || []).join(" ")
        ].join(" ").toLowerCase();
        return haystack.includes(search);
      });
    }

    function parseTrackDetail(detail) {
      const sourceMatch = detail.match(/Grouped by ([^.]+)\./);
      const cpuMatch = detail.match(/CPU requested ([^;]+);/);
      const memMatch = detail.match(/memory requested ([^.]+)\./);
      const nodesMatch = detail.match(/Nodes: ([^.]+)\./);
      const nodesText = nodesMatch ? nodesMatch[1].trim() : "";
      return {
        source: sourceMatch ? sourceMatch[1] : "",
        cpu: cpuMatch ? cpuMatch[1] : "",
        memory: memMatch ? memMatch[1] : "",
        nodes: nodesText && nodesText !== "none" ? nodesText.split(",").filter(Boolean).length : "0"
      };
    }

    function countBy(findings, key, value) {
      return findings.filter((finding) => finding[key] === value).length;
    }

    function shortName(subject) {
      const parts = String(subject || "").split("/");
      return parts[parts.length - 1] || subject;
    }

    function clamp(value, min, max) {
      return Math.max(min, Math.min(max, Number(value) || 0));
    }

    document.addEventListener("input", (event) => {
      if (event.target.id === "search") {
        filterState.search = event.target.value;
        renderAll();
      }
    });

    document.addEventListener("change", (event) => {
      if (event.target.id === "namespaceFilter") filterState.namespace = event.target.value;
      if (event.target.id === "categoryFilter") filterState.category = event.target.value;
      if (event.target.id === "severityFilter") filterState.severity = event.target.value;
      renderAll();
    });

    document.addEventListener("click", (event) => {
      const close = event.target.closest("#drawerClose");
      if (close || event.target.id === "drawerBackdrop") {
        closeDrawer();
        return;
      }
      const trigger = event.target.closest("[data-finding]");
      if (trigger) openFinding(trigger.dataset.finding);
    });

    document.addEventListener("keydown", (event) => {
      if (event.key === "Escape") {
        closeDrawer();
        return;
      }
      if ((event.key === "Enter" || event.key === " ") && event.target.matches("[data-finding]")) {
        event.preventDefault();
        openFinding(event.target.dataset.finding);
      }
    });

    refresh();
    setInterval(refresh, 5000);
  </script>
</body>
</html>`

func ListenAndServe(ctx context.Context, addr string, server *Server) error {
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		errc <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errc:
		if err == http.ErrServerClosed {
			return nil
		}
		return fmt.Errorf("dashboard server failed: %w", err)
	}
}
