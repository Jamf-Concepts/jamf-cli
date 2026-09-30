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

// bracelessPayload escapes a Go string literal without a `{`, which a path
// would otherwise read as a parameter.
const bracelessPayload = `x" + string(panic("pwned")) + "`

type injectionSpec struct {
	fileName, opName, collection, queryName, queryType, queryDefault, propName, fileProp string
}

func (s injectionSpec) yaml() string {
	querySchema := "type: " + s.queryType
	if s.queryType == "array" {
		querySchema += "\n            items:\n              type: string"
	}
	if s.queryDefault != "" {
		querySchema += "\n            default: " + s.queryDefault
	}
	return fmt.Sprintf(`openapi: 3.0.1
info:
  title: Widgets
  version: "1"
paths:
  '%[2]s':
    get:
      summary: List widgets
      parameters:
        - in: query
          name: '%[3]s'
          schema:
            %[4]s
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  results:
                    type: array
                    items:
                      $ref: '#/components/schemas/Widget'
  '%[2]s/{id}':
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
    delete:
      summary: Delete widget
      parameters:
        - in: path
          name: id
          required: true
          schema:
            type: string
      responses:
        '204':
          description: deleted
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
                '%[5]s':
                  type: string
      responses:
        '200':
          description: ok
  '%[2]s/{id}/upload':
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
                '%[6]s':
                  type: string
                  format: binary
      responses:
        '201':
          description: ok
components:
  schemas:
    Widget:
      type: object
      properties:
        id:
          type: string
        name:
          type: string
`, s.opName, s.collection, s.queryName, querySchema, s.propName, s.fileProp)
}

// A spec string must reach generated Go only inside a string literal: the
// rendered file has to parse, and the payload's panic call must not appear as
// code anywhere in it. A value that becomes an identifier or a typed
// expression instead must be refused with the named reason.
func TestGeneratedSourceKeepsSpecStringsInsideLiterals(t *testing.T) {
	clean := injectionSpec{"Widgets.yaml", "get", "/v1/widgets", "filter", "string", `""`, "name", "file"}
	withLookups := func(r *Resource) {
		r.LookupFields = []LookupField{{Flag: "serial", RSQLField: "serialNumber", Desc: "Look up widget by serial number", Section: "HARDWARE"}}
		r.GroupsClassicPath = "computergroups"
	}
	cases := map[string]struct {
		mutate func(*injectionSpec)
		adjust func(*Resource)
		refuse string
	}{
		"x-operation-name": {
			mutate: func(s *injectionSpec) { s.opName = injectionPayload },
			refuse: "lowercase kebab-case",
		},
		"resource name from the spec file name": {
			mutate: func(s *injectionSpec) { s.fileName = `W` + bracelessPayload + `.yaml` },
			refuse: "lowercase kebab-case",
		},
		"string default on an integer parameter": {
			mutate: func(s *injectionSpec) {
				s.queryType = "integer"
				s.queryDefault = `'func() int { panic("pwned") }()'`
			},
			refuse: "does not match the declared integer type",
		},
		"collection path, with name lookup, lookup fields and group delete": {
			mutate: func(s *injectionSpec) { s.collection = "/v1/widgets" + bracelessPayload },
			adjust: withLookups,
		},
		"query parameter name":                {mutate: func(s *injectionSpec) { s.queryName = injectionPayload }},
		"query parameter name without spaces": {mutate: func(s *injectionSpec) { s.queryName = `x"+func()string{panic("pwned")}()+"` }},
		"integer query parameter name": {mutate: func(s *injectionSpec) {
			s.queryName, s.queryType, s.queryDefault = injectionPayload, "integer", "3"
		}},
		"boolean query parameter name": {mutate: func(s *injectionSpec) {
			s.queryName, s.queryType, s.queryDefault = injectionPayload, "boolean", "true"
		}},
		"array query parameter name": {mutate: func(s *injectionSpec) {
			s.queryName, s.queryType, s.queryDefault = injectionPayload, "array", ""
		}},
		"schema property name":    {mutate: func(s *injectionSpec) { s.propName = injectionPayload }},
		"multipart file property": {mutate: func(s *injectionSpec) { s.fileProp = injectionPayload }},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			spec := clean
			tc.mutate(&spec)
			dir := t.TempDir()
			specPath := filepath.Join(dir, spec.fileName)
			if err := os.WriteFile(specPath, []byte(spec.yaml()), 0o644); err != nil {
				t.Fatal(err)
			}
			resources, err := ParseSpec(specPath)
			if err != nil {
				t.Fatalf("ParseSpec: %v", err)
			}
			if len(resources) == 0 {
				t.Fatal("fixture parsed to no resources; the site was not exercised")
			}
			outDir := t.TempDir()
			var generated int
			for _, r := range resources {
				if tc.adjust != nil {
					tc.adjust(r)
				}
				out, err := NewGenerator(outDir).Generate(r)
				if tc.refuse != "" {
					if err == nil {
						t.Fatalf("Generate wrote %s; want a refusal mentioning %q", out, tc.refuse)
					}
					if !strings.Contains(err.Error(), tc.refuse) {
						t.Fatalf("Generate refused with %q; want it to mention %q", err, tc.refuse)
					}
					continue
				}
				if err != nil {
					t.Fatalf("Generate: %v", err)
				}
				generated++
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
			if tc.refuse != "" {
				if entries, _ := os.ReadDir(outDir); len(entries) > 0 {
					t.Errorf("a refused resource left %d file(s) in the output directory", len(entries))
				}
			} else if generated == 0 {
				t.Fatal("no file was generated; the site was not exercised")
			}
		})
	}
}
