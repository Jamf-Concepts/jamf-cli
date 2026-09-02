// Copyright 2026, Jamf Software LLC

package classic

import (
	"slices"

	"github.com/Jamf-Concepts/jamf-cli/generator/parser"
)

// ClassicResource represents a Classic API resource parsed from the YAML manifest.
type ClassicResource struct {
	Name             string // e.g., "policies"
	Path             string // URL segment under /JSSResource/: "policies"
	CLIName          string // e.g., "classic-policies"
	GoName           string // e.g., "ClassicPolicies"
	Singular         string // JSON root key for a single object: "policy"
	Description      string
	Operations       []string // ["list", "get", "create", "update", "delete"]
	Lookups          []string // ["id", "name", "serialnumber", "macaddress", "udid"]
	HasScope         bool     // true if the resource supports scope operations
	IDPath           string   // path segment between base path and ID value; defaults to "id" (e.g. "groupid" → /accounts/groupid/{id})
	IsConfigProfile  bool     // true for macOS and mobile device configuration profile resources
	HasCustomPayload bool     // true only for osxconfigurationprofiles (supports --custom-payload-file)
	FileFields       []ClassicFileField
	// ListSubset marks a list operation as sharing the list endpoint with a
	// sibling resource: GET /JSSResource/{path} returns both, and the generated
	// list command extracts only the named sub-element before formatting.
	// Used for /accounts (returns users + groups combined under <accounts>).
	// Empty for normal list endpoints.
	ListSubset string
	// GroupPath is the Classic API path (without /JSSResource/) for the group
	// list/detail endpoints, e.g. "mobiledevicegroups". When set, delete gets
	// a --group flag that resolves members and deletes them in bulk.
	GroupPath string
	// Subsets is the curated list of server-side subset section names a get
	// command exposes via --subset (e.g. General, Commands for computerhistory).
	// Drives shell completion only; values are passed through to the API
	// verbatim, so unknown values still work. Empty when the resource declares
	// no subsets.
	Subsets []string
	// GatewayLevel and GatewayDetail record whether the Jamf Platform gateway
	// exposes this resource at all, from specs/gateway/coverage.json.
	// Resource-level because a Classic resource's paths are built at runtime
	// from Path plus the lookup in play, so there is no fixed set of paths to
	// enumerate. Empty when the gateway serves it or when no manifest was
	// available.
	GatewayLevel  string
	GatewayBasis  string
	GatewayDetail string
	// GatewayMethods narrows that verdict to the HTTP method a subcommand
	// sends, keyed by method. A resource can be served and still have a dead
	// subcommand: Classic API 11.28.0 withdrew every read on patchsoftwaretitles
	// while keeping POST /patchsoftwaretitles/id/{}, so the resource is carried
	// and `list`, `get`, `update` and `delete` are all refused. The method is
	// fixed at generate time even though the path is not, and a method the
	// gateway declares nowhere beneath the resource cannot work under any
	// lookup. Absent key or empty Level means served.
	GatewayMethods map[string]GatewayVerdict
	// GatewayList is the verdict for the collection GET — /JSSResource/{Path}
	// with no lookup, the one Classic path that IS fixed at generate time. It is
	// separate from GatewayMethods["GET"] because a resource can keep GET on its
	// {id} paths and lose it on the collection, which is what 11.28.0 did to
	// patchpolicies: `get` works, `list` does not.
	GatewayList GatewayVerdict
	// GatewayPrivileges are the Jamf Account capability permissions the gateway
	// requires, keyed by HTTP method — per method rather than per resource
	// because that is the granularity that differs (accounts:read for a GET,
	// accounts:update for a PUT), even though the served/unserved verdict above
	// is resource-wide. A different vocabulary from Jamf Pro's own privilege
	// names, and Classic commands carry none of those.
	GatewayPrivileges map[string][]string
	// BodySchema is the request-body shape for create/update/apply, parsed from
	// the committed specs/classic/schemas.json artifact. Nil when the artifact
	// is absent or names no schema for this resource — six of the manifest's
	// resources have none, four of them withdrawn from the Classic API
	// altogether — in which case the resource ships without --scaffold, --set or
	// field help, exactly as every Classic resource did before the artifact
	// existed.
	//
	// The Classic manifest is hand-written and carries no field information, so
	// this is the only route by which a Classic write command can say what goes
	// in its body.
	BodySchema *parser.Schema
	// BodyRoot is the XML root element a request body must be wrapped in, e.g.
	// "policy". Read off the spec (a schema's xml.name, else its component key)
	// rather than reused from Singular, so a disagreement between the two is
	// reported at derivation time instead of silently picking one.
	BodyRoot string
	// BodySchemaName is the component schema key BodySchema was parsed from,
	// recorded so generated help can name its provenance.
	BodySchemaName string
}

// GatewayVerdict is one gateway-coverage verdict in the three string values
// the template renders as annotations. Strings rather than generator/gateway's
// own types so this package needs no dependency on it — generator/main.go
// converts at the one point the two meet.
type GatewayVerdict struct {
	Level  string
	Basis  string
	Detail string
}

// ClassicFileField declares a resource field whose value is sourced from a
// local file via a dedicated CLI flag on create/update/apply. The file contents
// are injected as children of a named XML parent before the body is sent.
// Encoding controls how the file contents are embedded: "xml-cdata" wraps them
// in a CDATA section (for mobileconfig), "raw" inserts the XML subtree as-is
// (for AppConfig plists).
type ClassicFileField struct {
	Flag                       string // CLI flag name, e.g. "mobileconfig-file"
	XMLPath                    string // Slash-delimited path under the resource root, e.g. "general/payloads"
	Encoding                   string // "xml-cdata" | "raw"
	Desc                       string // Flag description shown in --help
	NameFallback               string // "none" | "keep-ext" | "strip-ext"
	PreservePayloadIdentifiers bool   // If true, fetch existing payloads on update and call profileconvert.InjectIdentifiers
	FetchMergePut              bool   // If true, apply must fetch existing record, overlay only the file field, and PUT
}

// HasOperation returns true if the resource supports the given operation.
func (r *ClassicResource) HasOperation(op string) bool {
	return slices.Contains(r.Operations, op)
}

// HasLookup returns true if the resource supports the given lookup type.
func (r *ClassicResource) HasLookup(lookup string) bool {
	return slices.Contains(r.Lookups, lookup)
}

// ExtraLookups returns lookups beyond "id" (e.g., name, serialnumber, macaddress, udid).
func (r *ClassicResource) ExtraLookups() []string {
	var extra []string
	for _, l := range r.Lookups {
		if l != "id" {
			extra = append(extra, l)
		}
	}
	return extra
}
