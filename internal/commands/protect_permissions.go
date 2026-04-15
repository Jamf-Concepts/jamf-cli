// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newProtectPermissionsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "permissions",
		Short: "Show RBAC permissions for the current API client",
		Long:  `Display the read and write permissions granted to the currently authenticated API client.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			perms, err := cliCtx.ProtectClient.GetCurrentPermissions(cmd.Context())
			if err != nil {
				return err
			}
			row := map[string]any{
				"read":  strings.Join(perms.Read, ", "),
				"write": strings.Join(perms.Write, ", "),
			}
			data, err := json.Marshal([]map[string]any{row})
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}
