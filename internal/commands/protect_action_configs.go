// Copyright 2026, Jamf Software LLC

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

func newProtectActionConfigsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "action-configs",
		Short: "Manage Jamf Protect action configurations",
	}

	cmd.AddCommand(newProtectActionConfigsListCmd(cliCtx))
	cmd.AddCommand(newProtectActionConfigsGetCmd(cliCtx))
	cmd.AddCommand(newProtectActionConfigsApplyCmd(cliCtx))
	cmd.AddCommand(newProtectActionConfigsDeleteCmd(cliCtx))
	cmd.AddCommand(newProtectActionConfigsExportCmd(cliCtx))

	return cmd
}

func newProtectActionConfigsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all action configurations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.ProtectClient.ListActionConfigs(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(items))
			for _, a := range items {
				rows = append(rows, map[string]any{
					"name": a.Name,
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

func newProtectActionConfigsGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get an action configuration by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolveActionConfigID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.ProtectClient.GetActionConfig(cmd.Context(), id)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}
}

func newProtectActionConfigsApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		yes      bool
		scaffold bool
	)
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update an action configuration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scaffold {
				return printExport(jamfprotect.ActionConfigInput{})
			}
			ctx := cmd.Context()
			data, err := readInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfprotect.ActionConfigInput
			if err := unmarshalInput(data, &input); err != nil {
				return fmt.Errorf("parsing input JSON: %w", err)
			}

			if input.Name == "" {
				return fmt.Errorf("input must include a 'Name' field")
			}

			// Check if action config exists by name
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolveActionConfigID(ctx, input.Name)
			if err != nil {
				// Not found — create
				result, err := cliCtx.ProtectClient.CreateActionConfig(ctx, input)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Created action configuration %q\n", input.Name)
				return protect.PrintOne(cliCtx.Output, result)
			}

			// Found — confirm before replacing
			proceed, err := confirmReplace("action config", input.Name, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			result, err := cliCtx.ProtectClient.UpdateActionConfig(ctx, id, input)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated action configuration %q\n", input.Name)
			return protect.PrintOne(cliCtx.Output, result)
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "Print an empty JSON template and exit")
	return cmd
}

func newProtectActionConfigsDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:         "delete <name>",
		Short:       "Delete an action configuration",
		Annotations: map[string]string{"jamf:destructive": "true"},
		Args:        cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolveActionConfigID(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			proceed, err := confirmDelete("action configuration", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			if err := cliCtx.ProtectClient.DeleteActionConfig(cmd.Context(), id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted action configuration %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newProtectActionConfigsExportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "export <name>",
		Short: "Export an action configuration as JSON or YAML",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolveActionConfigID(ctx, args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.ProtectClient.GetActionConfig(ctx, id)
			if err != nil {
				return err
			}
			export, err := actionConfigToInput(item)
			if err != nil {
				return err
			}
			return printExport(export)
		},
	}
}

// actionConfigToInput converts an ActionConfig response to an ActionConfigInput, stripping server-only fields.
// AlertConfig and Clients use map[string]any in the input type, so we marshal/unmarshal to convert.
func actionConfigToInput(a *jamfprotect.ActionConfig) (jamfprotect.ActionConfigInput, error) {
	input := jamfprotect.ActionConfigInput{
		Name:        a.Name,
		Description: a.Description,
	}
	if a.AlertConfig != nil {
		b, _ := json.Marshal(a.AlertConfig)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		input.AlertConfig = m
	}
	if len(a.Clients) > 0 {
		clients := make([]map[string]any, len(a.Clients))
		for i, c := range a.Clients {
			b, _ := json.Marshal(c)
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			// Strip server-generated id field
			delete(m, "id")
			// params is read back as an object — the ReportClientParams union —
			// but the input schema declares it AWSJSON!, a non-null JSON-encoded
			// *string*. Handing the object straight back is refused ("Variable
			// 'params' has an invalid value"), and dropping it is refused too
			// ("coerced Null value for NonNull type 'AWSJSON!'"), so it must be
			// re-encoded as a string.
			//
			// The union also means the response carries every member's fields at
			// their zero value — port 0, empty scheme, empty host — for a client
			// type that uses none of them. Those are pruned first so a JamfCloud
			// client sends "{}" rather than a wall of empty settings.
			// batchConfig has the same zero-value problem, but only its nullable
			// fields may be dropped: sizeIndex and windowInSeconds are Int! and
			// must be sent even as 0, while sizeInBytes carries a minimum of 1000
			// that an unset 0 violates, and delimiter is a nullable String.
			if batch, ok := m["batchConfig"].(map[string]any); ok {
				for _, key := range []string{"sizeInBytes", "delimiter"} {
					switch v := batch[key].(type) {
					case nil:
						delete(batch, key)
					case string:
						if v == "" {
							delete(batch, key)
						}
					case float64:
						if v == 0 {
							delete(batch, key)
						}
					}
				}
				m["batchConfig"] = batch
			}

			params, _ := m["params"].(map[string]any)
			// No "{}" fallback: params is AWSJSON! and carries the client's whole
			// configuration, so an empty object is not a degraded version of it —
			// it is a client that would be backed up broken and restored broken.
			encoded, err := json.Marshal(pruneEmptyValues(params))
			if err != nil {
				return input, fmt.Errorf("encoding params for client %d of action config %q: %w", i, a.Name, err)
			}
			m["params"] = string(encoded)
			clients[i] = m
		}
		input.Clients = clients
	}
	return input, nil
}

// pruneEmptyValues removes keys whose values carry no information — nil, the
// empty string, numeric zero, and empty collections — recursing into nested
// maps. Boolean false is kept: unlike an unset port or an empty host, it is a
// meaningful setting.
func pruneEmptyValues(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case nil:
			continue
		case string:
			if val == "" {
				continue
			}
		case float64:
			if val == 0 {
				continue
			}
		case int64:
			if val == 0 {
				continue
			}
		case []any:
			if len(val) == 0 {
				continue
			}
		case map[string]any:
			nested := pruneEmptyValues(val)
			if len(nested) == 0 {
				continue
			}
			out[k] = nested
			continue
		}
		out[k] = v
	}
	return out
}
