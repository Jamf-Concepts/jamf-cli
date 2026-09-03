#!/usr/bin/env bash
# Fetch the SDK's published api/ specs from GitHub into a drop directory.
#
# jamfplatform-go-sdk's api/ is the source of truth for every spec this repo
# ingests: it is the only place they are normalised and wire-verified against a
# live tenant, and the SDK is always updated before this repo ingests. So the
# default source is the repo itself rather than a checkout somebody has to keep
# current — a stale local clone is how a spec drifts, and the compliance-benchmark
# case (a renamed query parameter answering 0 rules for every baseline, with no
# error) is what that costs.
#
# Usage: fetch-sdk-specs.sh <owner/repo> <ref> <dest-dir> <file>...
#
# Prints the short commit SHA the specs were taken from on stdout, and nothing
# else there — the caller records it as the derived manifests' provenance.
# Progress goes to stderr.
set -euo pipefail

if [ "$#" -lt 4 ]; then
  echo "usage: $0 <owner/repo> <ref> <dest-dir> <file>..." >&2
  exit 2
fi

repo=$1; ref=$2; dest=$3; shift 3

# GitHub's API is authenticated when a token happens to be in the environment
# (60 requests/hour unauthenticated is ample for one call, but a shared CI IP
# can exhaust it), and anonymous otherwise. Never required: the repo is public.
#
# The array is seeded with the flags every request wants rather than left empty,
# because macOS ships bash 3.2, where `"${a[@]}"` on an empty array is an
# unbound-variable error under `set -u` — the failure surfaces as an unrelated
# JSON parse error from the pipeline downstream.
curl_args=(-fsSL --retry 2)
token=${GITHUB_TOKEN:-${GH_TOKEN:-}}
if [ -n "$token" ]; then
  curl_args+=(-H "Authorization: Bearer $token")
fi

# Resolve the ref to a commit SHA and download at that SHA, never at the ref.
# A branch can move between the resolution and the last download, which would
# leave a drop assembled from two revisions and a provenance stamp naming
# neither — the failure would be a silent spec mismatch, not an error.
if [[ $ref =~ ^[0-9a-f]{40}$ ]]; then
  sha=$ref
else
  sha=$(curl "${curl_args[@]}" \
    -H "Accept: application/vnd.github+json" \
    "https://api.github.com/repos/$repo/commits/$ref" |
    python3 -c 'import json,sys; print(json.load(sys.stdin)["sha"])' 2>/dev/null) || {
    echo "Error: cannot resolve $repo@$ref — check the repository exists and the ref is a branch, tag or full SHA" >&2
    exit 1
  }
fi

mkdir -p "$dest"
for f in "$@"; do
  url="https://raw.githubusercontent.com/$repo/$sha/api/$f"
  tmp=$(mktemp)
  if ! curl "${curl_args[@]}" "$url" -o "$tmp"; then
    rm -f "$tmp"
    echo "Error: $f not found at $repo@${sha:0:7} ($url)" >&2
    exit 1
  fi
  # A spec that is not JSON means the download succeeded on something that is
  # not the file — a redirect to an HTML page, or a truncated body. Both would
  # reach the generator as a parse error pages away from the cause.
  if ! python3 -c 'import json,sys; json.load(open(sys.argv[1]))' "$tmp" 2>/dev/null; then
    rm -f "$tmp"
    echo "Error: $f downloaded from $url is not valid JSON" >&2
    exit 1
  fi
  mv "$tmp" "$dest/$f"
  chmod 644 "$dest/$f"
  echo "  fetched $f" >&2
done

echo "  from $repo@$ref (${sha:0:7})" >&2
echo "${sha:0:7}"
