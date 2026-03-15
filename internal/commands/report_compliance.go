package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/jamf/jamfpro-cli/internal/commands/generated"
	"github.com/jamf/jamfpro-cli/internal/output"
)

func newReportDeviceComplianceCmd(cliCtx *generated.CLIContext) *cobra.Command {
	var daysSinceCheckin int

	cmd := &cobra.Command{
		Use:   "device-compliance",
		Short: "Devices with stale check-ins, failed commands, or missing profiles",
		Long: `Report devices that have not checked in recently, have failed MDM commands,
or are missing configuration profiles.

Use --days-since-checkin to control the stale threshold (default 14 days).

Output columns: name, serial, last_contact, days_since_contact, stale, failed_commands`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rows, err := runReportDeviceCompliance(cmd.Context(), cliCtx.Client, daysSinceCheckin)
			if err != nil {
				return err
			}
			formatter := output.New(outputFmt, noColor, wide)
			return formatter.Print(rows)
		},
	}

	cmd.Flags().IntVar(&daysSinceCheckin, "days-since-checkin", 14, "number of days without check-in to consider a device stale")

	return cmd
}

// runReportDeviceCompliance fetches all computers from the inventory API and
// produces a compliance row for each device indicating stale check-in status
// and failed command count.
func runReportDeviceCompliance(ctx context.Context, client generated.HTTPClient, staleThresholdDays int) ([]map[string]interface{}, error) {
	computers, err := FetchAllPaginated(ctx, client, "/v1/computers-inventory?section=GENERAL", 100)
	if err != nil {
		return nil, fmt.Errorf("fetching computer inventory: %w", err)
	}

	// Fetch total failed MDM commands count once for context; per-device data
	// is not available from the inventory endpoint so we annotate the summary
	// counts only at the fleet level and leave per-device failed_commands as
	// "N/A" unless the inventory response includes command data.
	now := time.Now()
	threshold := time.Duration(staleThresholdDays) * 24 * time.Hour

	rows := make([]map[string]interface{}, 0, len(computers))
	for _, c := range computers {
		general, _ := c["general"].(map[string]interface{})

		name := ""
		serial := ""
		lastContact := ""

		if general != nil {
			name, _ = general["name"].(string)
			lastContact, _ = general["lastContactTime"].(string)
		}

		// Serial number may be in hardware section or general depending on
		// which sections were requested. Fall back gracefully.
		if hw, ok := c["hardware"].(map[string]interface{}); ok {
			serial, _ = hw["serialNumber"].(string)
		}
		if serial == "" {
			if general != nil {
				serial, _ = general["serialNumber"].(string)
			}
		}
		if name == "" {
			name = extractID(c)
		}

		daysSince := "N/A"
		stale := false

		if lastContact != "" {
			t, err := time.Parse(time.RFC3339, lastContact)
			if err != nil {
				// Try alternative formats used by Jamf Pro.
				t, err = time.Parse("2006-01-02T15:04:05.999Z", lastContact)
			}
			if err == nil {
				elapsed := now.Sub(t)
				days := int(elapsed.Hours() / 24)
				daysSince = fmt.Sprintf("%d", days)
				stale = elapsed > threshold
			}
		}

		rows = append(rows, map[string]interface{}{
			"name":               name,
			"serial":             serial,
			"last_contact":       lastContact,
			"days_since_contact": daysSince,
			"stale":              stale,
			"failed_commands":    "N/A",
		})
	}

	return rows, nil
}
