// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"testing"
)

// ---------------------------------------------------------------------------
// tryAggregate
// ---------------------------------------------------------------------------

func TestTryAggregate_ReportFormat(t *testing.T) {
	results := []childResult{
		{profileName: "pro-a", stdout: []byte(`[{"summary":{"total":10,"days":30},"failures":[{"name":"P1","errors":5}]}]`)},
		{profileName: "pro-b", stdout: []byte(`[{"summary":{"total":20,"days":30},"failures":[{"name":"P2","errors":3}]}]`)},
	}
	merged := tryAggregate(results)
	if merged == nil {
		t.Fatal("expected aggregation, got nil")
		return
	}
	// Summary should be summed (total) but not days
	summary, _ := merged["summary"].(map[string]any)
	if summary == nil {
		t.Fatal("missing summary")
		return
	}
	if total, _ := summary["total"].(float64); total != 30 {
		t.Errorf("total = %v, want 30", total)
	}
	if days, _ := summary["days"].(float64); days != 30 {
		t.Errorf("days = %v, want 30 (not summed)", days)
	}
	// Failures should be concatenated with profile
	failures, _ := merged["failures"].([]any)
	if len(failures) != 2 {
		t.Fatalf("failures = %d, want 2", len(failures))
	}
	row0, _ := failures[0].(map[string]any)
	if row0["profile"] != "pro-a" {
		t.Errorf("profile = %q, want pro-a", row0["profile"])
	}
}

func TestTryAggregate_FlatArray(t *testing.T) {
	results := []childResult{
		{profileName: "pro-a", stdout: []byte(`[{"id":"1","name":"Chrome"},{"id":"2","name":"Firefox"}]`)},
		{profileName: "pro-b", stdout: []byte(`[{"id":"3","name":"Safari"}]`)},
	}
	merged := tryAggregate(results)
	if merged == nil {
		t.Fatal("expected aggregation, got nil")
		return
	}
	resultsList, _ := merged["results"].([]any)
	if len(resultsList) != 3 {
		t.Fatalf("results = %d, want 3", len(resultsList))
	}
	// Check profile injected
	row0, _ := resultsList[0].(map[string]any)
	if row0["profile"] != "pro-a" {
		t.Errorf("profile = %q, want pro-a", row0["profile"])
	}
}

func TestTryAggregate_EmptyArray(t *testing.T) {
	results := []childResult{
		{profileName: "pro-a", stdout: []byte(`[]`)},
		{profileName: "pro-b", stdout: []byte(`[{"id":"1","name":"Test"}]`)},
	}
	merged := tryAggregate(results)
	if merged == nil {
		t.Fatal("expected aggregation, got nil")
		return
	}
	resultsList, _ := merged["results"].([]any)
	if len(resultsList) != 1 {
		t.Fatalf("results = %d, want 1", len(resultsList))
	}
}

func TestTryAggregate_FailedChildExcluded(t *testing.T) {
	results := []childResult{
		{profileName: "pro-a", stdout: []byte(`[{"id":"1","name":"Test"}]`)},
		{profileName: "pro-b", err: fmt.Errorf("auth failed"), stdout: nil},
	}
	merged := tryAggregate(results)
	if merged == nil {
		t.Fatal("expected aggregation, got nil")
		return
	}
	resultsList, _ := merged["results"].([]any)
	if len(resultsList) != 1 {
		t.Fatalf("results = %d, want 1", len(resultsList))
	}
}

func TestTryAggregate_NonJSON(t *testing.T) {
	results := []childResult{
		{profileName: "pro-a", stdout: []byte(`not json at all`)},
	}
	merged := tryAggregate(results)
	if merged != nil {
		t.Errorf("expected nil for non-JSON, got %v", merged)
	}
}

func TestTryAggregate_ScalarOnlyObject(t *testing.T) {
	// Single object with only scalars (create/update response) — not aggregatable
	results := []childResult{
		{profileName: "pro-a", stdout: []byte(`[{"id":"42","name":"Created"}]`)},
		{profileName: "pro-b", stdout: []byte(`[{"id":"43","name":"Created"}]`)},
	}
	merged := tryAggregate(results)
	if merged == nil {
		t.Fatal("expected aggregation for flat array, got nil")
		return
	}
	// These are flat arrays (len > 1 per child isn't required), so they go through
	// the flat array path, not the single-object report path
}

// ---------------------------------------------------------------------------
// isSummaryList
// ---------------------------------------------------------------------------

func TestIsSummaryList_WithCount(t *testing.T) {
	items := []any{
		map[string]any{"status": "IDLE", "count": float64(100)},
		map[string]any{"status": "ERROR", "count": float64(5)},
	}
	if !isSummaryList(items) {
		t.Error("expected true for rows with count and no device fields")
	}
}

func TestIsSummaryList_WithDeviceFields(t *testing.T) {
	items := []any{
		map[string]any{"name": "Mac-A", "serial": "C02X", "count": float64(10)},
	}
	if isSummaryList(items) {
		t.Error("expected false for rows with device fields")
	}
}

func TestIsSummaryList_NoCount(t *testing.T) {
	items := []any{
		map[string]any{"name": "Chrome", "id": "1"},
	}
	if isSummaryList(items) {
		t.Error("expected false for rows without count")
	}
}

func TestIsSummaryList_Empty(t *testing.T) {
	if isSummaryList(nil) {
		t.Error("expected false for nil")
	}
	if isSummaryList([]any{}) {
		t.Error("expected false for empty")
	}
}

// ---------------------------------------------------------------------------
// summaryRowLabel
// ---------------------------------------------------------------------------

func TestSummaryRowLabel_SingleField(t *testing.T) {
	row := map[string]any{"status": "ERROR", "count": float64(5)}
	label := summaryRowLabel(row)
	if label != "ERROR" {
		t.Errorf("label = %q, want ERROR", label)
	}
}

func TestSummaryRowLabel_MultiField(t *testing.T) {
	row := map[string]any{"model": "MacBook Pro", "os_version": "15.3", "count": float64(50)}
	label := summaryRowLabel(row)
	// Should be deterministic: fields sorted alphabetically
	if label != "MacBook Pro|15.3" {
		t.Errorf("label = %q, want 'MacBook Pro|15.3'", label)
	}
}

func TestSummaryRowLabel_Empty(t *testing.T) {
	row := map[string]any{"count": float64(5)}
	label := summaryRowLabel(row)
	if label != "unknown" {
		t.Errorf("label = %q, want unknown", label)
	}
}

// ---------------------------------------------------------------------------
// hasAggregatableSections
// ---------------------------------------------------------------------------

func TestHasAggregatableSections_WithList(t *testing.T) {
	obj := map[string]any{"summary": map[string]any{"total": 10}, "failures": []any{}}
	if !hasAggregatableSections(obj) {
		t.Error("expected true for object with list section")
	}
}

func TestHasAggregatableSections_ScalarsOnly(t *testing.T) {
	obj := map[string]any{"id": "42", "name": "Created", "priority": float64(9)}
	if hasAggregatableSections(obj) {
		t.Error("expected false for scalar-only object")
	}
}

// ---------------------------------------------------------------------------
// summaryFieldShouldSum
// ---------------------------------------------------------------------------

func TestSummaryFieldShouldSum(t *testing.T) {
	if summaryFieldShouldSum("days") {
		t.Error("days should not sum")
	}
	if !summaryFieldShouldSum("total_errors") {
		t.Error("total_errors should sum")
	}
	if !summaryFieldShouldSum("warnings") {
		t.Error("warnings should sum")
	}
}

// ---------------------------------------------------------------------------
// formatSectionTitle
// ---------------------------------------------------------------------------

func TestFormatSectionTitle(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"summary", "Summary"},
		{"config_findings", "Config Findings"},
		{"plan_state_summary", "Plan State Summary"},
		{"results", "Results"},
	}
	for _, tc := range tests {
		got := formatSectionTitle(tc.input)
		if got != tc.want {
			t.Errorf("formatSectionTitle(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// stripOutputFlag
// ---------------------------------------------------------------------------

func TestStripOutputFlag(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  int
	}{
		{"no flag", []string{"pro", "comp", "list"}, 3},
		{"-o json", []string{"pro", "comp", "list", "-o", "json"}, 3},
		{"-o=json", []string{"pro", "comp", "list", "-o=json"}, 3},
		{"-ojson", []string{"pro", "comp", "list", "-ojson"}, 3},
		{"--output table", []string{"pro", "comp", "list", "--output", "table"}, 3},
		{"--output=yaml", []string{"pro", "comp", "list", "--output=yaml"}, 3},
		{"middle", []string{"pro", "-o", "csv", "comp", "list"}, 3},
	}
	for _, tc := range tests {
		got := stripOutputFlag(tc.input)
		if len(got) != tc.want {
			t.Errorf("%s: stripOutputFlag(%v) = %d args, want %d: %v", tc.name, tc.input, len(got), tc.want, got)
		}
	}
}
