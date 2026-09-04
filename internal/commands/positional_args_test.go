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

// isNoPositionalCompletion reports whether the guard installed its own
// completion function, as opposed to the command declaring one.
func isNoPositionalCompletion(fn func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)) bool {
	if fn == nil {
		return false
	}
	return sameFunc(fn, noPositionalCompletion)
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
	for _, l := range leaves {
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

// unreachableScaffoldLeaves is the number of leaves whose --scaffold cannot be
// invoked at all, because the modern Pro generator emits a bare ExactArgs and no
// scaffold relaxation, so an operation under one or more path parameters with no
// --name lookup demands identifiers its scaffold makes no request with
// (`enrollment-customization-panels update` sits under two). That is the
// opposite defect to issue 350 and is not fixed here: relaxing those validators
// needs each RunE checked for reading args before its scaffold return, which is
// its own change. Held as a count so a new one fails this test, and so does the
// fix, which drops the number to zero.
const unreachableScaffoldLeaves = 26

// unmatchedExampleLeaves is the number of leaves whose Example resolves to no
// invocation of that leaf, so this test reads nothing for them.
//
// Eighteen are internal/scope's add and remove across nine classic resources,
// whose examples are written without the binary name (`scope add "Deploy
// Chrome" …`). The other three carry an example that resolves to a different
// command than the leaf it sits on: `pro mobile-devices delete` documents
// `pro classic-mobile-devices delete`, `pro packages sync` documents
// `pro jcds sync`, and `pro computer-groups get` belongs to the second of two
// identical computer-groups subtrees, the generated registry calling
// NewComputerGroupsCmd twice, so `pro --help` prints the row twice and Find
// resolves every path under it to the first copy. All three are pre-existing and
// are a question about the example, or about the registry, rather than about
// arity.
//
// Pinned so a reader that stops matching a form it handles today, or a new
// unmatchable form, fails rather than quietly shrinking the population.
const unmatchedExampleLeaves = 21

// TestScaffoldKeepsTheDeclaredPositionalCeiling covers the path the walk above
// cannot see, because that walk reads each validator with no flag set.
//
// The classic and the platform generator both relax a command's Args validator
// while --scaffold is set, since --scaffold needs no identifier and cobra
// validates Args before RunE. Relaxing it to no validator at all reopened issue
// 350 behind a flag: `pro classic-jwt-configs update aaa bbb ccc --scaffold`
// printed the template and discarded three positionals with exit 0.
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
		if l.cmd.Args(l.cmd, nil) != nil {
			unreachable++
		}
	}

	if unreachable != unreachableScaffoldLeaves {
		t.Errorf("%d leaves cannot reach their own --scaffold, want %d: a relaxation was added or removed without moving unreachableScaffoldLeaves", unreachable, unreachableScaffoldLeaves)
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
// Only the ceiling is asserted, because the floor is the opposite defect and is
// not fixed here: a bounded validator that refuses too FEW positionals is the
// same too-strict shape unreachableScaffoldLeaves counts, and asserting the
// floor over every leaf would report all 26 of those as well.
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
			// A --scaffold example is exempt from the floor: the flag makes no
			// request and needs no identifier, and a validator that demands one
			// anyway is the pre-existing too-strict defect
			// unreachableScaffoldLeaves counts.
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
				// A --scaffold example is exempt, as it is in the test above:
				// 26 leaves demand an identifier their scaffold makes no
				// request with, which is the pre-existing too-strict defect
				// unreachableScaffoldLeaves counts.
				if cmd.Args == nil || slices.Contains(remaining, "--scaffold") {
					continue
				}
				if err := cmd.Args(cmd, args); err != nil {
					t.Errorf("leaf %q: Example line %q passes %d positional(s) to %q, which refuses them: %v",
						l.path, strings.TrimSpace(line), len(args), cmd.CommandPath(), err)
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
