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
		fn:     "buildOutputFormatter",
		reason: "the shared formatter itself — the setters that carry --out-file, --select, --compact, --quiet and --no-hints are applied here",
	},
	{
		file:   "internal/commands/output_route.go",
		fn:     "formatterFor",
		reason: "clones the shared formatter for every command that prints rows, and for the two whose own argument names the format; the fresh formatter is the fallback for a caller reached with a test double or before PersistentPreRunE ran, mirroring writerFor's nil guard",
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

// finding is one unaccounted construction of an output.Formatter.
type finding struct {
	file string
	line int
	fn   string
	form string
}

// result carries both directions of the check: constructions no exemption
// covers, and exemptions that cover no construction. A stale entry fails the
// lint so a fixed site cannot leave its excuse behind.
type result struct {
	findings []finding
	stale    []exemption
}

func (r result) clean() bool {
	return len(r.findings) == 0 && len(r.stale) == 0
}

// scan walks root and reports every construction of an output.Formatter that
// no exemption accounts for. Test files are skipped: a test needs a formatter
// to assemble a CLIContext, and no global output flag is set in a unit test.
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

		for _, built := range constructions(file, local) {
			pos := fset.Position(built.pos)
			if i := exemptionFor(exemptions, rel, built.fn); i >= 0 {
				matched[i] = true
				continue
			}
			res.findings = append(res.findings, finding{file: rel, line: pos.Line, fn: built.fn, form: built.form})
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

const (
	constructorName = "New"
	formatterType   = "Formatter"
)

type construction struct {
	pos  token.Pos
	fn   string
	form string
}

// constructions returns every way the file builds an output.Formatter, tagged
// with the function that encloses it. A construction outside any function
// declaration reports an empty name, which no exemption can match.
//
// Two forms, because the constructor is not the only one. Every field of
// output.Formatter is unexported but the type and its setters are not, so
// `&output.Formatter{}` followed by SetWriter builds a working second formatter
// that never names New. A rule matching only the constructor would report the
// tree clean while that shape carried the defect, which is worse than no rule,
// since a reviewer would then trust the silence.
func constructions(file *ast.File, local string) []construction {
	var found []construction
	for _, decl := range file.Decls {
		fn := enclosingName(decl)
		ast.Inspect(decl, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.SelectorExpr:
				// The selector rather than the call, so `f := output.New`
				// followed by `f(...)` is not a way past the rule either.
				if isPackageIdent(node.X, local) && node.Sel.Name == constructorName {
					found = append(found, construction{node.Sel.Pos(), fn, "output.New"})
				}
			case *ast.CompositeLit:
				if isFormatterType(node.Type, local) {
					found = append(found, construction{node.Pos(), fn, "output.Formatter literal"})
				}
			case *ast.CallExpr:
				if local != "." {
					return true
				}
				if id, ok := node.Fun.(*ast.Ident); ok && id.Name == constructorName {
					found = append(found, construction{node.Lparen, fn, "dot-imported New"})
				}
			}
			return true
		})
	}
	return found
}

// enclosingName names the function an exemption keys on. A method carries its
// receiver type, because two same-named methods on different types in one file
// would otherwise share a single exemption slot: exempting one would silently
// exempt the other, which is the per-file looseness the per-function key was
// chosen to avoid.
func enclosingName(decl ast.Decl) string {
	fd, ok := decl.(*ast.FuncDecl)
	if !ok {
		return ""
	}
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	return receiverName(fd.Recv.List[0].Type) + "." + fd.Name.Name
}

func receiverName(expr ast.Expr) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if idx, ok := expr.(*ast.IndexExpr); ok { // a generic receiver, Type[T]
		expr = idx.X
	}
	if id, ok := expr.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func isPackageIdent(expr ast.Expr, local string) bool {
	id, ok := expr.(*ast.Ident)
	return ok && id.Name == local
}

// isFormatterType reports whether a composite literal builds an
// output.Formatter. A reference to the type in any other position is left
// alone: cliOutput embeds one and formatterFor returns one, and neither
// constructs anything.
func isFormatterType(expr ast.Expr, local string) bool {
	if local == "." {
		id, ok := expr.(*ast.Ident)
		return ok && id.Name == formatterType
	}
	sel, ok := expr.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == formatterType && isPackageIdent(sel.X, local)
}
