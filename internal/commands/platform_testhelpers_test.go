// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
)

// newTestPlatformSDK returns a *jamfplatform.Client wired to a fresh
// httptest.Server. The returned mux is preregistered with /auth/token
// returning a fixed bearer; tests register handlers for the API endpoints
// they exercise. The server is torn down via t.Cleanup.
//
// Use this for tests of code that calls cliCtx.PlatformSDKClient.<Subpkg>.X()
// — the SDK's transport will hit the test server, exercising real HTTP
// path/query/body construction.
const testTenantID = "test-tenant"

func newTestPlatformSDK(t *testing.T) (*jamfplatform.Client, *http.ServeMux) {
	t.Helper()
	mux := http.NewServeMux()
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
		jamfplatform.WithTenantID(testTenantID),
	), mux
}

// writeJSON writes v as a JSON response with HTTP 200 and the JSON
// content-type. Helper for handler functions registered on the test mux.
func writeJSON(w http.ResponseWriter, v any) {
	writeJSONStatus(w, http.StatusOK, v)
}

// writeJSONStatus writes v as a JSON response with the given status code.
// Sets Content-Type before WriteHeader so the SDK's response decoder gets the
// expected media type.
func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// newTestPlatformContext returns a CLIContext with PlatformSDKClient backed
// by an httptest server, plus the captureOutput for inspection and the mux
// for handler registration. Tests register handlers for the endpoints they
// exercise.
func newTestPlatformContext(t *testing.T) (*registry.CLIContext, *http.ServeMux, *captureOutput) {
	t.Helper()
	sdk, mux := newTestPlatformSDK(t)
	out := &captureOutput{}
	return &registry.CLIContext{
		PlatformSDKClient: sdk,
		Output:            out,
	}, mux, out
}
