// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/jamf/jamfplatform-go-sdk/jamfplatform"
	"github.com/jamf/jamfplatform-go-sdk/jamfplatform/securitycloud"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

// emptyInventory stands in for an inventory pass that could not be fetched.
func failedInventory() *proInventory {
	return &proInventory{err: errors.New("computers-inventory: boom")}
}

// TestCollectProDataFast_RecordsFailuresWhenEveryCallFails asserts that a
// dashboard whose collectors could not fetch anything must not report success.
func TestCollectProDataFast_RecordsFailuresWhenEveryCallFails(t *testing.T) {
	mock := &overviewMockClient{responses: map[string]overviewMockResponse{}}
	data := &DashboardData{}
	status := &collectStatus{}

	collectProDataFast(context.Background(), mock, data, nil, status, failedInventory())

	if status.failures() == 0 {
		t.Fatal("expected collectProDataFast to record failures against an empty mock, got 0")
	}
	if data.Fleet != nil {
		t.Error("Fleet should be nil when its fetch failed")
	}
	if data.Security != nil {
		t.Error("Security should be nil when its fetch failed")
	}
}

// TestCollectProDataFast_EveryWarningReachesTheTally is the finding this file
// exists for: the collectors that never return an error — check-in, environment
// stats, device compliance, fleet's user count — used to warn to stderr and
// leave their fields at zero with nothing recorded, so the banner read "0" and
// the run exited 0 while six figures were wrong.
//
// Against an empty mock every one of those sub-fetches fails, so every section
// they belong to must be named as failed.
func TestCollectProDataFast_EveryWarningReachesTheTally(t *testing.T) {
	mock := &overviewMockClient{responses: map[string]overviewMockResponse{}}
	data := &DashboardData{}
	status := &collectStatus{}

	collectProDataFast(context.Background(), mock, data, nil, status, failedInventory())

	failed := map[string]bool{}
	for _, name := range status.failedSections() {
		failed[name] = true
	}
	for _, want := range []string{
		sectionFleet, sectionSecurity, sectionAudit, sectionCheckin,
		sectionOSDist, sectionEnvironment, sectionSmartGroups,
	} {
		if !failed[want] {
			t.Errorf("section %q lost every fetch and was not recorded as failed; recorded: %v", want, status.failedSections())
		}
	}
	if status.successes() != 0 {
		t.Errorf("successes() = %d, want 0 when nothing was fetched", status.successes())
	}
}

// TestCollectCheckinStatus_MarksAFailedHalfRatherThanRenderingZero pins the
// sharpest case: the overdue query failing while the total succeeds leaves
// ComputersOverdue at 0, which makes CheckedInPct read 100 — a green ring and
// "0 overdue" on a fleet where nothing has checked in.
func TestCollectCheckinStatus_MarksAFailedHalfRatherThanRenderingZero(t *testing.T) {
	// The total answers; the filtered (overdue) query does not.
	mock := &overviewMockClient{
		keyed: map[string]overviewMockResponse{
			"GET /v4/computers-inventory?section=GENERAL&page-size=1": {200, `{"totalCount":500,"results":[]}`},
			"GET /v2/mobile-devices?page-size=1":                      {200, `{"totalCount":0,"results":[]}`},
		},
	}
	status := &collectStatus{}

	ck := collectCheckinStatus(context.Background(), mock, status)

	if !ck.ComputerDataMissing {
		t.Fatal("ComputerDataMissing = false; a failed overdue query must mark the half missing, or CheckedInPct renders 100%")
	}
	if ck.ComputersReliable() {
		t.Error("ComputersReliable() = true, want false")
	}
	if ck.OverallReliable() {
		t.Error("OverallReliable() = true, want false — the overall ring sums the failed half")
	}

	// And the rendering must not draw the ring.
	var buf bytes.Buffer
	if err := renderDashboard(&buf, &DashboardData{Checkin: ck}); err != nil {
		t.Fatalf("renderDashboard: %v", err)
	}
	if !strings.Contains(buf.String(), "unavailable") {
		t.Error("HTML must mark an uncollected check-in half as unavailable rather than drawing a ring")
	}
}

// TestCollectDeviceCompliance_MarksAFailedCountRatherThanHidingTheCard: the
// template hides an alert card whose count is zero, so a failed fetch used to
// remove the card entirely — the quietest way to report a problem.
func TestCollectDeviceCompliance_MarksAFailedCountRatherThanHidingTheCard(t *testing.T) {
	mock := &overviewMockClient{responses: map[string]overviewMockResponse{}}
	status := &collectStatus{}

	devices := collectDeviceCompliance(context.Background(), mock, status)

	if !devices.StaleMissing || !devices.MDMMissing {
		t.Fatalf("StaleMissing=%v MDMMissing=%v, want both true", devices.StaleMissing, devices.MDMMissing)
	}
	if got := status.failedSections(); len(got) != 1 || got[0] != sectionFleet {
		t.Errorf("failedSections() = %v, want [%q]", got, sectionFleet)
	}

	var buf bytes.Buffer
	if err := renderDashboard(&buf, &DashboardData{Devices: devices}); err != nil {
		t.Fatalf("renderDashboard: %v", err)
	}
	if !strings.Contains(buf.String(), "Failed MDM commands") {
		t.Error("a failed MDM-command fetch must still render its card, marked unavailable")
	}
}

// TestCollectProDataFast_NoFailuresWhenEveryCallSucceeds is the mirror.
func TestCollectProDataFast_NoFailuresWhenEveryCallSucceeds(t *testing.T) {
	data := &DashboardData{}
	status := &collectStatus{}
	mock := buildDashboardFastMock()

	collectProDataFast(context.Background(), mock, data, nil, status,
		fetchProInventory(context.Background(), mock, []string{"SECURITY", "DISK_ENCRYPTION", "OPERATING_SYSTEM"}))

	if got := status.failedSections(); len(got) != 0 {
		t.Fatalf("expected no failed sections against a full mock, got %v", got)
	}
	if data.Fleet == nil {
		t.Error("Fleet should be populated on success")
	}
	if data.Security == nil {
		t.Error("Security should be populated on success")
	}
	if data.OSDist == nil {
		t.Error("OSDist should be populated on success")
	}
	if data.Fleet != nil && data.Fleet.UsersMissing {
		t.Error("UsersMissing should be false when /v1/users answered")
	}
}

// TestFetchProInventory_RequestsEverySectionItsReadersNeed pins the request's
// shape. The section list is what every downstream collector reads, and a
// dropped `&section=…` leaves the field it feeds silently empty — which no test
// could see while the mock fell back to a query-stripped lookup.
func TestFetchProInventory_RequestsEverySectionItsReadersNeed(t *testing.T) {
	mock := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v4/computers-inventory": {200, `{"totalCount":0,"results":[]}`},
		},
	}

	sections := []string{"SECURITY", "DISK_ENCRYPTION", "OPERATING_SYSTEM", "HARDWARE", "GENERAL"}
	fetchProInventory(context.Background(), mock, sections)

	got := mock.requestedMatching("/v4/computers-inventory")
	if len(got) == 0 {
		t.Fatal("no inventory request recorded")
	}
	for _, s := range sections {
		if !strings.Contains(got[0], "section="+s) {
			t.Errorf("inventory request %q is missing section=%s", got[0], s)
		}
	}
}

// TestCollectProData_SweepsInventoryOnce is finding (6)'s efficiency half: six
// collectors used to sweep /v4/computers-inventory independently, so a
// 50,000-device instance paid for the same records six times.
func TestCollectProData_SweepsInventoryOnce(t *testing.T) {
	mock := buildDashboardFastMock()
	collectProData(context.Background(), mock, &DashboardData{}, nil, false, &collectStatus{})

	// The check-in collector's own count requests are page-size=1 lookups
	// rather than sweeps, so only the section-bearing sweep is counted.
	sweeps := 0
	for _, r := range mock.requestedMatching("/v4/computers-inventory") {
		if strings.Contains(r, "section=SECURITY") {
			sweeps++
		}
	}
	if sweeps != 1 {
		t.Errorf("counted %d inventory sweeps, want exactly 1 (requests: %v)", sweeps, mock.requestedMatching("/v4/computers-inventory"))
	}
}

// TestFastTierCostIsIndependentOfFleetSize is finding (6)'s headline: the fast
// tier was documented as "~20 API calls, independent of instance size" while
// four of its audit checks swept the fleet or issued one request per policy.
//
// The assertion is a comparison rather than an absolute: a 10,000-record fleet
// and a 100-record one must cost the same number of requests once the shared
// inventory page sequence is discounted.
func TestFastTierCostIsIndependentOfFleetSize(t *testing.T) {
	count := func(records int) int {
		mock := buildDashboardFastMock()
		// One page, but a totalCount claiming a large fleet: anything reading
		// the count rather than the records would scale off this.
		mock.responses["/v4/computers-inventory"] = overviewMockResponse{200, `{"totalCount":` + itoa(records) + `,"results":[]}`}
		mock.responses["/JSSResource/policies"] = overviewMockResponse{200, manyPolicies(records / 100)}
		collectProData(context.Background(), mock, &DashboardData{}, nil, false, &collectStatus{})
		return len(mock.recorded())
	}

	small, large := count(100), count(10000)
	if small != large {
		t.Errorf("fast tier cost %d requests for a 100-record fleet and %d for a 10,000-record one; the tier must be fixed-cost", small, large)
	}
}

// TestFleetScaledAuditChecksAreNotInTheFastTier is the structural half of the
// same finding: the four checks are named, so moving one back into the
// fixed-cost list fails here rather than on a customer's 50,000-device
// instance.
func TestFleetScaledAuditChecksAreNotInTheFastTier(t *testing.T) {
	fixed := map[string]bool{}
	for _, c := range fixedCostAuditChecks() {
		fixed[c.Name] = true
	}
	for _, name := range []string{
		"Unencrypted devices", "Gatekeeper disabled",
		"Duplicate serial numbers", "Policies with no scope",
	} {
		if fixed[name] {
			t.Errorf("audit check %q scales with the fleet or the policy count and must not be in the fast tier", name)
		}
	}
	// Every fleet-scaled check must be answerable from a shared pass, or the
	// full tier pays for its own sweep on top of the one it already made.
	for _, c := range fleetScaledAuditChecks() {
		if c.FromInventory == nil && c.FromPolicies == nil {
			t.Errorf("fleet-scaled check %q has no FromInventory or FromPolicies twin", c.Name)
		}
	}
	// And allAuditChecks must still be the union, so `pro audit` loses nothing.
	if got, want := len(allAuditChecks()), len(fixedCostAuditChecks())+len(fleetScaledAuditChecks()); got != want {
		t.Errorf("allAuditChecks() has %d checks, want %d", got, want)
	}
}

// TestCollectStatus_RecordsSectionsNotFetches documents the contract at the
// collectStatus boundary. It used to count fetches, so a Security Cloud outage
// — five calls behind one rendered section — reported "5 sections could not be
// collected" on a report with one section missing.
func TestCollectStatus_RecordsSectionsNotFetches(t *testing.T) {
	status := &collectStatus{}
	if status.failures() != 0 {
		t.Fatal("a fresh collectStatus must start at zero failures")
	}
	status.recordFailure(sectionSecurityCloud)
	status.recordFailure(sectionSecurityCloud)
	status.recordFailure(sectionSecurityCloud)
	if got := status.failures(); got != 1 {
		t.Fatalf("failures() = %d, want 1 — three failed fetches in one section are one missing section", got)
	}
	status.recordFailure(sectionPlatform)
	if got := status.failedSections(); len(got) != 2 || got[0] != sectionPlatform || got[1] != sectionSecurityCloud {
		t.Fatalf("failedSections() = %v, want [%q %q] sorted", got, sectionPlatform, sectionSecurityCloud)
	}
	status.clearSection(sectionSecurityCloud)
	if got := status.failedSections(); len(got) != 1 || got[0] != sectionPlatform {
		t.Fatalf("after clearSection, failedSections() = %v, want [%q]", got, sectionPlatform)
	}
}

// TestFinishDashboard_ExitContract is the seam finding (13) asked for: the exit
// code is what a pipeline depends on, and runDashboard had no test caller at
// all — which is why the exit-7 regression in the MCP path shipped.
func TestFinishDashboard_ExitContract(t *testing.T) {
	t.Run("clean run exits 0", func(t *testing.T) {
		status := &collectStatus{}
		status.recordSuccess(sectionFleet)
		var buf bytes.Buffer
		if err := finishDashboard(&buf, &DashboardData{}, status); err != nil {
			t.Fatalf("finishDashboard returned %v, want nil", err)
		}
		// "Incomplete report:" rather than the CSS class name, which is in the
		// stylesheet on every render.
		if strings.Contains(buf.String(), "Incomplete report:") {
			t.Error("a clean run must not render the incomplete banner")
		}
	})

	t.Run("partial run exits 7 and names the sections", func(t *testing.T) {
		status := &collectStatus{}
		status.recordSuccess(sectionFleet)
		status.recordFailure(sectionSecurityCloud)
		status.recordFailure(sectionPlatform)

		var buf bytes.Buffer
		data := &DashboardData{}
		err := finishDashboard(&buf, data, status)
		if err == nil {
			t.Fatal("finishDashboard returned nil for a partial run, want exit 7")
		}
		if got := exitcode.CodeFrom(err); got != exitcode.PartialFailure {
			t.Fatalf("exit code = %d, want %d", got, exitcode.PartialFailure)
		}
		var e *exitcode.Error
		if !errors.As(err, &e) {
			t.Fatal("error is not an *exitcode.Error, so it carries no details")
		}
		if got, ok := e.Details["failed_sections"].([]string); !ok || len(got) != 2 {
			t.Errorf("failed_sections detail = %v, want two names", e.Details["failed_sections"])
		}
		if len(data.IncompleteSections) != 2 || data.TotalSections != 3 {
			t.Errorf("IncompleteSections=%v TotalSections=%d, want 2 of 3", data.IncompleteSections, data.TotalSections)
		}
		html := buf.String()
		if !strings.Contains(html, "Incomplete report:") {
			t.Error("a partial run must render the incomplete banner")
		}
		if !strings.Contains(html, sectionPlatform) || !strings.Contains(html, sectionSecurityCloud) {
			t.Error("the banner must name the missing sections; a bare count is not checkable by the reader")
		}
	})

	t.Run("nothing succeeded propagates the underlying code", func(t *testing.T) {
		status := &collectStatus{}
		status.recordFailureErr(sectionFleet, exitcode.New(exitcode.Authentication, "401"))

		var buf bytes.Buffer
		err := finishDashboard(&buf, &DashboardData{}, status)
		if got := exitcode.CodeFrom(err); got != exitcode.Authentication {
			t.Fatalf("exit code = %d, want %d — a run that collected nothing is not a partial success", got, exitcode.Authentication)
		}
	})

	t.Run("--allow-partial-failure downgrades to 0", func(t *testing.T) {
		prev := allowPartialFailure
		allowPartialFailure = true
		defer func() { allowPartialFailure = prev }()

		status := &collectStatus{}
		status.recordSuccess(sectionFleet)
		status.recordFailure(sectionPlatform)

		var buf bytes.Buffer
		if err := finishDashboard(&buf, &DashboardData{}, status); err != nil {
			t.Fatalf("finishDashboard returned %v; --allow-partial-failure must downgrade a partial run to exit 0", err)
		}
		if !strings.Contains(buf.String(), "Incomplete report:") {
			t.Error("the banner must still render: the flag changes the exit code, not the document")
		}
	})

	t.Run("--allow-partial-failure does not mask a total failure", func(t *testing.T) {
		prev := allowPartialFailure
		allowPartialFailure = true
		defer func() { allowPartialFailure = prev }()

		status := &collectStatus{}
		status.recordFailureErr(sectionFleet, exitcode.New(exitcode.Authentication, "401"))

		var buf bytes.Buffer
		if err := finishDashboard(&buf, &DashboardData{}, status); err == nil {
			t.Fatal("a run where nothing succeeded must still fail, whatever --allow-partial-failure says")
		}
	})
}

// TestRefuseOverlappingProducts covers finding (5): two profiles populating the
// same sections overwrite each other while the header badges both, so one
// tenant's figures were presented as covering two.
func TestRefuseOverlappingProducts(t *testing.T) {
	pro := func(name string) resolvedClients {
		return resolvedClients{profile: dashboardProfile{Name: name, Product: "pro"}}
	}
	platform := func(name string) resolvedClients {
		return resolvedClients{profile: dashboardProfile{Name: name, Product: "platform"}}
	}
	protect := func(name string) resolvedClients {
		return resolvedClients{profile: dashboardProfile{Name: name, Product: "protect"}}
	}

	t.Run("two pro profiles are refused", func(t *testing.T) {
		err := refuseOverlappingProducts([]resolvedClients{pro("a"), pro("b")})
		if err == nil {
			t.Fatal("two pro profiles must be refused; their collectors overwrite each other")
		}
		for _, want := range []string{`"a"`, `"b"`, "pro"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("refusal %q must name %s", err, want)
			}
		}
	})

	t.Run("a platform profile collides with a pro profile", func(t *testing.T) {
		if err := refuseOverlappingProducts([]resolvedClients{platform("plat"), pro("inst")}); err == nil {
			t.Fatal("a platform profile collects the Pro sections too, so it collides with a pro profile")
		}
	})

	t.Run("two platform profiles are refused", func(t *testing.T) {
		if err := refuseOverlappingProducts([]resolvedClients{platform("a"), platform("b")}); err == nil {
			t.Fatal("two platform profiles must be refused")
		}
	})

	t.Run("disjoint products are allowed", func(t *testing.T) {
		if err := refuseOverlappingProducts([]resolvedClients{pro("inst"), protect("prot")}); err != nil {
			t.Fatalf("pro + protect populate different sections and must be allowed, got %v", err)
		}
	})

	t.Run("one profile is allowed", func(t *testing.T) {
		if err := refuseOverlappingProducts([]resolvedClients{platform("only")}); err != nil {
			t.Fatalf("a single profile cannot collide, got %v", err)
		}
	})
}

// --- Security Cloud entitlement ---

type fakeSecurityCloud struct {
	err error
	// apps is returned when err is nil.
	apps []securitycloud.App
	// only the first call fails when partial is set.
	partialErr error
}

func (f *fakeSecurityCloud) ListZtnaAppsV1(context.Context) ([]securitycloud.App, error) {
	if f.partialErr != nil {
		return nil, f.partialErr
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.apps, nil
}

func (f *fakeSecurityCloud) ListZtnaGatewaysV1(context.Context) (*securitycloud.GatewayListResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &securitycloud.GatewayListResponse{}, nil
}

func (f *fakeSecurityCloud) ListDeviceGroupsV2(context.Context) (*securitycloud.GroupListResponseV2, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &securitycloud.GroupListResponseV2{}, nil
}

func (f *fakeSecurityCloud) ListDnsZonesV1(context.Context, string) (*securitycloud.ZoneList, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &securitycloud.ZoneList{}, nil
}

func (f *fakeSecurityCloud) ListUemConnectorsV1(context.Context) (*securitycloud.ConnectorPage, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &securitycloud.ConnectorPage{}, nil
}

// TestCollectSecurityCloud_EveryCallFailingSuppressesTheSection is round 1's
// finding (5) fix, now with a test: an all-zeros result renders identically to
// a genuinely empty tenant, so it must not be attached.
func TestCollectSecurityCloud_EveryCallFailingSuppressesTheSection(t *testing.T) {
	data := &DashboardData{}
	status := &collectStatus{}

	collectSecurityCloudFrom(context.Background(), &fakeSecurityCloud{err: errors.New("5xx")}, data, status)

	if data.SecurityCloud != nil {
		t.Error("the section must not be attached when every call failed; an all-zeros card reads as a real empty tenant")
	}
	if got := status.failedSections(); len(got) != 1 || got[0] != sectionSecurityCloud {
		t.Errorf("failedSections() = %v, want [%q] — one section, not five fetches", got, sectionSecurityCloud)
	}
}

// TestCollectSecurityCloud_OneSuccessAttachesTheSection is the mirror.
func TestCollectSecurityCloud_OneSuccessAttachesTheSection(t *testing.T) {
	data := &DashboardData{}
	status := &collectStatus{}

	collectSecurityCloudFrom(context.Background(), &fakeSecurityCloud{}, data, status)

	if data.SecurityCloud == nil {
		t.Fatal("the section must be attached when a call returned, even with legitimate zeros")
	}
	if got := status.failedSections(); len(got) != 0 {
		t.Errorf("failedSections() = %v, want none", got)
	}
}

// TestSecurityCloudEntitlementAbsent covers finding (7): a product the customer
// does not own answers 403/404 on every call, which is an absent section rather
// than five failures — and reporting it as five made a correct configuration a
// permanent nightly partial failure.
func TestSecurityCloudEntitlementAbsent(t *testing.T) {
	forbidden := apiStatusError(http.StatusForbidden)
	notFound := apiStatusError(http.StatusNotFound)
	serverErr := apiStatusError(http.StatusBadGateway)

	cases := []struct {
		name string
		errs []error
		want bool
	}{
		{"all forbidden", []error{forbidden, forbidden, forbidden, forbidden, forbidden}, true},
		{"mixed 403 and 404", []error{forbidden, notFound, forbidden, notFound, forbidden}, true},
		{"one 5xx among them", []error{forbidden, forbidden, serverErr, forbidden, forbidden}, false},
		{"a non-API error", []error{forbidden, forbidden, forbidden, forbidden, errors.New("timeout")}, false},
		{"partial refusal is a failure", []error{forbidden, forbidden}, false},
		{"nothing failed", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := securityCloudEntitlementAbsent(tc.errs, 5); got != tc.want {
				t.Errorf("securityCloudEntitlementAbsent() = %v, want %v", got, tc.want)
			}
		})
	}
}

// apiStatusError builds the SDK's own API error type at a given status, which
// is what securityCloudEntitlementAbsent inspects. A bare errors.New would
// not reach jamfplatform.AsAPIError, which is the distinction the function is
// about.
func apiStatusError(status int) error {
	return &jamfplatform.APIResponseError{
		StatusCode: status,
		Method:     http.MethodGet,
		URL:        "https://eu.api.jamfcloud.com/securitycloud/v1/categories",
		Body:       `{"httpStatus":` + itoa(status) + `,"errors":[{"code":"BAD_PERMISSIONS"}]}`,
	}
}

// itoa and manyPolicies keep the cost test readable.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func manyPolicies(n int) string {
	var b strings.Builder
	b.WriteString(`{"policy":[`)
	for i := range n {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"id":` + itoa(i+1) + `}`)
	}
	b.WriteString(`]}`)
	return b.String()
}

// buildDashboardFastMock returns a mock covering every endpoint the fast-tier
// collectors hit, so collectProDataFast completes with zero recorded failures.
func buildDashboardFastMock() *overviewMockClient {
	return &overviewMockClient{
		responses: map[string]overviewMockResponse{
			// Fleet counts.
			"/v1/inventory-information": {200, `{"managedComputers":500,"unmanagedComputers":10,"managedDevices":200,"unmanagedDevices":5}`},
			"/v1/users":                 {200, `{"totalCount":42,"results":[]}`},

			// Security posture + OS distribution (the shared inventory pass).
			"/v4/computers-inventory": {200, `{"totalCount":0,"results":[]}`},

			// Device compliance.
			"/v2/mdm/commands": {200, `{"totalCount":0,"results":[]}`},

			// Environment stats.
			"/v1/scripts":                           {200, `{"totalCount":25,"results":[]}`},
			"/v1/computer-extension-attributes":     {200, `{"totalCount":3,"results":[]}`},
			"/v1/categories":                        {200, `{"totalCount":12,"results":[]}`},
			"/JSSResource/policies":                 {200, `{"policies":[{"id":1}]}`},
			"/JSSResource/osxconfigurationprofiles": {200, `{"os_x_configuration_profiles":[{"id":1}]}`},
			"/JSSResource/packages":                 {200, `{"packages":[{"id":1}]}`},

			// Check-in status.
			"/v2/mobile-devices": {200, `{"totalCount":0,"results":[]}`},

			// Computer smart groups.
			"/v3/computer-groups/smart-groups":      {200, `{"totalCount":0,"results":[]}`},
			"/v2/mobile-device-groups/smart-groups": {200, `{"totalCount":0,"results":[]}`},

			// Fixed-cost audit checks.
			"/v1/computer-groups":    {200, `{"totalCount":0,"results":[]}`},
			"/v1/device-enrollments": {200, `{"totalCount":0,"results":[]}`},
			"/v3/computer-prestages": {200, `{"totalCount":0,"results":[]}`},
			"/v1/notifications":      {200, `[]`},
		},
	}
}

// TestRenderDashboard_SortsPatchTitlesWorstFirst covers the round-1 mutation
// survivor at the patch sort. The point of the section is to surface the titles
// an administrator has to act on, so the order is the section's whole argument:
// reverse it and the report leads with everything that is already compliant.
func TestRenderDashboard_SortsPatchTitlesWorstFirst(t *testing.T) {
	data := &DashboardData{
		Patch: &patchCompliance{Titles: []patchTitle{
			{Name: "Compliant", CompliancePct: 98},
			{Name: "Worst", CompliancePct: 12},
			{Name: "Middling", CompliancePct: 60},
		}},
	}

	var buf bytes.Buffer
	if err := renderDashboard(&buf, data); err != nil {
		t.Fatalf("renderDashboard: %v", err)
	}

	// The slice is sorted in place, so assert on the data as well as on the
	// rendered order: either alone leaves half the behaviour unobserved.
	got := []string{data.Patch.Titles[0].Name, data.Patch.Titles[1].Name, data.Patch.Titles[2].Name}
	want := []string{"Worst", "Middling", "Compliant"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("patch titles sorted %v, want %v (least compliant first)", got, want)
		}
	}

	html := buf.String()
	worst, middling := strings.Index(html, "Worst"), strings.Index(html, "Middling")
	compliant := strings.Index(html, "Compliant")
	if worst < 0 || middling < 0 || compliant < 0 {
		t.Fatal("every patch title must be rendered")
	}
	if worst >= middling || middling >= compliant {
		t.Errorf("rendered order is Worst=%d Middling=%d Compliant=%d, want ascending compliance", worst, middling, compliant)
	}
}

// TestCollectFleetCounts_ReadsEachFieldFromItsOwnKey covers the round-1 mutation
// survivor where swapping managed and unmanaged survived, because the success
// test asserted only that the fields were non-nil.
func TestCollectFleetCounts_ReadsEachFieldFromItsOwnKey(t *testing.T) {
	mock := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			// Four distinct values, so any two fields swapped is observable.
			"/v1/inventory-information": {200, `{"managedComputers":501,"unmanagedComputers":11,"managedDevices":202,"unmanagedDevices":3}`},
			"/v1/users":                 {200, `{"totalCount":42,"results":[]}`},
		},
	}

	fleet, err := collectFleetCounts(context.Background(), mock, &collectStatus{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, tc := range []struct {
		field string
		got   int
		want  int
	}{
		{"ManagedComputers", fleet.ManagedComputers, 501},
		{"UnmanagedComputers", fleet.UnmanagedComputers, 11},
		{"ManagedMobile", fleet.ManagedMobile, 202},
		{"UnmanagedMobile", fleet.UnmanagedMobile, 3},
		{"Users", fleet.Users, 42},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.field, tc.got, tc.want)
		}
	}
	// And the derived percentages, which are what the rings render.
	if pct := fleet.ComputerManagedPct(); pct < 97.8 || pct > 98.0 {
		t.Errorf("ComputerManagedPct() = %.2f, want ~97.9", pct)
	}
}

// TestCollectFleetCounts_MarksAFailedUserCountRatherThanRenderingZero: Users is
// a headline figure, so a failed /v1/users used to render "0 Users" — a number
// the reader has no way to tell from a real one.
func TestCollectFleetCounts_MarksAFailedUserCountRatherThanRenderingZero(t *testing.T) {
	mock := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/inventory-information": {200, `{"managedComputers":500}`},
		},
	}
	status := &collectStatus{}

	fleet, err := collectFleetCounts(context.Background(), mock, status)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fleet.UsersMissing {
		t.Fatal("UsersMissing = false; a failed user count is not zero users")
	}
	if got := status.failedSections(); len(got) != 1 || got[0] != sectionFleet {
		t.Errorf("failedSections() = %v, want [%q]", got, sectionFleet)
	}

	var buf bytes.Buffer
	if err := renderDashboard(&buf, &DashboardData{Fleet: fleet}); err != nil {
		t.Fatalf("renderDashboard: %v", err)
	}
	if !strings.Contains(buf.String(), "unavailable") {
		t.Error("the Users headline must read unavailable rather than 0")
	}
}

// TestCollectEnvironmentStats_NamesEachUnfetchedStat: each of the eight is an
// independent request, and a failed one used to render 0.
func TestCollectEnvironmentStats_NamesEachUnfetchedStat(t *testing.T) {
	// Only scripts answers; the other seven fail.
	mock := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/scripts": {200, `{"totalCount":25,"results":[]}`},
		},
	}
	status := &collectStatus{}

	stats := collectEnvironmentStats(context.Background(), mock, status)

	if stats.Scripts != 25 {
		t.Errorf("Scripts = %d, want 25", stats.Scripts)
	}
	if stats.Unavailable(envStatScripts) {
		t.Error("a stat that answered must not be marked unavailable")
	}
	if !stats.Unavailable(envStatPolicies) {
		t.Error("a stat whose fetch failed must be marked unavailable rather than left at 0")
	}
	if got := len(stats.MissingLabels()); got != 7 {
		t.Errorf("MissingLabels() has %d entries, want 7: %v", got, stats.MissingLabels())
	}
	if got := status.failedSections(); len(got) != 1 || got[0] != sectionEnvironment {
		t.Errorf("failedSections() = %v, want [%q] — seven failed fetches in one section", got, sectionEnvironment)
	}

	var buf bytes.Buffer
	if err := renderDashboard(&buf, &DashboardData{EnvStats: stats}); err != nil {
		t.Fatalf("renderDashboard: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Not collected:") {
		t.Error("the section must name the stats it could not fetch")
	}
	if !strings.Contains(html, ">n/a<") {
		t.Error("an unfetched stat must render n/a rather than a confident zero")
	}
}

// TestCollectCategoryUsage covers the Org Structure category count, which used
// to be an always-zero "Items" column — a column of zeros reads as "these
// categories are empty" rather than as "nothing was measured".
func TestCollectCategoryUsage(t *testing.T) {
	// One policy and one profile in "Apps", one script in "Apps", one package
	// in "Global"; every other source empty.
	mock := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/scripts":  {200, `{"totalCount":1,"results":[{"id":"1","categoryName":"Apps"}]}`},
			"/v1/packages": {200, `{"totalCount":1,"results":[{"id":"1","categoryId":"7"}]}`},
			"/v1/ebooks":   {200, `{"totalCount":0,"results":[]}`},
			"/v3/patch-software-title-configurations":        {200, `{"totalCount":0,"results":[]}`},
			"/JSSResource/mobiledeviceconfigurationprofiles": {200, `{"configuration_profile":[]}`},
			"/JSSResource/macapplications":                   {200, `{"mac_application":[]}`},
			"/JSSResource/mobiledeviceapplications":          {200, `{"mobile_device_application":[]}`},
			"/JSSResource/printers":                          {200, `{"printer":[]}`},
		},
	}
	policies := &policyDetailSet{details: []map[string]any{
		{"general": map[string]any{"category": map[string]any{"name": "Apps"}}},
	}}
	profiles := &policyDetailSet{details: []map[string]any{
		{"general": map[string]any{"category": "Apps"}},
	}}

	usage := collectCategoryUsage(context.Background(), mock, map[string]string{"7": "Global"}, policies, profiles)

	if !usage.reliable() {
		t.Fatalf("usage must be reliable when every source answered; failed=%v skipped=%d", usage.failed, usage.skipped)
	}
	// A policy nests the category as an object and a profile carries it as a
	// bare string. Both shapes are live, and counting one shape only is how a
	// column of zeros happens.
	if got := usage.countFor("Apps"); got != 3 {
		t.Errorf("Apps count = %d, want 3 (one policy, one profile, one script)", got)
	}
	if got := usage.countFor("Global"); got != 1 {
		t.Errorf("Global count = %d, want 1 (one package, resolved from its id)", got)
	}
	if got := len(usage.countedSources()); got != 10 {
		t.Errorf("counted %d sources, want all 10 category-bearing object types: %v", got, usage.countedSources())
	}
}

// TestCollectCategoryUsage_WithholdsWhenASourceFails: a count assembled from a
// source that could not be read is a floor, and publishing a floor as a figure
// is the same defect as the row of zeros it replaces.
func TestCollectCategoryUsage_WithholdsWhenASourceFails(t *testing.T) {
	mock := &overviewMockClient{responses: map[string]overviewMockResponse{}}
	usage := collectCategoryUsage(context.Background(), mock, nil,
		&policyDetailSet{}, &policyDetailSet{})

	if usage.reliable() {
		t.Error("reliable() = true with every list unreadable")
	}

	// And the rendered table withholds the column.
	var buf bytes.Buffer
	err := renderDashboard(&buf, &DashboardData{OrgStructure: &orgStructure{
		Categories:             []orgEntry{{Name: "Apps", Count: 0}},
		CategoryCountsReliable: false,
	}})
	if err != nil {
		t.Fatalf("renderDashboard: %v", err)
	}
	html := buf.String()
	if strings.Contains(html, "<th>Objects</th>") {
		t.Error("the count column must be withheld when the tally is a floor")
	}
	if !strings.Contains(html, "Object counts withheld") {
		t.Error("the section must say why the column is absent")
	}
}

// TestCategoryIDString normalises the id shapes the Pro API sends, and rejects
// Jamf's -1 no-category sentinel.
func TestCategoryIDString(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{float64(7), "7"},
		{float64(-1), ""},
		{float64(0), ""},
		{"7", "7"},
		{"-1", ""},
		{"0", ""},
		{"", ""},
		{nil, ""},
		{true, ""},
	}
	for _, tc := range cases {
		if got := categoryIDString(tc.in); got != tc.want {
			t.Errorf("categoryIDString(%#v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
