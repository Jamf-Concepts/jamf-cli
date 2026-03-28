package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

func newProtectApiClientsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api-clients",
		Short: "Manage Jamf Protect API clients",
	}

	cmd.AddCommand(newProtectApiClientsListCmd(cliCtx))
	cmd.AddCommand(newProtectApiClientsGetCmd(cliCtx))
	cmd.AddCommand(newProtectApiClientsCreateCmd(cliCtx))
	cmd.AddCommand(newProtectApiClientsUpdateCmd(cliCtx))
	cmd.AddCommand(newProtectApiClientsDeleteCmd(cliCtx))

	return cmd
}

func newProtectApiClientsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all API clients",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.ProtectClient.ListApiClients(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(items))
			for _, c := range items {
				rows = append(rows, flattenApiClient(c))
			}
			data, _ := json.Marshal(rows)
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

// flattenApiClient converts an ApiClient into a clean map for readable table
// output. The Password field is intentionally omitted.
func flattenApiClient(c jamfprotect.ApiClient) map[string]any {
	m := map[string]any{
		"name":     c.Name,
		"clientId": c.ClientID,
		"created":  c.Created,
	}
	if len(c.AssignedRoles) > 0 {
		names := make([]string, 0, len(c.AssignedRoles))
		for _, r := range c.AssignedRoles {
			names = append(names, r.Name)
		}
		m["assignedRoles"] = strings.Join(names, ", ")
	}
	return m
}

func newProtectApiClientsGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get an API client by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			clientID, err := r.ResolveApiClientID(ctx, args[0])
			if err != nil {
				return err
			}

			item, err := cliCtx.ProtectClient.GetApiClient(ctx, clientID)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}
}

func newProtectApiClientsCreateCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var fromFile string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an API client",
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := readProtectInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfprotect.ApiClientInput
			if err := json.Unmarshal(data, &input); err != nil {
				return fmt.Errorf("parsing input file: %w", err)
			}

			item, err := cliCtx.ProtectClient.CreateApiClient(cmd.Context(), input)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")

	return cmd
}

func newProtectApiClientsUpdateCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var fromFile string

	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update an API client",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			clientID, err := r.ResolveApiClientID(ctx, args[0])
			if err != nil {
				return err
			}

			data, err := readProtectInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfprotect.ApiClientInput
			if err := json.Unmarshal(data, &input); err != nil {
				return fmt.Errorf("parsing input file: %w", err)
			}

			item, err := cliCtx.ProtectClient.UpdateApiClient(ctx, clientID, input)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")

	return cmd
}

func newProtectApiClientsDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete an API client",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			clientID, err := r.ResolveApiClientID(ctx, args[0])
			if err != nil {
				return err
			}

			proceed, err := confirmProtectDelete("API client", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			if err := cliCtx.ProtectClient.DeleteApiClient(ctx, clientID); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted API client %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}
