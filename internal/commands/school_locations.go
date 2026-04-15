// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/school"
	"github.com/Jamf-Concepts/jamfschool-go-sdk/jamfschool"
)

func newSchoolLocationsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "locations",
		Short: "View Jamf School locations",
	}

	cmd.AddCommand(newSchoolLocationsListCmd(cliCtx))
	cmd.AddCommand(newSchoolLocationsGetCmd(cliCtx))

	return cmd
}

func newSchoolLocationsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all locations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.SchoolClient.GetLocations(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(items))
			for _, l := range items {
				rows = append(rows, flattenSchoolLocation(l))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

func flattenSchoolLocation(l jamfschool.Location) map[string]any {
	m := map[string]any{
		"id":         l.ID,
		"name":       l.Name,
		"isDistrict": l.IsDistrict,
		"source":     l.Source,
	}
	if l.City != nil {
		m["city"] = *l.City
	}
	return m
}

func newSchoolLocationsGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get a location by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := school.NewResolver(cliCtx.SchoolClient)

			id, err := r.ResolveLocationID(ctx, args[0])
			if err != nil {
				return err
			}

			item, err := cliCtx.SchoolClient.GetLocation(ctx, id)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, item, flattenSchoolLocation(*item))
		},
	}
}
