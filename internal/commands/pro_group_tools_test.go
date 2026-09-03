// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// ─────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────

// groupToolsMockClient returns an overviewMockClient pre-populated with
// a standard set of computer-group and policy responses suitable for most
// group-tools tests.
func groupToolsMockClient() *overviewMockClient {
	return &overviewMockClient{
		responses: map[string]overviewMockResponse{
			// Paginated list — single page
			"/v1/computer-groups": {200, `{
				"totalCount": 4,
				"results": [
					{"id": "1", "name": "All Computers",  "smartGroup": true,  "memberCount": 42},
					{"id": "2", "name": "Empty Static",   "smartGroup": false, "memberCount": 0},
					{"id": "3", "name": "Dev Machines",   "smartGroup": true,  "memberCount": 7},
					{"id": "4", "name": "Prod Servers",   "smartGroup": false, "memberCount": 2}
				]
			}`},
			// Smart group membership (id=1) — v2 returns integer IDs
			"/v3/computer-groups/smart-group-membership/1": {200, `{
				"members": [10, 11]
			}`},
			// Smart group membership (id=3)
			"/v3/computer-groups/smart-group-membership/3": {200, `{
				"members": [20, 21, 22]
			}`},
			// Static group detail (id=2) — Classic API, empty group
			"/JSSResource/computergroups/id/2": {200, `{"computer_group":{"id":2,"name":"Empty Static","is_smart":false,"computers":{"computer":[]}}}`},
			// Static group detail (id=4) — Classic API, with members
			"/JSSResource/computergroups/id/4": {200, `{"computer_group":{"id":4,"name":"Prod Servers","is_smart":false,"computers":{"computer":[{"id":100,"name":"mac-static-01"},{"id":101,"name":"mac-static-02"}]}}}`},
			// Classic policy list
			"/JSSResource/policies": {200, `{
				"policies": [
					{"id": 101, "name": "Deploy Chrome"},
					{"id": 102, "name": "Restrict Safari"}
				]
			}`},
			// Policy detail 101 — scopes "All Computers"
			"/JSSResource/policies/id/101": {200, `{
				"policy": {
					"general": {"name": "Deploy Chrome"},
					"scope": {
						"computerGroups": [
							{"id": 1, "name": "All Computers"}
						]
					}
				}
			}`},
			// Policy detail 102 — scopes "Dev Machines"
			"/JSSResource/policies/id/102": {200, `{
				"policy": {
					"general": {"name": "Restrict Safari"},
					"scope": {
						"computerGroups": [
							{"id": 3, "name": "Dev Machines"}
						]
					}
				}
			}`},
		},
	}
}

// ─────────────────────────────────────────────────────────────────
// list
// ─────────────────────────────────────────────────────────────────

func TestGroupToolsList_NoFilters(t *testing.T) {
	client := groupToolsMockClient()
	groups, err := FetchAllPaginated(context.Background(), client, "/v1/computer-groups", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 4 {
		t.Errorf("got %d groups, want 4", len(groups))
	}
}

func TestGroupToolsList_FilterSmart(t *testing.T) {
	client := groupToolsMockClient()
	groups, err := FetchAllPaginated(context.Background(), client, "/v1/computer-groups", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var smart []map[string]any
	for _, g := range groups {
		isSmart, _ := g["smartGroup"].(bool)
		if isSmart {
			smart = append(smart, g)
		}
	}
	if len(smart) != 2 {
		t.Errorf("got %d smart groups, want 2", len(smart))
	}
}

func TestGroupToolsList_FilterStatic(t *testing.T) {
	client := groupToolsMockClient()
	groups, err := FetchAllPaginated(context.Background(), client, "/v1/computer-groups", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var static []map[string]any
	for _, g := range groups {
		isSmart, _ := g["smartGroup"].(bool)
		if !isSmart {
			static = append(static, g)
		}
	}
	if len(static) != 2 {
		t.Errorf("got %d static groups, want 2", len(static))
	}
	if n, _ := static[0]["name"].(string); n != "Empty Static" {
		t.Errorf("static group[0] name = %q, want %q", n, "Empty Static")
	}
}

func TestGroupToolsList_FilterEmpty(t *testing.T) {
	client := groupToolsMockClient()
	groups, err := FetchAllPaginated(context.Background(), client, "/v1/computer-groups", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var empty []map[string]any
	for _, g := range groups {
		if groupMemberCount(g) == 0 {
			empty = append(empty, g)
		}
	}
	if len(empty) != 1 {
		t.Errorf("got %d empty groups, want 1", len(empty))
	}
	if n, _ := empty[0]["name"].(string); n != "Empty Static" {
		t.Errorf("empty group name = %q, want %q", n, "Empty Static")
	}
}

func TestGroupToolsList_FilterNamePattern(t *testing.T) {
	client := groupToolsMockClient()
	groups, err := FetchAllPaginated(context.Background(), client, "/v1/computer-groups", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pattern := "dev"
	var matched []map[string]any
	for _, g := range groups {
		name, _ := g["name"].(string)
		if strings.Contains(strings.ToLower(name), strings.ToLower(pattern)) {
			matched = append(matched, g)
		}
	}
	if len(matched) != 1 {
		t.Errorf("got %d groups matching %q, want 1", len(matched), pattern)
	}
	if n, _ := matched[0]["name"].(string); n != "Dev Machines" {
		t.Errorf("matched group = %q, want %q", n, "Dev Machines")
	}
}

func TestGroupToolsList_NamePatternCaseInsensitive(t *testing.T) {
	client := groupToolsMockClient()
	groups, err := FetchAllPaginated(context.Background(), client, "/v1/computer-groups", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// "COMPUTERS" should match "All Computers" case-insensitively
	pattern := "COMPUTERS"
	var matched []map[string]any
	for _, g := range groups {
		name, _ := g["name"].(string)
		if strings.Contains(strings.ToLower(name), strings.ToLower(pattern)) {
			matched = append(matched, g)
		}
	}
	if len(matched) != 1 {
		t.Errorf("got %d groups matching %q, want 1", len(matched), pattern)
	}
}

func TestGroupToolsList_EmptyResults(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/computer-groups": {200, `{"totalCount":0,"results":[]}`},
		},
	}

	groups, err := FetchAllPaginated(context.Background(), client, "/v1/computer-groups", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("got %d groups, want 0", len(groups))
	}
}

// ─────────────────────────────────────────────────────────────────
// members
// ─────────────────────────────────────────────────────────────────

func TestGroupToolsMembers_SmartGroup(t *testing.T) {
	client := groupToolsMockClient()
	ctx := context.Background()

	// Look up group "All Computers" (smart)
	groups, err := FetchAllPaginated(ctx, client, "/v1/computer-groups", 100)
	if err != nil {
		t.Fatalf("fetching groups: %v", err)
	}

	var groupID string
	var isSmart bool
	for _, g := range groups {
		if n, _ := g["name"].(string); strings.EqualFold(n, "All Computers") {
			groupID = extractID(g)
			isSmart, _ = g["smartGroup"].(bool)
			break
		}
	}
	if groupID != "1" {
		t.Errorf("group ID = %q, want %q", groupID, "1")
	}
	if !isSmart {
		t.Error("expected All Computers to be a smart group")
	}

	// Fetch smart group membership via v2 endpoint
	detail, err := FetchJSON(ctx, client, fmt.Sprintf("/v3/computer-groups/smart-group-membership/%s", groupID))
	if err != nil {
		t.Fatalf("fetching smart group membership: %v", err)
	}

	members, _ := detail["members"].([]any)
	if len(members) != 2 {
		t.Errorf("got %d members, want 2", len(members))
	}

	if anyToIDString(members[0]) != "10" {
		t.Errorf("member[0] ID = %q, want %q", anyToIDString(members[0]), "10")
	}
}

func TestGroupToolsMembers_NotFound(t *testing.T) {
	client := groupToolsMockClient()
	ctx := context.Background()

	groups, err := FetchAllPaginated(ctx, client, "/v1/computer-groups", 100)
	if err != nil {
		t.Fatalf("fetching groups: %v", err)
	}

	var groupID string
	for _, g := range groups {
		if n, _ := g["name"].(string); strings.EqualFold(n, "nonexistent-group") {
			groupID = extractID(g)
			break
		}
	}
	if groupID != "" {
		t.Errorf("expected no match for nonexistent group, got ID %q", groupID)
	}
}

func TestGroupToolsMembers_EmptyStaticGroup(t *testing.T) {
	mock := groupToolsMockClient()
	cliCtx := &registry.CLIContext{Client: mock}

	oldFmt := outputFmt
	outputFmt = "json"
	defer func() { outputFmt = oldFmt }()

	err := runGroupToolsMembers(context.Background(), cliCtx, "Empty Static")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGroupToolsMembers_StaticGroupWithMembers(t *testing.T) {
	mock := groupToolsMockClient()
	cliCtx := &registry.CLIContext{Client: mock}

	oldFmt := outputFmt
	outputFmt = "json"
	defer func() { outputFmt = oldFmt }()

	// Run the full members command for a static group with members
	err := runGroupToolsMembers(context.Background(), cliCtx, "Prod Servers")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- runGroupToolsMembers integration tests ---

func TestRunGroupToolsMembers_Found(t *testing.T) {
	mock := groupToolsMockClient()
	cliCtx := &registry.CLIContext{Client: mock}

	oldFmt := outputFmt
	outputFmt = "json"
	defer func() { outputFmt = oldFmt }()

	// Should succeed for a known group name (case-insensitive)
	err := runGroupToolsMembers(context.Background(), cliCtx, "all computers")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunGroupToolsMembers_NotFound(t *testing.T) {
	mock := groupToolsMockClient()
	cliCtx := &registry.CLIContext{Client: mock}

	err := runGroupToolsMembers(context.Background(), cliCtx, "Nonexistent Group")
	if err == nil {
		t.Fatal("expected error for nonexistent group")
		return
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want to contain 'not found'", err.Error())
	}
}

// --- Array-format mock for real API shape ---

func TestGroupToolsList_ArrayFormat(t *testing.T) {
	// Real /v1/computer-groups returns a plain array
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/computer-groups": {200, `[
				{"id":"1","name":"All Macs","smartGroup":true,"memberCount":10},
				{"id":"2","name":"Empty","smartGroup":true,"memberCount":0}
			]`},
		},
	}

	cliCtx := &registry.CLIContext{Client: client}

	oldFmt := outputFmt
	outputFmt = "json"
	defer func() { outputFmt = oldFmt }()

	err := runGroupToolsList(context.Background(), cliCtx, "", false, "")
	if err != nil {
		t.Fatalf("unexpected error with array response: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────
// analyze --unused
// ─────────────────────────────────────────────────────────────────

func TestGroupToolsAnalyze_UnusedDetection(t *testing.T) {
	client := groupToolsMockClient()
	ctx := context.Background()

	// Fetch all groups
	groups, err := FetchAllPaginated(ctx, client, "/v1/computer-groups", 100)
	if err != nil {
		t.Fatalf("fetching groups: %v", err)
	}

	// Fetch policy list
	policyItems, err := FetchClassicList(ctx, client, "/JSSResource/policies", "policies")
	if err != nil {
		t.Fatalf("fetching policies: %v", err)
	}
	if len(policyItems) != 2 {
		t.Errorf("got %d policies, want 2", len(policyItems))
	}

	// Fetch policy details and collect referenced group names
	referenced := make(map[string]bool)
	for _, item := range policyItems {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := extractID(m)
		data, err := FetchJSON(ctx, client, "/JSSResource/policies/id/"+id)
		if err != nil {
			continue
		}
		detail := unwrapClassicDetail(data)
		if detail == nil {
			continue
		}
		scope, _ := detail["scope"].(map[string]any)
		if scope != nil {
			addGroupNamesFromScope(scope, referenced)
		}
	}

	// "All Computers" and "Dev Machines" are referenced; "Empty Static" and "Prod Servers" are not
	if !referenced["All Computers"] {
		t.Error("expected 'All Computers' to be referenced")
	}
	if !referenced["Dev Machines"] {
		t.Error("expected 'Dev Machines' to be referenced")
	}
	if referenced["Empty Static"] {
		t.Error("expected 'Empty Static' to be unreferenced")
	}
	if referenced["Prod Servers"] {
		t.Error("expected 'Prod Servers' to be unreferenced")
	}

	// Count unreferenced groups
	var unused []map[string]any
	for _, g := range groups {
		name, _ := g["name"].(string)
		if !referenced[name] {
			unused = append(unused, g)
		}
	}
	if len(unused) != 2 {
		t.Errorf("got %d unused groups, want 2", len(unused))
	}
}

func TestGroupToolsAnalyze_AllReferenced(t *testing.T) {
	// All groups referenced → no unused results
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/computer-groups": {200, `{
				"totalCount": 1,
				"results": [{"id": "1", "name": "All Computers", "smartGroup": true, "memberCount": 5}]
			}`},
			"/JSSResource/policies": {200, `{
				"policies": [{"id": 1, "name": "P1"}]
			}`},
			"/JSSResource/policies/id/1": {200, `{
				"policy": {
					"scope": {
						"computerGroups": [{"id": 1, "name": "All Computers"}]
					}
				}
			}`},
		},
	}

	ctx := context.Background()
	groups, _ := FetchAllPaginated(ctx, client, "/v1/computer-groups", 100)
	policyItems, _ := FetchClassicList(ctx, client, "/JSSResource/policies", "policies")

	referenced := make(map[string]bool)
	for _, item := range policyItems {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := extractID(m)
		data, err := FetchJSON(ctx, client, "/JSSResource/policies/id/"+id)
		if err != nil {
			continue
		}
		detail := unwrapClassicDetail(data)
		if detail != nil {
			if scope, ok := detail["scope"].(map[string]any); ok {
				addGroupNamesFromScope(scope, referenced)
			}
		}
	}

	var unused []map[string]any
	for _, g := range groups {
		if n, _ := g["name"].(string); !referenced[n] {
			unused = append(unused, g)
		}
	}
	if len(unused) != 0 {
		t.Errorf("got %d unused groups, want 0", len(unused))
	}
}

func TestGroupToolsAnalyze_NoPolicies(t *testing.T) {
	// No policies → all groups unused
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/computer-groups": {200, `{
				"totalCount": 2,
				"results": [
					{"id": "1", "name": "Group A", "smartGroup": true,  "memberCount": 3},
					{"id": "2", "name": "Group B", "smartGroup": false, "memberCount": 0}
				]
			}`},
			"/JSSResource/policies": {200, `{"policies":[]}`},
		},
	}

	ctx := context.Background()
	groups, _ := FetchAllPaginated(ctx, client, "/v1/computer-groups", 100)
	policyItems, _ := FetchClassicList(ctx, client, "/JSSResource/policies", "policies")

	referenced := make(map[string]bool)
	for _, item := range policyItems {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := extractID(m)
		data, err := FetchJSON(ctx, client, "/JSSResource/policies/id/"+id)
		if err != nil {
			continue
		}
		detail := unwrapClassicDetail(data)
		if detail != nil {
			if scope, ok := detail["scope"].(map[string]any); ok {
				addGroupNamesFromScope(scope, referenced)
			}
		}
	}

	var unused []map[string]any
	for _, g := range groups {
		if n, _ := g["name"].(string); !referenced[n] {
			unused = append(unused, g)
		}
	}
	if len(unused) != 2 {
		t.Errorf("got %d unused groups, want 2", len(unused))
	}
}

// ─────────────────────────────────────────────────────────────────
// export
// ─────────────────────────────────────────────────────────────────

func TestGroupToolsExport_JSON(t *testing.T) {
	client := groupToolsMockClient()
	ctx := context.Background()

	groups, err := FetchAllPaginated(ctx, client, "/v1/computer-groups", 100)
	if err != nil {
		t.Fatalf("fetching groups: %v", err)
	}
	if len(groups) != 4 {
		t.Errorf("got %d groups, want 4", len(groups))
	}

	data, err := marshalGroupsJSON(groups)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	if !strings.Contains(string(data), "All Computers") {
		t.Error("JSON output missing 'All Computers'")
	}
	if !strings.Contains(string(data), "Empty Static") {
		t.Error("JSON output missing 'Empty Static'")
	}
}

func TestGroupToolsExport_YAML(t *testing.T) {
	client := groupToolsMockClient()
	ctx := context.Background()

	groups, err := FetchAllPaginated(ctx, client, "/v1/computer-groups", 100)
	if err != nil {
		t.Fatalf("fetching groups: %v", err)
	}

	data, err := marshalGroupsYAML(groups)
	if err != nil {
		t.Fatalf("marshal YAML: %v", err)
	}
	if !strings.Contains(string(data), "All Computers") {
		t.Error("YAML output missing 'All Computers'")
	}
}

func TestGroupToolsExport_Empty(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/computer-groups": {200, `{"totalCount":0,"results":[]}`},
		},
	}
	ctx := context.Background()

	groups, err := FetchAllPaginated(ctx, client, "/v1/computer-groups", 100)
	if err != nil {
		t.Fatalf("fetching groups: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("got %d groups, want 0", len(groups))
	}

	data, err := marshalGroupsJSON(groups)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	// An empty JSON array
	if strings.TrimSpace(string(data)) != "[]" {
		t.Errorf("empty export JSON = %q, want %q", strings.TrimSpace(string(data)), "[]")
	}
}

// ─────────────────────────────────────────────────────────────────
// groupSummaryRow / groupMemberCount
// ─────────────────────────────────────────────────────────────────

func TestGroupSummaryRow_Smart(t *testing.T) {
	g := map[string]any{
		"id":          "42",
		"name":        "My Smart Group",
		"smartGroup":  true,
		"memberCount": float64(15),
	}
	row := groupSummaryRow(g)

	if row["type"] != "smart" {
		t.Errorf("type = %q, want %q", row["type"], "smart")
	}
	if row["memberCount"] != 15 {
		t.Errorf("memberCount = %v, want 15", row["memberCount"])
	}
	if row["name"] != "My Smart Group" {
		t.Errorf("name = %q, want %q", row["name"], "My Smart Group")
	}
}

func TestGroupSummaryRow_Static(t *testing.T) {
	g := map[string]any{
		"id":          "7",
		"name":        "Static Group",
		"smartGroup":  false,
		"memberCount": float64(0),
	}
	row := groupSummaryRow(g)

	if row["type"] != "static" {
		t.Errorf("type = %q, want %q", row["type"], "static")
	}
	if row["memberCount"] != 0 {
		t.Errorf("memberCount = %v, want 0", row["memberCount"])
	}
}

func TestGroupMemberCount_FromField(t *testing.T) {
	g := map[string]any{"memberCount": float64(99)}
	if c := groupMemberCount(g); c != 99 {
		t.Errorf("groupMemberCount = %d, want 99", c)
	}
}

func TestGroupMemberCount_FromArray(t *testing.T) {
	g := map[string]any{
		"members": []any{
			map[string]any{"id": "1"},
			map[string]any{"id": "2"},
			map[string]any{"id": "3"},
		},
	}
	if c := groupMemberCount(g); c != 3 {
		t.Errorf("groupMemberCount = %d, want 3", c)
	}
}

func TestGroupMemberCount_Missing(t *testing.T) {
	g := map[string]any{"name": "No Count"}
	if c := groupMemberCount(g); c != 0 {
		t.Errorf("groupMemberCount = %d, want 0", c)
	}
}

// ─────────────────────────────────────────────────────────────────
// addGroupNamesFromScope
// ─────────────────────────────────────────────────────────────────

func TestAddGroupNamesFromScope_ModernKey(t *testing.T) {
	scope := map[string]any{
		"computerGroups": []any{
			map[string]any{"id": float64(1), "name": "Group A"},
			map[string]any{"id": float64(2), "name": "Group B"},
		},
	}
	out := make(map[string]bool)
	addGroupNamesFromScope(scope, out)

	if !out["Group A"] {
		t.Error("expected 'Group A' in referenced set")
	}
	if !out["Group B"] {
		t.Error("expected 'Group B' in referenced set")
	}
}

func TestAddGroupNamesFromScope_ClassicKey(t *testing.T) {
	scope := map[string]any{
		"computer_groups": []any{
			map[string]any{"id": float64(5), "name": "Classic Group"},
		},
	}
	out := make(map[string]bool)
	addGroupNamesFromScope(scope, out)

	if !out["Classic Group"] {
		t.Error("expected 'Classic Group' in referenced set")
	}
}

func TestAddGroupNamesFromScope_Empty(t *testing.T) {
	scope := map[string]any{
		"computerGroups": []any{},
	}
	out := make(map[string]bool)
	addGroupNamesFromScope(scope, out)

	if len(out) != 0 {
		t.Errorf("expected empty referenced set, got %v", out)
	}
}

func TestAddGroupNamesFromScope_NoGroupsKey(t *testing.T) {
	scope := map[string]any{
		"computers": []any{
			map[string]any{"id": float64(1), "name": "mac-01"},
		},
	}
	out := make(map[string]bool)
	addGroupNamesFromScope(scope, out)

	if len(out) != 0 {
		t.Errorf("expected empty referenced set for scope without group keys, got %v", out)
	}
}
