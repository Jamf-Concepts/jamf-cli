// Copyright 2026, Jamf Software LLC

package parser

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
	// "status" already ends in 's'-sound — auto-pluralizer appends an extra 's'.
	"ddm-statuss":              "ddm-status",
	"startup-statuss":          "startup-status",
	"apns-client-push-statuss": "apns-client-push-status",
	// "m2m" → kebab → "m-2-m" → pluralize → "m-2-ms"; preserve the original token.
	"m-2-ms": "m2m",
}

// documentedStatusResults lists operations for which a non-2xx status is a
// documented *result* rather than a failure, keyed by "METHOD path" and holding
// the statuses to carry through to the user.
//
// There is no spec signal for this: almost every Jamf operation documents a 403
// (and a 401/404) as boilerplate error responses, so "declares a 403" cannot
// distinguish a check endpoint whose 403 body is the answer from a normal
// endpoint whose 403 means "your token is not allowed". Hence an explicit map,
// the same way TagFilenameOverrides and resourceNameFieldOverrides handle what
// the specs can't express.
//
// The generated command carries these through registry.WithAllowedStatuses, so
// the client returns the response instead of mapping it to an exit-code error,
// and renders the body through the normal formatter. See the resourceTemplate
// in generator/generator.go.
var documentedStatusResults = map[string][]int{
	// 204 = the DigiCert account holds every deployment permission; 403 = the
	// body lists the ones it is missing. Without this the 403 became
	// exitcode.PermissionDenied with a hint blaming the caller's own API role,
	// and the missing-permission list only ever appeared inside an error string.
	"GET /v1/pki/digicert/trust-lifecycle-manager/{id}/privilege-check": {403},
}

// applyDocumentedStatusResults populates op.StatusResults/NoContentDescription
// from documentedStatusResults, pulling each status's human description from the
// spec so the generated command doesn't hardcode Jamf's wording.
func applyDocumentedStatusResults(op *Operation) {
	statuses, ok := documentedStatusResults[op.Method+" "+op.Path]
	if !ok {
		return
	}
	for _, code := range statuses {
		result := StatusResult{Code: code}
		if resp, ok := op.Responses[strconv.Itoa(code)]; ok {
			result.Description = strings.TrimSpace(resp.Description)
		}
		op.StatusResults = append(op.StatusResults, result)
	}
	if resp, ok := op.Responses["204"]; ok {
		op.NoContentDescription = strings.TrimSpace(resp.Description)
	}
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
		// hardware.serialNumber lives in the HARDWARE section, which the default
		// (GENERAL) response omits — request it so filterResultsByName can verify
		// the match rather than trusting the server's RSQL filter blindly. udid and
		// id are top-level and always present, so udid needs no section.
		{Flag: "serial", RSQLField: "hardware.serialNumber", Desc: "Look up computer by serial number", Section: "HARDWARE"},
		{Flag: "udid", RSQLField: "udid", Desc: "Look up computer by UDID"},
	},
	// /v2/mobile-devices/detail (the lookup path) uses top-level "serialNumber"
	// and "udid" filter fields — not the nested "hardware.serialNumber".
	"mobile-devices": {
		{Flag: "serial", RSQLField: "serialNumber", Desc: "Look up mobile device by serial number"},
		{Flag: "udid", RSQLField: "udid", Desc: "Look up mobile device by UDID"},
	},
}

// resourceGroupPaths maps canonical resource names to the Classic API group list
// path (without /JSSResource/ prefix) for --group flag support on delete.
var resourceGroupPaths = map[string]string{
	"computers-inventory": "computergroups",
}

// ApplyLookupFields sets LookupFields and GroupsClassicPath on resources.
// Must be called after DeduplicateVersioned so resource names are canonical.
func ApplyLookupFields(resources []*Resource) {
	for _, r := range resources {
		if fields, ok := resourceLookupFields[r.Name]; ok {
			r.LookupFields = fields
		}
		if path, ok := resourceGroupPaths[r.Name]; ok {
			r.GroupsClassicPath = path
		}
	}
}

// resourceFileFields maps canonical resource names to file-sourced body fields
// exposed on create/update/apply/patch. Each entry adds a dedicated flag whose
// file contents are injected into the parsed request body pre-marshal, so callers
// don't need to include the target property/value in their JSON input.
var resourceFileFields = map[string][]FileField{
	"scripts": {{
		Flag:         "script-file",
		Field:        "scriptContents",
		Encoding:     "raw",
		Desc:         "Path to a script file; contents populate scriptContents",
		NameFallback: "keep-ext",
	}},
	"computer-extension-attributes": {{
		Flag:         "script-file",
		Field:        "scriptContents",
		Encoding:     "raw",
		Desc:         "Path to a script file; contents populate scriptContents (only meaningful for SCRIPT inputType)",
		NameFallback: "keep-ext",
	}},
	"vpp-locations": {{
		Flag:  "token-file",
		Field: "serviceToken",
		// .vpptoken files are already a base64-encoded JSON blob; Jamf expects
		// that string verbatim — base64-encoding again would double-wrap and
		// get rejected with INVALID_FIELD ("not parsable as a VPP token").
		Encoding:     "raw",
		Desc:         "Path to a VPP service token (.vpptoken); contents populate serviceToken verbatim",
		NameFallback: "none",
		NameFlag:     true,
	}},
	"device-enrollment-instances": {{
		Flag:              "token-file",
		Field:             "encodedToken",
		Encoding:          "base64",
		Desc:              "Path to a DEP server token (.p7m); contents are base64-encoded into encodedToken",
		CompanionField:    "tokenFileName",
		NameFallback:      "none",
		NameFlag:          true,
		RenameAfterUpload: true, // /upload-token and /{id}/upload-token reject a "name" field; follow up with PUT /v1/device-enrollments/{id}
	}},
}

// ApplyFileFields sets FileFields on resources listed in resourceFileFields.
// Must be called after DeduplicateVersioned/ApplyNameOverrides so names are canonical.
func ApplyFileFields(resources []*Resource) {
	for _, r := range resources {
		if fields, ok := resourceFileFields[r.Name]; ok {
			r.FileFields = fields
		}
	}
}

// resourceCreateOpOverrides maps resources whose creation happens via a sub-path
// action (e.g. POST /collection/<action>) rather than a bare POST on the collection
// path. When set, the matching operation is renamed to "create" so the standard
// create subcommand is generated (with its --scaffold / file-field / apply wiring),
// and no parallel action subcommand is emitted.
var resourceCreateOpOverrides = map[string]OpPathMethod{
	// Jamf Pro has no POST /v1/device-enrollments; creation goes through the
	// token-upload action endpoint.
	"device-enrollment-instances": {Path: "/v1/device-enrollments/upload-token", Method: "POST"},
}

// OpPathMethod is a (path, method) pair used to identify an operation for overrides.
type OpPathMethod struct {
	Path   string
	Method string
}

// ApplyCreateOpOverrides renames operations listed in resourceCreateOpOverrides to
// "create" so the generator emits them as the resource's canonical create command.
// Must be called after DeduplicateVersioned/ApplyNameOverrides so resource names
// are canonical.
func ApplyCreateOpOverrides(resources []*Resource) {
	for _, r := range resources {
		target, ok := resourceCreateOpOverrides[r.Name]
		if !ok {
			continue
		}
		for _, op := range r.Operations {
			if op.Path == target.Path && op.Method == target.Method {
				op.Name = "create"
				break
			}
		}
	}
}

// resourceUpdateTokenOpOverrides maps resources that have an auxiliary PUT endpoint
// for file-field payloads (e.g. PUT /v1/device-enrollments/{id}/upload-token). When
// matched, the operation is lifted off the resource's Operations slice (so it does
// not produce its own subcommand) and attached to r.UpdateTokenOp, and the update/
// apply templates route the resource's file-field flag to it.
var resourceUpdateTokenOpOverrides = map[string]OpPathMethod{
	"device-enrollment-instances": {Path: "/v1/device-enrollments/{id}/upload-token", Method: "PUT"},
}

// ApplyUpdateTokenOpOverrides detaches the configured auxiliary token-update op from
// the resource's Operations slice and records it on r.UpdateTokenOp. The main update
// command then composes a token PUT + a body PUT based on which flags are supplied.
func ApplyUpdateTokenOpOverrides(resources []*Resource) {
	for _, r := range resources {
		target, ok := resourceUpdateTokenOpOverrides[r.Name]
		if !ok {
			continue
		}
		filtered := r.Operations[:0]
		for _, op := range r.Operations {
			if op.Path == target.Path && op.Method == target.Method {
				r.UpdateTokenOp = op
				continue
			}
			filtered = append(filtered, op)
		}
		r.Operations = filtered
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
	// Mobile device groups (smart + static) expose "groupName" — the detected
	// "displayName" is used in the POST/PUT body but does not appear in list
	// responses, so both filter lookups and backup file naming fail silently.
	"mobile-device-groups-smart-groups":  "groupName",
	"mobile-device-groups-static-groups": "groupName",
	// mdm-commands is a command log, not a name-addressable resource. The
	// detector otherwise picks up `userName` from action payload schemas
	// (DeleteUserCommand, UnlockUserAccountCommand) that live in the same
	// spec as request bodies. Force-clear it.
	"mdm-commands": "",
	// inventory-preloads records are keyed by serialNumber, not a "name" field.
	// Override so --name lookups and backup file naming both use serial number.
	"inventory-preloads": "serialNumber",
}

// resourceNameLookupPathOverrides maps resource names to an alternate list path
// used exclusively for name-based resolution. Needed when the primary list
// endpoint ignores RSQL filter params but a sibling endpoint supports it.
var resourceNameLookupPathOverrides = map[string]string{
	// /v2/mobile-devices ignores RSQL; /v2/mobile-devices/detail supports it
	// and exposes "displayName" as the filterable name field.
	"mobile-devices": "/v2/mobile-devices/detail",
}

// resourceNameLookupIDFieldOverrides maps resource names to the ID field name
// in the NameLookupPath response when it differs from IDField. Needed when the
// alternate lookup endpoint uses a different property for the record identifier.
var resourceNameLookupIDFieldOverrides = map[string]string{
	// /v2/mobile-devices/detail returns "mobileDeviceId" (not "id").
	"mobile-devices": "mobileDeviceId",
}

// resourceIDFieldOverrides maps canonical resource names to the correct response
// field for ID extraction during name-to-ID resolution. Used when detectIDField()
// returns the wrong value — typically for resources that expose a UUID platform ID
// (used in PATCH/DELETE paths) alongside a legacy integer Jamf Pro ID.
var resourceIDFieldOverrides = map[string]string{
	// Groups list response uses "groupPlatformId" (UUID) for PATCH/DELETE paths,
	// not the legacy integer "groupJamfProId".
	"groups": "groupPlatformId",
	// Mobile device groups (smart + static) list response uses "groupId" as
	// the identifier; "id" is not present.
	"mobile-device-groups-smart-groups":  "groupId",
	"mobile-device-groups-static-groups": "groupId",
}

// resourceTableColumns maps canonical resource names to preferred columns for
// list table output. When set, the generated list command selects exactly these
// columns for table/csv/plain output instead of the generic alphabetical selection.
// JSON/YAML output is unaffected.
var resourceTableColumns = map[string][]TableColumn{
	"computers-inventory": {
		{Field: "id", Label: "id"},
		{Field: "general.name", Label: "name"},
		{Field: "hardware.serialNumber", Label: "serial"},
		{Field: "hardware.model", Label: "model"},
		{Field: "operatingSystem.version", Label: "osVersion"},
		{Field: "general.lastContactTime", Label: "lastContactTime"},
	},
	"mobile-devices": {
		{Field: "mobileDeviceId", Label: "id"},
		{Field: "general.displayName", Label: "name"},
		{Field: "hardware.serialNumber", Label: "serial"},
		{Field: "hardware.model", Label: "model"},
		{Field: "general.osVersion", Label: "osVersion"},
		{Field: "general.lastInventoryUpdateDate", Label: "lastInventoryUpdate"},
	},
}

// resourceDefaultSections maps canonical resource names to the default --section
// values for list commands. When set, the generated list command fetches these
// sections by default to ensure table output has the necessary data.
var resourceDefaultSections = map[string][]string{
	"computers-inventory": {"GENERAL", "HARDWARE", "OPERATING_SYSTEM"},
	"mobile-devices":      {"GENERAL", "HARDWARE"},
}

// resourceListDetailPathOverrides maps canonical resource names to a detail list
// endpoint that supports --section filtering. The list operation's path is swapped
// and a section query parameter is injected. DefaultSections provides defaults.
var resourceListDetailPathOverrides = map[string]string{
	// /v2/mobile-devices returns flat basic records;
	// /v2/mobile-devices/detail returns sectioned records with richer data.
	"mobile-devices": "/v2/mobile-devices/detail",
}

// ApplyListDetailPaths swaps the "list" operation's path to a richer detail
// endpoint and injects a section query parameter. Must be called after
// ApplyNameOverrides and before ApplyTableColumns.
func ApplyListDetailPaths(resources []*Resource) {
	for _, r := range resources {
		detailPath, ok := resourceListDetailPathOverrides[r.Name]
		if !ok {
			continue
		}
		for _, op := range r.Operations {
			if op.Name == "list" && op.Method == "GET" {
				op.Path = detailPath
				// Inject section parameter if not already present.
				hasSection := false
				for _, p := range op.Parameters {
					if p.Name == "section" {
						hasSection = true
						break
					}
				}
				if !hasSection {
					op.Parameters = append(op.Parameters, &Parameter{
						Name:        "section",
						In:          "query",
						Description: "section of mobile device details, if not specified, General section data is returned. Multiple section parameters are supported, e.g. section=GENERAL&section=HARDWARE",
						Type:        "string",
						IsArray:     true,
					})
				}
				break
			}
		}
	}
}

// resourceGetDetailPathOverrides maps canonical resource names to an alternate
// detail path that the "get" command should use instead of the basic GET/{id}.
// When set, the "get" operation's path is swapped to the detail endpoint (which
// returns all sections/fields) and any operations made redundant by the swap
// (e.g. a "detail" subcommand) are removed.
var resourceGetDetailPathOverrides = map[string]struct {
	DetailPath string   // Path to use for "get" (must contain {id})
	Remove     []string // Operation names to remove (now redundant)
}{
	// /v3/computers-inventory/{id} returns only requested sections;
	// /v3/computers-inventory-detail/{id} returns all sections — better for "get".
	"computers-inventory": {
		DetailPath: "/v3/computers-inventory-detail/{id}",
	},
	// /v2/mobile-devices/{id} returns basic info;
	// /v2/mobile-devices/{id}/detail returns full detail — better for "get".
	// The parser auto-created a "detail" subcommand from the detail path; remove it.
	"mobile-devices": {
		DetailPath: "/v2/mobile-devices/{id}/detail",
		Remove:     []string{"detail"},
	},
}

// ApplyGetDetailPaths configures "get" commands to use a richer detail endpoint.
// For resources whose get has a section parameter (e.g. computers-inventory), the
// detail path is stored in GetDetailPath and used as the default — specifying
// --section overrides back to the original path. For resources without section
// filtering (e.g. mobile-devices), the get path is swapped outright.
// Must be called after ApplyNameOverrides.
func ApplyGetDetailPaths(resources []*Resource) {
	for _, r := range resources {
		override, ok := resourceGetDetailPathOverrides[r.Name]
		if !ok {
			continue
		}

		// Collect all "get" GET operations. Some resources have two: one on the
		// basic path (with section param) and one on the detail path (without).
		var getOps []*Operation
		for _, op := range r.Operations {
			if op.Name == "get" && op.Method == "GET" {
				getOps = append(getOps, op)
			}
		}
		if len(getOps) == 0 {
			continue
		}

		// Check if any get op has a section query parameter.
		var sectionGet *Operation
		for _, op := range getOps {
			for _, p := range op.Parameters {
				if p.In == "query" && p.Name == "section" {
					sectionGet = op
					break
				}
			}
			if sectionGet != nil {
				break
			}
		}

		// Pick the surviving get operation and configure the detail path.
		var keeper *Operation
		if sectionGet != nil {
			// Keep the section-capable get. Template will use GetDetailPath when
			// --section is not explicitly set, falling back to the original path
			// with section params when --section is provided.
			keeper = sectionGet
			r.GetDetailPath = override.DetailPath
		} else {
			// No section filtering — swap the path outright and strip query params.
			keeper = getOps[0]
			keeper.Path = override.DetailPath
			var kept []*Parameter
			for _, p := range keeper.Parameters {
				if p.In == "path" {
					kept = append(kept, p)
				}
			}
			keeper.Parameters = kept
		}

		// Remove duplicate "get" GET ops and any operations listed in Remove.
		removeSet := make(map[string]bool, len(override.Remove))
		for _, name := range override.Remove {
			removeSet[name] = true
		}
		var kept []*Operation
		for _, op := range r.Operations {
			if removeSet[op.Name] {
				continue
			}
			if op.Name == "get" && op.Method == "GET" && op != keeper {
				continue
			}
			kept = append(kept, op)
		}
		r.Operations = kept
	}
}

// ApplyTableColumns sets TableColumns and DefaultSections on resources that have
// preferred column configuration. Must be called after ApplyNameOverrides.
func ApplyTableColumns(resources []*Resource) {
	for _, r := range resources {
		if cols, ok := resourceTableColumns[r.Name]; ok {
			r.TableColumns = cols
		}
		if sections, ok := resourceDefaultSections[r.Name]; ok {
			r.DefaultSections = sections
		}
	}
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
		if path, ok := resourceNameLookupPathOverrides[r.Name]; ok {
			r.NameLookupPath = path
		}
		if idField, ok := resourceNameLookupIDFieldOverrides[r.Name]; ok {
			r.NameLookupIDField = idField
		}
	}
}

// singularize converts a plural resource name to singular.
// Handles: -ies → -y (policies → policy), -sses → -ss (statuses → status),
// -s → "" (buildings → building).
func singularize(name string) string {
	if before, ok := strings.CutSuffix(name, "ies"); ok {
		return before + "y"
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
	// 0. Reclassify collection-root POSTs that upstream mis-tagged as x-action
	//    but which clearly belong to a CRUD family (evidenced by the presence
	//    of a sibling /{collection}/{id} path). The per-op 201-response check
	//    in parseOperation misses cases where the monolith declares only 200.
	//    /v1/deploy-thing (no {id} sibling) keeps its path-segment name.
	reclassifyMisannotatedCreates(allOps)
	// 0b. Pre-rename the singleton root GET from "list" to "get" so it is
	//     immune to the conflict-resolution pass below. Without this, a
	//     non-list sibling GET (e.g. /singleton/download lacking x-action in
	//     the monolith) collides with the root as "list" and both get
	//     renamed to their terminal segments, hiding the canonical get op.
	renameSingletonRootGet(allOps)
	// 1. Drop lower-version duplicates: when the same path exists at multiple API
	//    versions in one spec (e.g. /v2/foo and /v3/foo), keep only the highest.
	allOps = deduplicateVersionedOps(allOps)
	// 1b. Pair a per-{id} action with its collection-level bulk sibling (surfaced
	//     as --all) and drop the sibling, before name disambiguation runs — so the
	//     per-{id} op keeps its clean terminal name and no duplicate command emits.
	allOps = pairCollectionBulkActions(allOps)
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
		found := slices.Contains(filteredOps, op)
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
		// Rename "list" → "get" for the singleton root path only. Sub-paths
		// that still carry the "list" name here (e.g. /v2/sso/cert/download
		// when upstream stripped x-action) are not list endpoints either, but
		// blindly renaming them to "get" collides with the canonical root get.
		// Leave them as "list" and let the sub-path fallback (resolveNoParam
		// below) handle the rename to their terminal segment if needed.
		rootPath := ""
		for _, op := range resource.Operations {
			if op.Method == "GET" && !op.IsList && !hasPathParam(op.Path) {
				if rootPath == "" || len(op.Path) < len(rootPath) {
					rootPath = op.Path
				}
			}
		}
		for _, op := range resource.Operations {
			if op.Name != "list" {
				continue
			}
			if op.Path == rootPath {
				op.Name = "get"
				continue
			}
			// Sub-path GETs still carrying "list" inside a singleton are not
			// list endpoints — rename to their terminal segment (e.g.
			// /v2/sso/cert/download → "download") so they don't collide with
			// the root "get" or confuse users.
			parts := strings.Split(op.Path, "/")
			tail := parts[len(parts)-1]
			if !strings.HasPrefix(tail, "{") && tail != "" {
				op.Name = strcase.ToKebab(tail)
			}
		}
	} else {
		resourceName := pluralize(kebabName)
		resource.Name = resourceName
		resource.NameSingular = singularize(resourceName)
		resource.GoName = strcase.ToCamel(resourceName)
	}

	// Detect optimistic locking (prestages use versionLock in PUT/POST request bodies).
	resource.HasVersionLock = detectVersionLock(allOps)

	return []*Resource{resource}, nil
}

// pairCollectionBulkActions links a per-{id} x-action to a sibling
// collection-level action at the same path minus its {id} segment (e.g. POST
// /deployments/{id}/computers/installation-retry and the bulk POST
// /deployments/computers/installation-retry). It records the bulk endpoint's
// path on the per-{id} op as BulkActionPath — the generator surfaces that as an
// --all flag — and removes the bulk op so it neither emits a duplicate command
// nor forces the per-{id} op to be renamed by same-terminal disambiguation.
// Collection-level actions with no per-{id} sibling (e.g. "export") are left
// untouched and still become their own commands. Matches on path structure
// rather than operation name, and MUST run before disambiguateSameTerminalOps so
// the surviving per-{id} op keeps its clean terminal name.
func pairCollectionBulkActions(ops []*Operation) []*Operation {
	// Index collection-level (no path param) actions by path.
	bulkByPath := make(map[string]*Operation)
	for _, op := range ops {
		if op.IsAction && !hasPathParam(op.Path) {
			bulkByPath[op.Path] = op
		}
	}
	if len(bulkByPath) == 0 {
		return ops
	}

	remove := make(map[*Operation]bool)
	for _, op := range ops {
		if !op.IsAction || !hasPathParam(op.Path) {
			continue
		}
		// The per-{id} op must end in a literal verb segment (e.g. .../installation-retry).
		// A path ending in the {id} param itself (e.g. PUT /accounts/{id}) is a CRUD
		// op the spec mis-tagged x-action — its stripped path would spuriously match a
		// collection-level create/list, so exclude it.
		if strings.HasSuffix(op.Path, "}") {
			continue
		}
		// Exactly one path param: stripping it must yield the collection-level bulk
		// path. A multi-param action (e.g. .../{id}/computers/{computerId}/installation-retry)
		// pairs with its own single-param parent, not the no-param bulk endpoint, so
		// it stays a standalone command rather than claiming --all.
		if strings.Count(op.Path, "{") != 1 {
			continue
		}
		// The bulk sibling is this path with every {param} segment removed, and must
		// use the same HTTP method (a retry POST pairs with a bulk retry POST, never
		// with a create POST or an update PUT at the collection root).
		bulk, ok := bulkByPath[stripParamSegments(op.Path)]
		if !ok || bulk == op || bulk.Method != op.Method {
			continue
		}
		op.BulkActionPath = bulk.Path
		remove[bulk] = true
	}
	if len(remove) == 0 {
		return ops
	}

	kept := make([]*Operation, 0, len(ops))
	for _, op := range ops {
		if !remove[op] {
			kept = append(kept, op)
		}
	}
	return kept
}

// stripParamSegments removes every {param} segment from a path,
// e.g. /v1/foo/{id}/bar → /v1/foo/bar.
func stripParamSegments(path string) string {
	parts := strings.Split(path, "/")
	kept := parts[:0]
	for _, p := range parts {
		if strings.HasPrefix(p, "{") {
			continue
		}
		kept = append(kept, p)
	}
	return strings.Join(kept, "/")
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
			r.HasVersionLock = detectVersionLock(familyOps)

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
		r.HasVersionLock = detectVersionLock(parentOps)
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

// hasResponseCode reports whether an openapi3 operation declares the given
// HTTP response code. Used alongside isCollectionRootPath to distinguish a
// mis-annotated CRUD create (201) from a legitimate collection-root action
// (200/202/204) when x-action: true is set.
func hasResponseCode(op *openapi3.Operation, code string) bool {
	if op == nil || op.Responses == nil {
		return false
	}
	return op.Responses.Value(code) != nil
}

// isCollectionRootPath reports whether path is a plain collection root: a
// version prefix followed by exactly one resource segment, with no path
// parameters (e.g. /v1/foo, /preview/bar). Used to guard against upstream
// specs that mis-tag a collection-root CRUD op with x-action: true.
func isCollectionRootPath(path string) bool {
	if strings.Contains(path, "{") {
		return false
	}
	stripped := stripVersionPrefix(path)
	if stripped == path {
		return false
	}
	rest := strings.TrimPrefix(stripped, "/")
	return rest != "" && !strings.Contains(rest, "/")
}

// stripVersionPrefix removes a leading version segment (v1, v2, v3, preview,
// etc.) from a path, leaving the leading slash intact.
// e.g. /v1/self-service/branding → /self-service/branding
// Paths without a version prefix are returned unchanged.
func stripVersionPrefix(path string) string {
	trimmed := strings.TrimPrefix(path, "/")
	if before, after, ok := strings.Cut(trimmed, "/"); ok {
		prefix := before
		if strings.HasPrefix(prefix, "v") || prefix == "preview" {
			return "/" + after
		}
	}
	return path
}

// versionSegment matches a path segment that names an API version — "v1",
// "v12", or the pre-release "preview".
var versionSegment = regexp.MustCompile(`^(v\d+|preview)$`)

// stripVersionSegments removes every version segment from a path, wherever it
// sits, leaving the rest of the path intact.
//
// This is the version-blind identity of an endpoint, and it is what
// deduplicateVersionedOps keys on. stripVersionPrefix only handles a *leading*
// version, which is enough for Jamf Pro ("/v1/computers-inventory") but blind
// to the shape the Platform Gateway uses, where the version follows the service
// namespace ("/securitycloud/v1/groups") or a sub-namespace
// ("/securitycloud/uem-connect/v1/connectors"). Without this, two versions
// of the same gateway endpoint hash to different keys and both ship as
// commands — which is how Security Cloud's device groups ended up with a "list"
// on the deprecated v1 alongside a "list-v2" on its successor.
//
// Removing every segment rather than the first is safe rather than merely
// convenient: across all Jamf Pro specs and every published platform spec it
// introduces exactly one new collision, the v1/v2 device-groups list it is
// meant to catch.
func stripVersionSegments(path string) string {
	if !strings.Contains(path, "/v") && !strings.Contains(path, "/preview") {
		return path
	}
	segs := strings.Split(path, "/")
	kept := segs[:0:len(segs)]
	for i, s := range segs {
		// i == 0 is the empty string before the leading slash; keep it so the
		// result stays absolute.
		if i > 0 && versionSegment.MatchString(s) {
			continue
		}
		kept = append(kept, s)
	}
	return strings.Join(kept, "/")
}

// apiVersionRank ranks a path by the first version segment it carries, wherever
// that sits: a numeric version scores its own number, "preview" scores below
// any of them as a pre-release, and a path carrying no version at all scores 0
// (legacy, beaten by any explicit version).
//
// The first segment rather than the highest, because a path carrying two is
// versioning two different things — Security Cloud's UEM Connect nests a
// service version under the gateway namespace — and it is the outermost one
// that distinguishes siblings.
func apiVersionRank(path string) int {
	for _, s := range strings.Split(path, "/") {
		if !versionSegment.MatchString(s) {
			continue
		}
		if s == "preview" {
			return -1
		}
		var n int
		if _, err := fmt.Sscanf(s[1:], "%d", &n); err == nil {
			return n
		}
	}
	return 0
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

// detectVersionLock returns true if any PUT or POST request body in the resource
// includes a versionLock property. This indicates the resource uses optimistic
// locking (prestages and prestage scopes).
func detectVersionLock(ops []*Operation) bool {
	for _, op := range ops {
		if op.Method != "PUT" && op.Method != "POST" {
			continue
		}
		if op.RequestBody == nil || op.RequestBody.Schema == nil {
			continue
		}
		if _, ok := op.RequestBody.Schema.Properties["versionLock"]; ok {
			return true
		}
	}
	return false
}

// detectSingleton returns true if the operations describe a singleton resource:
// a settings-style object accessible via a single path with no {id} parameter,
// identified by a non-paginated GET and a PUT on the same path.
// readOnlySingletonPaths marks resources that are singletons with no PUT: one
// GET, no {id}, nothing to update. The GET+PUT rule below cannot see them, and
// without this they generate `list` for an endpoint that returns a single object
// (and a `--field id` example for a schema with no id).
//
// Keyed by the resource's root GET path. Deliberately an allowlist rather than a
// rule: inferring "GET-only and no {id} ⇒ singleton" would also rename the
// existing startup-status / ddm-status / apns-client-push-status commands from
// `list` to `get`, which is a user-visible break that belongs in its own PR.
var readOnlySingletonPaths = map[string]bool{
	// Reports the one cloud-services environment this instance talks to.
	"/v2/environment-type": true,
}

func detectSingleton(ops []*Operation) bool {
	// Any path parameter means this is a collection or keyed resource, not a singleton.
	for _, op := range ops {
		if hasPathParam(op.Path) {
			return false
		}
	}

	for _, op := range ops {
		if op.Method == "GET" && readOnlySingletonPaths[op.Path] {
			return true
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
//
// For GET and DELETE operations the displaced lower-version path is appended to
// the winner's FallbackPaths (in descending version order) so the runtime can
// retry on 404 against older Jamf Pro tenants.
func deduplicateVersionedOps(ops []*Operation) []*Operation {
	type key struct{ method, path string }
	seen := make(map[key]*Operation)
	result := make([]*Operation, 0, len(ops))

	for _, op := range ops {
		k := key{op.Method, stripVersionSegments(op.Path)}
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
			// Carry fallback chain: try prev first, then its own fallbacks.
			if op.Method == "GET" || op.Method == "DELETE" {
				op.FallbackPaths = append([]string{prev.Path}, prev.FallbackPaths...)
			}
		} else {
			// Keep prev (higher or equal version), record op as a fallback.
			if op.Method == "GET" || op.Method == "DELETE" {
				prev.FallbackPaths = append(prev.FallbackPaths, op.Path)
				prev.FallbackPaths = append(prev.FallbackPaths, op.FallbackPaths...)
			}
		}
	}

	// Sort each op's FallbackPaths in descending version order so the runtime
	// always tries the newest available fallback first.
	for _, op := range result {
		if len(op.FallbackPaths) > 1 {
			sort.Slice(op.FallbackPaths, func(i, j int) bool {
				return compareAPIVersions(op.FallbackPaths[i], op.FallbackPaths[j]) > 0
			})
		}
	}

	return result
}

// compareAPIVersions returns >0 if path1 should be preferred over path2.
// Comparison order: explicit /v3 > /v2 > /v1 > unversioned > preview (treated as
// pre-release, kept for now but lower than any numeric version).
//
// The version is read from wherever it sits in the path, not just the leading
// segment — see apiVersionRank. A gateway path carries it after the service
// namespace, and ranking those by their leading segment scored every one of
// them 0, making the "prefer the higher version" branch a coin toss decided by
// map iteration order.
func compareAPIVersions(path1, path2 string) int {
	r1, r2 := apiVersionRank(path1), apiVersionRank(path2)
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
		candidate = candidate + "-by-" + strcase.ToKebab(lastExtraParam)
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
	// Correct an upstream mis-annotation: some Jamf specs tag a plain
	// collection-root CRUD create (e.g. POST /v1/mobile-device-extension-attributes)
	// with x-action: true, which otherwise causes the CREATE to be named after
	// the collection segment and shadowed under the group. Detect by path shape
	// (version prefix + single non-param segment) plus a 201 response, which is
	// the reliable signal that this POST actually creates a new resource. True
	// actions like /v1/slasa (204), /v1/deploy-package (200), and
	// /v2/patch-management-accept-disclaimer (202) keep their x-action naming.
	if isAction && strings.ToLower(method) == "post" && isCollectionRootPath(path) && hasResponseCode(op, "201") {
		opName = "create"
	}
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
		// IsPaginated is broader than IsList: any GET exposing pagination
		// params (page/page-size) returns a {totalCount, results} collection
		// and should support --all / --limit auto-pagination — including
		// report/action ops like patch-report. It gates only the Pro auto-
		// pagination flags+loop; IsList still drives list-only semantics
		// (default sections, output array key, singleton detection). See #245.
		IsPaginated: strings.ToUpper(method) == "GET" && hasPaginationParams(op),
		APIVersion:  extractAPIVersion(path),
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
			// Capture the JSON response schema (when present) so downstream
			// generators can tell whether the operation returns a body to
			// unmarshal. We don't deeply parse the schema here — the presence
			// signal alone is enough for many generation decisions.
			if jsonContent, ok := respRef.Value.Content["application/json"]; ok && jsonContent != nil && jsonContent.Schema != nil && jsonContent.Schema.Value != nil {
				resp.Schema = parseSchema("", jsonContent.Schema.Value)
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
		// Prefer sub-resource naming over the generic "download" when a path
		// ends in a non-param segment (e.g. /{id}/der → "der"). This avoids
		// collapsing two distinct binary formats (/{id}/der + /{id}/pem) into
		// a single "download" op, which drops one in dedupe.
		subResourceTail := ""
		if strings.Contains(path, "{") {
			parts := strings.Split(path, "/")
			lastPart := parts[len(parts)-1]
			if !strings.HasPrefix(lastPart, "{") {
				for _, p := range parts[:len(parts)-1] {
					if strings.HasPrefix(p, "{") {
						subResourceTail = strcase.ToKebab(lastPart)
						break
					}
				}
			}
		}
		if hasBinaryResponse && subResourceTail == "" {
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

	applyDocumentedStatusResults(operation)

	return operation
}

func parseSchema(name string, schema *openapi3.Schema) *Schema {
	return parseSchemaDepth(name, schema, 0)
}

// maxSchemaDepth caps how far parseSchemaDepth descends into nested object and
// array element schemas.
//
// Object nesting has always terminated on its own: a property whose own
// Properties map is empty ends the walk. Array elements do not have that
// property — a schema may name itself as its own element type (a node with a
// children[] of nodes), and kin-openapi resolves $ref inline, so following
// element schemas without a cap recurses until the stack dies. The cap is
// generous enough that no committed spec reaches it (asserted by
// TestParseSchema_DepthCapUnreachedByLiveSpecs) and exists so that a future one
// degrades to a shallower scaffold rather than crashing generation.
const maxSchemaDepth = 8

// enumValueString renders one spec enum value for the "Allowed values:" line
// generated help carries. Reports false for a value with no useful literal form
// — null, or a composite — so it is dropped rather than printed as Go's default
// formatting of a map or slice.
//
// Non-string values matter: Security Cloud's grouped-gateway recoveryDelayInSec
// is an enum of five integers, required on create, and 0 — the value a caller
// gets by leaving the field at its zero value — is rejected with a 400. A
// string-only walk dropped the whole set, so the help listed nothing and the
// scaffold showed one arbitrary number with no sign the others existed.
// JSON numbers decode to float64, so an integral value is printed without a
// trailing ".0" and a fractional one keeps its digits.
func enumValueString(v any) (string, bool) {
	switch val := v.(type) {
	case string:
		return val, true
	case bool:
		return strconv.FormatBool(val), true
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64), true
	case float32:
		return strconv.FormatFloat(float64(val), 'f', -1, 32), true
	case int:
		return strconv.Itoa(val), true
	case int64:
		return strconv.FormatInt(val, 10), true
	case json.Number:
		return val.String(), true
	default:
		return "", false
	}
}

func parseSchemaDepth(name string, schema *openapi3.Schema, depth int) *Schema {
	s := &Schema{
		Name:       name,
		Type:       "object",
		Properties: make(map[string]*Property),
		Required:   schema.Required,
	}

	if len(schema.Type.Slice()) > 0 {
		s.Type = schema.Type.Slice()[0]
	}

	for _, v := range schema.Enum {
		if str, ok := enumValueString(v); ok {
			s.Enum = append(s.Enum, str)
		}
	}
	// A schema that is itself an array carries its shape in items, not in
	// properties — a bare-array request body has no properties at all.
	if s.Type == "array" && depth < maxSchemaDepth {
		if schema.Items != nil && schema.Items.Value != nil {
			s.Items = parseSchemaDepth(name, schema.Items.Value, depth+1)
		}
	}

	// Collect properties from direct properties and allOf items.
	// allOf is used for schema composition (e.g. PostComputerPrestageV3 =
	// allOf[ComputerPrestageV3, {accountSettings, recoveryLockPassword}]).
	// kin-openapi resolves $ref inline, so allOf item .Value.Properties is populated.
	propSources := []openapi3.Schemas{schema.Properties}
	for _, item := range schema.AllOf {
		if item != nil && item.Value != nil {
			propSources = append(propSources, item.Value.Properties)
			// Recurse into nested allOf (e.g. ComputerPrestageV3 → DeviceEnrollmentPrestageV2)
			for _, nested := range item.Value.AllOf {
				if nested != nil && nested.Value != nil {
					propSources = append(propSources, nested.Value.Properties)
				}
			}
		}
	}

	for _, props := range propSources {
		for propName, propRef := range props {
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
				WriteOnly:   prop.WriteOnly,
			}
			if len(prop.Type.Slice()) > 0 {
				p.Type = prop.Type.Slice()[0]
			}
			for _, v := range prop.Enum {
				if s, ok := enumValueString(v); ok {
					p.Enum = append(p.Enum, s)
				}
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
			if len(prop.Properties) > 0 && depth < maxSchemaDepth {
				p.Nested = parseSchemaDepth(propName, prop, depth+1)
			}
			// Populate Items for array types so a scaffold can show one element.
			// Same cap as Nested, and load-bearing here rather than defensive —
			// see maxSchemaDepth.
			if p.Type == "array" && depth < maxSchemaDepth {
				if prop.Items != nil && prop.Items.Value != nil {
					p.Items = parseSchemaDepth(propName, prop.Items.Value, depth+1)
				}
			}
			s.Properties[propName] = p
		}
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

// renameSingletonRootGet detects the classic singleton/settings pattern
// (non-paginated GET + PUT on the same non-param path, with no path-parameter
// siblings anywhere in the resource) and renames that path's root GET from
// "list" to "get". Must run before resolveNoParamConflicts so the root GET
// is excluded from "list"-collision rename logic.
func renameSingletonRootGet(ops []*Operation) {
	for _, op := range ops {
		if hasPathParam(op.Path) {
			return
		}
	}
	getPaths := map[string]*Operation{}
	putPaths := map[string]bool{}
	for _, op := range ops {
		if op.Method == "GET" && !op.IsList && op.Name == "list" {
			getPaths[op.Path] = op
		}
		if op.Method == "PUT" {
			putPaths[op.Path] = true
		}
	}
	for path, op := range getPaths {
		if putPaths[path] {
			op.Name = "get"
		}
	}
}

// reclassifyMisannotatedCreates finds collection-root POSTs that were tagged
// x-action upstream (and so got their CLI name derived from the path segment)
// but actually belong to a CRUD family. A sibling /{collection}/{id} path in
// the same spec is the "this is a CRUD" signal: true standalone actions like
// /v1/deploy-thing never have one, while CRUD creates like /v1/api-roles do.
// Matching ops are renamed to "create".
func reclassifyMisannotatedCreates(ops []*Operation) {
	siblings := map[string]bool{}
	for _, op := range ops {
		if strings.Contains(op.Path, "/{") {
			siblings[op.Path[:strings.Index(op.Path, "/{")]] = true
		}
	}
	for _, op := range ops {
		if op.Method != "POST" || !op.IsAction {
			continue
		}
		if !isCollectionRootPath(op.Path) {
			continue
		}
		if !siblings[op.Path] {
			continue
		}
		op.Name = "create"
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
	if before, ok := strings.CutSuffix(pathParam, "Id"); ok {
		bare := before
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
	destructive := []string{"delete", "delete-multiple", "erase", "wipe", "remove", "lock", "restart", "shutdown", "unmanage"}
	for _, d := range destructive {
		if strings.Contains(opName, d) {
			return true
		}
	}
	return false
}
