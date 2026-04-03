// Copyright 2026, Jamf Software LLC

package commands

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
)

func newMultiCmd() *cobra.Command {
	var (
		filter      string
		profilesCSV string
		fromFile    string
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

Examples:
  # Interactive profile selection (no multi flags, no -- needed)
  jamf-cli multi pro overview
  jamf-cli multi pro comp list

  # Filter by glob pattern (use -- to separate flags)
  jamf-cli multi --filter 'pro-*' -- pro comp list -o table

  # Explicit list
  jamf-cli multi --profiles pro-school1,pro-school2 -- pro overview

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

			var succeeded, failed int
			var failures []string

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

				// Build command: jamf-cli --profile <name> <inner args...>
				cmdArgs := append([]string{"--profile", profileName}, innerArgs...)
				child := exec.Command(executable, cmdArgs...)
				child.Stdout = cmd.OutOrStdout()
				child.Stderr = cmd.ErrOrStderr()

				if err := child.Run(); err != nil {
					_, _ = fmt.Fprintf(w, "  ✗ FAILED: %v\n", err)
					failures = append(failures, fmt.Sprintf("%s: %v", profileName, err))
					failed++
				} else {
					succeeded++
				}
			}

			// Summary
			_, _ = fmt.Fprintf(w, "\n── Summary ──\n")
			_, _ = fmt.Fprintf(w, "  Succeeded: %d\n", succeeded)
			if failed > 0 {
				_, _ = fmt.Fprintf(w, "  Failed:    %d\n", failed)
				for _, f := range failures {
					_, _ = fmt.Fprintf(w, "    - %s\n", f)
				}
				return fmt.Errorf("%d of %d profile(s) failed", failed, len(profiles))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&filter, "filter", "", "glob pattern to match profile names (e.g., 'pro-*')")
	cmd.Flags().StringVar(&profilesCSV, "profiles", "", "comma-separated list of profile names")
	cmd.Flags().StringVar(&fromFile, "from-file", "", "file containing profile names or instance URLs (one per line)")
	cmd.MarkFlagsMutuallyExclusive("filter", "profiles", "from-file")

	return cmd
}

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

// detectProduct inspects the inner command args to determine the target product.
// Returns "pro", "protect", or "" if indeterminate.
func detectProduct(innerArgs []string) string {
	for _, arg := range innerArgs {
		switch arg {
		case "pro":
			return "pro"
		case "protect":
			return "protect"
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
