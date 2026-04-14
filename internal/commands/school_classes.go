// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/school"
	"github.com/Jamf-Concepts/jamfschool-go-sdk/jamfschool"
)

func newSchoolClassesCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "classes",
		Short: "Manage Jamf School classes",
	}

	cmd.AddCommand(newSchoolClassesListCmd(cliCtx))
	cmd.AddCommand(newSchoolClassesGetCmd(cliCtx))
	cmd.AddCommand(newSchoolClassesApplyCmd(cliCtx))
	cmd.AddCommand(newSchoolClassesDeleteCmd(cliCtx))
	cmd.AddCommand(newSchoolClassesExportCmd(cliCtx))
	cmd.AddCommand(newSchoolClassesAssignUsersCmd(cliCtx))
	cmd.AddCommand(newSchoolClassesDevicesCmd(cliCtx))

	return cmd
}

func newSchoolClassesListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all classes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.SchoolClient.GetClasses(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(items))
			for _, c := range items {
				rows = append(rows, flattenSchoolClass(c))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

func flattenSchoolClass(c jamfschool.Class) map[string]any {
	return map[string]any{
		"uuid":         c.UUID,
		"name":         c.Name,
		"description":  c.Description,
		"source":       c.Source,
		"studentCount": c.StudentCount,
		"teacherCount": c.TeacherCount,
		"deviceCount":  c.DeviceCount,
		"locationId":   c.LocationID,
	}
}

func newSchoolClassesGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get a class by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := school.NewResolver(cliCtx.SchoolClient)

			uuid, err := r.ResolveClassUUID(ctx, args[0])
			if err != nil {
				return err
			}

			item, err := cliCtx.SchoolClient.GetClass(ctx, uuid)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, item, flattenSchoolClass(*item))
		},
	}
}

func newSchoolClassesApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		yes      bool
		scaffold bool
	)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update a class",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scaffold {
				return printExport(jamfschool.ClassCreateInput{})
			}
			ctx := cmd.Context()
			data, err := readInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfschool.ClassCreateInput
			if err := unmarshalInput(data, &input); err != nil {
				return fmt.Errorf("parsing input file: %w", err)
			}

			if input.Name == "" {
				return fmt.Errorf("input must include a 'Name' field")
			}

			// Check if class exists by name
			r := school.NewResolver(cliCtx.SchoolClient)
			uuid, err := r.ResolveClassUUID(ctx, input.Name)
			if err != nil {
				var notFound *school.ErrNotFound
				if !errors.As(err, &notFound) {
					return err
				}
				// Not found — create
				newUUID, createErr := cliCtx.SchoolClient.CreateClass(ctx, input)
				if createErr != nil {
					return createErr
				}
				fmt.Fprintf(os.Stderr, "Created class %q (UUID: %s)\n", input.Name, newUUID)
				item, getErr := cliCtx.SchoolClient.GetClass(ctx, newUUID)
				if getErr != nil {
					return nil // created successfully, just can't fetch back
				}
				return printResult(cliCtx.Output, item, flattenSchoolClass(*item))
			}

			// Found — confirm before replacing
			proceed, err := confirmReplace("class", input.Name, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			updateInput := jamfschool.ClassUpdateInput{
				Name:        input.Name,
				Description: input.Description,
			}
			if err := cliCtx.SchoolClient.UpdateClass(ctx, uuid, updateInput); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated class %q\n", input.Name)
			item, err := cliCtx.SchoolClient.GetClass(ctx, uuid)
			if err != nil {
				return nil // updated successfully, just can't fetch back
			}
			return printResult(cliCtx.Output, item, flattenSchoolClass(*item))
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "Print an empty JSON template and exit")

	return cmd
}

func newSchoolClassesDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a class",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := school.NewResolver(cliCtx.SchoolClient)

			uuid, err := r.ResolveClassUUID(ctx, args[0])
			if err != nil {
				return err
			}

			proceed, err := confirmDelete("class", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			if err := cliCtx.SchoolClient.DeleteClass(ctx, uuid); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted class %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newSchoolClassesExportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "export <name>",
		Short: "Export a class as JSON or YAML",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := school.NewResolver(cliCtx.SchoolClient)
			uuid, err := r.ResolveClassUUID(ctx, args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.SchoolClient.GetClass(ctx, uuid)
			if err != nil {
				return err
			}
			return printExport(schoolClassToInput(item))
		},
	}
}

func schoolClassToInput(c *jamfschool.Class) jamfschool.ClassCreateInput {
	input := jamfschool.ClassCreateInput{
		Name:        c.Name,
		Description: c.Description,
		LocationID:  &c.LocationID,
	}
	for _, s := range c.Students {
		input.Students = append(input.Students, s.ID)
	}
	for _, t := range c.Teachers {
		input.Teachers = append(input.Teachers, t.ID)
	}
	return input
}

func newSchoolClassesAssignUsersCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		students string
		teachers string
	)

	cmd := &cobra.Command{
		Use:   "assign-users <name>",
		Short: "Assign students and teachers to a class",
		Long: `Assign users to a class by providing comma-separated user IDs.
Use --students and --teachers flags to specify which users to assign.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := school.NewResolver(cliCtx.SchoolClient)

			uuid, err := r.ResolveClassUUID(ctx, args[0])
			if err != nil {
				return err
			}

			studentIDs, err := parseInt64List(students)
			if err != nil {
				return fmt.Errorf("parsing student IDs: %w", err)
			}
			teacherIDs, err := parseInt64List(teachers)
			if err != nil {
				return fmt.Errorf("parsing teacher IDs: %w", err)
			}

			if err := cliCtx.SchoolClient.AssignClassUsers(ctx, uuid, studentIDs, teacherIDs); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Assigned users to class %q\n", args[0])
			return nil
		},
	}

	cmd.Flags().StringVar(&students, "students", "", "Comma-separated student user IDs")
	cmd.Flags().StringVar(&teachers, "teachers", "", "Comma-separated teacher user IDs")

	return cmd
}

func newSchoolClassesDevicesCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "devices <name>",
		Short: "List devices in a class",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := school.NewResolver(cliCtx.SchoolClient)

			uuid, err := r.ResolveClassUUID(ctx, args[0])
			if err != nil {
				return err
			}

			items, err := cliCtx.SchoolClient.GetClassDevices(ctx, uuid)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(items))
			for _, d := range items {
				rows = append(rows, map[string]any{
					"udid":         d.UDID,
					"serialNumber": d.SerialNumber,
					"name":         d.Name,
				})
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

// parseInt64List parses a comma-separated string of int64 values.
func parseInt64List(s string) ([]int64, error) {
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	result := make([]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid ID %q: %w", p, err)
		}
		result = append(result, id)
	}
	return result, nil
}
