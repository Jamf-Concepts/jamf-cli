// Copyright 2026, Jamf Software LLC

package classic

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/generator/classicschema"
)

const breakout = "x` + func() string { panic(\"pwned\") }() + `y"

func injectionResource(t *testing.T, description string, schema map[string]any) ClassicResource {
	t.Helper()
	return boundResource(t, description, "widget", "widget", schema)
}

func boundResource(t *testing.T, description, schemaName, root string, schema map[string]any) ClassicResource {
	t.Helper()
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	r := ClassicResource{
		Name:        "widgets",
		Path:        "widgets",
		CLIName:     "classic-widgets",
		GoName:      "ClassicWidgets",
		Singular:    "widget",
		Description: description,
		Operations:  []string{"list", "get", "create", "update", "delete"},
		Lookups:     []string{"id", "name"},
		IDPath:      "id",
	}
	art := &classicschema.Artifact{
		Resources:  map[string]*classicschema.Res{"widgets": {Schema: schemaName, Root: root}},
		Components: classicschema.Components{Schemas: map[string]json.RawMessage{schemaName: raw}},
	}
	res := []ClassicResource{r}
	if err := AttachSchemas(res, art); err != nil {
		t.Fatalf("AttachSchemas: %v", err)
	}
	return res[0]
}

func TestSpecDerivedStringsCannotEscapeTheirGoLiteral(t *testing.T) {
	plain := map[string]any{"type": "object", "properties": map[string]any{
		"name": map[string]any{"type": "string"},
	}}
	cases := map[string]ClassicResource{
		"control": injectionResource(t, "Widgets", map[string]any{
			"type": "object", "required": []string{"name"},
			"properties": map[string]any{
				"name":      map[string]any{"type": "string"},
				"frequency": map[string]any{"type": "string", "enum": []string{"Once", "Daily"}},
			},
		}),
		"enum value": injectionResource(t, "Widgets", map[string]any{
			"type": "object", "required": []string{"name"},
			"properties": map[string]any{
				"name":      map[string]any{"type": "string"},
				"frequency": map[string]any{"type": "string", "enum": []string{"Once", breakout}},
			},
		}),
		"property name": injectionResource(t, "Widgets", map[string]any{
			"type": "object", "required": []string{breakout},
			"properties": map[string]any{
				breakout: map[string]any{"type": "string"},
			},
		}),
		"manifest description double quote": injectionResource(t,
			`Widgets" + func() string { panic("pwned") }() + "`, plain),
	}

	for name, r := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := NewGenerator(t.TempDir()).Generate(r)
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			src, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			f, err := parser.ParseFile(token.NewFileSet(), out, src, 0)
			if err != nil {
				t.Fatalf("generated source does not parse: %v", err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				kv, ok := n.(*ast.KeyValueExpr)
				if !ok {
					return true
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok || (key.Name != "Use" && key.Name != "Short" && key.Name != "Long") {
					return true
				}
				if _, ok := kv.Value.(*ast.BasicLit); !ok {
					t.Errorf("%s: %s is a %T, not a single string literal; spec text became Go code", out, key.Name, kv.Value)
				}
				return true
			})
		})
	}
}

// A scaffold that cannot be rendered fails generation; its error text, which
// carries the spec's schema name, must never be written into generated source.
func TestUnrenderableScaffoldFailsGenerationAndWritesNothing(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{
		"name": map[string]any{"type": "string"},
	}}
	cases := map[string]struct {
		schemaName, root, wantErr string
	}{
		"hostile schema name":     {"x */; func init() { panic(\"pwned\") }; /*", "", "body_schema value"},
		"empty root, clean names": {"widget", "", "no root element name"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := boundResource(t, "Widgets", tc.schemaName, tc.root, schema)
			if !r.HasBodySchema() {
				t.Fatal("fixture carries no body schema; the scaffold path is not exercised")
			}
			dir := t.TempDir()
			out, err := NewGenerator(dir).Generate(r)
			if err == nil {
				t.Fatalf("Generate wrote %s; want a refusal", out)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not mention %q", err, tc.wantErr)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				t.Errorf("Generate left %s behind after refusing", filepath.Join(dir, e.Name()))
			}
		})
	}
}
