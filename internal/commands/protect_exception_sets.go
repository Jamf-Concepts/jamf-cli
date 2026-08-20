// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
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
			return printResult(cliCtx.Output, item, flattenExceptionSet(*item))
		},
	}
}

func newProtectExceptionSetsApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		yes      bool
		scaffold bool
	)
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update an exception set",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scaffold {
				return printExport(exceptionSetExport{
					Exceptions:   []exceptionExport{},
					EsExceptions: []jamfprotect.EsExceptionInput{},
				})
			}
			ctx := cmd.Context()
			data, err := readInput(fromFile)
			if err != nil {
				return err
			}
			var doc exceptionSetExport
			if err := unmarshalInput(data, &doc); err != nil {
				return fmt.Errorf("parsing input JSON: %w", err)
			}

			if doc.Name == "" {
				return fmt.Errorf("input must include a 'name' field")
			}

			// Check if exception set exists by name
			r := protect.NewResolver(cliCtx.ProtectClient)
			input, err := exceptionSetExportToInput(ctx, doc, r)
			if err != nil {
				return err
			}
			uuid, err := r.ResolveExceptionSetUUID(ctx, doc.Name)
			if err != nil {
				// Not found — create
				result, err := cliCtx.ProtectClient.CreateExceptionSet(ctx, input)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Created exception set %q\n", input.Name)
				return printResult(cliCtx.Output, result, flattenExceptionSet(result))
			}

			// Found — confirm before replacing
			proceed, err := confirmReplace("exception set", input.Name, yes)
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
			return printResult(cliCtx.Output, result, flattenExceptionSet(result))
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "Print an empty JSON template and exit")
	return cmd
}

func newProtectExceptionSetsDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:         "delete <name>",
		Short:       "Delete an exception set",
		Annotations: map[string]string{"jamf:destructive": "true"},
		Args:        cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r := protect.NewResolver(cliCtx.ProtectClient)
			uuid, err := r.ResolveExceptionSetUUID(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			proceed, err := confirmDelete("exception set", args[0], yes)
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
		Value:          strPtrIfNonEmpty(e.Value),
		IgnoreActivity: e.IgnoreActivity,
		AnalyticTypes:  e.AnalyticTypes,
	}
	if e.Analytic != nil {
		input.AnalyticUUID = strPtrIfNonEmpty(e.Analytic.UUID)
	}
	if e.AppSigningInfo != nil {
		input.AppSigningInfo = &jamfprotect.AppSigningInfoInput{
			AppID:  e.AppSigningInfo.AppID,
			TeamID: e.AppSigningInfo.TeamID,
		}
	}
	return input
}

// esExceptionToInput converts an EsException response to an EsExceptionInput for updates.
func esExceptionToInput(e jamfprotect.EsException) jamfprotect.EsExceptionInput {
	input := jamfprotect.EsExceptionInput{
		Type:              e.Type,
		Value:             strPtrIfNonEmpty(e.Value),
		IgnoreActivity:    e.IgnoreActivity,
		IgnoreListType:    e.IgnoreListType,
		IgnoreListSubType: strPtrIfNonEmpty(e.IgnoreListSubType),
		EventType:         strPtrIfNonEmpty(e.EventType),
	}
	if e.AppSigningInfo != nil {
		input.AppSigningInfo = &jamfprotect.AppSigningInfoInput{
			AppID:  e.AppSigningInfo.AppID,
			TeamID: e.AppSigningInfo.TeamID,
		}
	}
	return input
}

// An exception that targets a specific analytic used to export only that
// analytic's UUID. Jamf-published analytics carry the same UUID in every tenant,
// so those survived a move; a custom analytic gets a per-tenant UUID, so the same
// document applied elsewhere named an analytic the target does not have.
//
// The server does reject that, but with an error that identifies nothing:
//
//	createExceptionSet: Action blocked due to dependencies on this resource.
//
// Established on the wire by applying two documents differing only in the uuid —
// the real one created the set, the foreign one produced the message above. So
// the old behaviour was not a silently dead exception; it was an unactionable
// restore failure naming neither the analytic, nor the uuid, nor the reason.
//
// exceptionSetExport therefore carries the analytic's *name* as well, and apply
// resolves it against the target, failing with the analytic and set names when it
// cannot. The document keys are otherwise a field-for-field mirror of the SDK
// input types, json tags included and yaml tags deliberately absent, so files
// written before this change decode unchanged and only gain the new key.

// exceptionSetExport is the portable representation of an exception set.
type exceptionSetExport struct {
	Name         string                         `json:"name"`
	Description  string                         `json:"description"`
	Exceptions   []exceptionExport              `json:"exceptions"`
	EsExceptions []jamfprotect.EsExceptionInput `json:"esExceptions"`
}

// exceptionExport is one exception, with its analytic reference by name.
type exceptionExport struct {
	Type           string                           `json:"type"`
	Value          *string                          `json:"value,omitempty"`
	AppSigningInfo *jamfprotect.AppSigningInfoInput `json:"appSigningInfo,omitempty"`
	IgnoreActivity string                           `json:"ignoreActivity"`
	// Analytic is the target analytic's name and is what apply resolves.
	// AnalyticUUID is still read so pre-existing documents keep working, and is
	// still written so a document stays usable with an older CLI, but Analytic
	// wins whenever both are present.
	//
	// This is the one field carrying a yaml tag: the others deliberately have
	// none so their keys stay byte-identical to what the SDK input types
	// produced, but there is no legacy key for a new field to preserve, and
	// without omitempty every exception that targets no analytic would carry a
	// noisy `analytic: ""`.
	Analytic      string   `json:"analytic,omitempty" yaml:"analytic,omitempty"`
	AnalyticUUID  *string  `json:"analyticUuid,omitempty"`
	AnalyticTypes []string `json:"analyticTypes,omitempty"`
}

// exceptionSetToExport builds the portable document from a fetched set. The
// analytic name needs no extra call: the SDK's query already selects
// `analytic { name uuid }`.
func exceptionSetToExport(set *jamfprotect.ExceptionSet) exceptionSetExport {
	e := exceptionSetExport{
		Name:         set.Name,
		Description:  set.Description,
		Exceptions:   make([]exceptionExport, 0, len(set.Exceptions)),
		EsExceptions: make([]jamfprotect.EsExceptionInput, 0, len(set.EsExceptions)),
	}
	for _, ex := range set.Exceptions {
		out := exceptionExport{
			Type:           ex.Type,
			Value:          strPtrIfNonEmpty(ex.Value),
			IgnoreActivity: ex.IgnoreActivity,
			AnalyticTypes:  ex.AnalyticTypes,
		}
		if ex.Analytic != nil {
			out.Analytic = ex.Analytic.Name
			out.AnalyticUUID = strPtrIfNonEmpty(ex.Analytic.UUID)
		}
		if ex.AppSigningInfo != nil {
			out.AppSigningInfo = &jamfprotect.AppSigningInfoInput{
				AppID:  ex.AppSigningInfo.AppID,
				TeamID: ex.AppSigningInfo.TeamID,
			}
		}
		e.Exceptions = append(e.Exceptions, out)
	}
	for _, ex := range set.EsExceptions {
		e.EsExceptions = append(e.EsExceptions, esExceptionToInput(ex))
	}
	return e
}

// exceptionSetExportToInput resolves each exception's analytic name against the
// target tenant and builds the SDK input.
//
// A named analytic that the target does not have is an error rather than a
// pass-through: writing the source tenant's UUID is what produced a silently
// dead exception in the first place.
func exceptionSetExportToInput(ctx context.Context, e exceptionSetExport, r *protect.Resolver) (jamfprotect.ExceptionSetInput, error) {
	input := jamfprotect.ExceptionSetInput{
		Name:         e.Name,
		Description:  e.Description,
		Exceptions:   make([]jamfprotect.ExceptionInput, 0, len(e.Exceptions)),
		EsExceptions: e.EsExceptions,
	}
	if input.EsExceptions == nil {
		input.EsExceptions = []jamfprotect.EsExceptionInput{}
	}
	for _, ex := range e.Exceptions {
		out := jamfprotect.ExceptionInput{
			Type:           ex.Type,
			Value:          ex.Value,
			AppSigningInfo: ex.AppSigningInfo,
			IgnoreActivity: ex.IgnoreActivity,
			AnalyticTypes:  ex.AnalyticTypes,
			AnalyticUUID:   ex.AnalyticUUID,
		}
		if ex.Analytic != "" {
			uuid, err := r.ResolveAnalyticUUID(ctx, ex.Analytic)
			if err != nil {
				return jamfprotect.ExceptionSetInput{}, fmt.Errorf("resolving analytic %q for an exception in set %q: %w", ex.Analytic, e.Name, err)
			}
			out.AnalyticUUID = &uuid
		}
		input.Exceptions = append(input.Exceptions, out)
	}
	return input, nil
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
				Value:          strPtrIfNonEmpty(value),
				IgnoreActivity: ignoreActivity,
			})

			result, err := cliCtx.ProtectClient.UpdateExceptionSet(ctx, uuid, input)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, result, flattenExceptionSet(result))
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
				if e.Type == exType && e.Value != nil && *e.Value == value {
					continue
				}
				filtered = append(filtered, e)
			}
			input.Exceptions = filtered

			result, err := cliCtx.ProtectClient.UpdateExceptionSet(ctx, uuid, input)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, result, flattenExceptionSet(result))
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
			return printExport(exceptionSetToExport(item))
		},
	}
}
