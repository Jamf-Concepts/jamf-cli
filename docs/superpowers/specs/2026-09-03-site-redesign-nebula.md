# Site Redesign on Nebula v5 — Design Spec

**Date:** 2026-09-03
**Design canvas:** https://claude.ai/code/artifact/f853d400-0007-4541-86fc-9f0fc84205e1 (artboard "Desktop · Nebula v5 tokens" is the target; "Phone · Terminal-first" shows the stacked layout)
**Token source:** `jamf/ds-nebula`, `shared/style-dictionary/properties/v2/` and `global/color/coreV2.json`
**Site:** `docs/site/` (GitHub Pages, static HTML, CSS, and JS; no build step)

## Goal

Rebuild the showcase site on the Nebula v5 token set. Keep every function the site has today. Support light and dark. Default to the visitor's system setting.

## Non-goals

- No framework, no bundler, no Nebula web components. The site stays three static files.
- No change to `commands.json`, `llms.txt`, or the generator.
- No change to the Cmd+K palette logic, deep links, or keyboard shortcuts. They are restyled only.

## Page structure (top to bottom)

1. **Top nav.** 64px. Brand, outlined version pill, links (Commands, Quick start, Install, Wiki, GitHub), a search field that opens the Cmd+K palette, theme toggle.
2. **Hero.** Two columns at 1440px. Left: eyebrow, 64px headline with one italic primary phrase, one paragraph, black primary button "Browse the commands", outlined "Install", the brew command in a dark code block with a Copy button, and a product strip of Nebula themed tags with counts. Right: the animated terminal inside a Nebula example frame (label tab, dark code block, grey action footer).
3. **Facts band.** Three columns: structured output, credentials, automation.
4. **Quick start.** Three Nebula cards (Install, Connect, Run) with dark code blocks, and one inline info alert for CI.
5. **Commands.** Underline tabs per product. Below: side navigation of groups with counts (280px) and a data table of commands (Command, Description, Marks). A row expands into a drawer with flags as gray tags, aliases, privileges, and an example code block with Copy.
6. **Questions.** Two-column FAQ.
7. **Footer.** Grey band with version, generated date, and links.

## Tokens

Fonts: Inter (400, 500, 600, 700, 700 italic) and Noto Sans Mono (400, 500, 600) from Google Fonts.

| Token | Light | Dark |
|---|---|---|
| primary base | `#1268EC` | `#2FA3FF` |
| primary active | `#1F47CD` | `#0093FF` |
| structure base (page) | `#FFFFFF` | `#141920` |
| structure secondary | `#F5F7F8` | `#1F242B` |
| structure tertiary | `#ECEEF1` | `#2B2F36` |
| border base | `rgba(20,25,32,0.12)` | `rgba(255,255,255,0.12)` |
| border secondary | `rgba(20,25,32,0.09)` | `rgba(255,255,255,0.09)` |
| font base | `rgba(0,0,0,0.87)` | `rgba(255,255,255,0.87)` |
| font secondary | `rgba(0,0,0,0.74)` | `rgba(255,255,255,0.74)` |
| font tertiary | `rgba(0,0,0,0.60)` | `rgba(255,255,255,0.60)` |
| code block background | `#141920` | `#0B0E12` |
| danger base | `#CA1E25` | `#ED7577` |
| success base | `#00AB00` | `#32D74B` |
| focus | `#008FFF` | `#008FFF` |

Themed tags (font color on a 16 percent tint):

| Product | Hue | Light font | Dark font | Tint base |
|---|---|---|---|---|
| Jamf Pro | cyan | `#0C5393` | `#87D9F8` | `rgba(85,190,240,0.16)` |
| Jamf Protect | green | `#006F00` | `#9AE89C` | `rgba(40,205,65,0.16)` |
| Jamf School | yellow | `#A05A00` | `#FFF495` | `rgba(255,204,0,0.16)` |
| Security Cloud | red | `#C30001` | `#F59C9D` | `rgba(255,59,48,0.16)` |
| Platform API | blue | `#1F47CD` | `#8CC9FF` | `rgba(0,122,255,0.16)` |
| Core | gray | `rgba(0,0,0,0.87)` | `rgba(255,255,255,0.87)` | `rgba(142,142,147,0.16)` |
| New (highlight) | indigo | `#36219E` on gradient `#BBE2FF → #CCCDF4` | `#C7C2F2` on `rgba(88,86,214,0.28)` | — |

Sizes: radius 8px base, 4px small, 100px pill. Controls 36px. Small controls and tags 24px. Spacing scale 4, 8, 12, 16, 24, 32, 48, 64. Shadows: xs `0 2px 4px -1px`, lg `0 8px 32px -8px`.

Type ramp: h1 32/40 700, h2 28/36 700, h3 24/28 700, h4 20/24 700, h5 16/24 700, body 14/20 400, large 20/24, helper 12/16, code 12 or 13. The hero headline uses a display size of 64/72 700, as the Nebula docs home does.

## Theme behavior

- No stored preference: follow `prefers-color-scheme`. This is the default.
- The toggle cycles system → light → dark → system. The choice is stored in `localStorage` under `theme` as `light` or `dark`. The system state removes the key.
- The head script applies a stored choice before first paint. `meta[name=theme-color]` tracks the rendered page background.
- Every color token is one `light-dark()` declaration. The override is `color-scheme` on `:root[data-theme]`.

## Constraints

- `make verify-site` must pass. Keep these hooks per product: `id="stat-<product>"`, `data-tab="<product>"`, `data-filter="<product>"` in `index.html`; `--product-<product>`, `[data-product="<product>"]`, `[data-filter="<product>"]` in `style.css`; `stat-<product>` and `'<product>'` in `catalog.js`.
- Keep `id="commands"`, `id="install"`, `id="faq"` anchors. External links point at them.
- Keep the `noscript` fallback, JSON-LD, Open Graph, and `link rel=alternate` tags.
- Icons are inline SVG. No emoji in headings or labels.
- Respect `prefers-reduced-motion`.
- Body text stays at or above 14px. Hit targets stay at or above 36px on desktop and 44px on phones.
