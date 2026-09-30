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

// A cobra.Command's help fields in committed generated code must be string
// literals joined by `+`; anything else there means a spec string became code.
func TestGeneratedCommandHelpFieldsAreOnlyStringLiterals(t *testing.T) {
	helpFields := map[string]bool{"Use": true, "Short": true, "Long": true, "Example": true}
	var files []string
	for _, tree := range []string{"pro", "platform", "security"} {
		matches, err := filepath.Glob(filepath.Join("..", "..", "internal", "commands", tree, "generated", "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	var checked int
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := goparser.ParseFile(fset, path, nil, goparser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
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
		t.Fatal("no cobra.Command help fields found; the walk is not reading the generated trees")
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

func onlyStringLiterals(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return e.Kind == token.STRING
	case *ast.BinaryExpr:
		return e.Op == token.ADD && onlyStringLiterals(e.X) && onlyStringLiterals(e.Y)
	}
	return false
}
