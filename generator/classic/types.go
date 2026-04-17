// Copyright 2026, Jamf Software LLC

package classic

import "slices"

// ClassicResource represents a Classic API resource parsed from the YAML manifest.
type ClassicResource struct {
	Name            string // e.g., "policies"
	Path            string // URL segment under /JSSResource/: "policies"
	CLIName         string // e.g., "classic-policies"
	GoName          string // e.g., "ClassicPolicies"
	Singular        string // JSON root key for a single object: "policy"
	Description     string
	Operations      []string // ["list", "get", "create", "update", "delete"]
	Lookups         []string // ["id", "name", "serialnumber", "macaddress", "udid"]
	HasScope        bool     // true if the resource supports scope operations
	IDPath          string   // path segment between base path and ID value; defaults to "id" (e.g. "groupid" → /accounts/groupid/{id})
	IsConfigProfile bool     // true for macOS and mobile device configuration profile resources
	FileFields      []ClassicFileField
	// ListSubset marks a list operation as sharing the list endpoint with a
	// sibling resource: GET /JSSResource/{path} returns both, and the generated
	// list command extracts only the named sub-element before formatting.
	// Used for /accounts (returns users + groups combined under <accounts>).
	// Empty for normal list endpoints.
	ListSubset string
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
