#!/usr/bin/env python3
"""Report which mutating gateway requests this CLI sends are covered by a Tyk audit rule.

The gateway's audit rules are path globs, and a request that is routed but
unaudited succeeds silently — the mutation happens and nothing records it. That
failure mode already bit once: Security Cloud URLs were version-first, which
matched no glob, so 19 of 27 mutating operations executed unaudited for weeks
(docs/solutions/logic-errors/securitycloud-tenant-first-url-audit-2026-08-21.md).

The scope has since moved into an X-Tenant-Id header and every generated path
changed shape, so coverage has to be recomputed rather than assumed. This script
does that by simulating the real matcher rather than reasoning about it:

  - glob semantics from jamf/tyk-custom-plugins/plugins/audit-plugin/rules.go
    (CompileGlob: "**" -> ".+", "*" -> "[^/]+", everything else literal, anchored)
  - the matched path is stripListenPath(request path, listen_path), i.e. the raw
    inbound path with the product's listen_path removed, before any rewriting
  - request paths are read out of the generated commands, so this measures what
    the CLI actually sends

Usage:
    python3 scripts/audit-coverage.py [path-to-tyk-gateway-management] [git-ref]

Defaults to /Users/Shared/GitHub/jamf/tyk-gateway-management at HEAD. Pass a ref
when the local checkout is behind the deployed state — a stale checkout is the
easiest way to draw the wrong conclusion here, and the script prints which ref it
read so the answer is attributable.

Exits non-zero when any mutating request the CLI sends is unaudited.
"""

import os
import re
import subprocess
import sys
from glob import glob

DEFAULT_TYK = "/Users/Shared/GitHub/jamf/tyk-gateway-management"
GENERATED = "internal/commands/platform/generated/*.go"


def compile_glob(pattern):
    """Mirror audit-plugin's CompileGlob."""
    out = ["^"]
    i = 0
    while i < len(pattern):
        c = pattern[i]
        if c == "*":
            if i + 1 < len(pattern) and pattern[i + 1] == "*":
                out.append(".+")  # one-or-more: never matches empty
                i += 2
                continue
            out.append("[^/]+")
            i += 1
            continue
        out.append(re.escape(c))
        i += 1
    out.append("$")
    return re.compile("".join(out))


def api_blocks(text):
    """Return [(listen_path, [(methods, glob)])] for every API in one document.

    One document holds several APIs — jamf-pro declares /api/pro and
    /api/proclassic, compliance-benchmarks declares five — so rules have to be
    attributed to the API whose listen_path precedes them. Parsing the file as a
    whole lumps one API's globs onto another and reports coverage that does not
    exist.
    """
    lines = text.splitlines()
    starts = [i for i, line in enumerate(lines) if re.match(r"\s*listen_path:\s*\S", line)]
    blocks = []
    for n, start in enumerate(starts):
        end = starts[n + 1] if n + 1 < len(starts) else len(lines)
        listen = re.match(r"\s*listen_path:\s*(\S+)", lines[start]).group(1).strip("\"'")
        rules, pending = [], None
        for line in lines[start:end]:
            m = re.search(r"-?\s*methods:\s*\[([^\]]*)\]", line)
            if m:
                pending = [x.strip() for x in m.group(1).split(",") if x.strip()]
            g = re.search(r"path_glob:\s*(\S+)", line)
            if g and pending is not None:
                rules.append((pending, g.group(1).strip("\"'")))
                pending = None
        blocks.append((listen, rules))
    return blocks


def cli_mutating_requests():
    """Every non-GET (method, path) a generated platform command sends."""
    found = set()
    for path in glob(GENERATED):
        src = open(path).read()
        for block in re.split(r"\nfunc new", src)[1:]:
            p = re.search(r'path := "([^"]+)"', block)
            m = re.search(r"http\.Method(Get|Post|Put|Patch|Delete)", block)
            if not p or not m:
                continue
            method = m.group(1).upper()
            if method == "GET":
                continue
            found.add((method, p.group(1)))
    return sorted(found)


def definitions(tyk_root, ref):
    """Yield (label, text) for every prod api-definition, from a ref or the worktree."""
    rel = "prod/api-products"
    if ref:
        listing = subprocess.run(
            ["git", "-C", tyk_root, "ls-tree", "-r", "--name-only", ref, rel],
            capture_output=True, text=True, check=True,
        ).stdout.split()
        for name in listing:
            if not name.endswith((".yaml", ".yml")):
                continue
            text = subprocess.run(
                ["git", "-C", tyk_root, "show", f"{ref}:{name}"],
                capture_output=True, text=True, check=True,
            ).stdout
            yield name, text
        return
    for name in sorted(glob(os.path.join(tyk_root, rel, "*", "*.y*ml"))):
        yield os.path.relpath(name, tyk_root), open(name).read()


def main():
    tyk_root = sys.argv[1] if len(sys.argv) > 1 else DEFAULT_TYK
    ref = sys.argv[2] if len(sys.argv) > 2 else "HEAD"
    if not os.path.isdir(tyk_root):
        sys.exit(f"no tyk-gateway-management checkout at {tyk_root}")

    requests = cli_mutating_requests()
    if not requests:
        sys.exit("no mutating requests found: is the generated code present?")

    # (document, listen_path, rules) for every API in every prod api-definition.
    apis = []
    for name, text in definitions(tyk_root, ref):
        for listen, rules in api_blocks(text):
            apis.append((name, listen, rules))

    print(f"tyk-gateway-management: {tyk_root} @ {ref or 'worktree'}")
    print(f"CLI mutating requests:  {len(requests)}\n")

    gaps, unruled, covered = [], [], []
    for method, path in requests:
        serving = [(n, lp, rs) for n, lp, rs in apis
                   if lp.rstrip("/") and path.startswith(lp.rstrip("/") + "/")]
        if not serving:
            gaps.append((method, path, "no api-definition declares a listen_path for this namespace"))
            continue
        # A product either audits or it does not. Only a product that HAS rules
        # and matches none of them is a gap this repo can cause: that is the
        # shape-of-the-URL failure. A product with no rules at all audits nothing
        # whatever the CLI sends, which is an upstream decision, not a bug here.
        with_rules = [(n, lp, rs) for n, lp, rs in serving if rs]
        if not with_rules:
            unruled.append((method, path))
            continue
        missed = []
        for name, listen, rules in with_rules:
            match_path = path[len(listen.rstrip("/")):]
            if not any(method in ms and compile_glob(g).match(match_path) for ms, g in rules):
                missed.append(name)
        if missed:
            gaps.append((method, path, f"declared audit rules match nothing in: {', '.join(sorted(missed))}"))
        else:
            covered.append((method, path))

    if covered:
        print(f"audited: {len(covered)} request(s) matched an audit rule in every region document that serves them")
    if unruled:
        products = sorted({p.split("/")[2] for _, p in unruled})
        print(f"not audited upstream: {len(unruled)} request(s) on products that declare no audit rules at all")
        print(f"  ({', '.join(products)}) — nothing this repo sends changes that")
    if gaps:
        print("\nGAPS — the product audits, but these mutations match no rule:\n")
        for method, path, why in gaps:
            print(f"  {method:6s} {path}\n         {why}")
        print(f"\n{len(gaps)} gap(s)")
        return 1
    print("\nNo gaps: every mutation on an auditing product is covered.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
