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

// collectProData orchestrates all Jamf Pro data collection in parallel.
// Failures in individual sections are logged to stderr; they do not abort
// the dashboard — other sections continue normally.
func collectProData(ctx context.Context, client registry.HTTPClient, data *DashboardData, smartGroupNames []string) {
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(13)

	go func() {
		defer wg.Done()
		fleet, err := collectFleetCounts(ctx, client)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: fleet counts: %v\n", err)
			return
		}
		mu.Lock()
		data.Fleet = fleet
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		security, err := collectSecurityPosture(ctx, client)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: security posture: %v\n", err)
			return
		}
		mu.Lock()
		data.Security = security
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		audit := collectAuditFindings(ctx, client)
		if len(audit.Results) > 0 {
			mu.Lock()
			data.Audit = audit
			mu.Unlock()
		}
	}()

	go func() {
		defer wg.Done()
		patch, spread, err := collectPatchCompliance(ctx, client)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: patch compliance: %v\n", err)
			return
		}
		mu.Lock()
		data.Patch = patch
		data.PatchSpread = spread
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		devices, err := collectDeviceCompliance(ctx, client)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: device compliance: %v\n", err)
			return
		}
		mu.Lock()
		data.Devices = devices
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		osDist, err := collectOSDistribution(ctx, client)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: OS distribution: %v\n", err)
			return
		}
		mu.Lock()
		data.OSDist = osDist
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		envStats, err := collectEnvironmentStats(ctx, client)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: environment stats: %v\n", err)
			return
		}
		mu.Lock()
		data.EnvStats = envStats
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		checkin, err := collectCheckinStatus(ctx, client)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: check-in status: %v\n", err)
			return
		}
		mu.Lock()
		data.Checkin = checkin
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		hw, err := collectHardwareModels(ctx, client)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: hardware models: %v\n", err)
			return
		}
		mu.Lock()
		data.Hardware = hw
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		sg, err := collectSmartGroups(ctx, client, "/v2/computer-groups/smart-groups", smartGroupNames, "membershipCount")
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: computer smart groups: %v\n", err)
			return
		}
		mu.Lock()
		data.ComputerSmartGroups = sg
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		sg, err := collectSmartGroups(ctx, client, "/v1/mobile-device-groups/smart-groups", smartGroupNames, "count")
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: mobile smart groups: %v\n", err)
			return
		}
		mu.Lock()
		data.MobileSmartGroups = sg
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		cleanup, err := collectCleanupAnalysis(ctx, client)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: cleanup analysis: %v\n", err)
			return
		}
		mu.Lock()
		data.Cleanup = cleanup
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		org, err := collectOrgStructure(ctx, client)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: org structure: %v\n", err)
			return
		}
		mu.Lock()
		data.OrgStructure = org
		mu.Unlock()
	}()

	wg.Wait()

	// Populate smart group fleet totals from already-fetched fleet data (avoids re-fetching inventory-information).
	if data.Fleet != nil {
		if data.ComputerSmartGroups != nil {
			data.ComputerSmartGroups.TotalFleet = data.Fleet.ManagedComputers + data.Fleet.UnmanagedComputers
		}
		if data.MobileSmartGroups != nil {
			data.MobileSmartGroups.TotalFleet = data.Fleet.ManagedMobile + data.Fleet.UnmanagedMobile
		}
	}
}

// collectFleetCounts fetches managed/unmanaged computer and mobile counts
// from /v1/inventory-information, and the total user count from /v1/users.
func collectFleetCounts(ctx context.Context, client registry.HTTPClient) (*fleetSummary, error) {
	inv, err := fetchJSON(ctx, client, "/v1/inventory-information")
	if err != nil {
		return nil, fmt.Errorf("inventory-information: %w", err)
	}

	managed, _ := inv["managedComputers"].(float64)
	unmanaged, _ := inv["unmanagedComputers"].(float64)
	managedMobile, _ := inv["managedDevices"].(float64)
	unmanagedMobile, _ := inv["unmanagedDevices"].(float64)

	usersData, err := fetchJSON(ctx, client, "/v1/users?page-size=1")
	users := 0
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: user count: %v\n", err)
	} else {
		if tc, ok := usersData["totalCount"].(float64); ok {
			users = int(tc)
		}
	}

	return &fleetSummary{
		ManagedComputers:   int(managed),
		UnmanagedComputers: int(unmanaged),
		ManagedMobile:      int(managedMobile),
		UnmanagedMobile:    int(unmanagedMobile),
		Users:              users,
	}, nil
}

// collectSecurityPosture fetches all computers with SECURITY and DISK_ENCRYPTION
// sections, then counts how many have each security feature enabled.
func collectSecurityPosture(ctx context.Context, client registry.HTTPClient) (*securityPosture, error) {
	all, err := FetchAllPaginated(ctx, client, "/v3/computers-inventory?section=SECURITY&section=DISK_ENCRYPTION", 500)
	if err != nil {
		return nil, fmt.Errorf("computers-inventory security: %w", err)
	}

	posture := &securityPosture{Total: len(all)}

	for _, comp := range all {
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

// collectAuditFindings runs all audit checks sequentially and collects results.
// Individual check failures are logged to stderr and skipped.
func collectAuditFindings(ctx context.Context, client registry.HTTPClient) *auditSummary {
	checks := allAuditChecks()
	summary := &auditSummary{}

	for _, check := range checks {
		result, err := check.Run(ctx, client, 14)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: audit check %q: %v\n", check.Name, err)
			continue
		}
		if result != nil {
			summary.Results = append(summary.Results, *result)
		}
	}

	return summary
}

// collectPatchCompliance fetches all patch software title configurations and
// then collects a per-title patch summary in parallel.
func collectPatchCompliance(ctx context.Context, client registry.HTTPClient) (*patchCompliance, []patchVersionSpread, error) {
	configs, err := FetchAllPaginated(ctx, client, "/v2/patch-software-title-configurations", 100)
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
		summaryPath := fmt.Sprintf("/v2/patch-software-title-configurations/%s/patch-summary", id)
		summary, err := fetchJSON(ctx, client, summaryPath)
		if err != nil {
			return patchResult{}, err
		}

		versionsPath := fmt.Sprintf("/v2/patch-software-title-configurations/%s/patch-summary/versions", id)
		versions, vErr := FetchAllPaginated(ctx, client, versionsPath, 100)
		if vErr != nil {
			fmt.Fprintf(os.Stderr, "WARNING: patch versions for %s: %v\n", id, vErr)
		}

		return patchResult{Summary: summary, Versions: versions}, nil
	})

	for _, err := range errs {
		fmt.Fprintf(os.Stderr, "WARNING: patch summary fetch: %v\n", err)
	}

	compliance := &patchCompliance{}
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

// collectDeviceCompliance fetches stale check-in count and failed MDM command count.
func collectDeviceCompliance(ctx context.Context, client registry.HTTPClient) (*deviceCompliance, error) {
	const staleDays = 14
	cutoff := timeNow().AddDate(0, 0, -staleDays).UTC().Format("2006-01-02")
	staleData, err := fetchJSON(ctx, client,
		fmt.Sprintf("/v3/computers-inventory?section=GENERAL&page-size=1&filter=general.lastContactTime%%3C%s", cutoff))
	staleCount := 0
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: stale device check-in count: %v\n", err)
	} else {
		if tc, ok := staleData["totalCount"].(float64); ok {
			staleCount = int(tc)
		}
	}

	mdmData, err := fetchJSON(ctx, client, "/v2/mdm/commands?filter=status%3D%3DError&page-size=1")
	failedMDM := 0
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed MDM commands: %v\n", err)
	} else {
		if tc, ok := mdmData["totalCount"].(float64); ok {
			failedMDM = int(tc)
		}
	}

	return &deviceCompliance{
		StaleDevices:       staleCount,
		FailedMDMCommands:  failedMDM,
		StaleThresholdDays: staleDays,
	}, nil
}

// collectOSDistribution fetches all computers with OPERATING_SYSTEM section
// and groups them by OS version, sorted by count descending.
func collectOSDistribution(ctx context.Context, client registry.HTTPClient) (*osDistribution, error) {
	all, err := FetchAllPaginated(ctx, client, "/v3/computers-inventory?section=OPERATING_SYSTEM", 500)
	if err != nil {
		return nil, fmt.Errorf("computers-inventory OS section: %w", err)
	}

	counts := make(map[string]int)
	for _, comp := range all {
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

func collectEnvironmentStats(ctx context.Context, client registry.HTTPClient) (*environmentStats, error) {
	stats := &environmentStats{}
	var mu sync.Mutex
	var wg sync.WaitGroup

	type countTask struct {
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
		{&stats.Policies, classicCount("/JSSResource/policies")},
		{&stats.ConfigProfiles, classicCount("/JSSResource/osxconfigurationprofiles")},
		{&stats.Scripts, paginatedCount("/v1/scripts")},
		{&stats.Packages, classicCount("/JSSResource/packages")},
		{&stats.ComputerSmartGrps, paginatedCount("/v2/computer-groups/smart-groups")},
		{&stats.MobileSmartGrps, paginatedCount("/v1/mobile-device-groups/smart-groups")},
		{&stats.ExtAttributes, paginatedCount("/v1/computer-extension-attributes")},
		{&stats.Categories, paginatedCount("/v1/categories")},
	}

	wg.Add(len(tasks))
	for _, t := range tasks {
		go func(tgt *int, fn func() (int, error)) {
			defer wg.Done()
			val, err := fn()
			if err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: environment stat: %v\n", err)
				return
			}
			mu.Lock()
			*tgt = val
			mu.Unlock()
		}(t.target, t.fn)
	}
	wg.Wait()

	return stats, nil
}

func collectCheckinStatus(ctx context.Context, client registry.HTTPClient) (*checkinStatus, error) {
	const thresholdDays = 7
	cutoff := timeNow().AddDate(0, 0, -thresholdDays).UTC().Format("2006-01-02")

	status := &checkinStatus{ThresholdDays: thresholdDays}
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(4)

	go func() {
		defer wg.Done()
		n, err := fetchPaginatedCountInt(ctx, client, "/v3/computers-inventory?section=GENERAL")
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: computer total count: %v\n", err)
			return
		}
		mu.Lock()
		status.ComputersTotal = n
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		n, err := fetchPaginatedCountInt(ctx, client,
			fmt.Sprintf("/v3/computers-inventory?section=GENERAL&filter=general.lastContactTime%%3C%s", cutoff))
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: overdue computer count: %v\n", err)
			return
		}
		mu.Lock()
		status.ComputersOverdue = n
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		n, err := fetchPaginatedCountInt(ctx, client, "/v2/mobile-devices")
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: mobile total count: %v\n", err)
			return
		}
		mu.Lock()
		status.MobileTotal = n
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		n, err := fetchPaginatedCountInt(ctx, client,
			fmt.Sprintf("/v2/mobile-devices?filter=lastInventoryUpdateDate%%3C%s", cutoff))
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: overdue mobile count: %v\n", err)
			return
		}
		mu.Lock()
		status.MobileOverdue = n
		mu.Unlock()
	}()

	wg.Wait()
	return status, nil
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

func collectHardwareModels(ctx context.Context, client registry.HTTPClient) (*hardwareModels, error) {
	hw := &hardwareModels{}
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		defer wg.Done()
		all, err := FetchAllPaginated(ctx, client, "/v3/computers-inventory?section=HARDWARE", 500)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: computer hardware: %v\n", err)
			return
		}
		counts := make(map[string]int)
		for _, comp := range all {
			hardware, _ := comp["hardware"].(map[string]any)
			if hardware == nil {
				continue
			}
			if model, _ := hardware["model"].(string); model != "" {
				counts[model]++
			}
		}
		mu.Lock()
		hw.ComputerModels = topNModels(counts, 10)
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		all, err := FetchAllPaginated(ctx, client, "/v2/mobile-devices", 500)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: mobile device models: %v\n", err)
			return
		}
		counts := make(map[string]int)
		for _, dev := range all {
			if model, _ := dev["model"].(string); model != "" {
				counts[model]++
			}
		}
		mu.Lock()
		hw.MobileModels = topNModels(counts, 10)
		mu.Unlock()
	}()

	wg.Wait()

	if len(hw.ComputerModels) == 0 && len(hw.MobileModels) == 0 {
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

// collectCleanupAnalysis identifies housekeeping candidates:
// disabled policies, unscoped policies, unscoped config profiles,
// packages not referenced by any policy, and scripts not referenced by any policy.
func collectCleanupAnalysis(ctx context.Context, client registry.HTTPClient) (*cleanupAnalysis, error) {
	// Fetch policies with full detail to check scope and enabled state.
	policies, err := FetchClassicList(ctx, client, "/JSSResource/policies", "policy")
	if err != nil {
		return nil, fmt.Errorf("policies: %w", err)
	}

	var disabledPolicies, unscopedPolicies int
	referencedPackages := make(map[string]bool)
	referencedScripts := make(map[string]bool)

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
			continue
		}
		pol, _ := detail["policy"].(map[string]any)
		if pol == nil {
			pol = detail
		}

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

	// Unscoped config profiles.
	profiles, err := FetchClassicList(ctx, client, "/JSSResource/osxconfigurationprofiles", "configuration_profile")
	if err != nil {
		return nil, fmt.Errorf("config profiles: %w", err)
	}
	var unscopedProfiles int
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
			continue
		}
		prof, _ := detail["os_x_configuration_profile"].(map[string]any)
		if prof == nil {
			continue
		}
		scope, _ := prof["scope"].(map[string]any)
		if isEmptyScope(scope) {
			unscopedProfiles++
		}
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

// collectOrgStructure fetches sites, buildings, departments, and categories
// with their device counts to give an overview of the org hierarchy.
func collectOrgStructure(ctx context.Context, client registry.HTTPClient) (*orgStructure, error) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	org := &orgStructure{}

	type orgTask struct {
		target *[]orgEntry
		path   string
		key    string
	}

	tasks := []orgTask{
		{&org.Sites, "/JSSResource/sites", "site"},
		{&org.Buildings, "/JSSResource/buildings", "building"},
		{&org.Departments, "/JSSResource/departments", "department"},
	}

	// Fetch all computer inventory once to count per site/building/department.
	allComputers, err := FetchAllPaginated(ctx, client, "/v3/computers-inventory?section=GENERAL", 500)
	if err != nil {
		return nil, fmt.Errorf("computers-inventory: %w", err)
	}

	// Build site/building/department counts from inventory.
	siteCounts := make(map[string]int)
	buildingCounts := make(map[string]int)
	deptCounts := make(map[string]int)
	for _, comp := range allComputers {
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

	// Categories: fetch list + item count per category in parallel.
	wg.Add(1)
	go func() {
		defer wg.Done()
		cats, err := FetchAllPaginated(ctx, client, "/v1/categories", 100)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: categories: %v\n", err)
			return
		}
		entries := make([]orgEntry, 0, len(cats))
		for _, cat := range cats {
			name, _ := cat["name"].(string)
			if name == "" || name == "No category assigned" {
				continue
			}
			entries = append(entries, orgEntry{Name: name})
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name < entries[j].Name
		})
		mu.Lock()
		org.Categories = entries
		mu.Unlock()
	}()

	_ = tasks // used above via direct count maps
	wg.Wait()

	return org, nil
}
