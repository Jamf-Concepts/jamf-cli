// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newReportDuplicateSerialsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "duplicate-serials",
		Short: "Computer records sharing a serial number",
		Long: `List computer records that share a serial number with another record.

A replaced logic board re-enrolls as a fresh record, so the old and new records
collide on serial number. Duplicates break every serial-based lookup
(get/update/patch --serial and device actions) because the serial no longer
resolves to a single device. Jamf smart groups cannot surface this — their
criteria are evaluated per record, not across the fleet.

Only serials shared by two or more records are listed, grouped so duplicates of
the same serial appear together. Delete the stale record for each serial to
restore unambiguous lookups.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rows, err := runReportDuplicateSerials(cmd.Context(), cliCtx.Client)
			if err != nil {
				return err
			}
			formatter := output.New(outputFmt, noColor, wide)
			return formatter.Print(rows)
		},
	}
	return cmd
}

// runReportDuplicateSerials fetches all computer records, groups them by serial
// number, and returns one row per record for every serial shared by more than
// one record. Blank serials are ignored — pending/placeholder records commonly
// share an empty serial. Rows are ordered by serial, then by numeric ID, so
// duplicates cluster together.
func runReportDuplicateSerials(ctx context.Context, client registry.HTTPClient) ([]map[string]any, error) {
	computers, err := FetchAllPaginated(ctx, client, "/v3/computers-inventory?section=GENERAL&section=HARDWARE", 100)
	if err != nil {
		return nil, fmt.Errorf("fetching computer inventory: %w", err)
	}

	type record struct {
		id          string
		name        string
		lastContact string
	}
	bySerial := make(map[string][]record)
	for _, c := range computers {
		hw, _ := c["hardware"].(map[string]any)
		if hw == nil {
			continue
		}
		serial, _ := hw["serialNumber"].(string)
		serial = strings.TrimSpace(serial)
		if serial == "" {
			continue
		}
		rec := record{id: extractID(c)}
		if gen, ok := c["general"].(map[string]any); ok {
			rec.name, _ = gen["name"].(string)
			rec.lastContact, _ = gen["lastContactTime"].(string)
		}
		if rec.name == "" {
			rec.name = rec.id
		}
		bySerial[serial] = append(bySerial[serial], rec)
	}

	// Keep only serials with more than one record, sorted for stable output.
	dupSerials := make([]string, 0)
	for serial, recs := range bySerial {
		if len(recs) > 1 {
			dupSerials = append(dupSerials, serial)
		}
	}
	sort.Strings(dupSerials)

	rows := make([]map[string]any, 0)
	for _, serial := range dupSerials {
		recs := bySerial[serial]
		sort.Slice(recs, func(i, j int) bool {
			return idLess(recs[i].id, recs[j].id)
		})
		for _, rec := range recs {
			rows = append(rows, map[string]any{
				"serial":       serial,
				"id":           rec.id,
				"name":         rec.name,
				"last_contact": rec.lastContact,
			})
		}
	}
	return rows, nil
}

// idLess orders IDs numerically when both parse as integers, otherwise lexically.
func idLess(a, b string) bool {
	ai, aerr := strconv.Atoi(a)
	bi, berr := strconv.Atoi(b)
	if aerr == nil && berr == nil {
		return ai < bi
	}
	return a < b
}
