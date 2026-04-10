// Copyright 2026, Jamf Software LLC

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

func newProtectRemovableStorageControlSetsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "removable-storage-control-sets",
		Short: "Manage Jamf Protect removable storage control sets",
	}

	cmd.AddCommand(newProtectRSCSListCmd(cliCtx))
	cmd.AddCommand(newProtectRSCSGetCmd(cliCtx))
	cmd.AddCommand(newProtectRSCSApplyCmd(cliCtx))
	cmd.AddCommand(newProtectRSCSDeleteCmd(cliCtx))
	cmd.AddCommand(newProtectRSCSAddRuleCmd(cliCtx))
	cmd.AddCommand(newProtectRSCSRemoveRuleCmd(cliCtx))
	cmd.AddCommand(newProtectRSCSExportCmd(cliCtx))

	return cmd
}

func newProtectRSCSListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all removable storage control sets",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.ProtectClient.ListRemovableStorageControlSets(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(items))
			for _, s := range items {
				rows = append(rows, flattenRSCS(s))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

// flattenRSCS converts a RemovableStorageControlSet into a clean map for
// readable table output, summarising rules and plans.
func flattenRSCS(s jamfprotect.RemovableStorageControlSet) map[string]any {
	m := map[string]any{
		"name":               s.Name,
		"defaultMountAction": s.DefaultMountAction,
		"rulesCount":         len(s.Rules),
	}
	if len(s.Plans) > 0 {
		names := make([]string, 0, len(s.Plans))
		for _, p := range s.Plans {
			names = append(names, p.Name)
		}
		m["plans"] = strings.Join(names, ", ")
	}
	return m
}

func newProtectRSCSGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get a removable storage control set by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolveRemovableStorageControlSetID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.ProtectClient.GetRemovableStorageControlSet(cmd.Context(), id)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, item, flattenRSCS(*item))
		},
	}
}

func newProtectRSCSApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		yes      bool
	)
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update a removable storage control set",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			data, err := readInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfprotect.RemovableStorageControlSetInput
			if err := unmarshalInput(data, &input); err != nil {
				return fmt.Errorf("parsing input JSON: %w", err)
			}

			if input.Name == "" {
				return fmt.Errorf("input must include a 'Name' field")
			}

			// Check if removable storage control set exists by name
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolveRemovableStorageControlSetID(ctx, input.Name)
			if err != nil {
				// Not found — create
				result, err := cliCtx.ProtectClient.CreateRemovableStorageControlSet(ctx, input)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Created removable storage control set %q\n", input.Name)
				return printResult(cliCtx.Output, result, flattenRSCS(result))
			}

			// Found — confirm before replacing
			proceed, err := confirmReplace("removable storage control set", input.Name, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			result, err := cliCtx.ProtectClient.UpdateRemovableStorageControlSet(ctx, id, input)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated removable storage control set %q\n", input.Name)
			return printResult(cliCtx.Output, result, flattenRSCS(result))
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing")
	return cmd
}

func newProtectRSCSDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a removable storage control set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolveRemovableStorageControlSetID(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			proceed, err := confirmDelete("removable storage control set", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			if err := cliCtx.ProtectClient.DeleteRemovableStorageControlSet(cmd.Context(), id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted removable storage control set %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

// rscRuleToInput converts a RemovableStorageControlRule response to a RemovableStorageControlRuleInput for updates.
func rscRuleToInput(rule jamfprotect.RemovableStorageControlRule) jamfprotect.RemovableStorageControlRuleInput {
	input := jamfprotect.RemovableStorageControlRuleInput{
		Type: rule.Type,
	}

	msgAction := rule.MessageAction
	applyTo := rule.ApplyTo

	switch strings.ToLower(rule.Type) {
	case "vendor":
		input.VendorRule = &jamfprotect.RemovableStorageControlRuleDetails{
			MountAction:   rule.MountAction,
			MessageAction: strPtrIfNonEmpty(msgAction),
			ApplyTo:       strPtrIfNonEmpty(applyTo),
			Vendors:       rule.Vendors,
		}
	case "serial":
		input.SerialRule = &jamfprotect.RemovableStorageControlRuleDetails{
			MountAction:   rule.MountAction,
			MessageAction: strPtrIfNonEmpty(msgAction),
			ApplyTo:       strPtrIfNonEmpty(applyTo),
			Serials:       rule.Serials,
		}
	case "product":
		input.ProductRule = &jamfprotect.RemovableStorageControlProductRuleDetails{
			MountAction:   rule.MountAction,
			MessageAction: strPtrIfNonEmpty(msgAction),
			ApplyTo:       strPtrIfNonEmpty(applyTo),
			Products:      rule.Products,
		}
	case "encryption":
		input.EncryptionRule = &jamfprotect.RemovableStorageControlRuleDetails{
			MountAction:   rule.MountAction,
			MessageAction: strPtrIfNonEmpty(msgAction),
			ApplyTo:       strPtrIfNonEmpty(applyTo),
		}
	}

	return input
}

// strPtrIfNonEmpty returns a pointer to s if non-empty, otherwise nil.
func strPtrIfNonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// rebuildRSCSInput reconstructs a RemovableStorageControlSetInput from the current set state.
func rebuildRSCSInput(set *jamfprotect.RemovableStorageControlSet) jamfprotect.RemovableStorageControlSetInput {
	rules := make([]jamfprotect.RemovableStorageControlRuleInput, 0, len(set.Rules))
	for _, r := range set.Rules {
		rules = append(rules, rscRuleToInput(r))
	}
	return jamfprotect.RemovableStorageControlSetInput{
		Name:                 set.Name,
		Description:          set.Description,
		DefaultMountAction:   set.DefaultMountAction,
		DefaultMessageAction: set.DefaultMessageAction,
		Rules:                rules,
	}
}

func newProtectRSCSAddRuleCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var ruleType, mountAction, applyTo, vendors, serials string
	var yes bool
	cmd := &cobra.Command{
		Use:   "add-rule <set-name>",
		Short: "Add a rule to a removable storage control set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			id, err := r.ResolveRemovableStorageControlSetID(ctx, args[0])
			if err != nil {
				return err
			}

			set, err := cliCtx.ProtectClient.GetRemovableStorageControlSet(ctx, id)
			if err != nil {
				return err
			}

			input := rebuildRSCSInput(set)

			// Normalize flag values to canonical API casing
			switch strings.ToLower(applyTo) {
			case "all":
				applyTo = "All"
			case "encrypted":
				applyTo = "Encrypted"
			case "unencrypted":
				applyTo = "Unencrypted"
			default:
				return fmt.Errorf("unsupported --apply-to value %q; must be All, Encrypted, or Unencrypted", applyTo)
			}
			switch strings.ToLower(mountAction) {
			case "readwrite", "read-write":
				mountAction = "ReadWrite"
			case "readonly", "read-only":
				mountAction = "ReadOnly"
			case "prevented", "deny":
				mountAction = "Prevented"
			default:
				return fmt.Errorf("unsupported --mount-action value %q; must be ReadWrite, ReadOnly, or Prevented", mountAction)
			}

			var newRule jamfprotect.RemovableStorageControlRuleInput
			switch strings.ToLower(ruleType) {
			case "vendor":
				newRule = jamfprotect.RemovableStorageControlRuleInput{
					Type: "Vendor",
					VendorRule: &jamfprotect.RemovableStorageControlRuleDetails{
						MountAction: mountAction,
						ApplyTo:     &applyTo,
						Vendors:     splitCSV(vendors),
					},
				}
			case "serial":
				newRule = jamfprotect.RemovableStorageControlRuleInput{
					Type: "Serial",
					SerialRule: &jamfprotect.RemovableStorageControlRuleDetails{
						MountAction: mountAction,
						ApplyTo:     &applyTo,
						Serials:     splitCSV(serials),
					},
				}
			case "product":
				newRule = jamfprotect.RemovableStorageControlRuleInput{
					Type: "Product",
					ProductRule: &jamfprotect.RemovableStorageControlProductRuleDetails{
						MountAction: mountAction,
						ApplyTo:     &applyTo,
					},
				}
			case "encryption":
				newRule = jamfprotect.RemovableStorageControlRuleInput{
					Type: "Encryption",
					EncryptionRule: &jamfprotect.RemovableStorageControlRuleDetails{
						MountAction: mountAction,
					},
				}
			default:
				return fmt.Errorf("unsupported rule type %q; must be vendor, serial, product, or encryption", ruleType)
			}

			// Replace existing rule of the same type, or append if new
			replaced := false
			for i, r := range input.Rules {
				if strings.EqualFold(r.Type, ruleType) {
					proceed, err := confirmReplace(ruleType+" rule", args[0], yes)
					if err != nil {
						return err
					}
					if !proceed {
						return nil
					}
					input.Rules[i] = newRule
					replaced = true
					break
				}
			}
			if !replaced {
				input.Rules = append(input.Rules, newRule)
			}

			result, err := cliCtx.ProtectClient.UpdateRemovableStorageControlSet(ctx, id, input)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, result, flattenRSCS(result))
		},
	}
	cmd.Flags().StringVar(&ruleType, "type", "", "Rule type: vendor, serial, product, encryption")
	cmd.Flags().StringVar(&mountAction, "mount-action", "", "Mount action: ReadWrite, ReadOnly, Prevented")
	cmd.Flags().StringVar(&applyTo, "apply-to", "All", "Apply to: All, Encrypted, Unencrypted (for vendor/serial/product rules)")
	cmd.Flags().StringVar(&vendors, "vendors", "", "Comma-separated vendor identifiers (for vendor rules)")
	cmd.Flags().StringVar(&serials, "serials", "", "Comma-separated serial numbers (for serial rules)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing an existing rule")
	_ = cmd.MarkFlagRequired("type")
	_ = cmd.MarkFlagRequired("mount-action")
	return cmd
}

func newProtectRSCSRemoveRuleCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var ruleType string
	cmd := &cobra.Command{
		Use:   "remove-rule <set-name>",
		Short: "Remove a rule from a removable storage control set by type",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			id, err := r.ResolveRemovableStorageControlSetID(ctx, args[0])
			if err != nil {
				return err
			}

			set, err := cliCtx.ProtectClient.GetRemovableStorageControlSet(ctx, id)
			if err != nil {
				return err
			}

			input := rebuildRSCSInput(set)

			// Remove matching rules by type
			filtered := make([]jamfprotect.RemovableStorageControlRuleInput, 0, len(input.Rules))
			for _, rule := range input.Rules {
				if strings.EqualFold(rule.Type, ruleType) {
					continue
				}
				filtered = append(filtered, rule)
			}
			input.Rules = filtered

			result, err := cliCtx.ProtectClient.UpdateRemovableStorageControlSet(ctx, id, input)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, result, flattenRSCS(result))
		},
	}
	cmd.Flags().StringVar(&ruleType, "type", "", "Rule type to remove: vendor, serial, product, encryption")
	_ = cmd.MarkFlagRequired("type")
	return cmd
}

// splitCSV splits a comma-separated string into a trimmed slice.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			result = append(result, v)
		}
	}
	return result
}

func newProtectRSCSExportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "export <name>",
		Short: "Export a removable storage control set as JSON or YAML",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolveRemovableStorageControlSetID(ctx, args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.ProtectClient.GetRemovableStorageControlSet(ctx, id)
			if err != nil {
				return err
			}
			return printExport(rebuildRSCSInput(item))
		},
	}
}
