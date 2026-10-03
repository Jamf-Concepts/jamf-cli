// Copyright 2026, Jamf Software LLC

package resolve

import (
	"context"
	"strings"
	"testing"
)

// A smart-group search the server answers 404 is read as "no smart group",
// so the static-group lookup runs.
func TestResolveMobileDeviceGroup_SmartSearch404FallsBackToStatic(t *testing.T) {
	list := `{"mobile_device_groups":[{"id":"7","name":"Carts"}]}`
	detail := `{"mobile_device_group":{"id":"7","name":"Carts","mobile_devices":[]}}`
	client := &mockClient{responses: map[string]mockResponse{
		"/v2/mobile-device-groups/smart-groups?": {404, `{"httpStatus":404}`},
		"GET /JSSResource/mobiledevicegroups":    {200, list},
		"/JSSResource/mobiledevicegroups/id/7":   {200, detail},
	}}
	if _, err := ResolveMobileDeviceGroup(context.Background(), client, "Carts"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hits := fetchedPaths(client.calls, "/JSSResource/mobiledevicegroups/id/7"); len(hits) == 0 {
		t.Errorf("a smart-group 404 must fall back to the static group; calls: %v", client.calls)
	}
}

// Only a 404 means "no smart group"; a refused search must not be read as
// absence and fall through to the static lookup.
func TestResolveComputerGroup_SmartSearch403DoesNotFallBack(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		"/v3/computer-groups/smart-groups?": {403, `{"httpStatus":403}`},
		"GET /JSSResource/computergroups":   {200, `{"computer_groups":[{"id":"5","name":"All Macs"}]}`},
	}}
	_, err := ResolveComputerGroup(context.Background(), client, "All Macs")
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Errorf("err = %v, want the smart-group search's 403", err)
	}
	if hits := fetchedPaths(client.calls, "/JSSResource/computergroups"); len(hits) > 0 {
		t.Errorf("a 403 on the smart-group search fell back to the static lookup: %v", hits)
	}
}
