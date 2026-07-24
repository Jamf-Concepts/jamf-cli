// Copyright 2026, Jamf Software LLC

package generated

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
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

	setDoc, err := buildMergePatchFromSet([]string{"city=Boston"}, map[string]string{"name": "string", "city": "string"})
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

// TestHasNestedKey covers the dot-notation presence check used to decide whether
// a write-only field was supplied via --set before warning that update would blank it.
func TestHasNestedKey(t *testing.T) {
	m := map[string]any{
		"department":      "IT",
		"accountSettings": map[string]any{"adminPassword": "x"},
	}
	cases := map[string]bool{
		"department":                    true,
		"accountSettings.adminPassword": true,
		"accountSettings.adminUsername": false, // sibling not set
		"missing":                       false,
		"department.child":              false, // scalar has no children
		"accountSettings":               true,  // the object itself is present
	}
	for path, want := range cases {
		if got := hasNestedKey(m, path); got != want {
			t.Errorf("hasNestedKey(%q) = %v, want %v", path, got, want)
		}
	}
}

// captureStderr redirects os.Stderr for the duration of fn and returns what was written.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = orig }()
	fn()
	_ = w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	return string(out)
}

// TestUpdateSetWriteOnlyWarning drives "computer-prestages update <id> --set ..."
// and asserts the write-only warning fires for a nested password field that the
// caller did not supply (regression for issue #302: a bare metadata change would
// silently blank accountSettings.adminPassword), and is suppressed once the caller
// supplies that field.
func TestUpdateSetWriteOnlyWarning(t *testing.T) {
	// GET response never contains adminPassword — it is write-only server-side.
	getBody := []byte(`{"id":"12","displayName":"Test","department":"Old","accountSettings":{"adminUsername":"admin","versionLock":1},"versionLock":3}`)

	t.Run("warns when write-only password omitted", func(t *testing.T) {
		mock := &updateSetRecordingClient{getBody: getBody}
		ctx := &registry.CLIContext{Client: mock, Output: newNDJSONOutput()}
		cmd := newComputerPrestagesUpdateCmd(ctx)
		cmd.SetArgs([]string{"12", "--set", `department=Config Management`})
		stderr := captureStderr(t, func() {
			if err := cmd.Execute(); err != nil {
				t.Fatalf("update --set failed: %v", err)
			}
		})
		if !strings.Contains(stderr, `field "accountSettings.adminPassword" is write-only`) {
			t.Errorf("expected write-only warning for accountSettings.adminPassword, got: %q", stderr)
		}
	})

	t.Run("no warning when write-only password supplied", func(t *testing.T) {
		mock := &updateSetRecordingClient{getBody: getBody}
		ctx := &registry.CLIContext{Client: mock, Output: newNDJSONOutput()}
		cmd := newComputerPrestagesUpdateCmd(ctx)
		cmd.SetArgs([]string{"12", "--set", `department=Config Management`, "--set", "accountSettings.adminPassword=hunter2"})
		stderr := captureStderr(t, func() {
			if err := cmd.Execute(); err != nil {
				t.Fatalf("update --set failed: %v", err)
			}
		})
		if strings.Contains(stderr, `field "accountSettings.adminPassword" is write-only`) {
			t.Errorf("did not expect adminPassword warning when supplied, got: %q", stderr)
		}
		// The supplied password must actually reach the PUT body.
		var putBody map[string]any
		if err := json.Unmarshal(mock.putBody, &putBody); err != nil {
			t.Fatalf("PUT body not valid JSON: %v (%s)", err, mock.putBody)
		}
		acct, _ := putBody["accountSettings"].(map[string]any)
		if acct == nil || acct["adminPassword"] != "hunter2" {
			t.Errorf("PUT body missing supplied adminPassword: %v", putBody)
		}
	})
}

// updateSetRecordingClient records every call and returns a canned GET body,
// capturing whatever body the command PUTs back.
type updateSetRecordingClient struct {
	getBody []byte
	calls   []string
	putBody []byte
}

func (c *updateSetRecordingClient) Do(_ context.Context, method, path string, body io.Reader) (*http.Response, error) {
	c.calls = append(c.calls, method+" "+path)
	switch method {
	case "GET":
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(c.getBody)))}, nil
	case "PUT":
		if body != nil {
			b, err := io.ReadAll(body)
			if err != nil {
				return nil, err
			}
			c.putBody = b
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	default:
		return nil, io.ErrUnexpectedEOF
	}
}

var _ registry.HTTPClient = (*updateSetRecordingClient)(nil)

// TestGeneratedUpdateSetCommand drives "categories update <id> --set ..." through
// its real RunE against a fake HTTPClient, verifying: the GET and PUT hit the
// same resolved path, read-only fields ("id", "href") are stripped from the
// fetched body before the PUT, the --set change is merged in, and stdin is
// never consumed once --set is present (even when data is waiting on it).
func TestGeneratedUpdateSetCommand(t *testing.T) {
	mock := &updateSetRecordingClient{getBody: []byte(`{"id":"12","name":"HQ","priority":1,"href":"/x"}`)}
	ctx := &registry.CLIContext{
		Client: mock,
		Output: newNDJSONOutput(),
	}

	// Stdin has data waiting on it but is never closed; if the --set path ever
	// regresses to also reading stdin, io.ReadAll blocks and the test hangs
	// instead of silently passing.
	origStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	defer func() {
		os.Stdin = origStdin
		_ = r.Close()
		_ = w.Close()
	}()
	if _, err := w.Write([]byte(`{"name":"from stdin, should be ignored"}`)); err != nil {
		t.Fatal(err)
	}

	cmd := newCategoriesUpdateCmd(ctx)
	cmd.SetArgs([]string{"12", "--set", "priority=9"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("update --set failed: %v", err)
	}

	wantCalls := []string{"GET /v1/categories/12", "PUT /v1/categories/12"}
	if !reflect.DeepEqual(mock.calls, wantCalls) {
		t.Errorf("calls = %v, want %v (GET and PUT must hit the same resolved path)", mock.calls, wantCalls)
	}

	var putBody map[string]any
	if err := json.Unmarshal(mock.putBody, &putBody); err != nil {
		t.Fatalf("PUT body is not valid JSON: %v (%s)", err, mock.putBody)
	}
	want := map[string]any{"name": "HQ", "priority": float64(9)}
	if !reflect.DeepEqual(putBody, want) {
		t.Errorf("PUT body = %v, want %v (id/href must be stripped, priority merged, stdin ignored)", putBody, want)
	}
}
