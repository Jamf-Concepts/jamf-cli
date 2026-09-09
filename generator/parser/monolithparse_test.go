// Copyright 2026, Jamf Software LLC

package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// loadCommittedSpecs loads every committed root spec, plus the shared library
// files, which declare no paths but hold components the others reference.
func loadCommittedSpecs(t *testing.T) []*openapi3.T {
	t.Helper()
	specs, err := filepath.Glob("../../specs/*.yaml")
	if err != nil {
		t.Fatalf("globbing specs: %v", err)
	}
	var docs []*openapi3.T
	for _, s := range specs {
		if strings.HasPrefix(filepath.Base(s), ".") {
			continue
		}
		loader := openapi3.NewLoader()
		loader.IsExternalRefsAllowed = true
		doc, err := loader.LoadFromFile(s)
		if err != nil {
			continue
		}
		docs = append(docs, doc)
	}
	if len(docs) == 0 {
		t.Fatal("no specs loaded from ../../specs")
	}
	return docs
}

// endpointShape strips a leading version segment, so /v1/foo and /v3/foo are
// the same endpoint asked at two versions.
var versionPrefix = regexp.MustCompile(`^/v\d+/`)

func endpointShape(method, path string) string {
	return method + " " + versionPrefix.ReplaceAllString(path, "/")
}

// baselineFile is a snapshot of every endpoint `pro` could reach under the
// 165-file spec layout, taken at that layout's final commit.
//
// A snapshot rather than a live comparison, because the parse it describes no
// longer exists: the per-resource files are gone and with them any way to
// re-derive it. That is the point of committing it — the guard for a redesign
// that removes its own baseline has to carry the baseline.
//
// Deletable once the deprecated aliases expire, which is the same moment the old
// names stop mattering.
const baselineFile = "testdata/endpoints-before-path-grouping.tsv"

// baselineEndpoint is one row of that snapshot.
type baselineEndpoint struct {
	Resource, Operation, Method, Path, NameField, IDField string
}

func readBaseline(t *testing.T) []baselineEndpoint {
	t.Helper()
	data, err := os.ReadFile(baselineFile)
	if err != nil {
		t.Fatalf("reading %s: %v\n\nIt is a committed artifact: the endpoint set the path-derived naming must not shrink.", baselineFile, err)
	}
	var out []baselineEndpoint
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 6 {
			t.Fatalf("malformed baseline row: %q", line)
		}
		out = append(out, baselineEndpoint{f[0], f[1], f[2], f[3], f[4], f[5]})
	}
	if len(out) == 0 {
		t.Fatalf("%s holds no rows; this test cannot pass vacuously", baselineFile)
	}
	return out
}

// parseCommittedSpecs is the live parse: merge every committed document and
// derive resources from the paths.
func parseCommittedSpecs(t *testing.T) []*Resource {
	t.Helper()
	merged, _, err := MergeDocuments(loadCommittedSpecs(t))
	if err != nil {
		t.Fatalf("MergeDocuments: %v", err)
	}
	resources, err := ParseMonolith(merged)
	if err != nil {
		t.Fatalf("ParseMonolith: %v", err)
	}
	return resources
}

// This is the guard for the whole redesign: grouping paths by collection must
// not make any endpoint unreachable.
//
// Compared as method plus version-stripped path, because which *version* of an
// endpoint a command sends is deduplicateVersionedOps' business and the point of
// the change is that it now decides that from the paths rather than from a
// filename's `-vN` suffix. What must not change is whether the endpoint can be
// reached at all.
//
// The only permitted losses are the legacy paths KeepPath drops by name.
func TestParseMonolith_LosesNoEndpoint(t *testing.T) {
	after := map[string]bool{}
	for _, r := range parseCommittedSpecs(t) {
		// AllOperations, because "reachable" is about the endpoint and not
		// about how many tokens deep the command sits: nine sub-paths are now
		// nested sub-resources, and reading only r.Operations reported all 28
		// of their endpoints as lost.
		for _, op := range r.AllOperations() {
			after[endpointShape(op.Method, op.Path)] = true
		}
	}

	var lost []string
	for _, want := range readBaseline(t) {
		shape := endpointShape(want.Method, want.Path)
		if after[shape] || droppedByPolicy(shape) {
			continue
		}
		lost = append(lost, fmt.Sprintf("%s  (served by %s %s)", shape, want.Resource, want.Operation))
	}
	sort.Strings(lost)
	lost = compactStrings(lost)
	if len(lost) > 0 {
		t.Errorf("%d endpoint(s) became unreachable:\n  %s", len(lost), strings.Join(lost, "\n  "))
	}
}

// droppedByPolicy reports whether an endpoint shape is one KeepPath removes on
// purpose. Matched on the path rather than by re-running KeepPath, because the
// shape has had its version stripped and the tags are not to hand.
func droppedByPolicy(shape string) bool {
	for _, p := range []string{
		"/preview/computers",
		"/preview/remote-administration-configurations",
		"/settings/issueTomcatSslCertificate",
		"/settings/obj/policyProperties",
	} {
		if strings.HasSuffix(shape, " "+p) {
			return true
		}
	}
	return false
}

// Grouping restores an endpoint the per-file layout discarded.
//
// `GET /v1/branding-images/download/{id}` lived in Icon.yaml, whose canonical
// prefix is /v1/icon, so filterToCanonicalPrefix dropped it with a warning on
// every generate. It has its own tag and its own path root, so it is now a
// resource.
//
// Note what is deliberately *not* claimed here. An earlier version of this test
// asserted that `PUT /v1/inventory-preload/{id}` was a lost capability being
// restored, on the grounds that the cross-resource version pass suppressed the
// base resource and v2 declared no `/v2/inventory-preload/{id}`. That was wrong: v2
// moved every record operation under `records/`, so the update lives at
// `PUT /v2/inventory-preload/records/{id}` and is served. The v1 family is
// withdrawn from the gateway and fully superseded, and parser.droppedPaths
// drops it — leaving it in gave the refused v1 paths the plain `list`, `get`
// and `update` names while the served v2 CRUD sat under a second command.
func TestParseMonolith_RestoresEndpointsTheFileLayoutHid(t *testing.T) {
	reachable := map[string]bool{}
	for _, r := range parseCommittedSpecs(t) {
		for _, op := range r.AllOperations() {
			reachable[op.Method+" "+op.Path] = true
		}
	}
	const want = "GET /v1/branding-images/download/{id}"
	if !reachable[want] {
		t.Errorf("%s is still unreachable", want)
	}
	// And it was genuinely absent before, so this is about a fix rather than an
	// endpoint that was always there.
	for _, row := range readBaseline(t) {
		if row.Method+" "+row.Path == want {
			t.Errorf("the baseline already reached %s; this test no longer demonstrates anything", want)
		}
	}
}

// Each resource sees only the schemas its own operations reach.
//
// A merged document has one components block, so without the $ref closure every
// resource would see all ~750 schemas — and detectNameField/detectIDField scan
// that set, so they would answer from an unrelated resource's fields.
//
// Asserted on those two fields rather than on a schema count, because zero
// schemas is a legitimate state: health-check answers 204, the branding-image
// download answers `image/*`, and accept-disclaimer answers 202. A resource
// whose only endpoint returns no JSON has no schema to reach.
func TestParseMonolith_ScopesSchemasToTheResource(t *testing.T) {
	merged, _, err := MergeDocuments(loadCommittedSpecs(t))
	if err != nil {
		t.Fatalf("MergeDocuments: %v", err)
	}
	total := len(merged.Components.Schemas)
	resources, err := ParseMonolith(merged)
	if err != nil {
		t.Fatalf("ParseMonolith: %v", err)
	}

	var widest int
	var widestName string
	byPath := map[string]*Resource{}
	// Flattened: a nested sub-resource inherits its parent's closure, and its
	// operations still have to resolve to a resource that can detect a name or
	// id field.
	for _, r := range FlattenResources(resources) {
		if len(r.Schemas) > widest {
			widest, widestName = len(r.Schemas), r.QualifiedName()
		}
		for _, op := range r.Operations {
			byPath[op.Method+" "+op.Path] = r
		}
	}
	if widest >= total {
		t.Errorf("%s sees %d of %d schemas — the closure is not scoping anything", widestName, widest, total)
	}

	// The real regression: an endpoint whose resource could detect a name or id
	// field must still belong to one that can. That is the only thing the
	// closure feeds, so a scoping bug shows up here rather than in a count.
	checked := 0
	for _, row := range readBaseline(t) {
		if row.NameField == "" && row.IDField == "" {
			continue
		}
		now, ok := byPath[row.Method+" "+row.Path]
		if !ok {
			continue // dropped or superseded; covered by the parity test
		}
		checked++
		if row.NameField != "" && now.NameField == "" {
			t.Errorf("%s %s detected name field %q; %s now detects none",
				row.Method, row.Path, row.NameField, now.Name)
		}
		if row.IDField != "" && now.IDField == "" {
			t.Errorf("%s %s detected id field %q; %s now detects none",
				row.Method, row.Path, row.IDField, now.Name)
		}
	}
	if checked == 0 {
		t.Fatal("compared no endpoints; this test cannot pass vacuously")
	}
	t.Logf("%d schemas merged; widest resource (%s) sees %d; %d endpoints compared", total, widestName, widest, checked)
}

// compactStrings removes adjacent duplicates from a sorted slice.
func compactStrings(xs []string) []string {
	var out []string
	for i, x := range xs {
		if i == 0 || xs[i-1] != x {
			out = append(out, x)
		}
	}
	return out
}

// A path declared twice is a hard error; a component declared twice is normal
// and reported only when the two declarations disagree.
func TestMergeDocuments_DuplicatePathIsAnErrorAndDuplicateSchemaIsNot(t *testing.T) {
	mk := func(path, schemaField string) *openapi3.T {
		doc := &openapi3.T{
			OpenAPI:    "3.0.1",
			Info:       &openapi3.Info{Title: "t", Version: "1"},
			Paths:      openapi3.NewPaths(),
			Components: &openapi3.Components{Schemas: openapi3.Schemas{}},
		}
		doc.Paths.Set(path, &openapi3.PathItem{Get: &openapi3.Operation{
			Responses: openapi3.NewResponses(),
		}})
		doc.Components.Schemas["Shared"] = openapi3.NewSchemaRef("", &openapi3.Schema{
			Type:       &openapi3.Types{"object"},
			Properties: openapi3.Schemas{schemaField: openapi3.NewStringSchema().NewRef()},
		})
		return doc
	}

	if _, _, err := MergeDocuments([]*openapi3.T{mk("/v1/a", "x"), mk("/v1/a", "x")}); err == nil {
		t.Error("a path declared by two documents should be an error")
	}

	_, reports, err := MergeDocuments([]*openapi3.T{mk("/v1/a", "x"), mk("/v1/b", "x")})
	if err != nil {
		t.Fatalf("identical duplicate schemas should merge silently: %v", err)
	}
	if len(reports) != 0 {
		t.Errorf("identical duplicate schemas reported: %v", reports)
	}

	_, reports, err = MergeDocuments([]*openapi3.T{mk("/v1/a", "x"), mk("/v1/b", "y")})
	if err != nil {
		t.Fatalf("a disagreeing duplicate schema must not fail the merge: %v", err)
	}
	if len(reports) != 1 || !strings.Contains(reports[0], "Shared") {
		t.Errorf("a disagreeing duplicate schema should be reported once naming it, got %v", reports)
	}
}
