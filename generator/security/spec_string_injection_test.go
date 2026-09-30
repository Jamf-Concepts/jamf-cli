// Copyright 2026, Jamf Software LLC

package security

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenerate_SpecStringsCannotEscapeGoLiterals mutates the committed risk
// spec — operation names here are hand-authored, so only the query parameter
// names and the paginated array key reach emitted source from the spec — and
// fails when either compiles as code rather than string content.
func TestGenerate_SpecStringsCannotEscapeGoLiterals(t *testing.T) {
	base, err := os.ReadFile(filepath.Join("..", "..", "specs", "security", "jamf-risk-api.json"))
	if err != nil {
		t.Fatal(err)
	}
	devicesGet := func(doc map[string]any) map[string]any {
		return doc["paths"].(map[string]any)["/risk/v2/devices"].(map[string]any)["get"].(map[string]any)
	}
	cases := map[string]func(op map[string]any){
		"query parameter name": func(op map[string]any) {
			op["parameters"] = append(op["parameters"].([]any), map[string]any{
				"name": `w"+func()string{panic("jamfcli-pwned")}()+"`, "in": "query",
				"schema": map[string]any{"type": "string"},
			})
		},
		"paginated array key, raw struct tag": func(op map[string]any) {
			resp := op["responses"].(map[string]any)["200"].(map[string]any)
			resp["content"] = map[string]any{"application/json": map[string]any{"schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"x` }; _ = func() int { panic(31337) }(); var _ struct { X int `y": map[string]any{
						"type": "array", "items": map[string]any{"type": "string"},
					},
				},
			}}}
		},
	}

	for site, mutate := range cases {
		t.Run(site, func(t *testing.T) {
			var doc map[string]any
			if err := json.Unmarshal(base, &doc); err != nil {
				t.Fatal(err)
			}
			mutate(devicesGet(doc))
			raw, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			specDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(specDir, "jamf-risk-api.json"), raw, 0o644); err != nil {
				t.Fatal(err)
			}
			resources, scopeOf, _, err := LoadResources(specDir)
			if err != nil {
				t.Logf("refused at load: %v", err)
				return
			}
			files, err := Generate(resources, scopeOf, t.TempDir())
			if err != nil {
				before, _, _ := strings.Cut(err.Error(), "\n")
				t.Logf("refused at generate: %v", before)
				return
			}
			if len(files) == 0 {
				t.Fatal("fixture generated no files; the site was not exercised")
			}
			for _, f := range files {
				src, err := os.ReadFile(f)
				if err != nil {
					t.Fatal(err)
				}
				fset := token.NewFileSet()
				file, err := parser.ParseFile(fset, f, src, 0)
				if err != nil {
					t.Fatalf("generated %s does not parse: %v", filepath.Base(f), err)
				}
				lines := strings.Split(string(src), "\n")
				ast.Inspect(file, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok || len(call.Args) == 0 {
						return true
					}
					lit, ok := call.Args[0].(*ast.BasicLit)
					if !ok || (lit.Value != `"jamfcli-pwned"` && lit.Value != "31337") {
						return true
					}
					line := fset.Position(call.Pos()).Line
					t.Errorf("spec value escaped its Go literal and compiled as code at %s:%d:\n\t%s",
						filepath.Base(f), line, strings.TrimSpace(lines[line-1]))
					return true
				})
			}
		})
	}
}
