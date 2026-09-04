// Copyright 2026, Jamf Software LLC

package commands

import (
	"go/ast"
	"go/parser"
	gotoken "go/token" // the package already declares a `token` variable
	"path/filepath"
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
	documented := 0
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
		if err := l.cmd.Args(l.cmd, strayArgs(count+1)); err == nil {
			t.Errorf("leaf %q documents %d positional(s) in Use %q but accepted %d", l.path, count, l.cmd.Use, count+1)
		} else if count == 0 {
			// refuseStrayPositionals carries exit 2 itself. A bounded validator
			// still answers cobra's unclassified "accepts at most" message and
			// exits 1, so only the zero-arity refusal is checked for its code.
			if code := exitcode.CodeFrom(ClassifyError(err)); code != exitcode.Usage {
				t.Errorf("leaf %q: stray positional exits %d, want %d (usage)", l.path, code, exitcode.Usage)
			}
			// Asserted without ClassifyError too, so the code cannot come to
			// depend on a message prefix again.
			if code := exitcode.CodeFrom(err); code != exitcode.Usage {
				t.Errorf("leaf %q: refusal does not carry its own exit code, got %d", l.path, code)
			}
		}
		if err := l.cmd.Args(l.cmd, strayArgs(count)); err != nil {
			t.Errorf("leaf %q documents %d positional(s) in Use %q but refused that many: %v", l.path, count, l.cmd.Use, err)
		}
	}

	for path, reason := range unboundedPositionalLeaves {
		if !seenUnbounded[path] {
			t.Errorf("unboundedPositionalLeaves names %q (%s), which is not a leaf this binary ships", path, reason)
		}
	}

	if len(leaves) < 700 {
		t.Fatalf("found only %d runnable leaves — tree walk likely broken", len(leaves))
	}
	if documented < 100 {
		t.Fatalf("only %d leaves document a positional — the arity reader is likely returning 0 for everything", documented)
	}
	t.Logf("verified %d runnable leaves (%d documenting a positional, %d unbounded)", len(leaves), documented, len(seenUnbounded))
}

// unreachableScaffoldLeaves is the number of leaves whose --scaffold cannot be
// invoked at all, because the modern Pro generator emits a bare ExactArgs and no
// scaffold relaxation, so an operation under a path parameter with no --name
// lookup demands an identifier its scaffold makes no request with. That is the
// opposite defect to issue 350 and is not fixed here: relaxing those validators
// needs each RunE checked for reading args before its scaffold return, which is
// its own change. Held as a count so a new one fails this test, and so does the
// fix, which drops the number to zero.
const unreachableScaffoldLeaves = 26

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
// Only the ceiling is asserted. An example the validator refuses for supplying
// too FEW positionals is the opposite defect, counted by
// unreachableScaffoldLeaves and not fixed here.
func TestNoExampleDocumentsAnUndeclaredPositional(t *testing.T) {
	leaves := runnableLeaves(NewRootCmd("test", "none", "none", "none"))

	checked := 0
	for _, l := range leaves {
		count, variadic := declaredPositionals(l.cmd.Use, l.cmd.Name())
		if variadic {
			continue
		}
		for _, invocation := range exampleInvocations(l.cmd, l.path) {
			checked++
			if len(invocation.args) > count {
				t.Errorf("leaf %q documents %d positional(s) in Use %q but its Example says %q, which passes %d",
					l.path, count, l.cmd.Use, invocation.line, len(invocation.args))
			}
		}
	}

	if checked < 500 {
		t.Fatalf("only %d example invocations parsed — the Example reader is likely not matching command paths", checked)
	}
	t.Logf("verified %d example invocations against their declared arity", checked)
}

type exampleInvocation struct {
	line string
	args []string
}

// exampleInvocations pulls every invocation of this command out of its own
// Example block, as the positionals it passes.
//
// It matches on the command's full path, so a line that pipes a sibling command
// into this one contributes only this command's own half, and a line naming no
// leaf contributes nothing. A token is a flag's value, rather than a positional,
// when the preceding flag is registered on the command and is not boolean.
func exampleInvocations(cmd *cobra.Command, path string) []exampleInvocation {
	pathWords := strings.Fields(path)
	var found []exampleInvocation

	for _, line := range strings.Split(cmd.Example, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Split the way a shell would, so a quoted value holding a space stays
		// one token instead of reading as two positionals.
		tokens, err := shlex.Split(line)
		if err != nil {
			tokens = strings.Fields(line)
		}
		for i := 0; i < len(tokens); i++ {
			if !strings.HasSuffix(tokens[i], "jamf-cli") || !tokensMatch(tokens[i+1:], pathWords) {
				continue
			}
			args := positionalsIn(cmd, tokens[i+1+len(pathWords):])
			found = append(found, exampleInvocation{line: line, args: args})
		}
	}
	return found
}

func tokensMatch(tokens, want []string) bool {
	if len(tokens) < len(want) {
		return false
	}
	for i, w := range want {
		if tokens[i] != w {
			return false
		}
	}
	return true
}

// positionalsIn reads the tokens after a command path, up to a shell pipe or
// redirect, and returns those that are positionals.
func positionalsIn(cmd *cobra.Command, tokens []string) []string {
	var args []string
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if tok == "|" || tok == ">" || tok == "&&" || tok == ";" {
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
	// Every package that builds a command reachable from NewRootCmd. internal/scope
	// is one: NewScopeCmd contributes get, add and remove to nine classic
	// resources, and it is the guard's over-reach case that no tree walk can see,
	// since the walk reads Args after the guard has already set it.
	dirs := []string{".", "pro/generated", "platform/generated", "security/generated", "../scope"}

	fset := gotoken.NewFileSet()
	literals := 0
	for _, dir := range dirs {
		paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		perDir := 0
		for _, path := range paths {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok || !isCobraCommandType(lit.Type) {
					return true
				}
				literals++
				perDir++
				if literalReadsArgs(lit) && literalField(lit, "Args") == nil {
					t.Errorf("%s: cobra.Command %s reads a positional but declares no Args validator",
						fset.Position(lit.Pos()), literalUse(lit))
				}
				return true
			})
		}
		// Per directory, because the total is dominated by two of them: dropping
		// both platform/generated and security/generated still clears a total
		// floor, so only this catches a renamed or mistyped path.
		if perDir == 0 {
			t.Errorf("directory %q contributed no cobra.Command literals — it was renamed, or the path is wrong", dir)
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
