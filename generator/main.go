// Copyright 2026, Jamf Software LLC

package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/Jamf-Concepts/jamf-cli/generator/classic"
	"github.com/Jamf-Concepts/jamf-cli/generator/classicschema"
	"github.com/Jamf-Concepts/jamf-cli/generator/gateway"
	"github.com/Jamf-Concepts/jamf-cli/generator/monolith"
	"github.com/Jamf-Concepts/jamf-cli/generator/parser"
	"github.com/Jamf-Concepts/jamf-cli/generator/platform"
	secgen "github.com/Jamf-Concepts/jamf-cli/generator/security"
)

// smokeEntry collects endpoint metadata from both modern and classic generators.
type smokeEntry struct {
	Resource      string
	Operation     string
	Method        string
	Path          string
	IsList        bool
	HasPathParams bool
	IsClassic     bool
	WrapperKey    string
	SingularKey   string
}

// backupEntry describes a resource eligible for backup: it has both a canonical
// list endpoint and a per-ID get endpoint. Emitted to backup_registry.go so the
// backup and diff commands can derive paths from the specs rather than maintain
// a duplicated hand-written list of hard-coded URLs.
type backupEntry struct {
	Name             string // CLI command name, e.g. "classic-policies", "scripts"
	ListPath         string // e.g. "/JSSResource/policies" or "/v1/scripts"
	GetPath          string // with {id} placeholder, e.g. "/JSSResource/policies/id/{id}"
	FallbackListPath string // lower-version list path tried on 404, e.g. "/v1/foo"
	FallbackGetPath  string // lower-version get path tried on 404, e.g. "/v1/foo/{id}"
	IsClassic        bool
	WrapperKey       string // classic list wrapper element, e.g. "policies"
	SingularKey      string // classic detail wrapper element, e.g. "policy"
	ListSubset       string // set when the list endpoint is shared (e.g. "users" for /JSSResource/accounts)
	NameField        string // field on list items holding the human name (default "name")
	IDField          string // field on list items holding the resource ID (default "id")
}

func main() {
	var (
		specsDir      string
		outputDir     string
		monolithPath  string
		gatewaySource string
		gatewaySDKRev string
	)
	flag.StringVar(&specsDir, "specs", "./specs", "Directory containing per-resource OpenAPI spec files")
	flag.StringVar(&outputDir, "output", "./internal/commands/pro/generated", "Directory to write generated Go files into")
	flag.StringVar(&monolithPath, "monolith", "", "Optional consolidated OpenAPI document to split into per-resource spec files before generation. Accepts a local path or http(s):// URL")
	// One flag rather than two, because both artifacts derive from the same
	// drop directory and the same two SDK specs. A second flag that must always
	// carry the same value is a code path nothing exercises independently, which
	// is how the gateway URL-shape bug survived weeks.
	flag.StringVar(&gatewaySource, "gateway-source", "", "Optional directory holding the SDK's pro_api.json and classic_api_resource_documentation.json, to re-derive specs/gateway/coverage.json and specs/classic/schemas.json from before generation")
	flag.StringVar(&gatewaySDKRev, "gateway-sdk-rev", "", "Optional jamfplatform-go-sdk revision to record as the derived manifests' provenance")
	flag.Parse()

	fmt.Println("jamf-cli code generator")
	fmt.Println("==========================")
	fmt.Printf("Specs directory: %s\n", specsDir)
	fmt.Printf("Output directory: %s\n", outputDir)
	if monolithPath != "" {
		fmt.Printf("Monolith input:  %s\n", monolithPath)
	}
	fmt.Println()

	// Create output directory
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output directory: %v\n", err)
		os.Exit(1)
	}

	// If a monolith was provided, split it into per-resource spec files first.
	// The splitter overwrites *.yaml in the root of specsDir; the classic/
	// subdirectory is left untouched.
	if monolithPath != "" {
		fmt.Println("Splitting monolith spec")
		fmt.Println("-----------------------")
		written, warnings, err := monolith.Split(monolithPath, specsDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error splitting monolith: %v\n", err)
			os.Exit(1)
		}
		for _, w := range warnings {
			fmt.Fprintln(os.Stderr, "  note:", w)
		}
		fmt.Printf("  Wrote %d spec files (incl. %s)\n\n", len(written), monolith.LibraryFilename)
	}

	// Re-derive the gateway coverage manifest from a bundle drop, when one was
	// pointed at. Done before generation so the same invocation that refreshes
	// the manifest also stamps the verdicts it implies.
	coveragePath := filepath.Join(specsDir, gateway.CoverageFile)
	if gatewaySource != "" {
		fmt.Println("Deriving gateway coverage")
		fmt.Println("-------------------------")
		cov, err := gateway.Extract(gatewaySource, gatewaySDKRev)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error deriving gateway coverage: %v\n", err)
			os.Exit(1)
		}
		// Keep provenance this run could not determine for itself — see
		// gateway.CarryForwardProvenance.
		if prev, err := gateway.Load(coveragePath); err == nil {
			gateway.CarryForwardProvenance(cov, prev)
		}
		if err := gateway.Write(cov, coveragePath); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing gateway coverage: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("  %s: %s %s (%d paths, %d ops), %s %s (%d paths, %d ops), SDK %s\n\n",
			coveragePath,
			cov.Sources.Pro.Title, cov.Sources.Pro.Version, cov.Sources.Pro.Paths, cov.Sources.Pro.Operations,
			cov.Sources.Classic.Title, cov.Sources.Classic.Version, cov.Sources.Classic.Paths, cov.Sources.Classic.Operations,
			orUnknown(cov.Sources.SDKCommit))
	}

	// Derive the App Installer specs from the same drop. They are the one Jamf
	// Pro surface no monolith carries — see monolith.ExtractSubtree — so the
	// only place they can come from is the gateway's published Pro API spec,
	// which is the file gateway coverage was just derived from. Same source,
	// same run, same reason the classic schemas are derived here: an artefact
	// that has to be refreshed alongside another one is not a second flag.
	//
	// Before the spec glob below, so the files this writes are the ones parsed.
	if gatewaySource != "" {
		fmt.Println("Deriving App Installer specs")
		fmt.Println("----------------------------")
		written, warnings, err := monolith.ExtractSubtree(
			filepath.Join(gatewaySource, gateway.ProSpecFile),
			specsDir, monolith.AppInstallerSubtree, monolith.AppInstallerSpecs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error deriving App Installer specs: %v\n", err)
			os.Exit(1)
		}
		for _, w := range warnings {
			fmt.Fprintln(os.Stderr, "  note:", w)
		}
		fmt.Printf("  Wrote %d spec file(s)\n\n", len(written))
	}

	// Load it for the verdict passes below. A missing manifest is not an error:
	// it is the "unknown" answer, and the passes then stamp nothing so no
	// command is refused. `make generate` has to work in a tree where nobody
	// has fetched a bundle.
	coverage, err := gateway.Load(coveragePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading gateway coverage: %v\n", err)
		os.Exit(1)
	}

	// Find all YAML specs
	specs, err := filepath.Glob(filepath.Join(specsDir, "*.yaml"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error finding specs: %v\n", err)
		os.Exit(1)
	}

	if len(specs) == 0 {
		fmt.Println("No OpenAPI specs found in", specsDir)
		fmt.Println("Run 'make sync-specs' to fetch specs from jamf-pro-server")
		os.Exit(0)
	}

	fmt.Printf("Found %d spec(s)\n\n", len(specs))

	// Parse and generate for each spec
	var resources []*parser.Resource
	for _, specPath := range specs {
		fmt.Printf("Parsing: %s\n", filepath.Base(specPath))
		parsed, err := parser.ParseSpec(specPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Error: %v\n", err)
			continue
		}
		for _, resource := range parsed {
			resources = append(resources, resource)
			fmt.Printf("  Resource: %s (%d operations)\n", resource.Name, len(resource.Operations))
		}
	}

	fmt.Println()

	// Filter out resources with no operations
	var validResources []*parser.Resource
	for _, r := range resources {
		if len(r.Operations) > 0 {
			validResources = append(validResources, r)
		}
	}
	resources = validResources

	// Consolidate multi-version resources: keep only the highest version per resource
	// family, renamed to its clean canonical name (e.g. "mobile-device-prestages-v-3s"
	// becomes "mobile-device-prestages"). This also suppresses any older non-versioned
	// base resource that a versioned sibling supersedes.
	resources = parser.DeduplicateVersioned(resources)

	// Fix names where auto-pluralization produces unnatural results
	// (e.g. computers-inventories → computers-inventory).
	parser.ApplyNameOverrides(resources)

	// Fix NameField values where detectNameField() returned the wrong field
	// (e.g. general.name for computers-inventory, groupName for groups).
	parser.ApplyNameFieldOverrides(resources)

	// Apply static lookup fields (e.g. serial number for computers) now that
	// resource names are in their final canonical form.
	parser.ApplyLookupFields(resources)

	// Attach file-sourced request-body fields (e.g. --script-file → scriptContents).
	parser.ApplyFileFields(resources)

	// Rename sub-path action POSTs to "create" for resources that lack a bare
	// collection POST (e.g. device-enrollment-instances creates via /upload-token).
	parser.ApplyCreateOpOverrides(resources)

	// Detach auxiliary PUT /{id}/<action> endpoints so update/apply can compose
	// them alongside the main update body instead of emitting a parallel subcommand.
	parser.ApplyUpdateTokenOpOverrides(resources)

	// Swap list operation paths to richer detail endpoints where available.
	parser.ApplyListDetailPaths(resources)

	// Apply preferred table columns and default sections for list commands.
	parser.ApplyTableColumns(resources)

	// Swap "get" operation paths to richer detail endpoints where available.
	parser.ApplyGetDetailPaths(resources)

	// Stamp each operation with whether the Jamf Platform gateway serves it, now
	// that every path-rewriting pass above has settled — ApplyListDetailPaths
	// and ApplyGetDetailPaths both move an op onto a different endpoint, and the
	// verdict has to be about the path the command will actually send.
	gatewayEntries := gateway.Apply(coverage, modernGatewayOps(resources))

	// Track every file we write so we can delete stale files from previous generator runs.
	generatedFiles := make(map[string]bool)

	// Generate code
	gen := parser.NewGenerator(outputDir)
	for _, resource := range resources {
		outPath, err := gen.Generate(resource)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error generating %s: %v\n", resource.Name, err)
			continue
		}
		fmt.Printf("Generated: %s\n", outPath)
		generatedFiles[filepath.Base(outPath)] = true
	}

	// Generate registry file
	registryPath, err := gen.GenerateRegistry(resources)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating registry: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Generated: %s\n", registryPath)
	generatedFiles[filepath.Base(registryPath)] = true

	fmt.Println()
	fmt.Printf("Successfully generated %d resource command(s)\n", len(resources))

	// Track which spec files contributed to the Pro generated package; we
	// emit provenance.go after Classic also runs (Classic adds its manifest).
	proSpecSources := append([]string(nil), specs...)

	// ── Classic API generation ─────────────────────────────────────
	var classicResources []classic.ClassicResource
	classicManifest := filepath.Join(specsDir, "classic", "resources.yaml")
	if _, err := os.Stat(classicManifest); err == nil {
		fmt.Println()
		fmt.Println("Classic API generation")
		fmt.Println("======================")
		fmt.Printf("Manifest: %s\n\n", classicManifest)

		classicResources, err = classic.ParseManifest(classicManifest)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing classic manifest: %v\n", err)
			os.Exit(1)
		}

		gatewayEntries = append(gatewayEntries, gateway.Apply(coverage, classicGatewayOps(classicResources))...)

		// Re-derive the Classic schema artifact when an SDK drop was pointed at,
		// then load it. Derivation needs the manifest, which is why this sits
		// here rather than beside the gateway derivation above. A missing
		// artifact is the "unknown" answer: Classic commands then ship with no
		// body help, which is what they shipped before this existed.
		classicSchemaPath := filepath.Join(specsDir, classicschema.ArtifactFile)
		if gatewaySource != "" {
			art, warnings, err := classicschema.Extract(gatewaySource, gatewaySDKRev, classicManifestEntries(classicResources))
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error deriving classic schemas: %v\n", err)
				os.Exit(1)
			}
			if prev, err := classicschema.Load(classicSchemaPath); err == nil {
				classicschema.CarryForwardProvenance(art, prev)
			}
			if err := classicschema.Write(art, classicSchemaPath); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing classic schemas: %v\n", err)
				os.Exit(1)
			}
			for _, w := range warnings {
				fmt.Fprintln(os.Stderr, "  note:", w)
			}
			fmt.Printf("Derived: %s (%d schemas, %d of %d resources bound)\n",
				classicSchemaPath, art.Source.Schemas, art.Source.Resources, len(classicResources))
		}

		classicSchemas, err := classicschema.Load(classicSchemaPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading classic schemas: %v\n", err)
			os.Exit(1)
		}
		if err := classic.AttachSchemas(classicResources, classicSchemas); err != nil {
			fmt.Fprintf(os.Stderr, "Error attaching classic schemas: %v\n", err)
			os.Exit(1)
		}

		classicGen := classic.NewGenerator(outputDir)
		for _, r := range classicResources {
			outPath, err := classicGen.Generate(r)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error generating classic %s: %v\n", r.CLIName, err)
				continue
			}
			fmt.Printf("Generated: %s\n", outPath)
			generatedFiles[filepath.Base(outPath)] = true
		}

		classicRegistryPath, err := classicGen.GenerateRegistry(classicResources)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error generating classic registry: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Generated: %s\n", classicRegistryPath)
		generatedFiles[filepath.Base(classicRegistryPath)] = true

		fmt.Println()
		fmt.Printf("Successfully generated %d classic resource command(s)\n", len(classicResources))

		proSpecSources = append(proSpecSources, classicManifest)
	}

	// Emit Pro provenance now that both modern and classic generation have
	// finished and we know every spec that contributed.
	if err := writeProvenanceFile(outputDir, "generated", proSpecSources); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing Pro provenance: %v\n", err)
		os.Exit(1)
	}
	generatedFiles["provenance.go"] = true
	fmt.Printf("Generated: %s\n", filepath.Join(outputDir, "provenance.go"))

	// ── Gateway coverage table ───────────────────────────────────
	// Emitted after both Pro passes so it holds the modern and Classic verdicts
	// together. Written unconditionally, including when there is no manifest:
	// an empty table is the honest "unknown" state, and leaving a previous
	// run's table behind would keep refusing commands on evidence that is no
	// longer in the tree.
	const gatewayTablePath = "./internal/gateway/coverage_gen.go"
	if err := gateway.Emit(coverage, gatewayEntries, gatewayTablePath); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing gateway coverage table: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Generated: %s (%s)\n", gatewayTablePath, gateway.Summary(gatewayEntries))

	// ── Platform Gateway commands ────────────────────────────────
	platformSpecsDir := filepath.Join(specsDir, "platform")
	if _, err := os.Stat(platformSpecsDir); err == nil {
		fmt.Println()
		fmt.Println("Platform Gateway command generation")
		fmt.Println("===================================")
		fmt.Printf("Specs directory: %s\n\n", platformSpecsDir)

		platformOutputDir := "./internal/commands/platform/generated"
		if err := os.MkdirAll(platformOutputDir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating platform output directory: %v\n", err)
			os.Exit(1)
		}

		platformResources, platformSpecs, err := platform.LoadResources(platformSpecsDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading platform specs: %v\n", err)
			os.Exit(1)
		}

		platformFiles, err := platform.Generate(platformResources, platformOutputDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error generating platform commands: %v\n", err)
			os.Exit(1)
		}
		platformGenerated := make(map[string]bool, len(platformFiles))
		for _, f := range platformFiles {
			fmt.Printf("Generated: %s\n", f)
			platformGenerated[filepath.Base(f)] = true
		}
		fmt.Println()
		fmt.Printf("Successfully generated %d platform resource file(s)\n", len(platformFiles))

		// Emit platform provenance using the spec files LoadResources
		// actually consumed (no re-glob — single source of truth).
		if err := writeProvenanceFile(platformOutputDir, "generated", platformSpecs); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing Platform provenance: %v\n", err)
			os.Exit(1)
		}
		platformGenerated["provenance.go"] = true
		fmt.Printf("Generated: %s\n", filepath.Join(platformOutputDir, "provenance.go"))

		// Remove stale files in the platform output dir that this run did not
		// produce — keeps the directory in sync when resources are dropped
		// from specs.
		existingPlatform, _ := filepath.Glob(filepath.Join(platformOutputDir, "*.go"))
		for _, f := range existingPlatform {
			base := filepath.Base(f)
			if strings.HasSuffix(base, "_test.go") {
				continue
			}
			if !platformGenerated[base] {
				if err := os.Remove(f); err == nil {
					fmt.Printf("Removed stale: %s\n", base)
				}
			}
		}
	}

	// ── Jamf Security Cloud commands ──────────────────────────────
	securitySpecsDir := filepath.Join(specsDir, "security")
	if _, err := os.Stat(securitySpecsDir); err == nil {
		fmt.Println()
		fmt.Println("Jamf Security Cloud command generation")
		fmt.Println("=======================================")
		fmt.Printf("Specs directory: %s\n\n", securitySpecsDir)

		securityOutputDir := "./internal/commands/security/generated"
		if err := os.MkdirAll(securityOutputDir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating security output directory: %v\n", err)
			os.Exit(1)
		}

		securityResources, securityScopes, securitySpecs, err := secgen.LoadResources(securitySpecsDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading security specs: %v\n", err)
			os.Exit(1)
		}

		securityFiles, err := secgen.Generate(securityResources, securityScopes, securityOutputDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error generating security commands: %v\n", err)
			os.Exit(1)
		}
		securityGenerated := make(map[string]bool, len(securityFiles))
		for _, f := range securityFiles {
			fmt.Printf("Generated: %s\n", f)
			securityGenerated[filepath.Base(f)] = true
		}
		fmt.Println()
		fmt.Printf("Successfully generated %d security resource file(s)\n", len(securityFiles))

		// Emit security provenance using the spec files LoadResources
		// actually consumed (no re-glob — single source of truth).
		if err := writeProvenanceFile(securityOutputDir, "generated", securitySpecs); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing Security provenance: %v\n", err)
			os.Exit(1)
		}
		securityGenerated["provenance.go"] = true
		fmt.Printf("Generated: %s\n", filepath.Join(securityOutputDir, "provenance.go"))

		// Remove stale files in the security output dir that this run did
		// not produce — keeps the directory in sync when resources are
		// dropped from specs.
		existingSecurity, _ := filepath.Glob(filepath.Join(securityOutputDir, "*.go"))
		for _, f := range existingSecurity {
			base := filepath.Base(f)
			if strings.HasSuffix(base, "_test.go") {
				continue
			}
			if !securityGenerated[base] {
				if err := os.Remove(f); err == nil {
					fmt.Printf("Removed stale: %s\n", base)
				}
			}
		}
	}

	// ── Smoke test registry ──────────────────────────────────────
	smokeRegistryPath, err := generateSmokeRegistry(outputDir, resources, classicResources)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating smoke registry: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\nGenerated: %s\n", smokeRegistryPath)
	generatedFiles[filepath.Base(smokeRegistryPath)] = true

	// ── Backup registry ──────────────────────────────────────────
	backupRegistryPath, err := generateBackupRegistry(outputDir, resources, classicResources)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating backup registry: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Generated: %s\n", backupRegistryPath)
	generatedFiles[filepath.Base(backupRegistryPath)] = true

	// ── Clean up stale generated files ────────────────────────────
	// Remove any .go files in the output directory that were not produced by this
	// run. This handles cases where a versioned resource (e.g. mobile_device_prestages_v_3s.go)
	// has been superseded by a renamed canonical file (mobile_device_prestages.go).
	existing, _ := filepath.Glob(filepath.Join(outputDir, "*.go"))
	for _, f := range existing {
		base := filepath.Base(f)
		if strings.HasSuffix(base, "_test.go") {
			continue // never delete hand-written test files
		}
		if !generatedFiles[base] {
			if err := os.Remove(f); err == nil {
				fmt.Printf("Removed stale: %s\n", base)
			}
		}
	}
}

// generateSmokeRegistry collects all GET endpoints from both modern and classic
// resources and writes smoke_registry.go for use by smoke tests.
func generateSmokeRegistry(outputDir string, modern []*parser.Resource, classicRes []classic.ClassicResource) (string, error) {
	var entries []smokeEntry

	// Collect modern GET endpoints
	for _, r := range modern {
		for _, op := range r.Operations {
			if op.Method != "GET" {
				continue
			}
			entries = append(entries, smokeEntry{
				Resource:      r.Name,
				Operation:     op.Name,
				Method:        op.Method,
				Path:          op.Path,
				IsList:        op.IsList,
				HasPathParams: strings.Contains(op.Path, "{"),
			})
		}
	}

	// Collect classic GET endpoints
	for _, r := range classicRes {
		if r.HasOperation("list") {
			entries = append(entries, smokeEntry{
				Resource:   r.CLIName,
				Operation:  "list",
				Method:     "GET",
				Path:       "/JSSResource/" + r.Path,
				IsList:     true,
				IsClassic:  true,
				WrapperKey: r.Name,
			})
		}
		if r.HasOperation("get") {
			entries = append(entries, smokeEntry{
				Resource:      r.CLIName,
				Operation:     "get",
				Method:        "GET",
				Path:          "/JSSResource/" + r.Path + "/" + r.IDPath + "/{id}",
				HasPathParams: true,
				IsClassic:     true,
				SingularKey:   r.Singular,
			})
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Resource != entries[j].Resource {
			return entries[i].Resource < entries[j].Resource
		}
		return entries[i].Operation < entries[j].Operation
	})

	tmpl, err := template.New("smoke_registry").Parse(smokeRegistryTemplate)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	outPath := filepath.Join(outputDir, "smoke_registry.go")
	f, err := os.Create(outPath)
	if err != nil {
		return "", fmt.Errorf("creating file: %w", err)
	}

	if err := tmpl.Execute(f, entries); err != nil {
		_ = f.Close()
		_ = os.Remove(outPath)
		return "", fmt.Errorf("executing template: %w", err)
	}

	_ = f.Close()
	return outPath, nil
}

const smokeRegistryTemplate = `// Copyright 2026, Jamf Software LLC
// Code generated by jamf-cli generator. DO NOT EDIT.
package generated

// SmokeEndpoint describes a single GET endpoint for smoke testing.
type SmokeEndpoint struct {
	Resource      string
	Operation     string
	Method        string
	Path          string
	IsList        bool
	HasPathParams bool
	IsClassic     bool
	WrapperKey    string
	SingularKey   string
}

// AllSmokeEndpoints returns every GET endpoint known to the CLI.
func AllSmokeEndpoints() []SmokeEndpoint {
	return []SmokeEndpoint{
{{- range . }}
		{Resource: {{ printf "%q" .Resource }}, Operation: {{ printf "%q" .Operation }}, Method: {{ printf "%q" .Method }}, Path: {{ printf "%q" .Path }}, IsList: {{ .IsList }}, HasPathParams: {{ .HasPathParams }}, IsClassic: {{ .IsClassic }}, WrapperKey: {{ printf "%q" .WrapperKey }}, SingularKey: {{ printf "%q" .SingularKey }}},
{{- end }}
	}
}
`

// generateBackupRegistry builds the canonical list+get pairs for every resource
// that is backup-eligible (i.e. has both a list endpoint without path params and
// a detail endpoint keyed by {id}). Singletons and sub-list endpoints (history,
// status, action routes) are filtered out. The generated map is consumed by the
// backup and diff commands via a curated allowlist in pro_resources.go.
func generateBackupRegistry(outputDir string, modern []*parser.Resource, classicRes []classic.ClassicResource) (string, error) {
	var entries []backupEntry

	// Modern resources: find the canonical list + get pair. Resources with a
	// list endpoint but no per-ID detail endpoint (e.g. sites) are included
	// with an empty GetPath — the backup runtime treats those as list-only.
	for _, r := range modern {
		if r.IsSingleton {
			continue
		}
		var listPath, getPath, fallbackListPath, fallbackGetPath string
		for _, op := range r.Operations {
			if op.Method != "GET" {
				continue
			}
			switch op.Name {
			case "list":
				if !strings.Contains(op.Path, "{") {
					listPath = op.Path
					// Only the first (highest) fallback is stored; the backup runtime
					// tries a single level of fallback, unlike generated commands which
					// walk the full FallbackPaths chain.
					if len(op.FallbackPaths) > 0 {
						fallbackListPath = op.FallbackPaths[0]
					}
				}
			case "get":
				if strings.Contains(op.Path, "{id}") {
					getPath = op.Path
					// Same single-level limitation as list above.
					if len(op.FallbackPaths) > 0 {
						fallbackGetPath = op.FallbackPaths[0]
					}
				}
			}
		}
		if listPath == "" {
			continue
		}
		nameField := r.NameField
		if nameField == "name" {
			nameField = "" // default; omit to keep the registry compact
		}
		idField := r.IDField
		if idField == "id" {
			idField = ""
		}
		entries = append(entries, backupEntry{
			Name:             r.Name,
			ListPath:         listPath,
			GetPath:          getPath,
			FallbackListPath: fallbackListPath,
			FallbackGetPath:  fallbackGetPath,
			NameField:        nameField,
			IDField:          idField,
		})
	}

	// Classic resources: both list and get must be declared. Subset resources
	// (account-users, account-groups) share the parent list endpoint but are
	// still backup-eligible — the runtime extracts the named subset.
	for _, r := range classicRes {
		if !r.HasOperation("list") || !r.HasOperation("get") {
			continue
		}
		idPath := r.IDPath
		if idPath == "" {
			idPath = "id"
		}
		entries = append(entries, backupEntry{
			Name:        r.CLIName,
			ListPath:    "/JSSResource/" + r.Path,
			GetPath:     "/JSSResource/" + r.Path + "/" + idPath + "/{id}",
			IsClassic:   true,
			WrapperKey:  r.Name,
			SingularKey: r.Singular,
			ListSubset:  r.ListSubset,
		})
	}

	// Sort alphabetically by CLI command name so the generated file diff is
	// stable across regenerations — unrelated spec changes don't shuffle the
	// map output and noise up review diffs.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	tmpl, err := template.New("backup_registry").Parse(backupRegistryTemplate)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	outPath := filepath.Join(outputDir, "backup_registry.go")
	f, err := os.Create(outPath)
	if err != nil {
		return "", fmt.Errorf("creating file: %w", err)
	}

	if err := tmpl.Execute(f, entries); err != nil {
		_ = f.Close()
		_ = os.Remove(outPath)
		return "", fmt.Errorf("executing template: %w", err)
	}

	_ = f.Close()
	return outPath, nil
}

const backupRegistryTemplate = `// Copyright 2026, Jamf Software LLC
// Code generated by jamf-cli generator. DO NOT EDIT.
package generated

// BackupEndpoint describes the list + get endpoint pair for a single
// backup-eligible resource. The backup command iterates a curated subset of
// these (see internal/commands/pro_resources.go) and exports each object as an
// individual file on disk.
type BackupEndpoint struct {
	// ListPath is the URL that returns the collection of objects.
	ListPath string
	// GetPath is the URL template for fetching a single object; the "{id}"
	// placeholder is substituted at request time.
	GetPath string
	// FallbackListPath is a lower-version list URL tried when ListPath returns
	// 404 (tenant on older Jamf Pro). Empty when no fallback is available.
	FallbackListPath string
	// FallbackGetPath is the lower-version get URL to use when FallbackListPath
	// was the effective list path. Empty when no fallback is available.
	FallbackGetPath string
	// IsClassic routes list parsing through the XML pipeline instead of the
	// paginated JSON pipeline.
	IsClassic bool
	// WrapperKey is the classic API list-response wrapper element (e.g.
	// "policies" for /JSSResource/policies). Empty for modern resources.
	WrapperKey string
	// SingularKey is the classic API detail-response wrapper element (e.g.
	// "policy" for a single policy). Empty for modern resources.
	SingularKey string
	// ListSubset is non-empty when the list endpoint is shared with a sibling
	// resource; the runtime slices only the named sub-element (e.g. "users" or
	// "groups" for /JSSResource/accounts). Both list and detail still share the
	// parent endpoint; detail lookups use the standard ID-keyed path.
	ListSubset string
	// NameField is the list-item field that holds the human-readable name.
	// Empty means the default "name".
	NameField string
	// IDField is the list-item field that holds the resource ID. Empty means
	// the default "id". Some modern resources (e.g. mobile device groups) use
	// a prefixed field like "groupId".
	IDField string
}

// BackupEndpoints maps a CLI command name (e.g. "classic-policies", "scripts")
// to its list+get endpoint pair. Populated at generation time from the OpenAPI
// specs and Classic API manifest.
var BackupEndpoints = map[string]BackupEndpoint{
{{- range . }}
	{{ printf "%q" .Name }}: {ListPath: {{ printf "%q" .ListPath }}, GetPath: {{ printf "%q" .GetPath }}, FallbackListPath: {{ printf "%q" .FallbackListPath }}, FallbackGetPath: {{ printf "%q" .FallbackGetPath }}, IsClassic: {{ .IsClassic }}, WrapperKey: {{ printf "%q" .WrapperKey }}, SingularKey: {{ printf "%q" .SingularKey }}, ListSubset: {{ printf "%q" .ListSubset }}, NameField: {{ printf "%q" .NameField }}, IDField: {{ printf "%q" .IDField }}},
{{- end }}
}
`

// modernGatewayOps adapts the parsed modern resources into the shape
// generator/gateway judges. The gateway path is the caller-facing one with the
// /pro namespace prefixed, which is exactly what client.rewritePathForGateway
// produces at runtime.
func modernGatewayOps(resources []*parser.Resource) []gateway.Op {
	var ops []gateway.Op
	for _, r := range resources {
		for _, op := range r.Operations {
			op := op
			ops = append(ops, gateway.Op{
				Method:      op.Method,
				GatewayPath: gateway.ProPrefix + op.Path,
				Set: func(v gateway.Verdict) {
					op.GatewayLevel, op.GatewayBasis, op.GatewayDetail = string(v.Level), string(v.Basis), v.Detail
					op.GatewayPrivileges = v.Scopes
				},
			})
		}
	}
	return ops
}

// classicGatewayMethods are the HTTP methods a Classic subcommand can send.
// Judged per method across the resource's subtree — see gateway.ScopeSubtreeMethod.
var classicGatewayMethods = []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete}

// classicGatewayOps adapts the Classic manifest, at three granularities,
// because a Classic resource's paths are assembled at runtime from the resource
// path plus whichever lookup is in play and there is no fixed set of op paths to
// enumerate here.
//
//   - One subtree op per resource: does the gateway carry the resource at all.
//   - One subtree-method op per resource per method: a method the gateway
//     declares nowhere beneath the resource cannot work under any lookup, so
//     every subcommand sending it is refused. The method a subcommand sends is
//     fixed at generate time even though its path is not.
//   - One exact op for the collection GET, the single Classic path that IS fixed.
//
// The last two exist because "the resource is carried" stopped implying "every
// verb on it is carried". Classic API 11.28.0 withdrew every read on
// patchsoftwaretitles while keeping POST /patchsoftwaretitles/id/{}, and
// withdrew GET /patchpolicies while keeping GET /patchpolicies/id/{} — so the
// whole-resource verdict reported `list`, `get`, `update` and `delete` on the
// first and `list` on the second as served, and each would have gone out to a
// bare 403 the refusal exists to pre-empt.
func classicGatewayOps(resources []classic.ClassicResource) []gateway.Op {
	ops := make([]gateway.Op, 0, len(resources)*(len(classicGatewayMethods)+2))
	for i := range resources {
		r := &resources[i]
		path := gateway.ClassicPrefix + "/" + r.Path
		ops = append(ops, gateway.Op{
			Method:      http.MethodGet,
			GatewayPath: path,
			Scope:       gateway.ScopeSubtree,
			Set: func(v gateway.Verdict) {
				r.GatewayLevel, r.GatewayBasis, r.GatewayDetail = string(v.Level), string(v.Basis), v.Detail
				r.GatewayPrivileges = v.ScopesByMethod
			},
		})
		for _, m := range classicGatewayMethods {
			m := m
			ops = append(ops, gateway.Op{
				Method:      m,
				GatewayPath: path,
				Scope:       gateway.ScopeSubtreeMethod,
				Set: func(v gateway.Verdict) {
					if r.GatewayMethods == nil {
						r.GatewayMethods = map[string]classic.GatewayVerdict{}
					}
					r.GatewayMethods[m] = classic.GatewayVerdict{
						Level: string(v.Level), Basis: string(v.Basis), Detail: v.Detail,
					}
				},
			})
		}
		ops = append(ops, gateway.Op{
			Method:      http.MethodGet,
			GatewayPath: path,
			Scope:       gateway.ScopeExact,
			Set: func(v gateway.Verdict) {
				r.GatewayList = classic.GatewayVerdict{
					Level: string(v.Level), Basis: string(v.Basis), Detail: v.Detail,
				}
			},
		})
	}
	return ops
}

// orUnknown renders an empty provenance string as something a reader can act on.
func orUnknown(s string) string {
	if s == "" {
		return "(revision not recorded)"
	}
	return s
}

// classicManifestEntries adapts the parsed Classic manifest to the subset
// generator/classicschema needs, so that package stays free of the manifest
// format and generator/classic stays its only reader.
func classicManifestEntries(resources []classic.ClassicResource) []classicschema.ManifestEntry {
	entries := make([]classicschema.ManifestEntry, 0, len(resources))
	for _, r := range resources {
		// Only write-capable resources are bound. The artifact describes request
		// bodies, and a resource with no create or update has none — asking for
		// one binds the wrong schema rather than nothing, because a read-only
		// Classic resource's "detail" endpoint can answer with a collection:
		// /accounts and /patchavailabletitles/sourceid/{id} resolve to the plural
		// `accounts` and `patch_available_titles` schemas, which are list
		// wrappers and not the shape of anything a caller would send.
		if !r.HasOperation("create") && !r.HasOperation("update") {
			continue
		}
		entries = append(entries, classicschema.ManifestEntry{
			Name:     r.Name,
			Path:     r.Path,
			Singular: r.Singular,
			IDPath:   r.IDPath,
		})
	}
	return entries
}
