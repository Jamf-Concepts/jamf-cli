# GitHub Pages Site — Design Spec

**Date:** 2026-04-04
**Branch:** `gh-pages-site`
**Status:** Design approved, ready for implementation planning

---

## Goal

Deploy a single-page showcase site on GitHub Pages that gives customers an interactive way to explore the CLI's capabilities. The site auto-updates on every merge to `main` — no manual maintenance required beyond curated hero command sample output embedded in the HTML.

## Audience

Dual audience:
- **Existing Jamf admins** who don't know this CLI exists — need the pitch
- **Broader IT community** (MacAdmins, etc.) who may not use Jamf yet — need the "what is this"

## Architecture Decision: Single HTML + Vanilla JS

A single `index.html` with a generated `commands.json` data file. No static site generator, no npm, no build dependencies beyond Go.

**Rationale:**
- Zero maintenance burden — no framework versions to bump
- CI builds in ~90 seconds (Go build + binary introspection + deploy)
- No supply chain risk — no third-party JS dependencies
- The `commands.json` pipeline works with any future frontend if the site outgrows this approach
- This repo is a Go project; adding a JS toolchain for a single page is unnecessary overhead

## Visual Design

### Color Palette

Matches jamf.com's live site aesthetic — white-dominant with a dark hero.

**Constants (both modes):**

| Token | Value | Usage |
|-------|-------|-------|
| `--brand-blue` | `#056AE6` | Primary accent, CTAs, command names, active tabs |
| `--link-blue` | `#0A84FF` | Secondary blue for links |
| `--hero-bg` | `#161617` | Hero section background (always dark) |
| `--terminal-bg` | `#222222` | Terminal blocks (always dark) |
| `--terminal-border` | `#333333` | Terminal block borders |
| `--terminal-text` | `#E2E8F0` | Terminal text |
| `--terminal-success` | `#28C840` | Green values in terminal |
| `--terminal-prompt` | `#66666B` | `$` prompt color |

**Light mode:**

| Token | Value | Usage |
|-------|-------|-------|
| `--page-bg` | `#FFFFFF` | Catalog background |
| `--card-bg` | `#F5F7F8` | Command row background |
| `--border` | `#D8DDE2` | Borders, dividers |
| `--text-primary` | `#222222` | Headings, command descriptions |
| `--text-secondary` | `#66666B` | Secondary text, flag labels |
| `--tab-inactive-bg` | `#F5F7F8` | Inactive product tab |

**Dark mode** (auto via `prefers-color-scheme: dark`):

| Token | Value | Usage |
|-------|-------|-------|
| `--page-bg` | `#161617` | Catalog background |
| `--card-bg` | `#222222` | Command row background |
| `--border` | `#333333` | Borders, dividers |
| `--text-primary` | `#E2E8F0` | Headings, command descriptions |
| `--text-secondary` | `#888888` | Secondary text |
| `--tab-inactive-bg` | `#222222` | Inactive product tab |

Implementation: CSS custom properties on `:root` with `@media (prefers-color-scheme: dark)` override. Zero JS for theming.

### Typography

- **UI chrome:** Inter (loaded from Google Fonts), fallback: -apple-system, system-ui, sans-serif
- **Command names & terminal blocks:** SF Mono / Fira Code / monospace system stack
- No custom font files — Google Fonts CDN for Inter only

## Page Structure

### 1. Nav Bar

- **Content:** "jamf-cli" logo text + version badge (Celtic Blue), nav links: Commands / Install / Wiki / GitHub
- **Behavior:** Sticky on scroll. Transparent over hero, gains `--hero-bg` background when scrolled past hero section.
- **Responsive:** Links collapse to a hamburger on mobile.

### 2. Hero Section (dark, always `#161617`)

- **Tagline:** "Your fleet, one command away"
- **Subtitle:** "Unified CLI for Jamf Pro and Jamf Protect. {commandCount} commands. Full API coverage. Zero clicks."
  - `{commandCount}` is read from `commands.json` at page load
- **Stat cards:** Three cards showing command count, product count (2), and API spec count (196). Subtle blue glow borders. Values read from `commands.json`.
- **Terminal simulator:** Centered, max-width 560px. macOS-style traffic light dots (red/yellow/green). Auto-types 5 curated commands on a loop:

| # | Command | Showcase value |
|---|---------|---------------|
| 1 | `jamf-cli pro overview` | 37 parallel API calls, instant dashboard |
| 2 | `jamf-cli pro report security` | Fleet-wide security posture |
| 3 | `jamf-cli pro device C02X1234` | Single device deep-dive |
| 4 | `jamf-cli protect analytics list -o table` | Protect coverage |
| 5 | `jamf-cli pro comp list --filter "osVersion>=15" -o table` | Filtering power |

  - Typewriter speed: 40ms/character
  - Output appears line-by-line with 200ms delay between lines
  - 3-second hold on completed output before cycling
  - Cursor blinks between commands
  - Pauses on hover so visitors can read
  - Sample output is hardcoded in `index.html` — display copy, not generated data

- **Install one-liner:** `brew install Jamf-Concepts/tap/jamf-cli` in a code block with copy-to-clipboard button.

### 3. Command Catalog (light/dark auto)

The primary interactive section. Powered by `commands.json` loaded at page init.

**Controls:**
- **Search bar:** Instant client-side filter as you type. Matches against command path, description, aliases, and flag names. No submit button — results update live.
- **Product tabs:** "All" / "Jamf Pro" / "Jamf Protect". Active tab: Celtic Blue fill, white text. Inactive: card background, secondary text.

**Command list:**
- Grouped by the CLI's help groups (Core Commands, Power Commands, Computer Management, etc.)
- Group headers are collapsible. All expanded by default.
- Each command row shows:
  - Command path in monospace (Celtic Blue)
  - Description in secondary text
  - Flag pills on the right (subtle blue background)
  - Alias badge if applicable

**Expanded command detail (click to expand):**
- Full description
- All flags with short descriptions (from `--help` output)
- Subcommands listed if it's a parent command
- **For 10 hero commands only:** example command + sample output in a dark terminal block. Sample output is hardcoded in `index.html`.

**Hero commands for expanded examples:**

1. `pro overview`
2. `pro device`
3. `pro report security`
4. `pro computers list`
5. `pro computers apply`
6. `pro setup`
7. `pro comp erase`
8. `pro policy-execute`
9. `protect analytics list`
10. `protect overview`

### 4. Install Section

Three install methods, each with a copy-to-clipboard code block:

1. **Homebrew:** `brew install Jamf-Concepts/tap/jamf-cli`
2. **Binary releases:** Link to GitHub Releases page
3. **From source:** `go install github.com/Jamf-Concepts/jamf-cli/cmd/jamf-cli@latest`

### 5. Footer

- Links: GitHub repo, Wiki, Releases
- "Auto-generated on every merge" badge
- Last updated timestamp from `commands.json` `generatedAt` field
- Version from `commands.json`

## Data Pipeline

### `commands.json` Schema

```json
{
  "generatedAt": "2026-04-04T21:00:00Z",
  "version": "0.1.55",
  "commandCount": 980,
  "commands": [
    {
      "command": "pro computers list",
      "description": "Manage computers",
      "aliases": ["comp"],
      "flags": ["--all", "--filter", "--limit", "--page", "--page-size", "--sort"],
      "product": "pro",
      "group": "Computer Management"
    }
  ]
}
```

**Field derivation:**
- `command`, `description`, `aliases`, `flags` — from `jamf-cli commands -o json`
- `product` — derived from command path prefix (`pro` or `protect`)
- `group` — the transform script runs `jamf-cli pro --help` and `jamf-cli protect --help`, parses the group headings and their child commands, then maps each command to its group. If the existing help output isn't machine-parseable enough, the fallback is adding a `--group` column to the `commands` subcommand's JSON output (small CLI enhancement)
- `generatedAt` — ISO timestamp at build time
- `version` — from `jamf-cli --version`

### Transform Script

A small Go program at `generator/site/main.go`:
1. Reads raw `jamf-cli commands -o json` output
2. Parses group assignments from help output
3. Derives product from command path
4. Injects metadata (version, timestamp, count)
5. Writes `commands.json`

Keeps the toolchain Go-only — no jq, no node.

### `commands.json` is `.gitignored`

It's a build artifact. Never checked in. Generated fresh on every deploy.

## GitHub Action

```yaml
name: Deploy Site
on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest
    permissions:
      pages: write
      id-token: write
    environment:
      name: github-pages
      url: ${{ steps.deployment.outputs.page_url }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: make build
      - name: Generate commands.json
        run: |
          go run ./generator/site/main.go \
            --binary ./bin/jamf-cli \
            --output ./docs/site/commands.json
      - uses: actions/upload-pages-artifact@v3
        with:
          path: docs/site
      - id: deployment
        uses: actions/deploy-pages@v4
```

**Triggers:** Every push to `main` (merges, direct pushes, spec syncs).
**Build time estimate:** ~90 seconds.
**Dependencies:** Go only. No additional toolchains.

## File Layout

```
docs/site/
  index.html           # The single-page site (HTML + CSS + JS, all inline)
  commands.json         # .gitignored — generated at build time

generator/site/
  main.go              # Transform script: binary introspection → commands.json

.github/workflows/
  deploy-site.yaml     # GitHub Action
```

## Responsive Design

- **Desktop (>768px):** Full layout as mocked — stat cards in a row, command rows with flag pills on the right
- **Mobile (<768px):** Stat cards stack vertically, flag pills wrap below description, nav collapses to hamburger, terminal simulator scales to full width

## Accessibility

- Color contrast meets WCAG 2.1 AA (4.5:1 for normal text, 3:1 for large text) per Jamf brand guidelines
- Search input has proper `aria-label`
- Command expansion uses `aria-expanded` and `aria-controls`
- Terminal simulator has `aria-hidden="true"` (decorative) with a text alternative nearby
- Respects `prefers-reduced-motion` — disables typewriter animation, shows static terminal output instead

## What's NOT in Scope

- Individual pages per command (SEO play — separate project if needed)
- Blog, changelog, or multi-page docs
- Server-side rendering or dynamic content
- Authentication or real API calls
- Analytics/tracking (can be added later)
