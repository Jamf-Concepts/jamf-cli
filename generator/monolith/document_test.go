// Copyright 2026, Jamf Software LLC

package monolith

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile writes content into dir/name and fails the test on error.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return path
}

const monolithJSON = `{
  "openapi": "3.0.1",
  "info": {"title": "Jamf Pro API", "version": "1.0.0"},
  "paths": {
    "/v1/buildings": {"get": {"tags": ["buildings"], "responses": {"200": {"description": "ok"}}}},
    "/v1/buildings/{id}": {"get": {"tags": ["buildings"], "responses": {"200": {"description": "ok"}}}}
  },
  "components": {
    "schemas": {
      "Building": {
        "type": "object",
        "properties": {
          "id": {"type": "string", "example": 3},
          "name": {"type": "string", "example": "HQ"}
        }
      }
    }
  }
}`

// One document in, one document out — no per-resource carving.
func TestNormalise_WritesOneDocument(t *testing.T) {
	specsDir := t.TempDir()
	src := writeFile(t, t.TempDir(), "monolith.json", monolithJSON)

	out, err := Normalise(src, specsDir)
	if err != nil {
		t.Fatalf("Normalise() error = %v", err)
	}
	if filepath.Base(out) != NormalisedSpecFile {
		t.Errorf("wrote %s, want %s", filepath.Base(out), NormalisedSpecFile)
	}

	entries, _ := os.ReadDir(specsDir)
	var yamls []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".yaml") {
			yamls = append(yamls, e.Name())
		}
	}
	if len(yamls) != 1 {
		t.Errorf("specsDir holds %v, want exactly one document", yamls)
	}

	doc, err := readDoc(out)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	paths, _ := asMap(doc["paths"])
	if len(paths) != 2 {
		t.Errorf("round trip kept %d paths, want 2", len(paths))
	}
}

// The document is JSON, so a `type: string` field with `example: 3` decodes as a
// number and every scaffold built from it emits a numeric literal.
func TestNormalise_CoercesAnExampleToItsDeclaredType(t *testing.T) {
	specsDir := t.TempDir()
	src := writeFile(t, t.TempDir(), "monolith.json", monolithJSON)
	out, err := Normalise(src, specsDir)
	if err != nil {
		t.Fatalf("Normalise() error = %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `example: "3"`) {
		t.Errorf("example was not coerced to a string; got:\n%s", data)
	}
}

// Sorted output, so a spec ingest produces a diff someone can read. That is the
// one thing the 165-file layout was genuinely good for, and it survives.
func TestNormalise_IsDeterministic(t *testing.T) {
	src := writeFile(t, t.TempDir(), "monolith.json", monolithJSON)
	first, second := t.TempDir(), t.TempDir()
	a, err := Normalise(src, first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Normalise(src, second)
	if err != nil {
		t.Fatal(err)
	}
	left, _ := os.ReadFile(a)
	right, _ := os.ReadFile(b)
	if string(left) != string(right) {
		t.Error("two runs over one input produced different bytes")
	}
	if !strings.Contains(string(left), "\n") || len(strings.Split(string(left), "\n")) < 10 {
		t.Error("output is not line-oriented; a one-line document is an unreviewable diff")
	}
}

// A document with no paths is refused rather than written, so a fetch that
// returned an error page does not overwrite the spec with it.
func TestNormalise_RefusesADocumentWithNoPaths(t *testing.T) {
	specsDir := t.TempDir()
	src := writeFile(t, t.TempDir(), "notaspec.json", `{"error":"unauthorized"}`)
	if _, err := Normalise(src, specsDir); err == nil {
		t.Error("Normalise accepted a document with no paths")
	}
	if entries, _ := os.ReadDir(specsDir); len(entries) != 0 {
		t.Errorf("a refused normalise still wrote %d file(s)", len(entries))
	}
}

// The prune is what turns the old 165-file layout into the documents replacing
// it, and it must leave alone everything it does not own.
func TestPruneStaleSpecs(t *testing.T) {
	specsDir := t.TempDir()
	writeFile(t, specsDir, NormalisedSpecFile, "openapi: 3.0.1\n")
	writeFile(t, specsDir, "AppInstallers.yaml", "openapi: 3.0.1\n")
	writeFile(t, specsDir, "Building.yaml", "openapi: 3.0.1\n")
	writeFile(t, specsDir, "MobileDevice.yaml", "openapi: 3.0.1\n")
	writeFile(t, specsDir, ".spec-version", "11.31.0\n")
	writeFile(t, specsDir, "notes.txt", "keep me\n")
	if err := os.Mkdir(filepath.Join(specsDir, "classic"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(specsDir, "classic"), "resources.yaml", "resources: []\n")

	removed, err := PruneStaleSpecs(specsDir, []string{
		filepath.Join(specsDir, NormalisedSpecFile),
		filepath.Join(specsDir, "AppInstallers.yaml"),
	})
	if err != nil {
		t.Fatalf("PruneStaleSpecs() error = %v", err)
	}
	if want := "Building.yaml MobileDevice.yaml"; strings.Join(removed, " ") != want {
		t.Errorf("removed %v, want %s", removed, want)
	}
	for _, keep := range []string{NormalisedSpecFile, "AppInstallers.yaml", ".spec-version", "notes.txt", "classic"} {
		if _, err := os.Stat(filepath.Join(specsDir, keep)); err != nil {
			t.Errorf("%s should have survived: %v", keep, err)
		}
	}
	if _, err := os.Stat(filepath.Join(specsDir, "classic", "resources.yaml")); err != nil {
		t.Errorf("the classic/ subdirectory was touched: %v", err)
	}
}
