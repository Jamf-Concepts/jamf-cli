#!/usr/bin/env bash
# Verify that the site generator produced well-formed output and the static
# site assets in docs/site/ are internally consistent. Pairs with
# verify-site-products.sh (which checks product-namespace coverage across
# the static HTML/CSS/JS): this one checks the *generated* artifacts and
# their structural relationship to the static site.
#
# Catches:
#   - Generator panics or output drift (run end-to-end before this script)
#   - Malformed commands.json / drift between commandCount and len(commands)
#   - llms.txt / llms-full.txt missing or not following llmstxt.org spec
#   - JSON-LD in index.html that doesn't parse (silent SEO regression)
#   - New Pro group added to internal/commands/groups.go without a matching
#     pillar entry in docs/site/catalog.js (commands would render without
#     a pillar divider in the Pro catalog view)
#
# Usage:
#   scripts/verify-site-output.sh \
#     --commands-json docs/site/commands.json \
#     --llms-txt docs/site/llms.txt \
#     --llms-full-txt docs/site/llms-full.txt \
#     --site-dir docs/site

set -euo pipefail

COMMANDS_JSON="docs/site/commands.json"
SITE_DIR="docs/site"
LLMS_TXT=""
LLMS_FULL_TXT=""
BINARY=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --commands-json)  COMMANDS_JSON="$2"; shift 2 ;;
    --site-dir)       SITE_DIR="$2"; shift 2 ;;
    --llms-txt)       LLMS_TXT="$2"; shift 2 ;;
    --llms-full-txt)  LLMS_FULL_TXT="$2"; shift 2 ;;
    --binary)         BINARY="$2"; shift 2 ;;
    -h|--help)
      sed -n '2,/^$/p' "$0" | sed 's/^# \?//'
      exit 0
      ;;
    *) echo "Unknown flag: $1" >&2; exit 1 ;;
  esac
done

# Default the llms paths to live alongside commands.json if unset.
[[ -z "$LLMS_TXT" ]]      && LLMS_TXT="$(dirname "$COMMANDS_JSON")/llms.txt"
[[ -z "$LLMS_FULL_TXT" ]] && LLMS_FULL_TXT="$(dirname "$COMMANDS_JSON")/llms-full.txt"

ERRORS=0
fail() { echo "  FAIL: $*" >&2; ERRORS=$((ERRORS + 1)); }
pass() { echo "  PASS: $*"; }

echo "==> commands.json structure ($COMMANDS_JSON)"
if [[ ! -s "$COMMANDS_JSON" ]]; then
  fail "$COMMANDS_JSON missing or empty"
elif ! python3 -m json.tool "$COMMANDS_JSON" >/dev/null 2>&1; then
  fail "$COMMANDS_JSON is not valid JSON"
else
  pass "valid JSON"
  python3 - "$COMMANDS_JSON" <<'PY' || ERRORS=$((ERRORS + 1))
import json, sys
data = json.load(open(sys.argv[1]))
required = {"generatedAt", "version", "commandCount", "commands"}
missing = required - data.keys()
if missing:
    print(f"  FAIL: commands.json missing top-level keys: {sorted(missing)}", file=sys.stderr)
    sys.exit(1)
if not isinstance(data["commands"], list):
    print("  FAIL: 'commands' is not a list", file=sys.stderr)
    sys.exit(1)
if data["commandCount"] != len(data["commands"]):
    print(f"  FAIL: commandCount={data['commandCount']} but len(commands)={len(data['commands'])}", file=sys.stderr)
    sys.exit(1)
per_command_required = {"command", "group"}
for i, c in enumerate(data["commands"]):
    miss = per_command_required - c.keys()
    if miss:
        print(f"  FAIL: commands[{i}] missing keys: {sorted(miss)}", file=sys.stderr)
        sys.exit(1)
print(f"  PASS: {data['commandCount']} commands, version {data['version']}")
PY
fi

echo "==> llms.txt ($LLMS_TXT)"
if [[ ! -s "$LLMS_TXT" ]]; then
  fail "$LLMS_TXT missing or empty"
else
  if ! head -1 "$LLMS_TXT" | grep -q '^# jamf-cli$'; then
    fail "$LLMS_TXT missing H1 (expected '# jamf-cli' on line 1)"
  elif ! head -5 "$LLMS_TXT" | grep -q '^> '; then
    fail "$LLMS_TXT missing summary blockquote (per llmstxt.org spec)"
  else
    pass "llmstxt.org-compliant ($(wc -c <"$LLMS_TXT" | tr -d ' ') bytes)"
  fi
fi

echo "==> llms-full.txt ($LLMS_FULL_TXT)"
if [[ ! -s "$LLMS_FULL_TXT" ]]; then
  fail "$LLMS_FULL_TXT missing or empty"
else
  bytes=$(wc -c <"$LLMS_FULL_TXT" | tr -d ' ')
  # Catalog has 1,200+ commands; markdown reference is ~150KB. Anything
  # under 50KB means the generator silently emitted an empty or truncated file.
  if [[ $bytes -lt 50000 ]]; then
    fail "$LLMS_FULL_TXT suspiciously small ($bytes bytes; expected >50KB)"
  elif ! head -1 "$LLMS_FULL_TXT" | grep -q '^# jamf-cli'; then
    fail "$LLMS_FULL_TXT missing H1 header"
  else
    pass "well-formed ($bytes bytes)"
  fi
fi

echo "==> JSON-LD in $SITE_DIR/index.html"
if [[ ! -f "$SITE_DIR/index.html" ]]; then
  fail "$SITE_DIR/index.html missing"
else
  python3 - "$SITE_DIR/index.html" <<'PY' || ERRORS=$((ERRORS + 1))
import json, re, sys
html = open(sys.argv[1]).read()
matches = re.findall(
    r'<script[^>]+type="application/ld\+json"[^>]*>(.*?)</script>',
    html, re.DOTALL,
)
if not matches:
    print("  FAIL: no JSON-LD <script> block found in index.html", file=sys.stderr)
    sys.exit(1)
for i, m in enumerate(matches, 1):
    try:
        data = json.loads(m)
    except json.JSONDecodeError as e:
        print(f"  FAIL: JSON-LD block #{i} invalid JSON: {e}", file=sys.stderr)
        sys.exit(1)
    print(f"  PASS: JSON-LD block #{i} valid (@type={data.get('@type', '?')})")
PY
fi

echo "==> FAQ <-> FAQPage consistency in $SITE_DIR/index.html"
if [[ ! -f "$SITE_DIR/index.html" ]]; then
  fail "$SITE_DIR/index.html missing"
else
  python3 - "$SITE_DIR/index.html" <<'PY' || ERRORS=$((ERRORS + 1))
import json, re, sys
from html import unescape
html = open(sys.argv[1]).read()

def norm(s):
    # Strip inline tags (e.g. <code>), decode entities, collapse whitespace —
    # so the visible markup and the plain-text JSON-LD compare apples to apples.
    s = re.sub(r'<[^>]+>', '', s)
    s = unescape(s)
    return re.sub(r'\s+', ' ', s).strip()

# Question -> answer declared in the FAQPage structured data.
faq = None
for m in re.findall(r'<script[^>]+type="application/ld\+json"[^>]*>(.*?)</script>', html, re.DOTALL):
    try:
        data = json.loads(m)
    except json.JSONDecodeError:
        continue
    if data.get("@type") == "FAQPage":
        faq = data
        break
if faq is None:
    print("  FAIL: no FAQPage JSON-LD block found in index.html", file=sys.stderr)
    sys.exit(1)
jsonld = {norm(q.get("name", "")): norm((q.get("acceptedAnswer") or {}).get("text", ""))
          for q in faq.get("mainEntity", [])}

# Question -> answer a human sees in the visible #faq section.
sec = re.search(r'<section id="faq".*?</section>', html, re.DOTALL)
if not sec:
    print("  FAIL: no visible <section id=\"faq\"> found in index.html", file=sys.stderr)
    sys.exit(1)
visible = {norm(h): norm(p)
           for h, p in re.findall(r'<h3>(.*?)</h3>\s*<p>(.*?)</p>', sec.group(0), re.DOTALL)}

# Google requires the structured-data answer to match the visible answer, so
# check question presence AND answer-text parity (one is for humans, one for
# answer engines — they must not drift apart).
problems = []
for q, ans in jsonld.items():
    if q not in visible:
        problems.append(f"question not shown to humans: {q!r}")
    elif visible[q] != ans:
        problems.append(f"answer text differs between JSON-LD and visible FAQ for {q!r}")
if problems:
    print("  FAIL: FAQPage <-> visible #faq drift:", file=sys.stderr)
    for x in problems:
        print(f"    - {x}", file=sys.stderr)
    print("  The structured data and the on-page FAQ (questions and answers) must stay in sync.", file=sys.stderr)
    sys.exit(1)
print(f"  PASS: all {len(jsonld)} FAQPage questions and answers match the visible #faq section")
PY
fi

echo "==> llms.txt flag names exist in the CLI ($LLMS_TXT)"
if [[ -z "$BINARY" ]]; then
  echo "  SKIP: --binary not provided; cannot validate flag names against the CLI"
elif [[ ! -x "$BINARY" ]]; then
  fail "--binary $BINARY is not executable"
elif [[ ! -s "$LLMS_TXT" ]]; then
  fail "$LLMS_TXT missing; cannot validate flag names"
else
  # Authoritative flag set = global/persistent flags (root --help) plus every
  # per-command local flag (commands.json). The agent contract must not name a
  # flag outside that union.
  global_flags="$("$BINARY" --help 2>&1 | grep -oE '\-\-[a-z][a-z0-9-]+' | sort -u)"
  python3 - "$LLMS_TXT" "$COMMANDS_JSON" "$global_flags" <<'PY' || ERRORS=$((ERRORS + 1))
import json, re, sys
llms = open(sys.argv[1]).read()
cmds = json.load(open(sys.argv[2]))
valid = set(sys.argv[3].split())
for c in cmds["commands"]:
    for f in (c.get("flags") or []):
        valid.add(f)
named = set(re.findall(r'(?<![\w-])--[a-z][a-z0-9-]+', llms))
unknown = sorted(named - valid)
if unknown:
    print("  FAIL: llms.txt names flags that do not exist in the CLI:", file=sys.stderr)
    for f in unknown:
        print(f"    - {f}", file=sys.stderr)
    print("  A flag was renamed/removed but the agent contract still advertises it.", file=sys.stderr)
    sys.exit(1)
print(f"  PASS: all {len(named)} flag names in llms.txt are real CLI flags")
PY
fi

echo "==> product coverage & command count in llms.txt / FAQ"
if [[ ! -s "$COMMANDS_JSON" || ! -f "$SITE_DIR/index.html" ]]; then
  fail "commands.json or index.html missing; cannot validate product/count docs"
else
  python3 - "$COMMANDS_JSON" "$LLMS_TXT" "$SITE_DIR/index.html" <<'PY' || ERRORS=$((ERRORS + 1))
import json, re, sys
cmds = json.load(open(sys.argv[1]))
llms = open(sys.argv[2]).read()
html = open(sys.argv[3]).read()

labels = {"platform": "Jamf Platform", "pro": "Jamf Pro",
          "protect": "Jamf Protect", "school": "Jamf School",
          "security": "Jamf Security Cloud"}
products = sorted({c.get("product") for c in cmds["commands"] if c.get("product")})

m = re.search(r'<section id="faq".*?</section>', html, re.DOTALL)
faq = m.group(0) if m else ""

problems = []
for p in products:
    label = labels.get(p)
    if not label:
        problems.append(f"unknown product {p!r}: add a display name here and mention it in llms.txt + the FAQ")
        continue
    if label not in llms:
        problems.append(f"product {label!r} in commands.json but not mentioned in llms.txt")
    if label not in faq:
        problems.append(f"product {label!r} in commands.json but not mentioned in the FAQ")

# The FAQ states a rounded floor ("over N commands"); reality must stay above it.
count = cmds["commandCount"]
fm = re.search(r'over\s+([\d,]+)\s+commands', faq)
if fm:
    floor = int(fm.group(1).replace(",", ""))
    if count < floor:
        problems.append(f"FAQ claims 'over {fm.group(1)} commands' but commands.json has only {count}")

if problems:
    print("  FAIL: product/count documentation drift:", file=sys.stderr)
    for x in problems:
        print(f"    - {x}", file=sys.stderr)
    sys.exit(1)
print(f"  PASS: {len(products)} products documented; FAQ floor <= {count} commands")
PY
fi

echo "==> Pro pillar coverage in $SITE_DIR/catalog.js"
if [[ ! -f "$SITE_DIR/catalog.js" ]]; then
  fail "$SITE_DIR/catalog.js missing"
elif [[ ! -s "$COMMANDS_JSON" ]]; then
  fail "skipped — commands.json missing or empty"
else
  python3 - "$COMMANDS_JSON" "$SITE_DIR/catalog.js" <<'PY' || ERRORS=$((ERRORS + 1))
import json, re, sys
data = json.load(open(sys.argv[1]))
catalog = open(sys.argv[2]).read()
m = re.search(r"var\s+PILLARS\s*=\s*\[(.*?)\];", catalog, re.DOTALL)
if not m:
    print("  FAIL: could not locate `var PILLARS = [...]` in catalog.js", file=sys.stderr)
    sys.exit(1)
pillar_groups = set(re.findall(r"'([^']+)'", m.group(1)))
pro_groups = {
    c.get("group")
    for c in data["commands"]
    if c.get("product") == "pro" and c.get("group")
}
missing = pro_groups - pillar_groups
if missing:
    print(f"  FAIL: Pro groups not assigned to a pillar in catalog.js:", file=sys.stderr)
    for g in sorted(missing):
        print(f"    - {g!r}", file=sys.stderr)
    print("  Add them to docs/site/catalog.js's PILLARS array, or document why they're pillar-less.", file=sys.stderr)
    sys.exit(1)
print(f"  PASS: all {len(pro_groups)} Pro groups covered by pillars")
PY
fi

echo ""
if [[ $ERRORS -gt 0 ]]; then
  echo "Error: $ERRORS site-output check(s) failed."
  exit 1
fi
echo "All site-output checks passed."
