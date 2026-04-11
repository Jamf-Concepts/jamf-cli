// Copyright 2026, Jamf Software LLC

package commands

import "time"

// DashboardData holds all collected data for the HTML report template.
type DashboardData struct {
	Title       string
	GeneratedAt time.Time
	CLIVersion  string
	Profiles    []dashboardProfile

	// Conditional sections — nil means "don't render this section"
	Fleet    *fleetSummary
	Security *securityPosture
	Audit    *auditSummary
	Patch    *patchCompliance
	Devices  *deviceCompliance
	OSDist   *osDistribution
	Protect  *protectCoverage
	Platform *platformStatus
}

type dashboardProfile struct {
	Name    string
	Product string // "pro", "protect", or "platform"
	URL     string
}

type fleetSummary struct {
	ManagedComputers   int
	UnmanagedComputers int
	ManagedMobile      int
	UnmanagedMobile    int
	Users              int
}

type securityPosture struct {
	Total             int
	FileVaultEnabled  int
	FirewallEnabled   int
	GatekeeperEnabled int
	SIPEnabled        int
}

func (s *securityPosture) Pct(count int) float64 {
	if s.Total == 0 {
		return 0
	}
	return float64(count) / float64(s.Total) * 100
}

type auditSummary struct {
	Results []auditResult
}

func (a *auditSummary) CriticalCount() int {
	n := 0
	for _, r := range a.Results {
		if r.Severity == severityCritical {
			n++
		}
	}
	return n
}

func (a *auditSummary) WarningCount() int {
	n := 0
	for _, r := range a.Results {
		if r.Severity == severityWarning {
			n++
		}
	}
	return n
}

func (a *auditSummary) InfoCount() int {
	n := 0
	for _, r := range a.Results {
		if r.Severity == severityInfo {
			n++
		}
	}
	return n
}

type patchCompliance struct {
	Titles []patchTitle
}

type patchTitle struct {
	Name          string
	LatestVersion string
	UpToDate      int
	OutOfDate     int
	Total         int
	CompliancePct float64
}

type deviceCompliance struct {
	StaleDevices       int
	FailedMDMCommands  int
	StaleThresholdDays int
}

type osDistribution struct {
	Versions []osVersionCount
}

type osVersionCount struct {
	Version string
	Count   int
}

type protectCoverage struct {
	Plans           int
	AnalyticsTotal  int
	AnalyticsActive int
	Endpoints       int
	AnalyticSets    int
	ExceptionSets   int
}

type platformStatus struct {
	Blueprints []blueprintEntry
	Benchmarks []benchmarkEntry
}

type blueprintEntry struct {
	Name            string
	DeploymentState string
}

type benchmarkEntry struct {
	Title         string
	CompliancePct float64
	FailingRules  int
}
