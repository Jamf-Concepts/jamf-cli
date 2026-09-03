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
// gateway-form path, mirroring internal/gateway's own two-pass lookup: literal
// patterns before parameterised ones.
//
// The two passes are the fix for a matcher that skipped the comparison whenever
// EITHER side carried a "{}" and then unioned the methods of every pattern that
// matched. A code path's "{}" therefore matched a manifest pattern's literal
// segment, so /pro/v1/packages/{} matched the real {} pattern (DELETE/GET/PUT)
// and ALSO /pro/v1/packages/delete-multiple and /export (both POST) — reporting
// POST as published on a path where the gateway declares none. And a literal
// whose leading segment was substituted matched everything at that arity, so
// two live sites were counted as checked and constrained by nothing.
//
// A code "{}" now matches only a manifest "{}". The reverse direction is kept,
// because it is what the real lookup does: /pro/v1/computers-inventory/detail
// legitimately matches both .../detail and .../{}, and the literal has to win.
func (m coverageManifest) methodsFor(path string) []string {
	if out := m.methodsMatching(path, false); len(out) > 0 {
		return out
	}
	return m.methodsMatching(path, true)
}

func (m coverageManifest) methodsMatching(path string, allowParameterised bool) []string {
	var out []string
	seen := map[string]bool{}
	for pattern, methods := range m.Spec {
		if !manifestPathMatches(pattern, path, allowParameterised) {
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

// manifestPathMatches compares a manifest pattern against a gateway-form code
// path. A "{}" in the code path matches only a "{}" in the pattern. A "{}" in
// the pattern matches a literal code segment only when allowParameterised is
// set, which is the second pass.
func manifestPathMatches(pattern, path string, allowParameterised bool) bool {
	ps := strings.Split(strings.Trim(pattern, "/"), "/")
	cs := strings.Split(strings.Trim(path, "/"), "/")
	if len(ps) != len(cs) {
		return false
	}
	for i := range ps {
		switch {
		case ps[i] == cs[i]:
			// Equal, "{}" against "{}" included.
		case cs[i] == "{}":
			// The code substitutes a value here, so only a pattern parameter
			// can be what it addresses. A literal cannot.
			return false
		case ps[i] == "{}":
			if !allowParameterised {
				return false
			}
		default:
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

	// The ±3 window is deliberately tight, and it misses the common shape of a
	// path built at the top of a function and sent at the bottom. Widen to the
	// enclosing function, but accept the answer only when it is UNAMBIGUOUS —
	// exactly one distinct method in the whole function. A function that sends
	// two methods says nothing about which one this path takes, and guessing
	// there is how a POST-only const came to be checked against GET.
	lo, hi = enclosingFuncRange(lines, i)
	ctx = strings.Join(lines[lo:hi], "\n")
	out = nil
	seen = map[string]bool{}
	for _, m := range methodLiteral.FindAllStringSubmatch(ctx, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	// Helper calls count towards the ambiguity, not only towards the answer.
	// Counting only the method literals made verifyPackageUpload — which POSTs
	// a JCDS refresh and polls the package through a GET helper — report POST
	// for the poll path, because the single literal looked unambiguous. The
	// widened window is only worth trusting when the whole function speaks one
	// method.
	if getHelperCall.MatchString(ctx) && !seen["GET"] {
		seen["GET"] = true
		out = append(out, "GET")
	}
	if postHelperCall.MatchString(ctx) && !seen["POST"] {
		seen["POST"] = true
		out = append(out, "POST")
	}
	if len(out) == 1 {
		return out
	}
	return nil
}

// enclosingFuncRange returns the [lo, hi) line range of the function containing
// line i, or the whole file when the line sits above the first declaration.
func enclosingFuncRange(lines []string, i int) (int, int) {
	lo := 0
	for j := i; j >= 0; j-- {
		if strings.HasPrefix(lines[j], "func ") {
			lo = j
			break
		}
	}
	hi := len(lines)
	for j := i + 1; j < len(lines); j++ {
		if strings.HasPrefix(lines[j], "func ") {
			hi = j
			break
		}
	}
	return lo, hi
}

// handWrittenDynamicCollections expands a path whose LEADING segment is
// substituted into the concrete paths the code can actually build, keyed on the
// normalised gateway-form path.
//
// Two live helpers assemble a Classic collection name from a closed set:
// internal/resolve's resolveClassicGroupID takes "computergroups" or
// "mobiledevicegroups", and pro_blueprints.go's classicProfilePath takes
// "osxconfigurationprofiles" or "mobiledeviceconfigurationprofiles". The
// resulting literal is "/JSSResource/%s/name/%s", which under the old matcher
// matched every manifest pattern of the same arity and was therefore counted as
// checked while being constrained by nothing. Expanded here, each concrete path
// is checked like any other.
//
// A new entry needs the call sites read: the value is the set of collection
// names the callers pass, not the set the helper could accept.
var handWrittenDynamicCollections = map[string][]string{
	// internal/resolve/resolve.go, resolveClassicGroupID — callers pass
	// "computergroups" (resolveComputerGroupID) and "mobiledevicegroups"
	// (resolveMobileDeviceGroupID).
	"/proclassic/{}/name/{}": {
		"/proclassic/computergroups/name/{}",
		"/proclassic/mobiledevicegroups/name/{}",
	},
	// pro_blueprints.go, classicProfilePath — classicProfileCollection returns
	// "osxconfigurationprofiles" for "computer" and
	// "mobiledeviceconfigurationprofiles" for "mobile".
	"/proclassic/{}": {
		"/proclassic/osxconfigurationprofiles/id/{}",
		"/proclassic/mobiledeviceconfigurationprofiles/id/{}",
	},
}

// expandDynamicCollections returns the concrete paths a normalised path stands
// for, or the path itself when it is already concrete.
func expandDynamicCollections(path string) []string {
	if expanded, ok := handWrittenDynamicCollections[path]; ok {
		return expanded
	}
	return []string{path}
}

// handWrittenUndeterminedMethods names the methods a call site sends where
// methodsAtSite cannot read them off the ±3 lines — a path assigned to a const
// or a variable and sent elsewhere. Keyed on the normalised gateway-form path.
//
// It exists because the fallback for "method undetermined" used to be the
// weaker question, "is this path published under ANY method", and unioning
// every matching pattern's methods made that weaker still. The sharpest case
// was in the guard's own subject file: pro_device_actions.go's
// `const mdmCommandsPath = "/v2/mdm/commands"` has no method in its window, so
// it passed on GET being published — the one method the gateway serves, and the
// opposite of what the const is used for.
var handWrittenUndeterminedMethods = map[string][]string{
	// pro_device_actions.go, mdmCommandsPath — every use is a POST
	// (sendComputerModernMDMCommand, sendMobileModernMDMCommand). The gateway
	// declares GET here and not POST, which is why the POST entry in
	// unservedHandWrittenPaths below exists and why markGatewayCoverage stamps
	// the senders. This is the entry the tightening was written for: under the
	// old "published under any method" fallback the const passed on GET.
	"/pro/v2/mdm/commands": {"POST"},

	// pro_packages_upload.go — the multipart upload goes through
	// client.Upload rather than client.Do, so no method literal appears
	// anywhere in the function. The other case the tightening catches: a POST
	// that the old fallback checked against whatever /pro/v1/packages/{}
	// declares.
	"/pro/v1/packages/{}/upload": {"POST"},

	// pro_packages_upload.go, verifyPackageUpload — this one function POSTs the
	// JCDS refresh and polls the package record with a GET, so the whole
	// function is genuinely ambiguous and each path has to say which it is.
	"/pro/v1/cloud-distribution-point/refresh-inventory": {"POST"},
	"/pro/v1/packages/{}":                                {"GET"},

	// internal/resolve/resolve.go — the Classic static-group fallback paths are
	// arguments to fetchClassicGroupMemberIDs, which reads the collection and
	// filters client-side.
	"/proclassic/computergroups":     {"GET"},
	"/proclassic/mobiledevicegroups": {"GET"},

	// internal/resolve/resolve.go — fileEntrySpec.basePath, a field in a
	// package-level struct literal consumed by the batch lookup's reader.
	"/pro/v4/computers-inventory":   {"GET"},
	"/pro/v2/mobile-devices/detail": {"GET"},

	// internal/resolve/resolve.go — the smart mobile group lookup and its
	// membership fetch, both read paths (resolveGroupIDByName, fetchAllPages).
	"/pro/v2/mobile-device-groups/smart-groups":              {"GET"},
	"/pro/v2/mobile-device-groups/smart-group-membership/{}": {"GET"},

	// pro_backup.go — the path appears only in a backupFailure record; the
	// request itself is jcdsListFiles, a read.
	"/pro/v1/jcds/files": {"GET"},

	// pro_blueprints.go, classicProfilePath — the helper only assembles the
	// path; its one caller reads the profile (`Do(ctx, "GET", ...)`).
	// Also expanded by handWrittenDynamicCollections above, since the
	// collection segment is substituted.
	"/proclassic/{}": {"GET"},

	// pro_group_tools.go, scopeableResources — a table of Classic list and
	// detail endpoints that `group-tools analyze --unused` reads to find
	// unreferenced computer groups. Reads only.
	"/proclassic/policies":                       {"GET"},
	"/proclassic/policies/id/{}":                 {"GET"},
	"/proclassic/osxconfigurationprofiles":       {"GET"},
	"/proclassic/osxconfigurationprofiles/id/{}": {"GET"},
	"/proclassic/restrictedsoftware":             {"GET"},
	"/proclassic/restrictedsoftware/id/{}":       {"GET"},
	"/proclassic/ebooks":                         {"GET"},
	"/proclassic/ebooks/id/{}":                   {"GET"},
	"/proclassic/patchpolicies":                  {"GET"},
	"/proclassic/patchpolicies/id/{}":            {"GET"},

	// pro_resources.go, BackupResources.ScopePath — read by `pro backup` and
	// `pro diff` to capture a prestage's device scope.
	"/pro/v2/computer-prestages/{}/scope":      {"GET"},
	"/pro/v2/mobile-device-prestages/{}/scope": {"GET"},
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
	"GET /proclassic/patchpolicies": "no successor: /patchpolicies/id/{} survives but the collection listing does not, and Pro's /v2/patch-policies has no per-id read",

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
	usedDynamic := map[string]bool{}
	usedUndetermined := map[string]bool{}
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
				normalised := gatewayFormPath(literal)
				checked++

				methods := methodsAtSite(lines, i)
				if len(methods) == 0 {
					// "Method undetermined" is no longer allowed to pass on
					// the weaker "published under any method" question: that is
					// what let a POST-only const pass on GET. It needs an
					// explicit entry naming what the code sends.
					declared, ok := handWrittenUndeterminedMethods[normalised]
					if !ok {
						usedUndetermined[normalised] = true
						t.Errorf("%s:%d sends %s and no method could be read from the surrounding lines.\n"+
							"Name the methods this site sends in handWrittenUndeterminedMethods, so the "+
							"path is checked against them rather than against whatever the manifest "+
							"declares under any method.", file, i+1, normalised)
						continue
					}
					usedUndetermined[normalised] = true
					methods = declared
				}

				for _, path := range expandDynamicCollections(normalised) {
					if path != normalised {
						usedDynamic[normalised] = true
					}
					published := manifest.methodsFor(path)
					for _, meth := range methods {
						if slices.Contains(published, meth) {
							// Published, but a recorded wire probe can still say
							// the gateway does not route it.
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
	for key := range handWrittenDynamicCollections {
		if !usedDynamic[key] {
			t.Errorf("handWrittenDynamicCollections entry %q matches no hand-written path any more — the helper was changed or removed, so drop the entry", key)
		}
	}
	for key := range handWrittenUndeterminedMethods {
		if !usedUndetermined[key] {
			t.Errorf("handWrittenUndeterminedMethods entry %q matches no hand-written path whose method is undetermined any more — the call site now names its method, so drop the entry", key)
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
