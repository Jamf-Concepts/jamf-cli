// Copyright 2026, Jamf Software LLC

package commands

import (
	"sort"
	"time"
)

// DashboardData holds all collected data for the HTML report template.
type DashboardData struct {
	Title       string
	GeneratedAt time.Time
	CLIVersion  string
	Profiles    []dashboardProfile

	// IncompleteSections names the sections whose data could not be fetched, and
	// TotalSections how many the run attempted. Together they drive an in-HTML
	// banner: a recipient who receives only the file (not the stderr warnings,
	// not the exit code) can otherwise not tell a failed section from a
	// genuinely empty one, and would read a partial report as authoritative.
	//
	// Names rather than a count, because a count is not checkable by the
	// reader. It also used to be a count of *fetches*, so a Security Cloud
	// outage — five calls behind one rendered section — reported "5 sections
	// could not be collected" on a report with one section missing.
	IncompleteSections []string
	TotalSections      int

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
	SecurityCloud       *securityCloudStatus
	Cleanup             *cleanupAnalysis
	OrgStructure        *orgStructure
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
	// UsersMissing marks the user count as unfetched rather than zero. Users
	// is a headline figure, so a failed /v1/users used to render "0 Users" —
	// a number the reader has no way to tell from a real one.
	UsersMissing bool
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
	// ChecksSkipped counts the audit checks that errored. Every figure in the
	// section is a count of findings, so a failed check removes an alert rather
	// than showing an empty one — "0 Critical" and "the critical check did not
	// run" render identically without this.
	ChecksSkipped int
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
	// TitlesSkipped counts the patch titles whose summary could not be read.
	// Those titles are absent from Titles, so the section is a subset of the
	// instance's patch estate rather than all of it.
	TitlesSkipped int
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
	// StaleMissing / MDMMissing mark a count as unfetched. The template hides
	// an alert card whose count is zero, so a failed fetch used to remove the
	// card entirely — the quietest possible way to report a problem.
	StaleMissing bool
	MDMMissing   bool
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

// Environment stat labels. They double as the Missing map's keys and as the
// label the template renders, so a stat cannot be marked missing under a name
// the reader never sees.
const (
	envStatPolicies          = "Policies"
	envStatConfigProfiles    = "Config Profiles"
	envStatScripts           = "Scripts"
	envStatPackages          = "Packages"
	envStatComputerSmartGrps = "Computer Smart Groups"
	envStatMobileSmartGrps   = "Mobile Smart Groups"
	envStatExtAttributes     = "Extension Attributes"
	envStatCategories        = "Categories"
)

type environmentStats struct {
	Policies          int
	ConfigProfiles    int
	Scripts           int
	Packages          int
	ComputerSmartGrps int
	MobileSmartGrps   int
	ExtAttributes     int
	Categories        int
	// Missing holds the labels whose count could not be fetched. Each of these
	// eight is an independent request, and a failed one used to render 0.
	Missing map[string]bool
}

// Unavailable reports whether the named stat could not be fetched, so the
// template can render "—" where it would otherwise render a confident zero.
func (e *environmentStats) Unavailable(label string) bool { return e.Missing[label] }

// MissingLabels lists the unfetched stats, sorted, for the section's own note.
func (e *environmentStats) MissingLabels() []string {
	labels := make([]string, 0, len(e.Missing))
	for label := range e.Missing {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	return labels
}

type checkinStatus struct {
	ComputersTotal   int
	ComputersOverdue int
	MobileTotal      int
	MobileOverdue    int
	ThresholdDays    int
	// ComputerDataMissing / MobileDataMissing mark a half whose total or
	// overdue count could not be fetched. Either one alone is enough to make
	// the ratio wrong in the direction that reads as good news: a failed
	// overdue query beside a successful total renders a 100% green ring on a
	// fleet where nothing has checked in.
	ComputerDataMissing bool
	MobileDataMissing   bool
}

// ComputersReliable and MobileReliable gate the rings. Overall needs both,
// since it sums the two.
func (c *checkinStatus) ComputersReliable() bool { return !c.ComputerDataMissing }
func (c *checkinStatus) MobileReliable() bool    { return !c.MobileDataMissing }
func (c *checkinStatus) OverallReliable() bool {
	return !c.ComputerDataMissing && !c.MobileDataMissing
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
	// ComputerModelsMissing / MobileModelsMissing mark a half that could not be
	// fetched, so an empty table reads as "not available" rather than as an
	// estate with no models in it.
	ComputerModelsMissing bool
	MobileModelsMissing   bool
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
	// BenchmarksSkipped counts the benchmarks whose compliance or rule stats
	// could not be read. Those are absent from Benchmarks, so the list is a
	// subset rather than the tenant's whole set.
	BenchmarksSkipped int
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

type cleanupAnalysis struct {
	DisabledPolicies int
	UnscopedPolicies int
	UnscopedProfiles int
	UnusedPackages   int
	UnusedScripts    int
	// PoliciesSkipped is the number of policy detail fetches that failed. When
	// non-zero the reference sets are incomplete, so UnusedPackages/UnusedScripts
	// would over-count, and DisabledPolicies/UnscopedPolicies would under-count:
	// both are tallied inside the same loop body the error path skips.
	PoliciesSkipped int
	// ProfilesSkipped is the same for config profile details, which
	// UnscopedProfiles is derived from.
	ProfilesSkipped int
}

// PackageScriptUsageReliable reports whether every policy detail was read. Unused
// package/script counts derive from the union of references across all policies,
// so a single unread policy can make an in-use package look unused.
func (c *cleanupAnalysis) PackageScriptUsageReliable() bool {
	return c.PoliciesSkipped == 0
}

// PolicyCountsReliable gates the two figures derived from the policy loop's own
// body. They were rendered as bare numbers directly above two rows honestly
// reading "not available (N policies unreadable)" — the same pass, two
// different stories.
func (c *cleanupAnalysis) PolicyCountsReliable() bool { return c.PoliciesSkipped == 0 }

// ProfileCountsReliable gates UnscopedProfiles.
func (c *cleanupAnalysis) ProfileCountsReliable() bool { return c.ProfilesSkipped == 0 }

// Reliable reports whether every figure in the section can be published.
func (c *cleanupAnalysis) Reliable() bool {
	return c.PolicyCountsReliable() && c.ProfileCountsReliable()
}

// Total sums only the figures that can be published, so a partial tally is
// never presented as a complete one.
func (c *cleanupAnalysis) Total() int {
	total := 0
	if c.PolicyCountsReliable() {
		total += c.DisabledPolicies + c.UnscopedPolicies
	}
	if c.ProfileCountsReliable() {
		total += c.UnscopedProfiles
	}
	if c.PackageScriptUsageReliable() {
		total += c.UnusedPackages + c.UnusedScripts
	}
	return total
}

type orgStructure struct {
	Sites       []orgEntry
	Buildings   []orgEntry
	Departments []orgEntry
	Categories  []orgEntry
	// CategoriesMissing marks the category list as unfetched, so an empty
	// Categories reads as "not available" rather than as no categories.
	CategoriesMissing bool
	// CategoryCountsReliable reports whether every object the category tally
	// covers was read. When false the counts are a floor, and the column is
	// withheld rather than shown as a figure — a column of zeros reads as
	// "these categories are empty", which is what it did when the count was
	// never computed at all.
	CategoryCountsReliable bool
	// CategorySources names the object types the tally counted, so the column
	// header says what it is a count of rather than claiming "items".
	CategorySources []string
}

// categoryUnassigned is what Jamf renders for an object filed under no
// category. It is not a category, so it is excluded from both the category
// list and the per-category tally.
const categoryUnassigned = "No category assigned"

type orgEntry struct {
	Name  string
	Count int
}

type securityCloudStatus struct {
	ZtnaApps       int
	ZtnaGateways   int
	DeviceGroups   int
	DnsZones       int
	UemConnector   bool
	AppsByCategory []secCloudCategory
}

type secCloudCategory struct {
	Name  string
	Count int
}
