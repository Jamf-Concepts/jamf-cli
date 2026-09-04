// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newReportSoftwareInstallsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		titleFilter   string
		includeSystem bool
		showBundleID  bool
		showPath      bool
	)

	cmd := &cobra.Command{
		Use:   "software-installs",
		Short: "Installed software version distribution",
		Long: `Report the distribution of installed software versions across the fleet.

By default, system apps (installed in /System/ or /Library/) are excluded.
Use --include-system to show all applications.

Use --title to filter to a specific application name (case-insensitive substring match).

Use --bundle-id to add a bundle_id column, and --path to add a path column.
Each also becomes part of the grouping, so one title and version that spans
several bundle IDs or install paths reports a row for each rather than a single
merged row. Neither flag is on by default, so the default output is unchanged.

Output columns: title, version, device_count (plus bundle_id with --bundle-id,
path with --path)`,
		// --bundle-id and --path are boolean, so their names invite a value that
		// cobra would leave as a positional. Without this the whole report runs
		// as if the value had never been typed.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			rows, err := runReportSoftwareInstalls(cmd.Context(), cliCtx.Client, titleFilter, includeSystem, showBundleID, showPath)
			if err != nil {
				return err
			}
			return printRows(cliCtx, rows)
		},
	}

	cmd.Flags().StringVar(&titleFilter, "title", "", "filter to application names containing this substring (case-insensitive)")
	cmd.Flags().BoolVar(&includeSystem, "include-system", false, "include system apps from /System/ and /Library/")
	cmd.Flags().BoolVar(&showBundleID, "bundle-id", false, "add a bundle_id column and group by bundle ID")
	cmd.Flags().BoolVar(&showPath, "path", false, "add a path column and group by install path")

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
//
// --bundle-id and --path extend this key rather than adding a display column:
// one title + version can span several bundle IDs or install paths, and a
// column would print one arbitrary member and silently hide the rest.
type softwareKey struct {
	title    string
	version  string
	bundleID string
	path     string
}

// runReportSoftwareInstalls fetches computer inventory with the APPLICATIONS
// section and aggregates device counts per softwareKey.
func runReportSoftwareInstalls(ctx context.Context, client registry.HTTPClient, titleFilter string, includeSystem, showBundleID, showPath bool) ([]map[string]any, error) {
	computers, err := FetchAllPaginated(ctx, client, "/v4/computers-inventory?section=APPLICATIONS", 100)
	if err != nil {
		return nil, fmt.Errorf("fetching computer inventory: %w", err)
	}

	counts := make(map[softwareKey]int)
	filterLower := strings.ToLower(titleFilter)

	for _, c := range computers {
		apps, _ := c["applications"].([]any)
		for _, a := range apps {
			app, ok := a.(map[string]any)
			if !ok {
				continue
			}
			name, _ := app["name"].(string)
			version, _ := app["version"].(string)
			path, _ := app["path"].(string)
			bundleID, _ := app["bundleId"].(string)

			if name == "" {
				continue
			}
			if !includeSystem && isSystemApp(path) {
				continue
			}
			if filterLower != "" && !strings.Contains(strings.ToLower(name), filterLower) {
				continue
			}

			key := softwareKey{title: name, version: version}
			if showBundleID {
				key.bundleID = bundleID
			}
			if showPath {
				key.path = path
			}
			counts[key]++
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
		if keys[i].version != keys[j].version {
			return keys[i].version > keys[j].version
		}
		if keys[i].bundleID != keys[j].bundleID {
			return keys[i].bundleID < keys[j].bundleID
		}
		return keys[i].path < keys[j].path
	})

	rows := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		row := map[string]any{
			"title":        k.title,
			"version":      k.version,
			"device_count": counts[k],
		}
		if showBundleID {
			row["bundle_id"] = k.bundleID
		}
		if showPath {
			row["path"] = k.path
		}
		rows = append(rows, row)
	}

	if len(rows) == 0 && titleFilter != "" {
		return nil, fmt.Errorf("no software found matching %q", titleFilter)
	}

	return rows, nil
}
