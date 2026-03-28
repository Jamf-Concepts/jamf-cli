package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

func newProtectUsersCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "users",
		Short: "Manage Jamf Protect users",
	}

	cmd.AddCommand(newProtectUsersListCmd(cliCtx))
	cmd.AddCommand(newProtectUsersGetCmd(cliCtx))
	cmd.AddCommand(newProtectUsersCreateCmd(cliCtx))
	cmd.AddCommand(newProtectUsersUpdateCmd(cliCtx))
	cmd.AddCommand(newProtectUsersDeleteCmd(cliCtx))

	return cmd
}

func newProtectUsersListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all users",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.ProtectClient.ListUsers(cmd.Context())
			if err != nil {
				return err
			}
			return protect.PrintList(cliCtx.Output, items)
		},
	}
}

func newProtectUsersGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <email>",
		Short: "Get a user by email",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			id, err := r.ResolveUserID(ctx, args[0])
			if err != nil {
				return err
			}

			item, err := cliCtx.ProtectClient.GetUser(ctx, id)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}
}

func newProtectUsersCreateCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var fromFile string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a user",
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := readProtectInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfprotect.UserInput
			if err := json.Unmarshal(data, &input); err != nil {
				return fmt.Errorf("parsing input file: %w", err)
			}

			item, err := cliCtx.ProtectClient.CreateUser(cmd.Context(), input)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")

	return cmd
}

func newProtectUsersUpdateCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var fromFile string

	cmd := &cobra.Command{
		Use:   "update <email>",
		Short: "Update a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			id, err := r.ResolveUserID(ctx, args[0])
			if err != nil {
				return err
			}

			data, err := readProtectInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfprotect.UserInput
			if err := json.Unmarshal(data, &input); err != nil {
				return fmt.Errorf("parsing input file: %w", err)
			}

			item, err := cliCtx.ProtectClient.UpdateUser(ctx, id, input)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")

	return cmd
}

func newProtectUsersDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <email>",
		Short: "Delete a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			id, err := r.ResolveUserID(ctx, args[0])
			if err != nil {
				return err
			}

			proceed, err := confirmProtectDelete("user", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			if err := cliCtx.ProtectClient.DeleteUser(ctx, id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted user %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}
