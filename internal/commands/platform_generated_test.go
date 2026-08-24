// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	platformgen "github.com/Jamf-Concepts/jamf-cli/internal/commands/platform/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// TestGeneratedBlueprintsGet validates that `blueprints get` returns the full
// blueprint object — not just the steps array. Regression for the bug where
// detectListArrayKey fired on non-list ops and stripped the response.
func TestGeneratedBlueprintsGet(t *testing.T) {
	sdk, mux := newTestPlatformSDK(t)

	const blueprintID = "bp-123"
	wantPath := "/api/blueprints/v1/tenant/" + testTenantID + "/blueprints/" + blueprintID
	mux.HandleFunc(wantPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"id":              blueprintID,
			"name":            "My Blueprint",
			"deploymentState": map[string]any{"state": "deployed"},
			"steps":           []any{map[string]any{"id": "step-1"}},
		})
	})

	cliCtx, _, out := newTestPlatformContext(t)
	_ = mux // already registered above via sdk's server
	cliCtx.PlatformSDKClient = sdk
	cliCtx.Output = out

	cmd := platformgen.NewBlueprintsCmd(cliCtx)
	cmd.SetArgs([]string{"get", blueprintID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("blueprints get: %v", err)
	}

	body := string(out.rawData)
	// Full object fields must be present.
	if !strings.Contains(body, `"id"`) || !strings.Contains(body, `"name"`) || !strings.Contains(body, `"deploymentState"`) {
		t.Errorf("get response missing top-level fields: %s", body)
	}
	// Must not be unwrapped to just the steps array.
	if !strings.Contains(body, `"steps"`) || strings.HasPrefix(strings.TrimSpace(body), "[") {
		t.Errorf("get response looks like unwrapped steps array instead of full object: %s", body)
	}
}

// TestGeneratedDeviceGroupsGet validates that `platform-device-groups get` returns the
// full group object — not just the criteria array. Same regression as blueprints.
func TestGeneratedPlatformDeviceGroupsGet(t *testing.T) {
	sdk, mux := newTestPlatformSDK(t)

	const groupID = "dg-456"
	wantPath := "/api/device-groups/v1/tenant/" + testTenantID + "/device-groups/" + groupID
	mux.HandleFunc(wantPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"id":          groupID,
			"name":        "Test Group",
			"groupType":   "static",
			"memberCount": 5,
			"criteria":    []any{map[string]any{"field": "serialNumber", "value": "ABC"}},
		})
	})

	cliCtx, _, out := newTestPlatformContext(t)
	cliCtx.PlatformSDKClient = sdk
	cliCtx.Output = out

	cmd := platformgen.NewPlatformDeviceGroupsCmd(cliCtx)
	cmd.SetArgs([]string{"get", groupID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("device-groups get: %v", err)
	}

	body := string(out.rawData)
	if !strings.Contains(body, `"id"`) || !strings.Contains(body, `"name"`) || !strings.Contains(body, `"memberCount"`) {
		t.Errorf("get response missing top-level fields: %s", body)
	}
	if strings.HasPrefix(strings.TrimSpace(body), "[") {
		t.Errorf("get response is an array instead of full object: %s", body)
	}
}

// TestGeneratedDeviceActionsUnmanageRequiresConfirm validates that `device-actions
// unmanage` is treated as destructive and rejects without --yes in non-TTY mode.
func TestGeneratedDeviceActionsUnmanageRequiresConfirm(t *testing.T) {
	sdk, _ := newTestPlatformSDK(t)

	cliCtx := &registry.CLIContext{PlatformSDKClient: sdk, Output: &captureOutput{}}
	cmd := platformgen.NewDeviceActionsCmd(cliCtx)
	cmd.SetArgs([]string{"unmanage", "device-id-1"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("unmanage without --yes in non-TTY should return an error")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("error should mention --yes, got: %v", err)
	}
}

// TestGeneratedCommandNilClientError validates that generated commands return
// the full setup guidance (not a bare one-liner) when PlatformSDKClient is nil.
func TestGeneratedCommandNilClientError(t *testing.T) {
	cliCtx := &registry.CLIContext{PlatformSDKClient: nil, Output: &captureOutput{}}
	cmd := platformgen.NewBlueprintsCmd(cliCtx)
	cmd.SetArgs([]string{"list"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when PlatformSDKClient is nil")
	}
	if !strings.Contains(err.Error(), "config add-profile") {
		t.Errorf("error should contain setup instructions, got: %v", err)
	}
}

// TestGeneratedBaselinesList exercises the spec-generated `pro baselines list`
// command end-to-end using an httptest-backed SDK client. Validates that
// generated code builds the expected URL (with tenant injection), parses the
// envelope ({baselines: [...]}), and unwraps to a flat array.
func TestGeneratedBaselinesList(t *testing.T) {
	sdk, mux := newTestPlatformSDK(t)

	const wantPath = "/api/compliance-benchmarks/v1/tenant/" + testTenantID + "/baselines"
	var seenPath string
	mux.HandleFunc(wantPath, func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		writeJSON(w, map[string]any{
			"baselines": []map[string]any{
				{"id": "cis_lvl1", "title": "CIS Level 1", "ruleCount": 107},
				{"id": "cis_lvl2", "title": "CIS Level 2", "ruleCount": 130},
			},
		})
	})

	out := &captureOutput{}
	cliCtx := &registry.CLIContext{
		PlatformSDKClient: sdk,
		Output:            out,
	}

	cmd := platformgen.NewBaselinesCmd(cliCtx)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list baselines: %v", err)
	}

	if seenPath != wantPath {
		t.Errorf("server saw path %q, want %q", seenPath, wantPath)
	}
	body := string(out.rawData)
	if !strings.Contains(body, "cis_lvl1") || !strings.Contains(body, "cis_lvl2") {
		t.Errorf("output missing expected baselines: %s", body)
	}
	// List unwrapping should emit a flat array (no envelope key).
	if strings.Contains(body, `"baselines"`) {
		t.Errorf("output should be unwrapped flat array, still has envelope: %s", body)
	}
}

// TestGeneratedRulesListWithQueryParam validates that a required query flag
// (--baseline-id) round-trips through the generator's flag → query-string path.
func TestGeneratedRulesListWithQueryParam(t *testing.T) {
	sdk, mux := newTestPlatformSDK(t)

	var seenQuery string
	mux.HandleFunc("/api/compliance-benchmarks/v1/tenant/"+testTenantID+"/rules", func(w http.ResponseWriter, r *http.Request) {
		seenQuery = r.URL.RawQuery
		writeJSON(w, map[string]any{
			"rules":   []map[string]any{{"id": "rule-1", "title": "Some rule"}},
			"sources": []map[string]any{{"branch": "main"}},
		})
	})

	out := &captureOutput{}
	cliCtx := &registry.CLIContext{PlatformSDKClient: sdk, Output: out}
	cmd := platformgen.NewRulesCmd(cliCtx)
	cmd.SetArgs([]string{"list", "--baseline-id", "cis_lvl1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list rules: %v", err)
	}
	// Assert the exact wire key, not merely that some key round-tripped. The
	// spec renamed this parameter to kebab-case; while the CLI still sent
	// "baselineId" the server ignored it and `pro rules list` returned an empty
	// list for every baseline — 0 rules where "baseline-id" returns 110. The
	// old assertion passed throughout, because it checked the generator's
	// flag→query plumbing against whatever the stale spec happened to declare.
	if !strings.Contains(seenQuery, "baseline-id=cis_lvl1") {
		t.Errorf("query string missing baseline-id: %q", seenQuery)
	}
	if strings.Contains(seenQuery, "baselineId") {
		t.Errorf("sent the camelCase parameter the server ignores: %q", seenQuery)
	}
}

// TestGeneratedSecurityCloudListIsTenantFirstAndEmptyIsAnArray covers two things
// that are only visible at runtime, on one request.
//
// The path: Security Cloud puts /tenant/{id} ahead of the version, and the
// generated command holds that path as a literal — so the SDK getting the
// ordering right in TenantPrefix does nothing for it. An exact mux registration
// is what catches a regression, since both orderings are routed by the real
// gateway and only the audit rules can tell them apart.
//
// The body: the paginated list path aggregates into a slice, and a nil slice
// marshals to "null". An empty collection therefore used to print "null" while
// the unpaginated path printed "[]" for the identical wire response, which broke
// any jq consumer on exactly the tenants where the collection was empty.
func TestGeneratedSecurityCloudListIsTenantFirstAndEmptyIsAnArray(t *testing.T) {
	sdk, mux := newTestPlatformSDK(t)

	const wantPath = "/api/securitycloud/tenant/" + testTenantID + "/v1/ztna/gateways"
	var seenPath string
	mux.HandleFunc(wantPath, func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		// The empty state every Security Cloud collection answers with.
		writeJSON(w, map[string]any{"totalCount": 0, "results": []any{}})
	})

	out := &captureOutput{}
	cliCtx := &registry.CLIContext{PlatformSDKClient: sdk, Output: out}
	cmd := platformgen.NewZtnaGatewaysCmd(cliCtx)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("ztna-gateways list: %v", err)
	}

	if seenPath != wantPath {
		t.Errorf("server saw path %q, want %q", seenPath, wantPath)
	}
	if got := strings.TrimSpace(string(out.rawData)); got != "[]" {
		t.Errorf("empty list printed %q, want %q", got, "[]")
	}
}

// TestGeneratedPlatformMutationsHonourDryRun pins the P0 that --dry-run used to
// be a lie outside Jamf Pro. The root flag is advertised as "preview changes
// without executing" and is wired by wrapping the Pro HTTPClient in
// dryRunClient — a decorator the Platform SDK client and the Security Cloud
// client never pass through. So every generated platform and gateway-served
// Security Cloud mutation executed for real under -n: a create returned the
// object it had just made, a delete deleted and reported nothing, both exiting
// 0. Anyone could destroy production data believing they were simulating.
//
// The assertion is that the server is never touched, not merely that the output
// looks like a preview.
func TestGeneratedPlatformMutationsHonourDryRun(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		newCmd  func(*registry.CLIContext) *cobra.Command
		args    []string
		wantPre string
	}{
		{
			name:    "create",
			path:    "/api/securitycloud/tenant/" + testTenantID + "/v1/groups",
			newCmd:  platformgen.NewDeviceGroupsCmd,
			args:    []string{"create", "--set", "name=dry-run-probe"},
			wantPre: "[dry-run] POST /api/securitycloud/tenant/" + testTenantID + "/v1/groups",
		},
		{
			name:    "delete",
			path:    "/api/securitycloud/tenant/" + testTenantID + "/v1/groups/abc123",
			newCmd:  platformgen.NewDeviceGroupsCmd,
			args:    []string{"delete", "abc123", "--yes"},
			wantPre: "[dry-run] DELETE /api/securitycloud/tenant/" + testTenantID + "/v1/groups/abc123",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, mux := newTestPlatformSDK(t)
			var hits int
			mux.HandleFunc(tc.path, func(w http.ResponseWriter, _ *http.Request) {
				hits++
				writeJSON(w, map[string]any{"id": "abc123"})
			})

			cliCtx := &registry.CLIContext{PlatformSDKClient: sdk, Output: &captureOutput{}, DryRun: true}
			cmd := tc.newCmd(cliCtx)
			var stderr bytes.Buffer
			cmd.SetErr(&stderr)
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if hits != 0 {
				t.Errorf("server saw %d request(s) under --dry-run, want 0", hits)
			}
			if got := stderr.String(); !strings.Contains(got, tc.wantPre) {
				t.Errorf("stderr = %q, want it to contain %q", got, tc.wantPre)
			}
		})
	}
}

// TestDryRunGuardRefusesUnpreviewedWrites covers the backstop for the
// hand-written platform commands, which orchestrate several SDK calls and have
// no per-command preview. Refusing at the transport is the conservative
// reading: nothing is sent and the exit code is non-zero, rather than writing
// under a flag that promised otherwise.
//
// It answers with a 412 rather than a transport error on purpose. The SDK's
// retry client treats a nil response as always retryable, so refusing by error
// made -n hang through the full backoff ladder before reporting anything.
func TestDryRunGuardRefusesUnpreviewedWrites(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		path       string
		wantPassed bool
	}{
		{name: "post refused", method: http.MethodPost, path: "/api/securitycloud/tenant/t/v1/groups"},
		{name: "patch refused", method: http.MethodPatch, path: "/api/securitycloud/tenant/t/v1/groups/1"},
		{name: "delete refused", method: http.MethodDelete, path: "/api/securitycloud/tenant/t/v1/groups/1"},
		{name: "get passes", method: http.MethodGet, path: "/api/securitycloud/tenant/t/v1/groups", wantPassed: true},
		{name: "token passes", method: http.MethodPost, path: "/auth/token", wantPassed: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inner := &recordingRoundTripper{}
			rt := &dryRunGuardTransport{inner: inner}
			req, err := http.NewRequest(tc.method, "https://gw.example.com"+tc.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := rt.RoundTrip(req)
			if err != nil {
				t.Fatalf("RoundTrip returned an error (%v); a nil response is retried by the SDK, so the guard must answer with a status", err)
			}
			if tc.wantPassed {
				if inner.calls != 1 {
					t.Errorf("inner transport called %d time(s), want 1", inner.calls)
				}
				return
			}
			if inner.calls != 0 {
				t.Errorf("inner transport called %d time(s) for a refused write, want 0", inner.calls)
			}
			if resp.StatusCode != http.StatusPreconditionFailed {
				t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusPreconditionFailed)
			}
			body, _ := io.ReadAll(resp.Body)
			if !strings.Contains(string(body), "DRY_RUN") {
				t.Errorf("body = %q, want it to name DRY_RUN", body)
			}
		})
	}
}

// TestIdentityEncodingOnWrites covers the workaround for a gateway bug: a
// gzipped create response comes back with "href": null and no Location header,
// and an uncompressed one carries both. Go asks for gzip on every request, so
// every create through this CLI saw null for a field the schema declares
// required. Reads keep gzip — a full list is where it earns its keep.
func TestIdentityEncodingOnWrites(t *testing.T) {
	cases := []struct {
		method string
		want   string
	}{
		{http.MethodPost, "identity"},
		{http.MethodPut, "identity"},
		{http.MethodPatch, "identity"},
		{http.MethodGet, ""},
		{http.MethodDelete, ""},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			inner := &recordingRoundTripper{}
			rt := &identityEncodingOnWrites{inner: inner}
			req, err := http.NewRequest(tc.method, "https://gw.example.com/x", nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := rt.RoundTrip(req); err != nil {
				t.Fatal(err)
			}
			if got := inner.lastHeader.Get("Accept-Encoding"); got != tc.want {
				t.Errorf("Accept-Encoding = %q, want %q", got, tc.want)
			}
		})
	}
}

// recordingRoundTripper counts calls and keeps the last request's headers.
type recordingRoundTripper struct {
	calls      int
	lastHeader http.Header
}

func (r *recordingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.calls++
	r.lastHeader = req.Header.Clone()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("{}")),
		Request:    req,
	}, nil
}
