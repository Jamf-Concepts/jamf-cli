// Copyright 2026, Jamf Software LLC

package security

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseSetValue(t *testing.T) {
	cases := []struct {
		raw  string
		want any
	}{
		{"true", true},
		{"false", false},
		{"null", nil},
		{"42", int64(42)},
		{"3.14", 3.14},
		{"plain", "plain"},
		{`"quoted"`, "quoted"},
		{"[1,2]", []any{float64(1), float64(2)}},
		{`{"a":1}`, map[string]any{"a": float64(1)}},
	}
	for _, c := range cases {
		got := parseSetValue(c.raw)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("parseSetValue(%q) = %#v, want %#v", c.raw, got, c.want)
		}
	}
}

func TestApplySet_NestedCreatesMaps(t *testing.T) {
	m := map[string]any{}
	if err := applySet(m, []string{"a", "b", "c"}, "v"); err != nil {
		t.Fatalf("applySet() error = %v", err)
	}
	want := map[string]any{"a": map[string]any{"b": map[string]any{"c": "v"}}}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("applySet() = %#v, want %#v", m, want)
	}
}

func TestApplySet_OverwritesLeaf(t *testing.T) {
	m := map[string]any{"a": "old"}
	if err := applySet(m, []string{"a"}, "new"); err != nil {
		t.Fatalf("applySet() error = %v", err)
	}
	if m["a"] != "new" {
		t.Errorf("m[a] = %v, want %q", m["a"], "new")
	}
}

func TestApplySet_ErrorsThroughNonObject(t *testing.T) {
	// Regression: a --set path that descends through a segment already
	// holding a scalar must error, not silently replace it with {}.
	m := map[string]any{"delivery": "immediate"}
	err := applySet(m, []string{"delivery", "method"}, "push")
	if err == nil {
		t.Fatal("applySet() error = nil, want error")
	}
	if !strings.Contains(err.Error(), `"delivery"`) {
		t.Errorf("error %q missing offending field name", err.Error())
	}
	if m["delivery"] != "immediate" {
		t.Errorf("m[delivery] = %v, want original value preserved", m["delivery"])
	}
}

func TestApplySet_ErrorsThroughArray(t *testing.T) {
	m := map[string]any{"tags": []any{"a", "b"}}
	if err := applySet(m, []string{"tags", "0"}, "c"); err == nil {
		t.Fatal("applySet() error = nil, want error")
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

func TestReadBody_SetOnly(t *testing.T) {
	body, err := ReadBody("", []string{"a.b=1", "c=true"})
	if err != nil {
		t.Fatalf("ReadBody() error = %v", err)
	}
	want := map[string]any{"a": map[string]any{"b": int64(1)}, "c": true}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("ReadBody() = %#v, want %#v", body, want)
	}
}

func TestReadBody_FileAndSetMerge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.json")
	if err := os.WriteFile(path, []byte(`{"delivery":{"method":"pull"},"kept":"yes"}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	body, err := ReadBody(path, []string{"delivery.method=push"})
	if err != nil {
		t.Fatalf("ReadBody() error = %v", err)
	}
	want := map[string]any{"delivery": map[string]any{"method": "push"}, "kept": "yes"}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("ReadBody() = %#v, want %#v", body, want)
	}
}

func TestReadBody_SetThroughFileScalarErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.json")
	if err := os.WriteFile(path, []byte(`{"delivery":"immediate"}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := ReadBody(path, []string{"delivery.method=push"})
	if err == nil {
		t.Fatal("ReadBody() error = nil, want error")
	}
}

func TestReadBody_InvalidSetSyntax(t *testing.T) {
	if _, err := ReadBody("", []string{"no-equals-sign"}); err == nil {
		t.Fatal("ReadBody() error = nil, want error")
	}
}

func TestReadBody_NonObjectFileRejectsSet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.json")
	if err := os.WriteFile(path, []byte(`[1,2,3]`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := ReadBody(path, []string{"a=1"}); err == nil {
		t.Fatal("ReadBody() error = nil, want error")
	}
}

func TestReadBody_FileOnlyRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.json")
	raw := `{"a":1,"b":"two"}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	body, err := ReadBody(path, nil)
	if err != nil {
		t.Fatalf("ReadBody() error = %v", err)
	}
	got, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var gotM, wantM map[string]any
	_ = json.Unmarshal(got, &gotM)
	_ = json.Unmarshal([]byte(raw), &wantM)
	if !reflect.DeepEqual(gotM, wantM) {
		t.Errorf("ReadBody() = %v, want %v", gotM, wantM)
	}
}

func TestReadBody_YAMLFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.yaml")
	raw := "delivery:\n  method: pull\nkept: \"yes\"\nenabled: true\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	body, err := ReadBody(path, nil)
	if err != nil {
		t.Fatalf("ReadBody() error = %v", err)
	}
	// yaml.v3 follows the YAML 1.2 core schema, so "yes" stays a string and
	// only true/false are booleans — worth pinning, since YAML 1.1 parsers
	// would turn "yes" into a boolean and change what gets sent.
	want := map[string]any{"delivery": map[string]any{"method": "pull"}, "kept": "yes", "enabled": true}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("ReadBody() = %#v, want %#v", body, want)
	}
}

// --set has to descend into a YAML-supplied body exactly as it does a JSON one;
// the two formats share one decode path so that they cannot diverge here.
func TestReadBody_YAMLFileAndSetMerge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.yaml")
	if err := os.WriteFile(path, []byte("delivery:\n  method: pull\nkept: \"yes\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	body, err := ReadBody(path, []string{"delivery.method=push"})
	if err != nil {
		t.Fatalf("ReadBody() error = %v", err)
	}
	want := map[string]any{"delivery": map[string]any{"method": "push"}, "kept": "yes"}
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
