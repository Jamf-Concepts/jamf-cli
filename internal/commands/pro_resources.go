// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"sort"

	"github.com/Jamf-Concepts/jamf-cli/internal/commands/pro/generated"
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

	// Computer groups — modern v2 endpoints (separate smart/static avoids the
	// deprecated /v1/computer-groups combined endpoint that triggers 403s on
	// role configurations without the legacy umbrella privilege).
	{Key: "computer-groups-smart-groups", FilterName: "smart-groups", SubDir: "smart-groups/computers"},
	{Key: "static-computer-groups", FilterName: "static-groups", SubDir: "static-groups/computers"},

	// Mobile device groups
	{Key: "mobile-device-groups-smart-groups", FilterName: "smart-groups", SubDir: "smart-groups/mobile"},
	{Key: "mobile-device-groups-static-groups", FilterName: "static-groups", SubDir: "static-groups/mobile"},

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
	{Key: "classic-patch-titles", FilterName: "patch-titles", SubDir: "patch-titles"},

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
	{FilterName: "inventory-preloads"},
	// Platform SDK; blueprintToExport emits "name"
	{FilterName: "blueprints"},
	// Platform SDK; benchmarkToExport emits the name as "title"
	{FilterName: "compliance-benchmarks", NameField: "title"},
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
// It matters because thirteen of the curated resources nest two levels deep
// (profiles/macos, smart-groups/computers, accounts/users, …). `diff` used to
// treat every top-level directory as a resource and read only the files sitting
// directly inside it, so those thirteen contributed nothing to either snapshot
// and their changes were reported as no change at all — silently, exit 0.
func BackupSubDirs() map[string]string {
	out := make(map[string]string, len(BackupResources))
	for _, r := range BackupResources {
		out[r.SubDir] = r.FilterName
	}
	return out
}
