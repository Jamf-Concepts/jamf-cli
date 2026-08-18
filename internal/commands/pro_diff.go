// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/client"
	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/blueprints"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/compliancebenchmarks"
)

// diffChangeKind classifies a single diff result.
type diffChangeKind string

const (
	diffAdded    diffChangeKind = "added"
	diffRemoved  diffChangeKind = "removed"
	diffModified diffChangeKind = "modified"
)

// diffResult holds a single diff finding.
type diffResult struct {
	Resource string         `json:"resource"`
	Name     string         `json:"name"`
	Change   diffChangeKind `json:"change"`
	// Field is set for Modified changes; empty for Added/Removed.
	Field    string `json:"field,omitempty"`
	OldValue string `json:"old_value,omitempty"`
	NewValue string `json:"new_value,omitempty"`
}

// diffOptions holds parsed flag values for the diff command.
type diffOptions struct {
	Source    string
	Target    string
	Resources string
}

func newDiffCmd() *cobra.Command {
	var opts diffOptions

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Compare configuration between two Jamf Pro instances or backup directories",
		Long: `Compare configuration objects between two sources.

Each source can be either:
  - A config profile name (e.g., "production", "staging")
  - A local backup directory (paths starting with /, ./, or ~)

Examples:
  jamf-cli diff --source staging --target production
  jamf-cli diff --source ./backup-2026-01 --target production
  jamf-cli diff --source ./old-backup --target ./new-backup
  jamf-cli diff --source staging --target production --resources policies,scripts`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringVar(&opts.Source, "source", "", "source: config profile name or backup directory path (required)")
	cmd.Flags().StringVar(&opts.Target, "target", "", "target: config profile name or backup directory path (required)")
	cmd.Flags().StringVar(&opts.Resources, "resources", "", "comma-separated resource filter (e.g., policies,scripts)")
	_ = cmd.MarkFlagRequired("source")
	_ = cmd.MarkFlagRequired("target")

	return cmd
}

// isDirectoryPath returns true when the string looks like a filesystem path.
// Paths must start with /, ./, or ~ to be treated as directories; everything
// else is interpreted as a profile name.
func isDirectoryPath(s string) bool {
	return strings.HasPrefix(s, "/") ||
		strings.HasPrefix(s, "./") ||
		strings.HasPrefix(s, "~/") ||
		s == "." || s == "~"
}

// resourceSnapshot maps resource type name → (object name → stripped fields).
type resourceSnapshot map[string]map[string]map[string]any

// loadSourceSnapshot loads objects from either a backup directory or a live profile.
func loadSourceSnapshot(ctx context.Context, source string, nameFilter []string) (resourceSnapshot, error) {
	if isDirectoryPath(source) {
		return loadSnapshotFromDirectory(source, nameFilter)
	}
	return loadSnapshotFromProfile(ctx, source, nameFilter)
}

// loadSnapshotFromDirectory reads YAML/JSON backup files written by `backup`.
// The directory layout is: <dir>/<resource-subdir>/<object>.yaml (or .json).
func loadSnapshotFromDirectory(dir string, nameFilter []string) (resourceSnapshot, error) {
	// Expand ~ to home directory.
	if strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("expanding ~: %w", err)
		}
		dir = filepath.Join(home, dir[2:])
	}

	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("accessing directory %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%q is not a directory", dir)
	}

	// Warn if a _failures.yaml / _failures.json exists in the backup root.
	for _, name := range []string{"_failures.yaml", "_failures.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			fmt.Fprintf(os.Stderr, "WARNING: %s found in %s — backup may be incomplete\n", name, dir)
		}
	}

	// Build a set of allowed resource names for quick lookup.
	allowedResources := buildNameSet(nameFilter)

	snapshot := make(resourceSnapshot)

	// Walk one level of subdirectories; each subdir is a resource type.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading directory %q: %w", dir, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue // skip files at the root (e.g., _failures.yaml)
		}

		resourceName := entry.Name()
		if len(allowedResources) > 0 && !allowedResources[resourceName] {
			continue
		}

		subDir := filepath.Join(dir, resourceName)
		objects, err := readObjectsFromSubdir(subDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: reading %s: %v\n", subDir, err)
			continue
		}
		if len(objects) > 0 {
			snapshot[resourceName] = objects
		}
	}

	return snapshot, nil
}

// readObjectsFromSubdir reads all .yaml and .json files in subDir and returns
// a map of object name → fields (with _meta stripped).
func readObjectsFromSubdir(subDir string) (map[string]map[string]any, error) {
	entries, err := os.ReadDir(subDir)
	if err != nil {
		return nil, err
	}

	objects := make(map[string]map[string]any)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".json") {
			continue
		}

		path := filepath.Join(subDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: reading file %s: %v\n", path, err)
			continue
		}

		var obj map[string]any
		if strings.HasSuffix(name, ".json") {
			if err := json.Unmarshal(data, &obj); err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: parsing %s: %v\n", path, err)
				continue
			}
		} else {
			if err := yaml.Unmarshal(data, &obj); err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: parsing %s: %v\n", path, err)
				continue
			}
			// yaml.v3 decodes maps as map[string]interface{} — normalise to
			// consistent types by round-tripping through JSON.
			obj = normaliseViaJSON(obj)
		}

		// Strip _meta block added by backup — not part of the config.
		delete(obj, "_meta")

		// Use the "name" field as the object key; fall back to the filename stem.
		objName := ""
		if n, ok := obj["name"].(string); ok && n != "" {
			objName = n
		} else {
			stem := strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".json")
			objName = stem
		}

		objects[objName] = obj
	}

	return objects, nil
}

// normaliseViaJSON round-trips a map through JSON to coerce yaml.v3 types
// (e.g., map[string]interface{} nested under interface{}) to the same types
// produced by json.Unmarshal. This makes deep equality comparisons reliable.
func normaliseViaJSON(v map[string]any) map[string]any {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return v
	}
	return out
}

// loadSnapshotFromProfile resolves auth for the named profile, then fetches
// all resource objects from the live Jamf Pro instance.
func loadSnapshotFromProfile(ctx context.Context, profileName string, nameFilter []string) (resourceSnapshot, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	resolvedURL, authProvider, err := ResolveAuthForProfile(cfg, AuthParams{Profile: profileName})
	if err != nil {
		return nil, fmt.Errorf("resolving auth for profile %q: %w", profileName, err)
	}

	httpCli := &cliClient{client.New(resolvedURL, authProvider, client.WithVerbose(verboseLevel))}

	defs, err := ResolveBackupResources(nameFilter)
	if err != nil {
		return nil, err
	}
	if len(defs) == 0 && len(nameFilter) > 0 {
		return nil, fmt.Errorf("no resources match filter %q", strings.Join(nameFilter, ","))
	}

	snapshot := make(resourceSnapshot)

	for _, def := range defs {
		items, err := listResourceItems(ctx, httpCli, def)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: listing %s from profile %q: %v\n", def.Key, profileName, err)
			continue
		}

		if len(items) == 0 {
			continue
		}

		objects := make(map[string]map[string]any)
		for _, item := range items {
			path := strings.Replace(def.GetPath, "{id}", item.ID, 1)
			data, err := fetchJSON(ctx, httpCli, path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: fetching %s id=%s from %q: %v\n", def.Key, item.ID, profileName, err)
				continue
			}
			if def.ScopePath != "" {
				if serials, serr := fetchPrestageScope(ctx, httpCli, def.ScopePath, item.ID); serr == nil {
					data["scope"] = serials
				} else {
					fmt.Fprintf(os.Stderr, "WARNING: fetching scope for %s id=%s from %q: %v\n", def.Key, item.ID, profileName, serr)
				}
			}
			data = unwrapClassicDetail(data)
			data = StripServerFields(data)

			objName := item.Name
			if objName == "" {
				objName = item.ID
			}
			objects[objName] = data
		}

		if len(objects) > 0 {
			// Merge into existing bucket for this filter name (multiple curated
			// entries — e.g. macOS + iOS profiles, or accounts users + groups —
			// share a single FilterName token).
			if existing, ok := snapshot[def.FilterName]; ok {
				maps.Copy(existing, objects)
			} else {
				snapshot[def.FilterName] = objects
			}
		}
	}

	// Load platform resources when platform auth is available
	if p, ok := authProvider.(*auth.PlatformOAuth2Provider); ok {
		wantPlatform := func(name string) bool {
			if len(nameFilter) == 0 {
				return true
			}
			for _, n := range nameFilter {
				if n == name {
					return true
				}
			}
			return false
		}

		sdk := newPlatformSDKClient(resolvedURL, p.ClientID(), p.ClientSecret(), p.TenantID(),
			resolveSecurityCloudTenantID(cfg, profileName), !quiet && verboseLevel == 0)

		if wantPlatform("blueprints") {
			bp := blueprints.New(sdk)
			bps, err := bp.ListBlueprints(ctx, nil, "")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not list blueprints: %v\n", err)
			} else {
				objects := make(map[string]map[string]any)
				for _, item := range bps {
					detail, err := bp.GetBlueprint(ctx, item.ID)
					if err != nil {
						continue
					}
					exp := blueprintToExport(ctx, sdk, detail)
					objects[detail.Name] = normaliseViaJSON(structToMap(exp))
				}
				if len(objects) > 0 {
					snapshot["blueprints"] = objects
				}
			}
		}

		if wantPlatform("compliance-benchmarks") {
			cb := compliancebenchmarks.New(sdk)
			resp, err := cb.ListBenchmarks(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not list benchmarks: %v\n", err)
			} else {
				objects := make(map[string]map[string]any)
				for _, b := range resp.Benchmarks {
					bm, err := cb.GetBenchmark(ctx, b.ID)
					if err != nil {
						continue
					}
					obj := map[string]any{
						"title":           bm.Title,
						"description":     bm.Description,
						"baselineId":      bm.BaselineID,
						"enforcementMode": bm.EnforcementMode,
						"target":          bm.Target,
					}
					objects[bm.Title] = normaliseViaJSON(obj)
				}
				if len(objects) > 0 {
					snapshot["compliance-benchmarks"] = objects
				}
			}
		}
	}

	return snapshot, nil
}

// structToMap converts a struct to map[string]any via JSON round-trip.
func structToMap(v any) map[string]any {
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	return m
}

// buildNameSet returns a set from a slice of strings; an empty slice returns nil
// (meaning "no filter applied").
func buildNameSet(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	s := make(map[string]bool, len(names))
	for _, n := range names {
		s[n] = true
	}
	return s
}

// compareSnapshots computes the diff between source and target snapshots.
// Returns a flat list of diffResult entries ordered by resource → name → field.
func compareSnapshots(source, target resourceSnapshot) []diffResult {
	// Collect all resource type names seen in either snapshot.
	resourceNames := make(map[string]bool)
	for k := range source {
		resourceNames[k] = true
	}
	for k := range target {
		resourceNames[k] = true
	}

	sortedResources := sortedKeys(resourceNames)
	var results []diffResult

	for _, resource := range sortedResources {
		srcObjects := source[resource] // may be nil
		tgtObjects := target[resource] // may be nil

		// Collect all object names.
		allNames := make(map[string]bool)
		for n := range srcObjects {
			allNames[n] = true
		}
		for n := range tgtObjects {
			allNames[n] = true
		}

		for _, name := range sortedKeys(allNames) {
			srcObj, inSrc := srcObjects[name]
			tgtObj, inTgt := tgtObjects[name]

			switch {
			case inSrc && !inTgt:
				results = append(results, diffResult{
					Resource: resource,
					Name:     name,
					Change:   diffRemoved,
				})

			case !inSrc && inTgt:
				results = append(results, diffResult{
					Resource: resource,
					Name:     name,
					Change:   diffAdded,
				})

			default:
				// Both present — check field-level differences.
				fieldDiffs := diffObjects(srcObj, tgtObj)
				for _, fd := range fieldDiffs {
					results = append(results, diffResult{
						Resource: resource,
						Name:     name,
						Change:   diffModified,
						Field:    fd.field,
						OldValue: fd.oldVal,
						NewValue: fd.newVal,
					})
				}
			}
		}
	}

	return results
}

type fieldDiff struct {
	field  string
	oldVal string
	newVal string
}

// diffObjects performs a shallow field-level comparison between two maps.
// For nested objects, it compares their JSON representation as a single field.
func diffObjects(src, tgt map[string]any) []fieldDiff {
	allKeys := make(map[string]bool)
	for k := range src {
		allKeys[k] = true
	}
	for k := range tgt {
		allKeys[k] = true
	}

	var diffs []fieldDiff
	for _, k := range sortedKeys(allKeys) {
		sv := src[k]
		tv := tgt[k]

		if reflect.DeepEqual(sv, tv) {
			continue
		}

		diffs = append(diffs, fieldDiff{
			field:  k,
			oldVal: formatFieldValue(sv),
			newVal: formatFieldValue(tv),
		})
	}
	return diffs
}

// formatFieldValue converts a field value to a compact string for display.
func formatFieldValue(v any) string {
	if v == nil {
		return "<nil>"
	}
	switch val := v.(type) {
	case string:
		return val
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
}

// runDiff is the main entry point called by the cobra RunE.
func runDiff(ctx context.Context, opts diffOptions) error {
	// Parse resource filter.
	var nameFilter []string
	if opts.Resources != "" {
		for r := range strings.SplitSeq(opts.Resources, ",") {
			if t := strings.TrimSpace(r); t != "" {
				nameFilter = append(nameFilter, t)
			}
		}
	}

	fmt.Fprintf(os.Stderr, "Loading source: %s\n", opts.Source)
	srcSnapshot, err := loadSourceSnapshot(ctx, opts.Source, nameFilter)
	if err != nil {
		return fmt.Errorf("loading source %q: %w", opts.Source, err)
	}

	fmt.Fprintf(os.Stderr, "Loading target: %s\n", opts.Target)
	tgtSnapshot, err := loadSourceSnapshot(ctx, opts.Target, nameFilter)
	if err != nil {
		return fmt.Errorf("loading target %q: %w", opts.Target, err)
	}

	results := compareSnapshots(srcSnapshot, tgtSnapshot)

	if len(results) == 0 {
		fmt.Fprintln(os.Stderr, "No differences found.")
		return nil
	}

	// Convert to output rows.
	rows := make([]map[string]any, len(results))
	for i, r := range results {
		row := map[string]any{
			"resource": r.Resource,
			"name":     r.Name,
			"change":   string(r.Change),
		}
		if r.Field != "" {
			row["field"] = r.Field
			row["old_value"] = r.OldValue
			row["new_value"] = r.NewValue
		}
		rows[i] = row
	}

	formatter := output.New(outputFmt, noColor, wide)
	return formatter.Print(rows)
}
