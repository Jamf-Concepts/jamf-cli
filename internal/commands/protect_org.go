package commands

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

// --- Data Forwarding ---

func newProtectDataForwardingCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "data-forwarding",
		Short: "Manage data forwarding settings",
	}

	cmd.AddCommand(newProtectDataForwardingGetCmd(cliCtx))
	cmd.AddCommand(newProtectDataForwardingUpdateCmd(cliCtx))

	return cmd
}

func newProtectDataForwardingGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Get data forwarding settings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			item, err := cliCtx.ProtectClient.GetDataForwarding(cmd.Context())
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}
}

func newProtectDataForwardingUpdateCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var fromFile string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update data forwarding settings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := readProtectInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfprotect.DataForwardingInput
			if err := json.Unmarshal(data, &input); err != nil {
				return fmt.Errorf("parsing input file: %w", err)
			}

			item, err := cliCtx.ProtectClient.UpdateDataForwarding(cmd.Context(), input)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")

	return cmd
}

// --- Data Retention ---

func newProtectDataRetentionCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "data-retention",
		Short: "Manage data retention settings",
	}

	cmd.AddCommand(newProtectDataRetentionGetCmd(cliCtx))
	cmd.AddCommand(newProtectDataRetentionUpdateCmd(cliCtx))

	return cmd
}

func newProtectDataRetentionGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Get data retention settings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			item, err := cliCtx.ProtectClient.GetDataRetention(cmd.Context())
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}
}

func newProtectDataRetentionUpdateCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var fromFile string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update data retention settings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := readProtectInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfprotect.DataRetentionInput
			if err := json.Unmarshal(data, &input); err != nil {
				return fmt.Errorf("parsing input file: %w", err)
			}

			item, err := cliCtx.ProtectClient.UpdateDataRetention(cmd.Context(), input)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")

	return cmd
}

// --- Downloads ---

func newProtectDownloadsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "downloads",
		Short: "Get organization download links",
		RunE: func(cmd *cobra.Command, _ []string) error {
			item, err := cliCtx.ProtectClient.GetOrganizationDownloads(cmd.Context())
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}
}

// --- Config Freeze ---

func newProtectConfigFreezeCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config-freeze",
		Short: "Manage configuration freeze",
	}

	cmd.AddCommand(newProtectConfigFreezeGetCmd(cliCtx))
	cmd.AddCommand(newProtectConfigFreezeEnableCmd(cliCtx))
	cmd.AddCommand(newProtectConfigFreezeDisableCmd(cliCtx))

	return cmd
}

func newProtectConfigFreezeGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Get config freeze status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			item, err := cliCtx.ProtectClient.GetConfigFreeze(cmd.Context())
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}
}

func newProtectConfigFreezeEnableCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "enable",
		Short: "Enable config freeze",
		RunE: func(cmd *cobra.Command, _ []string) error {
			item, err := cliCtx.ProtectClient.UpdateOrganizationConfigFreeze(cmd.Context(), true)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}
}

func newProtectConfigFreezeDisableCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Disable config freeze",
		RunE: func(cmd *cobra.Command, _ []string) error {
			item, err := cliCtx.ProtectClient.UpdateOrganizationConfigFreeze(cmd.Context(), false)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}
}

// --- Connections ---

func newProtectConnectionsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connections",
		Short: "Manage identity provider connections",
	}

	cmd.AddCommand(newProtectConnectionsListCmd(cliCtx))

	return cmd
}

func newProtectConnectionsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all connections",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.ProtectClient.ListConnections(cmd.Context())
			if err != nil {
				return err
			}
			return protect.PrintList(cliCtx.Output, items)
		},
	}
}
