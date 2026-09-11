// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"

	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/securitycloud"
)

func collectSecurityCloudData(ctx context.Context, platform *jamfplatform.Client, data *DashboardData) {
	sc := securitycloud.New(platform)

	var result securityCloudStatus
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(4)

	// ZTNA Apps — also builds per-category breakdown
	go func() {
		defer wg.Done()
		apps, err := sc.ListZtnaAppsV1(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: security cloud ztna apps: %v\n", err)
			return
		}
		counts := map[string]int{}
		for _, a := range apps {
			cat := a.CategoryName
			if cat == "" {
				cat = "Uncategorized"
			}
			counts[cat]++
		}
		cats := make([]secCloudCategory, 0, len(counts))
		for name, count := range counts {
			cats = append(cats, secCloudCategory{Name: name, Count: count})
		}
		sort.Slice(cats, func(i, j int) bool {
			if cats[i].Count != cats[j].Count {
				return cats[i].Count > cats[j].Count
			}
			return cats[i].Name < cats[j].Name
		})
		mu.Lock()
		result.ZtnaApps = len(apps)
		result.AppsByCategory = cats
		mu.Unlock()
	}()

	// ZTNA Gateways
	go func() {
		defer wg.Done()
		resp, err := sc.ListZtnaGatewaysV1(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: security cloud ztna gateways: %v\n", err)
			return
		}
		mu.Lock()
		result.ZtnaGateways = len(resp.Results)
		mu.Unlock()
	}()

	// Device Groups
	go func() {
		defer wg.Done()
		resp, err := sc.ListDeviceGroupsV2(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: security cloud device groups: %v\n", err)
			return
		}
		mu.Lock()
		result.DeviceGroups = len(resp.Groups)
		mu.Unlock()
	}()

	// DNS Zones
	go func() {
		defer wg.Done()
		resp, err := sc.ListDnsZonesV1(ctx, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: security cloud dns zones: %v\n", err)
			return
		}
		mu.Lock()
		result.DnsZones = len(resp.Results)
		mu.Unlock()
	}()

	wg.Wait()

	// UEM connector — sequential, only worth checking if gateway is reachable
	connectors, err := sc.ListUemConnectorsV1(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dashboard: security cloud uem connectors: %v\n", err)
	} else {
		result.UemConnector = len(connectors.Results) > 0
	}

	data.SecurityCloud = &result
}
