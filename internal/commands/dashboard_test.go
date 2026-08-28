// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestRenderDashboard_ProOnly(t *testing.T) {
	data := &DashboardData{
		Title:       "Jamf Environment Dashboard",
		GeneratedAt: time.Date(2026, 4, 11, 14, 30, 0, 0, time.UTC),
		CLIVersion:  "1.5.0",
		Profiles: []dashboardProfile{
			{Name: "production", Product: "pro", URL: "https://prod.jamfcloud.com"},
		},
		Fleet: &fleetSummary{
			ManagedComputers:   1200,
			UnmanagedComputers: 50,
			ManagedMobile:      800,
			UnmanagedMobile:    25,
			Users:              950,
		},
		Security: &securityPosture{
			Total:             200,
			FileVaultEnabled:  180,
			FirewallEnabled:   160,
			GatekeeperEnabled: 195,
			SIPEnabled:        198,
		},
		Audit: &auditSummary{
			Results: []auditResult{
				{Category: "Security", Severity: severityWarning, Name: "Weak Passwords", AffectedCount: 12, Recommendation: "Enforce stronger passwords"},
				{Category: "Compliance", Severity: severityCritical, Name: "Missing Encryption", AffectedCount: 5, Recommendation: "Enable FileVault"},
				{Category: "Inventory", Severity: severityInfo, Name: "Stale Records", AffectedCount: 3, Recommendation: "Clean up stale records"},
			},
		},
		Patch: &patchCompliance{
			Titles: []patchTitle{
				{Name: "Google Chrome", LatestVersion: "120.0", UpToDate: 900, OutOfDate: 100, Total: 1000, CompliancePct: 90.0},
				{Name: "Zoom", LatestVersion: "5.17", UpToDate: 600, OutOfDate: 400, Total: 1000, CompliancePct: 60.0},
			},
		},
		Devices: &deviceCompliance{
			StaleDevices:       15,
			FailedMDMCommands:  3,
			StaleThresholdDays: 14,
		},
		OSDist: &osDistribution{
			Versions: []osVersionCount{
				{Version: "macOS 14.2", Count: 500},
				{Version: "macOS 13.6", Count: 300},
				{Version: "macOS 12.7", Count: 100},
			},
		},
	}

	var buf bytes.Buffer
	if err := renderDashboard(&buf, data); err != nil {
		t.Fatalf("renderDashboard error: %v", err)
	}

	html := buf.String()
	mustContain := []string{
		"Jamf Environment Dashboard",
		"production",
		"Security Posture",
		"FileVault",
		"Gatekeeper",
		"CRITICAL",
		"WARNING",
		"INFO",
		"Google Chrome",
		"macOS 14.2",
		"1.5.0",
		"data-theme=\"dark\"",
		"Fleet & Devices",
		"Patch Compliance",
		"OS Distribution",
	}

	for _, s := range mustContain {
		if !strings.Contains(html, s) {
			t.Errorf("HTML output missing expected string: %q", s)
		}
	}

	// Verify no Chart.js reference
	if strings.Contains(html, "new Chart") {
		t.Error("HTML should not contain Chart.js references")
	}
}

func TestRenderDashboard_ProtectSection(t *testing.T) {
	data := &DashboardData{
		Title:       "Protect Dashboard",
		GeneratedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		CLIVersion:  "1.5.0",
		Profiles: []dashboardProfile{
			{Name: "protect-prod", Product: "protect", URL: "https://protect.jamfcloud.com"},
		},
		Protect: &protectCoverage{
			Plans:           5,
			AnalyticsTotal:  120,
			AnalyticsActive: 95,
			Endpoints:       2000,
			AnalyticSets:    8,
			ExceptionSets:   3,
		},
	}

	var buf bytes.Buffer
	if err := renderDashboard(&buf, data); err != nil {
		t.Fatalf("renderDashboard error: %v", err)
	}

	html := buf.String()
	if !strings.Contains(html, "Jamf Protect") {
		t.Error("HTML output missing 'Jamf Protect' section")
	}
	if !strings.Contains(html, "accent-protect") {
		t.Error("HTML output missing protect accent border")
	}
	if !strings.Contains(html, "2,000") {
		t.Error("HTML output missing formatted endpoint count '2,000'")
	}
}

func TestRenderDashboard_PlatformSection(t *testing.T) {
	data := &DashboardData{
		Title:       "Platform Dashboard",
		GeneratedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		CLIVersion:  "1.5.0",
		Platform: &platformStatus{
			Blueprints: []blueprintEntry{
				{Name: "Corp Mac Standard", DeploymentState: "ACTIVE"},
			},
			Benchmarks: []benchmarkEntry{
				{Title: "CIS macOS 15", CompliancePct: 87.4, FailingRules: 19},
			},
		},
	}

	var buf bytes.Buffer
	if err := renderDashboard(&buf, data); err != nil {
		t.Fatalf("renderDashboard error: %v", err)
	}

	html := buf.String()
	if !strings.Contains(html, "Jamf Platform") {
		t.Error("HTML output missing 'Jamf Platform' section")
	}
	if !strings.Contains(html, "accent-platform") {
		t.Error("HTML output missing platform accent border")
	}
	if !strings.Contains(html, "Corp Mac Standard") {
		t.Error("HTML output missing blueprint name")
	}
	if !strings.Contains(html, "CIS macOS 15") {
		t.Error("HTML output missing benchmark title")
	}
}

func TestRenderDashboard_NoSectionsWhenNil(t *testing.T) {
	data := &DashboardData{
		Title:       "Empty Dashboard",
		GeneratedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		CLIVersion:  "1.0.0",
	}

	var buf bytes.Buffer
	if err := renderDashboard(&buf, data); err != nil {
		t.Fatalf("renderDashboard error: %v", err)
	}

	html := buf.String()
	absent := []string{
		"Fleet & Devices",
		"Jamf Protect",
		"Security Posture",
		"Audit Findings",
		"Patch Compliance",
		"Patch Version Spread",
		"OS Distribution",
		"Jamf Platform",
		"Check-in Status",
		"Computer Models",
		"Mobile Models",
		"Environment",
		"Computer Smart Groups",
		"Mobile Smart Groups",
	}
	for _, s := range absent {
		if strings.Contains(html, s) {
			t.Errorf("HTML should not contain %q when all sections are nil", s)
		}
	}
}

func TestRenderDashboard_DarkThemeDefault(t *testing.T) {
	data := &DashboardData{
		Title:       "Theme Test",
		GeneratedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		CLIVersion:  "1.0.0",
	}

	var buf bytes.Buffer
	if err := renderDashboard(&buf, data); err != nil {
		t.Fatalf("renderDashboard error: %v", err)
	}

	html := buf.String()
	if !strings.Contains(html, `data-theme="dark"`) {
		t.Error("HTML should default to dark theme")
	}
	if !strings.Contains(html, "toggleTheme") {
		t.Error("HTML should include theme toggle function")
	}
	if !strings.Contains(html, `[data-theme="light"]`) {
		t.Error("HTML should include light theme CSS")
	}
}

func TestSecurityPosturePct(t *testing.T) {
	s := &securityPosture{Total: 200, FileVaultEnabled: 180}
	got := s.Pct(180)
	if got != 90.0 {
		t.Errorf("Pct(180) = %v, want 90.0", got)
	}

	zero := &securityPosture{Total: 0}
	got = zero.Pct(0)
	if got != 0 {
		t.Errorf("Pct(0) with Total=0 = %v, want 0", got)
	}
}

func TestFleetManagedPct(t *testing.T) {
	f := &fleetSummary{ManagedComputers: 1200, UnmanagedComputers: 50, ManagedMobile: 800, UnmanagedMobile: 25}

	cpct := f.ComputerManagedPct()
	if cpct < 95.0 || cpct > 96.5 {
		t.Errorf("ComputerManagedPct() = %v, want ~96.0", cpct)
	}

	mpct := f.MobileManagedPct()
	if mpct < 96.0 || mpct > 97.5 {
		t.Errorf("MobileManagedPct() = %v, want ~96.97", mpct)
	}

	empty := &fleetSummary{}
	if empty.ComputerManagedPct() != 0 {
		t.Error("ComputerManagedPct() should be 0 for empty fleet")
	}
}

func TestProtectActiveAnalyticsPct(t *testing.T) {
	p := &protectCoverage{AnalyticsTotal: 120, AnalyticsActive: 95}
	pct := p.ActiveAnalyticsPct()
	if pct < 79.0 || pct > 80.0 {
		t.Errorf("ActiveAnalyticsPct() = %v, want ~79.17", pct)
	}

	empty := &protectCoverage{}
	if empty.ActiveAnalyticsPct() != 0 {
		t.Error("ActiveAnalyticsPct() should be 0 for empty protect")
	}
}
