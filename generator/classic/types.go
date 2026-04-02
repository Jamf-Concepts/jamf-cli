package classic

import "slices"

// ClassicResource represents a Classic API resource parsed from the YAML manifest.
type ClassicResource struct {
	Name        string // e.g., "policies"
	Path        string // URL segment under /JSSResource/: "policies"
	CLIName     string // e.g., "classic-policies"
	GoName      string // e.g., "ClassicPolicies"
	Singular    string // JSON root key for a single object: "policy"
	Description string
	Operations  []string // ["list", "get", "create", "update", "delete"]
	Lookups     []string // ["id", "name", "serialnumber", "macaddress", "udid"]
	HasScope    bool     // true if the resource supports scope operations
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
