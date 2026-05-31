// Copyright 2026, Jamf Software LLC

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// siteURL is the canonical deployed URL of the showcase site. Used to build
// absolute links inside llms.txt / llms-full.txt so AI agents that fetch
// just one of those files still resolve every link correctly.
const siteURL = "https://jamf-concepts.github.io/jamf-cli"

type siteData struct {
	GeneratedAt  time.Time     `json:"generatedAt"`
	Version      string        `json:"version"`
	CommandCount int           `json:"commandCount"`
	NewCommands  []string      `json:"newCommands,omitempty"`
	Commands     []siteCommand `json:"commands"`
}

type siteCommand struct {
	Command     string   `json:"command"`
	Description string   `json:"description"`
	Aliases     []string `json:"aliases"`
	Flags       []string `json:"flags"`
	Product     string   `json:"product,omitempty"`
	Group       string   `json:"group,omitempty"`
}

type rawCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
	Aliases     string `json:"aliases"`
	Flags       string `json:"flags"`
	Product     string `json:"product"`
	Group       string `json:"group"`
}

func main() {
	binary := flag.String("binary", "./bin/jamf-cli", "path to jamf-cli binary")
	output := flag.String("output", "docs/site/commands.json", "output file path")
	llmsOutput := flag.String("llms-output", "docs/site/llms.txt", "path for llms.txt (high-level AI/LLM index); empty disables")
	llmsFullOutput := flag.String("llms-full-output", "docs/site/llms-full.txt", "path for llms-full.txt (full command catalog as markdown); empty disables")
	previous := flag.String("previous", "", "path to previous commands.json for new-command detection")
	flag.Parse()

	versionOut, err := exec.Command(*binary, "version", "-o", "json").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error running %s version: %v\n", *binary, err)
		os.Exit(1)
	}
	version := parseVersion(string(versionOut))

	commandsOut, err := exec.Command(*binary, "commands", "-o", "json").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error running %s commands: %v\n", *binary, err)
		os.Exit(1)
	}

	var previousCommands map[string]bool
	var prevNewCommands []string
	var prevVersion string
	if *previous != "" {
		previousCommands, prevNewCommands, prevVersion, err = loadPreviousCommands(*previous)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not load previous commands: %v\n", err)
		}
	}

	result, err := transformCommands(commandsOut, version, previousCommands, prevNewCommands, prevVersion)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error transforming commands: %v\n", err)
		os.Exit(1)
	}

	if *output == "/dev/stdout" || *output == "-" {
		_, err = os.Stdout.Write(result)
	} else {
		err = os.WriteFile(*output, result, 0o644)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error writing output: %v\n", err)
		os.Exit(1)
	}

	// Decode the (now structured) JSON we just wrote so we can re-render it
	// as markdown for the AI/LLM-discoverability companion files. Cheaper
	// than re-running the binary, and keeps the count + version in lockstep.
	var data siteData
	if err := json.Unmarshal(result, &data); err != nil {
		fmt.Fprintf(os.Stderr, "error decoding site data for llms.txt: %v\n", err)
		os.Exit(1)
	}

	if *llmsOutput != "" {
		if err := os.WriteFile(*llmsOutput, []byte(renderLLMSTxt(data)), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "error writing %s: %v\n", *llmsOutput, err)
			os.Exit(1)
		}
	}
	if *llmsFullOutput != "" {
		if err := os.WriteFile(*llmsFullOutput, []byte(renderLLMSFullTxt(data)), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "error writing %s: %v\n", *llmsFullOutput, err)
			os.Exit(1)
		}
	}
}

// renderLLMSTxt returns the high-level llms.txt — a markdown document
// following the llmstxt.org convention: H1 + summary blockquote, optional
// detail paragraph, then H2 sections of links. Designed to be the first
// thing an AI agent reads to understand the project and find deeper data.
func renderLLMSTxt(d siteData) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# jamf-cli\n\n")
	fmt.Fprintf(&b, "> Unified, scriptable command-line interface for Jamf Platform API Gateway, Jamf Pro, Jamf Protect, and Jamf School. %s commands. Full API coverage. Zero clicks.\n\n", thousands(d.CommandCount))
	fmt.Fprintf(&b, "jamf-cli is the official CLI for managing Apple devices via Jamf. One binary, three auth methods (OAuth2 client credentials, bearer token, Platform Gateway), full coverage of every Jamf API surface. Auto-generated from the live binary on every deploy — see commands.json for build metadata (version, generation timestamp).\n\n")

	fmt.Fprintf(&b, "## Install\n\n")
	fmt.Fprintf(&b, "- [Homebrew](https://github.com/Jamf-Concepts/jamf-cli#install): `brew install Jamf-Concepts/tap/jamf-cli`\n")
	fmt.Fprintf(&b, "- [Go install](https://github.com/Jamf-Concepts/jamf-cli#install): `go install github.com/Jamf-Concepts/jamf-cli/cmd/jamf-cli@latest`\n")
	fmt.Fprintf(&b, "- [Pre-built binaries](https://github.com/Jamf-Concepts/jamf-cli/releases): macOS, Linux, Windows\n\n")

	fmt.Fprintf(&b, "## Commands\n\n")
	fmt.Fprintf(&b, "- [Full command reference (markdown)](%s/llms-full.txt): every command, description, flags, and aliases — auto-generated from the binary on each release\n", siteURL)
	fmt.Fprintf(&b, "- [Machine-readable command index (JSON)](%s/commands.json): the same data as structured JSON\n", siteURL)
	fmt.Fprintf(&b, "- [Interactive catalog](%s/#commands): browsable in a web browser\n\n", siteURL)

	// Per-product summary with command counts so an AI knows the rough shape
	// of the catalog without opening llms-full.txt.
	fmt.Fprintf(&b, "## Products\n\n")
	counts := productCounts(d.Commands)
	for _, p := range []struct {
		key, label, summary string
	}{
		{"platform", "Jamf Platform", "cross-product Platform API gateway commands (blueprints, compliance benchmarks, declarative device management, platform-wide device groups)"},
		{"pro", "Jamf Pro", "device management for macOS, iOS/iPadOS, tvOS — computers, mobile devices, configuration profiles, scripts, packages, MDM commands, classic and modern APIs"},
		{"protect", "Jamf Protect", "endpoint security — analytics, plans, alerts, exception sets, telemetry, prevent lists, ULF filters"},
		{"school", "Jamf School", "education-focused MDM — devices, classes, locations, blueprints, DDM reports"},
	} {
		fmt.Fprintf(&b, "- **%s** (%d commands): %s\n", p.label, counts[p.key], p.summary)
	}
	fmt.Fprintf(&b, "\n")

	fmt.Fprintf(&b, "## Authentication\n\n")
	fmt.Fprintf(&b, "- **OAuth2 client credentials** (`auth-method: oauth2`) — per-instance API roles + integrations\n")
	fmt.Fprintf(&b, "- **Bearer token** (`auth-method: token`) — pre-existing access token, no refresh\n")
	fmt.Fprintf(&b, "- **Jamf Platform Gateway** (`auth-method: platform`) — single profile enables both Pro API (gateway-routed) and Platform API\n")
	fmt.Fprintf(&b, "- Credentials never accepted via CLI flags or stdin (shell-history safe). Interactive prompts, env vars (`JAMF_*`, `JAMFPROTECT_*`), or keychain-backed config profiles.\n\n")

	fmt.Fprintf(&b, "## Output Formats\n\n")
	fmt.Fprintf(&b, "Every command supports `-o {table,plain,json,yaml,csv}` plus `--field <path>` for jq-free scalar extraction. Pipe-friendly, scriptable, NO_COLOR aware.\n\n")

	fmt.Fprintf(&b, "## Optional\n\n")
	fmt.Fprintf(&b, "- [Source code (Go)](https://github.com/Jamf-Concepts/jamf-cli)\n")
	fmt.Fprintf(&b, "- [Wiki](https://github.com/Jamf-Concepts/jamf-cli/wiki)\n")
	fmt.Fprintf(&b, "- [Releases & changelog](https://github.com/Jamf-Concepts/jamf-cli/releases)\n")
	fmt.Fprintf(&b, "- [Issue tracker](https://github.com/Jamf-Concepts/jamf-cli/issues)\n")
	fmt.Fprintf(&b, "- [Jamf Concepts (org)](https://concepts.jamf.com)\n")

	return b.String()
}

// renderLLMSFullTxt returns the full command catalog as a single markdown
// document, grouped by product → group → command. Stable ordering so diffs
// stay clean release over release.
func renderLLMSFullTxt(d siteData) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# jamf-cli — Full Command Reference\n\n")
	fmt.Fprintf(&b, "> Auto-generated from the live binary. Version %s, %s commands, generated %s.\n\n", d.Version, thousands(d.CommandCount), d.GeneratedAt.Format("2006-01-02 15:04 UTC"))
	fmt.Fprintf(&b, "Project home: %s/\n\n", siteURL)
	fmt.Fprintf(&b, "Each entry below is one CLI command. Format:\n\n")
	fmt.Fprintf(&b, "    ## jamf-cli <command path>\n    <description>\n    Aliases: <comma-separated or \"none\">\n    Flags: <comma-separated or \"none\">\n\n")
	fmt.Fprintf(&b, "---\n\n")

	// Group by product, then by group title, then alphabetical command path.
	byProduct := map[string]map[string][]siteCommand{}
	for _, c := range d.Commands {
		p := c.Product
		if p == "" {
			p = "core"
		}
		g := c.Group
		if g == "" {
			g = "Other"
		}
		if byProduct[p] == nil {
			byProduct[p] = map[string][]siteCommand{}
		}
		byProduct[p][g] = append(byProduct[p][g], c)
	}

	productOrder := []string{"platform", "pro", "protect", "school", "core"}
	productLabels := map[string]string{
		"platform": "Jamf Platform",
		"pro":      "Jamf Pro",
		"protect":  "Jamf Protect",
		"school":   "Jamf School",
		"core":     "Core / Shared",
	}

	for _, p := range productOrder {
		groups, ok := byProduct[p]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "# %s\n\n", productLabels[p])

		gNames := make([]string, 0, len(groups))
		for g := range groups {
			gNames = append(gNames, g)
		}
		sort.Strings(gNames)

		for _, g := range gNames {
			cmds := groups[g]
			sort.Slice(cmds, func(i, j int) bool { return cmds[i].Command < cmds[j].Command })
			fmt.Fprintf(&b, "## %s\n\n", g)
			for _, c := range cmds {
				fmt.Fprintf(&b, "### `jamf-cli %s`\n\n", c.Command)
				if c.Description != "" {
					fmt.Fprintf(&b, "%s\n\n", c.Description)
				}
				if len(c.Aliases) > 0 {
					fmt.Fprintf(&b, "Aliases: %s\n\n", strings.Join(c.Aliases, ", "))
				}
				if len(c.Flags) > 0 {
					fmt.Fprintf(&b, "Flags: %s\n\n", strings.Join(c.Flags, ", "))
				}
			}
		}
	}

	return b.String()
}

// productCounts tallies commands per product. Empty product is reported
// under "core". Platform additionally cross-cuts: any command whose group
// title starts with "Platform" counts toward Platform too, mirroring the
// site catalog's Platform-tab filter (so `pro blueprints …` shows up under
// both Pro and Platform — they're the same physical command, surfaced via
// two semantic lenses).
func productCounts(cmds []siteCommand) map[string]int {
	out := map[string]int{}
	for _, c := range cmds {
		p := c.Product
		if p == "" {
			p = "core"
		}
		out[p]++
		if c.Product != "platform" && strings.HasPrefix(c.Group, "Platform") {
			out["platform"]++
		}
	}
	return out
}

// thousands returns a comma-formatted integer (e.g. 1251 → "1,251").
func thousands(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
		if len(s) > pre {
			b.WriteByte(',')
		}
	}
	for i := pre; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteByte(',')
		}
	}
	return b.String()
}

func transformCommands(rawJSON []byte, version string, previousCommands map[string]bool, prevNewCommands []string, prevVersion string) ([]byte, error) {
	var raw []rawCommand
	if err := json.Unmarshal(rawJSON, &raw); err != nil {
		return nil, fmt.Errorf("parsing commands JSON: %w", err)
	}

	currentCommands := make(map[string]bool, len(raw))
	commands := make([]siteCommand, len(raw))
	var newCommands []string
	for i, r := range raw {
		commands[i] = siteCommand{
			Command:     r.Command,
			Description: r.Description,
			Aliases:     splitCSV(r.Aliases),
			Flags:       splitCSV(r.Flags),
			Product:     r.Product,
			Group:       r.Group,
		}
		currentCommands[r.Command] = true
		if previousCommands != nil && !previousCommands[r.Command] {
			newCommands = append(newCommands, r.Command)
		}
	}

	// If no new commands were detected and the version hasn't changed,
	// carry forward the previous deploy's new-command list (filtered to
	// commands that still exist). This keeps "New" badges visible across
	// non-release deploys but clears them on a new release.
	if len(newCommands) == 0 && len(prevNewCommands) > 0 && prevVersion == version {
		for _, cmd := range prevNewCommands {
			if currentCommands[cmd] {
				newCommands = append(newCommands, cmd)
			}
		}
	}

	data := siteData{
		GeneratedAt:  time.Now().UTC(),
		Version:      version,
		CommandCount: len(commands),
		NewCommands:  newCommands,
		Commands:     commands,
	}

	return json.MarshalIndent(data, "", "  ")
}

func loadPreviousCommands(path string) (map[string]bool, []string, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, "", err
	}
	var prev siteData
	if err := json.Unmarshal(data, &prev); err != nil {
		return nil, nil, "", err
	}
	m := make(map[string]bool, len(prev.Commands))
	for _, c := range prev.Commands {
		m[c.Command] = true
	}
	return m, prev.NewCommands, prev.Version, nil
}

func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func parseVersion(output string) string {
	v := extractRawVersion(output)
	if v == "" {
		return "unknown"
	}
	// Drop the git-describe "-dirty" marker so the site shows the clean
	// release tag. The deploy workflow builds from the latest release tag but
	// overlays site files from main, which dirties the tree — without this a
	// clean tag surfaces as "v1.17.0-dirty" instead of "v1.17.0".
	v = strings.TrimSuffix(v, "-dirty")
	// Strip git-describe suffix (e.g. "v1.2.0-52-gffc0b5a" → "v1.2.0")
	if parts := strings.SplitN(v, "-", 2); len(parts) == 2 && strings.ContainsAny(parts[1], "0123456789") {
		if _, err := strconv.Atoi(strings.Split(parts[1], "-")[0]); err == nil {
			v = parts[0]
		}
	}
	// Normalize: strip "v" prefix so display layer can format consistently
	v = strings.TrimPrefix(v, "v")
	return v
}

// extractRawVersion pulls the raw version string out of `jamf-cli version`
// output. The command emits JSON by default (the global -o default is json),
// so we parse that first; older binaries that predate -o support print a
// "jamf-cli <version>" banner, so we fall back to that. Returns "" when
// neither yields a version, letting the caller map it to "unknown".
func extractRawVersion(output string) string {
	if strings.HasPrefix(strings.TrimSpace(output), "{") {
		var report struct {
			Version string `json:"version"`
		}
		if json.Unmarshal([]byte(output), &report) == nil {
			return report.Version
		}
	}
	line, _, _ := strings.Cut(output, "\n")
	if fields := strings.Fields(line); len(fields) >= 2 {
		return fields[1]
	}
	return ""
}
