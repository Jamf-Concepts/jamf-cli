// Copyright 2026, Jamf Software LLC

package commands

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The root CLAUDE.md is an index: it carries almost no content itself and points
// at the rules, skills, subdirectory CLAUDE.md files, and guides that hold the
// real material. A pointer that names a file which does not exist is worse than
// no pointer — the reader (human or AI session) is told the content lives
// somewhere it does not, and the CRITICAL credential policy in particular is
// inert if its @-import target is missing. The restructure that split CLAUDE.md
// into `.claude/` left exactly such a dead pointer (`.claude/skills/dev-commands.md`
// where the file is `.claude/skills/dev-commands/SKILL.md`), which is what this
// guard exists to stop recurring.
//
// It extracts paths from CLAUDE.md rather than keeping a hand-maintained list, so
// a newly-added pointer is checked the moment it lands — the failure mode is a
// broken pointer, not a stale test.

// claudeMDPath is the root index, relative to this package.
const claudeMDPath = "../../CLAUDE.md"

// docSearchBases are the directories a bare path token may be relative to. Most
// tokens are repo-root-relative; the exception is the `docs/solutions/` bullet,
// which names example files (`conventions/…`, `design-patterns/…`) relative to
// that directory.
var docSearchBases = []string{"../..", "../../docs/solutions"}

// backtickToken matches a `…`-quoted span; slashPathToken filters those down to
// the ones that look like a path (contain a separator, no whitespace, not a URL).
var (
	backtickToken  = regexp.MustCompile("`([^`]+)`")
	importLineRe   = regexp.MustCompile(`(?m)^@(\S+)\s*$`)
	looksLikeAPath = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
)

func TestEveryPathInRootCLAUDEmdResolves(t *testing.T) {
	body, err := os.ReadFile(claudeMDPath)
	if err != nil {
		t.Fatalf("reading %s: %v", claudeMDPath, err)
	}
	text := string(body)

	// @-imports are the load-bearing ones: Claude Code reads each named file into
	// every session. A missing target silently drops that content — for
	// credentials-and-auth.md that is the CRITICAL policy going unenforced.
	imports := importLineRe.FindAllStringSubmatch(text, -1)
	if len(imports) == 0 {
		t.Error("CLAUDE.md declares no @-imports; the always-loaded rules are no " +
			"longer wired in — the CRITICAL credential policy would not load")
	}
	for _, m := range imports {
		rel := m[1]
		p := filepath.Join("../..", rel)
		info, err := os.Stat(p)
		if err != nil {
			t.Errorf("@-import %q does not resolve (%v); the file it loads every "+
				"session is missing", rel, err)
			continue
		}
		if info.IsDir() {
			t.Errorf("@-import %q is a directory; an import must name a file", rel)
		}
	}

	// Every backtick token that looks like a path must resolve. Directory tokens
	// (trailing slash) resolve to a directory; everything else to a file or dir.
	seen := map[string]bool{}
	for _, m := range backtickToken.FindAllStringSubmatch(text, -1) {
		tok := m[1]
		if !strings.Contains(tok, "/") || !looksLikeAPath.MatchString(tok) {
			continue
		}
		if seen[tok] {
			continue
		}
		seen[tok] = true

		wantDir := strings.HasSuffix(tok, "/")
		clean := strings.TrimSuffix(tok, "/")

		resolved := false
		for _, base := range docSearchBases {
			info, err := os.Stat(filepath.Join(base, clean))
			if err != nil {
				continue
			}
			if wantDir && !info.IsDir() {
				continue
			}
			resolved = true
			break
		}
		if !resolved {
			kind := "file or directory"
			if wantDir {
				kind = "directory"
			}
			t.Errorf("CLAUDE.md names %q but no such %s exists (searched %v) — "+
				"a pointer to a missing path misdirects every reader that follows it",
				tok, kind, docSearchBases)
		}
	}

	// The loop above fails open: it iterates backtick tokens, so stripping every
	// backtick from CLAUDE.md — a reformat to markdown links, a table, or plain
	// prose — leaves it passing while the half covering ~20 pointers is silently
	// disabled. The @-import loop above carries the same guard; this mirrors it,
	// with a floor the file plainly clears today.
	const minPathPointers = 10
	if len(seen) < minPathPointers {
		t.Fatalf("only %d path pointers found in CLAUDE.md, want at least %d — "+
			"the check iterates backtick-quoted tokens, so a reformat away from "+
			"backticks disables it silently rather than failing",
			len(seen), minPathPointers)
	}
}
