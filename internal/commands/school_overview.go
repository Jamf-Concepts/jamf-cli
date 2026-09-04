// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/blueprints"
)

// runSchoolOverview executes all Jamf School API calls in parallel and returns
// grouped sections for display.
func runSchoolOverview(cmd *cobra.Command, cliCtx *registry.CLIContext) ([]overviewSection, error) {
	ctx := cmd.Context()
	client := cliCtx.SchoolClient

	var mu sync.Mutex
	results := make(map[string]string)
	var wg sync.WaitGroup

	send := func(key, value string, err error) {
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			results[key] = "N/A"
		} else {
			results[key] = value
		}
	}

	// Devices
	wg.Go(func() {
		devices, err := client.GetDevices(ctx)
		send("devices", formatCount(len(devices)), err)
	})

	wg.Go(func() {
		groups, err := client.GetDeviceGroups(ctx)
		send("device_groups", formatCount(len(groups)), err)
	})

	// Users & Organization
	wg.Go(func() {
		users, err := client.GetUsers(ctx)
		send("users", formatCount(len(users)), err)
	})

	wg.Go(func() {
		groups, err := client.GetGroups(ctx)
		send("user_groups", formatCount(len(groups)), err)
	})

	wg.Go(func() {
		classes, err := client.GetClasses(ctx)
		send("classes", formatCount(len(classes)), err)
	})

	// Content
	wg.Go(func() {
		profiles, err := client.GetProfiles(ctx)
		send("profiles", formatCount(len(profiles)), err)
	})

	wg.Go(func() {
		apps, err := client.GetApps(ctx)
		send("apps", formatCount(len(apps)), err)
	})

	// Infrastructure
	wg.Go(func() {
		locations, err := client.GetLocations(ctx)
		send("locations", formatCount(len(locations)), err)
	})

	wg.Go(func() {
		depDevices, err := client.GetDEPDevices(ctx)
		send("dep_devices", formatCount(len(depDevices)), err)
	})

	// Platform (only when PlatformSDKClient is configured)
	if pc := cliCtx.PlatformSDKClient; pc != nil {
		wg.Go(func() {
			bps, err := blueprints.New(pc).ListBlueprints(ctx, nil, "")
			send("blueprints", formatCount(len(bps)), err)
		})
	}

	wg.Wait()

	get := func(key string) string {
		if v, ok := results[key]; ok {
			return v
		}
		return "N/A"
	}

	item := func(resource, value string) overviewItem {
		return overviewItem{resource, value, ""}
	}

	sections := []overviewSection{
		{
			Name: "Devices",
			Items: []overviewItem{
				item("Devices", get("devices")),
				item("Device Groups", get("device_groups")),
			},
		},
		{
			Name: "Users & Organization",
			Items: []overviewItem{
				item("Users", get("users")),
				item("User Groups", get("user_groups")),
				item("Classes", get("classes")),
			},
		},
		{
			Name: "Content",
			Items: []overviewItem{
				item("Profiles", get("profiles")),
				item("Apps", get("apps")),
			},
		},
		{
			Name: "Infrastructure",
			Items: []overviewItem{
				item("Locations", get("locations")),
				item("DEP Devices", get("dep_devices")),
			},
		},
	}

	// Only show Platform section when platform credentials are configured
	if cliCtx.PlatformSDKClient != nil {
		sections = append(sections, overviewSection{
			Name: "Platform",
			Items: []overviewItem{
				item("Blueprints", get("blueprints")),
			},
		})
	}

	return sections, nil
}

// printSchoolOverviewTable renders a grouped overview table for Jamf School.
func printSchoolOverviewTable(w io.Writer, sections []overviewSection, useColor bool) {
	colorize := func(text, code string) string {
		if !useColor {
			return text
		}
		return code + text + "\033[0m"
	}

	bold := "\033[1m"
	dim := "\033[2m"

	const labelWidth = 34
	const totalWidth = 72

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, colorize("  JAMF SCHOOL OVERVIEW", bold))
	_, _ = fmt.Fprintln(w, colorize("  "+strings.Repeat("━", totalWidth), dim))

	for _, section := range sections {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintf(w, "  %s\n", colorize(section.Name, bold))
		_, _ = fmt.Fprintln(w, colorize("  "+strings.Repeat("─", totalWidth), dim))

		for _, item := range section.Items {
			displayValue := item.Value
			visibleLen := len(item.Value)

			if item.Value == "N/A" {
				displayValue = colorize(item.Value, dim)
			}

			padding := totalWidth - labelWidth - visibleLen
			if padding >= 1 {
				_, _ = fmt.Fprintf(w, "  %-*s%*s%s\n", labelWidth, item.Resource, padding, "", displayValue)
			} else {
				_, _ = fmt.Fprintf(w, "  %-*s %s\n", labelWidth, item.Resource, displayValue)
			}
		}
	}
	_, _ = fmt.Fprintln(w)
}

func newSchoolOverviewCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "overview",
		Short: "Show a summary of the Jamf School instance",
		Long: `Display a grouped summary of your Jamf School instance including device
counts, user counts, classes, profiles, apps, and locations.

Makes parallel API calls for fast results. Items that fail to load show "N/A".

With no -o flag, this command writes a grouped table. Then --out-file receives
that table, not JSON. Use -o json to write structured data to the file.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			sections, err := runSchoolOverview(cmd, cliCtx)
			if err != nil {
				return err
			}

			if !cmd.Flags().Changed("output") || outputFmt == "table" {
				printSchoolOverviewTable(writerFor(cliCtx), sections, !noColor)
				return nil
			}

			rows := overviewToRows(sections)
			return printRows(cliCtx, rows)
		},
	}
}
