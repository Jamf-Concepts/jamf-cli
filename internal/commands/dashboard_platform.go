// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/blueprints"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/compliancebenchmarks"
)

func collectPlatformData(ctx context.Context, client *jamfplatform.Client, data *DashboardData) {
	bp := blueprints.New(client)
	cb := compliancebenchmarks.New(client)

	var platform platformStatus
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(2)

	// Blueprints
	go func() {
		defer wg.Done()
		bps, err := bp.ListBlueprints(ctx, nil, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: blueprints: %v\n", err)
			return
		}
		entries := make([]blueprintEntry, 0, len(bps))
		for _, b := range bps {
			state := ""
			if b.DeploymentState != nil {
				state = b.DeploymentState.State
			}
			entries = append(entries, blueprintEntry{
				Name:            b.Name,
				DeploymentState: state,
			})
		}
		mu.Lock()
		platform.Blueprints = entries
		mu.Unlock()
	}()

	// Benchmarks — DDM declaration reports are omitted; per-device report calls are too expensive for a summary dashboard.
	go func() {
		defer wg.Done()
		resp, err := cb.ListBenchmarks(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: benchmarks: %v\n", err)
			return
		}
		entries := make([]benchmarkEntry, 0, len(resp.Benchmarks))
		for _, b := range resp.Benchmarks {
			pct, err := cb.GetBenchmarkCompliancePercentage(ctx, b.ID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "dashboard: benchmark %s compliance: %v\n", b.ID, err)
				continue
			}
			rules, err := cb.ListBenchmarkRulesStats(ctx, b.ID, "", "")
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
