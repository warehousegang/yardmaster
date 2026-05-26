package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	yardv1alpha1 "github.com/warehousegang/yardmaster/api/v1alpha1"
)

type Server struct {
	client           client.Client
	findingNamespace string
	template         *template.Template
}

type Finding struct {
	Name            string   `json:"name"`
	Severity        string   `json:"severity"`
	Category        string   `json:"category"`
	CategoryTitle   string   `json:"categoryTitle"`
	Subject         string   `json:"subject"`
	Summary         string   `json:"summary"`
	Detail          string   `json:"detail"`
	Recommendations []string `json:"recommendations"`
	LastSeen        string   `json:"lastSeen"`
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
	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
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
		template:         template.Must(template.New("dashboard").Parse(pageHTML)),
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/findings", s.handleFindings)
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
		finding := Finding{
			Name:            item.Name,
			Severity:        string(item.Spec.Severity),
			Category:        item.Spec.Category,
			CategoryTitle:   categoryTitle(item.Spec.Category),
			Subject:         subjectLabel(item),
			Summary:         item.Spec.Summary,
			Detail:          item.Spec.Detail,
			Recommendations: item.Spec.Recommendations,
			LastSeen:        timeLabel(item.Status.LastSeen.Time),
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
	subject := finding.Spec.Subject
	if subject.Namespace == "" {
		return strings.ToLower(subject.Kind) + "/" + subject.Name
	}
	return subject.Namespace + "/" + subject.Name
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

const pageHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Yardmaster</title>
  <style>
    :root {
      color-scheme: light;
      --ink: #17211b;
      --muted: #66736b;
      --line: #d9dfd9;
      --panel: #ffffff;
      --wash: #f4f6f2;
      --accent: #2f6f4f;
      --warning: #a85720;
      --info: #26659b;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      color: var(--ink);
      background: var(--wash);
      font: 14px/1.45 ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 24px;
      padding: 18px 28px;
      background: #23372a;
      color: #f6faf5;
      border-bottom: 1px solid #17251c;
    }
    h1 {
      margin: 0;
      font-size: 20px;
      font-weight: 700;
    }
    main {
      max-width: 1180px;
      margin: 0 auto;
      padding: 22px 24px 40px;
    }
    .meta {
      display: flex;
      gap: 18px;
      flex-wrap: wrap;
      color: #d4ddd4;
      font-size: 13px;
    }
    .stats {
      display: grid;
      grid-template-columns: repeat(4, minmax(140px, 1fr));
      gap: 12px;
      margin-bottom: 18px;
    }
    .stat, .finding {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
    }
    .stat {
      padding: 14px 16px;
    }
    .stat b {
      display: block;
      font-size: 24px;
      line-height: 1.1;
      margin-bottom: 4px;
    }
    .stat span {
      color: var(--muted);
      font-size: 13px;
    }
    .section {
      margin-top: 22px;
    }
    .section h2 {
      margin: 0 0 10px;
      font-size: 16px;
    }
    .list {
      display: grid;
      gap: 10px;
    }
    .finding {
      padding: 14px 16px;
    }
    .finding-head {
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      gap: 14px;
      margin-bottom: 8px;
    }
    .subject {
      font-weight: 700;
      overflow-wrap: anywhere;
    }
    .summary {
      margin: 4px 0 0;
      color: var(--ink);
    }
    .detail {
      margin: 8px 0 0;
      color: var(--muted);
    }
    .pill {
      display: inline-flex;
      align-items: center;
      height: 24px;
      padding: 0 9px;
      border-radius: 999px;
      font-size: 12px;
      font-weight: 700;
      text-transform: uppercase;
      letter-spacing: .04em;
      white-space: nowrap;
    }
    .warning { color: #fff; background: var(--warning); }
    .critical { color: #fff; background: #8f1d2c; }
    .info { color: #fff; background: var(--info); }
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
    .error {
      display: none;
      margin-bottom: 14px;
      padding: 12px 14px;
      border: 1px solid #d7a08c;
      background: #fff1ec;
      color: #7b341f;
      border-radius: 8px;
    }
    @media (max-width: 760px) {
      header { align-items: flex-start; flex-direction: column; }
      main { padding: 16px; }
      .stats { grid-template-columns: repeat(2, minmax(0, 1fr)); }
      .finding-head { flex-direction: column; }
    }
  </style>
</head>
<body>
  <header>
    <h1>Yardmaster</h1>
    <div class="meta">
      <span>Namespace: {{ .Namespace }}</span>
      <span id="updated">Loading...</span>
    </div>
  </header>
  <main>
    <div id="error" class="error"></div>
    <section class="stats">
      <div class="stat"><b id="total">0</b><span>Total findings</span></div>
      <div class="stat"><b id="warnings">0</b><span>Warnings</span></div>
      <div class="stat"><b id="requests">0</b><span>Request findings</span></div>
      <div class="stat"><b id="tracks">0</b><span>Node pools</span></div>
    </section>
    <div id="content" class="empty">Loading findings...</div>
  </main>
  <script>
    const categoryOrder = ["scheduling", "requests", "tracks"];
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
        error.textContent = "Could not load findings: " + err.message;
        error.style.display = "block";
      }
    }

    function render(data) {
      document.getElementById("updated").textContent = "Updated: " + new Date(data.updatedAt).toLocaleTimeString();
      document.getElementById("total").textContent = data.counts.total;
      document.getElementById("warnings").textContent = data.counts.warnings;
      document.getElementById("requests").textContent = data.counts.requests;
      document.getElementById("tracks").textContent = data.counts.tracks;

      const content = document.getElementById("content");
      if (!data.findings.length) {
        content.className = "empty";
        content.textContent = "No active findings.";
        return;
      }

      content.className = "";
      const groups = new Map();
      for (const finding of data.findings) {
        if (!groups.has(finding.category)) groups.set(finding.category, []);
        groups.get(finding.category).push(finding);
      }

      const categories = [...groups.keys()].sort((a, b) => {
        const ai = categoryOrder.indexOf(a);
        const bi = categoryOrder.indexOf(b);
        return (ai === -1 ? 99 : ai) - (bi === -1 ? 99 : bi) || a.localeCompare(b);
      });

      content.innerHTML = categories.map((category) => {
        const findings = groups.get(category);
        const title = findings[0].categoryTitle;
        return "<section class=\"section\"><h2>" + escapeHTML(title) + "</h2><div class=\"list\">" + findings.map(renderFinding).join("") + "</div></section>";
      }).join("");
    }

    function renderFinding(finding) {
      const recs = finding.recommendations || [];
      const recommendations = recs.length ? "<ul>" + recs.map((rec) => "<li>" + escapeHTML(rec) + "</li>").join("") + "</ul>" : "";
      const detail = finding.detail ? "<p class=\"detail\">" + escapeHTML(finding.detail) + "</p>" : "";
      return "<article class=\"finding\">" +
        "<div class=\"finding-head\">" +
          "<div>" +
            "<div class=\"subject\">" + escapeHTML(finding.subject) + "</div>" +
            "<p class=\"summary\">" + escapeHTML(finding.summary) + "</p>" +
          "</div>" +
          "<span class=\"pill " + escapeHTML(finding.severity) + "\">" + escapeHTML(finding.severity) + "</span>" +
        "</div>" +
        detail +
        recommendations +
      "</article>";
    }

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
