// Copyright 2026, Jamf Software LLC

package platform

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReadBody_JSONAndYAMLFilesAgree(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "body.json")
	yamlPath := filepath.Join(dir, "body.yaml")
	if err := os.WriteFile(jsonPath, []byte(`{"name":"blueprint","scope":{"deviceGroupIds":["g-1"]}}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(yamlPath, []byte("name: blueprint\nscope:\n  deviceGroupIds:\n    - g-1\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	fromJSON, err := ReadBody(jsonPath, nil)
	if err != nil {
		t.Fatalf("ReadBody(json) error = %v", err)
	}
	fromYAML, err := ReadBody(yamlPath, nil)
	if err != nil {
		t.Fatalf("ReadBody(yaml) error = %v", err)
	}
	if !reflect.DeepEqual(fromJSON, fromYAML) {
		t.Errorf("formats disagree:\n json = %#v\n yaml = %#v", fromJSON, fromYAML)
	}
}

// --set has to descend into a YAML-supplied body exactly as it does a JSON one.
func TestReadBody_YAMLFileAndSetMerge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.yaml")
	if err := os.WriteFile(path, []byte("name: blueprint\nsecureDns: false\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	body, err := ReadBody(path, []string{"secureDns=true"})
	if err != nil {
		t.Fatalf("ReadBody() error = %v", err)
	}
	want := map[string]any{"name": "blueprint", "secureDns": true}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("ReadBody() = %#v, want %#v", body, want)
	}
}

// An empty file used to decode to a nil body, which every caller reads as
// "send no body" — a write that silently sent nothing.
func TestReadBody_EmptyFileErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.yaml")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := ReadBody(path, nil); err == nil {
		t.Fatal("ReadBody() error = nil, want error")
	}
}

func TestReadBody_UnparseableFileNamesTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.yaml")
	if err := os.WriteFile(path, []byte("{this is: [not valid, either\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := ReadBody(path, nil)
	if err == nil {
		t.Fatal("ReadBody() error = nil, want error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error should name the file, got %q", err)
	}
}

func TestReadBody_NoFileNoSet(t *testing.T) {
	body, err := ReadBody("", nil)
	if err != nil {
		t.Fatalf("ReadBody() error = %v", err)
	}
	if body != nil {
		t.Errorf("ReadBody() = %#v, want nil", body)
	}
}
