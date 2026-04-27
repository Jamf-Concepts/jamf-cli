// Copyright 2026, Jamf Software LLC

package parser

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/iancoleman/strcase"
)

// ParsePlatformSpec parses a Platform Gateway OpenAPI spec and returns one
// Resource per operation tag. Platform paths share a /v1/tenant/{tenantId}/
// prefix that the runtime fills from auth context — it is not a per-call
// parameter. This loader strips the prefix to /v1/ and removes the tenantId
// path parameter from each operation before parsing.
//
// Resources are grouped by the first tag on each operation. Operations without
// tags fall back to filename-based grouping via ParseSpec.
func ParsePlatformSpec(specPath string) ([]*Resource, error) {
	raw, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("reading platform spec: %w", err)
	}

	var rawDoc map[string]any
	if err := json.Unmarshal(raw, &rawDoc); err != nil {
		return nil, fmt.Errorf("decoding platform spec: %w", err)
	}
	service := serviceSegment(rawDoc)
	stripTenantPrefix(rawDoc)
	prependServiceSegment(rawDoc, service)

	tmpPath, err := writeNormalisedTempSpec(specPath, rawDoc)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(filepath.Dir(tmpPath)) }()

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	doc, err := loader.LoadFromFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("loading platform spec: %w", err)
	}

	// Parse all operations + schemas using shared helpers.
	allOps, opTags := parsePlatformOps(doc)
	if len(allOps) == 0 {
		return nil, nil
	}

	schemas := make(map[string]*Schema)
	if doc.Components != nil {
		for name, schemaRef := range doc.Components.Schemas {
			if schemaRef != nil && schemaRef.Value != nil {
				schemas[name] = parseSchema(name, schemaRef.Value)
			}
		}
	}

	// If every op has a tag, group by tag. Otherwise fall back to ParseSpec.
	groupable := true
	for _, op := range allOps {
		if opTags[op] == "" {
			groupable = false
			break
		}
	}
	if !groupable {
		return ParseSpec(tmpPath)
	}

	// Group ops by tag.
	byTag := make(map[string][]*Operation)
	for _, op := range allOps {
		tag := opTags[op]
		byTag[tag] = append(byTag[tag], op)
	}

	tags := make([]string, 0, len(byTag))
	for t := range byTag {
		tags = append(tags, t)
	}
	sort.Strings(tags)

	nameField := detectNameField(schemas)
	description := doc.Info.Description

	resources := make([]*Resource, 0, len(tags))
	for _, tag := range tags {
		ops := byTag[tag]
		// Apply standard post-processing.
		reclassifyMisannotatedCreates(ops)
		renameSingletonRootGet(ops)
		ops = deduplicateVersionedOps(ops)
		resolveNoParamConflicts(ops)
		disambiguateSameTerminalOps(ops)

		// Each tag may span multiple collection roots (e.g. the "blueprints"
		// tag covers both /blueprints and /blueprint-components). Reuse the
		// existing path-family splitter to cleanly produce one resource per
		// collection. Falls back to a single resource named after the tag
		// when no sibling collections are present.
		families := splitByPathFamilies(description, ops, schemas, nameField, detectIDField(schemas, ops))
		if families != nil {
			for _, fam := range families {
				// splitByPathFamilies derives names from the full collection path
				// ("/api/blueprints/v1/blueprints" → "api-blueprints-v1-blueprints").
				// Platform paths share the /api/{service}/v{n}/ prefix; strip it
				// so names stay short and match the spec resource (e.g. "blueprints").
				fam.Name = applyResourceNameOverride(trimPlatformPathPrefix(fam.Name))
				fam.NameSingular = fam.Name
				fam.GoName = strcase.ToCamel(fam.Name)
				if detectSingleton(fam.Operations) {
					fam.IsSingleton = true
					for _, op := range fam.Operations {
						if op.Name == "list" {
							op.Name = "get"
						}
					}
				}
				fam.HasVersionLock = detectVersionLock(fam.Operations)
				resources = append(resources, fam)
			}
			continue
		}

		name := applyResourceNameOverride(strcase.ToKebab(tag))
		idField := detectIDField(schemas, ops)

		r := &Resource{
			Name:         name,
			NameSingular: name,
			GoName:       strcase.ToCamel(name),
			Description:  description,
			Operations:   ops,
			Schemas:      schemas,
			NameField:    nameField,
			IDField:      idField,
		}
		if detectSingleton(ops) {
			r.IsSingleton = true
			for _, op := range ops {
				if op.Name == "list" {
					op.Name = "get"
				}
			}
		}
		r.HasVersionLock = detectVersionLock(ops)
		resources = append(resources, r)
	}
	return resources, nil
}

// parsePlatformOps walks the doc's paths, parses each operation via
// parseOperation, and returns the operations alongside a map of operation →
// first tag (kebab-cased) for grouping.
func parsePlatformOps(doc *openapi3.T) ([]*Operation, map[*Operation]string) {
	pathsMap := doc.Paths.Map()
	sortedPaths := make([]string, 0, len(pathsMap))
	for p := range pathsMap {
		sortedPaths = append(sortedPaths, p)
	}
	sort.Strings(sortedPaths)

	var ops []*Operation
	tagOf := make(map[*Operation]string)
	for _, path := range sortedPaths {
		pathItem := pathsMap[path]
		if pathItem == nil {
			continue
		}
		opsMap := pathItem.Operations()
		methods := make([]string, 0, len(opsMap))
		for m := range opsMap {
			methods = append(methods, m)
		}
		sort.Strings(methods)
		for _, method := range methods {
			rawOp := opsMap[method]
			if rawOp == nil {
				continue
			}
			parsed := parseOperation(path, method, rawOp)
			ops = append(ops, parsed)
			if len(rawOp.Tags) > 0 {
				tagOf[parsed] = strings.TrimSpace(rawOp.Tags[0])
			}
		}
	}
	return ops, tagOf
}

// stripTenantPrefix rewrites /v1/tenant/{tenantId}/... paths to /v1/... and
// removes the tenantId parameter from every operation. Mutates doc in place.
func stripTenantPrefix(doc map[string]any) {
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		return
	}
	rewritten := make(map[string]any, len(paths))
	for path, item := range paths {
		newPath := stripTenantSegment(path)
		if pi, ok := item.(map[string]any); ok {
			stripTenantParam(pi)
		}
		rewritten[newPath] = item
	}
	doc["paths"] = rewritten
}

// serviceSegment extracts the "{service}" path component from the spec's
// servers[0].url (e.g. "https://{region}.apigw.jamf.com/api/blueprints" →
// "blueprints"). Returns empty string when the URL does not match the expected
// gateway shape.
func serviceSegment(doc map[string]any) string {
	servers, _ := doc["servers"].([]any)
	if len(servers) == 0 {
		return ""
	}
	srv, _ := servers[0].(map[string]any)
	if srv == nil {
		return ""
	}
	url, _ := srv["url"].(string)
	const marker = "/api/"
	_, after, ok := strings.Cut(url, marker)
	if !ok {
		return ""
	}
	return after
}

// prependServiceSegment rewrites every path key from "/v1/foo" to
// "/api/{service}/v1/foo" so each operation carries its full URL path. Mutates
// doc in place.
func prependServiceSegment(doc map[string]any, service string) {
	if service == "" {
		return
	}
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		return
	}
	rewritten := make(map[string]any, len(paths))
	for path, item := range paths {
		rewritten["/api/"+service+path] = item
	}
	doc["paths"] = rewritten
}

// platformResourceNameOverrides maps tag-derived resource names to renamed
// CLI/Go names where the natural tag would collide with another product's
// resource (notably "users" — Pro, Protect, and School all have one).
var platformResourceNameOverrides = map[string]string{
	// "users" is a reserved CLI name shared with Pro/Protect/School;
	// the platform's users tag covers /users/{id}/devices only.
	"users": "platform-users",
}

// applyResourceNameOverride applies platformResourceNameOverrides if present.
func applyResourceNameOverride(name string) string {
	if override, ok := platformResourceNameOverrides[name]; ok {
		return override
	}
	return name
}

// trimPlatformPathPrefix strips the leading "api-{service}-v{n}-" segment from a
// path-derived resource name. Platform paths all share that shape, so the
// remainder is the actual collection name (e.g. "blueprints",
// "blueprint-components"). Returns the input unchanged when the expected
// prefix isn't present.
func trimPlatformPathPrefix(name string) string {
	if !strings.HasPrefix(name, "api-") {
		return name
	}
	rest := strings.TrimPrefix(name, "api-")
	// rest = "blueprints-v1-blueprints" — split on '-' and drop everything up
	// to and including the first segment that starts with 'v' followed by a digit.
	parts := strings.Split(rest, "-")
	for i, p := range parts {
		if len(p) >= 2 && p[0] == 'v' && p[1] >= '0' && p[1] <= '9' {
			if i+1 < len(parts) {
				return strings.Join(parts[i+1:], "-")
			}
			return ""
		}
	}
	return name
}

func stripTenantSegment(p string) string {
	const marker = "/tenant/{tenantId}"
	before, after, ok := strings.Cut(p, marker)
	if !ok {
		return p
	}
	return before + after
}

func stripTenantParam(pi map[string]any) {
	if params, ok := pi["parameters"].([]any); ok {
		pi["parameters"] = filterTenantParams(params)
	}
	for _, method := range []string{"get", "post", "put", "patch", "delete", "head", "options"} {
		op, ok := pi[method].(map[string]any)
		if !ok {
			continue
		}
		if params, ok := op["parameters"].([]any); ok {
			op["parameters"] = filterTenantParams(params)
		}
	}
}

func filterTenantParams(params []any) []any {
	out := params[:0]
	for _, p := range params {
		m, ok := p.(map[string]any)
		if !ok {
			out = append(out, p)
			continue
		}
		if name, _ := m["name"].(string); name == "tenantId" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// writeNormalisedTempSpec writes the rewritten doc to a temp file that
// preserves the original filename (so filename-based fallbacks still work).
func writeNormalisedTempSpec(originalPath string, doc map[string]any) (string, error) {
	dir, err := os.MkdirTemp("", "platform-spec-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}
	tmpPath := filepath.Join(dir, filepath.Base(originalPath))
	tmp, err := os.Create(tmpPath)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("creating temp spec: %w", err)
	}
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		_ = tmp.Close()
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("encoding normalised spec: %w", err)
	}
	_ = tmp.Close()
	return tmpPath, nil
}
