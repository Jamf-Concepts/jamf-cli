// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/smartgroup"
)

func newSmartGroupCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "smart-group",
		Short: "Curated smart-group templates: list, preview, apply, verify",
		Long: `Create useful Jamf Pro smart groups from a curated library of templates.

Templates encode operationally-essential smart groups (devices not encrypted,
recovery keys invalid, OS versions behind, bootstrap tokens missing, etc.) so
admins don't have to assemble them by hand.

Templates are sourced from JSS canonical criterion-name strings. Run
'pro smart-group verify-templates' once against your tenant to confirm each
template matches as expected.`,
	}

	cmd.AddCommand(newSmartGroupTemplatesCmd(cliCtx))
	cmd.AddCommand(newSmartGroupPreviewCmd(cliCtx))
	cmd.AddCommand(newSmartGroupApplyCmd(cliCtx))
	cmd.AddCommand(newSmartGroupVerifyTemplatesCmd(cliCtx))

	return cmd
}

func newSmartGroupTemplatesCmd(_ *registry.CLIContext) *cobra.Command {
	var (
		category string
		format   string
	)
	cmd := &cobra.Command{
		Use:   "templates",
		Short: "List available smart-group templates",
		Long: `List all curated smart-group templates. Use --category to filter
to one of: encryption, updates, mdm, compliance, lifecycle.`,
		Example: `  # All templates grouped by category
  jamf-cli pro smart-group templates

  # Just encryption templates
  jamf-cli pro smart-group templates --category encryption

  # Machine-readable
  jamf-cli pro smart-group templates -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var tmpls []smartgroup.Template
			if category != "" {
				tmpls = smartgroup.ByCategory(category)
			} else {
				tmpls = smartgroup.All()
			}
			return renderTemplatesList(cmd, tmpls, format)
		},
	}
	cmd.Flags().StringVar(&category, "category", "", "Filter by category (encryption|updates|mdm|compliance|lifecycle)")
	cmd.Flags().StringVarP(&format, "output", "o", "table", "Output format: table|json")
	return cmd
}

func renderTemplatesList(cmd *cobra.Command, tmpls []smartgroup.Template, format string) error {
	out := cmd.OutOrStdout()
	if format == "json" {
		return writeTemplatesJSON(out, tmpls)
	}
	if len(tmpls) == 0 {
		fmt.Fprintln(out, "0 templates match the filter.")
		return nil
	}
	cats := uniqueCategories(tmpls)
	noun := "category"
	if len(cats) != 1 {
		noun = "categories"
	}
	fmt.Fprintf(out, "Smart Group Templates — %d available across %d %s\n\n", len(tmpls), len(cats), noun)
	for _, cat := range cats {
		bucket := filterByCategory(tmpls, cat)
		fmt.Fprintf(out, "Category: %s (%d)\n", cat, len(bucket))
		for _, t := range bucket {
			suffix := ""
			if len(t.Params) > 0 {
				suffix = fmt.Sprintf(" (params: --%s)", t.Params[0].Name)
			}
			fmt.Fprintf(out, "  %-40s %s%s\n", t.Slug, t.Description, suffix)
		}
		fmt.Fprintln(out)
	}
	return nil
}

func writeTemplatesJSON(out io.Writer, tmpls []smartgroup.Template) error {
	type paramOut struct {
		Name        string `json:"name"`
		Type        string `json:"type"`
		Default     any    `json:"default,omitempty"`
		Description string `json:"description"`
		Required    bool   `json:"required"`
	}
	type tmplOut struct {
		Slug        string     `json:"slug"`
		Category    string     `json:"category"`
		Description string     `json:"description"`
		Params      []paramOut `json:"params"`
	}
	rows := make([]tmplOut, 0, len(tmpls))
	for _, t := range tmpls {
		row := tmplOut{Slug: t.Slug, Category: t.Category, Description: t.Description, Params: []paramOut{}}
		for _, p := range t.Params {
			row.Params = append(row.Params, paramOut{
				Name: p.Name, Type: p.Type, Default: p.Default,
				Description: p.Description, Required: p.Required,
			})
		}
		rows = append(rows, row)
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

func uniqueCategories(tmpls []smartgroup.Template) []string {
	seen := make(map[string]struct{}, 5)
	out := make([]string, 0, 5)
	for _, t := range tmpls {
		if _, ok := seen[t.Category]; ok {
			continue
		}
		seen[t.Category] = struct{}{}
		out = append(out, t.Category)
	}
	sort.Strings(out)
	return out
}

func filterByCategory(tmpls []smartgroup.Template, cat string) []smartgroup.Template {
	out := make([]smartgroup.Template, 0)
	for _, t := range tmpls {
		if t.Category == cat {
			out = append(out, t)
		}
	}
	return out
}

// Stubs for the remaining subcommands. Replaced in Tasks 13-15.

func newSmartGroupPreviewCmd(_ *registry.CLIContext) *cobra.Command {
	return &cobra.Command{Use: "preview", Short: "Preview a template (stub)"}
}

func newSmartGroupApplyCmd(_ *registry.CLIContext) *cobra.Command {
	return &cobra.Command{Use: "apply", Short: "Apply a template (stub)"}
}

func newSmartGroupVerifyTemplatesCmd(_ *registry.CLIContext) *cobra.Command {
	return &cobra.Command{Use: "verify-templates", Short: "Verify templates against the live tenant (stub)"}
}
