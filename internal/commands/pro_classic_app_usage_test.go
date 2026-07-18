// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func TestParseLastWindow(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		last      string
		wantStart string
		wantEnd   string
		wantErr   bool
	}{
		{"30 days", "30d", "2026-06-17", "2026-07-17", false},
		{"2 weeks", "2w", "2026-07-03", "2026-07-17", false},
		{"1 day", "1d", "2026-07-16", "2026-07-17", false},
		{"empty", "", "", "", true},
		{"zero", "0d", "", "", true},
		{"negative", "-5d", "", "", true},
		{"bad number", "xd", "", "", true},
		{"bad suffix", "30y", "", "", true},
		{"no suffix", "30", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := parseLastWindow(tt.last, now)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got none", tt.last)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("parseLastWindow(%q) = (%q, %q), want (%q, %q)", tt.last, start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestFlattenAppUsage(t *testing.T) {
	data := map[string]any{
		"computer_application_usage": []any{
			map[string]any{
				"date": "2026-06-01",
				"apps": []any{
					map[string]any{"name": "Safari", "version": "17.0", "foreground": float64(42), "open": float64(3)},
					map[string]any{"name": "Mail", "version": "16.0", "foreground": float64(10), "open": float64(1)},
				},
			},
			map[string]any{
				"date": "2026-06-02",
				"apps": []any{
					map[string]any{"name": "Safari", "version": "17.0", "foreground": float64(55), "open": float64(4)},
				},
			},
		},
	}

	rows := flattenAppUsage(data)
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if rows[0]["date"] != "2026-06-01" || rows[0]["name"] != "Safari" {
		t.Errorf("row 0 = %+v", rows[0])
	}
	if rows[0]["foreground"] != float64(42) || rows[0]["open"] != float64(3) {
		t.Errorf("row 0 usage fields = %+v", rows[0])
	}
	if rows[2]["date"] != "2026-06-02" || rows[2]["name"] != "Safari" {
		t.Errorf("row 2 = %+v", rows[2])
	}
}

func TestFlattenAppUsage_Empty(t *testing.T) {
	rows := flattenAppUsage(map[string]any{"computer_application_usage": []any{}})
	if rows == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(rows))
	}

	rows = flattenAppUsage(map[string]any{})
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows for missing key, got %d", len(rows))
	}
}

func TestResolveAppUsageRange(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name             string
		start, end, last string
		wantStart        string
		wantEnd          string
		wantErr          bool
	}{
		{"explicit range", "2026-06-01", "2026-06-30", "", "2026-06-01", "2026-06-30", false},
		{"last window", "", "", "30d", "2026-06-17", "2026-07-17", false},
		{"both forms", "2026-06-01", "2026-06-30", "30d", "", "", true},
		{"neither form", "", "", "", "", "", true},
		{"start without end", "2026-06-01", "", "", "", "", true},
		{"end without start", "", "2026-06-30", "", "", "", true},
		{"bad start format", "06/01/2026", "2026-06-30", "", "", "", true},
		{"bad end format", "2026-06-01", "2026-13-99", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := resolveAppUsageRange(tt.start, tt.end, tt.last, now)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got none (start=%q end=%q)", start, end)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("got (%q, %q), want (%q, %q)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// appUsageMockClient implements registry.HTTPClient, returning a canned body
// for the app-usage path and a canned id-lookup for identifier resolution.
type appUsageMockClient struct {
	responses  map[string]string // path (query stripped) -> JSON body
	lastAccept string            // Accept header captured from context on most recent Do call
}

func (m *appUsageMockClient) Do(ctx context.Context, _, path string, _ io.Reader) (*http.Response, error) {
	m.lastAccept = registry.AcceptFromContext(ctx)
	key := path
	if before, _, ok := strings.Cut(path, "?"); ok {
		key = before
	}
	body, ok := m.responses[key]
	if !ok {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
}

func TestResolveAppUsageComputerID_UDID(t *testing.T) {
	// resolveComputerByFilter hits /v3/computers-inventory (query stripped by mock).
	// The inventory response must contain id and udid at the top level plus a
	// general section (required by parseComputerInventory).
	inventoryResp := `{
		"totalCount": 1,
		"results": [{
			"id": "42",
			"udid": "AABBCCDD-1122-3344-5566-778899AABBCC",
			"general": {"name": "Test Mac", "managementId": "mgmt-uuid"},
			"hardware": {"serialNumber": "C02XL0ABCDEF"}
		}]
	}`

	client := &appUsageMockClient{
		responses: map[string]string{
			"/v3/computers-inventory": inventoryResp,
		},
	}

	resolvedID, err := resolveAppUsageComputerID(
		context.Background(), client,
		"", "", "AABBCCDD-1122-3344-5566-778899AABBCC", "",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolvedID != "42" {
		t.Errorf("expected id %q, got %q", "42", resolvedID)
	}
}

func TestResolveAppUsageComputerID_IDDirect(t *testing.T) {
	// --id must be returned without any network call; pass a client with no
	// routes so any HTTP call would return 404.
	client := &appUsageMockClient{responses: map[string]string{}}

	resolvedID, err := resolveAppUsageComputerID(context.Background(), client, "99", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolvedID != "99" {
		t.Errorf("expected id %q, got %q", "99", resolvedID)
	}
}

func TestFetchAppUsageRows_ByID(t *testing.T) {
	client := &appUsageMockClient{
		responses: map[string]string{
			"/JSSResource/computerapplicationusage/id/42/2026-06-01_2026-06-02": `{
				"computer_application_usage": [
					{"date": "2026-06-01", "apps": [
						{"name": "Safari", "version": "17.0", "foreground": 42, "open": 3}
					]}
				]
			}`,
		},
	}

	rows, err := fetchAppUsageRows(context.Background(), client, "42", "2026-06-01", "2026-06-02")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0]["name"] != "Safari" || rows[0]["date"] != "2026-06-01" {
		t.Errorf("row = %+v", rows[0])
	}
}

func TestFetchAppUsageRows_NegotiatesJSON(t *testing.T) {
	client := &appUsageMockClient{
		responses: map[string]string{
			"/JSSResource/computerapplicationusage/id/42/2026-06-01_2026-06-02": `{
				"computer_application_usage": [
					{"date": "2026-06-01", "apps": [
						{"name": "Safari", "version": "17.0", "foreground": 42, "open": 3}
					]}
				]
			}`,
		},
	}

	_, err := fetchAppUsageRows(context.Background(), client, "42", "2026-06-01", "2026-06-02")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.lastAccept != "application/json" {
		t.Errorf("expected Accept %q, got %q", "application/json", client.lastAccept)
	}
}

func TestFetchAppUsageRows_404Error(t *testing.T) {
	// Mock with no routes so every call returns 404.
	client := &appUsageMockClient{responses: map[string]string{}}

	_, err := fetchAppUsageRows(context.Background(), client, "99", "2026-06-01", "2026-06-02")
	if err == nil {
		t.Fatal("expected error for 404, got none")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error message, got: %v", err)
	}
}

func TestAppUsageOutputFormats(t *testing.T) {
	rows := flattenAppUsage(map[string]any{
		"computer_application_usage": []any{
			map[string]any{"date": "2026-06-01", "apps": []any{
				map[string]any{"name": "Safari", "version": "17.0", "foreground": float64(42), "open": float64(3)},
			}},
		},
	})

	for _, format := range []string{"table", "json", "yaml", "csv", "plain"} {
		t.Run(format, func(t *testing.T) {
			f := output.New(format, true, false) // noColor=true
			var buf bytes.Buffer
			f.SetWriter(&buf)
			if err := f.Print(rows); err != nil {
				t.Fatalf("Print(%s) error: %v", format, err)
			}
			out := buf.String()
			if !strings.Contains(out, "Safari") {
				t.Errorf("format %s missing expected content; got:\n%s", format, out)
			}
		})
	}
}

func TestFlattenAppUsage_SingleObject(t *testing.T) {
	// Classic API may collapse a single-element array to a plain object.
	// asSlice must handle both the top-level list and the per-day apps list.
	data := map[string]any{
		"computer_application_usage": map[string]any{
			"date": "2026-06-01",
			"apps": map[string]any{"name": "Safari", "version": "17.0", "foreground": float64(42), "open": float64(3)},
		},
	}

	rows := flattenAppUsage(data)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0]["name"] != "Safari" {
		t.Errorf("expected name %q, got %q", "Safari", rows[0]["name"])
	}
	if rows[0]["date"] != "2026-06-01" {
		t.Errorf("expected date %q, got %q", "2026-06-01", rows[0]["date"])
	}
}

func TestResolveAppUsageComputerID_Serial(t *testing.T) {
	// resolveDeviceByIdentifier tries the id-detail path first (/v3/computers-inventory-detail/<val>).
	// For a serial value that path returns 404 (no route in mock), so it falls back to the
	// serial RSQL filter on /v3/computers-inventory. The mock strips query strings, so the
	// inventory path matches regardless of filter params.
	inventoryResp := `{
		"totalCount": 1,
		"results": [{
			"id": "55",
			"udid": "AABBCCDD-0000-0000-0000-000000000001",
			"general": {"name": "Mac Mini", "managementId": "mgmt-uuid-2"},
			"hardware": {"serialNumber": "C02XL0SERIAL"}
		}]
	}`

	client := &appUsageMockClient{
		responses: map[string]string{
			// No route for /v3/computers-inventory-detail/C02XL0SERIAL → 404, triggers fallback.
			"/v3/computers-inventory": inventoryResp,
		},
	}

	resolvedID, err := resolveAppUsageComputerID(
		context.Background(), client,
		"", "C02XL0SERIAL", "", "",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolvedID != "55" {
		t.Errorf("expected id %q, got %q", "55", resolvedID)
	}
}
