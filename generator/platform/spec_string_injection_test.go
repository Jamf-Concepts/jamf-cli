// Copyright 2026, Jamf Software LLC

package platform

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

const injectionMarker = `"jamfcli-pwned"`

func injectionSpec(path string, op map[string]any) map[string]any {
	return map[string]any{
		"openapi": "3.0.3",
		"info":    map[string]any{"title": "Injection fixture", "version": "1.0.0"},
		"servers": []any{map[string]any{"url": "https://{region}.api.jamfcloud.com/widgets"}},
		"tags":    []any{map[string]any{"name": "widgets"}},
		"paths":   map[string]any{path: map[string]any{"get": op}},
	}
}

func objectResponse(props map[string]any) map[string]any {
	return map[string]any{"200": map[string]any{
		"description": "ok",
		"content": map[string]any{"application/json": map[string]any{
			"schema": map[string]any{"type": "object", "properties": props},
		}},
	}}
}

func specQueryParam(name, typ string) map[string]any {
	return map[string]any{"name": name, "in": "query", "schema": map[string]any{"type": typ}}
}

// TestGenerate_SpecStringsCannotEscapeGoLiterals feeds hostile spec values
// through LoadResources+Generate. A value may be refused or rendered as string
// content; a panic call carrying the marker in the AST means it became code.
func TestGenerate_SpecStringsCannotEscapeGoLiterals(t *testing.T) {
	quoteBreak := `list" + func() string { panic("jamfcli-pwned") }() + "`
	// strcase.ToKebab keeps punctuation and turns spaces into dashes, so a
	// value that also reaches a ToKebab'd literal has to carry no spaces.
	noSpaceBreak := `w"+func()string{panic("jamfcli-pwned")}()+"`
	arrayProp := map[string]any{"type": "array", "items": map[string]any{"type": "string"}}

	cases := map[string]map[string]any{
		"x-operation-name": injectionSpec("/v1/widgets", map[string]any{
			"tags":             []any{"widgets"},
			"x-operation-name": quoteBreak,
			"responses":        objectResponse(map[string]any{"name": map[string]any{"type": "string"}}),
		}),
		"path parameter name": injectionSpec(`/v1/widgets/{id" + strings.Repeat("jamfcli-pwned", -1) + "}`, map[string]any{
			"tags": []any{"widgets"},
			"parameters": []any{map[string]any{
				"name": `id" + strings.Repeat("jamfcli-pwned", -1) + "`, "in": "path", "required": true,
				"schema": map[string]any{"type": "string"},
			}},
			"responses": objectResponse(map[string]any{"name": map[string]any{"type": "string"}}),
		}),
		"query parameter name": injectionSpec("/v1/widgets", map[string]any{
			"tags":       []any{"widgets"},
			"parameters": []any{specQueryParam(`q" + func() string { panic("jamfcli-pwned") }() + "`, "string")},
			"responses":  objectResponse(map[string]any{"name": map[string]any{"type": "string"}}),
		}),
		"query parameter name, no spaces": injectionSpec("/v1/widgets", map[string]any{
			"tags":       []any{"widgets"},
			"parameters": []any{specQueryParam(noSpaceBreak, "string")},
			"responses":  objectResponse(map[string]any{"name": map[string]any{"type": "string"}}),
		}),
		"tag (resource name), no spaces": func() map[string]any {
			spec := injectionSpec("/v1/widgets", map[string]any{
				"tags":      []any{noSpaceBreak},
				"responses": objectResponse(map[string]any{"name": map[string]any{"type": "string"}}),
			})
			spec["tags"] = []any{map[string]any{"name": noSpaceBreak}}
			return spec
		}(),
		"list array key, interpreted literal": injectionSpec("/v1/widgets", map[string]any{
			"tags":      []any{"widgets"},
			"responses": objectResponse(map[string]any{`items" + func() string { panic("jamfcli-pwned") }() + "`: arrayProp}),
		}),
		"list array key, raw struct tag": injectionSpec("/v1/widgets", map[string]any{
			"tags":       []any{"widgets"},
			"parameters": []any{specQueryParam("page", "integer"), specQueryParam("page-size", "integer")},
			"responses":  objectResponse(map[string]any{"x` }; _ = func() int { panic(31337) }(); var _ struct { X int `y": arrayProp}),
		}),
	}

	for site, spec := range cases {
		t.Run(site, func(t *testing.T) {
			specDir := t.TempDir()
			raw, err := json.Marshal(spec)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(specDir, "widgets_api.json"), raw, 0o644); err != nil {
				t.Fatal(err)
			}
			resources, _, err := LoadResources(specDir)
			if err != nil {
				t.Logf("refused at load: %v", err)
				return
			}
			files, err := Generate(resources, t.TempDir())
			if err != nil {
				t.Logf("refused at generate: %v", firstLine(err.Error()))
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
					if !ok || (lit.Value != injectionMarker && lit.Value != "31337") {
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

func firstLine(s string) string {
	before, _, _ := strings.Cut(s, "\n")
	return before
}
