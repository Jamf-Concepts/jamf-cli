// Copyright 2026, Jamf Software LLC

package generated

import (
	"encoding/json"
	"testing"
)

func TestSetVersionLockZero(t *testing.T) {
	t.Run("sets top-level versionLock to 0", func(t *testing.T) {
		input := []byte(`{"displayName":"Test","versionLock":42}`)
		got := parseJSON(t, setVersionLockZero(input))
		if vl := got["versionLock"]; vl != float64(0) {
			t.Errorf("versionLock = %v, want 0", vl)
		}
	})

	t.Run("adds versionLock when missing", func(t *testing.T) {
		input := []byte(`{"displayName":"Test"}`)
		got := parseJSON(t, setVersionLockZero(input))
		// versionLock was not present, so it should NOT be added
		// (zeroVersionLocks only modifies existing fields)
		if _, ok := got["versionLock"]; ok {
			t.Error("expected versionLock to not be added when absent")
		}
	})

	t.Run("zeros nested versionLock fields", func(t *testing.T) {
		input := []byte(`{
			"displayName": "Test",
			"versionLock": 5,
			"locationInformation": {"versionLock": 10, "room": "A1"},
			"purchasingInformation": {"versionLock": 20, "vendor": "Apple"}
		}`)
		got := parseJSON(t, setVersionLockZero(input))
		if vl := got["versionLock"]; vl != float64(0) {
			t.Errorf("top-level versionLock = %v, want 0", vl)
		}
		loc := got["locationInformation"].(map[string]any)
		if vl := loc["versionLock"]; vl != float64(0) {
			t.Errorf("locationInformation.versionLock = %v, want 0", vl)
		}
		purch := got["purchasingInformation"].(map[string]any)
		if vl := purch["versionLock"]; vl != float64(0) {
			t.Errorf("purchasingInformation.versionLock = %v, want 0", vl)
		}
	})

	t.Run("invalid JSON returns input unchanged", func(t *testing.T) {
		input := []byte(`not json`)
		out := setVersionLockZero(input)
		if string(out) != string(input) {
			t.Errorf("expected unchanged input, got %s", out)
		}
	})
}

func TestInjectVersionLocks(t *testing.T) {
	t.Run("injects top-level versionLock from server response", func(t *testing.T) {
		userData := []byte(`{"displayName":"Test","versionLock":0}`)
		serverResp := []byte(`{"displayName":"Test","versionLock":42,"id":"1"}`)
		out, err := injectVersionLocks(userData, serverResp)
		if err != nil {
			t.Fatal(err)
		}
		got := parseJSON(t, out)
		if vl := got["versionLock"]; vl != float64(42) {
			t.Errorf("versionLock = %v, want 42", vl)
		}
	})

	t.Run("injects nested versionLock fields", func(t *testing.T) {
		userData := []byte(`{
			"displayName": "Test",
			"versionLock": 0,
			"locationInformation": {"versionLock": 0, "room": "A1"},
			"purchasingInformation": {"versionLock": 0, "vendor": "Apple"}
		}`)
		serverResp := []byte(`{
			"displayName": "Test",
			"versionLock": 10,
			"locationInformation": {"versionLock": 5, "room": "B2", "id": "49"},
			"purchasingInformation": {"versionLock": 3, "id": "1"}
		}`)
		out, err := injectVersionLocks(userData, serverResp)
		if err != nil {
			t.Fatal(err)
		}
		got := parseJSON(t, out)
		if vl := got["versionLock"]; vl != float64(10) {
			t.Errorf("top-level versionLock = %v, want 10", vl)
		}
		loc := got["locationInformation"].(map[string]any)
		if vl := loc["versionLock"]; vl != float64(5) {
			t.Errorf("locationInformation.versionLock = %v, want 5", vl)
		}
		// User field preserved
		if room := loc["room"]; room != "A1" {
			t.Errorf("locationInformation.room = %v, want A1", room)
		}
		purch := got["purchasingInformation"].(map[string]any)
		if vl := purch["versionLock"]; vl != float64(3) {
			t.Errorf("purchasingInformation.versionLock = %v, want 3", vl)
		}
		// User field preserved
		if vendor := purch["vendor"]; vendor != "Apple" {
			t.Errorf("purchasingInformation.vendor = %v, want Apple", vendor)
		}
	})

	t.Run("does not add versionLock to objects absent in user data", func(t *testing.T) {
		userData := []byte(`{"displayName":"Test","versionLock":0}`)
		serverResp := []byte(`{
			"displayName": "Test",
			"versionLock": 5,
			"locationInformation": {"versionLock": 3}
		}`)
		out, err := injectVersionLocks(userData, serverResp)
		if err != nil {
			t.Fatal(err)
		}
		got := parseJSON(t, out)
		if vl := got["versionLock"]; vl != float64(5) {
			t.Errorf("versionLock = %v, want 5", vl)
		}
		// locationInformation not in user data — should not appear
		if _, ok := got["locationInformation"]; ok {
			t.Error("expected locationInformation to not be added")
		}
	})

	t.Run("preserves non-versionLock user fields", func(t *testing.T) {
		userData := []byte(`{"displayName":"MyName","versionLock":0,"mandatory":true}`)
		serverResp := []byte(`{"displayName":"OldName","versionLock":7,"mandatory":false}`)
		out, err := injectVersionLocks(userData, serverResp)
		if err != nil {
			t.Fatal(err)
		}
		got := parseJSON(t, out)
		if name := got["displayName"]; name != "MyName" {
			t.Errorf("displayName = %v, want MyName (should preserve user value)", name)
		}
		if m := got["mandatory"]; m != true {
			t.Errorf("mandatory = %v, want true (should preserve user value)", m)
		}
		if vl := got["versionLock"]; vl != float64(7) {
			t.Errorf("versionLock = %v, want 7 (should take server value)", vl)
		}
	})

	t.Run("invalid user JSON passes through", func(t *testing.T) {
		userData := []byte(`not json`)
		serverResp := []byte(`{"versionLock":5}`)
		out, err := injectVersionLocks(userData, serverResp)
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != "not json" {
			t.Errorf("expected unchanged input, got %s", out)
		}
	})

	t.Run("invalid server JSON returns error", func(t *testing.T) {
		userData := []byte(`{"versionLock":0}`)
		serverResp := []byte(`not json`)
		_, err := injectVersionLocks(userData, serverResp)
		if err == nil {
			t.Error("expected error for invalid server response")
		}
	})
}

func TestMergeVersionLocks(t *testing.T) {
	t.Run("deeply nested merge", func(t *testing.T) {
		dst := map[string]any{
			"versionLock": float64(0),
			"outer": map[string]any{
				"versionLock": float64(0),
				"inner": map[string]any{
					"versionLock": float64(0),
					"data":        "keep",
				},
			},
		}
		src := map[string]any{
			"versionLock": float64(10),
			"outer": map[string]any{
				"versionLock": float64(20),
				"inner": map[string]any{
					"versionLock": float64(30),
					"data":        "ignore",
				},
			},
		}
		mergeVersionLocks(dst, src)
		if dst["versionLock"] != float64(10) {
			t.Errorf("top versionLock = %v, want 10", dst["versionLock"])
		}
		outer := dst["outer"].(map[string]any)
		if outer["versionLock"] != float64(20) {
			t.Errorf("outer.versionLock = %v, want 20", outer["versionLock"])
		}
		inner := outer["inner"].(map[string]any)
		if inner["versionLock"] != float64(30) {
			t.Errorf("outer.inner.versionLock = %v, want 30", inner["versionLock"])
		}
		// Non-versionLock fields untouched
		if inner["data"] != "keep" {
			t.Errorf("outer.inner.data = %v, want keep", inner["data"])
		}
	})

	t.Run("skips non-object values", func(t *testing.T) {
		dst := map[string]any{
			"versionLock": float64(0),
			"name":        "keep",
		}
		src := map[string]any{
			"versionLock": float64(5),
			"name":        "ignore",
		}
		mergeVersionLocks(dst, src)
		if dst["versionLock"] != float64(5) {
			t.Errorf("versionLock = %v, want 5", dst["versionLock"])
		}
		if dst["name"] != "keep" {
			t.Errorf("name = %v, want keep", dst["name"])
		}
	})
}

func TestFilterResultsByName(t *testing.T) {
	results := []json.RawMessage{
		json.RawMessage(`{"id":"1","displayName":"1:1"}`),
		json.RawMessage(`{"id":"3","displayName":"Shared"}`),
		json.RawMessage(`{"id":"4","displayName":"All - Customer - 1:1"}`),
		json.RawMessage(`{"id":"80","displayName":"Test"}`),
		json.RawMessage(`{"id":"94","displayName":"test"}`),
	}

	t.Run("exact match returns single result", func(t *testing.T) {
		got := filterResultsByName(results, "displayName", "Shared")
		if len(got) != 1 {
			t.Fatalf("got %d results, want 1", len(got))
		}
		m := parseJSON(t, got[0])
		if m["id"] != "3" {
			t.Errorf("id = %v, want 3", m["id"])
		}
	})

	t.Run("case sensitive", func(t *testing.T) {
		got := filterResultsByName(results, "displayName", "Test")
		if len(got) != 1 {
			t.Fatalf("got %d results, want 1", len(got))
		}
		m := parseJSON(t, got[0])
		if m["id"] != "80" {
			t.Errorf("id = %v, want 80", m["id"])
		}
	})

	t.Run("no match returns empty", func(t *testing.T) {
		got := filterResultsByName(results, "displayName", "Nonexistent")
		if len(got) != 0 {
			t.Errorf("got %d results, want 0", len(got))
		}
	})

	t.Run("multiple exact matches", func(t *testing.T) {
		dupes := []json.RawMessage{
			json.RawMessage(`{"id":"1","name":"Foo"}`),
			json.RawMessage(`{"id":"2","name":"Bar"}`),
			json.RawMessage(`{"id":"3","name":"Foo"}`),
		}
		got := filterResultsByName(dupes, "name", "Foo")
		if len(got) != 2 {
			t.Fatalf("got %d results, want 2", len(got))
		}
	})

	t.Run("partial match not included", func(t *testing.T) {
		got := filterResultsByName(results, "displayName", "1:1")
		if len(got) != 1 {
			t.Fatalf("got %d results, want 1 (not partial matches)", len(got))
		}
		m := parseJSON(t, got[0])
		if m["id"] != "1" {
			t.Errorf("id = %v, want 1", m["id"])
		}
	})
}

// parseJSON is a test helper that unmarshals JSON and fails the test on error.
func parseJSON(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to parse JSON %q: %v", data, err)
	}
	return m
}
