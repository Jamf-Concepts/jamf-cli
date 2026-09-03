// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/gateway"
)

// The generated commands are covered: gatewayOps in gateway_coverage_test.go
// builds its operation list from parser.ParseSpec over specs/*.yaml, so every
// path a generated command sends is checked against the coverage manifest, and
// the generators stamp jamf:gateway on whatever the gateway does not publish.
//
// A hand-written command's path is structurally invisible to all of that. It is
// a string literal in a .go file that no spec describes and no annotation
// covers, so a version withdrawal reaches it only when someone greps. That is
// how twelve files kept sending /v3/computers-inventory after the surviving
// version became v4: the manifest published only v4, nothing refused the
// request, v3 still answered on the wire, and the mocks in the tests were keyed
// off the code rather than the published surface, so correcting one file broke
// six tests.
//
// These two tests close that. They read the committed manifest rather than
// gateway.Lookup alone, because Lookup answers from the `unserved` table, which
// is deliberately the intersection of "the gateway omits it" and "a generated
// command sends it" — a withdrawn version no generated command sends any more
// is absent from that table and therefore looks Served. The manifest's `spec`
// section is the whole published surface, so a path missing from it is the
// question actually being asked.

// coverageManifestPath is the committed manifest, relative to this package.
const coverageManifestPath = "../../specs/gateway/coverage.json"

// coverageManifest is the shape this test needs from specs/gateway/coverage.json:
// the published Pro and Classic surface as path → methods. Gateway-form paths
// with every parameter rendered as "{}".
type coverageManifest struct {
	Spec map[string][]string `json:"spec"`
}

func loadCoverageManifest(t *testing.T) coverageManifest {
	t.Helper()
	data, err := os.ReadFile(coverageManifestPath)
	if err != nil {
		t.Fatalf("reading %s: %v", coverageManifestPath, err)
	}
	var m coverageManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parsing %s: %v", coverageManifestPath, err)
	}
	if len(m.Spec) == 0 {
		t.Fatalf("%s declares no operations — a passing run against an empty manifest proves nothing", coverageManifestPath)
	}
	return m
}

// methodsFor returns every method the published surface declares for a
// gateway-form path, matched segment-wise. A "{}" on either side matches one
// segment: the manifest renders its own path parameters that way, and a path
// lifted out of a fmt.Sprintf carries one wherever a verb was.
func (m coverageManifest) methodsFor(path string) []string {
	var out []string
	seen := map[string]bool{}
	for pattern, methods := range m.Spec {
		if !manifestPathMatches(pattern, path) {
			continue
		}
		for _, meth := range methods {
			if !seen[meth] {
				seen[meth] = true
				out = append(out, meth)
			}
		}
	}
	sort.Strings(out)
	return out
}

func manifestPathMatches(pattern, path string) bool {
	ps := strings.Split(strings.Trim(pattern, "/"), "/")
	cs := strings.Split(strings.Trim(path, "/"), "/")
	if len(ps) != len(cs) {
		return false
	}
	for i := range ps {
		if ps[i] == "{}" || cs[i] == "{}" {
			continue
		}
		if ps[i] != cs[i] {
			return false
		}
	}
	return true
}

// handWrittenPathDirs are the packages that build Jamf Pro and Classic request
// paths by hand, relative to this one.
//
// internal/resolve is in it because it is where the device lookup every `pro`
// command shares actually sends its requests — three literals, and the reason
// re-keying the device, flush, app-usage and bulk mocks broke six tests: the
// path was a version behind in a package the finding did not name.
//
// Deliberately not scanned: internal/scope and internal/client build only
// fully-parameterised Classic paths ("/JSSResource/%s/id/%s"), so they carry no
// version segment that can go stale; internal/security and internal/platform
// speak the Radar and Platform APIs, which this manifest does not describe at
// all, so a /v1/login there would be misread as a Jamf Pro path.
var handWrittenPathDirs = []string{".", "../resolve"}

// handWrittenPathLiteral matches a Jamf Pro or Classic path literal: a quoted
// string starting with a version segment, the "preview" pseudo-version, or the
// Classic prefix. Platform gateway paths (/blueprints/v1/..., /securitycloud/...)
// start with their namespace and are correctly not matched — they are served by
// the SDK client, not by internal/client, and the coverage manifest does not
// describe them.
var handWrittenPathLiteral = regexp.MustCompile(`"(/(?:v[0-9]+|preview|JSSResource)[^"]*)"`)

var (
	fmtVerb        = regexp.MustCompile(`%[-+#0-9.\[\]]*[a-zA-Z]`)
	namedParam     = regexp.MustCompile(`\{[a-zA-Z0-9_]+\}`)
	methodLiteral  = regexp.MustCompile(`"(GET|POST|PUT|PATCH|DELETE)"`)
	getHelperCall  = regexp.MustCompile(`\b(fetchJSON|FetchJSON|FetchAllPaginated|fetchPaginatedCount|fetchArrayCount|fetchClassicCount|FetchClassicList|fetchXML|fetchText|fetchBytes)\b`)
	postHelperCall = regexp.MustCompile(`\bdoPostAction\b`)
)

// normalisePathLiteral turns a source literal into the form the manifest is
// keyed on: no query string, and one "{}" wherever the code substitutes a value.
func normalisePathLiteral(p string) string {
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	p = fmtVerb.ReplaceAllString(p, "{}")
	p = namedParam.ReplaceAllString(p, "{}")
	// A literal ending in "/" is concatenated with an identifier, so the real
	// request carries one more segment. Without this the path is a segment
	// short and matches nothing, which would read as "served".
	if strings.HasSuffix(p, "/") {
		p += "{}"
	}
	return p
}

// isRequestPath rejects a literal that is a prefix rather than a request path:
// the bare "/JSSResource" that gatewayFormPath and client.rewritePathForGateway
// cut off a path, and a bare version segment. Two segments is the minimum a
// real Pro or Classic path has.
func isRequestPath(p string) bool {
	return len(strings.Split(strings.Trim(p, "/"), "/")) >= 2
}

// methodsAtSite reads the HTTP methods a path literal is used with, from the
// lines around it. A method named in a string literal wins; otherwise the
// request helper the line calls says it (fetchJSON and friends are GET-only,
// doPostAction is POST). An empty result means the method could not be
// determined — a path assigned to a variable and sent several lines later — and
// the caller then falls back to asking whether the path is published at all.
func methodsAtSite(lines []string, i int) []string {
	const window = 3
	lo := max(i-window, 0)
	hi := min(i+window+1, len(lines))
	ctx := strings.Join(lines[lo:hi], "\n")

	var out []string
	seen := map[string]bool{}
	for _, m := range methodLiteral.FindAllStringSubmatch(ctx, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	if len(out) > 0 {
		sort.Strings(out)
		return out
	}
	switch {
	case getHelperCall.MatchString(ctx):
		return []string{"GET"}
	case postHelperCall.MatchString(ctx):
		return []string{"POST"}
	}
	return nil
}

// unservedHandWrittenPath is one accepted exception, keyed "METHOD gateway-path"
// (or "* gateway-path" when the method could not be read off the call site).
// Every entry names a path the gateway has withdrawn with no drop-in successor
// this change could move it to, and says why it is not simply re-versioned.
//
// Prefer fixing the path. An entry here is a documented gap, not a waiver: the
// command still fails on a gateway profile, and the response-side
// gatewayUnservedNote is what explains it to the operator.
var unservedHandWrittenPaths = map[string]string{
	// pro_group_tools.go, scopeableResources — `group-tools analyze --unused`
	// lists five Classic resources and reads each object's scope to find
	// unreferenced computer groups. capi v1993 withdrew GET /patchpolicies and
	// kept GET /patchpolicies/id/{}, so the collection listing has no Classic
	// successor; the Pro API's /v2/patch-policies declares no per-id read, which
	// is where the scope lives, so there is nothing to move to. This is a
	// fan-out: addReferencedGroupsFromClassic warns and continues per resource,
	// so the other four still answer and the response carries
	// gatewayUnservedNote. Refusing the whole command would cost four working
	// resources to report one.
	"* /proclassic/patchpolicies": "no successor: /patchpolicies/id/{} survives but the collection listing does not, and Pro's /v2/patch-policies has no per-id read",

	// pro_bulk.go — capi v1993 withdrew the whole Classic /computers GET
	// surface. The successor is Pro's /v4/computers-inventory, a different
	// response shape (and a different paging model) rather than a re-version,
	// so it is a rewrite of the bulk lookup rather than a literal edit. Not in
	// this change's scope; recorded so it is not mistaken for coverage.
	"GET /proclassic/computers/id/{}": "no drop-in successor: the modern /v4/computers-inventory replacement is a different response shape, so pro_bulk.go needs a rewrite rather than a re-version",

	// pro_device_actions.go, sendComputerModernMDMCommand and
	// sendMobileModernMDMCommand — the gateway declares GET on
	// /v2/mdm/commands and not POST, and there is no served successor: Classic's
	// /computercommands takes a different command vocabulary and a computer id
	// rather than a managementId, so it is a different feature and not a
	// re-version.
	//
	// Resolved the other way instead. Every command that POSTs here is stamped
	// by markGatewayCoverage, so checkAPIMatch refuses it before anything is
	// sent — the same treatment the generated `pro mdm-commands create` already
	// had. The point of the finding was the asymmetry: one command name refused
	// while a dozen others sent the identical request, which told the operator
	// the endpoint was out of the supported API and then let them bulk-issue it
	// across a group. TestModernMDMCommandsCarryTheGatewayRefusal pins that.
	"POST /pro/v2/mdm/commands": "no successor, and refused pre-flight instead: every command sending it is stamped by markGatewayCoverage",
}

// TestHandWrittenPathsAreServed greps the hand-written command files for Jamf
// Pro (/vN/...) and Classic (/JSSResource/...) path literals and asserts each
// resolves to an operation the gateway publishes, against the committed
// coverage manifest. Where the call site names its method, the method is
// checked too; otherwise the path is required to be published under some
// method, which is what catches a withdrawn version.
func TestHandWrittenPathsAreServed(t *testing.T) {
	manifest := loadCoverageManifest(t)

	var files []string
	for _, dir := range handWrittenPathDirs {
		found, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		if len(found) == 0 {
			t.Fatalf("no Go files under %s — the scan directory has moved", dir)
		}
		files = append(files, found...)
	}
	sort.Strings(files)

	used := map[string]bool{}
	checked := 0

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(src), "\n")
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			for _, m := range handWrittenPathLiteral.FindAllStringSubmatch(line, -1) {
				literal := normalisePathLiteral(m[1])
				if !isRequestPath(literal) {
					continue
				}
				path := gatewayFormPath(literal)
				published := manifest.methodsFor(path)
				methods := methodsAtSite(lines, i)
				checked++

				if len(methods) == 0 {
					// Method unknown: the weaker question, "does the gateway
					// publish this path at all". A withdrawn version answers no.
					if len(published) > 0 {
						continue
					}
					key := "* " + path
					if reason, ok := unservedHandWrittenPaths[key]; ok {
						used[key] = true
						t.Logf("known gap %s:%d %s (%s)", file, i+1, path, reason)
						continue
					}
					t.Errorf("%s:%d sends %s, which the gateway's published Pro/Classic surface does not declare under any method.\n"+
						"Re-version it onto the published path, or add an entry to unservedHandWrittenPaths saying why it cannot be moved.",
						file, i+1, path)
					continue
				}

				for _, meth := range methods {
					if slices.Contains(published, meth) {
						// Published, but a recorded wire probe can still say the
						// gateway does not route it.
						if f, ok := gateway.Lookup(meth, path); ok && f.Level == gateway.Unserved {
							t.Errorf("%s:%d sends %s %s, which internal/gateway reports Unserved (%s: %s)",
								file, i+1, meth, path, f.Basis, f.Detail)
						}
						continue
					}
					key := meth + " " + path
					if reason, ok := unservedHandWrittenPaths[key]; ok {
						used[key] = true
						t.Logf("known gap %s:%d %s %s (%s)", file, i+1, meth, path, reason)
						continue
					}
					t.Errorf("%s:%d sends %s %s, which the gateway's published Pro/Classic surface does not declare (it declares %v).\n"+
						"Re-version it onto the published path, or add an entry to unservedHandWrittenPaths saying why it cannot be moved.",
						file, i+1, meth, path, published)
				}
			}
		}
	}

	if checked == 0 {
		t.Fatal("no hand-written Pro or Classic path literals found — the scanner has stopped matching, so a pass proves nothing")
	}

	// An exception that no longer matches anything is a stale claim about the
	// code. Same reasoning as TestEveryOverrideStillMatchesACommandThisCLISends.
	for key := range unservedHandWrittenPaths {
		if !used[key] {
			t.Errorf("unservedHandWrittenPaths entry %q matches no hand-written path any more — the path was fixed or removed, so drop the entry", key)
		}
	}
}

// TestBackupResourcePathsAreServed asserts every curated backup/diff resource
// resolves to endpoints the gateway publishes.
//
// `pro backup` and `pro diff` fan out over BackupResources, and each entry names
// a generated command's Key rather than a path — so a key naming a resource the
// gateway has withdrawn is invisible in this file and only shows up as a failed
// resource at runtime. Worse under --allow-partial-failure, which downgrades
// that to a warning: the backup then exits 0 while silently missing every
// object of that resource.
//
// DeduplicateVersioned cannot catch it either. It keys a family on a "V<n>"
// name suffix, so `static-computer-groups` (v2) and `computer-groups-static-groups`
// (v3) are never treated as the same family and both survive — picking between
// them is pro_resources.go's job.
func TestBackupResourcePathsAreServed(t *testing.T) {
	manifest := loadCoverageManifest(t)

	resolved, err := ResolveBackupResources(nil)
	if err != nil {
		t.Fatalf("resolving backup resources: %v", err)
	}
	if len(resolved) == 0 {
		t.Fatal("no backup resources resolved")
	}

	for _, r := range resolved {
		for _, ep := range []struct {
			role, path, method string
		}{
			{"ListPath", r.ListPath, "GET"},
			{"GetPath", r.GetPath, "GET"},
			{"ScopePath", r.ScopePath, "GET"},
		} {
			if ep.path == "" {
				continue
			}
			path := gatewayFormPath(normalisePathLiteral(ep.path))
			if slices.Contains(manifest.methodsFor(path), ep.method) {
				if f, ok := gateway.Lookup(ep.method, path); ok && f.Level == gateway.Unserved {
					t.Errorf("backup resource %q %s is %s %s, which internal/gateway reports Unserved (%s: %s)",
						r.Key, ep.role, ep.method, path, f.Basis, f.Detail)
				}
				continue
			}
			t.Errorf("backup resource %q %s is %s %s, which the gateway's published Pro/Classic surface does not declare.\n"+
				"`pro backup` would send a request the resource's own command refuses on the same profile. Point the entry at a key whose paths are published.",
				r.Key, ep.role, ep.method, path)
		}
	}
}

// modernMDMRefusedCommands are the hand-written device actions that POST
// /v2/mdm/commands, the path the gateway declares GET on and not POST. Named
// rather than discovered, because the failure being pinned is a command that
// silently stops being annotated.
var modernMDMRefusedCommands = [][]string{
	{"pro", "computers-inventory", "lock"},
	{"pro", "computers-inventory", "restart"},
	{"pro", "computers-inventory", "shutdown"},
	{"pro", "computers-inventory", "enable-remote-desktop"},
	{"pro", "computers-inventory", "disable-remote-desktop"},
	{"pro", "computers-inventory", "set-recovery-lock"},
	{"pro", "computers-inventory", "set-auto-admin-password"},
	{"pro", "computers-inventory", "settings"},
	{"pro", "mobile-devices", "lock"},
	{"pro", "mobile-devices", "restart"},
	{"pro", "mobile-devices", "shutdown"},
	{"pro", "mobile-devices", "clear-passcode"},
	{"pro", "mobile-devices", "enable-lost-mode"},
	{"pro", "mobile-devices", "settings"},
}

// TestModernMDMCommandsCarryTheGatewayRefusal pins finding (22)'s resolution:
// the same wire operation must not be refused under one command name and sent
// under a dozen others.
//
// `pro mdm-commands create` is generated, carries jamf:gateway from the
// manifest and exits non-zero on a gateway profile. Every command listed above
// POSTs the identical /v2/mdm/commands through sendComputerModernMDMCommand or
// sendMobileModernMDMCommand and carried no annotation at all — so the operator
// was told the endpoint is outside the supported API by one command and could
// then bulk-issue it across a whole group with another.
func TestModernMDMCommandsCarryTheGatewayRefusal(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")

	for _, path := range modernMDMRefusedCommands {
		name := strings.Join(path, " ")
		cmd, _, err := root.Find(path)
		if err != nil || cmd == nil || cmd.Name() != path[len(path)-1] {
			t.Errorf("%s: not found in the command tree (%v)", name, err)
			continue
		}
		if got := cmd.Annotations[annotationGateway]; got != string(gateway.Unserved) {
			t.Errorf("%s: jamf:gateway is %q, want %q — it POSTs /v2/mdm/commands, which the gateway does not publish",
				name, got, gateway.Unserved)
			continue
		}
		if got := cmd.Annotations[annotationAPI]; got != apiPro {
			t.Errorf("%s: jamf:api is %q, want %q — checkAPIMatch only reads the gateway verdict for a Pro or Classic command",
				name, got, apiPro)
		}
		if err := checkAPIMatch(cmd, &auth.PlatformOAuth2Provider{}, "platform-ga"); err == nil {
			t.Errorf("%s: allowed through on a gateway profile", name)
		}
	}
}

// A served hand-written action must not be annotated. markGatewayCoverage reads
// the runtime table, so this also fails if that lookup ever starts matching too
// broadly — `pro comp erase` POSTs /v4/computers-inventory/{id}/erase, which the
// gateway publishes, and `pro md update-inventory` POSTs Classic
// /mobiledevicecommands/command/{}/id/{}, likewise.
func TestServedHandWrittenActionsAreNotAnnotatedUnserved(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")

	for _, path := range [][]string{
		{"pro", "computers-inventory", "erase"},
		{"pro", "computers-inventory", "remove-mdm"},
		{"pro", "mobile-devices", "update-inventory"},
	} {
		name := strings.Join(path, " ")
		cmd, _, err := root.Find(path)
		if err != nil || cmd == nil {
			t.Errorf("%s: not found in the command tree (%v)", name, err)
			continue
		}
		if got := cmd.Annotations[annotationGateway]; got != "" {
			t.Errorf("%s: jamf:gateway is %q, want unset — its endpoint is published, so refusing it costs a working command", name, got)
		}
	}
}
