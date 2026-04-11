// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func collectProtectData(ctx context.Context, client registry.ProtectClient, data *DashboardData) {
	var protect protectCoverage
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(5)

	// Plans
	go func() {
		defer wg.Done()
		plans, err := client.ListPlans(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: protect plans: %v\n", err)
			return
		}
		mu.Lock()
		protect.Plans = len(plans)
		mu.Unlock()
	}()

	// Analytics — the SDK Analytic type has no Enabled field, so total == active.
	go func() {
		defer wg.Done()
		analytics, err := client.ListAnalytics(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: protect analytics: %v\n", err)
			return
		}
		mu.Lock()
		protect.AnalyticsTotal = len(analytics)
		protect.AnalyticsActive = len(analytics)
		mu.Unlock()
	}()

	// Computers (endpoints)
	go func() {
		defer wg.Done()
		computers, err := client.ListComputers(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: protect computers: %v\n", err)
			return
		}
		mu.Lock()
		protect.Endpoints = len(computers)
		mu.Unlock()
	}()

	// Analytic Sets
	go func() {
		defer wg.Done()
		sets, err := client.ListAnalyticSets(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: protect analytic sets: %v\n", err)
			return
		}
		mu.Lock()
		protect.AnalyticSets = len(sets)
		mu.Unlock()
	}()

	// Exception Sets
	go func() {
		defer wg.Done()
		sets, err := client.ListExceptionSets(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: protect exception sets: %v\n", err)
			return
		}
		mu.Lock()
		protect.ExceptionSets = len(sets)
		mu.Unlock()
	}()

	wg.Wait()
	data.Protect = &protect
}
