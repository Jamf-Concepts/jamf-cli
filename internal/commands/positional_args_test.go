// Copyright 2026, Jamf Software LLC

package commands

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	gotoken "go/token" // the package already declares a `token` variable
	"io/fs"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/google/shlex"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// unboundedPositionalLeaves names the leaf commands whose positional arity is
// unbounded by design, so there is no argument count they can refuse. Each entry
// states why. A leaf listed here must still document a variadic tail in its Use
// string, and a variadic leaf that is absent from the table fails the test — so
// neither a stale entry nor a silent addition survives.
var unboundedPositionalLeaves = map[string]string{
	"multi": "forwards every positional after the inner command name to that command",
}

type commandLeaf struct {
	path string
	cmd  *cobra.Command
}

// runnableLeaves collects every runnable command with no subcommands, named by
// its full path. Walking the assembled tree is what lets a new command be
// covered without an edit to the tests that read it.
func runnableLeaves(root *cobra.Command) []commandLeaf {
	var leaves []commandLeaf
	var walk func(c *cobra.Command, path string)
	walk = func(c *cobra.Command, path string) {
		for _, sub := range c.Commands() {
			subPath := strings.TrimSpace(path + " " + sub.Name())
			if sub.Runnable() && !sub.HasSubCommands() {
				leaves = append(leaves, commandLeaf{subPath, sub})
			}
			walk(sub, subPath)
		}
	}
	walk(root, "")
	return leaves
}

// sameFunc reports whether two function values are the same function. The two
// arguments are often of different named function types that share an
// underlying signature, which is what rules out ==.
func sameFunc(a, b any) bool {
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

// isNoPositionalCompletion reports whether the guard installed the no-file
// completion, as opposed to the command declaring a completion of its own.
// cobra.NoFileCompletions is cobra's own name for the function the guard used
// to spell out locally.
func isNoPositionalCompletion(fn func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)) bool {
	if fn == nil {
		return false
	}
	return sameFunc(fn, cobra.NoFileCompletions)
}

// requiredPositionals counts the placeholders a Use string states are not
// optional, which is the floor where declaredPositionals gives the ceiling. A
// bracketed placeholder is optional by cobra's own convention: `get [<id>]`
// takes an id or a --name instead, so its example passing neither is correct.
//
// The guard reads only the ceiling, so this lives here rather than beside it.
func requiredPositionals(use, name string) int {
	fields := strings.Fields(use)
	if len(fields) > 0 && fields[0] == name {
		fields = fields[1:]
	}
	required := 0
	for _, f := range fields {
		if strings.HasPrefix(f, "[") {
			continue
		}
		required++
	}
	return required
}

func strayArgs(n int) []string {
	a := make([]string, n)
	for i := range a {
		a[i] = "zzstrayargument"
	}
	return a
}

// TestEveryLeafRefusesAnUndocumentedPositional holds every runnable leaf to the
// positional contract its own Use string declares: it accepts the documented
// count and refuses one more. Cobra validates Args per command and supplies no
// default, so a leaf with no validator accepts any positional and discards it.
//
// Because it walks the whole tree, a new leaf — generated or hand-written, at
// any depth — is covered without editing this test.
func TestEveryLeafRefusesAnUndocumentedPositional(t *testing.T) {
	leaves := runnableLeaves(NewRootCmd("test", "none", "none", "none"))

	seenUnbounded := map[string]bool{}
	documented, unwrapped := 0, 0
	stubs := 0
	for _, l := range leaves {
		if isMovedStub(l.cmd) {
			// A movedInvocations refusal stub takes whatever it is given and
			// answers that the invocation no longer exists. Its arity is not a
			// contract — the command it stands in for is gone, so inventing a
			// placeholder in its Use would document a positional nothing
			// reads, and clamping it would answer `pro csa delete 5` with
			// "takes no positional arguments" instead of naming where the
			// operation went. TestEveryMovedInvocationRefusesAndNamesIts-
			// Replacement is what holds these to their own contract.
			stubs++
			continue
		}
		count, variadic := declaredPositionals(l.cmd.Use, l.cmd.Name())
		if reason, listed := unboundedPositionalLeaves[l.path]; listed {
			seenUnbounded[l.path] = true
			if !variadic {
				t.Errorf("leaf %q is listed as unbounded (%s) but its Use %q documents a bounded arity; drop the entry", l.path, reason, l.cmd.Use)
			}
			continue
		}
		if variadic {
			t.Errorf("leaf %q documents a variadic positional in Use %q but is absent from unboundedPositionalLeaves", l.path, l.cmd.Use)
			continue
		}
		if count > 0 {
			documented++
		}
		if l.cmd.Args == nil {
			t.Errorf("leaf %q declares no Args validator: it accepts any positional and discards it", l.path)
			continue
		}
		// A refusal reached without classifyArgsErrors' wrap means the two calls
		// in NewRootCmd were reordered, and nothing else in the suite can see
		// it: the guard installs a validator on every leaf that has none, so
		// running it second leaves each of those unwrapped, which is harmless
		// only while refuseStrayPositionals classifies itself. Swapping the two
		// calls left the whole suite green.
		if count == 0 && sameFunc(l.cmd.Args, refuseStrayPositionals) {
			unwrapped++
		}
		if err := l.cmd.Args(l.cmd, strayArgs(count+1)); err == nil {
			t.Errorf("leaf %q documents %d positional(s) in Use %q but accepted %d", l.path, count, l.cmd.Use, count+1)
		} else if count == 0 {
			// refuseStrayPositionals carries exit 2 itself. A bounded validator
			// still answers cobra's unclassified "accepts at most" message and
			// exits 1, so only the zero-arity refusal is checked for its code.
			if code := exitcode.CodeFrom(ClassifyError(err)); code != exitcode.Usage {
				t.Errorf("leaf %q: stray positional exits %d, want %d (usage)", l.path, code, exitcode.Usage)
			}
			// Wording and hint, not only the code. Two sibling leaves under
			// `pro report` answered the same mistake two ways for a release,
			// one of them by declaring cobra.NoArgs by hand, and this walk read
			// only the code, which both shapes carry. The hint is what a plain
			// error cannot carry, so it is also what fails if a refusal stops
			// classifying itself — the code alone cannot, classifyArgsErrors
			// supplying that at cobra's own call site. That wrap is why the
			// error is read with errors.As rather than a type assertion.
			var e *exitcode.Error
			switch {
			case !errors.As(err, &e):
				t.Errorf("leaf %q: refusal carries no exit code: %T", l.path, err)
			case strings.Contains(e.Message, "unknown command"):
				t.Errorf("leaf %q: refusal reports an unknown command, which is a parent's mistake, not a leaf's: %q", l.path, e.Message)
			case e.Hint == "":
				t.Errorf("leaf %q: refusal carries no hint, so it does not say where to find the flags", l.path)
			}
		}
		if err := l.cmd.Args(l.cmd, strayArgs(count)); err != nil {
			t.Errorf("leaf %q documents %d positional(s) in Use %q but refused that many: %v", l.path, count, l.cmd.Use, err)
		}

		// The guard installs a completion function as well as a validator, and
		// reading only the validator left its clamp unverified: widening the
		// clamp suppressed completion on all 700 leaves that do take an
		// identifier, with every assertion above still passing.
		suppressed := isNoPositionalCompletion(l.cmd.ValidArgsFunction)
		if count == 0 && l.cmd.ValidArgsFunction == nil {
			t.Errorf("leaf %q takes no positional but offers file completion for one", l.path)
		}
		if count > 0 && suppressed {
			t.Errorf("leaf %q documents %d positional(s) in Use %q but its completion is suppressed", l.path, count, l.cmd.Use)
		}
	}

	for path, reason := range unboundedPositionalLeaves {
		if !seenUnbounded[path] {
			t.Errorf("unboundedPositionalLeaves names %q (%s), which is not a leaf this binary ships", path, reason)
		}
	}

	if unwrapped > 0 {
		t.Errorf("%d zero-arity leaves carry refuseStrayPositionals unwrapped: guardStrayPositionals must run before classifyArgsErrors in NewRootCmd", unwrapped)
	}

	// The exemption above is only sound while every skipped leaf really is a
	// stub, so the count has to match the table it comes from. A stub that
	// stopped being registered, or a real leaf that grew the stub's Short,
	// would otherwise leave this walk quietly skipping commands.
	if want := len(movedInvocations) + movedAliasSpellings(); stubs != want {
		t.Errorf("skipped %d refusal stub(s), want %d — a movedInvocations entry lost its stub, or a real leaf is being skipped", stubs, want)
	}

	if len(leaves) < 700 {
		t.Fatalf("found only %d runnable leaves — tree walk likely broken", len(leaves))
	}
	if documented < 100 {
		t.Fatalf("only %d leaves document a positional — the arity reader is likely returning 0 for everything", documented)
	}
	t.Logf("verified %d runnable leaves (%d documenting a positional, %d unbounded)", len(leaves), documented, len(seenUnbounded))
}

// TestDeclaredPositionals reads the arity reader directly, because the tree walk
// cannot verify it. Only `multi` carries a [flags], -- or [--] token today, and
// it is the one leaf the walk skips as variadic, so the arm that drops cobra's
// own tokens ran on every invocation with nothing asserting it: deleting it left
// the whole suite green while turning `backup [flags]` into a leaf the guard
// reads as taking one positional and therefore never clamps.
func TestDeclaredPositionals(t *testing.T) {
	for _, tc := range []struct {
		use, name string
		count     int
		variadic  bool
	}{
		{"backup", "backup", 0, false},
		{"backup [flags]", "backup", 0, false},
		{"update --", "update", 0, false},
		{"update [--]", "update", 0, false},
		{"update <id>", "update", 1, false},
		{"update [<id>]", "update", 1, false},
		{"get <id> [<name>]", "get", 2, false},
		{"multi [flags] [--] <command> [args...]", "multi", 2, true},
		{"doctor [profile]", "doctor", 1, false},
		// The name is dropped only as the leading token, so a resource whose
		// placeholder repeats its own name still counts.
		{"delete <delete>", "delete", 1, false},
	} {
		count, variadic := declaredPositionals(tc.use, tc.name)
		if count != tc.count || variadic != tc.variadic {
			t.Errorf("declaredPositionals(%q, %q) = (%d, %v), want (%d, %v)", tc.use, tc.name, count, variadic, tc.count, tc.variadic)
		}
	}
}

// TestRefuseStrayPositionalsNamesTheRealMistake pins the wording, which is the
// whole reason this validator exists rather than cobra.NoArgs. Without it the
// message could be reverted to cobra's "unknown command" phrasing behind a
// correct exit code and nothing would fail.
func TestRefuseStrayPositionalsNamesTheRealMistake(t *testing.T) {
	cmd := &cobra.Command{Use: "backup"}
	cmd.SetArgs(nil)

	if err := refuseStrayPositionals(cmd, nil); err != nil {
		t.Errorf("no positional must be accepted: %v", err)
	}

	err := refuseStrayPositionals(cmd, []string{"/tmp/out"})
	if err == nil {
		t.Fatal("a stray positional must be refused")
	}
	var e *exitcode.Error
	if !errors.As(err, &e) {
		t.Fatalf("refusal does not carry an exit code: %T", err)
	}
	if e.Code != exitcode.Usage {
		t.Errorf("refusal exits %d, want %d (usage)", e.Code, exitcode.Usage)
	}
	if strings.Contains(e.Message, "unknown command") {
		t.Errorf("refusal reports an unknown command, which is a parent's error, not a leaf's: %q", e.Message)
	}
	if !strings.Contains(e.Message, "/tmp/out") {
		t.Errorf("refusal does not name the offending value: %q", e.Message)
	}
	if !strings.Contains(e.Message, "backup") {
		t.Errorf("refusal does not name the command: %q", e.Message)
	}
	if e.Hint == "" {
		t.Error("refusal carries no hint, so it does not say where to find the flags")
	}
}

// unmatchedExampleLeaves is the number of leaves whose Example resolves to no
// invocation of that leaf, so this test reads nothing for them.
//
// Eighteen are internal/scope's add and remove across nine classic resources,
// whose examples are written without the binary name (`scope add "Deploy
// Chrome" …`). The other two carry an example that resolves to a different
// command than the leaf it sits on: `pro mobile-devices delete` documents
// `pro classic-mobile-devices delete` and `pro packages sync` documents
// `pro jcds sync`. Both are pre-existing and are a question about the example
// rather than about arity.
//
// It was 21. The third was `pro computer-groups get`, which sat in the second of
// two identical computer-groups subtrees — the generated registry called
// NewComputerGroupsCmd twice, so `pro --help` printed the row twice and Find
// resolved every path under it to the first copy. Resource identity comes from
// the spec's paths and tags now rather than from two filenames that named the
// same resource, so there is one subtree and the example resolves to its own
// leaf. A registry defect, fixed by removing what caused it.
//
// Thirteen more arrived with the nested sub-resource split, and for those the
// mismatch is the point: each is a leaf under a hidden compatibility stub
// (`pro sso-settings-cert update`, `pro self-service-settings get` and the rest
// of nestedAliases), built by calling the nested resource's own constructor, so
// its Example correctly names the live path — `pro sso-settings cert update` —
// which resolves to a different leaf. A stub whose --help taught its own dead
// name would be the defect. Their arity is still covered, once, on the leaf the
// examples do resolve to.
//
// Pinned so a reader that stops matching a form it handles today, or a new
// unmatchable form, fails rather than quietly shrinking the population.
const unmatchedExampleLeaves = 33

// TestScaffoldKeepsTheDeclaredPositionalCeiling covers the path the walk above
// cannot see, because that walk reads each validator with no flag set.
//
// All three generators relax a command's Args validator while --scaffold is set,
// since --scaffold needs no identifier and cobra validates Args before RunE.
// Relaxing it to no validator at all reopened issue 350 behind a flag:
// `pro classic-jwt-configs update aaa bbb ccc --scaffold` printed the template
// and discarded three positionals with exit 0. Relaxing nothing is the opposite
// defect, issue 363: the modern Pro generator emitted a bare ExactArgs, so 26
// leaves under one or more path parameters with no --name lookup demanded
// identifiers their scaffold makes no request with (`pro
// enrollment-customization-panels update` sat under two) and the flag was
// unusable. So this test pins both ends: every scaffold-bearing leaf accepts no
// positional and refuses one more than its Use documents.
func TestScaffoldKeepsTheDeclaredPositionalCeiling(t *testing.T) {
	leaves := runnableLeaves(NewRootCmd("test", "none", "none", "none"))

	checked, unreachable := 0, 0
	for _, l := range leaves {
		if l.cmd.Flags().Lookup("scaffold") == nil || l.cmd.Args == nil {
			continue
		}
		if err := l.cmd.Flags().Set("scaffold", "true"); err != nil {
			t.Fatalf("leaf %q: setting --scaffold: %v", l.path, err)
		}
		checked++

		count, _ := declaredPositionals(l.cmd.Use, l.cmd.Name())
		if err := l.cmd.Args(l.cmd, strayArgs(count+1)); err == nil {
			t.Errorf("leaf %q with --scaffold accepted %d positional(s), one more than its Use %q documents", l.path, count+1, l.cmd.Use)
		}
		// The ceiling has to be the declared one, not merely bounded. Without
		// this the assertion above passes for a validator that is too STRICT:
		// a mutation of the classic relaxation to MaximumNArgs(0) broke
		// "update <id> --scaffold" on 35 leaves and left this package green.
		if err := l.cmd.Args(l.cmd, strayArgs(count)); err != nil {
			t.Errorf("leaf %q with --scaffold refused the %d positional(s) its Use %q documents: %v", l.path, count, l.cmd.Use, err)
		}
		// The floor has to be zero, or the flag cannot be used at all: cobra
		// refuses before RunE reaches the scaffold return. This is the half
		// issue 363 was about.
		if err := l.cmd.Args(l.cmd, nil); err != nil {
			unreachable++
			t.Errorf("leaf %q cannot reach its own --scaffold: %v", l.path, err)
		}
	}

	if checked < 100 {
		t.Fatalf("only %d leaves carry both --scaffold and an Args validator — the walk or the flag name is likely wrong", checked)
	}
	t.Logf("verified %d scaffold-bearing leaves keep their declared ceiling (%d cannot reach their scaffold)", checked, unreachable)
}

// TestNoExampleDocumentsAnUndeclaredPositional holds each leaf's Example block to
// the same ceiling guardStrayPositionals enforces, because a refusal the
// command's own --help teaches is worse than the discarded positional it
// replaced.
//
// It caught 26 example lines on 22 singleton commands, four of them destructive:
// the Pro generator rendered `delete 1` and `history 1` whatever the path shape,
// so a resource with no id in its path documented an argument the guard then
// refused. Reading Use and Example apart is what let that ship, so this reads
// them together.
//
// Both ends are asserted, with one exemption: a --scaffold line is held to the
// ceiling only, because every generator lowers the floor to zero while that flag
// is set (issue 363). Every other line has to satisfy the floor too, or --help
// teaches an invocation the command refuses for want of an identifier.
func TestNoExampleDocumentsAnUndeclaredPositional(t *testing.T) {
	root := NewRootCmd("test", "none", "none", "none")
	leaves := runnableLeaves(root)

	checked, unmatched := 0, 0
	for _, l := range leaves {
		count, variadic := declaredPositionals(l.cmd.Use, l.cmd.Name())
		if variadic {
			continue
		}
		required := requiredPositionals(l.cmd.Use, l.cmd.Name())
		invocations := exampleInvocations(root, l)
		if l.cmd.Example != "" && len(invocations) == 0 {
			unmatched++
		}
		for _, invocation := range invocations {
			checked++
			if len(invocation.args) > count {
				t.Errorf("leaf %q documents %d positional(s) in Use %q but its Example says %q, which passes %d",
					l.path, count, l.cmd.Use, invocation.line, len(invocation.args))
			}
			// A --scaffold example is exempt from the floor, because the
			// generators lower that floor to zero while the flag is set: the
			// flag makes no request and needs no identifier.
			if !invocation.scaffold && len(invocation.args) < required {
				t.Errorf("leaf %q requires %d positional(s) per Use %q but its Example says %q, which passes %d",
					l.path, required, l.cmd.Use, invocation.line, len(invocation.args))
			}
		}
	}

	if checked < 500 {
		t.Fatalf("only %d example invocations parsed — the Example reader is likely not matching command paths", checked)
	}
	if unmatched != unmatchedExampleLeaves {
		t.Errorf("%d leaves carry an Example that resolves to no invocation, want %d: the reader stopped matching a form, or a new one appeared", unmatched, unmatchedExampleLeaves)
	}
	t.Logf("verified %d example invocations against their declared arity (%d leaves unmatched)", checked, unmatched)
}

type exampleInvocation struct {
	line     string
	args     []string
	scaffold bool
}

// exampleInvocations pulls every invocation of this command out of its own
// Example block, as the positionals it passes.
//
// Resolution goes through cobra's own Find rather than a textual path match, so
// an example written with an alias resolves the way the shell resolves it.
// Matching the canonical path missed 36 examples on the destructive computer and
// mobile-device actions, which write `pro comp erase` for
// `pro computers-inventory erase`. A line that pipes a sibling command into this
// one contributes only this command's own half, and a line naming another
// command contributes nothing. A token is a flag's value, rather than a
// positional, when the preceding flag is registered and is not boolean.
func exampleInvocations(root *cobra.Command, leaf commandLeaf) []exampleInvocation {
	var found []exampleInvocation

	for _, line := range strings.Split(leaf.cmd.Example, "\n") {
		line = strings.TrimSpace(line)
		for _, rest := range binaryInvocations(line) {
			cmd, remaining, err := root.Find(rest)
			if err != nil || cmd != leaf.cmd {
				continue
			}
			found = append(found, exampleInvocation{
				line:     line,
				args:     positionalsIn(cmd, remaining),
				scaffold: slices.Contains(remaining, "--scaffold"),
			})
		}
	}
	return found
}

// binaryInvocations splits one Example line into the argument list behind each
// `jamf-cli` on it, up to the next shell separator. A line that pipes one
// command into another yields both halves, which is what lets a reader of the
// head assert on it — exampleInvocations keeps only the half that resolves to
// the leaf, and so saw no pipe head at all.
func binaryInvocations(line string) [][]string {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return nil
	}
	// Split the way a shell would, so a quoted value holding a space stays one
	// token instead of reading as two positionals.
	tokens, err := shlex.Split(line)
	if err != nil {
		tokens = strings.Fields(line)
	}
	var found [][]string
	for i, tok := range tokens {
		if !strings.HasSuffix(tok, "jamf-cli") {
			continue
		}
		rest := tokens[i+1:]
		if end := slices.IndexFunc(rest, isShellSeparator); end >= 0 {
			rest = rest[:end]
		}
		found = append(found, rest)
	}
	return found
}

// TestEveryExampleInvocationNamesACommandThatExists reads every `jamf-cli` on
// every leaf's Example, where TestNoExampleDocumentsAnUndeclaredPositional reads
// only the invocations that resolve to the leaf the Example sits on. That filter
// is what hid the head of every pipe: 13 example lines on 8 resources opened
// with `<resource> get`, on resources that ship no get, so `--help` taught a
// command answering `unknown command "get"` — the same defect as the 22
// singleton examples, on the other half of the same lines.
//
// A parent resolving with a positional left over is the interesting case, since
// cobra's Find stops at the deepest command it knows and reports no error for a
// non-root parent. The arity check repeats one this file already makes for the
// resolved half, over a different population: it caught two more pipe heads on
// resources that do ship a get, of an arity the example had assumed rather than
// read (`pro jamf-protect create` documented `get 1` against a singleton get,
// `pro mdm-renewals patch` a bare `get` against one that requires an id).
func TestEveryExampleInvocationNamesACommandThatExists(t *testing.T) {
	root := NewRootCmd("test", "none", "none", "none")

	checked := 0
	for _, l := range runnableLeaves(root) {
		for _, line := range strings.Split(l.cmd.Example, "\n") {
			for _, rest := range binaryInvocations(line) {
				cmd, remaining, err := root.Find(rest)
				if err != nil {
					t.Errorf("leaf %q: Example line %q names no command: %v", l.path, strings.TrimSpace(line), err)
					continue
				}
				checked++
				args := positionalsIn(cmd, remaining)
				if cmd.HasSubCommands() && len(args) > 0 {
					t.Errorf("leaf %q: Example line %q resolves to %q, which ships no %q subcommand",
						l.path, strings.TrimSpace(line), cmd.CommandPath(), args[0])
					continue
				}
				if cmd.Args == nil {
					continue
				}
				// A --scaffold line is validated with the flag actually set,
				// rather than skipped: the generators lower the floor to zero
				// under it and keep the declared ceiling, so the line is still
				// held to that ceiling. Skipping it was what let issue 363 hide
				// here — 26 leaves whose scaffold cobra refused outright.
				scaffolding := slices.Contains(remaining, "--scaffold") && cmd.Flags().Lookup("scaffold") != nil
				if scaffolding {
					if err := cmd.Flags().Set("scaffold", "true"); err != nil {
						t.Fatalf("leaf %q: setting --scaffold: %v", l.path, err)
					}
				}
				argsErr := cmd.Args(cmd, args)
				if scaffolding {
					if resetErr := cmd.Flags().Set("scaffold", "false"); resetErr != nil {
						t.Fatalf("leaf %q: clearing --scaffold: %v", l.path, resetErr)
					}
				}
				if argsErr != nil {
					t.Errorf("leaf %q: Example line %q passes %d positional(s) to %q, which refuses them: %v",
						l.path, strings.TrimSpace(line), len(args), cmd.CommandPath(), argsErr)
				}
			}
		}
	}

	if checked < 1500 {
		t.Fatalf("only %d example invocations resolved — the line reader is likely not matching the binary name", checked)
	}
	t.Logf("verified %d example invocations name a command that exists", checked)
}

func isShellSeparator(token string) bool {
	switch token {
	case "|", ">", ">>", "&&", ";":
		return true
	}
	return false
}

// positionalsIn reads the tokens after a command path, up to a shell pipe or
// redirect, and returns those that are positionals.
func positionalsIn(cmd *cobra.Command, tokens []string) []string {
	var args []string
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if isShellSeparator(tok) {
			break
		}
		if tok == "--" {
			continue
		}
		if strings.HasPrefix(tok, "-") {
			if flagTakesAValue(cmd, tok) && i+1 < len(tokens) {
				i++
			}
			continue
		}
		args = append(args, tok)
	}
	return args
}

func flagTakesAValue(cmd *cobra.Command, token string) bool {
	if strings.Contains(token, "=") {
		return false
	}
	name := strings.TrimLeft(token, "-")
	// A persistent flag declared by a parent reaches cmd.Flags() only once
	// execution merges it, so an unexecuted command has to be asked separately.
	for _, set := range []*pflag.FlagSet{cmd.Flags(), cmd.InheritedFlags(), cmd.PersistentFlags()} {
		flag := set.Lookup(name)
		// ShorthandLookup panics on anything longer than one character.
		if flag == nil && len(name) == 1 {
			flag = set.ShorthandLookup(name)
		}
		if flag != nil {
			return flag.Value.Type() != "bool"
		}
	}
	// An unregistered flag belongs to an inner command, or to an example naming a
	// flag the command never registered. Either way assume it carries a value, so
	// that value is not counted as a positional and this test reports only the
	// arity defect it exists for.
	return true
}

// TestNoCommandLiteralReadsAnUndeclaredPositional guards a case no tree walk can
// see. guardStrayPositionals reads the Use string, so a command that
// consumes a positional without documenting one would be clamped to NoArgs and
// stop honouring an argument it accepts today. Requiring every command literal
// that reads args to declare an Args validator keeps that command out of the
// guard's reach, and the tree walk then catches a validator that disagrees with
// the Use string. The generator output is scanned too, so a template that starts
// reading args without emitting Args fails here.
func TestNoCommandLiteralReadsAnUndeclaredPositional(t *testing.T) {
	// Walked rather than listed, so a package added later cannot be silently
	// unscanned. A hardcoded list of four directories was already one short:
	// internal/scope contributes get, add and remove to nine classic resources,
	// and it is the guard's over-reach case no tree walk can see, since the walk
	// reads Args after the guard has already set it.
	roots := []string{".", "../scope"}

	fset := gotoken.NewFileSet()
	literals := 0
	perRoot := map[string]int{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, parseErr := parser.ParseFile(fset, path, nil, 0)
			if parseErr != nil {
				return fmt.Errorf("parse %s: %w", path, parseErr)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok || !isCobraCommandType(lit.Type) {
					return true
				}
				literals++
				perRoot[root]++
				if literalReadsArgs(lit) && literalField(lit, "Args") == nil {
					t.Errorf("%s: cobra.Command %s reads a positional but declares no Args validator",
						fset.Position(lit.Pos()), literalUse(lit))
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// Per root, because the total is dominated by one of them: a root that stops
	// contributing anything still clears a total floor.
	for _, root := range roots {
		if perRoot[root] == 0 {
			t.Errorf("root %q contributed no cobra.Command literals — it was renamed, or the path is wrong", root)
		}
	}
	if literals < 500 {
		t.Fatalf("found only %d cobra.Command literals — the scan is likely not reaching the generated trees", literals)
	}
	t.Logf("scanned %d cobra.Command literals", literals)
}

func isCobraCommandType(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Command" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "cobra"
}

func literalField(lit *ast.CompositeLit, name string) ast.Expr {
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, ok := kv.Key.(*ast.Ident); ok && key.Name == name {
			return kv.Value
		}
	}
	return nil
}

// literalUse renders a command literal's Use string for a failure message, or a
// placeholder when it is not a plain string.
func literalUse(lit *ast.CompositeLit) string {
	use, ok := literalField(lit, "Use").(*ast.BasicLit)
	if !ok {
		return "(unknown Use)"
	}
	return use.Value
}

// literalReadsArgs reports whether a command literal consumes a positional.
//
// Any mention of the slice counts, not only indexing it. Handing it to a helper
// consumes it just as much: pro_blueprints.go passes `args` to
// resolveBlueprintID, and the generated venafis command's only reference is
// strings.Join(args, " "), so a walk looking for `args[i]`, `len(args)` and
// `range args` alone reported that command as reading nothing.
//
// A function signature's own `args []string` parameter is not a mention, so each
// closure is walked from its body and its type is skipped. An inner closure that
// shadows the name is reported, which only ever asks for an explicit Args
// validator the command should have anyway.
func literalReadsArgs(lit *ast.CompositeLit) bool {
	found := false
	var inspect func(n ast.Node) bool
	inspect = func(n ast.Node) bool {
		if found {
			return false
		}
		switch e := n.(type) {
		case *ast.FuncLit:
			ast.Inspect(e.Body, inspect)
			return false
		case *ast.Ident:
			found = e.Name == "args"
		}
		return !found
	}
	ast.Inspect(lit, inspect)
	return found
}

// TestStrayPositionalRedactsASecret covers the case where the stray positional
// IS the secret. Omitting --new-password while supplying its value leaves the
// password as args[0], and the refusal reaches stdout as JSON whenever output
// is piped — the CI case. Measured on the branch before the fix:
// `pro comp set-recovery-lock --serial C02X1234 --yes 'S3cur3P@ss123'` printed
// the password verbatim into the error envelope.
//
// Refusing the invocation is still right. The same typo on main ran the command
// with an empty password, and an empty --new-password clears the device's
// existing Recovery Lock, so the guard made that safer. Only the echo was wrong.
//
// The population is derived, not listed: every zero-arity leaf registering a
// secret-bearing string flag has to redact, so a command that gains such a flag
// later is covered without editing this test.
func TestStrayPositionalRedactsASecret(t *testing.T) {
	root := NewRootCmd("test", "none", "none", "none")

	secret, plain := 0, 0
	for _, l := range runnableLeaves(root) {
		count, _ := declaredPositionals(l.cmd.Use, l.cmd.Name())
		if count > 0 || l.cmd.Args == nil {
			continue
		}
		err := l.cmd.Args(l.cmd, []string{"S3cur3P@ss123"})
		if err == nil {
			continue
		}
		if carriesASecretFlag(l.cmd) {
			secret++
			if strings.Contains(err.Error(), "S3cur3P@ss123") {
				t.Errorf("leaf %q registers a secret-bearing flag and reproduced the value in its refusal, which reaches a CI log: %v", l.path, err)
			}
			if !strings.Contains(err.Error(), "<redacted>") {
				t.Errorf("leaf %q redacted nothing: %v", l.path, err)
			}
			continue
		}
		plain++
		// The value has to survive everywhere else, or every typo becomes
		// unreportable to fix three commands.
		if !strings.Contains(err.Error(), "S3cur3P@ss123") {
			t.Errorf("leaf %q carries no secret-bearing flag but hid the value, so the operator cannot see their typo: %v", l.path, err)
		}
	}

	// Both arms must be exercised, or the walk proves nothing.
	if secret == 0 {
		t.Error("no leaf registering a secret-bearing string flag was found, so the redaction is untested")
	}
	if plain == 0 {
		t.Error("no ordinary leaf was found, so nothing pins that the value still appears")
	}
	t.Logf("checked %d secret-bearing and %d ordinary zero-arity leaves", secret, plain)
}

// TestCarriesASecretFlag_MatchesSegmentsNotSubstrings pins the matcher's shape.
// "mapping" contains "pin" and "keychain" contains "key", so a substring test
// would redact the value of flags that carry no secret at all.
func TestCarriesASecretFlag_MatchesSegmentsNotSubstrings(t *testing.T) {
	for _, tc := range []struct {
		flag string
		kind string
		want bool
	}{
		{"new-password", "string", true},
		{"unlock-token", "string", true},
		{"pin", "string", true},
		{"client-secret", "string", true},
		{"api-key", "string", true},
		// A path is worth reporting: the caller needs to see the typo.
		{"token-file", "string", false},
		{"password-file", "string", false},
		// Substring collisions that must not match.
		{"field-mapping", "string", false},
		{"keychain", "string", false},
		{"monkey", "string", false},
		// A boolean named like a secret carries no value to leak.
		{"password", "bool", false},
	} {
		t.Run(tc.flag, func(t *testing.T) {
			cmd := &cobra.Command{Use: "probe"}
			if tc.kind == "bool" {
				var b bool
				cmd.Flags().BoolVar(&b, tc.flag, false, "")
			} else {
				var s string
				cmd.Flags().StringVar(&s, tc.flag, "", "")
			}
			if got := carriesASecretFlag(cmd); got != tc.want {
				t.Errorf("carriesASecretFlag(--%s %s) = %v, want %v", tc.flag, tc.kind, got, tc.want)
			}
		})
	}
}

// TestGuardKeepsAHandDeclaredValidator pins the `cmd.Args == nil` condition in
// guardStrayPositionals. Deleting it so the walk overwrites unconditionally
// left the whole package green: no leaf today documents zero positionals AND
// declares a validator of its own, so the overwrite is silent.
//
// It matters the moment one does. A hand-written zero-arity leaf that declares
// its own validator — to refuse a value with a message of its own, or to accept
// one under a flag — would have it replaced by the generic refusal, and nothing
// would say so.
func TestGuardKeepsAHandDeclaredValidator(t *testing.T) {
	sentinelCalled := false
	sentinel := func(*cobra.Command, []string) error {
		sentinelCalled = true
		return nil
	}

	root := &cobra.Command{Use: "root"}
	// Use documents no positional, so the guard would otherwise claim it.
	leaf := &cobra.Command{Use: "leaf", Run: func(*cobra.Command, []string) {}, Args: sentinel}
	root.AddCommand(leaf)

	guardStrayPositionals(root)

	if !sameFunc(leaf.Args, sentinel) {
		t.Fatal("guardStrayPositionals replaced a hand-declared Args validator, so a leaf's own refusal is silently discarded")
	}
	if err := leaf.Args(leaf, []string{"x"}); err != nil {
		t.Errorf("the sentinel validator no longer decides: %v", err)
	}
	if !sentinelCalled {
		t.Error("the sentinel was never called, so this test proves nothing about which validator runs")
	}

	// The completion half is decided separately and must still be installed:
	// cobra derives no completion from Args, so the leaf would offer filenames
	// for a positional its own validator may well refuse.
	if leaf.ValidArgsFunction == nil {
		t.Error("guardStrayPositionals skipped the completion half for a leaf that declared its own Args")
	}
}

// TestSecretShapedAssignment covers the carrier carriesASecretFlag cannot see.
// --set is a stringArray, not a string, and its NAME holds no secret word while
// its value can. --set is repeatable, so omitting it before the second pair
// drops that pair to a positional:
// `security uem-connectors create --set authStrategy=X deviceSyncAuth.clientSecret=SEKRET`
// echoed the secret into stdout as JSON, and from there into a CI log.
//
// Both directions are load-bearing. Redacting on "does this leaf take --set"
// instead would hide an ordinary typo on several hundred leaves, so the test
// pins the values that must still be reported verbatim.
func TestSecretShapedAssignment(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		// camelCase body keys: a separator is inserted at each lower→upper
		// TRANSITION, so the one segment set reads a body key as well as a
		// flag name.
		{"deviceSyncAuth.clientSecret=SEKRET", true},
		{"account.password=SEKRET", true},
		{"pin=123456", true},
		{"unlock_token=abc", true},
		{"apiKey=abc", true},
		// A run of capitals. Inserting a separator before EVERY uppercase rune
		// shredded these into single letters — "-t-o-k-e-n" — so none matched
		// and the credential was echoed verbatim, while the doc comment claimed
		// the transition rule the code did not implement.
		{"TOKEN=x", true},
		{"CLIENT_SECRET=x", true},
		{"CLIENTSECRET=x", true},
		{"APIKEY=x", true},
		// A run of capitals followed by a camel tail. This is the row that
		// separates the two mechanisms: the transition normalises it to
		// "secret-value" and the exact match reads "secret", while the suffix
		// fallback sees "secretvalue" and no secret word ends it. Without the
		// transition fix nothing catches it — reverting that fix left every
		// other row here passing, because the fallback covers them.
		{"SECRETValue=x", true},
		{"TOKENFile=x", true},
		// No transition and no separator: one opaque segment the exact match
		// cannot reach, so a suffix test carries these.
		{"clientsecret=x", true},
		{"apikey=x", true},
		{"authtoken=x", true},
		// Not secrets: these must keep naming the value, or a typo is unfixable.
		{"authStrategy=JAMF_PRO_OAUTH", false},
		{"general.name=Foo", false},
		// The negatives a plain substring test would have caught: "pin" sits
		// inside "mapping" and "key" starts "keychain". A credential key names
		// the credential LAST, so the fallback matches a suffix.
		{"mapping=x", false},
		{"keychain=x", false},
		{"deviceFieldMappings.userEmailMapping=x", false},
		// Not an assignment at all. The no-"=" case is handled at the call
		// site, gated on --set having been supplied, so this row stays false.
		{"/tmp/out", false},
		{"junkarg", false},
		{"S3cur3P@ss123", false},
	} {
		t.Run(tc.value, func(t *testing.T) {
			if got := secretShapedAssignment(tc.value); got != tc.want {
				t.Errorf("secretShapedAssignment(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// TestStrayPositionalRedactsASecretShapedAssignment pins the WIRING of the
// value-side redaction, which TestSecretShapedAssignment cannot: that one
// checks the predicate alone, and TestStrayPositionalRedactsASecret walks
// leaves carrying a secret FLAG with a value that is not an assignment. So
// deleting `|| secretShapedAssignment(value)` from refuseStrayPositionals left
// both of them passing while the credential went back into the message.
//
// It drives an ordinary leaf — one with no credential flag of its own — because
// that is the population a dropped --set lands on.
func TestStrayPositionalRedactsASecretShapedAssignment(t *testing.T) {
	leaf := &cobra.Command{Use: "create", Run: func(*cobra.Command, []string) {}}
	if carriesASecretFlag(leaf) {
		t.Fatal("the probe leaf must carry no credential flag, or this proves nothing")
	}

	err := refuseStrayPositionals(leaf, []string{"deviceSyncAuth.clientSecret=SEKRET"})
	if err == nil {
		t.Fatal("the stray positional was accepted")
	}
	if strings.Contains(err.Error(), "SEKRET") {
		t.Errorf("the credential survived into the refusal, which reaches stdout as JSON when piped: %v", err)
	}
	if !strings.Contains(err.Error(), "<redacted>") {
		t.Errorf("nothing was redacted: %v", err)
	}

	// An ordinary assignment on the same leaf must still name its value.
	err = refuseStrayPositionals(leaf, []string{"general.name=Foo"})
	if err == nil {
		t.Fatal("the stray positional was accepted")
	}
	if !strings.Contains(err.Error(), "general.name=Foo") {
		t.Errorf("an ordinary assignment was redacted, so a typo is unfixable: %v", err)
	}
}

// TestStrayPositionalRedactsASplitSetPair covers the value half of a --set pair
// typed with a space instead of an "=". Cobra takes the KEY as the flag's one
// element and drops the credential to args[0], where no "=" remains for
// secretShapedAssignment to split on and carriesASecretFlag cannot help — --set
// is a stringArray whose name holds no secret word. That is the invocation
// CLAUDE.md names as discouraged, one keystroke off.
//
// It is gated on --set having been SUPPLIED, which is what the second half
// pins: 122 zero-arity leaves register the flag, so redacting every "="-less
// positional on all of them would answer `create body.json` with <redacted>.
func TestStrayPositionalRedactsASplitSetPair(t *testing.T) {
	newLeaf := func(setPairs ...string) *cobra.Command {
		cmd := &cobra.Command{Use: "create", Run: func(*cobra.Command, []string) {}}
		var pairs []string
		cmd.Flags().StringArrayVar(&pairs, "set", nil, "")
		for _, p := range setPairs {
			if err := cmd.Flags().Set("set", p); err != nil {
				t.Fatalf("setting --set %q: %v", p, err)
			}
		}
		return cmd
	}

	const secret = "S3cr3tRealCred"

	for _, tc := range []struct {
		name string
		set  []string
		arg  string
		want bool // redact
	}{
		// The structural signal is a supplied --set element with no "=", not
		// anything about the value. Keying on the value gave up the moment an
		// "=" appeared in it — and an Intune or Azure application secret
		// routinely carries one, as base64 padding or the character itself.
		{"split pair, plain value", []string{"deviceSyncAuth.clientSecret"}, secret, true},
		{"split pair, base64 padding", []string{"deviceSyncAuth.clientSecret"}, "dGVzdHNlY3JldA==", true},
		{"split pair, literal equals", []string{"deviceSyncAuth.clientSecret"}, "abc=def", true},
		{"split pair, connection string", []string{"deviceSyncAuth.clientSecret"}, "Server=tcp:x;Password=Sup3r=Secret", true},
		// Every pair well formed: the positional is a typo, not a value, and
		// hiding it costs the operator the filename they mistyped on any of the
		// 225 --set-bearing leaves. Supplying --set is exactly what a caller
		// building a body does, so gating on "was --set supplied" alone was not
		// the narrowing it claimed to be.
		{"well-formed pair, filename", []string{"vendor=JAMF_PRO"}, "body.json", false},
		{"two well-formed pairs, typo", []string{"a=1", "b=2"}, "junkarg", false},
		// --set never supplied.
		{"no --set, filename", nil, "body.json", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			leaf := newLeaf(tc.set...)
			err := refuseStrayPositionals(leaf, []string{tc.arg})
			if err == nil {
				t.Fatal("the stray positional was accepted")
			}
			redacted := strings.Contains(err.Error(), "<redacted>")
			if redacted != tc.want {
				t.Errorf("redacted = %v, want %v: %v", redacted, tc.want, err)
			}
			if tc.want && strings.Contains(err.Error(), tc.arg) {
				t.Errorf("the credential survived into the refusal: %v", err)
			}
			if !tc.want && !strings.Contains(err.Error(), tc.arg) {
				t.Errorf("an ordinary value was hidden, so the operator cannot see their typo: %v", err)
			}
		})
	}

	// A leaf with no --set at all is unaffected either way.
	bare := &cobra.Command{Use: "list", Run: func(*cobra.Command, []string) {}}
	err := refuseStrayPositionals(bare, []string{"junkarg"})
	if err == nil || !strings.Contains(err.Error(), "junkarg") {
		t.Errorf("a leaf without --set should name its value: %v", err)
	}
}

// TestSetPairSplitIgnoresAnUnsuppliedDefault pins the Changed gate in
// setPairSplitByASpace, which is otherwise unreachable: all 231 --set
// registrations in the tree use a nil default, so an unsupplied flag yields an
// empty slice and the element loop returns false without the gate. Deleting the
// gate therefore left the whole suite green.
//
// It matters the moment a --set is registered with a default. That default
// would read as a supplied pair, and a bare default with no "=" would redact
// every stray positional on that command.
func TestSetPairSplitIgnoresAnUnsuppliedDefault(t *testing.T) {
	cmd := &cobra.Command{Use: "create", Run: func(*cobra.Command, []string) {}}
	var pairs []string
	// A default with no "=" — the shape that would trip the element loop.
	cmd.Flags().StringArrayVar(&pairs, "set", []string{"deviceSyncAuth.clientSecret"}, "")

	if setPairSplitByASpace(cmd) {
		t.Error("an unsupplied --set default was read as a supplied pair, so every stray positional on this command would redact")
	}

	// Supplying it is what makes the signal real.
	if err := cmd.Flags().Set("set", "deviceSyncAuth.clientSecret"); err != nil {
		t.Fatalf("setting --set: %v", err)
	}
	if !setPairSplitByASpace(cmd) {
		t.Error("a supplied element with no \"=\" is the split-pair signature and was missed")
	}
}

// movedAliasSpellings counts the extra stubs registered under a nested-alias
// spelling of a movedInvocations key, which are the same refusal reachable by a
// second path.
func movedAliasSpellings() int {
	n := 0
	for key := range movedInvocations {
		n += len(movedKeySpellings(key)) - 1
	}
	return n
}
