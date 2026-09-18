// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func collectProtectData(ctx context.Context, client registry.ProtectClient, data *DashboardData, status *collectStatus) {
	var protect protectCoverage
	var succeeded bool
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(5)

	// Plans
	go func() {
		defer wg.Done()
		plans, err := client.ListPlans(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: protect plans: %v\n", err)
			status.recordFailureErr(sectionProtect, err)
			return
		}
		mu.Lock()
		protect.Plans = len(plans)
		succeeded = true
		mu.Unlock()
	}()

	// Analytics — the SDK Analytic type has no Enabled field, so total == active.
	go func() {
		defer wg.Done()
		analytics, err := client.ListAnalytics(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: protect analytics: %v\n", err)
			status.recordFailureErr(sectionProtect, err)
			return
		}
		mu.Lock()
		protect.AnalyticsTotal = len(analytics)
		protect.AnalyticsActive = len(analytics)
		succeeded = true
		mu.Unlock()
	}()

	// Computers (endpoints)
	go func() {
		defer wg.Done()
		computers, err := client.ListComputers(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: protect computers: %v\n", err)
			status.recordFailureErr(sectionProtect, err)
			return
		}
		mu.Lock()
		protect.Endpoints = len(computers)
		succeeded = true
		mu.Unlock()
	}()

	// Analytic Sets
	go func() {
		defer wg.Done()
		sets, err := client.ListAnalyticSets(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: protect analytic sets: %v\n", err)
			status.recordFailureErr(sectionProtect, err)
			return
		}
		mu.Lock()
		protect.AnalyticSets = len(sets)
		succeeded = true
		mu.Unlock()
	}()

	// Exception Sets
	go func() {
		defer wg.Done()
		sets, err := client.ListExceptionSets(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: protect exception sets: %v\n", err)
			status.recordFailureErr(sectionProtect, err)
			return
		}
		mu.Lock()
		protect.ExceptionSets = len(sets)
		succeeded = true
		mu.Unlock()
	}()

	wg.Wait()

	// Keyed on call outcome, not on values. A brand-new Protect tenant whose
	// five calls all succeed with legitimate zeros has data worth rendering,
	// and a partial success would otherwise render "Endpoints: 0" as a figure
	// nobody fetched.
	if succeeded {
		status.recordSuccess(sectionProtect)
		data.Protect = &protect
	}
}
