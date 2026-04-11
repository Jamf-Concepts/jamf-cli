// Copyright 2026, Jamf Software LLC

//go:build smoke

package commands

import (
	"os"
	"testing"
	"time"
)

func TestSmokeDashboard(t *testing.T) {
	data := &DashboardData{
		Title:       "Jamf Fleet Dashboard — Q2 2026",
		GeneratedAt: time.Now(),
		CLIVersion:  "1.6.0",
		Profiles: []dashboardProfile{
			{Name: "production-pro", Product: "pro", URL: "https://acme.jamfcloud.com"},
			{Name: "production-protect", Product: "protect", URL: "https://acme-protect.jamfcloud.com"},
			{Name: "production-platform", Product: "platform", URL: "https://acme.jamfcloud.com"},
		},
		Fleet: &fleetSummary{
			ManagedComputers: 2847, UnmanagedComputers: 63,
			ManagedMobile: 1592, UnmanagedMobile: 41, Users: 3120,
		},
		Security: &securityPosture{
			Total: 2847, FileVaultEnabled: 2705, FirewallEnabled: 2204,
			GatekeeperEnabled: 2790, SIPEnabled: 2831,
		},
		Audit: &auditSummary{Results: []auditResult{
			{Category: "Security", Severity: severityCritical, Name: "Computers with no encryption", AffectedCount: 142, Recommendation: "Enable FileVault on all managed Macs."},
			{Category: "Security", Severity: severityCritical, Name: "Firewall disabled on endpoints", AffectedCount: 643, Recommendation: "Deploy a configuration profile enforcing macOS Firewall."},
			{Category: "Compliance", Severity: severityWarning, Name: "Stale inventory (>30 days)", AffectedCount: 89, Recommendation: "Review check-in policies."},
			{Category: "Compliance", Severity: severityWarning, Name: "Missing management framework", AffectedCount: 34, Recommendation: "Re-enroll affected devices."},
			{Category: "Compliance", Severity: severityWarning, Name: "Expired push certificates", AffectedCount: 2, Recommendation: "Renew APNs certificates."},
			{Category: "Inventory", Severity: severityInfo, Name: "Duplicate serial numbers", AffectedCount: 7, Recommendation: "Merge or remove duplicates."},
			{Category: "Inventory", Severity: severityInfo, Name: "Extension attributes not scoped", AffectedCount: 15, Recommendation: "Scope to relevant smart groups."},
			{Category: "Configuration", Severity: severityInfo, Name: "Unused policies (no scope)", AffectedCount: 23, Recommendation: "Archive or delete."},
		}},
		Patch: &patchCompliance{Titles: []patchTitle{
			{Name: "Chrome", LatestVersion: "125.0.6422.60", UpToDate: 2340, OutOfDate: 507, Total: 2847, CompliancePct: 82.2},
			{Name: "Firefox", LatestVersion: "126.0", UpToDate: 1890, OutOfDate: 410, Total: 2300, CompliancePct: 82.2},
			{Name: "Zoom", LatestVersion: "6.0.2", UpToDate: 1200, OutOfDate: 847, Total: 2047, CompliancePct: 58.6},
			{Name: "Teams", LatestVersion: "24114.2726", UpToDate: 1800, OutOfDate: 200, Total: 2000, CompliancePct: 90.0},
			{Name: "Slack", LatestVersion: "4.38.125", UpToDate: 1680, OutOfDate: 120, Total: 1800, CompliancePct: 93.3},
			{Name: "VS Code", LatestVersion: "1.89.1", UpToDate: 890, OutOfDate: 310, Total: 1200, CompliancePct: 74.2},
			{Name: "1Password", LatestVersion: "8.10.30", UpToDate: 950, OutOfDate: 50, Total: 1000, CompliancePct: 95.0},
			{Name: "Docker", LatestVersion: "4.30.0", UpToDate: 380, OutOfDate: 220, Total: 600, CompliancePct: 63.3},
		}},
		Devices: &deviceCompliance{StaleDevices: 89, FailedMDMCommands: 12, StaleThresholdDays: 14},
		OSDist: &osDistribution{Versions: []osVersionCount{
			{Version: "macOS 15.4", Count: 892},
			{Version: "macOS 15.3.2", Count: 634},
			{Version: "macOS 15.2", Count: 412},
			{Version: "macOS 14.7.5", Count: 389},
			{Version: "macOS 14.6", Count: 201},
			{Version: "macOS 13.7.2", Count: 156},
			{Version: "macOS 13.6", Count: 89},
			{Version: "macOS 12.7.6", Count: 42},
			{Version: "macOS 12.6", Count: 18},
			{Version: "macOS 11.7.10", Count: 14},
		}},
		Protect: &protectCoverage{Plans: 8, AnalyticsTotal: 247, AnalyticsActive: 247, Endpoints: 2891, AnalyticSets: 12, ExceptionSets: 4},
		Platform: &platformStatus{
			Blueprints: []blueprintEntry{
				{Name: "Corporate Mac — Standard", DeploymentState: "ACTIVE"},
				{Name: "Corporate Mac — Engineering", DeploymentState: "ACTIVE"},
				{Name: "Corporate Mac — Finance", DeploymentState: "ACTIVE"},
				{Name: "BYOD Mac — Baseline", DeploymentState: "ACTIVE"},
				{Name: "Kiosk — Retail", DeploymentState: "DRAFT"},
			},
			Benchmarks: []benchmarkEntry{
				{Title: "CIS macOS 15 Level 1", CompliancePct: 87.4, FailingRules: 19},
				{Title: "CIS macOS 14 Level 1", CompliancePct: 82.1, FailingRules: 27},
				{Title: "NIST 800-171 Baseline", CompliancePct: 91.6, FailingRules: 8},
			},
		},
	}

	f, err := os.Create("/tmp/dashboard-smoke.html")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := renderDashboard(f, data); err != nil {
		_ = f.Close()
		t.Fatalf("render: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	t.Logf("Dashboard written to /tmp/dashboard-smoke.html")
}
