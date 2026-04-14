// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/school"
	"github.com/Jamf-Concepts/jamfschool-go-sdk/jamfschool"
)

func newSchoolIBeaconsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ibeacons",
		Short: "Manage Jamf School iBeacons",
	}

	cmd.AddCommand(newSchoolIBeaconsListCmd(cliCtx))
	cmd.AddCommand(newSchoolIBeaconsGetCmd(cliCtx))
	cmd.AddCommand(newSchoolIBeaconsApplyCmd(cliCtx))
	cmd.AddCommand(newSchoolIBeaconsDeleteCmd(cliCtx))
	cmd.AddCommand(newSchoolIBeaconsExportCmd(cliCtx))

	return cmd
}

func newSchoolIBeaconsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all iBeacons",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.SchoolClient.GetIBeacons(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(items))
			for _, b := range items {
				rows = append(rows, flattenSchoolIBeacon(b))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

func flattenSchoolIBeacon(b jamfschool.IBeacon) map[string]any {
	return map[string]any{
		"id":          b.ID,
		"name":        b.Name,
		"uuid":        b.UUID,
		"major":       b.Major,
		"minor":       b.Minor,
		"description": b.Description,
	}
}

func newSchoolIBeaconsGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get an iBeacon by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := school.NewResolver(cliCtx.SchoolClient)

			id, err := r.ResolveIBeaconID(ctx, args[0])
			if err != nil {
				return err
			}

			item, err := cliCtx.SchoolClient.GetIBeacon(ctx, id)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, item, flattenSchoolIBeacon(*item))
		},
	}
}

func newSchoolIBeaconsApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		yes      bool
		scaffold bool
	)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update an iBeacon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scaffold {
				return printExport(jamfschool.IBeaconCreateInput{})
			}
			ctx := cmd.Context()
			data, err := readInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfschool.IBeaconCreateInput
			if err := unmarshalInput(data, &input); err != nil {
				return fmt.Errorf("parsing input file: %w", err)
			}

			if input.Name == "" {
				return fmt.Errorf("input must include a 'Name' field")
			}

			// Check if iBeacon exists by name
			r := school.NewResolver(cliCtx.SchoolClient)
			id, err := r.ResolveIBeaconID(ctx, input.Name)
			if err != nil {
				var notFound *school.ErrNotFound
				if !errors.As(err, &notFound) {
					return err
				}
				// Not found — create
				result, createErr := cliCtx.SchoolClient.CreateIBeacon(ctx, input)
				if createErr != nil {
					return createErr
				}
				fmt.Fprintf(os.Stderr, "Created iBeacon %q\n", input.Name)
				return printResult(cliCtx.Output, result, flattenSchoolIBeacon(*result))
			}

			// Found — confirm before replacing
			proceed, err := confirmReplace("ibeacon", input.Name, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			updateInput := jamfschool.IBeaconUpdateInput(input)
			result, err := cliCtx.SchoolClient.UpdateIBeacon(ctx, id, updateInput)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated iBeacon %q\n", input.Name)
			return printResult(cliCtx.Output, result, flattenSchoolIBeacon(*result))
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "Print an empty JSON template and exit")

	return cmd
}

func newSchoolIBeaconsDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete an iBeacon",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := school.NewResolver(cliCtx.SchoolClient)

			id, err := r.ResolveIBeaconID(ctx, args[0])
			if err != nil {
				return err
			}

			proceed, err := confirmDelete("ibeacon", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			if err := cliCtx.SchoolClient.DeleteIBeacon(ctx, id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted iBeacon %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newSchoolIBeaconsExportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "export <name>",
		Short: "Export an iBeacon as JSON or YAML",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := school.NewResolver(cliCtx.SchoolClient)
			id, err := r.ResolveIBeaconID(ctx, args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.SchoolClient.GetIBeacon(ctx, id)
			if err != nil {
				return err
			}
			return printExport(schoolIBeaconToInput(item))
		},
	}
}

func schoolIBeaconToInput(b *jamfschool.IBeacon) jamfschool.IBeaconCreateInput {
	return jamfschool.IBeaconCreateInput{
		Name:        b.Name,
		Description: b.Description,
		UUID:        b.UUID,
		Major:       &b.Major,
		Minor:       &b.Minor,
	}
}
