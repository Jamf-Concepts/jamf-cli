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

// resourceNameOverrides maps auto-generated canonical names to preferred CLI names,
// for cases where auto-pluralization produces unnatural results. Applied after
// DeduplicateVersioned by ApplyNameOverrides.
var resourceNameOverrides = map[string]string{
	// "computers-inventory" pluralizes to "computers-inventories" via the -y→-ies
	// rule, but the Jamf API path (/v3/computers-inventory) treats "inventory" as a
	// collective noun — no further pluralization needed.
	"computers-inventories": "computers-inventory",
}

// ApplyNameOverrides corrects resource names that auto-pluralization got wrong.
// Must be called after DeduplicateVersioned.
func ApplyNameOverrides(resources []*Resource) {
	for _, r := range resources {
		if preferred, ok := resourceNameOverrides[r.Name]; ok {
			r.Name = preferred
			r.NameSingular = singularize(preferred)
			r.GoName = strcase.ToCamel(preferred)
		}
	}
}

// resourceLookupFields maps canonical resource names to their alternate identifier
// fields for patch-by-name commands. Keyed by the final canonical resource name
// (after DeduplicateVersioned and ApplyNameOverrides).
var resourceLookupFields = map[string][]LookupField{
	"computers-inventory": {
		{Flag: "serial", RSQLField: "hardware.serialNumber", Desc: "Look up computer by serial number"},
		{Flag: "udid", RSQLField: "udid", Desc: "Look up computer by UDID"},
	},
	"mobile-devices": {
		{Flag: "serial", RSQLField: "hardware.serialNumber", Desc: "Look up mobile device by serial number"},
		{Flag: "udid", RSQLField: "udid", Desc: "Look up mobile device by UDID"},
	},
}

// ApplyLookupFields sets LookupFields on resources that have alternate identifier
// fields defined in resourceLookupFields. Must be called after DeduplicateVersioned
// so resource names are in their final canonical form.
func ApplyLookupFields(resources []*Resource) {
	for _, r := range resources {
		if fields, ok := resourceLookupFields[r.Name]; ok {
			r.LookupFields = fields
		}
	}
}

// resourceNameFieldOverrides maps canonical resource names to the correct RSQL
// filter field for name-based lookups. Used when detectNameField() returns the
// wrong value — typically because the name lives in a nested object (e.g.
// general.name) that the heuristic can't see, or the schema uses a prefixed
// field (e.g. groupName) but also exposes a plain "name" field that wins.
var resourceNameFieldOverrides = map[string]string{
	// Jamf Pro list endpoint requires "general.name" not "name".
	"computers-inventory": "general.name",
	// Groups list endpoint requires "groupName"; plain "name" field wins the
	// heuristic but is not a filterable field on this endpoint.
	"groups": "groupName",
}

// resourceIDFieldOverrides maps canonical resource names to the correct response
// field for ID extraction during name-to-ID resolution. Used when detectIDField()
// returns the wrong value — typically for resources that expose a UUID platform ID
// (used in PATCH/DELETE paths) alongside a legacy integer Jamf Pro ID.
var resourceIDFieldOverrides = map[string]string{
	// Groups list response uses "groupPlatformId" (UUID) for PATCH/DELETE paths,
	// not the legacy integer "groupJamfProId".
	"groups": "groupPlatformId",
}

// ApplyNameFieldOverrides corrects NameField and IDField values that the
// auto-detection heuristics got wrong. Must be called after ApplyNameOverrides
// so resource names are in their final canonical form.
func ApplyNameFieldOverrides(resources []*Resource) {
	for _, r := range resources {
		if field, ok := resourceNameFieldOverrides[r.Name]; ok {
			r.NameField = field
		}
		if field, ok := resourceIDFieldOverrides[r.Name]; ok {
			r.IDField = field
		}
	}
}

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
	idField := detectIDField(schemas, allOps)

	// Post-process operation names before family detection.
	//
	// 1. Drop lower-version duplicates: when the same path exists at multiple API
	//    versions in one spec (e.g. /v2/foo and /v3/foo), keep only the highest.
	allOps = deduplicateVersionedOps(allOps)
	// 2. Rename no-param sub-path ops that would otherwise collide with another
	//    op of the same name (e.g. GET /settings competing with GET /pending-rotations
	//    for the "list" name — settings becomes "settings").
	resolveNoParamConflicts(allOps)
	// 3. Disambiguate ops that share the same terminal segment but differ in
	//    path-param count (e.g. /{username}/audit vs /{username}/{guid}/audit).
	disambiguateSameTerminalOps(allOps)

	// Check for multi-family spec: multiple sibling collection paths in one file.
	// Example: SelfServiceBranding.yaml has both /v1/.../branding/macos and
	// /v1/.../branding/ios — each needs its own resource and command group.
	if families := splitByPathFamilies(doc.Info.Description, allOps, schemas, nameField, idField); families != nil {
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
		IDField:     idField,
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
func splitByPathFamilies(description string, ops []*Operation, schemas map[string]*Schema, nameField, idField string) []*Resource {
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

			// Each family may have different path params; re-detect the ID field.
			familyIDField := detectIDField(schemas, familyOps)

			r := &Resource{
				Name:         name,
				NameSingular: name, // path-derived names are already singular; don't strip trailing chars
				GoName:       strcase.ToCamel(name),
				Description:  description,
				Operations:   familyOps,
				Schemas:      schemas,
				NameField:    nameField,
				IDField:      familyIDField,
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
		parentIDField := detectIDField(schemas, parentOps)
		r := &Resource{
			Name:         name,
			NameSingular: singularize(name),
			GoName:       strcase.ToCamel(name),
			Description:  description,
			Operations:   parentOps,
			Schemas:      schemas,
			NameField:    nameField,
			IDField:      parentIDField,
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

	// Also match the "-detail" sibling path variant used by some Jamf endpoints
	// (e.g. /v3/computers-inventory-detail belongs to the /v3/computers-inventory family).
	detailBase := strippedBase + "-detail"

	var filtered []*Operation
	for _, op := range ops {
		opStripped := stripVersionPrefix(op.Path)
		if opStripped == strippedBase || strings.HasPrefix(opStripped, strippedBase+"/") ||
			opStripped == detailBase || strings.HasPrefix(opStripped, detailBase+"/") {
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

// deduplicateVersionedOps removes lower-version duplicates when the same path
// exists at multiple API versions in one spec (e.g. /v2/foo and /v3/foo with the
// same method). The highest version is kept; ties prefer explicitly versioned paths
// over unversioned legacy paths.
func deduplicateVersionedOps(ops []*Operation) []*Operation {
	type key struct{ method, path string }
	seen := make(map[key]*Operation)
	result := make([]*Operation, 0, len(ops))

	for _, op := range ops {
		k := key{op.Method, stripVersionPrefix(op.Path)}
		prev, exists := seen[k]
		if !exists {
			seen[k] = op
			result = append(result, op)
			continue
		}
		// Prefer the higher API version. For equal versions, prefer an explicitly
		// versioned path over a legacy unversioned one.
		cmp := compareAPIVersions(op.Path, prev.Path)
		if cmp > 0 {
			for i, r := range result {
				if r == prev {
					result[i] = op
					break
				}
			}
			seen[k] = op
			fmt.Fprintf(os.Stderr, "  Info: preferring %s %s over %s (higher version)\n", op.Method, op.Path, prev.Path)
		}
		// else: keep prev, drop op (lower or equal version — implicit, no warning)
	}
	return result
}

// compareAPIVersions returns >0 if path1 should be preferred over path2.
// Comparison order: explicit /v3 > /v2 > /v1 > unversioned > preview (treated as
// pre-release, kept for now but lower than any numeric version).
func compareAPIVersions(path1, path2 string) int {
	rank := func(path string) int {
		trimmed := strings.TrimPrefix(path, "/")
		idx := strings.Index(trimmed, "/")
		var prefix string
		if idx != -1 {
			prefix = trimmed[:idx]
		} else {
			prefix = trimmed
		}
		if prefix == "preview" {
			return -1 // lower than any versioned path
		}
		if strings.HasPrefix(prefix, "v") {
			var n int
			if _, err := fmt.Sscanf(prefix[1:], "%d", &n); err == nil {
				return n
			}
		}
		return 0 // unversioned legacy path
	}
	r1, r2 := rank(path1), rank(path2)
	if r1 > r2 {
		return 1
	}
	if r1 < r2 {
		return -1
	}
	return 0
}

// resolveNoParamConflicts renames ops on non-parameterized sub-paths that would
// otherwise collide with another op of the same name in the resource.
//
// Canonical collection paths (those with a /{param} child or that have no conflict)
// keep their inferred name. Non-canonical sub-paths are renamed to their last
// path segment (or prefixed with their HTTP verb for write methods on paths that
// also have a same-segment GET).
//
// Example: GET /settings + PUT /settings competing in a resource that also has
// GET /pending-rotations — both are "list"/"update" duplicates. GET /settings
// becomes "settings" and (if PUT /settings also conflicts) "update-settings".
func resolveNoParamConflicts(ops []*Operation) {
	// Identify canonical collection paths: no-param paths with a /{param} child.
	isCanonical := make(map[string]bool)
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
			if len(remainder) > 1 && strings.HasPrefix(remainder[1:], "{") {
				isCanonical[op.Path] = true
				break
			}
		}
	}

	// Group ops by their current name.
	byName := make(map[string][]*Operation)
	for _, op := range ops {
		byName[op.Name] = append(byName[op.Name], op)
	}

	// Track which segment names are already in use so we can add method prefixes
	// when GET and PUT/PATCH on the same sub-path both need renaming.
	renamedGETs := make(map[string]string) // path → new name (set by GET renames first)

	for _, group := range byName {
		if len(group) <= 1 {
			continue
		}
		// Rename GET "list" ops on non-canonical sub-paths first.
		for _, op := range group {
			if op.Method != "GET" || hasPathParam(op.Path) || isCanonical[op.Path] {
				continue
			}
			parts := strings.Split(op.Path, "/")
			seg := strcase.ToKebab(parts[len(parts)-1])
			op.Name = seg
			renamedGETs[op.Path] = seg
		}
	}

	// Re-group after GET renames (names changed).
	byName = make(map[string][]*Operation)
	for _, op := range ops {
		byName[op.Name] = append(byName[op.Name], op)
	}

	// Rename PUT/PATCH/POST/DELETE on non-canonical sub-paths.
	for _, group := range byName {
		if len(group) <= 1 {
			continue
		}
		for _, op := range group {
			if op.Method == "GET" || hasPathParam(op.Path) || isCanonical[op.Path] {
				continue
			}
			parts := strings.Split(op.Path, "/")
			seg := strcase.ToKebab(parts[len(parts)-1])
			// If a GET on the same path already claimed this segment name, prefix.
			if _, getClaimedSeg := renamedGETs[op.Path]; getClaimedSeg {
				switch op.Method {
				case "PUT":
					seg = "update-" + seg
				case "PATCH":
					seg = "patch-" + seg
				case "POST":
					seg = "create-" + seg
				case "DELETE":
					seg = "delete-" + seg
				}
			}
			op.Name = seg
		}
	}
}

// disambiguateSameTerminalOps renames duplicate-named operations that all share
// the same non-param terminal path segment but differ in the number of path params
// (e.g. /{username}/audit vs /{username}/{guid}/audit).
//
// The shortest path keeps the base name. Longer paths receive a distinguishing
// prefix (from extra fixed segments) and/or a "-by-{param}" suffix.
func disambiguateSameTerminalOps(ops []*Operation) {
	byName := make(map[string][]*Operation)
	for _, op := range ops {
		byName[op.Name] = append(byName[op.Name], op)
	}

	for baseName, group := range byName {
		if len(group) <= 1 {
			continue
		}

		// Only apply when ALL paths share the same non-param terminal segment.
		terminal := lastPathSeg(group[0].Path)
		if strings.HasPrefix(terminal, "{") {
			continue // terminal is a param — handled elsewhere
		}
		allSame := true
		for _, op := range group[1:] {
			if lastPathSeg(op.Path) != terminal {
				allSame = false
				break
			}
		}
		if !allSame {
			continue
		}

		// Sort ascending by path segment count so the shortest keeps the base name.
		// Within the same length, GET comes before PUT/PATCH so GET keeps the base name.
		sort.Slice(group, func(i, j int) bool {
			ci := strings.Count(group[i].Path, "/")
			cj := strings.Count(group[j].Path, "/")
			if ci != cj {
				return ci < cj
			}
			// Same path length: GET < PUT < PATCH < POST < DELETE for name priority.
			order := map[string]int{"GET": 0, "PUT": 1, "PATCH": 2, "POST": 3, "DELETE": 4}
			oi := order[group[i].Method]
			oj := order[group[j].Method]
			return oi < oj
		})

		// group[0] keeps baseName. Rename the rest using group[0]'s path as reference.
		assigned := map[string]bool{baseName: true}
		for _, op := range group[1:] {
			// Same path as canonical: differentiate by HTTP method verb prefix.
			if op.Path == group[0].Path {
				var prefix string
				switch op.Method {
				case "PUT":
					prefix = "update-"
				case "PATCH":
					prefix = "patch-"
				case "POST":
					prefix = "create-"
				case "DELETE":
					prefix = "delete-"
				default:
					prefix = strings.ToLower(op.Method) + "-"
				}
				op.Name = prefix + baseName
				assigned[op.Name] = true
				continue
			}
			newName := buildDisambiguatedName(baseName, op.Path, group[0].Path, assigned)
			op.Name = newName
			assigned[newName] = true
		}
	}
}

// buildDisambiguatedName computes a unique name for longerPath relative to
// shorterPath. It prepends extra fixed segments and appends "-by-{param}" for
// extra path parameters.
func buildDisambiguatedName(baseName, longerPath, shorterPath string, assigned map[string]bool) string {
	longerParts := strings.Split(longerPath, "/")
	shorterParts := strings.Split(shorterPath, "/")

	// Remove the shared terminal segment from both.
	longerParts = longerParts[:len(longerParts)-1]
	shorterParts = shorterParts[:len(shorterParts)-1]

	var extraFixed []string
	var lastExtraParam string

	for i := 0; i < len(longerParts); i++ {
		if i >= len(shorterParts) || longerParts[i] != shorterParts[i] {
			seg := longerParts[i]
			if !strings.HasPrefix(seg, "{") {
				extraFixed = append(extraFixed, strcase.ToKebab(seg))
			} else {
				lastExtraParam = strings.Trim(seg, "{}")
			}
		}
	}

	candidate := baseName
	if len(extraFixed) > 0 {
		candidate = strings.Join(extraFixed, "-") + "-" + baseName
	}
	if assigned[candidate] && lastExtraParam != "" {
		candidate = candidate + "-by-" + lastExtraParam
	}
	return candidate
}

// lastPathSeg returns the last "/" delimited segment of a path.
func lastPathSeg(path string) string {
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
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
		} else if content, ok := rb.Content["application/merge-patch+json"]; ok && content.Schema != nil && content.Schema.Value != nil {
			operation.RequestBody.IsMergePatch = true
			operation.RequestBody.Schema = parseSchema("", content.Schema.Value)
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

	// Post-inference name fixes that require parsed response/path information:
	//
	// 1. Binary GET ops named "get" → "download" (e.g. GET /images/{id} returning image/*).
	// 2. Sub-resource GET ops ending with a non-param segment after a param
	//    (e.g. GET /{id}/prestages) → use that segment as the operation name.
	//
	// These prevent legitimate operations from being silently dropped by dedupeOps
	// when a spec has multiple GET/{id} paths.
	if operation.Name == "get" && operation.Method == "GET" {
		hasBinaryResponse := false
		for _, resp := range operation.Responses {
			if resp.IsBinary {
				hasBinaryResponse = true
				break
			}
		}
		if hasBinaryResponse {
			operation.Name = "download"
		} else if strings.Contains(path, "{") {
			parts := strings.Split(path, "/")
			lastPart := parts[len(parts)-1]
			if !strings.HasPrefix(lastPart, "{") {
				// Path ends with a non-param segment after a param → sub-resource GET.
				// e.g. GET /{id}/prestages → "prestages"
				hasParam := false
				for _, p := range parts[:len(parts)-1] {
					if strings.HasPrefix(p, "{") {
						hasParam = true
						break
					}
				}
				if hasParam {
					operation.Name = strcase.ToKebab(lastPart)
				}
			} else if len(parts) >= 3 {
				// Path ends in a param but the segment before it is a named segment
				// following a prior param — use that named segment.
				// e.g. GET /{id}/ldap/{panel-id} → "ldap"
				secondToLast := parts[len(parts)-2]
				if !strings.HasPrefix(secondToLast, "{") {
					hasPriorParam := false
					for _, p := range parts[:len(parts)-2] {
						if strings.HasPrefix(p, "{") {
							hasPriorParam = true
							break
						}
					}
					if hasPriorParam {
						operation.Name = strcase.ToKebab(secondToLast)
					}
				}
			}
		}
	}

	// Sub-resource renaming for PUT/PATCH: when the path ends with a named
	// (non-param) segment after a path param, the segment is the operation identity.
	// e.g. PUT /{id}/set-password → "set-password"
	if (operation.Name == "update" || operation.Name == "patch") && strings.Contains(path, "{") {
		parts := strings.Split(path, "/")
		lastPart := parts[len(parts)-1]
		if !strings.HasPrefix(lastPart, "{") {
			hasParam := false
			for _, p := range parts[:len(parts)-1] {
				if strings.HasPrefix(p, "{") {
					hasParam = true
					break
				}
			}
			if hasParam {
				operation.Name = strcase.ToKebab(lastPart)
				operation.IsDestructive = isDestructiveAction(operation.Name)
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
		// Capture the referenced component schema name so the generator can
		// resolve nested object fields (e.g. for --set dot-notation paths).
		if propRef.Ref != "" {
			if idx := strings.LastIndex(propRef.Ref, "/"); idx != -1 {
				p.SchemaRef = propRef.Ref[idx+1:]
			}
		}
		// Populate Nested for object types so flattenSchemaToScalarFields can
		// resolve sub-fields even for cross-file $ref schemas not in doc.Components.Schemas.
		// kin-openapi resolves $ref inline, so prop.Properties is always populated.
		if len(prop.Properties) > 0 {
			p.Nested = parseSchema(propName, prop)
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
// filtering. Read-only properties are skipped — they cannot appear in apply input.
// Priority among writable fields: "displayName" > "name" > schema-typed *name field > "name".
//
// The "schema-typed" heuristic handles resources like Package (packageName) or
// User (username): if a non-readonly string field named "{prefix}name" exists and
// its prefix (min 3 chars) is a substring of the schema name, it is treated as the
// type-specific name field. If exactly one such candidate is found across all schemas,
// it is used.
func detectNameField(schemas map[string]*Schema) string {
	var (
		hasName, hasDisplayName bool
		typedCandidates         []string
		seenCandidates          = map[string]bool{}
	)

	for schemaName, s := range schemas {
		schemaLower := strings.ToLower(schemaName)
		for fieldName, prop := range s.Properties {
			if prop.ReadOnly {
				continue
			}
			lower := strings.ToLower(fieldName)
			if !strings.HasSuffix(lower, "name") {
				continue
			}
			switch fieldName {
			case "name":
				hasName = true
			case "displayName":
				hasDisplayName = true
			default:
				// e.g. "packageName" → prefix "package" must appear in schema name "Package".
				// Require at least 3 chars to avoid false positives from short prefixes like "ca".
				prefix := lower[:len(lower)-len("name")]
				if len(prefix) >= 3 && strings.Contains(schemaLower, prefix) && !seenCandidates[fieldName] {
					typedCandidates = append(typedCandidates, fieldName)
					seenCandidates[fieldName] = true
				}
			}
		}
	}

	// Priority: displayName > name (preserves existing behaviour)
	if hasDisplayName {
		return "displayName"
	}
	if hasName {
		return "name"
	}

	// No standard name field: if exactly one schema-typed *name candidate, use it.
	if len(typedCandidates) == 1 {
		return typedCandidates[0]
	}

	return "name"
}

// detectIDField inspects response schemas and operations to determine the correct
// field for extracting the resource identifier during name-to-ID resolution.
//
// The path parameter on the get-by-id endpoint tells us what value the API expects
// (e.g. {id}, {clientManagementId}, {fileName}). We search response schemas for
// a property whose name matches that path parameter. When the path parameter is
// the generic "{id}" but no "id" property exists, we fall back to heuristics:
// look for a unique property ending in "Id", "Uuid", or named "uuid".
func detectIDField(schemas map[string]*Schema, ops []*Operation) string {
	// 1. Find the primary path parameter name from the get operation.
	pathParam := ""
	for _, op := range ops {
		if op.Name == "get" && op.Method == "GET" && hasPathParam(op.Path) {
			// Extract the last {param} from the path — that's the resource identifier.
			start := strings.LastIndex(op.Path, "{")
			end := strings.LastIndex(op.Path, "}")
			if start != -1 && end > start {
				pathParam = op.Path[start+1 : end]
			}
			break
		}
	}
	if pathParam == "" {
		return "id"
	}

	// 2. Check if any schema has a property matching the path parameter name exactly.
	if schemaHasProperty(schemas, pathParam) {
		return pathParam
	}

	// 3. If the path param ends in "Id", try the bare prefix as a property name.
	//    e.g. "keyId" → "key", "languageId" → "language"
	if strings.HasSuffix(pathParam, "Id") {
		bare := strings.TrimSuffix(pathParam, "Id")
		if bare != "" && schemaHasProperty(schemas, bare) {
			return bare
		}
		// 3b. Spec inconsistency: path param uses "*Id" but response field uses a
		//     different suffix (e.g. "languageId" → "languageCode"). Try known
		//     identifier-like suffixes before falling back to the raw path param name.
		if bare != "" {
			for _, suffix := range []string{"Code", "Key", "Uuid", "Token"} {
				candidate := bare + suffix
				if schemaHasProperty(schemas, candidate) {
					return candidate
				}
			}
		}
	}

	// 4. If the path param is the generic "id" but no "id" property exists,
	//    search for a unique identifier-like property across all schemas.
	if pathParam == "id" {
		if candidate := findUniqueIDProperty(schemas); candidate != "" {
			return candidate
		}
	}

	// 5. Fall back to the path parameter name itself. Even if no schema property
	//    matches, using the path param name is the best guess and makes the
	//    generated code's intent clear.
	return pathParam
}

// schemaHasProperty checks whether any schema in the map contains a property
// with the given name.
func schemaHasProperty(schemas map[string]*Schema, name string) bool {
	for _, s := range schemas {
		if _, ok := s.Properties[name]; ok {
			return true
		}
	}
	return false
}

// findUniqueIDProperty searches all schema properties for a single candidate
// that looks like an identifier: ends in "Id" or "Uuid", or is exactly "uuid".
// Returns the candidate if exactly one is found across all schemas; returns ""
// if zero or multiple candidates exist (ambiguous).
func findUniqueIDProperty(schemas map[string]*Schema) string {
	seen := map[string]bool{}
	for _, s := range schemas {
		for name := range s.Properties {
			if name == "id" {
				continue // already checked by caller
			}
			lower := strings.ToLower(name)
			if strings.HasSuffix(lower, "id") || strings.HasSuffix(lower, "uuid") || lower == "uuid" {
				seen[name] = true
			}
		}
	}

	// Only use the heuristic when there's exactly one candidate to avoid ambiguity.
	if len(seen) == 1 {
		for name := range seen {
			return name
		}
	}
	return ""
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
