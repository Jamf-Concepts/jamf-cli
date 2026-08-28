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
		PatchSpread: []patchVersionSpread{
			{Title: "Chrome", Versions: []patchVersionEntry{
				{Version: "125.0.6422.60", Count: 1890},
				{Version: "124.0.6367.91", Count: 340},
				{Version: "123.0.6312.86", Count: 120},
				{Version: "122.0.6261.94", Count: 47},
			}},
			{Title: "Firefox", Versions: []patchVersionEntry{
				{Version: "126.0", Count: 1680},
				{Version: "125.0.3", Count: 310},
				{Version: "124.0.2", Count: 210},
				{Version: "123.0", Count: 100},
			}},
			{Title: "Zoom", Versions: []patchVersionEntry{
				{Version: "6.0.2", Count: 1200},
				{Version: "5.17.11", Count: 520},
				{Version: "5.16.10", Count: 210},
				{Version: "5.15.5", Count: 117},
			}},
		},
		EnvStats: &environmentStats{
			Policies: 187, ConfigProfiles: 42, Scripts: 31, Packages: 156,
			ComputerSmartGrps: 64, MobileSmartGrps: 18, ExtAttributes: 23, Categories: 12,
		},
		Checkin: &checkinStatus{
			ComputersTotal: 2910, ComputersOverdue: 143,
			MobileTotal: 1633, MobileOverdue: 67,
			ThresholdDays: 7,
		},
		Hardware: &hardwareModels{
			ComputerModels: []modelCount{
				{Model: "MacBook Pro (16-inch, 2024)", Count: 612},
				{Model: "MacBook Pro (14-inch, 2024)", Count: 489},
				{Model: "MacBook Air (15-inch, M3)", Count: 387},
				{Model: "MacBook Pro (16-inch, 2023)", Count: 341},
				{Model: "MacBook Air (M2)", Count: 298},
				{Model: "Mac mini (M4)", Count: 201},
				{Model: "iMac (24-inch, M3)", Count: 178},
				{Model: "Mac Studio (M2 Ultra)", Count: 124},
				{Model: "Mac Pro (2023)", Count: 89},
				{Model: "MacBook Pro (13-inch, M2)", Count: 128},
			},
			MobileModels: []modelCount{
				{Model: "iPad Air (M2)", Count: 412},
				{Model: "iPad Pro 12.9-inch (6th gen)", Count: 334},
				{Model: "iPad (10th gen)", Count: 289},
				{Model: "iPhone 15 Pro", Count: 201},
				{Model: "iPad mini (6th gen)", Count: 178},
				{Model: "iPhone 14 Pro", Count: 112},
				{Model: "iPad Air (5th gen)", Count: 66},
			},
		},
		ComputerSmartGroups: &smartGroupSummary{
			TotalFleet: 2910,
			Groups: []smartGroupEntry{
				{Name: "Encrypted Macs", Count: 2705},
				{Name: "FileVault Disabled", Count: 142},
				{Name: "Stale Check-in (30d+)", Count: 89},
				{Name: "Missing Management", Count: 34},
				{Name: "VPN Configured", Count: 2401},
				{Name: "Dev Team", Count: 612},
			},
		},
		MobileSmartGroups: &smartGroupSummary{
			TotalFleet: 1633,
			Groups: []smartGroupEntry{
				{Name: "Managed iPads", Count: 1420},
				{Name: "Supervised Devices", Count: 1105},
				{Name: "BYOD iPhones", Count: 201},
				{Name: "Shared iPads", Count: 89},
				{Name: "DEP Enrolled", Count: 1350},
			},
		},
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
