// Copyright 2026, Jamf Software LLC

package platform

import (
	"bytes"
	"fmt"
	"go/format"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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

// platformTableColumns maps a platform resource to its preferred columns for
// list table output. Only applies to paginated list operations.
//
// Keyed "{namespace}/{name}" — the gateway namespace, in full, plus the resource
// name — for the same reason platformResourceNameOverrides is: a bare resource
// name is not unique across services. "In full" matters: the namespace can be
// several segments ("ai/governance/policies", "securitycloud/uem-connect"), and
// keying on the first alone both collides and, since a key that matches nothing
// silently emits no columns, does so without a word of complaint. See
// namespaceFromPath. Two specs produce a "device-groups": the Pro device
// group inventory and Security Cloud's device groups. Keyed on the bare name,
// the columns below landed on whichever of the two kept that name after
// applyResourceNameOverride ran, which was Security Cloud's — so the Pro
// inventory the columns actually describe rendered without them, while Security
// Cloud groups, which carry only id and name, printed four permanently empty
// columns.
var platformTableColumns = map[string][]tableColumn{
	// AI Governance. schemaDrift and hasDraft are the two fields an operator
	// actually scans a list for — the first says the stored settings were
	// authored against an older tool schema, the second that unpublished
	// changes are sitting on the policy — and neither is discoverable from a
	// default JSON dump of eleven fields.
	"ai/governance/policies/ai-policies": {
		{Field: "id", Label: "id"},
		{Field: "name", Label: "name"},
		{Field: "toolId", Label: "tool"},
		{Field: "status", Label: "status"},
		{Field: "currentVersionNumber", Label: "version"},
		{Field: "hasDraft", Label: "hasDraft"},
		{Field: "schemaDrift", Label: "schemaDrift"},
		{Field: "updatedAt", Label: "updated"},
	},
	"ai/governance/policies/ai-tools": {
		{Field: "id", Label: "id"},
		{Field: "displayName", Label: "displayName"},
		{Field: "schemaVersion", Label: "schemaVersion"},
	},
	// Jamf Account. Each of these lists is a wide object — a license carries
	// sixteen fields, an SSO connection twenty-four — and the default JSON dump
	// buries the two or three an operator scans for. The activation code is
	// deliberately absent from the licence table: it is the value that entitles
	// an installation, so it belongs in a JSON read someone asked for rather
	// than in the output of a bare `list`.
	"licensing/account-licenses": {
		{Field: "productName", Label: "product"},
		{Field: "sku", Label: "sku"},
		{Field: "licenseType", Label: "type"},
		{Field: "purchasedSeats", Label: "seats"},
		{Field: "startDate", Label: "start"},
		{Field: "endDate", Label: "end"},
	},
	"partners/deal-registrations": {
		{Field: "partnerRegistrationId", Label: "id"},
		{Field: "organizationName", Label: "organization"},
		{Field: "registrationStatus", Label: "status"},
		{Field: "businessType", Label: "businessType"},
		{Field: "expirationDate", Label: "expires"},
	},
	"sso/sso-connections": {
		{Field: "id", Label: "id"},
		{Field: "name", Label: "name"},
		{Field: "type", Label: "type"},
		{Field: "region", Label: "region"},
		{Field: "domains", Label: "domains"},
	},
	"sso/sso-domains": {
		{Field: "id", Label: "id"},
		{Field: "domain", Label: "domain"},
		{Field: "domainStatus", Label: "status"},
		{Field: "sharedDomain", Label: "shared"},
		{Field: "lastVerificationDate", Label: "lastVerified"},
	},
	// Audit events. auditType and auditSource are the pair that says what
	// happened and which service reported it, and actor.displayName answers
	// "who" for a gateway event — a service event carries no actor, so the
	// column renders empty there rather than being wrong.
	"audit/audit": {
		{Field: "time", Label: "time"},
		{Field: "auditType", Label: "type"},
		{Field: "auditSource", Label: "source"},
		{Field: "actor.displayName", Label: "actor"},
		{Field: "resourceId", Label: "resource"},
		{Field: "txId", Label: "txId"},
	},
	"blueprints/blueprints": {
		{Field: "id", Label: "id"},
		{Field: "name", Label: "name"},
		{Field: "deploymentState.state", Label: "state"},
		{Field: "deploymentState.lastDeployment.started", Label: "lastDeployed"},
		{Field: "created", Label: "created"},
		{Field: "updated", Label: "updated"},
	},
	"compliance-benchmarks/benchmarks": {
		{Field: "id", Label: "id"},
		{Field: "title", Label: "title"},
		{Field: "description", Label: "description"},
		{Field: "syncState", Label: "syncState"},
		{Field: "updateAvailable", Label: "updateAvailable"},
		{Field: "modified", Label: "modified"},
	},
	"devices/devices": {
		{Field: "id", Label: "id"},
		{Field: "name", Label: "name"},
		{Field: "model", Label: "model"},
		{Field: "serialNumber", Label: "serialNumber"},
		{Field: "operatingSystemVersion", Label: "osVersion"},
		{Field: "enrollmentType", Label: "enrollmentType"},
		{Field: "lastInventoryUpdateTime", Label: "lastInventory"},
	},
	"device-groups/platform-device-groups": {
		{Field: "id", Label: "id"},
		{Field: "name", Label: "name"},
		{Field: "description", Label: "description"},
		{Field: "deviceType", Label: "deviceType"},
		{Field: "groupType", Label: "groupType"},
		{Field: "memberCount", Label: "memberCount"},
	},
}

// platformNameLookupFields names the property a resource's --name lookup should
// match, for resources whose human-readable identifier is not "name", "title"
// or "displayName" — the three internal/platform.ResolveIDByName tries.
//
// Keyed "{namespace}/{name}" via namespaceFromPath, the same key shape
// platformTableColumns uses. Without an entry the lookup matched nothing and
// reported `not found: no item with name "example.com"`, which reads as a typo:
// an SSO domain's identifier is its "domain" property, and its only other
// handle is an opaque integer ID.
//
// Named per resource rather than appended to the resolver's default list
// because a global "domain" match would let any resource carrying an unrelated
// domain property resolve on it.
var platformNameLookupFields = map[string]string{
	"sso/sso-domains": "domain",
}

// platformNoNameLookup names operations whose --name flag cannot work, so it is
// not emitted. Keyed "{METHOD} {path}", the same shape
// parser.platformOperationNameOverrides uses.
//
// A documented flag that does nothing is worse than an absent one, because the
// operator uses it as documented. --name is emitted whenever an op takes one
// path param and its resource has a list op, which is a structural test and
// says nothing about whether that param is a name-resolvable ID. Two ways it
// is wrong:
//
//   - The collection has no name to match. Audit's holds events, which carry
//     no name-ish property under any spelling, and its list also requires a
//     `since` parameter the lookup does not send. The positionals are a
//     resource ID and a transaction ID — neither is a name.
//   - The positional already *is* the name. sso-domains' allocation op takes a
//     {domain} hostname, so resolving a name to the domain's integer ID and
//     substituting it produced GET /sso/v1/domains/allocation/1552 and a 404
//     "Domain not found" — the flag actively broke a call that works when the
//     domain is passed positionally.
//
// Enrollment's activation profiles are the first case where the *whole read
// model* rules the flag out: ActivationProfile carries a code and nothing else
// — no name under any spelling — so a create's name is not readable back and
// there is nothing for ResolveIDByName to match. Its list would also refuse the
// lookup's request outright, requiring an `origin` parameter the resolver does
// not send.
var platformNoNameLookup = map[string]bool{
	"GET /audit/v1/audit/resources/{resourceId}/lineage": true,
	"GET /audit/v1/audit/transactions/{txId}":            true,
	"GET /sso/v1/domains/allocation/{domain}":            true,

	"GET /securitycloud/v1/activation-profiles/{activationProfileId}":         true,
	"POST /securitycloud/v1/activation-profiles/{activationProfileId}/pause":  true,
	"POST /securitycloud/v1/activation-profiles/{activationProfileId}/resume": true,
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
	"benchmark-reports": "/compliance-benchmarks/v1/benchmarks",
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
	Scaffold           string        // pretty-printed JSON template for the request body, surfaced via --scaffold (empty when the body has no shape to show)
	HasScaffold        bool          // body carries enough shape for --scaffold to be worth offering (parser.HasScaffoldShape)
	SupportsNameLookup bool          // op accepts a single positional ID arg AND its resource has a list op — emit --name as alternative
	NameLookupField    string        // extra property the --name lookup matches, for resources whose name is not name/title/displayName
	ListPath           string        // sibling list-op path (used by --name lookup); only populated when SupportsNameLookup is true
	Service            string        // gateway namespace segment ("blueprints", "securitycloud")

	// DocumentedStatuses lists non-2xx statuses this operation documents as
	// results rather than failures, from platformDocumentedStatusResults. The
	// generated command routes through platform.DoExpectDocumented, which
	// renders their body instead of returning an exit-code error. Empty for all
	// but a handful of singleton reads.
	DocumentedStatuses []documentedStatus
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
		// confirmStmt renders the destructive-action confirmation. A function
		// rather than a literal because the statement is emitted at two points
		// — after the dry-run preview on the single-request path, and before the
		// loop on the paginated one, which has no preview to sit behind — and a
		// second copy of the identifier expression is a second place for the two
		// to drift apart.
		"confirmStmt": confirmStmt,
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
			// The scope levels the published spec says a credential must be
			// created at. A Jamf Platform API integration is created at exactly
			// one of organization, platform environment or tenant, and the
			// credential only works with that level — so this is the one
			// requirement an operator cannot discover from a 403, which names a
			// permission and says nothing about the level.
			//
			// Reported, never enforced. The spec is currently stricter than the
			// gateway: build v2082 moved six Platform specs to
			// environment-only, and a tenant credential still reaches
			// platform-devices and platform-device-groups today. See
			// parser.Operation.ScopeTypes.
			if len(op.ScopeTypes) > 0 {
				pairs = append(pairs, fmt.Sprintf("%q: %q", "jamf:scopes", strings.Join(op.ScopeTypes, ",")))
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
		tr, err := buildTemplateResource(&filtered)
		if err != nil {
			return nil, err
		}
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

func buildTemplateResource(r *parser.Resource) (templateResource, error) {
	// Find the resource's list op upfront (if any) so single-ID ops can
	// expose --name as an alternative to the positional arg.
	var listPath string
	for _, op := range r.Operations {
		if op.Name != "list" {
			continue
		}
		userParams := filterTenantPathParams(extractPathParams(op.Path))
		if len(userParams) == 0 {
			listPath = op.Path
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
		// The path is the request path: the scope travels as an X-Tenant-Id
		// header set by the transport, so nothing is substituted into the URL.
		opCopy := *op
		userParams := filterTenantPathParams(extractPathParams(opCopy.Path))
		successCode, hasResult := successStatus(&opCopy)
		supportsName := listPath != "" && len(userParams) == 1 &&
			!platformNoNameLookup[strings.ToUpper(opCopy.Method)+" "+opCopy.Path]
		opListPath := ""
		if supportsName {
			opListPath = listPath
		}
		var listTableCols []tableColumn
		if opCopy.Name == "list" {
			listTableCols = platformTableColumns[namespaceFromPath(opCopy.Path)+"/"+r.Name]
		}
		scaffold, err := buildScaffold(&opCopy)
		if err != nil {
			return templateResource{}, fmt.Errorf("resource %q op %q: %w", r.Name, opCopy.Name, err)
		}
		ops = append(ops, templateOp{
			Operation:      &opCopy,
			GoName:         strcase.ToCamel(opCopy.Name),
			Short:          shortFromOp(&opCopy),
			Long:           appendEnumChoices(appendVariantNote(firstParagraph(opCopy.Description), &opCopy), buildEnumChoices(&opCopy)),
			Use:            buildUse(opCopy.Name, userParams),
			PathParams:     userParams,
			HasBody:        opCopy.RequestBody != nil,
			IsDestructive:  opCopy.IsDestructive,
			UsesMergePatch: opCopy.RequestBody != nil && opCopy.RequestBody.IsMergePatch,
			SuccessCode:    successCode,
			HasResult:      hasResult,
			QueryParams:    buildQueryParams(opCopy.Parameters, serviceFromPath(opCopy.Path), hasPaginationParams(opCopy.Parameters)),
			Paginate:       hasPaginationParams(opCopy.Parameters),
			ListArrayKey: func() string {
				if opCopy.Name == "list" || opCopy.IsList {
					return detectListArrayKey(&opCopy)
				}
				return ""
			}(),
			ListTableColumns:   listTableCols,
			Scaffold:           scaffold,
			HasScaffold:        scaffold != "",
			SupportsNameLookup: supportsName,
			NameLookupField:    platformNameLookupFields[namespaceFromPath(opCopy.Path)+"/"+r.Name],
			ListPath:           opListPath,
			Service:            serviceFromPath(opCopy.Path),
			DocumentedStatuses: platformDocumentedStatusResults[strings.ToUpper(opCopy.Method)+" "+opCopy.Path],
		})
	}
	return templateResource{
		Name:       r.Name,
		GoName:     r.GoName,
		APILabel:   apiLabel(ops),
		Long:       firstParagraph(r.Description),
		Operations: ops,
	}, nil
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
// ("/securitycloud/v1/dns/zones" → "securitycloud"). The GA gateway mounts each
// namespace at the root, so the namespace is the first path segment.
// Returns "" for a relative or empty path, which leaves the runtime on the
// client-wide tenant ID — the correct fallback, since a namespace override only
// exists for namespaces that were named explicitly.
func serviceFromPath(path string) string {
	rest, ok := strings.CutPrefix(path, "/")
	if !ok {
		return ""
	}
	service, _, _ := strings.Cut(rest, "/")
	return service
}

// namespaceFromPath returns a path's whole gateway namespace — every segment
// before the version ("/ai/governance/policies/v1/policies" →
// "ai/governance/policies", "/securitycloud/uem-connect/v1/connectors" →
// "securitycloud/uem-connect").
//
// Distinct from serviceFromPath, which returns the first segment only. That is
// the right answer for the jamf:api label, where "securitycloud" is the product
// whichever sub-namespace an operation sits in. It is the wrong answer for a
// lookup key: two namespaces sharing a first segment would collide, and — the
// way this was actually found — a three-segment namespace produced the key
// "ai/ai-policies", which matched nothing and silently emitted a list with no
// table columns. A missing key is invisible, hence TestPlatformTableColumnKeys.
func namespaceFromPath(path string) string {
	rest, ok := strings.CutPrefix(path, "/")
	if !ok {
		return ""
	}
	segs := strings.Split(rest, "/")
	for i, seg := range segs {
		if len(seg) >= 2 && seg[0] == 'v' && seg[1] >= '0' && seg[1] <= '9' {
			return strings.Join(segs[:i], "/")
		}
	}
	return segs[0]
}

// enumChoice names one request-body field that is restricted to a fixed set of
// values, by its dotted path from the body root.
type enumChoice struct {
	Path   string
	Values []string
}

// buildEnumChoices lists every enum-constrained field in an operation's request
// body, depth-first and sorted by path.
//
// This is what makes a scaffold usable for those fields. The scaffold renders
// them as "" — indistinguishable from a free-text field — so without the choices
// written down somewhere a caller has to read the spec or guess. Guessing is not
// cheap: Security Cloud's ipsec.right.vendor is case-sensitive, and sending
// "cisco" for "Cisco" is rejected with a 400 that does not name the field.
func buildEnumChoices(op *parser.Operation) []enumChoice {
	if op.RequestBody == nil || op.RequestBody.Schema == nil {
		return nil
	}
	var out []enumChoice
	collectEnumChoices(op.RequestBody.Schema, "", &out, 0)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// collectEnumChoices walks a schema's properties, recursing into nested objects.
// depth caps the walk so a schema that references itself cannot loop.
func collectEnumChoices(s *parser.Schema, prefix string, out *[]enumChoice, depth int) {
	if s == nil || depth > 6 {
		return
	}
	for name, prop := range s.Properties {
		if prop == nil {
			continue
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		if len(prop.Enum) > 0 {
			*out = append(*out, enumChoice{Path: path, Values: prop.Enum})
		}
		if prop.Nested != nil {
			collectEnumChoices(prop.Nested, path, out, depth+1)
		}
		// For an array the constraint lives on the element, not the property, so
		// walking properties alone misses it entirely. That is not a corner case:
		// six of the ZTNA gateway's IPSec cipher-suite fields are arrays of
		// enum-constrained strings, and the server requires ipsec.esp and
		// ipsec.ike, so anyone configuring IPSec has to fill them — while the
		// scaffold showed "[]" and the help listed nothing. The "[]" suffix marks
		// that it is each element that is constrained.
		if prop.Items != nil {
			if len(prop.Items.Enum) > 0 {
				*out = append(*out, enumChoice{Path: path + "[]", Values: prop.Items.Enum})
			}
			// An array of objects can carry enums inside the element too.
			if len(prop.Items.Properties) > 0 {
				collectEnumChoices(prop.Items, path+"[]", out, depth+1)
			}
		}
	}
}

// appendVariantNote says so when the request body is a discriminated union, and
// which of its shapes --scaffold renders.
//
// Without it the scaffold is quietly one of several legal bodies: it carries the
// first variant's fields, so a caller creating anything but a JAMF_PRO connector
// fills in a template for the wrong contract and learns of it from the server.
// The alternative — rendering every variant — cannot work, because the whole
// point of --scaffold's output is that it pipes into --file.
func appendVariantNote(long string, op *parser.Operation) string {
	if op.RequestBody == nil || op.RequestBody.Schema == nil {
		return long
	}
	sch := op.RequestBody.Schema
	if len(sch.Variants) < 2 {
		return long
	}
	note := fmt.Sprintf("The request body is one of %d shapes", len(sch.Variants))
	if sch.Discriminator != "" {
		note += fmt.Sprintf(", selected by %q", sch.Discriminator)
	}
	named := make([]string, 0, len(sch.Variants))
	for _, v := range sch.Variants {
		if v != "" {
			named = append(named, v)
		}
	}
	if len(named) > 0 {
		note += ": " + strings.Join(named, ", ")
	}
	note += ". --scaffold renders the first; the others differ in which fields they accept."
	if long == "" {
		return note
	}
	return long + "\n\n" + note
}

// appendEnumChoices adds an "Allowed values:" tail to an operation's long help,
// one line per enum-constrained field. Returns long unchanged when the operation
// has none.
func appendEnumChoices(long string, choices []enumChoice) string {
	if len(choices) == 0 {
		return long
	}
	var b strings.Builder
	b.WriteString(long)
	if long != "" {
		b.WriteString("\n\n")
	}
	b.WriteString("Allowed values:")
	for _, c := range choices {
		b.WriteString("\n  ")
		b.WriteString(c.Path)
		b.WriteString(": ")
		b.WriteString(strings.Join(c.Values, ", "))
	}
	return b.String()
}

// buildScaffold returns a pretty-printed JSON example for the operation's
// request body, derived from the spec schema. Returns "" when the op's body has
// no shape worth showing, which is the signal the template uses to omit
// --scaffold entirely.
//
// The rendering itself is parser.ScaffoldJSON, shared with the Jamf Pro and
// Security Cloud generators. It used to be a local copy — byte-identical between
// this file and generator/security/emitter.go, and subtly different from Pro's —
// so the same schema could scaffold three ways depending on which API served it.
func buildScaffold(op *parser.Operation) (string, error) {
	if op.RequestBody == nil || !parser.HasScaffoldShape(op.RequestBody.Schema) {
		return "", nil
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

// platformIgnoredRequiredParams lists query parameters a spec marks required
// that the server in fact ignores, keyed "{service}/{param}".
//
// Only wire probing reveals these, so an entry needs a recorded observation
// behind it. This duplicates knowledge the SDK holds, which is normally the
// thing to avoid; it lives here only because the SDK's generator config has no
// way to express "declared required, actually ignored" for it to publish. Move
// an entry into the published spec once it can.
//
// Empty as of the v1865 device-groups ingest, which is that last sentence
// happening rather than a sign the table is unused: the one entry it ever held
// — securitycloud/customer-id, declared required on both device-group list ops
// while the scope decided the customer and the parameter changed nothing — is
// gone from the spec upstream, so suppressing it here would now suppress
// nothing. The knob stays for the next one.
var platformIgnoredRequiredParams = map[string]bool{}

// documentedStatus names a non-2xx response an operation documents as a result
// rather than a failure, plus the error code its body must carry.
type documentedStatus struct {
	Code      int
	ErrorCode string
	// Empty marks a status meaning "not configured" rather than one whose body
	// is the answer — see platform.DocumentedStatus.Empty.
	Empty bool
}

// platformDocumentedStatusResults lists operations for which a non-2xx status is
// a documented *result*, keyed "{METHOD} {full tenant-bearing path}" — the form
// the generated command dispatches, and the same key shape
// platformOperationNameOverrides uses.
//
// There is no spec signal to derive this from: every one of these operations
// documents a 404 alongside 401/403/500 as boilerplate, so "declares a 404"
// cannot separate a singleton whose 404 body is the answer from an endpoint
// whose 404 means the thing genuinely is not there. Hence an explicit table,
// the same way the Jamf Pro generator's documentedStatusResults handles it.
//
// Each entry names the error code as well as the status, so the allowance is
// narrow: only the server's own "not configured" code is read as an answer,
// and a 404 arriving for any other reason still fails.
var platformDocumentedStatusResults = map[string][]documentedStatus{
	// A tenant with no search domain configured is the ordinary empty state of
	// a singleton settings endpoint, and the server answers it 404
	// SEARCH_DOMAIN_NOT_SET. Without this, `security dns-search-domains get`
	// exited 1 with a traceId for a tenant that was simply not using the
	// feature, so nothing could distinguish "not configured" from "the request
	// failed". Wire-confirmed on tenant wisconsam, 2026-08-20.
	"GET /securitycloud/v1/dns/search-domains": {
		{Code: 404, ErrorCode: "SEARCH_DOMAIN_NOT_SET", Empty: true},
	},
}

// buildQueryParams returns the user-facing query flags for an operation.
// tenantId would never be query (it's path) so no special-case is needed here.
//
// page/page-size are filtered out only when the op actually paginates, because
// then the runtime loop owns them and a flag would fight it. When it does not,
// they are ordinary query parameters and dropping them silently removes a
// control the spec declares: audit's list and lineage declare page-size with
// **cursor** rather than page, so hasPaginationParams is false, no loop is
// generated — and --page-size disappeared while --cursor was emitted beside it,
// leaving the one operation that pages by hand unable to say how big a page is.
// Latent until 2026-09-03, when the credential to reach audit at all first
// existed.
func buildQueryParams(params []*parser.Parameter, service string, paginate bool) []queryParam {
	var out []queryParam
	for _, p := range params {
		if p == nil || p.In != "query" {
			continue
		}
		if paginate {
			switch p.Name {
			case "page", "page-size":
				continue // managed by the pagination loop
			}
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

// filterTenantPathParams drops a "tenantId" placeholder from the list of
// CLI-facing positional params. Belt and braces since the scope moved into the
// X-Tenant-Id header: normalisePlatformPaths strips both the path segment and
// its parameter declaration, so nothing should reach here — but a spec that
// spells the segment somewhere new must not turn it into a positional arg the
// user is asked to supply.
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

// confirmStmt renders the platform.ConfirmAction guard for a destructive
// operation, identifying the target the same way the command resolves it: the
// name-resolved ID where the operation supports --name, the first positional
// otherwise, and the operation's own name when it takes no identifier at all.
func confirmStmt(op templateOp) string {
	target := fmt.Sprintf("%q", op.Name)
	switch {
	case op.SupportsNameLookup:
		target = "resolvedID"
	case len(op.PathParams) > 0:
		target = "args[0]"
	}
	return fmt.Sprintf("\t\t\tif err := platform.ConfirmAction(%q, %s, yes); err != nil {\n\t\t\t\treturn err\n\t\t\t}", op.Name, target)
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
