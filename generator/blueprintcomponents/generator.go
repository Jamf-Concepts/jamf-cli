// Copyright 2026, Jamf Software LLC

package blueprintcomponents

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/getkin/kin-openapi/openapi3"
)

// componentMapping maps an API component identifier to its spec path slug.
type componentMapping struct {
	Identifier string // e.g. "com.jamf.ddm.software-update-settings"
	Slug       string // spec path slug under /v1/components/, empty if no schema
	Label      string // human-readable label
}

// componentMappings defines all known blueprint component types. Components with
// an empty Slug have no configuration schema in any spec and get an empty scaffold.
var componentMappings = []componentMapping{
	{"com.jamf.ddm.software-update-settings", "software-update-settings", "Software Update Settings"},
	{"com.jamf.ddm.sw-updates", "sw-update", "Software Updates"},
	{"com.jamf.ddm.safari-settings", "safari-settings", "Safari Settings"},
	{"com.jamf.ddm.safari-extensions", "safari-extensions", "Safari Extensions"},
	{"com.jamf.ddm.safari-bookmarks", "safari-bookmarks", "Safari Bookmarks"},
	{"com.jamf.ddm.passcode-settings", "passcode", "Passcode Policy"},
	{"com.jamf.ddm.math-settings", "math-settings", "Math Settings"},
	{"com.jamf.ddm.disk-management", "disk-management", "Disk Management"},
	{"com.jamf.ddm.audio-accessory-settings", "audio-accessory-settings", "Audio Accessory Settings"},
	{"com.jamf.ddm.service-configuration-files", "service-configuration-files", "Service Configuration Files"},
	{"com.jamf.ddm.service-background-tasks", "service-background-tasks", "Service Background Tasks"},
	{"com.jamf.ddm.custom-declarations", "free-form", "Custom Declarations"},
	// No configuration schema available in any spec
	{"com.jamf.ddm-configuration-profile", "", "Configuration Profile"},
	{"com.jamf.ddm.app-managed", "", "Managed App"},
}

// scaffoldEntry holds the data for one component scaffold in the generated file.
type scaffoldEntry struct {
	Identifier string
	Short      string
	JSON       string
}

// Generate parses all OpenAPI specs in specsDir, extracts component configuration
// schemas, walks them to produce example JSON, and writes a Go source file to outputDir.
func Generate(specsDir, outputDir string) (string, error) {
	// Discover all JSON spec files
	specFiles, err := filepath.Glob(filepath.Join(specsDir, "*.json"))
	if err != nil {
		return "", fmt.Errorf("globbing spec files: %w", err)
	}
	if len(specFiles) == 0 {
		return "", fmt.Errorf("no JSON spec files found in %s", specsDir)
	}

	// Load all specs and build a unified slug → config schema map
	slugSchemas := make(map[string]*openapi3.SchemaRef)
	for _, specFile := range specFiles {
		loader := openapi3.NewLoader()
		doc, err := loader.LoadFromFile(specFile)
		if err != nil {
			return "", fmt.Errorf("loading %s: %w", filepath.Base(specFile), err)
		}
		for path, item := range doc.Paths.Map() {
			if !strings.HasSuffix(path, "/validate") {
				continue
			}
			slug := extractSlug(path)
			if slug == "" {
				continue
			}
			configSchema := extractConfigSchema(item)
			if configSchema != nil {
				slugSchemas[slug] = configSchema
			}
		}
	}

	// Build scaffold entries
	var entries []scaffoldEntry
	for _, m := range componentMappings {
		var jsonStr string
		if m.Slug == "" {
			jsonStr = "{}"
		} else {
			schemaRef, ok := slugSchemas[m.Slug]
			if !ok {
				fmt.Fprintf(os.Stderr, "  warning: no schema found for slug %q (identifier: %s)\n", m.Slug, m.Identifier)
				jsonStr = "{}"
			} else {
				example := schemaToExample(schemaRef, 0)
				applyOverrides(m.Identifier, example)
				data, err := marshalNoHTMLEscape(example)
				if err != nil {
					return "", fmt.Errorf("marshalling scaffold for %s: %w", m.Identifier, err)
				}
				jsonStr = string(data)
			}
		}
		entries = append(entries, scaffoldEntry{
			Identifier: m.Identifier,
			Short:      shortName(m.Identifier),
			JSON:       jsonStr,
		})
	}

	// Sort by identifier for stable output
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Identifier < entries[j].Identifier
	})

	// Generate the Go file
	outPath := filepath.Join(outputDir, "scaffolds.go")
	f, err := os.Create(outPath)
	if err != nil {
		return "", fmt.Errorf("creating output file: %w", err)
	}
	defer func() { _ = f.Close() }()

	tmpl, err := template.New("scaffolds").Parse(scaffoldsTemplate)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}
	if err := tmpl.Execute(f, entries); err != nil {
		_ = os.Remove(outPath)
		return "", fmt.Errorf("executing template: %w", err)
	}

	return outPath, nil
}

// extractSlug extracts the component slug from a validate path.
// "/v1/components/software-update-settings/validate" → "software-update-settings"
func extractSlug(path string) string {
	path = strings.TrimPrefix(path, "/v1/components/")
	path = strings.TrimSuffix(path, "/validate")
	if strings.Contains(path, "/") {
		return "" // unexpected format
	}
	return path
}

// extractConfigSchema extracts the configuration schema from a validate endpoint's
// request body. It follows the pattern: POST body → { configuration: <schema> }.
func extractConfigSchema(item *openapi3.PathItem) *openapi3.SchemaRef {
	if item.Post == nil || item.Post.RequestBody == nil {
		return nil
	}
	content := item.Post.RequestBody.Value
	if content == nil {
		return nil
	}
	mt := content.Content.Get("application/json")
	if mt == nil || mt.Schema == nil || mt.Schema.Value == nil {
		return nil
	}
	configProp := mt.Schema.Value.Properties["configuration"]
	if configProp == nil {
		return nil
	}
	return configProp
}

// schemaToExample recursively walks an OpenAPI schema and produces an example
// value suitable for JSON marshalling.
func schemaToExample(ref *openapi3.SchemaRef, depth int) any {
	if ref == nil || ref.Value == nil {
		return map[string]any{}
	}
	if depth > 20 {
		return nil // guard against circular references
	}

	schema := ref.Value

	// Handle oneOf: pick first non-deprecated variant
	if len(schema.OneOf) > 0 {
		return walkOneOf(schema.OneOf, depth)
	}

	// Handle allOf: merge properties from all branches
	if len(schema.AllOf) > 0 {
		return walkAllOf(schema.AllOf, depth)
	}

	// Determine the type
	schemaType := resolveType(schema)

	switch schemaType {
	case "object":
		return walkObject(schema, depth)
	case "string":
		return walkString(schema)
	case "integer", "number":
		return walkNumber(schema)
	case "boolean":
		return walkBoolean(schema)
	case "array":
		return walkArray(schema, depth)
	default:
		// Empty schema (e.g. JsonNode) — return empty object
		if len(schema.Properties) > 0 {
			return walkObject(schema, depth)
		}
		return map[string]any{}
	}
}

// resolveType returns the effective type string for a schema.
func resolveType(schema *openapi3.Schema) string {
	if schema.Type != nil {
		types := schema.Type.Slice()
		if len(types) > 0 {
			return types[0]
		}
	}
	// Infer from properties
	if len(schema.Properties) > 0 {
		return "object"
	}
	return ""
}

func walkOneOf(refs openapi3.SchemaRefs, depth int) any {
	// Prefer non-deprecated
	for _, ref := range refs {
		if ref.Value != nil && !ref.Value.Deprecated {
			return schemaToExample(ref, depth+1)
		}
	}
	// Fallback to first
	if len(refs) > 0 {
		return schemaToExample(refs[0], depth+1)
	}
	return map[string]any{}
}

func walkAllOf(refs openapi3.SchemaRefs, depth int) any {
	result := make(map[string]any)
	for _, ref := range refs {
		sub := schemaToExample(ref, depth+1)
		if m, ok := sub.(map[string]any); ok {
			maps.Copy(result, m)
		}
	}
	return result
}

func walkObject(schema *openapi3.Schema, depth int) any {
	// Handle additionalProperties (map types)
	if len(schema.Properties) == 0 && schema.AdditionalProperties.Schema != nil {
		return map[string]any{
			"<key>": schemaToExample(schema.AdditionalProperties.Schema, depth+1),
		}
	}

	result := make(map[string]any)
	for name, propRef := range schema.Properties {
		if propRef == nil || propRef.Value == nil {
			continue
		}
		if propRef.Value.ReadOnly {
			continue
		}
		result[name] = schemaToExample(propRef, depth+1)
	}

	// Handle mixed: properties + additionalProperties
	if schema.AdditionalProperties.Schema != nil {
		result["<key>"] = schemaToExample(schema.AdditionalProperties.Schema, depth+1)
	}

	return result
}

func walkString(schema *openapi3.Schema) any {
	// Check const via extensions
	if c := constValue(schema); c != nil {
		return c
	}
	if len(schema.Enum) > 0 {
		return schema.Enum[0]
	}
	if schema.Example != nil {
		return schema.Example
	}
	if schema.Default != nil {
		return schema.Default
	}
	return ""
}

func walkNumber(schema *openapi3.Schema) any {
	if c := constValue(schema); c != nil {
		return c
	}
	if schema.Example != nil {
		return schema.Example
	}
	if schema.Default != nil {
		return schema.Default
	}
	if schema.Min != nil {
		return *schema.Min
	}
	return 0
}

func walkBoolean(schema *openapi3.Schema) any {
	if schema.Default != nil {
		return schema.Default
	}
	return false
}

func walkArray(schema *openapi3.Schema, depth int) any {
	if schema.Items != nil {
		return []any{schemaToExample(schema.Items, depth+1)}
	}
	return []any{}
}

// constValue extracts a const value from a schema. OpenAPI 3.1 const is
// represented by kin-openapi as a single-element enum or via extensions.
func constValue(schema *openapi3.Schema) any {
	// kin-openapi may expose const as a single-element enum
	if len(schema.Enum) == 1 {
		return schema.Enum[0]
	}
	// Check extensions map for "const"
	if v, ok := schema.Extensions["const"]; ok {
		return v
	}
	return nil
}

// shortName derives the short CLI name from a full component identifier.
// "com.jamf.ddm.software-update-settings" → "software-update-settings"
// "com.jamf.ddm-configuration-profile" → "ddm-configuration-profile"
func shortName(identifier string) string {
	if after, ok := strings.CutPrefix(identifier, "com.jamf.ddm."); ok {
		return after
	}
	if after, ok := strings.CutPrefix(identifier, "com.jamf."); ok {
		return after
	}
	return identifier
}

// applyOverrides corrects spec-vs-reality mismatches for specific components.
// The passcode-settings API expects "version" as a string despite the spec
// declaring it as an integer. Disk management works with the spec's integer.
func applyOverrides(identifier string, example any) {
	m, ok := example.(map[string]any)
	if !ok {
		return
	}
	switch identifier {
	case "com.jamf.ddm.passcode-settings":
		if v, exists := m["version"]; exists {
			m["version"] = fmt.Sprintf("%v", v)
		}
	}
}

// marshalNoHTMLEscape produces indented JSON without HTML entity escaping,
// so placeholder keys like "<key>" render cleanly.
func marshalNoHTMLEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// Encode appends a trailing newline; trim it
	b := buf.Bytes()
	return bytes.TrimRight(b, "\n"), nil
}

const scaffoldsTemplate = `// Copyright 2026, Jamf Software LLC
// Code generated by jamf-cli generator. DO NOT EDIT.

package blueprintcomponents

// Scaffolds maps blueprint component identifiers to example JSON configurations.
// Each value is a complete, valid configuration object that can be used as the
// "configuration" field in a BlueprintComponentV1.
var Scaffolds = map[string]string{
{{- range .}}
	` + "`" + `{{.Identifier}}` + "`" + `: ` + "`" + `{{.JSON}}` + "`" + `,
{{- end}}
}

// ShortNames maps short names to full component identifiers for CLI convenience.
var ShortNames = map[string]string{
{{- range .}}
	` + "`" + `{{.Short}}` + "`" + `: ` + "`" + `{{.Identifier}}` + "`" + `,
{{- end}}
}

// Identifiers returns all known component identifiers in sorted order.
func Identifiers() []string {
	return []string{
{{- range .}}
		` + "`" + `{{.Identifier}}` + "`" + `,
{{- end}}
	}
}
`
