// Copyright 2026, Jamf Software LLC

package generated

import (
	"encoding/json"
	"testing"
)

// TestParsePatchValue covers the type-coercion rules used by --set.
func TestParsePatchValue(t *testing.T) {
	tests := []struct {
		input string
		want  any
	}{
		{"true", true},
		{"false", false},
		{"null", nil},
		{"42", int64(42)},
		{"-7", int64(-7)},
		{"0", int64(0)},
		{"3.14", "3.14"}, // floats stay as strings (no float coercion)
		{"hello", "hello"},
		{"", ""},
		{"123abc", "123abc"}, // not a pure integer
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parsePatchValue(tt.input)
			if got != tt.want {
				t.Errorf("parsePatchValue(%q) = %#v (%T), want %#v (%T)", tt.input, got, got, tt.want, tt.want)
			}
		})
	}
}

// TestSetNestedValue covers flat and dot-notation path setting.
func TestSetNestedValue(t *testing.T) {
	t.Run("flat key", func(t *testing.T) {
		m := map[string]any{}
		if err := setNestedValue(m, []string{"name"}, "Alice"); err != nil {
			t.Fatal(err)
			return
		}
		if m["name"] != "Alice" {
			t.Errorf("got %v, want Alice", m["name"])
		}
	})

	t.Run("nested key creates intermediate map", func(t *testing.T) {
		m := map[string]any{}
		if err := setNestedValue(m, []string{"general", "managed"}, true); err != nil {
			t.Fatal(err)
			return
		}
		general, ok := m["general"].(map[string]any)
		if !ok {
			t.Fatalf("general is not a map: %T", m["general"])
		}
		if general["managed"] != true {
			t.Errorf("got %v, want true", general["managed"])
		}
	})

	t.Run("existing intermediate map is reused", func(t *testing.T) {
		m := map[string]any{
			"general": map[string]any{"name": "existing"},
		}
		if err := setNestedValue(m, []string{"general", "assetTag"}, "TAG"); err != nil {
			t.Fatal(err)
			return
		}
		general := m["general"].(map[string]any)
		if general["name"] != "existing" {
			t.Error("existing sibling key was clobbered")
		}
		if general["assetTag"] != "TAG" {
			t.Errorf("assetTag = %v, want TAG", general["assetTag"])
		}
	})

	t.Run("conflict: non-object at intermediate path", func(t *testing.T) {
		m := map[string]any{"general": "not-a-map"}
		err := setNestedValue(m, []string{"general", "managed"}, true)
		if err == nil {
			t.Error("expected error setting nested key under non-object, got nil")
		}
	})
}

// TestBuildMergePatchFromSet covers the full --set pipeline.
func TestBuildMergePatchFromSet(t *testing.T) {
	t.Run("single flat key", func(t *testing.T) {
		data, err := buildMergePatchFromSet([]string{"name=Alice"}, nil)
		if err != nil {
			t.Fatal(err)
			return
		}
		var got map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
			return
		}
		if got["name"] != "Alice" {
			t.Errorf("name = %v, want Alice", got["name"])
		}
	})

	t.Run("nested dot-notation key", func(t *testing.T) {
		data, err := buildMergePatchFromSet([]string{"general.managed=true"}, nil)
		if err != nil {
			t.Fatal(err)
			return
		}
		var got map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
			return
		}
		general, ok := got["general"].(map[string]any)
		if !ok {
			t.Fatalf("general not a map: %T", got["general"])
		}
		if general["managed"] != true {
			t.Errorf("general.managed = %v, want true", general["managed"])
		}
	})

	t.Run("null clears a field", func(t *testing.T) {
		data, err := buildMergePatchFromSet([]string{"assetTag=null"}, nil)
		if err != nil {
			t.Fatal(err)
			return
		}
		var got map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
			return
		}
		v, exists := got["assetTag"]
		if !exists {
			t.Fatal("assetTag key missing from output")
			return
		}
		if v != nil {
			t.Errorf("assetTag = %v, want nil (JSON null)", v)
		}
	})

	t.Run("multiple pairs merged", func(t *testing.T) {
		data, err := buildMergePatchFromSet([]string{
			"general.assetTag=CORP-001",
			"general.name=My Mac",
			"udid=abc-123",
		}, nil)
		if err != nil {
			t.Fatal(err)
			return
		}
		var got map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
			return
		}
		general := got["general"].(map[string]any)
		if general["assetTag"] != "CORP-001" {
			t.Errorf("general.assetTag = %v", general["assetTag"])
		}
		if general["name"] != "My Mac" {
			t.Errorf("general.name = %v", general["name"])
		}
		if got["udid"] != "abc-123" {
			t.Errorf("udid = %v", got["udid"])
		}
	})

	t.Run("missing equals sign is an error", func(t *testing.T) {
		_, err := buildMergePatchFromSet([]string{"noequals"}, nil)
		if err == nil {
			t.Error("expected error for pair without '=', got nil")
		}
	})

	t.Run("empty key before equals is an error", func(t *testing.T) {
		_, err := buildMergePatchFromSet([]string{"=value"}, nil)
		if err == nil {
			t.Error("expected error for empty key, got nil")
		}
	})

	t.Run("integer value coerced", func(t *testing.T) {
		data, err := buildMergePatchFromSet([]string{"purchasing.lifeExpectancy=5"}, nil)
		if err != nil {
			t.Fatal(err)
			return
		}
		var got map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
			return
		}
		purchasing := got["purchasing"].(map[string]any)
		// JSON numbers unmarshal to float64; confirm it round-trips as 5
		if purchasing["lifeExpectancy"] != float64(5) {
			t.Errorf("lifeExpectancy = %v (%T), want 5", purchasing["lifeExpectancy"], purchasing["lifeExpectancy"])
		}
	})
}

// TestBuildMergePatchFromSet_Typed covers schema-driven value parsing: array and
// object fields are JSON-decoded, scalars are coerced to their declared type, and
// type-mismatched input is rejected rather than silently stringified (issue #304).
func TestBuildMergePatchFromSet_Typed(t *testing.T) {
	types := map[string]string{
		"customPackageIds": "array",
		"skipSetupItems":   "object",
		"enrollmentSiteId": "string",
		"versionLock":      "integer",
		"purchasePrice":    "number",
		"mandatory":        "boolean",
	}

	decode := func(t *testing.T, data []byte) map[string]any {
		t.Helper()
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return m
	}

	t.Run("array field parses JSON array", func(t *testing.T) {
		data, err := buildMergePatchFromSet([]string{`customPackageIds=["295"]`}, types)
		if err != nil {
			t.Fatal(err)
		}
		got := decode(t, data)
		arr, ok := got["customPackageIds"].([]any)
		if !ok {
			t.Fatalf("customPackageIds is %T, want []any", got["customPackageIds"])
		}
		if len(arr) != 1 || arr[0] != "295" {
			t.Errorf("customPackageIds = %#v, want [\"295\"]", arr)
		}
	})

	t.Run("array field rejects bare scalar", func(t *testing.T) {
		_, err := buildMergePatchFromSet([]string{"customPackageIds=295"}, types)
		if err == nil {
			t.Fatal("expected error for scalar into array field, got nil")
		}
	})

	t.Run("array field rejects dot index", func(t *testing.T) {
		_, err := buildMergePatchFromSet([]string{"customPackageIds.0=295"}, types)
		if err == nil {
			t.Fatal("expected error for dot index into array field, got nil")
		}
	})

	t.Run("object field parses JSON object", func(t *testing.T) {
		data, err := buildMergePatchFromSet([]string{`skipSetupItems={"FileVault":true}`}, types)
		if err != nil {
			t.Fatal(err)
		}
		got := decode(t, data)
		obj, ok := got["skipSetupItems"].(map[string]any)
		if !ok {
			t.Fatalf("skipSetupItems is %T, want map", got["skipSetupItems"])
		}
		if obj["FileVault"] != true {
			t.Errorf("skipSetupItems.FileVault = %v, want true", obj["FileVault"])
		}
	})

	t.Run("string field keeps numeric-looking value as string", func(t *testing.T) {
		data, err := buildMergePatchFromSet([]string{"enrollmentSiteId=-1"}, types)
		if err != nil {
			t.Fatal(err)
		}
		got := decode(t, data)
		if got["enrollmentSiteId"] != "-1" {
			t.Errorf("enrollmentSiteId = %#v (%T), want \"-1\" (string)", got["enrollmentSiteId"], got["enrollmentSiteId"])
		}
	})

	t.Run("integer field coerces and rejects non-integer", func(t *testing.T) {
		data, err := buildMergePatchFromSet([]string{"versionLock=7"}, types)
		if err != nil {
			t.Fatal(err)
		}
		if got := decode(t, data); got["versionLock"] != float64(7) {
			t.Errorf("versionLock = %v, want 7", got["versionLock"])
		}
		if _, err := buildMergePatchFromSet([]string{"versionLock=abc"}, types); err == nil {
			t.Error("expected error for non-integer versionLock, got nil")
		}
	})

	t.Run("boolean field coerces and rejects non-boolean", func(t *testing.T) {
		data, err := buildMergePatchFromSet([]string{"mandatory=true"}, types)
		if err != nil {
			t.Fatal(err)
		}
		if got := decode(t, data); got["mandatory"] != true {
			t.Errorf("mandatory = %v, want true", got["mandatory"])
		}
		if _, err := buildMergePatchFromSet([]string{"mandatory=yes"}, types); err == nil {
			t.Error("expected error for non-boolean mandatory, got nil")
		}
	})

	t.Run("number field coerces float", func(t *testing.T) {
		data, err := buildMergePatchFromSet([]string{"purchasePrice=19.99"}, types)
		if err != nil {
			t.Fatal(err)
		}
		if got := decode(t, data); got["purchasePrice"] != 19.99 {
			t.Errorf("purchasePrice = %v, want 19.99", got["purchasePrice"])
		}
	})

	t.Run("null clears a typed array field", func(t *testing.T) {
		data, err := buildMergePatchFromSet([]string{"customPackageIds=null"}, types)
		if err != nil {
			t.Fatal(err)
		}
		got := decode(t, data)
		if v, ok := got["customPackageIds"]; !ok || v != nil {
			t.Errorf("customPackageIds = %#v, want JSON null", got["customPackageIds"])
		}
	})

	t.Run("unmodelled field still parses JSON array (fallback)", func(t *testing.T) {
		data, err := buildMergePatchFromSet([]string{`unknownArr=["a","b"]`}, types)
		if err != nil {
			t.Fatal(err)
		}
		got := decode(t, data)
		if arr, ok := got["unknownArr"].([]any); !ok || len(arr) != 2 {
			t.Errorf("unknownArr = %#v, want [\"a\",\"b\"]", got["unknownArr"])
		}
	})
}
