// Copyright 2026, Jamf Software LLC

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
)

// outputPkgPath is the package whose New constructor this lint governs.
const outputPkgPath = "github.com/Jamf-Concepts/jamf-cli/internal/output"

// exemption names one call site allowed to build a formatter of its own, and
// why it cannot print through the shared one. It is keyed by file and enclosing
// function rather than by file alone: root.go holds the sanctioned construction
// site, and `commands -o json --out-file` wrote a 0-byte file for as long as a
// second site in that same file was free to build its own formatter.
type exemption struct {
	file   string
	fn     string
	reason string
}

// defaultExemptions is the whole set. A site absent from it is a finding.
var defaultExemptions = []exemption{
	{
		file:   "internal/commands/root.go",
		fn:     "NewRootCmd",
		reason: "the shared formatter itself — the setters that carry --out-file, --select, --compact, --quiet and --no-hints are applied here",
	},
	{
		file:   "internal/commands/pro_bulk.go",
		fn:     "bulkPreviewTable",
		reason: "a confirmation preview is prompt decoration rather than the command's output: it stays a table whatever -o says, and must not follow --out-file into the data file",
	},
	{
		file:   "internal/commands/pro_device_actions.go",
		fn:     "executeAction",
		reason: "the bulk-targeting confirmation preview, for the same reason as bulkPreviewTable",
	},
}

// finding is one unaccounted call to output.New.
type finding struct {
	file string
	line int
	fn   string
}

// result carries both directions of the check: call sites no exemption covers,
// and exemptions that cover no call site. A stale entry fails the lint so a
// fixed site cannot leave its excuse behind.
type result struct {
	findings []finding
	stale    []exemption
}

func (r result) clean() bool {
	return len(r.findings) == 0 && len(r.stale) == 0
}

// scan walks root and reports every call to the output package's New
// constructor that no exemption accounts for. Test files are skipped: a test
// needs a formatter to assemble a CLIContext, and no global output flag is set
// in a unit test.
func scan(root string, exemptions []exemption) (result, error) {
	var res result
	matched := make([]bool, len(exemptions))

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		rel := filepath.ToSlash(filepath.Clean(path))
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}

		local, ok := importName(file, outputPkgPath)
		if !ok {
			return nil
		}

		for _, call := range constructorCalls(file, local) {
			pos := fset.Position(call.pos)
			if i := exemptionFor(exemptions, rel, call.fn); i >= 0 {
				matched[i] = true
				continue
			}
			res.findings = append(res.findings, finding{file: rel, line: pos.Line, fn: call.fn})
		}
		return nil
	})
	if err != nil {
		return result{}, err
	}

	for i, e := range exemptions {
		if !matched[i] && strings.HasPrefix(e.file, filepath.ToSlash(filepath.Clean(root))) {
			res.stale = append(res.stale, e)
		}
	}
	return res, nil
}

func exemptionFor(exemptions []exemption, file, fn string) int {
	for i, e := range exemptions {
		if e.file == file && e.fn == fn {
			return i
		}
	}
	return -1
}

// importName returns the identifier the file refers to pkgPath by. A file that
// does not import the package at all cannot be calling its constructor, whatever
// a local identifier happens to be named.
func importName(file *ast.File, pkgPath string) (string, bool) {
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != pkgPath {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name, imp.Name.Name != "_"
		}
		return filepath.Base(path), true
	}
	return "", false
}

type constructorCall struct {
	pos token.Pos
	fn  string
}

// constructorCalls returns every call to the output package's New in the file,
// tagged with the function that encloses it. A call outside any function
// declaration reports an empty name, which no exemption can match.
func constructorCalls(file *ast.File, local string) []constructorCall {
	var calls []constructorCall
	for _, decl := range file.Decls {
		fn := ""
		if fd, ok := decl.(*ast.FuncDecl); ok {
			fn = fd.Name.Name
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if isConstructor(call.Fun, local) {
				calls = append(calls, constructorCall{pos: call.Lparen, fn: fn})
			}
			return true
		})
	}
	return calls
}

// isConstructor reports whether fun names the output package's New. The bare
// identifier is matched as well as the qualified one, because a dot-import
// would otherwise be a one-character way past this lint.
func isConstructor(fun ast.Expr, local string) bool {
	if local == "." {
		id, ok := fun.(*ast.Ident)
		return ok && id.Name == "New"
	}
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "New" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == local
}
