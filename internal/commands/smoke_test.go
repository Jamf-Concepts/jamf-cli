// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/client"
	"github.com/Jamf-Concepts/jamf-cli/internal/commands/pro/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// ---------------------------------------------------------------------------
// Client setup
// ---------------------------------------------------------------------------

// smokeClient builds a real HTTP client from the CLI config, or skips the test
// if no profile is available.
func smokeClient(t *testing.T) registry.HTTPClient {
	t.Helper()

	cfg, err := config.Load()
	if err != nil {
		t.Skipf("cannot load config: %v", err)
	}

	profileName := os.Getenv("JAMF_PROFILE")
	url, provider, err := ResolveAuthForProfile(cfg, AuthParams{
		Profile:       profileName,
		ServerURL:     os.Getenv("JAMF_URL"),
		Token:         os.Getenv("JAMF_TOKEN"),
		ClientID:      os.Getenv("JAMF_CLIENT_ID"),
		ClientSecret:  os.Getenv("JAMF_CLIENT_SECRET"),
		TenantID:      os.Getenv("JAMF_TENANT_ID"),
		EnvironmentID: os.Getenv("JAMF_ENVIRONMENT_ID"),
	})
	if err != nil {
		t.Skipf("cannot resolve auth: %v", err)
	}

	// Enable gateway mode for a platform credential, the way root.go's
	// PersistentPreRunE does. Without this the suite sent instance-shaped
	// /api/v1/... paths to the gateway host, so every request 404'd and the
	// whole sweep skipped itself — leaving the suite structurally blind to
	// gateway mode, which is exactly where the URL shape changed at GA.
	var opts []client.Option
	if p, ok := provider.(*auth.PlatformOAuth2Provider); ok {
		opts = append(opts, client.WithGatewayScope(p.Scope()))
	}

	return &cliClient{client.New(url, provider, opts...)}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// smokeGET makes a GET request and returns the response body. It handles
// expected error codes (404, 403) as skips rather than failures.
func smokeGET(t *testing.T, httpClient registry.HTTPClient, path string) (body []byte, skipped bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := httpClient.Do(ctx, "GET", path, nil)
	if err != nil {
		var exitErr *exitcode.Error
		if errors.As(err, &exitErr) {
			switch exitErr.Code {
			case exitcode.NotFound:
				t.Skipf("404: %s (endpoint may not exist in this version)", path)
				return nil, true
			case exitcode.PermissionDenied:
				t.Skipf("403: %s (insufficient privileges)", path)
				return nil, true
			case exitcode.RateLimited:
				t.Fatalf("429: %s (rate limited after retries)", path)
				return nil, true
			}
			// 400 Bad Request typically means the endpoint requires parameters
			// we can't infer — skip rather than fail.
			if strings.Contains(exitErr.Message, "HTTP 400") {
				t.Skipf("400: %s (endpoint requires parameters)", path)
				return nil, true
			}
			// 503 Service Unavailable — transient or feature not provisioned.
			if strings.Contains(exitErr.Message, "HTTP 503") {
				t.Skipf("503: %s (service unavailable)", path)
				return nil, true
			}
		}
		t.Fatalf("GET %s: %v", path, err)
		return nil, true
	}
	defer func() { _ = resp.Body.Close() }()

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("GET %s: reading body: %v", path, err)
	}

	if len(body) == 0 {
		t.Skipf("GET %s: empty response body", path)
		return nil, true
	}
	// Some endpoints intentionally return non-JSON (e.g., CSV exports).
	if strings.HasSuffix(strings.SplitN(path, "?", 2)[0], "/csv") {
		t.Skipf("GET %s: non-JSON endpoint (CSV)", path)
		return body, true
	}
	if !json.Valid(body) {
		t.Errorf("GET %s: response is not valid JSON (first 200 bytes): %s", path, truncate(body, 200))
	}

	return body, false
}

// smokeExtractFirstID extracts the first resource ID from a list response.
func smokeExtractFirstID(body []byte, ep generated.SmokeEndpoint) string {
	if ep.IsClassic && ep.WrapperKey != "" {
		var wrapper map[string]json.RawMessage
		if json.Unmarshal(body, &wrapper) != nil {
			return ""
		}
		inner, ok := wrapper[ep.WrapperKey]
		if !ok {
			return ""
		}
		return firstIDFromArray(inner)
	}

	// Modern: try paginated {"results": [...]} first
	var paginated struct {
		Results []json.RawMessage `json:"results"`
	}
	if json.Unmarshal(body, &paginated) == nil && len(paginated.Results) > 0 {
		return idFromObject(paginated.Results[0])
	}

	// Modern: try plain array
	return firstIDFromArray(body)
}

func firstIDFromArray(data []byte) string {
	var arr []json.RawMessage
	if json.Unmarshal(data, &arr) != nil || len(arr) == 0 {
		return ""
	}
	return idFromObject(arr[0])
}

func idFromObject(data []byte) string {
	var obj map[string]any
	if json.Unmarshal(data, &obj) != nil {
		return ""
	}
	for _, key := range []string{"id", "udid", "deviceId", "mobileDeviceId", "groupJamfProId"} {
		if v, ok := obj[key]; ok {
			switch val := v.(type) {
			case string:
				return val
			case float64:
				return fmt.Sprintf("%d", int(val))
			}
		}
	}
	return ""
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// ---------------------------------------------------------------------------
// Tier 1: Endpoint reachability
// ---------------------------------------------------------------------------

func TestSmoke_Tier1(t *testing.T) {
	if os.Getenv("JAMF_SMOKE_TEST") == "" {
		t.Skip("set JAMF_SMOKE_TEST=1 to run smoke tests against a real Jamf Pro instance")
	}

	httpClient := smokeClient(t)
	endpoints := generated.AllSmokeEndpoints()

	// Concurrency limiter
	sem := make(chan struct{}, 5)

	// Phase 1: Run all endpoints without path params (lists and singletons).
	// Collect first IDs from list endpoints for Phase 2.
	var mu sync.Mutex
	listIDs := make(map[string]string) // resource -> first ID

	t.Run("lists", func(t *testing.T) {
		for _, ep := range endpoints {
			if ep.HasPathParams {
				continue
			}
			ep := ep
			t.Run(ep.Resource+"/"+ep.Operation, func(t *testing.T) {
				t.Parallel()
				sem <- struct{}{}
				defer func() { <-sem }()

				body, skipped := smokeGET(t, httpClient, ep.Path)
				if skipped {
					return
				}

				if ep.IsList {
					id := smokeExtractFirstID(body, ep)
					if id != "" {
						mu.Lock()
						// Only set if not already set (first list wins)
						if _, exists := listIDs[ep.Resource]; !exists {
							listIDs[ep.Resource] = id
						}
						mu.Unlock()
					}
				}
			})
		}
	})

	// Phase 2: Run endpoints with path params using discovered IDs.
	t.Run("gets", func(t *testing.T) {
		for _, ep := range endpoints {
			if !ep.HasPathParams {
				continue
			}
			ep := ep
			t.Run(ep.Resource+"/"+ep.Operation, func(t *testing.T) {
				t.Parallel()
				sem <- struct{}{}
				defer func() { <-sem }()

				id, ok := listIDs[ep.Resource]
				if !ok {
					t.Skip("no ID available (list was empty or missing)")
					return
				}

				path := strings.Replace(ep.Path, "{id}", id, 1)
				smokeGET(t, httpClient, path)
			})
		}
	})
}

// ---------------------------------------------------------------------------
// Tier 2: Field-path assertions for power commands
// ---------------------------------------------------------------------------

type tier2Check struct {
	Name       string
	Path       string
	FieldPaths []string // dot-separated, numeric indices for arrays
	IsClassic  bool
	WrapperKey string
}

var tier2Checks = []tier2Check{
	{
		Name:       "device-compliance/GENERAL+HARDWARE",
		Path:       "/v3/computers-inventory?section=GENERAL&section=HARDWARE&page-size=1",
		FieldPaths: []string{"results.0.general.name", "results.0.general.lastContactTime", "results.0.hardware.serialNumber"},
	},
	{
		Name:       "inventory-summary/HARDWARE+OS",
		Path:       "/v3/computers-inventory?section=HARDWARE&section=OPERATING_SYSTEM&page-size=1",
		FieldPaths: []string{"results.0.hardware.model", "results.0.operatingSystem.version"},
	},
	{
		Name:       "software-installs/APPLICATIONS",
		Path:       "/v3/computers-inventory?section=APPLICATIONS&page-size=1",
		FieldPaths: []string{"results.0.applications"},
	},
	{
		Name:       "ea-results/EXTENSION_ATTRIBUTES",
		Path:       "/v3/computers-inventory?section=EXTENSION_ATTRIBUTES&page-size=1",
		FieldPaths: []string{"results.0.extensionAttributes.0.definitionId", "results.0.extensionAttributes.0.name"},
	},
	{
		Name:       "classic-ea-definitions",
		Path:       "/JSSResource/computerextensionattributes",
		IsClassic:  true,
		WrapperKey: "computer_extension_attributes",
		FieldPaths: []string{"0.id", "0.name"},
	},
	{
		Name:       "jamf-pro-version",
		Path:       "/v1/jamf-pro-version",
		FieldPaths: []string{"version"},
	},
	{
		Name:       "check-in-settings",
		Path:       "/v3/check-in",
		FieldPaths: []string{"checkInFrequency"},
	},
	{
		Name:       "notifications",
		Path:       "/v1/notifications",
		FieldPaths: []string{}, // just verify it's valid JSON (array)
	},
}

func TestSmoke_Tier2(t *testing.T) {
	if os.Getenv("JAMF_SMOKE_TEST") == "" {
		t.Skip("set JAMF_SMOKE_TEST=1 to run smoke tests against a real Jamf Pro instance")
	}

	httpClient := smokeClient(t)

	for _, check := range tier2Checks {
		t.Run(check.Name, func(t *testing.T) {
			body, skipped := smokeGET(t, httpClient, check.Path)
			if skipped {
				return
			}

			var parsed any
			if check.IsClassic && check.WrapperKey != "" {
				var wrapper map[string]json.RawMessage
				if err := json.Unmarshal(body, &wrapper); err != nil {
					t.Fatalf("unmarshal wrapper: %v", err)
				}
				inner, ok := wrapper[check.WrapperKey]
				if !ok {
					t.Fatalf("wrapper key %q not found in response", check.WrapperKey)
				}
				if err := json.Unmarshal(inner, &parsed); err != nil {
					t.Fatalf("unmarshal inner: %v", err)
				}
			} else {
				if err := json.Unmarshal(body, &parsed); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
			}

			for _, fp := range check.FieldPaths {
				val, ok := resolveFieldPath(parsed, fp)
				if !ok {
					t.Errorf("field path %q not found in response", fp)
					continue
				}
				if val == nil {
					t.Errorf("field path %q is nil", fp)
				}
			}
		})
	}
}

// resolveFieldPath walks a dot-separated path through nested maps and arrays.
// Numeric path segments index into arrays.
func resolveFieldPath(data any, path string) (any, bool) {
	parts := strings.Split(path, ".")
	current := data
	for _, part := range parts {
		switch v := current.(type) {
		case map[string]any:
			val, ok := v[part]
			if !ok {
				return nil, false
			}
			current = val
		case []any:
			idx, err := strconv.Atoi(part)
			if err != nil || idx < 0 || idx >= len(v) {
				return nil, false
			}
			current = v[idx]
		default:
			return nil, false
		}
	}
	return current, true
}
