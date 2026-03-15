package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jamf/jamfpro-cli/internal/commands/generated"
	"github.com/jamf/jamfpro-cli/internal/output"
)

func newReportEAResultsCmd(cliCtx *generated.CLIContext) *cobra.Command {
	var nameFilter string

	cmd := &cobra.Command{
		Use:   "ea-results",
		Short: "Extension attribute results across devices",
		Long: `Fetch extension attribute definitions and their values across all computers.

Use --name to filter to extension attributes whose name contains the given substring
(case-insensitive).

Output columns: ea_name, ea_id, device, value`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rows, err := runReportEAResults(cmd.Context(), cliCtx.Client, nameFilter)
			if err != nil {
				return err
			}
			formatter := output.New(outputFmt, noColor, wide)
			return formatter.Print(rows)
		},
	}

	cmd.Flags().StringVar(&nameFilter, "name", "", "filter to EA names containing this substring (case-insensitive)")

	return cmd
}

// runReportEAResults fetches computer extension attribute definitions, then
// queries each computer's inventory to collect EA values.
func runReportEAResults(ctx context.Context, client generated.HTTPClient, nameFilter string) ([]map[string]interface{}, error) {
	// Fetch EA definitions from the Classic API.
	eaItems, err := FetchClassicList(ctx, client, "/JSSResource/computerextensionattributes", "computer_extension_attributes")
	if err != nil {
		return nil, fmt.Errorf("fetching extension attribute definitions: %w", err)
	}

	filterLower := strings.ToLower(nameFilter)

	// Build a set of EA IDs/names we care about.
	type eaDef struct {
		id   string
		name string
	}
	var targetEAs []eaDef
	for _, item := range eaItems {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		eaName, _ := m["name"].(string)
		eaID := extractID(m)

		if filterLower != "" && !strings.Contains(strings.ToLower(eaName), filterLower) {
			continue
		}
		targetEAs = append(targetEAs, eaDef{id: eaID, name: eaName})
	}

	if len(targetEAs) == 0 {
		if nameFilter != "" {
			return nil, fmt.Errorf("no extension attributes found matching %q", nameFilter)
		}
		// No EAs configured at all — return empty result.
		return []map[string]interface{}{}, nil
	}

	// Fetch computer inventory with the EXTENSION_ATTRIBUTES section.
	computers, err := FetchAllPaginated(ctx, client, "/v1/computers-inventory?section=GENERAL&section=EXTENSION_ATTRIBUTES", 100)
	if err != nil {
		return nil, fmt.Errorf("fetching computer inventory: %w", err)
	}

	// Build a lookup of EA IDs we care about for fast filtering.
	targetEAIDs := make(map[string]string, len(targetEAs)) // id -> name
	for _, ea := range targetEAs {
		targetEAIDs[ea.id] = ea.name
	}

	var rows []map[string]interface{}

	for _, c := range computers {
		deviceName := ""
		if general, ok := c["general"].(map[string]interface{}); ok {
			deviceName, _ = general["name"].(string)
		}
		if deviceName == "" {
			deviceName = extractID(c)
		}

		eas, _ := c["extensionAttributes"].([]interface{})
		for _, e := range eas {
			ea, ok := e.(map[string]interface{})
			if !ok {
				continue
			}
			eaID := extractID(ea)
			eaName, _ := ea["name"].(string)

			// Apply filter: check both the definition list and the per-device name.
			if filterLower != "" {
				if _, wantByID := targetEAIDs[eaID]; !wantByID {
					if !strings.Contains(strings.ToLower(eaName), filterLower) {
						continue
					}
				}
			} else if len(targetEAs) > 0 {
				// No filter — emit all EAs (targetEAs contains all of them).
				// We still want to use the canonical name from definitions if
				// available.
				if canonicalName, ok := targetEAIDs[eaID]; ok && canonicalName != "" {
					eaName = canonicalName
				}
			}

			value := ""
			if v, ok := ea["value"].(string); ok {
				value = v
			} else if ea["value"] != nil {
				value = fmt.Sprintf("%v", ea["value"])
			}

			rows = append(rows, map[string]interface{}{
				"ea_name": eaName,
				"ea_id":   eaID,
				"device":  deviceName,
				"value":   value,
			})
		}
	}

	return rows, nil
}
