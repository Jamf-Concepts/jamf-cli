// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/blueprints"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/compliancebenchmarks"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/ddmreport"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/devices"
)

// auditSeverity levels for findings.
const (
	severityCritical = "CRITICAL"
	severityWarning  = "WARNING"
	severityInfo     = "INFO"
)

// auditResult represents a single audit finding.
type auditResult struct {
	Category       string `json:"category"`
	Severity       string `json:"severity"`
	Name           string `json:"name"`
	AffectedCount  int    `json:"affected"`
	Recommendation string `json:"recommendation"`
}

// auditCheck defines a single audit check.
type auditCheck struct {
	Category string
	Name     string
	Run      func(ctx context.Context, client registry.HTTPClient, days int) (*auditResult, error)
}

func newAuditCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		checks string
		days   int
	)

	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Run cross-resource health checks on a Jamf Pro instance",
		Long: `Audit performs automated health checks across your Jamf Pro instance.

Check categories: security, compliance, hygiene, enrollment, platform.
Use --checks to filter to a specific category.
Platform checks run automatically when platform gateway auth is configured.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAudit(cmd.Context(), cliCtx, auditOptions{
				Checks: checks,
				Days:   days,
			})
		},
	}

	cmd.Flags().StringVar(&checks, "checks", "", "filter to category: security, compliance, hygiene, enrollment, platform")
	cmd.Flags().IntVar(&days, "days", 14, "stale check-in threshold in days")

	return cmd
}

type auditOptions struct {
	Checks string
	Days   int
}

// allAuditChecks returns the full set of audit checks.
func allAuditChecks() []auditCheck {
	return []auditCheck{
		// Security
		{Category: "security", Name: "Unencrypted devices", Run: checkUnencryptedDevices},
		{Category: "security", Name: "Gatekeeper disabled", Run: checkGatekeeper},

		// Compliance
		{Category: "compliance", Name: "Stale check-in", Run: checkStaleCheckin},
		{Category: "compliance", Name: "Failed MDM commands", Run: checkFailedMDMCommands},

		// Hygiene
		{Category: "hygiene", Name: "Empty smart groups", Run: checkEmptySmartGroups},
		{Category: "hygiene", Name: "Policies with no scope", Run: checkPoliciesNoScope},
		{Category: "hygiene", Name: "Empty categories", Run: checkEmptyCategories},
		{Category: "hygiene", Name: "Duplicate serial numbers", Run: checkDuplicateSerials},

		// Enrollment
		{Category: "enrollment", Name: "DEP token expiry", Run: checkDEPTokenExpiry},
		{Category: "enrollment", Name: "Prestage coverage", Run: checkPrestageCoverage},
		{Category: "security", Name: "Notification alerts", Run: checkNotificationAlerts},
	}
}

func runAudit(ctx context.Context, cliCtx *registry.CLIContext, opts auditOptions) error {
	client := cliCtx.Client
	checks := allAuditChecks()

	// Filter by category if specified
	wantPlatformOnly := opts.Checks == "platform"
	if opts.Checks != "" && !wantPlatformOnly {
		var filtered []auditCheck
		for _, c := range checks {
			if c.Category == opts.Checks {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("no checks match category %q (valid: security, compliance, hygiene, enrollment, platform)", opts.Checks)
		}
		checks = filtered
	}

	var results []auditResult

	// Run Pro API checks (skip when --checks platform is specified)
	if !wantPlatformOnly {
		for _, check := range checks {
			result, err := check.Run(ctx, client, opts.Days)
			if err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: check %q failed: %v\n", check.Name, err)
				continue
			}
			if result != nil {
				results = append(results, *result)
			}
		}
	}

	// Run platform checks when platform auth is active (or --checks platform)
	if cliCtx.PlatformSDKClient != nil && (opts.Checks == "" || wantPlatformOnly) {
		platformResults := runPlatformAuditChecks(ctx, cliCtx.PlatformSDKClient)
		results = append(results, platformResults...)
	}

	if len(results) == 0 {
		fmt.Fprintln(os.Stderr, "All checks passed — no findings.")
		return nil
	}

	// Convert to output format
	rows := make([]map[string]any, len(results))
	for i, r := range results {
		rows[i] = map[string]any{
			"category":       r.Category,
			"severity":       r.Severity,
			"name":           r.Name,
			"affected":       r.AffectedCount,
			"recommendation": r.Recommendation,
		}
	}

	formatter := output.New(outputFmt, noColor, wide)
	if outFile != "" {
		data, err := json.Marshal(rows)
		if err != nil {
			return fmt.Errorf("marshalling audit results: %w", err)
		}
		return cliCtx.Output.PrintRaw(data)
	}
	return formatter.Print(rows)
}

// --- Individual Check Implementations ---

func checkUnencryptedDevices(ctx context.Context, client registry.HTTPClient, _ int) (*auditResult, error) {
	// Fetch all computers with DISK_ENCRYPTION section to check FileVault status.
	// v3 moved FileVault data from the SECURITY section to DISK_ENCRYPTION.
	all, err := FetchAllPaginated(ctx, client, "/v4/computers-inventory?section=DISK_ENCRYPTION", 100)
	if err != nil {
		return nil, err
	}
	count := 0
	for _, comp := range all {
		diskEnc, _ := comp["diskEncryption"].(map[string]any)
		if diskEnc == nil {
			continue
		}
		fv := fileVaultStatus(diskEnc)
		if fv != "" && fv != statusFVEncrypted {
			count++
		}
	}
	if count == 0 {
		return nil, nil
	}
	return &auditResult{
		Category:       "security",
		Severity:       severityCritical,
		Name:           "Unencrypted devices",
		AffectedCount:  count,
		Recommendation: "Enable FileVault via configuration profile or policy",
	}, nil
}

func checkGatekeeper(ctx context.Context, client registry.HTTPClient, _ int) (*auditResult, error) {
	// Fetch all computers with SECURITY section to check Gatekeeper locally
	all, err := FetchAllPaginated(ctx, client, "/v4/computers-inventory?section=SECURITY", 100)
	if err != nil {
		return nil, err
	}
	count := 0
	for _, comp := range all {
		sec, _ := comp["security"].(map[string]any)
		if sec == nil {
			continue
		}
		gk, _ := sec["gatekeeperStatus"].(string)
		if gk == "DISABLED" || gk == "Disabled" {
			count++
		}
	}
	if count == 0 {
		return nil, nil
	}
	return &auditResult{
		Category:       "security",
		Severity:       severityWarning,
		Name:           "Gatekeeper disabled",
		AffectedCount:  count,
		Recommendation: "Enable Gatekeeper via configuration profile",
	}, nil
}

func checkStaleCheckin(ctx context.Context, client registry.HTTPClient, days int) (*auditResult, error) {
	// Compute the ISO 8601 cutoff date
	cutoff := timeNow().AddDate(0, 0, -days).UTC().Format("2006-01-02")
	path := fmt.Sprintf("/v4/computers-inventory?section=GENERAL&page-size=1&filter=general.lastContactTime%%3C%s", cutoff)
	data, err := fetchJSON(ctx, client, path)
	if err != nil {
		return nil, err
	}
	tc, _ := data["totalCount"].(float64)
	count := int(tc)
	if count == 0 {
		return nil, nil
	}
	return &auditResult{
		Category:       "compliance",
		Severity:       severityWarning,
		Name:           fmt.Sprintf("Stale check-in (>%d days)", days),
		AffectedCount:  count,
		Recommendation: fmt.Sprintf("Investigate devices not checking in for %d+ days", days),
	}, nil
}

func checkFailedMDMCommands(ctx context.Context, client registry.HTTPClient, _ int) (*auditResult, error) {
	data, err := fetchJSON(ctx, client, "/v2/mdm/commands?filter=status%3D%3DError&page-size=1")
	if err != nil {
		return nil, err
	}
	tc, _ := data["totalCount"].(float64)
	count := int(tc)
	if count == 0 {
		return nil, nil
	}
	sev := severityWarning
	if count > 100 {
		sev = severityCritical
	}
	return &auditResult{
		Category:       "compliance",
		Severity:       sev,
		Name:           "Failed MDM commands",
		AffectedCount:  count,
		Recommendation: "Review failed MDM commands and re-issue or investigate blocked devices",
	}, nil
}

func checkEmptySmartGroups(ctx context.Context, client registry.HTTPClient, _ int) (*auditResult, error) {
	// /v1/computer-groups returns a plain array — FetchAllPaginated handles both formats
	groups, err := FetchAllPaginated(ctx, client, "/v1/computer-groups", 100)
	if err != nil {
		return nil, err
	}

	emptyCount := 0
	for _, g := range groups {
		smart, _ := g["smartGroup"].(bool)
		if !smart {
			continue
		}
		if count, ok := g["memberCount"].(float64); ok && count == 0 {
			emptyCount++
		}
	}
	if emptyCount == 0 {
		return nil, nil
	}
	return &auditResult{
		Category:       "hygiene",
		Severity:       severityInfo,
		Name:           "Empty smart groups",
		AffectedCount:  emptyCount,
		Recommendation: "Review and remove unused smart groups to reduce evaluation overhead",
	}, nil
}

func checkPoliciesNoScope(ctx context.Context, client registry.HTTPClient, _ int) (*auditResult, error) {
	raw, err := FetchClassicList(ctx, client, "/JSSResource/policies", "policies")
	if err != nil {
		return nil, err
	}

	noScopeCount := 0
	skippedCount := 0
	for _, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		id := extractID(m)
		if id == "" {
			continue
		}

		// Fetch detail to check scope
		path := fmt.Sprintf("/JSSResource/policies/id/%s", id)
		detail, err := fetchJSON(ctx, client, path)
		if err != nil {
			skippedCount++
			fmt.Fprintf(os.Stderr, "WARNING: skipping policy id=%s: %v\n", id, err)
			continue
		}
		detail = unwrapClassicDetail(detail)
		scope, ok := detail["scope"].(map[string]any)
		if !ok {
			noScopeCount++
			continue
		}
		// Check if scope has any targets
		allComputers, _ := scope["all_computers"].(bool)
		allJSS, _ := scope["all_jss_users"].(bool)
		computers, _ := scope["computers"].([]any)
		groups, _ := scope["computer_groups"].([]any)
		if !allComputers && !allJSS && len(computers) == 0 && len(groups) == 0 {
			noScopeCount++
		}
	}

	if noScopeCount == 0 && skippedCount == 0 {
		return nil, nil
	}
	rec := "Add scope to policies or disable/delete unscoped ones"
	if skippedCount > 0 {
		rec = fmt.Sprintf("%s (%d policies could not be checked)", rec, skippedCount)
	}
	if noScopeCount == 0 {
		return nil, nil
	}
	return &auditResult{
		Category:       "hygiene",
		Severity:       severityWarning,
		Name:           "Policies with no scope",
		AffectedCount:  noScopeCount,
		Recommendation: rec,
	}, nil
}

func checkEmptyCategories(ctx context.Context, client registry.HTTPClient, _ int) (*auditResult, error) {
	cats, err := FetchAllPaginated(ctx, client, "/v1/categories", 100)
	if err != nil {
		return nil, err
	}
	// Categories don't have a member count in the API, but we can check
	// if policies/profiles reference them. For v1, just count total categories
	// that seem unused. This is a simplified check.
	if len(cats) == 0 {
		return nil, nil
	}

	// For now, report total categories as info (future: cross-reference with policies)
	return &auditResult{
		Category:       "hygiene",
		Severity:       severityInfo,
		Name:           "Categories audit",
		AffectedCount:  len(cats),
		Recommendation: "Review categories for unused entries; consider consolidating",
	}, nil
}

// checkDuplicateSerials flags serial numbers shared by more than one computer
// record. A replaced logic board re-enrolls as a fresh record, so the old and
// new records collide on serial — which breaks every serial-based lookup
// (get/update/patch --serial, device actions) since the CLI can no longer
// resolve the serial to a single device. Jamf smart groups cannot surface this
// (their criteria are per-record, not cross-record), so the audit is the only
// place to catch it. Blank serials are ignored — many pending/placeholder
// records legitimately share an empty serial.
func checkDuplicateSerials(ctx context.Context, client registry.HTTPClient, _ int) (*auditResult, error) {
	all, err := FetchAllPaginated(ctx, client, "/v4/computers-inventory?section=HARDWARE", 100)
	if err != nil {
		return nil, err
	}

	counts := make(map[string]int)
	for _, comp := range all {
		hw, _ := comp["hardware"].(map[string]any)
		if hw == nil {
			continue
		}
		serial, _ := hw["serialNumber"].(string)
		serial = strings.TrimSpace(serial)
		if serial == "" {
			continue
		}
		counts[serial]++
	}

	duplicatedSerials := 0
	affectedRecords := 0
	for _, n := range counts {
		if n > 1 {
			duplicatedSerials++
			affectedRecords += n
		}
	}
	if affectedRecords == 0 {
		return nil, nil
	}

	return &auditResult{
		Category:       "hygiene",
		Severity:       severityWarning,
		Name:           "Duplicate serial numbers",
		AffectedCount:  affectedRecords,
		Recommendation: fmt.Sprintf("%d serial(s) map to multiple computer records (typically a logic-board swap); run 'pro report duplicate-serials' to list the affected records, then delete the stale record for each so serial-based lookups resolve to one device", duplicatedSerials),
	}, nil
}

func checkDEPTokenExpiry(ctx context.Context, client registry.HTTPClient, _ int) (*auditResult, error) {
	data, err := fetchJSON(ctx, client, "/v1/device-enrollments")
	if err != nil {
		return nil, err
	}

	results, _ := data["results"].([]any)
	expiringCount := 0
	for _, r := range results {
		item, ok := r.(map[string]any)
		if !ok {
			continue
		}
		dateStr, ok := item["tokenExpirationDate"].(string)
		if !ok || dateStr == "" {
			continue
		}
		_, color := formatExpirationDate(dateStr, timeNow())
		if color == "red" || color == "yellow" {
			expiringCount++
		}
	}

	if expiringCount == 0 {
		return nil, nil
	}
	sev := severityWarning
	if expiringCount > 1 {
		sev = severityCritical
	}
	return &auditResult{
		Category:       "enrollment",
		Severity:       sev,
		Name:           "DEP token expiry",
		AffectedCount:  expiringCount,
		Recommendation: "Renew expiring DEP tokens in Apple Business Manager",
	}, nil
}

func checkPrestageCoverage(ctx context.Context, client registry.HTTPClient, _ int) (*auditResult, error) {
	// Compare enrollment instances vs prestages
	enrollData, err := fetchJSON(ctx, client, "/v1/device-enrollments")
	if err != nil {
		return nil, err
	}
	enrollCount, _ := enrollData["totalCount"].(float64)

	prestageData, err := fetchJSON(ctx, client, "/v3/computer-prestages?page-size=1")
	if err != nil {
		return nil, err
	}
	prestageCount, _ := prestageData["totalCount"].(float64)

	if int(enrollCount) == 0 {
		return nil, nil // No DEP instances, nothing to check
	}
	if int(prestageCount) > 0 {
		return nil, nil // Has prestages, looks configured
	}

	return &auditResult{
		Category:       "enrollment",
		Severity:       severityWarning,
		Name:           "No computer prestages configured",
		AffectedCount:  int(enrollCount),
		Recommendation: "Create computer prestages to auto-enroll DEP devices",
	}, nil
}

// checkNotificationAlerts fetches /v1/notifications once and checks for
// push certificate and VPP token issues. Returns the highest-severity finding.
func checkNotificationAlerts(ctx context.Context, client registry.HTTPClient, _ int) (*auditResult, error) {
	notifications, err := FetchAllPaginated(ctx, client, "/v1/notifications", 100)
	if err != nil {
		return nil, err
	}

	var pushExpired, vppExpired bool
	for _, n := range notifications {
		nType, _ := n["type"].(string)
		switch nType {
		case "PUSH_CERT_EXPIRED", "PUSH_CERT_WILL_EXPIRE":
			pushExpired = true
		case "VPP_ACCOUNT_EXPIRED", "VPP_ACCOUNT_WILL_EXPIRE":
			vppExpired = true
		}
	}

	// Push cert is more critical than VPP — return the worst finding.
	if pushExpired {
		return &auditResult{
			Category:       "security",
			Severity:       severityCritical,
			Name:           "Push certificate expired or expiring",
			AffectedCount:  1,
			Recommendation: "Renew the Apple Push Notification certificate in Jamf Pro > Settings > Push Certificates",
		}, nil
	}
	if vppExpired {
		return &auditResult{
			Category:       "security",
			Severity:       severityWarning,
			Name:           "VPP/Apps and Books token expired",
			AffectedCount:  1,
			Recommendation: "Renew the VPP token in Apple Business Manager and re-upload in Jamf Pro",
		}, nil
	}
	return nil, nil
}

// timeNow returns current time (extracted for testability).
var timeNow = func() time.Time { return time.Now() }

// runPlatformAuditChecks runs all platform-specific audit checks and returns findings.
// It fetches blueprints and benchmarks once upfront and passes them to individual checks.
func runPlatformAuditChecks(ctx context.Context, c *jamfplatform.Client) []auditResult {
	var results []auditResult

	// Fetch shared data once
	bps, bpErr := blueprints.New(c).ListBlueprints(ctx, nil, "")
	if bpErr != nil {
		fmt.Fprintf(os.Stderr, "WARNING: listing blueprints for audit: %v\n", bpErr)
	}

	bmResp, bmErr := compliancebenchmarks.New(c).ListBenchmarks(ctx)
	if bmErr != nil {
		fmt.Fprintf(os.Stderr, "WARNING: listing benchmarks for audit: %v\n", bmErr)
	}

	// Blueprint checks (skip all if listing failed)
	if bpErr == nil {
		if r := checkUndeployedBlueprints(bps); r != nil {
			results = append(results, *r)
		}
		if r := checkBlueprintFailures(ctx, c, bps); r != nil {
			results = append(results, *r)
		}
		if r := checkStaleBlueprints(bps); r != nil {
			results = append(results, *r)
		}
	}

	// Benchmark checks (skip all if listing failed)
	if bmErr == nil {
		if r := checkBenchmarkUpdates(bmResp); r != nil {
			results = append(results, *r)
		}
		if r := checkBenchmarkMonitorOnly(ctx, c, bmResp); r != nil {
			results = append(results, *r)
		}
	}

	// Empty scope check uses both (tolerates either being nil)
	if r := checkEmptyPlatformScope(ctx, c, bps, bmResp); r != nil {
		results = append(results, *r)
	}

	// Devices with failed DDM declarations
	if r := checkFailedDDMDeclarations(ctx, c); r != nil {
		results = append(results, *r)
	}

	return results
}

func checkUndeployedBlueprints(bps []blueprints.BlueprintOverview) *auditResult {
	count := 0
	for _, bp := range bps {
		if bp.DeploymentState == nil || bp.DeploymentState.State != "DEPLOYED" {
			count++
		}
	}
	if count == 0 {
		return nil
	}
	return &auditResult{
		Category:       "platform",
		Severity:       severityWarning,
		Name:           "Undeployed blueprints",
		AffectedCount:  count,
		Recommendation: "Deploy or remove blueprints that are not in use",
	}
}

func checkBlueprintFailures(ctx context.Context, c *jamfplatform.Client, bps []blueprints.BlueprintOverview) *auditResult {
	count := 0
	for _, bp := range bps {
		if bp.DeploymentState == nil || bp.DeploymentState.State != "DEPLOYED" {
			continue
		}
		report, err := blueprints.New(c).GetBlueprintReport(ctx, bp.ID)
		if err != nil {
			continue
		}
		if report.Failed > 0 {
			count++
		}
	}
	if count == 0 {
		return nil
	}
	return &auditResult{
		Category:       "platform",
		Severity:       severityCritical,
		Name:           "Blueprint deployment failures",
		AffectedCount:  count,
		Recommendation: "Review blueprint deployment reports for failed devices",
	}
}

func checkStaleBlueprints(bps []blueprints.BlueprintOverview) *auditResult {
	count := 0
	for _, bp := range bps {
		if bp.DeploymentState == nil || bp.DeploymentState.State != "DEPLOYED" || bp.DeploymentState.LastDeployment == nil {
			continue
		}
		// Blueprint was updated after its last deployment
		if bp.Updated.After(bp.DeploymentState.LastDeployment.Started) {
			count++
		}
	}
	if count == 0 {
		return nil
	}
	return &auditResult{
		Category:       "platform",
		Severity:       severityWarning,
		Name:           "Stale blueprints (updated since last deploy)",
		AffectedCount:  count,
		Recommendation: "Redeploy blueprints that have been modified since last deployment",
	}
}

func checkBenchmarkUpdates(resp *compliancebenchmarks.BenchmarksResponseV2) *auditResult {
	count := 0
	for _, b := range resp.Benchmarks {
		if b.UpdateAvailable {
			count++
		}
	}
	if count == 0 {
		return nil
	}
	return &auditResult{
		Category:       "platform",
		Severity:       severityWarning,
		Name:           "Compliance benchmarks with updates available",
		AffectedCount:  count,
		Recommendation: "Review and apply available baseline updates to compliance benchmarks",
	}
}

func checkBenchmarkMonitorOnly(ctx context.Context, c *jamfplatform.Client, resp *compliancebenchmarks.BenchmarksResponseV2) *auditResult {
	count := 0
	for _, b := range resp.Benchmarks {
		bm, err := compliancebenchmarks.New(c).GetBenchmark(ctx, b.ID)
		if err != nil {
			continue
		}
		if bm.EnforcementMode == "MONITOR" && bm.CanSwitchToEnforce {
			count++
		}
	}
	if count == 0 {
		return nil
	}
	return &auditResult{
		Category:       "platform",
		Severity:       severityInfo,
		Name:           "Compliance benchmarks in MONITOR mode",
		AffectedCount:  count,
		Recommendation: "Consider switching eligible benchmarks from MONITOR to ENFORCE mode",
	}
}

func checkEmptyPlatformScope(ctx context.Context, c *jamfplatform.Client, bps []blueprints.BlueprintOverview, bmResp *compliancebenchmarks.BenchmarksResponseV2) *auditResult {
	count := 0

	// Check blueprints with empty scope
	for _, bp := range bps {
		detail, err := blueprints.New(c).GetBlueprint(ctx, bp.ID)
		if err != nil {
			continue
		}
		if detail.Scope == nil || len(detail.Scope.DeviceGroups) == 0 {
			count++
		}
	}

	// Check benchmarks with empty target
	if bmResp != nil {
		for _, b := range bmResp.Benchmarks {
			if b.Target == nil || len(b.Target.DeviceGroups) == 0 {
				count++
			}
		}
	}

	if count == 0 {
		return nil
	}
	return &auditResult{
		Category:       "platform",
		Severity:       severityWarning,
		Name:           "Empty platform scope",
		AffectedCount:  count,
		Recommendation: "Add device groups to blueprints and benchmarks with empty scope",
	}
}

func checkFailedDDMDeclarations(ctx context.Context, c *jamfplatform.Client) *auditResult {
	devices, err := devices.New(c).ListDevices(ctx, nil, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: platform check %q failed: %v\n", "Failed DDM declarations", err)
		return nil
	}

	const maxDevicesToScan = 100
	scanned := 0
	devicesWithFailures := 0
	for _, dev := range devices {
		if scanned >= maxDevicesToScan {
			break
		}
		scanned++
		declarations, err := ddmreport.New(c).GetDeviceDeclarationReportFiltered(ctx, dev.ID, ddmAllDeclarationsFilter, nil)
		if err != nil {
			continue
		}
		hasFailed := false
		for _, d := range declarations {
			if d.Status == "SUCCESSFUL" && d.ValidityState == "VALID" {
				continue
			}
			// Skip if the only reason is non-actionable
			if onlyHasIgnorableReasons(d.Reasons) {
				continue
			}
			hasFailed = true
			break
		}
		if hasFailed {
			devicesWithFailures++
		}
	}

	if devicesWithFailures == 0 {
		return nil
	}
	rec := "Run 'pro ddm-reports device <serial>' to diagnose declaration failures"
	if len(devices) > maxDevicesToScan {
		rec = fmt.Sprintf("Scanned %d of %d devices; run 'pro ddm-reports errors <decl-id>' for full analysis", maxDevicesToScan, len(devices))
	}
	return &auditResult{
		Category:       "platform",
		Severity:       severityCritical,
		Name:           "Devices with failed DDM declarations",
		AffectedCount:  devicesWithFailures,
		Recommendation: rec,
	}
}
