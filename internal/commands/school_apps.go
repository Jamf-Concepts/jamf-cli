// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/school"
	"github.com/Jamf-Concepts/jamfschool-go-sdk/jamfschool"
)

func newSchoolAppsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apps",
		Short: "Manage Jamf School apps",
	}

	cmd.AddCommand(newSchoolAppsListCmd(cliCtx))
	cmd.AddCommand(newSchoolAppsGetCmd(cliCtx))
	cmd.AddCommand(newSchoolAppsCreateCmd(cliCtx))
	cmd.AddCommand(newSchoolAppsTrashCmd(cliCtx))

	return cmd
}

func newSchoolAppsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all apps",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.SchoolClient.GetApps(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(items))
			for _, a := range items {
				rows = append(rows, flattenSchoolApp(a))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

func flattenSchoolApp(a jamfschool.App) map[string]any {
	return map[string]any{
		"id":         a.ID,
		"name":       a.Name,
		"bundleId":   a.BundleID,
		"adamId":     a.AdamID,
		"vendor":     a.Vendor,
		"version":    a.Version,
		"platform":   a.Platform,
		"locationId": a.LocationID,
	}
}

func newSchoolAppsGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get an app by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := school.NewResolver(cliCtx.SchoolClient)

			id, err := r.ResolveAppID(ctx, args[0])
			if err != nil {
				return err
			}

			item, err := cliCtx.SchoolClient.GetApp(ctx, id)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, item, flattenSchoolApp(*item))
		},
	}
}

func newSchoolAppsCreateCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		scaffold bool
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Add an App Store app",
		Long:  "Add an app from the App Store by providing its Adam ID and country code.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scaffold {
				return printExport(jamfschool.AppCreateInput{})
			}
			ctx := cmd.Context()
			data, err := readInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfschool.AppCreateInput
			if err := unmarshalInput(data, &input); err != nil {
				return fmt.Errorf("parsing input: %w", err)
			}

			if input.AdamID == 0 {
				return fmt.Errorf("input must include an 'AdamID' field")
			}

			newID, err := cliCtx.SchoolClient.CreateApp(ctx, input)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Created app (ID: %d)\n", newID)

			item, err := cliCtx.SchoolClient.GetApp(ctx, newID)
			if err != nil {
				return nil
			}
			return printResult(cliCtx.Output, item, flattenSchoolApp(*item))
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "Print an empty JSON template and exit")

	return cmd
}

func newSchoolAppsTrashCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "trash <name>",
		Short: "Move an app to trash",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := school.NewResolver(cliCtx.SchoolClient)

			id, err := r.ResolveAppID(ctx, args[0])
			if err != nil {
				return err
			}

			proceed, err := confirmDelete("app", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			if err := cliCtx.SchoolClient.TrashApp(ctx, id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Trashed app %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}
