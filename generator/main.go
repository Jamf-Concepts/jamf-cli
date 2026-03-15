package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jamf/jamfpro-cli/generator/classic"
	"github.com/jamf/jamfpro-cli/generator/parser"
)

func main() {
	specsDir := "./specs"
	outputDir := "./internal/commands/generated"

	fmt.Println("jamfpro-cli code generator")
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
		resource, err := parser.ParseSpec(specPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Error: %v\n", err)
			continue
		}
		if resource != nil {
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

	// Generate code
	gen := parser.NewGenerator(outputDir)
	for _, resource := range resources {
		outPath, err := gen.Generate(resource)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error generating %s: %v\n", resource.Name, err)
			continue
		}
		fmt.Printf("Generated: %s\n", outPath)
	}

	// Generate registry file
	registryPath, err := gen.GenerateRegistry(resources)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating registry: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Generated: %s\n", registryPath)

	fmt.Println()
	fmt.Printf("Successfully generated %d resource command(s)\n", len(resources))

	// ── Classic API generation ─────────────────────────────────────
	classicManifest := filepath.Join(specsDir, "classic", "resources.yaml")
	if _, err := os.Stat(classicManifest); err == nil {
		fmt.Println()
		fmt.Println("Classic API generation")
		fmt.Println("======================")
		fmt.Printf("Manifest: %s\n\n", classicManifest)

		classicResources, err := classic.ParseManifest(classicManifest)
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
		}

		classicRegistryPath, err := classicGen.GenerateRegistry(classicResources)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error generating classic registry: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Generated: %s\n", classicRegistryPath)

		fmt.Println()
		fmt.Printf("Successfully generated %d classic resource command(s)\n", len(classicResources))
	}
}
