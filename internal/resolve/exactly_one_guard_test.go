// Copyright 2026, Jamf Software LLC

package resolve

import (
	"context"
	"strings"
	"testing"
)

// A wildcard in the requested value makes the server's == match some other
// record; each case answers with a record whose field differs from the request.
func TestResolveDevice_ReadBackRefusesARecordThatDoesNotCarryTheRequestedValue(t *testing.T) {
	computer := `{"totalCount":1,"results":[{"id":"42","udid":"AAAA-BBBB","general":{"name":"Lab-1","managementId":"mgmt-1"},"hardware":{"serialNumber":"C02X1234"}}]}`
	mobile := `{"totalCount":1,"results":[{"mobileDeviceId":"99","general":{"displayName":"iPad-1","udid":"MOBILE-UDID","managementId":"mgmt-m"},"hardware":{"serialNumber":"F4GH5678"}}]}`
	computerClient := func() *mockClient {
		return &mockClient{responses: map[string]mockResponse{"/v4/computers-inventory?": {200, computer}}}
	}
	mobileClient := func() *mockClient {
		return &mockClient{responses: map[string]mockResponse{"/v2/mobile-devices/detail?": {200, mobile}}}
	}
	ctx := context.Background()

	for _, tc := range []struct {
		name    string
		resolve func() (*DeviceIdentifiers, error)
	}{
		{"computer serial", func() (*DeviceIdentifiers, error) { return ResolveComputer(ctx, computerClient(), "C02*", "", "") }},
		{"computer name", func() (*DeviceIdentifiers, error) { return ResolveComputer(ctx, computerClient(), "", "Lab*", "") }},
		{"computer UDID", func() (*DeviceIdentifiers, error) { return ResolveComputerByUDID(ctx, computerClient(), "AAAA*") }},
		{"computer management ID", func() (*DeviceIdentifiers, error) {
			return ResolveComputerByManagementID(ctx, computerClient(), "mgmt*")
		}},
		{"mobile serial", func() (*DeviceIdentifiers, error) { return ResolveMobileDevice(ctx, mobileClient(), "F4GH*", "", "") }},
		{"mobile name", func() (*DeviceIdentifiers, error) { return ResolveMobileDevice(ctx, mobileClient(), "", "iPad*", "") }},
	} {
		d, err := tc.resolve()
		if err == nil || !strings.Contains(err.Error(), "the server returned") {
			t.Errorf("%s: got %+v, %v; want a refusal naming the record the server returned", tc.name, d, err)
		}
		if d != nil {
			t.Errorf("%s: returned %+v; a refused lookup must return no device to act on", tc.name, d)
		}
	}
}

func TestResolveMobileDevice_MultipleMatches(t *testing.T) {
	two := `{"totalCount":2,"results":[` +
		`{"mobileDeviceId":"99","general":{"displayName":"Lab iPad","udid":"U1","managementId":"M1"},"hardware":{"serialNumber":"F4GH5678"}},` +
		`{"mobileDeviceId":"100","general":{"displayName":"Lab iPad","udid":"U2","managementId":"M2"},"hardware":{"serialNumber":"F4GH9999"}}]}`
	client := &mockClient{responses: map[string]mockResponse{"/v2/mobile-devices/detail?": {200, two}}}

	d, err := ResolveMobileDevice(context.Background(), client, "", "Lab iPad", "")
	if err == nil || !strings.Contains(err.Error(), "multiple mobile devices found") {
		t.Fatalf("got %+v, %v; want a refusal of the two matches", d, err)
	}
	if d != nil {
		t.Errorf("returned %+v; an ambiguous lookup must return no device to act on", d)
	}
}

func TestResolveComputerGroup_ReadBackRefusesAWildcardMatch(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		"/v3/computer-groups/smart-groups?": {200, `{"totalCount":1,"results":[{"id":"9","name":"Labs-1"}]}`},
		"GET /JSSResource/computergroups":   {200, `{"computer_groups":[]}`},
	}}
	_, err := ResolveComputerGroup(context.Background(), client, "Lab*")
	if err == nil {
		t.Fatal("--group 'Lab*' resolved to the smart group named Labs-1")
	}
	if hits := fetchedPaths(client.calls, "smart-group-membership/9"); len(hits) > 0 {
		t.Errorf("fetched the membership of a group the read-back should have refused: %v", hits)
	}
}
