// Copyright 2026, Jamf Software LLC

package classicschema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSpec writes a minimal SDK-shaped Classic spec into a temp drop dir.
func writeSpec(t *testing.T, spec map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, SourceFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func getOp(schema string) map[string]any {
	return map[string]any{
		"get": map[string]any{
			"responses": map[string]any{
				"200": map[string]any{"content": map[string]any{
					"application/xml": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/" + schema}},
				}},
			},
		},
	}
}

func TestExtract_BindsAResourceFromItsDetailGet(t *testing.T) {
	dir := writeSpec(t, map[string]any{
		"info":  map[string]any{"title": "Classic API", "version": "11.28.0"},
		"paths": map[string]any{"/policies/id/{id}": getOp("policy")},
		"components": map[string]any{"schemas": map[string]any{
			"policy": map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}},
		}},
	})

	art, warnings, err := Extract(dir, "abc1234", []ManifestEntry{{Name: "policies", Path: "policies", Singular: "policy"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	res := art.Resources["policies"]
	if res == nil {
		t.Fatal("policies not bound")
	}
	if res.Schema != "policy" || res.Root != "policy" {
		t.Errorf("got schema %q root %q, want both %q", res.Schema, res.Root, "policy")
	}
	if res.From != "GET /policies/id/{id}" {
		t.Errorf("From = %q, want the detail GET it was read from", res.From)
	}
	if art.Source.SDKCommit != "abc1234" {
		t.Errorf("SDKCommit = %q, want the revision passed in", art.Source.SDKCommit)
	}
	if _, hasPaths := any(art).(interface{ Paths() any }); hasPaths {
		t.Error("the artifact must carry no paths")
	}
}

// TestExtract_PrefersTheWriteShapedPostSchema pins the reason the "_post"
// swap exists. computer_group declares no top-level required list;
// computer_group_post declares [name, is_smart], and both are genuinely required
// on the wire — a create omitting either answers 409. Reading the base schema
// alone shipped a --help that named neither.
func TestExtract_PrefersTheWriteShapedPostSchema(t *testing.T) {
	dir := writeSpec(t, map[string]any{
		"info":  map[string]any{"title": "Classic API", "version": "11.28.0"},
		"paths": map[string]any{"/computergroups/id/{id}": getOp("computer_group")},
		"components": map[string]any{"schemas": map[string]any{
			"computer_group": map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}},
			"computer_group_post": map[string]any{
				"type":       "object",
				"required":   []any{"name", "is_smart"},
				"xml":        map[string]any{"name": "computer_group"},
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
			},
		}},
	})

	art, _, err := Extract(dir, "", []ManifestEntry{{Name: "computergroups", Path: "computergroups", Singular: "computer_group"}})
	if err != nil {
		t.Fatal(err)
	}
	res := art.Resources["computergroups"]
	if res.Schema != "computer_group_post" {
		t.Errorf("Schema = %q, want the write-shaped sibling", res.Schema)
	}
	if res.Root != "computer_group" {
		t.Errorf("Root = %q, want the element name from xml.name, not the schema key", res.Root)
	}
}

// TestExtract_StripsThePostSuffixWhenNoXMLNameIsDeclared covers the three "_post"
// schemas that declare no xml.name — ldap_server_post,
// mobile_device_invitation_post and user_post. Without the fallback a request
// body would be wrapped in <ldap_server_post>, an element the Classic API has
// never heard of.
func TestExtract_StripsThePostSuffixWhenNoXMLNameIsDeclared(t *testing.T) {
	dir := writeSpec(t, map[string]any{
		"info":  map[string]any{"title": "Classic API", "version": "11.28.0"},
		"paths": map[string]any{"/ldapservers/id/{id}": getOp("ldap_server")},
		"components": map[string]any{"schemas": map[string]any{
			"ldap_server":      map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}},
			"ldap_server_post": map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}},
		}},
	})

	art, warnings, err := Extract(dir, "", []ManifestEntry{{Name: "ldapservers", Path: "ldapservers", Singular: "ldap_server"}})
	if err != nil {
		t.Fatal(err)
	}
	res := art.Resources["ldapservers"]
	if res.Schema != "ldap_server_post" {
		t.Fatalf("Schema = %q, want the _post sibling", res.Schema)
	}
	if res.Root != "ldap_server" {
		t.Errorf("Root = %q, want the _post suffix stripped", res.Root)
	}
	if len(warnings) != 0 {
		t.Errorf("stripping the suffix should agree with the manifest, got warnings: %v", warnings)
	}
}

// TestExtract_WarnsWhenTheManifestSingularDisagrees covers the two real
// disagreements in the shipped manifest. Wire-checked 2026-09-02: an account
// group's XML root is <group> and an account user's is <account>, so the spec is
// right and the manifest's invented account_group/account_user are not. Reported
// rather than silently resolved, because two sources of one fact disagreeing is
// worth a human looking at.
func TestExtract_WarnsWhenTheManifestSingularDisagrees(t *testing.T) {
	dir := writeSpec(t, map[string]any{
		"info":  map[string]any{"title": "Classic API", "version": "11.28.0"},
		"paths": map[string]any{"/accounts/groupid/{id}": getOp("group")},
		"components": map[string]any{"schemas": map[string]any{
			"group": map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}},
		}},
	})

	art, warnings, err := Extract(dir, "", []ManifestEntry{
		{Name: "account-groups", Path: "accounts", Singular: "account_group", IDPath: "groupid"},
	})
	if err != nil {
		t.Fatal(err)
	}
	res := art.Resources["account-groups"]
	if res.Root != "group" {
		t.Errorf("Root = %q, want the spec's %q — it is the element the server sends", res.Root, "group")
	}
	if res.SingularAgrees {
		t.Error("SingularAgrees should be false")
	}
	if res.Singular != "account_group" {
		t.Errorf("Singular = %q, want the manifest value recorded", res.Singular)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "account_group") || !strings.Contains(warnings[0], "group") {
		t.Errorf("expected one warning naming both values, got %v", warnings)
	}
}

func TestExtract_ReportsAnUnresolvedResourceWithoutFailing(t *testing.T) {
	// computerconfigurations and patchsoftwaretitles are in this state today,
	// both because the Classic API withdrew what they read. A resource with no
	// schema ships without body help, which is where every Classic resource was
	// before this package existed — not an error.
	dir := writeSpec(t, map[string]any{
		"info":       map[string]any{"title": "Classic API", "version": "11.28.0"},
		"paths":      map[string]any{},
		"components": map[string]any{"schemas": map[string]any{"policy": map[string]any{"type": "object"}}},
	})

	art, warnings, err := Extract(dir, "", []ManifestEntry{{Name: "computerconfigurations", Path: "computerconfigurations", Singular: "computer_configuration"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(art.Resources) != 0 {
		t.Errorf("expected no binding, got %v", art.Resources)
	}
	if art.Source.Unresolved != 1 {
		t.Errorf("Unresolved = %d, want 1", art.Source.Unresolved)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "computerconfigurations") {
		t.Errorf("expected a warning naming the resource, got %v", warnings)
	}
}

func TestExtract_HonoursTheManifestIDPathOverride(t *testing.T) {
	// account-users reaches its detail endpoint at /accounts/userid/{id}, not
	// /accounts/id/{id} — so a candidate-path list built without the override
	// binds nothing.
	dir := writeSpec(t, map[string]any{
		"info":  map[string]any{"title": "Classic API", "version": "11.28.0"},
		"paths": map[string]any{"/accounts/userid/{id}": getOp("account")},
		"components": map[string]any{"schemas": map[string]any{
			"account": map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}},
		}},
	})

	art, _, err := Extract(dir, "", []ManifestEntry{{Name: "account-users", Path: "accounts", Singular: "account", IDPath: "userid"}})
	if err != nil {
		t.Fatal(err)
	}
	if art.Resources["account-users"] == nil {
		t.Fatal("account-users not bound; the id_path override was ignored")
	}
}

func TestExtract_IgnoresANonSuccessResponseSchema(t *testing.T) {
	// A 4xx body is an error envelope, not the resource.
	dir := writeSpec(t, map[string]any{
		"info": map[string]any{"title": "Classic API", "version": "11.28.0"},
		"paths": map[string]any{"/policies/id/{id}": map[string]any{
			"get": map[string]any{"responses": map[string]any{
				"409": map[string]any{"content": map[string]any{"application/xml": map[string]any{
					"schema": map[string]any{"$ref": "#/components/schemas/error"},
				}}},
				"200": map[string]any{"content": map[string]any{"application/xml": map[string]any{
					"schema": map[string]any{"$ref": "#/components/schemas/policy"},
				}}},
			}},
		}},
		"components": map[string]any{"schemas": map[string]any{
			"error":  map[string]any{"type": "object"},
			"policy": map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}},
		}},
	})

	art, _, err := Extract(dir, "", []ManifestEntry{{Name: "policies", Path: "policies", Singular: "policy"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := art.Resources["policies"].Schema; got != "policy" {
		t.Errorf("Schema = %q, want policy — a 4xx envelope was preferred", got)
	}
}

func TestExtract_MissingSourceFileIsAnError(t *testing.T) {
	if _, _, err := Extract(t.TempDir(), "", nil); err == nil {
		t.Fatal("expected an error when the SDK spec is absent")
	}
}

func TestExtract_SpecWithNoSchemasIsAnError(t *testing.T) {
	// A truncated or wrong-file drop must fail loudly. Binding nothing silently
	// would regenerate every Classic command without --scaffold or --set while
	// `make generate` exits 0.
	dir := writeSpec(t, map[string]any{
		"info":       map[string]any{"title": "Classic API", "version": "11.28.0"},
		"paths":      map[string]any{},
		"components": map[string]any{"schemas": map[string]any{}},
	})
	if _, _, err := Extract(dir, "", nil); err == nil {
		t.Fatal("expected an error for a spec declaring no component schemas")
	}
}

func TestLoad_MissingArtifactIsNotAnError(t *testing.T) {
	art, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("a missing artifact is the unknown answer, not an error: %v", err)
	}
	if art != nil {
		t.Error("expected a nil artifact")
	}
}

func TestWriteAndLoad_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "schemas.json")
	want := &Artifact{
		OpenAPI:    "3.0.3",
		Info:       Info{Title: "t", Version: "11.28.0"},
		Source:     Source{Spec: SourceFile, SDKCommit: "deadbee"},
		Resources:  map[string]*Res{"policies": {Schema: "policy_post", Root: "policy", SingularAgrees: true}},
		Components: Components{Schemas: map[string]json.RawMessage{"policy_post": json.RawMessage(`{"type":"object"}`)}},
	}
	if err := Write(want, path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Resources["policies"].Schema != "policy_post" || got.Source.SDKCommit != "deadbee" {
		t.Errorf("round trip lost data: %#v", got.Resources["policies"])
	}
	if _, ok := got.Components.Schemas["policy_post"]; !ok {
		t.Error("round trip lost the schema map")
	}
}

// TestWrite_IsByteStable matters because make verify-classic-schemas compares
// the re-derived artifact against the committed one with git status. Unstable
// key order would report a stale artifact on every run.
func TestWrite_IsByteStable(t *testing.T) {
	art := &Artifact{
		OpenAPI:   "3.0.3",
		Resources: map[string]*Res{"b": {Schema: "b"}, "a": {Schema: "a"}, "c": {Schema: "c"}},
		Components: Components{Schemas: map[string]json.RawMessage{
			"z": json.RawMessage(`{}`), "a": json.RawMessage(`{}`), "m": json.RawMessage(`{}`),
		}},
	}
	dir := t.TempDir()
	first := filepath.Join(dir, "a.json")
	second := filepath.Join(dir, "b.json")
	if err := Write(art, first); err != nil {
		t.Fatal(err)
	}
	if err := Write(art, second); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(first)
	b, _ := os.ReadFile(second)
	if string(a) != string(b) {
		t.Error("two writes of the same artifact differ")
	}
}

// TestCarryForwardProvenance_KeepsARevisionThisRunWasNotTold mirrors
// gateway.CarryForwardProvenance. Without it, re-deriving from unchanged specs
// with no SDK path blanks the recorded revision, and the verify target then
// reports a stale artifact that differs only in the field it just erased.
func TestCarryForwardProvenance_KeepsARevisionThisRunWasNotTold(t *testing.T) {
	fresh := &Artifact{Source: Source{}}
	prev := &Artifact{Source: Source{SDKCommit: "c91fce8"}}
	CarryForwardProvenance(fresh, prev)
	if fresh.Source.SDKCommit != "c91fce8" {
		t.Errorf("SDKCommit = %q, want the previous revision carried forward", fresh.Source.SDKCommit)
	}

	told := &Artifact{Source: Source{SDKCommit: "newrev"}}
	CarryForwardProvenance(told, prev)
	if told.Source.SDKCommit != "newrev" {
		t.Errorf("SDKCommit = %q, want this run's own revision to win", told.Source.SDKCommit)
	}

	// Must not panic on either being absent.
	CarryForwardProvenance(nil, prev)
	CarryForwardProvenance(fresh, nil)
}
