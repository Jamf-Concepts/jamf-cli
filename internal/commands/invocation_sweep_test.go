// Copyright 2026, Jamf Software LLC

package commands

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// invocationBaselineFile is every `pro` command path the CLI shipped before
// spec-derived naming, with the shape it had then and the shape it has now.
//
// A snapshot, for the same reason
// generator/parser/testdata/endpoints-before-path-grouping.tsv is one: the tree
// it describes no longer exists, so a redesign that removes its own baseline
// has to carry it. Delete it with the deprecation aliases.
const invocationBaselineFile = "testdata/pro-invocations-before-nesting.tsv"

// TestEveryFormerInvocationKeepsItsShape is the sweep that found the six
// override defects on this branch, committed so the next spec drop cannot
// repeat them silently.
//
// TestEveryFormerResourceNameStillResolves covers resource *names*. It cannot
// see an operation rename, and the sub-resource split is entirely operation
// renames — `pro sso-settings delete` became `pro sso-settings cert delete` —
// so the invocations a script actually types were unguarded.
//
// **Resolution is not the property; shape is.** Two failure modes hide behind a
// resolution check, and both were live here:
//
//   - cobra's Find falls back to the deepest command it matched, so
//     `pro sso-settings delete` "resolves" to `pro sso-settings` with `delete`
//     left over. `len(rest) == 0` is what separates the two.
//   - A leaf can become a *group*. `pro csas token` returned the CSA token and
//     now resolves to the `pro csa token` parent, which prints help and exits
//     **0** — so a script piping it into jq gets usage text and no error. Three
//     invocations did that, and a resolution-only sweep called all three fine.
//
// So each row records `leaf`, `group`, `refused` or `gone`, and the current
// shape has to match. Any drift in either direction fails: a `gone` row that
// starts working is a name coming back, and a `leaf` row that stops is a
// command going away. Both belong in CHANGELOG.md's migration table, and
// neither reports itself.
//
// `refused` is the strongest of the four and is what movedInvocations produces:
// the path resolves to a stub that refuses and names where the operation went.
// It is deliberately not folded into `gone` — a row moving from `refused` back
// to `gone` is the pointer being lost, which is the whole value of the table,
// and a resolution-only check reads the two as identical.
//
// Do not probe by running the command with a bogus flag: cobra reports the flag
// error before the unknown subcommand, which reported 0 broken when 61 were.
func TestEveryFormerInvocationKeepsItsShape(t *testing.T) {
	f, err := os.Open(invocationBaselineFile)
	if err != nil {
		t.Fatalf("%s is a required committed artifact: %v", invocationBaselineFile, err)
	}
	defer func() { _ = f.Close() }()

	root := NewRootCmd("test", "none", "none", "none")
	var drifted []string
	counts := map[string]int{}
	total := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			t.Fatalf("malformed baseline row %q: want invocation, was, now", line)
		}
		invocation, was, want := strings.TrimSpace(fields[0]), strings.TrimSpace(fields[1]), strings.TrimSpace(fields[2])
		total++
		counts[was+"->"+want]++

		if got := invocationShape(root, invocation); got != want {
			drifted = append(drifted, fmt.Sprintf("%s: was %s, recorded as %s, is now %s", invocation, was, want, got))
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("reading %s: %v", invocationBaselineFile, err)
	}
	if total == 0 {
		t.Fatalf("%s holds no rows, so this test cannot pass vacuously", invocationBaselineFile)
	}
	// The three transitions that make this guard worth running. Asserted so a
	// baseline regenerated without them — the easy way to make a failure go
	// away — fails instead.
	for _, transition := range []string{"leaf->gone", "leaf->group", "leaf->refused"} {
		if counts[transition] == 0 {
			t.Errorf("no %s row survives, so the baseline no longer records the shape changes it exists for", transition)
		}
	}
	sort.Strings(drifted)
	if len(drifted) > 0 {
		t.Errorf("%d former invocation(s) changed shape without the baseline saying so. Update %s and "+
			"CHANGELOG.md's migration table together, or restore the command:\n  %s",
			len(drifted), invocationBaselineFile, strings.Join(drifted, "\n  "))
	}
	t.Logf("%d former invocations checked: %d migrated away, %d refused with a pointer, %d became groups",
		total, counts["leaf->gone"], counts["leaf->refused"], counts["leaf->group"])
}

// invocationShape reports what a command path resolves to today.
//
// `group` is decided by HasSubCommands rather than by Runnable, because
// guardUnknownSubcommands makes every parent runnable so a typo gets a refusal
// instead of cobra's bare usage — so Runnable() is true for all 235 groups and
// cannot separate them from a leaf.
func invocationShape(root *cobra.Command, invocation string) string {
	cmd, rest, err := root.Find(strings.Fields(invocation))
	if err != nil || cmd == nil || len(rest) != 0 {
		return "gone"
	}
	if cmd.HasSubCommands() {
		return "group"
	}
	if isMovedStub(cmd) {
		// Resolves, but only to say where the operation went. Reporting this as
		// `leaf` would mean a restored command and a refusal stub read the
		// same, which is the distinction the `refused` rows exist to hold.
		return "refused"
	}
	return "leaf"
}
