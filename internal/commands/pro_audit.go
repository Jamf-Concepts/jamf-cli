// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
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

Check categories: security, compliance, hygiene, enrollment.
Use --checks to filter to a specific category.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAudit(cmd.Context(), cliCtx, auditOptions{
				Checks: checks,
				Days:   days,
			})
		},
	}

	cmd.Flags().StringVar(&checks, "checks", "", "filter to category: security, compliance, hygiene, enrollment")
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
	if opts.Checks != "" {
		var filtered []auditCheck
		for _, c := range checks {
			if c.Category == opts.Checks {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("no checks match category %q (valid: security, compliance, hygiene, enrollment)", opts.Checks)
		}
		checks = filtered
	}

	var results []auditResult
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
	// Fetch all computers with SECURITY section to check FileVault status locally
	all, err := FetchAllPaginated(ctx, client, "/v3/computers-inventory?section=SECURITY", 100)
	if err != nil {
		return nil, err
	}
	count := 0
	for _, comp := range all {
		sec, _ := comp["security"].(map[string]any)
		if sec == nil {
			continue
		}
		status, _ := sec["fileVault2Status"].(string)
		if status != "" && status != "ALL_ENCRYPTED" && status != "BOOT_ENCRYPTED" {
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
	all, err := FetchAllPaginated(ctx, client, "/v3/computers-inventory?section=SECURITY", 100)
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
	path := fmt.Sprintf("/v3/computers-inventory?section=GENERAL&page-size=1&filter=general.lastContactTime%%3C%s", cutoff)
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
