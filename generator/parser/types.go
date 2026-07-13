// Copyright 2026, Jamf Software LLC

package parser

// LookupField represents an alternate identifier that can be used to resolve a
// resource ID instead of the primary name field (e.g. serial number for computers).
type LookupField struct {
	Flag      string // CLI flag name (e.g. "serial")
	RSQLField string // RSQL filter field path (e.g. "hardware.serialNumber")
	Desc      string // Flag description shown in --help
	Section   string // Optional inventory section to request so the RSQLField is present in the response (e.g. "HARDWARE"); empty when the field is in the default section.
}

// FileField declares a resource field whose value is sourced from a local file
// via a dedicated CLI flag on create/update/apply/patch. The file contents are
// injected into the request body pre-marshal, overwriting any value the caller
// may have supplied in the body. Encoding, companion-field population, and
// name fallback are all driven per entry.
type FileField struct {
	Flag              string // CLI flag name, e.g. "script-file"
	Field             string // Request-body property that receives the file contents, e.g. "scriptContents"
	Encoding          string // "raw" (string) or "base64"
	Desc              string // Flag description shown in --help
	CompanionField    string // Optional: body property auto-populated with filepath.Base(path) when absent (e.g. "tokenFileName" for DEP)
	NameFallback      string // "none" | "keep-ext" | "strip-ext" — when the body lacks a name, derive one from the filename
	NameFlag          bool   // When true, emit a --name flag on create/apply/upload-style ops that sets the body's name field (for tokens whose filename makes a poor record name).
	RenameAfterUpload bool   // When true, and --name is supplied on an upload-style op whose request schema rejects a name field (e.g. DEP /upload-token → DeviceEnrollmentTokenDto has only encodedToken + tokenFileName), the generator emits a follow-up GET+PUT on the standard update path to apply the name.
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
	GroupsClassicPath string        // When set, delete gets --group resolved via Classic API group list (e.g. "computergroups")
	FileFields        []FileField   // File-sourced request-body fields (attached via --script-file, --token-file, etc.)
	TableColumns      []TableColumn // Preferred columns for list table output (when set, overrides generic column selection)
	DefaultSections   []string      // Default --section values for list (when set, fetches these sections for table output)
	GetDetailPath     string        // When set, "get" uses this path by default (returns all sections). If the get op has a section param, --section overrides back to the original path.
	UpdateTokenOp     *Operation    // Optional: auxiliary PUT endpoint for file-field payloads (e.g. PUT /{id}/upload-token). When set, update/apply route the file-field flag to this endpoint instead of the main update body, and no standalone subcommand is emitted for it.
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
	IsPaginated   bool     // Any GET with pagination params (broader than IsList); gates --all/--limit auto-pagination
	APIVersion    string   // v1, v2, preview, etc.
	Privileges    []string // x-required-privileges
	// FallbackPaths holds lower-version base paths for GET/DELETE ops where the
	// same endpoint exists at multiple API versions. Listed in descending version
	// order so the runtime tries the newest fallback first.
	FallbackPaths []string
	// BulkActionPath is set on a per-{id} x-action when the spec also declares a
	// sibling collection-level action of the same name (e.g. the per-deployment
	// installation-retry and the no-{id} bulk installation-retry). It holds the
	// bulk endpoint's path; the generator surfaces it as an --all flag that hits
	// the collection-level endpoint in a single call instead of the {id} one.
	BulkActionPath string
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
	WriteOnly   bool    // true when the field is accepted in requests but never returned in responses (e.g. passwords, secrets)
	SchemaRef   string  // name of the referenced component schema for object/array types (e.g. "ComputerGeneralUpdate")
	Nested      *Schema // resolved nested schema for object types (may be nil)
}
