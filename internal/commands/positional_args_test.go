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
	"github.com/spf13/cobra"
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
			// cobra.NoArgs reports "unknown command", which ClassifyError maps to
			// a usage exit. A bounded validator's "accepts at most" message is not
			// classified on this branch, so only the zero-arity refusal is checked
			// for its code here.
			if code := exitcode.CodeFrom(ClassifyError(err)); code != exitcode.Usage {
				t.Errorf("leaf %q: stray positional exits %d, want %d (usage)", l.path, code, exitcode.Usage)
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

// TestNoCommandLiteralReadsAnUndeclaredPositional guards a case no tree walk can
// see. guardStrayPositionals reads the Use string, so a command that
// consumes a positional without documenting one would be clamped to NoArgs and
// stop honouring an argument it accepts today. Requiring every command literal
// that reads args to declare an Args validator keeps that command out of the
// guard's reach, and the tree walk then catches a validator that disagrees with
// the Use string. The generator output is scanned too, so a template that starts
// reading args without emitting Args fails here.
func TestNoCommandLiteralReadsAnUndeclaredPositional(t *testing.T) {
	dirs := []string{".", "pro/generated", "platform/generated", "security/generated"}

	fset := gotoken.NewFileSet()
	literals := 0
	for _, dir := range dirs {
		paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
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
				if literalReadsArgs(lit) && literalField(lit, "Args") == nil {
					t.Errorf("%s: cobra.Command %s reads a positional but declares no Args validator",
						fset.Position(lit.Pos()), literalUse(lit))
				}
				return true
			})
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

// literalReadsArgs reports whether a command literal consumes a positional:
// indexing, slicing, ranging over or measuring the args slice. An inner closure
// that shadows the name is reported too, which only ever asks for an explicit
// Args validator the command should have anyway.
func literalReadsArgs(lit *ast.CompositeLit) bool {
	isArgs := func(expr ast.Expr) bool {
		id, ok := expr.(*ast.Ident)
		return ok && id.Name == "args"
	}
	found := false
	ast.Inspect(lit, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.IndexExpr:
			found = found || isArgs(e.X)
		case *ast.SliceExpr:
			found = found || isArgs(e.X)
		case *ast.RangeStmt:
			found = found || isArgs(e.X)
		case *ast.CallExpr:
			if fn, ok := e.Fun.(*ast.Ident); ok && fn.Name == "len" && len(e.Args) == 1 {
				found = found || isArgs(e.Args[0])
			}
		}
		return !found
	})
	return found
}
