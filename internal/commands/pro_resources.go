// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Jamf-Concepts/jamf-cli/internal/commands/pro/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
)

// BackupResource is a curated entry in the backup/diff resource set. Each entry
// names a generated CLI command (Key) whose list+get endpoints are resolved at
// runtime from generated.BackupEndpoints. FilterName is the user-facing token
// accepted by --resources (e.g. "profiles" covers both macOS and iOS profiles);
// SubDir is the output directory under the backup root.
//
// Rule of thumb: prefer a modern-API resource over its classic counterpart when
// both exist (modern responses are richer and paginated). Only fall back to
// classic when there is no modern equivalent.
type BackupResource struct {
	Key        string // lookup in generated.BackupEndpoints
	FilterName string // --resources filter token
	SubDir     string // output subdirectory under --output
	// ListOnly bypasses the per-ID detail fetch — each list item is written
	// directly to disk as its own file. Used for resources whose list response
	// is already the complete record (e.g. sites: {id, name}).
	ListOnly bool
	// ScopePath, when set, names a per-ID device-scope endpoint
	// (e.g. "/v2/computer-prestages/{id}/scope"). After fetching each detail
	// record the backup/diff code fetches this path and embeds the sorted
	// serial numbers under a "scope" key so the assignment list travels with
	// the prestage config in a single, diff-friendly file.
	ScopePath string
	// DropKeys names top-level response keys that are executed output rather
	// than configuration, removed by dropResponseKeys in both backup and diff.
	//
	// A Classic advanced-search GET runs the search and returns the devices it
	// currently matches. That membership is not configuration: it churns on
	// every inventory change, it is never equal between two instances, and it
	// carries device names and UDIDs into a directory meant for version
	// control. StripServerFields cannot do this job — it drops ids and
	// timestamps generically, and it is skipped under --include-ids, which is
	// about identifiers rather than about membership.
	DropKeys []string
}

// dropResponseKeys removes the executed-output keys a resource declares. One
// function for backup and diff, because a resource whose backup omits a key and
// whose diff compares it reports a permanent modification on that key.
func dropResponseKeys(obj map[string]any, keys []string) map[string]any {
	for _, k := range keys {
		delete(obj, k)
	}
	return obj
}

// BackupResources is the curated set of resources included in `backup` and
// `diff`. Endpoint paths live in generated/backup_registry.go; this file only
// decides which resources are in scope and how they lay out on disk.
//
// Grouping follows a logical config-object order: policies/profiles first, then
// scripts/EAs/groups, then supporting objects, then administration.
var BackupResources = []BackupResource{
	// Policies
	{Key: "classic-policies", FilterName: "policies", SubDir: "policies"},

	// Configuration profiles (no modern equivalent for CRUD)
	{Key: "classic-macos-config-profiles", FilterName: "profiles", SubDir: "profiles/macos"},
	{Key: "classic-mobile-config-profiles", FilterName: "profiles", SubDir: "profiles/ios"},

	// Prestage enrollments — modern v3 config + embedded device scope (serials).
	{Key: "computer-prestages", FilterName: "prestages", SubDir: "prestages/computers", ScopePath: "/v2/computer-prestages/{id}/scope"},
	{Key: "mobile-device-prestages", FilterName: "prestages", SubDir: "prestages/mobile", ScopePath: "/v2/mobile-device-prestages/{id}/scope"},

	// Scripts
	{Key: "scripts", FilterName: "scripts", SubDir: "scripts"},

	// Extension attributes — modern for computer + mobile, classic only for user
	{Key: "computer-extension-attributes", FilterName: "extension-attributes", SubDir: "extension-attributes/computer"},
	{Key: "mobile-device-extension-attributes", FilterName: "extension-attributes", SubDir: "extension-attributes/mobile"},
	{Key: "classic-user-ext-attrs", FilterName: "extension-attributes", SubDir: "extension-attributes/user"},

	// Computer groups — separate smart/static endpoints avoid the deprecated
	// /v1/computer-groups combined endpoint that triggers 403s on role
	// configurations without the legacy umbrella privilege.
	//
	// Both keys are the v3 resources. `static-computer-groups` names the same
	// objects on v2, whose five operations the gateway withdrew, so an entry on
	// it made `backup` and `diff` send a request that
	// `pro static-computer-groups list` refuses on the same profile — and under
	// --allow-partial-failure produced a backup silently missing every static
	// computer group. DeduplicateVersioned keys a family on a V<n> name suffix,
	// so a pair whose derived names differ is never collapsed and both survive:
	// picking between them is this file's job.
	{Key: "computer-groups-smart-groups", FilterName: "smart-groups", SubDir: "smart-groups/computers"},
	{Key: "computer-groups-static-groups", FilterName: "static-groups", SubDir: "static-groups/computers"},

	// Mobile device groups
	{Key: "mobile-device-groups-smart-groups", FilterName: "smart-groups", SubDir: "smart-groups/mobile"},
	{Key: "mobile-device-groups-static-groups", FilterName: "static-groups", SubDir: "static-groups/mobile"},

	// Advanced searches — one token, two APIs, because there is no modern
	// advanced-computer-searches spec at all: specs/ carries only
	// AdvancedMobileDeviceSearch.yaml and AdvancedUserContentSearch.yaml. So the
	// asymmetry is not a preference but the modern-over-classic rule above
	// answering differently for the two halves, and all four list/get paths
	// declare GET in specs/gateway/coverage.json.
	{Key: "classic-advanced-computer-searches", FilterName: "advanced-searches", SubDir: "advanced-searches/computers", DropKeys: []string{"computers"}},
	{Key: "advanced-mobile-device-searches", FilterName: "advanced-searches", SubDir: "advanced-searches/mobile"},

	// Supporting objects (modern preferred)
	{Key: "categories", FilterName: "categories", SubDir: "categories"},
	{Key: "buildings", FilterName: "buildings", SubDir: "buildings"},
	{Key: "departments", FilterName: "departments", SubDir: "departments"},
	// Sites have no per-ID detail endpoint — the list response is the full
	// record, so each site is written directly without a fan-out fetch.
	{Key: "sites", FilterName: "sites", SubDir: "sites", ListOnly: true},

	// App Store applications — classic only
	{Key: "classic-mac-apps", FilterName: "mac-apps", SubDir: "mac-apps"},
	{Key: "classic-mobile-apps", FilterName: "mobile-apps", SubDir: "mobile-apps"},

	// Packages, printers, dock items — classic only
	{Key: "classic-packages", FilterName: "packages", SubDir: "packages"},
	{Key: "classic-printers", FilterName: "printers", SubDir: "printers"},
	{Key: "classic-dock-items", FilterName: "dock-items", SubDir: "dock-items"},

	// Network + security config — classic only
	{Key: "classic-network-segments", FilterName: "network-segments", SubDir: "network-segments"},
	{Key: "classic-restricted-software", FilterName: "restricted-software", SubDir: "restricted-software"},
	{Key: "classic-disk-encryption-configs", FilterName: "disk-encryption", SubDir: "disk-encryption"},
	// Patch software titles — the Pro API's configurations, not Classic's
	// /patchsoftwaretitles. capi v1993 withdrew every read on that Classic
	// resource (only POST /id/{} survives), so the classic key's ListPath and
	// GetPath are both refused on a gateway profile. Same objects, and the
	// modern-over-classic preference above already pointed here.
	{Key: "patch-software-title-configurations", FilterName: "patch-titles", SubDir: "patch-titles"},

	// Administration — accounts (users + groups, split by classic list_subset)
	{Key: "classic-account-users", FilterName: "accounts", SubDir: "accounts/users"},
	{Key: "classic-account-groups", FilterName: "accounts", SubDir: "accounts/groups"},
}

// ResolvedBackupResource couples a curated entry with its generated endpoint
// metadata. Runtime code iterates the resolved form so it doesn't need to
// cross-reference two slices at every step.
type ResolvedBackupResource struct {
	BackupResource
	generated.BackupEndpoint
}

// ResolveBackupResources joins the curated BackupResources against the
// generated endpoint registry, optionally filtering by user-supplied resource
// names (matched against FilterName). An unknown filter name produces an empty
// result; a filter name that matches zero curated entries is reported by the
// caller.
//
// An entry whose Key is missing from generated.BackupEndpoints is a programming
// error — a regenerated registry and a stale curation list have diverged —
// and surfaces via an error so tests catch the drift at build time.
func ResolveBackupResources(filter []string) ([]ResolvedBackupResource, error) {
	nameSet := make(map[string]bool, len(filter))
	for _, n := range filter {
		nameSet[n] = true
	}

	var out []ResolvedBackupResource
	for _, r := range BackupResources {
		if len(nameSet) > 0 && !nameSet[r.FilterName] {
			continue
		}
		ep, ok := generated.BackupEndpoints[r.Key]
		if !ok {
			return nil, fmt.Errorf("backup resource %q references unknown endpoint key %q — regenerate the registry", r.FilterName, r.Key)
		}
		out = append(out, ResolvedBackupResource{
			BackupResource: r,
			BackupEndpoint: ep,
		})
	}
	return out, nil
}

// isKnownBackupFilter returns true if name matches any curated BackupResource
// FilterName or any non-standard filter handled outside BackupResources. Used
// by runBackup to distinguish "no results because it's a non-standard resource"
// from "no results because the user typed a garbage filter name".
func isKnownBackupFilter(name string) bool {
	for _, r := range BackupResources {
		if r.FilterName == name {
			return true
		}
	}
	for _, n := range nonStandardBackupFilters {
		if n.FilterName == name {
			return true
		}
	}
	return false
}

// nonStandardBackupFilter describes a backup resource handled outside
// BackupResources. NameField is the field its documents keep their name in, the
// same role BackupEndpoint.NameField plays for a curated resource: `diff` reads
// it to key the resource's objects the way the live side does. Leave it empty
// when the name is in "name", which backupObjectName already finds.
type nonStandardBackupFilter struct {
	FilterName string
	NameField  string
	// Source names the API or mechanism backing the resource, in the same
	// vocabulary `backup list-resources` derives for a curated entry. It is
	// stated rather than derived because these resources have no
	// generated.BackupEndpoint to read IsClassic off.
	Source string
}

// nonStandardBackupFilters lists the backup resources that are handled outside
// BackupResources (CSV downloads, SDK-backed resources, etc.). These appear in
// BackupFilterNames so shell completion and help text stay accurate even though
// they have no entry in the curated list.
//
// One table rather than two: a name field listed apart from the filter name can
// name a directory no resource writes, and nothing would catch it. `diff` reads
// NameField from here for the resources it finds in the backup root.
var nonStandardBackupFilters = []nonStandardBackupFilter{
	// downloaded as a single CSV via /v2/inventory-preload/csv
	{FilterName: "inventory-preloads", Source: "csv download"},
	// Platform SDK; blueprintToExport emits "name"
	{FilterName: "blueprints", Source: "platform sdk"},
	// Platform SDK; benchmarkToExport emits the name as "title"
	{FilterName: "compliance-benchmarks", NameField: "title", Source: "platform sdk"},
}

// nonStandardBackupNameField returns the name field declared for a
// non-standard backup resource, or "" when it declares none or is not one.
func nonStandardBackupNameField(filterName string) string {
	for _, n := range nonStandardBackupFilters {
		if n.FilterName == filterName {
			return n.NameField
		}
	}
	return ""
}

// BackupFilterNames returns the unique set of FilterName values (sorted) — used
// for CLI help text and completion hints.
func BackupFilterNames() []string {
	seen := make(map[string]bool, len(BackupResources)+len(nonStandardBackupFilters))
	var names []string
	for _, r := range BackupResources {
		if !seen[r.FilterName] {
			seen[r.FilterName] = true
			names = append(names, r.FilterName)
		}
	}
	for _, n := range nonStandardBackupFilters {
		if !seen[n.FilterName] {
			seen[n.FilterName] = true
			names = append(names, n.FilterName)
		}
	}
	sort.Strings(names)
	return names
}

// BackupSubDirs maps each curated resource's on-disk subdirectory (relative to
// the backup root, slash-separated) to the FilterName that owns it. `diff`
// reads this table rather than walking the backup tree, so files off disk are
// bucketed under exactly the key live mode uses and a directory and an instance
// are comparable; it also uses the key set to tell which directories in the
// backup root a curated resource already owns from those it must key by name.
//
// It matters because many of the curated resources nest two levels deep
// (profiles/macos, smart-groups/computers, accounts/users,
// advanced-searches/computers, …). `diff` used to treat every top-level
// directory as a resource and read only the files sitting directly inside it, so
// every nested resource contributed nothing to either snapshot and its changes
// were reported as no change at all — silently, exit 0.
//
// Deliberately no tally: this comment said thirteen while the table held
// fifteen, and a number nothing reads is wrong again the first time the table
// grows. Read the table.
func BackupSubDirs() map[string]string {
	out := make(map[string]string, len(BackupResources))
	for _, r := range BackupResources {
		out[r.SubDir] = r.FilterName
	}
	return out
}

// backupResourceNoCommand is the "objects" note for a non-standard filter. The
// column names the generated commands behind a token, and these tokens are
// precisely the ones with no entry in generated.BackupEndpoints.
const backupResourceNoCommand = "no generated command"

// backupResourceRows renders one row per distinct --resources token for
// `pro backup list-resources`. The token order and the token set both come from
// BackupFilterNames, so a token that exists for `--resources` cannot go
// unlisted here.
//
// Every row carries all three keys deliberately: a table's columns are the keys
// of its first row, so a key some rows omit is a column that appears and
// disappears with the sort order. backupResourceRowsForFormat drops a key from
// every row at once, which is a different thing.
func backupResourceRows() ([]map[string]any, error) {
	resolved, err := ResolveBackupResources(nil)
	if err != nil {
		return nil, err
	}

	objects := make(map[string][]string, len(resolved))
	apis := make(map[string]map[string]bool, len(resolved))
	for _, r := range resolved {
		objects[r.FilterName] = append(objects[r.FilterName], r.Key)
		api := "pro api"
		if r.IsClassic {
			api = "classic api"
		}
		if apis[r.FilterName] == nil {
			apis[r.FilterName] = make(map[string]bool, 2)
		}
		apis[r.FilterName][api] = true
	}

	notes := make(map[string]string, len(objects)+len(nonStandardBackupFilters))
	sources := make(map[string]string, len(objects)+len(nonStandardBackupFilters))
	for _, n := range nonStandardBackupFilters {
		notes[n.FilterName] = backupResourceNoCommand
		sources[n.FilterName] = n.Source
	}
	for name, keys := range objects {
		sort.Strings(keys)
		notes[name] = strings.Join(keys, ", ")

		names := make([]string, 0, len(apis[name]))
		for api := range apis[name] {
			names = append(names, api)
		}
		sort.Strings(names)
		sources[name] = strings.Join(names, ", ")
	}

	tokens := BackupFilterNames()
	rows := make([]map[string]any, 0, len(tokens))
	for _, t := range tokens {
		rows = append(rows, map[string]any{
			"resource": t,
			"objects":  notes[t],
			"source":   sources[t],
		})
	}
	return rows, nil
}

// backupResourceRowsForFormat adapts the `pro backup list-resources` rows to the
// output format, the way listRowsForFormat does for `config list`.
//
// The data-preserving formats get the rows untouched, objects included; every
// other format gets resource and source. sortedKeys (internal/output) floats
// only id and name, so the rest are alphabetical: objects led, and being a
// joined command list it is by far the widest value, which left the token this
// command exists to report in the middle of a 137-character row. Dropping the
// column is what the formatter allows, since printTable takes []map[string]any
// and a struct's field order has nowhere to be read from. Nothing parsing the
// output loses the backing commands, because the kept formats keep all three.
//
// The switch names the formats that keep the column rather than the ones that
// drop it, because the formatter's own dispatch has a default arm that renders a
// table and nothing normalises the case of a -o value. Matching table, csv and
// plain exactly therefore restored the buried column for -o "", -o Table,
// -o TABLE, -o tabel, -o plaintext and -o bogus, every one of which prints a
// table at exit 0. Raw and xml are here because PrintRaw hands both of them the
// bytes without the JSON-to-rows conversion, so a caller asking for either
// wants the field rather than a narrower table.
//
// The list is selectTableColumns' (generated/registry.go) exactly, and
// json-multi is absent from both for the same reason: it means JSON on the wire
// and a table on the screen. `multi` captures every display format as
// json-multi and re-renders it, and Print's switch has no case for it, so it
// reaches printTable — the one route by which the buried column could still
// reach a terminal.
func backupResourceRowsForFormat(rows []map[string]any, format string) []map[string]any {
	switch output.Format(format) {
	case output.FormatJSON, output.FormatYAML, output.FormatNDJSON,
		output.FormatXML, output.FormatRaw:
		return rows
	}

	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]any{
			"resource": r["resource"],
			"source":   r["source"],
		})
	}
	return out
}
