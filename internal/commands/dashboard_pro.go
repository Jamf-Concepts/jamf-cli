// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// collectProData orchestrates all Jamf Pro data collection.
//
// Cost, as a formula rather than a constant — dashboardCostNote is the single
// source for the wording every surface repeats. Both tiers share one
// /v4/computers-inventory pass and (under --full) one policy-detail pass, so a
// record is fetched once per run however many collectors read it.
func collectProData(ctx context.Context, client registry.HTTPClient, data *DashboardData, smartGroupNames []string, full bool, status *collectStatus) {
	// The section set is decided before the first request so both tiers read
	// one pass. Fetching per tier would sweep the fleet twice under --full.
	sections := []string{"SECURITY", "DISK_ENCRYPTION", "OPERATING_SYSTEM"}
	if full {
		sections = append(sections, "HARDWARE", "GENERAL")
	}
	inv := fetchProInventory(ctx, client, sections)

	collectProDataFast(ctx, client, data, smartGroupNames, status, inv)
	if full {
		collectProDataFull(ctx, client, data, smartGroupNames, status, inv)
	}
}

// proInventory is one pass over /v4/computers-inventory, shared by every
// collector and audit check that needs whole-fleet records.
//
// It exists because six collectors used to sweep the fleet independently —
// security posture, OS distribution, hardware models, org structure and three
// audit checks — so a 50,000-device instance paid for the same records six
// times. One pass carries the union of the sections they read.
type proInventory struct {
	records []map[string]any
	err     error
}

// fetchProInventory runs the shared pass. A failure is carried rather than
// returned: each reader records its own section's miss, so one unreachable
// endpoint does not decide how many sections the banner names.
func fetchProInventory(ctx context.Context, client registry.HTTPClient, sections []string) *proInventory {
	path := "/v4/computers-inventory"
	for i, s := range sections {
		sep := "&"
		if i == 0 {
			sep = "?"
		}
		path += sep + "section=" + s
	}
	records, err := FetchAllPaginated(ctx, client, path, 500)
	if err != nil {
		return &proInventory{err: fmt.Errorf("computers-inventory: %w", err)}
	}
	return &proInventory{records: records}
}

// collectProDataFast runs the fixed-cost collectors: their request count does
// not grow with the computer or policy count.
//
// Cost is 1 inventory page sequence (⌈C/500⌉ requests, shared with the full
// tier) plus roughly 22 fixed requests. The four fleet- and policy-scaled audit
// checks deliberately live in the full tier — see fleetScaledAuditChecks.
func collectProDataFast(ctx context.Context, client registry.HTTPClient, data *DashboardData, smartGroupNames []string, status *collectStatus, inv *proInventory) {
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(8)

	go func() {
		defer wg.Done()
		fleet, err := collectFleetCounts(ctx, client, status)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: fleet counts: %v\n", err)
			status.recordFailureErr(sectionFleet, err)
			return
		}
		status.recordSuccess(sectionFleet)
		mu.Lock()
		data.Fleet = fleet
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		security, err := collectSecurityPosture(inv)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: security posture: %v\n", err)
			status.recordFailureErr(sectionSecurity, err)
			return
		}
		status.recordSuccess(sectionSecurity)
		mu.Lock()
		data.Security = security
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		audit := collectAuditFindings(ctx, client, fixedCostAuditChecks(), status)
		if len(audit.Results) > 0 || audit.ChecksSkipped > 0 {
			mu.Lock()
			data.Audit = audit
			mu.Unlock()
		}
	}()

	go func() {
		defer wg.Done()
		devices := collectDeviceCompliance(ctx, client, status)
		if !devices.StaleMissing || !devices.MDMMissing {
			status.recordSuccess(sectionFleet)
		}
		mu.Lock()
		data.Devices = devices
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		osDist, err := collectOSDistribution(inv)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: OS distribution: %v\n", err)
			status.recordFailureErr(sectionOSDist, err)
			return
		}
		status.recordSuccess(sectionOSDist)
		mu.Lock()
		data.OSDist = osDist
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		envStats := collectEnvironmentStats(ctx, client, status)
		mu.Lock()
		data.EnvStats = envStats
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		checkin := collectCheckinStatus(ctx, client, status)
		mu.Lock()
		data.Checkin = checkin
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		sg, err := collectSmartGroups(ctx, client, "/v3/computer-groups/smart-groups", smartGroupNames, "membershipCount")
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: computer smart groups: %v\n", err)
			status.recordFailureErr(sectionSmartGroups, err)
			return
		}
		status.recordSuccess(sectionSmartGroups)
		mu.Lock()
		data.ComputerSmartGroups = sg
		mu.Unlock()
	}()

	wg.Wait()

	if data.Fleet != nil && data.ComputerSmartGroups != nil {
		data.ComputerSmartGroups.TotalFleet = data.Fleet.ManagedComputers + data.Fleet.UnmanagedComputers
	}
}

// collectProDataFull runs the variable-cost collectors, whose request count
// grows with the instance: 2 requests per patch title, 1 per policy, 1 per
// config profile, plus the four fleet- and policy-scaled audit checks. Those
// last four and the cleanup analysis read the shared inventory and policy
// passes rather than sweeping again, so a policy detail is fetched once per run
// where it used to be fetched twice.
func collectProDataFull(ctx context.Context, client registry.HTTPClient, data *DashboardData, smartGroupNames []string, status *collectStatus, inv *proInventory) {
	// Two shared Classic detail passes. Policies are read by the cleanup
	// analysis and the policies-with-no-scope audit check; profiles by the
	// cleanup analysis and the org structure's category counts. Both used to be
	// fetched twice per run.
	policies := fetchPolicyDetails(ctx, client)
	profiles := fetchConfigProfileDetails(ctx, client)

	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(5)

	go func() {
		defer wg.Done()
		patch, spread, err := collectPatchCompliance(ctx, client, status)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: patch compliance: %v\n", err)
			status.recordFailureErr(sectionPatch, err)
			return
		}
		status.recordSuccess(sectionPatch)
		mu.Lock()
		data.Patch = patch
		data.PatchSpread = spread
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		hw, err := collectHardwareModels(ctx, client, status, inv)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: hardware models: %v\n", err)
			status.recordFailureErr(sectionHardware, err)
			return
		}
		mu.Lock()
		data.Hardware = hw
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		cleanup, err := collectCleanupAnalysis(ctx, client, policies, profiles, status)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: cleanup analysis: %v\n", err)
			status.recordFailureErr(sectionCleanup, err)
			return
		}
		status.recordSuccess(sectionCleanup)
		mu.Lock()
		data.Cleanup = cleanup
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		org, err := collectOrgStructure(ctx, client, status, inv, policies, profiles)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: org structure: %v\n", err)
			status.recordFailureErr(sectionOrgStructure, err)
			return
		}
		status.recordSuccess(sectionOrgStructure)
		mu.Lock()
		data.OrgStructure = org
		mu.Unlock()
	}()

	// The fleet- and policy-scaled audit checks. They read the shared passes,
	// so they cost nothing beyond what the tier already fetched.
	go func() {
		defer wg.Done()
		extra := collectScaledAuditFindings(inv, policies, status)
		mu.Lock()
		if data.Audit == nil {
			data.Audit = &auditSummary{}
		}
		data.Audit.Results = append(data.Audit.Results, extra.Results...)
		data.Audit.ChecksSkipped += extra.ChecksSkipped
		mu.Unlock()
	}()

	wg.Wait()

	// Mobile smart groups are also full-tier: member counts on large fleets
	// can be slow. Populate fleet total once mobile data is in.
	mgSG, err := collectSmartGroups(ctx, client, "/v2/mobile-device-groups/smart-groups", smartGroupNames, "count")
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: mobile smart groups: %v\n", err)
		status.recordFailureErr(sectionSmartGroups, err)
	} else {
		status.recordSuccess(sectionSmartGroups)
		mu.Lock()
		data.MobileSmartGroups = mgSG
		mu.Unlock()
	}

	if data.Fleet != nil && data.MobileSmartGroups != nil {
		data.MobileSmartGroups.TotalFleet = data.Fleet.ManagedMobile + data.Fleet.UnmanagedMobile
	}
}

// collectFleetCounts fetches managed/unmanaged computer and mobile counts
// from /v1/inventory-information, and the total user count from /v1/users.
func collectFleetCounts(ctx context.Context, client registry.HTTPClient, status *collectStatus) (*fleetSummary, error) {
	inv, err := fetchJSON(ctx, client, "/v1/inventory-information")
	if err != nil {
		return nil, fmt.Errorf("inventory-information: %w", err)
	}

	managed, _ := inv["managedComputers"].(float64)
	unmanaged, _ := inv["unmanagedComputers"].(float64)
	managedMobile, _ := inv["managedDevices"].(float64)
	unmanagedMobile, _ := inv["unmanagedDevices"].(float64)

	summary := &fleetSummary{
		ManagedComputers:   int(managed),
		UnmanagedComputers: int(unmanaged),
		ManagedMobile:      int(managedMobile),
		UnmanagedMobile:    int(unmanagedMobile),
	}

	usersData, err := fetchJSON(ctx, client, "/v1/users?page-size=1")
	if err != nil {
		// A failed user count is not zero users. Rendering "0 Users" as a
		// headline figure is the shape this flag exists to prevent.
		fmt.Fprintf(os.Stderr, "WARNING: user count: %v\n", err)
		status.recordFailureErr(sectionFleet, err)
		summary.UsersMissing = true
	} else if tc, ok := usersData["totalCount"].(float64); ok {
		summary.Users = int(tc)
	}

	return summary, nil
}

// collectSecurityPosture counts security features across the shared inventory
// pass. It takes records rather than a client because the same pass answers the
// OS distribution, the hardware models, the org structure and three audit
// checks.
func collectSecurityPosture(inv *proInventory) (*securityPosture, error) {
	if inv.err != nil {
		return nil, inv.err
	}

	posture := &securityPosture{Total: len(inv.records)}

	for _, comp := range inv.records {
		diskEnc, _ := comp["diskEncryption"].(map[string]any)
		if fileVaultStatus(diskEnc) == statusFVEncrypted {
			posture.FileVaultEnabled++
		}

		sec, _ := comp["security"].(map[string]any)
		if sec == nil {
			continue
		}

		if fw, _ := sec["firewallEnabled"].(bool); fw {
			posture.FirewallEnabled++
		}

		gk, _ := sec["gatekeeperStatus"].(string)
		if gk != statusGKDisabled && gk != statusGKDisabledAlt && gk != "" {
			posture.GatekeeperEnabled++
		}

		sip, _ := sec["sipStatus"].(string)
		if sip == statusSIPEnabled || sip == statusSIPEnabledAlt {
			posture.SIPEnabled++
		}
	}

	return posture, nil
}

// collectAuditFindings runs the given audit checks sequentially. A check that
// fails is counted as well as warned about: the Audit section's own figures are
// a count of findings, so a failed check silently removes an alert rather than
// showing an empty one.
func collectAuditFindings(ctx context.Context, client registry.HTTPClient, checks []auditCheck, status *collectStatus) *auditSummary {
	summary := &auditSummary{}

	for _, check := range checks {
		result, err := check.Run(ctx, client, 14)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: audit check %q: %v\n", check.Name, err)
			status.recordFailureErr(sectionAudit, err)
			summary.ChecksSkipped++
			continue
		}
		if result != nil {
			summary.Results = append(summary.Results, *result)
		}
	}
	if summary.ChecksSkipped < len(checks) {
		status.recordSuccess(sectionAudit)
	}

	return summary
}

// collectScaledAuditFindings answers the fleet- and policy-scaled checks from
// the shared passes. Each check is skipped — and recorded as skipped — when the
// pass it reads could not be fetched.
func collectScaledAuditFindings(inv *proInventory, policies *policyDetailSet, status *collectStatus) *auditSummary {
	summary := &auditSummary{}
	ran := 0

	for _, check := range fleetScaledAuditChecks() {
		switch {
		case check.FromInventory != nil:
			if inv.err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: audit check %q: %v\n", check.Name, inv.err)
				status.recordFailureErr(sectionAudit, inv.err)
				summary.ChecksSkipped++
				continue
			}
			ran++
			if r := check.FromInventory(inv.records); r != nil {
				summary.Results = append(summary.Results, *r)
			}
		case check.FromPolicies != nil:
			if policies.err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: audit check %q: %v\n", check.Name, policies.err)
				status.recordFailureErr(sectionAudit, policies.err)
				summary.ChecksSkipped++
				continue
			}
			ran++
			if r := check.FromPolicies(policies.details, policies.skipped); r != nil {
				summary.Results = append(summary.Results, *r)
			}
		}
	}
	if ran > 0 {
		status.recordSuccess(sectionAudit)
	}

	return summary
}

// collectPatchCompliance fetches all patch software title configurations and
// then collects a per-title patch summary in parallel.
func collectPatchCompliance(ctx context.Context, client registry.HTTPClient, status *collectStatus) (*patchCompliance, []patchVersionSpread, error) {
	configs, err := FetchAllPaginated(ctx, client, "/v3/patch-software-title-configurations", 100)
	if err != nil {
		return nil, nil, fmt.Errorf("patch-software-title-configurations: %w", err)
	}

	if len(configs) == 0 {
		return &patchCompliance{}, nil, nil
	}

	type patchResult struct {
		Summary  map[string]any
		Versions []map[string]any
	}

	results, errs := BoundedParallelFetch(ctx, configs, 5, func(ctx context.Context, cfg map[string]any) (patchResult, error) {
		id := extractID(cfg)
		if id == "" {
			return patchResult{}, fmt.Errorf("missing id in patch config")
		}
		summaryPath := fmt.Sprintf("/v3/patch-software-title-configurations/%s/patch-summary", id)
		summary, err := fetchJSON(ctx, client, summaryPath)
		if err != nil {
			return patchResult{}, err
		}

		versionsPath := fmt.Sprintf("/v3/patch-software-title-configurations/%s/patch-summary/versions", id)
		versions, vErr := FetchAllPaginated(ctx, client, versionsPath, 100)
		if vErr != nil {
			fmt.Fprintf(os.Stderr, "WARNING: patch versions for %s: %v\n", id, vErr)
			status.recordFailureErr(sectionPatch, vErr)
		}

		return patchResult{Summary: summary, Versions: versions}, nil
	})

	compliance := &patchCompliance{TitlesSkipped: len(errs)}
	for _, err := range errs {
		fmt.Fprintf(os.Stderr, "WARNING: patch summary fetch: %v\n", err)
		status.recordFailureErr(sectionPatch, err)
	}

	var spreads []patchVersionSpread

	for _, r := range results {
		s := r.Summary
		if s == nil {
			continue
		}
		name, _ := s["title"].(string)
		if name == "" {
			name, _ = s["softwareTitleName"].(string)
		}
		latestVersion, _ := s["latestVersion"].(string)
		upToDate, _ := s["upToDate"].(float64)
		outOfDate, _ := s["outOfDate"].(float64)
		total := int(upToDate) + int(outOfDate)

		var pct float64
		if total > 0 {
			pct = float64(upToDate) / float64(total) * 100
		}

		compliance.Titles = append(compliance.Titles, patchTitle{
			Name:          name,
			LatestVersion: latestVersion,
			UpToDate:      int(upToDate),
			OutOfDate:     int(outOfDate),
			Total:         total,
			CompliancePct: pct,
		})

		if len(r.Versions) > 0 {
			spread := patchVersionSpread{Title: name}
			for _, v := range r.Versions {
				version, _ := v["version"].(string)
				onVersion, _ := v["onVersion"].(float64)
				if version != "" && int(onVersion) > 0 {
					spread.Versions = append(spread.Versions, patchVersionEntry{
						Version: version,
						Count:   int(onVersion),
					})
				}
			}
			if len(spread.Versions) > 0 {
				sort.Slice(spread.Versions, func(i, j int) bool {
					return spread.Versions[i].Count > spread.Versions[j].Count
				})
				if len(spread.Versions) > 8 {
					other := 0
					for _, v := range spread.Versions[7:] {
						other += v.Count
					}
					spread.Versions = append(spread.Versions[:7], patchVersionEntry{Version: "Other", Count: other})
				}
				spreads = append(spreads, spread)
			}
		}
	}

	return compliance, spreads, nil
}

// collectDeviceCompliance fetches stale check-in count and failed MDM command
// count. Each half is independently optional: a failed fetch marks its own
// field missing rather than leaving a zero, because the template hides an alert
// card whose count is zero — so a failed MDM-command fetch used to remove the
// alert entirely rather than saying it could not be read.
func collectDeviceCompliance(ctx context.Context, client registry.HTTPClient, status *collectStatus) *deviceCompliance {
	const staleDays = 14
	cutoff := timeNow().AddDate(0, 0, -staleDays).UTC().Format("2006-01-02")

	compliance := &deviceCompliance{StaleThresholdDays: staleDays}

	staleData, err := fetchJSON(ctx, client,
		fmt.Sprintf("/v4/computers-inventory?section=GENERAL&page-size=1&filter=general.lastCheckIn%%3C%s", cutoff))
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: stale device check-in count: %v\n", err)
		status.recordFailureErr(sectionFleet, err)
		compliance.StaleMissing = true
	} else if tc, ok := staleData["totalCount"].(float64); ok {
		compliance.StaleDevices = int(tc)
	}

	mdmData, err := fetchJSON(ctx, client, "/v2/mdm/commands?filter=status%3D%3DError&page-size=1")
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed MDM commands: %v\n", err)
		status.recordFailureErr(sectionFleet, err)
		compliance.MDMMissing = true
	} else if tc, ok := mdmData["totalCount"].(float64); ok {
		compliance.FailedMDMCommands = int(tc)
	}

	return compliance
}

// collectOSDistribution groups the shared inventory pass by OS version, sorted
// by count descending.
func collectOSDistribution(inv *proInventory) (*osDistribution, error) {
	if inv.err != nil {
		return nil, inv.err
	}

	counts := make(map[string]int)
	for _, comp := range inv.records {
		osInfo, _ := comp["operatingSystem"].(map[string]any)
		if osInfo == nil {
			continue
		}
		version, _ := osInfo["version"].(string)
		if version != "" {
			counts[version]++
		}
	}

	versions := make([]osVersionCount, 0, len(counts))
	for v, c := range counts {
		versions = append(versions, osVersionCount{Version: v, Count: c})
	}
	sort.Slice(versions, func(i, j int) bool {
		if versions[i].Count != versions[j].Count {
			return versions[i].Count > versions[j].Count
		}
		return versions[i].Version > versions[j].Version
	})

	return &osDistribution{Versions: versions}, nil
}

// collectEnvironmentStats counts each environment object type. A count that
// could not be fetched is named in Missing rather than left at zero, so the
// template renders "unavailable" where it would otherwise render a confident 0.
func collectEnvironmentStats(ctx context.Context, client registry.HTTPClient, status *collectStatus) *environmentStats {
	stats := &environmentStats{Missing: map[string]bool{}}
	var mu sync.Mutex
	var wg sync.WaitGroup

	type countTask struct {
		label  string
		target *int
		fn     func() (int, error)
	}

	paginatedCount := func(path string) func() (int, error) {
		return func() (int, error) {
			return fetchPaginatedCountInt(ctx, client, path)
		}
	}

	classicCount := func(path string) func() (int, error) {
		return func() (int, error) {
			items, err := FetchClassicList(ctx, client, path, "")
			if err != nil {
				return 0, err
			}
			return len(items), nil
		}
	}

	tasks := []countTask{
		{envStatPolicies, &stats.Policies, classicCount("/JSSResource/policies")},
		{envStatConfigProfiles, &stats.ConfigProfiles, classicCount("/JSSResource/osxconfigurationprofiles")},
		{envStatScripts, &stats.Scripts, paginatedCount("/v1/scripts")},
		{envStatPackages, &stats.Packages, classicCount("/JSSResource/packages")},
		{envStatComputerSmartGrps, &stats.ComputerSmartGrps, paginatedCount("/v3/computer-groups/smart-groups")},
		{envStatMobileSmartGrps, &stats.MobileSmartGrps, paginatedCount("/v2/mobile-device-groups/smart-groups")},
		{envStatExtAttributes, &stats.ExtAttributes, paginatedCount("/v1/computer-extension-attributes")},
		{envStatCategories, &stats.Categories, paginatedCount("/v1/categories")},
	}

	wg.Add(len(tasks))
	for _, t := range tasks {
		go func(label string, tgt *int, fn func() (int, error)) {
			defer wg.Done()
			val, err := fn()
			if err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: environment stat %s: %v\n", label, err)
				status.recordFailureErr(sectionEnvironment, err)
				mu.Lock()
				stats.Missing[label] = true
				mu.Unlock()
				return
			}
			mu.Lock()
			*tgt = val
			mu.Unlock()
		}(t.label, t.target, t.fn)
	}
	wg.Wait()

	if len(stats.Missing) < len(tasks) {
		status.recordSuccess(sectionEnvironment)
	}

	return stats
}

// collectCheckinStatus counts overdue devices against their totals. Each half
// is independently optional: an overdue count that failed while its total
// succeeded would make CheckedInPct read 100% on a fleet where nothing has
// checked in, which is the single most misleading zero in the report.
func collectCheckinStatus(ctx context.Context, client registry.HTTPClient, status *collectStatus) *checkinStatus {
	const thresholdDays = 7
	cutoff := timeNow().AddDate(0, 0, -thresholdDays).UTC().Format("2006-01-02")

	ck := &checkinStatus{ThresholdDays: thresholdDays}
	var mu sync.Mutex
	var wg sync.WaitGroup
	failures := 0

	fail := func(what string, err error, mark *bool) {
		fmt.Fprintf(os.Stderr, "WARNING: %s: %v\n", what, err)
		status.recordFailureErr(sectionCheckin, err)
		mu.Lock()
		*mark = true
		failures++
		mu.Unlock()
	}

	wg.Add(4)

	go func() {
		defer wg.Done()
		n, err := fetchPaginatedCountInt(ctx, client, "/v4/computers-inventory?section=GENERAL")
		if err != nil {
			fail("computer total count", err, &ck.ComputerDataMissing)
			return
		}
		mu.Lock()
		ck.ComputersTotal = n
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		n, err := fetchPaginatedCountInt(ctx, client,
			fmt.Sprintf("/v4/computers-inventory?section=GENERAL&filter=general.lastCheckIn%%3C%s", cutoff))
		if err != nil {
			fail("overdue computer count", err, &ck.ComputerDataMissing)
			return
		}
		mu.Lock()
		ck.ComputersOverdue = n
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		n, err := fetchPaginatedCountInt(ctx, client, "/v2/mobile-devices")
		if err != nil {
			fail("mobile total count", err, &ck.MobileDataMissing)
			return
		}
		mu.Lock()
		ck.MobileTotal = n
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		n, err := fetchPaginatedCountInt(ctx, client,
			fmt.Sprintf("/v2/mobile-devices?filter=lastInventoryUpdateDate%%3C%s", cutoff))
		if err != nil {
			fail("overdue mobile count", err, &ck.MobileDataMissing)
			return
		}
		mu.Lock()
		ck.MobileOverdue = n
		mu.Unlock()
	}()

	wg.Wait()

	if failures < 4 {
		status.recordSuccess(sectionCheckin)
	}
	return ck
}

// topNModels sorts counts by descending frequency, caps at n, and rolls the
// remainder into an "Other" entry.
func topNModels(counts map[string]int, n int) []modelCount {
	models := make([]modelCount, 0, len(counts))
	for m, c := range counts {
		models = append(models, modelCount{Model: m, Count: c})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Count > models[j].Count })
	if len(models) > n {
		other := 0
		for _, m := range models[n:] {
			other += m.Count
		}
		models = append(models[:n], modelCount{Model: "Other", Count: other})
	}
	return models
}

// collectHardwareModels reads computer models off the shared inventory pass and
// fetches mobile devices itself. Either half failing marks that half missing
// rather than rendering an empty model table as "no models".
func collectHardwareModels(ctx context.Context, client registry.HTTPClient, status *collectStatus, inv *proInventory) (*hardwareModels, error) {
	hw := &hardwareModels{}

	if inv.err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: computer hardware: %v\n", inv.err)
		status.recordFailureErr(sectionHardware, inv.err)
		hw.ComputerModelsMissing = true
	} else {
		counts := make(map[string]int)
		for _, comp := range inv.records {
			hardware, _ := comp["hardware"].(map[string]any)
			if hardware == nil {
				continue
			}
			if model, _ := hardware["model"].(string); model != "" {
				counts[model]++
			}
		}
		hw.ComputerModels = topNModels(counts, 10)
	}

	all, err := FetchAllPaginated(ctx, client, "/v2/mobile-devices", 500)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: mobile device models: %v\n", err)
		status.recordFailureErr(sectionHardware, err)
		hw.MobileModelsMissing = true
	} else {
		counts := make(map[string]int)
		for _, dev := range all {
			if model, _ := dev["model"].(string); model != "" {
				counts[model]++
			}
		}
		hw.MobileModels = topNModels(counts, 10)
	}

	if hw.ComputerModelsMissing && hw.MobileModelsMissing {
		// Nothing to render, and both misses are already recorded.
		return nil, nil
	}
	status.recordSuccess(sectionHardware)
	if len(hw.ComputerModels) == 0 && len(hw.MobileModels) == 0 &&
		!hw.ComputerModelsMissing && !hw.MobileModelsMissing {
		return nil, nil
	}
	return hw, nil
}

func collectSmartGroups(ctx context.Context, client registry.HTTPClient, endpoint string, names []string, countField string) (*smartGroupSummary, error) {
	allGroups, err := FetchAllPaginated(ctx, client, endpoint, 100)
	if err != nil {
		return nil, fmt.Errorf("smart groups: %w", err)
	}

	groupName := func(g map[string]any) string {
		if n, ok := g["name"].(string); ok && n != "" {
			return n
		}
		if n, ok := g["groupName"].(string); ok && n != "" {
			return n
		}
		return ""
	}

	summary := &smartGroupSummary{}

	if len(names) > 0 {
		nameSet := make(map[string]bool, len(names))
		for _, n := range names {
			nameSet[strings.ToLower(n)] = true
		}
		for _, g := range allGroups {
			name := groupName(g)
			if !nameSet[strings.ToLower(name)] {
				continue
			}
			count, _ := g[countField].(float64)
			summary.Groups = append(summary.Groups, smartGroupEntry{
				Name:  name,
				Count: int(count),
			})
		}
	} else {
		for _, g := range allGroups {
			name := groupName(g)
			count, _ := g[countField].(float64)
			summary.Groups = append(summary.Groups, smartGroupEntry{
				Name:  name,
				Count: int(count),
			})
		}
		sort.Slice(summary.Groups, func(i, j int) bool {
			return summary.Groups[i].Count > summary.Groups[j].Count
		})
		if len(summary.Groups) > 10 {
			summary.Groups = summary.Groups[:10]
		}
	}

	return summary, nil
}

// policyDetailSet is one pass over every policy's Classic detail, shared by the
// cleanup analysis and the policies-with-no-scope audit check. Both used to
// fetch every policy independently, so a 500-policy instance paid for 1,000
// identical Classic GETs per --full run.
//
// A failed list fetch is carried in err and leaves details empty; a failed
// per-policy detail increments skipped, which is what makes every figure
// derived from the pass unpublishable rather than merely low.
type policyDetailSet struct {
	details []map[string]any
	skipped int
	err     error
}

// fetchPolicyDetails runs the shared pass. Each skipped policy is named on
// stderr: "150 policies unreadable" is not actionable without the ids.
func fetchPolicyDetails(ctx context.Context, client registry.HTTPClient) *policyDetailSet {
	policies, err := FetchClassicList(ctx, client, "/JSSResource/policies", "policy")
	if err != nil {
		return &policyDetailSet{err: fmt.Errorf("policies: %w", err)}
	}

	set := &policyDetailSet{}
	for _, raw := range policies {
		p, _ := raw.(map[string]any)
		if p == nil {
			continue
		}
		id := extractClassicID(p)
		if id == "" {
			continue
		}
		detail, err := fetchJSON(ctx, client, "/JSSResource/policies/id/"+id)
		if err != nil {
			// A missed policy detail leaves its packages/scripts out of the
			// reference sets and its enabled/scope state out of the counters,
			// so everything the pass derives has to be withheld rather than
			// published as an under-counted tally.
			set.skipped++
			fmt.Fprintf(os.Stderr, "WARNING: policy %s detail: %v\n", id, err)
			continue
		}
		pol, _ := detail["policy"].(map[string]any)
		if pol == nil {
			pol = detail
		}
		set.details = append(set.details, pol)
	}
	return set
}

// fetchConfigProfileDetails is the config-profile twin of fetchPolicyDetails,
// shared by the cleanup analysis (which derives UnscopedProfiles from it) and
// the org structure (which counts each category's members).
//
// It is a pass of its own for the same reason the policy one is: the loop used
// to live inside collectCleanupAnalysis with a bare `continue` on error — no
// counter and no warning, the quietest site in the file — so 80 failed
// detail fetches rendered "Unscoped Profiles: 0" with no output of any kind.
func fetchConfigProfileDetails(ctx context.Context, client registry.HTTPClient) *policyDetailSet {
	profiles, err := FetchClassicList(ctx, client, "/JSSResource/osxconfigurationprofiles", "configuration_profile")
	if err != nil {
		return &policyDetailSet{err: fmt.Errorf("config profiles: %w", err)}
	}

	set := &policyDetailSet{}
	for _, raw := range profiles {
		p, _ := raw.(map[string]any)
		if p == nil {
			continue
		}
		id := extractClassicID(p)
		if id == "" {
			continue
		}
		detail, err := fetchJSON(ctx, client, "/JSSResource/osxconfigurationprofiles/id/"+id)
		if err != nil {
			set.skipped++
			fmt.Fprintf(os.Stderr, "WARNING: config profile %s detail: %v\n", id, err)
			continue
		}
		prof, _ := detail["os_x_configuration_profile"].(map[string]any)
		if prof == nil {
			set.skipped++
			fmt.Fprintf(os.Stderr, "WARNING: config profile %s detail carried no profile object\n", id)
			continue
		}
		set.details = append(set.details, prof)
	}
	return set
}

// classicCategoryName reads the category a Classic policy or configuration
// profile is filed under. Jamf renders an unfiled object's category as the
// literal "No category assigned", which is not a category.
func classicCategoryName(detail map[string]any) string {
	gen, _ := detail["general"].(map[string]any)
	if gen == nil {
		return ""
	}
	// A policy nests it as an object; a configuration profile carries it as a
	// bare string. Both shapes are live.
	switch cat := gen["category"].(type) {
	case map[string]any:
		name, _ := cat["name"].(string)
		return name
	case string:
		return cat
	}
	return ""
}

// collectCleanupAnalysis identifies housekeeping candidates:
// disabled policies, unscoped policies, unscoped config profiles,
// packages not referenced by any policy, and scripts not referenced by any policy.
//
// Every figure it publishes is gated on the pass that produced it. A policy
// detail that could not be read removes its packages and scripts from the
// reference sets and its own state from the counters, so publishing either as a
// number is deletion advice derived from records nobody read.
func collectCleanupAnalysis(ctx context.Context, client registry.HTTPClient, policies, profiles *policyDetailSet, status *collectStatus) (*cleanupAnalysis, error) {
	if policies.err != nil {
		return nil, policies.err
	}
	if profiles.err != nil {
		return nil, profiles.err
	}

	var disabledPolicies, unscopedPolicies int
	referencedPackages := make(map[string]bool)
	referencedScripts := make(map[string]bool)

	for _, pol := range policies.details {
		gen, _ := pol["general"].(map[string]any)
		if enabled, _ := gen["enabled"].(bool); !enabled {
			disabledPolicies++
		}

		scope, _ := pol["scope"].(map[string]any)
		if isEmptyScope(scope) {
			unscopedPolicies++
		}

		// Track which packages and scripts this policy references.
		if pkgs, _ := pol["package_configuration"].(map[string]any); pkgs != nil {
			if pkgList, _ := pkgs["packages"].([]any); pkgList != nil {
				for _, pkg := range pkgList {
					if pm, _ := pkg.(map[string]any); pm != nil {
						if name, _ := pm["name"].(string); name != "" {
							referencedPackages[name] = true
						}
					}
				}
			}
		}
		if scripts, _ := pol["scripts"].(map[string]any); scripts != nil {
			if scriptList, _ := scripts["script"].([]any); scriptList != nil {
				for _, scr := range scriptList {
					if sm, _ := scr.(map[string]any); sm != nil {
						if name, _ := sm["name"].(string); name != "" {
							referencedScripts[name] = true
						}
					}
				}
			}
		}
	}
	if policies.skipped > 0 {
		status.recordFailure(sectionCleanup)
	}

	// Unscoped config profiles, from the shared profile-detail pass.
	var unscopedProfiles int
	for _, prof := range profiles.details {
		scope, _ := prof["scope"].(map[string]any)
		if isEmptyScope(scope) {
			unscopedProfiles++
		}
	}
	if profiles.skipped > 0 {
		status.recordFailure(sectionCleanup)
	}

	// Unused packages: packages not referenced by any policy.
	allPackages, err := FetchClassicList(ctx, client, "/JSSResource/packages", "package")
	if err != nil {
		return nil, fmt.Errorf("packages: %w", err)
	}
	unusedPackages := 0
	for _, raw := range allPackages {
		pkg, _ := raw.(map[string]any)
		name, _ := pkg["name"].(string)
		if !referencedPackages[name] {
			unusedPackages++
		}
	}

	// Unused scripts: scripts not referenced by any policy.
	allScripts, err := FetchClassicList(ctx, client, "/JSSResource/scripts", "script")
	if err != nil {
		return nil, fmt.Errorf("scripts: %w", err)
	}
	unusedScripts := 0
	for _, raw := range allScripts {
		scr, _ := raw.(map[string]any)
		name, _ := scr["name"].(string)
		if !referencedScripts[name] {
			unusedScripts++
		}
	}

	return &cleanupAnalysis{
		DisabledPolicies: disabledPolicies,
		UnscopedPolicies: unscopedPolicies,
		UnscopedProfiles: unscopedProfiles,
		UnusedPackages:   unusedPackages,
		UnusedScripts:    unusedScripts,
		PoliciesSkipped:  policies.skipped,
		ProfilesSkipped:  profiles.skipped,
	}, nil
}

// isEmptyScope returns true when a Classic API scope object has no targets.
// An unscoped policy/profile has no computers, groups, buildings, departments,
// or network segments.
func isEmptyScope(scope map[string]any) bool {
	if scope == nil {
		return true
	}
	for _, key := range []string{"computers", "computer_groups", "buildings", "departments", "network_segments", "mobile_devices", "mobile_device_groups"} {
		if items, _ := scope[key].([]any); len(items) > 0 {
			return false
		}
	}
	// A scope targeting "All Computers" or similar is non-empty.
	if allComp, _ := scope["all_computers"].(bool); allComp {
		return false
	}
	if allMobile, _ := scope["all_mobile_devices"].(bool); allMobile {
		return false
	}
	return true
}

// extractClassicID extracts the integer id field from a Classic API list item.
func extractClassicID(item map[string]any) string {
	if v, ok := item["id"].(float64); ok && v > 0 {
		return fmt.Sprintf("%.0f", v)
	}
	return ""
}

// collectOrgStructure derives sites, buildings and departments from the shared
// inventory pass, and fetches category names itself.
func collectOrgStructure(ctx context.Context, client registry.HTTPClient, status *collectStatus, inv *proInventory, policies, profiles *policyDetailSet) (*orgStructure, error) {
	org := &orgStructure{}

	if inv.err != nil {
		return nil, inv.err
	}

	// Build site/building/department counts from the shared inventory pass.
	siteCounts := make(map[string]int)
	buildingCounts := make(map[string]int)
	deptCounts := make(map[string]int)
	for _, comp := range inv.records {
		gen, _ := comp["general"].(map[string]any)
		if gen == nil {
			continue
		}
		if site, _ := gen["site"].(map[string]any); site != nil {
			if name, _ := site["name"].(string); name != "" && name != "None" {
				siteCounts[name]++
			}
		}
		if bldg, _ := gen["building"].(map[string]any); bldg != nil {
			if name, _ := bldg["name"].(string); name != "" && name != "None" {
				buildingCounts[name]++
			}
		}
		if dept, _ := gen["department"].(map[string]any); dept != nil {
			if name, _ := dept["name"].(string); name != "" && name != "None" {
				deptCounts[name]++
			}
		}
	}

	toEntries := func(counts map[string]int) []orgEntry {
		entries := make([]orgEntry, 0, len(counts))
		for name, count := range counts {
			entries = append(entries, orgEntry{Name: name, Count: count})
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].Count != entries[j].Count {
				return entries[i].Count > entries[j].Count
			}
			return entries[i].Name < entries[j].Name
		})
		return entries
	}

	org.Sites = toEntries(siteCounts)
	org.Buildings = toEntries(buildingCounts)
	org.Departments = toEntries(deptCounts)

	// Categories carry a real member count, tallied across every object type
	// that can hold one — see dashboard_pro_categories.go. The category list
	// has to land first, because three of the four Pro collections carry a
	// category id rather than a name.
	cats, err := FetchAllPaginated(ctx, client, "/v1/categories", 100)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: categories: %v\n", err)
		status.recordFailureErr(sectionOrgStructure, err)
		org.CategoriesMissing = true
		return org, nil
	}

	byID := make(map[string]string, len(cats))
	entries := make([]orgEntry, 0, len(cats))
	for _, cat := range cats {
		name, _ := cat["name"].(string)
		if name == "" || name == categoryUnassigned {
			continue
		}
		if id := categoryIDString(cat["id"]); id != "" {
			byID[id] = name
		}
		entries = append(entries, orgEntry{Name: name})
	}

	usage := collectCategoryUsage(ctx, client, byID, policies, profiles)
	for i := range entries {
		entries[i].Count = usage.countFor(entries[i].Name)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Count != entries[j].Count {
			return entries[i].Count > entries[j].Count
		}
		return entries[i].Name < entries[j].Name
	})
	org.Categories = entries
	org.CategoryCountsReliable = usage.reliable()
	org.CategorySources = usage.countedSources()

	return org, nil
}
