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
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	yardv1alpha1 "github.com/warehousegang/yardmaster/api/v1alpha1"
)

type Server struct {
	client           client.Client
	findingNamespace string
	logoPath         string
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
		logoPath:         findLogoPath(),
		template:         template.Must(template.New("dashboard").Parse(pageHTML)),
	}, nil
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
      color-scheme: dark;
      --ink: #f4f8ff;
      --muted: #99abc4;
      --line: rgba(120, 163, 255, .22);
      --panel: rgba(9, 23, 45, .86);
      --panel-strong: rgba(12, 34, 69, .94);
      --wash: #06101f;
      --blue: #2677ff;
      --blue-soft: #55a2ff;
      --cyan: #26d3ff;
      --warning: #d48a35;
      --critical: #e45b76;
      --info: #2d85ff;
      --ok: #2fc78a;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      color: var(--ink);
      background:
        radial-gradient(circle at 48% -18%, rgba(43, 119, 255, .28), transparent 34%),
        linear-gradient(180deg, #071324 0%, #06101f 42%, #08182b 100%);
      font: 14px/1.45 ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 24px;
      padding: 18px 28px;
      background: rgba(4, 12, 26, .88);
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
      width: 52px;
      height: 52px;
      object-fit: cover;
      border-radius: 8px;
      border: 1px solid rgba(85, 162, 255, .32);
      background: #020712;
    }
    h1 {
      margin: 0;
      font-size: 22px;
      font-weight: 800;
      letter-spacing: 0;
    }
    .tagline {
      margin-top: 2px;
      color: var(--muted);
      font-size: 13px;
    }
    main {
      max-width: 1280px;
      margin: 0 auto;
      padding: 22px 24px 44px;
    }
    .meta {
      display: flex;
      justify-content: flex-end;
      gap: 18px;
      flex-wrap: wrap;
      color: var(--muted);
      font-size: 13px;
    }
    .stats {
      display: grid;
      grid-template-columns: repeat(4, minmax(150px, 1fr));
      gap: 12px;
      margin-bottom: 18px;
    }
    .stat, .finding, .yard-board {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      box-shadow: 0 18px 54px rgba(0, 0, 0, .22);
    }
    .stat {
      padding: 14px 16px;
    }
    .stat b {
      display: block;
      font-size: 26px;
      line-height: 1.1;
      margin-bottom: 4px;
    }
    .stat span {
      color: var(--muted);
      font-size: 13px;
    }
    .yard-board {
      display: grid;
      grid-template-columns: minmax(0, 1fr) 320px;
      gap: 18px;
      padding: 18px;
      margin-bottom: 22px;
      background:
        linear-gradient(135deg, rgba(16, 45, 86, .92), rgba(4, 16, 34, .94)),
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
      font-size: 16px;
    }
    .yard-title span {
      color: var(--muted);
      font-size: 13px;
    }
    .yard-map {
      min-height: 300px;
      display: grid;
      gap: 12px;
      align-content: start;
      padding: 14px;
      border: 1px solid rgba(85, 162, 255, .18);
      border-radius: 8px;
      background:
        linear-gradient(90deg, rgba(85, 162, 255, .08) 1px, transparent 1px) 0 0 / 42px 42px,
        rgba(3, 12, 27, .46);
    }
    .track-lane {
      min-height: 92px;
      border: 1px solid rgba(85, 162, 255, .26);
      border-radius: 8px;
      background: linear-gradient(90deg, rgba(16, 44, 86, .92), rgba(7, 24, 52, .82));
      position: relative;
      overflow: hidden;
      padding: 12px 14px 12px 146px;
    }
    .track-lane:before, .track-lane:after {
      content: "";
      position: absolute;
      left: 16px;
      right: 16px;
      height: 2px;
      background: rgba(194, 218, 255, .45);
    }
    .track-lane:before { top: 29px; }
    .track-lane:after { bottom: 29px; }
    .track-name {
      position: absolute;
      left: 14px;
      top: 13px;
      max-width: 118px;
      font-weight: 800;
      overflow-wrap: anywhere;
      color: #e9f3ff;
    }
    .track-detail {
      position: absolute;
      left: 14px;
      bottom: 13px;
      max-width: 118px;
      color: var(--muted);
      font-size: 12px;
    }
    .cargo-row {
      display: flex;
      gap: 8px;
      flex-wrap: wrap;
      align-items: center;
      min-height: 66px;
      position: relative;
      z-index: 1;
    }
    .cargo {
      width: 78px;
      min-height: 48px;
      padding: 7px 8px;
      border-radius: 4px;
      background: linear-gradient(180deg, #1f7bff, #0d43b5);
      border: 1px solid rgba(151, 197, 255, .62);
      box-shadow: inset 8px 0 0 rgba(255, 255, 255, .10);
      font-size: 11px;
      font-weight: 700;
      overflow-wrap: anywhere;
    }
    .cargo.warning {
      background: linear-gradient(180deg, #f0a24b, #ad5f19);
      border-color: rgba(255, 211, 154, .78);
    }
    .dispatch-panel {
      display: grid;
      align-content: start;
      gap: 12px;
    }
    .dispatch-card {
      border: 1px solid rgba(85, 162, 255, .23);
      border-radius: 8px;
      padding: 12px;
      background: rgba(4, 14, 31, .66);
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
    .section {
      margin-top: 22px;
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
      font-weight: 800;
      overflow-wrap: anywhere;
    }
    .summary {
      margin: 4px 0 0;
      color: #dce9ff;
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
      font-weight: 800;
      text-transform: uppercase;
      letter-spacing: .04em;
      white-space: nowrap;
    }
    .warning { color: #071324; background: var(--warning); }
    .critical { color: #fff; background: var(--critical); }
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
      border: 1px solid rgba(228, 91, 118, .5);
      background: rgba(79, 19, 36, .72);
      color: #ffdce4;
      border-radius: 8px;
    }
    @media (max-width: 900px) {
      header { align-items: flex-start; flex-direction: column; }
      main { padding: 16px; }
      .stats { grid-template-columns: repeat(2, minmax(0, 1fr)); }
      .yard-board { grid-template-columns: 1fr; }
      .track-lane { padding-left: 14px; padding-top: 82px; }
      .track-name, .track-detail { max-width: calc(100% - 28px); }
      .track-detail { top: 42px; bottom: auto; }
      .finding-head { flex-direction: column; }
      .meta { justify-content: flex-start; }
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
    <section class="stats">
      <div class="stat"><b id="total">0</b><span>Total findings</span></div>
      <div class="stat"><b id="warnings">0</b><span>Dispatch warnings</span></div>
      <div class="stat"><b id="requests">0</b><span>Cargo request gaps</span></div>
      <div class="stat"><b id="tracks">0</b><span>Tracks</span></div>
    </section>
    <section class="yard-board">
      <div>
        <div class="yard-title">
          <h2>The Yard</h2>
          <span>Tracks and Cargo</span>
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
        document.getElementById("updated").textContent = "Cluster offline";
        document.getElementById("total").textContent = "0";
        document.getElementById("warnings").textContent = "0";
        document.getElementById("requests").textContent = "0";
        document.getElementById("tracks").textContent = "0";
        renderYard([]);
        renderFindings([]);
      }
    }

    function render(data) {
      document.getElementById("updated").textContent = "Updated: " + new Date(data.updatedAt).toLocaleTimeString();
      document.getElementById("total").textContent = data.counts.total;
      document.getElementById("warnings").textContent = data.counts.warnings;
      document.getElementById("requests").textContent = data.counts.requests;
      document.getElementById("tracks").textContent = data.counts.tracks;
      renderYard(data.findings || []);
      renderFindings(data.findings || []);
    }

    function renderYard(findings) {
      const tracks = findings.filter((finding) => finding.category === "tracks");
      const cargo = findings.filter((finding) => finding.category === "requests");
      const dispatch = findings.filter((finding) => finding.category === "scheduling");
      const yard = document.getElementById("yardObjects");
      const dispatchBox = document.getElementById("dispatchObjects");
      document.getElementById("dispatchCount").textContent = dispatch.length + " active";

      const lanes = tracks.length ? tracks : [{ subject: "track/unassigned", summary: "No Track findings yet.", detail: "Start the controller to summarize node pools." }];
      yard.innerHTML = lanes.map((track, index) => {
        const laneCargo = cargo.filter((_, cargoIndex) => cargoIndex % lanes.length === index);
        const blocks = laneCargo.length ? laneCargo.map(renderCargo).join("") : "<div class=\"cargo\">clear</div>";
        return "<div class=\"track-lane\">" +
          "<div class=\"track-name\">" + escapeHTML(track.subject) + "</div>" +
          "<div class=\"track-detail\">" + escapeHTML(track.summary) + "</div>" +
          "<div class=\"cargo-row\">" + blocks + "</div>" +
        "</div>";
      }).join("");

      dispatchBox.innerHTML = dispatch.length ? dispatch.map((finding) =>
        "<div class=\"dispatch-card\">" +
          "<strong>" + escapeHTML(finding.subject) + "</strong>" +
          "<p>" + escapeHTML(finding.summary) + "</p>" +
        "</div>"
      ).join("") : "<div class=\"dispatch-card\"><strong>Dispatch clear</strong><p>No active scheduling blockers.</p></div>";
    }

    function renderCargo(finding) {
      return "<div class=\"cargo warning\">" + escapeHTML(shortName(finding.subject)) + "</div>";
    }

    function renderFindings(findings) {
      const content = document.getElementById("content");
      if (!findings.length) {
        content.className = "empty";
        content.textContent = "No active findings.";
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
        return "<section class=\"section\"><h2>" + escapeHTML(title) + "</h2><div class=\"list\">" + grouped.map(renderFinding).join("") + "</div></section>";
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

    function shortName(subject) {
      const parts = String(subject || "").split("/");
      return parts[parts.length - 1] || subject;
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
