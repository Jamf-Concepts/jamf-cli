// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/keychain"
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
			// of "Read " (trailing space) matches both "Read Computers" (prefix) and
			// "blueprints read" (verb-suffix derived as " read"), but NOT "blueprints create".
			// This is the verb-suffix path, distinct from the *suffix path below.
			name:     "verb-suffix match: platform services caught by prefix pattern",
			all:      []string{"Read Computers", "blueprints read", "compliance-benchmarks read", "blueprints create"},
			patterns: []string{"Read "},
			include:  []string{"Read Computers", "blueprints read", "compliance-benchmarks read"},
			exclude:  []string{"blueprints create"},
		},
		{
			// *suffix patterns match any privilege ending with the given suffix.
			// Used in standard's exclude list to catch Remote Wipe/Lock variants
			// ("Send Computer Remote Wipe Command", "Send Mobile Device Remote Wipe Command", etc.)
			// without enumerating each combination of device type and command name.
			name:     "*suffix match: catches all variants sharing a common ending",
			all:      []string{"Send Computer Remote Wipe Command", "Send Mobile Device Remote Wipe Command", "Send MDM Check In Command"},
			patterns: []string{"*Remote Wipe Command"},
			include:  []string{"Send Computer Remote Wipe Command", "Send Mobile Device Remote Wipe Command"},
			exclude:  []string{"Send MDM Check In Command"},
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
	// Operational privileges included in standard (non-destructive, non-audit-destroying).
	safeOperational := []string{
		"Edit Return To Service Configurations",
		"Allow User to Enroll",
		"Assign Users to Computers",
		"Assign Users to Mobile Devices",
		"Enroll Computers",
		"Enroll Mobile Devices",
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
	// Operational privileges excluded from standard: irreversible or destructive.
	destructiveOperational := []string{
		"Dismiss Notifications",             // irreversible
		"Flush MDM Commands",                // destroys audit data
		"Flush Policy Logs",                 // destroys audit data
		"Send Computer Remote Wipe Command", // irreversible data loss
		"Send Mobile Device Remote Wipe Command",
		"Send Computer Remote Lock Command", // device becomes inaccessible
		"Send Mobile Device Remote Lock Command",
	}
	platformItems := []string{
		"blueprints read", "blueprints create", "blueprints update", "blueprints delete",
		"compliance-benchmarks read", "compliance-benchmarks create", "compliance-benchmarks update", "compliance-benchmarks delete",
	}

	all = append(all, viewItems...)
	all = append(all, safeOperational...)
	all = append(all, destructiveOperational...)
	all = append(all, platformItems...)

	toSet := func(privs []string) map[string]bool {
		s := make(map[string]bool, len(privs))
		for _, p := range privs {
			s[p] = true
		}
		return s
	}

	t.Run("read-only includes all Read and View, excludes everything else", func(t *testing.T) {
		got := toSet(applyPrivilegeFilter(all, scopeOptionByKey("read-only")))

		// Every Read X and View X must be included (covers platform " read" suffix too)
		for _, p := range all {
			if strings.HasPrefix(p, "Read ") || strings.HasPrefix(p, "View ") ||
				strings.HasSuffix(p, " read") {
				if !got[p] {
					t.Errorf("read-only: missing %q", p)
				}
			}
		}
		// Write verbs must be excluded
		for _, p := range all {
			if strings.HasPrefix(p, "Create ") || strings.HasPrefix(p, "Update ") ||
				strings.HasPrefix(p, "Delete ") || strings.HasPrefix(p, "Flush ") ||
				strings.HasPrefix(p, "Dismiss ") {
				if got[p] {
					t.Errorf("read-only: should not contain %q", p)
				}
			}
		}
		// Platform non-read must be excluded
		for _, p := range []string{
			"blueprints create", "blueprints update", "blueprints delete",
			"compliance-benchmarks create", "compliance-benchmarks update", "compliance-benchmarks delete",
		} {
			if got[p] {
				t.Errorf("read-only: should not contain %q", p)
			}
		}
	})

	t.Run("standard excludes Delete/Flush/Dismiss, includes everything else", func(t *testing.T) {
		got := toSet(applyPrivilegeFilter(all, scopeOptionByKey("standard")))

		// Read/View/Create/Update must be included
		for _, p := range all {
			if strings.HasPrefix(p, "Read ") || strings.HasPrefix(p, "View ") ||
				strings.HasPrefix(p, "Create ") || strings.HasPrefix(p, "Update ") {
				if !got[p] {
					t.Errorf("standard: missing %q", p)
				}
			}
		}
		// Safe operational items must be included
		for _, p := range safeOperational {
			if !got[p] {
				t.Errorf("standard: missing %q", p)
			}
		}
		// Platform read/create/update must be included
		for _, p := range []string{
			"blueprints read", "blueprints create", "blueprints update",
			"compliance-benchmarks read", "compliance-benchmarks create", "compliance-benchmarks update",
		} {
			if !got[p] {
				t.Errorf("standard: missing %q", p)
			}
		}
		// Delete, Flush, Dismiss must be excluded (covers platform verb suffixes too)
		for _, p := range all {
			if strings.HasPrefix(p, "Delete ") || strings.HasPrefix(p, "Flush ") ||
				strings.HasPrefix(p, "Dismiss ") {
				if got[p] {
					t.Errorf("standard: should not contain %q", p)
				}
			}
		}
		for _, p := range append(destructiveOperational, "blueprints delete", "compliance-benchmarks delete") {
			if got[p] {
				t.Errorf("standard: should not contain %q", p)
			}
		}
	})

	t.Run("full-admin passes all privileges through unchanged", func(t *testing.T) {
		got := applyPrivilegeFilter(all, scopeOptionByKey("full-admin"))
		if len(got) != len(all) {
			t.Errorf("full-admin: got %d privileges, want %d (all)", len(got), len(all))
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
		return
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
		return
	}
}

func TestScopePresets(t *testing.T) {
	for _, key := range []string{"read-only", "standard", "full-admin"} {
		if scopeOptionByKey(key).key == "" {
			t.Errorf("missing scope option %q", key)
		}
	}
	opt := scopeOptionByKey("full-admin")
	if opt.include != nil || len(opt.exclude) > 0 {
		t.Error("full-admin should have nil include and no excludes (all privileges)")
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
		return
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
		return
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
		return
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
		return
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
		return
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
		return
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
		return
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
		return
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
		return
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
		return
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
		return
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
		return
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
		return
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
		return
	}
	defer func() { _ = os.Remove(f.Name()) }()

	_, _ = f.WriteString("# only comments\n\n")
	_ = f.Close()

	_, err = readURLsFromFile(f.Name())
	if err == nil {
		t.Fatal("expected error for empty file")
		return
	}
	if !strings.Contains(err.Error(), "no URLs found") {
		t.Errorf("error = %q, want it to contain 'no URLs found'", err.Error())
	}
}

func TestReadURLsFromFile_NotFound(t *testing.T) {
	_, err := readURLsFromFile("/tmp/nonexistent-url-file-12345.txt")
	if err == nil {
		t.Fatal("expected error for missing file")
		return
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

// setupInstanceServer builds a test HTTP server that handles the Jamf Pro API
// calls made by setupInstance. existingIntID controls whether an existing
// integration is returned (non-zero) or not (zero = create path).
func setupInstanceServer(t *testing.T, existingIntID int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/api/v1/auth/token"):
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "test-bearer"})

		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/api-role-privileges"):
			_ = json.NewEncoder(w).Encode(map[string]any{"privileges": []string{"Read Computers"}})

		case r.Method == "GET" && strings.Contains(r.URL.Path, "/api-roles"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"totalCount": 1,
				"results":    []map[string]any{{"id": "role-1"}},
			})

		case r.Method == "PUT" && strings.Contains(r.URL.Path, "/api-roles/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "role-1"})

		case r.Method == "GET" && strings.Contains(r.URL.Path, "/api-integrations"):
			if existingIntID != 0 {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"totalCount": 1,
					"results":    []map[string]any{{"id": existingIntID}},
				})
			} else {
				_ = json.NewEncoder(w).Encode(map[string]any{"totalCount": 0, "results": []any{}})
			}

		case r.Method == "POST" && strings.Contains(r.URL.Path, "/api-integrations") && strings.HasSuffix(r.URL.Path, "/client-credentials"):
			_ = json.NewEncoder(w).Encode(map[string]string{"clientId": "new-cid", "clientSecret": "new-csec"})

		case r.Method == "PUT" && strings.Contains(r.URL.Path, "/api-integrations/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": existingIntID})

		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/api-integrations"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 99})

		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/client-credentials"):
			_ = json.NewEncoder(w).Encode(map[string]string{"clientId": "new-cid", "clientSecret": "new-csec"})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// TestSetupInstance_ExistingIntegration_KeychainMissing reproduces the bug where
// re-running setup with the same profile name after deleting the keychain items
// results in a profile with broken keychain references. The fix detects that the
// keychain items are absent and regenerates credentials instead of skipping.
func TestSetupInstance_ExistingIntegration_KeychainMissing(t *testing.T) {
	server := setupInstanceServer(t, 42) // integration ID 42 already exists
	defer server.Close()

	mock := newMockKeychainStore()
	old := config.KeychainStore
	config.KeychainStore = mock
	defer func() { config.KeychainStore = old }()

	cfg := &config.Config{Profiles: make(map[string]config.Profile)}
	var out bytes.Buffer

	err := setupInstance(context.Background(), &out, cfg, server.URL, "user", "pass", "standard", "my-profile", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Credentials must have been stored despite integration already existing.
	cidKey := keychain.DefaultService + "/my-profile/client-id"
	csecKey := keychain.DefaultService + "/my-profile/client-secret"
	if mock.items[cidKey] == "" {
		t.Errorf("expected client-id in keychain at %q, got nothing", cidKey)
	}
	if mock.items[csecKey] == "" {
		t.Errorf("expected client-secret in keychain at %q, got nothing", csecKey)
	}

	// Config must reference the keychain items.
	p := cfg.Profiles["my-profile"]
	wantCID := keychain.KeychainRef("my-profile", "client-id")
	wantCSec := keychain.KeychainRef("my-profile", "client-secret")
	if p.ClientID != wantCID {
		t.Errorf("ClientID = %q, want %q", p.ClientID, wantCID)
	}
	if p.ClientSecret != wantCSec {
		t.Errorf("ClientSecret = %q, want %q", p.ClientSecret, wantCSec)
	}

	// The output should mention that credentials were missing and being regenerated.
	if !strings.Contains(out.String(), "missing") && !strings.Contains(out.String(), "regenerat") {
		t.Errorf("expected output to mention missing/regenerating credentials, got:\n%s", out.String())
	}
}

// TestSetupInstance_ExistingIntegration_KeychainPresent verifies that when
// credentials already exist in keychain, setup does NOT regenerate them.
func TestSetupInstance_ExistingIntegration_KeychainPresent(t *testing.T) {
	server := setupInstanceServer(t, 42)
	defer server.Close()

	mock := newMockKeychainStore()
	mock.items[keychain.DefaultService+"/my-profile/client-id"] = "existing-cid"
	mock.items[keychain.DefaultService+"/my-profile/client-secret"] = "existing-csec"

	old := config.KeychainStore
	config.KeychainStore = mock
	defer func() { config.KeychainStore = old }()

	cfg := &config.Config{Profiles: make(map[string]config.Profile)}
	var out bytes.Buffer

	err := setupInstance(context.Background(), &out, cfg, server.URL, "user", "pass", "standard", "my-profile", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Existing credentials must not have been overwritten.
	if got := mock.items[keychain.DefaultService+"/my-profile/client-id"]; got != "existing-cid" {
		t.Errorf("client-id overwritten: got %q, want %q", got, "existing-cid")
	}
	if got := mock.items[keychain.DefaultService+"/my-profile/client-secret"]; got != "existing-csec" {
		t.Errorf("client-secret overwritten: got %q, want %q", got, "existing-csec")
	}

	if !strings.Contains(out.String(), "unchanged") {
		t.Errorf("expected output to say credentials unchanged, got:\n%s", out.String())
	}
}
