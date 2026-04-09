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
		}
		if m["name"] != "Alice" {
			t.Errorf("got %v, want Alice", m["name"])
		}
	})

	t.Run("nested key creates intermediate map", func(t *testing.T) {
		m := map[string]any{}
		if err := setNestedValue(m, []string{"general", "managed"}, true); err != nil {
			t.Fatal(err)
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
		data, err := buildMergePatchFromSet([]string{"name=Alice"})
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		if got["name"] != "Alice" {
			t.Errorf("name = %v, want Alice", got["name"])
		}
	})

	t.Run("nested dot-notation key", func(t *testing.T) {
		data, err := buildMergePatchFromSet([]string{"general.managed=true"})
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
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
		data, err := buildMergePatchFromSet([]string{"assetTag=null"})
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		v, exists := got["assetTag"]
		if !exists {
			t.Fatal("assetTag key missing from output")
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
		})
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
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
		_, err := buildMergePatchFromSet([]string{"noequals"})
		if err == nil {
			t.Error("expected error for pair without '=', got nil")
		}
	})

	t.Run("empty key before equals is an error", func(t *testing.T) {
		_, err := buildMergePatchFromSet([]string{"=value"})
		if err == nil {
			t.Error("expected error for empty key, got nil")
		}
	})

	t.Run("integer value coerced", func(t *testing.T) {
		data, err := buildMergePatchFromSet([]string{"purchasing.lifeExpectancy=5"})
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		purchasing := got["purchasing"].(map[string]any)
		// JSON numbers unmarshal to float64; confirm it round-trips as 5
		if purchasing["lifeExpectancy"] != float64(5) {
			t.Errorf("lifeExpectancy = %v (%T), want 5", purchasing["lifeExpectancy"], purchasing["lifeExpectancy"])
		}
	})
}
