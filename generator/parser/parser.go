// Copyright 2026, Jamf Software LLC

package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/iancoleman/strcase"
)

// singularize converts a plural resource name to singular.
// Handles: -ies → -y (policies → policy), -sses → -ss (statuses → status),
// -s → "" (buildings → building).
func singularize(name string) string {
	if strings.HasSuffix(name, "ies") {
		return strings.TrimSuffix(name, "ies") + "y"
	}
	if strings.HasSuffix(name, "sses") {
		return strings.TrimSuffix(name, "es")
	}
	return strings.TrimSuffix(name, "s")
}

// ParseSpec parses an OpenAPI spec file and returns one or more Resources.
// Most specs produce a single resource, but specs with multiple sibling
// collection paths (e.g. /v1/foo/macos and /v1/foo/ios in the same file)
// produce one resource per family. Returns nil when the file should be skipped.
func ParseSpec(specPath string) ([]*Resource, error) {
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

	kebabName := strcase.ToKebab(baseName)

	// Parse paths into operations (sorted for deterministic output)
	var allOps []*Operation
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
			allOps = append(allOps, parseOperation(path, method, op))
		}
	}

	// Parse schemas (shared across all resources from this spec)
	schemas := make(map[string]*Schema)
	if doc.Components != nil {
		for name, schemaRef := range doc.Components.Schemas {
			if schemaRef != nil && schemaRef.Value != nil {
				schemas[name] = parseSchema(name, schemaRef.Value)
			}
		}
	}
	nameField := detectNameField(schemas)

	// Check for multi-family spec: multiple sibling collection paths in one file.
	// Example: SelfServiceBranding.yaml has both /v1/.../branding/macos and
	// /v1/.../branding/ios — each needs its own resource and command group.
	if families := splitByPathFamilies(doc.Info.Description, allOps, schemas, nameField); families != nil {
		return families, nil
	}

	// Single-family spec: build one resource using the filename-derived name.
	// Filter out operations that don't belong to the canonical collection prefix to
	// avoid polluting a resource with unrelated endpoints from the same spec file
	// (e.g. /v1/branding-images/download/{id} appearing in Icon.yaml alongside /v1/icon).
	filteredOps := filterToCanonicalPrefix(allOps)
	for _, op := range allOps {
		found := false
		for _, fop := range filteredOps {
			if fop == op {
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintf(os.Stderr, "  Warning: %s %s not in canonical prefix family — skipped\n", op.Method, op.Path)
		}
	}
	allOps = filteredOps

	resource := &Resource{
		Description: doc.Info.Description,
		Operations:  allOps,
		Schemas:     schemas,
		NameField:   nameField,
	}

	// Detect singleton before naming: a singleton has no {id} in any path and
	// has a non-paginated GET + PUT on the same path (settings/configuration pattern).
	if detectSingleton(allOps) {
		resource.IsSingleton = true
		// Singletons use the exact kebab name — no pluralization.
		resource.Name = kebabName
		resource.NameSingular = kebabName
		resource.GoName = strcase.ToCamel(kebabName)
		// Rename "list" → "get": a non-paginated GET on the root path is a get, not a list.
		for _, op := range resource.Operations {
			if op.Name == "list" {
				op.Name = "get"
			}
		}
	} else {
		resourceName := pluralize(kebabName)
		resource.Name = resourceName
		resource.NameSingular = singularize(resourceName)
		resource.GoName = strcase.ToCamel(resourceName)
	}

	return []*Resource{resource}, nil
}

// splitByPathFamilies detects specs with multiple sibling collection paths
// (e.g. /v1/foo/macos and /v1/foo/ios both having full CRUD under the same
// parent prefix). Returns nil for normal single-family specs.
func splitByPathFamilies(description string, ops []*Operation, schemas map[string]*Schema, nameField string) []*Resource {
	// Find "collection paths": non-parameterized paths that have a /{param}
	// child path — i.e., they serve as the list/create endpoint for a CRUD family.
	collectionPaths := make(map[string]bool)
	for _, op := range ops {
		if hasPathParam(op.Path) {
			continue
		}
		for _, other := range ops {
			if hasPathParam(other.Path) && strings.HasPrefix(other.Path, op.Path+"/") {
				// The remainder after op.Path must be a single /{param} segment.
				remainder := other.Path[len(op.Path):]
				parts := strings.SplitN(remainder, "/", 3)
				if len(parts) == 2 && strings.HasPrefix(parts[1], "{") {
					collectionPaths[op.Path] = true
					break
				}
			}
		}
	}

	if len(collectionPaths) < 2 {
		return nil // single family or no collection paths
	}

	// Group collection paths by their parent prefix (everything before the last /).
	parents := make(map[string][]string) // parent → sibling collection paths
	for cp := range collectionPaths {
		idx := strings.LastIndex(cp, "/")
		if idx < 0 {
			continue
		}
		parent := cp[:idx]
		parents[parent] = append(parents[parent], cp)
	}

	// Find parent groups with 2+ siblings — those are the multi-family specs.
	var siblingGroups [][]string
	for _, siblings := range parents {
		if len(siblings) >= 2 {
			sort.Strings(siblings) // deterministic order
			siblingGroups = append(siblingGroups, siblings)
		}
	}
	if len(siblingGroups) == 0 {
		return nil
	}

	// Build one Resource per sibling family.
	var resources []*Resource
	for _, siblings := range siblingGroups {
		for _, cp := range siblings {
			// Collect only the operations that belong to this family.
			var familyOps []*Operation
			for _, op := range ops {
				if op.Path == cp || strings.HasPrefix(op.Path, cp+"/") {
					familyOps = append(familyOps, op)
				}
			}
			if len(familyOps) == 0 {
				continue
			}

			// Derive the resource name from the collection path rather than the
			// filename — e.g. /v1/self-service/branding/macos → self-service-branding-macos.
			name := pathToResourceName(cp)

			r := &Resource{
				Name:         name,
				NameSingular: name, // path-derived names are already singular; don't strip trailing chars
				GoName:       strcase.ToCamel(name),
				Description:  description,
				Operations:   familyOps,
				Schemas:      schemas,
				NameField:    nameField,
			}

			// Apply singleton detection to each family independently.
			if detectSingleton(familyOps) {
				r.IsSingleton = true
				for _, op := range familyOps {
					if op.Name == "list" {
						op.Name = "get"
					}
				}
			}

			resources = append(resources, r)
		}
	}

	if len(resources) == 0 {
		return nil
	}

	// Collect orphaned operations (not assigned to any sibling family).
	assignedPaths := make(map[string]bool)
	for _, r := range resources {
		for _, op := range r.Operations {
			assignedPaths[op.Path] = true
		}
	}

	// For each multi-family parent path, collect its orphaned operations and
	// build an additional resource to hold them. This preserves cross-cutting
	// endpoints like GET /v1/mobile-device-groups (list all) and
	// POST /v1/mobile-device-groups/{id}/erase.
	for _, siblings := range siblingGroups {
		// Derive parent path from the first sibling — everything before the last /.
		parentPath := siblings[0][:strings.LastIndex(siblings[0], "/")]

		var parentOps []*Operation
		for _, op := range ops {
			if assignedPaths[op.Path] {
				continue
			}
			strippedParent := stripVersionPrefix(parentPath)
			strippedOp := stripVersionPrefix(op.Path)
			if op.Path == parentPath || strings.HasPrefix(op.Path, parentPath+"/") ||
				(strippedParent != parentPath && (strippedOp == strippedParent || strings.HasPrefix(strippedOp, strippedParent+"/"))) {
				parentOps = append(parentOps, op)
			}
		}
		if len(parentOps) == 0 {
			continue
		}

		// When all collected ops have version-stripped paths (none are at the canonical
		// parent path), name the resource from the common stripped prefix of those ops
		// rather than the versioned parent path — this gives more specific names like
		// "self-service-branding-images" instead of the generic "self-service-brandings".
		namePath := parentPath
		strippedParent := stripVersionPrefix(parentPath)
		if strippedParent != parentPath {
			allStripped := true
			for _, op := range parentOps {
				if strings.HasPrefix(op.Path, parentPath) {
					allStripped = false
					break
				}
			}
			if allStripped && len(parentOps) > 0 {
				// Use the stripped path of the first op as the name source.
				namePath = stripVersionPrefix(parentOps[0].Path)
				// Strip trailing path parameter if present.
				if idx := strings.LastIndex(namePath, "/{"); idx != -1 {
					namePath = namePath[:idx]
				}
			}
		}
		name := pluralize(pathToResourceName(namePath))
		r := &Resource{
			Name:         name,
			NameSingular: singularize(name),
			GoName:       strcase.ToCamel(name),
			Description:  description,
			Operations:   parentOps,
			Schemas:      schemas,
			NameField:    nameField,
		}
		if detectSingleton(parentOps) {
			r.IsSingleton = true
			r.Name = pathToResourceName(parentPath)
			r.NameSingular = r.Name
			r.GoName = strcase.ToCamel(r.Name)
			for _, op := range parentOps {
				if op.Name == "list" {
					op.Name = "get"
				}
			}
		}
		resources = append(resources, r)

		for _, op := range parentOps {
			assignedPaths[op.Path] = true
		}
	}

	// Warn about any operations that still aren't assigned (e.g. paths with a
	// different version prefix that don't fall under any detected family parent).
	for _, op := range ops {
		if !assignedPaths[op.Path] {
			fmt.Fprintf(os.Stderr, "  Warning: %s %s not assigned to any resource family (orphaned — will not be generated)\n", op.Method, op.Path)
		}
	}

	return resources
}

// filterToCanonicalPrefix returns only the operations that belong to the same
// resource family as the canonical collection path. A "collection path" is the
// non-parameterized path that has a direct /{param} child (e.g. /v1/icon or
// /v2/inventory-preload/records). Unrelated endpoints in the same spec file
// (e.g. /v1/branding-images/download/{id} in Icon.yaml) are excluded.
//
// Matching is based on the version-stripped resource base so that sibling paths
// like /v2/inventory-preload/csv are included when the canonical path is
// /v2/inventory-preload/records (both share the /inventory-preload base).
//
// If no clear canonical prefix is found, all ops are returned unchanged.
func filterToCanonicalPrefix(ops []*Operation) []*Operation {
	// Find all non-parameterized paths that have a direct /{param} child.
	var collectionPaths []string
	seen := make(map[string]bool)
	for _, op := range ops {
		if hasPathParam(op.Path) {
			continue
		}
		for _, other := range ops {
			if !hasPathParam(other.Path) {
				continue
			}
			if !strings.HasPrefix(other.Path, op.Path+"/") {
				continue
			}
			remainder := other.Path[len(op.Path):]
			parts := strings.SplitN(remainder, "/", 3)
			if len(parts) == 2 && strings.HasPrefix(parts[1], "{") {
				if !seen[op.Path] {
					seen[op.Path] = true
					collectionPaths = append(collectionPaths, op.Path)
				}
				break
			}
		}
	}

	if len(collectionPaths) != 1 {
		// 0 = no collection path (e.g. action-only spec), 2+ = multi-family (handled upstream).
		// In both cases return all ops unfiltered.
		return ops
	}

	cp := collectionPaths[0]

	// Compute the version-stripped resource base: the canonical path with the
	// version segment removed and (if it has depth) the last component removed too.
	// Examples:
	//   /v1/icon                      → stripped=/icon         → base=/icon
	//   /v2/inventory-preload/records → stripped=/inventory-preload/records → base=/inventory-preload
	stripped := stripVersionPrefix(cp)
	var strippedBase string
	if idx := strings.LastIndex(stripped, "/"); idx > 0 {
		strippedBase = stripped[:idx]
	} else {
		strippedBase = stripped
	}

	var filtered []*Operation
	for _, op := range ops {
		opStripped := stripVersionPrefix(op.Path)
		if opStripped == strippedBase || strings.HasPrefix(opStripped, strippedBase+"/") {
			filtered = append(filtered, op)
		}
	}
	return filtered
}

// stripVersionPrefix removes a leading version segment (v1, v2, v3, preview,
// etc.) from a path, leaving the leading slash intact.
// e.g. /v1/self-service/branding → /self-service/branding
// Paths without a version prefix are returned unchanged.
func stripVersionPrefix(path string) string {
	trimmed := strings.TrimPrefix(path, "/")
	if idx := strings.Index(trimmed, "/"); idx != -1 {
		prefix := trimmed[:idx]
		if strings.HasPrefix(prefix, "v") || prefix == "preview" {
			return "/" + trimmed[idx+1:]
		}
	}
	return path
}

// pathToResourceName converts an API path to a kebab-case resource name.
// Strips the version prefix and replaces slashes with dashes.
// e.g. /v1/self-service/branding/macos → self-service-branding-macos
func pathToResourceName(path string) string {
	path = strings.TrimPrefix(path, "/")
	// Strip version prefix (v1, v2, v3, preview, etc.)
	if idx := strings.Index(path, "/"); idx != -1 {
		prefix := path[:idx]
		if strings.HasPrefix(prefix, "v") || prefix == "preview" {
			path = path[idx+1:]
		}
	}
	return strings.ReplaceAll(path, "/", "-")
}

// detectSingleton returns true if the operations describe a singleton resource:
// a settings-style object accessible via a single path with no {id} parameter,
// identified by a non-paginated GET and a PUT on the same path.
func detectSingleton(ops []*Operation) bool {
	// Any path parameter means this is a collection or keyed resource, not a singleton.
	for _, op := range ops {
		if hasPathParam(op.Path) {
			return false
		}
	}

	// Must have a non-paginated GET and a PUT sharing the same path.
	getNonList := make(map[string]bool)
	hasPUT := make(map[string]bool)
	for _, op := range ops {
		if op.Method == "GET" && !op.IsList {
			getNonList[op.Path] = true
		}
		if op.Method == "PUT" {
			hasPUT[op.Path] = true
		}
	}
	for path := range getNonList {
		if hasPUT[path] {
			return true
		}
	}
	return false
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
	// Allow spec authors to override the inferred operation name via x-operation-name.
	// Useful for disambiguating endpoints that would otherwise collide
	// (e.g. /active/pem → "active-pem" vs /{id}/pem → "pem").
	if nameOverride, ok := op.Extensions["x-operation-name"]; ok {
		if s, ok := nameOverride.(string); ok && s != "" {
			opName = s
		}
	}
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
		if content, ok := rb.Content["multipart/form-data"]; ok && content.Schema != nil && content.Schema.Value != nil {
			operation.RequestBody.IsMultipart = true
			// Find the file field: prefer a property with format:binary, fall back to "file".
			operation.RequestBody.FileField = "file"
			for propName, propRef := range content.Schema.Value.Properties {
				if propRef != nil && propRef.Value != nil && propRef.Value.Format == "binary" {
					operation.RequestBody.FileField = propName
					break
				}
			}
			// Non-x-action multipart POSTs get "upload" rather than the generic "create".
			if operation.Name == "create" {
				operation.Name = "upload"
			}
		} else if content, ok := rb.Content["application/json"]; ok && content.Schema != nil && content.Schema.Value != nil {
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
			resp := &Response{
				StatusCode:  code,
				Description: *respRef.Value.Description,
			}
			// Detect binary responses: image/*, text/csv, or format:binary schema.
			for ctType, mediaType := range respRef.Value.Content {
				if strings.HasPrefix(ctType, "image/") || ctType == "text/csv" {
					resp.IsBinary = true
					break
				}
				if mediaType != nil && mediaType.Schema != nil && mediaType.Schema.Value != nil {
					if mediaType.Schema.Value.Format == "binary" {
						resp.IsBinary = true
						break
					}
				}
			}
			operation.Responses[code] = resp
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
	// Paths ending in /download/{param} are download (binary) operations, not generic gets.
	if method == "get" && strings.Contains(path, "{") {
		parts := strings.Split(path, "/")
		for i, p := range parts {
			if strings.HasPrefix(p, "{") && i > 0 && parts[i-1] == "download" {
				return "download"
			}
		}
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

// versionedName matches CLI resource names with a version suffix, e.g. "inventory-preload-v-2s"
// or "mobile-device-prestages-v-3s". Captures the base part and the version number.
var versionedName = regexp.MustCompile(`^(.*)-v-(\d+)s?$`)

// DeduplicateVersioned consolidates multi-version resources so each resource group
// surfaces as a single command using the latest API version.
//
// When multiple spec files cover the same resource at different API versions
// (e.g. MobileDevicePrestagesV2.yaml + MobileDevicePrestagesV3.yaml), the generator
// produces commands like "mobile-device-prestages-v-2s" and "mobile-device-prestages-v-3s".
// This function:
//   - Detects versioned resource names via the "-v-{N}s" suffix pattern
//   - For each version family, keeps only the highest version
//   - Renames the winning resource to the clean canonical name (no version suffix)
//   - Suppresses any non-versioned base resource that the versioned family supersedes
func DeduplicateVersioned(resources []*Resource) []*Resource {
	type entry struct {
		res     *Resource
		version int
	}

	// First pass: find each version family's highest version.
	latest := make(map[string]entry) // canonical name → highest version entry
	for _, r := range resources {
		m := versionedName.FindStringSubmatch(r.Name)
		if m == nil {
			continue
		}
		base := m[1]
		ver, _ := strconv.Atoi(m[2])
		canonical := pluralize(base)
		if cur, ok := latest[canonical]; !ok || ver > cur.version {
			latest[canonical] = entry{res: r, version: ver}
		}
	}

	if len(latest) == 0 {
		return resources
	}

	// Build set of canonical names that have at least one versioned sibling.
	hasVersioned := make(map[string]bool, len(latest))
	for name := range latest {
		hasVersioned[name] = true
	}

	// Second pass: emit only keepers, renaming the winner to the canonical name.
	result := make([]*Resource, 0, len(resources))
	for _, r := range resources {
		m := versionedName.FindStringSubmatch(r.Name)
		if m != nil {
			canonical := pluralize(m[1])
			win := latest[canonical]
			if win.res != r {
				continue // older version — drop
			}
			// Rename winner to clean canonical name (strip version suffix).
			r.Name = canonical
			r.NameSingular = singularize(canonical)
			r.GoName = strcase.ToCamel(canonical)
			result = append(result, r)
			continue
		}
		// Non-versioned resource: suppress if a versioned family covers the same name.
		if hasVersioned[r.Name] {
			continue
		}
		result = append(result, r)
	}
	return result
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
