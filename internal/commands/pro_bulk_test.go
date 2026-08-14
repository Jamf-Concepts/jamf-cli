// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// ─────────────────────────────────────────────────────────────────
// Test helpers / mock client
// ─────────────────────────────────────────────────────────────────

// bulkMockClient records calls and serves canned responses.
type bulkMockClient struct {
	responses map[string]overviewMockResponse // key: "METHOD /path"
	// filterResponses is substring-matched (longest pattern wins) against the
	// query-unescaped "METHOD /path?query", so a test can distinguish the
	// batched RSQL lookups that share one base path — e.g.
	// `filter=id=in=(7)` vs `filter=hardware.serialNumber=in=("FVFC41HCLYWP")`.
	filterResponses map[string]overviewMockResponse
	calls           []string // recorded as "METHOD /path", query-unescaped
	bodies          []string // request bodies, parallel to calls
}

func (m *bulkMockClient) Do(_ context.Context, method, path string, body io.Reader) (*http.Response, error) {
	key := method + " " + path
	decoded := key
	if unescaped, err := url.QueryUnescape(key); err == nil {
		decoded = unescaped
	}
	m.calls = append(m.calls, decoded)
	bodyStr := ""
	if body != nil {
		b, _ := io.ReadAll(body)
		bodyStr = string(b)
	}
	m.bodies = append(m.bodies, bodyStr)

	// Filter-specific matches win over the base-path maps below.
	bestPattern := ""
	var bestResp overviewMockResponse
	for pattern, resp := range m.filterResponses {
		if len(pattern) > len(bestPattern) && strings.Contains(decoded, pattern) {
			bestPattern, bestResp = pattern, resp
		}
	}
	if bestPattern != "" {
		return &http.Response{
			StatusCode: bestResp.statusCode,
			Body:       io.NopCloser(strings.NewReader(bestResp.body)),
			Header:     make(http.Header),
		}, nil
	}

	// Exact match next
	if resp, ok := m.responses[key]; ok {
		return &http.Response{
			StatusCode: resp.statusCode,
			Body:       io.NopCloser(strings.NewReader(resp.body)),
			Header:     make(http.Header),
		}, nil
	}
	// GET with prefix match (strip query params)
	if method == "GET" {
		if before, _, ok := strings.Cut(path, "?"); ok {
			base := "GET " + before
			if resp, ok := m.responses[base]; ok {
				return &http.Response{
					StatusCode: resp.statusCode,
					Body:       io.NopCloser(strings.NewReader(resp.body)),
					Header:     make(http.Header),
				}, nil
			}
		}
	}
	return nil, fmt.Errorf("bulk mock: no response for %s", key)
}

func (m *bulkMockClient) hasMutatingCall() bool {
	for _, c := range m.calls {
		if strings.HasPrefix(c, "POST ") || strings.HasPrefix(c, "PUT ") ||
			strings.HasPrefix(c, "PATCH ") || strings.HasPrefix(c, "DELETE ") {
			return true
		}
	}
	return false
}

func (m *bulkMockClient) callsMatching(prefix string) []string {
	var out []string
	for _, c := range m.calls {
		if strings.Contains(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

// bodiesMatching returns the request bodies of calls whose "METHOD /path" matches prefix.
func (m *bulkMockClient) bodiesMatching(prefix string) []string {
	var out []string
	for i, c := range m.calls {
		if strings.Contains(c, prefix) {
			out = append(out, m.bodies[i])
		}
	}
	return out
}

// v3Computer builds one v3 computers-inventory record.
func v3Computer(id, name, serial string) string {
	return fmt.Sprintf(`{"id":%q,"udid":"udid-%s","general":{"name":%q},"hardware":{"serialNumber":%q}}`,
		id, id, name, serial)
}

// v3ComputerPage wraps inventory records in the paginated envelope the v3
// computers-inventory endpoint returns for a filtered query.
func v3ComputerPage(records ...string) string {
	return fmt.Sprintf(`{"totalCount":%d,"results":[%s]}`, len(records), strings.Join(records, ","))
}

// quarantineGroupResponses serves the "Quarantine" static group lookup and its
// membership PUT, the fixture shared by the --from-file group tests.
func quarantineGroupResponses() map[string]overviewMockResponse {
	return map[string]overviewMockResponse{
		"GET /JSSResource/computergroups": {200, `{"computer_groups":[
			{"id":200,"name":"Quarantine"}
		]}`},
		"GET /JSSResource/computergroups/id/200": {200, targetStaticGroupJSON},
		"PUT /JSSResource/computergroups/id/200": {200, `<computer_group/>`},
	}
}

// newBulkCLIContext builds a CLIContext backed by the given mock.
func newBulkCLIContext(mock *bulkMockClient) *registry.CLIContext {
	return &registry.CLIContext{Client: mock}
}

// runCobraCmd executes a cobra command with a pre-configured context and
// returns stderr output.  The command's stdout is discarded.
func runCobraCmd(t *testing.T, cmd *cobra.Command, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err = cmd.ExecuteContext(context.Background())
	return outBuf.String(), errBuf.String(), err
}

// ─────────────────────────────────────────────────────────────────
// policy list JSON helpers
// ─────────────────────────────────────────────────────────────────

const policyListJSON = `{"policies":[
  {"id":1,"name":"Deploy Chrome"},
  {"id":2,"name":"Install Zoom"},
  {"id":3,"name":"Security Baseline"}
]}`

func policyDetailJSON(id int, name string, enabled bool, category, groupName string) string {
	catJSON := fmt.Sprintf(`{"id":10,"name":"%s"}`, category)
	groupJSON := fmt.Sprintf(`{"id":20,"name":"%s"}`, groupName)
	enabledStr := "false"
	if enabled {
		enabledStr = "true"
	}
	// Classic API nests id, name, enabled, category under "general".
	return fmt.Sprintf(`{"policy":{"general":{"id":%d,"name":"%s","enabled":%s,"category":%s},"scope":{"computer_groups":[%s]}}}`,
		id, name, enabledStr, catJSON, groupJSON)
}

// ─────────────────────────────────────────────────────────────────
// Tests: disable-policies
// ─────────────────────────────────────────────────────────────────

func TestDisablePolicies_DryRunDefault(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/policies":      {200, policyListJSON},
			"GET /JSSResource/policies/id/1": {200, policyDetailJSON(1, "Deploy Chrome", true, "Apps", "All Computers")},
			"GET /JSSResource/policies/id/2": {200, policyDetailJSON(2, "Install Zoom", true, "Apps", "All Computers")},
			"GET /JSSResource/policies/id/3": {200, policyDetailJSON(3, "Security Baseline", true, "Security", "All Computers")},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	// Run without --yes — should NOT mutate
	cmd := newBulkCmd(cliCtx)
	_, stderr, err := runCobraCmd(t, cmd, "disable-policies")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.hasMutatingCall() {
		t.Errorf("dry-run should not issue any mutating calls; got: %v", mock.callsMatching("PUT"))
	}
	if !strings.Contains(stderr, "[dry-run]") {
		t.Errorf("expected [dry-run] in stderr, got: %q", stderr)
	}
}

func TestEnablePolicies_DryRunDefault(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/policies": {200, policyListJSON},
			// All disabled → enable-policies should want to enable them
			"GET /JSSResource/policies/id/1": {200, policyDetailJSON(1, "Deploy Chrome", false, "Apps", "All Computers")},
			"GET /JSSResource/policies/id/2": {200, policyDetailJSON(2, "Install Zoom", false, "Apps", "All Computers")},
			"GET /JSSResource/policies/id/3": {200, policyDetailJSON(3, "Security Baseline", false, "Security", "All Computers")},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, stderr, err := runCobraCmd(t, cmd, "enable-policies")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.hasMutatingCall() {
		t.Errorf("dry-run should not issue any mutating calls; got: %v", mock.callsMatching("PUT"))
	}
	if !strings.Contains(stderr, "[dry-run]") {
		t.Errorf("expected [dry-run] in stderr, got: %q", stderr)
	}
}

func TestDisablePolicies_YesDispatches(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/policies":      {200, policyListJSON},
			"GET /JSSResource/policies/id/1": {200, policyDetailJSON(1, "Deploy Chrome", true, "Apps", "All Computers")},
			"GET /JSSResource/policies/id/2": {200, policyDetailJSON(2, "Install Zoom", true, "Apps", "All Computers")},
			"GET /JSSResource/policies/id/3": {200, policyDetailJSON(3, "Security Baseline", true, "Security", "All Computers")},
			// Accept any PUT
			"PUT /JSSResource/policies/id/1": {200, `<policy><general><enabled>false</enabled></general></policy>`},
			"PUT /JSSResource/policies/id/2": {200, `<policy><general><enabled>false</enabled></general></policy>`},
			"PUT /JSSResource/policies/id/3": {200, `<policy><general><enabled>false</enabled></general></policy>`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, stderr, err := runCobraCmd(t, cmd, "disable-policies", "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	puts := mock.callsMatching("PUT /JSSResource/policies/id/")
	if len(puts) == 0 {
		t.Errorf("expected PUT calls after --yes, got none")
	}
	if strings.Contains(stderr, "[dry-run]") {
		t.Errorf("should not print [dry-run] when --yes is set")
	}
}

func TestDisablePolicies_CategoryFilter(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/policies":      {200, policyListJSON},
			"GET /JSSResource/policies/id/1": {200, policyDetailJSON(1, "Deploy Chrome", true, "Apps", "All Computers")},
			"GET /JSSResource/policies/id/2": {200, policyDetailJSON(2, "Install Zoom", true, "Apps", "All Computers")},
			"GET /JSSResource/policies/id/3": {200, policyDetailJSON(3, "Security Baseline", true, "Security", "All Computers")},
			"PUT /JSSResource/policies/id/1": {200, `<policy/>`},
			"PUT /JSSResource/policies/id/2": {200, `<policy/>`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(t, cmd, "disable-policies", "--yes", "--category", "Apps")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only policy 1 and 2 are in "Apps" → 2 PUTs expected
	puts := mock.callsMatching("PUT /JSSResource/policies/id/")
	if len(puts) != 2 {
		t.Errorf("expected 2 PUTs (Apps filter), got %d: %v", len(puts), puts)
	}
	// Policy 3 (Security) should NOT be touched
	for _, c := range puts {
		if strings.Contains(c, "/id/3") {
			t.Error("policy 3 (Security) should not be disabled when filtering by Apps")
		}
	}
}

func TestDisablePolicies_NamePatternFilter(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/policies": {200, `{"policies":[
				{"id":1,"name":"Deploy Chrome"},
				{"id":2,"name":"Deploy Firefox"},
				{"id":3,"name":"Security Baseline"}
			]}`},
			"GET /JSSResource/policies/id/1": {200, policyDetailJSON(1, "Deploy Chrome", true, "Apps", "All Computers")},
			"GET /JSSResource/policies/id/2": {200, policyDetailJSON(2, "Deploy Firefox", true, "Apps", "All Computers")},
			"PUT /JSSResource/policies/id/1": {200, `<policy/>`},
			"PUT /JSSResource/policies/id/2": {200, `<policy/>`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(t, cmd, "disable-policies", "--yes", "--name-pattern", "Deploy *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	puts := mock.callsMatching("PUT /JSSResource/policies/id/")
	if len(puts) != 2 {
		t.Errorf("expected 2 PUTs (Deploy * pattern), got %d: %v", len(puts), puts)
	}
	// Policy 3 must not be touched
	for _, c := range puts {
		if strings.Contains(c, "/id/3") {
			t.Errorf("policy 3 (Security Baseline) should not match Deploy * pattern")
		}
	}
}

func TestDisablePolicies_ScopeGroupFilter(t *testing.T) {
	// Classic API XML without <size> produces {"computer_group": [...]} not a flat array.
	scopeDetail := func(id int, name string, groupName string) string {
		return fmt.Sprintf(`{"policy":{"general":{"id":%d,"name":"%s","enabled":true,"category":{"id":10,"name":"Apps"}},"scope":{"computer_groups":{"computer_group":{"id":20,"name":"%s"}}}}}`,
			id, name, groupName)
	}
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/policies": {200, `{"policies":[
				{"id":1,"name":"Policy A"},
				{"id":2,"name":"Policy B"},
				{"id":3,"name":"Policy C"}
			]}`},
			"GET /JSSResource/policies/id/1": {200, scopeDetail(1, "Policy A", "Lab Macs")},
			"GET /JSSResource/policies/id/2": {200, scopeDetail(2, "Policy B", "All Computers")},
			"GET /JSSResource/policies/id/3": {200, scopeDetail(3, "Policy C", "Lab Macs")},
			"PUT /JSSResource/policies/id/1": {200, `<policy/>`},
			"PUT /JSSResource/policies/id/3": {200, `<policy/>`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(t, cmd, "disable-policies", "--yes", "--scope-group", "Lab Macs")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	puts := mock.callsMatching("PUT /JSSResource/policies/id/")
	if len(puts) != 2 {
		t.Errorf("expected 2 PUTs (Lab Macs scope), got %d: %v", len(puts), puts)
	}
	for _, c := range puts {
		if strings.Contains(c, "/id/2") {
			t.Error("policy 2 (All Computers) should not be disabled when filtering by Lab Macs")
		}
	}
}

// fullPolicyDetailJSON builds a policy detail JSON with targets, limitations, and exclusions.
func fullPolicyDetailJSON(id int, name string, enabled bool, opts map[string]any) string {
	enabledStr := "false"
	if enabled {
		enabledStr = "true"
	}

	category := "General"
	if c, ok := opts["category"].(string); ok {
		category = c
	}
	allComputers := "false"
	if ac, ok := opts["all_computers"].(bool); ok && ac {
		allComputers = "true"
	}

	// Build scope sections from opts
	targetGroups := buildScopeSection(opts, "target_groups", "computer_group")
	targetBuildings := buildScopeSection(opts, "target_buildings", "building")
	targetDepartments := buildScopeSection(opts, "target_departments", "department")
	limitNetSegments := buildScopeSection(opts, "limit_network_segments", "network_segment")
	limitUserGroups := buildScopeSection(opts, "limit_user_groups", "user_group")
	excludeGroups := buildScopeSection(opts, "exclude_groups", "computer_group")
	excludeBuildings := buildScopeSection(opts, "exclude_buildings", "building")
	excludeDepartments := buildScopeSection(opts, "exclude_departments", "department")

	return fmt.Sprintf(`{"policy":{
		"general":{"id":%d,"name":"%s","enabled":%s,"category":{"id":10,"name":"%s"}},
		"scope":{
			"all_computers":%s,
			"computer_groups":%s,
			"buildings":%s,
			"departments":%s,
			"limitations":{
				"network_segments":%s,
				"user_groups":%s
			},
			"exclusions":{
				"computer_groups":%s,
				"buildings":%s,
				"departments":%s
			}
		}
	}}`, id, name, enabledStr, category, allComputers,
		targetGroups, targetBuildings, targetDepartments,
		limitNetSegments, limitUserGroups,
		excludeGroups, excludeBuildings, excludeDepartments)
}

// buildScopeSection builds a JSON scope section from an opts map entry.
// The entry should be a []string of names. Uses the wrapped map form (no <size>).
func buildScopeSection(opts map[string]any, key, childKey string) string {
	names, _ := opts[key].([]string)
	if len(names) == 0 {
		return `""`
	}
	var items []string
	for i, n := range names {
		items = append(items, fmt.Sprintf(`{"id":%d,"name":"%s"}`, i+100, n))
	}
	if len(items) == 1 {
		return fmt.Sprintf(`{"%s":%s}`, childKey, items[0])
	}
	return fmt.Sprintf(`{"%s":[%s]}`, childKey, strings.Join(items, ","))
}

func TestDisablePolicies_ScopeGroupMultiple(t *testing.T) {
	// --scope-group "Lab" --scope-group "Dev" should match policies scoped to EITHER group.
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/policies": {200, `{"policies":[
				{"id":1,"name":"Policy A"},
				{"id":2,"name":"Policy B"},
				{"id":3,"name":"Policy C"}
			]}`},
			"GET /JSSResource/policies/id/1": {200, fullPolicyDetailJSON(1, "Policy A", true, map[string]any{
				"target_groups": []string{"Lab Macs"},
			})},
			"GET /JSSResource/policies/id/2": {200, fullPolicyDetailJSON(2, "Policy B", true, map[string]any{
				"target_groups": []string{"Dev Team"},
			})},
			"GET /JSSResource/policies/id/3": {200, fullPolicyDetailJSON(3, "Policy C", true, map[string]any{
				"target_groups": []string{"QA Team"},
			})},
			"PUT /JSSResource/policies/id/1": {200, `<policy/>`},
			"PUT /JSSResource/policies/id/2": {200, `<policy/>`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(t, cmd, "disable-policies", "--yes",
		"--scope-group", "Lab Macs", "--scope-group", "Dev Team")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	puts := mock.callsMatching("PUT /JSSResource/policies/id/")
	if len(puts) != 2 {
		t.Errorf("expected 2 PUTs (Lab Macs OR Dev Team), got %d: %v", len(puts), puts)
	}
	for _, c := range puts {
		if strings.Contains(c, "/id/3") {
			t.Error("policy 3 (QA Team) should not match Lab Macs or Dev Team")
		}
	}
}

func TestDisablePolicies_ScopeBuildingFilter(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/policies": {200, `{"policies":[
				{"id":1,"name":"Policy A"},
				{"id":2,"name":"Policy B"}
			]}`},
			"GET /JSSResource/policies/id/1": {200, fullPolicyDetailJSON(1, "Policy A", true, map[string]any{
				"target_buildings": []string{"HQ"},
			})},
			"GET /JSSResource/policies/id/2": {200, fullPolicyDetailJSON(2, "Policy B", true, map[string]any{
				"target_buildings": []string{"Branch Office"},
			})},
			"PUT /JSSResource/policies/id/1": {200, `<policy/>`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(t, cmd, "disable-policies", "--yes", "--scope-building", "HQ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	puts := mock.callsMatching("PUT /JSSResource/policies/id/")
	if len(puts) != 1 {
		t.Errorf("expected 1 PUT (HQ building), got %d: %v", len(puts), puts)
	}
}

func TestDisablePolicies_AllComputersFilter(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/policies": {200, `{"policies":[
				{"id":1,"name":"Policy A"},
				{"id":2,"name":"Policy B"}
			]}`},
			"GET /JSSResource/policies/id/1": {200, fullPolicyDetailJSON(1, "Policy A", true, map[string]any{
				"all_computers": true,
			})},
			"GET /JSSResource/policies/id/2": {200, fullPolicyDetailJSON(2, "Policy B", true, map[string]any{
				"target_groups": []string{"Lab Macs"},
			})},
			"PUT /JSSResource/policies/id/1": {200, `<policy/>`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(t, cmd, "disable-policies", "--yes", "--all-computers")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	puts := mock.callsMatching("PUT /JSSResource/policies/id/")
	if len(puts) != 1 {
		t.Errorf("expected 1 PUT (all-computers), got %d: %v", len(puts), puts)
	}
	if len(puts) > 0 && !strings.Contains(puts[0], "/id/1") {
		t.Errorf("expected policy 1 to match, got: %v", puts)
	}
}

func TestDisablePolicies_ExcludeGroupFilter(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/policies": {200, `{"policies":[
				{"id":1,"name":"Policy A"},
				{"id":2,"name":"Policy B"}
			]}`},
			"GET /JSSResource/policies/id/1": {200, fullPolicyDetailJSON(1, "Policy A", true, map[string]any{
				"exclude_groups": []string{"Excluded Devices"},
			})},
			"GET /JSSResource/policies/id/2": {200, fullPolicyDetailJSON(2, "Policy B", true, map[string]any{
				"exclude_groups": []string{"Test Machines"},
			})},
			"PUT /JSSResource/policies/id/1": {200, `<policy/>`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(t, cmd, "disable-policies", "--yes", "--exclude-group", "Excluded Devices")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	puts := mock.callsMatching("PUT /JSSResource/policies/id/")
	if len(puts) != 1 {
		t.Errorf("expected 1 PUT (Excluded Devices), got %d: %v", len(puts), puts)
	}
}

func TestDisablePolicies_LimitNetworkSegmentFilter(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/policies": {200, `{"policies":[
				{"id":1,"name":"Policy A"},
				{"id":2,"name":"Policy B"}
			]}`},
			"GET /JSSResource/policies/id/1": {200, fullPolicyDetailJSON(1, "Policy A", true, map[string]any{
				"limit_network_segments": []string{"Office WiFi"},
			})},
			"GET /JSSResource/policies/id/2": {200, fullPolicyDetailJSON(2, "Policy B", true, map[string]any{
				"limit_network_segments": []string{"VPN"},
			})},
			"PUT /JSSResource/policies/id/1": {200, `<policy/>`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(t, cmd, "disable-policies", "--yes", "--limit-network-segment", "Office WiFi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	puts := mock.callsMatching("PUT /JSSResource/policies/id/")
	if len(puts) != 1 {
		t.Errorf("expected 1 PUT (Office WiFi), got %d: %v", len(puts), puts)
	}
}

func TestDisablePolicies_CombinedANDFilters(t *testing.T) {
	// --scope-group "All Managed" --category "Apps" — only policies matching BOTH.
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/policies": {200, `{"policies":[
				{"id":1,"name":"Policy A"},
				{"id":2,"name":"Policy B"},
				{"id":3,"name":"Policy C"}
			]}`},
			"GET /JSSResource/policies/id/1": {200, fullPolicyDetailJSON(1, "Policy A", true, map[string]any{
				"category":      "Apps",
				"target_groups": []string{"All Managed"},
			})},
			"GET /JSSResource/policies/id/2": {200, fullPolicyDetailJSON(2, "Policy B", true, map[string]any{
				"category":      "Security",
				"target_groups": []string{"All Managed"},
			})},
			"GET /JSSResource/policies/id/3": {200, fullPolicyDetailJSON(3, "Policy C", true, map[string]any{
				"category":      "Apps",
				"target_groups": []string{"Dev Team"},
			})},
			"PUT /JSSResource/policies/id/1": {200, `<policy/>`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(t, cmd, "disable-policies", "--yes",
		"--scope-group", "All Managed", "--category", "Apps")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	puts := mock.callsMatching("PUT /JSSResource/policies/id/")
	if len(puts) != 1 {
		t.Errorf("expected 1 PUT (AND: Apps + All Managed), got %d: %v", len(puts), puts)
	}
	if len(puts) > 0 && !strings.Contains(puts[0], "/id/1") {
		t.Errorf("expected policy 1 to match, got: %v", puts)
	}
}

func TestDisablePolicies_AlreadyDisabledSkipped(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/policies": {200, `{"policies":[{"id":1,"name":"Deploy Chrome"}]}`},
			// Already disabled
			"GET /JSSResource/policies/id/1": {200, policyDetailJSON(1, "Deploy Chrome", false, "Apps", "All Computers")},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, stderr, err := runCobraCmd(t, cmd, "disable-policies", "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.hasMutatingCall() {
		t.Error("already-disabled policy should not generate a PUT")
	}
	if !strings.Contains(stderr, "No policies require changes") {
		t.Errorf("expected 'No policies require changes' message, got: %q", stderr)
	}
}

// ─────────────────────────────────────────────────────────────────
// Tests: add-to-group / remove-from-group
// ─────────────────────────────────────────────────────────────────

const computerGroupsListJSON = `{"computer_groups":[
  {"id":100,"name":"Lab Macs"},
  {"id":101,"name":"DevTeam"}
]}`

const staticGroupDetailJSON = `{"computer_group":{"id":100,"name":"Lab Macs","is_smart":false,"computers":{"computer":[
  {"id":1,"name":"Mac-01"},
  {"id":2,"name":"Mac-02"}
]}}}`

const targetStaticGroupJSON = `{"computer_group":{"id":200,"name":"Quarantine","is_smart":false,"computers":{"computer":[]}}}`

func TestAddToGroup_DryRunDefault(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			// lookup source group members
			"GET /JSSResource/computergroups":        {200, computerGroupsListJSON},
			"GET /JSSResource/computergroups/id/100": {200, staticGroupDetailJSON},
			// fetch computer names
			"GET /JSSResource/computers/id/1": {200, `{"computer":{"id":1,"name":"Mac-01"}}`},
			"GET /JSSResource/computers/id/2": {200, `{"computer":{"id":2,"name":"Mac-02"}}`},
			// lookup target static group
			"GET /JSSResource/computergroups/id/200": {200, targetStaticGroupJSON},
		},
	}
	// Add "Quarantine" to the group list so lookupStaticGroupID can find it
	mock.responses["GET /JSSResource/computergroups"] = overviewMockResponse{
		200,
		`{"computer_groups":[{"id":100,"name":"Lab Macs"},{"id":200,"name":"Quarantine"}]}`,
	}

	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, stderr, err := runCobraCmd(
		t, cmd, "add-to-group",
		"--target-group", "Quarantine",
		"--group", "Lab Macs",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.hasMutatingCall() {
		t.Errorf("dry-run should not issue mutating calls; got: %v", mock.callsMatching("PUT"))
	}
	if !strings.Contains(stderr, "[dry-run]") {
		t.Errorf("expected [dry-run] in stderr, got: %q", stderr)
	}
}

func TestAddToGroup_YesDispatches(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/computergroups": {200, `{"computer_groups":[
				{"id":100,"name":"Lab Macs"},
				{"id":200,"name":"Quarantine"}
			]}`},
			"GET /JSSResource/computergroups/id/100": {200, staticGroupDetailJSON},
			"GET /JSSResource/computergroups/id/200": {200, targetStaticGroupJSON},
			"GET /JSSResource/computers/id/1":        {200, `{"computer":{"id":1,"name":"Mac-01"}}`},
			"GET /JSSResource/computers/id/2":        {200, `{"computer":{"id":2,"name":"Mac-02"}}`},
			"PUT /JSSResource/computergroups/id/200": {200, `<computer_group/>`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(
		t, cmd, "add-to-group",
		"--target-group", "Quarantine",
		"--group", "Lab Macs",
		"--yes",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	puts := mock.callsMatching("PUT /JSSResource/computergroups/id/200")
	if len(puts) == 0 {
		t.Error("expected PUT calls after --yes, got none")
	}
}

func TestRemoveFromGroup_YesDispatches(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/computergroups": {200, `{"computer_groups":[
				{"id":100,"name":"Lab Macs"},
				{"id":200,"name":"Quarantine"}
			]}`},
			"GET /JSSResource/computergroups/id/100": {200, staticGroupDetailJSON},
			"GET /JSSResource/computergroups/id/200": {200, targetStaticGroupJSON},
			"GET /JSSResource/computers/id/1":        {200, `{"computer":{"id":1,"name":"Mac-01"}}`},
			"GET /JSSResource/computers/id/2":        {200, `{"computer":{"id":2,"name":"Mac-02"}}`},
			"PUT /JSSResource/computergroups/id/200": {200, `<computer_group/>`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(
		t, cmd, "remove-from-group",
		"--target-group", "Quarantine",
		"--group", "Lab Macs",
		"--yes",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	puts := mock.callsMatching("PUT /JSSResource/computergroups/id/200")
	if len(puts) == 0 {
		t.Error("expected PUT calls after --yes, got none")
	}
}

func TestAddToGroup_SmartGroupRejected(t *testing.T) {
	smartGroupJSON := `{"computer_group":{"id":200,"name":"SmartTarget","is_smart":true,"computers":{"computer":[]}}}`
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/computergroups": {200, `{"computer_groups":[
				{"id":100,"name":"Lab Macs"},
				{"id":200,"name":"SmartTarget"}
			]}`},
			"GET /JSSResource/computergroups/id/100": {200, staticGroupDetailJSON},
			"GET /JSSResource/computergroups/id/200": {200, smartGroupJSON},
			"GET /JSSResource/computers/id/1":        {200, `{"computer":{"id":1,"name":"Mac-01"}}`},
			"GET /JSSResource/computers/id/2":        {200, `{"computer":{"id":2,"name":"Mac-02"}}`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(
		t, cmd, "add-to-group",
		"--target-group", "SmartTarget",
		"--group", "Lab Macs",
		"--yes",
	)
	if err == nil {
		t.Fatal("expected error when targeting a smart group, got nil")
		return
	}
	if !strings.Contains(err.Error(), "smart group") {
		t.Errorf("expected 'smart group' in error, got: %v", err)
	}
	if mock.hasMutatingCall() {
		t.Error("should not make mutating calls when the target is a smart group")
	}
}

// ─────────────────────────────────────────────────────────────────
// Tests: from-file parsing
// ─────────────────────────────────────────────────────────────────

func TestAddToGroup_FromFile(t *testing.T) {
	// Mix a serial number with a numeric Classic ID — regression test for
	// the bug where a raw serial was sent straight through as the Classic
	// <id>, producing a 409 "Unable to match computer" (internal/resolve
	// now resolves serials to a real ID before this ever reaches the
	// Classic API).
	dir := t.TempDir()
	filePath := filepath.Join(dir, "computers.txt")
	content := "FVFC41HCLYWP\n# comment\n\n7\n"
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	mock := &bulkMockClient{
		responses: quarantineGroupResponses(),
		filterResponses: map[string]overviewMockResponse{
			// One batched RSQL query per identifier kind, not one per line.
			`filter=hardware.serialNumber=in=("FVFC41HCLYWP")`: {200, v3ComputerPage(v3Computer("5", "FVFC41HCLYWP", "FVFC41HCLYWP"))},
			`filter=id=in=(7)`: {200, v3ComputerPage(v3Computer("7", "Mac-07", "C02ABCDEF"))},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(
		t, cmd, "add-to-group",
		"--target-group", "Quarantine",
		"--from-file", filePath,
		"--yes",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 2 entries in file (blank line and comment are skipped) → 2 PUTs
	bodies := mock.bodiesMatching("PUT /JSSResource/computergroups/id/200")
	if len(bodies) != 2 {
		t.Fatalf("expected 2 PUTs (2 entries from file), got %d", len(bodies))
	}

	joined := strings.Join(bodies, "\n")
	if strings.Contains(joined, "<id>FVFC41HCLYWP</id>") {
		t.Error("raw serial number leaked into the Classic API <id> payload")
	}
	if !strings.Contains(joined, "<id>5</id>") {
		t.Errorf("expected the serial to resolve to computer id 5, got bodies: %v", bodies)
	}
	if !strings.Contains(joined, "<id>7</id>") {
		t.Errorf("expected numeric id 7 in PUT bodies, got: %v", bodies)
	}
}

// A file of numeric IDs must not cost one inventory lookup per line — the
// entries are resolved in a single batched RSQL query.
func TestAddToGroup_FromFile_BatchesLookups(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "computers.txt")
	if err := os.WriteFile(filePath, []byte("5\n7\n8\n"), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	mock := &bulkMockClient{
		responses: quarantineGroupResponses(),
		filterResponses: map[string]overviewMockResponse{
			`filter=id=in=(5,7,8)`: {200, v3ComputerPage(
				v3Computer("5", "Mac-05", "C02AAA"),
				v3Computer("7", "Mac-07", "C02BBB"),
				v3Computer("8", "Mac-08", "C02CCC"),
			)},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	if _, _, err := runCobraCmd(
		t, cmd, "add-to-group",
		"--target-group", "Quarantine",
		"--from-file", filePath,
		"--yes",
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lookups := mock.callsMatching("GET /v3/computers-inventory")
	if len(lookups) != 1 {
		t.Errorf("made %d inventory lookups for 3 IDs, want 1 batched query: %v", len(lookups), lookups)
	}
	if got := len(mock.bodiesMatching("PUT /JSSResource/computergroups/id/200")); got != 3 {
		t.Errorf("expected 3 PUTs, got %d", got)
	}
}

// One unresolvable entry must not abort the whole batch — the remaining
// entries are still mutated, matching the --group path's soft-fail behavior
// (and so --allow-partial-failure still means something for --from-file).
func TestAddToGroup_FromFile_PartialResolutionFailure(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "computers.txt")
	// "999" has no v3 inventory record in the mock → resolution fails.
	if err := os.WriteFile(filePath, []byte("FVFC41HCLYWP\n999\n7\n"), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	mock := &bulkMockClient{
		responses: quarantineGroupResponses(),
		filterResponses: map[string]overviewMockResponse{
			`filter=hardware.serialNumber=in=("FVFC41HCLYWP")`: {200, v3ComputerPage(v3Computer("5", "FVFC41HCLYWP", "FVFC41HCLYWP"))},
			// 999 has no inventory record, so the batched ID query returns only 7.
			`filter=id=in=(999,7)`: {200, v3ComputerPage(v3Computer("7", "Mac-07", "C02ABCDEF"))},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, stderr, err := runCobraCmd(
		t, cmd, "add-to-group",
		"--target-group", "Quarantine",
		"--from-file", filePath,
		"--yes",
	)
	// The unresolved entry counts as a failure alongside 2 successes, so the
	// batch reports partial failure (exit 7) rather than a clean exit 0.
	if err == nil {
		t.Fatal("expected a partial-failure error when one entry cannot be resolved")
	}
	if got := exitcode.CodeFrom(err); got != exitcode.PartialFailure {
		t.Errorf("exit code = %d, want %d (partial failure)", got, exitcode.PartialFailure)
	}
	if !strings.Contains(stderr, "1 unresolved") {
		t.Errorf("expected the summary line to name the unresolved entry, got: %q", stderr)
	}

	bodies := mock.bodiesMatching("PUT /JSSResource/computergroups/id/200")
	if len(bodies) != 2 {
		t.Fatalf("expected 2 PUTs (the 2 resolvable entries), got %d: %v", len(bodies), bodies)
	}
	joined := strings.Join(bodies, "\n")
	for _, want := range []string{"<id>5</id>", "<id>7</id>"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %s in PUT bodies, got: %v", want, bodies)
		}
	}
	if strings.Contains(joined, "<id>999</id>") {
		t.Error("unresolvable entry 999 should not have been sent to the Classic API")
	}
}

// --allow-partial-failure still silences a resolution failure, the same way it
// silences a mutation failure.
func TestAddToGroup_FromFile_PartialResolutionAllowed(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "computers.txt")
	if err := os.WriteFile(filePath, []byte("999\n7\n"), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	mock := &bulkMockClient{
		responses: quarantineGroupResponses(),
		filterResponses: map[string]overviewMockResponse{
			`filter=id=in=(999,7)`: {200, v3ComputerPage(v3Computer("7", "Mac-07", "C02ABCDEF"))},
		},
	}

	prev := allowPartialFailure
	allowPartialFailure = true
	t.Cleanup(func() { allowPartialFailure = prev })

	cmd := newBulkCmd(newBulkCLIContext(mock))
	if _, _, err := runCobraCmd(
		t, cmd, "add-to-group",
		"--target-group", "Quarantine",
		"--from-file", filePath,
		"--yes",
	); err != nil {
		t.Fatalf("--allow-partial-failure should tolerate an unresolved entry, got: %v", err)
	}
	if got := len(mock.bodiesMatching("PUT /JSSResource/computergroups/id/200")); got != 1 {
		t.Errorf("expected 1 PUT for the resolvable entry, got %d", got)
	}
}

// A file where nothing resolves must fail loudly — before this it printed
// "No target computers found." and exited 0, reading as a clean no-op batch.
func TestAddToGroup_FromFile_AllUnresolvableFails(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "computers.txt")
	if err := os.WriteFile(filePath, []byte("998\n999\n"), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	mock := &bulkMockClient{
		responses: quarantineGroupResponses(),
		filterResponses: map[string]overviewMockResponse{
			`filter=id=in=(998,999)`: {200, `{"totalCount":0,"results":[]}`},
		},
	}

	cmd := newBulkCmd(newBulkCLIContext(mock))
	_, _, err := runCobraCmd(
		t, cmd, "add-to-group",
		"--target-group", "Quarantine",
		"--from-file", filePath,
		"--yes",
	)
	if err == nil {
		t.Fatal("expected an error when no entry in the file resolves")
	}
	if !strings.Contains(err.Error(), "none of the 2 entries") {
		t.Errorf("error = %q, want it to report that no entry resolved", err.Error())
	}
	if mock.hasMutatingCall() {
		t.Error("no mutation should be attempted when nothing resolved")
	}
}

// A computer with no inventory name falls back to its ID for display.
func TestAddToGroup_FromFile_NameFallsBackToID(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "computers.txt")
	if err := os.WriteFile(filePath, []byte("8\n"), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	mock := &bulkMockClient{
		responses: quarantineGroupResponses(),
		filterResponses: map[string]overviewMockResponse{
			// No general.name → the target's display name should be "8".
			`filter=id=in=(8)`: {200, `{"totalCount":1,"results":[{
				"id":"8","udid":"u3",
				"general":{},
				"hardware":{"serialNumber":"C02NONAME"}
			}]}`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, stderr, err := runCobraCmd(
		t, cmd, "add-to-group",
		"--target-group", "Quarantine",
		"--from-file", filePath,
		"--yes",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stderr, "[bulk] add to group") || !strings.Contains(stderr, "8") {
		t.Errorf("expected mutation log to name the computer by its ID, got: %q", stderr)
	}
}

func TestFromFileMutualExclusion(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "ids.txt")
	_ = os.WriteFile(filePath, []byte("mac-01\n"), 0o600)

	mock := &bulkMockClient{responses: map[string]overviewMockResponse{}}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(
		t, cmd, "add-to-group",
		"--target-group", "Quarantine",
		"--from-file", filePath,
		"--group", "Lab Macs",
	)
	if err == nil {
		t.Fatal("expected error when both --from-file and --group are set")
		return
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected 'mutually exclusive' in error, got: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────
// Tests: send-command
// ─────────────────────────────────────────────────────────────────

func TestSendCommand_DryRunDefault(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/computergroups": {200, `{"computer_groups":[
				{"id":100,"name":"Lab Macs"}
			]}`},
			"GET /JSSResource/computergroups/id/100": {200, staticGroupDetailJSON},
			"GET /JSSResource/computers/id/1":        {200, `{"computer":{"id":1,"name":"Mac-01"}}`},
			"GET /JSSResource/computers/id/2":        {200, `{"computer":{"id":2,"name":"Mac-02"}}`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, stderr, err := runCobraCmd(
		t, cmd, "send-command",
		"--command", "BlankPush",
		"--group", "Lab Macs",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.hasMutatingCall() {
		t.Errorf("dry-run should not issue mutating calls; got: %v", mock.callsMatching("POST"))
	}
	if !strings.Contains(stderr, "[dry-run]") {
		t.Errorf("expected [dry-run] in stderr, got: %q", stderr)
	}
}

func TestSendCommand_YesDispatches(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/computergroups": {200, `{"computer_groups":[
				{"id":100,"name":"Lab Macs"}
			]}`},
			"GET /JSSResource/computergroups/id/100":                    {200, staticGroupDetailJSON},
			"GET /JSSResource/computers/id/1":                           {200, `{"computer":{"id":1,"name":"Mac-01"}}`},
			"GET /JSSResource/computers/id/2":                           {200, `{"computer":{"id":2,"name":"Mac-02"}}`},
			"POST /JSSResource/computercommands/command/BlankPush/id/1": {200, `<computer_command/>`},
			"POST /JSSResource/computercommands/command/BlankPush/id/2": {200, `<computer_command/>`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(
		t, cmd, "send-command",
		"--command", "BlankPush",
		"--group", "Lab Macs",
		"--yes",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	posts := mock.callsMatching("POST /JSSResource/computercommands/command/BlankPush")
	if len(posts) == 0 {
		t.Error("expected POST calls after --yes, got none")
	}
}

// send-command --from-file must resolve serials to numeric IDs before the
// Classic MDM command POST — the raw serial 409s ("Unable to match computer").
func TestSendCommand_FromFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "computers.txt")
	if err := os.WriteFile(filePath, []byte("FVFC41HCLYWP\n7\n"), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"POST /JSSResource/computercommands/command/BlankPush/id/5": {200, `<computer_command/>`},
			"POST /JSSResource/computercommands/command/BlankPush/id/7": {200, `<computer_command/>`},
		},
		filterResponses: map[string]overviewMockResponse{
			`filter=hardware.serialNumber=in=("FVFC41HCLYWP")`: {200, v3ComputerPage(v3Computer("5", "FVFC41HCLYWP", "FVFC41HCLYWP"))},
			`filter=id=in=(7)`: {200, v3ComputerPage(v3Computer("7", "Mac-07", "C02ABCDEF"))},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(
		t, cmd, "send-command",
		"--command", "BlankPush",
		"--from-file", filePath,
		"--yes",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	posts := mock.callsMatching("POST /JSSResource/computercommands/command/BlankPush")
	if len(posts) != 2 {
		t.Fatalf("expected 2 command POSTs, got %d: %v", len(posts), posts)
	}
	joined := strings.Join(posts, "\n")
	if strings.Contains(joined, "FVFC41HCLYWP") {
		t.Error("raw serial number leaked into the Classic MDM command path")
	}
	for _, want := range []string{"/id/5", "/id/7"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %s in POST paths, got: %v", want, posts)
		}
	}
}

// A resolution failure must reach the exit code the same way a POST failure
// does — otherwise a stale line in the file reads as a clean full success.
func TestSendCommand_FromFile_UnresolvedCountsAsFailure(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "computers.txt")
	if err := os.WriteFile(filePath, []byte("7\n999\n"), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"POST /JSSResource/computercommands/command/BlankPush/id/7": {200, `<computer_command/>`},
		},
		filterResponses: map[string]overviewMockResponse{
			`filter=id=in=(7,999)`: {200, v3ComputerPage(v3Computer("7", "Mac-07", "C02ABCDEF"))},
		},
	}

	cmd := newBulkCmd(newBulkCLIContext(mock))
	_, stderr, err := runCobraCmd(
		t, cmd, "send-command",
		"--command", "BlankPush",
		"--from-file", filePath,
		"--yes",
	)
	if err == nil {
		t.Fatal("expected a partial-failure error when one entry cannot be resolved")
	}
	if got := exitcode.CodeFrom(err); got != exitcode.PartialFailure {
		t.Errorf("exit code = %d, want %d (partial failure)", got, exitcode.PartialFailure)
	}
	if !strings.Contains(stderr, "1 unresolved") {
		t.Errorf("expected the summary line to name the unresolved entry, got: %q", stderr)
	}
	if got := len(mock.callsMatching("POST /JSSResource/computercommands")); got != 1 {
		t.Errorf("expected 1 command POST for the resolvable entry, got %d", got)
	}
}

func TestSendCommand_DestructiveRequiresConfirm(t *testing.T) {
	mock := &bulkMockClient{responses: map[string]overviewMockResponse{}}
	cliCtx := newBulkCLIContext(mock)

	for _, cmd2 := range []string{"EraseDevice", "DeviceLock"} {
		t.Run(cmd2, func(t *testing.T) {
			cmd := newBulkCmd(cliCtx)
			_, _, err := runCobraCmd(
				t, cmd, "send-command",
				"--command", cmd2,
				"--from-file", "/dev/null",
				"--yes",
				// --confirm-destructive is NOT set
			)
			if err == nil {
				t.Fatalf("expected error for destructive command %q without --confirm-destructive", cmd2)
			}
			if !strings.Contains(err.Error(), "confirm-destructive") {
				t.Errorf("expected 'confirm-destructive' in error, got: %v", err)
			}
			if mock.hasMutatingCall() {
				t.Error("should not issue any calls when the destructive gate is not cleared")
			}
		})
	}
}

func TestSendCommand_DestructiveWithBothFlags(t *testing.T) {
	// An empty --from-file now fails at target resolution (internal/resolve
	// rejects a file with no entries), not at the destructive gate — this
	// confirms --yes + --confirm-destructive together clear the gate before
	// target resolution ever runs.
	dir := t.TempDir()
	filePath := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(filePath, []byte("# only comments\n"), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	mock := &bulkMockClient{responses: map[string]overviewMockResponse{}}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(
		t, cmd, "send-command",
		"--command", "EraseDevice",
		"--from-file", filePath,
		"--yes",
		"--confirm-destructive",
	)
	if err == nil {
		t.Fatal("expected an error resolving an empty target file")
	}
	if strings.Contains(err.Error(), "destructive") {
		t.Errorf("gate should already be cleared; got a destructive-gate error instead of a target-resolution error: %v", err)
	}
	if mock.hasMutatingCall() {
		t.Error("should not issue any calls when target resolution fails")
	}
}

func TestSendCommand_DestructiveRequiresYesToo(t *testing.T) {
	mock := &bulkMockClient{responses: map[string]overviewMockResponse{}}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(
		t, cmd, "send-command",
		"--command", "EraseDevice",
		"--from-file", "/dev/null",
		// --yes is NOT set, --confirm-destructive IS set
		"--confirm-destructive",
	)
	if err == nil {
		t.Fatal("expected error for EraseDevice without --yes")
		return
	}
	if !strings.Contains(err.Error(), "destructive") {
		t.Errorf("expected 'destructive' in error, got: %v", err)
	}
}

func TestSendCommand_InvalidCommandName(t *testing.T) {
	mock := &bulkMockClient{responses: map[string]overviewMockResponse{}}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(
		t, cmd, "send-command",
		"--command", "SelfDestruct",
		"--from-file", "/dev/null",
		"--yes",
	)
	if err == nil {
		t.Fatal("expected error for unknown command name")
		return
	}
	if !strings.Contains(err.Error(), "SelfDestruct") {
		t.Errorf("expected command name in error, got: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────
// Tests: partial failure
// ─────────────────────────────────────────────────────────────────

func TestDisablePolicies_PartialFailure(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/policies": {200, `{"policies":[
				{"id":1,"name":"Deploy Chrome"},
				{"id":2,"name":"Install Zoom"}
			]}`},
			"GET /JSSResource/policies/id/1": {200, policyDetailJSON(1, "Deploy Chrome", true, "Apps", "All Computers")},
			"GET /JSSResource/policies/id/2": {200, policyDetailJSON(2, "Install Zoom", true, "Apps", "All Computers")},
			"PUT /JSSResource/policies/id/1": {200, `<policy/>`},
			// policy 2 PUT fails
			"PUT /JSSResource/policies/id/2": {500, `Internal Server Error`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, stderr, err := runCobraCmd(t, cmd, "disable-policies", "--yes")
	// Partial failure: command should return an error for non-zero exit code
	if err == nil {
		t.Fatal("expected error on partial failure")
		return
	}

	// Stderr should mention both "ok" (policy 1) and "ERROR" (policy 2)
	if !strings.Contains(stderr, "ok") {
		t.Error("expected 'ok' in stderr for successful policy")
	}
	if !strings.Contains(stderr, "ERROR") {
		t.Error("expected 'ERROR' in stderr for failed policy")
	}
}

func TestSendCommand_PartialFailure(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/computergroups": {200, `{"computer_groups":[
				{"id":100,"name":"Lab Macs"}
			]}`},
			"GET /JSSResource/computergroups/id/100": {200, staticGroupDetailJSON},
			"GET /JSSResource/computers/id/1":        {200, `{"computer":{"id":1,"name":"Mac-01"}}`},
			"GET /JSSResource/computers/id/2":        {200, `{"computer":{"id":2,"name":"Mac-02"}}`},
			// Mac-01 succeeds, Mac-02 fails
			"POST /JSSResource/computercommands/command/BlankPush/id/1": {200, `<computer_command/>`},
			"POST /JSSResource/computercommands/command/BlankPush/id/2": {500, `Internal Server Error`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, stderr, err := runCobraCmd(
		t, cmd, "send-command",
		"--command", "BlankPush",
		"--group", "Lab Macs",
		"--yes",
	)
	// Partial failure: command should return an error for non-zero exit code
	if err == nil {
		t.Fatal("expected error on partial failure")
		return
	}

	if !strings.Contains(stderr, "ok") {
		t.Error("expected 'ok' in stderr for successful computer")
	}
	if !strings.Contains(stderr, "ERROR") {
		t.Error("expected 'ERROR' in stderr for failed computer")
	}
}

// ─────────────────────────────────────────────────────────────────
// Tests: helper unit tests
// ─────────────────────────────────────────────────────────────────

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    bool
	}{
		{"Deploy *", "Deploy Chrome", true},
		{"Deploy *", "deploy chrome", true}, // case-insensitive
		{"Deploy *", "Install Chrome", false},
		{"*Security*", "Enforce Security Policy", true},
		{"*Security*", "Install Chrome", false},
		{"Exact", "Exact", true},
		{"Exact", "ExactMatch", false},
		{"*", "anything", true},
	}
	for _, tt := range tests {
		got, err := matchGlob(tt.pattern, tt.input)
		if err != nil {
			t.Errorf("matchGlob(%q, %q) error: %v", tt.pattern, tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.input, got, tt.want)
		}
	}
}

func TestCapitalize(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"enable", "Enable"},
		{"disable", "Disable"},
		{"", ""},
		{"a", "A"},
	}
	for _, tt := range tests {
		if got := capitalize(tt.in); got != tt.want {
			t.Errorf("capitalize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestExtractScopeItems(t *testing.T) {
	tests := []struct {
		name          string
		parent        map[string]any
		collectionKey string
		childKey      string
		wantNames     []string
	}{
		{
			name: "array form (with size)",
			parent: map[string]any{
				"computer_groups": []any{
					map[string]any{"id": 1.0, "name": "Group A"},
					map[string]any{"id": 2.0, "name": "Group B"},
				},
			},
			collectionKey: "computer_groups",
			childKey:      "computer_group",
			wantNames:     []string{"Group A", "Group B"},
		},
		{
			name: "map with child slice (no size)",
			parent: map[string]any{
				"computer_groups": map[string]any{
					"computer_group": []any{
						map[string]any{"id": 1.0, "name": "Group A"},
						map[string]any{"id": 2.0, "name": "Group B"},
					},
				},
			},
			collectionKey: "computer_groups",
			childKey:      "computer_group",
			wantNames:     []string{"Group A", "Group B"},
		},
		{
			name: "map with single child (no size, single item)",
			parent: map[string]any{
				"buildings": map[string]any{
					"building": map[string]any{"id": 1.0, "name": "HQ"},
				},
			},
			collectionKey: "buildings",
			childKey:      "building",
			wantNames:     []string{"HQ"},
		},
		{
			name: "empty string (self-closing XML)",
			parent: map[string]any{
				"buildings": "",
			},
			collectionKey: "buildings",
			childKey:      "building",
			wantNames:     nil,
		},
		{
			name:          "nil parent",
			parent:        nil,
			collectionKey: "buildings",
			childKey:      "building",
			wantNames:     nil,
		},
		{
			name:          "missing key",
			parent:        map[string]any{},
			collectionKey: "buildings",
			childKey:      "building",
			wantNames:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := extractScopeItems(tt.parent, tt.collectionKey, tt.childKey)
			var got []string
			for _, item := range items {
				if n, ok := item["name"].(string); ok {
					got = append(got, n)
				}
			}
			if len(got) != len(tt.wantNames) {
				t.Fatalf("got %d items %v, want %d items %v", len(got), got, len(tt.wantNames), tt.wantNames)
			}
			for i := range got {
				if got[i] != tt.wantNames[i] {
					t.Errorf("item[%d] = %q, want %q", i, got[i], tt.wantNames[i])
				}
			}
		})
	}
}

func TestScopeItemsContainAny(t *testing.T) {
	items := []map[string]any{
		{"id": 1.0, "name": "Lab Macs"},
		{"id": 2.0, "name": "Dev Team"},
	}

	tests := []struct {
		name  string
		items []map[string]any
		names []string
		want  bool
	}{
		{"match single", items, []string{"Lab Macs"}, true},
		{"case insensitive", items, []string{"lab macs"}, true},
		{"no match", items, []string{"QA Team"}, false},
		{"OR logic - second matches", items, []string{"QA Team", "Dev Team"}, true},
		{"empty items", nil, []string{"Lab Macs"}, false},
		{"empty names", items, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scopeItemsContainAny(tt.items, tt.names); got != tt.want {
				t.Errorf("scopeItemsContainAny() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSortedKeys(t *testing.T) {
	m := map[string]bool{"banana": true, "apple": true, "cherry": true}
	keys := sortedKeys(m)
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}
	if keys[0] != "apple" || keys[1] != "banana" || keys[2] != "cherry" {
		t.Errorf("unexpected order: %v", keys)
	}
}

func TestStaticGroupXMLBodies(t *testing.T) {
	addXML := staticGroupAddComputerXML("42")
	if !strings.Contains(addXML, "<computer_additions>") {
		t.Error("add XML missing computer_additions")
	}
	if !strings.Contains(addXML, "<id>42</id>") {
		t.Error("add XML missing computer id")
	}

	removeXML := staticGroupRemoveComputerXML("42")
	if !strings.Contains(removeXML, "<computer_deletions>") {
		t.Error("remove XML missing computer_deletions")
	}
	if !strings.Contains(removeXML, "<id>42</id>") {
		t.Error("remove XML missing computer id")
	}
}

func TestBulkCmd_HasExpectedSubcommands(t *testing.T) {
	mock := &bulkMockClient{responses: map[string]overviewMockResponse{}}
	cliCtx := newBulkCLIContext(mock)
	cmd := newBulkCmd(cliCtx)

	names := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		names[sub.Name()] = true
	}

	expected := []string{"enable-policies", "disable-policies", "add-to-group", "remove-from-group", "send-command"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("bulk command missing subcommand %q", name)
		}
	}
}
