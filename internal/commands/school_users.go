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

func newSchoolUsersCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "users",
		Short: "Manage Jamf School users",
	}

	cmd.AddCommand(newSchoolUsersListCmd(cliCtx))
	cmd.AddCommand(newSchoolUsersGetCmd(cliCtx))
	cmd.AddCommand(newSchoolUsersApplyCmd(cliCtx))
	cmd.AddCommand(newSchoolUsersDeleteCmd(cliCtx))
	cmd.AddCommand(newSchoolUsersExportCmd(cliCtx))

	return cmd
}

func newSchoolUsersListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all users",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.SchoolClient.GetUsers(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(items))
			for _, u := range items {
				rows = append(rows, flattenSchoolUser(u))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

func flattenSchoolUser(u jamfschool.User) map[string]any {
	return map[string]any{
		"id":          u.ID,
		"username":    u.Username,
		"email":       u.Email,
		"firstName":   u.FirstName,
		"lastName":    u.LastName,
		"status":      u.Status,
		"deviceCount": u.DeviceCount,
		"locationId":  u.LocationID,
		"inTrash":     u.InTrash,
	}
}

func newSchoolUsersGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <username-or-email>",
		Short: "Get a user by username or email",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := school.NewResolver(cliCtx.SchoolClient)

			id, err := r.ResolveUserID(ctx, args[0])
			if err != nil {
				return err
			}

			item, err := cliCtx.SchoolClient.GetUser(ctx, id)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, item, flattenSchoolUser(*item))
		},
	}
}

func newSchoolUsersApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		yes      bool
		scaffold bool
	)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update a user",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scaffold {
				return printExport(jamfschool.UserCreateInput{})
			}
			ctx := cmd.Context()
			data, err := readInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfschool.UserCreateInput
			if err := unmarshalInput(data, &input); err != nil {
				return fmt.Errorf("parsing input file: %w", err)
			}

			if input.Username == "" {
				return fmt.Errorf("input must include a 'Username' field")
			}

			// Check if user exists by username
			r := school.NewResolver(cliCtx.SchoolClient)
			id, err := r.ResolveUserID(ctx, input.Username)
			if err != nil {
				var notFound *school.ErrNotFound
				if !errors.As(err, &notFound) {
					return err
				}
				// Not found — create
				newID, createErr := cliCtx.SchoolClient.CreateUser(ctx, input)
				if createErr != nil {
					return createErr
				}
				fmt.Fprintf(os.Stderr, "Created user %q (ID: %d)\n", input.Username, newID)
				item, getErr := cliCtx.SchoolClient.GetUser(ctx, newID)
				if getErr != nil {
					return nil // created successfully, just can't fetch back
				}
				return printResult(cliCtx.Output, item, flattenSchoolUser(*item))
			}

			// Found — confirm before replacing
			proceed, err := confirmReplace("user", input.Username, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			updateInput := jamfschool.UserUpdateInput{
				Username:  input.Username,
				Password:  input.Password,
				Email:     input.Email,
				FirstName: input.FirstName,
				LastName:  input.LastName,
				Domain:    input.Domain,
				Notes:     input.Notes,
			}
			if err := cliCtx.SchoolClient.UpdateUser(ctx, id, updateInput); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated user %q\n", input.Username)
			item, err := cliCtx.SchoolClient.GetUser(ctx, id)
			if err != nil {
				return nil // updated successfully, just can't fetch back
			}
			return printResult(cliCtx.Output, item, flattenSchoolUser(*item))
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "Print an empty JSON template and exit")

	return cmd
}

func newSchoolUsersDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:         "delete <username-or-email>",
		Short:       "Delete a user",
		Annotations: map[string]string{"jamf:destructive": "true"},
		Args:        cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := school.NewResolver(cliCtx.SchoolClient)

			id, err := r.ResolveUserID(ctx, args[0])
			if err != nil {
				return err
			}

			proceed, err := confirmDelete("user", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			if err := cliCtx.SchoolClient.DeleteUser(ctx, id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted user %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newSchoolUsersExportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "export <username-or-email>",
		Short: "Export a user as JSON or YAML",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := school.NewResolver(cliCtx.SchoolClient)
			id, err := r.ResolveUserID(ctx, args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.SchoolClient.GetUser(ctx, id)
			if err != nil {
				return err
			}
			return printExport(schoolUserToInput(item))
		},
	}
}

func schoolUserToInput(u *jamfschool.User) jamfschool.UserCreateInput {
	return jamfschool.UserCreateInput{
		Username:   u.Username,
		Email:      u.Email,
		FirstName:  u.FirstName,
		LastName:   u.LastName,
		Domain:     u.Domain,
		Notes:      u.Notes,
		Exclude:    u.Exclude,
		LocationID: &u.LocationID,
	}
}
