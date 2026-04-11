// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// collectProData orchestrates all Jamf Pro data collection in parallel.
// Failures in individual sections are logged to stderr; they do not abort
// the dashboard — other sections continue normally.
func collectProData(ctx context.Context, client registry.HTTPClient, data *DashboardData) {
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(6)

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
		patch, err := collectPatchCompliance(ctx, client)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: patch compliance: %v\n", err)
			return
		}
		mu.Lock()
		data.Patch = patch
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
func collectPatchCompliance(ctx context.Context, client registry.HTTPClient) (*patchCompliance, error) {
	configs, err := FetchAllPaginated(ctx, client, "/v2/patch-software-title-configurations", 100)
	if err != nil {
		return nil, fmt.Errorf("patch-software-title-configurations: %w", err)
	}

	if len(configs) == 0 {
		return &patchCompliance{}, nil
	}

	summaries, errs := BoundedParallelFetch(ctx, configs, 5, func(ctx context.Context, cfg map[string]any) (map[string]any, error) {
		id := extractID(cfg)
		if id == "" {
			return nil, fmt.Errorf("missing id in patch config")
		}
		path := fmt.Sprintf("/v2/patch-software-title-configurations/%s/patch-summary", id)
		return fetchJSON(ctx, client, path)
	})

	for _, err := range errs {
		fmt.Fprintf(os.Stderr, "WARNING: patch summary fetch: %v\n", err)
	}

	compliance := &patchCompliance{}
	for _, s := range summaries {
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
	}

	return compliance, nil
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
		os, _ := comp["operatingSystem"].(map[string]any)
		if os == nil {
			continue
		}
		version, _ := os["version"].(string)
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
