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

// titleOf reads info.title out of a written spec file.
func titleOf(t *testing.T, path string) string {
	t.Helper()
	doc, err := readDoc(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	info, ok := asMap(doc["info"])
	if !ok {
		t.Fatalf("%s has no info object", path)
	}
	title, _ := info["title"].(string)
	return title
}

// An existing spec file, with one path already in the layout under tag "buildings".
// Its title is filename-derived, as every committed spec's is.
const existingBuildingsSpec = `openapi: 3.0.1
info:
  title: Jamf Pro API - Buildings
  version: 0.0.1
paths:
  /v1/buildings:
    get:
      tags:
        - buildings
      responses:
        "200":
          description: ok
`

// A monolith that gives Buildings.yaml one *new* path under its existing tag,
// and introduces a genuinely new resource under an unseen tag.
const monolithWithNewPaths = `{
  "openapi": "3.0.1",
  "info": {"title": "Jamf Pro API", "version": "1.0.0"},
  "paths": {
    "/v1/buildings": {
      "get": {"tags": ["buildings"], "responses": {"200": {"description": "ok"}}}
    },
    "/v1/buildings/export": {
      "post": {"tags": ["buildings"], "responses": {"200": {"description": "ok"}}}
    },
    "/v2/environment-type": {
      "get": {"tags": ["environment-type"], "responses": {"200": {"description": "ok"}}}
    }
  }
}`

// An existing bucket's info.title must stay filename-derived even when that sync
// happens to route a new path into it. Otherwise the title flips between
// filename- and tag-derived forms depending on whether a given sync brought the
// file a new endpoint, churning the diff with no semantic change.
func TestSplit_ExistingFileKeepsFilenameDerivedTitle(t *testing.T) {
	specsDir := t.TempDir()
	writeFile(t, specsDir, "Buildings.yaml", existingBuildingsSpec)
	monolithPath := writeFile(t, t.TempDir(), "monolith.json", monolithWithNewPaths)

	if _, _, err := Split(monolithPath, specsDir); err != nil {
		t.Fatalf("Split() error = %v", err)
	}

	got := titleOf(t, filepath.Join(specsDir, "Buildings.yaml"))
	if want := "Jamf Pro API - Buildings"; got != want {
		t.Errorf("title = %q, want %q (the new /v1/buildings/export path must not flip it to the tag-derived form)", got, want)
	}
}

// A file the layout has never seen takes its title from the tag that created it.
func TestSplit_NewFileTakesTagDerivedTitle(t *testing.T) {
	specsDir := t.TempDir()
	writeFile(t, specsDir, "Buildings.yaml", existingBuildingsSpec)
	monolithPath := writeFile(t, t.TempDir(), "monolith.json", monolithWithNewPaths)

	if _, _, err := Split(monolithPath, specsDir); err != nil {
		t.Fatalf("Split() error = %v", err)
	}

	newFile := filepath.Join(specsDir, "EnvironmentType.yaml")
	if _, err := os.Stat(newFile); err != nil {
		entries, _ := os.ReadDir(specsDir)
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected a new EnvironmentType.yaml; specsDir holds %v", names)
	}

	got := titleOf(t, newFile)
	if want := "Jamf Pro API - environment-type"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
}

// Both paths under the existing tag stay in the file that already owned the tag,
// rather than fragmenting into a new Building.yaml bucket.
func TestSplit_NewPathJoinsExistingTagOwner(t *testing.T) {
	specsDir := t.TempDir()
	writeFile(t, specsDir, "Buildings.yaml", existingBuildingsSpec)
	monolithPath := writeFile(t, t.TempDir(), "monolith.json", monolithWithNewPaths)

	_, warnings, err := Split(monolithPath, specsDir)
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}

	doc, err := readDoc(filepath.Join(specsDir, "Buildings.yaml"))
	if err != nil {
		t.Fatalf("reading Buildings.yaml: %v", err)
	}
	paths, _ := asMap(doc["paths"])
	for _, want := range []string{"/v1/buildings", "/v1/buildings/export"} {
		if _, ok := paths[want]; !ok {
			t.Errorf("Buildings.yaml missing %s; got paths %v", want, paths)
		}
	}

	// The new path is still reported, so a sync never adds an endpoint silently.
	var reported bool
	for _, w := range warnings {
		if strings.Contains(w, "/v1/buildings/export") {
			reported = true
		}
	}
	if !reported {
		t.Errorf("new path not reported in warnings: %v", warnings)
	}
}
