// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

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
		_, _ = fmt.Fprintln(out, "0 templates match the filter.")
		return nil
	}
	cats := uniqueCategories(tmpls)
	noun := "category"
	if len(cats) != 1 {
		noun = "categories"
	}
	_, _ = fmt.Fprintf(out, "Smart Group Templates — %d available across %d %s\n\n", len(tmpls), len(cats), noun)
	for _, cat := range cats {
		bucket := filterByCategory(tmpls, cat)
		_, _ = fmt.Fprintf(out, "Category: %s (%d)\n", cat, len(bucket))
		for _, t := range bucket {
			suffix := ""
			if len(t.Params) > 0 {
				suffix = fmt.Sprintf(" (params: --%s)", t.Params[0].Name)
			}
			_, _ = fmt.Fprintf(out, "  %-40s %s%s\n", t.Slug, t.Description, suffix)
		}
		_, _ = fmt.Fprintln(out)
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

func newSmartGroupPreviewCmd(_ *registry.CLIContext) *cobra.Command {
	var slug string
	cmd := &cobra.Command{
		Use:   "preview",
		Short: "Print the JSON body that would be POSTed (no API call)",
		Long: `Preview the JSON request that 'apply' would POST to
/v2/computer-groups/smart-groups for the chosen template. Use this to inspect
criteria before creating a group.`,
		Example: `  jamf-cli pro smart-group preview --template encryption/invalid-recovery-key
  jamf-cli pro smart-group preview --template encryption/encryption-stalled --stalled-after 14`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			tmpl, ok := smartgroup.Lookup(slug)
			if !ok {
				return unknownTemplateError(slug)
			}
			opts, err := collectParamValues(tmpl, cmd.Flags())
			if err != nil {
				return err
			}
			resolved, err := tmpl.ResolveOpts(opts)
			if err != nil {
				return err
			}
			req, err := tmpl.Build(resolved)
			if err != nil {
				return err
			}
			req.Name = "<--name required when running apply>"
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintln(out, "POST /v2/computer-groups/smart-groups")
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			return enc.Encode(req)
		},
	}
	cmd.Flags().StringVar(&slug, "template", "", "Template slug (required) — e.g. encryption/invalid-recovery-key")
	_ = cmd.MarkFlagRequired("template")
	registerTemplateParamFlags(cmd)
	return cmd
}

// registerTemplateParamFlags declares the union of all per-template param
// flag names on the cobra command as generic string flags. collectParamValues
// reads only the flags the chosen template actually declares.
func registerTemplateParamFlags(cmd *cobra.Command) {
	seen := make(map[string]bool)
	for _, t := range smartgroup.All() {
		for _, p := range t.Params {
			if seen[p.Name] {
				continue
			}
			seen[p.Name] = true
			cmd.Flags().String(p.Name, "", p.Description)
		}
	}
}

// flagReader is the minimal flag-access interface used by collectParamValues
// — satisfied by *pflag.FlagSet returned by cobra's cmd.Flags().
type flagReader interface {
	GetString(string) (string, error)
	Changed(string) bool
}

func collectParamValues(tmpl smartgroup.Template, flags flagReader) (map[string]any, error) {
	out := make(map[string]any, len(tmpl.Params))
	for _, p := range tmpl.Params {
		if !flags.Changed(p.Name) {
			continue
		}
		v, err := flags.GetString(p.Name)
		if err != nil {
			return nil, err
		}
		out[p.Name] = v // ResolveOpts coerces strings to int when Type is "int".
	}
	return out, nil
}

func unknownTemplateError(slug string) error {
	suggestions := smartgroup.FuzzyMatch(slug)
	if len(suggestions) == 0 {
		return fmt.Errorf("unknown template %q (run 'pro smart-group templates' to list available)", slug)
	}
	return fmt.Errorf("unknown template %q — did you mean: %s?", slug, strings.Join(suggestions, ", "))
}

func newSmartGroupApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		slug        string
		name        string
		recalculate bool
		dryRun      bool
		yes         bool
	)
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update a smart group from a template (idempotent by --name)",
		Long: `Apply a template against the live tenant. If a smart group with the
given --name already exists, it is updated (PUT); otherwise it is created
(POST). After apply, the membership endpoint is consulted and the count is
logged. Use --dry-run to inspect the request body without calling the API.`,
		Example: `  jamf-cli pro smart-group apply --template encryption/invalid-recovery-key --name "FV Invalid Recovery Keys"
  jamf-cli pro sg apply --template mdm/stale-checkin --name "Stale 30d" --days 30 --recalculate
  jamf-cli pro sg apply --template encryption/not-encrypted --name "Not Encrypted" --dry-run`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			tmpl, ok := smartgroup.Lookup(slug)
			if !ok {
				return unknownTemplateError(slug)
			}
			opts, err := collectParamValues(tmpl, cmd.Flags())
			if err != nil {
				return err
			}
			resolved, err := tmpl.ResolveOpts(opts)
			if err != nil {
				return err
			}
			req, err := tmpl.Build(resolved)
			if err != nil {
				return err
			}
			req.Name = name
			if dryRun {
				return printDryRun(cmd.OutOrStdout(), req)
			}
			if cliCtx.Client == nil {
				return fmt.Errorf("not authenticated to a Jamf Pro tenant; run 'jamf-cli pro setup' first")
			}
			return runApplyFlow(cmd.Context(), cmd.OutOrStdout(), cliCtx.Client, req, recalculate, yes)
		},
	}
	cmd.Flags().StringVar(&slug, "template", "", "Template slug (required)")
	cmd.Flags().StringVar(&name, "name", "", "Smart group name (required)")
	cmd.Flags().BoolVar(&recalculate, "recalculate", false, "After apply, force smart-group recalculation")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the request body without calling the API")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation when updating an existing group")
	_ = cmd.MarkFlagRequired("template")
	_ = cmd.MarkFlagRequired("name")
	registerTemplateParamFlags(cmd)
	return cmd
}

func printDryRun(out io.Writer, req smartgroup.SmartGroupRequest) error {
	_, _ = fmt.Fprintln(out, "POST /v2/computer-groups/smart-groups")
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(req)
}

func runApplyFlow(ctx context.Context, out io.Writer, client registry.HTTPClient, req smartgroup.SmartGroupRequest, recalculate, yes bool) error {
	existingID, err := lookupSmartGroupByName(ctx, client, req.Name)
	if err != nil {
		return err
	}

	var id string
	switch existingID {
	case "":
		newID, err := createSmartGroup(ctx, client, req)
		if err != nil {
			return err
		}
		id = newID
		_, _ = fmt.Fprintf(out, "Created smart group %q (ID: %s)\n", req.Name, id)
	default:
		if !yes {
			return fmt.Errorf("smart group %q already exists (ID %s); pass --yes to replace", req.Name, existingID)
		}
		if err := updateSmartGroup(ctx, client, existingID, req); err != nil {
			return err
		}
		id = existingID
		_, _ = fmt.Fprintf(out, "Updated smart group %q (ID: %s)\n", req.Name, id)
	}

	if recalculate {
		if err := recalculateSmartGroup(ctx, client, id); err != nil {
			_, _ = fmt.Fprintf(out, "Warning: recalculate did not complete: %v\n", err)
		}
	}

	count, err := smartgroup.CountMembers(ctx, client, id)
	if err != nil {
		_, _ = fmt.Fprintf(out, "Warning: membership check failed: %v\n", err)
		return nil
	}
	_, _ = fmt.Fprintf(out, "Membership: %d devices.\n", count)
	if count == 0 {
		_, _ = fmt.Fprintln(out, "This template matched 0 devices. Run 'pro sg verify-templates' to check criterion compatibility with your tenant.")
	}
	return nil
}

func lookupSmartGroupByName(ctx context.Context, client registry.HTTPClient, name string) (string, error) {
	filter := url.QueryEscape(fmt.Sprintf(`name=="%s"`, name))
	path := "/v2/computer-groups/smart-groups?filter=" + filter
	resp, err := client.Do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("lookup smart group: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		TotalCount int `json:"totalCount"`
		Results    []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	for _, r := range out.Results {
		if r.Name == name {
			return r.ID, nil
		}
	}
	return "", nil
}

func createSmartGroup(ctx context.Context, client registry.HTTPClient, req smartgroup.SmartGroupRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(ctx, http.MethodPost, "/v2/computer-groups/smart-groups", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == 403 {
		return "", fmt.Errorf("permission denied: the OAuth role is missing the 'Create Smart Computer Groups' privilege")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create smart group: HTTP %d: %s", resp.StatusCode, string(buf))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func updateSmartGroup(ctx context.Context, client registry.HTTPClient, id string, req smartgroup.SmartGroupRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	resp, err := client.Do(ctx, http.MethodPut, "/v2/computer-groups/smart-groups/"+id, bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == 403 {
		return fmt.Errorf("permission denied: the OAuth role is missing the 'Update Smart Computer Groups' privilege")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update smart group: HTTP %d: %s", resp.StatusCode, string(buf))
	}
	return nil
}

func recalculateSmartGroup(ctx context.Context, client registry.HTTPClient, id string) error {
	resp, err := client.Do(ctx, http.MethodPost, "/v1/smart-computer-groups/"+id+"/recalculate", nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("recalculate: HTTP %d", resp.StatusCode)
	}
	return nil
}

func newSmartGroupVerifyTemplatesCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		category   string
		noCleanup  bool
		jsonOutput bool
	)
	cmd := &cobra.Command{
		Use:   "verify-templates",
		Short: "Smoke-test every template against the live tenant",
		Long: `Create one temporary smart group per template (prefixed "__verify_"),
recalculate it, read the membership count, and report. Temporary groups are
deleted on completion unless --no-cleanup is set.

Use this on first run after install (and after any sync-specs that touches
JSS) to confirm criterion-name strings match your Jamf Pro version.`,
		Example: `  jamf-cli pro smart-group verify-templates
  jamf-cli pro sg verify-templates --category encryption
  jamf-cli pro sg verify-templates --no-cleanup`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cliCtx.Client == nil {
				return fmt.Errorf("not authenticated to a Jamf Pro tenant; run 'jamf-cli pro setup' first")
			}
			var tmpls []smartgroup.Template
			if category != "" {
				tmpls = smartgroup.ByCategory(category)
			} else {
				tmpls = smartgroup.All()
			}
			results := make([]smartgroup.VerifyResult, 0, len(tmpls))
			for _, t := range tmpls {
				results = append(results, smartgroup.RunOneVerification(cmd.Context(), cliCtx.Client, t, !noCleanup))
			}
			return renderVerifyResults(cmd.OutOrStdout(), results, jsonOutput)
		},
	}
	cmd.Flags().StringVar(&category, "category", "", "Verify only one category")
	cmd.Flags().BoolVar(&noCleanup, "no-cleanup", false, "Keep temporary groups instead of deleting them")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output JSON instead of human-readable summary")
	return cmd
}

func renderVerifyResults(out io.Writer, results []smartgroup.VerifyResult, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}
	ok, zero, errs := 0, 0, 0
	_, _ = fmt.Fprintf(out, "Verifying %d templates...\n\n", len(results))
	for _, r := range results {
		switch r.Outcome {
		case smartgroup.VerifyOK:
			ok++
			_, _ = fmt.Fprintf(out, "✓ %-40s — %d devices match\n", r.Slug, r.MemberCount)
		case smartgroup.VerifyZeroMatch:
			zero++
			_, _ = fmt.Fprintf(out, "⚠ %-40s — 0 devices match (possible criterion mismatch)\n", r.Slug)
		case smartgroup.VerifyError:
			errs++
			_, _ = fmt.Fprintf(out, "✗ %-40s — ERROR: %s\n", r.Slug, r.Error)
		}
	}
	_, _ = fmt.Fprintf(out, "\nSummary: %d OK, %d zero-match warnings, %d errors.\n", ok, zero, errs)
	return nil
}
