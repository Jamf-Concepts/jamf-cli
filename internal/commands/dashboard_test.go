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
			StaleThresholdDays: 90,
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
		"Fleet Summary",
		"FileVault",
		"Gatekeeper",
		"CRITICAL",
		"WARNING",
		"INFO",
		"Google Chrome",
		"macOS 14.2",
		"1.5.0",
		"Apr 11, 2026 at 2:30 PM",
	}

	for _, s := range mustContain {
		if !strings.Contains(html, s) {
			t.Errorf("HTML output missing expected string: %q", s)
		}
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
	if !strings.Contains(html, "95") {
		t.Error("HTML output missing analytics active count '95'")
	}
	if !strings.Contains(html, "120") {
		t.Error("HTML output missing analytics total count '120'")
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
	if strings.Contains(html, "Fleet Summary") {
		t.Error("HTML should not contain 'Fleet Summary' when Fleet is nil")
	}
	if strings.Contains(html, "Jamf Protect") {
		t.Error("HTML should not contain 'Jamf Protect' when Protect is nil")
	}
	if strings.Contains(html, "Security Posture") {
		t.Error("HTML should not contain 'Security Posture' when Security is nil")
	}
	if strings.Contains(html, "Audit Findings") {
		t.Error("HTML should not contain 'Audit Findings' when Audit is nil")
	}
	if strings.Contains(html, "Patch Compliance") {
		t.Error("HTML should not contain 'Patch Compliance' when Patch is nil")
	}
	if strings.Contains(html, "Device Compliance") {
		t.Error("HTML should not contain 'Device Compliance' when Devices is nil")
	}
	if strings.Contains(html, "OS Distribution") {
		t.Error("HTML should not contain 'OS Distribution' when OSDist is nil")
	}
	if strings.Contains(html, "Jamf Platform") {
		t.Error("HTML should not contain 'Jamf Platform' when Platform is nil")
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
