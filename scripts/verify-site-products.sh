#!/usr/bin/env bash
# Verify that the showcase site (docs/site/) has full support for every
# product namespace the CLI binary reports.  Intended to run in CI after
# "make build" so that adding a new product without wiring up the site
# is caught before merge.
#
# Usage: scripts/verify-site-products.sh [--binary ./bin/jamf-cli]

set -euo pipefail

BINARY="./bin/jamf-cli"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary) BINARY="$2"; shift 2 ;;
    *) echo "Unknown flag: $1" >&2; exit 1 ;;
  esac
done

if [[ ! -x "$BINARY" ]]; then
  echo "Error: binary not found at $BINARY (run 'make build' first)" >&2
  exit 1
fi

# Extract product names from the binary's command metadata.
# "core" is a virtual product (commands without a product namespace) — skip it.
PRODUCTS=$("$BINARY" commands -o json \
  | python3 -c "
import json, sys
data = json.load(sys.stdin)
products = sorted(set(c['product'] for c in data if c.get('product')))
print('\n'.join(products))
")

if [[ -z "$PRODUCTS" ]]; then
  echo "Error: no products found in binary output" >&2
  exit 1
fi

ERRORS=0

check() {
  local file="$1" pattern="$2" label="$3"
  # -F: every pattern here is a literal, and a selector carrying [ or . is
  # a regex that matches something else entirely. `.tag[data-product="pro"]`
  # as a basic regex is `.tag` plus one character from a set, which matches
  # nothing in the file and reported a missing hook that is present.
  if ! grep -qF -- "$pattern" "$file"; then
    echo "  MISSING: $label in $file (expected pattern: $pattern)"
    ERRORS=$((ERRORS + 1))
  fi
}

for product in $PRODUCTS; do
  echo "Checking product: $product"

  # index.html
  check docs/site/index.html "id=\"stat-${product}\"" "product tag counter"
  check docs/site/index.html "data-filter=\"${product}\"" "command filter tab"
  check docs/site/index.html "data-tab=\"${product}\"" "hero product tag link"

  # style.css
  check docs/site/style.css "--product-${product}" "CSS custom property"
  check docs/site/style.css "data-product=\"${product}\"" "product data-attribute selector"
  check docs/site/style.css "data-filter=\"${product}\"]" "tab filter selector"
  # The three above are substring checks, and a bare data-product match is
  # satisfied by any one of the palette, sidebar or tag rules. Name each of
  # the Nebula tag hooks, so deleting one fails instead of leaning on a
  # sibling. Without these, a product tag with no colour rule shipped green.
  check docs/site/style.css "--tag-${product}-fg:" "Nebula tag foreground token"
  check docs/site/style.css "--tag-${product}-bg:" "Nebula tag background token"
  check docs/site/style.css ".tag[data-product=\"${product}\"]" "product tag colour rule"
  check docs/site/style.css ".group-nav-product[data-product=\"${product}\"] .group-nav-dot" "sidebar product dot rule"

  # catalog.js
  check docs/site/catalog.js "stat-${product}" "product tag counter (animateCount call)"
  check docs/site/catalog.js "'${product}'" "product label in PRODUCT_LABELS"
done

if [[ $ERRORS -gt 0 ]]; then
  echo ""
  echo "Error: $ERRORS missing site reference(s) found."
  echo "See docs in CLAUDE.md under 'Adding a new product to the showcase site' for the full checklist."
  exit 1
fi

echo "All products have full site support."
