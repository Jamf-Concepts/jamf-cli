// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/resolve"
)

func TestFilterPrivileges(t *testing.T) {
	tests := []struct {
		name     string
		all      []string
		patterns []string
		include  []string // must appear in result
		exclude  []string // must not appear in result
	}{
		{
			name:     "prefix match includes all matching, excludes others",
			all:      []string{"Read Computers", "Read Scripts", "Create Computers", "Delete Computers"},
			patterns: []string{"Read "},
			include:  []string{"Read Computers", "Read Scripts"},
			exclude:  []string{"Create Computers", "Delete Computers"},
		},
		{
			name:     "exact match only matches that string, not similar ones",
			all:      []string{"blueprints read", "blueprints create", "blueprints delete"},
			patterns: []string{"blueprints read"},
			include:  []string{"blueprints read"},
			exclude:  []string{"blueprints create", "blueprints delete"},
		},
		{
			name:     "multiple prefixes combined",
			all:      []string{"Read A", "View B", "Create C", "Delete D"},
			patterns: []string{"Read ", "View "},
			include:  []string{"Read A", "View B"},
			exclude:  []string{"Create C", "Delete D"},
		},
		{
			name:     "no matching patterns returns nothing",
			all:      []string{"Read Computers", "Create Computers"},
			patterns: []string{"Delete "},
			exclude:  []string{"Read Computers", "Create Computers"},
		},
		{
			// Platform Services privileges end with " verb" (lowercase). A pattern
			// of "Read " should match both "Read Computers" (prefix) and
			// "blueprints read" (suffix), but not "blueprints create".
			name:     "verb-suffix match: platform services caught by prefix pattern",
			all:      []string{"Read Computers", "blueprints read", "compliance-benchmarks read", "blueprints create"},
			patterns: []string{"Read "},
			include:  []string{"Read Computers", "blueprints read", "compliance-benchmarks read"},
			exclude:  []string{"blueprints create"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterPrivileges(tt.all, tt.patterns)
			gotSet := make(map[string]bool, len(got))
			for _, g := range got {
				gotSet[g] = true
			}
			for _, p := range tt.include {
				if !gotSet[p] {
					t.Errorf("expected %q in result", p)
				}
			}
			for _, p := range tt.exclude {
				if gotSet[p] {
					t.Errorf("unexpected %q in result", p)
				}
			}
		})
	}
}

func TestScopePresets_PrivilegeCoverage(t *testing.T) {
	// Build a synthetic privilege set that mirrors the real API structure.
	// Uses representative resource names so tests don't need updating when
	// Jamf adds new resources — the invariants are checked by verb prefix.
	resources := []string{"Computers", "Scripts", "Policies", "Buildings", "Categories"}

	var all []string
	for _, r := range resources {
		all = append(all, "Read "+r, "Create "+r, "Update "+r, "Delete "+r)
	}

	viewItems := []string{
		"View MDM command information in Jamf Pro API",
		"View Disk Encryption Recovery Key",
		"View Local Admin Password",
		"View Event Logs",
		"View JSS Information",
		"View Recovery Lock",
	}
	operationalItems := []string{
		"Edit Return To Service Configurations",
		"Allow User to Enroll",
		"Assign Users to Computers",
		"Assign Users to Mobile Devices",
		"Dismiss Notifications",
		"Enroll Computers",
		"Enroll Mobile Devices",
		"Flush MDM Commands",
		"Flush Policy Logs",
		"Jamf Connect Deployment Retry",
		"Jamf Packages Action",
		"Jamf Protect Deployment Retry",
		"Send Blank Pushes to Mobile Devices",
		"Send Command to Renew MDM Profile",
		"Send Declarative Management Command",
		"Send Device Information Command",
		"Send Inventory Requests to Mobile Devices",
		"Send MDM Check In Command",
	}
	destructiveSends := []string{
		"Send Computer Remote Wipe Command",
		"Send Computer Remote Lock Command",
		"Send Mobile Device Remote Wipe Command",
		"Send Mobile Device Remote Lock Command",
	}
	platformItems := []string{
		"blueprints read", "blueprints create", "blueprints update", "blueprints delete",
		"compliance-benchmarks read", "compliance-benchmarks create", "compliance-benchmarks update", "compliance-benchmarks delete",
	}

	all = append(all, viewItems...)
	all = append(all, operationalItems...)
	all = append(all, destructiveSends...)
	all = append(all, platformItems...)

	toSet := func(privs []string) map[string]bool {
		s := make(map[string]bool, len(privs))
		for _, p := range privs {
			s[p] = true
		}
		return s
	}

	t.Run("read-only includes all Read and View, excludes write and destructive", func(t *testing.T) {
		got := toSet(filterPrivileges(all, scopePresets["read-only"]))

		// Every Read X and View X in the live list must be included
		for _, p := range all {
			if strings.HasPrefix(p, "Read ") || strings.HasPrefix(p, "View ") {
				if !got[p] {
					t.Errorf("read-only: missing %q", p)
				}
			}
		}
		// Platform read items must be included
		for _, p := range []string{"blueprints read", "compliance-benchmarks read"} {
			if !got[p] {
				t.Errorf("read-only: missing %q", p)
			}
		}
		// Write verbs must be excluded
		for _, p := range all {
			if strings.HasPrefix(p, "Create ") || strings.HasPrefix(p, "Update ") || strings.HasPrefix(p, "Delete ") {
				if got[p] {
					t.Errorf("read-only: should not contain %q", p)
				}
			}
		}
		// Destructive sends and platform write/delete must be excluded
		for _, p := range append(destructiveSends, "blueprints create", "blueprints delete", "compliance-benchmarks create", "compliance-benchmarks delete") {
			if got[p] {
				t.Errorf("read-only: should not contain %q", p)
			}
		}
	})

	t.Run("standard includes Read/View/Create/Update and operational, excludes Delete and destructive sends", func(t *testing.T) {
		got := toSet(filterPrivileges(all, scopePresets["standard"]))

		// Every Read/View/Create/Update X must be included
		for _, p := range all {
			if strings.HasPrefix(p, "Read ") || strings.HasPrefix(p, "View ") ||
				strings.HasPrefix(p, "Create ") || strings.HasPrefix(p, "Update ") {
				if !got[p] {
					t.Errorf("standard: missing %q", p)
				}
			}
		}
		// All operational and safe sends must be included
		for _, p := range operationalItems {
			if !got[p] {
				t.Errorf("standard: missing operational privilege %q", p)
			}
		}
		// Platform read/create/update must be included, delete must not
		for _, p := range []string{"blueprints read", "blueprints create", "blueprints update", "compliance-benchmarks read", "compliance-benchmarks create", "compliance-benchmarks update"} {
			if !got[p] {
				t.Errorf("standard: missing %q", p)
			}
		}
		// Delete verbs must be excluded
		for _, p := range all {
			if strings.HasPrefix(p, "Delete ") {
				if got[p] {
					t.Errorf("standard: should not contain %q", p)
				}
			}
		}
		// Destructive sends and platform delete must be excluded
		for _, p := range append(destructiveSends, "blueprints delete", "compliance-benchmarks delete") {
			if got[p] {
				t.Errorf("standard: should not contain %q", p)
			}
		}
	})

	t.Run("full-admin uses nil so caller passes all privileges through", func(t *testing.T) {
		if scopePresets["full-admin"] != nil {
			t.Error("full-admin should have nil patterns")
		}
	})
}

func TestFilterPrivileges_NilPrefixes(t *testing.T) {
	// nil prefixes means full-admin (handled by caller, not filterPrivileges)
	got := filterPrivileges([]string{"Read X"}, nil)
	if len(got) != 0 {
		t.Errorf("expected 0 results for nil prefixes, got %d", len(got))
	}
}

func TestSetupClient_FetchPrivileges(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/api-role-privileges" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing or incorrect authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"privileges": []string{"Read Computers", "Create Computers"},
		})
	}))
	defer server.Close()

	client := newSetupClient(server.URL, "test-token")
	privs, err := client.fetchPrivileges(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(privs) != 2 {
		t.Errorf("expected 2 privileges, got %d", len(privs))
	}
}

func TestSetupClient_CreateAPIRole(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/api-roles" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["displayName"] != "test-role" {
			t.Errorf("expected displayName=test-role, got %v", body["displayName"])
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "role-42"})
	}))
	defer server.Close()

	client := newSetupClient(server.URL, "test-token")
	id, err := client.createAPIRole(context.Background(), "test-role", []string{"Read Computers"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "role-42" {
		t.Errorf("expected role-42, got %s", id)
	}
}

func TestSetupClient_CreateAPIRole_Forbidden(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	client := newSetupClient(server.URL, "test-token")
	_, err := client.createAPIRole(context.Background(), "test-role", nil)
	if err == nil {
		t.Fatal("expected error for forbidden")
	}
}

func TestSetupClient_CreateAPIIntegration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/api-integrations" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["accessTokenLifetimeSeconds"] != 300.0 {
			t.Errorf("accessTokenLifetimeSeconds = %v, want 300", body["accessTokenLifetimeSeconds"])
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]int{"id": 7})
	}))
	defer server.Close()

	client := newSetupClient(server.URL, "test-token")
	id, err := client.createAPIIntegration(context.Background(), "test-int", []string{"role-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 7 {
		t.Errorf("expected 7, got %d", id)
	}
}

func TestSetupClient_GenerateClientCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/api-integrations/5/client-credentials" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"clientId":     "cid-abc",
			"clientSecret": "csec-xyz",
		})
	}))
	defer server.Close()

	client := newSetupClient(server.URL, "test-token")
	cid, csec, err := client.generateClientCredentials(context.Background(), 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cid != "cid-abc" {
		t.Errorf("expected cid-abc, got %s", cid)
	}
	if csec != "csec-xyz" {
		t.Errorf("expected csec-xyz, got %s", csec)
	}
}

func TestSetupClient_GenerateClientCredentials_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer server.Close()

	client := newSetupClient(server.URL, "test-token")
	_, _, err := client.generateClientCredentials(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error for server error")
	}
}

func TestScopePresets(t *testing.T) {
	if _, ok := scopePresets["read-only"]; !ok {
		t.Error("missing read-only preset")
	}
	if _, ok := scopePresets["standard"]; !ok {
		t.Error("missing standard preset")
	}
	if _, ok := scopePresets["full-admin"]; !ok {
		t.Error("missing full-admin preset")
	}
	if scopePresets["full-admin"] != nil {
		t.Error("full-admin should have nil prefixes (all privileges)")
	}
}

func TestSetupClient_CreateAPIIntegration_Forbidden(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	client := newSetupClient(server.URL, "test-token")
	_, err := client.createAPIIntegration(context.Background(), "test-int", nil)
	if err == nil {
		t.Fatal("expected error for forbidden")
	}
	if !strings.Contains(err.Error(), "lacks permission") {
		t.Errorf("error = %q, want to contain 'lacks permission'", err.Error())
	}
}

func TestSetupClient_CreateAPIIntegration_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := newSetupClient(server.URL, "test-token")
	_, err := client.createAPIIntegration(context.Background(), "test-int", nil)
	if err == nil {
		t.Fatal("expected error for server error")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("error = %q, want to contain 'HTTP 500'", err.Error())
	}
}

func TestSetupClient_FetchPrivileges_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	}))
	defer server.Close()

	client := newSetupClient(server.URL, "test-token")
	_, err := client.fetchPrivileges(context.Background())
	if err == nil {
		t.Fatal("expected error for HTTP error")
	}
	if !strings.Contains(err.Error(), "HTTP 403") {
		t.Errorf("error = %q, want to contain 'HTTP 403'", err.Error())
	}
}

func TestSetupClient_FetchPrivileges_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	client := newSetupClient(server.URL, "test-token")
	_, err := client.fetchPrivileges(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "parsing privileges") {
		t.Errorf("error = %q, want to contain 'parsing privileges'", err.Error())
	}
}

func TestSetupClient_CreateAPIRole_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer server.Close()

	client := newSetupClient(server.URL, "test-token")
	_, err := client.createAPIRole(context.Background(), "test-role", nil)
	if err == nil {
		t.Fatal("expected error for bad request")
	}
	if !strings.Contains(err.Error(), "HTTP 400") {
		t.Errorf("error = %q, want to contain 'HTTP 400'", err.Error())
	}
}

func TestSetupClient_Do_NilBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify no Content-Type header when body is nil
		if r.Header.Get("Content-Type") != "" {
			t.Error("Content-Type should not be set for nil body")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := newSetupClient(server.URL, "test-token")
	body, status, err := client.do(context.Background(), "GET", "/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if string(body) != `{}` {
		t.Errorf("body = %q, want %q", string(body), "{}")
	}
}

func TestSetupClient_FindAPIRoleByName_Found(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.HasPrefix(r.URL.Path, "/api/v1/api-roles") {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if !strings.Contains(r.URL.RawQuery, "filter=") {
			t.Error("expected filter query parameter")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalCount": 1,
			"results":    []map[string]any{{"id": "role-42", "displayName": "jamf-cli-standard"}},
		})
	}))
	defer server.Close()

	client := newSetupClient(server.URL, "test-token")
	id, err := client.findAPIRoleByName(context.Background(), "jamf-cli-standard")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "role-42" {
		t.Errorf("id = %q, want %q", id, "role-42")
	}
}

func TestSetupClient_FindAPIRoleByName_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalCount": 0,
			"results":    []map[string]any{},
		})
	}))
	defer server.Close()

	client := newSetupClient(server.URL, "test-token")
	id, err := client.findAPIRoleByName(context.Background(), "jamf-cli-standard")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "" {
		t.Errorf("id = %q, want empty string", id)
	}
}

func TestSetupClient_FindAPIRoleByName_Multiple(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalCount": 3,
			"results":    []map[string]any{{"id": "1"}, {"id": "2"}},
		})
	}))
	defer server.Close()

	client := newSetupClient(server.URL, "test-token")
	_, err := client.findAPIRoleByName(context.Background(), "jamf-cli-standard")
	if err == nil {
		t.Fatal("expected error for multiple matches")
	}
	if !strings.Contains(err.Error(), "multiple") {
		t.Errorf("error = %q, want it to contain 'multiple'", err.Error())
	}
}

func TestSetupClient_FindAPIRoleByName_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	}))
	defer server.Close()

	client := newSetupClient(server.URL, "test-token")
	_, err := client.findAPIRoleByName(context.Background(), "jamf-cli-standard")
	if err == nil {
		t.Fatal("expected error for HTTP error")
	}
	if !strings.Contains(err.Error(), "HTTP 403") {
		t.Errorf("error = %q, want it to contain 'HTTP 403'", err.Error())
	}
}

func TestSetupClient_FindAPIIntegrationByName_Found(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.HasPrefix(r.URL.Path, "/api/v1/api-integrations") {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalCount": 1,
			"results":    []map[string]any{{"id": 7, "displayName": "jamf-cli"}},
		})
	}))
	defer server.Close()

	client := newSetupClient(server.URL, "test-token")
	id, err := client.findAPIIntegrationByName(context.Background(), "jamf-cli")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 7 {
		t.Errorf("id = %d, want 7", id)
	}
}

func TestSetupClient_FindAPIIntegrationByName_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalCount": 0,
			"results":    []map[string]any{},
		})
	}))
	defer server.Close()

	client := newSetupClient(server.URL, "test-token")
	id, err := client.findAPIIntegrationByName(context.Background(), "jamf-cli")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 0 {
		t.Errorf("id = %d, want 0", id)
	}
}

func TestSetupClient_FindAPIIntegrationByName_Multiple(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalCount": 2,
			"results":    []map[string]any{{"id": 1}, {"id": 2}},
		})
	}))
	defer server.Close()

	client := newSetupClient(server.URL, "test-token")
	_, err := client.findAPIIntegrationByName(context.Background(), "jamf-cli")
	if err == nil {
		t.Fatal("expected error for multiple matches")
	}
	if !strings.Contains(err.Error(), "multiple") {
		t.Errorf("error = %q, want it to contain 'multiple'", err.Error())
	}
}

func TestSetupClient_UpdateAPIRole_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.URL.Path != "/api/v1/api-roles/role-42" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["displayName"] != "jamf-cli-standard" {
			t.Errorf("displayName = %v, want jamf-cli-standard", body["displayName"])
		}
		if privs, ok := body["privileges"].([]any); !ok || len(privs) != 1 {
			t.Errorf("privileges = %v, want 1 element", body["privileges"])
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "role-42"})
	}))
	defer server.Close()

	client := newSetupClient(server.URL, "test-token")
	err := client.updateAPIRole(context.Background(), "role-42", "jamf-cli-standard", []string{"Read Computers"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetupClient_UpdateAPIRole_Forbidden(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	client := newSetupClient(server.URL, "test-token")
	err := client.updateAPIRole(context.Background(), "role-42", "jamf-cli-standard", nil)
	if err == nil {
		t.Fatal("expected error for forbidden")
	}
	if !strings.Contains(err.Error(), "lacks permission") {
		t.Errorf("error = %q, want it to contain 'lacks permission'", err.Error())
	}
}

func TestSetupClient_UpdateAPIIntegration_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.URL.Path != "/api/v1/api-integrations/7" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["displayName"] != "jamf-cli" {
			t.Errorf("displayName = %v, want jamf-cli", body["displayName"])
		}
		if body["enabled"] != true {
			t.Errorf("enabled = %v, want true", body["enabled"])
		}
		if body["accessTokenLifetimeSeconds"] != 300.0 {
			t.Errorf("accessTokenLifetimeSeconds = %v, want 300", body["accessTokenLifetimeSeconds"])
		}
		scopes, ok := body["authorizationScopes"].([]any)
		if !ok || len(scopes) != 1 || scopes[0] != "jamf-cli-standard" {
			t.Errorf("authorizationScopes = %v, want [jamf-cli-standard]", body["authorizationScopes"])
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 7})
	}))
	defer server.Close()

	client := newSetupClient(server.URL, "test-token")
	err := client.updateAPIIntegration(context.Background(), 7, "jamf-cli", []string{"jamf-cli-standard"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetupClient_UpdateAPIIntegration_Forbidden(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	client := newSetupClient(server.URL, "test-token")
	err := client.updateAPIIntegration(context.Background(), 7, "jamf-cli", nil)
	if err == nil {
		t.Fatal("expected error for forbidden")
	}
	if !strings.Contains(err.Error(), "lacks permission") {
		t.Errorf("error = %q, want it to contain 'lacks permission'", err.Error())
	}
}

func TestExtractSubdomain(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://nmartin.jamfcloud.com", "nmartin"},
		{"https://datajar-school1.jamfcloud.com", "datajar-school1"},
		{"nmartin.jamfcloud.com", "nmartin"},
		{"https://nmartin.jamfcloud.com/", "nmartin"},
		{"https://localhost:8080", "localhost"},
		{"https://10.0.1.1:8443", "10"},
		{"singlehost", "singlehost"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := extractSubdomain(tt.input)
			if got != tt.want {
				t.Errorf("extractSubdomain(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestReadURLsFromFile(t *testing.T) {
	// Create a temp file with URLs
	f, err := os.CreateTemp("", "urls-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	_, _ = f.WriteString("https://school1.jamfcloud.com\n")
	_, _ = f.WriteString("# this is a comment\n")
	_, _ = f.WriteString("\n")
	_, _ = f.WriteString("https://school2.jamfcloud.com\n")
	_, _ = f.WriteString("  school3.jamfcloud.com  \n")
	_ = f.Close()

	urls, err := readURLsFromFile(f.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(urls) != 3 {
		t.Fatalf("got %d URLs, want 3", len(urls))
	}
	if urls[0] != "https://school1.jamfcloud.com" {
		t.Errorf("urls[0] = %q, want %q", urls[0], "https://school1.jamfcloud.com")
	}
	if urls[2] != "https://school3.jamfcloud.com" {
		t.Errorf("urls[2] = %q, want %q (should be normalized)", urls[2], "https://school3.jamfcloud.com")
	}
}

func TestReadURLsFromFile_Duplicates(t *testing.T) {
	f, err := os.CreateTemp("", "urls-dup-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	_, _ = f.WriteString("https://school1.jamfcloud.com\n")
	_, _ = f.WriteString("https://school2.jamfcloud.com\n")
	_, _ = f.WriteString("https://school1.jamfcloud.com\n")
	_, _ = f.WriteString("https://school2.jamfcloud.com\n")
	_ = f.Close()

	urls, err := readURLsFromFile(f.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(urls) != 2 {
		t.Fatalf("got %d URLs, want 2 (duplicates should be removed)", len(urls))
	}
}

func TestReadURLsFromFile_DuplicatesAcrossFormats(t *testing.T) {
	f, err := os.CreateTemp("", "urls-dup-fmt-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	_, _ = f.WriteString("school1.jamfcloud.com\n")
	_, _ = f.WriteString("https://school1.jamfcloud.com\n")
	_, _ = f.WriteString("https://school2.jamfcloud.com/\n")
	_, _ = f.WriteString("school2.jamfcloud.com\n")
	_ = f.Close()

	urls, err := readURLsFromFile(f.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(urls) != 2 {
		t.Fatalf("got %d URLs, want 2 (equivalent URLs should be deduped)", len(urls))
	}
	// URLs should be normalized
	if urls[0] != "https://school1.jamfcloud.com" {
		t.Errorf("urls[0] = %q, want %q", urls[0], "https://school1.jamfcloud.com")
	}
}

func TestReadURLsFromFile_Empty(t *testing.T) {
	f, err := os.CreateTemp("", "urls-empty-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	_, _ = f.WriteString("# only comments\n\n")
	_ = f.Close()

	_, err = readURLsFromFile(f.Name())
	if err == nil {
		t.Fatal("expected error for empty file")
	}
	if !strings.Contains(err.Error(), "no URLs found") {
		t.Errorf("error = %q, want it to contain 'no URLs found'", err.Error())
	}
}

func TestReadURLsFromFile_NotFound(t *testing.T) {
	_, err := readURLsFromFile("/tmp/nonexistent-url-file-12345.txt")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestEscapeRSQL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"alice", "alice"},
		{`o"brien`, `o\"brien`},
		{`a"b"c`, `a\"b\"c`},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := resolve.EscapeRSQL(tt.input)
			if got != tt.want {
				t.Errorf("EscapeRSQL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"example.jamfcloud.com", "https://example.jamfcloud.com"},
		{"https://example.jamfcloud.com", "https://example.jamfcloud.com"},
		{"http://localhost:8080", "http://localhost:8080"},
		{"example.jamfcloud.com/", "https://example.jamfcloud.com"},
		{"https://example.jamfcloud.com/", "https://example.jamfcloud.com"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeURL(tt.input)
			if got != tt.want {
				t.Errorf("normalizeURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
