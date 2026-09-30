// Copyright 2026, Jamf Software LLC

package parser

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// A cobra.Command's help fields and a flag registration's name, shorthand and
// usage in committed generated code must be string literals joined by `+`, a
// flag default may contain no call, and no function literal may be invoked
// outside a defer; any of these means a spec string became code. It does not
// judge the RunE body, where calls between string literals are ordinary.
func TestGeneratedCommandHelpFieldsAreOnlyStringLiterals(t *testing.T) {
	helpFields := map[string]bool{"Use": true, "Short": true, "Long": true, "Example": true}
	for _, tree := range []string{"pro", "platform", "security"} {
		t.Run(tree, func(t *testing.T) {
			files, err := filepath.Glob(filepath.Join("..", "..", "internal", "commands", tree, "generated", "*.go"))
			if err != nil {
				t.Fatal(err)
			}
			var checked, checkedFlags int
			for _, path := range files {
				if strings.HasSuffix(path, "_test.go") {
					continue
				}
				fset := token.NewFileSet()
				file, err := goparser.ParseFile(fset, path, nil, goparser.SkipObjectResolution)
				if err != nil {
					t.Fatalf("%s: %v", path, err)
				}
				deferred := map[*ast.CallExpr]bool{}
				ast.Inspect(file, func(n ast.Node) bool {
					if d, ok := n.(*ast.DeferStmt); ok {
						deferred[d.Call] = true
					}
					return true
				})
				ast.Inspect(file, func(n ast.Node) bool {
					if call, ok := n.(*ast.CallExpr); ok {
						if _, ok := call.Fun.(*ast.FuncLit); ok && !deferred[call] {
							t.Errorf("%s: function literal invoked outside a defer", fset.Position(call.Pos()))
						}
						if method, ok := flagRegistration(call); ok {
							checkedFlags++
							strArgs := []ast.Expr{call.Args[1], call.Args[len(call.Args)-1]}
							if strings.HasSuffix(method, "VarP") {
								strArgs = append(strArgs, call.Args[2])
							}
							for _, arg := range strArgs {
								if !onlyStringLiterals(arg) {
									t.Errorf("%s: %s name, shorthand or usage is not a string literal", fset.Position(arg.Pos()), method)
								}
							}
							for _, arg := range call.Args[1 : len(call.Args)-1] {
								if containsCall(arg) {
									t.Errorf("%s: %s argument contains a call", fset.Position(arg.Pos()), method)
								}
							}
						}
					}
					lit, ok := n.(*ast.CompositeLit)
					if !ok || !isCobraCommand(lit.Type) {
						return true
					}
					for _, elt := range lit.Elts {
						kv, ok := elt.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						key, ok := kv.Key.(*ast.Ident)
						if !ok || !helpFields[key.Name] {
							continue
						}
						checked++
						if !onlyStringLiterals(kv.Value) {
							t.Errorf("%s: cobra.Command %s is not a string literal", fset.Position(kv.Value.Pos()), key.Name)
						}
					}
					return true
				})
			}
			if checked == 0 {
				t.Fatalf("no cobra.Command help fields found in the %s tree; the walk is not reading it", tree)
			}
			if checkedFlags == 0 {
				t.Fatalf("no flag registrations found in the %s tree; the walk is not reading them", tree)
			}
		})
	}
}

func isCobraCommand(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Command" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "cobra"
}

// flagRegistration reports whether call is `<x>.Flags().<Type>Var[P](...)` or
// the PersistentFlags form, and returns the method name.
func flagRegistration(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (!strings.HasSuffix(sel.Sel.Name, "Var") && !strings.HasSuffix(sel.Sel.Name, "VarP")) {
		return "", false
	}
	recv, ok := sel.X.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	flags, ok := recv.Fun.(*ast.SelectorExpr)
	if !ok || (flags.Sel.Name != "Flags" && flags.Sel.Name != "PersistentFlags") {
		return "", false
	}
	minArgs := 4
	if strings.HasSuffix(sel.Sel.Name, "VarP") {
		minArgs = 5
	}
	return sel.Sel.Name, len(call.Args) >= minArgs
}

func containsCall(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if _, ok := n.(*ast.CallExpr); ok {
			found = true
		}
		return !found
	})
	return found
}

func onlyStringLiterals(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return e.Kind == token.STRING
	case *ast.BinaryExpr:
		return e.Op == token.ADD && onlyStringLiterals(e.X) && onlyStringLiterals(e.Y)
	}
	return false
}
