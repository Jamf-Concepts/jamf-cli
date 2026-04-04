// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// analyzePolicies — config check tests
// ---------------------------------------------------------------------------

func TestAnalyzePolicies_NoFindings(t *testing.T) {
	policies := []policyInfo{{
		ID: "1", Name: "Good Policy",
		Data: map[string]any{
			"general": map[string]any{
				"enabled":         true,
				"trigger_checkin": true,
				"frequency":       "Once per computer",
				"category":        map[string]any{"id": "5", "name": "Deployment"},
			},
			"scope": map[string]any{
				"all_computers":   false,
				"computers":       []any{},
				"computer_groups": []any{map[string]any{"id": 5, "name": "All Managed"}},
			},
			"package_configuration": map[string]any{
				"packages": []any{map[string]any{"id": 1, "name": "Chrome.pkg"}},
			},
		},
	}}

	findings := analyzePolicies(policies)
	if len(findings) != 0 {
		t.Errorf("got %d findings, want 0: %+v", len(findings), findings)
	}
}

func TestAnalyzePolicies_NoScope(t *testing.T) {
	policies := []policyInfo{{
		ID: "1", Name: "Orphan",
		Data: map[string]any{
			"general":               map[string]any{"enabled": true, "trigger_checkin": true, "category": map[string]any{"id": "5", "name": "Apps"}},
			"scope":                 map[string]any{"all_computers": false, "computers": []any{}, "computer_groups": []any{}},
			"package_configuration": map[string]any{"packages": []any{map[string]any{"id": 1}}},
		},
	}}

	findings := analyzePolicies(policies)
	if !hasCheck(findings, "no_scope") {
		t.Error("expected no_scope finding")
	}
}

func TestAnalyzePolicies_ScopedToGroupIsNotNoScope(t *testing.T) {
	// A policy scoped to a smart group should NOT trigger no_scope,
	// even if that group happens to be empty right now.
	policies := []policyInfo{{
		ID: "1", Name: "Smart Group Policy",
		Data: map[string]any{
			"general":               map[string]any{"enabled": true, "trigger_checkin": true, "category": map[string]any{"id": "5", "name": "Apps"}},
			"scope":                 map[string]any{"all_computers": false, "computers": []any{}, "computer_groups": []any{map[string]any{"id": 5, "name": "Needs Update"}}},
			"package_configuration": map[string]any{"packages": []any{map[string]any{"id": 1}}},
		},
	}}

	findings := analyzePolicies(policies)
	if hasCheck(findings, "no_scope") {
		t.Error("should NOT flag no_scope when scoped to a group")
	}
}

func TestAnalyzePolicies_DisabledScoped(t *testing.T) {
	policies := []policyInfo{{
		ID: "1", Name: "Stale",
		Data: map[string]any{
			"general": map[string]any{"enabled": false},
			"scope":   map[string]any{"all_computers": false, "computers": []any{map[string]any{"id": 10}}, "computer_groups": []any{}},
		},
	}}

	findings := analyzePolicies(policies)
	if !hasCheck(findings, "disabled_scoped") {
		t.Error("expected disabled_scoped finding")
	}
}

func TestAnalyzePolicies_NoPayload(t *testing.T) {
	policies := []policyInfo{{
		ID: "1", Name: "Empty",
		Data: map[string]any{
			"general": map[string]any{"enabled": true, "trigger_checkin": true, "category": map[string]any{"id": "5", "name": "Apps"}},
			"scope":   map[string]any{"all_computers": true},
			// No packages, scripts, etc.
		},
	}}

	findings := analyzePolicies(policies)
	if !hasCheck(findings, "no_payload") {
		t.Error("expected no_payload finding")
	}
}

func TestAnalyzePolicies_PayloadWithScript(t *testing.T) {
	policies := []policyInfo{{
		ID: "1", Name: "Scripted",
		Data: map[string]any{
			"general": map[string]any{"enabled": true, "trigger_checkin": true, "category": map[string]any{"id": "5", "name": "Apps"}},
			"scope":   map[string]any{"all_computers": true},
			"scripts": []any{map[string]any{"id": 1, "name": "fix.sh"}},
		},
	}}

	findings := analyzePolicies(policies)
	if hasCheck(findings, "no_payload") {
		t.Error("should NOT have no_payload finding when scripts are present")
	}
}

func TestAnalyzePolicies_PayloadWithRecon(t *testing.T) {
	policies := []policyInfo{{
		ID: "1", Name: "Recon Only",
		Data: map[string]any{
			"general":     map[string]any{"enabled": true, "trigger_checkin": true, "category": map[string]any{"id": "5", "name": "Apps"}},
			"scope":       map[string]any{"all_computers": true},
			"maintenance": map[string]any{"recon": true},
		},
	}}

	findings := analyzePolicies(policies)
	if hasCheck(findings, "no_payload") {
		t.Error("should NOT have no_payload finding when recon is enabled")
	}
}

func TestAnalyzePolicies_NoTrigger(t *testing.T) {
	policies := []policyInfo{{
		ID: "1", Name: "Dead",
		Data: map[string]any{
			"general": map[string]any{
				"enabled":                       true,
				"trigger_checkin":               false,
				"trigger_enrollment_complete":   false,
				"trigger_login":                 false,
				"trigger_logout":                false,
				"trigger_network_state_changed": false,
				"trigger_startup":               false,
				"category":                      map[string]any{"id": "5", "name": "Apps"},
			},
			"scope":                 map[string]any{"all_computers": true},
			"self_service":          map[string]any{"use_for_self_service": false},
			"package_configuration": map[string]any{"packages": []any{map[string]any{"id": 1}}},
		},
	}}

	findings := analyzePolicies(policies)
	if !hasCheck(findings, "no_trigger") {
		t.Error("expected no_trigger finding")
	}
}

func TestAnalyzePolicies_SelfServiceNoTriggerIsOK(t *testing.T) {
	policies := []policyInfo{{
		ID: "1", Name: "SS Only",
		Data: map[string]any{
			"general": map[string]any{"enabled": true, "category": map[string]any{"id": "5", "name": "Apps"}},
			"scope":   map[string]any{"all_computers": true},
			"self_service": map[string]any{
				"use_for_self_service":     true,
				"self_service_description": "Install Chrome",
				"self_service_icon":        map[string]any{"id": "42"},
			},
			"package_configuration": map[string]any{"packages": []any{map[string]any{"id": 1}}},
		},
	}}

	findings := analyzePolicies(policies)
	if hasCheck(findings, "no_trigger") {
		t.Error("should NOT flag no_trigger for Self Service policies")
	}
}

func TestAnalyzePolicies_NoCategory(t *testing.T) {
	policies := []policyInfo{{
		ID: "1", Name: "Uncategorized",
		Data: map[string]any{
			"general":               map[string]any{"enabled": true, "trigger_checkin": true, "category": map[string]any{"id": "-1", "name": "No category assigned"}},
			"scope":                 map[string]any{"all_computers": true},
			"package_configuration": map[string]any{"packages": []any{map[string]any{"id": 1}}},
		},
	}}

	findings := analyzePolicies(policies)
	if !hasCheck(findings, "no_category") {
		t.Error("expected no_category finding")
	}
}

func TestAnalyzePolicies_SortOrder(t *testing.T) {
	policies := []policyInfo{
		{ID: "1", Name: "B-Info", Data: map[string]any{
			"general": map[string]any{"enabled": false},
			"scope":   map[string]any{"all_computers": true},
		}},
		{ID: "2", Name: "A-Warning", Data: map[string]any{
			"general":               map[string]any{"enabled": true, "trigger_checkin": true, "category": map[string]any{"id": "5", "name": "Apps"}},
			"scope":                 map[string]any{"all_computers": false, "computers": []any{}, "computer_groups": []any{}},
			"package_configuration": map[string]any{"packages": []any{map[string]any{"id": 1}}},
		}},
	}

	findings := analyzePolicies(policies)
	if len(findings) < 2 {
		t.Fatalf("got %d findings, want >= 2", len(findings))
	}
	// Warnings should come before info
	if findings[0].Severity != "warning" {
		t.Errorf("first finding severity = %q, want warning", findings[0].Severity)
	}
}

// ---------------------------------------------------------------------------
// aggregatePolicyFailures
// ---------------------------------------------------------------------------

func TestAggregatePolicyFailures_Basic(t *testing.T) {
	results := []computerHistoryResult{
		{ComputerID: "1", Entries: []policyLogEntry{
			{PolicyID: "10", PolicyName: "Install Chrome", Status: "Completed", Date: time.Now()},
			{PolicyID: "10", PolicyName: "Install Chrome", Status: "Failed", Date: time.Now()},
			{PolicyID: "20", PolicyName: "Deploy VPN", Status: "Completed", Date: time.Now()},
		}},
		{ComputerID: "2", Entries: []policyLogEntry{
			{PolicyID: "10", PolicyName: "Install Chrome", Status: "Failed", Date: time.Now()},
			{PolicyID: "10", PolicyName: "Install Chrome", Status: "Failed", Date: time.Now()},
		}},
	}

	summaries := aggregatePolicyFailures(results)

	// Only policy 10 should appear (policy 20 has no failures)
	if len(summaries) != 1 {
		t.Fatalf("got %d summaries, want 1", len(summaries))
	}
	s := summaries[0]
	if s.PolicyID != "10" {
		t.Errorf("policy_id = %q, want 10", s.PolicyID)
	}
	if s.TotalRuns != 4 {
		t.Errorf("total_runs = %d, want 4", s.TotalRuns)
	}
	if s.Failures != 3 {
		t.Errorf("failures = %d, want 3", s.Failures)
	}
	if s.FailureRate != "75.0%" {
		t.Errorf("failure_rate = %q, want 75.0%%", s.FailureRate)
	}
}

func TestAggregatePolicyFailures_NoFailures(t *testing.T) {
	results := []computerHistoryResult{
		{ComputerID: "1", Entries: []policyLogEntry{
			{PolicyID: "10", PolicyName: "Good Policy", Status: "Completed", Date: time.Now()},
		}},
	}

	summaries := aggregatePolicyFailures(results)
	if len(summaries) != 0 {
		t.Errorf("got %d summaries, want 0 (no failures)", len(summaries))
	}
}

func TestAggregatePolicyFailures_Empty(t *testing.T) {
	summaries := aggregatePolicyFailures(nil)
	if len(summaries) != 0 {
		t.Errorf("got %d summaries, want 0", len(summaries))
	}
}

func TestAggregatePolicyFailures_SortedByFailureCount(t *testing.T) {
	results := []computerHistoryResult{
		{ComputerID: "1", Entries: []policyLogEntry{
			{PolicyID: "1", PolicyName: "Few Fails", Status: "Failed", Date: time.Now()},
			{PolicyID: "2", PolicyName: "Many Fails", Status: "Failed", Date: time.Now()},
			{PolicyID: "2", PolicyName: "Many Fails", Status: "Failed", Date: time.Now()},
			{PolicyID: "2", PolicyName: "Many Fails", Status: "Failed", Date: time.Now()},
		}},
	}

	summaries := aggregatePolicyFailures(results)
	if len(summaries) != 2 {
		t.Fatalf("got %d summaries, want 2", len(summaries))
	}
	if summaries[0].PolicyID != "2" {
		t.Errorf("first summary should be policy 2 (most failures), got %q", summaries[0].PolicyID)
	}
}

// ---------------------------------------------------------------------------
// parsePolicyLogDate
// ---------------------------------------------------------------------------

func TestParsePolicyLogDate_Epoch(t *testing.T) {
	m := map[string]any{"date_completed_epoch": float64(1711929600000)}
	d := parsePolicyLogDate(m)
	if d.IsZero() {
		t.Fatal("expected non-zero date")
	}
	if d.Year() != 2024 {
		t.Errorf("year = %d, want 2024", d.Year())
	}
}

func TestParsePolicyLogDate_RFC3339(t *testing.T) {
	m := map[string]any{"date_completed": "2026-04-01T08:00:00Z"}
	d := parsePolicyLogDate(m)
	if d.IsZero() {
		t.Fatal("expected non-zero date")
	}
	if d.Day() != 1 || d.Month() != 4 {
		t.Errorf("date = %v, want April 1", d)
	}
}

func TestParsePolicyLogDate_Empty(t *testing.T) {
	m := map[string]any{}
	d := parsePolicyLogDate(m)
	if !d.IsZero() {
		t.Errorf("expected zero date, got %v", d)
	}
}

// ---------------------------------------------------------------------------
// fetchAllPolicyLogs
// ---------------------------------------------------------------------------

func TestFetchAllPolicyLogs_ParsesAndFilters(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/computerhistory/id/10": {200, `{
				"computer_history": {
					"general": {"name": "Mac-A"},
					"policy_logs": [
						{"policy_id": 1, "policy_name": "Policy A", "status": "Completed", "date_completed_epoch": 1711929600000},
						{"policy_id": 2, "policy_name": "Policy B", "status": "Failed", "date_completed_epoch": 1711929600000}
					]
				}
			}`},
		},
	}

	cutoff := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	entries, err := fetchAllPolicyLogs(context.Background(), client, "10", cutoff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
}

func TestFetchAllPolicyLogs_CutoffFilters(t *testing.T) {
	// epoch 1711929600000 = 2024-04-01
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/computerhistory/id/10": {200, `{
				"computer_history": {
					"general": {"name": "Mac-A"},
					"policy_logs": [
						{"policy_id": 1, "policy_name": "Old", "status": "Failed", "date_completed_epoch": 1711929600000}
					]
				}
			}`},
		},
	}

	cutoff := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	entries, err := fetchAllPolicyLogs(context.Background(), client, "10", cutoff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0 (filtered by cutoff)", len(entries))
	}
}

func TestFetchAllPolicyLogs_FetchError(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/computerhistory/id/10": {500, `{}`},
		},
	}

	_, err := fetchAllPolicyLogs(context.Background(), client, "10", time.Time{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// Integration: fetchAndCheckPolicies
// ---------------------------------------------------------------------------

func TestFetchAndCheckPolicies_Integration(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/policies": {200, `{
				"policies": [
					{"id": 1, "name": "Good"},
					{"id": 2, "name": "Bad"}
				]
			}`},
			"/JSSResource/policies/id/1": {200, `{
				"policy": {
					"general": {"id": 1, "name": "Good", "enabled": true, "trigger_checkin": true, "category": {"id": "5", "name": "Deployment"}},
					"scope": {"all_computers": true, "computers": [], "computer_groups": []},
					"package_configuration": {"packages": [{"id": 1}]}
				}
			}`},
			"/JSSResource/policies/id/2": {200, `{
				"policy": {
					"general": {"id": 2, "name": "Bad", "enabled": true, "trigger_checkin": true, "frequency": "Ongoing", "category": {"id": "-1", "name": "No category assigned"}},
					"scope": {"all_computers": false, "computers": [], "computer_groups": []}
				}
			}`},
		},
	}

	policies, findings, fetchErrors, err := fetchAndCheckPolicies(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fetchErrors != 0 {
		t.Errorf("fetch_errors = %d, want 0", fetchErrors)
	}
	if len(policies) != 2 {
		t.Fatalf("got %d policies, want 2", len(policies))
	}
	// "Bad" should have: no_scope, no_payload, no_category
	if len(findings) < 3 {
		t.Errorf("got %d findings, want >= 3 for 'Bad' policy", len(findings))
	}
}

func TestFetchAndCheckPolicies_ListError(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/policies": {500, `{}`},
		},
	}

	_, _, _, err := fetchAndCheckPolicies(context.Background(), client)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// fetchAllPolicyLogs — xmlconv wrapped format (no <size> element)
// ---------------------------------------------------------------------------

func TestFetchAllPolicyLogs_WrappedMap(t *testing.T) {
	// Classic API XML without <size> in policy_logs produces a map wrapper
	// after xmlconv: {"policy_log": [...]} instead of []any.
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/computerhistory/id/10": {200, `{
				"computer_history": {
					"general": {"name": "Mac-A"},
					"policy_logs": {
						"policy_log": [
							{"policy_id": 1, "policy_name": "Policy A", "status": "Failed", "date_completed_epoch": 1711929600000},
							{"policy_id": 2, "policy_name": "Policy B", "status": "Completed", "date_completed_epoch": 1711929600000}
						]
					}
				}
			}`},
		},
	}

	cutoff := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	entries, err := fetchAllPolicyLogs(context.Background(), client, "10", cutoff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Status != "Failed" {
		t.Errorf("first entry status = %q, want Failed", entries[0].Status)
	}
}

func TestFetchAllPolicyLogs_WrappedMapSingleEntry(t *testing.T) {
	// Single policy log entry: xmlconv produces {"policy_log": {...}} not an array.
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/computerhistory/id/10": {200, `{
				"computer_history": {
					"general": {"name": "Mac-A"},
					"policy_logs": {
						"policy_log": {"policy_id": 1, "policy_name": "Solo", "status": "Failed", "date_completed_epoch": 1711929600000}
					}
				}
			}`},
		},
	}

	cutoff := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	entries, err := fetchAllPolicyLogs(context.Background(), client, "10", cutoff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func hasCheck(findings []policyHealthFinding, check string) bool {
	for _, f := range findings {
		if f.Check == check {
			return true
		}
	}
	return false
}
