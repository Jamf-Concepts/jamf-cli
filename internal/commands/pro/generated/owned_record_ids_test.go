// Copyright 2026, Jamf Software LLC

package generated

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func TestResetOwnedRecordIDs(t *testing.T) {
	t.Run("resets every nested record carrying a versionLock", func(t *testing.T) {
		input := []byte(`{
			"id":"350","displayName":"Shared","versionLock":1,
			"locationInformation":{"id":"385","versionLock":0,"buildingId":"7"},
			"purchasingInformation":{"id":"385","versionLock":0},
			"accountSettings":{"id":"385","versionLock":0}
		}`)
		got := parseJSON(t, resetOwnedRecordIDs(input))
		for _, key := range []string{"locationInformation", "purchasingInformation", "accountSettings"} {
			rec, _ := got[key].(map[string]any)
			if rec["id"] != newOwnedRecordID {
				t.Errorf("%s.id = %v, want %q", key, rec["id"], newOwnedRecordID)
			}
		}
		if got["id"] != "350" {
			t.Errorf("top-level id = %v, want it left alone", got["id"])
		}
		loc, _ := got["locationInformation"].(map[string]any)
		if loc["buildingId"] != "7" {
			t.Errorf("buildingId = %v, want a foreign key left alone", loc["buildingId"])
		}
	})

	t.Run("leaves an id on an object with no versionLock", func(t *testing.T) {
		// Without a versionLock the object is a reference, not an owned row.
		input := []byte(`{"site":{"id":"4","name":"HQ"}}`)
		got := parseJSON(t, resetOwnedRecordIDs(input))
		if site, _ := got["site"].(map[string]any); site["id"] != "4" {
			t.Errorf("site.id = %v, want 4", site["id"])
		}
	})

	t.Run("does not add an id that is absent", func(t *testing.T) {
		input := []byte(`{"accountSettings":{"versionLock":0}}`)
		got := parseJSON(t, resetOwnedRecordIDs(input))
		if acct, _ := got["accountSettings"].(map[string]any); acct["id"] != nil {
			t.Errorf("accountSettings.id = %v, want absent", acct["id"])
		}
	})

	t.Run("reaches records inside arrays", func(t *testing.T) {
		input := []byte(`{"items":[{"id":"9","versionLock":3}]}`)
		got := parseJSON(t, resetOwnedRecordIDs(input))
		items, _ := got["items"].([]any)
		if item, _ := items[0].(map[string]any); item["id"] != newOwnedRecordID {
			t.Errorf("items[0].id = %v, want %q", item["id"], newOwnedRecordID)
		}
	})

	t.Run("returns non-JSON input unchanged", func(t *testing.T) {
		if got := string(resetOwnedRecordIDs([]byte("not json"))); got != "not json" {
			t.Errorf("got %q", got)
		}
	})
}

// postRecordingClient answers every GET with an empty list, so apply takes its
// create branch, and records the body of the POST.
type postRecordingClient struct {
	postBody []byte
}

func (c *postRecordingClient) Do(_ context.Context, method, _ string, body io.Reader) (*http.Response, error) {
	switch method {
	case "GET":
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"totalCount":0,"results":[]}`))}, nil
	case "POST":
		b, err := io.ReadAll(body)
		if err != nil {
			return nil, err
		}
		c.postBody = b
		return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(`{"id":"999"}`))}, nil
	default:
		return nil, io.ErrUnexpectedEOF
	}
}

var _ registry.HTTPClient = (*postRecordingClient)(nil)

// TestPrestageCreateDoesNotReuseNestedRecords drives create and apply on both
// prestage resources with a body copied from a GET, the clone the generated
// help recommends. Sent unchanged, the server binds the nested ids to the
// source's rows and moves them onto the new prestage (#393).
func TestPrestageCreateDoesNotReuseNestedRecords(t *testing.T) {
	computerBody := `{"id":"352","displayName":"Copy","versionLock":1,
		"locationInformation":{"id":"387","versionLock":0},
		"purchasingInformation":{"id":"387","versionLock":0},
		"accountSettings":{"id":"387","versionLock":0}}`
	mobileBody := `{"id":"347","displayName":"Copy","versionLock":1,
		"locationInformation":{"id":"366","versionLock":0},
		"purchasingInformation":{"id":"360","versionLock":0}}`

	cases := []struct {
		name    string
		newCmd  func(*registry.CLIContext) *cobra.Command
		body    string
		records []string
	}{
		{"computer create", newComputerPrestagesCreateCmd, computerBody, []string{"locationInformation", "purchasingInformation", "accountSettings"}},
		{"computer apply", newComputerPrestagesApplyCmd, computerBody, []string{"locationInformation", "purchasingInformation", "accountSettings"}},
		{"mobile create", newMobileDevicePrestagesCreateCmd, mobileBody, []string{"locationInformation", "purchasingInformation"}},
		{"mobile apply", newMobileDevicePrestagesApplyCmd, mobileBody, []string{"locationInformation", "purchasingInformation"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &postRecordingClient{}
			ctx := &registry.CLIContext{Client: mock, Output: newNDJSONOutput()}

			bodyFile := t.TempDir() + "/body.json"
			if err := os.WriteFile(bodyFile, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			in, err := os.Open(bodyFile)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = in.Close() }()
			origStdin := os.Stdin
			os.Stdin = in
			defer func() { os.Stdin = origStdin }()

			cmd := tc.newCmd(ctx)
			cmd.SetArgs([]string{"--yes"})
			if cmd.Flags().Lookup("yes") == nil {
				cmd.SetArgs(nil)
			}
			if err := cmd.Execute(); err != nil {
				t.Fatalf("execute: %v", err)
			}
			if mock.postBody == nil {
				t.Fatal("no POST was sent")
			}
			sent := parseJSON(t, mock.postBody)
			for _, key := range tc.records {
				rec, _ := sent[key].(map[string]any)
				if rec["id"] != newOwnedRecordID {
					t.Errorf("POST %s.id = %v, want %q", key, rec["id"], newOwnedRecordID)
				}
			}
			if sent["versionLock"] != float64(0) {
				t.Errorf("POST versionLock = %v, want 0", sent["versionLock"])
			}
		})
	}
}

func TestInjectVersionLocksPinsTheTargetsOwnRecords(t *testing.T) {
	// The body was copied from prestage 354; the PUT goes to 355.
	body := []byte(`{"id":"354","displayName":"B","versionLock":1,
		"locationInformation":{"id":"388","versionLock":0,"buildingId":"7"},
		"purchasingInformation":{"id":"388","versionLock":0},
		"accountSettings":{"id":"388","versionLock":0}}`)
	target := []byte(`{"id":"355","versionLock":4,
		"locationInformation":{"id":"389","versionLock":2},
		"purchasingInformation":null,
		"accountSettings":{"id":"389","versionLock":1}}`)

	out, err := injectVersionLocks(body, target)
	if err != nil {
		t.Fatal(err)
	}
	got := parseJSON(t, out)
	want := map[string]string{
		"locationInformation":   "389",
		"purchasingInformation": newOwnedRecordID, // the target has no record to keep
		"accountSettings":       "389",
	}
	for key, id := range want {
		rec, _ := got[key].(map[string]any)
		if rec["id"] != id {
			t.Errorf("%s.id = %v, want %q", key, rec["id"], id)
		}
	}
	if loc, _ := got["locationInformation"].(map[string]any); loc["buildingId"] != "7" || loc["versionLock"] != float64(2) {
		t.Errorf("locationInformation = %v, want buildingId kept and versionLock 2", loc)
	}
	if got["id"] != "354" || got["versionLock"] != float64(4) {
		t.Errorf("top level = id %v versionLock %v, want id untouched and versionLock 4", got["id"], got["versionLock"])
	}
}

func TestInjectVersionLocksLeavesAScopeBodyAlone(t *testing.T) {
	body := []byte(`{"serialNumbers":["C02X"],"versionLock":0}`)
	out, err := injectVersionLocks(body, []byte(`{"prestageId":"1","assignments":[],"versionLock":7}`))
	if err != nil {
		t.Fatal(err)
	}
	got := parseJSON(t, out)
	if got["versionLock"] != float64(7) || len(got) != 2 {
		t.Errorf("got %v, want only versionLock updated", got)
	}
}

// TestPrestageUpdateKeepsTheTargetsRecords drives update with a body copied
// from a different prestage. The PUT must name the target's own nested rows.
func TestPrestageUpdateKeepsTheTargetsRecords(t *testing.T) {
	mock := &updateSetRecordingClient{getBody: []byte(`{"id":"355","versionLock":1,
		"locationInformation":{"id":"389","versionLock":0},
		"purchasingInformation":{"id":"389","versionLock":0},
		"accountSettings":{"id":"389","versionLock":0}}`)}
	ctx := &registry.CLIContext{Client: mock, Output: newNDJSONOutput()}

	bodyFile := t.TempDir() + "/body.json"
	copied := `{"id":"354","displayName":"B","versionLock":1,
		"locationInformation":{"id":"388","versionLock":0},
		"purchasingInformation":{"id":"388","versionLock":0},
		"accountSettings":{"id":"388","versionLock":0}}`
	if err := os.WriteFile(bodyFile, []byte(copied), 0o600); err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(bodyFile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close() }()
	origStdin := os.Stdin
	os.Stdin = in
	defer func() { os.Stdin = origStdin }()

	cmd := newComputerPrestagesUpdateCmd(ctx)
	cmd.SetArgs([]string{"355"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	sent := parseJSON(t, mock.putBody)
	for _, key := range []string{"locationInformation", "purchasingInformation", "accountSettings"} {
		if rec, _ := sent[key].(map[string]any); rec["id"] != "389" {
			t.Errorf("PUT %s.id = %v, want the target's 389", key, rec["id"])
		}
	}
}
