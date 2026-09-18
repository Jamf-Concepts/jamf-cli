// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"sync"

	"github.com/jamf/jamfplatform-go-sdk/jamfplatform"
	"github.com/jamf/jamfplatform-go-sdk/jamfplatform/securitycloud"
)

// securityCloudEntitlementAbsent reports whether every Security Cloud call was
// refused the way an unentitled tenant is refused.
//
// The gateway answers 403 BAD_PERMISSIONS for a capability the credential does
// not hold and 404 for a namespace the tenant does not have, and it answers
// them for every call in the product rather than for one endpoint. So "all five
// calls returned 403 or 404" is absence, and treating it as five collection
// failures made a correct configuration report a permanent nightly partial
// failure — which is how an operator learns to ignore the exit code.
//
// A 5xx, a timeout or a mixed result stays a failure: those are a product the
// tenant does have, not answering.
func securityCloudEntitlementAbsent(errs []error, calls int) bool {
	if len(errs) != calls || calls == 0 {
		return false
	}
	for _, err := range errs {
		apiErr := jamfplatform.AsAPIError(err)
		if apiErr == nil {
			return false
		}
		if apiErr.StatusCode != http.StatusForbidden && apiErr.StatusCode != http.StatusNotFound {
			return false
		}
	}
	return true
}

// securityCloudLister is the five calls the section is built from, behind an
// interface so the collector's decisions — which failures are an absent
// product, whether the section is attached at all — are testable without a
// tenant. securitycloud.Client satisfies it as-is.
type securityCloudLister interface {
	ListZtnaAppsV1(ctx context.Context) ([]securitycloud.App, error)
	ListZtnaGatewaysV1(ctx context.Context) (*securitycloud.GatewayListResponse, error)
	ListDeviceGroupsV2(ctx context.Context) (*securitycloud.GroupListResponseV2, error)
	ListDnsZonesV1(ctx context.Context, sort string) (*securitycloud.ZoneList, error)
	ListUemConnectorsV1(ctx context.Context) (*securitycloud.ConnectorPage, error)
}

func collectSecurityCloudData(ctx context.Context, platform *jamfplatform.Client, data *DashboardData, status *collectStatus) {
	collectSecurityCloudFrom(ctx, securitycloud.New(platform), data, status)
}

func collectSecurityCloudFrom(ctx context.Context, sc securityCloudLister, data *DashboardData, status *collectStatus) {
	const calls = 5

	var result securityCloudStatus
	var succeeded bool
	var errs []error
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Errors are collected rather than recorded as they happen: whether they
	// are failures at all depends on how the other four calls answered.
	fail := func(what string, err error) {
		fmt.Fprintf(os.Stderr, "dashboard: security cloud %s: %v\n", what, err)
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	}

	wg.Add(4)

	// ZTNA Apps — also builds per-category breakdown
	go func() {
		defer wg.Done()
		apps, err := sc.ListZtnaAppsV1(ctx)
		if err != nil {
			fail("ztna apps", err)
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
		succeeded = true
		mu.Unlock()
	}()

	// ZTNA Gateways
	go func() {
		defer wg.Done()
		resp, err := sc.ListZtnaGatewaysV1(ctx)
		if err != nil {
			fail("ztna gateways", err)
			return
		}
		mu.Lock()
		result.ZtnaGateways = len(resp.Results)
		succeeded = true
		mu.Unlock()
	}()

	// Device Groups
	go func() {
		defer wg.Done()
		resp, err := sc.ListDeviceGroupsV2(ctx)
		if err != nil {
			fail("device groups", err)
			return
		}
		mu.Lock()
		result.DeviceGroups = len(resp.Groups)
		succeeded = true
		mu.Unlock()
	}()

	// DNS Zones
	go func() {
		defer wg.Done()
		resp, err := sc.ListDnsZonesV1(ctx, "")
		if err != nil {
			fail("dns zones", err)
			return
		}
		mu.Lock()
		result.DnsZones = len(resp.Results)
		succeeded = true
		mu.Unlock()
	}()

	wg.Wait()

	// UEM connector — sequential, only worth checking if gateway is reachable
	connectors, err := sc.ListUemConnectorsV1(ctx)
	if err != nil {
		fail("uem connectors", err)
	} else {
		result.UemConnector = len(connectors.Results) > 0
		succeeded = true
	}

	if securityCloudEntitlementAbsent(errs, calls) {
		// Not a failure: the tenant does not own the product. Nothing is
		// recorded, so the banner does not name a section the report was never
		// going to carry, and the run can still exit 0.
		fmt.Fprintln(os.Stderr, "dashboard: Jamf Security Cloud is not available to this credential — omitting the section")
		status.clearSection(sectionSecurityCloud)
		return
	}
	for _, err := range errs {
		status.recordFailureErr(sectionSecurityCloud, err)
	}

	// Only attach the section if at least one call returned. Every call failing
	// leaves an all-zeros result that would render identically to a genuinely
	// empty tenant; suppressing it keeps the failures (already recorded above)
	// from masquerading as real data.
	if succeeded {
		status.recordSuccess(sectionSecurityCloud)
		data.SecurityCloud = &result
	}
}
