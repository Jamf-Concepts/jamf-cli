// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// --- SlugifyName ---

func TestSlugifyName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Deploy Chrome - v1.2", "deploy-chrome-v1-2"},
		{"simple", "simple"},
		{"CamelCase Name", "camelcase-name"},
		{"  spaces  ", "spaces"},
		{"special!@#$chars", "special-chars"},
		{"unicode-café", "unicode-caf"},
		{"---leading-trailing---", "leading-trailing"},
		{"", "unnamed"},
		{"   ", "unnamed"},
		{"ALLCAPS", "allcaps"},
		{"dots.and.more.dots", "dots-and-more-dots"},
		{"slashes/and\\backslashes", "slashes-and-backslashes"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := SlugifyName(tt.input)
			if got != tt.want {
				t.Errorf("SlugifyName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- StripServerFields ---

func TestStripServerFields(t *testing.T) {
	obj := map[string]any{
		"id":          float64(42),
		"name":        "Test Policy",
		"enabled":     true,
		"createdDate": "2026-01-01",
		"updatedDate": "2026-03-01",
		"href":        "/api/v1/policies/42",
		"scope": map[string]any{
			"id":   float64(1),
			"name": "All Computers",
		},
	}

	stripped := StripServerFields(obj)

	// Should keep name and enabled
	if stripped["name"] != "Test Policy" {
		t.Errorf("name should be preserved, got %v", stripped["name"])
	}
	if stripped["enabled"] != true {
		t.Errorf("enabled should be preserved, got %v", stripped["enabled"])
	}

	// Should strip server fields
	for _, k := range []string{"id", "createdDate", "updatedDate", "href"} {
		if _, ok := stripped[k]; ok {
			t.Errorf("expected %q to be stripped", k)
		}
	}

	// Should strip nested id
	scope, ok := stripped["scope"].(map[string]any)
	if !ok {
		t.Fatal("scope should be a nested map")
		return
	}
	if _, ok := scope["id"]; ok {
		t.Error("nested id should be stripped")
	}
	if scope["name"] != "All Computers" {
		t.Errorf("nested name should be preserved, got %v", scope["name"])
	}
}

func TestStripServerFields_Arrays(t *testing.T) {
	obj := map[string]any{
		"name": "Test Policy",
		"scope": map[string]any{
			"computers": []any{
				map[string]any{"id": float64(1), "name": "Mac1", "href": "/api/v1/computers/1"},
				map[string]any{"id": float64(2), "name": "Mac2"},
			},
		},
	}

	stripped := StripServerFields(obj)
	scope := stripped["scope"].(map[string]any)
	computers := scope["computers"].([]any)
	if len(computers) != 2 {
		t.Fatalf("expected 2 computers, got %d", len(computers))
	}
	comp := computers[0].(map[string]any)
	if _, ok := comp["id"]; ok {
		t.Error("id should be stripped from array elements")
	}
	if _, ok := comp["href"]; ok {
		t.Error("href should be stripped from array elements")
	}
	if comp["name"] != "Mac1" {
		t.Errorf("name should be preserved in array elements, got %v", comp["name"])
	}
}

func TestStripServerFields_TimestampSuffixes(t *testing.T) {
	obj := map[string]any{
		"name":                "Test",
		"last_modified_epoch": float64(1234567890),
		"some_field_utc":      "2026-01-01T00:00:00Z",
		"normal_field":        "keep",
	}

	stripped := StripServerFields(obj)
	if _, ok := stripped["last_modified_epoch"]; ok {
		t.Error("_epoch suffix key should be stripped")
	}
	if _, ok := stripped["some_field_utc"]; ok {
		t.Error("_utc suffix key should be stripped")
	}
	if stripped["normal_field"] != "keep" {
		t.Error("normal fields should be preserved")
	}
}

func TestStripServerFields_EmptyMap(t *testing.T) {
	result := StripServerFields(map[string]any{})
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

// --- DeduplicateSlug ---

func TestDeduplicateSlug(t *testing.T) {
	seen := map[string]bool{}

	// First use: no dedup needed
	s1 := DeduplicateSlug("deploy-chrome", seen)
	if s1 != "deploy-chrome" {
		t.Errorf("first slug = %q, want %q", s1, "deploy-chrome")
	}

	// Second use: should append -2
	s2 := DeduplicateSlug("deploy-chrome", seen)
	if s2 != "deploy-chrome-2" {
		t.Errorf("second slug = %q, want %q", s2, "deploy-chrome-2")
	}

	// Third use: should append -3
	s3 := DeduplicateSlug("deploy-chrome", seen)
	if s3 != "deploy-chrome-3" {
		t.Errorf("third slug = %q, want %q", s3, "deploy-chrome-3")
	}

	// Different name: no dedup
	s4 := DeduplicateSlug("install-firefox", seen)
	if s4 != "install-firefox" {
		t.Errorf("different name = %q, want %q", s4, "install-firefox")
	}
}

// --- FetchAllPaginated ---

type paginatedMockClient struct {
	pages map[string]string // path -> response body
}

func (m *paginatedMockClient) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	if body, ok := m.pages[path]; ok {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	}
	return nil, fmt.Errorf("no mock for %s", path)
}

func TestFetchAllPaginated(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v1/scripts?page=0&page-size=2": `{"totalCount":3,"results":[{"id":"1","name":"A"},{"id":"2","name":"B"}]}`,
			"/v1/scripts?page=1&page-size=2": `{"totalCount":3,"results":[{"id":"3","name":"C"}]}`,
		},
	}

	results, err := FetchAllPaginated(context.Background(), client, "/v1/scripts", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("got %d results, want 3", len(results))
	}
}

func TestFetchAllPaginated_Empty(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v1/scripts?page=0&page-size=100": `{"totalCount":0,"results":[]}`,
		},
	}

	results, err := FetchAllPaginated(context.Background(), client, "/v1/scripts", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}

func TestFetchAllPaginated_ArrayResponse(t *testing.T) {
	// Endpoints like /v1/computer-groups, /v1/sites return plain arrays
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v1/sites?page=0&page-size=100": `[{"id":"1","name":"Main"},{"id":"2","name":"Branch"},{"id":"3","name":"Remote"}]`,
		},
	}

	results, err := FetchAllPaginated(context.Background(), client, "/v1/sites", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("got %d results, want 3", len(results))
	}
	if results[0]["name"] != "Main" {
		t.Errorf("first result name = %v, want Main", results[0]["name"])
	}
}

func TestFetchAllPaginated_EmptyArray(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v1/computer-groups?page=0&page-size=100": `[]`,
		},
	}

	results, err := FetchAllPaginated(context.Background(), client, "/v1/computer-groups", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}

func TestFetchAllPaginated_ArrayWithPaginationParams(t *testing.T) {
	// Even when we send ?page=0&page-size=2, array endpoints return everything
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v2/patch-software-title-configurations?page=0&page-size=2": `[{"id":"1","displayName":"Chrome"},{"id":"2","displayName":"Firefox"}]`,
		},
	}

	results, err := FetchAllPaginated(context.Background(), client, "/v2/patch-software-title-configurations", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("got %d results, want 2", len(results))
	}
}

func TestFetchAllPaginated_HTTPError(t *testing.T) {
	// Mock returns a non-200 status
	client := &paginatedMockClient{
		pages: map[string]string{}, // no pages → paginatedMockClient returns error
	}

	_, err := FetchAllPaginated(context.Background(), client, "/v1/scripts", 100)
	if err == nil {
		t.Fatal("expected error for missing mock")
		return
	}
}

func TestFetchAllPaginated_HTTP403(t *testing.T) {
	// Use overviewMockClient which can return non-200 status codes
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/categories": {403, `{"httpStatus":403}`},
		},
	}

	_, err := FetchAllPaginated(context.Background(), client, "/v1/categories", 100)
	if err == nil {
		t.Fatal("expected error for HTTP 403")
		return
	}
	if !strings.Contains(err.Error(), "HTTP 403") {
		t.Errorf("error = %q, want to contain 'HTTP 403'", err.Error())
	}
}

// --- FetchClassicList ---

func TestFetchClassicList(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/policies": {200, `{"policies":[{"id":1,"name":"P1"},{"id":2,"name":"P2"}]}`},
		},
	}

	items, err := FetchClassicList(context.Background(), client, "/JSSResource/policies", "policies")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("got %d items, want 2", len(items))
	}
}

func TestFetchClassicList_MissingKey(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/policies": {200, `{"other_key":[{"id":1}]}`},
		},
	}

	items, err := FetchClassicList(context.Background(), client, "/JSSResource/policies", "policies")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if items != nil {
		t.Errorf("expected nil for missing key, got %v", items)
	}
}

func TestFetchClassicList_HTTPError(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/policies": {403, `{}`},
		},
	}

	_, err := FetchClassicList(context.Background(), client, "/JSSResource/policies", "policies")
	if err == nil {
		t.Fatal("expected error for HTTP 403")
		return
	}
}

// --- BoundedParallelFetch ---

func TestBoundedParallelFetch(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	results, errs := BoundedParallelFetch(context.Background(), items, 2, func(_ context.Context, n int) (int, error) {
		return n * 10, nil
	})

	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
	if len(results) != 5 {
		t.Fatalf("got %d results, want 5", len(results))
	}

	// Results should contain all multiplied values (order may vary)
	sum := 0
	for _, r := range results {
		sum += r
	}
	if sum != 150 {
		t.Errorf("sum = %d, want 150 (10+20+30+40+50)", sum)
	}
}

func TestBoundedParallelFetch_PartialErrors(t *testing.T) {
	items := []string{"ok", "fail", "ok2"}
	results, errs := BoundedParallelFetch(context.Background(), items, 10, func(_ context.Context, s string) (string, error) {
		if s == "fail" {
			return "", fmt.Errorf("simulated failure")
		}
		return s + "-done", nil
	})

	if len(errs) != 1 {
		t.Errorf("expected 1 error, got %d", len(errs))
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestBoundedParallelFetch_Empty(t *testing.T) {
	results, errs := BoundedParallelFetch(context.Background(), []int{}, 5, func(_ context.Context, n int) (int, error) {
		return n, nil
	})

	if len(results) != 0 {
		t.Errorf("expected no results, got %d", len(results))
	}
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %d", len(errs))
	}
}

// --- reportSampleSize ---

func TestReportSampleSize(t *testing.T) {
	tests := []struct {
		name  string
		total int
		limit int
		want  int
	}{
		// Smart default (limit < 0): max(10%, 100), clamped to total
		{"smart default large fleet", 2000, -1, 200},
		{"smart default small fleet", 50, -1, 50}, // 10%=5 < 100, but 100 > 50, so clamp to 50
		{"smart default 1000", 1000, -1, 100},     // 10%=100 == 100
		{"smart default 500", 500, -1, 100},       // 10%=50 < 100
		{"smart default 1001", 1001, -1, 100},     // 10%=100 rounds down

		// Explicit limit (limit > 0)
		{"explicit limit under total", 500, 50, 50},
		{"explicit limit over total", 50, 500, 50},
		{"explicit limit equals total", 100, 100, 100},

		// No limit (limit == 0): all
		{"no limit", 1234, 0, 1234},
		{"no limit zero total", 0, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reportSampleSize(tt.total, tt.limit)
			if got != tt.want {
				t.Errorf("reportSampleSize(%d, %d) = %d, want %d", tt.total, tt.limit, got, tt.want)
			}
		})
	}
}
