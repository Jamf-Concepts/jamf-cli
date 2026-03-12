package commands

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFilterPrivileges(t *testing.T) {
	all := []string{
		"Read Computers",
		"Create Computers",
		"Update Computers",
		"Delete Computers",
		"Read Users",
		"Create Users",
	}

	tests := []struct {
		name     string
		prefixes []string
		want     int
	}{
		{"read-only", []string{"Read "}, 2},
		{"standard", []string{"Read ", "Create ", "Update "}, 5},
		{"no match", []string{"Assign "}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterPrivileges(all, tt.prefixes)
			if len(got) != tt.want {
				t.Errorf("filterPrivileges(%v) returned %d results, want %d", tt.prefixes, len(got), tt.want)
			}
		})
	}
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
		json.NewEncoder(w).Encode(map[string]interface{}{
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
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["displayName"] != "test-role" {
			t.Errorf("expected displayName=test-role, got %v", body["displayName"])
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"id": "role-42"})
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
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]int{"id": 7})
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
		json.NewEncoder(w).Encode(map[string]string{
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
		w.Write([]byte("server error"))
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
