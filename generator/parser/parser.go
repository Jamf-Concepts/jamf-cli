package parser

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/iancoleman/strcase"
)

// ParseSpec parses an OpenAPI spec file and returns a Resource
func ParseSpec(specPath string) (*Resource, error) {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true

	doc, err := loader.LoadFromFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("loading spec: %w", err)
	}

	// Extract resource name from filename (e.g., "Building.yaml" -> "buildings")
	baseName := filepath.Base(specPath)
	baseName = strings.TrimSuffix(baseName, filepath.Ext(baseName))

	// Skip non-resource files
	if strings.Contains(strings.ToLower(baseName), "library") ||
		strings.Contains(strings.ToLower(baseName), "definitions") ||
		strings.Contains(strings.ToLower(baseName), "common") {
		return nil, nil
	}

	resourceName := strcase.ToKebab(baseName)
	resourceName = pluralize(resourceName)

	resource := &Resource{
		Name:         resourceName,
		NameSingular: strings.TrimSuffix(resourceName, "s"),
		GoName:       strcase.ToCamel(resourceName),
		Description:  doc.Info.Description,
		Operations:   make([]*Operation, 0),
		Schemas:      make(map[string]*Schema),
	}

	// Parse paths into operations (sorted for deterministic output)
	pathsMap := doc.Paths.Map()
	sortedPaths := make([]string, 0, len(pathsMap))
	for p := range pathsMap {
		sortedPaths = append(sortedPaths, p)
	}
	sort.Strings(sortedPaths)

	for _, path := range sortedPaths {
		pathItem := pathsMap[path]
		if pathItem == nil {
			continue
		}

		opsMap := pathItem.Operations()
		sortedMethods := make([]string, 0, len(opsMap))
		for m := range opsMap {
			sortedMethods = append(sortedMethods, m)
		}
		sort.Strings(sortedMethods)

		for _, method := range sortedMethods {
			op := opsMap[method]
			if op == nil {
				continue
			}

			operation := parseOperation(path, method, op)
			resource.Operations = append(resource.Operations, operation)
		}
	}

	// Parse schemas
	if doc.Components != nil {
		for name, schemaRef := range doc.Components.Schemas {
			if schemaRef != nil && schemaRef.Value != nil {
				resource.Schemas[name] = parseSchema(name, schemaRef.Value)
			}
		}
	}

	// Detect the name field for filter lookups by inspecting request schemas.
	// Most resources use "name", but some (e.g., API Roles, API Integrations) use "displayName".
	resource.NameField = detectNameField(resource.Schemas)

	return resource, nil
}

func parseOperation(path, method string, op *openapi3.Operation) *Operation {
	// Parse x-action extension first (needed for name inference)
	isAction := false
	if action, ok := op.Extensions["x-action"]; ok {
		if b, ok := action.(bool); ok {
			isAction = b
		}
	}

	opName := inferOperationName(path, method, isAction)
	operation := &Operation{
		Name:          opName,
		Method:        strings.ToUpper(method),
		Path:          path,
		Summary:       strings.TrimSpace(op.Summary),
		Description:   strings.TrimSpace(op.Description),
		Parameters:    make([]*Parameter, 0),
		Responses:     make(map[string]*Response),
		IsAction:      isAction,
		IsDestructive: isDestructiveAction(opName),
		IsList:        (opName == "list" || opName == "history") && hasPaginationParams(op),
		APIVersion:    extractAPIVersion(path),
	}

	// Parse x-required-privileges extension
	if privs, ok := op.Extensions["x-required-privileges"]; ok {
		if arr, ok := privs.([]any); ok {
			for _, p := range arr {
				if s, ok := p.(string); ok {
					operation.Privileges = append(operation.Privileges, s)
				}
			}
		}
	}

	// Parse parameters
	for _, paramRef := range op.Parameters {
		if paramRef == nil || paramRef.Value == nil {
			continue
		}
		param := paramRef.Value
		p := &Parameter{
			Name:        param.Name,
			In:          param.In,
			Description: param.Description,
			Required:    param.Required,
		}
		if param.Schema != nil && param.Schema.Value != nil {
			p.Type = param.Schema.Value.Type.Slice()[0]
			p.Default = param.Schema.Value.Default
			if p.Type == "array" {
				p.IsArray = true
				if param.Schema.Value.Items != nil && param.Schema.Value.Items.Value != nil {
					p.Type = param.Schema.Value.Items.Value.Type.Slice()[0]
				}
			}
		}
		operation.Parameters = append(operation.Parameters, p)
	}

	// Parse request body
	if op.RequestBody != nil && op.RequestBody.Value != nil {
		rb := op.RequestBody.Value
		operation.RequestBody = &RequestBody{
			Description: rb.Description,
			Required:    rb.Required,
		}
		// Resolve request body schema for scaffold generation
		if content, ok := rb.Content["application/json"]; ok && content.Schema != nil && content.Schema.Value != nil {
			operation.RequestBody.Schema = parseSchema("", content.Schema.Value)
		}
	}

	// Parse responses (sorted for deterministic output)
	if op.Responses != nil {
		respMap := op.Responses.Map()
		sortedCodes := make([]string, 0, len(respMap))
		for code := range respMap {
			sortedCodes = append(sortedCodes, code)
		}
		sort.Strings(sortedCodes)

		for _, code := range sortedCodes {
			respRef := respMap[code]
			if respRef == nil || respRef.Value == nil {
				continue
			}
			operation.Responses[code] = &Response{
				StatusCode:  code,
				Description: *respRef.Value.Description,
			}
		}
	}

	return operation
}

func parseSchema(name string, schema *openapi3.Schema) *Schema {
	s := &Schema{
		Name:       name,
		Type:       "object",
		Properties: make(map[string]*Property),
		Required:   schema.Required,
	}

	if len(schema.Type.Slice()) > 0 {
		s.Type = schema.Type.Slice()[0]
	}

	for propName, propRef := range schema.Properties {
		if propRef == nil || propRef.Value == nil {
			continue
		}
		prop := propRef.Value
		p := &Property{
			Name:        propName,
			Description: prop.Description,
			Example:     prop.Example,
			Nullable:    prop.Nullable,
			ReadOnly:    prop.ReadOnly,
		}
		if len(prop.Type.Slice()) > 0 {
			p.Type = prop.Type.Slice()[0]
		}
		s.Properties[propName] = p
	}

	return s
}

// pluralize handles basic English pluralization
func pluralize(s string) string {
	// Already plural common cases
	if strings.HasSuffix(s, "ers") || strings.HasSuffix(s, "ies") || strings.HasSuffix(s, "ves") {
		return s
	}
	// Words ending in 's' that are already plural (computers, devices, etc.)
	if strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss") && !strings.HasSuffix(s, "us") {
		return s
	}
	if strings.HasSuffix(s, "ss") || strings.HasSuffix(s, "x") || strings.HasSuffix(s, "ch") || strings.HasSuffix(s, "sh") {
		return s + "es"
	}
	if strings.HasSuffix(s, "y") && len(s) > 1 {
		// Check if preceded by a consonant
		prev := s[len(s)-2]
		if prev != 'a' && prev != 'e' && prev != 'i' && prev != 'o' && prev != 'u' {
			return s[:len(s)-1] + "ies"
		}
	}
	return s + "s"
}

// inferOperationName determines the CLI command name from path and method
func inferOperationName(path, method string, isAction bool) string {
	method = strings.ToLower(method)

	// Check for specific patterns
	if strings.HasSuffix(path, "/delete-multiple") {
		return "delete-multiple"
	}
	if strings.HasSuffix(path, "/history/export") {
		return "history-export"
	}
	if strings.HasSuffix(path, "/export") {
		return "export"
	}
	if strings.HasSuffix(path, "/history") {
		if method == "get" {
			return "history"
		}
		return "add-history-note"
	}

	// Extract action verb from path for x-action endpoints
	// e.g., /v1/computers/{id}/erase -> "erase"
	// e.g., /v1/computers/{id}/lock -> "lock"
	if isAction {
		parts := strings.Split(path, "/")
		lastPart := parts[len(parts)-1]
		// If last part is not a path param, use it as the action name
		if !strings.HasPrefix(lastPart, "{") {
			return strcase.ToKebab(lastPart)
		}
	}

	// Check if path has an ID parameter
	hasID := strings.Contains(path, "{id}") || strings.Contains(path, "{")

	switch method {
	case "get":
		if hasID {
			return "get"
		}
		return "list"
	case "post":
		if hasID && isAction {
			// Action on a specific resource
			return "action"
		}
		return "create"
	case "put":
		return "update"
	case "patch":
		return "patch"
	case "delete":
		return "delete"
	default:
		return method
	}
}

// extractAPIVersion extracts API version from path (v1, v2, preview, etc.)
func extractAPIVersion(path string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) > 0 {
		first := parts[0]
		if strings.HasPrefix(first, "v") || first == "preview" {
			return first
		}
	}
	return "v1"
}

// hasPaginationParams returns true if the operation has page/page-size query params
func hasPaginationParams(op *openapi3.Operation) bool {
	for _, p := range op.Parameters {
		if p.Value != nil && (p.Value.Name == "page" || p.Value.Name == "page-size" || p.Value.Name == "pagesize") {
			return true
		}
	}
	return false
}

// detectNameField inspects schemas to determine the correct field for name-based
// filtering. Returns "displayName" if any schema has it without "name", otherwise "name".
func detectNameField(schemas map[string]*Schema) string {
	hasDisplayName := false
	for _, s := range schemas {
		if _, ok := s.Properties["displayName"]; ok {
			hasDisplayName = true
		}
	}
	if hasDisplayName {
		return "displayName"
	}
	return "name"
}

// isDestructiveAction returns true for operations that modify/delete data
func isDestructiveAction(opName string) bool {
	destructive := []string{"delete", "delete-multiple", "erase", "wipe", "remove", "lock", "restart", "shutdown"}
	for _, d := range destructive {
		if strings.Contains(opName, d) {
			return true
		}
	}
	return false
}
