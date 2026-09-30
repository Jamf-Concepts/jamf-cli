// Copyright 2026, Jamf Software LLC

package parser

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const injectionPayload = `foo" + func() string { panic("pwned") }() + "`

type injectionSpec struct {
	opName, path, queryName, queryType, queryDefault, propName, fileProp string
}

func (s injectionSpec) yaml() string {
	return fmt.Sprintf(`openapi: 3.0.1
info:
  title: Widgets
  version: "1"
paths:
  '/v1/widgets':
    get:
      summary: List widgets
      parameters:
        - in: query
          name: '%[3]s'
          schema:
            type: %[4]s
            default: %[5]s
      responses:
        '200':
          description: ok
  '%[2]s':
    get:
      summary: Get widget
      x-operation-name: '%[1]s'
      parameters:
        - in: path
          name: id
          required: true
          schema:
            type: string
      responses:
        '200':
          description: ok
    patch:
      summary: Patch widget
      parameters:
        - in: path
          name: id
          required: true
          schema:
            type: string
      requestBody:
        content:
          application/merge-patch+json:
            schema:
              type: object
              properties:
                '%[6]s':
                  type: string
      responses:
        '200':
          description: ok
  '/v1/widgets/{id}/upload':
    post:
      summary: Upload widget file
      parameters:
        - in: path
          name: id
          required: true
          schema:
            type: string
      requestBody:
        content:
          multipart/form-data:
            schema:
              type: object
              properties:
                '%[7]s':
                  type: string
                  format: binary
      responses:
        '201':
          description: ok
`, s.opName, s.path, s.queryName, s.queryType, s.queryDefault, s.propName, s.fileProp)
}

// A spec string must reach generated Go only inside a string literal: the
// rendered file has to parse, and the payload's panic call must not appear as
// code anywhere in it.
func TestGeneratedSourceKeepsSpecStringsInsideLiterals(t *testing.T) {
	clean := injectionSpec{"get", "/v1/widgets/{id}", "filter", "string", `""`, "name", "file"}
	cases := map[string]func(*injectionSpec){
		"x-operation-name": func(s *injectionSpec) { s.opName = injectionPayload },
		"path key": func(s *injectionSpec) {
			s.path = "/v1/widgets" + strings.TrimPrefix(injectionPayload, "foo") + "/{id}"
		},
		"query parameter name": func(s *injectionSpec) { s.queryName = injectionPayload },
		"query parameter name without spaces": func(s *injectionSpec) {
			s.queryName = `x"+func()string{panic("pwned")}()+"`
		},
		"string default on an integer parameter": func(s *injectionSpec) {
			s.queryType = "integer"
			s.queryDefault = `'func() int { panic("pwned") }()'`
		},
		"schema property name":    func(s *injectionSpec) { s.propName = injectionPayload },
		"multipart file property": func(s *injectionSpec) { s.fileProp = injectionPayload },
	}
	// An operation name becomes an identifier and a default becomes a typed
	// expression, so these two may fail generation instead of being quoted.
	mayRefuse := map[string]bool{
		"x-operation-name":                       true,
		"string default on an integer parameter": true,
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			spec := clean
			mutate(&spec)
			dir := t.TempDir()
			specPath := filepath.Join(dir, "Widgets.yaml")
			if err := os.WriteFile(specPath, []byte(spec.yaml()), 0o644); err != nil {
				t.Fatal(err)
			}
			resources, err := ParseSpec(specPath)
			if err != nil {
				if mayRefuse[name] {
					t.Logf("refused at parse: %v", err)
					return
				}
				t.Fatalf("ParseSpec: %v", err)
			}
			for _, r := range resources {
				out, err := NewGenerator(dir).Generate(r)
				if err != nil {
					if mayRefuse[name] {
						t.Logf("refused at generate: %v", err)
						return
					}
					t.Fatalf("Generate: %v", err)
				}
				fset := token.NewFileSet()
				file, err := goparser.ParseFile(fset, out, nil, 0)
				if err != nil {
					t.Errorf("generated source does not parse: %v", err)
					continue
				}
				ast.Inspect(file, func(n ast.Node) bool {
					if call, ok := n.(*ast.CallExpr); ok {
						if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "panic" {
							t.Errorf("spec string escaped its literal: panic call compiled as code at %s", fset.Position(call.Pos()))
						}
					}
					return true
				})
			}
		})
	}
}
