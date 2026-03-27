package commands

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newReportSoftwareInstallsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		titleFilter   string
		includeSystem bool
	)

	cmd := &cobra.Command{
		Use:   "software-installs",
		Short: "Installed software version distribution",
		Long: `Report the distribution of installed software versions across the fleet.

By default, system apps (installed in /System/ or /Library/) are excluded.
Use --include-system to show all applications.

Use --title to filter to a specific application name (case-insensitive substring match).

Output columns: title, version, device_count`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rows, err := runReportSoftwareInstalls(cmd.Context(), cliCtx.Client, titleFilter, includeSystem)
			if err != nil {
				return err
			}
			formatter := output.New(outputFmt, noColor, wide)
			return formatter.Print(rows)
		},
	}

	cmd.Flags().StringVar(&titleFilter, "title", "", "filter to application names containing this substring (case-insensitive)")
	cmd.Flags().BoolVar(&includeSystem, "include-system", false, "include system apps from /System/ and /Library/")

	return cmd
}

// isSystemApp returns true for apps installed in macOS system paths.
func isSystemApp(path string) bool {
	return strings.HasPrefix(path, "/System/") ||
		strings.HasPrefix(path, "/Library/") ||
		strings.HasPrefix(path, "/usr/") ||
		strings.HasPrefix(path, "/bin/") ||
		strings.HasPrefix(path, "/Applications/Utilities/")
}

// softwareKey groups installs by application name + version.
type softwareKey struct {
	title   string
	version string
}

// runReportSoftwareInstalls fetches computer inventory with the APPLICATIONS
// section and aggregates device counts per (title, version).
func runReportSoftwareInstalls(ctx context.Context, client registry.HTTPClient, titleFilter string, includeSystem bool) ([]map[string]interface{}, error) {
	computers, err := FetchAllPaginated(ctx, client, "/v1/computers-inventory?section=APPLICATIONS", 100)
	if err != nil {
		return nil, fmt.Errorf("fetching computer inventory: %w", err)
	}

	counts := make(map[softwareKey]int)
	filterLower := strings.ToLower(titleFilter)

	for _, c := range computers {
		apps, _ := c["applications"].([]interface{})
		for _, a := range apps {
			app, ok := a.(map[string]interface{})
			if !ok {
				continue
			}
			name, _ := app["name"].(string)
			version, _ := app["version"].(string)
			path, _ := app["path"].(string)

			if name == "" {
				continue
			}
			if !includeSystem && isSystemApp(path) {
				continue
			}
			if filterLower != "" && !strings.Contains(strings.ToLower(name), filterLower) {
				continue
			}

			counts[softwareKey{name, version}]++
		}
	}

	// Sort: title asc, version desc (newest first).
	keys := make([]softwareKey, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].title != keys[j].title {
			return keys[i].title < keys[j].title
		}
		return keys[i].version > keys[j].version
	})

	rows := make([]map[string]interface{}, 0, len(keys))
	for _, k := range keys {
		rows = append(rows, map[string]interface{}{
			"title":        k.title,
			"version":      k.version,
			"device_count": counts[k],
		})
	}

	if len(rows) == 0 && titleFilter != "" {
		return nil, fmt.Errorf("no software found matching %q", titleFilter)
	}

	return rows, nil
}
