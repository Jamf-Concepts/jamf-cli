// Copyright 2026, Jamf Software LLC

package useragent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestEveryUserAgentComesFromThisPackage refuses a User-Agent built from a
// string literal anywhere under internal/ (generated code included): a
// req.Header.Set("User-Agent", …) or a WithUserAgent(…) whose value spells
// any text of its own. A value passed through a variable or parameter is
// plumbing and is allowed; its origin is one of the literal-free call sites.
// The Jamf Pro client sent "jamf-cli/1.0" for every release while the four
// SDK clients sent the real version, because each site built its own string.
func TestEveryUserAgentComesFromThisPackage(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		if filepath.Dir(path) == filepath.Join(root, "useragent") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			var value ast.Expr
			switch {
			case (sel.Sel.Name == "Set" || sel.Sel.Name == "Add") && len(call.Args) == 2 && isUserAgentKey(call.Args[0]):
				value = call.Args[1]
			case sel.Sel.Name == "WithUserAgent" && len(call.Args) == 1:
				value = call.Args[0]
			default:
				return true
			}
			checked++
			if containsStringLiteral(value) {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s:%d builds a User-Agent from a literal; use useragent.String()",
					rel, fset.Position(call.Pos()).Line)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 8 {
		t.Fatalf("checked only %d User-Agent sites; the walk is not seeing the clients", checked)
	}
}

func isUserAgentKey(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	s, err := strconv.Unquote(lit.Value)
	return err == nil && strings.EqualFold(s, "User-Agent")
}

func containsStringLiteral(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			found = true
		}
		return !found
	})
	return found
}
