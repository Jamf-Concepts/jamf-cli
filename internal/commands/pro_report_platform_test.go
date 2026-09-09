// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// blueprintStatusCanonicalRows is the shape newReportBlueprintStatusCmd builds
// before handing it to blueprintStatusRowsForFormat: a NOT_DEPLOYED blueprint
// first, so it is the row whose keys a table would take its columns from, and
// a DEPLOYED one carrying counts second.
func blueprintStatusCanonicalRows() []map[string]any {
	return []map[string]any{
		{
			"name": "Restrictions - Example", "state": "NOT_DEPLOYED",
			"scope": 0, "steps": 1,
			"succeeded": nil, "failed": nil, "pending": nil,
		},
		{
			"name": "App Settings - Example", "state": "DEPLOYED",
			"scope": 1, "steps": 2,
			"succeeded": 12, "failed": 0, "pending": 2,
		},
	}
}

// TestBlueprintStatusKeepsItsCountColumnsWhenTheFirstRowIsNotDeployed is issue
// #356. A table's columns are the keys of its *first* row, so a NOT_DEPLOYED
// blueprint sorting first took SUCCEEDED / FAILED / PENDING off the report
// entirely — while -o json showed the counts on the rows below it, which is
// what made the report look like a rendering bug rather than a missing column.
func TestBlueprintStatusKeepsItsCountColumnsWhenTheFirstRowIsNotDeployed(t *testing.T) {
	for _, format := range []string{"table", "csv", "plain"} {
		t.Run(format, func(t *testing.T) {
			rows := blueprintStatusRowsForFormat(blueprintStatusCanonicalRows(), format)

			var out strings.Builder
			f := output.New(format, true, false)
			f.SetWriter(&out)
			if err := f.Print(rows); err != nil {
				t.Fatalf("print: %v", err)
			}

			body := out.String()
			if format == "plain" {
				// plain emits no header, so the column set is only visible as
				// the field count, which has to be the same on both rows.
				for i, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
					if n := len(strings.Split(line, "\t")); n != 7 {
						t.Errorf("-o plain row %d has %d fields, want 7:\n%s", i, n, body)
					}
				}
			} else {
				for _, col := range []string{"succeeded", "failed", "pending"} {
					// The header is upper-cased in table mode and lower-cased
					// in csv, so match case-insensitively rather than per
					// format.
					if !strings.Contains(strings.ToLower(body), col) {
						t.Errorf("-o %s dropped the %s column:\n%s", format, col, body)
					}
				}
			}
			// The count that exists must still be rendered, so a fix that
			// blanks every row's counts does not pass.
			if !strings.Contains(body, "12") {
				t.Errorf("-o %s lost the DEPLOYED row's succeeded count:\n%s", format, body)
			}
			// And a NOT_DEPLOYED row must not claim zero successes.
			if !strings.Contains(body, notApplicable) {
				t.Errorf("-o %s rendered no not-applicable placeholder, so a NOT_DEPLOYED row reads as 0 succeeded:\n%s", format, body)
			}
		})
	}
}

// TestBlueprintStatusStructuredOutputOmitsInapplicableCounts pins the other
// half: json and yaml keep the shape they have always emitted, where a
// NOT_DEPLOYED blueprint simply carries no count keys. Substituting the
// placeholder there would put an em dash where a consumer expects a number.
func TestBlueprintStatusStructuredOutputOmitsInapplicableCounts(t *testing.T) {
	for _, format := range []string{"json", "json-multi", "yaml", "ndjson"} {
		t.Run(format, func(t *testing.T) {
			rows := blueprintStatusRowsForFormat(blueprintStatusCanonicalRows(), format)

			if _, ok := rows[0]["succeeded"]; ok {
				t.Errorf("-o %s gave the NOT_DEPLOYED row a succeeded key: %v", format, rows[0])
			}
			if got := rows[1]["succeeded"]; got != 12 {
				t.Errorf("-o %s: DEPLOYED row succeeded = %v, want 12", format, got)
			}
			for _, row := range rows {
				for k, v := range row {
					if v == notApplicable {
						t.Errorf("-o %s put the placeholder in %s: %v", format, k, row)
					}
				}
			}
		})
	}
}

// TestBlueprintStatusRowsForFormatDoesNotMutateItsInput guards the ordering the
// command depends on: it builds the canonical rows once and the resolver is the
// only thing that decides how a nil reads, so a resolver writing back into the
// caller's maps would make the format decision sticky.
func TestBlueprintStatusRowsForFormatDoesNotMutateItsInput(t *testing.T) {
	rows := blueprintStatusCanonicalRows()
	before, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	blueprintStatusRowsForFormat(rows, "table")

	after, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("input mutated:\n before %s\n after  %s", before, after)
	}
}

// TestReportBlueprintStatusRendersEveryColumnFromTheWire drives the command
// itself over the wire shape issue #356 reported: a NOT_DEPLOYED blueprint
// listed first, a DEPLOYED one after it. The resolver tests above take the
// canonical rows as given, so only this one fails if the command stops
// carrying a key on the row that has no count.
func TestReportBlueprintStatusRendersEveryColumnFromTheWire(t *testing.T) {
	restoreOutputFlags(t)
	outputFmt = "table"

	sdk, mux := newTestPlatformSDK(t)

	mux.HandleFunc("/blueprints/v1/blueprints", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"totalCount": 2,
			"results": []map[string]any{
				{"id": "bp-1", "name": "Restrictions - Example", "deploymentState": map[string]any{"state": "NOT_DEPLOYED"}},
				{"id": "bp-2", "name": "App Settings - Example", "deploymentState": map[string]any{"state": "DEPLOYED"}},
			},
		})
	})
	mux.HandleFunc("/blueprints/v1/blueprints/bp-1", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"id": "bp-1", "state": "NOT_DEPLOYED", "steps": []map[string]any{{}}})
	})
	mux.HandleFunc("/blueprints/v1/blueprints/bp-2", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"id":    "bp-2",
			"state": "DEPLOYED",
			"scope": map[string]any{"deviceGroups": []map[string]any{{"id": "dg-1"}}},
			"steps": []map[string]any{{}, {}},
		})
	})
	mux.HandleFunc("/blueprints/v1/blueprints/bp-2/report", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"succeeded": 12, "failed": 0, "pending": 2})
	})

	var out strings.Builder
	f := output.New("table", true, false)
	f.SetWriter(&out)

	cmd := newReportBlueprintStatusCmd(&registry.CLIContext{
		PlatformSDKClient: sdk,
		Output:            &cliOutput{f},
	})
	cmd.SetContext(context.Background())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("blueprint-status: %v", err)
	}

	body := out.String()
	for _, col := range []string{"SUCCEEDED", "FAILED", "PENDING", "SCOPE", "STEPS"} {
		if !strings.Contains(body, col) {
			t.Errorf("the %s column is missing even though a DEPLOYED blueprint carries it:\n%s", col, body)
		}
	}
	if !strings.Contains(body, "12") {
		t.Errorf("the DEPLOYED row lost its succeeded count:\n%s", body)
	}
}
