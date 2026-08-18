// Copyright 2026, Jamf Software LLC

package security

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/iancoleman/strcase"

	"github.com/Jamf-Concepts/jamf-cli/generator/parser"
)

// queryParam describes a CLI flag bound to a query string parameter.
type queryParam struct {
	Name        string // spec parameter name (e.g. "externalId", "guid")
	FlagName    string // cobra flag name (kebab-case)
	Var         string // Go variable name (camelCase)
	Description string
	GoType      string // "string", "bool", "int", "[]string"
}

// templateOp wraps *parser.Operation with template-friendly fields.
type templateOp struct {
	*parser.Operation
	GoName          string // PascalCase form of Name, used in Go identifiers
	Short           string // Short help text for the cobra subcommand
	HasBody         bool   // operation accepts a request body — emit --file/--set/--scaffold flags
	Scaffold        string // pretty-printed JSON template for the request body ("" when op has no body)
	QueryParams     []queryParam
	Paginate        bool   // op exposes page+pageSize — always fetch every page, aggregating the array named by UnwrapArrayKey
	UnwrapArrayKey  string // response object property holding the result array (empty: print the raw response)
	NeedsCustomerID bool   // Device Lifecycle ops with {customerId} in Path — filled from SecurityClient.LifecycleCustomerID at request time
}

// templateResource wraps *parser.Resource for template rendering.
type templateResource struct {
	Name       string
	GoName     string
	Scope      string // "Risk", "Lifecycle", or "SSE" — selects cliCtx.SecurityClient.DoExpect{Scope}
	Operations []templateOp
}

// Generate emits one Go file per resource into outputDir. Returns the list of
// generated file paths.
func Generate(resources []*parser.Resource, scopeOf map[string]string, outputDir string) ([]string, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating output dir: %w", err)
	}

	tmpl, err := template.New("resource").Funcs(template.FuncMap{
		"opAnnotations": func(op templateOp) string {
			var pairs []string
			if op.IsDestructive {
				pairs = append(pairs, `"jamf:destructive": "true"`)
			}
			// Which API serves this command, and so which credentials it needs.
			// The `security` namespace also carries commands served through the
			// platform gateway, which annotate themselves "platform-gateway";
			// these reach Radar directly with per-API scoped credentials.
			pairs = append(pairs, `"jamf:api": "radar"`)
			return "map[string]string{" + strings.Join(pairs, ", ") + "}"
		},
	}).Parse(resourceTemplate)
	if err != nil {
		return nil, fmt.Errorf("parsing template: %w", err)
	}

	var generated []string
	for _, r := range resources {
		tr, err := buildTemplateResource(r, scopeOf[r.Name])
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, tr); err != nil {
			return nil, fmt.Errorf("rendering %s: %w", r.Name, err)
		}

		formatted, err := format.Source(buf.Bytes())
		if err != nil {
			return nil, fmt.Errorf("gofmt %s: %w\n--- raw ---\n%s", r.Name, err, buf.String())
		}

		outPath := filepath.Join(outputDir, strings.ReplaceAll(r.Name, "-", "_")+".go")
		if err := os.WriteFile(outPath, formatted, 0o644); err != nil {
			return nil, fmt.Errorf("writing %s: %w", outPath, err)
		}
		generated = append(generated, outPath)
	}
	return generated, nil
}

func buildTemplateResource(r *parser.Resource, scope string) (templateResource, error) {
	ops := make([]templateOp, 0, len(r.Operations))
	for _, op := range r.Operations {
		opCopy := *op
		unwrapKey := ""
		if opCopy.IsList && hasPageParams(opCopy.Parameters) {
			unwrapKey = detectListArrayKey(&opCopy)
			if unwrapKey == "" {
				return templateResource{}, fmt.Errorf(
					"resource %q op %q (%s %s): has page/pageSize params but no 2xx response could be identified as a single-array-property object to paginate over; add a securityOpsByFile override or fix the spec",
					r.Name, opCopy.Name, opCopy.Method, opCopy.Path,
				)
			}
		}
		// Paginate implies a non-empty UnwrapArrayKey: an always-fetch-
		// everything loop only makes sense when the template knows which
		// response property to aggregate pages into.
		paginate := unwrapKey != ""
		ops = append(ops, templateOp{
			Operation:       &opCopy,
			GoName:          strcase.ToCamel(opCopy.Name),
			Short:           shortFromOp(&opCopy),
			HasBody:         opCopy.RequestBody != nil,
			Scaffold:        buildScaffold(&opCopy),
			QueryParams:     buildQueryParams(opCopy.Parameters),
			Paginate:        paginate,
			UnwrapArrayKey:  unwrapKey,
			NeedsCustomerID: strings.Contains(opCopy.Path, "{customerId}"),
		})
	}
	return templateResource{
		Name:       r.Name,
		GoName:     r.GoName,
		Scope:      scope,
		Operations: ops,
	}, nil
}

// hasPageParams reports whether params includes both a "page" and a
// "pageSize" query parameter (the Risk API's own casing — unlike Platform's
// kebab-case "page-size").
func hasPageParams(params []*parser.Parameter) bool {
	var hasPage, hasPageSize bool
	for _, p := range params {
		if p == nil || p.In != "query" {
			continue
		}
		switch p.Name {
		case "page":
			hasPage = true
		case "pageSize":
			hasPageSize = true
		}
	}
	return hasPage && hasPageSize
}

// detectListArrayKey returns the response property name holding the result
// array, when any declared 2xx response is a JSON object with exactly one
// array property. The three Security Cloud APIs don't consistently declare a
// single success code (the SSE "set" endpoints document both 200 and 202),
// so every 2xx response is checked rather than just the primary one.
func detectListArrayKey(op *parser.Operation) string {
	for status, resp := range op.Responses {
		n, err := strconv.Atoi(status)
		if err != nil || n < 200 || n >= 300 {
			continue
		}
		if resp == nil || resp.Schema == nil || resp.Schema.Type != "object" {
			continue
		}
		var key string
		count := 0
		for name, prop := range resp.Schema.Properties {
			if prop != nil && prop.Type == "array" {
				key = name
				count++
			}
		}
		if count == 1 {
			return key
		}
	}
	return ""
}

// buildQueryParams returns the user-facing query flags for an operation.
// page/pageSize are excluded — the always-fetch-everything pagination loop
// manages them internally, mirroring the Platform generator's approach.
func buildQueryParams(params []*parser.Parameter) []queryParam {
	var out []queryParam
	for _, p := range params {
		if p == nil || p.In != "query" {
			continue
		}
		if p.Name == "page" || p.Name == "pageSize" {
			continue
		}
		goType := "string"
		switch p.Type {
		case "boolean":
			goType = "bool"
		case "integer":
			goType = "int"
		case "array":
			goType = "[]string"
		}
		desc := p.Description
		if desc == "" {
			desc = "Filter by " + strcase.ToKebab(p.Name)
		}
		out = append(out, queryParam{
			Name:        p.Name,
			FlagName:    strcase.ToKebab(p.Name),
			Var:         strcase.ToLowerCamel(p.Name),
			Description: desc,
			GoType:      goType,
		})
	}
	return out
}

// buildScaffold returns a pretty-printed JSON example for the operation's
// request body, derived from the spec schema. Returns "" when the op has no
// body.
func buildScaffold(op *parser.Operation) string {
	if op.RequestBody == nil || op.RequestBody.Schema == nil {
		return ""
	}
	example := schemaExample(op.RequestBody.Schema)
	b, err := json.MarshalIndent(example, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}

// schemaExample walks a parsed schema and emits a JSON-marshallable Go value
// with placeholder zero values for every property.
func schemaExample(s *parser.Schema) any {
	if s == nil {
		return nil
	}
	switch s.Type {
	case "object", "":
		m := map[string]any{}
		for name, prop := range s.Properties {
			m[name] = propertyExample(prop)
		}
		return m
	case "array":
		return []any{}
	case "string":
		return ""
	case "boolean":
		return false
	case "integer", "number":
		return 0
	}
	return nil
}

func propertyExample(p *parser.Property) any {
	if p == nil {
		return nil
	}
	switch p.Type {
	case "object":
		if p.Nested != nil {
			return schemaExample(p.Nested)
		}
		return map[string]any{}
	case "array":
		return []any{}
	case "string":
		return ""
	case "boolean":
		return false
	case "integer", "number":
		return 0
	}
	return nil
}

// shortFromOp produces a brief help string for an operation. Falls back to a
// generic phrasing when the spec lacks a summary.
func shortFromOp(op *parser.Operation) string {
	if op.Summary != "" {
		return escapeQuote(op.Summary)
	}
	if op.Description != "" {
		return escapeQuote(firstParagraph(op.Description))
	}
	return op.Name
}

// firstParagraph returns the leading paragraph of s, with internal newlines
// folded to spaces.
func firstParagraph(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.Index(s, "\n\n"); idx >= 0 {
		s = s[:idx]
	}
	s = strings.ReplaceAll(s, "\n", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return s
}

func escapeQuote(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '"' || r == '\\' {
			out = append(out, '\\')
		}
		out = append(out, r)
	}
	return string(out)
}
