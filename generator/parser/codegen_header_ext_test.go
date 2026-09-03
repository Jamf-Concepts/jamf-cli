// Copyright 2026, Jamf Software LLC

// This test lives in an external test package because it is the one test that
// needs BOTH generators' codegen header constants, and generator/classic imports
// generator/parser (for the shared schema walk behind --scaffold and --set on
// Classic commands). An in-package test importing classic would be an import
// cycle.
package parser_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/generator/classic"
	"github.com/Jamf-Concepts/jamf-cli/generator/parser"
)

func TestGeneratedFiles_HaveCodegenHeader(t *testing.T) {
	generatedDir := filepath.Join("..", "..", "internal", "commands", "pro", "generated")

	entries, err := os.ReadDir(generatedDir)
	if err != nil {
		t.Skipf("generated directory not found (run from repo root): %v", err)
	}

	modernHeader := parser.CodegenHeader
	classicHeader := classic.CodegenHeader

	var checked int
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		path := filepath.Join(generatedDir, entry.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("failed to read %s: %v", entry.Name(), err)
			continue
		}

		lines := strings.SplitN(string(content), "\n", 3)
		hasCopyright := lines[0] == "// Copyright 2026, Jamf Software LLC"
		headerLine := lines[0]
		if hasCopyright && len(lines) > 1 {
			headerLine = lines[1]
		}
		if headerLine != modernHeader && headerLine != classicHeader {
			t.Errorf("%s: missing code generation header\n  got:  %q\n  want: %q or %q",
				entry.Name(), headerLine, modernHeader, classicHeader)
		}
		if !hasCopyright {
			t.Errorf("%s: missing copyright header on first line", entry.Name())
		}
		checked++
	}

	if checked == 0 {
		t.Skip("no .go files found in generated directory")
	}
	t.Logf("verified %d generated files have correct headers", checked)
}

// --- shouldGenerateApply tests ---
