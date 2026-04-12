// Copyright 2026, Jamf Software LLC

package generated

import (
	"encoding/json"
	"testing"
)

func TestNormalizeInputToJSON(t *testing.T) {
	t.Run("valid JSON is returned unchanged", func(t *testing.T) {
		input := []byte(`{"name":"Test","enabled":true}`)
		out, err := normalizeInputToJSON(input)
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != string(input) {
			t.Errorf("got %s, want %s", out, input)
		}
	})

	t.Run("valid YAML is converted to JSON", func(t *testing.T) {
		input := []byte("name: Test\nenabled: true\n")
		out, err := normalizeInputToJSON(input)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatalf("output is not valid JSON: %v", err)
		}
		if got["name"] != "Test" {
			t.Errorf("name = %v, want Test", got["name"])
		}
		if got["enabled"] != true {
			t.Errorf("enabled = %v, want true", got["enabled"])
		}
	})

	t.Run("YAML with nested fields converts correctly", func(t *testing.T) {
		input := []byte("general:\n  name: My Computer\n  assetTag: CORP-001\n")
		out, err := normalizeInputToJSON(input)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatalf("output is not valid JSON: %v", err)
		}
		general, ok := got["general"].(map[string]any)
		if !ok {
			t.Fatalf("general is not a map: %T", got["general"])
		}
		if general["name"] != "My Computer" {
			t.Errorf("general.name = %v, want My Computer", general["name"])
		}
		if general["assetTag"] != "CORP-001" {
			t.Errorf("general.assetTag = %v, want CORP-001", general["assetTag"])
		}
	})

	t.Run("YAML with integer and null values", func(t *testing.T) {
		input := []byte("count: 42\nempty: null\n")
		out, err := normalizeInputToJSON(input)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatalf("output is not valid JSON: %v", err)
		}
		if got["count"] != float64(42) {
			t.Errorf("count = %v (%T), want 42", got["count"], got["count"])
		}
		if got["empty"] != nil {
			t.Errorf("empty = %v, want nil", got["empty"])
		}
	})

	t.Run("invalid input returns error", func(t *testing.T) {
		input := []byte("{{not valid yaml or json")
		_, err := normalizeInputToJSON(input)
		if err == nil {
			t.Error("expected error for invalid input, got nil")
		}
	})

	t.Run("JSON array is returned unchanged", func(t *testing.T) {
		input := []byte(`[{"id":1},{"id":2}]`)
		out, err := normalizeInputToJSON(input)
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != string(input) {
			t.Errorf("got %s, want %s", out, input)
		}
	})
}
