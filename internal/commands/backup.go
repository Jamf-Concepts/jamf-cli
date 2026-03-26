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

	"github.com/Jamf-Concepts/jamfpro-cli/internal/commands/generated"
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

func newBackupCmd(cliCtx *generated.CLIContext) *cobra.Command {
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

func runBackup(ctx context.Context, cliCtx *generated.CLIContext, opts backupOptions) error {
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
			Data map[string]interface{}
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
				CLIVersion:    "dev",
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

	return nil
}

// resourceItem is a minimal representation of a listed resource.
type resourceItem struct {
	ID   string
	Name string
}

// listResourceItems fetches the list of objects for a resource definition.
func listResourceItems(ctx context.Context, client generated.HTTPClient, def ResourceDef) ([]resourceItem, error) {
	if def.IsClassic {
		return listClassicItems(ctx, client, def)
	}
	return listModernItems(ctx, client, def)
}

func listClassicItems(ctx context.Context, client generated.HTTPClient, def ResourceDef) ([]resourceItem, error) {
	raw, err := FetchClassicList(ctx, client, def.ListPath, def.WrapperKey)
	if err != nil {
		return nil, err
	}

	var items []resourceItem
	for _, r := range raw {
		m, ok := r.(map[string]interface{})
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

func listModernItems(ctx context.Context, client generated.HTTPClient, def ResourceDef) ([]resourceItem, error) {
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
func extractID(m map[string]interface{}) string {
	return extractField(m, "id")
}

// extractName gets the "name" field from a map.
func extractName(m map[string]interface{}) string {
	if n, ok := m["name"].(string); ok {
		return n
	}
	return extractID(m) // fallback to id if no name
}

// unwrapClassicDetail unwraps Classic API single-object responses.
// Classic GET /id/{id} returns {"policy": {...}} — we want the inner object.
func unwrapClassicDetail(obj map[string]interface{}) map[string]interface{} {
	if len(obj) == 1 {
		for _, v := range obj {
			if inner, ok := v.(map[string]interface{}); ok {
				return inner
			}
		}
	}
	return obj
}

// writeBackupFile writes an object to disk in the specified format.
func writeBackupFile(path string, data interface{}, format string) error {
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
