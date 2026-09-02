// Copyright 2026, Jamf Software LLC

package classic

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/Jamf-Concepts/jamf-cli/generator/classicschema"
	"github.com/Jamf-Concepts/jamf-cli/generator/parser"
)

// AttachSchemas binds each Classic resource to the request-body schema the
// committed artifact names for it, so the template can emit --scaffold, --set
// and required/enum help.
//
// A nil artifact attaches nothing and is not an error: Classic commands then
// ship exactly as they did before the artifact existed, reading their body from
// --from-file or stdin with no guidance. `make generate` has to work in a tree
// where nobody has fetched an SDK spec.
//
// The schemas are loaded through kin-openapi so $ref resolves — a Classic schema
// refers to a dozen shared components (site, category, id_name, scope) — and then
// through parser.SchemaFromOpenAPI, so a Classic body is walked by the same code
// that walks a Pro, Platform or Security Cloud one.
func AttachSchemas(resources []ClassicResource, art *classicschema.Artifact) error {
	if art == nil || len(art.Resources) == 0 {
		return nil
	}

	doc, err := loadArtifactDoc(art)
	if err != nil {
		return err
	}

	for i := range resources {
		res, ok := art.Resources[resources[i].Name]
		if !ok {
			continue
		}
		ref, ok := doc.Components.Schemas[res.Schema]
		if !ok || ref.Value == nil {
			return fmt.Errorf("classic resource %q names schema %q, which the artifact does not declare", resources[i].Name, res.Schema)
		}
		resources[i].BodySchema = parser.SchemaFromOpenAPI(res.Schema, ref.Value)
		resources[i].BodyRoot = res.Root
		resources[i].BodySchemaName = res.Schema
	}
	return nil
}

// loadArtifactDoc turns the artifact into a kin-openapi document so its internal
// $refs resolve.
//
// The artifact deliberately carries no paths (see package classicschema), and
// kin-openapi will not load a document without them, so an empty Paths object is
// synthesised here rather than committed to the file. Keeping it out of the file
// is the point: a committed spec with no operations cannot be mistaken for one
// the platform generator should be pointed at.
func loadArtifactDoc(art *classicschema.Artifact) (*openapi3.T, error) {
	shim := map[string]any{
		"openapi":    "3.0.3",
		"info":       map[string]any{"title": art.Info.Title, "version": art.Info.Version},
		"paths":      map[string]any{},
		"components": map[string]any{"schemas": art.Components.Schemas},
	}
	data, err := json.Marshal(shim)
	if err != nil {
		return nil, fmt.Errorf("marshalling classic schema document: %w", err)
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(data)
	if err != nil {
		return nil, fmt.Errorf("loading classic schema document: %w", err)
	}
	return doc, nil
}

// ── Template helpers ──────────────────────────────────────────────────────
//
// These render the parsed schema into the three things a Classic write command
// gains from it: an XML body template, a required/optional split, and the enum
// choices for a constrained field. All three are computed at generation time and
// baked into the generated source as literals, so the CLI carries no schema at
// runtime.

// HasBodySchema reports whether a resource carries enough shape for --scaffold
// and --set to be worth emitting.
func (r ClassicResource) HasBodySchema() bool {
	return r.BodySchema != nil && parser.HasScaffoldShape(r.BodySchema)
}

// ScaffoldXML renders the resource's request-body template as XML.
func (r ClassicResource) ScaffoldXML() (string, error) {
	if r.BodySchema == nil {
		return "", nil
	}
	return parser.ScaffoldXML(r.BodySchema, r.BodyRoot)
}

// RequiredFields lists the fields the spec marks required on the body's own
// schema, in sorted order.
//
// Worth surfacing because the server's answer to a missing required field is an
// HTTP 409 carrying an HTML error page — `Error: ComputerGroup name is required`
// buried in a `<body style="font-family: sans-serif;">` — one field at a time.
// It is discoverable, but only by trying.
//
// Top level only, and deliberately not the whole tree. A nested `required` means
// "required *if* the enclosing object is sent", and every Classic object that
// declares one sits under an optional parent: computer_group's only nested entry
// is site.name, so reporting it would tell a caller that a group cannot be
// created without naming a site, which is false. When a Classic schema first
// declares a required object, this needs to grow the ancestor check rather than
// the whole tree.
func (r ClassicResource) RequiredFields() []string {
	if r.BodySchema == nil {
		return nil
	}
	out := append([]string(nil), r.BodySchema.Required...)
	sort.Strings(out)
	return out
}

// TopLevelOptionalFields lists the resource's top-level body sections that are
// not required, in sorted order.
//
// Top level only, deliberately. A Classic policy has 271 fields at depth 5, and
// an exhaustive optional list in --help would bury the required one; the full
// tree is what --scaffold is for.
func (r ClassicResource) TopLevelOptionalFields() []string {
	if r.BodySchema == nil {
		return nil
	}
	required := map[string]bool{}
	for _, req := range r.BodySchema.Required {
		required[req] = true
	}
	var out []string
	for name, prop := range r.BodySchema.Properties {
		if prop == nil || prop.ReadOnly || required[name] || parser.ClassicIsCountElement(name, prop, r.BodySchema) {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// EnumChoice is one constrained field and the values it accepts.
type EnumChoice struct {
	Path   string
	Values []string
}

// EnumChoices lists every enum-constrained field in the resource's body schema,
// as dotted paths with a "[]" segment marking an array element.
//
// This is the part of the schema the wire will not teach you. Probed on a live
// tenant 2026-09-02: the Classic API does not enforce its own enums. A policy
// created with `<frequency>Twice per fortnight</frequency>` answers 201 and reads
// back `Once per computer`; a computer group criterion with `<and_or>maybe</and_or>`
// answers 201 and reads back `and`. The value is silently replaced with the
// default, with no error and no warning — so a caller who guesses wrong gets a
// working object that does the wrong thing, and --help is the only place the
// legal set can come from.
func (r ClassicResource) EnumChoices() []EnumChoice {
	if r.BodySchema == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []EnumChoice
	walkSchema(r.BodySchema, "", func(path string, s *parser.Schema) {
		for name, prop := range s.Properties {
			if prop == nil || prop.ReadOnly || len(prop.Enum) == 0 {
				continue
			}
			full := joinPath(path, name)
			if seen[full] {
				continue
			}
			seen[full] = true
			out = append(out, EnumChoice{Path: full, Values: prop.Enum})
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// SetFieldTypes maps every settable dotted path in the body schema to its spec
// type, for the generated --set parser to coerce against.
//
// Schema-driven rather than a looks-like-JSON heuristic, following the modern Pro
// generator: docs/solutions/logic-errors/set-array-object-stringification-2026-07-24.md
// records what the heuristic costs, and Classic makes it worse. Classic XML is
// untyped on the wire, so `--set general.name=42` has to stay the string "42"
// and a boolean field has to render `true` rather than `1`; nothing in the
// response would reveal a wrong guess.
func (r ClassicResource) SetFieldTypes() map[string]string {
	if r.BodySchema == nil {
		return nil
	}
	types := map[string]string{}
	walkSchema(r.BodySchema, "", func(path string, s *parser.Schema) {
		// A path inside a repeated element is not settable. --set builds one
		// element per dotted segment and has no way to say which member of a
		// repeated element it means, so `criteria[].and_or` is a help path only
		// — leaving it in here made `--set 'criteria[].and_or=and'` succeed and
		// emit an element literally named `criteria[]`.
		if strings.Contains(path, "[]") {
			return
		}
		for name, prop := range s.Properties {
			if prop == nil || prop.ReadOnly || parser.ClassicIsCountElement(name, prop, s) {
				continue
			}
			full := joinPath(path, name)
			if _, ok := types[full]; ok {
				continue
			}
			types[full] = prop.Type
		}
	})
	return types
}

// SetFieldTypeKeys returns SetFieldTypes' keys in sorted order, so the generated
// literal is stable across runs.
func (r ClassicResource) SetFieldTypeKeys() []string {
	types := r.SetFieldTypes()
	keys := make([]string, 0, len(types))
	for k := range types {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// SetCompletions lists the scalar dotted paths worth offering as shell
// completions for --set, each with a trailing "=".
//
// Two exclusions, both because a completion is a suggestion and suggesting
// something that will be refused is worse than suggesting nothing:
//
//   - Objects and arrays. --set cannot set either as a whole, and the generated
//     parser refuses them by name with an explanation.
//   - Credential fields. --set refuses these on purpose, so offering
//     `read_write_password=` at the shell prompt would walk a caller straight
//     into the thing the credential policy exists to prevent — and it is the
//     shell that records the command line.
func (r ClassicResource) SetCompletions() []string {
	credentials := r.CredentialFields()
	var out []string
	for path, kind := range r.SetFieldTypes() {
		switch kind {
		case "object", "array", "":
			continue
		}
		if slices.Contains(credentials, path) {
			continue
		}
		out = append(out, path+"=")
	}
	sort.Strings(out)
	return out
}

// RepeatedElements maps an array-typed dotted path to the element name its
// members are wrapped in, so the runtime --set builder can render XML.
//
// Classic models a repeated element as a JSON array whose items carry one named
// object child: `<criteria><criterion>…</criterion></criteria>` is declared as
// `criteria: {type: array, items: {properties: {criterion: {...}, size: {...}}}}`.
// The element name is not derivable from the array's own name — `criteria` holds
// `criterion`, `computers` holds `computer`, `scope.limit_to_users.user_groups`
// holds `user_group` — so it is read off the schema and recorded here.
func (r ClassicResource) RepeatedElements() map[string]string {
	if r.BodySchema == nil {
		return nil
	}
	out := map[string]string{}
	walkSchema(r.BodySchema, "", func(path string, s *parser.Schema) {
		for name, prop := range s.Properties {
			if prop == nil || prop.Type != "array" {
				continue
			}
			if elem := parser.ClassicRepeatedElement(prop.Items); elem != "" {
				out[joinPath(path, name)] = elem
			}
		}
	})
	return out
}

// RepeatedElementKeys returns RepeatedElements' keys in sorted order.
func (r ClassicResource) RepeatedElementKeys() []string {
	m := r.RepeatedElements()
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// CredentialFields lists the dotted paths whose value is a credential, so the
// generated --set refuses them.
//
// The repo's credential policy forbids passwords, tokens and client secrets in
// flag values, because a flag value lands in shell history, in `ps` output and
// in CI logs. None of the three existing --set implementations enforces it, and
// on the Classic surface that gap is wide: an SMTP server, an LDAP server, a
// distribution point, a GSX connection, a VPP account and a directory binding
// all carry one, and `apply`/`create`/`update` are exactly the commands a
// caller reaches for. So Classic refuses the pair and names --from-file
// instead, rather than inheriting a hazard the policy already rules out.
//
// Matched on the field name, not on a schema marker: the Classic spec declares
// writeOnly 28 times in total and on none of these fields.
func (r ClassicResource) CredentialFields() []string {
	if r.BodySchema == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	walkSchema(r.BodySchema, "", func(path string, s *parser.Schema) {
		for name, prop := range s.Properties {
			if prop == nil || !isCredentialField(name, prop.Type) {
				continue
			}
			full := joinPath(path, name)
			if !seen[full] {
				seen[full] = true
				out = append(out, full)
			}
		}
	})
	sort.Strings(out)
	return out
}

// credentialFieldNames are the Classic body field names that carry a secret.
// Substring matching on a curated list rather than a bare "contains password",
// so `password_sha256` and `read_write_password` are caught while a field merely
// mentioning a credential in prose is not.
var credentialFieldNames = []string{
	"password",
	"secret",
	"service_token",
	"private_key",
	"keystore_password",
	"shared_secret",
}

// isCredentialField reports whether a body field carries a secret value.
//
// The type test is not belt-and-braces: a distribution point declares
// `username_password_required`, a boolean switch whose name contains "password"
// and whose value is not one. Refusing --set on it would block a legitimate
// setting and send the caller to --from-file for no reason, so only a
// string-valued field counts.
func isCredentialField(name, kind string) bool {
	if kind != "string" {
		return false
	}
	lower := strings.ToLower(name)
	for _, c := range credentialFieldNames {
		if strings.Contains(lower, c) {
			return true
		}
	}
	return false
}

// walkSchema visits a schema and every nested object and array-element schema
// beneath it, calling fn with the dotted path of each.
//
// The path uses "[]" for an array whose elements are objects, and elides the
// repeated-element wrapper the Classic JSON representation inserts — so a
// criterion's fields are reached at `criteria[].name`, matching what --scaffold
// prints and what --set accepts.
func walkSchema(s *parser.Schema, path string, fn func(string, *parser.Schema)) {
	if s == nil {
		return
	}
	fn(path, s)
	for name, prop := range s.Properties {
		if prop == nil || prop.ReadOnly {
			continue
		}
		full := joinPath(path, name)
		switch {
		case prop.Type == "array":
			if elem := parser.ClassicArrayElementSchema(prop.Items); elem != nil {
				walkSchema(elem, full+"[]", fn)
			}
		case prop.Nested != nil:
			walkSchema(prop.Nested, full, fn)
		}
	}
}

func joinPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}
