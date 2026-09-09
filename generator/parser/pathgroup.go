// Copyright 2026, Jamf Software LLC

package parser

import (
	"sort"
	"strconv"
	"strings"
)

// A resource's identity comes from the URL paths it serves, not from the name
// of a file someone put those paths in.
//
// The spec files under specs/ used to decide it: a single-family spec was named
// from its filename and a multi-family one from its paths, so `pro
// static-computer-groups` existed because of `StaticComputerGroups.yaml` — a
// string that appears in no spec and is upstream's jss module filename. Four
// things followed from that filename with nothing stating them: the command
// name, the endpoint-version family the resource joined (keyed on a `-vN`
// suffix), whether an upstream `-preview` tag reached a command, and whether the
// splitter could delete the file. Splitting one document into 165 files to carry
// that is overkill; grouping the paths directly says the same thing and says it
// from the spec.
//
// It also removes two whole classes of bug rather than guarding against them.
// Version consolidation stops depending on a filename suffix — every version of
// a path lands in one group and the highest wins, which is what the CLI wanted
// when a mis-keyed family cost it the v4 computer-inventory endpoints. And
// preview suppression keys on the operation's tag rather than on a filename
// happening to name the canonical resource, which is what kept 17 `-preview`
// tags out of the command surface by accident.

// PathGroup is one resource: the literal path segments that identify it, and
// every path that belongs to it.
type PathGroup struct {
	// Name is the kebab-case command name, derived from Root unless an override
	// replaces it.
	Name string
	// Root is the literal (non-parameter, non-version) path segments that
	// identify the resource.
	Root []string
	// Paths are the document paths assigned to this group, sorted.
	Paths []string
	// Versions are the API versions the group's paths are served at. A group
	// spanning several is normal and is not itself a consolidation event —
	// deduplicateVersionedOps decides that per version-stripped path shape.
	Versions []int
}

// GroupPathsByCollection assigns every path to the resource that owns it.
//
// A path belongs to the longest *root* that prefixes it, where a root is a run
// of literal segments that is either
//
//   - one segment deep — a top-level collection is always its own resource,
//     even when nothing hangs off it; or
//   - deeper, and both answers as a path itself *and* has a `{param}` child.
//
// The second condition is what separates a sub-collection from an action that
// happens to take an id. `computer-groups/smart-groups` answers as a collection
// and has `{id}` beneath it, so it is a resource. `icon/download` has
// `{id}` beneath it and does *not* answer as a collection, so `download` stays
// an operation on `icon` — without that test, five thin resources appear
// (`icon-download`, `jcds-files`, `scheduler-jobs`, `log-flushing-task`,
// `health-status`) for what are plainly operations.
//
// Everything deeper than the root becomes part of the operation's name, which
// is the job the templates already do from Operation.Path.
func GroupPathsByCollection(paths []string) []*PathGroup {
	exact, withParamChild := classifyPrefixes(paths)

	isRoot := func(prefix []string) bool {
		if len(prefix) == 0 {
			return false
		}
		if len(prefix) == 1 {
			return true
		}
		key := strings.Join(prefix, "/")
		return exact[key] && withParamChild[key]
	}

	byRoot := map[string]*PathGroup{}
	for _, p := range paths {
		version, segs := splitVersionSegment(p)
		root := longestRoot(segs, isRoot)
		key := strings.Join(root, "/")
		g := byRoot[key]
		if g == nil {
			g = &PathGroup{Root: root}
			byRoot[key] = g
		}
		g.Paths = append(g.Paths, p)
		if !containsInt(g.Versions, version) {
			g.Versions = append(g.Versions, version)
		}
	}

	mergeRoots(byRoot)

	out := make([]*PathGroup, 0, len(byRoot))
	for _, g := range byRoot {
		sort.Strings(g.Paths)
		sort.Ints(g.Versions)
		g.Name = groupName(g.Root)
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// mergeRoots folds one group's paths into another's, for the case where two
// path roots are the same resource.
//
// Distinct from a name override: the two roots produce two groups, and what is
// wanted is one. Every entry is forced by a collision with an established
// command alias rather than chosen on taste — see pathGroupRootMerges.
func mergeRoots(byRoot map[string]*PathGroup) {
	for source, target := range pathGroupRootMerges {
		from, ok := byRoot[source]
		if !ok {
			continue
		}
		into, ok := byRoot[target]
		if !ok {
			continue
		}
		into.Paths = append(into.Paths, from.Paths...)
		for _, v := range from.Versions {
			if !containsInt(into.Versions, v) {
				into.Versions = append(into.Versions, v)
			}
		}
		delete(byRoot, source)
	}
}

// pathGroupRootMerges folds a derived root into another, keyed by the "/"-joined
// literal segments of each.
//
// Both entries exist because the derived name collides with an alias this CLI
// has shipped for a long time, and in both cases the paths act on the resource
// that alias names — so merging is what the collision was pointing at rather
// than a workaround for it. `pro computers` in particular is the alias for the
// primary computer resource and one of the most-used commands in the CLI.
//
// Note the merge restores a symmetry the paths break on their own:
// /v1/mobile-devices/{id}/recalculate-smart-groups and
// /v1/users/{id}/recalculate-smart-groups already land inside their real
// resources, because those live at the matching path. The computer resource is
// served at /vN/computers-inventory, so its recalculate action was the only one
// of the three left stranded in a group of its own.
var pathGroupRootMerges = map[string]string{
	"computers":             "computers-inventory",
	"smart-computer-groups": "computer-groups/smart-groups",
}

// classifyPrefixes records, for every literal path prefix in the document,
// whether that prefix answers as a path in its own right and whether any path
// extends it with a parameter segment.
func classifyPrefixes(paths []string) (exact, withParamChild map[string]bool) {
	exact = map[string]bool{}
	withParamChild = map[string]bool{}
	for _, p := range paths {
		_, segs := splitVersionSegment(p)
		var lits []string
		sawParam := false
		for _, s := range segs {
			if isPathParam(s) {
				withParamChild[strings.Join(lits, "/")] = true
				sawParam = true
				break
			}
			lits = append(lits, s)
		}
		if !sawParam {
			exact[strings.Join(lits, "/")] = true
		}
	}
	return exact, withParamChild
}

// longestRoot returns the longest leading run of literal segments that isRoot
// accepts. It stops at the first parameter segment: a root is always an
// unparameterised prefix, so nothing beyond one can name a resource.
func longestRoot(segs []string, isRoot func([]string) bool) []string {
	var best []string
	for i := 1; i <= len(segs); i++ {
		if isPathParam(segs[i-1]) {
			break
		}
		prefix := segs[:i]
		if isRoot(prefix) && i > len(best) {
			best = prefix
		}
	}
	if best == nil {
		// Every segment is a parameter, or the document declares a bare "/".
		// Neither is a resource; the caller reports it rather than inventing one.
		return nil
	}
	return append([]string(nil), best...)
}

// splitVersionSegment removes a leading `vN` segment and returns the version it
// declared (0 when the path carries none) plus the remaining segments.
//
// Only a *leading* version is stripped, which is where every Jamf Pro API path
// carries it. A version appearing deeper would be part of a resource's own
// name.
func splitVersionSegment(path string) (int, []string) {
	segs := splitPathSegments(path)
	if len(segs) == 0 {
		return 0, nil
	}
	if n, ok := apiVersionSegment(segs[0]); ok {
		return n, segs[1:]
	}
	return 0, segs
}

// apiVersionSegment reports whether seg is a `vN` version segment.
func apiVersionSegment(seg string) (int, bool) {
	if len(seg) < 2 || seg[0] != 'v' {
		return 0, false
	}
	n, err := strconv.Atoi(seg[1:])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// splitPathSegments splits a URL path into its non-empty segments.
func splitPathSegments(path string) []string {
	var out []string
	for _, s := range strings.Split(path, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// isPathParam reports whether seg is an OpenAPI path parameter like `{id}`.
func isPathParam(seg string) bool {
	return strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}")
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// groupName turns a root into a command name, applying an override when the
// derived one is unusable.
func groupName(root []string) string {
	derived := strings.Join(root, "-")
	if override, ok := pathGroupNameOverrides[derived]; ok {
		return override
	}
	return derived
}

// pathGroupNameOverrides replaces a derived resource name that cannot stand as
// a command name, keyed on the derived name — the root's segments joined with
// "-", which is what groupName builds. Note this differs from
// pathGroupRootMerges, which keys on the root joined with "/" because it
// operates on the grouping map before any name exists.
//
// Kept deliberately small. Every entry is a name the path structure genuinely
// does not supply — a legacy path whose first segment is a generic word, or a
// collision with a command this CLI already ships — and not a matter of taste:
// a rule that is overridden wherever someone prefers a different noun is not a
// rule.
var pathGroupNameOverrides = map[string]string{
	// `/v1/auth`, `/v1/auth/token`, `/v1/auth/keep-alive`. The hand-written
	// `pro auth token` already owns this name, and a generated resource cannot
	// share a parent's name with a hand-written one — cobra dispatches by
	// declaration order and `pro auth --help` would list one of them.
	"auth": "authentications",
	// `/v1/ldap/groups`, `/v1/ldap/servers`. Bare `ldap` reads as a settings
	// object; these are lookups against a configured directory.
	"ldap": "ldap-lookups",
	// `/v1/pki/digicert-trust-lifecycle-manager`. The derived name is 37
	// characters of vendor product branding; the siblings under the same
	// namespace are `pki-venafi` and `pki-adcs-settings`.
	"pki-digicert-trust-lifecycle-manager": "pki-digicert",
	// `/v1/ddm/{clientManagementId}/status-items` and `/sync`. `pro ddm` is an
	// established alias for the platform `ddm-reports` command, which a real
	// subcommand of that name would shadow — cobra prefers an exact name over
	// an alias.
	"ddm": "ddm-clients",
	// Every path is `/v1/pki/certificate-authority/...`, but the collection
	// itself is not served — only `/active` and `/{id}` — so the root falls back
	// to `pki`, which also parents venafi, adcs-settings and digicert. Naming
	// the certificate-authority resource after its whole namespace would claim
	// its siblings' ground.
	"pki": "certificate-authorities",
	// `POST /v1/deploy-package`. The endpoint is verb-first; a command name is
	// a noun, and the verb belongs to the operation under it.
	"deploy-package": "package-deployments",
	// `/v1/enrollment-customization/{id}/...` — panel management, which upstream
	// serves beside a *separate* `/v1/enrollment-customizations` collection.
	// Both are real, so the derived names differ only by a trailing `s`; keep
	// the name that says which one it is.
	"enrollment-customization": "enrollment-customization-panels",
	// `/preview/remote-administration-configurations/team-viewer/...`. The
	// `preview` segment is where upstream parks the endpoint, not part of the
	// resource's identity, and the derived name is 55 characters.
	"preview-remote-administration-configurations-team-viewer": "team-viewer-remote-administrations",
}

// KeepPath reports whether a document path should be ingested at all, given the
// tags its operations carry.
//
// This is the declared "do not turn this into a command" list, and it is the
// only thing standing between the command surface and upstream's legacy
// endpoints. Grouping by path is faithful to the document, which means it is
// also faithful to the parts of the document nobody should be calling.
func KeepPath(path string, tags []string) bool {
	if droppedPaths[path] {
		return false
	}
	for _, t := range tags {
		if droppedTags[t] {
			return false
		}
	}
	return true
}

// droppedTags lists OpenAPI tags whose paths must never become commands.
//
// A tag is the right unit when it covers exactly the legacy endpoints — it
// survives a path being renamed upstream, and it reads as a statement about the
// endpoints rather than about their spelling.
var droppedTags = map[string]bool{
	// `/preview/computers` — a stub returning names only. The real computer
	// surface is `/vN/computers-inventory`, which this CLI already ships.
	"computers-preview": true,
	// `/settings/issueTomcatSslCertificate` — unversioned legacy, and the one
	// path this tag covers.
	"tomcat-settings-preview": true,
	// `/preview/remote-administration-configurations` — the bare collection
	// stub above the team-viewer family. Dropping the stub leaves
	// `/preview/remote-administration-configurations/team-viewer/...` intact,
	// which is the surface the gateway actually publishes.
	"remote-administration": true,
	// `/devices/extensionAttributes` — a preview endpoint returning names only.
	// The real CRUD is `/v1/mobile-device-extension-attributes`, tagged
	// separately.
	"mobile-device-extension-attributes-preview": true,
	// `/user`, `/user/updateSession` — legacy session-token endpoints unrelated
	// to the canonical `/v1/user-sessions/*` resource.
	"user-session-preview": true,
}

// droppedPaths lists individual paths to skip, for the case a tag cannot
// express.
//
// It exists because **a tag is not always a safe unit**, and assuming it was
// would have deleted a live command. `policies-preview` tags the legacy
// `/settings/obj/policyProperties` *and* `/v1/policy-properties`, the real
// versioned resource — so dropping that tag would have taken `pro
// policy-properties` with it, silently, since the resource simply stops being
// generated. Check a tag's blast radius before adding one above.
var droppedPaths = map[string]bool{
	// Unversioned legacy twin of `/v1/policy-properties`. Shares the
	// `policies-preview` tag with that live path, hence the path-keyed drop.
	"/settings/obj/policyProperties": true,
	// Inventory preload v1, superseded by v2 at a restructured path.
	//
	// This is the one case a version rule cannot settle on its own. v2 moved
	// every record operation under `records/`, so `/v1/inventory-preload/{id}`
	// and `/v2/inventory-preload/records/{id}` are the same endpoint at two
	// path *shapes* — and deduplicateVersionedOps matches on the shape, so it
	// sees two unrelated endpoints and keeps both. The gateway withdrew the v1
	// family, and v2 serves list, get, create, update, delete and delete-all,
	// so nothing is lost by dropping these.
	//
	// Left in, they were actively worse than absent: the v1 paths took the
	// plain `list`, `get`, `create`, `update` and `delete` names on
	// `pro inventory-preload` while being refused on a gateway profile, and the
	// served v2 CRUD sat under `pro inventory-preload-records`. The obvious
	// command was the broken one.
	"/inventory-preload":                 true,
	"/v1/inventory-preload":              true,
	"/inventory-preload/{id}":            true,
	"/v1/inventory-preload/{id}":         true,
	"/inventory-preload/validate-csv":    true,
	"/v1/inventory-preload/validate-csv": true,
	"/inventory-preload/history":         true,
	"/v1/inventory-preload/history":      true,
	"/inventory-preload/history/notes":   true,
	"/inventory-preload/csv-template":    true,
	"/v1/inventory-preload/csv-template": true,

	// A sub-lookup with no sibling CRUD on the base collection, so it produces
	// a lone `groups` command with no context. Membership is already reachable
	// through the computer-groups and mobile-device-groups resources.
	//
	// Dropped by path even though its `devices` tag covers nothing else,
	// because the path is versioned and the rule above forbids a tag-keyed drop
	// reaching one: a versioned path is a live endpoint, and if upstream ever
	// tags another with `devices` the tag-keyed drop would take it silently.
	"/v1/devices/{id}/groups": true,
}
