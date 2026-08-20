// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v3"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/blueprints"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/compliancebenchmarks"
)

// Concurrency defaults for the backup command. The API can be pushed to 429s
// quickly on modest-sized instances when every resource type fans out to
// per-ID GETs in parallel, so the default is conservative. The ceiling exists
// as an escape hatch for larger instances that want to trade rate-limit risk
// for throughput on very large exports.
const (
	backupDefaultConcurrency = 3
	backupMaxConcurrency     = 10
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
	// SourceURL is the URL the CLI connected to. For direct auth this equals
	// the Jamf Pro instance URL; for platform gateway auth it is the gateway
	// (which is shared across tenants and not useful on its own).
	SourceURL string `json:"source_url" yaml:"source_url"`
	// JamfProURL is the actual Jamf Pro instance URL, fetched from
	// /v1/jamf-pro-server-url. Populated best-effort; empty if the lookup
	// failed (e.g. permissions, network).
	JamfProURL string `json:"jamf_pro_url,omitempty" yaml:"jamf_pro_url,omitempty"`
	// TenantID is the platform gateway tenant ID; non-empty only when the
	// backup ran against a platform-auth profile.
	TenantID string `json:"tenant_id,omitempty" yaml:"tenant_id,omitempty"`
}

func newBackupCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		outputDir        string
		format           string
		resources        string
		includeIDs       bool
		concurrency      int
		downloadPackages bool
	)

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Export all Jamf Pro configuration to a local directory",
		Long: `Export configuration objects from a Jamf Pro instance to a local directory.

Each object is saved as an individual YAML or JSON file. Server-generated fields
(id, timestamps) are stripped by default for clean version-control diffs.

Partial failures are tolerated — objects that fail to fetch are recorded in
_failures.yaml and the backup continues.

--download-packages additionally downloads the package binaries backing the
"packages" resource, saved to packages/files/. Only packages hosted on the
Jamf Cloud Distribution Service (JCDS) can be downloaded this way — packages
on other distribution points (on-prem file share, third-party cloud) have no
reliable download path and are skipped with a warning.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBackup(cmd.Context(), cliCtx, backupOptions{
				OutputDir:        outputDir,
				Format:           format,
				Resources:        resources,
				IncludeIDs:       includeIDs,
				Concurrency:      concurrency,
				DownloadPackages: downloadPackages,
			})
		},
	}

	cmd.Flags().StringVar(&outputDir, "output", "", "destination directory (required)")
	cmd.Flags().StringVar(&format, "format", "yaml", "output format: yaml or json")
	cmd.Flags().StringVar(&resources, "resources", "", "comma-separated resource filter (e.g., policies,scripts)")
	cmd.Flags().BoolVar(&includeIDs, "include-ids", false, "retain server-generated IDs in output")
	cmd.Flags().IntVar(&concurrency, "concurrency", backupDefaultConcurrency, fmt.Sprintf("max parallel API requests (ceiling %d)", backupMaxConcurrency))
	cmd.Flags().BoolVar(&downloadPackages, "download-packages", false, "also download package binaries hosted on JCDS to packages/files/")
	_ = cmd.MarkFlagRequired("output")

	return cmd
}

type backupOptions struct {
	OutputDir        string
	Format           string
	Resources        string
	IncludeIDs       bool
	Concurrency      int
	DownloadPackages bool
}

func runBackup(ctx context.Context, cliCtx *registry.CLIContext, opts backupOptions) error {
	opts.Concurrency = clampConcurrency(opts.Concurrency)
	client := cliCtx.Client

	// Best-effort lookup of the actual Jamf Pro instance URL. For platform
	// gateway auth the CLI's serverURL is the gateway host, which is useless
	// for audit — the real instance URL lives behind /v1/jamf-pro-server-url.
	// A failure here just leaves JamfProURL empty; it must not abort the run.
	jamfProURL := ""
	if data, err := fetchJSON(ctx, client, "/v1/jamf-pro-server-url"); err == nil {
		if u, ok := data["url"].(string); ok {
			jamfProURL = strings.TrimSuffix(u, "/")
		}
	} else if verboseLevel >= 1 {
		fmt.Fprintf(os.Stderr, "WARNING: could not resolve Jamf Pro URL for _meta: %v\n", err)
	}

	// Resolve tenant ID for _meta. The package-level tenantID var only holds
	// the CLI flag / env override; the authoritative value lives on the
	// resolved platform auth provider (loaded from config + keychain).
	resolvedTenant := tenantID
	if p, ok := cliCtx.AuthProvider.(*auth.PlatformOAuth2Provider); ok && resolvedTenant == "" {
		resolvedTenant = p.TenantID()
	}

	newMeta := func(resourceType string) backupMeta {
		return backupMeta{
			SchemaVersion: 1,
			CLIVersion:    cliVersion,
			ResourceType:  resourceType,
			ExportedAt:    time.Now().UTC().Format(time.RFC3339),
			SourceURL:     serverURL,
			JamfProURL:    jamfProURL,
			TenantID:      resolvedTenant,
		}
	}

	// Filter resources if specified
	var nameFilter []string
	if opts.Resources != "" {
		nameFilter = strings.Split(opts.Resources, ",")
		for i := range nameFilter {
			nameFilter[i] = strings.TrimSpace(nameFilter[i])
		}
	}
	defs, err := ResolveBackupResources(nameFilter)
	if err != nil {
		return err
	}
	if len(defs) == 0 {
		// Allow filters that resolve only to non-standard resources (inventory-preloads,
		// blueprints, compliance-benchmarks) — those are handled after this loop.
		allUnknown := true
		for _, n := range nameFilter {
			if isKnownBackupFilter(n) {
				allUnknown = false
				break
			}
		}
		if allUnknown {
			return fmt.Errorf("no resources match filter %q", opts.Resources)
		}
	}

	ext := ".yaml"
	if opts.Format == "json" {
		ext = ".json"
	}

	var failures []backupFailure
	totalExported := 0

	// packageFilenames collects the "filename" field of every exported package
	// object so --download-packages can attempt a JCDS download after the
	// metadata pass completes, without a redundant re-fetch of the list.
	var packageFilenames []string

	for _, def := range defs {
		// Create output subdirectory
		subDir := filepath.Join(opts.OutputDir, def.SubDir)
		if err := os.MkdirAll(subDir, 0o755); err != nil {
			return fmt.Errorf("creating directory %s: %w", subDir, err)
		}

		// List objects for this resource type
		items, raw, err := listResourceItemsAndMaps(ctx, client, &def)
		if err != nil {
			failures = append(failures, backupFailure{
				Resource: def.FilterName,
				Path:     def.ListPath,
				Error:    err.Error(),
			})
			fmt.Fprintf(os.Stderr, "WARNING: failed to list %s: %v\n", def.Key, err)
			continue
		}

		if len(items) == 0 {
			continue
		}

		// Collect (name, object) pairs — either by fetching detail per ID, or
		// by using the list response directly for ListOnly resources.
		slugSeen := make(map[string]bool)
		type itemResult struct {
			Name string
			Data map[string]any
		}

		var results []itemResult
		if def.ListOnly {
			for i, item := range items {
				results = append(results, itemResult{Name: item.Name, Data: raw[i]})
			}
		} else {
			fetched, errs := BoundedParallelFetch(ctx, items, opts.Concurrency, func(ctx context.Context, item resourceItem) (itemResult, error) {
				path := strings.Replace(def.GetPath, "{id}", item.ID, 1)
				data, err := fetchJSON(ctx, client, path)
				if err != nil {
					return itemResult{}, fmt.Errorf("%s id=%s: %w", def.Key, item.ID, err)
				}
				if def.ScopePath != "" {
					serials, serr := fetchPrestageScope(ctx, client, def.ScopePath, item.ID)
					if serr != nil {
						return itemResult{}, fmt.Errorf("%s id=%s scope: %w", def.Key, item.ID, serr)
					}
					data["scope"] = serials
				}
				return itemResult{Name: item.Name, Data: data}, nil
			})
			for _, e := range errs {
				failures = append(failures, backupFailure{
					Resource: def.FilterName,
					Path:     def.GetPath,
					Error:    e.Error(),
				})
			}
			results = fetched
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
			obj["_meta"] = newMeta(def.FilterName)

			if opts.DownloadPackages && def.FilterName == "packages" {
				if fn, ok := obj["filename"].(string); ok && fn != "" {
					packageFilenames = append(packageFilenames, fn)
				}
			}

			slug := SlugifyName(r.Name)
			slug = DeduplicateSlug(slug, slugSeen)

			outPath := filepath.Join(subDir, slug+ext)
			if err := writeBackupFile(outPath, obj, opts.Format); err != nil {
				failures = append(failures, backupFailure{
					Resource: def.FilterName,
					Path:     outPath,
					Error:    err.Error(),
				})
				continue
			}
			totalExported++
		}
	}

	// Package binaries — opt-in, JCDS-only (see backupPackageFiles).
	if opts.DownloadPackages {
		n, errs := backupPackageFiles(ctx, client, opts, packageFilenames)
		totalExported += n
		failures = append(failures, errs...)
	}

	// wantFilter reports whether a named resource should be included given the
	// user's --resources filter. Empty filter means include everything.
	wantFilter := func(name string) bool {
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

	// Inventory preload — single CSV download, not a JSON list+get resource.
	if wantFilter("inventory-preloads") {
		n, errs := backupInventoryPreloadCSV(ctx, client, opts)
		totalExported += n
		failures = append(failures, errs...)
	}

	// Platform resources (blueprints, compliance-benchmarks) via SDK
	if cliCtx.PlatformSDKClient != nil {
		if wantFilter("blueprints") {
			n, errs := backupBlueprints(ctx, cliCtx, opts, newMeta)
			totalExported += n
			failures = append(failures, errs...)
		}
		if wantFilter("compliance-benchmarks") {
			n, errs := backupBenchmarks(ctx, cliCtx, opts, newMeta)
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
		if allowPartialFailure && totalExported > 0 {
			fmt.Fprintf(os.Stderr, "warning: backup completed with %d failures; continuing (--allow-partial-failure)\n", len(failures))
			return nil
		}
		msg := fmt.Sprintf("backup completed with %d failures (see _failures%s)", len(failures), ext)
		return exitcode.PartialOrPropagate(totalExported, len(failures), nil, msg)
	}
	return nil
}

// resourceItem is a minimal representation of a listed resource.
type resourceItem struct {
	ID   string
	Name string
}

// clampConcurrency enforces the backup command's concurrency bounds. Values
// below 1 fall back to the default; values above the ceiling are capped so a
// typo like --concurrency 100 can't overload an instance.
func clampConcurrency(n int) int {
	switch {
	case n <= 0:
		return backupDefaultConcurrency
	case n > backupMaxConcurrency:
		return backupMaxConcurrency
	}
	return n
}

// listResourceItemsAndMaps fetches the list for a backup resource and returns
// both the (id, name) summary and the raw list items. The raw items are kept
// around so ListOnly resources can write them to disk directly without a
// redundant per-ID GET.
// isBackup404 returns true when err is an exitcode.Error with NotFound code,
// indicating the endpoint does not exist on this tenant (version mismatch).
func isBackup404(err error) bool {
	var e *exitcode.Error
	return errors.As(err, &e) && e.Code == exitcode.NotFound
}

func listResourceItemsAndMaps(ctx context.Context, client registry.HTTPClient, def *ResolvedBackupResource) ([]resourceItem, []map[string]any, error) {
	var raw []map[string]any
	var err error
	switch {
	case def.ListSubset != "":
		raw, err = FetchClassicListSubset(ctx, client, def.ListPath, def.ListSubset)
	case def.IsClassic:
		var anyItems []any
		anyItems, err = FetchClassicList(ctx, client, def.ListPath, def.WrapperKey)
		if err == nil {
			for _, it := range anyItems {
				if m, ok := it.(map[string]any); ok {
					raw = append(raw, m)
				}
			}
		}
	default:
		raw, err = FetchAllPaginated(ctx, client, def.ListPath, 100)
		if err != nil && isBackup404(err) && def.FallbackListPath != "" {
			raw, err = FetchAllPaginated(ctx, client, def.FallbackListPath, 100)
			if err == nil {
				fmt.Fprintf(os.Stderr, "warning: GET %s returned 404; falling back to %s (tenant may be on older Jamf Pro)\n", def.ListPath, def.FallbackListPath)
				def.ListPath = def.FallbackListPath
				if def.FallbackGetPath != "" {
					def.GetPath = def.FallbackGetPath
				}
			}
		}
	}
	if err != nil {
		return nil, nil, err
	}

	// Keep raw and items aligned by index so ListOnly can zip them back up.
	var items []resourceItem
	var aligned []map[string]any
	for _, m := range raw {
		id := extractIDWithField(m, def.IDField)
		if id == "" && !def.ListOnly {
			continue
		}
		items = append(items, resourceItem{ID: id, Name: extractName(m, def.NameField, def.IDField)})
		aligned = append(aligned, m)
	}
	return items, aligned, nil
}

// listResourceItems is the summary-only variant used by the diff command,
// which does not write list-only payloads.
func listResourceItems(ctx context.Context, client registry.HTTPClient, def ResolvedBackupResource) ([]resourceItem, error) {
	items, _, err := listResourceItemsAndMaps(ctx, client, &def)
	return items, err
}

// fetchPrestageScope fetches the device scope for a single prestage and returns
// its assigned serial numbers, sorted for stable diffs. Server-generated
// assignment metadata (assignmentDate, userAssigned) and the optimistic-lock
// versionLock are dropped — only the serial set is meaningful config. A
// prestage with no devices yields an empty (non-nil) slice so the "scope" key
// is always present and renders as `[]` rather than `null`.
func fetchPrestageScope(ctx context.Context, client registry.HTTPClient, scopePath, id string) ([]string, error) {
	path := strings.Replace(scopePath, "{id}", id, 1)
	data, err := fetchJSON(ctx, client, path)
	if err != nil {
		return nil, err
	}
	serials := []string{}
	if assignments, ok := data["assignments"].([]any); ok {
		for _, a := range assignments {
			m, ok := a.(map[string]any)
			if !ok {
				continue
			}
			if s, ok := m["serialNumber"].(string); ok && s != "" {
				serials = append(serials, s)
			}
		}
	}
	sort.Strings(serials)
	return serials, nil
}

// extractID gets the "id" field from a map, handling both string and float64.
func extractID(m map[string]any) string {
	return extractField(m, "id")
}

// extractIDWithField honours a spec-declared ID field override (e.g. "groupId"
// for mobile device groups), falling back to the default "id".
func extractIDWithField(m map[string]any, field string) string {
	if field != "" {
		if v := extractField(m, field); v != "" {
			return v
		}
	}
	return extractField(m, "id")
}

// extractName returns the human-readable name for a list item. `field` is the
// spec-declared name field (e.g. "displayName" for mobile device groups); an
// empty value falls back to "name". If neither yields a name, the list item's
// ID is returned so per-ID fetches and slug generation still have something to
// key off. Callers pass the resource's IDField so the final fallback honours
// overrides (e.g. "groupId" for mobile device groups, which have neither
// "name" nor "id" in their list response).
func extractName(m map[string]any, field, idField string) string {
	if field != "" {
		if n, ok := m[field].(string); ok && n != "" {
			return n
		}
	}
	if n, ok := m["name"].(string); ok && n != "" {
		return n
	}
	return extractIDWithField(m, idField)
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
		// Coerce multi-line source-code / payload fields to YAML literal-block
		// scalars before marshaling so scripts and configuration profiles land
		// as `|-` on disk instead of quoted strings with embedded \n escapes.
		if m, ok := data.(map[string]any); ok {
			data = forceBlockLiteralFields(m)
		}
		content, err = yaml.Marshal(data)
		if err != nil {
			return err
		}
	}

	return os.WriteFile(path, content, 0o644)
}

// blockLiteralFieldNames lists the field names whose string values are forced
// to YAML literal-block (`|-`) scalars. These hold multi-line source code or
// XML blobs where quoted single-line `\n` encoding is unreadable and defeats
// git diffs.
var blockLiteralFieldNames = map[string]struct{}{
	"scriptContents":  {}, // modern API /v1/scripts
	"script_contents": {}, // classic snake_case (script contents embedded in policies)
	"payloads":        {}, // classic configuration profile XML blob
}

// forceBlockLiteralFields walks a decoded object graph and, for every string
// value whose key matches blockLiteralFieldNames, substitutes a yaml.Node with
// LiteralStyle so yaml.Marshal emits it as `|-`. Returns the transformed graph
// — an any because yaml.Node replaces what was previously a plain string.
//
// Two normalizations are applied to force yaml.v3 into literal-block mode:
//   - trailing newlines stripped → `|-` (strip chomp) instead of `|` (clip)
//   - trailing whitespace on each line stripped → yaml.v3 refuses literal-block
//     for any scalar with line-trailing spaces and falls back to double-quoted
//     with `\n` escapes, which is unreadable for scripts and XML blobs
//
// The trailing-whitespace trim is cosmetic and does not change the backup's
// purpose (diffing / audit). Trailing spaces on lines are almost always noise.
func forceBlockLiteralFields(obj map[string]any) any {
	return transformStrings(obj, func(key, val string) any {
		if _, ok := blockLiteralFieldNames[key]; !ok {
			return val
		}
		if !strings.Contains(val, "\n") {
			return val // single-line values look fine as plain scalars
		}
		return &yaml.Node{
			Kind:  yaml.ScalarNode,
			Value: trimLineTrailingSpace(strings.TrimRight(val, "\n")),
			Style: yaml.LiteralStyle,
		}
	})
}

// trimLineTrailingSpace strips trailing spaces and tabs from each line so
// yaml.v3 accepts the string as literal-block. Newline boundaries are
// preserved exactly.
func trimLineTrailingSpace(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.Join(lines, "\n")
}

// transformStrings recursively walks the graph, applying fn to every string
// value under a keyed map entry. The current key is passed so the callback can
// decide per-field. Slices are walked with an empty key.
func transformStrings(v any, fn func(key, val string) any) any {
	switch vv := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(vv))
		for k, child := range vv {
			if s, ok := child.(string); ok {
				out[k] = fn(k, s)
				continue
			}
			out[k] = transformStrings(child, fn)
		}
		return out
	case []any:
		out := make([]any, len(vv))
		for i, child := range vv {
			if s, ok := child.(string); ok {
				out[i] = fn("", s) // no key at list positions — skip substitution
				continue
			}
			out[i] = transformStrings(child, fn)
		}
		return out
	default:
		return v
	}
}

// normalizeViaJSON round-trips a value through JSON so yaml.v3 sees native Go
// types (maps, slices, primitives) instead of `json.RawMessage` / `[]byte`,
// which would otherwise serialize as integer arrays. Blueprint components
// specifically use `JsonNode = json.RawMessage` for the per-component
// configuration — without this step the YAML output is gibberish.
func normalizeViaJSON(v any) (map[string]any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// backupInventoryPreloadCSV downloads all inventory preload records as a single
// CSV file from /v2/inventory-preload/csv. The Jamf Pro API returns the complete
// dataset in one request — there is no paginated JSON list+get for this resource.
func backupInventoryPreloadCSV(ctx context.Context, client registry.HTTPClient, opts backupOptions) (int, []backupFailure) {
	subDir := filepath.Join(opts.OutputDir, "inventory-preloads")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		return 0, []backupFailure{{Resource: "inventory-preloads", Path: subDir, Error: err.Error()}}
	}

	csvCtx := registry.WithAccept(ctx, "text/csv")
	resp, err := client.Do(csvCtx, "GET", "/v2/inventory-preload/csv", nil)
	if err != nil {
		return 0, []backupFailure{{Resource: "inventory-preloads", Path: "/v2/inventory-preload/csv", Error: err.Error()}}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, []backupFailure{{Resource: "inventory-preloads", Path: "/v2/inventory-preload/csv", Error: fmt.Sprintf("HTTP %d", resp.StatusCode)}}
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, []backupFailure{{Resource: "inventory-preloads", Path: "/v2/inventory-preload/csv", Error: err.Error()}}
	}

	outPath := filepath.Join(subDir, "inventory-preload-all.csv")
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return 0, []backupFailure{{Resource: "inventory-preloads", Path: outPath, Error: err.Error()}}
	}
	return 1, nil
}

// backupPackageFiles downloads the actual package binaries named in
// filenames, gated behind --download-packages. Only packages hosted on the
// Jamf Cloud Distribution Service (JCDS) can be reliably downloaded — on-prem
// file-share and third-party cloud distribution points require credentials
// or network access this CLI cannot assume, so a filename with no match in
// /v1/jcds/files is skipped with a warning rather than treated as a failure.
func backupPackageFiles(ctx context.Context, client registry.HTTPClient, opts backupOptions, filenames []string) (int, []backupFailure) {
	if len(filenames) == 0 {
		return 0, nil
	}

	jcdsFiles, err := jcdsListFiles(ctx, client)
	if err != nil {
		return 0, []backupFailure{{Resource: "packages", Path: "/v1/jcds/files", Error: err.Error()}}
	}
	jcdsSet := make(map[string]struct{}, len(jcdsFiles))
	for _, f := range jcdsFiles {
		jcdsSet[f.FileName] = struct{}{}
	}

	filesDir := filepath.Join(opts.OutputDir, "packages", "files")
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		return 0, []backupFailure{{Resource: "packages", Path: filesDir, Error: err.Error()}}
	}

	var (
		mu         sync.Mutex
		failures   []backupFailure
		downloaded int
	)
	sem := make(chan struct{}, clampConcurrency(opts.Concurrency))
	var g errgroup.Group

	for _, name := range filenames {
		if _, onJCDS := jcdsSet[name]; !onJCDS {
			fmt.Fprintf(os.Stderr, "warning: package %q is not hosted on JCDS; skipping download\n", name)
			continue
		}

		sem <- struct{}{}
		g.Go(func() error {
			defer func() { <-sem }()

			outPath := filepath.Join(filesDir, name)
			if err := jcdsDownloadFile(ctx, client, name, outPath); err != nil {
				mu.Lock()
				failures = append(failures, backupFailure{Resource: "packages", Path: name, Error: err.Error()})
				mu.Unlock()
				return nil
			}
			mu.Lock()
			downloaded++
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()

	return downloaded, failures
}

// backupBlueprints exports all blueprints via the Platform SDK.
func backupBlueprints(ctx context.Context, cliCtx *registry.CLIContext, opts backupOptions, newMeta func(string) backupMeta) (int, []backupFailure) {
	bp := blueprints.New(cliCtx.PlatformSDKClient)
	ext := ".yaml"
	if opts.Format == "json" {
		ext = ".json"
	}

	subDir := filepath.Join(opts.OutputDir, "blueprints")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		return 0, []backupFailure{{Resource: "blueprints", Path: subDir, Error: err.Error()}}
	}

	bps, err := bp.ListBlueprints(ctx, nil, "")
	if err != nil {
		return 0, []backupFailure{{Resource: "blueprints", Path: "list", Error: err.Error()}}
	}

	var failures []backupFailure
	exported := 0
	slugSeen := make(map[string]bool)

	for _, item := range bps {
		detail, err := bp.GetBlueprint(ctx, item.ID)
		if err != nil {
			failures = append(failures, backupFailure{Resource: "blueprints", Path: item.ID, Error: err.Error()})
			continue
		}

		obj, err := normalizeViaJSON(blueprintToExport(ctx, cliCtx.PlatformSDKClient, detail))
		if err != nil {
			failures = append(failures, backupFailure{Resource: "blueprints", Path: item.ID, Error: err.Error()})
			continue
		}
		obj["_meta"] = newMeta("blueprints")

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
func backupBenchmarks(ctx context.Context, cliCtx *registry.CLIContext, opts backupOptions, newMeta func(string) backupMeta) (int, []backupFailure) {
	cb := compliancebenchmarks.New(cliCtx.PlatformSDKClient)
	ext := ".yaml"
	if opts.Format == "json" {
		ext = ".json"
	}

	subDir := filepath.Join(opts.OutputDir, "compliance-benchmarks")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		return 0, []backupFailure{{Resource: "compliance-benchmarks", Path: subDir, Error: err.Error()}}
	}

	resp, err := cb.ListBenchmarks(ctx)
	if err != nil {
		return 0, []backupFailure{{Resource: "compliance-benchmarks", Path: "list", Error: err.Error()}}
	}

	var failures []backupFailure
	exported := 0
	slugSeen := make(map[string]bool)

	for _, b := range resp.Benchmarks {
		bm, err := cb.GetBenchmark(ctx, b.ID)
		if err != nil {
			failures = append(failures, backupFailure{Resource: "compliance-benchmarks", Path: b.ID, Error: err.Error()})
			continue
		}

		// Strip server-generated fields for clean export. JSON round-trip so
		// any SDK types that embed json.RawMessage / []byte (now or later)
		// serialize as native Go types instead of yaml.v3's integer-array
		// fallback — same reason backupBlueprints normalizes.
		obj, err := normalizeViaJSON(benchmarkToExport(bm))
		if err != nil {
			failures = append(failures, backupFailure{Resource: "compliance-benchmarks", Path: b.ID, Error: err.Error()})
			continue
		}
		obj["_meta"] = newMeta("compliance-benchmarks")

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
