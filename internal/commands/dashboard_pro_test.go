// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"strconv"
	"testing"
)

// The dashboard collectors were shipped with no direct coverage: the render
// tests exercised hand-built DashboardData, so a mutation in the collection
// math (a swapped numerator, a dropped guard) survived green. These tests drive
// the collectors through overviewMockClient so the arithmetic and the
// scope/sort logic are pinned to the wire shapes they parse.

func TestCollectPatchCompliance_ComputesPercentageFromWire(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v3/patch-software-title-configurations": {200, `{"totalCount":1,"results":[{"id":"10"}]}`},
			"/v3/patch-software-title-configurations/10/patch-summary": {
				200,
				`{"title":"Google Chrome","latestVersion":"120.0","upToDate":90,"outOfDate":10}`,
			},
			"/v3/patch-software-title-configurations/10/patch-summary/versions": {200, `[]`},
		},
	}

	compliance, _, err := collectPatchCompliance(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(compliance.Titles) != 1 {
		t.Fatalf("got %d titles, want 1", len(compliance.Titles))
	}
	got := compliance.Titles[0]
	if got.Name != "Google Chrome" {
		t.Errorf("Name = %q, want %q", got.Name, "Google Chrome")
	}
	// 90 up-to-date of 100 total is 90.0%, not 90/10, not 0.9.
	if got.Total != 100 {
		t.Errorf("Total = %d, want 100", got.Total)
	}
	if got.CompliancePct < 89.99 || got.CompliancePct > 90.01 {
		t.Errorf("CompliancePct = %v, want 90.0", got.CompliancePct)
	}
}

func TestCollectPatchCompliance_ZeroTotalIsZeroPercentNotDivideByZero(t *testing.T) {
	// A title with neither up-to-date nor out-of-date devices must read 0%,
	// never NaN from a 0/0 division that the total>0 guard exists to prevent.
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v3/patch-software-title-configurations": {200, `{"totalCount":1,"results":[{"id":"7"}]}`},
			"/v3/patch-software-title-configurations/7/patch-summary": {
				200,
				`{"title":"Idle Title","upToDate":0,"outOfDate":0}`,
			},
			"/v3/patch-software-title-configurations/7/patch-summary/versions": {200, `[]`},
		},
	}

	compliance, _, err := collectPatchCompliance(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(compliance.Titles) != 1 {
		t.Fatalf("got %d titles, want 1", len(compliance.Titles))
	}
	if got := compliance.Titles[0].CompliancePct; got != 0 {
		t.Errorf("CompliancePct = %v, want 0", got)
	}
}

func TestCollectPatchCompliance_NoConfigsIsEmptyNotError(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v3/patch-software-title-configurations": {200, `{"totalCount":0,"results":[]}`},
		},
	}
	compliance, spreads, err := collectPatchCompliance(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(compliance.Titles) != 0 || len(spreads) != 0 {
		t.Errorf("expected empty result, got %d titles / %d spreads", len(compliance.Titles), len(spreads))
	}
}

func TestIsEmptyScope(t *testing.T) {
	tests := []struct {
		name  string
		scope map[string]any
		want  bool
	}{
		{"nil scope is empty", nil, true},
		{"scope with no targets is empty", map[string]any{}, true},
		{
			"all_computers true is not empty",
			map[string]any{"all_computers": true},
			false,
		},
		{
			"all_computers false with nothing else is empty",
			map[string]any{"all_computers": false},
			true,
		},
		{
			"all_mobile_devices true is not empty",
			map[string]any{"all_mobile_devices": true},
			false,
		},
		{
			"a targeted computer is not empty",
			map[string]any{"computers": []any{map[string]any{"id": 1.0}}},
			false,
		},
		{
			"a targeted computer group is not empty",
			map[string]any{"computer_groups": []any{map[string]any{"id": 2.0}}},
			false,
		},
		{
			"an empty computers list is empty",
			map[string]any{"computers": []any{}},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isEmptyScope(tt.scope); got != tt.want {
				t.Errorf("isEmptyScope(%v) = %v, want %v", tt.scope, got, tt.want)
			}
		})
	}
}

func TestRenderDashboard_OSDistributionSortedAndCapped(t *testing.T) {
	// Twelve versions in count-ascending order: after render they must be
	// count-descending, capped at 10, with the tail rolled into "Other".
	versions := make([]osVersionCount, 0, 12)
	for i := 1; i <= 12; i++ {
		versions = append(versions, osVersionCount{Version: "v" + strconv.Itoa(i), Count: i})
	}
	data := &DashboardData{
		OSDist: &osDistribution{Versions: versions},
	}

	var buf bytes.Buffer
	if err := renderDashboard(&buf, data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := data.OSDist.Versions
	if len(got) != 11 {
		t.Fatalf("got %d rows, want 11 (10 + Other)", len(got))
	}
	// Highest count first.
	if got[0].Count != 12 {
		t.Errorf("first row Count = %d, want 12 (descending sort)", got[0].Count)
	}
	if got[0].Count < got[1].Count {
		t.Errorf("rows not in descending order: %d then %d", got[0].Count, got[1].Count)
	}
	last := got[len(got)-1]
	if last.Version != "Other" {
		t.Errorf("last row Version = %q, want %q", last.Version, "Other")
	}
	// The two smallest counts (1 and 2) fall past index 10 and roll up.
	if last.Count != 3 {
		t.Errorf("Other Count = %d, want 3 (1+2)", last.Count)
	}
}

func TestCollectCleanupAnalysis_CountsUnusedAndUnscoped(t *testing.T) {
	// Two policies: one enabled and scoped referencing pkg "A" and script "run.sh",
	// one disabled and unscoped. Two packages (A used, B unused) and two scripts
	// (run.sh used, orphan.sh unused). The unused loops at the end of the collector
	// are what turn "referenced by no policy" into a count.
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/policies":                 {200, `{"policy":[{"id":1},{"id":2}]}`},
			"/JSSResource/osxconfigurationprofiles": {200, `{"configuration_profile":[]}`},
			"/JSSResource/policies/id/1": {200, `{"policy":{
				"general":{"enabled":true},
				"scope":{"all_computers":true},
				"package_configuration":{"packages":[{"name":"A"}]},
				"scripts":{"script":[{"name":"run.sh"}]}
			}}`},
			"/JSSResource/policies/id/2": {200, `{"policy":{
				"general":{"enabled":false},
				"scope":{}
			}}`},
			"/JSSResource/packages": {200, `{"package":[{"name":"A"},{"name":"B"}]}`},
			"/JSSResource/scripts":  {200, `{"script":[{"name":"run.sh"},{"name":"orphan.sh"}]}`},
		},
	}

	got, err := collectCleanupAnalysis(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.DisabledPolicies != 1 {
		t.Errorf("DisabledPolicies = %d, want 1", got.DisabledPolicies)
	}
	if got.UnscopedPolicies != 1 {
		t.Errorf("UnscopedPolicies = %d, want 1", got.UnscopedPolicies)
	}
	if got.UnusedPackages != 1 {
		t.Errorf("UnusedPackages = %d, want 1 (B)", got.UnusedPackages)
	}
	if got.UnusedScripts != 1 {
		t.Errorf("UnusedScripts = %d, want 1 (orphan.sh)", got.UnusedScripts)
	}
	if got.PoliciesSkipped != 0 {
		t.Errorf("PoliciesSkipped = %d, want 0", got.PoliciesSkipped)
	}
	if !got.PackageScriptUsageReliable() {
		t.Error("usage should be reliable when no policy detail was skipped")
	}
}

func TestCollectCleanupAnalysis_SkippedPolicyMarksUsageUnreliable(t *testing.T) {
	// A policy whose detail fetch fails leaves its referenced items out of the
	// reference sets, so any unused tally would be an over-count. The collector
	// records the skip so the report can withhold the derived counts.
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/policies":                 {200, `{"policy":[{"id":1}]}`},
			"/JSSResource/osxconfigurationprofiles": {200, `{"configuration_profile":[]}`},
			"/JSSResource/policies/id/1":            {500, `boom`},
			"/JSSResource/packages":                 {200, `{"package":[{"name":"A"}]}`},
			"/JSSResource/scripts":                  {200, `{"script":[{"name":"run.sh"}]}`},
		},
	}

	got, err := collectCleanupAnalysis(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.PoliciesSkipped != 1 {
		t.Errorf("PoliciesSkipped = %d, want 1", got.PoliciesSkipped)
	}
	if got.PackageScriptUsageReliable() {
		t.Error("usage must be unreliable when a policy detail was skipped")
	}
}
