// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// backupFailure records a single resource fetch failure.
type backupFailure struct {
	Resource string `json:"resource" yaml:"resource"`
	Path     string `json:"path" yaml:"path"`
	Error    string `json:"error" yaml:"error"`
}

// backupMeta is written into each exported file for versioning.
type backupMeta struct {
	SchemaVersion int    `json:"schema_version" yaml:"schema_version"`
	CLIVersion    string `json:"cli_version" yaml:"cli_version"`
	ResourceType  string `json:"resource_type" yaml:"resource_type"`
	ExportedAt    string `json:"exported_at" yaml:"exported_at"`
	SourceURL     string `json:"source_url" yaml:"source_url"`
}

func newBackupCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		outputDir   string
		format      string
		resources   string
		includeIDs  bool
		concurrency int
	)

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Export all Jamf Pro configuration to a local directory",
		Long: `Export configuration objects from a Jamf Pro instance to a local directory.

Each object is saved as an individual YAML or JSON file. Server-generated fields
(id, timestamps) are stripped by default for clean version-control diffs.

Partial failures are tolerated — objects that fail to fetch are recorded in
_failures.yaml and the backup continues.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBackup(cmd.Context(), cliCtx, backupOptions{
				OutputDir:   outputDir,
				Format:      format,
				Resources:   resources,
				IncludeIDs:  includeIDs,
				Concurrency: concurrency,
			})
		},
	}

	cmd.Flags().StringVar(&outputDir, "output", "", "destination directory (required)")
	cmd.Flags().StringVar(&format, "format", "yaml", "output format: yaml or json")
	cmd.Flags().StringVar(&resources, "resources", "", "comma-separated resource filter (e.g., policies,scripts)")
	cmd.Flags().BoolVar(&includeIDs, "include-ids", false, "retain server-generated IDs in output")
	cmd.Flags().IntVar(&concurrency, "concurrency", 10, "max parallel API requests")
	_ = cmd.MarkFlagRequired("output")

	return cmd
}

type backupOptions struct {
	OutputDir   string
	Format      string
	Resources   string
	IncludeIDs  bool
	Concurrency int
}

func runBackup(ctx context.Context, cliCtx *registry.CLIContext, opts backupOptions) error {
	client := cliCtx.Client

	// Filter resources if specified
	var nameFilter []string
	if opts.Resources != "" {
		nameFilter = strings.Split(opts.Resources, ",")
		for i := range nameFilter {
			nameFilter[i] = strings.TrimSpace(nameFilter[i])
		}
	}
	defs := FilterResources(BackupResources, nameFilter)
	if len(defs) == 0 {
		return fmt.Errorf("no resources match filter %q", opts.Resources)
	}

	ext := ".yaml"
	if opts.Format == "json" {
		ext = ".json"
	}

	var failures []backupFailure
	totalExported := 0

	for _, def := range defs {
		// Create output subdirectory
		subDir := filepath.Join(opts.OutputDir, def.SubDir)
		if err := os.MkdirAll(subDir, 0o755); err != nil {
			return fmt.Errorf("creating directory %s: %w", subDir, err)
		}

		// List objects for this resource type
		items, err := listResourceItems(ctx, client, def)
		if err != nil {
			failures = append(failures, backupFailure{
				Resource: def.Name,
				Path:     def.ListPath,
				Error:    err.Error(),
			})
			fmt.Fprintf(os.Stderr, "WARNING: failed to list %s: %v\n", def.Name, err)
			continue
		}

		if len(items) == 0 {
			continue
		}

		// Fetch each object's details in parallel
		slugSeen := make(map[string]bool)
		type itemResult struct {
			Name string
			Data map[string]any
		}

		results, errs := BoundedParallelFetch(ctx, items, opts.Concurrency, func(ctx context.Context, item resourceItem) (itemResult, error) {
			path := strings.Replace(def.GetPath, "{id}", item.ID, 1)
			data, err := fetchJSON(ctx, client, path)
			if err != nil {
				return itemResult{}, fmt.Errorf("%s id=%s: %w", def.Name, item.ID, err)
			}
			return itemResult{Name: item.Name, Data: data}, nil
		})

		for _, e := range errs {
			failures = append(failures, backupFailure{
				Resource: def.Name,
				Path:     def.GetPath,
				Error:    e.Error(),
			})
		}

		// Write each object to a file
		for _, r := range results {
			obj := r.Data

			// Unwrap Classic API single-object wrapper if present
			obj = unwrapClassicDetail(obj)

			if !opts.IncludeIDs {
				obj = StripServerFields(obj)
			}

			// Add _meta block
			obj["_meta"] = backupMeta{
				SchemaVersion: 1,
				CLIVersion:    cliVersion,
				ResourceType:  def.Name,
				ExportedAt:    time.Now().UTC().Format(time.RFC3339),
				SourceURL:     serverURL,
			}

			slug := SlugifyName(r.Name)
			slug = DeduplicateSlug(slug, slugSeen)

			outPath := filepath.Join(subDir, slug+ext)
			if err := writeBackupFile(outPath, obj, opts.Format); err != nil {
				failures = append(failures, backupFailure{
					Resource: def.Name,
					Path:     outPath,
					Error:    err.Error(),
				})
				continue
			}
			totalExported++
		}
	}

	// Platform resources (blueprints, compliance-benchmarks) via SDK
	if cliCtx.PlatformClient != nil {
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

		if wantPlatform("blueprints") {
			n, errs := backupBlueprints(ctx, cliCtx, opts)
			totalExported += n
			failures = append(failures, errs...)
		}
		if wantPlatform("compliance-benchmarks") {
			n, errs := backupBenchmarks(ctx, cliCtx, opts)
			totalExported += n
			failures = append(failures, errs...)
		}
	}

	// Write failures manifest if any
	if len(failures) > 0 {
		failPath := filepath.Join(opts.OutputDir, "_failures"+ext)
		if err := writeBackupFile(failPath, failures, opts.Format); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: could not write failures manifest: %v\n", err)
		}
	}

	// Print summary
	fmt.Fprintf(os.Stderr, "Backup complete: %d objects exported", totalExported)
	if len(failures) > 0 {
		fmt.Fprintf(os.Stderr, ", %d failures (see _failures%s)", len(failures), ext)
	}
	fmt.Fprintln(os.Stderr)

	if len(failures) > 0 {
		return fmt.Errorf("backup completed with %d failures (see _failures%s)", len(failures), ext)
	}
	return nil
}

// resourceItem is a minimal representation of a listed resource.
type resourceItem struct {
	ID   string
	Name string
}

// listResourceItems fetches the list of objects for a resource definition.
func listResourceItems(ctx context.Context, client registry.HTTPClient, def ResourceDef) ([]resourceItem, error) {
	if def.IsClassic {
		return listClassicItems(ctx, client, def)
	}
	return listModernItems(ctx, client, def)
}

func listClassicItems(ctx context.Context, client registry.HTTPClient, def ResourceDef) ([]resourceItem, error) {
	raw, err := FetchClassicList(ctx, client, def.ListPath, def.WrapperKey)
	if err != nil {
		return nil, err
	}

	var items []resourceItem
	for _, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		id := extractID(m)
		name := extractName(m)
		if id != "" {
			items = append(items, resourceItem{ID: id, Name: name})
		}
	}
	return items, nil
}

func listModernItems(ctx context.Context, client registry.HTTPClient, def ResourceDef) ([]resourceItem, error) {
	// FetchAllPaginated auto-detects array vs paginated responses
	all, err := FetchAllPaginated(ctx, client, def.ListPath, 100)
	if err != nil {
		return nil, err
	}

	var items []resourceItem
	for _, m := range all {
		id := extractID(m)
		name := extractName(m)
		if id != "" {
			items = append(items, resourceItem{ID: id, Name: name})
		}
	}
	return items, nil
}

// extractID gets the "id" field from a map, handling both string and float64.
func extractID(m map[string]any) string {
	return extractField(m, "id")
}

// extractName gets the "name" field from a map.
func extractName(m map[string]any) string {
	if n, ok := m["name"].(string); ok {
		return n
	}
	return extractID(m) // fallback to id if no name
}

// unwrapClassicDetail unwraps Classic API single-object responses.
// Classic GET /id/{id} returns {"policy": {...}} — we want the inner object.
func unwrapClassicDetail(obj map[string]any) map[string]any {
	if len(obj) == 1 {
		for _, v := range obj {
			if inner, ok := v.(map[string]any); ok {
				return inner
			}
		}
	}
	return obj
}

// writeBackupFile writes an object to disk in the specified format.
func writeBackupFile(path string, data any, format string) error {
	var content []byte
	var err error

	switch format {
	case "json":
		content, err = json.MarshalIndent(data, "", "  ")
		if err != nil {
			return err
		}
		content = append(content, '\n')
	default: // yaml
		content, err = yaml.Marshal(data)
		if err != nil {
			return err
		}
	}

	return os.WriteFile(path, content, 0o644)
}

// backupBlueprints exports all blueprints via the Platform SDK.
func backupBlueprints(ctx context.Context, cliCtx *registry.CLIContext, opts backupOptions) (int, []backupFailure) {
	pc := cliCtx.PlatformClient
	ext := ".yaml"
	if opts.Format == "json" {
		ext = ".json"
	}

	subDir := filepath.Join(opts.OutputDir, "blueprints")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		return 0, []backupFailure{{Resource: "blueprints", Path: subDir, Error: err.Error()}}
	}

	bps, err := pc.ListBlueprints(ctx, nil, "")
	if err != nil {
		return 0, []backupFailure{{Resource: "blueprints", Path: "list", Error: err.Error()}}
	}

	var failures []backupFailure
	exported := 0
	slugSeen := make(map[string]bool)

	for _, bp := range bps {
		detail, err := pc.GetBlueprint(ctx, bp.ID)
		if err != nil {
			failures = append(failures, backupFailure{Resource: "blueprints", Path: bp.ID, Error: err.Error()})
			continue
		}

		obj := blueprintToExport(detail)

		slug := SlugifyName(detail.Name)
		slug = DeduplicateSlug(slug, slugSeen)

		outPath := filepath.Join(subDir, slug+ext)
		if err := writeBackupFile(outPath, obj, opts.Format); err != nil {
			failures = append(failures, backupFailure{Resource: "blueprints", Path: outPath, Error: err.Error()})
			continue
		}
		exported++
	}

	return exported, failures
}

// backupBenchmarks exports all compliance benchmarks via the Platform SDK.
func backupBenchmarks(ctx context.Context, cliCtx *registry.CLIContext, opts backupOptions) (int, []backupFailure) {
	pc := cliCtx.PlatformClient
	ext := ".yaml"
	if opts.Format == "json" {
		ext = ".json"
	}

	subDir := filepath.Join(opts.OutputDir, "compliance-benchmarks")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		return 0, []backupFailure{{Resource: "compliance-benchmarks", Path: subDir, Error: err.Error()}}
	}

	resp, err := pc.ListBenchmarks(ctx)
	if err != nil {
		return 0, []backupFailure{{Resource: "compliance-benchmarks", Path: "list", Error: err.Error()}}
	}

	var failures []backupFailure
	exported := 0
	slugSeen := make(map[string]bool)

	for _, b := range resp.Benchmarks {
		bm, err := pc.GetBenchmark(ctx, b.ID)
		if err != nil {
			failures = append(failures, backupFailure{Resource: "compliance-benchmarks", Path: b.ID, Error: err.Error()})
			continue
		}

		// Strip server-generated fields for clean export
		obj := map[string]any{
			"title":           bm.Title,
			"description":     bm.Description,
			"baselineId":      bm.BaselineID,
			"enforcementMode": bm.EnforcementMode,
			"target":          bm.Target,
			"rules":           bm.Rules,
		}
		if len(bm.Sources) > 0 {
			obj["sources"] = bm.Sources
		}

		slug := SlugifyName(bm.Title)
		slug = DeduplicateSlug(slug, slugSeen)

		outPath := filepath.Join(subDir, slug+ext)
		if err := writeBackupFile(outPath, obj, opts.Format); err != nil {
			failures = append(failures, backupFailure{Resource: "compliance-benchmarks", Path: outPath, Error: err.Error()})
			continue
		}
		exported++
	}

	return exported, failures
}
