// Copyright 2026, Jamf Software LLC

package resolve

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
)

// mobileDetailClient serves /v2/mobile-devices/detail in the shape the server
// sends: the id as mobileDeviceId, identity under general, the serial under
// hardware, and "hardware": null unless section=HARDWARE is requested.
type mobileDetailClient struct {
	calls []string
}

func (m *mobileDetailClient) Do(_ context.Context, _, path string, _ io.Reader) (*http.Response, error) {
	m.calls = append(m.calls, path)
	u, err := url.Parse(path)
	if err != nil || u.Path != "/v2/mobile-devices/detail" {
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	q := u.Query()
	record := map[string]any{
		"mobileDeviceId": "10",
		"general": map[string]any{
			"displayName":  "Cart iPad",
			"udid":         "00008030-000A",
			"managementId": "mgmt-10",
		},
		"hardware": nil,
	}
	if slices.Contains(q["section"], "HARDWARE") {
		record["hardware"] = map[string]any{"serialNumber": "VGP6T0HP95"}
	}
	results := []any{}
	if f := q.Get("filter"); strings.Contains(f, "VGP6T0HP95") || strings.Contains(f, "Cart iPad") || strings.Contains(f, "(10)") {
		results = append(results, record)
	}
	body, _ := json.Marshal(map[string]any{"totalCount": len(results), "results": results})
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
}

func TestResolveMobileDevice_BySerialAgainstTheDetailShape(t *testing.T) {
	client := &mobileDetailClient{}
	d, err := ResolveMobileDevice(context.Background(), client, "VGP6T0HP95", "", "")
	if err != nil {
		t.Fatalf("serial lookup: %v", err)
	}
	want := DeviceIdentifiers{ID: "10", ManagementID: "mgmt-10", UDID: "00008030-000A", Name: "Cart iPad", SerialNumber: "VGP6T0HP95"}
	if *d != want {
		t.Errorf("resolved %+v, want %+v", *d, want)
	}

	d, err = ResolveMobileDevice(context.Background(), client, "", "Cart iPad", "")
	if err != nil {
		t.Fatalf("name lookup: %v", err)
	}
	if d.UDID != "00008030-000A" || d.SerialNumber != "VGP6T0HP95" {
		t.Errorf("name lookup resolved %+v, want the general.udid and hardware.serialNumber", *d)
	}
}

func TestResolveMobileDevicesFromFile_SerialAgainstTheDetailShape(t *testing.T) {
	path := writeEntriesFile(t, "VGP6T0HP95\n10\n")
	client := &mobileDetailClient{}
	results, skipped, err := ResolveMobileDevicesFromFile(context.Background(), client, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 || skipped != 0 {
		t.Fatalf("got %d results / %d skipped, want 2 / 0 (calls %v)", len(results), skipped, client.calls)
	}
	for _, d := range results {
		if d.SerialNumber != "VGP6T0HP95" || d.UDID != "00008030-000A" {
			t.Errorf("resolved %+v, want the nested serial and udid", *d)
		}
	}
}
