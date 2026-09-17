// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"testing"
)

// TestCollectProDataFast_RecordsFailuresWhenEveryCallFails asserts the review's
// ④ acceptance criterion: a dashboard whose collectors could not fetch anything
// must not report success. The fast tier has seven collectors with an error
// return; against an empty mock (every request errors), each must record a miss.
func TestCollectProDataFast_RecordsFailuresWhenEveryCallFails(t *testing.T) {
	mock := &overviewMockClient{responses: map[string]overviewMockResponse{}}
	data := &DashboardData{}
	status := &collectStatus{}

	collectProDataFast(context.Background(), mock, data, nil, status)

	if status.failures() == 0 {
		t.Fatal("expected collectProDataFast to record failures against an empty mock, got 0")
	}
	// EnvStats and Checkin never return an error (inner sub-failures are only
	// logged), so they stay populated even on total failure; the collectors
	// with an error return must leave their section nil when their sole call
	// errored.
	if data.Fleet != nil {
		t.Error("Fleet should be nil when its fetch failed")
	}
	if data.Security != nil {
		t.Error("Security should be nil when its fetch failed")
	}
}

// TestCollectProDataFast_NoFailuresWhenEveryCallSucceeds is the mirror: against a
// fully-populated mock, the fast tier records zero misses and populates the
// sections it collected.
func TestCollectProDataFast_NoFailuresWhenEveryCallSucceeds(t *testing.T) {
	data := &DashboardData{}
	status := &collectStatus{}

	collectProDataFast(context.Background(), buildDashboardFastMock(), data, nil, status)

	if got := status.failures(); got != 0 {
		t.Fatalf("expected 0 failures against a full mock, got %d", got)
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
}

// TestCollectStatus_RecordFailure documents the contract at the collectStatus
// boundary: recordFailure moves failures() above zero, which is the exact
// condition runDashboard uses to return exitcode.PartialFailure.
func TestCollectStatus_RecordFailure(t *testing.T) {
	status := &collectStatus{}
	if status.failures() != 0 {
		t.Fatal("a fresh collectStatus must start at zero failures")
	}
	status.recordFailure()
	status.recordFailure()
	if got := status.failures(); got != 2 {
		t.Fatalf("failures() = %d, want 2", got)
	}
}

// buildDashboardFastMock returns a mock covering every endpoint the fast-tier
// collectors hit, so collectProDataFast completes with zero recorded failures.
func buildDashboardFastMock() *overviewMockClient {
	return &overviewMockClient{
		responses: map[string]overviewMockResponse{
			// Fleet counts.
			"/v1/inventory-information": {200, `{"managedComputers":500,"unmanagedComputers":10,"managedDevices":200,"unmanagedDevices":5}`},
			"/v1/users":                 {200, `{"totalCount":42,"results":[]}`},

			// Security posture + OS distribution (paginated computers-inventory).
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
		},
	}
}
