// Copyright 2026, Jamf Software LLC

package generated

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRemoveJSONFields_StripsListedKeys(t *testing.T) {
	in := []byte(`{"name":"foo","encodedToken":"abc","tokenFileName":"foo.p7m","siteId":-1}`)
	out, err := removeJSONFields(in, "encodedToken", "tokenFileName")
	if err != nil {
		t.Fatalf("removeJSONFields: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output not valid JSON: %v (%s)", err, out)
	}
	want := map[string]any{"name": "foo", "siteId": float64(-1)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRemoveJSONFields_UnchangedWhenKeysAbsent(t *testing.T) {
	in := []byte(`{"name":"foo","siteId":-1}`)
	out, err := removeJSONFields(in, "encodedToken", "tokenFileName")
	if err != nil {
		t.Fatalf("removeJSONFields: %v", err)
	}
	// When nothing changes the helper returns the input unchanged (same slice).
	if string(out) != string(in) {
		t.Errorf("out = %q, want %q", out, in)
	}
}

func TestRemoveJSONFields_EmptyInputPassesThrough(t *testing.T) {
	out, err := removeJSONFields(nil, "x")
	if err != nil {
		t.Fatalf("removeJSONFields: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("got %q, want empty", out)
	}
}

func TestRemoveJSONFields_NoFieldsPassesThrough(t *testing.T) {
	in := []byte(`{"a":1}`)
	out, err := removeJSONFields(in)
	if err != nil {
		t.Fatalf("removeJSONFields: %v", err)
	}
	if string(out) != string(in) {
		t.Errorf("got %q, want %q", out, in)
	}
}

func TestRemoveJSONFields_NonObjectPassesThrough(t *testing.T) {
	// A JSON array is not an object; the helper returns it unchanged rather
	// than erroring, so callers can pass arbitrary bodies.
	in := []byte(`[1,2,3]`)
	out, err := removeJSONFields(in, "x")
	if err != nil {
		t.Fatalf("removeJSONFields: %v", err)
	}
	if string(out) != string(in) {
		t.Errorf("got %q, want %q", out, in)
	}
}

func TestRemoveJSONFields_ReturnsNilWhenObjectEmptiedOut(t *testing.T) {
	in := []byte(`{"encodedToken":"abc","tokenFileName":"foo.p7m"}`)
	out, err := removeJSONFields(in, "encodedToken", "tokenFileName")
	if err != nil {
		t.Fatalf("removeJSONFields: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("got %q, want empty (object fully stripped)", out)
	}
}
