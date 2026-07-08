// Copyright 2026, Jamf Software LLC

package parser

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/iancoleman/strcase"
)

// securityOpSpec hand-maps one (method, path) operation from a Jamf Security
// Cloud spec to its CLI resource + operation name and behavior flags.
//
// Unlike Pro and Platform, Security's three specs total only twelve
// operations spanning wildly different shapes — a paginated list, a
// singleton-style get/update/delete, and several bulk actions with no {id}
// in their path at all — too few and too irregular to benefit from the
// generic tag/path-family auto-detectors those generators use. Each
// operation is named explicitly here instead, the same way the Classic API
// generator is driven by a hand-authored manifest rather than auto-inference.
type securityOpSpec struct {
	method        string
	path          string // original path, including any {customerId} placeholder
	resource      string // CLI resource name, e.g. "risk", "device-lifecycle"
	opName        string // e.g. "list", "override", "get", "update", "delete", "trigger", "purge"
	summary       string // cobra Short text; the specs' own descriptions run several sentences
	isDestructive bool
	isList        bool
}

// securityOpsByFile maps each spec filename to its hand-authored operation
// list. Operations not listed (the deprecated /risk/v1/devices and
// /mobile-risk/v1/devices — superseded by /risk/v2/devices — and /v1/login,
// which internal/security.Client's login exchange handles internally and
// never surfaces as a command) are silently skipped.
var securityOpsByFile = map[string][]securityOpSpec{
	"jamf-risk-api.json": {
		{method: "GET", path: "/risk/v2/devices", resource: "risk", opName: "list", summary: "List device risk status", isList: true},
		{method: "PUT", path: "/risk/v1/override", resource: "risk", opName: "override", summary: "Override calculated device risk"},
	},
	"jamf-device-lifecycle-api.json": {
		{method: "POST", path: "/device-lifecycle/v1/{customerId}/devices/purge/async/external", resource: "device-lifecycle", opName: "purge", summary: "Purge devices from Jamf Security Cloud", isDestructive: true},
	},
	"shared-signals-events-configuration-and-management-api.json": {
		{method: "GET", path: "/sse/.well-known", resource: "well-known", opName: "get", summary: "Get the SSE transmitter discovery document"},
		{method: "GET", path: "/sse/v1/jwks.json", resource: "jwks", opName: "get", summary: "Get the SSE transmitter's JSON Web Key Set"},
		{method: "GET", path: "/sse/v1/stream", resource: "stream", opName: "get", summary: "Get the current event stream configuration"},
		{method: "POST", path: "/sse/v1/stream", resource: "stream", opName: "update", summary: "Create or replace the event stream configuration"},
		{method: "DELETE", path: "/sse/v1/stream", resource: "stream", opName: "delete", summary: "Delete the event stream configuration", isDestructive: true},
		{method: "GET", path: "/sse/v1/status", resource: "status", opName: "get", summary: "Get the current stream status"},
		{method: "POST", path: "/sse/v1/status", resource: "status", opName: "update", summary: "Update the stream status"},
		{method: "POST", path: "/sse/v1/verification", resource: "verification", opName: "trigger", summary: "Trigger SSE stream verification"},
	},
}

// SecurityScopeForFile returns the internal/security.Client scope name
// ("Risk", "Lifecycle", or "SSE") that owns every resource parsed out of the
// given spec filename — used by generator/security to pick which
// DoExpect{Scope} method generated commands call. Empty when the file isn't
// a recognized Security Cloud spec.
func SecurityScopeForFile(specPath string) string {
	switch filepath.Base(specPath) {
	case "jamf-risk-api.json":
		return "Risk"
	case "jamf-device-lifecycle-api.json":
		return "Lifecycle"
	case "shared-signals-events-configuration-and-management-api.json":
		return "SSE"
	default:
		return ""
	}
}

// ParseSecuritySpec parses one Jamf Security Cloud OpenAPI spec (Risk,
// Device Lifecycle, or Shared Signals & Events) into Resources, using
// securityOpsByFile's hand-authored operation list. Strips the manually
// declared "authorization" header parameter every operation carries (the
// runtime injects the scoped bearer token itself, via
// internal/security.Client) and, for Device Lifecycle, the {customerId}
// path parameter (backfilled at request time from the login JWT, the same
// way the Platform parser backfills {tenantId}).
func ParseSecuritySpec(specPath string) ([]*Resource, error) {
	specs, ok := securityOpsByFile[filepath.Base(specPath)]
	if !ok || len(specs) == 0 {
		return nil, nil
	}

	raw, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("reading security spec: %w", err)
	}
	var rawDoc map[string]any
	if err := json.Unmarshal(raw, &rawDoc); err != nil {
		return nil, fmt.Errorf("decoding security spec: %w", err)
	}
	stripSecurityAuthHeader(rawDoc)
	stripCustomerIDPathParam(rawDoc)

	tmpPath, err := writeNormalisedTempSpec(specPath, rawDoc)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(filepath.Dir(tmpPath)) }()

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	doc, err := loader.LoadFromFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("loading security spec: %w", err)
	}

	schemas := make(map[string]*Schema)
	if doc.Components != nil {
		for name, schemaRef := range doc.Components.Schemas {
			if schemaRef != nil && schemaRef.Value != nil {
				schemas[name] = parseSchema(name, schemaRef.Value)
			}
		}
	}
	description := strings.TrimSpace(doc.Info.Description)

	pathsMap := doc.Paths.Map()
	byResource := make(map[string]*Resource)
	var order []string

	for _, spec := range specs {
		lookupPath := strings.Replace(spec.path, "/{customerId}", "", 1)
		pathItem := pathsMap[lookupPath]
		if pathItem == nil {
			return nil, fmt.Errorf("security spec %s: path %q not found after normalisation", filepath.Base(specPath), lookupPath)
		}
		rawOp := pathItem.Operations()[spec.method]
		if rawOp == nil {
			return nil, fmt.Errorf("security spec %s: %s %q not found", filepath.Base(specPath), spec.method, lookupPath)
		}

		op := parseOperation(lookupPath, spec.method, rawOp)
		// parseOperation infers Name/IsList/IsDestructive from path shape —
		// tuned for Pro/Platform conventions that don't fit these irregular
		// endpoints, so overwrite with our hand-authored classification.
		op.Name = spec.opName
		op.Summary = spec.summary
		op.IsDestructive = spec.isDestructive
		op.IsList = spec.isList
		op.IsAction = !spec.isList
		// Restore the original path (with the {customerId} placeholder
		// intact) for Device Lifecycle — the runtime substitutes it at
		// request time the same way Platform substitutes {tenantId}.
		op.Path = spec.path

		r, ok := byResource[spec.resource]
		if !ok {
			r = &Resource{
				Name:         spec.resource,
				NameSingular: spec.resource,
				GoName:       strcase.ToCamel(spec.resource),
				Description:  description,
				Schemas:      schemas,
			}
			byResource[spec.resource] = r
			order = append(order, spec.resource)
		}
		r.Operations = append(r.Operations, op)
	}

	resources := make([]*Resource, 0, len(order))
	for _, name := range order {
		resources = append(resources, byResource[name])
	}
	return resources, nil
}

// stripSecurityAuthHeader removes the manually-declared "authorization"
// header parameter from every operation in doc. Mutates doc in place.
func stripSecurityAuthHeader(doc map[string]any) {
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		return
	}
	for _, item := range paths {
		pi, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, method := range []string{"get", "post", "put", "patch", "delete", "head", "options"} {
			op, ok := pi[method].(map[string]any)
			if !ok {
				continue
			}
			if params, ok := op["parameters"].([]any); ok {
				op["parameters"] = filterParamByInAndName(params, "header", "authorization")
			}
		}
	}
}

// stripCustomerIDPathParam rewrites paths containing "/{customerId}" to drop
// the segment and removes the customerId path parameter from every
// operation. Mutates doc in place.
func stripCustomerIDPathParam(doc map[string]any) {
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		return
	}
	rewritten := make(map[string]any, len(paths))
	for path, item := range paths {
		newPath := strings.Replace(path, "/{customerId}", "", 1)
		if pi, ok := item.(map[string]any); ok {
			for _, method := range []string{"get", "post", "put", "patch", "delete", "head", "options"} {
				op, ok := pi[method].(map[string]any)
				if !ok {
					continue
				}
				if params, ok := op["parameters"].([]any); ok {
					op["parameters"] = filterParamByInAndName(params, "path", "customerId")
				}
			}
			if params, ok := pi["parameters"].([]any); ok {
				pi["parameters"] = filterParamByInAndName(params, "path", "customerId")
			}
		}
		rewritten[newPath] = item
	}
	doc["paths"] = rewritten
}

// filterParamByInAndName drops parameters matching both "in" and "name"
// (case-insensitive on name) from a raw OpenAPI parameters array.
func filterParamByInAndName(params []any, in, name string) []any {
	out := params[:0]
	for _, p := range params {
		m, ok := p.(map[string]any)
		if !ok {
			out = append(out, p)
			continue
		}
		pIn, _ := m["in"].(string)
		pName, _ := m["name"].(string)
		if pIn == in && strings.EqualFold(pName, name) {
			continue
		}
		out = append(out, p)
	}
	return out
}
