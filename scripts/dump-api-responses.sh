#!/usr/bin/env bash
#
# dump-api-responses.sh — Hit every GET endpoint on a live Jamf Pro instance
# and save the JSON responses to a timestamped directory.
#
# Usage:
#   ./scripts/dump-api-responses.sh [--profile <name>] [--output-dir <dir>] [--limit <n>]
#
# Prerequisites:
#   - jamfpro-cli must be built and on PATH (or ./jamfpro-cli in project root)
#   - A configured profile with valid credentials
#
# What it does:
#   1. Auto-discovers every "list" subcommand from the CLI itself
#   2. Runs each one with --limit to cap results
#   3. Saves each response as <resource>__list.json
#   4. Writes a summary index.json and prints a report
#
# Examples:
#   ./scripts/dump-api-responses.sh                      # default profile, 2 results each
#   ./scripts/dump-api-responses.sh -p prod --limit 5    # named profile, 5 results each
#   ./scripts/dump-api-responses.sh --output-dir ./dump   # custom output directory

set -euo pipefail

# ── Defaults ─────────────────────────────────────────────────────────────
PROFILE=""
LIMIT=2
OUTPUT_DIR=""
CLI=""

# ── Parse arguments ──────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
    case "$1" in
        --profile|-p)  PROFILE="$2"; shift 2 ;;
        --output-dir)  OUTPUT_DIR="$2"; shift 2 ;;
        --limit)       LIMIT="$2"; shift 2 ;;
        --help|-h)
            sed -n '2,/^$/s/^# //p' "$0"
            exit 0
            ;;
        *) echo "Unknown flag: $1" >&2; exit 1 ;;
    esac
done

# ── Locate CLI binary (prefer local build over PATH) ────────────────────
for candidate in ./jamfpro-cli ./bin/jamfpro-cli jamfpro-cli; do
    if [[ -x "$candidate" ]] || command -v "$candidate" &>/dev/null; then
        CLI="$candidate"
        break
    fi
done

if [[ -z "$CLI" ]]; then
    echo "Error: jamfpro-cli not found. Build it first:" >&2
    echo "  go build -o jamfpro-cli ./cmd/jamfpro-cli" >&2
    exit 1
fi

# ── Build profile flag ──────────────────────────────────────────────────
PROFILE_FLAG=()
if [[ -n "$PROFILE" ]]; then
    PROFILE_FLAG=(--profile "$PROFILE")
fi

# ── Create output directory ─────────────────────────────────────────────
if [[ -z "$OUTPUT_DIR" ]]; then
    OUTPUT_DIR="api-responses-$(date +%Y%m%d-%H%M%S)"
fi
mkdir -p "$OUTPUT_DIR/modern" "$OUTPUT_DIR/classic"

echo "=== Jamf Pro API Response Dump ==="
echo "CLI:        $CLI"
echo "Profile:    ${PROFILE:-<default>}"
echo "Limit:      $LIMIT results per list endpoint"
echo "Output:     $OUTPUT_DIR/"
echo ""

# ── Verify connectivity ─────────────────────────────────────────────────
echo -n "Verifying connection... "
if ! $CLI "${PROFILE_FLAG[@]}" jamf-pro-versions list -o json &>/dev/null; then
    echo "FAILED"
    echo "Error: Cannot connect. Check your profile/credentials." >&2
    exit 1
fi
echo "OK"
echo ""

# ── Discover all list commands ───────────────────────────────────────────
echo -n "Discovering API commands... "
ALL_COMMANDS=$($CLI commands -o json --wide 2>/dev/null)

LIST_COMMANDS=$(echo "$ALL_COMMANDS" | python3 -c "
import sys, json
cmds = json.load(sys.stdin)
for c in sorted(cmds, key=lambda x: x['command']):
    cmd = c['command']
    # Only list subcommands, skip 'config list' (not an API endpoint)
    if cmd.endswith(' list') and not cmd.startswith('config '):
        print(cmd)
")

CMD_COUNT=$(echo "$LIST_COMMANDS" | wc -l | tr -d ' ')
echo "found $CMD_COUNT list endpoints"
echo ""

# ── Track results ────────────────────────────────────────────────────────
TOTAL=0
SUCCESS=0
FAILED=0
EMPTY=0
INDEX_FILE="$OUTPUT_DIR/index.json"

# Start JSON array
echo "[" > "$INDEX_FILE"
FIRST_ENTRY=true

run_command() {
    local cmd="$1"
    local resource subcmd outdir outfile
    resource=$(echo "$cmd" | awk '{print $1}')
    subcmd=$(echo "$cmd" | awk '{print $2}')

    # Route classic- commands to classic/ subdirectory
    if [[ "$resource" == classic-* ]]; then
        outdir="$OUTPUT_DIR/classic"
    else
        outdir="$OUTPUT_DIR/modern"
    fi
    outfile="$outdir/${resource}__${subcmd}.json"

    TOTAL=$((TOTAL + 1))

    # Only pass --limit if the command supports it (check its flags)
    local limit_flag=""
    if $CLI $cmd --help 2>/dev/null | grep -q -- '--limit'; then
        limit_flag="--limit $LIMIT"
    fi

    local stderr_file
    stderr_file=$(mktemp)

    local output="" exit_code=0
    output=$($CLI "${PROFILE_FLAG[@]}" $cmd -o json $limit_flag 2>"$stderr_file") || exit_code=$?

    if [[ $exit_code -ne 0 ]]; then
        # CLI error — capture the message from stdout (CLI writes errors there)
        local err
        err=$(echo "$output" | python3 -c "
import sys, json
try:
    d = json.load(sys.stdin)
    print(d.get('message', d.get('error', str(d))))
except:
    print(sys.stdin.read().strip() if False else 'exit code $exit_code')
" 2>/dev/null || echo "exit code $exit_code")
        # If output was empty, check stderr for the error
        if [[ -z "$err" || "$err" == "exit code $exit_code" ]]; then
            err=$(grep -v "^Warning:" "$stderr_file" | head -1)
            [[ -z "$err" ]] && err="exit code $exit_code"
        fi
        FAILED=$((FAILED + 1))
        printf "  %-65s %s\n" "$cmd" "FAIL: ${err:0:60}"
        echo "$output" > "$outfile"
        write_index_entry "$cmd" "$outfile" "failed" "" "$err"
    elif [[ -z "$output" || "$output" == "null" || "$output" == "[]" || "$output" == "{}" ]]; then
        echo "$output" > "$outfile"
        EMPTY=$((EMPTY + 1))
        printf "  %-65s %s\n" "$cmd" "EMPTY"
        write_index_entry "$cmd" "$outfile" "empty" "" ""
    else
        echo "$output" > "$outfile"
        local count
        count=$(echo "$output" | python3 -c "
import sys, json
try:
    d = json.load(sys.stdin)
    if isinstance(d, list):
        print(len(d))
    elif isinstance(d, dict) and 'results' in d:
        print(len(d['results']))
    elif isinstance(d, dict) and 'totalCount' in d:
        print(d['totalCount'])
    else:
        print('object')
except:
    print('?')
" 2>/dev/null || echo "?")
        SUCCESS=$((SUCCESS + 1))
        printf "  %-65s %s\n" "$cmd" "OK ($count)"
        write_index_entry "$cmd" "$outfile" "ok" "$count" ""
    fi

    rm -f "$stderr_file"
}

write_index_entry() {
    local cmd="$1" file="$2" status="$3" count="$4" error="$5"

    if [[ "$FIRST_ENTRY" == "true" ]]; then
        FIRST_ENTRY=false
    else
        echo "," >> "$INDEX_FILE"
    fi

    # Escape double quotes in error messages
    error=$(echo "$error" | sed 's/"/\\"/g')

    local entry="{\"command\":\"$cmd\",\"file\":\"$(basename "$(dirname "$file")")/$(basename "$file")\",\"status\":\"$status\""
    [[ -n "$count" ]] && entry="$entry,\"count\":\"$count\""
    [[ -n "$error" ]] && entry="$entry,\"error\":\"$error\""
    entry="$entry}"

    printf "  %s" "$entry" >> "$INDEX_FILE"
}

# ── Run all list commands ────────────────────────────────────────────────
echo "── Fetching API responses ─────────────────────────────────────────"
echo ""

while IFS= read -r cmd; do
    run_command "$cmd"
done <<< "$LIST_COMMANDS"

# ── Close index ──────────────────────────────────────────────────────────
echo "" >> "$INDEX_FILE"
echo "]" >> "$INDEX_FILE"

# ── Summary ──────────────────────────────────────────────────────────────
echo ""
echo "=== Summary ==="
echo "  Total endpoints:  $TOTAL"
echo "  Successful:       $SUCCESS"
echo "  Empty response:   $EMPTY"
echo "  Failed:           $FAILED"
echo "  Output directory: $OUTPUT_DIR/"
echo ""

if [[ $FAILED -gt 0 ]]; then
    echo "Failed endpoints:"
    python3 -c "
import json, sys
idx = json.load(open('$INDEX_FILE'))
for e in idx:
    if e['status'] == 'failed':
        print(f\"  {e['command']}: {e.get('error', 'unknown')}\")
" 2>/dev/null || true
    echo ""
fi

echo "Explore results:"
echo "  ls $OUTPUT_DIR/modern/   # Modern API responses"
echo "  ls $OUTPUT_DIR/classic/  # Classic API responses"
echo "  cat $OUTPUT_DIR/index.json | python3 -m json.tool"
echo ""
echo "Quick peek:"
echo "  cat $OUTPUT_DIR/modern/computers__list.json | python3 -m json.tool | head -60"
