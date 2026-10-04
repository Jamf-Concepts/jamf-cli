// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func TestPDGPartitionMembers(t *testing.T) {
	current := []string{"AAAA", "bbbb"}
	change, unchanged := pdgPartitionMembers(current, []string{"aaaa", "CCCC", "cccc", " ", "BBBB"}, true)
	if strings.Join(change, ",") != "CCCC" || strings.Join(unchanged, ",") != "aaaa,BBBB" {
		t.Errorf("add: change %v unchanged %v", change, unchanged)
	}
	change, unchanged = pdgPartitionMembers(current, []string{"aaaa", "cccc"}, false)
	if strings.Join(change, ",") != "aaaa" || strings.Join(unchanged, ",") != "cccc" {
		t.Errorf("remove: change %v unchanged %v", change, unchanged)
	}
}

// Both device-group PATCHes go out as the application/json their spec
// declares; the transport's default for a PATCH is merge-patch, which the
// server refuses for every body (jamf/jamfplatform-go-sdk#85).
func TestPDGPatchesSendJSON(t *testing.T) {
	const id = "39198011-5cef-431f-96f6-042d4435ffb3"
	for _, tc := range []struct {
		name string
		cmd  func(*registry.CLIContext) *cobra.Command
		want string
	}{
		{"patch", newPDGPatchCmd, "/device-groups/" + id},
		{"patch-members", newPDGPatchMembersCmd, "/device-groups/" + id + "/members"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cliCtx, mux, _ := newTestPlatformContext(t)
			var gotType, gotPath string
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPatch {
					gotType, gotPath = r.Header.Get("Content-Type"), r.URL.Path
				}
				w.WriteHeader(http.StatusNoContent)
			})
			body := filepath.Join(t.TempDir(), "b.json")
			if err := os.WriteFile(body, []byte(`{"description":"x"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := runCobraCmd(t, tc.cmd(cliCtx), id, "--from-file", body); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotType != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", gotType)
			}
			if !strings.HasSuffix(gotPath, tc.want) {
				t.Errorf("path = %q, want it to end %q", gotPath, tc.want)
			}
		})
	}
}

// remove-members sends every requested ID, the non-member too: removing a
// non-member is a no-op on the wire, but only a sent ID can be refused as
// INVALID_DEVICE, so a typo is not reported as "not a member, left alone".
func TestPDGRemoveMembersSendsEveryRequestedID(t *testing.T) {
	const group = "69db9494-d1dc-486a-9f90-5936c5bfa2a2"
	const member = "11111111-1111-4111-8111-111111111111"
	const other = "22222222-2222-4222-8222-222222222222"
	cliCtx, mux, _ := newTestPlatformContext(t)
	var sent map[string][]string
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPatch:
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &sent)
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/members"):
			_, _ = w.Write([]byte(`{"totalCount":1,"results":["` + member + `"]}`))
		default:
			_, _ = w.Write([]byte(`{"totalCount":1,"results":[{"id":"` + group + `","name":"G"}]}`))
		}
	})
	_, stderr, err := runCobraCmd(t, newPDGRemoveMembersCmd(cliCtx), "G", "--id", member, "--id", other)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := strings.Join(sent["removed"], ","); got != member+","+other {
		t.Errorf("removed = %s, want both requested IDs", got)
	}
	if !strings.Contains(stderr, "Removed 1 device(s)") || !strings.Contains(stderr, "1 not members") {
		t.Errorf("stderr = %q, want the report to count one change and one unchanged", stderr)
	}
}

// A numeric Jamf Pro ID is refused before any request, with a hint naming the
// management ID; the server would answer only "not a valid UUID".
func TestPDGMemberChangeRefusesANonUUID(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	called := false
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { called = true })
	_, _, err := runCobraCmd(t, newPDGRemoveMembersCmd(cliCtx), "G", "--id", "284")
	var ee *exitcode.Error
	if !errors.As(err, &ee) || ee.Code != exitcode.Usage || !strings.Contains(ee.Hint, "management ID") {
		t.Fatalf("err = %v, want a usage error hinting at the management ID", err)
	}
	if called {
		t.Error("a request was sent for an ID that cannot name a Platform device")
	}
}
