// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

func newProtectExceptionSetsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exception-sets",
		Short: "Manage Jamf Protect exception sets",
	}

	cmd.AddCommand(newProtectExceptionSetsListCmd(cliCtx))
	cmd.AddCommand(newProtectExceptionSetsGetCmd(cliCtx))
	cmd.AddCommand(newProtectExceptionSetsApplyCmd(cliCtx))
	cmd.AddCommand(newProtectExceptionSetsDeleteCmd(cliCtx))
	cmd.AddCommand(newProtectExceptionSetsAddExceptionCmd(cliCtx))
	cmd.AddCommand(newProtectExceptionSetsRemoveExceptionCmd(cliCtx))
	cmd.AddCommand(newProtectExceptionSetsExportCmd(cliCtx))

	return cmd
}

func newProtectExceptionSetsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all exception sets",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.ProtectClient.ListExceptionSets(cmd.Context())
			if err != nil {
				return err
			}
			return protect.PrintList(cliCtx.Output, items)
		},
	}
}

// flattenExceptionSet converts an ExceptionSet into a clean map for readable
// table output, reducing nested slices to counts.
func flattenExceptionSet(s jamfprotect.ExceptionSet) map[string]any {
	return map[string]any{
		"name":              s.Name,
		"description":       s.Description,
		"exceptionsCount":   len(s.Exceptions),
		"esExceptionsCount": len(s.EsExceptions),
		"managed":           s.Managed,
	}
}

func newProtectExceptionSetsGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get an exception set by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r := protect.NewResolver(cliCtx.ProtectClient)
			uuid, err := r.ResolveExceptionSetUUID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.ProtectClient.GetExceptionSet(cmd.Context(), uuid)
			if err != nil {
				return err
			}
			return printProtectResult(cliCtx.Output, item, flattenExceptionSet(*item))
		},
	}
}

func newProtectExceptionSetsApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		yes      bool
	)
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update an exception set",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			data, err := readProtectInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfprotect.ExceptionSetInput
			if err := unmarshalProtectInput(data, &input); err != nil {
				return fmt.Errorf("parsing input JSON: %w", err)
			}

			if input.Name == "" {
				return fmt.Errorf("input must include a 'Name' field")
			}

			// Check if exception set exists by name
			r := protect.NewResolver(cliCtx.ProtectClient)
			uuid, err := r.ResolveExceptionSetUUID(ctx, input.Name)
			if err != nil {
				// Not found — create
				result, err := cliCtx.ProtectClient.CreateExceptionSet(ctx, input)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Created exception set %q\n", input.Name)
				return printProtectResult(cliCtx.Output, result, flattenExceptionSet(result))
			}

			// Found — confirm before replacing
			proceed, err := confirmProtectReplace("exception set", input.Name, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			result, err := cliCtx.ProtectClient.UpdateExceptionSet(ctx, uuid, input)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated exception set %q\n", input.Name)
			return printProtectResult(cliCtx.Output, result, flattenExceptionSet(result))
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing")
	return cmd
}

func newProtectExceptionSetsDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete an exception set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r := protect.NewResolver(cliCtx.ProtectClient)
			uuid, err := r.ResolveExceptionSetUUID(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			proceed, err := confirmProtectDelete("exception set", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			if err := cliCtx.ProtectClient.DeleteExceptionSet(cmd.Context(), uuid); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted exception set %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

// exceptionToInput converts an Exception response to an ExceptionInput for updates.
func exceptionToInput(e jamfprotect.Exception) jamfprotect.ExceptionInput {
	input := jamfprotect.ExceptionInput{
		Type:           e.Type,
		Value:          e.Value,
		IgnoreActivity: e.IgnoreActivity,
		AnalyticTypes:  e.AnalyticTypes,
		AnalyticUuid:   e.AnalyticUuid,
	}
	if e.AppSigningInfo != nil {
		input.AppSigningInfo = &jamfprotect.AppSigningInfoInput{
			AppId:  e.AppSigningInfo.AppId,
			TeamId: e.AppSigningInfo.TeamId,
		}
	}
	return input
}

// esExceptionToInput converts an EsException response to an EsExceptionInput for updates.
func esExceptionToInput(e jamfprotect.EsException) jamfprotect.EsExceptionInput {
	input := jamfprotect.EsExceptionInput{
		Type:              e.Type,
		Value:             e.Value,
		IgnoreActivity:    e.IgnoreActivity,
		IgnoreListType:    e.IgnoreListType,
		IgnoreListSubType: e.IgnoreListSubType,
		EventType:         e.EventType,
	}
	if e.AppSigningInfo != nil {
		input.AppSigningInfo = &jamfprotect.AppSigningInfoInput{
			AppId:  e.AppSigningInfo.AppId,
			TeamId: e.AppSigningInfo.TeamId,
		}
	}
	return input
}

// rebuildExceptionSetInput reconstructs an ExceptionSetInput from the current ExceptionSet state.
func rebuildExceptionSetInput(set *jamfprotect.ExceptionSet) jamfprotect.ExceptionSetInput {
	exceptions := make([]jamfprotect.ExceptionInput, 0, len(set.Exceptions))
	for _, e := range set.Exceptions {
		exceptions = append(exceptions, exceptionToInput(e))
	}
	esExceptions := make([]jamfprotect.EsExceptionInput, 0, len(set.EsExceptions))
	for _, e := range set.EsExceptions {
		esExceptions = append(esExceptions, esExceptionToInput(e))
	}
	return jamfprotect.ExceptionSetInput{
		Name:         set.Name,
		Description:  set.Description,
		Exceptions:   exceptions,
		EsExceptions: esExceptions,
	}
}

func newProtectExceptionSetsAddExceptionCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var exType, value, ignoreActivity string
	cmd := &cobra.Command{
		Use:   "add-exception <set-name>",
		Short: "Add an exception to a set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			uuid, err := r.ResolveExceptionSetUUID(ctx, args[0])
			if err != nil {
				return err
			}

			set, err := cliCtx.ProtectClient.GetExceptionSet(ctx, uuid)
			if err != nil {
				return err
			}

			input := rebuildExceptionSetInput(set)

			// Check if this type+value already exists
			for _, e := range set.Exceptions {
				if e.Type == exType && e.Value == value {
					fmt.Fprintf(os.Stderr, "Exception %s=%q already exists in set %q\n", exType, value, args[0])
					return nil
				}
			}

			input.Exceptions = append(input.Exceptions, jamfprotect.ExceptionInput{
				Type:           exType,
				Value:          value,
				IgnoreActivity: ignoreActivity,
			})

			result, err := cliCtx.ProtectClient.UpdateExceptionSet(ctx, uuid, input)
			if err != nil {
				return err
			}
			return printProtectResult(cliCtx.Output, result, flattenExceptionSet(result))
		},
	}
	cmd.Flags().StringVar(&exType, "type", "", "Exception type (e.g. \"Path\")")
	cmd.Flags().StringVar(&value, "value", "", "Exception value (e.g. \"/usr/bin/foo\")")
	cmd.Flags().StringVar(&ignoreActivity, "ignore-activity", "", "Ignore activity setting (e.g. \"IGNORE_ACTIVITIES\")")
	_ = cmd.MarkFlagRequired("type")
	_ = cmd.MarkFlagRequired("value")
	return cmd
}

func newProtectExceptionSetsRemoveExceptionCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var exType, value string
	cmd := &cobra.Command{
		Use:   "remove-exception <set-name>",
		Short: "Remove an exception from a set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			uuid, err := r.ResolveExceptionSetUUID(ctx, args[0])
			if err != nil {
				return err
			}

			set, err := cliCtx.ProtectClient.GetExceptionSet(ctx, uuid)
			if err != nil {
				return err
			}

			input := rebuildExceptionSetInput(set)

			// Remove the matching exception
			filtered := make([]jamfprotect.ExceptionInput, 0, len(input.Exceptions))
			for _, e := range input.Exceptions {
				if e.Type == exType && e.Value == value {
					continue
				}
				filtered = append(filtered, e)
			}
			input.Exceptions = filtered

			result, err := cliCtx.ProtectClient.UpdateExceptionSet(ctx, uuid, input)
			if err != nil {
				return err
			}
			return printProtectResult(cliCtx.Output, result, flattenExceptionSet(result))
		},
	}
	cmd.Flags().StringVar(&exType, "type", "", "Exception type to match")
	cmd.Flags().StringVar(&value, "value", "", "Exception value to match")
	_ = cmd.MarkFlagRequired("type")
	_ = cmd.MarkFlagRequired("value")
	return cmd
}

func newProtectExceptionSetsExportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "export <name>",
		Short: "Export an exception set as JSON or YAML",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)
			uuid, err := r.ResolveExceptionSetUUID(ctx, args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.ProtectClient.GetExceptionSet(ctx, uuid)
			if err != nil {
				return err
			}
			return printProtectExport(rebuildExceptionSetInput(item))
		},
	}
}
