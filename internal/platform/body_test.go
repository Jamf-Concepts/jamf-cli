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

// withStdin replaces os.Stdin with a real pipe carrying data for the duration
// of one test. A pipe is what makes the ModeCharDevice check in readBodyInput
// take its "input was piped" branch; a temp file would not, since a regular
// file is not a character device either but also is not how a caller pipes.
func withStdin(t *testing.T, data string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = orig
		_ = r.Close()
	})
	go func() {
		_, _ = w.WriteString(data)
		_ = w.Close()
	}()
}

// The point of the rename: --from-file's whole reason for existing alongside a
// pipe is that `--scaffold | edit | apply` should be a pipeline, not a detour
// through a temp file. Before this, ReadBody only ever called os.ReadFile.
func TestReadBody_StdinPipeSuppliesBody(t *testing.T) {
	withStdin(t, `{"name":"piped"}`)

	body, err := ReadBody("", nil)
	if err != nil {
		t.Fatalf("ReadBody() error = %v", err)
	}
	want := map[string]any{"name": "piped"}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("ReadBody() = %#v, want %#v", body, want)
	}
}

func TestReadBody_StdinPipeAcceptsYAML(t *testing.T) {
	withStdin(t, "name: piped\n")

	body, err := ReadBody("", nil)
	if err != nil {
		t.Fatalf("ReadBody() error = %v", err)
	}
	want := map[string]any{"name": "piped"}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("ReadBody() = %#v, want %#v", body, want)
	}
}

// --set overlays onto a piped body exactly as it overlays onto a file one.
func TestReadBody_StdinPipeAndSetMerge(t *testing.T) {
	withStdin(t, `{"name":"piped","enabled":false}`)

	body, err := ReadBody("", []string{"enabled=true"})
	if err != nil {
		t.Fatalf("ReadBody() error = %v", err)
	}
	want := map[string]any{"name": "piped", "enabled": true}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("ReadBody() = %#v, want %#v", body, want)
	}
}

// A CI runner hands every process a stdin that is not a character device and
// carries nothing. That must read as "no body was supplied", not as an empty
// body and not as an error — otherwise every bodyless write breaks under CI.
// This is the one case an empty named file does NOT share.
func TestReadBody_EmptyStdinPipeIsNotABody(t *testing.T) {
	withStdin(t, "")

	body, err := ReadBody("", nil)
	if err != nil {
		t.Fatalf("ReadBody() error = %v", err)
	}
	if body != nil {
		t.Errorf("ReadBody() = %#v, want nil", body)
	}
}

// A named file wins: it is the explicit instruction, and a pipe is often just
// whatever the previous command in a shell chain happened to leave behind.
func TestReadBody_FileTakesPrecedenceOverStdin(t *testing.T) {
	withStdin(t, `{"name":"piped"}`)
	dir := t.TempDir()
	path := filepath.Join(dir, "body.json")
	if err := os.WriteFile(path, []byte(`{"name":"fromfile"}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	body, err := ReadBody(path, nil)
	if err != nil {
		t.Fatalf("ReadBody() error = %v", err)
	}
	want := map[string]any{"name": "fromfile"}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("ReadBody() = %#v, want %#v", body, want)
	}
}

// The parse error has to name stdin rather than interpolate an empty file path,
// which is what the file-shaped message would have produced ("parsing body file
// : ...").
func TestReadBody_UnparseableStdinNamesStdin(t *testing.T) {
	withStdin(t, "{ not json: [and not yaml\n")

	_, err := ReadBody("", nil)
	if err == nil {
		t.Fatal("ReadBody() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "stdin") {
		t.Errorf("ReadBody() error = %q, want it to mention stdin", err)
	}
}
