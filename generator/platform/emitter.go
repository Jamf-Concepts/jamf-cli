// Copyright 2026, Jamf Software LLC

package platform

import (
	"bytes"
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

// filterGenerableOps returns the subset of ops the current template can emit.
// Currently: GET (no body), POST/PATCH (with or without body), DELETE.
// Skipped: anything pagination-aware (handled separately later).
func filterGenerableOps(ops []*parser.Operation) []*parser.Operation {
	out := make([]*parser.Operation, 0, len(ops))
	for _, op := range ops {
		switch op.Method {
		case "GET":
			if op.RequestBody == nil {
				out = append(out, op)
			}
		case "POST", "PATCH", "DELETE":
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
		userParams := filterTenantPathParams(extractPathParams(restoreTenantSegment(op.Path)))
		if len(userParams) == 0 {
			listPath = restoreTenantSegment(op.Path)
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
		opCopy.Path = restoreTenantSegment(op.Path)
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
			QueryParams:    buildQueryParams(opCopy.Parameters),
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
		})
	}
	return templateResource{
		Name:       r.Name,
		GoName:     r.GoName,
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

// restoreTenantSegment inserts /tenant/{tenantId} after the /v{n} version
// segment of a platform path (e.g. /api/blueprints/v1/blueprints →
// /api/blueprints/v1/tenant/{tenantId}/blueprints). The platform parser
// strips this prefix so path-family detection works; emitted paths need it
// back, and the runtime substitutes {tenantId} from auth context before
// dispatching the request.
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
// body, which is the signal the template uses to omit --scaffold entirely.
//
// The rendering itself is parser.ScaffoldJSON, shared with the Jamf Pro and
// Security Cloud generators. It used to be a local copy — byte-identical between
// this file and generator/security/emitter.go, and subtly different from Pro's —
// so the same schema could scaffold three ways depending on which API served it.
func buildScaffold(op *parser.Operation) string {
	if op.RequestBody == nil || op.RequestBody.Schema == nil {
		return ""
	}
	return parser.ScaffoldJSON(op.RequestBody.Schema)
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

// buildQueryParams returns the user-facing query flags for an operation.
// page/page-size are filtered out — pagination is handled by the runtime
// loop, not exposed as flags. tenantId would never be query (it's path) so
// no special-case is needed here.
func buildQueryParams(params []*parser.Parameter) []queryParam {
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
			Required:    p.Required,
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
