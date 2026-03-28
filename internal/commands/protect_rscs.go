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
	cmd.AddCommand(newProtectRSCSCreateCmd(cliCtx))
	cmd.AddCommand(newProtectRSCSUpdateCmd(cliCtx))
	cmd.AddCommand(newProtectRSCSDeleteCmd(cliCtx))
	cmd.AddCommand(newProtectRSCSAddRuleCmd(cliCtx))
	cmd.AddCommand(newProtectRSCSRemoveRuleCmd(cliCtx))

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
			data, _ := json.Marshal(rows)
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

// flattenRSCS converts a RemovableStorageControlSet into a clean map for
// readable table output, summarising rules and plans.
func flattenRSCS(s jamfprotect.RemovableStorageControlSet) map[string]any {
	m := map[string]any{
		"name":                 s.Name,
		"description":          s.Description,
		"defaultMountAction":   s.DefaultMountAction,
		"defaultMessageAction": s.DefaultMessageAction,
		"created":              s.Created,
		"updated":              s.Updated,
	}
	if len(s.Rules) > 0 {
		types := make([]string, 0, len(s.Rules))
		for _, r := range s.Rules {
			types = append(types, r.Type)
		}
		m["rules"] = strings.Join(types, ", ")
		m["rulesCount"] = len(s.Rules)
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
			return protect.PrintOne(cliCtx.Output, item)
		},
	}
}

func newProtectRSCSCreateCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var fromFile string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a removable storage control set",
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := readProtectInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfprotect.RemovableStorageControlSetInput
			if err := json.Unmarshal(data, &input); err != nil {
				return fmt.Errorf("parsing input JSON: %w", err)
			}
			result, err := cliCtx.ProtectClient.CreateRemovableStorageControlSet(cmd.Context(), input)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, result)
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	return cmd
}

func newProtectRSCSUpdateCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var fromFile string
	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update a removable storage control set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolveRemovableStorageControlSetID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			data, err := readProtectInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfprotect.RemovableStorageControlSetInput
			if err := json.Unmarshal(data, &input); err != nil {
				return fmt.Errorf("parsing input JSON: %w", err)
			}
			result, err := cliCtx.ProtectClient.UpdateRemovableStorageControlSet(cmd.Context(), id, input)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, result)
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
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

			proceed, err := confirmProtectDelete("removable storage control set", args[0], yes)
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
	var ruleType, mountAction, vendors, serials string
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

			newRule := jamfprotect.RemovableStorageControlRuleInput{
				Type: ruleType,
			}
			switch strings.ToLower(ruleType) {
			case "vendor":
				newRule.VendorRule = &jamfprotect.RemovableStorageControlRuleDetails{
					MountAction: mountAction,
					Vendors:     splitCSV(vendors),
				}
			case "serial":
				newRule.SerialRule = &jamfprotect.RemovableStorageControlRuleDetails{
					MountAction: mountAction,
					Serials:     splitCSV(serials),
				}
			case "product":
				newRule.ProductRule = &jamfprotect.RemovableStorageControlProductRuleDetails{
					MountAction: mountAction,
				}
			case "encryption":
				newRule.EncryptionRule = &jamfprotect.RemovableStorageControlRuleDetails{
					MountAction: mountAction,
				}
			default:
				return fmt.Errorf("unsupported rule type %q; must be vendor, serial, product, or encryption", ruleType)
			}

			input.Rules = append(input.Rules, newRule)

			result, err := cliCtx.ProtectClient.UpdateRemovableStorageControlSet(ctx, id, input)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, result)
		},
	}
	cmd.Flags().StringVar(&ruleType, "type", "", "Rule type: vendor, serial, product, encryption")
	cmd.Flags().StringVar(&mountAction, "mount-action", "", "Mount action: ALLOW, DENY, READ_ONLY")
	cmd.Flags().StringVar(&vendors, "vendors", "", "Comma-separated vendor identifiers (for vendor rules)")
	cmd.Flags().StringVar(&serials, "serials", "", "Comma-separated serial numbers (for serial rules)")
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
			return protect.PrintOne(cliCtx.Output, result)
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
