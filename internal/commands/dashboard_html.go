// Copyright 2026, Jamf Software LLC

package commands

import (
	"html/template"
	"io"
	"sort"
	"strconv"
	"strings"
)

const dashboardTemplate = `<!DOCTYPE html>
<html lang="en" data-theme="dark">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{.Title}}</title>
<style>
/* ── Theme ────────────────────────────────── */
:root,[data-theme="dark"]{
  --bg:#0b0f19;--bg2:#111827;--card:rgba(255,255,255,0.035);--card-hover:rgba(255,255,255,0.06);
  --border:rgba(255,255,255,0.06);--border2:rgba(255,255,255,0.12);
  --text:#e2e8f0;--text2:#8892a4;--text3:#4a5568;
  --green:#10b981;--amber:#f59e0b;--red:#ef4444;--blue:#3b82f6;--purple:#8b5cf6;--teal:#14b8a6;
  --ring-track:rgba(255,255,255,0.06);--bar-track:rgba(255,255,255,0.06);
  --stripe:rgba(255,255,255,0.02);--header-bg:#060910;
  --glow1:rgba(59,130,246,0.07);--glow2:rgba(139,92,246,0.05);
  --alert-bg:rgba(239,68,68,0.08);--alert-border:rgba(239,68,68,0.2);
}
[data-theme="light"]{
  --bg:#f0f2f5;--bg2:#ffffff;--card:#ffffff;--card-hover:#f8fafc;
  --border:#e2e8f0;--border2:#cbd5e1;
  --text:#0f172a;--text2:#475569;--text3:#94a3b8;
  --green:#059669;--amber:#d97706;--red:#dc2626;--blue:#2563eb;--purple:#7c3aed;--teal:#0d9488;
  --ring-track:#e5e7eb;--bar-track:#e5e7eb;
  --stripe:#f8fafc;--header-bg:#0c101a;
  --glow1:rgba(59,130,246,0.04);--glow2:rgba(139,92,246,0.03);
  --alert-bg:rgba(239,68,68,0.06);--alert-border:rgba(239,68,68,0.15);
}

/* ── Reset & Base ─────────────────────────── */
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,'SF Pro Text','Segoe UI',system-ui,sans-serif;
  background:var(--bg);color:var(--text);line-height:1.5;-webkit-font-smoothing:antialiased}
a{color:var(--blue);text-decoration:none}

/* ── Header ───────────────────────────────── */
.header{background:var(--header-bg);color:#e2e8f0;padding:1.25rem 2rem 0;
  background-image:radial-gradient(ellipse at 15% 50%,var(--glow1),transparent 50%),
  radial-gradient(ellipse at 85% 80%,var(--glow2),transparent 50%)}
.header-top{display:flex;align-items:flex-start;justify-content:space-between;gap:1rem;flex-wrap:wrap}
.header h1{font-size:1.25rem;font-weight:700;letter-spacing:-0.01em}
.header .meta{font-size:.75rem;color:#64748b;margin-top:.15rem}
.header-actions{display:flex;align-items:center;gap:.75rem;flex-wrap:wrap}
.profiles{display:flex;gap:.35rem;flex-wrap:wrap}
.badge{display:inline-block;padding:.15rem .5rem;border-radius:9999px;font-size:.7rem;font-weight:600;color:#fff;letter-spacing:.02em;
  cursor:pointer;user-select:none;transition:opacity .2s,filter .2s}
.badge.dimmed{opacity:.3;filter:grayscale(.5)}
.badge-pro{background:var(--blue)}.badge-protect{background:var(--green)}.badge-platform{background:var(--purple)}
.theme-toggle{background:none;border:1px solid rgba(255,255,255,0.15);border-radius:.375rem;
  color:#94a3b8;cursor:pointer;padding:.25rem .5rem;font-size:.75rem;transition:all .15s}
.theme-toggle:hover{color:#e2e8f0;border-color:rgba(255,255,255,0.3)}
/* ── Hero Metrics ─────────────────────────── */
.hero{display:flex;gap:0;padding:1rem 2rem;background:var(--header-bg);
  border-top:1px solid rgba(255,255,255,0.04)}
.hero-stat{display:flex;flex-direction:column;padding:0 1.5rem}
.hero-stat:first-child{padding-left:0}
.hero-stat+.hero-stat{border-left:1px solid rgba(255,255,255,0.08)}
.hero-val{font-family:'SF Mono','Cascadia Code',Consolas,'Liberation Mono',monospace;
  font-size:1.35rem;font-weight:700;color:#fff;font-variant-numeric:tabular-nums;letter-spacing:-.02em}
.hero-lbl{font-size:.65rem;color:#64748b;font-weight:500;text-transform:uppercase;letter-spacing:.06em;margin-top:.1rem}
.hero-stat.hero-alert .hero-val{color:var(--red)}

/* ── Layout ───────────────────────────────── */
.container{max-width:1280px;margin:0 auto;padding:1rem 1.5rem;columns:2;column-gap:.75rem}

/* ── Section Cards ────────────────────────── */
.section{background:var(--card);border:1px solid var(--border);border-radius:.5rem;
  opacity:0;animation:fadeUp .4s ease forwards;break-inside:avoid;margin-bottom:.75rem}
.section.full-width{column-span:all}
@keyframes fadeUp{from{opacity:0;transform:translateY(6px)}to{opacity:1;transform:translateY(0)}}
.section-head{display:flex;align-items:center;justify-content:space-between;
  padding:.6rem .85rem;cursor:pointer;user-select:none;border-bottom:1px solid var(--border);transition:background .15s}
.section-head:hover{background:var(--card-hover)}
.section-head h2{font-size:.82rem;font-weight:750;text-transform:uppercase;letter-spacing:.06em;color:var(--text);
  display:flex;align-items:center;gap:.4rem}
.chevron{font-size:.55rem;color:var(--text3);transition:transform .2s;display:inline-block}
.section.collapsed .section-body{display:none}
.section.collapsed .chevron{transform:rotate(-90deg)}
.section-body{padding:.85rem .85rem 1.25rem}
.section-badges{display:flex;gap:.35rem}

/* ── Product Accent Borders ───────────────── */
.accent-pro{border-left:2px solid var(--blue)}
.accent-protect{border-left:2px solid var(--green)}
.accent-platform{border-left:2px solid var(--purple)}

/* ── Stat Row ─────────────────────────────── */
.stat-row{display:flex;gap:1.25rem;flex-wrap:wrap}
.stat{display:flex;flex-direction:column}
.stat-val{font-family:'SF Mono','Cascadia Code',Consolas,monospace;
  font-size:1.5rem;font-weight:700;color:var(--text);font-variant-numeric:tabular-nums;letter-spacing:-.02em}
.stat-lbl{font-size:.7rem;color:var(--text3);font-weight:500;text-transform:uppercase;letter-spacing:.04em}

/* ── CSS Doughnut Rings ───────────────────── */
@property --rv{syntax:"<number>";inherits:false;initial-value:0}
.ring-row{display:flex;gap:1.25rem;flex-wrap:wrap;align-items:flex-start}
.ring-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(85px,1fr));gap:.75rem}
.ring-item{display:flex;flex-direction:column;align-items:center;gap:.3rem}
.ring-wrap{position:relative;display:inline-grid;place-items:center}
.ring-wrap.ring-md{width:88px;height:88px}
.ring-wrap.ring-sm{width:72px;height:72px}
.ring{position:absolute;inset:0;border-radius:50%;--rv:0;
  background:conic-gradient(var(--c) calc(var(--rv) * 3.6deg),var(--ring-track) 0);
  -webkit-mask:radial-gradient(farthest-side,transparent calc(100% - 9px - .5px),#000 calc(100% - 9px));
  mask:radial-gradient(farthest-side,transparent calc(100% - 9px - .5px),#000 calc(100% - 9px));
  animation:ringFill .8s ease-out forwards}
.ring-wrap.ring-sm .ring{
  -webkit-mask:radial-gradient(farthest-side,transparent calc(100% - 8px - .5px),#000 calc(100% - 8px));
  mask:radial-gradient(farthest-side,transparent calc(100% - 8px - .5px),#000 calc(100% - 8px))}
@keyframes ringFill{to{--rv:var(--v)}}
/* Rounded end cap at arc tip */
.ring::after{content:"";position:absolute;width:9px;height:9px;border-radius:50%;background:var(--c);
  top:calc(50% - 4.5px);left:calc(50% - 4.5px);
  transform:rotate(calc(var(--rv) * 3.6deg)) translateY(calc(-50% + 4.5px));
  transform-origin:center calc(50%);opacity:0;animation:capFade .1s .7s ease forwards}
.ring-wrap.ring-sm .ring::after{width:8px;height:8px;top:calc(50% - 4px);left:calc(50% - 4px);
  transform:rotate(calc(var(--rv) * 3.6deg)) translateY(calc(-50% + 4px))}
@keyframes capFade{to{opacity:1}}
.ring-val{position:relative;font-family:'SF Mono','Cascadia Code',Consolas,monospace;
  font-weight:700;color:var(--text);font-variant-numeric:tabular-nums}
.ring-wrap.ring-md .ring-val{font-size:1.1rem}
.ring-wrap.ring-sm .ring-val{font-size:.95rem}
.ring-label{font-size:.7rem;font-weight:600;color:var(--text2);text-align:center;max-width:90px;line-height:1.2;
  display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden}
.ring-sub{font-size:.65rem;color:var(--text3);font-variant-numeric:tabular-nums}


/* ── Color Classes (avoid inline var() which html/template sanitizes) */
.c-good{--c:var(--green)}.c-warn{--c:var(--amber)}.c-bad{--c:var(--red)}
.bar-good{background:var(--green)}.bar-warn{background:var(--amber)}.bar-bad{background:var(--red)}

/* ── Tables ───────────────────────────────── */
table{width:100%;border-collapse:collapse;font-size:.78rem}
th{text-align:left;padding:.45rem .6rem;border-bottom:1px solid var(--border2);
  font-weight:600;color:var(--text3);font-size:.68rem;text-transform:uppercase;letter-spacing:.04em}
td{padding:.45rem .6rem;border-bottom:1px solid var(--border);color:var(--text)}
tr:nth-child(even) td{background:var(--stripe)}
tr:hover td{background:var(--card-hover)}

/* ── Severity ─────────────────────────────── */
.sev-dot{display:inline-block;width:7px;height:7px;border-radius:50%;margin-right:.3rem;vertical-align:middle}
.sev-dot.critical{background:var(--red)}.sev-dot.warning{background:var(--amber)}.sev-dot.info{background:var(--blue)}
.sev-badge{display:inline-block;padding:.1rem .4rem;border-radius:9999px;font-size:.65rem;font-weight:600;
  cursor:pointer;transition:opacity .15s,transform .15s;user-select:none}
.sev-badge:hover{transform:scale(1.05)}
.sev-badge.dimmed{opacity:.35}
.sev-badge.critical{background:var(--alert-bg);color:var(--red)}
.sev-badge.warning{background:rgba(245,158,11,0.1);color:var(--amber)}
.sev-badge.info{background:rgba(59,130,246,0.1);color:var(--blue)}

/* ── Compliance Bars ──────────────────────── */
.cbar{display:inline-flex;align-items:center;gap:.4rem;width:100%}
.cbar-track{flex:1;height:6px;background:var(--bar-track);border-radius:3px;overflow:hidden}
.cbar-fill{height:100%;border-radius:3px;transition:width .4s ease}
.cbar-text{font-size:.72rem;font-weight:600;min-width:2.5rem;text-align:right;
  font-family:'SF Mono','Cascadia Code',Consolas,monospace;font-variant-numeric:tabular-nums}

/* ── OS Distribution Bars ─────────────────── */
.os-row{display:flex;align-items:center;gap:.5rem;padding:.25rem 0}
.os-row+.os-row{border-top:1px solid var(--border)}
.os-label{font-size:.75rem;color:var(--text2);width:14rem;flex-shrink:0;font-weight:500;
  white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.os-bar-track{flex:1;height:6px;background:var(--bar-track);border-radius:3px;overflow:hidden}
.os-bar-fill{height:100%;border-radius:3px;background:var(--blue);transition:width .5s ease}
.os-count{font-size:.72rem;color:var(--text3);min-width:2.5rem;text-align:right;
  font-family:'SF Mono','Cascadia Code',Consolas,monospace;font-variant-numeric:tabular-nums}
.os-pct{font-size:.7rem;color:var(--text3);min-width:2.8rem;text-align:right;
  font-family:'SF Mono','Cascadia Code',Consolas,monospace;font-variant-numeric:tabular-nums;opacity:.7}

/* ── Version Spread ──────────────────────── */
.spread-title{font-size:.72rem;font-weight:600;color:var(--text2);margin-bottom:.3rem}
.spread-title:not(:first-child){margin-top:.6rem}
.spread-bar{display:flex;height:22px;border-radius:4px;overflow:hidden;gap:1px}
.spread-seg{min-width:2px;display:flex;align-items:center;justify-content:center;position:relative;overflow:hidden}
.spread-seg:nth-child(1){background:var(--green)}
.spread-seg:nth-child(2){background:var(--blue)}
.spread-seg:nth-child(3){background:var(--amber)}
.spread-seg:nth-child(4){background:#8b5cf6}
.spread-seg:nth-child(5){background:#ec4899}
.spread-seg:nth-child(6){background:#06b6d4}
.spread-seg:nth-child(7){background:#f97316}
.spread-seg:nth-child(n+8){background:var(--text3)}
.spread-seg-label{font-size:.58rem;font-weight:600;color:#fff;white-space:nowrap;padding:0 .25rem;
  text-shadow:0 0 2px rgba(0,0,0,.5);overflow:hidden;text-overflow:ellipsis}

/* ── Deploy Badges ────────────────────────── */
.deploy-badge{display:inline-block;padding:.1rem .4rem;border-radius:.2rem;font-size:.65rem;font-weight:600;text-transform:uppercase;letter-spacing:.03em}
.deploy-badge.active{background:rgba(16,185,129,0.12);color:var(--green)}
.deploy-badge.inactive{background:rgba(255,255,255,0.06);color:var(--text3)}
.deploy-badge.draft{background:rgba(245,158,11,0.12);color:var(--amber)}

/* ── Alert Cards ──────────────────────────── */
.alert-row{display:flex;gap:.5rem;flex-wrap:wrap;margin-top:.6rem}
.alert-card{background:var(--alert-bg);border:1px solid var(--alert-border);border-radius:.35rem;
  padding:.4rem .65rem;display:flex;align-items:center;gap:.4rem}
.alert-val{font-family:'SF Mono','Cascadia Code',Consolas,monospace;font-size:.85rem;font-weight:700;color:var(--red)}
.alert-lbl{font-size:.72rem;color:var(--text2)}



/* ── Protect Stat Grid ────────────────────── */
.prot-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(100px,1fr));gap:.6rem}
.prot-stat{text-align:center;padding:.5rem;background:rgba(255,255,255,0.02);border-radius:.35rem;border:1px solid var(--border)}
.prot-val{font-family:'SF Mono','Cascadia Code',Consolas,monospace;font-size:1.1rem;font-weight:700;color:var(--text)}
.prot-lbl{font-size:.67rem;color:var(--text3);font-weight:500;text-transform:uppercase;letter-spacing:.03em;margin-top:.1rem}

/* ── Subsection ───────────────────────────── */
.subsection-title{font-size:.68rem;font-weight:650;text-transform:uppercase;letter-spacing:.05em;
  color:var(--text3);margin-bottom:.5rem;margin-top:.75rem}
.subsection-title:first-child{margin-top:0}

/* ── Footer ───────────────────────────────── */
.footer{text-align:center;padding:1.25rem;color:var(--text3);font-size:.7rem}

/* ── Responsive ───────────────────────────── */
@media(max-width:900px){
  .container{columns:1;padding:.75rem}
  .hero{flex-wrap:wrap;gap:.5rem}
  .hero-stat{padding:0 1rem}
  .header,.hero{padding-left:1rem;padding-right:1rem}
}
@media(max-width:600px){
  .stat-row{gap:.75rem}
  .ring-row{gap:.75rem}
  .ring-grid{grid-template-columns:repeat(auto-fill,minmax(70px,1fr))}
  .os-label{width:9rem;font-size:.65rem}
  table{font-size:.72rem}
  th,td{padding:.35rem .4rem}
}

/* ── Print ────────────────────────────────── */
@media print{
  *{animation:none!important}
  .theme-toggle{display:none!important}
  body{-webkit-print-color-adjust:exact;print-color-adjust:exact}
  .section{break-inside:avoid;border:1px solid #ccc}
  .header,.hero{-webkit-print-color-adjust:exact;print-color-adjust:exact}
}
</style>
</head>
<body>

<div class="header">
  <div class="header-top">
    <div>
      <h1>{{.Title}}</h1>
      <div class="meta">Generated {{.GeneratedAt.Format "Jan 02, 2006 at 3:04 PM"}} · jamf-cli {{.CLIVersion}}</div>
    </div>
    <div class="header-actions">
      <div class="profiles">
        {{range .Profiles}}<span class="badge badge-{{.Product}}" data-product="{{.Product}}" onclick="filterProduct('{{.Product}}',this)">{{.Product}} · {{.Name}}</span>{{end}}
      </div>
      <button class="theme-toggle" onclick="toggleTheme()" title="Toggle theme">
        <span class="theme-icon-dark">☀︎ Light</span>
        <span class="theme-icon-light" style="display:none">☾ Dark</span>
      </button>
    </div>
  </div>
</div>

{{if or .Fleet .Protect .Audit}}
<div class="hero">
  {{if .Fleet}}
  <div class="hero-stat"><span class="hero-val">{{comma .Fleet.ManagedComputers}}</span><span class="hero-lbl">Computers</span></div>
  <div class="hero-stat"><span class="hero-val">{{comma .Fleet.ManagedMobile}}</span><span class="hero-lbl">Mobile</span></div>
  <div class="hero-stat"><span class="hero-val">{{comma .Fleet.Users}}</span><span class="hero-lbl">Users</span></div>
  {{end}}
  {{if .Protect}}
  <div class="hero-stat"><span class="hero-val">{{comma .Protect.Endpoints}}</span><span class="hero-lbl">Protected</span></div>
  {{end}}
  {{if .Audit}}{{if .Audit.CriticalCount}}
  <div class="hero-stat hero-alert"><span class="hero-val">{{.Audit.CriticalCount}}</span><span class="hero-lbl">Critical</span></div>
  {{end}}{{end}}
</div>
{{end}}

<div class="container">

{{/* ── 1. Security Posture + Check-in Status ── */}}

{{if .Security}}
<div class="section accent-pro" id="security">
  <div class="section-head" onclick="toggleSection(this)">
    <h2><span class="chevron">▾</span> Security Posture</h2>
  </div>
  <div class="section-body">
    <div class="ring-row">
      <div class="ring-item">
        <div class="ring-wrap ring-md">
          <div class="ring {{pctClass (.Security.Pct .Security.FileVaultEnabled)}}" style="--v:{{printf "%.0f" (.Security.Pct .Security.FileVaultEnabled)}}"></div>
          <span class="ring-val">{{printf "%.0f" (.Security.Pct .Security.FileVaultEnabled)}}%</span>
        </div>
        <span class="ring-label">FileVault</span>
      </div>
      <div class="ring-item">
        <div class="ring-wrap ring-md">
          <div class="ring {{pctClass (.Security.Pct .Security.FirewallEnabled)}}" style="--v:{{printf "%.0f" (.Security.Pct .Security.FirewallEnabled)}}"></div>
          <span class="ring-val">{{printf "%.0f" (.Security.Pct .Security.FirewallEnabled)}}%</span>
        </div>
        <span class="ring-label">Firewall</span>
      </div>
      <div class="ring-item">
        <div class="ring-wrap ring-md">
          <div class="ring {{pctClass (.Security.Pct .Security.GatekeeperEnabled)}}" style="--v:{{printf "%.0f" (.Security.Pct .Security.GatekeeperEnabled)}}"></div>
          <span class="ring-val">{{printf "%.0f" (.Security.Pct .Security.GatekeeperEnabled)}}%</span>
        </div>
        <span class="ring-label">Gatekeeper</span>
      </div>
      <div class="ring-item">
        <div class="ring-wrap ring-md">
          <div class="ring {{pctClass (.Security.Pct .Security.SIPEnabled)}}" style="--v:{{printf "%.0f" (.Security.Pct .Security.SIPEnabled)}}"></div>
          <span class="ring-val">{{printf "%.0f" (.Security.Pct .Security.SIPEnabled)}}%</span>
        </div>
        <span class="ring-label">SIP</span>
      </div>
    </div>
  </div>
</div>
{{end}}

{{if .Checkin}}
<div class="section accent-pro" id="checkin">
  <div class="section-head" onclick="toggleSection(this)">
    <h2><span class="chevron">▾</span> Check-in Status ({{.Checkin.ThresholdDays}}d)</h2>
  </div>
  <div class="section-body">
    <div class="ring-row">
      <div class="ring-item">
        <div class="ring-wrap ring-md">
          <div class="ring {{pctClass (.Checkin.CheckedInPct .Checkin.ComputersOverdue .Checkin.ComputersTotal)}}" style="--v:{{printf "%.0f" (.Checkin.CheckedInPct .Checkin.ComputersOverdue .Checkin.ComputersTotal)}}"></div>
          <span class="ring-val">{{printf "%.0f" (.Checkin.CheckedInPct .Checkin.ComputersOverdue .Checkin.ComputersTotal)}}%</span>
        </div>
        <span class="ring-label">Computers</span>
        <span class="ring-sub">{{comma .Checkin.ComputersOverdue}} overdue</span>
      </div>
      <div class="ring-item">
        <div class="ring-wrap ring-md">
          <div class="ring {{pctClass (.Checkin.CheckedInPct .Checkin.MobileOverdue .Checkin.MobileTotal)}}" style="--v:{{printf "%.0f" (.Checkin.CheckedInPct .Checkin.MobileOverdue .Checkin.MobileTotal)}}"></div>
          <span class="ring-val">{{printf "%.0f" (.Checkin.CheckedInPct .Checkin.MobileOverdue .Checkin.MobileTotal)}}%</span>
        </div>
        <span class="ring-label">Mobile</span>
        <span class="ring-sub">{{comma .Checkin.MobileOverdue}} overdue</span>
      </div>
      <div class="ring-item">
        <div class="ring-wrap ring-md">
          <div class="ring {{pctClass (.Checkin.CheckedInPct (.Checkin.TotalOverdue) (.Checkin.TotalDevices))}}" style="--v:{{printf "%.0f" (.Checkin.CheckedInPct (.Checkin.TotalOverdue) (.Checkin.TotalDevices))}}"></div>
          <span class="ring-val">{{printf "%.0f" (.Checkin.CheckedInPct (.Checkin.TotalOverdue) (.Checkin.TotalDevices))}}%</span>
        </div>
        <span class="ring-label">Overall</span>
        <span class="ring-sub">{{comma (.Checkin.TotalOverdue)}} / {{comma (.Checkin.TotalDevices)}}</span>
      </div>
    </div>
  </div>
</div>
{{end}}

{{/* ── 2. Audit Findings (full-width) ── */}}

{{if .Audit}}
<div class="section accent-pro full-width" id="audit">
  <div class="section-head" onclick="toggleSection(this)">
    <h2><span class="chevron">▾</span> Audit Findings</h2>
    <div class="section-badges" onclick="event.stopPropagation()">
      {{if .Audit.CriticalCount}}<span class="sev-badge critical" onclick="filterAudit('critical',this)" role="button">{{.Audit.CriticalCount}} Critical</span>{{end}}
      {{if .Audit.WarningCount}}<span class="sev-badge warning" onclick="filterAudit('warning',this)" role="button">{{.Audit.WarningCount}} Warning</span>{{end}}
      {{if .Audit.InfoCount}}<span class="sev-badge info" onclick="filterAudit('info',this)" role="button">{{.Audit.InfoCount}} Info</span>{{end}}
    </div>
  </div>
  <div class="section-body">
    <table>
      <thead><tr><th>Severity</th><th>Category</th><th>Check</th><th>Affected</th><th>Recommendation</th></tr></thead>
      <tbody>
        {{range .Audit.Results}}
        <tr data-severity="{{toLower .Severity}}">
          <td><span class="sev-dot {{toLower .Severity}}"></span>{{.Severity}}</td>
          <td>{{.Category}}</td>
          <td>{{.Name}}</td>
          <td>{{.AffectedCount}}</td>
          <td>{{.Recommendation}}</td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>
</div>
{{end}}

{{/* ── 3. Environment + Smart Group Distribution ── */}}

{{if .EnvStats}}
<div class="section accent-pro" id="envstats">
  <div class="section-head" onclick="toggleSection(this)">
    <h2><span class="chevron">▾</span> Environment</h2>
  </div>
  <div class="section-body">
    <div class="prot-grid">
      <div class="prot-stat"><div class="prot-val">{{comma .EnvStats.Policies}}</div><div class="prot-lbl">Policies</div></div>
      <div class="prot-stat"><div class="prot-val">{{comma .EnvStats.ConfigProfiles}}</div><div class="prot-lbl">Config Profiles</div></div>
      <div class="prot-stat"><div class="prot-val">{{comma .EnvStats.Scripts}}</div><div class="prot-lbl">Scripts</div></div>
      <div class="prot-stat"><div class="prot-val">{{comma .EnvStats.Packages}}</div><div class="prot-lbl">Packages</div></div>
      <div class="prot-stat"><div class="prot-val">{{comma .EnvStats.ComputerSmartGrps}}</div><div class="prot-lbl">Computer Smart Groups</div></div>
      <div class="prot-stat"><div class="prot-val">{{comma .EnvStats.MobileSmartGrps}}</div><div class="prot-lbl">Mobile Smart Groups</div></div>
      <div class="prot-stat"><div class="prot-val">{{comma .EnvStats.ExtAttributes}}</div><div class="prot-lbl">Extension Attributes</div></div>
      <div class="prot-stat"><div class="prot-val">{{comma .EnvStats.Categories}}</div><div class="prot-lbl">Categories</div></div>
    </div>
  </div>
</div>
{{end}}

{{if .ComputerSmartGroups}}
<div class="section accent-pro" id="smartgroups-computer">
  <div class="section-head" onclick="toggleSection(this)">
    <h2><span class="chevron">▾</span> Computer Smart Groups</h2>
  </div>
  <div class="section-body">
    <div class="ring-grid">
      {{range .ComputerSmartGroups.Groups}}
      <div class="ring-item">
        <div class="ring-wrap ring-sm">
          <div class="ring c-good" style="--v:{{printf "%.0f" (barPct .Count $.ComputerSmartGroups.TotalFleet)}}"></div>
          <span class="ring-val">{{comma .Count}}</span>
        </div>
        <span class="ring-label" title="{{.Name}}">{{.Name}}</span>
      </div>
      {{end}}
    </div>
  </div>
</div>
{{end}}

{{if .MobileSmartGroups}}
<div class="section accent-pro" id="smartgroups-mobile">
  <div class="section-head" onclick="toggleSection(this)">
    <h2><span class="chevron">▾</span> Mobile Smart Groups</h2>
  </div>
  <div class="section-body">
    <div class="ring-grid">
      {{range .MobileSmartGroups.Groups}}
      <div class="ring-item">
        <div class="ring-wrap ring-sm">
          <div class="ring c-good" style="--v:{{printf "%.0f" (barPct .Count $.MobileSmartGroups.TotalFleet)}}"></div>
          <span class="ring-val">{{comma .Count}}</span>
        </div>
        <span class="ring-label" title="{{.Name}}">{{.Name}}</span>
      </div>
      {{end}}
    </div>
  </div>
</div>
{{end}}

{{/* ── 4. Fleet & Devices + OS Distribution ── */}}

{{if or .Fleet .Devices}}
<div class="section accent-pro" id="fleet">
  <div class="section-head" onclick="toggleSection(this)">
    <h2><span class="chevron">▾</span> Fleet & Devices</h2>
  </div>
  <div class="section-body">
    {{if .Fleet}}
    <div class="ring-row">
      <div class="ring-item">
        <div class="ring-wrap ring-md">
          <div class="ring {{pctClass .Fleet.ComputerManagedPct}}" style="--v:{{printf "%.0f" .Fleet.ComputerManagedPct}}"></div>
          <span class="ring-val">{{printf "%.0f" .Fleet.ComputerManagedPct}}%</span>
        </div>
        <span class="ring-label">Computers</span>
        <span class="ring-sub">{{comma .Fleet.ManagedComputers}} / {{comma .Fleet.TotalComputers}}</span>
      </div>
      <div class="ring-item">
        <div class="ring-wrap ring-md">
          <div class="ring {{pctClass .Fleet.MobileManagedPct}}" style="--v:{{printf "%.0f" .Fleet.MobileManagedPct}}"></div>
          <span class="ring-val">{{printf "%.0f" .Fleet.MobileManagedPct}}%</span>
        </div>
        <span class="ring-label">Mobile</span>
        <span class="ring-sub">{{comma .Fleet.ManagedMobile}} / {{comma .Fleet.TotalMobile}}</span>
      </div>
    </div>
    {{end}}
    {{if .Devices}}
    <div class="alert-row">
      {{if gt .Devices.StaleDevices 0}}<div class="alert-card"><span class="alert-val">{{comma .Devices.StaleDevices}}</span><span class="alert-lbl">Stale devices (&gt;{{.Devices.StaleThresholdDays}}d)</span></div>{{end}}
      {{if gt .Devices.FailedMDMCommands 0}}<div class="alert-card"><span class="alert-val">{{comma .Devices.FailedMDMCommands}}</span><span class="alert-lbl">Failed MDM commands</span></div>{{end}}
    </div>
    {{end}}
  </div>
</div>
{{end}}

{{if .OSDist}}
<div class="section accent-pro" id="osdist">
  <div class="section-head" onclick="toggleSection(this)">
    <h2><span class="chevron">▾</span> OS Distribution</h2>
  </div>
  <div class="section-body">
    {{range .OSDist.Versions}}
    <div class="os-row">
      <span class="os-label">{{.Version}}</span>
      <div class="os-bar-track"><div class="os-bar-fill" style="width:{{printf "%.1f" (barPct .Count $.TotalOSCount)}}%"></div></div>
      <span class="os-count">{{comma .Count}}</span>
      <span class="os-pct">{{printf "%.1f" (barPct .Count $.TotalOSCount)}}%</span>
    </div>
    {{end}}
  </div>
</div>
{{end}}

{{/* ── 5. Computer Models + Mobile Models ── */}}

{{if .Hardware}}
{{if .Hardware.ComputerModels}}
<div class="section accent-pro" id="hw-computers">
  <div class="section-head" onclick="toggleSection(this)">
    <h2><span class="chevron">▾</span> Computer Models</h2>
  </div>
  <div class="section-body">
    {{range .Hardware.ComputerModels}}
    <div class="os-row">
      <span class="os-label">{{.Model}}</span>
      <div class="os-bar-track"><div class="os-bar-fill" style="width:{{printf "%.1f" (barPct .Count $.TotalComputerModels)}}%"></div></div>
      <span class="os-count">{{comma .Count}}</span>
      <span class="os-pct">{{printf "%.1f" (barPct .Count $.TotalComputerModels)}}%</span>
    </div>
    {{end}}
  </div>
</div>
{{end}}
{{if .Hardware.MobileModels}}
<div class="section accent-pro" id="hw-mobile">
  <div class="section-head" onclick="toggleSection(this)">
    <h2><span class="chevron">▾</span> Mobile Models</h2>
  </div>
  <div class="section-body">
    {{range .Hardware.MobileModels}}
    <div class="os-row">
      <span class="os-label">{{.Model}}</span>
      <div class="os-bar-track"><div class="os-bar-fill" style="width:{{printf "%.1f" (barPct .Count $.TotalMobileModels)}}%"></div></div>
      <span class="os-count">{{comma .Count}}</span>
      <span class="os-pct">{{printf "%.1f" (barPct .Count $.TotalMobileModels)}}%</span>
    </div>
    {{end}}
  </div>
</div>
{{end}}
{{end}}

{{/* ── 6. Patch Compliance + Version Spread ── */}}

{{if .Patch}}
<div class="section accent-pro" id="patch">
  <div class="section-head" onclick="toggleSection(this)">
    <h2><span class="chevron">▾</span> Patch Compliance</h2>
  </div>
  <div class="section-body">
    <div class="ring-grid">
      {{range .Patch.Titles}}
      <div class="ring-item">
        <div class="ring-wrap ring-sm">
          <div class="ring {{pctClass .CompliancePct}}" style="--v:{{printf "%.0f" .CompliancePct}}"></div>
          <span class="ring-val">{{printf "%.0f" .CompliancePct}}%</span>
        </div>
        <span class="ring-label" title="{{.Name}}">{{.Name}}</span>
      </div>
      {{end}}
    </div>
    <div style="margin-top:.75rem">
      <table>
        <thead><tr><th>Title</th><th>Version</th><th>Up to Date</th><th>Out of Date</th><th>Compliance</th></tr></thead>
        <tbody>
          {{range .Patch.Titles}}
          <tr>
            <td>{{.Name}}</td>
            <td>{{.LatestVersion}}</td>
            <td>{{comma .UpToDate}}</td>
            <td>{{comma .OutOfDate}}</td>
            <td><div class="cbar"><div class="cbar-track"><div class="cbar-fill {{barClass .CompliancePct}}" style="width:{{printf "%.0f" .CompliancePct}}%"></div></div><span class="cbar-text">{{printf "%.0f" .CompliancePct}}%</span></div></td>
          </tr>
          {{end}}
        </tbody>
      </table>
    </div>
  </div>
</div>
{{end}}

{{if .PatchSpread}}
<div class="section accent-pro full-width" id="patchspread">
  <div class="section-head" onclick="toggleSection(this)">
    <h2><span class="chevron">▾</span> Patch Version Spread</h2>
  </div>
  <div class="section-body">
    {{range .PatchSpread}}
    <div class="spread-title">{{.Title}}</div>
    <div class="spread-bar">
      {{range .Versions}}
      <div class="spread-seg" style="flex:{{.Count}}" title="{{.Version}}: {{comma .Count}}">
        <span class="spread-seg-label">{{.Version}}</span>
      </div>
      {{end}}
    </div>
    {{end}}
  </div>
</div>
{{end}}

{{/* ── 7. Jamf Protect + Jamf Platform ── */}}

{{if .Protect}}
<div class="section accent-protect" id="protect">
  <div class="section-head" onclick="toggleSection(this)">
    <h2><span class="chevron">▾</span> Jamf Protect</h2>
  </div>
  <div class="section-body">
    <div class="ring-row" style="margin-bottom:.6rem">
      <div class="ring-item">
        <div class="ring-wrap ring-sm">
          <div class="ring {{pctClass .Protect.ActiveAnalyticsPct}}" style="--v:{{printf "%.0f" .Protect.ActiveAnalyticsPct}}"></div>
          <span class="ring-val">{{printf "%.0f" .Protect.ActiveAnalyticsPct}}%</span>
        </div>
        <span class="ring-label">Analytics Active</span>
        <span class="ring-sub">{{comma .Protect.AnalyticsActive}} / {{comma .Protect.AnalyticsTotal}}</span>
      </div>
    </div>
    <div class="prot-grid">
      <div class="prot-stat"><div class="prot-val">{{comma .Protect.Plans}}</div><div class="prot-lbl">Plans</div></div>
      <div class="prot-stat"><div class="prot-val">{{comma .Protect.Endpoints}}</div><div class="prot-lbl">Endpoints</div></div>
      <div class="prot-stat"><div class="prot-val">{{.Protect.AnalyticSets}}</div><div class="prot-lbl">Analytic Sets</div></div>
      <div class="prot-stat"><div class="prot-val">{{.Protect.ExceptionSets}}</div><div class="prot-lbl">Exception Sets</div></div>
    </div>
  </div>
</div>
{{end}}

{{if .Platform}}
<div class="section accent-platform" id="platform">
  <div class="section-head" onclick="toggleSection(this)">
    <h2><span class="chevron">▾</span> Jamf Platform</h2>
  </div>
  <div class="section-body">
    {{if .Platform.Blueprints}}
    <div class="subsection-title">Blueprints</div>
    <table>
      <thead><tr><th>Name</th><th>State</th></tr></thead>
      <tbody>
        {{range .Platform.Blueprints}}
        <tr><td>{{.Name}}</td><td><span class="deploy-badge {{toLower .DeploymentState}}">{{.DeploymentState}}</span></td></tr>
        {{end}}
      </tbody>
    </table>
    {{end}}
    {{if .Platform.Benchmarks}}
    <div class="subsection-title">Benchmarks</div>
    <table>
      <thead><tr><th>Title</th><th>Compliance</th><th>Failing</th></tr></thead>
      <tbody>
        {{range .Platform.Benchmarks}}
        <tr>
          <td>{{.Title}}</td>
          <td><div class="cbar"><div class="cbar-track"><div class="cbar-fill {{barClass .CompliancePct}}" style="width:{{printf "%.0f" .CompliancePct}}%"></div></div><span class="cbar-text">{{printf "%.0f" .CompliancePct}}%</span></div></td>
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

<div class="footer">Generated by jamf-cli {{.CLIVersion}} · {{.GeneratedAt.Format "2006-01-02T15:04:05Z07:00"}}</div>

<script>
(function(){
  "use strict";

  // Theme toggle
  window.toggleTheme = function() {
    var html = document.documentElement;
    var isDark = html.getAttribute("data-theme") !== "light";
    html.setAttribute("data-theme", isDark ? "light" : "dark");
    document.querySelectorAll(".theme-icon-dark").forEach(function(e){ e.style.display = isDark ? "none" : ""; });
    document.querySelectorAll(".theme-icon-light").forEach(function(e){ e.style.display = isDark ? "" : "none"; });
  };

  // Section collapse
  window.toggleSection = function(header) {
    header.parentElement.classList.toggle("collapsed");
  };

  // Audit severity filter
  var activeFilter = null;
  window.filterAudit = function(severity, badge) {
    if (activeFilter === severity) {
      activeFilter = null;
    } else {
      activeFilter = severity;
    }
    document.querySelectorAll(".sev-badge").forEach(function(b){
      b.classList.toggle("dimmed", activeFilter !== null && b !== badge);
    });
    if (badge) badge.classList.remove("dimmed");
    document.querySelectorAll("#audit tbody tr").forEach(function(row){
      row.style.display = !activeFilter || row.getAttribute("data-severity") === activeFilter ? "" : "none";
    });
  };

  // Profile product filter
  var activeProduct = null;
  window.filterProduct = function(product, badge) {
    if (activeProduct === product) {
      activeProduct = null;
    } else {
      activeProduct = product;
    }
    document.querySelectorAll(".badge").forEach(function(b){
      b.classList.toggle("dimmed", activeProduct !== null && b.getAttribute("data-product") !== activeProduct);
    });
    document.querySelectorAll(".section").forEach(function(s){
      if (!activeProduct) {
        s.style.display = "";
        return;
      }
      var isMatch = s.classList.contains("accent-" + activeProduct);
      s.style.display = isMatch ? "" : "none";
    });
  };

  // Staggered section animation
  document.addEventListener("DOMContentLoaded", function() {
    document.querySelectorAll(".section").forEach(function(s, i){
      s.style.animationDelay = (i * 0.06) + "s";
    });
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

	// Compute total OS count for bar chart scaling
	totalOS := 0
	if data.OSDist != nil {
		for _, v := range data.OSDist.Versions {
			totalOS += v.Count
		}
	}

	// Compute total hardware model counts for bar scaling
	totalCompModels, totalMobileModels := 0, 0
	if data.Hardware != nil {
		for _, m := range data.Hardware.ComputerModels {
			totalCompModels += m.Count
		}
		for _, m := range data.Hardware.MobileModels {
			totalMobileModels += m.Count
		}
	}

	funcMap := template.FuncMap{
		"toLower": strings.ToLower,
		"pctClass": func(pct float64) string {
			if pct >= 90 {
				return "c-good"
			}
			if pct >= 70 {
				return "c-warn"
			}
			return "c-bad"
		},
		"barClass": func(pct float64) string {
			if pct >= 90 {
				return "bar-good"
			}
			if pct >= 70 {
				return "bar-warn"
			}
			return "bar-bad"
		},
		"comma": func(n int) string {
			s := strconv.Itoa(n)
			if len(s) <= 3 {
				return s
			}
			out := make([]byte, 0, len(s)+len(s)/3)
			mod := len(s) % 3
			if mod == 0 {
				mod = 3
			}
			out = append(out, s[:mod]...)
			for i := mod; i < len(s); i += 3 {
				out = append(out, ',')
				out = append(out, s[i:i+3]...)
			}
			return string(out)
		},
		"add": func(a, b int) int { return a + b },
		"barPct": func(count, max int) float64 {
			if max == 0 {
				return 0
			}
			return float64(count) / float64(max) * 100
		},
	}

	tmpl, err := template.New("dashboard").Funcs(funcMap).Parse(dashboardTemplate)
	if err != nil {
		return err
	}

	type templateData struct {
		*DashboardData
		TotalOSCount        int
		TotalComputerModels int
		TotalMobileModels   int
	}

	td := templateData{
		DashboardData:       data,
		TotalOSCount:        totalOS,
		TotalComputerModels: totalCompModels,
		TotalMobileModels:   totalMobileModels,
	}

	return tmpl.Execute(w, td)
}
