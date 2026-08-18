// Copyright 2026, Jamf Software LLC

package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/iancoleman/strcase"

	"github.com/Jamf-Concepts/jamf-cli/generator/parser"
)

// tableColumn maps a dot-notation JSON field path to an output label for list
// table ops. Used at generator time to populate the platformTableColumns map;
// the rendered Go code references platform.TableColumn at runtime.
type tableColumn struct {
	Field string
	Label string
}

// platformTableColumns maps canonical platform resource names to preferred
// columns for list table output. Only applies to paginated list operations.
var platformTableColumns = map[string][]tableColumn{
	"blueprints": {
		{Field: "id", Label: "id"},
		{Field: "name", Label: "name"},
		{Field: "deploymentState.state", Label: "state"},
		{Field: "deploymentState.lastDeployment.started", Label: "lastDeployed"},
		{Field: "created", Label: "created"},
		{Field: "updated", Label: "updated"},
	},
	"benchmarks": {
		{Field: "id", Label: "id"},
		{Field: "title", Label: "title"},
		{Field: "description", Label: "description"},
		{Field: "syncState", Label: "syncState"},
		{Field: "updateAvailable", Label: "updateAvailable"},
		{Field: "modified", Label: "modified"},
	},
	"devices": {
		{Field: "id", Label: "id"},
		{Field: "name", Label: "name"},
		{Field: "model", Label: "model"},
		{Field: "serialNumber", Label: "serialNumber"},
		{Field: "operatingSystemVersion", Label: "osVersion"},
		{Field: "enrollmentType", Label: "enrollmentType"},
		{Field: "lastInventoryUpdateTime", Label: "lastInventory"},
	},
	"device-groups": {
		{Field: "id", Label: "id"},
		{Field: "name", Label: "name"},
		{Field: "description", Label: "description"},
		{Field: "deviceType", Label: "deviceType"},
		{Field: "groupType", Label: "groupType"},
		{Field: "memberCount", Label: "memberCount"},
	},
}

// crossResourceNameLookupPath maps a resource name to a list endpoint owned by
// a *different* resource, used for --name → ID resolution when the resource has
// no list op of its own but its single {id} path param refers to a sibling
// resource's ID.
//
// Example: benchmark-reports' ops are GET /benchmarks/{id}/rules etc.; the {id}
// is a benchmark ID (owned by the separate "benchmarks" tag), so --name resolves
// against the benchmarks list. The generic platform.ResolveIDByName matches
// compliance-benchmark titles and reads the "id" field, so no SDK-specific code
// is needed. Paths carry a literal {tenantId} the template substitutes at runtime.
var crossResourceNameLookupPath = map[string]string{
	"benchmark-reports": "/api/compliance-benchmarks/v1/tenant/{tenantId}/benchmarks",
}

// templateOp wraps *parser.Operation with template-friendly fields.
type templateOp struct {
	*parser.Operation
	GoName             string        // PascalCase form of Name, used in Go identifiers
	Short              string        // Short help text for the cobra subcommand
	Long               string        // Long help text — first paragraph of op description, plain text
	Use                string        // cobra Use string, includes <param> placeholders for path params
	PathParams         []string      // path parameter names in order of appearance in Path
	HasBody            bool          // operation accepts a request body — emit --file/--set flags
	IsDestructive      bool          // destructive op — emit --yes flag, ConfirmAction guard, and jamf:destructive annotation
	UsesMergePatch     bool          // PATCH with application/merge-patch+json content type
	SuccessCode        int           // success HTTP status code (200 default; 201 for create, 204 for delete/patch, etc.)
	HasResult          bool          // operation returns a JSON response body to unmarshal/print
	QueryParams        []queryParam  // user-facing query flags (excludes pagination params we manage internally)
	Paginate           bool          // op exposes page+page-size — emit a pagination loop
	ListArrayKey       string        // JSON key holding the result array on a list response (empty if response shouldn't be unwrapped)
	ListTableColumns   []tableColumn // preferred columns for table output on a list op (empty = emit raw)
	Scaffold           string        // pretty-printed JSON template for the request body, surfaced via --scaffold (empty when op has no body)
	SupportsNameLookup bool          // op accepts a single positional ID arg AND its resource has a list op — emit --name as alternative
	ListPath           string        // sibling list-op path (used by --name lookup); only populated when SupportsNameLookup is true
	Service            string        // gateway namespace segment ("blueprints", "securitycloud") — selects which tenant ID the runtime injects
}

// queryParam describes a CLI flag bound to a query string parameter.
type queryParam struct {
	Name        string // spec parameter name (e.g. "baseline-id", "search")
	FlagName    string // cobra flag name (kebab-case, equal to Name)
	Var         string // Go variable name (camelCase, e.g. "baselineId")
	Description string // flag help text
	GoType      string // "string", "bool", "int", "[]string"
	Required    bool   // whether the spec marks the param required
}

// templateResource wraps *parser.Resource for template rendering.
type templateResource struct {
	Name       string
	GoName     string
	Long       string // First paragraph of resource description, plain text
	APILabel   string // product name for help text — which API the resource belongs to
	Operations []templateOp
}

// Generate emits one Go file per resource into outputDir. Returns the list of
// generated file paths.
func Generate(resources []*parser.Resource, outputDir string) ([]string, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating output dir: %w", err)
	}

	tmpl, err := template.New("resource").Funcs(template.FuncMap{
		"statusConstant": statusConstant,
		"methodConstant": methodConstant,
		"opAnnotations": func(op templateOp) string {
			var pairs []string
			if op.IsDestructive {
				pairs = append(pairs, `"jamf:destructive": "true"`)
			}
			if len(op.Privileges) > 0 {
				pairs = append(pairs, fmt.Sprintf("%q: %q", "jamf:privileges", strings.Join(op.Privileges, ",")))
			}
			// Which API serves this command, and so which credentials it needs.
			// Recorded per command rather than inferred from the namespace it is
			// wired under, because `security` mixes two transports and `pro`
			// mixes three.
			pairs = append(pairs, fmt.Sprintf("%q: %q", "jamf:api", "platform-gateway"))
			if len(pairs) == 0 {
				return ""
			}
			return "map[string]string{" + strings.Join(pairs, ", ") + "}"
		},
	}).Parse(resourceTemplate)
	if err != nil {
		return nil, fmt.Errorf("parsing template: %w", err)
	}

	var generated []string
	for _, r := range resources {
		// Filter out ops the template doesn't yet handle (request bodies,
		// DELETE, PATCH). Skip the resource entirely when nothing remains.
		generable := filterGenerableOps(r.Operations)
		if len(generable) == 0 {
			continue
		}
		filtered := *r
		filtered.Operations = generable
		tr := buildTemplateResource(&filtered)
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, tr); err != nil {
			return nil, fmt.Errorf("rendering %s: %w", r.Name, err)
		}

		formatted, err := format.Source(buf.Bytes())
		if err != nil {
			return nil, fmt.Errorf("gofmt %s: %w\n--- raw ---\n%s", r.Name, err, buf.String())
		}

		outPath := filepath.Join(outputDir, r.Name+".go")
		if err := os.WriteFile(outPath, formatted, 0o644); err != nil {
			return nil, fmt.Errorf("writing %s: %w", outPath, err)
		}
		generated = append(generated, outPath)
	}
	return generated, nil
}

// filterGenerableOps returns the subset of ops the current template can emit:
// GET (no body), POST/PUT/PATCH (with or without body), DELETE.
//
// PUT is here because Jamf Security Cloud is the first platform spec to use it —
// the settings-style resources (DNS search domains, custom hostname mappings,
// UEM connector enablement and sync settings) are written with PUT, and device
// groups update with it. Until then every platform mutation was POST or a
// merge-PATCH, so a PUT was silently dropped and the resource shipped read-only:
// `dns-search-domains` had no way to set a search domain at all.
func filterGenerableOps(ops []*parser.Operation) []*parser.Operation {
	out := make([]*parser.Operation, 0, len(ops))
	for _, op := range ops {
		switch op.Method {
		case "GET":
			if op.RequestBody == nil {
				out = append(out, op)
			}
		case "POST", "PUT", "PATCH", "DELETE":
			out = append(out, op)
		}
	}
	return out
}

// extractPathParams returns the names of {placeholders} in p, in the order
// they appear (e.g. "/v1/foo/{id}/bar/{ruleId}" → ["id", "ruleId"]).
func extractPathParams(p string) []string {
	var out []string
	for i := 0; i < len(p); i++ {
		if p[i] != '{' {
			continue
		}
		end := strings.IndexByte(p[i:], '}')
		if end < 0 {
			break
		}
		out = append(out, p[i+1:i+end])
		i += end
	}
	return out
}

func buildTemplateResource(r *parser.Resource) templateResource {
	// Find the resource's list op upfront (if any) so single-ID ops can
	// expose --name as an alternative to the positional arg.
	var listPath string
	for _, op := range r.Operations {
		if op.Name != "list" {
			continue
		}
		userParams := filterTenantPathParams(extractPathParams(tenantPath(op)))
		if len(userParams) == 0 {
			listPath = tenantPath(op)
			break
		}
	}
	// Resources with no list op of their own (e.g. benchmark-reports) resolve
	// --name against a sibling resource's list endpoint when their {id} refers
	// to that sibling's ID.
	if listPath == "" {
		listPath = crossResourceNameLookupPath[r.Name]
	}

	ops := make([]templateOp, 0, len(r.Operations))
	for _, op := range r.Operations {
		// Restore /tenant/{tenantId}/ that the platform parser stripped — the
		// runtime substitutes the tenant ID from client.Transport().TenantID().
		opCopy := *op
		opCopy.Path = tenantPath(op)
		// User-facing params drive cobra's positional args + Use string;
		// {tenantId} is filled from auth context at request time.
		userParams := filterTenantPathParams(extractPathParams(opCopy.Path))
		successCode, hasResult := successStatus(&opCopy)
		supportsName := listPath != "" && len(userParams) == 1
		opListPath := ""
		if supportsName {
			opListPath = listPath
		}
		var listTableCols []tableColumn
		if opCopy.Name == "list" {
			listTableCols = platformTableColumns[r.Name]
		}
		ops = append(ops, templateOp{
			Operation:      &opCopy,
			GoName:         strcase.ToCamel(opCopy.Name),
			Short:          shortFromOp(&opCopy),
			Long:           firstParagraph(opCopy.Description),
			Use:            buildUse(opCopy.Name, userParams),
			PathParams:     userParams,
			HasBody:        opCopy.RequestBody != nil,
			IsDestructive:  opCopy.IsDestructive,
			UsesMergePatch: opCopy.RequestBody != nil && opCopy.RequestBody.IsMergePatch,
			SuccessCode:    successCode,
			HasResult:      hasResult,
			QueryParams:    buildQueryParams(opCopy.Parameters, serviceFromPath(opCopy.Path)),
			Paginate:       hasPaginationParams(opCopy.Parameters),
			ListArrayKey: func() string {
				if opCopy.Name == "list" || opCopy.IsList {
					return detectListArrayKey(&opCopy)
				}
				return ""
			}(),
			ListTableColumns:   listTableCols,
			Scaffold:           buildScaffold(&opCopy),
			SupportsNameLookup: supportsName,
			ListPath:           opListPath,
			Service:            serviceFromPath(opCopy.Path),
		})
	}
	return templateResource{
		Name:       r.Name,
		GoName:     r.GoName,
		APILabel:   apiLabel(ops),
		Long:       firstParagraph(r.Description),
		Operations: ops,
	}
}

// firstParagraph returns the leading paragraph of s, with internal newlines
// folded to spaces. Used to keep cobra Long text concise — the spec
// description often has multi-paragraph markdown that doesn't render well in
// a terminal help dump.
func firstParagraph(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if idx := strings.Index(s, "\n\n"); idx >= 0 {
		s = s[:idx]
	}
	s = strings.ReplaceAll(s, "\n", " ")
	// Collapse runs of whitespace introduced by the join.
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return s
}

// apiLabel names the API a resource is served by, for its help text.
//
// Under `security` this is load-bearing rather than decorative: the namespace
// mixes commands reached through the platform gateway with commands reached
// directly on the Radar API, and the two take different credentials. Cobra uses
// Short as the shell-completion description, so naming the transport here is
// what makes `security <TAB>` reveal which credentials a command needs.
func apiLabel(ops []templateOp) string {
	for _, op := range ops {
		if op.Service == securityCloudService {
			return "Security Cloud · platform gateway"
		}
	}
	return "Platform API"
}

// securityCloudService is the gateway namespace Jamf Security Cloud is served
// under, and the value of the jamf:api annotation's gateway variant.
const securityCloudService = "securitycloud"

// serviceFromPath returns the gateway namespace segment of a full request path
// ("/api/securitycloud/v1/tenant/{tenantId}/dns/zones" → "securitycloud").
// Returns "" for a path that carries no /api/ prefix, which leaves the runtime
// on the client-wide tenant ID — the correct fallback, since a namespace
// override only exists for namespaces that were named explicitly.
func serviceFromPath(path string) string {
	rest, ok := strings.CutPrefix(path, "/api/")
	if !ok {
		return ""
	}
	service, _, _ := strings.Cut(rest, "/")
	return service
}

// tenantPath returns the operation's full request path, including
// /tenant/{tenantId} at the position its spec declares. ParsePlatformSpec
// records this on the operation; restoreTenantSegment covers operations that
// reached the emitter by some other route.
func tenantPath(op *parser.Operation) string {
	if op.TenantPath != "" {
		return op.TenantPath
	}
	return restoreTenantSegment(op.Path)
}

// restoreTenantSegment inserts /tenant/{tenantId} after the /v{n} version
// segment of a platform path (e.g. /api/blueprints/v1/blueprints →
// /api/blueprints/v1/tenant/{tenantId}/blueprints).
//
// This is a fallback for operations carrying no TenantPath. It assumes the
// tenant segment follows the version, which is not true of every gateway
// namespace — uem-connect serves /tenant/{tenantId}/uem-connect/v1/... — so
// prefer TenantPath, which records the spec's actual layout.
func restoreTenantSegment(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if len(p) >= 2 && p[0] == 'v' && p[1] >= '0' && p[1] <= '9' {
			out := append([]string{}, parts[:i+1]...)
			out = append(out, "tenant", "{tenantId}")
			out = append(out, parts[i+1:]...)
			return strings.Join(out, "/")
		}
	}
	return path
}

// buildScaffold returns a pretty-printed JSON example for the operation's
// request body, derived from the spec schema. Returns "" when the op has no
// body. Properties recurse into nested schemas; primitives use their
// zero-value, arrays render as []. Generated code surfaces this via the
// --scaffold flag so callers can `--scaffold > body.json && edit && apply`.
func buildScaffold(op *parser.Operation) string {
	if op.RequestBody == nil || op.RequestBody.Schema == nil {
		return ""
	}
	example := schemaExample(op.RequestBody.Schema)
	b, err := json.MarshalIndent(example, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}

// schemaExample walks a parsed schema and emits a JSON-marshallable Go value
// with placeholder zero values for every property.
func schemaExample(s *parser.Schema) any {
	if s == nil {
		return nil
	}
	switch s.Type {
	case "object", "":
		m := map[string]any{}
		for name, prop := range s.Properties {
			m[name] = propertyExample(prop)
		}
		return m
	case "array":
		return []any{}
	case "string":
		return ""
	case "boolean":
		return false
	case "integer", "number":
		return 0
	}
	return nil
}

func propertyExample(p *parser.Property) any {
	if p == nil {
		return nil
	}
	switch p.Type {
	case "object":
		if p.Nested != nil {
			return schemaExample(p.Nested)
		}
		return map[string]any{}
	case "array":
		return []any{}
	case "string":
		return ""
	case "boolean":
		return false
	case "integer", "number":
		return 0
	}
	return nil
}

// hasPaginationParams reports whether the op exposes page + page-size query
// parameters. When true, the generated command runs a pagination loop and
// hides the page params from the user-facing flag set.
func hasPaginationParams(params []*parser.Parameter) bool {
	var hasPage, hasPageSize bool
	for _, p := range params {
		if p == nil || p.In != "query" {
			continue
		}
		switch p.Name {
		case "page":
			hasPage = true
		case "page-size":
			hasPageSize = true
		}
	}
	return hasPage && hasPageSize
}

// detectListArrayKey returns the response property name holding the list
// array, when the operation's success response is a JSON object with exactly
// one array property. Returns "" when the shape doesn't match — generated
// code then emits the response unmodified.
func detectListArrayKey(op *parser.Operation) string {
	resp := op.Responses[strconv.Itoa(http.StatusOK)]
	if resp == nil || resp.Schema == nil || resp.Schema.Type != "object" {
		return ""
	}
	var key string
	count := 0
	for name, prop := range resp.Schema.Properties {
		if prop != nil && prop.Type == "array" {
			key = name
			count++
		}
	}
	if count == 1 {
		return key
	}
	return ""
}

// platformIgnoredRequiredParams lists query parameters a spec marks required
// that the server in fact ignores, keyed "{service}/{param}".
//
// Only wire probing reveals these, so an entry needs a recorded observation
// behind it. Security Cloud's device-groups declares customer-id required, but
// the tenant in the URL path decides the customer and the parameter changes
// nothing — enforcing it would make the CLI demand a value with no effect and
// no obvious source. See the Security Cloud section of the SDK's CLAUDE.md,
// which records the same finding for the generated Go client.
//
// This duplicates knowledge the SDK holds, which is normally the thing to
// avoid; it lives here only because the SDK's generator config has no way to
// express "declared required, actually ignored" for it to publish. Move it into
// the published spec once it can.
var platformIgnoredRequiredParams = map[string]bool{
	"securitycloud/customer-id": true,
}

// buildQueryParams returns the user-facing query flags for an operation.
// page/page-size are filtered out — pagination is handled by the runtime
// loop, not exposed as flags. tenantId would never be query (it's path) so
// no special-case is needed here.
func buildQueryParams(params []*parser.Parameter, service string) []queryParam {
	var out []queryParam
	for _, p := range params {
		if p == nil || p.In != "query" {
			continue
		}
		switch p.Name {
		case "page", "page-size":
			continue // managed by pagination
		}
		goType := "string"
		switch p.Type {
		case "boolean":
			goType = "bool"
		case "integer":
			goType = "int"
		case "array":
			goType = "[]string"
		}
		desc := p.Description
		if desc == "" {
			desc = "Filter by " + strcase.ToKebab(p.Name)
		}
		out = append(out, queryParam{
			Name:        p.Name,
			FlagName:    strcase.ToKebab(p.Name),
			Var:         strcase.ToLowerCamel(strings.ReplaceAll(p.Name, "-", "_")),
			Description: desc,
			GoType:      goType,
			Required:    p.Required && !platformIgnoredRequiredParams[service+"/"+p.Name],
		})
	}
	return out
}

// filterTenantPathParams drops the "tenantId" placeholder from the list of
// CLI-facing positional params; the runtime fills it from auth, not args.
func filterTenantPathParams(params []string) []string {
	out := params[:0]
	for _, p := range params {
		if p == "tenantId" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// successStatus picks the 2xx response code for op (lowest among declared 2xx,
// default http.StatusOK) and reports whether that response carries a body to
// unmarshal. http.StatusNoContent is treated as no body regardless of any
// schema.
func successStatus(op *parser.Operation) (code int, hasResult bool) {
	// A published-spec override wins: it records what the server was observed
	// to answer, against a spec that declares something else.
	if op.ExpectedStatus != 0 {
		resp := op.Responses[strconv.Itoa(op.ExpectedStatus)]
		if resp == nil {
			// The declared response is under the wrong code, so take the body
			// shape from whichever 2xx the spec does describe.
			for status, r := range op.Responses {
				n, err := strconv.Atoi(status)
				if err != nil || n < 200 || n >= 300 {
					continue
				}
				if r != nil && r.Schema != nil {
					resp = r
					break
				}
			}
		}
		return op.ExpectedStatus, op.ExpectedStatus != http.StatusNoContent && resp != nil && resp.Schema != nil
	}
	code = http.StatusOK
	for status := range op.Responses {
		n, err := strconv.Atoi(status)
		if err != nil || n < 200 || n >= 300 {
			continue
		}
		if code == http.StatusOK || n < code {
			code = n
		}
	}
	if code == http.StatusNoContent {
		return code, false
	}
	resp := op.Responses[strconv.Itoa(code)]
	hasResult = resp != nil && resp.Schema != nil
	return code, hasResult
}

// statusConstant returns the net/http constant name for a status code (e.g.
// 200 → "http.StatusOK", 204 → "http.StatusNoContent"). Falls back to the
// numeric literal when no well-known constant matches — generated code with
// a literal still compiles, just less readable.
func statusConstant(code int) string {
	switch code {
	case http.StatusOK:
		return "http.StatusOK"
	case http.StatusCreated:
		return "http.StatusCreated"
	case http.StatusAccepted:
		return "http.StatusAccepted"
	case http.StatusNoContent:
		return "http.StatusNoContent"
	case http.StatusPartialContent:
		return "http.StatusPartialContent"
	default:
		return strconv.Itoa(code)
	}
}

// methodConstant returns the net/http constant name for an HTTP method (e.g.
// "GET" → "http.MethodGet"). Falls back to the quoted literal for unknown
// verbs.
func methodConstant(method string) string {
	switch method {
	case http.MethodGet:
		return "http.MethodGet"
	case http.MethodPost:
		return "http.MethodPost"
	case http.MethodPut:
		return "http.MethodPut"
	case http.MethodPatch:
		return "http.MethodPatch"
	case http.MethodDelete:
		return "http.MethodDelete"
	case http.MethodHead:
		return "http.MethodHead"
	case http.MethodOptions:
		return "http.MethodOptions"
	default:
		return strconv.Quote(method)
	}
}

// buildUse produces a cobra Use string. With no path params the command name
// stands alone ("list"); with params each placeholder appears as <param>
// ("get <blueprintId>", "rules <id> <ruleId>").
func buildUse(name string, params []string) string {
	if len(params) == 0 {
		return name
	}
	parts := make([]string, 0, len(params)+1)
	parts = append(parts, name)
	for _, p := range params {
		parts = append(parts, "<"+p+">")
	}
	return strings.Join(parts, " ")
}

// shortFromOp produces a brief help string for an operation. Falls back to a
// generic phrasing when the spec lacks a summary.
func shortFromOp(op *parser.Operation) string {
	if op.Summary != "" {
		return escapeQuote(op.Summary)
	}
	switch op.Name {
	case "list":
		return "List items"
	case "get":
		return "Get an item"
	case "create":
		return "Create an item"
	case "delete":
		return "Delete an item"
	case "patch":
		return "Patch an item"
	default:
		return op.Name
	}
}

func escapeQuote(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '"' || r == '\\' {
			out = append(out, '\\')
		}
		out = append(out, r)
	}
	return string(out)
}
