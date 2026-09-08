// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// patchReportMock answers the two collections runReportPatchStatusFull reads.
// policyStatus lets a test make the patch-policies fetch fail, which is the
// case the document has to distinguish from a clean result.
func patchReportMock(policyStatus int) *overviewMockClient {
	return &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v3/patch-software-title-configurations": {200, `[{"id":"1","displayName":"Firefox"}]`},
			"/v3/patch-software-title-configurations/1/patch-summary": {200, `{
				"upToDate": 8, "outOfDate": 2, "latestVersion": "130.0"
			}`},
			"/v2/patch-policies": {policyStatus, `{"totalCount":0,"results":[]}`},
		},
	}
}

// TestPatchStatusFullEmitsOneDocumentWithNoNullSections drives the command
// function, which no test did: runReportPatchStatusFull was 0.0% covered, so
// the whole per-format contract was verified by nothing. Deleting the gathered
// branch left the suite green and brought back the three-array file jq rejects
// at the second document.
//
// Two assertions, because the shape and the null are separate defects. The
// document is one array of labelled sections; and an empty section is [], never
// null, which is CLAUDE.md's own rule and what broke the jq pipeline this
// command's help recommends on a healthy tenant.
func TestPatchStatusFullEmitsOneDocumentWithNoNullSections(t *testing.T) {
	restoreOutputFlags(t)
	outputFmt, quiet = "json", true

	var out strings.Builder
	formatter := output.New("json", true, false)
	formatter.SetWriter(&out)
	cliCtx := &registry.CLIContext{Client: patchReportMock(200), Output: &cliOutput{formatter}}

	if err := runReportPatchStatusFull(context.Background(), cliCtx, true); err != nil {
		t.Fatalf("runReportPatchStatusFull: %v", err)
	}

	// One top-level array, not three concatenated documents.
	var sections []map[string]any
	if err := json.Unmarshal([]byte(out.String()), &sections); err != nil {
		t.Fatalf("the output is not one JSON document, which is what jq rejects: %v\n%s", err, out.String())
	}
	if len(sections) != 3 {
		t.Fatalf("got %d sections, want 3: %s", len(sections), out.String())
	}

	wantNames := []string{"title_compliance", "policy_failures", "device_failures"}
	for i, want := range wantNames {
		if got, _ := sections[i]["section"].(string); got != want {
			t.Errorf("section %d is %q, want %q", i, got, want)
		}
		data, ok := sections[i]["data"]
		if !ok {
			t.Errorf("section %q carries no data key", want)
			continue
		}
		if data == nil {
			t.Errorf("section %q has data: null — an empty list prints [], never null, or jq cannot iterate it", want)
		}
		if _, isList := data.([]any); !isList {
			t.Errorf("section %q data is %T, want a list", want, data)
		}
	}
}

// TestPatchStatusFullNamesAFailedFetch pins the other half of the same
// document. An empty section and a section whose fetch failed were
// byte-identical, so a scheduled compliance job read "no patch failures" from
// a check that never ran. The only signal was a WARNING on a stream this
// command's help steers the reader away from.
func TestPatchStatusFullNamesAFailedFetch(t *testing.T) {
	restoreOutputFlags(t)
	outputFmt, quiet = "json", true

	render := func(policyStatus int) []map[string]any {
		t.Helper()
		var out strings.Builder
		formatter := output.New("json", true, false)
		formatter.SetWriter(&out)
		cliCtx := &registry.CLIContext{Client: patchReportMock(policyStatus), Output: &cliOutput{formatter}}
		if err := runReportPatchStatusFull(context.Background(), cliCtx, true); err != nil {
			t.Fatalf("runReportPatchStatusFull: %v", err)
		}
		var sections []map[string]any
		if err := json.Unmarshal([]byte(out.String()), &sections); err != nil {
			t.Fatalf("not one JSON document: %v\n%s", err, out.String())
		}
		return sections
	}

	clean := render(200)
	failed := render(500)

	if got, _ := clean[1]["fetch_error"].(string); got != "" {
		t.Errorf("a clean run reports fetch_error %q, want empty", got)
	}
	if got, _ := failed[1]["fetch_error"].(string); got == "" {
		t.Error("a failed patch-policies fetch is indistinguishable from an empty result — the document says nothing about it")
	}
}
