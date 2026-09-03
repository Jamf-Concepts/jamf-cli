// Copyright 2026, Jamf Software LLC

// Package classicschema derives the committed Classic API schema artifact,
// specs/classic/schemas.json, from jamfplatform-go-sdk's published
// classic_api_resource_documentation.json.
//
// The Classic API manifest (specs/classic/resources.yaml) says which resources
// the CLI ships and how their URLs are built. It says nothing about what goes
// *inside* a request body, so `classic-policies create` could only ever tell a
// caller to pipe XML at it. The SDK's Classic spec carries that missing half —
// 161 component schemas, 145 enum constraints, 1382 examples and a `required`
// list on 62 of them — and this package is how it reaches the generator.
//
// # Why an artifact rather than reading the SDK spec directly
//
// The same reason specs/gateway/coverage.json exists: `make generate` and CI
// have to work in a tree where nobody has an SDK checkout. specs/.platform-source
// is gitignored, so a generator that read it directly would emit a different CLI
// depending on what a developer last dropped there.
//
// # Why the artifact carries no paths
//
// classic_api_resource_documentation.json describes Jamf Pro APIs this repo
// already generates from specs/classic/resources.yaml, which is why the Makefile
// keeps it in PLATFORM_SDK_COVERAGE_SPECS and warns that it must never join
// PLATFORM_SDK_SPECS — handing it to the platform generator emits a second set of
// Pro commands built from gateway paths.
//
// Committing a trimmed copy into specs/classic/ puts that same hazard next door
// to a file the generator does read. So the artifact keeps components.schemas and
// drops `paths` entirely: with no operations in it, no generator can produce a
// command from it even if one is pointed at it by mistake. The resource-to-schema
// mapping the paths used to provide is resolved here, at derivation time, and
// recorded in x-jamf-classic-resources.
package classicschema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// SourceFile is the SDK spec this package derives from. Same file the gateway
// coverage manifest reads, and the same drop directory.
const SourceFile = "classic_api_resource_documentation.json"

// ArtifactFile is the committed artifact's path relative to the specs directory.
const ArtifactFile = "classic/schemas.json"

// Artifact is the committed schema artifact. It is a deliberately partial
// OpenAPI 3 document: Components is populated and Paths is absent, so nothing
// can generate a command from it.
type Artifact struct {
	OpenAPI    string          `json:"openapi"`
	Info       Info            `json:"info"`
	Source     Source          `json:"x-jamf-source"`
	Resources  map[string]*Res `json:"x-jamf-classic-resources"`
	Components Components      `json:"components"`
}

// Info is the artifact's OpenAPI info block.
type Info struct {
	Title   string `json:"title"`
	Version string `json:"version"`
}

// Source records where the artifact came from, so staleness is checkable
// without an SDK checkout.
type Source struct {
	Spec       string `json:"spec"`
	Title      string `json:"title"`
	Version    string `json:"version"`
	SDKCommit  string `json:"sdkCommit,omitempty"`
	Schemas    int    `json:"schemas"`
	Resources  int    `json:"resources"`
	Unresolved int    `json:"unresolved"`
}

// Components holds the schema map, keyed by component name.
type Components struct {
	Schemas map[string]json.RawMessage `json:"schemas"`
}

// Res binds one CLI Classic resource to the component schema that describes its
// body, and to the XML root element a request must be wrapped in.
type Res struct {
	// Schema is the component schema key, e.g. "policy".
	Schema string `json:"schema"`
	// Root is the XML root element name for a request body. Taken from the
	// schema's xml.name when it declares one, else the schema key itself.
	Root string `json:"root"`
	// From is the spec operation the binding was read off, e.g.
	// "GET /policies/id/{id}". Recorded so a surprising binding can be traced
	// back to the spec rather than to this package's inference.
	From string `json:"from"`
	// SingularAgrees is false when the manifest's `singular:` field disagrees
	// with Root. The manifest value is what the CLI already sends as the XML
	// root and as the JSON unwrap key, so a disagreement is a real conflict
	// between two sources of the same fact and is surfaced as a warning rather
	// than silently resolved.
	SingularAgrees bool `json:"singularAgrees"`
	// Singular is the manifest's value, recorded whenever it disagrees.
	Singular string `json:"singular,omitempty"`
}

// ManifestEntry is the subset of a specs/classic/resources.yaml entry this
// package needs. Passed in rather than parsed here, so generator/classic stays
// the only reader of the manifest format.
type ManifestEntry struct {
	Name     string
	Path     string
	Singular string
	IDPath   string
}

// Extract derives the artifact from the SDK spec in srcDir, binding each
// manifest entry to a component schema.
//
// Returns the artifact plus warnings: one per manifest resource whose schema
// could not be resolved, and one per resource whose manifest `singular`
// disagrees with the schema's XML root. Neither is fatal. An unresolved
// resource simply ships without body help, which is the state every Classic
// resource is in today, and four of the six unresolved ones are resources the
// gateway has already withdrawn.
func Extract(srcDir, sdkCommit string, manifest []ManifestEntry) (*Artifact, []string, error) {
	raw, err := os.ReadFile(filepath.Join(srcDir, SourceFile))
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", SourceFile, err)
	}

	var spec struct {
		Info struct {
			Title   string `json:"title"`
			Version string `json:"version"`
		} `json:"info"`
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		return nil, nil, fmt.Errorf("parsing %s: %w", SourceFile, err)
	}
	if len(spec.Components.Schemas) == 0 {
		return nil, nil, fmt.Errorf("%s declares no component schemas", SourceFile)
	}

	art := &Artifact{
		OpenAPI: "3.0.3",
		Info: Info{
			Title:   "Jamf Pro Classic API resource schemas",
			Version: spec.Info.Version,
		},
		Source: Source{
			Spec:      SourceFile,
			Title:     spec.Info.Title,
			Version:   spec.Info.Version,
			SDKCommit: sdkCommit,
			Schemas:   len(spec.Components.Schemas),
		},
		Resources:  make(map[string]*Res, len(manifest)),
		Components: Components{Schemas: spec.Components.Schemas},
	}

	var warnings []string
	for _, e := range manifest {
		res, from := resolveResource(spec.Paths, spec.Components.Schemas, e)
		if res == nil {
			art.Source.Unresolved++
			warnings = append(warnings, fmt.Sprintf(
				"classic resource %q: no component schema found (tried %s) — it ships without body help",
				e.Name, strings.Join(candidatePaths(e), ", ")))
			continue
		}
		res.From = from
		res.SingularAgrees = e.Singular == "" || e.Singular == res.Root
		if !res.SingularAgrees {
			res.Singular = e.Singular
			warnings = append(warnings, fmt.Sprintf(
				"classic resource %q: manifest singular %q disagrees with the spec's XML root %q (schema %q, from %s)",
				e.Name, e.Singular, res.Root, res.Schema, from))
		}
		art.Resources[e.Name] = res
	}
	art.Source.Resources = len(art.Resources)

	return art, warnings, nil
}

// candidatePaths lists the spec paths a resource's body schema is looked for on,
// most specific first. The {id} detail path is preferred over the collection:
// a collection GET returns a list wrapper, whose schema is the plural, and the
// body of a create or update is a single object.
func candidatePaths(e ManifestEntry) []string {
	idPath := e.IDPath
	if idPath == "" {
		idPath = "id"
	}
	return []string{
		fmt.Sprintf("/%s/%s/{id}", e.Path, idPath),
		fmt.Sprintf("/%s/%s/{name}", e.Path, "name"),
		"/" + e.Path,
	}
}

var refRE = regexp.MustCompile(`"\$ref"\s*:\s*"#/components/schemas/([A-Za-z0-9_.-]+)"`)

// resolveResource finds the component schema describing one resource's object
// body, by reading the 200 response of its detail GET.
//
// The spec is read rather than guessed from the resource name because the two
// disagree often enough to matter: the manifest's mobiledeviceconfigurationprofiles
// carries singular "configuration_profile" while the spec's schema is
// "mobile_device_configuration_profile", and /accounts/groupid/{id} answers with
// "group" rather than anything derived from "accounts".
func resolveResource(paths map[string]map[string]json.RawMessage, schemas map[string]json.RawMessage, e ManifestEntry) (*Res, string) {
	for _, p := range candidatePaths(e) {
		ops, ok := paths[p]
		if !ok {
			continue
		}
		get, ok := ops["get"]
		if !ok {
			continue
		}
		name := firstSchemaRef(get, schemas)
		if name == "" {
			continue
		}
		name = preferWriteSchema(name, schemas)
		return &Res{Schema: name, Root: xmlRoot(name, schemas[name])}, "GET " + p
	}
	return nil, ""
}

// firstSchemaRef returns the first component schema an operation's responses
// reference that actually exists in the schema map.
func firstSchemaRef(op json.RawMessage, schemas map[string]json.RawMessage) string {
	var wrapper struct {
		Responses map[string]json.RawMessage `json:"responses"`
	}
	if err := json.Unmarshal(op, &wrapper); err != nil {
		return ""
	}
	// Prefer a 2xx response; a 4xx body is an error envelope, not the resource.
	codes := make([]string, 0, len(wrapper.Responses))
	for c := range wrapper.Responses {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	for _, c := range codes {
		if !strings.HasPrefix(c, "2") {
			continue
		}
		for _, m := range refRE.FindAllStringSubmatch(string(wrapper.Responses[c]), -1) {
			if _, ok := schemas[m[1]]; ok {
				return m[1]
			}
		}
	}
	return ""
}

// xmlRoot returns the XML root element a request body for this schema must be
// wrapped in: the schema's own xml.name when it declares one, else the schema
// key.
//
// Only 24 of the 161 schemas declare xml.name, and they are the ones where the
// two differ — the write-shaped `*_post` schemas name the element their base
// schema is keyed on. For everything else the component key already is the
// element name, which is a property of the Classic API rather than a
// coincidence: its JSON representation is a mechanical rendering of the XML.
func xmlRoot(name string, schema json.RawMessage) string {
	var s struct {
		XML struct {
			Name string `json:"name"`
		} `json:"xml"`
	}
	if err := json.Unmarshal(schema, &s); err == nil && s.XML.Name != "" {
		return s.XML.Name
	}
	// A "_post" schema is a write variant of a base schema, not an element name.
	// Most of them declare xml.name and are caught above, but ldap_server_post,
	// mobile_device_invitation_post and user_post do not — so without this a
	// request body would be wrapped in <ldap_server_post>, which is not an
	// element the Classic API has ever heard of.
	return strings.TrimSuffix(name, "_post")
}

// Write marshals the artifact to path, creating parent directories. Output is
// indented and key-sorted so a re-derivation from an unchanged spec produces a
// byte-identical file and `make verify-classic-schemas` stays meaningful.
func Write(a *Artifact, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling classic schema artifact: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// Load reads the committed artifact. A missing file is not an error: it is the
// "unknown" answer, and the generator then emits Classic commands with no body
// help — exactly what it emitted before this existed. `make generate` has to
// work in a tree where nobody has fetched an SDK spec.
func Load(path string) (*Artifact, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var a Artifact
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &a, nil
}

// CarryForwardProvenance keeps a recorded SDK revision that this run was not
// told, so re-deriving from unchanged specs does not blank it.
//
// Same reasoning as gateway.CarryForwardProvenance: without it,
// `make verify-classic-schemas` reports a stale artifact that is byte-identical
// apart from the one field the verification run just erased.
func CarryForwardProvenance(a, prev *Artifact) {
	if a == nil || prev == nil {
		return
	}
	if a.Source.SDKCommit == "" {
		a.Source.SDKCommit = prev.Source.SDKCommit
	}
}

// preferWriteSchema swaps a read schema for its write-shaped "_post" sibling
// when the spec declares one.
//
// The spec carries 15 of these — policy_post, computer_group_post,
// network_segment_post and friends — and 13 are orphaned: no operation
// references them, so the POST and PUT bodies they describe are declared nowhere
// the paths can reach. They are property-identical to their read counterparts
// and differ only in `xml` and `required`, which makes swapping them in cheap
// and makes ignoring them a real loss:
//
// computer_group_post requires [name, is_smart] where computer_group declares no
// top-level required list at all. Both are genuinely required on the wire —
// probed 2026-09-02, a create omitting either answers 409 ("ComputerGroup name is
// required" / "Computer Group definition missing is_smart attribute") — so
// reading the base schema alone would have shipped a --help that named neither.
//
// Only one resource gains today. It is done for all of them because the
// asymmetry is a property of how the spec is published rather than of that one
// resource, so the next ingest can add more without a code change.
func preferWriteSchema(name string, schemas map[string]json.RawMessage) string {
	post := name + "_post"
	if _, ok := schemas[post]; ok {
		return post
	}
	return name
}
