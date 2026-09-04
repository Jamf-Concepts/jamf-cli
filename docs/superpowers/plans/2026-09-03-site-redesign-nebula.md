# Site Redesign on Nebula v5 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild `docs/site/` on the Nebula v5 token set with light and dark themes that default to the visitor's system setting.

**Architecture:** The site stays three static files: `index.html`, `style.css`, `catalog.js`, plus `terminal.js` and `palette.js`. Task 1 swaps the token layer under the existing CSS, so the site works at every commit. Later tasks replace one page section at a time: nav, hero, quick start, commands, FAQ and footer. The last task removes dead CSS and JS.

**Tech Stack:** Static HTML, CSS custom properties with `light-dark()`, vanilla ES5 JavaScript, Google Fonts (Inter, Noto Sans Mono), Python `http.server` for local preview, `make verify-site` as the CI gate.

**Spec:** `docs/superpowers/specs/2026-09-03-site-redesign-nebula.md`

## Global Constraints

- `make verify-site` must pass after every task. It greps for `id="stat-<product>"`, `data-tab="<product>"`, `data-filter="<product>"` in `index.html`; `--product-<product>`, `[data-product="<product>"]`, `[data-filter="<product>"]` in `style.css`; `stat-<product>` and `'<product>'` in `catalog.js`.
- Keep anchors `#hero`, `#commands`, `#install`, `#faq`.
- Keep the `noscript` block, JSON-LD, Open Graph, and `rel="alternate"` links in `index.html`.
- Fonts: `Inter` 400, 500, 600, 700, 700 italic. `Noto Sans Mono` 400, 500, 600. Fallbacks: `"Helvetica Neue", system-ui, -apple-system, sans-serif` and `ui-monospace, Menlo, monospace`.
- Radius: 8px base, 4px small, 100px pill. Controls: 36px. Tags and small controls: 24px.
- Body text is 14px/20px or larger. No emoji in headings or labels. Icons are inline SVG.
- Every color token is one `light-dark()` declaration in `:root`. Theme override is `color-scheme` on `:root[data-theme="light"]` and `:root[data-theme="dark"]`.
- Preview: `python3 -m http.server 8080 --directory docs/site` after `make build && go run ./generator/site/main.go --binary ./bin/jamf-cli --output ./docs/site/commands.json`. Check every task in light and dark. Force a mode in DevTools with "Emulate CSS prefers-color-scheme".
- Commit after each task with a `site:` prefix.

---

### Task 1: Nebula token layer and fonts

The old token names stay as aliases so every existing rule keeps working. New rules in later tasks use the new names. Task 9 removes the aliases.

**Files:**
- Modify: `docs/site/style.css:345-454` (the `:root` block and the two override rules)
- Modify: `docs/site/index.html:39-41` (font links)
- Modify: `docs/site/index.html:10-11` (`theme-color` meta values)
- Modify: `docs/site/index.html:14-25` (head script colors)
- Modify: `docs/site/style.css:456-473` (body and code font families)

**Interfaces:**
- Produces CSS custom properties used by every later task: `--primary`, `--primary-active`, `--structure-base`, `--structure-secondary`, `--structure-tertiary`, `--border-base`, `--border-secondary`, `--font-base`, `--font-secondary`, `--font-tertiary`, `--code-bg`, `--code-fg`, `--danger`, `--success`, `--focus`, `--font-family`, `--font-mono`, `--r-small`, `--r-base`, `--r-pill`, `--h-action`, `--h-small`, `--shadow-xs`, `--shadow-lg`, and per product `--tag-<product>-fg`, `--tag-<product>-bg`, `--product-<product>`.

- [ ] **Step 1: Replace the font links**

In `docs/site/index.html` replace line 41 with:

```html
  <link href="https://fonts.googleapis.com/css2?family=Inter:ital,wght@0,400;0,500;0,600;0,700;1,700&family=Noto+Sans+Mono:wght@400;500;600&display=swap" rel="stylesheet">
```

- [ ] **Step 2: Update the pre-paint colors**

In `docs/site/index.html` lines 10-11 set `content="#ffffff"` for light and `content="#141920"` for dark. In the head script (line 20) replace the two hex values the same way:

```js
        var c = t === 'dark' ? '#141920' : '#ffffff';
```

- [ ] **Step 3: Replace the `:root` block**

Replace `docs/site/style.css` lines 345-454 (from the `Custom Properties` comment through the two `:root[data-theme]` rules) with:

```css
/* ===== Custom Properties (Nebula v5 tokens) =====
   Source: jamf/ds-nebula shared/style-dictionary/properties/v2 and
   global/color/coreV2.json. Every color is one light-dark() declaration.
   color-scheme on :root[data-theme] is the whole override mechanism. */
:root {
  color-scheme: light dark;

  /* Type */
  --font-family: 'Inter', 'Helvetica Neue', system-ui, -apple-system, sans-serif;
  --font-mono: 'Noto Sans Mono', ui-monospace, Menlo, monospace;

  /* Brand */
  --primary: light-dark(#1268ec, #2fa3ff);
  --primary-active: light-dark(#1f47cd, #0093ff);
  --primary-subdued: light-dark(rgba(18, 104, 236, 0.07), rgba(47, 163, 255, 0.12));
  --focus: #008fff;

  /* Structure */
  --structure-base: light-dark(#ffffff, #141920);
  --structure-secondary: light-dark(#f5f7f8, #1f242b);
  --structure-tertiary: light-dark(#eceef1, #2b2f36);
  --structure-inverse: light-dark(#141920, #ffffff);
  --border-base: light-dark(rgba(20, 25, 32, 0.12), rgba(255, 255, 255, 0.12));
  --border-secondary: light-dark(rgba(20, 25, 32, 0.09), rgba(255, 255, 255, 0.09));

  /* Text */
  --font-base: light-dark(rgba(0, 0, 0, 0.87), rgba(255, 255, 255, 0.87));
  --font-secondary: light-dark(rgba(0, 0, 0, 0.74), rgba(255, 255, 255, 0.74));
  --font-tertiary: light-dark(rgba(0, 0, 0, 0.6), rgba(255, 255, 255, 0.6));
  --font-inverse: light-dark(#ffffff, rgba(0, 0, 0, 0.87));

  /* Code blocks are always dark. The dark theme steps them one shade deeper. */
  --code-bg: light-dark(#141920, #0b0e12);
  --code-fg: #ffffff;
  --code-muted: rgba(255, 255, 255, 0.6);
  --code-string: #ffd24f;
  --code-success: #32d74b;

  /* Semantic */
  --danger: light-dark(#ca1e25, #ed7577);
  --danger-bg: rgba(255, 59, 48, 0.16);
  --success: light-dark(#00ab00, #32d74b);
  --warning: light-dark(#d17800, #fbe504);
  --info-bg: light-dark(rgba(18, 104, 236, 0.05), rgba(47, 163, 255, 0.1));
  --info-border: light-dark(rgba(18, 104, 236, 0.25), rgba(47, 163, 255, 0.35));

  /* Product tags: Nebula themed tags. Font on a 16% tint of the hue.
     Add new products here. */
  --product-pro: light-dark(#0c5393, #87d9f8);
  --product-protect: light-dark(#006f00, #9ae89c);
  --product-school: light-dark(#a05a00, #fff495);
  --product-security: light-dark(#c30001, #f59c9d);
  --product-platform: light-dark(#1f47cd, #8cc9ff);
  --product-core: var(--font-base);
  --tag-pro-fg: var(--product-pro);
  --tag-pro-bg: rgba(85, 190, 240, 0.16);
  --tag-protect-fg: var(--product-protect);
  --tag-protect-bg: rgba(40, 205, 65, 0.16);
  --tag-school-fg: var(--product-school);
  --tag-school-bg: rgba(255, 204, 0, 0.16);
  --tag-security-fg: var(--product-security);
  --tag-security-bg: rgba(255, 59, 48, 0.16);
  --tag-platform-fg: var(--product-platform);
  --tag-platform-bg: rgba(0, 122, 255, 0.16);
  --tag-core-fg: var(--font-base);
  --tag-core-bg: rgba(142, 142, 147, 0.16);
  --tag-new-fg: light-dark(#36219e, #c7c2f2);
  --tag-new-bg: light-dark(linear-gradient(90deg, #bbe2ff, #cccdf4), rgba(88, 86, 214, 0.28));

  /* Terminal syntax (terminal.js). The terminal is always dark, so these
     are fixed dark-tuned values. */
  --terminal-bg: var(--code-bg);
  --terminal-border: rgba(255, 255, 255, 0.12);
  --terminal-text: #ffffff;
  --terminal-prompt: rgba(255, 255, 255, 0.6);
  --terminal-flag: rgba(255, 255, 255, 0.6);
  --terminal-success: #32d74b;
  --terminal-pro: #5ac8f5;
  --terminal-protect: #32d74b;
  --terminal-school: #fff068;
  --terminal-security: #f85454;
  --terminal-platform: #5ab4ff;

  /* Shape and size */
  --r-small: 4px;
  --r-base: 8px;
  --r-pill: 100px;
  --h-action: 36px;
  --h-small: 24px;

  /* Elevation. light-dark() only accepts colors, so the variance lives in
     the shadow color. */
  --shadow-xs: 0 2px 4px -1px light-dark(rgba(0, 0, 0, 0.1), rgba(0, 0, 0, 0.5));
  --shadow-md: 0 4px 10px -2px light-dark(rgba(0, 0, 0, 0.14), rgba(0, 0, 0, 0.55));
  --shadow-lg: 0 8px 32px -8px light-dark(rgba(0, 0, 0, 0.18), rgba(0, 0, 0, 0.65));
  --shadow-sticky: 0 12px 16px -13px light-dark(rgba(0, 0, 0, 0.18), rgba(0, 0, 0, 0.55));

  /* ---- Transitional aliases. Old rules read these. Task 9 deletes them
     together with the rules that read them. ---- */
  --brand-blue: var(--primary);
  --accent: var(--primary);
  --link-blue: var(--primary);
  --page-bg: var(--structure-base);
  --card-bg: var(--structure-secondary);
  --border: var(--border-base);
  --text-primary: var(--font-base);
  --text-secondary: var(--font-secondary);
  --tab-inactive-bg: var(--structure-base);
  --hero-bg: var(--structure-base);
  --new-indicator: light-dark(#d17800, #fbe504);
  --tint-pro: var(--tag-pro-bg);
  --tint-protect: var(--tag-protect-bg);
  --tint-school: var(--tag-school-bg);
  --tint-security: var(--tag-security-bg);
  --tint-platform: var(--tag-platform-bg);
  --accent-tint: var(--primary-subdued);
  --accent-tint-hover: color-mix(in srgb, var(--primary) 14%, var(--structure-base));
  --highlight: color-mix(in srgb, var(--primary) 22%, transparent);
  --pill-bg: var(--tag-core-bg);
  --pill-text: var(--font-tertiary);
  --badge-bg: var(--tag-core-bg);
  --muted-header-text: var(--font-secondary);
  --muted-header-border: var(--border-base);
  --muted-header-bg: var(--structure-secondary);
  --shadow-1: var(--shadow-xs);
  --shadow-2: var(--shadow-md);
  --shadow-3: var(--shadow-lg);
  --hero-glyph: light-dark(rgba(18, 104, 236, 0.18), rgba(47, 163, 255, 0.16));
  --hero-glow: light-dark(rgba(18, 104, 236, 0.05), rgba(47, 163, 255, 0.05));
}

/* Manual theme override. Flipping color-scheme flips every light-dark()
   token above. */
:root[data-theme="light"] { color-scheme: light; }
:root[data-theme="dark"] { color-scheme: dark; }
```

- [ ] **Step 4: Point body and code at the new font tokens**

In `docs/site/style.css` (the `Typography` section that follows) change:

```css
body {
  font-family: var(--font-family);
  color: var(--font-base);
  background: var(--structure-base);
  font-size: 14px;
  line-height: 20px;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
  font-synthesis: none;
}

code,
pre,
.mono {
  font-family: var(--font-mono);
}

a {
  color: var(--primary);
  text-decoration: none;
  text-underline-position: from-font;
  text-decoration-thickness: from-font;
}

a:hover {
  color: var(--primary-active);
  text-decoration: underline;
}
```

Delete the `font-feature-settings: "ss01", "cv11";` line. It was Geist-specific.

Also replace every remaining literal `'Geist Mono', 'SF Mono', 'Fira Code', 'Cascadia Code', monospace` and `'Geist Mono', 'SF Mono', monospace` in `style.css` with `var(--font-mono)`:

```bash
sed -i '' -E "s/'Geist Mono'(, '[A-Za-z ]+')*, monospace/var(--font-mono)/g" docs/site/style.css
```

- [ ] **Step 5: Verify**

Run:

```bash
grep -c "Geist" docs/site/style.css docs/site/index.html
```

Expected: `0` for both files.

Run:

```bash
make verify-site
```

Expected: `All products have full site support.`

Preview the site. Expected: the page renders in Inter, links are `#1268EC` in light and `#2FA3FF` in dark, the page background is white in light and `#141920` in dark. Layout is unchanged.

- [ ] **Step 6: Commit**

```bash
git add docs/site/style.css docs/site/index.html
git commit -m "site: swap token layer to Nebula v5 and load Inter + Noto Sans Mono"
```

---

### Task 2: Three-state theme toggle that defaults to system

**Files:**
- Modify: `docs/site/index.html:580-640` (toggle script)
- Modify: `docs/site/index.html:123-127` (toggle button markup)
- Modify: `docs/site/style.css:2623-2687` (Theme Toggle section)

**Interfaces:**
- Produces: `data-theme` on `<html>` is `"light"`, `"dark"`, or absent (system). `localStorage.theme` is `"light"`, `"dark"`, or removed.

- [ ] **Step 1: Replace the toggle markup**

Replace the `theme-toggle` button in `docs/site/index.html` with a button that carries three icons. The `title` and `aria-label` are updated by script.

```html
    <button class="theme-toggle" id="theme-toggle" type="button" aria-label="Theme: system" title="Theme: system. Click for light.">
      <svg class="theme-icon theme-icon-system" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="2" y="4" width="20" height="14" rx="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="18" x2="12" y2="21"/></svg>
      <svg class="theme-icon theme-icon-sun" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></svg>
      <svg class="theme-icon theme-icon-moon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>
    </button>
```

- [ ] **Step 2: Replace the toggle script**

Replace the IIFE at `docs/site/index.html:581-640` (it starts with `var toggle = document.getElementById('theme-toggle');`) with:

```js
    (function () {
      var toggle = document.getElementById('theme-toggle');
      var ORDER = ['system', 'light', 'dark'];
      var LABEL = { system: 'system', light: 'light', dark: 'dark' };

      function current() {
        var t = document.documentElement.getAttribute('data-theme');
        return t === 'light' || t === 'dark' ? t : 'system';
      }

      function syncThemeColor() {
        var c = getComputedStyle(document.body).backgroundColor;
        document.querySelectorAll('meta[name="theme-color"]').forEach(function (m) {
          m.setAttribute('content', c);
        });
      }

      function describe() {
        var mode = current();
        var next = ORDER[(ORDER.indexOf(mode) + 1) % ORDER.length];
        toggle.setAttribute('data-mode', mode);
        toggle.setAttribute('aria-label', 'Theme: ' + LABEL[mode]);
        toggle.setAttribute('title', 'Theme: ' + LABEL[mode] + '. Click for ' + LABEL[next] + '.');
      }

      function apply(mode) {
        if (mode === 'system') {
          document.documentElement.removeAttribute('data-theme');
          try { localStorage.removeItem('theme'); } catch (e) { /* storage blocked */ }
        } else {
          document.documentElement.setAttribute('data-theme', mode);
          try { localStorage.setItem('theme', mode); } catch (e) { /* storage blocked */ }
        }
        syncThemeColor();
        describe();
      }

      toggle.addEventListener('click', function () {
        var next = ORDER[(ORDER.indexOf(current()) + 1) % ORDER.length];
        var reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
        if (!document.startViewTransition || reduced) { apply(next); return; }

        var r = toggle.getBoundingClientRect();
        var x = r.left + r.width / 2;
        var y = r.top + r.height / 2;
        var radius = Math.hypot(
          Math.max(x, window.innerWidth - x),
          Math.max(y, window.innerHeight - y)
        );
        var vt = document.startViewTransition(function () { apply(next); });
        vt.ready.then(function () {
          document.documentElement.animate(
            { clipPath: ['circle(0px at ' + x + 'px ' + y + 'px)', 'circle(' + radius + 'px at ' + x + 'px ' + y + 'px)'] },
            { duration: 450, easing: 'ease-in-out', pseudoElement: '::view-transition-new(root)' }
          );
        });
      });

      // System mode follows the OS live.
      window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', function () {
        if (current() === 'system') syncThemeColor();
      });

      describe();
      syncThemeColor();
    })();
```

Keep the existing `::view-transition-old(root)` / `::view-transition-new(root)` CSS. It disables the default crossfade.

- [ ] **Step 3: Replace the toggle CSS**

Replace the `Theme Toggle` section in `docs/site/style.css` with:

```css
/* ===== Theme Toggle ===== */
.theme-toggle {
  position: relative;
  width: var(--h-action);
  height: var(--h-action);
  border: 1px solid var(--border-base);
  border-radius: var(--r-base);
  background: var(--structure-base);
  color: var(--font-secondary);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}
.theme-toggle:hover { background: var(--structure-secondary); color: var(--font-base); }
.theme-toggle:focus-visible { outline: none; box-shadow: 0 0 0 2px var(--focus); }

.theme-icon {
  position: absolute;
  width: 18px;
  height: 18px;
  opacity: 0;
  scale: 0.5;
  transition: opacity 0.2s ease, scale 0.2s ease;
}
.theme-toggle[data-mode="system"] .theme-icon-system,
.theme-toggle[data-mode="light"] .theme-icon-sun,
.theme-toggle[data-mode="dark"] .theme-icon-moon {
  opacity: 1;
  scale: 1;
}

@media (prefers-reduced-motion: reduce) {
  .theme-icon { transition: none; scale: 1; }
}
```

- [ ] **Step 4: Verify**

Preview with DevTools open. Clear site data. Expected: `<html>` has no `data-theme` and the page follows the OS setting. Click the toggle: light, then dark, then back to system. Expected: `localStorage.theme` is `light`, then `dark`, then absent. Reload after each click. Expected: the mode persists. In system mode, flip the OS appearance. Expected: the page follows.

Run `make verify-site`. Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add docs/site/index.html docs/site/style.css
git commit -m "site: three-state theme toggle that defaults to the system setting"
```

---

### Task 3: Top navigation

**Files:**
- Modify: `docs/site/index.html:88-130` (nav markup)
- Modify: `docs/site/style.css:487-623` (Nav section)
- Modify: `docs/site/palette.js` (expose an open function if none exists)

**Interfaces:**
- Consumes: `window.openCommandPalette()` from `palette.js`. If `palette.js` does not expose one, add `window.openCommandPalette = openPalette;` next to its existing open function.
- Produces: `.nav-search` button that opens the Cmd+K palette. `#version-badge` keeps its id (catalog.js writes it).

- [ ] **Step 1: Replace the nav markup**

```html
  <nav>
    <a href="#" class="nav-brand">
      <svg class="nav-logo" viewBox="0 0 43 43" fill="none" aria-hidden="true"><path fill-rule="evenodd" clip-rule="evenodd" d="M33.63 6.96c-5.52 0-8.78 2.33-10.27 7.31l-3.84 11.88c-1.41 3.91-3.68 5.52-7.82 5.52H2.19A2.19 2.19 0 000 33.87v6.06c0 1.16.94 2.1 2.1 2.1h38.57a2.19 2.19 0 002.19-2.19V9.08c0-1.17-.95-2.12-2.11-2.12h-7.12z" fill="currentColor"/><path fill-rule="evenodd" clip-rule="evenodd" d="M2.35 0A2.35 2.35 0 000 2.35v16.22a2.29 2.29 0 002.29 2.29h8.17c3.74 0 8.88-.77 10.29-7.41l2.23-10.6A2.35 2.35 0 0020.68 0H2.35z" fill="currentColor"/></svg>
      jamf-cli
    </a>
    <span class="version-badge" id="version-badge"></span>
    <button class="hamburger" aria-label="Toggle navigation" aria-expanded="false" type="button">
      <span></span><span></span><span></span>
    </button>
    <div class="nav-right">
      <button class="nav-search" type="button" aria-label="Search commands (Command K)">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="11" cy="11" r="7"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
        <span class="nav-search-label">Search <span id="nav-command-count">&mdash;</span> commands</span>
        <kbd>&#8984;K</kbd>
      </button>
      <ul class="nav-links">
        <li><a href="#commands">Commands</a></li>
        <li><a href="#quickstart">Quick start</a></li>
        <li><a href="#install">Install</a></li>
        <li><a href="https://github.com/Jamf-Concepts/jamf-cli/wiki" target="_blank" rel="noopener">Wiki</a></li>
        <li class="nav-sep" aria-hidden="true"></li>
        <li><a href="https://github.com/Jamf-Concepts/jamf-cli" target="_blank" rel="noopener">GitHub</a></li>
      </ul>
      <!-- theme toggle from Task 2 stays here -->
    </div>
  </nav>
```

Move the Task 2 toggle button inside `.nav-right` after `.nav-links`.

- [ ] **Step 2: Wire the search button to the palette**

In `docs/site/palette.js`, find the function that opens the palette (it adds the `open` class to `.cmd-palette`). After its definition add:

```js
  window.openCommandPalette = openPalette;
```

Use the real function name. Then in `docs/site/catalog.js` `setupSearch()` add:

```js
    var navSearch = document.querySelector('.nav-search');
    if (navSearch) {
      navSearch.addEventListener('click', function () {
        if (window.openCommandPalette) window.openCommandPalette();
        else { var s = document.getElementById('search'); if (s) s.focus(); }
      });
    }
```

In `populateStats()` add after `setText('command-count', ...)`:

```js
    setText('nav-command-count', count.toLocaleString());
```

- [ ] **Step 3: Replace the Nav CSS section**

```css
/* ===== Nav (Nebula top navigation) ===== */
nav {
  position: sticky;
  top: 0;
  z-index: 100;
  display: flex;
  align-items: center;
  gap: 12px;
  height: 64px;
  padding: 0 32px;
  background: var(--structure-base);
  border-bottom: 1px solid var(--border-base);
}
.nav-brand {
  display: flex;
  align-items: center;
  gap: 10px;
  font-weight: 700;
  font-size: 16px;
  color: var(--font-base);
  letter-spacing: -0.01em;
}
.nav-brand:hover { text-decoration: none; color: var(--font-base); }
.nav-logo { width: 22px; height: 22px; color: var(--primary); }
.version-badge {
  display: inline-flex;
  align-items: center;
  height: 28px;
  padding: 0 10px;
  border: 1px solid var(--border-base);
  border-radius: var(--r-base);
  font-size: 14px;
  color: var(--font-secondary);
}
.version-badge:empty { display: none; }
.nav-right { margin-left: auto; display: flex; align-items: center; gap: 24px; }
.nav-search {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 280px;
  height: var(--h-action);
  padding: 0 12px;
  border: 1px solid var(--border-base);
  border-radius: var(--r-base);
  background: var(--structure-base);
  color: var(--font-tertiary);
  font: inherit;
  cursor: text;
  text-align: left;
}
.nav-search:hover { border-color: var(--font-tertiary); }
.nav-search:focus-visible { outline: none; box-shadow: 0 0 0 2px var(--focus); }
.nav-search svg { width: 14px; height: 14px; flex-shrink: 0; }
.nav-search-label { flex: 1; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.nav-search kbd { font-family: var(--font-mono); font-size: 12px; color: var(--font-tertiary); }
.nav-links { display: flex; align-items: center; gap: 24px; list-style: none; margin: 0; padding: 0; }
.nav-links a { color: var(--font-secondary); font-weight: 500; }
.nav-links a:hover { color: var(--font-base); text-decoration: none; }
.nav-links a.active { color: var(--font-base); font-weight: 700; }
.nav-sep { width: 1px; height: 20px; background: var(--border-base); }
.hamburger { display: none; }
```

Delete the old `nav.scrolled` rule and the `.nav-icon`, `.nav-icon-sm` rules. In `catalog.js` `setupNavScroll()`, remove the code that toggles the `scrolled` class. Keep the active-link highlighting if it exists there.

- [ ] **Step 4: Verify**

Preview. Expected: a 64px sticky bar with hairline bottom border, the version pill outlined, the search field opens the palette on click, the toggle sits at the far right. Check both themes.

Run `make verify-site`. Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add docs/site/index.html docs/site/style.css docs/site/catalog.js docs/site/palette.js
git commit -m "site: Nebula top navigation with palette search field"
```

---

### Task 4: Hero

**Files:**
- Modify: `docs/site/index.html:132-243` (hero section)
- Modify: `docs/site/index.html:702-740` (delete the glyph twinkle script)
- Modify: `docs/site/style.css:1-66` (delete the hero reveal and glyph keyframes, then add the new reveal)
- Modify: `docs/site/style.css:624-1051` (Hero and Hero Examples sections)
- Modify: `docs/site/catalog.js:152-214` (`populateStats`), `docs/site/catalog.js:1224-1235` (`setupStatCards`)

**Interfaces:**
- Consumes: `terminal.js` writes into `#terminal-output`. Keep that id and the `#terminal` container.
- Produces: `.product-strip a[data-tab]` links with `id="stat-<product>"` counters. `setupStatCards()` keeps binding `[data-tab]` clicks to `activateProductTab`.

- [ ] **Step 1: Replace the hero markup**

```html
  <section id="hero">
    <div class="hero-copy">
      <p class="eyebrow">jamf-cli &middot; open source</p>
      <h1>Your fleet, <em>one command</em> away.</h1>
      <p class="subtitle">
        One binary for Jamf Pro, Jamf Protect, Jamf School, Jamf Security Cloud, and the Platform API.
        <span id="command-count">&mdash;</span> commands with structured output, stable exit codes, and no credentials on the command line.
      </p>
      <div class="hero-actions">
        <a href="#commands" class="btn btn-dark">Browse the commands</a>
        <a href="#install" class="btn btn-secondary">Install</a>
      </div>
      <div class="code-block hero-install">
        <span class="code-prompt" aria-hidden="true">$</span>
        <code>brew install Jamf-Concepts/tap/jamf-cli</code>
        <button class="btn btn-primary btn-small copy-btn" aria-label="Copy install command" data-copy="brew install Jamf-Concepts/tap/jamf-cli" type="button">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
          Copy
        </button>
      </div>
      <div class="product-strip" aria-label="Commands per product">
        <a href="#commands" class="tag" data-product="pro" data-tab="pro">Jamf Pro <span class="tag-count" id="stat-pro">&mdash;</span></a>
        <a href="#commands" class="tag" data-product="protect" data-tab="protect">Jamf Protect <span class="tag-count" id="stat-protect">&mdash;</span></a>
        <a href="#commands" class="tag" data-product="school" data-tab="school">Jamf School <span class="tag-count" id="stat-school">&mdash;</span></a>
        <a href="#commands" class="tag" data-product="security" data-tab="security">Security Cloud <span class="tag-count" id="stat-security">&mdash;</span></a>
        <a href="#commands" class="tag" data-product="platform" data-tab="platform" data-tip="Includes Pro commands routed through the Platform Gateway. Counts overlap with Jamf Pro.">Platform API <span class="tag-count" id="stat-platform">&mdash;</span></a>
        <a href="#commands" class="tag" data-product="core" data-tab="all">Core <span class="tag-count" id="stat-core">&mdash;</span></a>
      </div>
    </div>

    <div class="example-frame hero-terminal">
      <div class="example-tabs">
        <span class="example-tab active">Terminal</span>
        <span class="example-tab">Output as JSON</span>
      </div>
      <div id="terminal">
        <div id="terminal-output"></div>
        <div class="terminal-cursor"></div>
      </div>
      <div class="example-footer">
        <button class="btn btn-primary copy-btn" type="button" data-copy="jamf-cli pro overview" aria-label="Copy the last command">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
          Copy command
        </button>
      </div>
    </div>
  </section>
```

Delete the `hero-pattern` SVG block, the `hero-icon` image, the `stat-cards`, and `stat-secondary` blocks. Delete the glyph twinkle IIFE in the scripts block (the one that starts with the `Hero glyph twinkle` comment).

- [ ] **Step 2: Update `populateStats` and `setupStatCards`**

In `populateStats()`, keep every `animateCount` call. Remove `setText('stat-commands', ...)` and the `stat-resources` block. `setText` is null-safe, so the order does not matter, but dead lookups are noise.

`setupStatCards()` today queries `.stat-card[data-tab]`. Change the selector to `[data-tab]` inside `#hero`:

```js
  function setupStatCards() {
    var links = document.querySelectorAll('#hero [data-tab]');
    for (var i = 0; i < links.length; i++) {
      links[i].addEventListener('click', function (e) {
        var tab = this.getAttribute('data-tab');
        activateProductTab(tab);
      });
    }
  }
```

Keep whatever the current function does after `activateProductTab` (scrolling to `#commands`).

- [ ] **Step 3: Add shared component CSS**

Add a new section before `Hero` in `style.css`:

```css
/* ===== Nebula components shared across sections ===== */
.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  height: var(--h-action);
  padding: 0 16px;
  border-radius: var(--r-base);
  border: 1px solid transparent;
  font: inherit;
  font-weight: 600;
  cursor: pointer;
  text-decoration: none;
  white-space: nowrap;
}
.btn:hover { text-decoration: none; }
.btn:focus-visible { outline: none; box-shadow: 0 0 0 2px var(--focus); }
.btn svg { width: 14px; height: 14px; }
.btn-primary { background: var(--primary); color: #ffffff; }
.btn-primary:hover { background: var(--primary-active); color: #ffffff; }
.btn-secondary { background: var(--structure-base); color: var(--font-base); border-color: var(--border-base); }
.btn-secondary:hover { background: var(--structure-secondary); color: var(--font-base); }
.btn-dark { background: var(--structure-inverse); color: var(--font-inverse); }
.btn-dark:hover { opacity: 0.9; color: var(--font-inverse); }
.btn-small { height: var(--h-small); padding: 0 10px; font-size: 12px; }
.btn-small svg { width: 12px; height: 12px; }

.tag {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: var(--h-small);
  padding: 0 8px;
  border-radius: var(--r-small);
  font-size: 14px;
  font-weight: 500;
  background: var(--tag-core-bg);
  color: var(--tag-core-fg);
  text-decoration: none;
  white-space: nowrap;
}
.tag:hover { text-decoration: none; filter: brightness(0.96); }
.tag-count { font-family: var(--font-mono); font-size: 12px; font-weight: 400; }
.tag[data-product="pro"] { background: var(--tag-pro-bg); color: var(--tag-pro-fg); }
.tag[data-product="protect"] { background: var(--tag-protect-bg); color: var(--tag-protect-fg); }
.tag[data-product="school"] { background: var(--tag-school-bg); color: var(--tag-school-fg); }
.tag[data-product="security"] { background: var(--tag-security-bg); color: var(--tag-security-fg); }
.tag[data-product="platform"] { background: var(--tag-platform-bg); color: var(--tag-platform-fg); }
.tag[data-product="core"] { background: var(--tag-core-bg); color: var(--tag-core-fg); }
.tag-danger { background: var(--danger-bg); color: var(--danger); }
.tag-new { background: var(--tag-new-bg); color: var(--tag-new-fg); }
.tag-mono { font-family: var(--font-mono); font-size: 12px; font-weight: 400; }

.code-block {
  display: flex;
  align-items: center;
  gap: 12px;
  min-height: 44px;
  padding: 6px 6px 6px 16px;
  background: var(--code-bg);
  color: var(--code-fg);
  border-radius: var(--r-base);
  font-family: var(--font-mono);
  font-size: 13px;
  line-height: 20px;
}
.code-block code { flex: 1; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.code-prompt { color: var(--code-muted); }
.code-block .copy-btn.copied { background: var(--success); }

.example-frame {
  border: 1px solid var(--border-base);
  border-radius: var(--r-base);
  overflow: hidden;
  background: var(--structure-base);
  box-shadow: var(--shadow-lg);
}
.example-tabs { display: flex; border-bottom: 1px solid var(--border-base); }
.example-tab {
  display: flex;
  align-items: center;
  height: 40px;
  padding: 0 14px;
  color: var(--font-tertiary);
}
.example-tab.active { font-weight: 700; color: var(--font-base); border-right: 1px solid var(--border-base); }
.example-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 16px;
  background: var(--structure-tertiary);
}
```

- [ ] **Step 4: Replace the Hero CSS sections**

Replace `Hero` and `Hero Examples` sections (`style.css:624-1051`) with:

```css
/* ===== Hero ===== */
#hero {
  display: grid;
  grid-template-columns: 560px minmax(0, 1fr);
  gap: 96px;
  align-items: center;
  max-width: 1440px;
  margin: 0 auto;
  padding: 96px 130px 72px;
  box-sizing: border-box;
}
.hero-copy { display: flex; flex-direction: column; gap: 24px; }
.eyebrow {
  margin: 0;
  font-size: 12px;
  line-height: 16px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--font-secondary);
}
#hero h1 {
  margin: 0;
  font-size: 64px;
  line-height: 72px;
  font-weight: 700;
  letter-spacing: -0.025em;
  text-wrap: balance;
}
#hero h1 em { color: var(--primary); font-style: italic; }
#hero .subtitle {
  margin: 0;
  font-size: 20px;
  line-height: 28px;
  color: var(--font-secondary);
  text-wrap: pretty;
}
.hero-actions { display: flex; gap: 12px; }
.hero-install { width: 460px; max-width: 100%; }
.product-strip { display: flex; flex-wrap: wrap; gap: 8px; padding-top: 8px; }

#terminal {
  background: var(--terminal-bg);
  color: var(--terminal-text);
  padding: 20px 22px;
  font-family: var(--font-mono);
  font-size: 13px;
  line-height: 20px;
  min-height: 520px;
  text-align: left;
}
#terminal-output { white-space: pre; overflow: hidden; }

/* First-load reveal. Skipped under prefers-reduced-motion. */
@keyframes reveal {
  from { opacity: 0; transform: translateY(10px); }
  to   { opacity: 1; transform: translateY(0); }
}
.hero-copy > *,
.hero-terminal { animation: reveal 0.6s cubic-bezier(0.16, 1, 0.3, 1) backwards; }
.hero-copy > :nth-child(1) { animation-delay: 0.05s; }
.hero-copy > :nth-child(2) { animation-delay: 0.15s; }
.hero-copy > :nth-child(3) { animation-delay: 0.25s; }
.hero-copy > :nth-child(4) { animation-delay: 0.35s; }
.hero-copy > :nth-child(5) { animation-delay: 0.45s; }
.hero-copy > :nth-child(6) { animation-delay: 0.55s; }
.hero-terminal { animation-delay: 0.4s; }
@media (prefers-reduced-motion: reduce) {
  .hero-copy > *, .hero-terminal { animation: none; }
}
```

Delete `style.css:1-66` (old reveal and glyph keyframes). Keep the `.terminal-cursor` rule and the terminal color classes that `terminal.js` writes (`grep -n "className\|class=" docs/site/terminal.js` lists them). Delete `.terminal-titlebar`, `.terminal-dot`, `.stat-card*`, `.stat-secondary`, `.stat-tip`, `.hero-icon`, `.hero-pattern`, `.hero-glyph` rules.

- [ ] **Step 5: Verify**

Preview. Expected: two columns, headline with italic primary phrase, black and outlined buttons, install block with a blue Copy button, six product tags with live counts, terminal animating inside the example frame. Click a product tag. Expected: the commands section filters to that product. Check both themes.

Run:

```bash
make verify-site
grep -c "hero-glyph" docs/site/index.html docs/site/style.css
```

Expected: pass, then `0` and `0`.

- [ ] **Step 6: Commit**

```bash
git add docs/site/index.html docs/site/style.css docs/site/catalog.js
git commit -m "site: two-column hero with product tags and terminal example frame"
```

---

### Task 5: Facts band and quick start

**Files:**
- Modify: `docs/site/index.html:284-325` (replace the Install section)
- Modify: `docs/site/style.css:2054-2215` (Install Section)

**Interfaces:**
- Produces: `id="quickstart"` and `id="install"` anchors. `.copy-btn[data-copy]` buttons keep working through `setupCopyButtons()`.

- [ ] **Step 1: Replace the Install section markup**

Insert the facts band directly after `</section>` of `#hero`, before `#commands`:

```html
  <section id="facts" aria-label="What you get">
    <div class="fact">
      <h2 class="h5">Structured output</h2>
      <p>Every command prints <code>json</code>, <code>yaml</code>, <code>csv</code>, <code>table</code>, <code>plain</code>, or a single field.</p>
    </div>
    <div class="fact">
      <h2 class="h5">Credentials stay off the command line</h2>
      <p>OAuth2 client credentials, bearer token, or Platform Gateway. Secrets live in the keychain, env vars, or files. Never in argv.</p>
    </div>
    <div class="fact">
      <h2 class="h5">Built for scripts and agents</h2>
      <p>Stable exit codes, <code>--no-input</code>, <code>--quiet</code>, an <a href="llms.txt">llms.txt</a>, and a <a href="commands.json">JSON command index</a>.</p>
    </div>
  </section>
```

Replace the whole `#install` section with:

```html
  <section id="quickstart">
    <div class="section-head">
      <h2>Up and running in three commands</h2>
      <p id="install">Homebrew, <code>go install</code>, or a <a href="https://github.com/Jamf-Concepts/jamf-cli/releases" target="_blank" rel="noopener">binary from GitHub Releases</a> for macOS, Linux, and Windows.</p>
    </div>
    <div class="steps">
      <div class="card step">
        <div class="step-head"><span class="step-num">1</span><h3>Install</h3></div>
        <div class="code-block">
          <code>brew install Jamf-Concepts/tap/jamf-cli</code>
          <button class="btn btn-primary btn-small copy-btn" type="button" data-copy="brew install Jamf-Concepts/tap/jamf-cli" aria-label="Copy">Copy</button>
        </div>
        <p>One binary. Shell completion for bash, zsh, fish, and PowerShell.</p>
        <details class="alt-install">
          <summary>Other install options</summary>
          <div class="code-block"><code>go install github.com/Jamf-Concepts/jamf-cli/cmd/jamf-cli@latest</code><button class="btn btn-primary btn-small copy-btn" type="button" data-copy="go install github.com/Jamf-Concepts/jamf-cli/cmd/jamf-cli@latest" aria-label="Copy">Copy</button></div>
        </details>
      </div>
      <div class="card step">
        <div class="step-head"><span class="step-num">2</span><h3>Connect</h3></div>
        <div class="code-block">
          <code>jamf-cli <span class="tok-pro">pro</span> setup <span class="tok-flag">--url</span> https://jamf.company.com</code>
          <button class="btn btn-primary btn-small copy-btn" type="button" data-copy="jamf-cli pro setup --url https://jamf.company.com" aria-label="Copy">Copy</button>
        </div>
        <p>Prompts for credentials and saves a profile to the keychain. Nothing lands in shell history.</p>
      </div>
      <div class="card step">
        <div class="step-head"><span class="step-num">3</span><h3>Run</h3></div>
        <div class="code-block">
          <code>jamf-cli <span class="tok-pro">pro</span> overview <span class="tok-flag">-o json</span></code>
          <button class="btn btn-primary btn-small copy-btn" type="button" data-copy="jamf-cli pro overview -o json" aria-label="Copy">Copy</button>
        </div>
        <p>Add <code>-p staging</code> to target another profile. Pipe the JSON anywhere.</p>
      </div>
    </div>
    <div class="alert alert-info" role="note">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>
      <p><strong>Running in CI?</strong> Set <code>JAMF_URL</code>, <code>JAMF_CLIENT_ID</code>, and <code>JAMF_CLIENT_SECRET</code> and skip setup. Add <code>JAMF_CLI_ARGS='--quiet --no-input'</code> for unattended runs.</p>
    </div>
  </section>
```

Place `#quickstart` after `#facts` and before `#commands`. The old Install section sat after Commands. Update the JSON-LD `HowTo` block, if one references install steps, so its text matches.

- [ ] **Step 2: Replace the Install CSS section**

```css
/* ===== Facts band ===== */
#facts {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 48px;
  max-width: 1440px;
  margin: 0 auto;
  padding: 40px 130px;
  box-sizing: border-box;
  border-top: 1px solid var(--border-base);
  border-bottom: 1px solid var(--border-base);
}
.fact { display: flex; flex-direction: column; gap: 4px; }
.h5, .fact h2 { margin: 0; font-size: 16px; line-height: 24px; font-weight: 700; }
.fact p { margin: 0; color: var(--font-secondary); text-wrap: pretty; }
.fact code {
  font-size: 12px;
  padding: 1px 4px;
  border-radius: var(--r-small);
  background: var(--structure-secondary);
}

/* ===== Quick start ===== */
#quickstart {
  display: flex;
  flex-direction: column;
  gap: 32px;
  max-width: 1440px;
  margin: 0 auto;
  padding: 80px 130px 64px;
  box-sizing: border-box;
}
.section-head { display: flex; flex-direction: column; gap: 8px; }
.section-head h2 { margin: 0; font-size: 28px; line-height: 36px; font-weight: 700; }
.section-head p { margin: 0; color: var(--font-secondary); }
.steps { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 24px; }
.card {
  background: var(--structure-base);
  border: 1px solid var(--border-base);
  border-radius: var(--r-base);
  box-shadow: var(--shadow-xs);
}
.step { display: flex; flex-direction: column; gap: 16px; padding: 24px; }
.step-head { display: flex; align-items: center; gap: 12px; }
.step-head h3 { margin: 0; font-size: 20px; line-height: 24px; font-weight: 700; }
.step-num {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border-radius: var(--r-pill);
  background: var(--primary);
  color: #ffffff;
  font-size: 12px;
  font-weight: 700;
}
.step p { margin: 0; color: var(--font-secondary); }
.step .code-block { font-size: 12px; line-height: 16px; min-height: 40px; }
.tok-pro { color: var(--terminal-pro); }
.tok-flag { color: var(--code-muted); }
.alt-install summary { cursor: pointer; color: var(--primary); font-weight: 500; }
.alt-install .code-block { margin-top: 12px; }
.alert {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 12px 16px;
  border-radius: var(--r-base);
}
.alert svg { width: 18px; height: 18px; flex-shrink: 0; margin-top: 1px; }
.alert p { margin: 0; }
.alert-info { background: var(--info-bg); border: 1px solid var(--info-border); color: var(--font-base); }
.alert-info svg { color: var(--primary); }
```

- [ ] **Step 3: Verify**

Preview. Expected: a three-column facts band under the hero, three cards with dark code blocks and Copy buttons, one info alert. `#install` still scrolls to the quick start. Every Copy button copies. Check both themes.

Run:

```bash
make verify-site
grep -c "🍺\|🛠️\|📦" docs/site/index.html
```

Expected: pass, then `0`.

- [ ] **Step 4: Commit**

```bash
git add docs/site/index.html docs/site/style.css
git commit -m "site: facts band and three-step quick start replace the install section"
```

---

### Task 6: Commands section as side navigation plus data table

This is the largest task. The data flow in `catalog.js` stays: `fetchCommands` → `renderCatalog(commands, query, product)`. What changes is what `renderCatalog` draws. Groups become sidebar items. The selected group's commands become table rows. Search still filters across every group. A search result shows every matching group as its own table.

**Files:**
- Modify: `docs/site/index.html:246-281` (commands section markup)
- Modify: `docs/site/style.css:1052-2053` (Commands Catalog section)
- Modify: `docs/site/catalog.js:288-396` (`renderCatalog`, `renderGroupsWithPillars`, `renderPillarDivider`), `:421-436` (`renderProductDivider`), `:561-806` (`renderGroup` through `toggleGroup`), `:807-991` (`renderCommandRow`), `:1177-1200` (`activateProductTab`, `setupTabs`)

**Interfaces:**
- Consumes: `filterCommands`, `groupCommands`, `sortGroups`, `pillarFor`, `PRODUCT_LABELS`, `newCommandSet`, `isDestructiveCommand`, `detectRelatedProduct`, `buildDetailContent`, `copyWithFeedback`, `highlightText`, `navigateToCommand`. All exist today.
- Produces: module state `selectedGroup` (string or null). `renderCatalog(commands, query, product)` keeps its signature. `activateProductTab(filter)` keeps its signature. Row elements keep class `command-row` and attribute `data-command`, which deep linking and keyboard navigation read.

- [ ] **Step 1: Replace the commands markup**

```html
  <section id="commands">
    <div class="section-head">
      <h2>Commands</h2>
      <p><span id="commands-total">&mdash;</span> commands, regenerated from the binary on every merge to main.</p>
    </div>
    <div class="tabs" role="tablist" aria-label="Filter by product">
      <button class="tab active" role="tab" data-filter="all" type="button">All</button>
      <button class="tab" role="tab" data-filter="platform" type="button">Platform API</button>
      <button class="tab" role="tab" data-filter="pro" type="button">Jamf Pro</button>
      <button class="tab" role="tab" data-filter="protect" type="button">Jamf Protect</button>
      <button class="tab" role="tab" data-filter="school" type="button">Jamf School</button>
      <button class="tab" role="tab" data-filter="security" type="button">Security Cloud</button>
    </div>
    <div class="catalog-layout">
      <nav class="group-nav" id="group-nav" aria-label="Command groups"></nav>
      <div class="catalog-main">
        <div class="catalog-controls">
          <div class="search-wrap">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="11" cy="11" r="7"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
            <input id="search" type="text" placeholder="Search commands (press / to focus)" aria-label="Search commands">
            <button id="search-clear" class="search-clear" aria-label="Clear search" type="button">&times;</button>
          </div>
          <button id="toggle-all" class="btn btn-secondary" type="button" title="Expand or collapse every row">Expand all</button>
          <button class="btn btn-secondary kbd-hint-btn" id="kbd-hint-btn" type="button" title="Keyboard shortcuts (?)" aria-label="Keyboard shortcuts"><kbd>?</kbd></button>
        </div>
        <div id="catalog-loading">Loading commands...</div>
        <!-- keep the existing <noscript> block here unchanged -->
        <div id="catalog"></div>
      </div>
    </div>
  </section>
```

Add `setText('commands-total', count.toLocaleString());` to `populateStats()`.

- [ ] **Step 2: Add sidebar state and rendering to `catalog.js`**

Near the other module-level `var` declarations (after `var PRODUCT_LABELS`), add:

```js
  // The group shown in the table. null means "first group of the current
  // product". A search shows every matching group and ignores this.
  var selectedGroup = null;
```

Add these functions after `renderProductDivider`:

```js
  // Sidebar: one heading per product, one item per group with a count.
  // A product not in the active filter collapses to its heading.
  function renderGroupNav(productGroups, sharedGroups, activeProduct) {
    var nav = document.getElementById('group-nav');
    if (!nav) return;
    nav.innerHTML = '';

    var order = Object.keys(PRODUCT_LABELS).concat(['core']);
    for (var i = 0; i < order.length; i++) {
      var prod = order[i];
      var groups = prod === 'core' ? sharedGroups : (productGroups[prod] || []);
      if (groups.length === 0) continue;
      var total = 0;
      for (var g = 0; g < groups.length; g++) total += groups[g].commands.length;

      var head = document.createElement('div');
      head.className = 'group-nav-product';
      head.setAttribute('data-product', prod);
      var dot = document.createElement('span');
      dot.className = 'group-nav-dot';
      head.appendChild(dot);
      head.appendChild(document.createTextNode(prod === 'core' ? 'Core' : PRODUCT_LABELS[prod]));
      var totalEl = document.createElement('span');
      totalEl.className = 'group-nav-count';
      totalEl.textContent = total.toLocaleString();
      head.appendChild(totalEl);
      nav.appendChild(head);

      var expanded = activeProduct === 'all' || activeProduct === prod || (prod === 'core' && activeProduct === 'all');
      if (!expanded) continue;

      var lastPillar = null;
      for (var k = 0; k < groups.length; k++) {
        var pillar = prod === 'pro' ? pillarFor(groups[k].name) : null;
        if (pillar && pillar !== lastPillar) {
          var pl = document.createElement('div');
          pl.className = 'group-nav-pillar';
          pl.textContent = pillar;
          nav.appendChild(pl);
          lastPillar = pillar;
        }
        nav.appendChild(renderGroupNavItem(groups[k], prod));
      }
    }
  }

  function renderGroupNavItem(group, prod) {
    var item = document.createElement('button');
    item.type = 'button';
    item.className = 'group-nav-item';
    item.setAttribute('data-group', group.name);
    item.setAttribute('data-product', prod);
    if (group.name === selectedGroup) item.classList.add('active');
    var label = document.createElement('span');
    label.className = 'group-nav-label';
    label.textContent = group.name;
    item.appendChild(label);
    var count = document.createElement('span');
    count.className = 'group-nav-count';
    count.textContent = group.commands.length;
    item.appendChild(count);
    item.addEventListener('click', function () {
      selectedGroup = group.name;
      var search = document.getElementById('search');
      if (search && search.value) { search.value = ''; }
      renderCatalog(allCommands, '', currentProductFilter);
      var main = document.querySelector('.catalog-main');
      if (main) main.scrollIntoView({ behavior: 'smooth', block: 'start' });
    });
    return item;
  }
```

`allCommands` and `currentProductFilter` are the module variables `fetchCommands` and `setupTabs` already maintain. Confirm their names with `grep -n "var allCommands\|currentProductFilter" docs/site/catalog.js` and use the real names.

- [ ] **Step 3: Rewrite `renderCatalog`**

Replace `renderCatalog` (`catalog.js:288-378`) with:

```js
  function renderCatalog(commands, searchQuery, productFilter) {
    var catalog = document.getElementById('catalog');
    if (!catalog) return;

    currentSearchQuery = (searchQuery || '').trim().toLowerCase();
    expandedCommand = null;
    var filtered = filterCommands(commands, searchQuery, productFilter);
    var groups = groupCommands(filtered);
    var sorted = sortGroups(groups);
    var hasSearch = !!currentSearchQuery;

    // Split groups by product. A mixed group splits into one per product.
    var sharedGroups = [];
    var productGroups = {};
    for (var i = 0; i < sorted.length; i++) {
      var byProduct = {};
      var noProd = [];
      for (var j = 0; j < sorted[i].commands.length; j++) {
        var c = sorted[i].commands[j];
        if (c.product) { (byProduct[c.product] = byProduct[c.product] || []).push(c); }
        else noProd.push(c);
      }
      if (noProd.length) sharedGroups.push({ name: sorted[i].name, commands: noProd });
      var keys = Object.keys(byProduct);
      for (var k = 0; k < keys.length; k++) {
        (productGroups[keys[k]] = productGroups[keys[k]] || []).push({ name: sorted[i].name, commands: byProduct[keys[k]] });
      }
    }

    // Sidebar: full groups for the whole product filter, never narrowed by search.
    var navFiltered = filterCommands(commands, '', productFilter);
    var navSorted = sortGroups(groupCommands(navFiltered));
    var navShared = [], navProducts = {};
    for (var n = 0; n < navSorted.length; n++) {
      var byP = {}, np = [];
      for (var m = 0; m < navSorted[n].commands.length; m++) {
        var cc = navSorted[n].commands[m];
        if (cc.product) { (byP[cc.product] = byP[cc.product] || []).push(cc); } else np.push(cc);
      }
      if (np.length) navShared.push({ name: navSorted[n].name, commands: np });
      Object.keys(byP).forEach(function (p) {
        (navProducts[p] = navProducts[p] || []).push({ name: navSorted[n].name, commands: byP[p] });
      });
    }

    // Pick the table content.
    var tables = [];
    if (hasSearch) {
      var order = Object.keys(PRODUCT_LABELS);
      for (var o = 0; o < order.length; o++) {
        (productGroups[order[o]] || []).forEach(function (g) { tables.push({ group: g, product: order[o] }); });
      }
      sharedGroups.forEach(function (g) { tables.push({ group: g, product: 'core' }); });
    } else {
      var all = [];
      var ord = Object.keys(PRODUCT_LABELS);
      for (var q = 0; q < ord.length; q++) {
        (navProducts[ord[q]] || []).forEach(function (g) { all.push({ group: g, product: ord[q] }); });
      }
      navShared.forEach(function (g) { all.push({ group: g, product: 'core' }); });
      var pick = null;
      for (var a = 0; a < all.length; a++) { if (all[a].group.name === selectedGroup) { pick = all[a]; break; } }
      if (!pick && all.length) { pick = all[0]; selectedGroup = pick.group.name; }
      if (pick) tables.push(pick);
    }

    renderGroupNav(navProducts, navShared, productFilter);

    catalog.innerHTML = '';
    if (tables.length === 0) {
      var empty = document.createElement('p');
      empty.className = 'catalog-empty';
      empty.textContent = 'No commands match your search.';
      catalog.appendChild(empty);
      return;
    }
    for (var t = 0; t < tables.length; t++) {
      catalog.appendChild(renderGroupTable(tables[t].group, tables[t].product, hasSearch));
    }
  }
```

Delete `renderGroupsWithPillars`, `renderPillarDivider`, `renderProductDivider`, `renderGroup`, `renderGroupCommands`, `bucketByResource`, `shouldBucket`, `appendLeafCommands`, `renderResourceBucket`, `toggleResource`, and `toggleGroup`. Run `grep -n "renderGroup\|toggleGroup\|renderResourceBucket" docs/site/catalog.js` afterwards. Expected: no callers remain except `renderGroupTable`.

- [ ] **Step 4: Add `renderGroupTable` and rewrite `renderCommandRow`**

```js
  function renderGroupTable(group, product, showProduct) {
    var wrap = document.createElement('section');
    wrap.className = 'group-table';
    wrap.setAttribute('data-product', product);
    wrap.setAttribute('data-group', group.name);

    var head = document.createElement('div');
    head.className = 'group-table-head';
    var title = document.createElement('h3');
    title.textContent = group.name;
    head.appendChild(title);
    var meta = document.createElement('span');
    meta.className = 'group-table-meta';
    meta.textContent = group.commands.length + (group.commands.length === 1 ? ' command' : ' commands');
    head.appendChild(meta);
    if (showProduct) {
      var ptag = document.createElement('span');
      ptag.className = 'tag';
      ptag.setAttribute('data-product', product);
      ptag.textContent = product === 'core' ? 'Core' : PRODUCT_LABELS[product];
      head.appendChild(ptag);
    }
    wrap.appendChild(head);

    var table = document.createElement('div');
    table.className = 'cmd-table';
    table.setAttribute('role', 'table');
    var hdr = document.createElement('div');
    hdr.className = 'cmd-table-header';
    hdr.setAttribute('role', 'row');
    ['Command', 'Description', 'Marks'].forEach(function (h) {
      var cell = document.createElement('span');
      cell.setAttribute('role', 'columnheader');
      cell.textContent = h;
      hdr.appendChild(cell);
    });
    table.appendChild(hdr);
    for (var i = 0; i < group.commands.length; i++) {
      table.appendChild(renderCommandRow(group.commands[i]));
    }
    wrap.appendChild(table);
    return wrap;
  }

  function renderCommandRow(cmd) {
    var row = document.createElement('div');
    row.className = 'command-row';
    row.setAttribute('role', 'row');
    row.setAttribute('tabindex', '0');
    row.setAttribute('data-command', cmd.command);
    if (cmd.product) row.setAttribute('data-product', cmd.product);

    var nameCell = document.createElement('span');
    nameCell.className = 'command-name';
    nameCell.setAttribute('role', 'cell');
    var parts = cmd.command.split(' ');
    if (parts.length >= 2) {
      var prodSpan = document.createElement('span');
      prodSpan.className = 'cmd-product';
      highlightText(parts[0] + ' ', prodSpan);
      nameCell.appendChild(prodSpan);
      if (parts.length > 2) {
        var mid = document.createElement('span');
        mid.className = 'cmd-prefix';
        highlightText(parts.slice(1, -1).join(' ') + ' ', mid);
        nameCell.appendChild(mid);
      }
      var action = document.createElement('span');
      action.className = 'cmd-action';
      highlightText(parts[parts.length - 1], action);
      nameCell.appendChild(action);
    } else {
      highlightText(cmd.command, nameCell);
    }
    var copyIcon = document.createElement('button');
    copyIcon.type = 'button';
    copyIcon.className = 'command-copy-icon';
    copyIcon.setAttribute('aria-label', 'Copy jamf-cli ' + cmd.command);
    copyIcon.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>';
    copyIcon.addEventListener('click', function (e) {
      e.stopPropagation();
      copyWithFeedback('jamf-cli ' + cmd.command, nameCell);
    });
    nameCell.appendChild(copyIcon);
    row.appendChild(nameCell);

    var desc = document.createElement('span');
    desc.className = 'command-desc';
    desc.setAttribute('role', 'cell');
    highlightText(cmd.description || '', desc);
    row.appendChild(desc);

    var marks = document.createElement('span');
    marks.className = 'command-marks';
    marks.setAttribute('role', 'cell');
    if (newCommandSet[cmd.command]) {
      var nb = document.createElement('span');
      nb.className = 'tag tag-new';
      nb.textContent = 'New';
      marks.appendChild(nb);
    }
    if (isDestructiveCommand(cmd)) {
      var db = document.createElement('span');
      db.className = 'tag tag-danger';
      db.title = 'Destructive. Requires --confirm-destructive to run.';
      db.textContent = 'Destructive';
      marks.appendChild(db);
    }
    var related = detectRelatedProduct(cmd);
    if (related && PRODUCT_LABELS[related]) {
      var xb = document.createElement('span');
      xb.className = 'tag';
      xb.setAttribute('data-product', related);
      xb.title = 'Operates on ' + PRODUCT_LABELS[related] + ' data';
      xb.textContent = PRODUCT_LABELS[related];
      marks.appendChild(xb);
    }
    row.appendChild(marks);

    // Expand on click or Enter. The drawer is a sibling so the grid row stays 3 cells.
    row.addEventListener('click', function () { toggleRowDetail(row, cmd); });
    row.addEventListener('keydown', function (e) {
      if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); toggleRowDetail(row, cmd); }
    });
    return row;
  }

  function toggleRowDetail(row, cmd) {
    var next = row.nextElementSibling;
    if (next && next.classList.contains('command-detail')) {
      next.remove();
      row.classList.remove('expanded');
      row.setAttribute('aria-expanded', 'false');
      if (expandedCommand === cmd.command) expandedCommand = null;
      return;
    }
    var table = row.parentNode;
    var open = table.querySelectorAll('.command-detail');
    for (var i = 0; i < open.length; i++) {
      open[i].previousElementSibling.classList.remove('expanded');
      open[i].previousElementSibling.setAttribute('aria-expanded', 'false');
      open[i].remove();
    }
    var detail = document.createElement('div');
    detail.className = 'command-detail';
    detail.setAttribute('role', 'row');
    detail.appendChild(buildDetailContent(cmd));
    row.parentNode.insertBefore(detail, row.nextSibling);
    row.classList.add('expanded');
    row.setAttribute('aria-expanded', 'true');
    expandedCommand = cmd.command;
  }
```

Search the old `renderCommandRow` body (`catalog.js:807-991`) for anything else it did, for example the `hero-example` link or `data-copy` on the name. Carry it into `buildDetailContent` or drop it if the drawer covers it. The old row-click handler that called `buildDetailContent` is replaced by `toggleRowDetail`. Update `navigateToCommand` and `handleDeepLink` to call `toggleRowDetail(row, cmd)` where they used to expand a row, and to set `selectedGroup` to the command's group before `renderCatalog` so the row exists:

```js
  function navigateToCommand(command) {
    var cmd = null;
    for (var i = 0; i < allCommands.length; i++) { if (allCommands[i].command === command) { cmd = allCommands[i]; break; } }
    if (!cmd) return;
    selectedGroup = cmd.group;
    var search = document.getElementById('search');
    if (search) search.value = '';
    renderCatalog(allCommands, '', currentProductFilter);
    var row = document.querySelector('.command-row[data-command="' + command.replace(/"/g, '\\"') + '"]');
    if (row) {
      toggleRowDetail(row, cmd);
      row.scrollIntoView({ behavior: 'smooth', block: 'center' });
      row.focus();
    }
  }
```

- [ ] **Step 5: Update `buildDetailContent` output classes**

In `buildDetailContent` (`catalog.js:1039-1131`) render flags as `span.tag.tag-mono` (and `tag-danger` for `--confirm-destructive`), aliases as plain text after a `Aliases` label, and the example in a `.code-block` with a `.btn.btn-primary.btn-small.copy-btn`. Keep the breadcrumb and sibling links. Wrap the whole fragment in:

```js
    var grid = document.createElement('div');
    grid.className = 'detail-grid';
    var left = document.createElement('div');  left.className = 'detail-col';
    var right = document.createElement('div'); right.className = 'detail-col';
    // breadcrumb, flags, aliases, privileges → left
    // examples (hero-examples first, then examples.json) → right
    grid.appendChild(left); grid.appendChild(right);
    frag.appendChild(grid);
```

- [ ] **Step 6: Update the tabs**

`setupTabs()` and `activateProductTab(filter)` keep working with `.tab[data-filter]`. Add one line to `activateProductTab` before it calls `renderCatalog`:

```js
    selectedGroup = null; // first group of the new product
```

- [ ] **Step 7: Replace the Commands Catalog CSS section**

Replace `style.css` `Commands Catalog` section (`1052-2053`) with:

```css
/* ===== Commands ===== */
#commands {
  display: flex;
  flex-direction: column;
  gap: 24px;
  max-width: 1440px;
  margin: 0 auto;
  padding: 24px 130px 64px;
  box-sizing: border-box;
}
.tabs { display: flex; gap: 24px; border-bottom: 1px solid var(--border-base); }
.tab {
  padding: 10px 0;
  margin-bottom: -1px;
  border: 0;
  border-bottom: 2px solid transparent;
  background: none;
  color: var(--font-secondary);
  font: inherit;
  font-weight: 500;
  cursor: pointer;
}
.tab:hover { color: var(--font-base); }
.tab.active { color: var(--primary); border-bottom-color: var(--primary); }
.tab:focus-visible { outline: none; box-shadow: 0 0 0 2px var(--focus); border-radius: var(--r-small); }
/* verify-site needs one selector per product tab */
.tab[data-filter="pro"], .tab[data-filter="protect"], .tab[data-filter="school"],
.tab[data-filter="security"], .tab[data-filter="platform"] { position: relative; }

.catalog-layout { display: grid; grid-template-columns: 280px minmax(0, 1fr); gap: 32px; align-items: start; }

/* Side navigation */
.group-nav {
  position: sticky;
  top: 80px;
  max-height: calc(100vh - 96px);
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding-right: 24px;
  border-right: 1px solid var(--border-base);
}
.group-nav-product {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 12px 4px;
  font-size: 12px;
  line-height: 16px;
  font-weight: 700;
}
.group-nav-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--font-tertiary); }
.group-nav-product[data-product="pro"] .group-nav-dot { background: var(--product-pro); }
.group-nav-product[data-product="protect"] .group-nav-dot { background: var(--product-protect); }
.group-nav-product[data-product="school"] .group-nav-dot { background: var(--product-school); }
.group-nav-product[data-product="security"] .group-nav-dot { background: var(--product-security); }
.group-nav-product[data-product="platform"] .group-nav-dot { background: var(--product-platform); }
.group-nav-pillar {
  padding: 8px 12px 0;
  font-size: 12px;
  line-height: 16px;
  color: var(--font-tertiary);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.group-nav-item {
  display: flex;
  align-items: center;
  height: var(--h-action);
  padding: 0 12px;
  border: 1px solid transparent;
  border-radius: var(--r-base);
  background: none;
  color: var(--font-base);
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.group-nav-item:hover { background: var(--structure-secondary); }
.group-nav-item.active { background: var(--structure-secondary); border-color: var(--border-base); font-weight: 700; }
.group-nav-item:focus-visible { outline: none; box-shadow: 0 0 0 2px var(--focus); }
.group-nav-label { flex: 1; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.group-nav-count { margin-left: auto; font-family: var(--font-mono); font-size: 12px; color: var(--font-tertiary); font-weight: 400; }

/* Controls */
.catalog-main { display: flex; flex-direction: column; gap: 16px; }
.catalog-controls { display: flex; align-items: center; gap: 12px; }
.search-wrap {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 8px;
  height: var(--h-action);
  padding: 0 12px;
  border: 1px solid var(--border-base);
  border-radius: var(--r-base);
  background: var(--structure-base);
  color: var(--font-tertiary);
}
.search-wrap:focus-within { border-color: var(--primary); box-shadow: 0 0 0 2px var(--focus); }
.search-wrap svg { width: 14px; height: 14px; flex-shrink: 0; }
#search { flex: 1; border: 0; outline: 0; background: none; color: var(--font-base); font: inherit; }
#search::placeholder { color: var(--font-tertiary); }
.search-clear { border: 0; background: none; color: var(--font-tertiary); font-size: 18px; cursor: pointer; }
#catalog-loading, .catalog-empty { padding: 32px 0; text-align: center; color: var(--font-tertiary); }

/* Group table */
.group-table { display: flex; flex-direction: column; gap: 16px; }
.group-table + .group-table { margin-top: 32px; }
.group-table-head { display: flex; align-items: baseline; gap: 16px; }
.group-table-head h3 { margin: 0; font-size: 24px; line-height: 28px; font-weight: 700; }
.group-table-meta { color: var(--font-secondary); }
.cmd-table { border: 1px solid var(--border-base); border-radius: var(--r-base); overflow: hidden; }
.cmd-table-header,
.command-row {
  display: grid;
  grid-template-columns: 360px minmax(0, 1fr) 160px;
  gap: 16px;
  align-items: center;
  padding: 0 16px;
  box-sizing: border-box;
}
.cmd-table-header {
  height: 44px;
  background: var(--structure-secondary);
  border-bottom: 1px solid var(--border-base);
  font-weight: 700;
}
.command-row {
  min-height: 48px;
  padding-top: 8px;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--border-secondary);
  cursor: pointer;
}
.command-row:last-child { border-bottom: 0; }
.command-row:hover { background: var(--structure-secondary); }
.command-row:focus-visible { outline: none; box-shadow: inset 0 0 0 2px var(--focus); }
.command-row.expanded { background: var(--structure-secondary); border-bottom-color: transparent; }
.command-name { display: flex; align-items: center; gap: 8px; font-family: var(--font-mono); font-size: 13px; }
.cmd-product { color: var(--font-tertiary); }
.command-row[data-product="pro"] .cmd-product { color: var(--product-pro); }
.command-row[data-product="protect"] .cmd-product { color: var(--product-protect); }
.command-row[data-product="school"] .cmd-product { color: var(--product-school); }
.command-row[data-product="security"] .cmd-product { color: var(--product-security); }
.command-row[data-product="platform"] .cmd-product { color: var(--product-platform); }
.cmd-prefix { color: var(--font-tertiary); }
.cmd-action { font-weight: 600; }
.command-copy-icon { border: 0; background: none; color: var(--font-tertiary); width: 24px; height: 24px; padding: 4px; border-radius: var(--r-small); cursor: pointer; opacity: 0; }
.command-copy-icon svg { width: 14px; height: 14px; }
.command-row:hover .command-copy-icon, .command-copy-icon:focus-visible { opacity: 1; }
.command-name.copied .command-copy-icon { color: var(--success); opacity: 1; }
.command-desc { color: var(--font-secondary); }
.command-marks { display: flex; gap: 6px; flex-wrap: wrap; justify-content: flex-start; }
mark { background: var(--highlight); color: inherit; border-radius: 2px; }

/* Drawer */
.command-detail { padding: 4px 16px 20px; background: var(--structure-secondary); border-bottom: 1px solid var(--border-secondary); }
.detail-grid { display: grid; grid-template-columns: 360px minmax(0, 1fr); gap: 24px; }
.detail-col { display: flex; flex-direction: column; gap: 12px; }
.detail-heading { font-size: 12px; line-height: 16px; font-weight: 700; }
.detail-flags { display: flex; flex-wrap: wrap; gap: 6px; }
.detail-meta { font-size: 12px; line-height: 16px; color: var(--font-tertiary); }
.detail-meta strong { color: var(--font-base); font-weight: 500; }
.detail-breadcrumb { font-size: 12px; color: var(--font-tertiary); }
.command-detail .code-block { font-size: 12px; line-height: 18px; }
```

- [ ] **Step 8: Verify**

Preview. Expected: underline tabs, a sticky sidebar with product headings and group items, one table for the first group, click a group and the table changes, click a row and the drawer opens under it, type in search and every matching group renders as its own table with the product tag in its heading. Deep link `#pro-computers-inventory-erase` (or whatever hash format `handleDeepLink` uses) opens that row. Press `/` to focus search, `j`/`k` to move rows, `Enter` to expand, `c` to copy. Check both themes.

Run:

```bash
make verify-site
grep -n "renderGroup(\|toggleGroup\|renderResourceBucket\|catalog-group-header" docs/site/catalog.js docs/site/style.css
```

Expected: pass, then no output.

- [ ] **Step 9: Commit**

```bash
git add docs/site/index.html docs/site/style.css docs/site/catalog.js
git commit -m "site: commands section as side navigation plus data table with row drawer"
```

---

### Task 7: FAQ, footer, palette, and shortcuts modal

**Files:**
- Modify: `docs/site/index.html:332-366` (FAQ), `:446-460` (footer), keyboard shortcuts modal markup
- Modify: `docs/site/style.css:68-325` (Cmd+K palette), `2216-2249` (Footer), `2290-2418` (Skeleton, Keyboard Shortcuts Modal)

- [ ] **Step 1: FAQ markup**

```html
  <section id="faq">
    <h2>Questions</h2>
    <div class="faq-grid">
      <div class="faq-item"><h3>What is jamf-cli?</h3><p>A single binary that manages Jamf Platform API Gateway, Jamf Pro, Jamf Protect, Jamf School, and Jamf Security Cloud from the shell.</p></div>
      <div class="faq-item"><h3>How are credentials handled?</h3><p>Interactive prompts, <code>JAMF_*</code> environment variables, or keychain-backed profiles. Never flags, never stdin.</p></div>
      <div class="faq-item"><h3>Does it cover the Classic API?</h3><p>Yes. Classic and modern Jamf Pro APIs both live under <code>pro</code>. Every classic write takes <code>--from-file</code>.</p></div>
      <div class="faq-item"><h3>Can AI agents use it?</h3><p>Yes. The site publishes <a href="llms.txt">llms.txt</a> and <a href="commands.json">commands.json</a>, and the binary has <code>mcp</code> and <code>agent-context</code> commands.</p></div>
      <div class="faq-item"><h3>How current is this catalog?</h3><p>It is generated from the built binary on every merge to main. Commands added since the last release carry a New tag.</p></div>
      <div class="faq-item"><h3>Is it free?</h3><p>Yes. Open source under the repository license, built and maintained by Jamf Concepts.</p></div>
    </div>
  </section>
```

Update the JSON-LD `FAQPage` block so each `Question` matches a heading above.

- [ ] **Step 2: Footer markup**

```html
  <footer>
    <span class="footer-meta mono"><span id="footer-version"></span><span id="last-updated"></span></span>
    <ul class="footer-links">
      <li><a href="https://concepts.jamf.com" target="_blank" rel="noopener">Jamf Concepts</a></li>
      <li><a href="https://github.com/Jamf-Concepts/jamf-cli" target="_blank" rel="noopener">GitHub</a></li>
      <li><a href="https://github.com/Jamf-Concepts/jamf-cli/wiki" target="_blank" rel="noopener">Wiki</a></li>
      <li><a href="https://github.com/Jamf-Concepts/jamf-cli/releases" target="_blank" rel="noopener">Releases</a></li>
      <li><a href="llms.txt" class="mono">llms.txt</a></li>
      <li><a href="commands.json" class="mono">commands.json</a></li>
      <li>Press <kbd class="footer-kbd">?</kbd> for shortcuts</li>
    </ul>
  </footer>
```

In `populateStats()` change the footer writes to `footerVersion.textContent = 'jamf-cli v' + version;` and `lastUpdated.textContent = ' · generated ' + formatDate(generatedAt);`.

- [ ] **Step 3: CSS**

```css
/* ===== FAQ ===== */
#faq {
  display: grid;
  grid-template-columns: 300px minmax(0, 1fr);
  gap: 64px;
  max-width: 1440px;
  margin: 0 auto;
  padding: 56px 130px;
  box-sizing: border-box;
  border-top: 1px solid var(--border-base);
}
#faq h2 { margin: 0; font-size: 28px; line-height: 36px; font-weight: 700; }
.faq-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 28px 48px; }
.faq-item { display: flex; flex-direction: column; gap: 4px; }
.faq-item h3 { margin: 0; font-size: 16px; line-height: 24px; font-weight: 700; }
.faq-item p { margin: 0; color: var(--font-secondary); text-wrap: pretty; }
.faq-item code { font-size: 12px; padding: 1px 4px; border-radius: var(--r-small); background: var(--structure-secondary); }

/* ===== Footer ===== */
footer {
  display: flex;
  align-items: center;
  gap: 24px;
  padding: 24px 130px 40px;
  background: var(--structure-secondary);
  border-top: 1px solid var(--border-base);
  font-size: 12px;
  line-height: 16px;
  color: var(--font-tertiary);
}
.footer-links { margin-left: auto; display: flex; gap: 24px; list-style: none; margin-top: 0; margin-bottom: 0; padding: 0; }
.footer-links a { color: var(--font-secondary); }
.footer-kbd, .palette-kbd, .palette-footer kbd, .kbd-modal kbd {
  font-family: var(--font-mono);
  font-size: 11px;
  padding: 0 5px;
  border: 1px solid var(--border-base);
  border-radius: var(--r-small);
  background: var(--structure-base);
}
```

Retheme the palette and shortcuts modal by replacing their color references: `var(--card-bg)` → `var(--structure-base)`, `var(--border)` → `var(--border-base)`, `var(--text-primary)` → `var(--font-base)`, `var(--text-secondary)` → `var(--font-tertiary)`, `var(--accent)` → `var(--primary)`, `var(--shadow-3)` → `var(--shadow-lg)`, `border-radius: 12px` → `var(--r-base)`. Change every `'Geist Mono'` reference there (Task 1 already did this with `sed`). Set the palette backdrop to `light-dark(rgba(20, 25, 32, 0.4), rgba(0, 0, 0, 0.6))`.

- [ ] **Step 4: Verify**

Preview. Expected: two-column FAQ, grey footer band, palette opens with Cmd+K in Nebula colors, `?` opens the shortcuts modal in Nebula colors. Check both themes. Run `make verify-site`. Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add docs/site/index.html docs/site/style.css docs/site/catalog.js
git commit -m "site: retheme FAQ, footer, palette, and shortcuts modal"
```

---

### Task 8: Responsive layout

**Files:**
- Modify: `docs/site/style.css` (`Responsive` section, currently `2419-2622`)
- Modify: `docs/site/catalog.js` (`renderGroupNav`)

**Interfaces:**
- Produces: below 1024px the sidebar becomes a `<select id="group-select">` rendered by `renderGroupNav`. Both controls exist in the DOM. CSS shows one.

- [ ] **Step 1: Add the group select**

In `renderGroupNav`, after clearing `nav`, also build a select:

```js
    var select = document.getElementById('group-select');
    if (select) {
      select.innerHTML = '';
      var orderS = Object.keys(PRODUCT_LABELS).concat(['core']);
      for (var s = 0; s < orderS.length; s++) {
        var gs = orderS[s] === 'core' ? sharedGroups : (productGroups[orderS[s]] || []);
        if (!gs.length) continue;
        var og = document.createElement('optgroup');
        og.label = orderS[s] === 'core' ? 'Core' : PRODUCT_LABELS[orderS[s]];
        for (var t = 0; t < gs.length; t++) {
          var opt = document.createElement('option');
          opt.value = gs[t].name;
          opt.textContent = gs[t].name + ' (' + gs[t].commands.length + ')';
          if (gs[t].name === selectedGroup) opt.selected = true;
          og.appendChild(opt);
        }
        select.appendChild(og);
      }
    }
```

Add the select to `index.html` inside `.catalog-main` above `.catalog-controls`:

```html
        <label class="group-select-wrap">
          <span class="sr-only">Command group</span>
          <select id="group-select" aria-label="Command group"></select>
        </label>
```

In `setupTabs()` or `init()` bind it once:

```js
    var gsel = document.getElementById('group-select');
    if (gsel) gsel.addEventListener('change', function () {
      selectedGroup = this.value;
      var search = document.getElementById('search');
      if (search) search.value = '';
      renderCatalog(allCommands, '', currentProductFilter);
    });
```

- [ ] **Step 2: Replace the Responsive section**

```css
/* ===== Responsive ===== */
.sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; }
.group-select-wrap { display: none; }
#group-select {
  width: 100%;
  height: 44px;
  padding: 0 12px;
  border: 1px solid var(--border-base);
  border-radius: var(--r-base);
  background: var(--structure-base);
  color: var(--font-base);
  font: inherit;
}

@media (max-width: 1280px) {
  #hero, #facts, #quickstart, #commands, #faq, footer { padding-left: 64px; padding-right: 64px; }
  #hero { grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 48px; }
  #hero h1 { font-size: 48px; line-height: 56px; }
  .cmd-table-header, .command-row { grid-template-columns: 300px minmax(0, 1fr) 140px; }
  .detail-grid { grid-template-columns: 300px minmax(0, 1fr); }
}

@media (max-width: 1024px) {
  nav { padding: 0 24px; }
  .nav-search { width: 200px; }
  #hero { grid-template-columns: minmax(0, 1fr); }
  .hero-install { width: 100%; }
  #facts { grid-template-columns: minmax(0, 1fr); gap: 24px; }
  .steps { grid-template-columns: minmax(0, 1fr); }
  .catalog-layout { grid-template-columns: minmax(0, 1fr); }
  .group-nav { display: none; }
  .group-select-wrap { display: block; }
  #faq { grid-template-columns: minmax(0, 1fr); gap: 24px; }
  .faq-grid { grid-template-columns: minmax(0, 1fr); }
  .cmd-table-header { display: none; }
  .command-row { grid-template-columns: minmax(0, 1fr); gap: 4px; padding: 12px 16px; }
  .command-marks { justify-content: flex-start; }
  .detail-grid { grid-template-columns: minmax(0, 1fr); }
}

@media (max-width: 768px) {
  nav { height: 56px; padding: 0 16px; gap: 8px; }
  .nav-links, .nav-search { display: none; }
  .hamburger {
    display: inline-flex;
    flex-direction: column;
    justify-content: center;
    gap: 4px;
    width: 44px;
    height: 44px;
    margin-left: auto;
    border: 0;
    background: none;
    cursor: pointer;
  }
  .hamburger span { display: block; width: 18px; height: 2px; margin: 0 auto; background: var(--font-base); }
  nav.open .nav-links {
    display: flex;
    flex-direction: column;
    gap: 0;
    position: absolute;
    top: 56px;
    left: 0;
    right: 0;
    padding: 8px 16px 16px;
    background: var(--structure-base);
    border-bottom: 1px solid var(--border-base);
  }
  nav.open .nav-links a { display: block; padding: 12px 0; }
  .nav-sep { display: none; }
  #hero, #facts, #quickstart, #commands, #faq, footer { padding-left: 20px; padding-right: 20px; }
  #hero { padding-top: 40px; padding-bottom: 28px; gap: 24px; }
  #hero h1 { font-size: 38px; line-height: 42px; }
  #hero .subtitle { font-size: 16px; line-height: 24px; }
  .hero-actions { flex-direction: column; }
  .btn { height: 44px; }
  .btn-small { height: var(--h-small); }
  #terminal { min-height: 0; font-size: 11.5px; line-height: 17px; padding: 14px 16px; }
  .tabs { overflow-x: auto; gap: 16px; scrollbar-width: none; }
  .tab { white-space: nowrap; }
  .catalog-controls { flex-wrap: wrap; }
  .search-wrap { flex-basis: 100%; height: 44px; }
  .group-nav-item, #group-select { height: 44px; }
  footer { flex-direction: column; align-items: flex-start; gap: 12px; }
  .footer-links { margin-left: 0; flex-wrap: wrap; gap: 12px 20px; }
}
```

Keep the existing hamburger click handler in `index.html` if it toggles `nav.open` and `aria-expanded`. If it toggled another class, change it to `open`.

- [ ] **Step 3: Verify**

Preview at 1440, 1024, 768, and 390 wide (DevTools device toolbar). Expected: no horizontal scroll at any width, the sidebar becomes a select below 1024, rows stack below 1024, the hamburger opens the links below 768, every tap target is 44px or taller on the phone view. Check both themes.

Run `make verify-site`. Expected: pass.

- [ ] **Step 4: Commit**

```bash
git add docs/site/index.html docs/site/style.css docs/site/catalog.js
git commit -m "site: responsive layout with group select below 1024px"
```

---

### Task 9: Remove dead code, update docs, final check

**Files:**
- Modify: `docs/site/style.css` (delete transitional aliases and unused rules)
- Modify: `docs/site/catalog.js` (delete unused helpers)
- Modify: `scripts/verify-site-products.sh:52-54` (update the `stat card` label text; the pattern stays)
- Modify: `CLAUDE.md` ("Modify the GitHub Pages showcase site" row and "GitHub Pages Site" section)
- Create: `docs/solutions/conventions/site-nebula-tokens-2026-09-03.md`

- [ ] **Step 1: Find dead CSS selectors**

Run from the repo root:

```bash
python3 - <<'EOF'
import re
css = open('docs/site/style.css').read()
html = open('docs/site/index.html').read() + open('docs/site/catalog.js').read() + open('docs/site/palette.js').read() + open('docs/site/terminal.js').read()
classes = set(re.findall(r'\.([a-zA-Z][\w-]*)', css))
ids = set(re.findall(r'#([a-zA-Z][\w-]*)\s*[{,.:\[ ]', css))
dead = [c for c in sorted(classes) if c not in html]
dead_ids = [i for i in sorted(ids) if ('id="%s"' % i) not in html and ("'%s'" % i) not in html]
print('classes:', dead)
print('ids:', dead_ids)
EOF
```

Review the list. Delete each rule whose selector is dead. Expected survivors: pseudo-states and classes `terminal.js` builds by string concatenation. Check those by hand before deleting.

- [ ] **Step 2: Remove transitional aliases**

Run:

```bash
for v in brand-blue accent link-blue page-bg card-bg text-primary text-secondary tab-inactive-bg hero-bg new-indicator tint-pro tint-protect tint-school tint-security tint-platform accent-tint accent-tint-hover highlight pill-bg pill-text badge-bg muted-header-text muted-header-border muted-header-bg shadow-1 shadow-2 shadow-3 hero-glyph hero-glow; do
  n=$(grep -c "var(--$v)" docs/site/style.css docs/site/index.html docs/site/catalog.js docs/site/palette.js docs/site/terminal.js | awk -F: '{s+=$2} END {print s}')
  echo "$v $n"
done
```

For each alias with `0` users, delete its line from `:root`. For each alias with users, replace the users with the Nebula token it aliases, then delete the alias. Expected at the end: the `Transitional aliases` comment block is gone. Keep `--highlight` only if `mark` still reads it, and rename it `--search-highlight`.

- [ ] **Step 3: Remove dead JS**

Run:

```bash
for f in renderGroupsWithPillars renderPillarDivider renderProductDivider renderGroup renderGroupCommands bucketByResource shouldBucket appendLeafCommands renderResourceBucket toggleResource toggleGroup animateCount; do
  echo "$f $(grep -c "$f" docs/site/catalog.js)"
done
```

Delete any function whose count is `1` (its own definition). Keep `animateCount` if the tag counters use it.

- [ ] **Step 4: Update the verify script label**

In `scripts/verify-site-products.sh` change the label strings only:

```bash
  check docs/site/index.html "id=\"stat-${product}\"" "product tag counter"
  check docs/site/index.html "data-filter=\"${product}\"" "command filter tab"
  check docs/site/index.html "data-tab=\"${product}\"" "hero product tag link"
```

- [ ] **Step 5: Update docs**

In `CLAUDE.md`, in the "GitHub Pages Site" section, append:

```markdown
The site uses Nebula v5 tokens (`jamf/ds-nebula`, v2 token set) declared once in `:root` with `light-dark()`. Theme follows the system by default; the toggle cycles system → light → dark and stores `light`/`dark` in `localStorage.theme`. Products map to Nebula tag themes (`--tag-<product>-fg`/`-bg`), so adding a product means adding one hue pair there, one `.tag[data-product]` rule, one `.group-nav-product` dot rule, one `.command-row .cmd-product` rule, a tab, and a hero tag with `id="stat-<product>"`. `make verify-site` enforces the hooks.
```

Create `docs/solutions/conventions/site-nebula-tokens-2026-09-03.md`:

```markdown
---
title: Showcase site tokens come from Nebula v5
module: docs/site
tags: [site, design-system, nebula, theming]
date: 2026-09-03
---

# Showcase site tokens come from Nebula v5

**Decision.** `docs/site/style.css` declares every color once in `:root` as `light-dark(light, dark)` with values from `jamf/ds-nebula` (`shared/style-dictionary/properties/v2` and `global/color/coreV2.json`). No component is imported. The site copies Nebula anatomy as markup and CSS.

**Theme default.** No stored choice means `color-scheme: light dark` and the OS decides. The toggle cycles system, light, dark. A stored choice sets `data-theme` on `<html>` before first paint from the head script.

**Product colors.** Each product is a Nebula themed tag: Pro cyan, Protect green, School yellow, Security red, Platform blue, Core gray. The font color is the hue's 900 (light) or 200 (dark) step. The background is a 16 percent tint of the 500 step.

**Why no self-hosted fonts yet.** Inter and Noto Sans Mono load from Google Fonts, which the site already used for Geist. Self-hosting the woff2 files from `ds-nebula/shared/fonts` removes a third-party request. It needs the OFL license text beside the files. Do it as a follow-up.

**Do not.** Do not add a fourth color declaration path (a `[data-theme="dark"]` block with overrides). Every dark value lives in the second argument of `light-dark()`.
```

- [ ] **Step 6: Final verification**

Run:

```bash
make build
go run ./generator/site/main.go --binary ./bin/jamf-cli --output ./docs/site/commands.json
make verify-site
grep -c "Geist\|stat-card\|hero-glyph\|catalog-group-header" docs/site/style.css docs/site/index.html docs/site/catalog.js
```

Expected: verify passes, and the grep prints `0` for every file.

Preview in light and dark at 1440 and 390. Walk through: install copy, product tag click, tab click, group click, row expand, search, deep link, Cmd+K palette, `?` modal, theme toggle cycle, reload persistence.

- [ ] **Step 7: Commit**

```bash
git add docs/site scripts/verify-site-products.sh CLAUDE.md docs/solutions/conventions/site-nebula-tokens-2026-09-03.md
git commit -m "site: remove transitional aliases and dead rules, document Nebula tokens"
```
