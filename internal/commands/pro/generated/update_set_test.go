// Copyright 2026, Jamf Software LLC

package generated

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestFieldFilterApply covers the writable-field filtering that "update --set"
// applies to a fetched resource before merge-put: read-only / server-computed
// fields must be stripped, nested objects filtered recursively, and opaque
// subtrees (leaf entries and unknown keys' children) left intact.
func TestFieldFilterApply(t *testing.T) {
	t.Run("nil filter keeps everything", func(t *testing.T) {
		data := map[string]any{"id": "1", "name": "HQ"}
		var f *fieldFilter
		f.apply(data)
		if len(data) != 2 {
			t.Errorf("nil filter changed data: %v", data)
		}
	})

	t.Run("empty fields map keeps everything", func(t *testing.T) {
		data := map[string]any{"id": "1", "name": "HQ"}
		(&fieldFilter{}).apply(data)
		if len(data) != 2 {
			t.Errorf("empty filter changed data: %v", data)
		}
	})

	t.Run("drops fields not in the allowlist", func(t *testing.T) {
		data := map[string]any{"id": "1", "name": "HQ", "href": "/x"}
		f := &fieldFilter{fields: map[string]*fieldFilter{"name": nil}}
		f.apply(data)
		want := map[string]any{"name": "HQ"}
		if !reflect.DeepEqual(data, want) {
			t.Errorf("got %v, want %v", data, want)
		}
	})

	t.Run("recurses into nested objects", func(t *testing.T) {
		data := map[string]any{
			"name": "HQ",
			"general": map[string]any{
				"barcode": "abc",
				"id":      "computed", // read-only nested field, must drop
			},
		}
		f := &fieldFilter{fields: map[string]*fieldFilter{
			"name":    nil,
			"general": {fields: map[string]*fieldFilter{"barcode": nil}},
		}}
		f.apply(data)
		want := map[string]any{
			"name":    "HQ",
			"general": map[string]any{"barcode": "abc"},
		}
		if !reflect.DeepEqual(data, want) {
			t.Errorf("got %v, want %v", data, want)
		}
	})

	t.Run("leaf entry keeps whole subtree (opaque object)", func(t *testing.T) {
		data := map[string]any{
			"settings": map[string]any{"anything": "kept", "nested": map[string]any{"x": 1}},
			"drop":     "me",
		}
		// "settings" is a leaf (nil) => not filtered internally.
		f := &fieldFilter{fields: map[string]*fieldFilter{"settings": nil}}
		f.apply(data)
		want := map[string]any{
			"settings": map[string]any{"anything": "kept", "nested": map[string]any{"x": 1}},
		}
		if !reflect.DeepEqual(data, want) {
			t.Errorf("got %v, want %v", data, want)
		}
	})

	t.Run("non-object value under a recursing filter is left as-is", func(t *testing.T) {
		// filter expects "general" to be an object, but the data has a scalar.
		data := map[string]any{"general": "scalar"}
		f := &fieldFilter{fields: map[string]*fieldFilter{
			"general": {fields: map[string]*fieldFilter{"x": nil}},
		}}
		f.apply(data)
		if data["general"] != "scalar" {
			t.Errorf("scalar under recursing filter was mangled: %v", data)
		}
	})
}

// TestDeepMergeJSON covers the merge semantics used by "update --set": objects
// merge key-by-key, scalars and arrays replace.
func TestDeepMergeJSON(t *testing.T) {
	t.Run("scalar overwrite, sibling preserved", func(t *testing.T) {
		dst := map[string]any{"name": "old", "city": "NYC"}
		src := map[string]any{"name": "new"}
		deepMergeJSON(dst, src)
		want := map[string]any{"name": "new", "city": "NYC"}
		if !reflect.DeepEqual(dst, want) {
			t.Errorf("got %v, want %v", dst, want)
		}
	})

	t.Run("nested objects merge recursively", func(t *testing.T) {
		dst := map[string]any{"general": map[string]any{"a": "1", "b": "2"}}
		src := map[string]any{"general": map[string]any{"b": "changed"}}
		deepMergeJSON(dst, src)
		want := map[string]any{"general": map[string]any{"a": "1", "b": "changed"}}
		if !reflect.DeepEqual(dst, want) {
			t.Errorf("got %v, want %v", dst, want)
		}
	})

	t.Run("array replaces, not merges", func(t *testing.T) {
		dst := map[string]any{"tags": []any{"a", "b"}}
		src := map[string]any{"tags": []any{"c"}}
		deepMergeJSON(dst, src)
		want := map[string]any{"tags": []any{"c"}}
		if !reflect.DeepEqual(dst, want) {
			t.Errorf("got %v, want %v", dst, want)
		}
	})

	t.Run("object replaces scalar when types differ", func(t *testing.T) {
		dst := map[string]any{"field": "scalar"}
		src := map[string]any{"field": map[string]any{"x": "1"}}
		deepMergeJSON(dst, src)
		want := map[string]any{"field": map[string]any{"x": "1"}}
		if !reflect.DeepEqual(dst, want) {
			t.Errorf("got %v, want %v", dst, want)
		}
	})
}

// TestUpdateSetEndToEnd exercises the fetch→filter→merge→marshal pipeline the
// generated "update --set" code runs, using the real helpers.
func TestUpdateSetEndToEnd(t *testing.T) {
	// Simulated GET response: includes a read-only "id" the PUT would reject.
	fetched := []byte(`{"id":"12","name":"HQ","city":"NYC","href":"/x"}`)

	current := map[string]any{}
	if err := json.Unmarshal(fetched, &current); err != nil {
		t.Fatal(err)
	}
	// Writable allowlist from the (hypothetical) PUT schema: name, city.
	filter := &fieldFilter{fields: map[string]*fieldFilter{"name": nil, "city": nil}}
	filter.apply(current)

	setDoc, err := buildMergePatchFromSet([]string{"city=Boston"})
	if err != nil {
		t.Fatal(err)
	}
	setMap := map[string]any{}
	if err := json.Unmarshal(setDoc, &setMap); err != nil {
		t.Fatal(err)
	}
	deepMergeJSON(current, setMap)

	want := map[string]any{"name": "HQ", "city": "Boston"}
	if !reflect.DeepEqual(current, want) {
		t.Errorf("got %v, want %v (id/href should be dropped, city merged)", current, want)
	}
}
