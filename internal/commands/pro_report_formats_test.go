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

// TestEveryMultiSectionReportHonoursItsFormat drives the function that calls
// printSection in each multi-section report. No other test does:
// runReportPatchStatusFull was 0% covered and the rest reached only -o table.
//
// Two assertions per report. Under json the output is one top-level document,
// because a caller reads it with jq. Under csv no box-drawing line reaches the
// stream, because csv.Reader yields a one-field row for one.
//
// The structs are zero-valued. Each report prints its summary section
// unconditionally, so an empty struct reaches printSection without a mock, and
// the routing is what this test covers.
//
// This replaces an AST rule that refused a banner written before printRows. It
// catches the same regression at every printSection caller except the one in
// newReportDDMStatusCmd's RunE, which needs a platform client to reach.
func TestEveryMultiSectionReportHonoursItsFormat(t *testing.T) {
	reports := map[string]func(*registry.CLIContext) error{
		"security": func(c *registry.CLIContext) error {
			return printSecurityReport(c, &securityReport{})
		},
		"mdm-profile": func(c *registry.CLIContext) error {
			return printMDMHealthReport(c, &mdmHealthReport{}, "profile")
		},
		"policy": func(c *registry.CLIContext) error {
			return printPolicyHealthReport(c, &policyHealthReport{}, false)
		},
		"patch-status": func(c *registry.CLIContext) error {
			return runReportPatchStatusFull(context.Background(), c, true)
		},
		"update-status": func(c *registry.CLIContext) error {
			return runReportUpdateStatus(context.Background(), c, false, -1)
		},
	}

	for name, run := range reports {
		for _, format := range []string{"json", "csv"} {
			t.Run(name+"/"+format, func(t *testing.T) {
				restoreOutputFlags(t)
				outputFmt, quiet = format, true

				var out strings.Builder
				formatter := output.New(format, true, false)
				formatter.SetWriter(&out)
				cliCtx := &registry.CLIContext{
					Client: multiSectionReportMock(),
					Output: &cliOutput{formatter},
				}

				if err := run(cliCtx); err != nil {
					t.Fatalf("%s -o %s: %v", name, format, err)
				}

				if format == "csv" {
					if strings.Contains(out.String(), "──") {
						t.Errorf("%s -o csv put box-drawing lines into the stream, so csv.reader yields a one-field row:\n%s",
							name, out.String())
					}
					return
				}

				// N concatenated documents parse as far as the first, then fail.
				body := strings.TrimSpace(out.String())
				if body == "" {
					return // a report with nothing to say is allowed to say nothing
				}
				var doc any
				if err := json.Unmarshal([]byte(body), &doc); err != nil {
					t.Errorf("%s -o json is not one JSON document, which is what jq rejects: %v\n%s", name, err, body)
				}
			})
		}
	}
}

// multiSectionReportMock answers every collection the two client-driven
// reports read.
//
// update-status needs a non-empty result set: it returns before printSection
// when both of its collections are empty, so an empty mock made its case
// vacuous. The banner mutation failed in the other four and passed there.
func multiSectionReportMock() *overviewMockClient {
	empty := overviewMockResponse{200, `{"totalCount":0,"results":[]}`}
	return &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v3/patch-software-title-configurations": empty,
			"/v2/patch-policies":                      empty,
			"/v1/managed-software-updates/update-statuses": {200, `{
				"totalCount": 1,
				"results": [{"device": {"deviceId": "1", "objectType": "COMPUTER"}, "status": "PENDING", "updateAction": "DOWNLOAD_INSTALL"}]
			}`},
			"/v1/managed-software-updates/plans": {200, `{
				"totalCount": 1,
				"results": [{"planUuid": "p1", "device": {"deviceId": "1", "objectType": "COMPUTER"}, "status": {"state": "PlanCompleted"}}]
			}`},
			"/v2/computers-inventory": {200, `{
				"totalCount": 1,
				"results": [{"id": "1", "general": {"name": "Mac-01"}, "hardware": {"serialNumber": "S1"}}]
			}`},
			"/v2/mobile-devices": {200, `[]`},
		},
	}
}
