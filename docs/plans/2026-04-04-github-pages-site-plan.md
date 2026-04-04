# GitHub Pages Showcase Site — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deploy an auto-updating single-page showcase site to GitHub Pages that lets visitors explore all 980 CLI commands interactively, powered by a `commands.json` generated from the binary on every merge.

**Architecture:** A vanilla HTML/CSS/JS page (`docs/site/`) reads a `commands.json` produced by a Go transform script (`generator/site/main.go`) that introspects the built binary. A GitHub Action builds, generates, and deploys on every push to `main`. The CLI's `commands` subcommand is enhanced to include `product` and `group` fields.

**Tech Stack:** Go (CLI enhancement + transform script), HTML/CSS/JS (vanilla, no frameworks), GitHub Actions (deploy-pages)

**Design Spec:** `docs/plans/2026-04-04-github-pages-site-design.md`

---

## File Structure

| Action | File | Responsibility |
|--------|------|---------------|
| Modify | `internal/commands/root.go` | Add `Product`/`Group` to `commandEntry`, enhance `collectCommands` |
| Modify | `internal/commands/groups.go` | Add `groupTitle()` helper to resolve group ID → display name |
| Modify | `internal/commands/root_test.go` | Tests for new product/group fields |
| Create | `generator/site/main.go` | Transform script: run binary → enrich → write `commands.json` |
| Create | `generator/site/main_test.go` | Tests for transform logic |
| Create | `docs/site/index.html` | Page HTML structure |
| Create | `docs/site/style.css` | CSS variables, light/dark mode, responsive layout |
| Create | `docs/site/terminal.js` | Terminal simulator (typewriter, command cycling) |
| Create | `docs/site/catalog.js` | Command catalog (fetch, render, search, tabs, expand/collapse) |
| Create | `.github/workflows/deploy-site.yaml` | GitHub Action |
| Modify | `.gitignore` | Add `docs/site/commands.json` and `.superpowers/` |

---

## Task 1: Enhance `commands` subcommand with product and group fields

**Files:**
- Modify: `internal/commands/root.go:527-628`
- Modify: `internal/commands/groups.go` (add `groupTitle` function at end)
- Modify: `internal/commands/root_test.go:44-94` and `root_test.go:96-155`

- [ ] **Step 1: Write failing tests for product and group fields**

Add to `internal/commands/root_test.go` — a new test after the existing `TestCollectCommands`:

```go
func TestCollectCommands_ProductAndGroup(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01")
	entries := collectCommands(root, "", "", "")

	// Pro command should have product "pro"
	for _, e := range entries {
		if e.Command == "pro computers list" {
			if e.Product != "pro" {
				t.Errorf("pro computers list: product = %q, want %q", e.Product, "pro")
			}
			if e.Group != "Computer Management" {
				t.Errorf("pro computers list: group = %q, want %q", e.Group, "Computer Management")
			}
			return
		}
	}
	t.Error("expected 'pro computers list' in entries")
}

func TestCollectCommands_ProtectProductAndGroup(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01")
	entries := collectCommands(root, "", "", "")

	for _, e := range entries {
		if e.Command == "protect analytics list" {
			if e.Product != "protect" {
				t.Errorf("protect analytics list: product = %q, want %q", e.Product, "protect")
			}
			if e.Group != "Security Configuration" {
				t.Errorf("protect analytics list: group = %q, want %q", e.Group, "Security Configuration")
			}
			return
		}
	}
	t.Error("expected 'protect analytics list' in entries")
}

func TestCollectCommands_RootCommandsNoProduct(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01")
	entries := collectCommands(root, "", "", "")

	for _, e := range entries {
		if e.Command == "version" {
			if e.Product != "" {
				t.Errorf("version: product = %q, want empty", e.Product)
			}
			return
		}
	}
	t.Error("expected 'version' in entries")
}
```

Update the existing `TestCommandEntriesToMaps_Full` to include product/group:

```go
func TestCommandEntriesToMaps_Full(t *testing.T) {
	entries := []commandEntry{
		{
			Command:     "pro computers list",
			Description: "List computers",
			Aliases:     []string{"comp"},
			Flags:       []string{"--page", "--sort"},
			Product:     "pro",
			Group:       "Computer Management",
		},
		{
			Command:     "version",
			Description: "Print version",
		},
	}

	maps := commandEntriesToMaps(entries, true)
	if len(maps) != 2 {
		t.Fatalf("expected 2 maps, got %d", len(maps))
	}

	if maps[0]["command"] != "pro computers list" {
		t.Errorf("command = %q, want %q", maps[0]["command"], "pro computers list")
	}
	if maps[0]["aliases"] != "comp" {
		t.Errorf("aliases = %q, want %q", maps[0]["aliases"], "comp")
	}
	if maps[0]["flags"] != "--page, --sort" {
		t.Errorf("flags = %q, want %q", maps[0]["flags"], "--page, --sort")
	}
	if maps[0]["product"] != "pro" {
		t.Errorf("product = %q, want %q", maps[0]["product"], "pro")
	}
	if maps[0]["group"] != "Computer Management" {
		t.Errorf("group = %q, want %q", maps[0]["group"], "Computer Management")
	}

	// Entry without product/group should have empty strings
	if maps[1]["product"] != "" {
		t.Errorf("version product = %q, want empty", maps[1]["product"])
	}
	if maps[1]["group"] != "" {
		t.Errorf("version group = %q, want empty", maps[1]["group"])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v -run "TestCollectCommands_Product|TestCollectCommands_Protect|TestCollectCommands_Root|TestCommandEntriesToMaps_Full" ./internal/commands/...`

Expected: Compilation errors — `collectCommands` doesn't accept the new params, `commandEntry` missing fields.

- [ ] **Step 3: Add `Product` and `Group` to `commandEntry` struct**

In `internal/commands/root.go`, update the struct at line 528:

```go
type commandEntry struct {
	Command     string   `json:"command"`
	Description string   `json:"description"`
	Aliases     []string `json:"aliases,omitempty"`
	Flags       []string `json:"flags,omitempty"`
	Product     string   `json:"product,omitempty"`
	Group       string   `json:"group,omitempty"`
}
```

- [ ] **Step 4: Add `groupTitle` helper to `groups.go`**

Append at the end of `internal/commands/groups.go`:

```go
// groupTitle resolves a cobra group ID to its display title (without trailing colon).
// Used by the commands subcommand to include human-readable group names in JSON output.
func groupTitle(id string) string {
	allGroups := make([]*cobra.Group, 0, len(rootGroups)+len(proGroups)+len(protectGroups))
	allGroups = append(allGroups, rootGroups...)
	allGroups = append(allGroups, proGroups...)
	allGroups = append(allGroups, protectGroups...)

	for _, g := range allGroups {
		if g.ID == id {
			return strings.TrimSuffix(g.Title, ":")
		}
	}
	return ""
}
```

Add `"strings"` to the import block in `groups.go` if not present.

- [ ] **Step 5: Enhance `collectCommands` to thread product and group**

In `internal/commands/root.go`, update the `collectCommands` function signature and body:

```go
func collectCommands(cmd *cobra.Command, prefix, product, group string) []commandEntry {
	var entries []commandEntry
	for _, child := range cmd.Commands() {
		if child.Hidden || child.Name() == "help" || child.Name() == "commands" {
			continue
		}

		fullPath := child.Name()
		if prefix != "" {
			fullPath = prefix + " " + child.Name()
		}

		// Track product from top-level namespace
		childProduct := product
		if child.Name() == "pro" || child.Name() == "protect" {
			childProduct = child.Name()
		}

		// Track group — cobra GroupID is set on direct children of pro/protect
		childGroup := group
		if child.GroupID != "" {
			childGroup = groupTitle(child.GroupID)
		}

		// Leaf command: has RunE or Run
		if child.RunE != nil || child.Run != nil {
			entry := commandEntry{
				Command:     fullPath,
				Description: child.Short,
				Product:     childProduct,
				Group:       childGroup,
			}

			if len(child.Aliases) > 0 {
				entry.Aliases = child.Aliases
			} else if len(cmd.Aliases) > 0 {
				entry.Aliases = cmd.Aliases
			}

			var flags []string
			child.LocalFlags().VisitAll(func(f *pflag.Flag) {
				if !f.Hidden {
					flags = append(flags, "--"+f.Name)
				}
			})
			entry.Flags = flags

			entries = append(entries, entry)
		}

		// Recurse into subcommands
		if child.HasSubCommands() {
			entries = append(entries, collectCommands(child, fullPath, childProduct, childGroup)...)
		}
	}
	return entries
}
```

- [ ] **Step 6: Update the call site in `newCommandsCmd`**

In `internal/commands/root.go`, update the `RunE` in `newCommandsCmd` (around line 542):

```go
entries := collectCommands(root, "", "", "")
```

- [ ] **Step 7: Update `commandEntriesToMaps` to include product and group**

In `internal/commands/root.go`, in the `if full {` block of `commandEntriesToMaps`:

```go
if full {
	aliases := ""
	if len(e.Aliases) > 0 {
		aliases = strings.Join(e.Aliases, ", ")
	}
	flags := ""
	if len(e.Flags) > 0 {
		flags = strings.Join(e.Flags, ", ")
	}
	m["aliases"] = aliases
	m["flags"] = flags
	m["product"] = e.Product
	m["group"] = e.Group
}
```

- [ ] **Step 8: Update existing `TestCollectCommands` call site**

The existing `TestCollectCommands` test at line 44 calls `collectCommands(root, "")`. Update it to:

```go
entries := collectCommands(root, "", "", "")
```

Also update the existing `TestCommandsSubcommand_JSON` test — no changes needed there since it just calls the command via `root.Execute()`.

- [ ] **Step 9: Run all tests**

Run: `go test -v -run "TestCollectCommands|TestCommandEntriesToMaps" ./internal/commands/...`

Expected: All PASS — including the 3 new product/group tests.

- [ ] **Step 10: Run full test suite to verify no regressions**

Run: `make test`

Expected: All PASS.

- [ ] **Step 11: Commit**

```bash
git add internal/commands/root.go internal/commands/groups.go internal/commands/root_test.go
git commit -m "feat: add product and group fields to commands JSON output

The commands subcommand now includes product (pro/protect) and group
(Computer Management, Security Configuration, etc.) in structured
output. Used by the GitHub Pages site generator."
```

---

## Task 2: Create transform script (`generator/site/main.go`)

**Files:**
- Create: `generator/site/main.go`
- Create: `generator/site/main_test.go`

- [ ] **Step 1: Write failing test for the transform logic**

Create `generator/site/main_test.go`:

```go
package main

import (
	"encoding/json"
	"testing"
)

func TestTransformCommands(t *testing.T) {
	raw := `[
		{"command":"pro computers list","description":"Manage computers","aliases":"comp","flags":"--all, --filter","product":"pro","group":"Computer Management"},
		{"command":"protect analytics list","description":"List analytics","aliases":"","flags":"--limit","product":"protect","group":"Security Configuration"},
		{"command":"version","description":"Print version","aliases":"","flags":"","product":"","group":""}
	]`

	result, err := transformCommands([]byte(raw), "1.0.0")
	if err != nil {
		t.Fatalf("transformCommands error: %v", err)
	}

	var site siteData
	if err := json.Unmarshal(result, &site); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if site.Version != "1.0.0" {
		t.Errorf("version = %q, want %q", site.Version, "1.0.0")
	}
	if site.CommandCount != 3 {
		t.Errorf("commandCount = %d, want 3", site.CommandCount)
	}
	if site.GeneratedAt == "" {
		t.Error("generatedAt should not be empty")
	}

	// Check first command was parsed correctly
	if len(site.Commands) != 3 {
		t.Fatalf("expected 3 commands, got %d", len(site.Commands))
	}

	cmd := site.Commands[0]
	if cmd.Command != "pro computers list" {
		t.Errorf("command = %q", cmd.Command)
	}
	if cmd.Product != "pro" {
		t.Errorf("product = %q", cmd.Product)
	}
	if cmd.Group != "Computer Management" {
		t.Errorf("group = %q", cmd.Group)
	}
	if len(cmd.Aliases) != 1 || cmd.Aliases[0] != "comp" {
		t.Errorf("aliases = %v, want [comp]", cmd.Aliases)
	}
	if len(cmd.Flags) != 2 || cmd.Flags[0] != "--all" {
		t.Errorf("flags = %v, want [--all --filter]", cmd.Flags)
	}
}

func TestTransformCommands_EmptyAliasesAndFlags(t *testing.T) {
	raw := `[{"command":"version","description":"Print version","aliases":"","flags":"","product":"","group":""}]`

	result, err := transformCommands([]byte(raw), "0.1.0")
	if err != nil {
		t.Fatalf("transformCommands error: %v", err)
	}

	var site siteData
	if err := json.Unmarshal(result, &site); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	cmd := site.Commands[0]
	if cmd.Aliases != nil {
		t.Errorf("aliases should be nil for empty input, got %v", cmd.Aliases)
	}
	if cmd.Flags != nil {
		t.Errorf("flags should be nil for empty input, got %v", cmd.Flags)
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"jamf-cli 1.0.0\n  commit: abc123\n  built:  2024-01-01\n", "1.0.0"},
		{"jamf-cli 1.0.0-19-g6e19a4c\n  commit: 6e19a4c\n  built:  2026-04-04T00:27:01Z\n", "1.0.0-19-g6e19a4c"},
		{"jamf-cli 0.1.55\n", "0.1.55"},
	}

	for _, tt := range tests {
		got := parseVersion(tt.input)
		if got != tt.want {
			t.Errorf("parseVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./generator/site/...`

Expected: Compilation errors — `transformCommands`, `siteData`, `parseVersion` not defined.

- [ ] **Step 3: Write the transform script**

Create `generator/site/main.go`:

```go
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// siteData is the top-level structure written to commands.json.
type siteData struct {
	GeneratedAt  string        `json:"generatedAt"`
	Version      string        `json:"version"`
	CommandCount int           `json:"commandCount"`
	Commands     []siteCommand `json:"commands"`
}

// siteCommand represents a single command for the site.
type siteCommand struct {
	Command     string   `json:"command"`
	Description string   `json:"description"`
	Aliases     []string `json:"aliases,omitempty"`
	Flags       []string `json:"flags,omitempty"`
	Product     string   `json:"product,omitempty"`
	Group       string   `json:"group,omitempty"`
}

// rawCommand matches the JSON output of `jamf-cli commands -o json`.
type rawCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
	Aliases     string `json:"aliases"`
	Flags       string `json:"flags"`
	Product     string `json:"product"`
	Group       string `json:"group"`
}

func main() {
	binary := flag.String("binary", "./bin/jamf-cli", "Path to the jamf-cli binary")
	outputPath := flag.String("output", "docs/site/commands.json", "Output path for commands.json")
	flag.Parse()

	// Get version
	versionOut, err := exec.Command(*binary, "version").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error running %s version: %v\n", *binary, err)
		os.Exit(1)
	}
	version := parseVersion(string(versionOut))

	// Get commands
	commandsOut, err := exec.Command(*binary, "commands", "-o", "json").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error running %s commands: %v\n", *binary, err)
		os.Exit(1)
	}

	result, err := transformCommands(commandsOut, version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error transforming commands: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(*outputPath, result, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing %s: %v\n", *outputPath, err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "wrote %s (version=%s)\n", *outputPath, version)
}

// transformCommands converts raw CLI JSON output into the site's commands.json format.
func transformCommands(rawJSON []byte, version string) ([]byte, error) {
	var raw []rawCommand
	if err := json.Unmarshal(rawJSON, &raw); err != nil {
		return nil, fmt.Errorf("parsing commands JSON: %w", err)
	}

	commands := make([]siteCommand, len(raw))
	for i, r := range raw {
		commands[i] = siteCommand{
			Command:     r.Command,
			Description: r.Description,
			Aliases:     splitCSV(r.Aliases),
			Flags:       splitCSV(r.Flags),
			Product:     r.Product,
			Group:       r.Group,
		}
	}

	data := siteData{
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		Version:      version,
		CommandCount: len(commands),
		Commands:     commands,
	}

	return json.MarshalIndent(data, "", "  ")
}

// splitCSV splits a comma-separated string into a slice, trimming whitespace.
// Returns nil for empty input.
func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// parseVersion extracts the version string from `jamf-cli version` output.
// Expected format: "jamf-cli 1.0.0\n  commit: ...\n  built: ...\n"
func parseVersion(output string) string {
	line := strings.SplitN(output, "\n", 2)[0]
	parts := strings.SplitN(line, " ", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1])
	}
	return "unknown"
}
```

- [ ] **Step 4: Run tests**

Run: `go test -v ./generator/site/...`

Expected: All PASS.

- [ ] **Step 5: Verify the script works against the real binary**

Run: `go run ./generator/site/main.go --binary ./bin/jamf-cli --output /dev/stdout 2>/dev/null | head -20`

Expected: JSON output with `generatedAt`, `version`, `commandCount`, and enriched commands with `product` and `group` fields.

- [ ] **Step 6: Commit**

```bash
git add generator/site/main.go generator/site/main_test.go
git commit -m "feat: add site generator transform script

Introspects the jamf-cli binary to produce commands.json for the
GitHub Pages showcase site. Splits CSV aliases/flags into arrays,
derives version from binary output, injects build metadata."
```

---

## Task 3: Create page HTML and CSS

**Files:**
- Create: `docs/site/index.html`
- Create: `docs/site/style.css`

- [ ] **Step 1: Create `docs/site/` directory**

Run: `mkdir -p docs/site`

- [ ] **Step 2: Create `docs/site/style.css`**

Create the full CSS file with design tokens, light/dark mode, and responsive layout. All values from the design spec.

CSS custom properties on `:root` for light mode, `@media (prefers-color-scheme: dark)` override for dark mode. Key sections:

- Reset and base styles (Inter font from Google Fonts loaded in HTML)
- CSS variables (all color tokens from design spec)
- Nav bar (sticky, transparent-to-solid on scroll)
- Hero section (always dark)
- Stat cards
- Terminal simulator (always dark)
- Command catalog (light/dark adaptive)
- Search bar
- Product tabs
- Command rows (expandable)
- Flag pills and alias badges
- Install section
- Footer
- Responsive breakpoints (768px)
- `prefers-reduced-motion` overrides

- [ ] **Step 3: Create `docs/site/index.html`**

HTML structure with:
- `<head>`: meta tags, Google Fonts (Inter), link to `style.css`, scripts at bottom
- `<nav>`: logo + version badge, links
- `<section id="hero">`: tagline, subtitle (with `<span id="command-count">`), stat cards, terminal simulator container (`<div id="terminal">`), install one-liner with copy button
- `<section id="commands">`: search bar, product tabs, `<div id="catalog">` (populated by JS)
- `<section id="install">`: three install methods with copy buttons
- `<footer>`: links, auto-generated badge, timestamp (`<span id="last-updated">`)
- Hero terminal sample output data embedded as a `<script type="application/json" id="terminal-data">` block containing the 5 command/output pairs
- Hero expanded command examples embedded as a `<script type="application/json" id="hero-examples">` block containing the 10 curated examples
- `<script src="terminal.js"></script>` and `<script src="catalog.js"></script>` at end of body

- [ ] **Step 4: Verify the page opens in a browser without errors**

Run: `open docs/site/index.html`

Expected: Page renders with the hero section, static content visible. Catalog and terminal will be non-functional until JS is added. No console errors for the static parts.

- [ ] **Step 5: Commit**

```bash
git add docs/site/index.html docs/site/style.css
git commit -m "feat: add GitHub Pages site HTML and CSS shell

Static page structure with Jamf brand colors, auto dark/light mode,
responsive layout, and semantic HTML. Terminal and catalog sections
are containers populated by JS in subsequent commits."
```

---

## Task 4: Terminal simulator JS

**Files:**
- Create: `docs/site/terminal.js`

- [ ] **Step 1: Create `docs/site/terminal.js`**

The terminal simulator reads command/output pairs from the `#terminal-data` JSON block in the HTML. Implements:

- `Typewriter` class: types characters at 40ms intervals, configurable speed
- `TerminalSimulator` class:
  - Reads commands array from `#terminal-data` JSON
  - Cycles through commands in order
  - Types the command, waits, then reveals output lines one by one (200ms per line)
  - Holds completed output for 3 seconds
  - Clears and types the next command
  - Blinking cursor between commands
  - Pauses on `mouseenter` of the terminal element, resumes on `mouseleave`
  - Respects `prefers-reduced-motion`: if set, skips animation — shows first command and output statically, cycles on a 5-second interval without typing effect
- Self-initializes on `DOMContentLoaded`

Key DOM interactions:
- Reads `#terminal-data` script tag for command data
- Writes to `#terminal-output` inside `#terminal`
- Adds/removes `.typing` class for cursor animation

- [ ] **Step 2: Test in browser**

Run: `open docs/site/index.html`

Expected: Terminal in the hero section types commands, shows output, cycles. Hover pauses it.

- [ ] **Step 3: Commit**

```bash
git add docs/site/terminal.js
git commit -m "feat: add terminal simulator with typewriter effect

Auto-cycles through 5 curated commands showing realistic output.
Pauses on hover, respects prefers-reduced-motion."
```

---

## Task 5: Command catalog JS

**Files:**
- Create: `docs/site/catalog.js`

- [ ] **Step 1: Create `docs/site/catalog.js`**

The catalog fetches `commands.json` and renders the interactive command explorer. Implements:

- `fetchCommands()`: fetches `commands.json`, populates stat values (`#command-count`, `#last-updated`, version badge)
- `renderCatalog(commands, filter)`: groups commands by `group` field, renders grouped rows into `#catalog` div
  - Each group has a collapsible header (click to toggle, `aria-expanded`)
  - Each command row shows: command path (monospace, blue), description, flag pills, alias badge
  - Click a row to expand: reveals detail panel with full description, flags list
  - For the 10 hero commands: also shows example + sample output from `#hero-examples` JSON
- `filterCommands(commands, query, product)`: filters by search text (matches command, description, aliases, flags) and product tab
- Search input: `input` event listener calls `filterCommands` + `renderCatalog` on every keystroke
- Product tabs: click handler sets active tab, re-renders with product filter
- Copy-to-clipboard buttons: for install commands and the brew one-liner
- Nav scroll behavior: `IntersectionObserver` on the hero section — when hero leaves viewport, add `.scrolled` class to nav for opaque background
- Populates `#command-count` with `data.commandCount` and `#last-updated` with formatted `data.generatedAt`

Self-initializes on `DOMContentLoaded`.

- [ ] **Step 2: Generate a test `commands.json` for local dev**

Run: `go run ./generator/site/main.go --binary ./bin/jamf-cli --output ./docs/site/commands.json`

- [ ] **Step 3: Test in browser via local server (fetch requires HTTP)**

Run: `cd docs/site && python3 -m http.server 8080`

Then open `http://localhost:8080`. Expected: Full page working — search filters commands, tabs switch products, clicking expands commands, hero terminal types.

- [ ] **Step 4: Commit**

```bash
git add docs/site/catalog.js
git commit -m "feat: add interactive command catalog with search and filtering

Fetches commands.json, renders grouped command list with expand/collapse,
instant search, product tabs, and hero command examples."
```

---

## Task 6: GitHub Action

**Files:**
- Create: `.github/workflows/deploy-site.yaml`

- [ ] **Step 1: Create the workflow file**

Create `.github/workflows/deploy-site.yaml`:

```yaml
name: Deploy Site

on:
  push:
    branches: [main]
  workflow_dispatch:

permissions:
  contents: read
  pages: write
  id-token: write

concurrency:
  group: pages
  cancel-in-progress: false

jobs:
  deploy:
    runs-on: ubuntu-latest
    environment:
      name: github-pages
      url: ${{ steps.deployment.outputs.page_url }}
    steps:
      - uses: actions/checkout@v6

      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
          cache: true

      - name: Build CLI
        run: make build

      - name: Generate commands.json
        run: |
          go run ./generator/site/main.go \
            --binary ./bin/jamf-cli \
            --output ./docs/site/commands.json

      - name: Upload Pages artifact
        uses: actions/upload-pages-artifact@v3
        with:
          path: docs/site

      - name: Deploy to GitHub Pages
        id: deployment
        uses: actions/deploy-pages@v4
```

- [ ] **Step 2: Verify YAML is valid**

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/deploy-site.yaml'))"`

Expected: No errors.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/deploy-site.yaml
git commit -m "ci: add GitHub Pages deploy workflow

Builds the CLI, generates commands.json from binary introspection,
and deploys docs/site/ to GitHub Pages on every push to main."
```

---

## Task 7: Gitignore and integration smoke test

**Files:**
- Modify: `.gitignore`

- [ ] **Step 1: Add entries to `.gitignore`**

Append to `.gitignore`:

```
# Site build artifacts (generated by CI)
docs/site/commands.json

# Brainstorm sessions
.superpowers/
```

- [ ] **Step 2: Remove `commands.json` from git tracking if it was accidentally added**

Run: `git rm --cached docs/site/commands.json 2>/dev/null || true`

- [ ] **Step 3: Run full test suite**

Run: `make test`

Expected: All PASS.

- [ ] **Step 4: Run lint**

Run: `make lint`

Expected: PASS.

- [ ] **Step 5: End-to-end smoke test**

Run the full pipeline locally:

```bash
make build && \
go run ./generator/site/main.go --binary ./bin/jamf-cli --output ./docs/site/commands.json && \
echo "commands.json has $(python3 -c "import json; d=json.load(open('docs/site/commands.json')); print(d['commandCount'])"
) commands"
```

Expected: "commands.json has 980 commands" (or similar count).

Then serve and verify:

```bash
cd docs/site && python3 -m http.server 8080
```

Open `http://localhost:8080` and verify:
- Hero terminal types and cycles commands
- Stats show correct numbers (980 commands)
- Search filters commands
- Product tabs work
- Clicking a command expands it
- Dark mode toggle works (OS-level)
- Mobile responsive (resize browser)

- [ ] **Step 6: Commit**

```bash
git add .gitignore
git commit -m "chore: gitignore site build artifacts and brainstorm sessions"
```

---

## Task Summary

| # | Task | Depends On | Est. |
|---|------|-----------|------|
| 1 | Enhance `commands` subcommand | — | 5 min |
| 2 | Transform script | Task 1 | 5 min |
| 3 | HTML + CSS shell | — | 5 min |
| 4 | Terminal simulator JS | Task 3 | 5 min |
| 5 | Command catalog JS | Task 3 | 5 min |
| 6 | GitHub Action | Task 2 | 3 min |
| 7 | Gitignore + smoke test | All | 3 min |

**Parallelizable:** Tasks 1 and 3 can run in parallel. Tasks 4 and 5 can run in parallel after Task 3. Task 6 can run after Task 2.
