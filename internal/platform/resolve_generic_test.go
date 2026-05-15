// Copyright 2026, Jamf Software LLC

package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
)

const resolveTestTenantID = "resolve-tenant"

func newResolveTestClient(t *testing.T, mux *http.ServeMux) *jamfplatform.Client {
	t.Helper()
	mux.HandleFunc("/auth/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return jamfplatform.NewClient(
		srv.URL,
		"test-id",
		"test-secret",
		jamfplatform.WithTenantID(resolveTestTenantID),
	)
}

// TestResolveIDByName_NonPaginated verifies that the first request to a
// non-paginated endpoint (e.g. compliance-benchmarks) does not include
// page/page-size params. This was the root cause of 500s on benchmarks.
func TestResolveIDByName_NonPaginated(t *testing.T) {
	mux := http.NewServeMux()
	var capturedQuery string
	path := "/api/benchmarks/v1/tenant/" + resolveTestTenantID + "/benchmarks"
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"benchmarks": []any{
				map[string]any{"id": "bm-abc", "title": "CIS v8"},
				map[string]any{"id": "bm-def", "title": "STIG macOS"},
			},
		})
	})
	client := newResolveTestClient(t, mux)

	id, err := ResolveIDByName(context.Background(), client, path, "CIS v8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "bm-abc" {
		t.Errorf("id = %q, want %q", id, "bm-abc")
	}
	q, _ := url.ParseQuery(capturedQuery)
	if q.Get("page") != "" || q.Get("page-size") != "" {
		t.Errorf("first request must not include pagination params; got query %q", capturedQuery)
	}
}

// TestResolveIDByName_MatchesByTitle verifies matching on "title" field,
// which is used by benchmarks (not "name").
func TestResolveIDByName_MatchesByTitle(t *testing.T) {
	mux := http.NewServeMux()
	path := "/api/some/v1/tenant/" + resolveTestTenantID + "/items"
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []any{
				map[string]any{"id": "t-1", "title": "The One"},
				map[string]any{"id": "t-2", "title": "Other"},
			},
			"totalCount": 2,
		})
	})
	client := newResolveTestClient(t, mux)

	id, err := ResolveIDByName(context.Background(), client, path, "The One")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "t-1" {
		t.Errorf("id = %q, want %q", id, "t-1")
	}
}

// TestResolveIDByName_PaginatedMultiPage verifies that when the first page is
// full (100 items + totalCount present), subsequent pages are fetched with
// page=1, page=2, ... until the target is found.
func TestResolveIDByName_PaginatedMultiPage(t *testing.T) {
	const pageSize = 100
	mux := http.NewServeMux()
	path := "/api/res/v1/tenant/" + resolveTestTenantID + "/items"

	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		pageParam := r.URL.Query().Get("page")

		var results []any
		if pageParam == "" {
			// First request (no pagination params): return a full page of filler.
			for i := range pageSize {
				results = append(results, map[string]any{
					"id":   fmt.Sprintf("id-%d", i),
					"name": fmt.Sprintf("item-%d", i),
				})
			}
		} else {
			// Subsequent pages: return the target on page 1.
			results = []any{
				map[string]any{"id": "page2-target", "name": "Target Item"},
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results":    results,
			"totalCount": pageSize + 1,
		})
	})
	client := newResolveTestClient(t, mux)

	id, err := ResolveIDByName(context.Background(), client, path, "Target Item")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "page2-target" {
		t.Errorf("id = %q, want %q", id, "page2-target")
	}
}

// TestResolveIDByName_NotFound verifies ErrNotFound when no item matches.
func TestResolveIDByName_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	path := "/api/res/v1/tenant/" + resolveTestTenantID + "/items"
	mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results":    []any{map[string]any{"id": "id-1", "name": "Exists"}},
			"totalCount": 1,
		})
	})
	client := newResolveTestClient(t, mux)

	_, err := ResolveIDByName(context.Background(), client, path, "Missing")
	if !IsNotFound(err) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// TestResolveIDByName_Ambiguous verifies that when two items share the same
// name on the same page, an error is returned rather than silently picking one.
func TestResolveIDByName_Ambiguous(t *testing.T) {
	mux := http.NewServeMux()
	path := "/api/device-groups/v1/tenant/" + resolveTestTenantID + "/device-groups"
	mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []any{
				map[string]any{"id": "dg-computer", "name": "All Managed", "deviceType": "COMPUTER"},
				map[string]any{"id": "dg-mobile", "name": "All Managed", "deviceType": "MOBILE"},
			},
			"totalCount": 2,
		})
	})
	client := newResolveTestClient(t, mux)

	_, err := ResolveIDByName(context.Background(), client, path, "All Managed")
	if err == nil {
		t.Fatal("expected ambiguous error, got nil")
	}
	if IsNotFound(err) {
		t.Errorf("should not be ErrNotFound, got %v", err)
	}
}

// TestResolveIDByNameFiltered verifies that the filter expression is appended
// as a query param and narrows the result to the matching device type.
func TestResolveIDByNameFiltered(t *testing.T) {
	mux := http.NewServeMux()
	path := "/api/device-groups/v1/tenant/" + resolveTestTenantID + "/device-groups"
	var capturedFilter string
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		capturedFilter = r.URL.Query().Get("filter")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []any{
				map[string]any{"id": "dg-computer", "name": "All Managed", "deviceType": "COMPUTER"},
			},
			"totalCount": 1,
		})
	})
	client := newResolveTestClient(t, mux)

	id, err := ResolveIDByNameFiltered(context.Background(), client, path, "All Managed", `deviceType=="COMPUTER"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "dg-computer" {
		t.Errorf("id = %q, want %q", id, "dg-computer")
	}
	if capturedFilter != `deviceType=="COMPUTER"` {
		t.Errorf("filter = %q, want %q", capturedFilter, `deviceType=="COMPUTER"`)
	}
}

// TestResolveIDByName_BareArray verifies matching against bare-array responses.
func TestResolveIDByName_BareArray(t *testing.T) {
	mux := http.NewServeMux()
	path := "/api/res/v1/tenant/" + resolveTestTenantID + "/items"
	mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"id": "arr-1", "name": "Array Item"},
		})
	})
	client := newResolveTestClient(t, mux)

	id, err := ResolveIDByName(context.Background(), client, path, "Array Item")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "arr-1" {
		t.Errorf("id = %q, want %q", id, "arr-1")
	}
}
