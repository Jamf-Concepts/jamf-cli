// Copyright 2026, Jamf Software LLC

package privileges

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectGroupsActionsPerCapability(t *testing.T) {
	reqs := Collect([]string{"device-groups:read", "device-groups:create", "device-groups:delete", "device-groups:update"})
	if len(reqs) != 1 {
		t.Fatalf("got %d rows, want 1: %+v", len(reqs), reqs)
	}
	got := reqs[0].String()
	// The picker's own action order, not alphabetical: an operator reads the
	// four checkboxes as CRUD.
	want := "Inventory > Device groups: Create, Read, Update, Delete"
	if !strings.HasPrefix(got, want) {
		t.Errorf("row = %q, want prefix %q", got, want)
	}
}

func TestCollectSortsBySectionThenPermission(t *testing.T) {
	reqs := Collect([]string{"destructive-device-actions:execute", "devices:read", "categories:read"})
	var order []string
	for _, r := range reqs {
		order = append(order, r.Category+"/"+r.Permission)
	}
	want := []string{
		"Device actions/Destructive device actions",
		"Inventory/Devices",
		"Organizational context/Categories",
	}
	if len(order) != len(want) {
		t.Fatalf("rows = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, order[i], want[i])
		}
	}
}

// A capability with no catalogue row still has to be reported: the operator has
// to grant it either way, and dropping it would describe an integration that
// cannot make the call.
func TestCollectKeepsAnUnknownCapabilityAndSaysSo(t *testing.T) {
	reqs := Collect([]string{"devices:read", "not-a-real-capability:read"})
	if len(reqs) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(reqs), reqs)
	}
	last := reqs[len(reqs)-1]
	if !last.Unknown {
		t.Fatalf("unknown capability did not sort last: %+v", reqs)
	}
	if !strings.Contains(last.String(), "not-a-real-capability:read") {
		t.Errorf("row = %q, want the slug rendered verbatim", last.String())
	}
	if !strings.Contains(last.String(), "no permission name recorded") {
		t.Errorf("row = %q, want it to say the name is unknown", last.String())
	}
}

// The retired three-part beta slug must not be cut at the first colon: doing so
// yields the capability "create", which names a permission that does not exist.
func TestCollectRefusesToParseARetiredBetaSlug(t *testing.T) {
	reqs := Collect([]string{"create:pro:buildings"})
	if len(reqs) != 1 {
		t.Fatalf("got %d rows, want 1: %+v", len(reqs), reqs)
	}
	if !reqs[0].Unknown {
		t.Errorf("row = %+v, want it flagged unknown", reqs[0])
	}
	if got := reqs[0].String(); !strings.Contains(got, "create:pro:buildings") {
		t.Errorf("row = %q, want the whole slug verbatim", got)
	}
	if got := reqs[0].Permission; got == "create" {
		t.Error("parsed the beta slug's action as its capability")
	}
}

func TestHintEmptyForNoScopes(t *testing.T) {
	if got := Hint(nil); got != "" {
		t.Errorf("Hint(nil) = %q, want empty", got)
	}
	if got := Hint([]string{"", "   "}); got != "" {
		t.Errorf("Hint(blank) = %q, want empty", got)
	}
}

func TestHintNamesJamfAccountAndLinksTheArticle(t *testing.T) {
	got := Hint([]string{"blueprints:read"})
	for _, want := range []string{"Jamf Account", "Deployment > Blueprints: Read", "blueprints:read", permissionsMapURL} {
		if !strings.Contains(got, want) {
			t.Errorf("Hint() = %q, missing %q", got, want)
		}
	}
	if strings.Contains(got, "\n") {
		t.Error("Hint() must stay one line: it is printed as a single hint: line")
	}
}

// The catalogue is the only artefact carrying slug → permission name, so the
// guard is that every slug this CLI can print has a row. A slug arriving from a
// spec with no row renders as "no permission name recorded", which is a worse
// answer than the article's own words.
func TestCatalogueCoversEveryScopeThisCLISends(t *testing.T) {
	root := repoRoot(t)
	missing := map[string][]string{}

	for slug, where := range scopesFromGatewayManifest(t, root) {
		if _, ok := catalogue[slug]; !ok {
			missing[slug] = append(missing[slug], where)
		}
	}
	for slug, where := range scopesFromPlatformSpecs(t, root) {
		if _, ok := catalogue[slug]; !ok {
			missing[slug] = append(missing[slug], where)
		}
	}
	for slug, where := range missing {
		t.Errorf("capability %q has no catalogue row (required by %s) — add it from %s",
			slug, strings.Join(where, ", "), permissionsMapURL)
	}
}

// scopesFromGatewayManifest returns capability → an example path requiring it,
// from the committed gateway coverage manifest.
func scopesFromGatewayManifest(t *testing.T, root string) map[string]string {
	t.Helper()
	path := filepath.Join(root, "specs", "gateway", "coverage.json")
	data, err := os.ReadFile(path)
	if err != nil {
		// Not a skip. The manifest is a committed artefact, so its absence is a
		// broken tree — and skipping stood this guard down silently, which is
		// the one failure it exists to catch: with no manifest read, no scope
		// reaches the catalogue check and a missing row ships green.
		t.Fatalf("no gateway manifest at %s — it is a committed artefact; run `make sync-gateway-coverage-from-sdk`: %v", path, err)
	}
	var manifest struct {
		Scopes map[string]map[string][]string `json:"scopes"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	out := map[string]string{}
	for p, byMethod := range manifest.Scopes {
		for method, scopes := range byMethod {
			for _, s := range scopes {
				capability, _, ok := splitScope(s)
				if !ok {
					t.Errorf("manifest scope %q on %s %s is not in {capability}:{action} form", s, method, p)
					continue
				}
				if _, seen := out[capability]; !seen {
					out[capability] = method + " " + p
				}
			}
		}
	}
	return out
}

// scopesFromPlatformSpecs returns capability → an example spec requiring it,
// from x-required-privileges across the committed platform specs.
func scopesFromPlatformSpecs(t *testing.T, root string) map[string]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(root, "specs", "platform", "*.json"))
	if err != nil || len(files) == 0 {
		// Committed artefacts, same as the manifest above: absent means the tree
		// is broken, not that there is nothing to check.
		t.Fatalf("no platform specs under %s — they are committed artefacts; run `make sync-platform-specs-from-sdk`: %v",
			filepath.Join(root, "specs", "platform"), err)
	}
	out := map[string]string{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		// Operations are decoded one at a time because a path item also holds
		// non-operation keys ("parameters" is an array), which a typed
		// map[string]operation cannot survive.
		var spec struct {
			Paths map[string]map[string]json.RawMessage `json:"paths"`
		}
		if err := json.Unmarshal(data, &spec); err != nil {
			t.Fatalf("parsing %s: %v", f, err)
		}
		for _, ops := range spec.Paths {
			for method, raw := range ops {
				if !httpMethods[strings.ToLower(method)] {
					continue
				}
				var op struct {
					Privileges []string `json:"x-required-privileges"`
				}
				if err := json.Unmarshal(raw, &op); err != nil {
					t.Fatalf("parsing %s %s: %v", filepath.Base(f), method, err)
				}
				for _, s := range op.Privileges {
					capability, _, ok := splitScope(s)
					if !ok {
						t.Errorf("%s declares privilege %q, not in {capability}:{action} form", filepath.Base(f), s)
						continue
					}
					if _, seen := out[capability]; !seen {
						out[capability] = filepath.Base(f)
					}
				}
			}
		}
	}
	return out
}

// httpMethods is the operation keys a path item can carry; every other key
// ("parameters", "summary", "servers") is not an operation.
var httpMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "..", "..")
}
