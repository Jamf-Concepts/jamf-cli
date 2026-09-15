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

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
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
	} else if want := len(deprecatedNames) + len(nestedAliases) + len(withdrawnNames); len(names) != want {
		t.Errorf("named %d entries to remove, want all %d", len(names), want)
	}
}

// The advance signal, and the one test in this repo that is meant to fail
// before anything is wrong.
//
// The hard guard above fires once, on whatever pull request happens to run CI
// on or after the removal date — and the work it forces is a real PR, not a
// one-line deletion. Go has no warning level for a test, so the notice is a
// separate failing test run **only from the scheduled workflow**: it never
// blocks a pull request, and it turns the weekly build red 60 days out with the
// list of what has to go.
//
// Skipped unless JAMF_CLI_EXPIRY_NOTICE is set, which .github/workflows/
// expiry-notice.yaml is the only thing that sets. Do not add it to `make test`.
func TestDeprecatedNamesExpiryNotice(t *testing.T) {
	if os.Getenv("JAMF_CLI_EXPIRY_NOTICE") == "" {
		t.Skip("advance notice runs from the scheduled workflow only; set JAMF_CLI_EXPIRY_NOTICE to run it")
	}
	expiring, left := deprecatedNamesExpiring(time.Now())
	if !expiring {
		t.Logf("%s is %d days away; nothing to start yet", deprecatedNamesRemovedAfter, int(left.Hours()/24))
		return
	}
	t.Errorf("the deprecated `pro` resource names expire on %s, in %d days — open the removal PR now.\n"+
		"It deletes deprecated_names.go, moved_invocations.go, their wiring in pro.go and root.go, their tests, "+
		"and generator/parser/testdata/endpoints-before-path-grouping.tsv: %d resource aliases, %d nested aliases, "+
		"%d withdrawn stubs, %d moved invocations and %d former-leaf groups.",
		deprecatedNamesRemovedAfter, int(left.Hours()/24),
		len(deprecatedNames), len(nestedAliases), len(withdrawnNames), len(movedInvocations), len(formerLeafGroups))
}

// The notice window is what the test above turns on, so it is exercised the
// same way the deadline is: a window that never opens would make the notice
// permanently silent, which is the failure it exists to prevent.
func TestDeprecatedNamesNoticeWindowOpensSixtyDaysAhead(t *testing.T) {
	deadline, err := time.Parse(time.DateOnly, deprecatedNamesRemovedAfter)
	if err != nil {
		t.Fatalf("deprecatedNamesRemovedAfter is not a date: %v", err)
	}
	if expiring, _ := deprecatedNamesExpiring(deadline.Add(-deprecatedNamesNoticePeriod - 24*time.Hour)); expiring {
		t.Error("opened the notice window a day before it should")
	}
	if expiring, left := deprecatedNamesExpiring(deadline.Add(-deprecatedNamesNoticePeriod + 24*time.Hour)); !expiring {
		t.Error("did not open the notice window inside the period")
	} else if days := int(left.Hours() / 24); days != 59 {
		t.Errorf("reported %d days left, want 59", days)
	}
	// Past the date the hard guard is the one with something to say, so the
	// notice goes quiet rather than adding a second failure for one cause.
	if expiring, _ := deprecatedNamesExpiring(deadline.Add(48 * time.Hour)); expiring {
		t.Error("still noticing after the deadline; deprecatedNamesExpired owns that case")
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

	// A flag between the product token and the resource token used to yield the
	// flag's value: `pro -p ci-svc icons get` answered "ci-svc", which silenced
	// the deprecation warning for every retired name and left the moved-verb
	// refusal unreachable. Cobra does not require a global flag before the
	// subcommand, so every placement below is an ordinary invocation and each
	// one has to answer the same.
	flags := visibleFlags(found)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"jamf-cli", "pro", "icons", "get", "1"}, "icons"},
		{[]string{"jamf-cli", "-p", "prof", "pro", "icons", "get"}, "icons"},
		{[]string{"jamf-cli", "pro", "--quiet", "icons", "get"}, "icons"},
		// A value-taking flag after the product, long and short forms.
		{[]string{"jamf-cli", "pro", "-p", "ci-svc", "icons", "get"}, "icons"},
		{[]string{"jamf-cli", "pro", "--profile", "ci-svc", "icons", "get"}, "icons"},
		{[]string{"jamf-cli", "pro", "--profile=ci-svc", "icons", "get"}, "icons"},
		{[]string{"jamf-cli", "pro", "-o", "json", "icons", "get"}, "icons"},
		{[]string{"jamf-cli", "pro", "-ojson", "icons", "get"}, "icons"},
		// A boolean shorthand carries no value, so the next token is the
		// resource. `-n` is --dry-run; a run of them must not eat one either.
		{[]string{"jamf-cli", "pro", "-n", "icons", "get"}, "icons"},
		{[]string{"jamf-cli", "pro", "-nq", "icons", "get"}, "icons"},
		// Several flags, mixed forms and placements.
		{[]string{"jamf-cli", "--no-color", "pro", "-p", "ci-svc", "--output", "json", "icons", "get"}, "icons"},
		// An unrecognised long flag is treated as value-taking, which is what
		// cobra does. Agreeing with cobra matters more than being right about a
		// command line cobra is about to reject.
		{[]string{"jamf-cli", "pro", "--not-a-flag", "value", "icons", "get"}, "icons"},
		// The terminator makes everything after it positional.
		{[]string{"jamf-cli", "pro", "--", "icons", "get"}, "icons"},
		{[]string{"jamf-cli", "pro"}, ""},
		{[]string{"jamf-cli", "pro", "-p"}, ""},
		{[]string{"jamf-cli", "protect", "plans", "list"}, ""},
	} {
		if got := resourceTokenAfter(tc.args, "pro", flags); got != tc.want {
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

// The source grep above answers "does this parent name ship". It cannot answer
// the other half — "does this *child* name ship under it" — and that half is
// where the failure is worse, because a stale child key does not remove a
// command, it ships one. `removeSubcommand` leaves the generated command in
// place; `replaceSubcommand` adds its replacement beside it. So `pro
// computer-inventory --help` listed `erase` twice in effect: the hand-written
// one under that name and the served v4 one under
// `v-4-computers-inventory-erase`, the second without the
// `--confirm-destructive` gate the first carries for bulk.
//
// The assembled tree cannot tell a successful replace from a stale key, since
// the child exists either way. The helpers therefore record every miss at the
// moment of the failed lookup — before any replacement is added — and this
// asserts the record is empty. That also covers the whole-resource removals the
// grep above skips, and the parent lookups, in one place.
func TestProWiringResolvesEveryNameItUses(t *testing.T) {
	for k := range staleProWiring {
		delete(staleProWiring, k)
	}
	t.Cleanup(func() {
		for k := range staleProWiring {
			delete(staleProWiring, k)
		}
	})

	newProCmd(&registry.CLIContext{})

	if len(staleProWiring) == 0 {
		return
	}
	keys := make([]string, 0, len(staleProWiring))
	for k := range staleProWiring {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Errorf("pro.go wiring names something `pro` does not ship: %s — the call is discarded, and for a suppression that means the generated command ships beside the hand-written one it exists to displace", k)
	}
}

// A guard that only ever passes proves nothing. This drives one stale key
// through the same helpers and requires the record to hold it, so the
// no-findings pass above is a real answer rather than a broken accumulator.
func TestStaleProWiringIsRecorded(t *testing.T) {
	for k := range staleProWiring {
		delete(staleProWiring, k)
	}
	t.Cleanup(func() {
		for k := range staleProWiring {
			delete(staleProWiring, k)
		}
	})

	root := &cobra.Command{Use: "root"}
	parent := &cobra.Command{Use: "computer-inventory"}
	parent.AddCommand(&cobra.Command{Use: "erase"})
	root.AddCommand(parent)

	removeSubcommand(root, []string{"computer-inventory"}, "v-4-computers-inventory-erase")
	if len(staleProWiring) != 1 {
		t.Fatalf("a stale child key recorded %d misses, want 1: %v", len(staleProWiring), staleProWiring)
	}
	replaceSubcommand(root, []string{"nonexistent"}, "erase", &cobra.Command{Use: "erase"})
	if len(staleProWiring) != 2 {
		t.Fatalf("a stale parent path recorded %d misses in total, want 2: %v", len(staleProWiring), staleProWiring)
	}

	// A key that does resolve must not be recorded, or the guard fires on
	// everything and says nothing.
	before := len(staleProWiring)
	removeSubcommand(root, []string{"computer-inventory"}, "erase")
	if len(staleProWiring) != before {
		t.Errorf("a resolving key was recorded as stale: %v", staleProWiring)
	}
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

// TestNestedAliasesPointAtCommandsThatShip is the nested-redirect twin of
// TestDeprecatedNamesPointAtCommandsThatShip, and it exists for the same
// reason: applyNestedAliases resolves each entry through
// generated.NestedResourceCommands and skips one it cannot find, so a path that
// stops naming a nested sub-resource is a redirect into a wall that reports
// nothing.
func TestNestedAliasesPointAtCommandsThatShip(t *testing.T) {
	root := NewRootCmd("test", "test", "test", "test")
	if len(nestedAliases) == 0 {
		t.Fatal("nestedAliases is empty, so this guard and applyNestedAliases both cover nothing")
	}
	for old, na := range nestedAliases {
		// The target has to be a real two-token path under `pro`.
		target, rest, err := root.Find(append([]string{"pro"}, strings.Fields(na.Path)...))
		if err != nil || len(rest) != 0 {
			t.Errorf("nestedAliases[%q].Path = %q, which `pro` does not ship", old, na.Path)
			continue
		}
		// And the old name has to reach a command with the same children, which
		// is what "kept working" means for a resource-level redirect.
		stub, rest, err := root.Find([]string{"pro", old})
		if err != nil || len(rest) != 0 {
			t.Errorf("nestedAliases[%q] is not registered under `pro`", old)
			continue
		}
		if !stub.Hidden {
			t.Errorf("`pro %s` is visible in --help; a compatibility stub belongs in neither the group "+
				"listing nor Additional Commands", old)
		}
		if stub.GroupID == "" {
			t.Errorf("`pro %s` has no GroupID, so it lands in Additional Commands — the one listing it "+
				"must stay out of", old)
		}
		wantChildren := childNames(target)
		gotChildren := childNames(stub)
		if strings.Join(gotChildren, ",") != strings.Join(wantChildren, ",") {
			t.Errorf("`pro %s` ships %v but `pro %s` ships %v — the redirect has drifted from what it "+
				"redirects to", old, gotChildren, na.Path, wantChildren)
		}
	}
}

// TestNestedAliasesDoNotShadowALiveName covers the ambiguity a second command
// under a live name creates: cobra resolves by declaration order, which is not
// a choice this table gets to make.
func TestNestedAliasesDoNotShadowALiveName(t *testing.T) {
	byName, byAlias := proChildren(t)
	for old, na := range nestedAliases {
		if _, live := deprecatedNames[old]; live {
			t.Errorf("%q is in both deprecatedNames and nestedAliases; the alias and the stub would "+
				"both claim the name", old)
		}
		if target, ok := byAlias[old]; ok && target.Name() != old {
			t.Errorf("%q is a curated alias of `pro %s` as well as a nested redirect to %q",
				old, target.Name(), na.Path)
		}
		// The stub itself is the command registered under the name, so finding
		// it is expected; finding something else is not.
		if c, ok := byName[old]; ok && !c.Hidden {
			t.Errorf("`pro %s` is a live visible command, so the redirect is shadowing it", old)
		}
	}
}

// childNames returns a command's subcommand names, sorted.
func childNames(cmd *cobra.Command) []string {
	var out []string
	for _, c := range cmd.Commands() {
		if c.Name() == "help" {
			continue
		}
		out = append(out, c.Name())
	}
	sort.Strings(out)
	return out
}
