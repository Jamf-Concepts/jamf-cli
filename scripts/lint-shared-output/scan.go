// Copyright 2026, Jamf Software LLC

package main

import (
	"fmt"
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
		file:   "internal/commands/pro_bulk.go",
		fn:     "bulkPreviewTable",
		reason: "newBulkCmd's Long documents the contract this preview keeps: preview table on stdout, mutation log on stderr. It is the whole standard-output product of a run without --yes, and it stays a table whatever -o says, so it cannot come from the shared formatter and must not follow --out-file into the data file",
	},
	{
		file:   "internal/commands/pro_device_actions.go",
		fn:     "deviceActionPreviewTable",
		reason: "the bulk-targeting preview, keeping the same documented contract as bulkPreviewTable. Extracted from executeAction so the exemption covers three lines rather than that function's whole body, where a later output.New on a result-printing path would have been exempt in advance",
	},
}

// finding is one unaccounted construction of an output.Formatter.
type finding struct {
	file string
	line int
	fn   string
	form string
}

// staleReason separates an exemption whose site stopped building a formatter
// from one whose key stopped naming a site at all. Both fail the lint and the
// remedies are opposite: the first entry is deleted, the second is corrected.
// Reported as one class, "no longer builds a formatter" sent the reader to
// delete an entry whose path merely held a typo.
type staleReason int

const (
	siteBuildsNothing staleReason = iota
	fileNotFound
	funcNotFound
)

// stale is an exemption that covered no construction, with the reason it did
// not.
type stale struct {
	exemption
	reason staleReason
}

// result carries both directions of the check: constructions no exemption
// covers, and exemptions that cover no construction. A stale entry fails the
// lint so a fixed site cannot leave its excuse behind.
type result struct {
	findings []finding
	stale    []stale
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
	// Every function each scanned file declares, so a stale entry can say
	// whether its key names a site at all.
	declared := map[string]map[string]bool{}

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

		names := map[string]bool{}
		for _, decl := range file.Decls {
			if name := enclosingName(decl); name != "" {
				names[name] = true
			}
		}
		declared[rel] = names

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
		if matched[i] || !strings.HasPrefix(e.file, filepath.ToSlash(filepath.Clean(root))) {
			continue
		}
		res.stale = append(res.stale, stale{exemption: e, reason: staleReasonFor(declared, e)})
	}
	return res, nil
}

// staleReasonFor asks, of an exemption that covered nothing, which half of its
// key failed.
func staleReasonFor(declared map[string]map[string]bool, e exemption) staleReason {
	names, scanned := declared[e.file]
	switch {
	case !scanned:
		return fileNotFound
	// A closure is keyed on the function that holds it, which is the name the
	// file declares.
	case !names[strings.SplitN(e.fn, ".func", 2)[0]]:
		return funcNotFound
	default:
		return siteBuildsNothing
	}
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

const formatterType = "Formatter"

type construction struct {
	pos  token.Pos
	fn   string
	form string
}

// isConstructorName reports whether a name internal/output exports builds a
// formatter. The match is a prefix rather than the single name New because a
// second constructor — NewPlain, say — would otherwise be missed outright. A
// future New… returning something else is a false positive with an obvious
// remedy, which is the better of the two failures: a missed shape reports the
// tree clean while it carries the defect, and a reviewer then trusts silence.
func isConstructorName(name string) bool {
	return strings.HasPrefix(name, "New")
}

// constructions returns every way the file builds an output.Formatter, tagged
// with the function that encloses it. A construction outside any function
// declaration reports an empty name, which no exemption can match.
//
// Four forms, because the constructor is not the only one. Every field of
// output.Formatter is unexported but the type and its setters are not, so a
// value of the type built any other way is a working second formatter that
// never names a constructor: `&output.Formatter{}` and `new(output.Formatter)`
// each followed by SetWriter, and an element of a composite literal whose own
// type is elided.
func constructions(file *ast.File, local string) []construction {
	var found []construction
	for _, decl := range file.Decls {
		collectConstructions(decl, enclosingName(decl), local, &found)
	}
	return found
}

// collectConstructions attributes each construction to the function that
// immediately encloses it, function literals included. The enclosing name is
// resolved at the construction site rather than once per declaration because an
// exemption records why one function cannot print through the shared formatter,
// which says nothing about a callback written inside it: keyed on the outer
// name, a closure in an exempt function inherited that exemption and built a
// second formatter with nothing reported.
func collectConstructions(root ast.Node, fn, local string, found *[]construction) {
	literals := 0
	ast.Inspect(root, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncLit:
			literals++
			// Go's own name for a closure, so the report names a site its
			// reader can find, and no exemption keyed on the enclosing
			// function can reach it.
			collectConstructions(node.Body, fmt.Sprintf("%s.func%d", fn, literals), local, found)
			return false
		case *ast.SelectorExpr:
			// The selector rather than the call, so `f := output.New`
			// followed by `f(...)` is not a way past the rule either.
			if isPackageIdent(node.X, local) && isConstructorName(node.Sel.Name) {
				*found = append(*found, construction{node.Sel.Pos(), fn, "output." + node.Sel.Name})
			}
		case *ast.CompositeLit:
			// A literal with no type of its own is an element of an enclosing
			// literal, and is reported from there with the type it inherits.
			if node.Type != nil {
				collectLiteralConstructions(node, node.Type, fn, local, found)
			}
		case *ast.CallExpr:
			if id, ok := node.Fun.(*ast.Ident); ok && id.Name == "new" &&
				len(node.Args) == 1 && isFormatterType(node.Args[0], local) {
				*found = append(*found, construction{node.Pos(), fn, "new(output.Formatter)"})
			}
			if local != "." {
				return true
			}
			if id, ok := node.Fun.(*ast.Ident); ok && isConstructorName(id.Name) {
				*found = append(*found, construction{node.Lparen, fn, "dot-imported " + id.Name})
			}
		}
		return true
	})
}

// collectLiteralConstructions reports every formatter a composite literal
// builds. A slice, array or map literal may elide its elements' type, so
// `[]output.Formatter{{}}` names the type once and the element that actually
// builds the formatter carries no type at all — the shape a rule reading only a
// literal's own Type field cannot see.
func collectLiteralConstructions(lit *ast.CompositeLit, typ ast.Expr, fn, local string, found *[]construction) {
	if isFormatterType(typ, local) {
		*found = append(*found, construction{lit.Pos(), fn, "output.Formatter literal"})
		return
	}
	elem := elidedElementType(typ)
	if elem == nil {
		return
	}
	for _, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			el = kv.Value
		}
		// An element that writes its own type is reported where it is written,
		// so descending into it here would count it twice. `gofmt -s` elides
		// such a type, which is why no fixture in this repo can hold one.
		if inner, ok := el.(*ast.CompositeLit); ok && inner.Type == nil {
			collectLiteralConstructions(inner, elem, fn, local, found)
		}
	}
}

// elidedElementType returns the type an element of a composite literal takes
// when it writes none of its own. Only array, slice and map literals permit the
// elision; a struct literal's field values do not.
func elidedElementType(typ ast.Expr) ast.Expr {
	switch t := typ.(type) {
	case *ast.ArrayType:
		return t.Elt
	case *ast.MapType:
		return t.Value
	}
	return nil
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

// isFormatterType reports whether expr names the output.Formatter type in a
// position that builds one. A reference to the type anywhere else is left
// alone: cliOutput embeds one and formatterFor returns one, and neither
// constructs anything. The pointer is unwrapped because an element of a
// []*output.Formatter literal elides the `&` along with the type.
func isFormatterType(expr ast.Expr, local string) bool {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if local == "." {
		id, ok := expr.(*ast.Ident)
		return ok && id.Name == formatterType
	}
	sel, ok := expr.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == formatterType && isPackageIdent(sel.X, local)
}
