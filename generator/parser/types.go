// Copyright 2026, Jamf Software LLC

package parser

// LookupField represents an alternate identifier that can be used to resolve a
// resource ID instead of the primary name field (e.g. serial number for computers).
type LookupField struct {
	Flag      string // CLI flag name (e.g. "serial")
	RSQLField string // RSQL filter field path (e.g. "hardware.serialNumber")
	Desc      string // Flag description shown in --help
}

// TableColumn defines a preferred column for list table output.
type TableColumn struct {
	Field string // JSON field path (e.g., "general.name") — may use dot-notation for nested fields
	Label string // Display label (e.g., "name") — used as the column header
}

// Resource represents a parsed API resource (e.g., buildings, computers)
type Resource struct {
	Name              string // e.g., "buildings"
	NameSingular      string // e.g., "building"
	GoName            string // e.g., "Buildings"
	Description       string
	Operations        []*Operation
	Schemas           map[string]*Schema
	NameField         string        // Filter field for name lookups (default "name", some use "displayName")
	IDField           string        // Response field for ID extraction in name resolution (default "id", some use "templateId", "groupId", etc.)
	IsSingleton       bool          // True for settings-style resources: single object, GET+PUT, no {id} in any path
	LookupFields      []LookupField // Alternate identifier fields for patch-by-name / delete-by-name (e.g. serial number)
	NameLookupPath    string        // Override list path for name resolution (when the standard list endpoint ignores RSQL)
	NameLookupIDField string        // Override ID field extracted from NameLookupPath response (when it differs from IDField)
	HasVersionLock    bool          // True when PUT/POST request body includes versionLock (optimistic locking for prestages)
	TableColumns      []TableColumn // Preferred columns for list table output (when set, overrides generic column selection)
	DefaultSections   []string      // Default --section values for list (when set, fetches these sections for table output)
	GetDetailPath     string        // When set, "get" uses this path by default (returns all sections). If the get op has a section param, --section overrides back to the original path.
}

// Operation represents an API operation (endpoint)
type Operation struct {
	Name          string // e.g., "list", "get", "create"
	Method        string // HTTP method
	Path          string // API path
	Summary       string
	Description   string
	Parameters    []*Parameter
	RequestBody   *RequestBody
	Responses     map[string]*Response
	IsAction      bool     // x-action: true
	IsDestructive bool     // Requires confirmation (delete, erase, etc.)
	IsList        bool     // List operation with pagination support
	APIVersion    string   // v1, v2, preview, etc.
	Privileges    []string // x-required-privileges
}

// Parameter represents a query/path parameter
type Parameter struct {
	Name        string
	In          string // "query", "path"
	Description string
	Required    bool
	Type        string
	Default     any
	IsArray     bool
}

// RequestBody represents a request body
type RequestBody struct {
	Description  string
	Required     bool
	Schema       *Schema
	IsMultipart  bool   // true when content type is multipart/form-data
	IsMergePatch bool   // true when content type is application/merge-patch+json
	FileField    string // schema property that holds the binary file (e.g. "file")
}

// Response represents an API response
type Response struct {
	StatusCode  string
	Description string
	Schema      *Schema
	IsBinary    bool // true for image/* content types, text/csv, or format:binary schemas
}

// Schema represents a JSON schema
type Schema struct {
	Name       string
	Type       string
	Properties map[string]*Property
	Required   []string
}

// Property represents a schema property
type Property struct {
	Name        string
	Type        string
	Description string
	Example     any
	Nullable    bool
	ReadOnly    bool
	SchemaRef   string  // name of the referenced component schema for object/array types (e.g. "ComputerGeneralUpdate")
	Nested      *Schema // resolved nested schema for object types (may be nil)
}
