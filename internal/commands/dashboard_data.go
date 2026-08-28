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
	Fleet               *fleetSummary
	Security            *securityPosture
	Audit               *auditSummary
	Patch               *patchCompliance
	PatchSpread         []patchVersionSpread
	Devices             *deviceCompliance
	OSDist              *osDistribution
	ComputerSmartGroups *smartGroupSummary
	MobileSmartGroups   *smartGroupSummary
	EnvStats            *environmentStats
	Checkin             *checkinStatus
	Hardware            *hardwareModels
	Protect             *protectCoverage
	Platform            *platformStatus
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

func (f *fleetSummary) TotalComputers() int { return f.ManagedComputers + f.UnmanagedComputers }
func (f *fleetSummary) TotalMobile() int    { return f.ManagedMobile + f.UnmanagedMobile }

func (f *fleetSummary) ComputerManagedPct() float64 {
	t := f.ManagedComputers + f.UnmanagedComputers
	if t == 0 {
		return 0
	}
	return float64(f.ManagedComputers) / float64(t) * 100
}

func (f *fleetSummary) MobileManagedPct() float64 {
	t := f.ManagedMobile + f.UnmanagedMobile
	if t == 0 {
		return 0
	}
	return float64(f.ManagedMobile) / float64(t) * 100
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

func (p *protectCoverage) ActiveAnalyticsPct() float64 {
	if p.AnalyticsTotal == 0 {
		return 0
	}
	return float64(p.AnalyticsActive) / float64(p.AnalyticsTotal) * 100
}

type smartGroupSummary struct {
	Groups     []smartGroupEntry
	TotalFleet int
}

type smartGroupEntry struct {
	Name  string
	Count int
}

type environmentStats struct {
	Policies          int
	ConfigProfiles    int
	Scripts           int
	Packages          int
	ComputerSmartGrps int
	MobileSmartGrps   int
	ExtAttributes     int
	Categories        int
}

type checkinStatus struct {
	ComputersTotal   int
	ComputersOverdue int
	MobileTotal      int
	MobileOverdue    int
	ThresholdDays    int
}

func (c *checkinStatus) TotalOverdue() int { return c.ComputersOverdue + c.MobileOverdue }
func (c *checkinStatus) TotalDevices() int { return c.ComputersTotal + c.MobileTotal }

func (c *checkinStatus) OverduePct(overdue, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(overdue) / float64(total) * 100
}

func (c *checkinStatus) CheckedInPct(overdue, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(total-overdue) / float64(total) * 100
}

type hardwareModels struct {
	ComputerModels []modelCount
	MobileModels   []modelCount
}

type modelCount struct {
	Model string
	Count int
}

type patchVersionSpread struct {
	Title    string
	Versions []patchVersionEntry
}

type patchVersionEntry struct {
	Version string
	Count   int
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
