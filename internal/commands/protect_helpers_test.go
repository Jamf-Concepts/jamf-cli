// Copyright 2026, Jamf Software LLC

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

type testInput struct {
	Name  string `json:"Name" yaml:"Name"`
	Count int    `json:"Count" yaml:"Count"`
}

func TestUnmarshalProtectInput_JSON(t *testing.T) {
	var out testInput
	err := unmarshalInput([]byte(`{"Name":"hello","Count":42}`), &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Name != "hello" {
		t.Errorf("Name = %q, want %q", out.Name, "hello")
	}
	if out.Count != 42 {
		t.Errorf("Count = %d, want %d", out.Count, 42)
	}
}

func TestUnmarshalProtectInput_YAML(t *testing.T) {
	var out testInput
	err := unmarshalInput([]byte("Name: world\nCount: 7\n"), &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Name != "world" {
		t.Errorf("Name = %q, want %q", out.Name, "world")
	}
	if out.Count != 7 {
		t.Errorf("Count = %d, want %d", out.Count, 7)
	}
}

func TestUnmarshalProtectInput_Invalid(t *testing.T) {
	var out testInput
	err := unmarshalInput([]byte("<<<garbage>>>"), &out)
	if err == nil {
		t.Fatal("expected error for garbage input")
		return
	}
}

func TestUnmarshalProtectInput_YAMLPreferred(t *testing.T) {
	// YAML that is not valid JSON should still parse correctly.
	input := []byte("Name: yaml-only\nCount: 99\n")
	var out testInput
	err := unmarshalInput(input, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Name != "yaml-only" {
		t.Errorf("Name = %q, want %q", out.Name, "yaml-only")
	}
	if out.Count != 99 {
		t.Errorf("Count = %d, want %d", out.Count, 99)
	}
}

func TestReadProtectInput_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input.json")
	content := `{"Name":"from-file"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	data, err := readInput(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != content {
		t.Errorf("data = %q, want %q", string(data), content)
	}
}

func TestReadProtectInput_EmptyFromFile(t *testing.T) {
	_, err := readInput("/nonexistent/path/does-not-exist.json")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
		return
	}
}

func TestReadProtectInput_NoInput(t *testing.T) {
	// When fromFile is empty and stdin is a terminal, we should get an error.
	_, err := readInput("")
	if err == nil {
		t.Fatal("expected error when no file and no stdin pipe")
		return
	}
}

func TestWriteBase64File_Permissions(t *testing.T) {
	dir := t.TempDir()
	// "aGVsbG8=" is base64 for "hello"
	b64 := "aGVsbG8="

	tests := []struct {
		name string
		perm os.FileMode
	}{
		{"restrictive", 0o600},
		{"world-readable", 0o644},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".bin")
			if err := writeBase64File(b64, path, tc.perm); err != nil {
				t.Fatalf("writeBase64File failed: %v", err)
			}

			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat failed: %v", err)
			}
			if got := info.Mode().Perm(); got != tc.perm {
				t.Errorf("file mode = %04o, want %04o", got, tc.perm)
			}

			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading file: %v", err)
			}
			if string(content) != "hello" {
				t.Errorf("content = %q, want %q", string(content), "hello")
			}
		})
	}
}

// TestPrintResult_MisCasedFormatStillFlattens is issue 353 at this call site.
// The switch matched "table", "csv" and "plain" exactly, so a mis-cased
// -o Table — which Print renders as a table through its default arm — took the
// full nested struct to a table renderer instead of the flattened map the
// caller built for exactly that purpose.
func TestPrintResult_MisCasedFormatStillFlattens(t *testing.T) {
	type nested struct {
		Name  string         `json:"name"`
		Inner map[string]any `json:"inner"`
	}
	item := nested{Name: "plan-a", Inner: map[string]any{"deep": "value"}}
	flattened := map[string]any{"name": "plan-a", "inner.deep": "value"}

	old := outputFmt
	defer func() { outputFmt = old }()

	for _, format := range []string{"Table", "TABLE", "Csv", "json-multi", "wibble"} {
		outputFmt = format
		out := &captureRawFormatter{}
		if err := printResult(out, item, flattened); err != nil {
			t.Fatalf("printResult(-o %s): %v", format, err)
		}
		if !strings.Contains(string(out.raw), "inner.deep") {
			t.Errorf("-o %s did not print the flattened map, so a column renderer got the nested struct: %s", format, out.raw)
		}
	}
}

// The keep-set still gets the full struct, which is what printResult narrows
// away from.
func TestPrintResult_StructuredFormatsGetTheFullStruct(t *testing.T) {
	type nested struct {
		Name  string         `json:"name"`
		Inner map[string]any `json:"inner"`
	}
	item := nested{Name: "plan-a", Inner: map[string]any{"deep": "value"}}
	flattened := map[string]any{"name": "plan-a", "inner.deep": "value"}

	old := outputFmt
	defer func() { outputFmt = old }()

	for _, format := range []string{"json", "yaml", "ndjson", "xml", "raw"} {
		outputFmt = format
		out := &captureRawFormatter{}
		if err := printResult(out, item, flattened); err != nil {
			t.Fatalf("printResult(-o %s): %v", format, err)
		}
		if strings.Contains(string(out.raw), "inner.deep") {
			t.Errorf("-o %s printed the flattened map, so the narrowing reached a structured format: %s", format, out.raw)
		}
		if !strings.Contains(string(out.raw), `"inner"`) {
			t.Errorf("-o %s lost the nested struct: %s", format, out.raw)
		}
	}
}

// captureRawFormatter records what printResult writes. Only PrintRaw is
// implemented; protect.PrintOne routes through it too.
type captureRawFormatter struct {
	registry.OutputFormatter
	raw []byte
}

func (c *captureRawFormatter) PrintRaw(data []byte) error {
	c.raw = data
	return nil
}
