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
