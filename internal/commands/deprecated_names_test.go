// Copyright 2026, Jamf Software LLC

package commands

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// proChildren returns every resource command under `pro`, by name, including
// the names reachable through an alias.
func proChildren(t *testing.T) (byName map[string]*cobra.Command, byAlias map[string]*cobra.Command) {
	t.Helper()
	root := NewRootCmd("test", "test", "test", "test")
	var pro *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "pro" {
			pro = c
		}
	}
	if pro == nil {
		t.Fatal("the root command ships no `pro` namespace")
	}
	byName = map[string]*cobra.Command{}
	byAlias = map[string]*cobra.Command{}
	for _, c := range pro.Commands() {
		byName[c.Name()] = c
		for _, a := range c.Aliases {
			byAlias[a] = c
		}
	}
	return byName, byAlias
}

// The aliases are temporary and this is what makes them so. A comment saying
// "remove after March" is how dead code lives for years; the build failing on
// the date is the only mechanism that actually removes it.
//
// When this fires, delete deprecated_names.go, its wiring in pro.go and
// root.go, this file, and generator/parser/testdata/endpoints-before-path-grouping.tsv
// — the baseline exists to serve the same migration.
func TestDeprecatedNamesHaveNotExpired(t *testing.T) {
	expired, names := deprecatedNamesExpired(time.Now())
	if !expired {
		return
	}
	sort.Strings(names)
	t.Errorf("the deprecated `pro` resource names expired on %s — remove all %d of them and the machinery that serves them:\n  %s",
		deprecatedNamesRemovedAfter, len(names), strings.Join(names, "\n  "))
}

// The clock is what the guard turns on, so it has to be exercised rather than
// trusted: a date that never compares greater would make the test above
// permanently silent.
func TestDeprecatedNamesExpiryFiresOnTheDate(t *testing.T) {
	deadline, err := time.Parse(time.DateOnly, deprecatedNamesRemovedAfter)
	if err != nil {
		t.Fatalf("deprecatedNamesRemovedAfter is not a date: %v", err)
	}
	if expired, _ := deprecatedNamesExpired(deadline.Add(-24 * time.Hour)); expired {
		t.Error("reported expired the day before the deadline")
	}
	if expired, names := deprecatedNamesExpired(deadline.Add(48 * time.Hour)); !expired {
		t.Error("did not report expired two days after the deadline")
	} else if len(names) != len(deprecatedNames)+len(withdrawnNames) {
		t.Errorf("named %d entries to remove, want all %d", len(names), len(deprecatedNames)+len(withdrawnNames))
	}
}

// Every replacement has to name a command that ships, and every old name has to
// have stopped being one — an entry for a name still in use would make cobra
// ambiguous, and one pointing nowhere is a redirect into a wall.
func TestDeprecatedNamesPointAtCommandsThatShip(t *testing.T) {
	byName, byAlias := proChildren(t)
	for old, dep := range deprecatedNames {
		if _, ok := byName[dep.Now]; !ok {
			t.Errorf("deprecatedNames[%q] points at %q, which `pro` does not ship", old, dep.Now)
		}
		if _, ok := byName[old]; ok {
			t.Errorf("deprecatedNames[%q] is still a live command name; an alias for it makes cobra ambiguous", old)
		}
		if got := byAlias[old]; got == nil {
			t.Errorf("%q is in the table but reaches no command — applyDeprecatedNames did not wire it", old)
		} else if got.Name() != dep.Now {
			t.Errorf("%q resolves to %q, want %q", old, got.Name(), dep.Now)
		}
	}
	for old := range withdrawnNames {
		if _, ok := deprecatedNames[old]; ok {
			t.Errorf("%q is in both tables; a name is either redirected or refused, not both", old)
		}
		if _, ok := byName[old]; !ok {
			t.Errorf("withdrawnNames[%q] ships no stub, so the name fails as an unknown command", old)
		}
	}
}

// The completeness guard, and the one that matters: no resource name the CLI
// used to ship may simply vanish.
//
// Read from the committed pre-rename snapshot, because the parse that produced
// those names no longer exists. A name that is neither a live command, an
// alias, nor a withdrawal is a break with no migration path — which is exactly
// the state this branch was in before these aliases existed.
func TestEveryFormerResourceNameStillResolves(t *testing.T) {
	byName, byAlias := proChildren(t)

	f, err := os.Open(filepath.Join("..", "..", "generator", "parser", "testdata", "endpoints-before-path-grouping.tsv"))
	if err != nil {
		t.Fatalf("reading the pre-rename snapshot: %v\n"+
			"It records the resource names this CLI used to ship, and is what this guard compares against.", err)
	}
	defer func() { _ = f.Close() }()

	former := map[string]bool{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		former[strings.SplitN(line, "\t", 2)[0]] = true
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning the snapshot: %v", err)
	}
	if len(former) == 0 {
		t.Fatal("the snapshot names no resources; this guard cannot pass vacuously")
	}

	var orphaned []string
	for name := range former {
		switch {
		case byName[name] != nil, byAlias[name] != nil:
		case withdrawnNames[name] != "":
		default:
			orphaned = append(orphaned, name)
		}
	}
	sort.Strings(orphaned)
	if len(orphaned) > 0 {
		t.Errorf("%d former resource name(s) resolve to nothing — add each to deprecatedNames, or to withdrawnNames with the reason:\n  %s",
			len(orphaned), strings.Join(orphaned, "\n  "))
	}
	t.Logf("%d former names: %d live, %d aliased, %d withdrawn",
		len(former), countIn(former, byName), countIn(former, byAlias), len(withdrawnNames))
}

func countIn(names map[string]bool, in map[string]*cobra.Command) int {
	n := 0
	for name := range names {
		if in[name] != nil {
			n++
		}
	}
	return n
}

// A split gave its name to one half, so the other half is reachable only by its
// own name. Pinned because it is the one shape an alias cannot fully cover, and
// a reader of the table needs to know which way each went.
func TestDeprecatedNamesGiveASplitNameToTheParent(t *testing.T) {
	byName, _ := proChildren(t)
	// Only the splits whose name actually moved. `jcds` and
	// `computer-inventory-collection-settings` kept their own names for one half
	// and so need no alias at all.
	for old, other := range map[string]string{
		"app-requests":       "app-request-form-input-fields",
		"inventory-preloads": "inventory-preload-records",
		"log-flushings":      "log-flushing-task",
		"schedulers":         "scheduler-jobs",
	} {
		dep, ok := deprecatedNames[old]
		if !ok {
			t.Errorf("%q has no alias entry", old)
			continue
		}
		if len(dep.Now) >= len(other) {
			t.Errorf("%q inherited %q, but %q is the shorter name and should be the parent", old, dep.Now, other)
		}
		if byName[other] == nil {
			t.Errorf("the other half of the %q split, %q, ships no command", old, other)
		}
	}
}

// productToken finds the namespace whose next argument names the resource, and
// getting it wrong means warning about the wrong thing or not at all.
func TestProductTokenAndResourceToken(t *testing.T) {
	root := NewRootCmd("test", "test", "test", "test")
	found, _, err := root.Find([]string{"pro", "icon", "get"})
	if err != nil {
		t.Fatalf("resolving pro icon get: %v", err)
	}
	if got := productToken(found); got != "pro" {
		t.Errorf("productToken = %q, want pro", got)
	}
	if got := productToken(root); got != "" {
		t.Errorf("productToken(root) = %q, want empty", got)
	}

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"jamf-cli", "pro", "icons", "get", "1"}, "icons"},
		{[]string{"jamf-cli", "-p", "prof", "pro", "icons", "get"}, "icons"},
		{[]string{"jamf-cli", "pro", "--quiet", "icons", "get"}, "icons"},
		{[]string{"jamf-cli", "pro"}, ""},
		{[]string{"jamf-cli", "protect", "plans", "list"}, ""},
	} {
		if got := resourceTokenAfter(tc.args, "pro"); got != tc.want {
			t.Errorf("resourceTokenAfter(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

// pro.go wires handwritten commands onto generated parents by name, and
// addSubcommand/removeSubcommand/replaceSubcommand all no-op silently when the
// parent is not found. So a renamed resource takes the wiring with it and
// nothing reports it.
//
// This is not hypothetical: naming resources after their OpenAPI tag moved four
// of those parents, and `computer-inventory` changed meaning entirely — it had
// been the stray erase/remove-mdm pair and became the primary computer
// resource, so a suppression written for the former would have deleted
// `pro comp list`. Every one of those was silent.
func TestProWiringNamesCommandsThatShip(t *testing.T) {
	src, err := os.ReadFile("pro.go")
	if err != nil {
		t.Fatalf("reading pro.go: %v", err)
	}
	byName, byAlias := proChildren(t)

	// Every parent path passed to the wiring helpers, as a single-element
	// []string{"name"} literal. A deeper path is nested and out of scope here.
	//
	// Whole-resource removals — removeSubcommand(cmd, []string{}, "x") — are
	// deliberately not checked. The name is absent from the tree afterwards
	// whether the removal worked or the name was stale, so the assembled tree
	// cannot tell the two apart; catching a stale one needs the resource set the
	// generator produced, which this package cannot see.
	parents := regexp.MustCompile(`(?:add|remove|replace)Subcommand\(cmd, \[\]string\{"([a-z0-9-]+)"\}`)

	seen := 0
	for _, m := range parents.FindAllStringSubmatch(string(src), -1) {
		seen++
		if byName[m[1]] == nil && byAlias[m[1]] == nil {
			t.Errorf("pro.go wires onto parent %q, which `pro` does not ship — the wiring is silently discarded", m[1])
		}
	}
	if seen == 0 {
		t.Fatal("found no wiring calls in pro.go; the pattern stopped matching and this guard is vacuous")
	}
	t.Logf("checked %d wiring targets", seen)
}

// A withdrawal message points the caller somewhere, and pointing them at a
// command that does not exist is worse than the bare "unknown command" it
// replaced — it reads as authoritative.
//
// Both messages were stale when this was written: they named
// `pro team-viewer-remote-administrations` and
// `pro computers-inventory redeploy-framework`, the pre-rename forms. Prose is
// exactly what a rename does not update.
func TestWithdrawnNameMessagesNameCommandsThatShip(t *testing.T) {
	src, err := os.ReadFile("deprecated_names.go")
	if err != nil {
		t.Fatalf("reading deprecated_names.go: %v", err)
	}
	start := strings.Index(string(src), "var withdrawnNames = map[string]string{")
	if start < 0 {
		t.Fatal("withdrawnNames is gone; delete this guard with it")
	}
	block := string(src)[start:]
	if end := strings.Index(block, "\n}\n"); end > 0 {
		block = block[:end]
	}

	root := NewRootCmd("test", "test", "test", "test")
	quoted := regexp.MustCompile("`(pro [a-z0-9 -]+)`")
	found := 0
	for _, m := range quoted.FindAllStringSubmatch(block, -1) {
		found++
		args := strings.Fields(m[1])[1:] // drop the binary-relative "pro"
		cmd, _, err := root.Find(append([]string{"pro"}, args...))
		if err != nil || cmd == nil || cmd.CommandPath() != "jamf-cli "+m[1] {
			got := "not found"
			if cmd != nil {
				got = cmd.CommandPath()
			}
			t.Errorf("a withdrawal message names `%s`, which resolves to %q", m[1], got)
		}
	}
	if found == 0 {
		t.Error("no command names found in the withdrawal messages; the pattern stopped matching")
	}
}
