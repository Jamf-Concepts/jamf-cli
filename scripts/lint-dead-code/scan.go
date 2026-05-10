// Copyright 2026, Jamf Software LLC

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type deadFlag struct {
	file       string
	line       int
	flagName   string
	backingVar string
}

type deadFunc struct {
	file string
	line int
	name string
}

type findings struct {
	deadFlags []deadFlag
	deadFuncs []deadFunc
}

// flagBinding records a discovered `FlagSet.XVar(&backingVar, "flag-name", ...)`
// call. cmdVar is the *cobra.Command variable the flag was registered on, if it
// could be statically resolved (otherwise empty).
type flagBinding struct {
	file       string
	line       int
	flagName   string
	backingVar string
	cmdVar     string
	pkg        string
}

type funcDeclInfo struct {
	file string
	line int
	name string
	keep bool
	pkg  string
}

// scan walks root, identifies dead flag bindings and dead unexported funcs.
func scan(root string) (findings, error) {
	fset := token.NewFileSet()

	// Discover package directories.
	var pkgDirs []string
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			pkgDirs = append(pkgDirs, path)
		}
		return nil
	}); err != nil {
		return findings{}, err
	}

	var (
		allFiles     []*ast.File
		nonTestFiles []*ast.File
		fileToPath   = map[*ast.File]string{}
		fileToPkg    = map[*ast.File]string{}
	)

	for _, dir := range pkgDirs {
		// ParseDir is fine here: this tool only inspects identifier graphs and
		// doesn't need build-tag-aware package association. Pulling in
		// golang.org/x/tools/go/packages just to silence the deprecation would
		// be substantial overhead for a script-level walker.
		//nolint:staticcheck // SA1019: ParseDir is sufficient for this analyzer.
		pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
			return strings.HasSuffix(fi.Name(), ".go")
		}, parser.ParseComments)
		if err != nil {
			return findings{}, err
		}
		for _, pkg := range pkgs {
			for path, f := range pkg.Files {
				fileToPath[f] = path
				fileToPkg[f] = dir
				allFiles = append(allFiles, f)
				if !strings.HasSuffix(path, "_test.go") {
					nonTestFiles = append(nonTestFiles, f)
				}
			}
		}
	}

	// 1. Per-command allowlists: "cmdVar@pkg" -> {flagName: true}.
	cmdAllowlists := collectCommandAllowlists(nonTestFiles, fileToPkg)

	// 2. Flag bindings.
	var bindings []flagBinding
	for _, f := range nonTestFiles {
		bindings = append(bindings, collectFlagBindings(f, fset, fileToPath[f], fileToPkg[f])...)
	}

	// 3. Unexported top-level funcs.
	var funcs []funcDeclInfo
	for _, f := range nonTestFiles {
		funcs = append(funcs, collectFuncDecls(f, fset, fileToPath[f], fileToPkg[f])...)
	}

	// 4. Read counts for flag backing vars.
	flagReads := map[string]map[string]int{} // pkg -> var -> count
	for _, b := range bindings {
		if flagReads[b.pkg] == nil {
			flagReads[b.pkg] = map[string]int{}
		}
		flagReads[b.pkg][b.backingVar] = 0
	}

	// Build exclude-set of binding-site positions (the &IDENT use).
	exclude := map[token.Pos]bool{}
	for _, f := range nonTestFiles {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if !isFlagSetVarCall(call) {
				return true
			}
			if len(call.Args) < 1 {
				return true
			}
			unary, ok := call.Args[0].(*ast.UnaryExpr)
			if !ok || unary.Op != token.AND {
				return true
			}
			if id, ok := unary.X.(*ast.Ident); ok {
				exclude[id.Pos()] = true
			}
			return true
		})
	}

	for _, f := range allFiles {
		pkg := fileToPkg[f]
		pkgMap, hasFlags := flagReads[pkg]
		ast.Inspect(f, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			if exclude[id.Pos()] {
				return true
			}
			// Skip ValueSpec / FuncDecl declaration-site idents.
			if id.Obj != nil && id.Obj.Pos() == id.Pos() {
				return true
			}
			if hasFlags {
				if _, tracked := pkgMap[id.Name]; tracked {
					pkgMap[id.Name]++
				}
			}
			return true
		})
	}

	// 5. Read counts for unexported funcs.
	funcReads := map[string]map[string]int{}
	for _, fn := range funcs {
		if funcReads[fn.pkg] == nil {
			funcReads[fn.pkg] = map[string]int{}
		}
		funcReads[fn.pkg][fn.name] = 0
	}
	// Track FuncDecl name positions to exclude declarations themselves.
	funcDeclPos := map[token.Pos]bool{}
	for _, f := range nonTestFiles {
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				funcDeclPos[fn.Name.Pos()] = true
			}
		}
	}
	for _, f := range allFiles {
		pkg := fileToPkg[f]
		pkgMap, ok := funcReads[pkg]
		if !ok {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			if funcDeclPos[id.Pos()] {
				return true
			}
			if _, tracked := pkgMap[id.Name]; tracked {
				pkgMap[id.Name]++
			}
			return true
		})
	}

	// 6. Produce results.
	var result findings
	for _, b := range bindings {
		if allow, ok := cmdAllowlists[b.cmdVar+"@"+b.pkg]; ok && allow[b.flagName] {
			continue
		}
		if flagReads[b.pkg][b.backingVar] == 0 {
			result.deadFlags = append(result.deadFlags, deadFlag{
				file:       b.file,
				line:       b.line,
				flagName:   b.flagName,
				backingVar: b.backingVar,
			})
		}
	}
	for _, fn := range funcs {
		if fn.keep {
			continue
		}
		if funcReads[fn.pkg][fn.name] == 0 {
			result.deadFuncs = append(result.deadFuncs, deadFunc{
				file: fn.file,
				line: fn.line,
				name: fn.name,
			})
		}
	}

	sort.Slice(result.deadFlags, func(i, j int) bool {
		if result.deadFlags[i].file != result.deadFlags[j].file {
			return result.deadFlags[i].file < result.deadFlags[j].file
		}
		return result.deadFlags[i].line < result.deadFlags[j].line
	})
	sort.Slice(result.deadFuncs, func(i, j int) bool {
		if result.deadFuncs[i].file != result.deadFuncs[j].file {
			return result.deadFuncs[i].file < result.deadFuncs[j].file
		}
		return result.deadFuncs[i].line < result.deadFuncs[j].line
	})

	return result, nil
}

// isFlagSetVarCall reports whether call is `<x>.{Persistent}Flags().XVar(P)`.
func isFlagSetVarCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if !strings.HasSuffix(sel.Sel.Name, "Var") && !strings.HasSuffix(sel.Sel.Name, "VarP") {
		return false
	}
	flagsCall, ok := sel.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	flagsSel, ok := flagsCall.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return flagsSel.Sel.Name == "Flags" || flagsSel.Sel.Name == "PersistentFlags"
}

// collectCommandAllowlists scans assignments/value-specs for &cobra.Command{...}
// literals carrying Annotations["lint:keep-flag"]. Returns "cmdVar@pkg" -> set.
func collectCommandAllowlists(files []*ast.File, fileToPkg map[*ast.File]string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, f := range files {
		pkg := fileToPkg[f]
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				if len(x.Lhs) == 1 && len(x.Rhs) == 1 {
					if id, ok := x.Lhs[0].(*ast.Ident); ok {
						if allow := extractAllowlist(x.Rhs[0]); allow != nil {
							out[id.Name+"@"+pkg] = allow
						}
					}
				}
			case *ast.ValueSpec:
				for i, name := range x.Names {
					if i < len(x.Values) {
						if allow := extractAllowlist(x.Values[i]); allow != nil {
							out[name.Name+"@"+pkg] = allow
						}
					}
				}
			}
			return true
		})
	}
	return out
}

func extractAllowlist(expr ast.Expr) map[string]bool {
	if u, ok := expr.(*ast.UnaryExpr); ok && u.Op == token.AND {
		expr = u.X
	}
	cl, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	for _, elt := range cl.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "Annotations" {
			continue
		}
		mapLit, ok := kv.Value.(*ast.CompositeLit)
		if !ok {
			continue
		}
		for _, mElt := range mapLit.Elts {
			mKV, ok := mElt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			kLit, ok := mKV.Key.(*ast.BasicLit)
			if !ok || kLit.Kind != token.STRING {
				continue
			}
			if strings.Trim(kLit.Value, `"`) != "lint:keep-flag" {
				continue
			}
			vLit, ok := mKV.Value.(*ast.BasicLit)
			if !ok || vLit.Kind != token.STRING {
				continue
			}
			set := map[string]bool{}
			for _, p := range strings.Split(strings.Trim(vLit.Value, `"`), ",") {
				if s := strings.TrimSpace(p); s != "" {
					set[s] = true
				}
			}
			return set
		}
	}
	return nil
}

func collectFlagBindings(f *ast.File, fset *token.FileSet, filePath, pkg string) []flagBinding {
	var out []flagBinding
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if !isFlagSetVarCall(call) {
			return true
		}
		sel := call.Fun.(*ast.SelectorExpr)
		flagsCall := sel.X.(*ast.CallExpr)
		flagsSel := flagsCall.Fun.(*ast.SelectorExpr)
		cmdVar := ""
		if id, ok := flagsSel.X.(*ast.Ident); ok {
			cmdVar = id.Name
		}
		if len(call.Args) < 2 {
			return true
		}
		unary, ok := call.Args[0].(*ast.UnaryExpr)
		if !ok || unary.Op != token.AND {
			return true
		}
		varIdent, ok := unary.X.(*ast.Ident)
		if !ok {
			return true
		}
		nameLit, ok := call.Args[1].(*ast.BasicLit)
		if !ok || nameLit.Kind != token.STRING {
			return true
		}
		out = append(out, flagBinding{
			file:       filePath,
			line:       fset.Position(call.Pos()).Line,
			flagName:   strings.Trim(nameLit.Value, `"`),
			backingVar: varIdent.Name,
			cmdVar:     cmdVar,
			pkg:        pkg,
		})
		return true
	})
	return out
}

func collectFuncDecls(f *ast.File, fset *token.FileSet, filePath, pkg string) []funcDeclInfo {
	var out []funcDeclInfo
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Recv != nil {
			continue
		}
		name := fn.Name.Name
		if fn.Name.IsExported() {
			continue
		}
		if name == "init" || name == "main" {
			continue
		}
		if strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Benchmark") ||
			strings.HasPrefix(name, "Fuzz") || strings.HasPrefix(name, "Example") {
			continue
		}
		keep := false
		if fn.Doc != nil {
			for _, c := range fn.Doc.List {
				if strings.HasPrefix(strings.TrimSpace(c.Text), "//lint:keep") {
					keep = true
					break
				}
			}
		}
		out = append(out, funcDeclInfo{
			file: filePath,
			line: fset.Position(fn.Pos()).Line,
			name: name,
			keep: keep,
			pkg:  pkg,
		})
	}
	return out
}
