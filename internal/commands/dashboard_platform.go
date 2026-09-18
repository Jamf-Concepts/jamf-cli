// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/jamf/jamfplatform-go-sdk/jamfplatform"
	"github.com/jamf/jamfplatform-go-sdk/jamfplatform/blueprints"
	"github.com/jamf/jamfplatform-go-sdk/jamfplatform/compliancebenchmarks"
)

func collectPlatformData(ctx context.Context, client *jamfplatform.Client, data *DashboardData, status *collectStatus) {
	bp := blueprints.New(client)
	cb := compliancebenchmarks.New(client)

	var platform platformStatus
	var succeeded bool
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(2)

	// Blueprints
	go func() {
		defer wg.Done()
		bps, err := bp.ListBlueprints(ctx, nil, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: blueprints: %v\n", err)
			status.recordFailureErr(sectionPlatform, err)
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
		succeeded = true
		mu.Unlock()
	}()

	// Benchmarks — DDM declaration reports are omitted; per-device report calls are too expensive for a summary dashboard.
	go func() {
		defer wg.Done()
		resp, err := cb.ListBenchmarks(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: benchmarks: %v\n", err)
			status.recordFailureErr(sectionPlatform, err)
			return
		}
		entries := make([]benchmarkEntry, 0, len(resp.Benchmarks))
		skipped := 0
		for _, b := range resp.Benchmarks {
			pct, err := cb.GetBenchmarkCompliancePercentage(ctx, b.ID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "dashboard: benchmark %s compliance: %v\n", b.ID, err)
				status.recordFailureErr(sectionPlatform, err)
				skipped++
				continue
			}
			rules, err := cb.ListBenchmarkRulesStats(ctx, b.ID, "", "")
			if err != nil {
				fmt.Fprintf(os.Stderr, "dashboard: benchmark %s rules: %v\n", b.ID, err)
				status.recordFailureErr(sectionPlatform, err)
				skipped++
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
		platform.BenchmarksSkipped = skipped
		succeeded = true
		mu.Unlock()
	}()

	wg.Wait()

	// Keyed on call outcome, not on values: a tenant with no blueprints and no
	// benchmarks still has an answer worth rendering, and a partial success
	// otherwise renders an empty list as "none configured".
	if succeeded {
		status.recordSuccess(sectionPlatform)
		data.Platform = &platform
	}
}
