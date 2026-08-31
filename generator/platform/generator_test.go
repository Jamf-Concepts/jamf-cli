// Copyright 2026, Jamf Software LLC

package platform

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/generator/parser"
)

// TestLoadResources_LiveSpecs runs the orchestrator against the committed
// specs/platform/ tree and asserts the merged resource set is well-formed.
// This catches regressions in spec ingest, tenant stripping, service
// prepending, tag grouping, and collision-renaming.
func TestLoadResources_LiveSpecs(t *testing.T) {
	specsDir, err := filepath.Abs("../../specs/platform")
	if err != nil {
		t.Fatalf("resolving specs dir: %v", err)
	}

	resources, files, err := LoadResources(specsDir)
	if err != nil {
		t.Fatalf("LoadResources: %v", err)
	}
	if len(resources) == 0 {
		t.Fatal("expected resources, got 0 — is specs/platform/ populated?")
	}
	if len(files) == 0 {
		t.Fatal("LoadResources returned no consumed spec files")
	}
	for i := 1; i < len(files); i++ {
		if files[i-1] >= files[i] {
			t.Errorf("consumed files not sorted: %q >= %q", files[i-1], files[i])
		}
	}

	seenNames := make(map[string]bool, len(resources))
	for _, r := range resources {
		if r.Name == "" {
			t.Errorf("resource with empty Name (GoName=%s)", r.GoName)
		}
		if r.GoName == "" {
			t.Errorf("resource %q missing GoName", r.Name)
		}
		if len(r.Operations) == 0 {
			t.Errorf("resource %q has no operations", r.Name)
		}
		if seenNames[r.Name] {
			t.Errorf("duplicate resource name %q", r.Name)
		}
		seenNames[r.Name] = true

		for _, op := range r.Operations {
			if op.Name == "" {
				t.Errorf("%s: op with empty name (method=%s path=%s)", r.Name, op.Method, op.Path)
			}
			if op.Method == "" {
				t.Errorf("%s/%s: op with empty method", r.Name, op.Name)
			}
			// Every emitted path must include /tenant/{tenantId}/ once we
			// re-add it during template build. At parse time the prefix is
			// stripped, so the path should NOT contain "/tenant/".
			if strings.Contains(op.Path, "/tenant/{tenantId}") {
				t.Errorf("%s/%s: parser-stage path still contains tenant placeholder: %s", r.Name, op.Name, op.Path)
			}
			assertNamespacePrefixed(t, r.Name+"/"+op.Name, op.Path)
		}
	}

	// Check the well-known collision rename: platform spec tag "users" must
	// map to "platform-users" so it doesn't collide with Pro's users.
	if seenNames["users"] {
		t.Errorf(`tag "users" must be renamed to "platform-users"`)
	}
	// And the renamed form must be present (assuming the spec carries that tag).
	// device-inventory-api.json defines a /users/{id}/devices endpoint with
	// tag "users", so the merged set should always include platform-users.
	if !seenNames["platform-users"] {
		t.Logf("platform-users not present — may indicate spec change, not a hard failure")
	}
}

// TestExtractPathParams covers placeholder extraction order.
func TestExtractPathParams(t *testing.T) {
	cases := []struct {
		path string
		want []string
	}{
		{"/v1/foo", nil},
		{"/v1/foo/{id}", []string{"id"}},
		{"/v1/foo/{id}/bar/{ruleId}", []string{"id", "ruleId"}},
		{"/x/v1/tenant/{tenantId}/y/{id}", []string{"tenantId", "id"}},
	}
	for _, c := range cases {
		got := extractPathParams(c.path)
		if len(got) != len(c.want) {
			t.Errorf("extractPathParams(%q) len = %d, want %d", c.path, len(got), len(c.want))
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("extractPathParams(%q)[%d] = %q, want %q", c.path, i, got[i], c.want[i])
			}
		}
	}
}

// TestGenerate_EmitsPrivilegeAnnotation verifies the platform generator emits
// the jamf:privileges annotation for ops that declare x-required-privileges
// (6 platform specs / 43 occurrences carry it). Generates from the live specs
// into a temp dir and asserts at least one generated command carries it.
func TestGenerate_EmitsPrivilegeAnnotation(t *testing.T) {
	specsDir, err := filepath.Abs("../../specs/platform")
	if err != nil {
		t.Fatalf("resolving specs dir: %v", err)
	}
	resources, _, err := LoadResources(specsDir)
	if err != nil {
		t.Fatalf("LoadResources: %v", err)
	}

	outDir := t.TempDir()
	if _, err := Generate(resources, outDir); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	found := false
	walkErr := filepath.Walk(outDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if strings.Contains(string(b), `"jamf:privileges"`) {
			found = true
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk: %v", walkErr)
	}
	if !found {
		t.Error("no generated platform command carries the jamf:privileges annotation — template emission missing")
	}
}

// TestPlatformTableColumns_KeyedByService guards a mix-up that shipped: two
// specs produce a resource called "device-groups" — the Jamf Pro device group
// inventory and Jamf Security Cloud's device groups. Keyed on the bare name, the
// inventory's columns (description, deviceType, groupType, memberCount) landed
// on the Security Cloud resource, which carries only id and name, so `security
// device-groups list -o csv` emitted four permanently empty columns while the
// Pro resource the columns describe rendered without any.
func TestPlatformTableColumns_KeyedByService(t *testing.T) {
	for key := range platformTableColumns {
		service, name, ok := strings.Cut(key, "/")
		if !ok {
			t.Errorf("platformTableColumns key %q is not \"{service}/{name}\" — a bare resource name is not unique across services", key)
			continue
		}
		if service == "" || name == "" {
			t.Errorf("platformTableColumns key %q has an empty service or name", key)
		}
	}

	// The pairing that was inverted, asserted both ways round.
	if _, ok := platformTableColumns["device-groups/platform-device-groups"]; !ok {
		t.Error("expected the Pro device group inventory to own the inventory columns")
	}
	if _, ok := platformTableColumns["securitycloud/device-groups"]; ok {
		t.Error("Security Cloud device groups carry only id and name; giving them the inventory columns prints empty ones")
	}
}

// TestBuildEnumChoices_NestedAndSorted covers what makes a scaffold usable for
// enum fields: the scaffold renders them as "", so the choices have to be
// written down somewhere. Security Cloud's ipsec.right.vendor is the case that
// forced it — case-sensitive, and a wrong-case value is rejected with a 400 that
// does not name the field.
func TestBuildEnumChoices_NestedAndSorted(t *testing.T) {
	specsDir, err := filepath.Abs("../../specs/platform")
	if err != nil {
		t.Fatalf("resolving specs dir: %v", err)
	}
	resources, _, err := LoadResources(specsDir)
	if err != nil {
		t.Fatalf("LoadResources: %v", err)
	}

	var found []enumChoice
	for _, r := range resources {
		if r.Name != "ztna-gateways" {
			continue
		}
		for _, op := range r.Operations {
			if op.Name != "create" {
				continue
			}
			found = buildEnumChoices(op)
		}
	}
	if len(found) == 0 {
		t.Fatal("expected enum choices on ztna-gateways create — has the IPSec schema stopped constraining vendor?")
	}

	byPath := map[string][]string{}
	for _, c := range found {
		byPath[c.Path] = c.Values
	}
	// Nested two levels deep, which a properties-only walk would miss.
	vendor, ok := byPath["ipsec.right.vendor"]
	if !ok {
		t.Fatalf("expected ipsec.right.vendor collected, got paths %v", byPath)
	}
	if len(vendor) < 2 {
		t.Errorf("expected the vendor enum's values, got %v", vendor)
	}
	// Case matters on the wire, so the values must be carried verbatim.
	var sawMixedCase bool
	for _, v := range vendor {
		if v != strings.ToLower(v) && v != strings.ToUpper(v) {
			sawMixedCase = true
		}
	}
	if !sawMixedCase {
		t.Errorf("expected the vendor values to keep their original case (e.g. \"Palo Alto\"), got %v", vendor)
	}

	for i := 1; i < len(found); i++ {
		if found[i-1].Path >= found[i].Path {
			t.Errorf("enum choices not sorted by path: %q >= %q", found[i-1].Path, found[i].Path)
		}
	}
}

// TestAppendEnumChoices covers the help-text shaping, including the no-enum case
// where the long text must be returned untouched.
func TestAppendEnumChoices(t *testing.T) {
	if got := appendEnumChoices("Some description.", nil); got != "Some description." {
		t.Errorf("expected long text unchanged with no enums, got %q", got)
	}

	got := appendEnumChoices("Create a gateway.", []enumChoice{
		{Path: "ipsec.keyExchange", Values: []string{"ikev1", "ikev2"}},
	})
	for _, want := range []string{"Create a gateway.", "Allowed values:", "ipsec.keyExchange: ikev1, ikev2"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected help to contain %q, got %q", want, got)
		}
	}

	// An op with no description still gets the choices, without a leading blank.
	bare := appendEnumChoices("", []enumChoice{{Path: "x", Values: []string{"a"}}})
	if strings.HasPrefix(bare, "\n") {
		t.Errorf("expected no leading newline when there is no description, got %q", bare)
	}
}

// TestBuildEnumChoices_ReachesArrayElements covers the half a properties-only
// walk misses. For an array the enum sits on the *element* schema, not on the
// property, so six of the ZTNA gateway's IPSec cipher-suite fields were
// constrained on the wire while the scaffold showed "[]" and the help listed
// nothing — and the server requires ipsec.esp and ipsec.ike, so anyone
// configuring IPSec has to fill them.
func TestBuildEnumChoices_ReachesArrayElements(t *testing.T) {
	specsDir, err := filepath.Abs("../../specs/platform")
	if err != nil {
		t.Fatalf("resolving specs dir: %v", err)
	}
	resources, _, err := LoadResources(specsDir)
	if err != nil {
		t.Fatalf("LoadResources: %v", err)
	}

	choices := func(resource, opName string) map[string][]string {
		out := map[string][]string{}
		for _, r := range resources {
			if r.Name != resource {
				continue
			}
			for _, op := range r.Operations {
				if op.Name != opName {
					continue
				}
				for _, c := range buildEnumChoices(op) {
					out[c.Path] = c.Values
				}
			}
		}
		return out
	}

	gw := choices("ztna-gateways", "create")
	enc, ok := gw["ipsec.esp.encryption[]"]
	if !ok {
		t.Fatalf("expected the element enum of ipsec.esp.encryption, got paths %v", keysOf(gw))
	}
	if len(enc) == 0 {
		t.Error("expected the cipher values, got none")
	}
	// The suffix is what tells a reader the constraint is per element, not on
	// the array as a whole.
	for path := range gw {
		if path == "ipsec.esp.encryption" {
			t.Error(`element enum recorded without the "[]" suffix — reads as if the array itself were the enum`)
		}
	}

	// An enum nested inside an array-of-objects element: two hops a
	// properties-only walk cannot make.
	bm := choices("benchmarks", "create")
	if _, ok := bm["selectedOsVersions[].osType"]; !ok {
		t.Errorf("expected an enum inside an array element, got paths %v", keysOf(bm))
	}
}

func keysOf(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestLoadResourcesDropsTheTenantFromEveryPath pins the scope being out of the
// URL across the whole platform surface.
//
// It matters because a leftover tenant segment fails silently in the direction
// that is hardest to notice. The gateway still routes the old shape during the
// transition window, so a generated path carrying a literal "/tenant/{tenantId}"
// would 404 only once that window closes — and until then the header and the
// path would both be sent, with nothing to say which one the gateway honoured.
func TestLoadResourcesDropsTheTenantFromEveryPath(t *testing.T) {
	specsDir, err := filepath.Abs("../../specs/platform")
	if err != nil {
		t.Fatalf("resolving specs dir: %v", err)
	}
	resources, _, err := LoadResources(specsDir)
	if err != nil {
		t.Fatalf("LoadResources: %v", err)
	}

	securityCloudOps, total := 0, 0
	for _, r := range resources {
		for _, op := range r.Operations {
			total++
			if strings.Contains(op.Path, "tenant") {
				t.Errorf("%s/%s: request path still carries a tenant segment: %s", r.Name, op.Name, op.Path)
			}
			assertNamespacePrefixed(t, r.Name+"/"+op.Name, op.Path)
			if serviceFromPath(op.Path) == securityCloudService {
				securityCloudOps++
			}
		}
	}
	if total == 0 {
		t.Fatal("no operations loaded")
	}
	// Guard against the assertion passing because the Security Cloud specs
	// stopped being loaded at all.
	if securityCloudOps == 0 {
		t.Error("no Security Cloud operations loaded; the specs are not being read")
	}
}

// assertNamespacePrefixed checks a generated request path is mounted under its
// gateway namespace and carries no /api segment.
//
// Both halves matter. The GA gateway at {region}.api.jamfcloud.com mounts every
// namespace at the root and answers 404 "page not found" — the
// unknown-namespace tell — for anything under /api, so an /api prefix is a dead
// path. And a path whose first segment is a version means serviceSegment read
// no namespace off servers[0].url: it returns "" on a URL shape it does not
// recognise, the prefix then collapses to nothing, and every command in that
// spec 404s with no error raised anywhere in the build.
func assertNamespacePrefixed(t *testing.T, who, path string) {
	t.Helper()
	if strings.HasPrefix(path, "/api/") || path == "/api" {
		t.Errorf("%s: path is under /api, which the GA gateway does not serve: %s", who, path)
		return
	}
	service := serviceFromPath(path)
	if service == "" {
		t.Errorf("%s: path carries no namespace segment: %s", who, path)
		return
	}
	if len(service) >= 2 && service[0] == 'v' && service[1] >= '0' && service[1] <= '9' {
		t.Errorf("%s: path starts with a version, so no namespace was read from servers[0].url: %s", who, path)
	}
}

// TestDeviceGroupsUpdateStaysOnV1 asserts, against the committed specs, that
// `security device-groups update` is served by the v1 PUT and that the v2 PUT
// ships as no command at all.
//
// This is the end-to-end half of parser.TestDropUnroutedPlatformOps. Build
// v1865 declares PUT /v2/groups/{groupId} as the successor to the v1 PUT it
// deprecates, and the gateway does not route it — 403 BAD_PERMISSIONS 7/7,
// probed 2026-08-29 against a group created and deleted in the same run, with a
// v1 write on the same group as the control. deduplicateVersionedOps would
// prefer v2 by its own correct rule and FallbackPaths is no escape (populated
// for GETs and DELETEs only, and ignored by the platform template on purpose),
// so a working command would become a permanent 403 reading as a missing
// privilege.
//
// The v2 *list* is routed and is what list uses, so this also pins that the
// withholding is per-operation rather than a blanket "hold device groups at v1".
func TestDeviceGroupsUpdateStaysOnV1(t *testing.T) {
	specsDir, err := filepath.Abs("../../specs/platform")
	if err != nil {
		t.Fatalf("resolving specs dir: %v", err)
	}
	resources, _, err := LoadResources(specsDir)
	if err != nil {
		t.Fatalf("LoadResources: %v", err)
	}

	var groups *parser.Resource
	for _, r := range resources {
		if r.Name == "device-groups" && strings.Contains(r.Operations[0].Path, "/securitycloud/") {
			groups = r
			break
		}
	}
	if groups == nil {
		t.Fatal("no securitycloud device-groups resource — did the spec or its resource name move?")
	}

	var update, list *parser.Operation
	for _, op := range groups.Operations {
		if op.Path == "/securitycloud/v2/groups/{groupId}" {
			t.Errorf("op %q ships on the unrouted v2 PUT (%s %s)", op.Name, op.Method, op.Path)
		}
		switch op.Name {
		case "update":
			update = op
		case "list":
			list = op
		}
	}

	if update == nil {
		t.Fatal("device-groups has no update op")
	}
	if update.Path != "/securitycloud/v1/groups/{groupId}" {
		t.Errorf("update Path = %q, want the routed v1 path", update.Path)
	}
	if list == nil {
		t.Fatal("device-groups has no list op")
	}
	if list.Path != "/securitycloud/v2/groups" {
		t.Errorf("list Path = %q, want the routed v2 path — the v2 list is not withheld", list.Path)
	}
}

// TestPlatformTableColumnKeys asserts every key in platformTableColumns matches
// a real list operation in the committed specs.
//
// A key that matches nothing is invisible: the resource's list simply emits raw
// JSON, exactly as it would if no entry existed, so the entry looks configured
// and does nothing. That is how the AI Governance columns were first written
// wrong — the key was built from serviceFromPath, which returns only the first
// path segment, so a three-segment namespace produced "ai/ai-policies" instead
// of "ai/governance/policies/ai-policies".
func TestPlatformTableColumnKeys(t *testing.T) {
	specsDir, err := filepath.Abs("../../specs/platform")
	if err != nil {
		t.Fatalf("resolving specs dir: %v", err)
	}
	resources, _, err := LoadResources(specsDir)
	if err != nil {
		t.Fatalf("LoadResources: %v", err)
	}

	live := make(map[string]bool)
	for _, r := range resources {
		for _, op := range r.Operations {
			if op.Name == "list" {
				live[namespaceFromPath(op.Path)+"/"+r.Name] = true
			}
		}
	}

	for key := range platformTableColumns {
		if !live[key] {
			t.Errorf("platformTableColumns key %q matches no list operation — the columns are "+
				"silently unused; check the namespace against namespaceFromPath", key)
		}
	}
}

// TestNamespaceFromPath pins the whole-namespace read, including the
// multi-segment shapes that broke the first-segment implementation.
func TestNamespaceFromPath(t *testing.T) {
	cases := []struct{ path, want string }{
		{"/blueprints/v1/blueprints", "blueprints"},
		{"/securitycloud/v1/groups", "securitycloud"},
		{"/securitycloud/uem-connect/v1/connectors", "securitycloud/uem-connect"},
		{"/ai/governance/policies/v1/policies", "ai/governance/policies"},
		{"/ddm/report/v1/declaration-status", "ddm/report"},
		{"/devices", "devices"},
		{"relative/v1/x", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := namespaceFromPath(c.path); got != c.want {
			t.Errorf("namespaceFromPath(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}
