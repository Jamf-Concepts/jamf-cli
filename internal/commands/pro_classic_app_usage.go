// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/resolve"
)

const dateLayout = "2006-01-02"

// parseLastWindow converts a rolling-window string like "30d" or "2w" into
// start/end date strings (yyyy-mm-dd), with end anchored to now. Go's
// time.ParseDuration does not support day/week units, so we parse them here.
func parseLastWindow(last string, now time.Time) (string, string, error) {
	if last == "" {
		return "", "", fmt.Errorf("empty duration")
	}

	unit := last[len(last)-1]
	numStr := last[:len(last)-1]

	var mult int
	switch unit {
	case 'd':
		mult = 1
	case 'w':
		mult = 7
	default:
		return "", "", fmt.Errorf("invalid duration %q: use a number followed by 'd' (days) or 'w' (weeks), e.g. 30d or 2w", last)
	}
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return "", "", fmt.Errorf("invalid duration %q: %w", last, err)
	}
	days := n * mult

	if days <= 0 {
		return "", "", fmt.Errorf("invalid duration %q: must be positive", last)
	}

	end := now.Format(dateLayout)
	start := now.AddDate(0, 0, -days).Format(dateLayout)
	return start, end, nil
}

// resolveAppUsageRange validates the mutually exclusive date-range flags and
// returns the effective start/end date strings (yyyy-mm-dd).
func resolveAppUsageRange(start, end, last string, now time.Time) (string, string, error) {
	hasExplicit := start != "" || end != ""

	switch {
	case last != "" && hasExplicit:
		return "", "", fmt.Errorf("use either --last or --start/--end, not both")
	case last != "":
		return parseLastWindow(last, now)
	case start != "" && end != "":
		if _, err := time.Parse(dateLayout, start); err != nil {
			return "", "", fmt.Errorf("invalid --start %q: expected yyyy-mm-dd", start)
		}
		if _, err := time.Parse(dateLayout, end); err != nil {
			return "", "", fmt.Errorf("invalid --end %q: expected yyyy-mm-dd", end)
		}
		return start, end, nil
	case hasExplicit:
		return "", "", fmt.Errorf("--start and --end must be used together")
	default:
		return "", "", fmt.Errorf("a date range is required: pass --start and --end, or --last (e.g. 30d)")
	}
}

// fetchAppUsageRows builds the Classic app-usage path for the given numeric id
// and date range, fetches it, and flattens the response to rows.
func fetchAppUsageRows(ctx context.Context, client registry.HTTPClient, id, start, end string) ([]map[string]any, error) {
	// Negotiate JSON: the Classic client defaults to XML for /JSSResource paths,
	// but the app-usage endpoint returns the array-shaped body flattenAppUsage
	// expects only when JSON is requested.
	ctx = registry.WithAccept(ctx, "application/json")
	path := fmt.Sprintf("/JSSResource/computerapplicationusage/id/%s/%s_%s", id, start, end)
	data, err := fetchJSON(ctx, client, path)
	if err != nil {
		if strings.Contains(err.Error(), "HTTP 404") {
			return nil, fmt.Errorf("computer %s not found, or no usage data for %s to %s", id, start, end)
		}
		return nil, fmt.Errorf("fetching application usage: %w", err)
	}
	return flattenAppUsage(data), nil
}

func newClassicComputerAppUsageCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		id     string
		serial string
		udid   string
		name   string
		start  string
		end    string
		last   string
	)

	cmd := &cobra.Command{
		Use:   "classic-computer-app-usage",
		Short: "Computer application usage over a date range (Classic API)",
		Long: `Fetch per-day application usage for a computer from the Classic API.

Identify the computer with exactly one of --id, --serial, --udid, or --name.
Non-id identifiers are resolved to a computer id via inventory search.

Provide a date range with either --start and --end (yyyy-mm-dd), or --last
(a rolling window ending today, e.g. 30d or 2w).

Output columns: date, name, version, foreground, open (foreground is minutes,
open is launch count).`,
		Example: `  jamf-cli pro classic-computer-app-usage --id 42 --start 2026-06-01 --end 2026-06-30
  jamf-cli pro classic-computer-app-usage --serial C02XL0ABCDEF --last 30d`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate that exactly one identifier flag is set; resolveAppUsageComputerID re-reads them.
			if err := selectAppUsageIdentifier(id, serial, udid, name); err != nil {
				return err
			}

			startDate, endDate, err := resolveAppUsageRange(start, end, last, time.Now())
			if err != nil {
				return err
			}

			ctx := cmd.Context()

			resolvedID, err := resolveAppUsageComputerID(ctx, cliCtx.Client, id, serial, udid, name)
			if err != nil {
				return err
			}

			rows, err := fetchAppUsageRows(ctx, cliCtx.Client, resolvedID, startDate, endDate)
			if err != nil {
				return err
			}

			return printRows(cliCtx, rows)
		},
	}

	cmd.Flags().StringVar(&id, "id", "", "computer id")
	cmd.Flags().StringVar(&serial, "serial", "", "computer serial number")
	cmd.Flags().StringVar(&udid, "udid", "", "computer UDID")
	cmd.Flags().StringVar(&name, "name", "", "computer name")
	cmd.Flags().StringVar(&start, "start", "", "range start date (yyyy-mm-dd)")
	cmd.Flags().StringVar(&end, "end", "", "range end date (yyyy-mm-dd)")
	cmd.Flags().StringVar(&last, "last", "", "rolling window ending today, e.g. 30d or 2w")

	return cmd
}

// resolveAppUsageComputerID resolves the appropriate identifier flag to a
// numeric Jamf computer ID string. Each flag type routes to the correct
// resolution path:
//   - id: used directly (no network call)
//   - udid: resolved via the v3 inventory UDID filter
//   - serial / name: resolved via resolveDeviceByIdentifier (id → serial → name)
func resolveAppUsageComputerID(ctx context.Context, client registry.HTTPClient, id, serial, udid, name string) (string, error) {
	switch {
	case id != "":
		return id, nil
	case udid != "":
		di, err := resolve.ResolveComputerByUDID(ctx, client, udid)
		if err != nil {
			return "", err
		}
		return di.ID, nil
	default:
		// serial or name — resolveDeviceByIdentifier handles both via RSQL filters
		identifier := serial
		if identifier == "" {
			identifier = name
		}
		resolvedID, _, err := resolveDeviceByIdentifier(ctx, client, identifier)
		return resolvedID, err
	}
}

// selectAppUsageIdentifier returns an error if zero or more than one identifier flag was set.
func selectAppUsageIdentifier(id, serial, udid, name string) error {
	count := 0
	for _, v := range []string{id, serial, udid, name} {
		if v != "" {
			count++
		}
	}
	if count == 0 {
		return fmt.Errorf("a computer is required: pass one of --id, --serial, --udid, or --name")
	}
	if count > 1 {
		return fmt.Errorf("pass exactly one of --id, --serial, --udid, or --name")
	}
	return nil
}

// asSlice coerces v to []any. The Classic API JSON serializer can collapse a
// single-element array to a plain object; this helper tolerates both shapes.
func asSlice(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case map[string]any:
		return []any{t} // Classic JSON may collapse a single element to an object
	default:
		return nil
	}
}

// flattenAppUsage turns the nested Classic response (a list of days, each with
// a list of apps) into flat rows, one per (date, app). Returns a non-nil empty
// slice when there is no usage data.
func flattenAppUsage(data map[string]any) []map[string]any {
	rows := []map[string]any{}

	days := asSlice(data["computer_application_usage"])
	if days == nil {
		return rows
	}

	for _, d := range days {
		day, ok := d.(map[string]any)
		if !ok {
			continue
		}
		date, _ := day["date"].(string)

		apps := asSlice(day["apps"])
		if apps == nil {
			continue
		}
		for _, a := range apps {
			app, ok := a.(map[string]any)
			if !ok {
				continue
			}
			rows = append(rows, map[string]any{
				"date":       date,
				"name":       app["name"],
				"version":    app["version"],
				"foreground": app["foreground"],
				"open":       app["open"],
			})
		}
	}

	return rows
}
