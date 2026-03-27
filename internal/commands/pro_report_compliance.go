package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newReportDeviceComplianceCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var daysSinceCheckin int

	cmd := &cobra.Command{
		Use:   "device-compliance",
		Short: "Devices with stale check-ins or outdated OS versions",
		Long: `Report devices that have not checked in recently, including management
status and OS version for triage.

Use --days-since-checkin to control the stale threshold (default 14 days).

Output columns: name, serial, managed, os_version, last_contact, days_since_contact, stale`,
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
// produces a compliance row for each device indicating management status,
// OS version, and stale check-in status.
func runReportDeviceCompliance(ctx context.Context, client registry.HTTPClient, staleThresholdDays int) ([]map[string]any, error) {
	computers, err := FetchAllPaginated(ctx, client, "/v1/computers-inventory?section=GENERAL&section=HARDWARE&section=OPERATING_SYSTEM", 100)
	if err != nil {
		return nil, fmt.Errorf("fetching computer inventory: %w", err)
	}

	now := time.Now()
	threshold := time.Duration(staleThresholdDays) * 24 * time.Hour

	rows := make([]map[string]any, 0, len(computers))
	for _, c := range computers {
		general, _ := c["general"].(map[string]any)

		name := ""
		serial := ""
		lastContact := ""
		managed := false

		if general != nil {
			name, _ = general["name"].(string)
			lastContact, _ = general["lastContactTime"].(string)
			if rm, ok := general["remoteManagement"].(map[string]any); ok {
				managed, _ = rm["managed"].(bool)
			}
		}

		if hw, ok := c["hardware"].(map[string]any); ok {
			serial, _ = hw["serialNumber"].(string)
		}
		if serial == "" && general != nil {
			serial, _ = general["serialNumber"].(string)
		}
		if name == "" {
			name = extractID(c)
		}

		osVersion := ""
		if os, ok := c["operatingSystem"].(map[string]any); ok {
			osVersion, _ = os["version"].(string)
		}

		daysSince := "N/A"
		stale := false

		if lastContact != "" {
			t, err := time.Parse(time.RFC3339, lastContact)
			if err != nil {
				t, err = time.Parse("2006-01-02T15:04:05.999Z", lastContact)
			}
			if err == nil {
				elapsed := now.Sub(t)
				days := int(elapsed.Hours() / 24)
				daysSince = fmt.Sprintf("%d", days)
				stale = elapsed > threshold
			}
		}

		rows = append(rows, map[string]any{
			"name":               name,
			"serial":             serial,
			"managed":            managed,
			"os_version":         osVersion,
			"last_contact":       lastContact,
			"days_since_contact": daysSince,
			"stale":              stale,
		})
	}

	return rows, nil
}
