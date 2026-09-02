// Copyright 2026, Jamf Software LLC

package classic

import (
	"encoding/xml"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/generator/classicschema"
	"github.com/Jamf-Concepts/jamf-cli/generator/parser"
)

const (
	testManifest = "../../specs/classic/resources.yaml"
	testArtifact = "../../specs/classic/schemas.json"
)

// liveResources loads the shipped manifest with the committed schema artifact
// attached. Skips when either is absent, so the package still tests in a tree
// that has not been generated.
func liveResources(t *testing.T) []ClassicResource {
	t.Helper()
	res, err := ParseManifest(testManifest)
	if err != nil {
		t.Skipf("manifest unavailable: %v", err)
	}
	art, err := classicschema.Load(testArtifact)
	if err != nil {
		t.Fatalf("loading %s: %v", testArtifact, err)
	}
	if art == nil {
		t.Skip("no classic schema artifact committed")
	}
	if err := AttachSchemas(res, art); err != nil {
		t.Fatalf("attaching schemas: %v", err)
	}
	return res
}

func find(t *testing.T, res []ClassicResource, name string) ClassicResource {
	t.Helper()
	for _, r := range res {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("resource %q not in the manifest", name)
	return ClassicResource{}
}

// ── AttachSchemas ─────────────────────────────────────────────────────────

func TestAttachSchemas_NilArtifactAttachesNothing(t *testing.T) {
	res, err := ParseManifest(testManifest)
	if err != nil {
		t.Skipf("manifest unavailable: %v", err)
	}
	if err := AttachSchemas(res, nil); err != nil {
		t.Fatalf("a nil artifact must not be an error: %v", err)
	}
	for _, r := range res {
		if r.HasBodySchema() {
			t.Fatalf("%s got a schema from a nil artifact", r.Name)
		}
	}
}

func TestAttachSchemas_ResolvesRefsAcrossComponents(t *testing.T) {
	// A Classic schema refers to a dozen shared components — site, category,
	// id_name, scope — so a loader that did not resolve $ref would attach a
	// schema whose sections were all empty.
	r := find(t, liveResources(t), "computergroups")
	if !r.HasBodySchema() {
		t.Fatal("computergroups has no body schema")
	}
	site := r.BodySchema.Properties["site"]
	if site == nil || site.Nested == nil || len(site.Nested.Properties) == 0 {
		t.Fatalf("the shared site component did not resolve: %#v", site)
	}
}

// ── RequiredFields ────────────────────────────────────────────────────────

// TestRequiredFields_ReportsTopLevelOnly pins the reason the walk is not
// recursive. A nested `required` means "required if the enclosing object is
// sent", and computer_group's only nested entry is site.name — reporting it
// would tell a caller a group cannot be created without naming a site, which is
// false.
func TestRequiredFields_ReportsTopLevelOnly(t *testing.T) {
	r := find(t, liveResources(t), "computergroups")
	got := r.RequiredFields()
	want := []string{"is_smart", "name"}
	if !slices.Equal(got, want) {
		t.Errorf("RequiredFields() = %v, want %v", got, want)
	}
	if slices.Contains(got, "site.name") {
		t.Error("a nested required leaked into the top-level list")
	}
}

// TestRequiredFields_MatchTheWire records what the server actually enforces for
// the resource the "_post" preference exists for. Both values below were
// confirmed by a 409 on a create omitting them, 2026-09-02.
func TestRequiredFields_MatchTheWire(t *testing.T) {
	res := liveResources(t)
	for _, tc := range []struct {
		resource string
		want     []string
	}{
		{"computergroups", []string{"is_smart", "name"}},
		{"networksegments", []string{"ending_address", "name", "starting_address"}},
		{"distributionpoints", []string{"name", "read_only_username", "read_write_username", "share_name"}},
	} {
		if got := find(t, res, tc.resource).RequiredFields(); !slices.Equal(got, tc.want) {
			t.Errorf("%s RequiredFields() = %v, want %v", tc.resource, got, tc.want)
		}
	}
}

// ── EnumChoices ───────────────────────────────────────────────────────────

func TestEnumChoices_ReachesInsideARepeatedElement(t *testing.T) {
	// criteria[].and_or is the enum a smart group needs and the one a
	// properties-only walk would miss, since it sits on the element schema.
	r := find(t, liveResources(t), "computergroups")
	var found *EnumChoice
	for i, e := range r.EnumChoices() {
		if e.Path == "criteria[].and_or" {
			found = &r.EnumChoices()[i]
		}
	}
	if found == nil {
		t.Fatalf("criteria[].and_or missing; got %v", r.EnumChoices())
	}
	if !slices.Equal(found.Values, []string{"and", "or"}) {
		t.Errorf("values = %v, want [and or]", found.Values)
	}
}

func TestEnumChoices_CarriesThePolicyFrequencyTheServerSilentlyCoerces(t *testing.T) {
	// The case that justifies rendering enums at all: wire-checked 2026-09-02, a
	// policy created with frequency "Twice per fortnight" answers 201 and reads
	// back "Once per computer". Nothing but this list tells a caller the set.
	r := find(t, liveResources(t), "policies")
	for _, e := range r.EnumChoices() {
		if e.Path != "general.frequency" {
			continue
		}
		if !slices.Contains(e.Values, "Once per computer") || !slices.Contains(e.Values, "Once every week") {
			t.Errorf("general.frequency values look wrong: %v", e.Values)
		}
		return
	}
	t.Errorf("general.frequency not found among %d enum fields", len(r.EnumChoices()))
}

// ── SetFieldTypes / SetCompletions ────────────────────────────────────────

// TestSetFieldTypes_ExcludesPathsInsideARepeatedElement is the guard on a real
// bug: leaving them in made `--set 'criteria[].and_or=and'` succeed and emit an
// element literally named `criteria[]`.
func TestSetFieldTypes_ExcludesPathsInsideARepeatedElement(t *testing.T) {
	r := find(t, liveResources(t), "computergroups")
	for path := range r.SetFieldTypes() {
		if strings.Contains(path, "[]") {
			t.Errorf("settable path %q descends into a repeated element", path)
		}
	}
	// The array itself stays, typed, so --set can refuse it by name.
	if got := r.SetFieldTypes()["criteria"]; got != "array" {
		t.Errorf("criteria type = %q, want array", got)
	}
}

func TestSetFieldTypes_TypesScalarsFromTheSpec(t *testing.T) {
	r := find(t, liveResources(t), "computergroups")
	types := r.SetFieldTypes()
	for path, want := range map[string]string{
		"name":     "string",
		"is_smart": "boolean",
		"id":       "integer",
		"site":     "object",
	} {
		if got := types[path]; got != want {
			t.Errorf("%s type = %q, want %q", path, got, want)
		}
	}
}

func TestSetFieldTypes_DropsTheCountElement(t *testing.T) {
	r := find(t, liveResources(t), "policies")
	for path := range r.SetFieldTypes() {
		if path == "size" || strings.HasSuffix(path, ".size") {
			t.Errorf("the server-computed count element %q is settable", path)
		}
	}
}

func TestSetCompletions_OffersScalarsOnly(t *testing.T) {
	r := find(t, liveResources(t), "computergroups")
	got := r.SetCompletions()
	if !slices.Contains(got, "name=") || !slices.Contains(got, "site.name=") {
		t.Errorf("expected scalar leaves, got %v", got)
	}
	for _, c := range got {
		path := strings.TrimSuffix(c, "=")
		switch r.SetFieldTypes()[path] {
		case "object", "array", "":
			t.Errorf("completion %q is not a settable scalar", c)
		}
	}
}

// ── RepeatedElements ──────────────────────────────────────────────────────

// TestRepeatedElements_ReadsTheElementNameFromTheSchema pins the rule that the
// element name is never derived from the array's own name. 97 of the 373
// repeated elements in the Classic spec are not a naive singularisation:
// criteria holds criterion, computers holds computer.
func TestRepeatedElements_ReadsTheElementNameFromTheSchema(t *testing.T) {
	r := find(t, liveResources(t), "computergroups")
	got := r.RepeatedElements()
	for path, want := range map[string]string{
		"criteria":  "criterion",
		"computers": "computer",
	} {
		if got[path] != want {
			t.Errorf("RepeatedElements()[%q] = %q, want %q", path, got[path], want)
		}
	}
}

// ── CredentialFields ──────────────────────────────────────────────────────

// TestCredentialFields_FindsEveryClassicSecret covers the resources the
// credential policy is about. A Classic body is where an SMTP, LDAP,
// distribution-point or GSX password lives, and none of them is marked
// writeOnly in the spec, so the field name is the only signal.
func TestCredentialFields_FindsEveryClassicSecret(t *testing.T) {
	res := liveResources(t)
	for _, tc := range []struct{ resource, field string }{
		{"distributionpoints", "read_write_password"},
		{"distributionpoints", "http_password"},
		{"smtpserver", "password"},
		{"ldapservers", "connection.account.password"},
	} {
		got := find(t, res, tc.resource).CredentialFields()
		if !slices.Contains(got, tc.field) {
			t.Errorf("%s: %q not refused for --set; have %v", tc.resource, tc.field, got)
		}
	}
}

// TestCredentialFields_SkipsABooleanNamedLikeAPassword is the guard on the
// over-match the first version shipped: a distribution point declares
// username_password_required, a switch whose name contains "password" and whose
// value is not one. Refusing --set on it blocks a legitimate setting.
func TestCredentialFields_SkipsABooleanNamedLikeAPassword(t *testing.T) {
	r := find(t, liveResources(t), "distributionpoints")
	if slices.Contains(r.CredentialFields(), "username_password_required") {
		t.Error("username_password_required is a boolean switch, not a credential")
	}
	// It must still be settable.
	if got := r.SetFieldTypes()["username_password_required"]; got != "boolean" {
		t.Errorf("username_password_required type = %q, want boolean", got)
	}
}

// TestCredentialFieldsAreNeverSettable ties the two halves together: a field the
// generator names as a credential must not also appear in --set completion, or
// the shell would suggest exactly the thing the policy forbids.
func TestCredentialFieldsAreNeverSettableViaCompletion(t *testing.T) {
	for _, r := range liveResources(t) {
		if !r.HasBodySchema() {
			continue
		}
		creds := r.CredentialFields()
		for _, c := range r.SetCompletions() {
			if slices.Contains(creds, strings.TrimSuffix(c, "=")) {
				t.Errorf("%s: completion offers credential field %q", r.Name, c)
			}
		}
	}
}

// ── Guards over the whole shipped set ─────────────────────────────────────

// TestEveryBoundResourceScaffoldsWellFormedXML is the broad guard. A scaffold is
// pasted into a file and sent, so malformed output is a 409 the caller cannot
// diagnose — and the failure would be silent at generation time.
func TestEveryBoundResourceScaffoldsWellFormedXML(t *testing.T) {
	var checked int
	for _, r := range liveResources(t) {
		if !r.HasBodySchema() {
			continue
		}
		got, err := r.ScaffoldXML()
		if err != nil {
			t.Errorf("%s: %v", r.Name, err)
			continue
		}
		if !strings.HasPrefix(got, "<"+r.BodyRoot+">") {
			t.Errorf("%s: scaffold does not open with <%s>:\n%s", r.Name, r.BodyRoot, first(got, 120))
		}
		var v any
		if err := xml.Unmarshal([]byte(got), &v); err != nil {
			t.Errorf("%s: scaffold is not well-formed XML: %v\n%s", r.Name, err, first(got, 400))
		}
		if strings.Contains(got, "<>") || strings.Contains(got, "[]>") {
			t.Errorf("%s: scaffold contains an invalid element name:\n%s", r.Name, first(got, 400))
		}
		checked++
	}
	if checked < 40 {
		t.Errorf("only %d resources scaffolded; the artifact binds 43", checked)
	}
}

// TestNoBoundResourceCarriesASemanticSizeField is the guard the ClassicIsCountElement
// doc comment promises.
//
// The name `size` is overloaded in the Classic spec: beside a repeated element it
// is a server-computed counter to drop, but computer_post's
// hardware.storage[].device.size and the partition.size beneath it are physical
// capacities in MB. Neither is reachable today, because no Classic resource in
// the manifest binds computer_post. If an ingest ever binds one, this fails and
// the count discriminator has to be read off the spec again rather than guessed
// harder.
func TestNoBoundResourceCarriesASemanticSizeField(t *testing.T) {
	for _, r := range liveResources(t) {
		if !r.HasBodySchema() {
			continue
		}
		walkSchema(r.BodySchema, "", func(path string, s *parser.Schema) {
			prop := s.Properties[parser.ClassicCountElement]
			if prop == nil {
				return
			}
			if parser.ClassicIsCountElement(parser.ClassicCountElement, prop, s) {
				return
			}
			t.Errorf("%s: %s carries a %q that is not a collection counter (example %v) — "+
				"the discriminator in parser.ClassicIsCountElement needs revisiting",
				r.Name, path, parser.ClassicCountElement, prop.Example)
		})
	}
}

// TestEveryBoundResourceHasAWriteOperation pins the artifact's scope. It
// describes request bodies, so binding a read-only resource is a defect, not
// spare information: /accounts and /patchavailabletitles/sourceid/{id} answer
// with the plural `accounts` and `patch_available_titles` schemas, which are
// list wrappers and not the shape of anything a caller would send.
func TestEveryBoundResourceHasAWriteOperation(t *testing.T) {
	for _, r := range liveResources(t) {
		if !r.HasBodySchema() {
			continue
		}
		if !r.HasOperation("create") && !r.HasOperation("update") {
			t.Errorf("%s has a body schema but no create or update", r.Name)
		}
	}
}

// TestBodyRootIsNeverASchemaArtefact guards the "_post" suffix strip and any
// future naming convention that leaks into an element name.
func TestBodyRootIsNeverASchemaArtefact(t *testing.T) {
	for _, r := range liveResources(t) {
		if !r.HasBodySchema() {
			continue
		}
		if strings.HasSuffix(r.BodyRoot, "_post") {
			t.Errorf("%s: BodyRoot %q is a schema key, not an XML element name", r.Name, r.BodyRoot)
		}
		if r.BodyRoot == "" {
			t.Errorf("%s: empty BodyRoot", r.Name)
		}
	}
}

// TestSetFieldTypesAndScaffoldAgree checks that every path --set accepts is one
// --scaffold shows. They are rendered by different walks over the same schema,
// so a divergence means the help teaches a field the body builder rejects, or
// the reverse.
func TestSetFieldTypesAndScaffoldAgree(t *testing.T) {
	for _, r := range liveResources(t) {
		if !r.HasBodySchema() {
			continue
		}
		scaffold, err := r.ScaffoldXML()
		if err != nil {
			t.Fatalf("%s: %v", r.Name, err)
		}
		for path, kind := range r.SetFieldTypes() {
			if kind == "array" || kind == "object" {
				continue
			}
			leaf := path
			if i := strings.LastIndex(path, "."); i >= 0 {
				leaf = path[i+1:]
			}
			if !strings.Contains(scaffold, "<"+leaf+">") {
				t.Errorf("%s: --set accepts %q but --scaffold never shows <%s>", r.Name, path, leaf)
			}
		}
	}
}

// TestBodyHelpNamesRequiredAndEnums checks the rendered help tail, since that is
// the artifact's whole user-visible payoff.
func TestBodyHelpNamesRequiredAndEnums(t *testing.T) {
	r := find(t, liveResources(t), "computergroups")
	help := bodyHelp(r)
	for _, want := range []string{
		`schema "computer_group_post"`,
		"--scaffold",
		"Required: is_smart, name",
		"Allowed values:",
		"criteria[].and_or: and | or",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("bodyHelp missing %q:\n%s", want, help)
		}
	}
}

// TestRuntimeEnumsExcludeRepeatedElementPaths pins a deliberate divergence
// between what --help documents and what the binary carries.
//
// EnumChoices reports criteria[].and_or so --help can name the values a
// criterion accepts. --set can never receive that path — it cannot say which
// member of a repeated element it means, and refuses it by name — so carrying
// the values into the emitted literal would be dead weight in every one of the
// 43 bound resources.
func TestRuntimeEnumsExcludeRepeatedElementPaths(t *testing.T) {
	r := find(t, liveResources(t), "computergroups")

	if !strings.Contains(bodyHelp(r), "criteria[].and_or: and | or") {
		t.Error("--help should document the enum inside the repeated element")
	}
	if strings.Contains(bodySpecLiteral(r), "criteria[].and_or") {
		t.Error("the emitted literal should not carry an enum --set cannot receive")
	}
}

func TestBodyHelpIsEmptyWithoutASchema(t *testing.T) {
	if got := bodyHelp(ClassicResource{Name: "x"}); got != "" {
		t.Errorf("expected empty help for an unbound resource, got %q", got)
	}
	if got := bodySpecLiteral(ClassicResource{Name: "x"}); got != "" {
		t.Errorf("expected no literal for an unbound resource, got %q", got)
	}
}

// TestBodySpecLiteralCompilesAsAGoLiteral is a cheap shape check on the emitted
// source: the generated file will not compile if the literal is malformed, but
// that failure arrives as a wall of syntax errors in generated code rather than
// as a named test.
func TestBodySpecLiteralCompilesAsAGoLiteral(t *testing.T) {
	res := liveResources(t)
	r := find(t, res, "computergroups")
	got := bodySpecLiteral(r)
	for _, want := range []string{
		"classicBodySpec{",
		`Root:   "computer_group"`,
		`Schema: "computer_group_post"`,
		"FieldTypes: map[string]string{",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("literal missing %q:\n%s", want, first(got, 600))
		}
	}
	// A resource with a settable enum gets the Enums map; computergroups' only
	// enum lives inside a repeated element, which is asserted separately below.
	dp := bodySpecLiteral(find(t, res, "distributionpoints"))
	for _, want := range []string{"Enums: map[string][]string{", `"connection_type": {"SMB", "AFP"}`, "Credentials: map[string]bool{"} {
		if !strings.Contains(dp, want) {
			t.Errorf("distributionpoints literal missing %q:\n%s", want, first(dp, 900))
		}
	}
	if strings.Count(got, "{") != strings.Count(got, "}") {
		t.Errorf("unbalanced braces in the emitted literal:\n%s", got)
	}
	if strings.Contains(got, "ERROR rendering scaffold") {
		t.Errorf("scaffold rendering failed:\n%s", first(got, 300))
	}
}

// TestBackquoteFallsBackWhenContentHasABackquote guards the raw-string literal
// the scaffold is emitted as. A backquote in the content would terminate it and
// break the generated file — the same way a backquote in a template comment
// broke the registry template during development.
func TestBackquoteFallsBackWhenContentHasABackquote(t *testing.T) {
	if got := backquote("plain"); got != "`plain`" {
		t.Errorf("got %q, want a raw literal", got)
	}
	// A backquote needs no escaping inside an interpreted literal, so the
	// fallback is a plain strconv.Quote — what matters is that the output is not
	// a raw literal, which the content would terminate.
	got := backquote("has ` inside")
	if strings.HasPrefix(got, "`") {
		t.Errorf("got %q, want an interpreted literal", got)
	}
	if !strings.HasPrefix(got, `"`) || !strings.Contains(got, "` inside") {
		t.Errorf("got %q, want a quoted literal preserving the content", got)
	}
}

func TestArtifactPathIsUnderTheClassicSpecDir(t *testing.T) {
	// The artifact lives beside resources.yaml so the two move together, and its
	// name says what it is. Pinned because the Makefile's verify target and the
	// generator both hard-code the relationship.
	if got := filepath.ToSlash(classicschema.ArtifactFile); got != "classic/schemas.json" {
		t.Errorf("ArtifactFile = %q", got)
	}
}

func first(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
