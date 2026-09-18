### GitHub Pages Site

Site at `docs/site/` auto-deploys on push to `main` via `.github/workflows/deploy-site.yaml`. Introspects the built binary to generate `commands.json` — command list, counts, groups, products, flags, aliases auto-update.

Adding a product or reclassifying groups needs manual edits in `index.html`, `style.css`, `catalog.js` — `make verify-site` enforces in CI. Local dev: `make site`.

The site uses Nebula v5 tokens (`jamf/ds-nebula`, v2 token set) declared once in `:root` with `light-dark()`. Theme follows the system by default; the toggle cycles system → light → dark and stores `light`/`dark` in `localStorage.theme`. Products map to Nebula tag themes (`--tag-<product>-fg`/`-bg`), so adding a product means adding one `--product-<product>` hue plus the `--tag-<product>-fg`/`-bg` pair derived from it, one `.tag[data-product="<product>"]` colour rule, one `.group-nav-product[data-product="<product>"] .group-nav-dot` rule, one `.tab[data-filter="<product>"]` rule, a tab and a hero tag with `id="stat-<product>"` in `index.html`, and one `'<product>'` entry in `PRODUCT_LABELS` in `catalog.js`. A command row carries its product as a plain `data-product` attribute and needs no rule of its own. `make verify-site` enforces every hook in that list — **name each one, because the checks are `grep` substrings and a bare `data-product="<product>"` is satisfied by any rule that happens to mention the product.** That is how the tag hooks came to be documented as enforced while a product tag with its colour rule and both tokens deleted still passed.

