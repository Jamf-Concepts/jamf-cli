// Copyright 2026, Jamf Software LLC

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/Jamf-Concepts/jamf-cli/generator/classic"
	"github.com/Jamf-Concepts/jamf-cli/generator/parser"
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

func main() {
	specsDir := "./specs"
	outputDir := "./internal/commands/pro/generated"

	fmt.Println("jamf-cli code generator")
	fmt.Println("==========================")
	fmt.Printf("Specs directory: %s\n", specsDir)
	fmt.Printf("Output directory: %s\n", outputDir)
	fmt.Println()

	// Create output directory
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output directory: %v\n", err)
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
	}

	// ── Smoke test registry ──────────────────────────────────────
	smokeRegistryPath, err := generateSmokeRegistry(outputDir, resources, classicResources)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating smoke registry: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\nGenerated: %s\n", smokeRegistryPath)
	generatedFiles[filepath.Base(smokeRegistryPath)] = true

	// ── Clean up stale generated files ────────────────────────────
	// Remove any .go files in the output directory that were not produced by this
	// run. This handles cases where a versioned resource (e.g. mobile_device_prestages_v_3s.go)
	// has been superseded by a renamed canonical file (mobile_device_prestages.go).
	existing, _ := filepath.Glob(filepath.Join(outputDir, "*.go"))
	for _, f := range existing {
		if !generatedFiles[filepath.Base(f)] {
			if err := os.Remove(f); err == nil {
				fmt.Printf("Removed stale: %s\n", filepath.Base(f))
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
