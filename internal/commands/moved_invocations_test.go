// Copyright 2026, Jamf Software LLC

package commands

import (
	"errors"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

// proCmd returns the assembled `pro` namespace.
func proCmd(t *testing.T) *cobra.Command {
	t.Helper()
	root := NewRootCmd("test", "test", "test", "test")
	pro := childNamed(root, "pro")
	if pro == nil {
		t.Fatal("the root command ships no `pro` namespace")
	}
	return pro
}

// findCommand walks a space-separated path from cmd, following names and
// aliases.
func findCommand(cmd *cobra.Command, path string) *cobra.Command {
	cur := cmd
	for _, tok := range strings.Fields(path) {
		cur = childNamed(cur, tok)
		if cur == nil {
			return nil
		}
	}
	return cur
}

// Every entry has to still be a redirect: a key that names a live command would
// shadow it, and a replacement that names nothing sends the caller nowhere.
//
// Both directions matter and only one is obvious. The shadow check is the sharp
// one — a stub registered over a working command makes cobra resolve by
// declaration order, and the table does not get to make that choice.
func TestMovedInvocationsNameCommandsThatShip(t *testing.T) {
	pro := proCmd(t)
	if len(movedInvocations) == 0 {
		t.Fatal("movedInvocations is empty; this guard cannot pass vacuously")
	}

	var shadowed, missing []string
	for key, mv := range movedInvocations {
		// The stub itself is registered by now, so a key resolving to a
		// command is expected. What must not exist is a *runnable* command
		// there that is not the stub.
		if got := findCommand(pro, key); got != nil && !isMovedStub(got) {
			shadowed = append(shadowed, key)
		}
		if len(mv.Now) == 0 {
			missing = append(missing, key+" -> (nothing)")
		}
		for _, now := range mv.Now {
			target := findCommand(pro, now)
			if target == nil || !target.Runnable() {
				missing = append(missing, key+" -> "+now)
			}
		}
	}
	sort.Strings(shadowed)
	sort.Strings(missing)
	if len(shadowed) > 0 {
		t.Errorf("%d movedInvocations key(s) name a live command, so the stub shadows it and cobra resolves by declaration order:\n  %s",
			len(shadowed), strings.Join(shadowed, "\n  "))
	}
	if len(missing) > 0 {
		t.Errorf("%d movedInvocations replacement(s) name no runnable command:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
	t.Logf("%d moved invocations, all redirecting to commands that ship", len(movedInvocations))
}

// isMovedStub reports whether cmd is one of the refusal stubs this file
// registers, rather than a real command.
func isMovedStub(cmd *cobra.Command) bool {
	return strings.HasPrefix(cmd.Short, "Moved — use `pro ")
}

// The point of the table is the pointer, so the pointer is what is asserted:
// every entry answers with a refusal that names its replacement, in place of
// cobra's "unknown command".
//
// Replacing newMovedInvocationCmd's RunE with a nil return fails this.
func TestEveryMovedInvocationRefusesAndNamesItsReplacement(t *testing.T) {
	pro := proCmd(t)
	for _, key := range sortedKeysOfMoved() {
		stub := findCommand(pro, key)
		if stub == nil {
			t.Errorf("%q registered no stub", key)
			continue
		}
		if !isMovedStub(stub) {
			t.Errorf("%q resolves to a real command, not a stub", key)
			continue
		}
		err := stub.RunE(stub, nil)
		if err == nil {
			t.Errorf("%q ran and returned nil; the invocation no longer exists and has to say so", key)
			continue
		}
		var ec *exitcode.Error
		if !asExitcodeError(err, &ec) {
			t.Errorf("%q refused with a plain error, so it carries no hint: %v", key, err)
			continue
		}
		if ec.Code != exitcode.Usage {
			t.Errorf("%q exits %d, want %d", key, ec.Code, exitcode.Usage)
		}
		if !strings.Contains(ec.Message, key) {
			t.Errorf("%q message does not name the invocation typed: %q", key, ec.Message)
		}
		for _, now := range movedInvocations[key].Now {
			if !strings.Contains(ec.Hint, now) {
				t.Errorf("%q hint does not name %q: %q", key, now, ec.Hint)
			}
		}
	}
}

// asExitcodeError is errors.As without the import, kept local so the assertion
// reads the same as the ones beside it.
func asExitcodeError(err error, out **exitcode.Error) bool {
	for e := err; e != nil; {
		if ec, ok := e.(*exitcode.Error); ok {
			*out = ec
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

// A nested alias is a second *instance* of the sub-resource, so a stub added to
// the canonical subtree is not on its copy. `pro sso-settings-cert cert`
// resolved and returned data before the split and would otherwise get cobra's
// "unknown command" on the very spelling the alias exists to keep working.
func TestMovedInvocationsReachTheNestedAliasSpellings(t *testing.T) {
	pro := proCmd(t)
	covered := 0
	for old, na := range nestedAliases {
		for key := range movedInvocations {
			rest, ok := cutPathPrefix(key, na.Path)
			if !ok {
				continue
			}
			covered++
			stub := findCommand(pro, old+" "+rest)
			if stub == nil || !isMovedStub(stub) {
				t.Errorf("`pro %s %s` reaches no refusal stub, so the alias spelling of %q gets cobra's unknown command", old, rest, key)
			}
		}
	}
	if covered == 0 {
		t.Fatal("no movedInvocations key sits beneath a nestedAliases path, so this guard proves nothing; if the overlap really disappeared, delete movedKeySpellings with it")
	}
	t.Logf("%d moved invocation(s) reachable through a nested alias spelling", covered)
}

// The three former leaves are still groups, which is the premise of the guard
// on them: a group renders nothing, so a data request against one has to be
// refused rather than answered with help at exit 0.
func TestFormerLeafGroupsAreStillGroups(t *testing.T) {
	pro := proCmd(t)
	if len(formerLeafGroups) == 0 {
		t.Fatal("formerLeafGroups is empty; this guard cannot pass vacuously")
	}
	for key, leaf := range formerLeafGroups {
		group := findCommand(pro, key)
		if group == nil {
			t.Errorf("`pro %s` resolves to nothing; the entry names a path that moved", key)
			continue
		}
		if !group.HasSubCommands() {
			t.Errorf("`pro %s` is no longer a group, so it needs no pointer at %q — delete the entry", key, leaf)
		}
		if target := findCommand(pro, leaf); target == nil || !target.Runnable() {
			t.Errorf("`pro %s` points at `pro %s`, which is not a runnable command", key, leaf)
		}
	}
}

// A data request against one of the three refuses and names the leaf; a bare
// invocation still prints help and exits 0, which is this CLI's convention for
// every group parent.
//
// Both halves matter. Refusing the bare form would break exploring the tree,
// and refusing nothing is the state the finding reported: a job piping
// `pro csas token -o json` into jq got cobra's help text at exit 0.
func TestFormerLeafGroupRefusesOnlyADataRequest(t *testing.T) {
	for key, leaf := range formerLeafGroups {
		pro := proCmd(t)
		group := findCommand(pro, key)
		if group == nil {
			t.Fatalf("`pro %s` resolves to nothing", key)
		}

		if err := group.RunE(group, nil); err != nil {
			t.Errorf("bare `pro %s` returned %v; a group parent prints help and exits 0", key, err)
		}

		for _, flag := range []string{"output", "field", "select", "out-file"} {
			pro := proCmd(t)
			group := findCommand(pro, key)
			// Set it on the root's persistent set. These are inherited flags,
			// and cobra merges the inherited set into cmd.Flags() by reference
			// during ParseFlags — which Execute does and a direct RunE call
			// does not, so setting through group.Flags() finds no such flag.
			if err := group.Root().PersistentFlags().Set(flag, valueFor(flag)); err != nil {
				t.Fatalf("setting --%s: %v", flag, err)
			}
			err := group.RunE(group, nil)
			if err == nil {
				t.Errorf("`pro %s --%s` returned no error; it renders nothing and the caller asked for data", key, flag)
				continue
			}
			var ec *exitcode.Error
			if !asExitcodeError(err, &ec) {
				t.Errorf("`pro %s --%s` refused with a plain error, so it carries no hint: %v", key, flag, err)
				continue
			}
			if ec.Code == exitcode.Success {
				t.Errorf("`pro %s --%s` refused at exit 0", key, flag)
			}
			if !strings.Contains(ec.Hint, leaf) {
				t.Errorf("`pro %s --%s` hint does not name `pro %s`: %q", key, flag, leaf, ec.Hint)
			}
		}
	}
}

func valueFor(flag string) string {
	switch flag {
	case "output":
		return "json"
	case "out-file":
		return "/dev/null"
	default:
		return "id"
	}
}

// A typo beneath one of the three still gets the "did you mean" refusal, which
// is what guardFormerLeafGroups wrapping the RunE — rather than replacing it —
// is for.
func TestFormerLeafGroupStillRefusesATypoBeneathIt(t *testing.T) {
	pro := proCmd(t)
	for key := range formerLeafGroups {
		group := findCommand(pro, key)
		if group == nil {
			t.Fatalf("`pro %s` resolves to nothing", key)
		}
		err := group.RunE(group, []string{"nosuchverb"})
		if err == nil || !strings.Contains(err.Error(), "unknown command") {
			t.Errorf("`pro %s nosuchverb` = %v; want the group parent's unknown-command refusal", key, err)
		}
	}
}

// warnIfDeprecatedName prints the warning that is the whole migration
// mechanism: it is what tells a caller of one of the retired names to move.
//
// No test asserted it, and replacing the function body with a no-op left the
// whole package green — every other guard checks that an alias *resolves and
// runs*, and none checked that anything was said. A later reorganisation of
// PersistentPreRunE dropping the call site would have gone unnoticed until the
// aliases expired and every script broke together.
func TestWarnIfDeprecatedNameNamesTheReplacement(t *testing.T) {
	pro := proCmd(t)
	leaf := findCommand(pro, "buildings list")
	if leaf == nil {
		t.Fatal("`pro buildings list` does not ship; pick another live leaf for this guard")
	}

	// One deprecatedNames entry, one nestedAliases entry, and a live name.
	cases := []struct {
		name  string
		argv  []string
		wants []string
	}{
		{
			name:  "a renamed resource",
			argv:  []string{"jamf-cli", "pro", "csas", "tenant-id"},
			wants: []string{"`csas` is a deprecated name for `csa`", deprecatedNamesRemovedAfter, "Use `pro csa`"},
		},
		{
			name:  "a nested alias",
			argv:  []string{"jamf-cli", "pro", "sso-settings-cert", "get"},
			wants: []string{"`sso-settings-cert` is a deprecated name for `sso-settings cert`", "Use `pro sso-settings cert`"},
		},
	}
	for _, tc := range cases {
		got := captureStderr(t, func() {
			withArgs(tc.argv, func() { warnIfDeprecatedName(leaf) })
		})
		for _, want := range tc.wants {
			if !strings.Contains(got, want) {
				t.Errorf("%s: warning %q does not contain %q", tc.name, got, want)
			}
		}
	}

	// A live name must say nothing, or every ordinary invocation carries noise.
	got := captureStderr(t, func() {
		withArgs([]string{"jamf-cli", "pro", "buildings", "list"}, func() { warnIfDeprecatedName(leaf) })
	})
	if got != "" {
		t.Errorf("a live resource name warned: %q", got)
	}
}

// Every deprecated and nested name warns, not only the two spelled out above.
// The lookup is what a reorganisation would regress, and it is per-entry.
func TestEveryDeprecatedNameWarns(t *testing.T) {
	pro := proCmd(t)
	leaf := findCommand(pro, "buildings list")
	if leaf == nil {
		t.Fatal("`pro buildings list` does not ship")
	}
	names := make([]string, 0, len(deprecatedNames)+len(nestedAliases))
	for old := range deprecatedNames {
		names = append(names, old)
	}
	for old := range nestedAliases {
		names = append(names, old)
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatal("no deprecated names; this guard cannot pass vacuously")
	}
	var silent []string
	for _, old := range names {
		got := captureStderr(t, func() {
			withArgs([]string{"jamf-cli", "pro", old, "list"}, func() { warnIfDeprecatedName(leaf) })
		})
		if !strings.Contains(got, "deprecated name") {
			silent = append(silent, old)
		}
	}
	if len(silent) > 0 {
		t.Errorf("%d retired name(s) resolve with no warning:\n  %s", len(silent), strings.Join(silent, "\n  "))
	}
	t.Logf("%d retired names, all warning", len(names))
}

// withArgs runs fn with os.Args set. warnIfDeprecatedName reads the resource
// token out of argv rather than asking cobra, because a parent's alias is
// unreadable through CalledAs().
func withArgs(argv []string, fn func()) {
	saved := os.Args
	os.Args = argv
	defer func() { os.Args = saved }()
	fn()
}

// Every deprecatedNameVerbMoves entry names a live deprecated resource, a verb
// that exists on its replacement, and a replacement invocation that resolves to
// a leaf.
//
// A stale entry is the failure to watch for: the verb move is permanent, so an
// entry that stops matching means the refusal has silently gone and the old
// spelling is running the wrong operation again.
func TestDeprecatedNameVerbMovesResolve(t *testing.T) {
	pro := proCmd(t)
	if len(deprecatedNameVerbMoves) == 0 {
		t.Fatal("the table is empty, so this test cannot pass vacuously")
	}
	for _, old := range sortedKeysOfVerbMoves() {
		dep, ok := deprecatedNames[old]
		if !ok {
			t.Errorf("%q names no deprecatedNames entry, so nothing resolves it and nothing is refused", old)
			continue
		}
		resource := childNamed(pro, dep.Now)
		if resource == nil {
			t.Errorf("%q points at %q, which the binary does not ship", old, dep.Now)
			continue
		}
		for _, verb := range sortedKeysOfStringMap(deprecatedNameVerbMoves[old]) {
			if childNamed(resource, verb) == nil {
				t.Errorf("%s %s: %q has no %q, so the entry refuses nothing", old, verb, dep.Now, verb)
			}
			want := deprecatedNameVerbMoves[old][verb]
			target := findCommand(pro, want)
			if target == nil {
				t.Errorf("%s %s points at `pro %s`, which the binary does not ship", old, verb, want)
				continue
			}
			if target.HasSubCommands() {
				t.Errorf("%s %s points at `pro %s`, which is a command group and returns no data", old, verb, want)
			}
		}
	}
}

// The refusal itself: a moved verb typed against the old spelling refuses and
// names its replacement, while the live spelling is passed through.
//
// Asserted on the predicate the guard's RunE wrapper calls, rather than by
// running the command: the live-spelling half has to reach `inner`, and every
// generated RunE needs a CLIContext before it does anything at all.
func TestDeprecatedNameVerbMoveRefusesOnlyTheOldSpelling(t *testing.T) {
	const old = "enrollment-customization-panels"
	moves := deprecatedNameVerbMoves[old]
	if len(moves) == 0 {
		t.Fatalf("%q holds no verb moves, so this test cannot pass vacuously", old)
	}
	live := deprecatedNames[old].Now

	for verb, want := range moves {
		t.Run(verb, func(t *testing.T) {
			err := refuseMovedVerb(old, old, live, verb, want)
			if err == nil {
				t.Fatalf("`pro %s %s` was accepted; it addresses the resource root, not the panel", old, verb)
			}
			if !strings.Contains(err.Error(), old) {
				t.Errorf("refusal does not name the spelling typed: %v", err)
			}
			var ec *exitcode.Error
			if !errors.As(err, &ec) {
				t.Fatalf("refusal is not an *exitcode.Error, so it carries no hint: %v", err)
			}
			if ec.Code != exitcode.Usage {
				t.Errorf("exit code = %d, want %d", ec.Code, exitcode.Usage)
			}
			if !strings.Contains(ec.Hint, want) {
				t.Errorf("hint = %q, want it to name `pro %s`", ec.Hint, want)
			}
			if err := refuseMovedVerb(live, old, live, verb, want); err != nil {
				t.Errorf("`pro %s %s` was refused: %v", live, verb, err)
			}
		})
	}
}

// The wiring: the guard reaches the leaf and reads argv, so the refusal is what
// the built binary answers rather than only what the predicate returns.
func TestGuardDeprecatedNameVerbMovesWrapsTheLeaf(t *testing.T) {
	const old = "enrollment-customization-panels"
	root := NewRootCmd("test", "none", "none", "none")
	leaf := findCommand(childNamed(root, "pro"), deprecatedNames[old].Now+" create")
	if leaf == nil {
		t.Fatal("`pro enrollment-customization create` does not resolve")
	}
	withArgs([]string{"jamf-cli", "pro", old, "create", "--scaffold"}, func() {
		if err := leaf.RunE(leaf, nil); err == nil {
			t.Fatal("the guard did not reach the leaf: a moved verb ran under the old spelling")
		}
	})
}

// Cobra validates Args before RunE, so the refusal has to survive the form the
// old command took: `pro enrollment-customization-panels update <id> <panel-id>`
// against a leaf that now accepts at most one positional. Without the
// relaxation this answered "accepts at most 1 arg(s), received 2" and the
// pointer was unreachable from the invocation a caller actually types.
func TestDeprecatedNameVerbMoveSurvivesTheOldArity(t *testing.T) {
	const old = "enrollment-customization-panels"
	root := NewRootCmd("test", "none", "none", "none")
	leaf := findCommand(childNamed(root, "pro"), deprecatedNames[old].Now+" update")
	if leaf == nil {
		t.Fatal("`pro enrollment-customization update` does not resolve")
	}
	withArgs([]string{"jamf-cli", "pro", old, "update", "1", "2"}, func() {
		if err := leaf.Args(leaf, []string{"1", "2"}); err != nil {
			t.Errorf("two positionals under the old spelling = %v, want them through to the refusal", err)
		}
	})
	// And the live spelling keeps its ceiling, or the relaxation has replaced
	// the validator rather than fronting it.
	withArgs([]string{"jamf-cli", "pro", deprecatedNames[old].Now, "update", "1", "2", "3"}, func() {
		if err := leaf.Args(leaf, []string{"1", "2", "3"}); err == nil {
			t.Error("three positionals under the live spelling were accepted; the arity check is gone")
		}
	})
}
