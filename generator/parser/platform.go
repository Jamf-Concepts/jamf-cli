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
	tenantPaths, expectedStatuses := normalisePlatformPaths(rawDoc, service, tenantPathVersion(rawDoc))

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
	applyPlatformPathMetadata(allOps, tenantPaths, expectedStatuses)

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
				fam.Name = applyResourceNameOverride(service, trimPlatformPathPrefix(fam.Name))
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

		name := applyResourceNameOverride(service, strcase.ToKebab(tag))
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

// tenantPathVersionExt is the published-spec extension naming the URL version
// segment an operation's path needs but does not carry. The Jamf Security Cloud
// -beta specs inject /tenant/{tenantId} without the version, and the gateway
// answers 403 BAD_PERMISSIONS for the versionless form, so the SDK records the
// correct version here when it publishes the spec.
const tenantPathVersionExt = "x-jamf-tenant-path-version"

// expectedStatusExt is the published-spec extension naming the success status
// the server actually answers, where the spec's declared status is wrong.
const expectedStatusExt = "x-jamf-expected-status"

// tenantPathVersion returns the document-level tenant path version, or "" when
// the spec's own paths already carry their version.
func tenantPathVersion(doc map[string]any) string {
	v, _ := doc[tenantPathVersionExt].(string)
	return v
}

// normalisePlatformPaths rewrites every path key to its full gateway form
// ("/api/{service}[/{version}]{specPath}") and then strips /tenant/{tenantId}
// from it, because path-family detection groups siblings by path and the tenant
// segment is filled from auth context rather than supplied per call.
//
// It returns the stripped→full mapping so emitters can recover the real request
// path without guessing where the tenant segment belongs. That guesswork used
// to live in the emitter as "insert after the first /v{n} segment", which is
// wrong for both shapes Security Cloud introduced: paths carrying no version at
// all (dns, ztna, categories — the version arrives via the extension) and paths
// whose version sits *after* the tenant segment (uem-connect's
// /tenant/{tenantId}/uem-connect/v1/connectors, where the heuristic would
// produce /uem-connect/v1/tenant/{tenantId}/connectors and 404).
//
// The second return value maps "<strippedPath> <METHOD>" to the operation's
// expected-status override, for the same reason: it has to be read off the raw
// document before kin-openapi re-serialises it, but applied to operations that
// are keyed by their stripped path.
//
// Mutates doc in place.
func normalisePlatformPaths(doc map[string]any, service, version string) (tenantPaths map[string]string, expectedStatuses map[string]int) {
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		return nil, nil
	}
	var prefix string
	if service != "" {
		prefix = "/api/" + service
	}
	if version != "" {
		prefix += "/" + version
	}

	rewritten := make(map[string]any, len(paths))
	tenantPaths = make(map[string]string, len(paths))
	expectedStatuses = make(map[string]int)
	for path, item := range paths {
		full := tenantBeforeVersion(service, prefix+path)
		stripped := stripTenantSegment(full)
		if pi, ok := item.(map[string]any); ok {
			collectExpectedStatuses(pi, stripped, expectedStatuses)
			stripTenantParam(pi)
		}
		rewritten[stripped] = item
		if full != stripped {
			tenantPaths[stripped] = full
		}
	}
	doc["paths"] = rewritten
	return tenantPaths, expectedStatuses
}

// collectExpectedStatuses records any x-jamf-expected-status on a path item's
// operations, keyed by "<strippedPath> <METHOD>".
func collectExpectedStatuses(pathItem map[string]any, strippedPath string, out map[string]int) {
	for _, method := range []string{"get", "post", "put", "patch", "delete"} {
		op, ok := pathItem[method].(map[string]any)
		if !ok {
			continue
		}
		// JSON numbers decode to float64.
		code, ok := op[expectedStatusExt].(float64)
		if !ok || code == 0 {
			continue
		}
		out[strippedPath+" "+strings.ToUpper(method)] = int(code)
	}
}

// platformOperationNameOverrides renames operations whose auto-derived name —
// taken from the last meaningful path segment — reads badly as a CLI verb.
// Keyed "{METHOD} {full path}", the same form the generated command dispatches.
//
// UEM Connect models sync as a collection of runs, so the derived names come
// out as "runs"/"create-runs"/"current" — describing the resource rather than
// the action. The SDK names the same three operations List/Trigger/Cancel.
var platformOperationNameOverrides = map[string]string{
	"GET /api/securitycloud/tenant/{tenantId}/uem-connect/v1/connectors/{configId}/sync/runs":            "list",
	"POST /api/securitycloud/tenant/{tenantId}/uem-connect/v1/connectors/{configId}/sync/runs":           "trigger",
	"DELETE /api/securitycloud/tenant/{tenantId}/uem-connect/v1/connectors/{configId}/sync/runs/current": "cancel",

	// Enablement is a sub-resource written with PUT and cleared with DELETE;
	// named for the path it reads as "enablement"/"delete-enablement". The SDK
	// calls the same pair Enable/Disable.
	"PUT /api/securitycloud/tenant/{tenantId}/uem-connect/v1/connectors/{configId}/enablement":    "enable",
	"DELETE /api/securitycloud/tenant/{tenantId}/uem-connect/v1/connectors/{configId}/enablement": "disable",

	// Sync settings are a singleton under the connector, so the terminal
	// segment repeats the resource name it is already nested under.
	"GET /api/securitycloud/tenant/{tenantId}/uem-connect/v1/connectors/{configId}/sync-settings": "get",
	"PUT /api/securitycloud/tenant/{tenantId}/uem-connect/v1/connectors/{configId}/sync-settings": "update",
}

// applyPlatformPathMetadata attaches the full tenant-bearing path, any
// expected-status override, and any operation-name override to each parsed
// operation.
func applyPlatformPathMetadata(ops []*Operation, tenantPaths map[string]string, expectedStatuses map[string]int) {
	for _, op := range ops {
		if full, ok := tenantPaths[op.Path]; ok {
			op.TenantPath = full
		}
		if code, ok := expectedStatuses[op.Path+" "+strings.ToUpper(op.Method)]; ok {
			op.ExpectedStatus = code
		}
		path := op.TenantPath
		if path == "" {
			path = op.Path
		}
		if name, ok := platformOperationNameOverrides[strings.ToUpper(op.Method)+" "+path]; ok {
			op.Name = name
		}
	}
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

// platformResourceNameOverrides renames tag-derived resource names that would
// otherwise be ambiguous or collide. Keys are "{service}/{name}" — the service
// being the gateway namespace from the spec's servers[0].url — falling back to
// a bare "{name}" that matches any service.
//
// Two reasons an entry exists here:
//
//   - Collision. Every platform spec emits into one Go package, so two specs
//     whose tags kebab to the same name would merge into one file and redeclare
//     each other's constructors. Jamf Platform and Jamf Security Cloud both tag
//     a resource "device-groups"; the Platform one is renamed because it is
//     already presented as "platform-device-groups" under `pro`.
//   - Ambiguity. Security Cloud's tags are bare nouns ("zones", "Apps",
//     "gateways") that say nothing about which service they belong to once they
//     sit alongside every other Jamf resource. They take the prefix their
//     product uses ("dns-", "ztna-", "uem-").
var platformResourceNameOverrides = map[string]string{
	// "users" is a reserved CLI name shared with Pro/Protect/School;
	// the platform's users tag covers /users/{id}/devices only.
	"users": "platform-users",

	// Jamf Platform device groups — renamed so Security Cloud's device-groups
	// tag keeps the unprefixed name, matching how each is surfaced (this one
	// under `pro` as platform-device-groups, that one under `security`).
	"device-groups/device-groups": "platform-device-groups",

	// Jamf Security Cloud — DNS.
	"securitycloud/zones":                    "dns-zones",
	"securitycloud/search-domains":           "dns-search-domains",
	"securitycloud/custom-hostname-mappings": "dns-custom-hostname-mappings",

	// Jamf Security Cloud — ZTNA.
	"securitycloud/apps":             "ztna-apps",
	"securitycloud/gateways":         "ztna-gateways",
	"securitycloud/grouped-gateways": "ztna-grouped-gateways",
	"securitycloud/shared-gateways":  "ztna-shared-gateways",
	"securitycloud/predefined-apps":  "ztna-predefined-apps",

	// Jamf Security Cloud — content categories. "categories" alone collides
	// conceptually with Pro's categories.
	"securitycloud/categories": "content-categories",

	// Jamf Security Cloud — UEM Connect. One spec, five tags, all describing
	// the connector and its sub-resources.
	"securitycloud/connectors":           "uem-connectors",
	"securitycloud/connector-enablement": "uem-connector-enablement",
	"securitycloud/sync-configuration":   "uem-sync-settings",
	"securitycloud/sync-execution":       "uem-sync",
	"securitycloud/activation-profiles":  "uem-activation-profiles",
}

// applyResourceNameOverride applies platformResourceNameOverrides, preferring a
// service-scoped entry over a bare-name one.
func applyResourceNameOverride(service, name string) string {
	if override, ok := platformResourceNameOverrides[service+"/"+name]; ok {
		return override
	}
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

// tenantFirstServices lists the gateway namespaces whose URLs place
// /tenant/{tenantId} *before* the version segment. Everything else in the Jamf
// estate is version-first, so this is an explicit allowlist, not a default.
//
// Only Security Cloud is in it, and the reason is auditing rather than routing.
// Its Tyk definition is a catch-all proxy that routes both orderings
// identically — the SDK wire-verified 200 for every family in both shapes on
// 2026-08-21 — but the audit rules deciding which mutating requests get
// recorded are path globs of the form `/**/v{n}/{service}/...`, which match a
// stripped path only when the tenant precedes the version. Under version-first
// most Security Cloud mutations executed and were never audited, so a CLI
// `dns-zones delete` left no record on the gateway side.
//
// This duplicates tenantFirstNamespaces in the SDK's internal/client, which the
// CLI cannot import. The duplication is self-detecting rather than silent: the
// gateway setup command builds its validation path through the SDK's
// TenantPrefix, so a divergence fails TestValidatePlatformGatewayCredentials_
// SecurityCloudTenant, whose httptest mux registers the path the generator
// produces.
var tenantFirstServices = map[string]bool{
	securityCloudService: true,
}

// securityCloudService is the gateway namespace Jamf Security Cloud is served
// under.
const securityCloudService = "securitycloud"

// tenantFirstService reports whether a service's URLs put the tenant segment
// before the version. A service matches exactly or on its first path segment,
// mirroring the SDK, so a future "securitycloud/{sub}" inherits the ordering
// instead of silently reverting to version-first.
func tenantFirstService(service string) bool {
	if tenantFirstServices[service] {
		return true
	}
	root, _, found := strings.Cut(service, "/")
	return found && tenantFirstServices[root]
}

// tenantBeforeVersion moves /tenant/{tenantId} ahead of the first version
// segment for services that need it, leaving every other path untouched.
//
// The three shapes Security Cloud's specs arrive in all end up the same way:
// dns and ztna carry no version at all and take it from
// x-jamf-tenant-path-version, which prefix puts in front of the spec's
// /tenant/{tenantId} (/api/securitycloud/v1/tenant/{t}/dns/zones); categories
// and device groups carry the version inside the path, ahead of the tenant
// (/api/securitycloud/v1/tenant/{t}/groups); and uem-connect already declares
// the tenant first, with its version after the sub-namespace
// (/api/securitycloud/tenant/{t}/uem-connect/v1/connectors), which is why this
// moves the tenant to the version rather than swapping the two — the uem-connect
// version is not the segment the tenant belongs in front of.
func tenantBeforeVersion(service, p string) string {
	if !tenantFirstService(service) {
		return p
	}
	const marker = "tenant"
	parts := strings.Split(p, "/")
	tenantIdx, versionIdx := -1, -1
	for i, seg := range parts {
		if tenantIdx == -1 && seg == marker && i+1 < len(parts) && parts[i+1] == "{tenantId}" {
			tenantIdx = i
		}
		if versionIdx == -1 && isVersionSegment(seg) {
			versionIdx = i
		}
	}
	// Nothing to reorder when either segment is absent, or when the tenant
	// already precedes the version.
	if tenantIdx == -1 || versionIdx == -1 || tenantIdx < versionIdx {
		return p
	}
	out := make([]string, 0, len(parts))
	out = append(out, parts[:versionIdx]...)
	out = append(out, marker, "{tenantId}")
	out = append(out, parts[versionIdx:tenantIdx]...)
	out = append(out, parts[tenantIdx+2:]...)
	return strings.Join(out, "/")
}

// isVersionSegment reports whether a path segment is a URL version ("v1", "v2").
func isVersionSegment(seg string) bool {
	return len(seg) >= 2 && seg[0] == 'v' && seg[1] >= '0' && seg[1] <= '9'
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
