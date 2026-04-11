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

	wg.Add(11)

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
		sg, err := collectSmartGroups(ctx, client, "/v2/computer-groups/smart-groups", smartGroupNames, "managedComputers", "unmanagedComputers")
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
		sg, err := collectSmartGroups(ctx, client, "/v1/mobile-device-groups/smart-groups", smartGroupNames, "managedMobileDevices", "unmanagedMobileDevices")
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: mobile smart groups: %v\n", err)
			return
		}
		mu.Lock()
		data.MobileSmartGroups = sg
		mu.Unlock()
	}()

	wg.Wait()
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
			sep := "?"
			if strings.Contains(path, "?") {
				sep = "&"
			}
			data, err := fetchJSON(ctx, client, path+sep+"page-size=1")
			if err != nil {
				return 0, err
			}
			tc, _ := data["totalCount"].(float64)
			return int(tc), nil
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
		data, err := fetchJSON(ctx, client, "/v3/computers-inventory?section=GENERAL&page-size=1")
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: computer total count: %v\n", err)
			return
		}
		mu.Lock()
		status.ComputersTotal = int(data["totalCount"].(float64))
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		data, err := fetchJSON(ctx, client,
			fmt.Sprintf("/v3/computers-inventory?section=GENERAL&page-size=1&filter=general.lastContactTime%%3C%s", cutoff))
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: overdue computer count: %v\n", err)
			return
		}
		mu.Lock()
		status.ComputersOverdue = int(data["totalCount"].(float64))
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		data, err := fetchJSON(ctx, client, "/v2/mobile-devices?page-size=1")
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: mobile total count: %v\n", err)
			return
		}
		mu.Lock()
		status.MobileTotal = int(data["totalCount"].(float64))
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		data, err := fetchJSON(ctx, client,
			fmt.Sprintf("/v2/mobile-devices?page-size=1&filter=lastInventoryUpdateDate%%3C%s", cutoff))
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: overdue mobile count: %v\n", err)
			return
		}
		mu.Lock()
		status.MobileOverdue = int(data["totalCount"].(float64))
		mu.Unlock()
	}()

	wg.Wait()
	return status, nil
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
			model, _ := hardware["model"].(string)
			if model != "" {
				counts[model]++
			}
		}
		models := make([]modelCount, 0, len(counts))
		for m, c := range counts {
			models = append(models, modelCount{Model: m, Count: c})
		}
		sort.Slice(models, func(i, j int) bool { return models[i].Count > models[j].Count })
		if len(models) > 10 {
			other := 0
			for _, m := range models[10:] {
				other += m.Count
			}
			models = append(models[:10], modelCount{Model: "Other", Count: other})
		}
		mu.Lock()
		hw.ComputerModels = models
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
			model, _ := dev["model"].(string)
			if model != "" {
				counts[model]++
			}
		}
		models := make([]modelCount, 0, len(counts))
		for m, c := range counts {
			models = append(models, modelCount{Model: m, Count: c})
		}
		sort.Slice(models, func(i, j int) bool { return models[i].Count > models[j].Count })
		if len(models) > 10 {
			other := 0
			for _, m := range models[10:] {
				other += m.Count
			}
			models = append(models[:10], modelCount{Model: "Other", Count: other})
		}
		mu.Lock()
		hw.MobileModels = models
		mu.Unlock()
	}()

	wg.Wait()

	if len(hw.ComputerModels) == 0 && len(hw.MobileModels) == 0 {
		return nil, nil
	}
	return hw, nil
}

func collectSmartGroups(ctx context.Context, client registry.HTTPClient, endpoint string, names []string, managedKey, unmanagedKey string) (*smartGroupSummary, error) {
	allGroups, err := FetchAllPaginated(ctx, client, endpoint, 100)
	if err != nil {
		return nil, fmt.Errorf("smart groups: %w", err)
	}

	countField := "membershipCount"
	if endpoint != "/v2/computer-groups/smart-groups" {
		countField = "count"
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

	inv, err := fetchJSON(ctx, client, "/v1/inventory-information")
	if err == nil {
		managed, _ := inv[managedKey].(float64)
		unmanaged, _ := inv[unmanagedKey].(float64)
		summary.TotalFleet = int(managed) + int(unmanaged)
	}

	return summary, nil
}
