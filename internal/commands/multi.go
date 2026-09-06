// Copyright 2026, Jamf Software LLC

package commands

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newMultiCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		filter      string
		profilesCSV string
		fromFile    string
		sequential  bool
	)

	cmd := &cobra.Command{
		Use:   "multi [flags] [--] <command> [args...]",
		Short: "Run a command against multiple profiles",
		Long: `Execute any jamf-cli command against multiple config profiles.

Target profiles can be selected by glob pattern (--filter), explicit list
(--profiles), or from a file (--from-file). The file can contain profile
names or instance URLs — URLs are matched against profile URLs in your
config.

When no targeting flag is provided, an interactive selection prompt is
shown. The -- separator is optional but recommended when the inner
command has flags that could conflict with multi's flags.

Report commands are automatically aggregated: summaries are combined and
detail rows are merged with a profile column added.

Examples:
  # Interactive profile selection (no multi flags, no -- needed)
  jamf-cli multi pro overview
  jamf-cli multi pro comp list

  # Filter by glob pattern (use -- to separate flags)
  jamf-cli multi --filter 'pro-*' -- pro comp list -o table

  # Explicit list
  jamf-cli multi --profiles pro-school1,pro-school2 -- pro overview

  # Aggregated report across instances
  jamf-cli multi --filter 'pro-*' -- pro report profile-status

  # Reuse the same URL file from setup
  jamf-cli multi --from-file instances.txt -- pro buildings apply --from-file b.json --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.ErrOrStderr()

			// Determine inner command args.
			// With --:    multi --filter 'pro-*' -- pro comp list
			// Without --: multi pro comp list (interactive mode)
			dashIdx := cmd.ArgsLenAtDash()
			var innerArgs []string
			if dashIdx >= 0 {
				innerArgs = args[dashIdx:]
				if len(innerArgs) == 0 {
					return fmt.Errorf("no command specified after --")
				}
			} else if len(args) > 0 {
				innerArgs = args
			} else {
				return fmt.Errorf("no command specified\n\nUsage: jamf-cli multi [flags] [--] <command> [args...]")
			}

			// Load config
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			// Determine product from inner command to filter out wrong-product profiles
			product := detectProduct(innerArgs)

			// Resolve target profiles
			profiles, err := resolveMultiProfiles(cfg, filter, profilesCSV, fromFile, product, noInput)
			if err != nil {
				return err
			}

			// Get the executable path for re-invocation
			executable, err := os.Executable()
			if err != nil {
				return fmt.Errorf("determining executable path: %w", err)
			}

			_, _ = fmt.Fprintf(w, "Running against %d profile(s)...\n", len(profiles))

			// Try to aggregate output unless --sequential is set
			shouldAggregate := !sequential

			var succeeded, failed int
			var failures []string

			if shouldAggregate {
				// Aggregate mode: capture JSON from each child, merge, re-render
				desiredFmt, captureArgs := parseOutputFlag(innerArgs)
				// For structured output (json/yaml) capture full data; for display formats
				// capture with json-multi which applies column selection so re-rendered
				// tables match the columns from a direct command.
				captureFmt := "json-multi"
				if desiredFmt == "json" || desiredFmt == "yaml" {
					captureFmt = "json"
				}
				results := make([]childResult, len(profiles))
				for i, profileName := range profiles {
					url := ""
					if p, ok := cfg.Profiles[profileName]; ok {
						url = p.URL
					}

					_, _ = fmt.Fprintf(w, "  [%d/%d] %s...\n", i+1, len(profiles), profileName)

					cmdArgs := append([]string{"--profile", profileName, "-o", captureFmt}, captureArgs...)
					child := exec.Command(executable, cmdArgs...)
					child.Env = append(os.Environ(), "JAMF_CLI_MULTI_CAPTURE=1")
					var stdout bytes.Buffer
					child.Stdout = &stdout
					child.Stderr = cmd.ErrOrStderr()

					results[i] = childResult{
						profileName: profileName,
						profileURL:  url,
						err:         child.Run(),
					}
					results[i].stdout = stdout.Bytes()
				}

				for _, r := range results {
					if r.err != nil {
						failures = append(failures, fmt.Sprintf("%s: %v", r.profileName, r.err))
						failed++
					} else {
						succeeded++
					}
				}

				if aggregated := tryAggregate(results); aggregated != nil {
					if err := printAggregated(cliCtx, cmd, aggregated, desiredFmt); err != nil {
						return err
					}
				} else {
					// Aggregation failed — fall back to sequential with banners
					for _, r := range results {
						if r.err != nil {
							continue
						}
						url := r.profileURL
						if url != "" {
							_, _ = fmt.Fprintf(w, "\n── %s (%s) ──\n", r.profileName, url)
						} else {
							_, _ = fmt.Fprintf(w, "\n── %s ──\n", r.profileName)
						}
						_, _ = writerFor(cliCtx).Write(r.stdout)
					}
				}
			} else {
				// Sequential mode: stream each child's output directly
				total := len(profiles)
				for i, profileName := range profiles {
					url := ""
					if p, ok := cfg.Profiles[profileName]; ok {
						url = p.URL
					}

					if url != "" {
						_, _ = fmt.Fprintf(w, "\n── [%d/%d] %s (%s) ──\n", i+1, total, profileName, url)
					} else {
						_, _ = fmt.Fprintf(w, "\n── [%d/%d] %s ──\n", i+1, total, profileName)
					}

					cmdArgs := append([]string{"--profile", profileName}, innerArgs...)
					child := exec.Command(executable, cmdArgs...)
					// The child's payload is this command's output, so it
					// follows --out-file with the aggregated report's. Its
					// banners stay on stderr, being progress rather than data.
					child.Stdout = writerFor(cliCtx)
					child.Stderr = cmd.ErrOrStderr()

					if err := child.Run(); err != nil {
						_, _ = fmt.Fprintf(w, "  ✗ FAILED: %v\n", err)
						failures = append(failures, fmt.Sprintf("%s: %v", profileName, err))
						failed++
					} else {
						succeeded++
					}
				}
			}

			// Summary
			_, _ = fmt.Fprintf(w, "\n── Summary ──\n")
			_, _ = fmt.Fprintf(w, "  Succeeded: %d\n", succeeded)
			if len(failures) > 0 {
				_, _ = fmt.Fprintf(w, "  Failed:    %d\n", len(failures))
				for _, f := range failures {
					_, _ = fmt.Fprintf(w, "    - %s\n", f)
				}
				return fmt.Errorf("%d of %d profile(s) failed", len(failures), len(profiles))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&filter, "filter", "", "glob pattern to match profile names (e.g., 'pro-*')")
	cmd.Flags().StringVar(&profilesCSV, "profiles", "", "comma-separated list of profile names")
	cmd.Flags().StringVar(&fromFile, "from-file", "", "file containing profile names or instance URLs (one per line)")
	cmd.Flags().BoolVar(&sequential, "sequential", false, "show each instance's output separately instead of aggregating")
	cmd.MarkFlagsMutuallyExclusive("filter", "profiles", "from-file")

	return cmd
}

// ---------------------------------------------------------------------------
// Aggregation
// ---------------------------------------------------------------------------

// mergedListKey is the internal key used when wrapping a flat list result for
// aggregation. Referenced in both tryAggregate (wrap) and printAggregated (unwrap).
const mergedListKey = "results"

// tryAggregate attempts to parse and merge JSON output from multiple children.
// Returns nil if the output isn't aggregatable (non-JSON, or not a structured
// report format).
func tryAggregate(results []childResult) map[string]any {
	var parsed []struct {
		profileName string
		profileURL  string
		data        map[string]any
	}

	for _, r := range results {
		if r.err != nil || len(r.stdout) == 0 {
			continue
		}

		// Try to parse as JSON array containing a single object with sections
		// (report format: [{summary: {}, failures: [], ...}])
		var arr []map[string]any
		if err := json.Unmarshal(r.stdout, &arr); err == nil && len(arr) == 1 {
			// Only aggregate if the object has list or dict sections —
			// a flat scalar object (e.g. create/update response) should
			// not be aggregated.
			if hasAggregatableSections(arr[0]) {
				parsed = append(parsed, struct {
					profileName string
					profileURL  string
					data        map[string]any
				}{r.profileName, r.profileURL, arr[0]})
				continue
			}
		}

		// Try as flat array of rows (e.g. patch-status without --scan-failures)
		var flatArr []map[string]any
		if err := json.Unmarshal(r.stdout, &flatArr); err == nil {
			// Convert to []any for consistent type handling in merge
			asAny := make([]any, len(flatArr))
			for i, row := range flatArr {
				asAny[i] = row
			}
			parsed = append(parsed, struct {
				profileName string
				profileURL  string
				data        map[string]any
			}{r.profileName, r.profileURL, map[string]any{mergedListKey: asAny}})
			continue
		}

		// Not aggregatable
		return nil
	}

	if len(parsed) == 0 {
		return nil
	}

	// Merge: for each key across all parsed results:
	//   dict   → sum numeric values (summary)
	//   list   → concatenate, inject "profile" into each row (detail)
	//   number → sum
	//   null   → skip
	merged := make(map[string]any)

	for _, p := range parsed {
		for key, val := range p.data {
			switch v := val.(type) {
			case map[string]any:
				// Summary dict — sum count-like numeric fields, keep config
				// fields (like "days") as-is when they're the same across instances.
				existing, _ := merged[key].(map[string]any)
				if existing == nil {
					existing = make(map[string]any)
				}
				for sk, sv := range v {
					if summaryFieldShouldSum(sk) {
						switch sn := sv.(type) {
						case float64:
							prev, _ := existing[sk].(float64)
							existing[sk] = prev + sn
						case int:
							prev, _ := existing[sk].(float64)
							existing[sk] = prev + float64(sn)
						}
					} else if _, exists := existing[sk]; !exists {
						existing[sk] = sv
					}
				}
				merged[key] = existing

			case []any:
				if isSummaryList(v) {
					// Summary list — rows have a "count" field and no device-
					// specific data. Sum counts grouped by label keys.
					existing, _ := merged[key].(map[string]map[string]any)
					if existing == nil {
						existing = make(map[string]map[string]any)
					}
					for _, item := range v {
						row, ok := item.(map[string]any)
						if !ok {
							continue
						}
						label := summaryRowLabel(row)
						count, _ := row["count"].(float64)
						if prev, ok := existing[label]; ok {
							prevCount, _ := prev["count"].(float64)
							prev["count"] = prevCount + count
						} else {
							// Clone the row
							clone := make(map[string]any, len(row))
							for k, v := range row {
								clone[k] = v
							}
							existing[label] = clone
						}
					}
					merged[key] = existing
				} else {
					// Detail list — concatenate, inject profile
					existing, _ := merged[key].([]any)
					for _, item := range v {
						if row, ok := item.(map[string]any); ok {
							row["profile"] = p.profileName
						}
						existing = append(existing, item)
					}
					merged[key] = existing
				}

			case float64:
				prev, _ := merged[key].(float64)
				merged[key] = prev + v

			case nil:
				if _, exists := merged[key]; !exists {
					merged[key] = nil
				}
			}
		}
	}

	return merged
}

type childResult struct {
	profileName string
	profileURL  string
	stdout      []byte
	err         error
}

// printAggregated renders the merged report using the desired output format.
// desiredFmt is extracted from the inner command args; empty string defaults to table.
func printAggregated(cliCtx *registry.CLIContext, cmd *cobra.Command, merged map[string]any, desiredFmt string) error {
	renderFmt := desiredFmt
	if renderFmt == "" {
		// No -o in inner args — check if multi itself had -o set, else default to table
		if cmd.Flags().Changed("output") {
			renderFmt = outputFmt
		} else {
			renderFmt = "table"
		}
	}
	formatter := formatterFor(cliCtx, renderFmt)

	if renderFmt == "json" || renderFmt == "yaml" {
		// Convert aggregated summary maps back to list format for JSON
		jsonMerged := make(map[string]any, len(merged))
		for k, v := range merged {
			if rowMap, ok := v.(map[string]map[string]any); ok {
				rows := make([]map[string]any, 0, len(rowMap))
				for _, row := range rowMap {
					rows = append(rows, row)
				}
				jsonMerged[k] = rows
			} else {
				jsonMerged[k] = v
			}
		}
		// For plain list commands the merge wraps rows in {mergedListKey: [...]}.
		// Unwrap to a flat array so json/yaml output matches single-instance output.
		if len(jsonMerged) == 1 {
			if results, ok := jsonMerged[mergedListKey]; ok {
				if rows, ok := results.([]map[string]any); ok {
					// The same drop the table arms below apply. Without it this
					// branch answered a --select miss with `[{}]` and nothing on
					// stderr, a third shape for one condition.
					kept, dropped := selectSurvivors(rows)
					reportSelectMiss(dropped)
					return formatter.Print(kept)
				}
				return formatter.Print(results)
			}
		}
		return formatter.Print([]map[string]any{jsonMerged})
	}

	// Table mode: render each section
	// Sort keys for deterministic order, with "summary" first
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i] == "summary" {
			return true
		}
		if keys[j] == "summary" {
			return false
		}
		return keys[i] < keys[j]
	})

	// The section headers belong wherever the tables go, or --out-file splits one
	// report between a file and the terminal.
	out := formatter.Writer()

	first := true
	for _, key := range keys {
		val := merged[key]
		switch v := val.(type) {
		case map[string]any:
			// Summary dict — print as single-row table
			summaryRows := []map[string]any{v}
			// Gate before the separator and the header, not after: a --select
			// naming nothing here would otherwise leave a banner and a blank
			// table, which is the shape printRows was fixed to stop rendering.
			// Reachable only since this branch moved onto formatterFor for the
			// --out-file fix, which is what made --select live here at all.
			kept, dropped := selectSurvivors(summaryRows)
			reportSelectMiss(dropped)
			if len(kept) == 0 {
				continue
			}
			summaryRows = kept
			if !first {
				_, _ = fmt.Fprintln(out)
			}
			_, _ = fmt.Fprintf(out, "── %s ──\n", formatSectionTitle(key))
			if err := formatter.Print(summaryRows); err != nil {
				return err
			}
			first = false

		case map[string]map[string]any:
			// Aggregated summary list — render as table sorted by count desc
			if len(v) == 0 {
				continue
			}
			rows := make([]map[string]any, 0, len(v))
			for _, row := range v {
				rows = append(rows, row)
			}
			sort.Slice(rows, func(i, j int) bool {
				ci, _ := rows[i]["count"].(float64)
				cj, _ := rows[j]["count"].(float64)
				return ci > cj
			})
			kept, dropped := selectSurvivors(rows)
			reportSelectMiss(dropped)
			if len(kept) == 0 {
				continue
			}
			rows = kept
			if !first {
				_, _ = fmt.Fprintln(out)
			}
			_, _ = fmt.Fprintf(out, "── %s (%d) ──\n", formatSectionTitle(key), len(rows))
			if err := formatter.Print(rows); err != nil {
				return err
			}
			first = false

		case []any:
			if len(v) == 0 {
				continue
			}
			// Detail list — print as multi-row table
			rows := make([]map[string]any, 0, len(v))
			for _, item := range v {
				if row, ok := item.(map[string]any); ok {
					rows = append(rows, row)
				}
			}
			if len(rows) == 0 {
				continue
			}
			kept, dropped := selectSurvivors(rows)
			reportSelectMiss(dropped)
			if len(kept) == 0 {
				continue
			}
			rows = kept
			if !first {
				_, _ = fmt.Fprintln(out)
			}
			_, _ = fmt.Fprintf(out, "── %s (%d) ──\n", formatSectionTitle(key), len(rows))
			if err := formatter.Print(rows); err != nil {
				return err
			}
			first = false

		case float64:
			// Top-level scalar — skip in table mode (included in JSON)
		}
	}

	return nil
}

// summaryFieldShouldSum returns true if a summary field represents a count
// that should be summed across instances (e.g. total_errors, warnings).
// Config/parameter fields (days, threshold) should NOT be summed.
func summaryFieldShouldSum(field string) bool {
	// Fields that are config/parameters, not counts
	noSum := map[string]bool{
		"days": true,
	}
	if noSum[field] {
		return false
	}
	// Heuristic: fields with count-like names or known patterns should sum
	// Default to summing numeric fields
	return true
}

// parseOutputFlag extracts the -o/--output value from args and returns it along
// with a copy of args with all -o/--output occurrences removed. Handles all
// flag forms: -o json, -o=json, -ojson, --output json, --output=json.
func parseOutputFlag(args []string) (value string, stripped []string) {
	skip := false
	for i, arg := range args {
		if skip {
			skip = false
			continue
		}
		if arg == "-o" || arg == "--output" {
			if i+1 < len(args) && value == "" {
				value = args[i+1]
			}
			skip = true
			continue
		}
		if after, ok := strings.CutPrefix(arg, "-o="); ok {
			if value == "" {
				value = after
			}
			continue
		}
		if after, ok := strings.CutPrefix(arg, "--output="); ok {
			if value == "" {
				value = after
			}
			continue
		}
		if !strings.HasPrefix(arg, "--") && strings.HasPrefix(arg, "-o") && len(arg) > 2 {
			if value == "" {
				value = arg[2:]
			}
			continue
		}
		stripped = append(stripped, arg)
	}
	return
}

// hasAggregatableSections returns true if a single-object JSON response
// contains list or dict sections (indicating a report), not just scalars
// (indicating a create/update/delete response).
func hasAggregatableSections(obj map[string]any) bool {
	for _, v := range obj {
		switch v.(type) {
		case []any, map[string]any:
			return true
		}
	}
	return false
}

// isSummaryList returns true if a list of rows looks like a summary table
// (has "count" field and no device-identifying fields like serial, device_id,
// username). These should be aggregated by summing counts.
func isSummaryList(items []any) bool {
	if len(items) == 0 {
		return false
	}
	// Check first row
	row, ok := items[0].(map[string]any)
	if !ok {
		return false
	}
	if _, hasCount := row["count"]; !hasCount {
		return false
	}
	// If it has device-specific fields, it's detail not summary
	deviceFields := []string{"serial", "device_id", "device", "username", "management_id", "name"}
	for _, f := range deviceFields {
		if _, has := row[f]; has {
			return false
		}
	}
	return true
}

// summaryRowLabel builds a composite label from all non-"count" fields in a
// summary row, joined by "|". This handles rows with multiple label fields
// (e.g. model + os_version in inventory-summary).
func summaryRowLabel(row map[string]any) string {
	// Collect non-count string fields in sorted order for determinism
	var keys []string
	for k := range row {
		if k == "count" || k == "profile" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		switch v := row[k].(type) {
		case string:
			parts = append(parts, v)
		case float64:
			parts = append(parts, fmt.Sprintf("%g", v))
		}
	}
	if len(parts) == 0 {
		return "unknown"
	}
	return strings.Join(parts, "|")
}

// formatSectionTitle converts a JSON key to a display title.
// "config_findings" → "Config Findings", "summary" → "Summary"
func formatSectionTitle(key string) string {
	words := strings.Split(key, "_")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// ---------------------------------------------------------------------------
// Profile resolution (unchanged)
// ---------------------------------------------------------------------------

// resolveMultiProfiles determines which profiles to target based on flags.
// When no flag is set, prompts interactively (unless --no-input).
// The product parameter filters profiles to match the inner command's product
// ("pro" or "protect"), preventing accidental cross-product execution.
func resolveMultiProfiles(cfg *config.Config, filter, profilesCSV, fromFile, product string, noInput bool) ([]string, error) {
	allNames := sortedProfileNames(cfg)
	if len(allNames) == 0 {
		return nil, fmt.Errorf("no profiles configured — run 'jamf-cli pro setup' first")
	}

	var names []string
	var err error

	switch {
	case filter != "":
		names, err = filterProfiles(allNames, filter)
	case profilesCSV != "":
		names, err = validateProfileNames(cfg, splitAndTrim(profilesCSV, ","))
	case fromFile != "":
		names, err = readProfilesFromFile(cfg, fromFile)
	default:
		// For interactive selection, pre-filter the list by product
		filteredNames := filterByProduct(cfg, allNames, product)
		names, err = promptProfileSelection(cfg, filteredNames, noInput)
	}
	if err != nil {
		return nil, err
	}

	// Filter resolved profiles by product (skip silently for explicit --profiles)
	if product != "" && profilesCSV == "" {
		names = filterByProduct(cfg, names, product)
	}

	if len(names) == 0 {
		return nil, fmt.Errorf("no matching %s profiles found", product)
	}
	return names, nil
}

// isReportCommand checks if the inner command args contain "report" as a subcommand.
// detectProduct inspects the inner command args to determine the target product.
// Returns "pro", "protect", or "" if indeterminate.
func detectProduct(innerArgs []string) string {
	for _, arg := range innerArgs {
		switch arg {
		case "pro":
			return "pro"
		case "protect":
			return "protect"
		case "school":
			return "school"
		case "security":
			return "security"
		}
		// Stop at first non-flag argument
		if !strings.HasPrefix(arg, "-") {
			break
		}
	}
	return ""
}

// filterByProduct returns only profiles matching the given product type.
// Pro profiles have Product=="" or Product=="pro". Protect profiles have Product=="protect".
func filterByProduct(cfg *config.Config, names []string, product string) []string {
	if product == "" {
		return names
	}
	var filtered []string
	for _, name := range names {
		p := cfg.Profiles[name]
		profileProduct := p.Product
		if profileProduct == "" {
			profileProduct = "pro"
		}
		if profileProduct == product {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

// sortedProfileNames returns all profile names in sorted order.
func sortedProfileNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// filterProfiles returns profile names matching a glob pattern.
func filterProfiles(allNames []string, pattern string) ([]string, error) {
	var matched []string
	for _, name := range allNames {
		ok, err := filepath.Match(pattern, name)
		if err != nil {
			return nil, fmt.Errorf("invalid filter pattern %q: %w", pattern, err)
		}
		if ok {
			matched = append(matched, name)
		}
	}
	if len(matched) == 0 {
		return nil, fmt.Errorf("no profiles match filter %q", pattern)
	}
	return matched, nil
}

// validateProfileNames checks that all names exist in config and deduplicates.
func validateProfileNames(cfg *config.Config, names []string) ([]string, error) {
	seen := make(map[string]bool)
	var result []string
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, ok := cfg.Profiles[name]; !ok {
			return nil, fmt.Errorf("profile %q not found in config", name)
		}
		if !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no valid profile names provided")
	}
	return result, nil
}

// readProfilesFromFile reads profile names or instance URLs from a file.
// URLs are resolved to profile names by matching against config.
func readProfilesFromFile(cfg *config.Config, path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening file: %w", err)
	}
	defer func() { _ = f.Close() }()

	seen := make(map[string]bool)
	var names []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		var profileName string
		if _, ok := cfg.Profiles[line]; ok {
			// Exact profile name match — use it directly
			profileName = line
		} else if looksLikeURL(line) {
			// Not a profile name — try resolving as a URL
			resolved, err := resolveURLToProfile(cfg, line)
			if err != nil {
				return nil, err
			}
			profileName = resolved
		} else {
			return nil, fmt.Errorf("profile %q not found in config", line)
		}

		if !seen[profileName] {
			seen[profileName] = true
			names = append(names, profileName)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no profiles found in %s", path)
	}
	return names, nil
}

// looksLikeURL returns true if the string appears to be a URL rather than a profile name.
// Checks for scheme prefix or common Jamf cloud domain suffixes.
func looksLikeURL(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(s, "://") ||
		strings.HasSuffix(lower, ".jamfcloud.com") ||
		strings.HasSuffix(lower, ".jamf.com")
}

// resolveURLToProfile finds the profile name whose URL matches the given URL.
// When multiple profiles share the same URL, prefers pro-<subdomain> profiles
// (created by setup --from-file), then falls back to the first alphabetical match.
func resolveURLToProfile(cfg *config.Config, rawURL string) (string, error) {
	normalized := normalizeURLForMatch(rawURL)
	var matches []string
	for name, p := range cfg.Profiles {
		if normalizeURLForMatch(p.URL) == normalized {
			matches = append(matches, name)
		}
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("no profile found for %s — run 'jamf-cli pro setup' first", rawURL)
	}

	// Prefer pro-<subdomain> profiles (auto-generated by setup --from-file)
	for _, name := range matches {
		if strings.HasPrefix(name, "pro-") {
			return name, nil
		}
	}

	// Fall back to alphabetically first match for determinism
	sort.Strings(matches)
	return matches[0], nil
}

// normalizeURLForMatch normalizes a URL for comparison: adds https, strips trailing slash.
func normalizeURLForMatch(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	rawURL = strings.TrimRight(rawURL, "/")
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}
	return strings.ToLower(rawURL)
}

// promptProfileSelection shows an interactive profile picker.
func promptProfileSelection(cfg *config.Config, allNames []string, noInput bool) ([]string, error) {
	if noInput {
		return nil, fmt.Errorf("no profiles specified — use --filter, --profiles, or --from-file (required with --no-input)")
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return nil, fmt.Errorf("no profiles specified and stdin is not a terminal — use --filter, --profiles, or --from-file")
	}

	fmt.Fprintln(os.Stderr, "\nAvailable profiles:")
	for i, name := range allNames {
		url := cfg.Profiles[name].URL
		fmt.Fprintf(os.Stderr, "  %d. %s (%s)\n", i+1, name, url)
	}
	fmt.Fprint(os.Stderr, "\nSelect profiles (numbers, ranges, 'all', or glob pattern): ")

	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	input := strings.TrimSpace(line)

	if input == "" {
		return nil, fmt.Errorf("no selection made")
	}

	if strings.EqualFold(input, "all") {
		return allNames, nil
	}

	// Check if it's a glob pattern
	if strings.ContainsAny(input, "*?[") {
		return filterProfiles(allNames, input)
	}

	// Parse as space/comma separated tokens, supporting ranges (e.g., "1 3 5-8")
	tokens := tokenizeSelection(input)
	seen := make(map[string]bool)
	var selected []string
	for _, tok := range tokens {
		// Check for range: N-M
		if start, end, ok := parseRange(tok); ok {
			if start < 1 || end > len(allNames) || start > end {
				return nil, fmt.Errorf("range %q is out of bounds (1-%d)", tok, len(allNames))
			}
			for i := start; i <= end; i++ {
				name := allNames[i-1]
				if !seen[name] {
					seen[name] = true
					selected = append(selected, name)
				}
			}
			continue
		}

		// Try as single number
		var idx int
		if _, err := fmt.Sscanf(tok, "%d", &idx); err == nil && idx >= 1 && idx <= len(allNames) {
			name := allNames[idx-1]
			if !seen[name] {
				seen[name] = true
				selected = append(selected, name)
			}
			continue
		}

		// Try as profile name
		if _, ok := cfg.Profiles[tok]; ok {
			if !seen[tok] {
				seen[tok] = true
				selected = append(selected, tok)
			}
			continue
		}

		return nil, fmt.Errorf("%q is not a valid number (1-%d), range, or profile name", tok, len(allNames))
	}

	if len(selected) == 0 {
		return nil, fmt.Errorf("no profiles selected")
	}
	return selected, nil
}

// tokenizeSelection splits user input on spaces and commas.
// "1, 3 5-8" → ["1", "3", "5-8"]
func tokenizeSelection(input string) []string {
	// Replace commas with spaces, then split on whitespace
	input = strings.ReplaceAll(input, ",", " ")
	fields := strings.Fields(input)
	return fields
}

// parseRange parses a "N-M" range string. Returns (start, end, true) on success.
func parseRange(s string) (int, int, bool) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	var start, end int
	if _, err := fmt.Sscanf(parts[0], "%d", &start); err != nil {
		return 0, 0, false
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &end); err != nil {
		return 0, 0, false
	}
	return start, end, true
}

// splitAndTrim splits a string by sep and trims whitespace from each part.
func splitAndTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
