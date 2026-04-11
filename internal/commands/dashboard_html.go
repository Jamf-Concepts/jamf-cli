// Copyright 2026, Jamf Software LLC

package commands

import (
	_ "embed"
	"html/template"
	"io"
	"sort"
	"strings"
)

//go:embed chartjs.min.js
var chartJSMinified string

const dashboardTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{.Title}}</title>
<style>
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:#f8fafc;color:#1e293b;line-height:1.5}
a{color:#0073EC;text-decoration:none}
a:hover{text-decoration:underline}

.header{background:#0c101a;color:#e2e8f0;padding:2rem 2rem 1.5rem}
.header h1{font-size:1.5rem;font-weight:700;margin-bottom:.25rem}
.header .meta{font-size:.85rem;color:#94a3b8;margin-bottom:1rem}
.header .profiles{display:flex;gap:.5rem;flex-wrap:wrap;margin-bottom:1rem}
.badge{display:inline-block;padding:.2rem .6rem;border-radius:9999px;font-size:.75rem;font-weight:600;color:#fff}
.badge-pro{background:#0073EC}
.badge-protect{background:#0E9F6E}
.badge-platform{background:#8B5CF6}
.toggle-bar{display:flex;gap:.5rem}
.toggle-btn{background:#1e293b;color:#94a3b8;border:1px solid #334155;border-radius:.375rem;padding:.35rem .75rem;cursor:pointer;font-size:.8rem;font-weight:500;transition:all .15s}
.toggle-btn:hover{color:#e2e8f0;border-color:#475569}
.toggle-btn.active{background:#0073EC;color:#fff;border-color:#0073EC}

.container{max-width:1200px;margin:0 auto;padding:1.5rem}
.section{background:#fff;border-radius:.75rem;box-shadow:0 1px 3px rgba(0,0,0,.06);margin-bottom:1.5rem;overflow:hidden}
.section-header{display:flex;align-items:center;justify-content:space-between;padding:1rem 1.5rem;cursor:pointer;user-select:none;border-bottom:1px solid #e2e8f0}
.section-header:hover{background:#f8fafc}
.section-header h2{font-size:1.1rem;font-weight:600;display:flex;align-items:center;gap:.5rem}
.section-header .chevron{transition:transform .2s;font-size:.8rem;color:#94a3b8}
.section.collapsed .section-body{display:none}
.section.collapsed .chevron{transform:rotate(-90deg)}
.section-body{padding:1.5rem}
.section-badges{display:flex;gap:.5rem}

.stat-grid{display:grid;grid-template-columns:repeat(4,1fr);gap:1rem}
.stat-card{background:#f3f7ff;border-radius:.5rem;padding:1.25rem;text-align:center}
.stat-card.muted{background:#f1f5f9}
.stat-card.alert{background:#fef2f2;border:1px solid #fecaca}
.stat-card .stat-value{font-size:2rem;font-weight:700;color:#0c101a}
.stat-card.alert .stat-value{color:#dc2626}
.stat-card .stat-label{font-size:.8rem;color:#64748b;font-weight:500;margin-top:.25rem}

.chart-grid{display:grid;grid-template-columns:repeat(4,1fr);gap:1.5rem}
.chart-item{text-align:center}
.chart-item canvas{max-width:160px;margin:0 auto}
.chart-item .chart-label{font-size:.85rem;font-weight:600;margin-top:.5rem;color:#334155}
.chart-item .chart-pct{font-size:.8rem;color:#64748b}
.bar-chart-wrap{max-width:700px}
.bar-chart-wrap canvas{width:100%;max-height:300px}

table{width:100%;border-collapse:collapse;font-size:.85rem}
th{text-align:left;padding:.6rem .75rem;background:#f8fafc;border-bottom:2px solid #e2e8f0;font-weight:600;color:#475569;font-size:.75rem;text-transform:uppercase;letter-spacing:.03em}
td{padding:.6rem .75rem;border-bottom:1px solid #f1f5f9}
tr:hover td{background:#f8fafc}

.sev-dot{display:inline-block;width:8px;height:8px;border-radius:50%;margin-right:.35rem;vertical-align:middle}
.sev-dot.critical{background:#dc2626}
.sev-dot.warning{background:#d97706}
.sev-dot.info{background:#2563eb}
.sev-badge{display:inline-block;padding:.15rem .5rem;border-radius:9999px;font-size:.7rem;font-weight:600}
.sev-badge.critical{background:#fef2f2;color:#dc2626}
.sev-badge.warning{background:#fffbeb;color:#d97706}
.sev-badge.info{background:#eff6ff;color:#2563eb}

.compliance-bar{display:inline-flex;align-items:center;gap:.5rem;width:100%}
.compliance-bar .bar-track{flex:1;height:8px;background:#e2e8f0;border-radius:4px;overflow:hidden}
.compliance-bar .bar-fill{height:100%;border-radius:4px;transition:width .3s}
.compliance-bar .bar-text{font-size:.8rem;font-weight:600;min-width:3rem;text-align:right}

.deploy-badge{display:inline-block;padding:.15rem .5rem;border-radius:.25rem;font-size:.7rem;font-weight:600;text-transform:capitalize}
.deploy-badge.active{background:#dcfce7;color:#166534}
.deploy-badge.inactive{background:#f1f5f9;color:#475569}
.deploy-badge.draft{background:#fef9c3;color:#854d0e}

.filter-bar{display:flex;gap:.5rem;margin-bottom:1rem}
.filter-btn{background:#f1f5f9;border:1px solid #e2e8f0;border-radius:.375rem;padding:.3rem .65rem;cursor:pointer;font-size:.75rem;font-weight:500;transition:all .15s}
.filter-btn:hover{border-color:#94a3b8}
.filter-btn.active{background:#0073EC;color:#fff;border-color:#0073EC}

.detail-only{display:none}

.footer{text-align:center;padding:2rem;color:#94a3b8;font-size:.8rem}

@media(max-width:768px){
  .stat-grid,.chart-grid{grid-template-columns:repeat(2,1fr)}
  .header{padding:1.5rem 1rem}
  .container{padding:1rem}
  .section-body{padding:1rem}
  table{font-size:.78rem}
  th,td{padding:.5rem}
}

@media print{
  .toggle-bar,.filter-bar{display:none!important}
  .section{break-inside:avoid;box-shadow:none;border:1px solid #e2e8f0}
  .header{background:#0c101a;-webkit-print-color-adjust:exact;print-color-adjust:exact}
  .detail-only{display:revert!important}
}
</style>
</head>
<body>

<div class="header">
  <h1>{{.Title}}</h1>
  <div class="meta">Generated {{.GeneratedAt.Format "Jan 02, 2006 at 3:04 PM"}} &middot; jamf-cli {{.CLIVersion}}</div>
  <div class="profiles">
    {{range .Profiles}}<span class="badge badge-{{.Product}}">{{.Product}} &mdash; {{.Name}}</span>{{end}}
  </div>
  <div class="toggle-bar">
    <button class="toggle-btn active" onclick="toggleView('summary')">Summary</button>
    <button class="toggle-btn" onclick="toggleView('detail')">Detail</button>
  </div>
</div>

<div class="container">

{{if .Fleet}}
<div class="section" id="fleet">
  <div class="section-header" onclick="toggleSection(this)">
    <h2><span class="chevron">&#9660;</span> Fleet Summary</h2>
  </div>
  <div class="section-body">
    <div class="stat-grid">
      <div class="stat-card">
        <div class="stat-value">{{.Fleet.ManagedComputers}}</div>
        <div class="stat-label">Managed Computers</div>
      </div>
      <div class="stat-card">
        <div class="stat-value">{{.Fleet.ManagedMobile}}</div>
        <div class="stat-label">Managed Mobile</div>
      </div>
      <div class="stat-card">
        <div class="stat-value">{{.Fleet.Users}}</div>
        <div class="stat-label">Users</div>
      </div>
      <div class="stat-card muted">
        <div class="stat-value">{{.Fleet.UnmanagedComputers}}</div>
        <div class="stat-label">Unmanaged Computers</div>
      </div>
      <div class="stat-card muted">
        <div class="stat-value">{{.Fleet.UnmanagedMobile}}</div>
        <div class="stat-label">Unmanaged Mobile</div>
      </div>
    </div>
  </div>
</div>
{{end}}

{{if .Security}}
<div class="section" id="security">
  <div class="section-header" onclick="toggleSection(this)">
    <h2><span class="chevron">&#9660;</span> Security Posture</h2>
  </div>
  <div class="section-body">
    <div class="chart-grid">
      <div class="chart-item">
        <canvas id="chart-filevault" width="160" height="160"></canvas>
        <div class="chart-label">FileVault</div>
        <div class="chart-pct">{{printf "%.0f" (.Security.Pct .Security.FileVaultEnabled)}}%</div>
      </div>
      <div class="chart-item">
        <canvas id="chart-firewall" width="160" height="160"></canvas>
        <div class="chart-label">Firewall</div>
        <div class="chart-pct">{{printf "%.0f" (.Security.Pct .Security.FirewallEnabled)}}%</div>
      </div>
      <div class="chart-item">
        <canvas id="chart-gatekeeper" width="160" height="160"></canvas>
        <div class="chart-label">Gatekeeper</div>
        <div class="chart-pct">{{printf "%.0f" (.Security.Pct .Security.GatekeeperEnabled)}}%</div>
      </div>
      <div class="chart-item">
        <canvas id="chart-sip" width="160" height="160"></canvas>
        <div class="chart-label">SIP</div>
        <div class="chart-pct">{{printf "%.0f" (.Security.Pct .Security.SIPEnabled)}}%</div>
      </div>
    </div>
  </div>
</div>
{{end}}

{{if .Audit}}
<div class="section" id="audit">
  <div class="section-header" onclick="toggleSection(this)">
    <h2><span class="chevron">&#9660;</span> Audit Findings</h2>
    <div class="section-badges">
      {{if .Audit.CriticalCount}}<span class="sev-badge critical">{{.Audit.CriticalCount}} Critical</span>{{end}}
      {{if .Audit.WarningCount}}<span class="sev-badge warning">{{.Audit.WarningCount}} Warning</span>{{end}}
      {{if .Audit.InfoCount}}<span class="sev-badge info">{{.Audit.InfoCount}} Info</span>{{end}}
    </div>
  </div>
  <div class="section-body">
    <div class="filter-bar detail-only">
      <button class="filter-btn active" onclick="filterAudit('all')">All</button>
      <button class="filter-btn" onclick="filterAudit('critical')">Critical</button>
      <button class="filter-btn" onclick="filterAudit('warning')">Warning</button>
      <button class="filter-btn" onclick="filterAudit('info')">Info</button>
    </div>
    <table>
      <thead><tr><th>Severity</th><th>Category</th><th>Check</th><th>Affected</th><th class="detail-only">Recommendation</th></tr></thead>
      <tbody>
        {{range .Audit.Results}}
        <tr data-severity="{{toLower .Severity}}">
          <td><span class="sev-dot {{toLower .Severity}}"></span>{{.Severity}}</td>
          <td>{{.Category}}</td>
          <td>{{.Name}}</td>
          <td>{{.AffectedCount}}</td>
          <td class="detail-only">{{.Recommendation}}</td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>
</div>
{{end}}

{{if .Patch}}
<div class="section" id="patch">
  <div class="section-header" onclick="toggleSection(this)">
    <h2><span class="chevron">&#9660;</span> Patch Compliance</h2>
  </div>
  <div class="section-body">
    <div class="bar-chart-wrap detail-only" style="margin-bottom:1.5rem">
      <canvas id="chart-patch" height="200"></canvas>
    </div>
    <table>
      <thead><tr><th>Title</th><th>Latest Version</th><th>Up to Date</th><th>Out of Date</th><th>Compliance</th></tr></thead>
      <tbody>
        {{range .Patch.Titles}}
        <tr>
          <td>{{.Name}}</td>
          <td>{{.LatestVersion}}</td>
          <td>{{.UpToDate}}</td>
          <td>{{.OutOfDate}}</td>
          <td>
            <div class="compliance-bar">
              <div class="bar-track"><div class="bar-fill" style="width:{{printf "%.0f" .CompliancePct}}%;background:{{if ge .CompliancePct 90.0}}#16a34a{{else if ge .CompliancePct 70.0}}#d97706{{else}}#dc2626{{end}}"></div></div>
              <span class="bar-text">{{printf "%.0f" .CompliancePct}}%</span>
            </div>
          </td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>
</div>
{{end}}

{{if .Devices}}
<div class="section" id="devices">
  <div class="section-header" onclick="toggleSection(this)">
    <h2><span class="chevron">&#9660;</span> Device Compliance</h2>
  </div>
  <div class="section-body">
    <div class="stat-grid">
      <div class="stat-card{{if gt .Devices.StaleDevices 0}} alert{{end}}">
        <div class="stat-value">{{.Devices.StaleDevices}}</div>
        <div class="stat-label">Stale Devices (&gt;{{.Devices.StaleThresholdDays}}d)</div>
      </div>
      <div class="stat-card{{if gt .Devices.FailedMDMCommands 0}} alert{{end}}">
        <div class="stat-value">{{.Devices.FailedMDMCommands}}</div>
        <div class="stat-label">Failed MDM Commands</div>
      </div>
    </div>
  </div>
</div>
{{end}}

{{if .OSDist}}
<div class="section" id="osdist">
  <div class="section-header" onclick="toggleSection(this)">
    <h2><span class="chevron">&#9660;</span> OS Distribution</h2>
  </div>
  <div class="section-body">
    <div class="bar-chart-wrap">
      <canvas id="chart-os" height="250"></canvas>
    </div>
  </div>
</div>
{{end}}

{{if .Protect}}
<div class="section" id="protect">
  <div class="section-header" onclick="toggleSection(this)">
    <h2><span class="chevron">&#9660;</span> Jamf Protect</h2>
  </div>
  <div class="section-body">
    <div class="stat-grid">
      <div class="stat-card">
        <div class="stat-value">{{.Protect.Plans}}</div>
        <div class="stat-label">Plans</div>
      </div>
      <div class="stat-card">
        <div class="stat-value">{{.Protect.Endpoints}}</div>
        <div class="stat-label">Endpoints</div>
      </div>
      <div class="stat-card">
        <div class="stat-value">{{.Protect.AnalyticsActive}} / {{.Protect.AnalyticsTotal}}</div>
        <div class="stat-label">Analytics Active</div>
      </div>
      <div class="stat-card">
        <div class="stat-value">{{.Protect.AnalyticSets}}</div>
        <div class="stat-label">Analytic Sets</div>
      </div>
      <div class="stat-card">
        <div class="stat-value">{{.Protect.ExceptionSets}}</div>
        <div class="stat-label">Exception Sets</div>
      </div>
    </div>
  </div>
</div>
{{end}}

{{if .Platform}}
<div class="section" id="platform">
  <div class="section-header" onclick="toggleSection(this)">
    <h2><span class="chevron">&#9660;</span> Jamf Platform</h2>
  </div>
  <div class="section-body">
    {{if .Platform.Blueprints}}
    <h3 style="font-size:.95rem;font-weight:600;margin-bottom:.75rem">Blueprints</h3>
    <table style="margin-bottom:1.5rem">
      <thead><tr><th>Name</th><th>Deployment State</th></tr></thead>
      <tbody>
        {{range .Platform.Blueprints}}
        <tr>
          <td>{{.Name}}</td>
          <td><span class="deploy-badge {{toLower .DeploymentState}}">{{.DeploymentState}}</span></td>
        </tr>
        {{end}}
      </tbody>
    </table>
    {{end}}
    {{if .Platform.Benchmarks}}
    <h3 style="font-size:.95rem;font-weight:600;margin-bottom:.75rem">Benchmarks</h3>
    <table>
      <thead><tr><th>Title</th><th>Compliance</th><th>Failing Rules</th></tr></thead>
      <tbody>
        {{range .Platform.Benchmarks}}
        <tr>
          <td>{{.Title}}</td>
          <td>
            <div class="compliance-bar">
              <div class="bar-track"><div class="bar-fill" style="width:{{printf "%.0f" .CompliancePct}}%;background:{{if ge .CompliancePct 90.0}}#16a34a{{else if ge .CompliancePct 70.0}}#d97706{{else}}#dc2626{{end}}"></div></div>
              <span class="bar-text">{{printf "%.0f" .CompliancePct}}%</span>
            </div>
          </td>
          <td>{{.FailingRules}}</td>
        </tr>
        {{end}}
      </tbody>
    </table>
    {{end}}
  </div>
</div>
{{end}}

</div>

<div class="footer">
  Generated by jamf-cli {{.CLIVersion}} &middot; {{.GeneratedAt.Format "2006-01-02T15:04:05Z07:00"}}
</div>

<script>
` + "{{.ChartJS}}" + `
</script>
<script>
(function(){
  "use strict";

  // Toggle section collapse
  window.toggleSection = function(header) {
    header.parentElement.classList.toggle("collapsed");
  };

  // Toggle summary/detail view
  window.toggleView = function(mode) {
    var btns = document.querySelectorAll(".toggle-btn");
    btns.forEach(function(b){ b.classList.remove("active"); });
    var idx = mode === "summary" ? 0 : 1;
    if (btns[idx]) btns[idx].classList.add("active");

    var els = document.querySelectorAll(".detail-only");
    els.forEach(function(el){
      el.style.display = mode === "detail" ? "" : "none";
    });
  };

  // Filter audit table rows
  window.filterAudit = function(severity) {
    var btns = document.querySelectorAll(".filter-btn");
    btns.forEach(function(b){ b.classList.remove("active"); });
    // Find the clicked button
    btns.forEach(function(b){
      if (b.textContent.toLowerCase().replace(/\s/g,"") === severity.toLowerCase().replace(/\s/g,"")) {
        b.classList.add("active");
      }
    });

    var rows = document.querySelectorAll("#audit tbody tr");
    rows.forEach(function(row){
      if (severity === "all") {
        row.style.display = "";
      } else {
        row.style.display = row.getAttribute("data-severity") === severity ? "" : "none";
      }
    });
  };

  function pctColor(pct) {
    if (pct >= 90) return "#16a34a";
    if (pct >= 70) return "#d97706";
    return "#dc2626";
  }

  function createDoughnut(canvasId, enabled, disabled) {
    var canvas = document.getElementById(canvasId);
    if (!canvas) return;
    var total = enabled + disabled;
    var pct = total > 0 ? Math.round(enabled / total * 100) : 0;
    var color = pctColor(pct);
    new Chart(canvas, {
      type: "doughnut",
      data: {
        labels: ["Enabled", "Disabled"],
        datasets: [{
          data: [enabled, disabled],
          backgroundColor: [color, "#e2e8f0"],
          borderWidth: 0
        }]
      },
      options: {
        cutout: "70%",
        responsive: false,
        plugins: {
          legend: { display: false },
          tooltip: { enabled: true }
        }
      },
      plugins: [{
        id: "centerText",
        afterDraw: function(chart) {
          var ctx = chart.ctx;
          var w = chart.width;
          var h = chart.height;
          ctx.save();
          ctx.font = "bold 1.3rem -apple-system, sans-serif";
          ctx.fillStyle = "#1e293b";
          ctx.textAlign = "center";
          ctx.textBaseline = "middle";
          ctx.fillText(pct + "%", w / 2, h / 2);
          ctx.restore();
        }
      }]
    });
  }

  function createBarChart(canvasId, labels, values) {
    var canvas = document.getElementById(canvasId);
    if (!canvas) return;
    new Chart(canvas, {
      type: "bar",
      data: {
        labels: labels,
        datasets: [{
          data: values,
          backgroundColor: "#0073EC",
          borderRadius: 4
        }]
      },
      options: {
        indexAxis: "y",
        responsive: true,
        maintainAspectRatio: false,
        plugins: { legend: { display: false } },
        scales: {
          x: { beginAtZero: true, grid: { color: "#f1f5f9" } },
          y: { grid: { display: false } }
        }
      }
    });
  }

  function createHorizontalBar(canvasId, labels, values) {
    var canvas = document.getElementById(canvasId);
    if (!canvas) return;
    var colors = values.map(function(v){ return pctColor(v); });
    new Chart(canvas, {
      type: "bar",
      data: {
        labels: labels,
        datasets: [{
          data: values,
          backgroundColor: colors,
          borderRadius: 4
        }]
      },
      options: {
        indexAxis: "y",
        responsive: true,
        maintainAspectRatio: false,
        plugins: { legend: { display: false } },
        scales: {
          x: { min: 0, max: 100, grid: { color: "#f1f5f9" }, ticks: { callback: function(v){ return v + "%"; } } },
          y: { grid: { display: false } }
        }
      }
    });
  }

  // Initialize charts on load
  document.addEventListener("DOMContentLoaded", function() {
    // Default to summary view
    toggleView("summary");

    {{if .Security}}
    createDoughnut("chart-filevault", {{.Security.FileVaultEnabled}}, {{.Security.Total}} - {{.Security.FileVaultEnabled}});
    createDoughnut("chart-firewall", {{.Security.FirewallEnabled}}, {{.Security.Total}} - {{.Security.FirewallEnabled}});
    createDoughnut("chart-gatekeeper", {{.Security.GatekeeperEnabled}}, {{.Security.Total}} - {{.Security.GatekeeperEnabled}});
    createDoughnut("chart-sip", {{.Security.SIPEnabled}}, {{.Security.Total}} - {{.Security.SIPEnabled}});
    {{end}}

    {{if .OSDist}}
    createBarChart("chart-os",
      [{{range $i, $v := .OSDist.Versions}}{{if $i}},{{end}}"{{$v.Version}}"{{end}}],
      [{{range $i, $v := .OSDist.Versions}}{{if $i}},{{end}}{{$v.Count}}{{end}}]
    );
    {{end}}

    {{if .Patch}}
    createHorizontalBar("chart-patch",
      [{{range $i, $t := .Patch.Titles}}{{if $i}},{{end}}"{{$t.Name}}"{{end}}],
      [{{range $i, $t := .Patch.Titles}}{{if $i}},{{end}}{{printf "%.1f" $t.CompliancePct}}{{end}}]
    );
    {{end}}
  });
})();
</script>

</body>
</html>`

func renderDashboard(w io.Writer, data *DashboardData) error {
	// Sort OS distribution by count descending, cap at top 10
	if data.OSDist != nil {
		versions := make([]osVersionCount, len(data.OSDist.Versions))
		copy(versions, data.OSDist.Versions)
		sort.Slice(versions, func(i, j int) bool {
			return versions[i].Count > versions[j].Count
		})
		if len(versions) > 10 {
			other := 0
			for _, v := range versions[10:] {
				other += v.Count
			}
			versions = append(versions[:10], osVersionCount{Version: "Other", Count: other})
		}
		data.OSDist.Versions = versions
	}

	// Sort patch titles by CompliancePct ascending (worst first)
	if data.Patch != nil {
		sort.Slice(data.Patch.Titles, func(i, j int) bool {
			return data.Patch.Titles[i].CompliancePct < data.Patch.Titles[j].CompliancePct
		})
	}

	// Sort audit results by severity: CRITICAL -> WARNING -> INFO
	if data.Audit != nil {
		sevOrder := map[string]int{
			severityCritical: 0,
			severityWarning:  1,
			severityInfo:     2,
		}
		sort.SliceStable(data.Audit.Results, func(i, j int) bool {
			return sevOrder[data.Audit.Results[i].Severity] < sevOrder[data.Audit.Results[j].Severity]
		})
	}

	funcMap := template.FuncMap{
		"toLower": strings.ToLower,
	}

	tmpl, err := template.New("dashboard").Funcs(funcMap).Parse(dashboardTemplate)
	if err != nil {
		return err
	}

	type templateData struct {
		*DashboardData
		ChartJS template.JS
	}

	td := templateData{
		DashboardData: data,
		ChartJS:       template.JS(chartJSMinified), //nolint:gosec // Chart.js is a vendored trusted asset
	}

	return tmpl.Execute(w, td)
}
