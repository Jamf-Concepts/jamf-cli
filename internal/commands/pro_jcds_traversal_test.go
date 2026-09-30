// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// A JCDS file name is server-supplied, so sync must never write outside --dir
// whatever the name holds.
func TestJcdsSyncCmd_ServerFileNameCannotEscapeDir(t *testing.T) {
	const marker = "JCDS-TRAVERSAL-MARKER"
	const original = "ORIGINAL-VICTIM-CONTENT"

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"live", nil},
		{"live-delete", []string{"--delete"}},
		{"dry-run", []string{"--dry-run", "--delete"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			parent := filepath.Join(root, "a", "b")
			dir := filepath.Join(parent, "sync")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			victim := filepath.Join(parent, "escaped.pkg")
			if err := os.WriteFile(victim, []byte(original), 0o644); err != nil {
				t.Fatal(err)
			}

			names := []string{
				"../escaped.pkg",
				"sub/../../escaped2.pkg",
				filepath.Join(root, "abs-escaped.pkg"),
			}

			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/v1/jcds/files":
					files := make([]jcdsFileData, 0, len(names))
					for _, n := range names {
						files = append(files, jcdsFileData{FileName: n})
					}
					_ = json.NewEncoder(w).Encode(files)
				case strings.HasPrefix(r.URL.EscapedPath(), "/v1/jcds/files/"):
					_ = json.NewEncoder(w).Encode(map[string]string{"uri": srv.URL + "/blob"})
				case r.URL.Path == "/blob":
					_, _ = w.Write([]byte(marker))
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer srv.Close()

			ctx := &registry.CLIContext{Client: &jcdsTestClient{srv: srv}, Output: &captureOutput{}}
			cmd := newJcdsSyncCmd(ctx)
			cmd.SetArgs(append([]string{"--dir", dir}, tc.args...))
			_ = cmd.Execute()

			var report jcdsSyncReport
			if err := json.Unmarshal(ctx.Output.(*captureOutput).rawData, &report); err != nil {
				t.Fatalf("decoding sync report: %v", err)
			}
			if report.Summary.Failed != int64(len(names)) || report.Summary.Downloaded != 0 {
				t.Errorf("summary = %+v, want every unsafe name failed and none downloaded", report.Summary)
			}
			for _, r := range report.Files {
				if r.Status != jcdsStatusFailed {
					t.Errorf("%s reported %q, want %q", r.FileName, r.Status, jcdsStatusFailed)
				}
			}

			got, err := os.ReadFile(victim)
			if err != nil {
				t.Errorf("victim outside --dir is gone: %v", err)
			} else if string(got) != original {
				t.Errorf("victim outside --dir was overwritten: %s now holds %q", victim, got)
			}

			_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if p == dir {
					return filepath.SkipDir
				}
				if d.IsDir() || p == victim {
					return nil
				}
				rel, _ := filepath.Rel(root, p)
				t.Errorf("sync created %s outside --dir %s", rel, dir)
				return nil
			})
		})
	}
}
