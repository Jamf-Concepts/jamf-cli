// Copyright 2026, Jamf Software LLC

package commands

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

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
