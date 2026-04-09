// Copyright 2026, Jamf Software LLC

package parser

// Resource represents a parsed API resource (e.g., buildings, computers)
type Resource struct {
	Name         string // e.g., "buildings"
	NameSingular string // e.g., "building"
	GoName       string // e.g., "Buildings"
	Description  string
	Operations   []*Operation
	Schemas      map[string]*Schema
	NameField    string // Filter field for name lookups (default "name", some use "displayName")
	IDField      string // Response field for ID extraction in name resolution (default "id", some use "templateId", "groupId", etc.)
	IsSingleton  bool   // True for settings-style resources: single object, GET+PUT, no {id} in any path
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
	Description string
	Required    bool
	Schema      *Schema
	IsMultipart bool   // true when content type is multipart/form-data
	FileField   string // schema property that holds the binary file (e.g. "file")
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
}
