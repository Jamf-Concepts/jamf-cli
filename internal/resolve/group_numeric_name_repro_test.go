// Copyright 2026, Jamf Software LLC

package resolve

import (
	"context"
	"strings"
	"testing"
)

func classicComputerGroupList(groups ...[2]string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><computer_groups><size>2</size>`)
	for _, g := range groups {
		b.WriteString(`<computer_group><id>` + g[0] + `</id><name>` + g[1] + `</name><is_smart>false</is_smart></computer_group>`)
	}
	b.WriteString(`</computer_groups>`)
	return b.String()
}

func classicComputerGroupDetail(id, name, memberID string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><computer_group><id>` + id + `</id><name>` + name +
		`</name><is_smart>false</is_smart><computers><size>1</size><computer><id>` + memberID +
		`</id><name>Victim</name></computer></computers></computer_group>`
}

func fetchedPaths(calls []string, sub string) []string {
	var hits []string
	for _, c := range calls {
		if strings.Contains(c, sub) {
			hits = append(hits, c)
		}
	}
	return hits
}

// The static-group fallback compares the requested name against a value that
// xmlconv coerced to float64 and jsonString re-rendered with %d, so a request
// for a group that does not exist resolves to a differently named one.
func TestResolveComputerGroup_NumericLookingClassicNameIsNotCoerced(t *testing.T) {
	cases := []struct {
		requested  string
		storedName string
	}{
		{"14", "14.2"},
		{"14", "14.9"},
		{"1000", "1e3"},
		{"7", "7.0"},
		{"7", "+7"},
	}
	for _, tc := range cases {
		t.Run(tc.requested+"_vs_"+tc.storedName, func(t *testing.T) {
			client := &mockClient{responses: map[string]mockResponse{
				"GET /JSSResource/computergroups":   {200, classicComputerGroupList([2]string{"3", "Other"}, [2]string{"7", tc.storedName})},
				"/JSSResource/computergroups/id/7":  {200, classicComputerGroupDetail("7", tc.storedName, "42")},
				"v4/computers-inventory/42":         {200, computerV3DetailResponse},
				"/v3/computer-groups/smart-groups?": {200, `{"totalCount":0,"results":[]}`},
			}}

			devices, err := ResolveComputerGroup(context.Background(), client, tc.requested)
			if err == nil {
				t.Fatalf("--group %q resolved to the group named %q (%d device(s)); want a not-found error",
					tc.requested, tc.storedName, len(devices))
			}
			if hits := fetchedPaths(client.calls, "/JSSResource/computergroups/id/7"); len(hits) > 0 {
				t.Errorf("fetched membership of group %q for request %q: %v", tc.storedName, tc.requested, hits)
			}
		})
	}
}

// A group whose stored name is exactly the requested numeric-looking text must
// still resolve, so a fix that stops coercion cannot regress the honest case.
func TestResolveComputerGroup_ExactNumericNameStillResolves(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		"GET /JSSResource/computergroups":   {200, classicComputerGroupList([2]string{"3", "Other"}, [2]string{"7", "14.2"})},
		"/JSSResource/computergroups/id/7":  {200, classicComputerGroupDetail("7", "14.2", "42")},
		"v4/computers-inventory/42":         {200, computerV3DetailResponse},
		"/v3/computer-groups/smart-groups?": {200, `{"totalCount":0,"results":[]}`},
	}}
	devices, err := ResolveComputerGroup(context.Background(), client, "14.2")
	if err != nil {
		t.Fatalf("exact name 14.2: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("got %d devices, want 1", len(devices))
	}
}

// The smart-group lookup deliberately refuses an ambiguous name; the Classic
// fallback must not then pick one of the candidates.
func TestResolveComputerGroup_SmartAmbiguityIsNotResolvedByFallback(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		"/v3/computer-groups/smart-groups?": {200, `{"totalCount":2,"results":[{"id":"5","name":"Lab"},{"id":"6","name":"Lab"}]}`},
		"GET /JSSResource/computergroups":   {200, classicComputerGroupList([2]string{"5", "Lab"}, [2]string{"6", "Lab"})},
		"/JSSResource/computergroups/id/5":  {200, classicComputerGroupDetail("5", "Lab", "42")},
		"/JSSResource/computergroups/id/6":  {200, classicComputerGroupDetail("6", "Lab", "42")},
		"v4/computers-inventory/42":         {200, computerV3DetailResponse},
	}}
	devices, err := ResolveComputerGroup(context.Background(), client, "Lab")
	if err == nil {
		t.Fatalf("ambiguous smart-group name resolved through the Classic fallback (%d device(s)); want an error", len(devices))
	}
}

// Two Classic groups that differ only in case: the request must not silently
// take whichever the server lists first.
func TestResolveComputerGroup_CaseCollisionIsRefused(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		"GET /JSSResource/computergroups":   {200, classicComputerGroupList([2]string{"9", "Loaners"}, [2]string{"10", "loaners"})},
		"/JSSResource/computergroups/id/9":  {200, classicComputerGroupDetail("9", "Loaners", "42")},
		"/JSSResource/computergroups/id/10": {200, classicComputerGroupDetail("10", "loaners", "42")},
		"v4/computers-inventory/42":         {200, computerV3DetailResponse},
		"/v3/computer-groups/smart-groups?": {200, `{"totalCount":0,"results":[]}`},
	}}
	_, err := ResolveComputerGroup(context.Background(), client, "loaners")
	if err == nil && len(fetchedPaths(client.calls, "/JSSResource/computergroups/id/9")) > 0 {
		t.Fatalf("--group loaners resolved to group 9 (\"Loaners\") although group 10 is named exactly \"loaners\"")
	}
}

func TestResolveMobileDeviceGroup_NumericLookingClassicNameIsNotCoerced(t *testing.T) {
	list := `<?xml version="1.0" encoding="UTF-8"?><mobile_device_groups><size>2</size>` +
		`<mobile_device_group><id>3</id><name>Other</name></mobile_device_group>` +
		`<mobile_device_group><id>7</id><name>101.5</name></mobile_device_group></mobile_device_groups>`
	detail := `<?xml version="1.0" encoding="UTF-8"?><mobile_device_group><id>7</id><name>101.5</name>` +
		`<mobile_devices><size>1</size><mobile_device><id>55</id></mobile_device></mobile_devices></mobile_device_group>`
	client := &mockClient{responses: map[string]mockResponse{
		"GET /JSSResource/mobiledevicegroups":    {200, list},
		"/JSSResource/mobiledevicegroups/id/7":   {200, detail},
		"/v2/mobile-device-groups/smart-groups?": {200, `{"totalCount":0,"results":[]}`},
	}}
	_, _ = ResolveMobileDeviceGroup(context.Background(), client, "101")
	if hits := fetchedPaths(client.calls, "/JSSResource/mobiledevicegroups/id/7"); len(hits) > 0 {
		t.Fatalf("--group 101 fetched membership of the group named \"101.5\": %v", hits)
	}
}
