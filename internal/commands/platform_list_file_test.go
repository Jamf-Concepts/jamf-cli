// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	platformgen "github.com/Jamf-Concepts/jamf-cli/internal/commands/platform/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

const (
	bpA = "11111111-1111-1111-1111-111111111111"
	bpB = "22222222-2222-2222-2222-222222222222"
)

func writePlatformList(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "list.txt")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestGeneratedPlatformGet_ListFile: a UUID line is an ID, a name is resolved
// with one listing of the collection however many names the file holds, the
// records print as one array in file order, and a name nothing matches costs
// exit 7 without hiding the rest.
func TestGeneratedPlatformGet_ListFile(t *testing.T) {
	cliCtx, mux, out := newTestPlatformContext(t)
	var listings atomic.Int32
	mux.HandleFunc("/blueprints/v1/blueprints", func(w http.ResponseWriter, _ *http.Request) {
		listings.Add(1)
		writeJSON(w, map[string]any{"totalCount": 2, "results": []any{
			map[string]any{"id": bpA, "name": "Alpha"},
			map[string]any{"id": bpB, "name": "Beta"},
		}})
	})
	for _, id := range []string{bpA, bpB} {
		mux.HandleFunc("/blueprints/v1/blueprints/"+id, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]any{"id": id})
		})
	}

	cmd := platformgen.NewBlueprintsCmd(cliCtx)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.PersistentFlags().Bool("allow-partial-failure", false, "")
	cmd.SetArgs([]string{"get", "--list-file", writePlatformList(t, "Beta\n"+bpA+"\nGamma\nAlpha\n")})
	err := cmd.Execute()
	if exitcode.CodeFrom(err) != exitcode.PartialFailure {
		t.Fatalf("err = %v, want exit 7 for the unmatched name", err)
	}
	var got []map[string]any
	if jerr := json.Unmarshal(out.rawData, &got); jerr != nil {
		t.Fatalf("output is not a JSON array: %v\n%s", jerr, out.rawData)
	}
	if len(got) != 2 || got[0]["id"] != bpB || got[1]["id"] != bpA {
		t.Errorf("records = %v, want Beta then Alpha (Alpha's repeat dropped)", got)
	}
	if n := listings.Load(); n != 1 {
		t.Errorf("listed the collection %d times, want once for every name", n)
	}
}

// TestGeneratedPlatformDelete_ListFile: --dry-run previews every resolved
// record and sends no DELETE.
func TestGeneratedPlatformDelete_ListFile(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	var deletes atomic.Int32
	mux.HandleFunc("/blueprints/v1/blueprints/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes.Add(1)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	cliCtx.DryRun = true
	cmd := platformgen.NewBlueprintsCmd(cliCtx)
	var stderr strings.Builder
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"delete", "--list-file", writePlatformList(t, bpA+"\n"+bpB+"\n")})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if deletes.Load() != 0 || strings.Count(stderr.String(), "[dry-run] DELETE") != 2 {
		t.Errorf("deletes = %d, stderr = %q; want two previews and nothing sent", deletes.Load(), stderr.String())
	}
}
