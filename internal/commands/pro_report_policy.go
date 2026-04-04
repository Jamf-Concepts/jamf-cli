// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// Default concurrency for Classic API calls. The Classic API is sensitive to
// concurrent load — keep this deliberately low to avoid hammering the server.
const policyReportConcurrency = 3

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// policyHealthReport holds the complete health check result.
type policyHealthReport struct {
	Summary          policyHealthSummary
	ConfigFindings   []policyHealthFinding
	PolicyFailures   []policyFailureSummary
	ComputerFailures []computerFailureSummary
}

type policyHealthSummary struct {
	TotalPolicies      int
	Enabled            int
	Disabled           int
	ConfigFindings     int
	Warnings           int
	Info               int
	PoliciesWithFails  int
	ComputersWithFails int
	ComputersScanned   int
	FetchErrors        int
	Days               int
}

type policyHealthFinding struct {
	Policy   string
	PolicyID string
	Check    string
	Severity string
	Detail   string
}

type policyFailureSummary struct {
	Policy      string
	PolicyID    string
	TotalRuns   int
	Failures    int
	FailureRate string
	LastFailure string
}

type computerFailureSummary struct {
	ComputerID   string
	ComputerName string
	Serial       string
	OSVersion    string
	Username     string
	DiskPctUsed  int // -1 = unknown
	TotalRuns    int
	Failures     int
	FailureRate  string
	LastFailure  string
}

type computerMeta struct {
	name, serial, osVersion, username string
	diskPctUsed                       int // boot partition % used, -1 if unknown
}

type policyInfo struct {
	ID   string
	Name string
	Data map[string]any
}

// computerHistoryResult pairs a computer ID with its policy log entries.
type computerHistoryResult struct {
	ComputerID string
	Entries    []policyLogEntry
}

// policyLogEntry represents a single policy execution from computer history.
type policyLogEntry struct {
	PolicyID   string
	PolicyName string
	Status     string
	Date       time.Time
}

// ---------------------------------------------------------------------------
// Command
// ---------------------------------------------------------------------------

func newReportPolicyStatusCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		days         int
		limit        int
		scanFailures bool
	)

	cmd := &cobra.Command{
		Use:   "policy-status",
		Short: "Fleet-wide policy health check",
		Long: `Scans all policies for configuration issues.

Config checks (always run, fast):
  no_scope, disabled_scoped, no_payload, no_trigger, no_category,
  scope_computers, scope_users, excl_computers, excl_users, limit_users

Use --scan-failures to also scan computer history for policy execution
failures. This is API-expensive (one Classic API call per computer, 3
concurrent) so it is off by default. Use --days to control the lookback
window and --limit to cap the sample size.

Examples:
  # Config checks only (default, fast)
  jamf-cli pro report policy-status

  # Include failure scan from last 30 days
  jamf-cli pro report policy-status --scan-failures

  # Failure scan with narrow window
  jamf-cli pro report policy-status --scan-failures --days 7

  # Cap sample size for large fleets
  jamf-cli pro report policy-status --scan-failures --limit 500`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Default to table output for this report command
			if !cmd.Flags().Changed("output") {
				outputFmt = "table"
			}
			// -1 means "user didn't set --limit, use smart default"
			effectiveLimit := limit
			if !cmd.Flags().Changed("limit") {
				effectiveLimit = -1
			}
			return runPolicyHealthCheck(cmd.Context(), cliCtx.Client, days, effectiveLimit, !scanFailures)
		},
	}

	cmd.Flags().BoolVar(&scanFailures, "scan-failures", false, "scan computer history for policy failures (API-expensive)")
	cmd.Flags().IntVar(&days, "days", 30, "look back N days for failure scan")
	cmd.Flags().IntVar(&limit, "limit", 0, "max computers to scan for failures (0 = scan all)")

	return cmd
}

// ---------------------------------------------------------------------------
// Main entry point
// ---------------------------------------------------------------------------

func runPolicyHealthCheck(ctx context.Context, client registry.HTTPClient, days, limit int, configOnly bool) error {
	// 1. Config checks (always run)
	policies, configFindings, fetchErrors, err := fetchAndCheckPolicies(ctx, client)
	if err != nil {
		return err
	}

	var enabled, disabled int
	for _, p := range policies {
		general, _ := p.Data["general"].(map[string]any)
		if general != nil {
			if e, ok := general["enabled"].(bool); ok && !e {
				disabled++
				continue
			}
		}
		enabled++
	}

	var warnings, info int
	for _, f := range configFindings {
		switch f.Severity {
		case "warning":
			warnings++
		case "info":
			info++
		}
	}

	// 2. Failure scan (unless --config-only)
	var policyFails []policyFailureSummary
	var computerFails []computerFailureSummary
	var computersScanned int

	if !configOnly {
		var scanErr error
		policyFails, computerFails, computersScanned, scanErr = runPolicyFailureScan(ctx, client, days, limit)
		if scanErr != nil {
			fmt.Fprintf(os.Stderr, "WARNING: failure scan failed: %v\n", scanErr)
		}
	}

	// 3. Build and print report
	report := &policyHealthReport{
		Summary: policyHealthSummary{
			TotalPolicies:      len(policies),
			Enabled:            enabled,
			Disabled:           disabled,
			ConfigFindings:     len(configFindings),
			Warnings:           warnings,
			Info:               info,
			PoliciesWithFails:  len(policyFails),
			ComputersWithFails: len(computerFails),
			ComputersScanned:   computersScanned,
			FetchErrors:        fetchErrors,
			Days:               days,
		},
		ConfigFindings:   configFindings,
		PolicyFailures:   policyFails,
		ComputerFailures: computerFails,
	}

	return printPolicyHealthReport(report, configOnly)
}

// ---------------------------------------------------------------------------
// Config checks
// ---------------------------------------------------------------------------

// fetchAndCheckPolicies fetches all policy details and runs config checks.
// Returns the policies, findings, fetch error count, and any fatal error.
func fetchAndCheckPolicies(ctx context.Context, client registry.HTTPClient) ([]policyInfo, []policyHealthFinding, int, error) {
	raw, err := FetchClassicList(ctx, client, "/JSSResource/policies", "policies")
	if err != nil {
		return nil, nil, 0, fmt.Errorf("listing policies: %w", err)
	}

	var policyIDs []string
	for _, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		policyIDs = append(policyIDs, extractID(m))
	}

	fmt.Fprintf(os.Stderr, "Fetching details for %d policies...\n", len(policyIDs))

	details, errs := BoundedParallelFetch(ctx, policyIDs, policyReportConcurrency,
		func(ctx context.Context, id string) (policyInfo, error) {
			detail, err := fetchJSON(ctx, client, fmt.Sprintf("/JSSResource/policies/id/%s", id))
			if err != nil {
				return policyInfo{}, err
			}
			detail = unwrapClassicDetail(detail)
			general, _ := detail["general"].(map[string]any)
			name := ""
			if general != nil {
				name, _ = general["name"].(string)
			}
			return policyInfo{ID: id, Name: name, Data: detail}, nil
		})

	if len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "WARNING: %d policy detail fetches failed.\n", len(errs))
	}

	findings := analyzePolicies(details)
	return details, findings, len(errs), nil
}

// analyzePolicies runs all config checks against the given policies.
// Pure function — no I/O, fully testable.
func analyzePolicies(policies []policyInfo) []policyHealthFinding {
	var findings []policyHealthFinding

	for _, p := range policies {
		general, _ := p.Data["general"].(map[string]any)
		scope, _ := p.Data["scope"].(map[string]any)
		selfService, _ := p.Data["self_service"].(map[string]any)

		enabled := true
		if general != nil {
			if e, ok := general["enabled"].(bool); ok {
				enabled = e
			}
		}

		hasScope := scopeHasTargets(scope)

		// Check: disabled but scoped
		if !enabled && hasScope {
			findings = append(findings, policyHealthFinding{
				Policy: p.Name, PolicyID: p.ID,
				Check: "disabled_scoped", Severity: "info",
				Detail: "Disabled but still scoped",
			})
		}

		// Check: no scope — nothing configured in scope at all (not a smart
		// group that's currently empty — that group is still in scope config).
		if enabled && !hasScope {
			findings = append(findings, policyHealthFinding{
				Policy: p.Name, PolicyID: p.ID,
				Check: "no_scope", Severity: "warning",
				Detail: "No targets in scope",
			})
		}

		// Check: no payload
		if enabled && !policyHasPayload(p.Data) {
			findings = append(findings, policyHealthFinding{
				Policy: p.Name, PolicyID: p.ID,
				Check: "no_payload", Severity: "warning",
				Detail: "No actions configured",
			})
		}

		// Check: no trigger (and not self-service)
		isSelfService := false
		if selfService != nil {
			isSelfService, _ = selfService["use_for_self_service"].(bool)
		}
		if enabled && general != nil && !policyHasTrigger(general) && !isSelfService {
			findings = append(findings, policyHealthFinding{
				Policy: p.Name, PolicyID: p.ID,
				Check: "no_trigger", Severity: "warning",
				Detail: "No triggers, not Self Service",
			})
		}

		// Check: no category
		if enabled && !policyHasCategory(general) {
			findings = append(findings, policyHealthFinding{
				Policy: p.Name, PolicyID: p.ID,
				Check: "no_category", Severity: "info",
				Detail: "Uncategorised",
			})
		}

		// Scalability checks: individual computers/users in scope instead of groups
		if enabled {
			findings = append(findings, checkIndividualTargets(p, scope)...)
		}

	}

	// Sort: warnings first, then by check name, then policy name
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Severity != findings[j].Severity {
			return findings[i].Severity > findings[j].Severity // "warning" > "info"
		}
		if findings[i].Check != findings[j].Check {
			return findings[i].Check < findings[j].Check
		}
		return findings[i].Policy < findings[j].Policy
	})

	return findings
}

func scopeHasTargets(scope map[string]any) bool {
	if scope == nil {
		return false
	}
	if allComp, _ := scope["all_computers"].(bool); allComp {
		return true
	}
	// Classic API returns nested objects for scope collections:
	//   empty:    "" (empty string) or absent
	//   single:   {"computer_group": {"id": 5, ...}}     (map)
	//   multiple: {"computer_group": [{"id": 5}, ...]}   (map with array)
	//   modern:   [{"id": 5}, ...]                        (direct array)
	for _, key := range []string{"computers", "computer_groups", "buildings", "departments"} {
		if scopeFieldHasEntries(scope[key]) {
			return true
		}
	}
	return false
}

// scopeFieldHasEntries returns true if a scope field contains any entries.
// Handles Classic API shapes: "" (empty), map (has entries), []any (has entries).
func scopeFieldHasEntries(v any) bool {
	switch val := v.(type) {
	case []any:
		return len(val) > 0
	case map[string]any:
		// Classic API wraps collections: {"computer_group": ...}
		// If the map has any key, there are entries.
		return len(val) > 0
	case string:
		// Empty string means no entries
		return false
	default:
		return v != nil
	}
}

// checkIndividualTargets flags policies that target or exclude individual
// computers or users instead of using groups. This is a scalability issue —
// individual targets don't scale and are hard to maintain.
func checkIndividualTargets(p policyInfo, scope map[string]any) []policyHealthFinding {
	if scope == nil {
		return nil
	}

	var findings []policyHealthFinding

	// Scope: individual computers targeted
	if n := countArrayEntries(scope, "computers"); n > 0 {
		findings = append(findings, policyHealthFinding{
			Policy: p.Name, PolicyID: p.ID,
			Check: "scope_computers", Severity: "info",
			Detail: fmt.Sprintf("%d computer(s) scoped directly", n),
		})
	}

	// Scope: individual JSS users targeted
	if n := countArrayEntries(scope, "jss_users"); n > 0 {
		findings = append(findings, policyHealthFinding{
			Policy: p.Name, PolicyID: p.ID,
			Check: "scope_users", Severity: "info",
			Detail: fmt.Sprintf("%d user(s) scoped directly", n),
		})
	}

	// Exclusions
	if exclusions, ok := scope["exclusions"].(map[string]any); ok {
		if n := countArrayEntries(exclusions, "computers"); n > 0 {
			findings = append(findings, policyHealthFinding{
				Policy: p.Name, PolicyID: p.ID,
				Check: "excl_computers", Severity: "info",
				Detail: fmt.Sprintf("%d computer(s) excluded directly", n),
			})
		}
		if n := countArrayEntries(exclusions, "users"); n > 0 {
			findings = append(findings, policyHealthFinding{
				Policy: p.Name, PolicyID: p.ID,
				Check: "excl_users", Severity: "info",
				Detail: fmt.Sprintf("%d user(s) excluded directly", n),
			})
		}
		if n := countArrayEntries(exclusions, "jss_users"); n > 0 {
			findings = append(findings, policyHealthFinding{
				Policy: p.Name, PolicyID: p.ID,
				Check: "excl_jss_users", Severity: "info",
				Detail: fmt.Sprintf("%d JSS user(s) excluded directly", n),
			})
		}
	}

	// Limitations: individual users
	if limitations, ok := scope["limitations"].(map[string]any); ok {
		if n := countArrayEntries(limitations, "users"); n > 0 {
			findings = append(findings, policyHealthFinding{
				Policy: p.Name, PolicyID: p.ID,
				Check: "limit_users", Severity: "info",
				Detail: fmt.Sprintf("%d user(s) in limitations directly", n),
			})
		}
	}

	return findings
}

// countArrayEntries counts items in a Classic API collection field.
// Handles both shapes:
//
//	XML path (xmlconv):  "computers": [{"id":10}, {"id":20}]
//	JSON path:           "computers": {"computer": [{"id":10}, {"id":20}]}
//	JSON single item:    "computers": {"computer": {"id":10}}
//	Empty:               "computers": "" or "computers": []
func countArrayEntries(m map[string]any, key string) int {
	v := m[key]
	switch val := v.(type) {
	case []any:
		return len(val)
	case map[string]any:
		// Classic API JSON wraps: {"computer": [...]} or {"computer": {...}}
		for _, inner := range val {
			switch iv := inner.(type) {
			case []any:
				return len(iv)
			case map[string]any:
				return 1 // single item wrapped as object
			}
		}
	}
	return 0
}

func policyHasPayload(data map[string]any) bool {
	// Disk encryption (FileVault)
	if de, ok := data["disk_encryption"].(map[string]any); ok {
		if action, _ := de["action"].(string); action != "" && action != "none" {
			return true
		}
	}
	// Packages
	if pkgConf, ok := data["package_configuration"].(map[string]any); ok {
		if scopeFieldHasEntries(pkgConf["packages"]) {
			return true
		}
	}
	// Scripts
	if scopeFieldHasEntries(data["scripts"]) {
		return true
	}
	// Printers
	if scopeFieldHasEntries(data["printers"]) {
		return true
	}
	// Dock items
	if scopeFieldHasEntries(data["dock_items"]) {
		return true
	}
	// Account maintenance
	if acctMaint, ok := data["account_maintenance"].(map[string]any); ok {
		if scopeFieldHasEntries(acctMaint["accounts"]) {
			return true
		}
		if scopeFieldHasEntries(acctMaint["directory_bindings"]) {
			return true
		}
	}
	// Maintenance actions
	if maint, ok := data["maintenance"].(map[string]any); ok {
		for _, key := range []string{
			"recon", "reset_name", "install_all_cached_packages",
			"heal", "prebindings", "permissions", "byhost",
			"system_cache", "user_cache", "verify",
		} {
			if v, ok := maint[key].(bool); ok && v {
				return true
			}
		}
	}
	// Files & processes
	if fp, ok := data["files_processes"].(map[string]any); ok {
		for _, key := range []string{
			"run_command", "delete_file", "search_by_path",
			"spotlight_search", "search_for_process", "kill_process",
		} {
			if v := extractField(fp, key); v != "" {
				return true
			}
		}
	}
	return false
}

func policyHasTrigger(general map[string]any) bool {
	if general == nil {
		return false
	}
	for _, key := range []string{
		"trigger_checkin", "trigger_enrollment_complete",
		"trigger_login", "trigger_logout",
		"trigger_network_state_changed", "trigger_startup",
	} {
		if v, ok := general[key].(bool); ok && v {
			return true
		}
	}
	if v := extractField(general, "trigger_other"); v != "" {
		return true
	}
	return false
}

func policyHasCategory(general map[string]any) bool {
	if general == nil {
		return false
	}
	cat, ok := general["category"].(map[string]any)
	if !ok {
		return false
	}
	catID := extractField(cat, "id")
	catName := extractField(cat, "name")
	return catID != "" && catID != "-1" && catName != "" && catName != "No category assigned"
}

// bootPartitionPercentUsed extracts the percent used from the boot partition.
// Returns -1 if not available.
func bootPartitionPercentUsed(c map[string]any) int {
	storage, _ := c["storage"].(map[string]any)
	if storage == nil {
		return -1
	}
	disks, _ := storage["disks"].([]any)
	for _, d := range disks {
		disk, ok := d.(map[string]any)
		if !ok {
			continue
		}
		partitions, _ := disk["partitions"].([]any)
		for _, p := range partitions {
			part, ok := p.(map[string]any)
			if !ok {
				continue
			}
			pType, _ := part["partitionType"].(string)
			if pType == "BOOT" {
				if pct, ok := part["percentUsed"].(float64); ok {
					return int(pct)
				}
			}
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// Failure scan (computer history based)
// ---------------------------------------------------------------------------

func runPolicyFailureScan(ctx context.Context, client registry.HTTPClient, days, limit int) ([]policyFailureSummary, []computerFailureSummary, int, error) {
	// Fetch all computer IDs from inventory
	computers, err := FetchAllPaginated(ctx, client,
		"/v3/computers-inventory?section=GENERAL&section=HARDWARE&section=OPERATING_SYSTEM&section=USER_AND_LOCATION&section=STORAGE", 2000)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("fetching computer inventory: %w", err)
	}

	// Filter to computers that have checked in within the lookback window —
	// there's no point scanning stale devices that haven't been online.
	cutoff := time.Now().AddDate(0, 0, -days)
	var computerIDs []string
	computerLookup := make(map[string]computerMeta)
	for _, c := range computers {
		id := extractID(c)
		if id == "" {
			continue
		}
		general, _ := c["general"].(map[string]any)
		hardware, _ := c["hardware"].(map[string]any)
		osInfo, _ := c["operatingSystem"].(map[string]any)
		userLoc, _ := c["userAndLocation"].(map[string]any)

		name := strVal(general, "name")
		serial := strVal(hardware, "serialNumber")
		osVersion := strVal(osInfo, "version")
		lastContact := strVal(general, "lastContactTime")

		username := strVal(userLoc, "username")
		diskPct := bootPartitionPercentUsed(c)

		if lastContact != "" {
			t, err := time.Parse(time.RFC3339, lastContact)
			if err != nil {
				t, _ = time.Parse("2006-01-02T15:04:05.999Z", lastContact)
			}
			if !t.IsZero() && t.Before(cutoff) {
				continue // stale — skip
			}
		}
		computerIDs = append(computerIDs, id)
		computerLookup[id] = computerMeta{
			name: name, serial: serial,
			osVersion: osVersion, username: username,
			diskPctUsed: diskPct,
		}
	}

	totalActive := len(computerIDs)

	// Apply sample limit:
	//   limit < 0:  smart default — max(100, 10% of active fleet)
	//   limit == 0: scan all active computers
	//   limit > 0:  explicit cap
	sampleSize := totalActive
	switch {
	case limit > 0:
		sampleSize = limit
	case limit < 0:
		tenPct := totalActive / 10
		sampleSize = tenPct
		if sampleSize < 100 {
			sampleSize = 100
		}
	}

	if sampleSize < totalActive {
		rand.Shuffle(len(computerIDs), func(i, j int) {
			computerIDs[i], computerIDs[j] = computerIDs[j], computerIDs[i]
		})
		fmt.Fprintf(os.Stderr, "Randomly sampling %d of %d active computers for failure scan (use --limit to override).\n", sampleSize, totalActive)
		computerIDs = computerIDs[:sampleSize]
	} else {
		fmt.Fprintf(os.Stderr, "Scanning %d active computers for policy failures (last %d days)...\n", totalActive, days)
	}

	// Fetch computer histories in parallel. Each result carries its computer
	// ID so aggregation doesn't depend on index alignment.
	results, errs := BoundedParallelFetch(ctx, computerIDs, policyReportConcurrency,
		func(ctx context.Context, compID string) (computerHistoryResult, error) {
			entries, err := fetchAllPolicyLogs(ctx, client, compID, cutoff)
			if err != nil {
				return computerHistoryResult{}, err
			}
			return computerHistoryResult{ComputerID: compID, Entries: entries}, nil
		})

	if len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "WARNING: %d computer history fetches failed.\n", len(errs))
	}

	// Aggregate by policy and by computer
	scannedCount := len(computerIDs) - len(errs)
	policyFails := aggregatePolicyFailures(results)
	computerFails := aggregateComputerFailures(results, computerLookup)
	return policyFails, computerFails, scannedCount, nil
}

// fetchAllPolicyLogs fetches all policy log entries for a computer within the
// date window. Returns all entries (not filtered by policy ID).
func fetchAllPolicyLogs(ctx context.Context, client registry.HTTPClient, computerID string, cutoff time.Time) ([]policyLogEntry, error) {
	data, err := fetchJSON(ctx, client, fmt.Sprintf("/JSSResource/computerhistory/id/%s", computerID))
	if err != nil {
		return nil, err
	}
	data = unwrapClassicDetail(data)

	policyLogs, _ := data["policy_logs"].([]any)

	// Classic API XML may not include <size> in policy_logs, so xmlconv
	// produces a map {"policy_log": [...]} instead of []any. Unwrap it.
	if policyLogs == nil {
		if wrapper, ok := data["policy_logs"].(map[string]any); ok {
			switch inner := wrapper["policy_log"].(type) {
			case []any:
				policyLogs = inner
			case map[string]any:
				policyLogs = []any{inner} // single entry
			}
		}
	}

	var entries []policyLogEntry
	for _, pl := range policyLogs {
		m, ok := pl.(map[string]any)
		if !ok {
			continue
		}

		logDate := parsePolicyLogDate(m)
		if !logDate.IsZero() && logDate.Before(cutoff) {
			continue
		}

		entries = append(entries, policyLogEntry{
			PolicyID:   extractField(m, "policy_id"),
			PolicyName: extractField(m, "policy_name"),
			Status:     extractField(m, "status"),
			Date:       logDate,
		})
	}

	return entries, nil
}

// parsePolicyLogDate extracts the date from a policy log entry.
func parsePolicyLogDate(m map[string]any) time.Time {
	if epoch, ok := m["date_completed_epoch"].(float64); ok && epoch > 0 {
		return time.UnixMilli(int64(epoch))
	}
	dateStr := extractField(m, "date_completed")
	if dateStr == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.999Z",
		"Mon Jan 02 15:04:05 MST 2006",
	} {
		if t, err := time.Parse(layout, dateStr); err == nil {
			return t
		}
	}
	return time.Time{}
}

// aggregatePolicyFailures takes all policy log entries across all computers
// and produces per-policy failure summaries. Only policies with at least one
// failure are included. Sorted by failure count descending.
func aggregatePolicyFailures(allResults []computerHistoryResult) []policyFailureSummary {
	type accumulator struct {
		name        string
		totalRuns   int
		failures    int
		lastFailure time.Time
	}

	byPolicy := make(map[string]*accumulator)

	for _, r := range allResults {
		for _, e := range r.Entries {
			acc, ok := byPolicy[e.PolicyID]
			if !ok {
				acc = &accumulator{name: e.PolicyName}
				byPolicy[e.PolicyID] = acc
			}
			acc.totalRuns++
			if e.Status != "Completed" {
				acc.failures++
				if e.Date.After(acc.lastFailure) {
					acc.lastFailure = e.Date
				}
			}
		}
	}

	var summaries []policyFailureSummary
	for pid, acc := range byPolicy {
		if acc.failures == 0 {
			continue
		}
		lastFail := ""
		if !acc.lastFailure.IsZero() {
			lastFail = acc.lastFailure.Format("2006-01-02")
		}
		summaries = append(summaries, policyFailureSummary{
			Policy:      acc.name,
			PolicyID:    pid,
			TotalRuns:   acc.totalRuns,
			Failures:    acc.failures,
			FailureRate: pctStr(acc.failures, acc.totalRuns),
			LastFailure: lastFail,
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Failures > summaries[j].Failures
	})

	return summaries
}

// aggregateComputerFailures produces per-computer failure summaries from
// the history scan results. Only computers with >50% failure rate are included.
func aggregateComputerFailures(results []computerHistoryResult, lookup map[string]computerMeta) []computerFailureSummary {
	var summaries []computerFailureSummary

	for _, r := range results {
		if len(r.Entries) == 0 {
			continue
		}
		var failures int
		var lastFailure time.Time
		for _, e := range r.Entries {
			if e.Status != "Completed" {
				failures++
				if e.Date.After(lastFailure) {
					lastFailure = e.Date
				}
			}
		}
		if failures == 0 {
			continue
		}
		totalRuns := len(r.Entries)
		failRate := float64(failures) / float64(totalRuns) * 100
		if failRate < 50 {
			continue
		}

		meta := lookup[r.ComputerID]
		lastFail := ""
		if !lastFailure.IsZero() {
			lastFail = lastFailure.Format("2006-01-02")
		}
		summaries = append(summaries, computerFailureSummary{
			ComputerID:   r.ComputerID,
			ComputerName: meta.name,
			Serial:       meta.serial,
			OSVersion:    meta.osVersion,
			Username:     meta.username,
			DiskPctUsed:  meta.diskPctUsed,
			TotalRuns:    totalRuns,
			Failures:     failures,
			FailureRate:  pctStr(failures, totalRuns),
			LastFailure:  lastFail,
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Failures > summaries[j].Failures
	})

	return summaries
}

// ---------------------------------------------------------------------------
// Output
// ---------------------------------------------------------------------------

func printPolicyHealthReport(report *policyHealthReport, configOnly bool) error {
	formatter := output.New(outputFmt, noColor, wide)

	if outputFmt == "json" || outputFmt == "yaml" {
		combined := map[string]any{
			"summary":         summaryToMap(report.Summary, configOnly),
			"config_findings": findingsToRows(report.ConfigFindings, true),
		}
		if !configOnly {
			combined["policy_failures"] = failuresToRows(report.PolicyFailures)
			combined["computer_failures"] = computerFailuresToRows(report.ComputerFailures)
		}
		return formatter.Print([]map[string]any{combined})
	}

	// Table: summary
	fmt.Println("── Policy Health Summary ──")
	if err := formatter.Print([]map[string]any{summaryToMap(report.Summary, configOnly)}); err != nil {
		return err
	}

	// Table: config findings
	if len(report.ConfigFindings) > 0 {
		fmt.Printf("\n── Config Findings (%d) ──\n", len(report.ConfigFindings))
		if err := formatter.Print(findingsToRows(report.ConfigFindings, false)); err != nil {
			return err
		}
	} else {
		fmt.Fprintln(os.Stderr, "\nNo config issues found.")
	}

	// Table: policy failures
	if !configOnly {
		if len(report.PolicyFailures) > 0 {
			fmt.Printf("\n── Policy Failures (last %d days) ──\n", report.Summary.Days)
			if err := formatter.Print(failuresToRows(report.PolicyFailures)); err != nil {
				return err
			}
		} else {
			fmt.Fprintf(os.Stderr, "\nNo policy failures found in the last %d days.\n", report.Summary.Days)
		}

		// Table: computers with high failure rates
		if len(report.ComputerFailures) > 0 {
			fmt.Printf("\n── Computers With High Failure Rate (>50%%, last %d days) ──\n", report.Summary.Days)
			if err := formatter.Print(computerFailuresToRows(report.ComputerFailures)); err != nil {
				return err
			}
		}
	}

	return nil
}

func summaryToMap(s policyHealthSummary, configOnly bool) map[string]any {
	m := map[string]any{
		"total_policies":  s.TotalPolicies,
		"enabled":         s.Enabled,
		"disabled":        s.Disabled,
		"config_findings": s.ConfigFindings,
		"warnings":        s.Warnings,
		"info":            s.Info,
	}
	if s.FetchErrors > 0 {
		m["fetch_errors"] = s.FetchErrors
	}
	if !configOnly {
		m["policies_with_failures"] = s.PoliciesWithFails
		m["computers_with_high_failure_rate"] = s.ComputersWithFails
		m["computers_scanned"] = s.ComputersScanned
		m["days"] = s.Days
	}
	return m
}

func findingsToRows(findings []policyHealthFinding, includeCheck bool) []map[string]any {
	rows := make([]map[string]any, len(findings))
	for i, f := range findings {
		row := map[string]any{
			"severity":  f.Severity,
			"policy":    f.Policy,
			"policy_id": f.PolicyID,
			"detail":    f.Detail,
		}
		if includeCheck {
			row["check"] = f.Check
		}
		rows[i] = row
	}
	return rows
}

func failuresToRows(failures []policyFailureSummary) []map[string]any {
	rows := make([]map[string]any, len(failures))
	for i, f := range failures {
		rows[i] = map[string]any{
			"policy":       f.Policy,
			"policy_id":    f.PolicyID,
			"total_runs":   f.TotalRuns,
			"failures":     f.Failures,
			"failure_rate": f.FailureRate,
			"last_failure": f.LastFailure,
		}
	}
	return rows
}

func computerFailuresToRows(failures []computerFailureSummary) []map[string]any {
	rows := make([]map[string]any, len(failures))
	for i, f := range failures {
		disk := "N/A"
		if f.DiskPctUsed >= 0 {
			disk = fmt.Sprintf("%d%%", f.DiskPctUsed)
		}
		rows[i] = map[string]any{
			"name":         f.ComputerName,
			"serial":       f.Serial,
			"os_version":   f.OSVersion,
			"username":     f.Username,
			"disk_used":    disk,
			"total_runs":   f.TotalRuns,
			"failures":     f.Failures,
			"failure_rate": f.FailureRate,
			"last_failure": f.LastFailure,
		}
	}
	return rows
}
