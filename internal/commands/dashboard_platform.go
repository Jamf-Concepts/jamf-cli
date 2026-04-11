// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func collectPlatformData(ctx context.Context, client registry.PlatformClient, data *DashboardData) {
	var platform platformStatus
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(2)

	// Blueprints
	go func() {
		defer wg.Done()
		blueprints, err := client.ListBlueprints(ctx, nil, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: blueprints: %v\n", err)
			return
		}
		entries := make([]blueprintEntry, 0, len(blueprints))
		for _, b := range blueprints {
			entries = append(entries, blueprintEntry{
				Name:            b.Name,
				DeploymentState: b.DeploymentState.State,
			})
		}
		mu.Lock()
		platform.Blueprints = entries
		mu.Unlock()
	}()

	// Benchmarks — DDM declaration reports are omitted; per-device report calls are too expensive for a summary dashboard.
	go func() {
		defer wg.Done()
		resp, err := client.ListBenchmarks(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: benchmarks: %v\n", err)
			return
		}
		entries := make([]benchmarkEntry, 0, len(resp.Benchmarks))
		for _, b := range resp.Benchmarks {
			pct, err := client.GetBenchmarkCompliancePercentage(ctx, b.ID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "dashboard: benchmark %s compliance: %v\n", b.ID, err)
				continue
			}
			rules, err := client.ListBenchmarkRulesStats(ctx, b.ID, "", "")
			if err != nil {
				fmt.Fprintf(os.Stderr, "dashboard: benchmark %s rules: %v\n", b.ID, err)
				continue
			}
			failingRules := 0
			for _, r := range rules {
				if r.Failed > 0 {
					failingRules++
				}
			}
			entries = append(entries, benchmarkEntry{
				Title:         b.Title,
				CompliancePct: float64(pct.CompliancePercentage),
				FailingRules:  failingRules,
			})
		}
		mu.Lock()
		platform.Benchmarks = entries
		mu.Unlock()
	}()

	wg.Wait()

	// Only set the section if at least one API call returned data.
	if len(platform.Blueprints) > 0 || len(platform.Benchmarks) > 0 {
		data.Platform = &platform
	}
}
