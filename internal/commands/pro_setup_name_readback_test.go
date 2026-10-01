// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A server that ignores the filter returns some other role or integration.
// Setup must not update that record or rotate its credentials.
func TestSetupClient_SearchResultWithAnotherDisplayNameIsNotAMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var id any = 7
		if strings.Contains(r.URL.Path, "/api-roles") {
			id = "7"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalCount": 1,
			"results":    []map[string]any{{"id": id, "displayName": "Production Sync"}},
		})
	}))
	defer server.Close()
	client := newSetupClient(server.URL, "test-token")

	if id, err := client.findAPIRoleByName(context.Background(), "jamf-cli-standard"); err != nil || id != "" {
		t.Errorf("findAPIRoleByName = %q, %v; want not found for a role named Production Sync", id, err)
	}
	if id, err := client.findAPIIntegrationByName(context.Background(), "jamf-cli"); err != nil || id != 0 {
		t.Errorf("findAPIIntegrationByName = %d, %v; want not found for an integration named Production Sync", id, err)
	}
}
