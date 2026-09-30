// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func TestBackup_DownloadPackages_ServerFilenameCannotEscapeFilesDir(t *testing.T) {
	marker := []byte("ATTACKER-CONTROLLED-BYTES")

	cases := []struct {
		name     string
		filename string
	}{
		{"two parent segments", "../../escaped.pkg"},
		{"four parent segments", "../../../../escaped.pkg"},
		{"absolute path", "/abs-escaped.pkg"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write(marker)
			}))
			defer fileSrv.Close()

			fnJSON, _ := json.Marshal(tc.filename)
			mock := &backupMockClient{
				responses: map[string]overviewMockResponse{
					"/JSSResource/packages":                         {200, `{"packages":[{"id":1,"name":"Pkg"}]}`},
					"/JSSResource/packages/id/1":                    {200, fmt.Sprintf(`{"package":{"id":1,"name":"Pkg","filename":%s}}`, fnJSON)},
					"/v1/jcds/files":                                {200, fmt.Sprintf(`[{"fileName":%s,"length":25,"md5":"x","region":"us","sha3":"y"}]`, fnJSON)},
					"/v1/jcds/files/" + url.PathEscape(tc.filename): {200, fmt.Sprintf(`{"uri":%q}`, fileSrv.URL+"/payload")},
				},
			}

			oldURL := serverURL
			serverURL = "https://test.jamfcloud.com"
			defer func() { serverURL = oldURL }()

			root := t.TempDir()
			outDir := filepath.Join(root, "a", "b", "c")
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				t.Fatal(err)
			}
			filesDir := filepath.Join(outDir, "packages", "files")

			err := runBackup(context.Background(), &registry.CLIContext{Client: mock}, backupOptions{
				OutputDir:        outDir,
				Format:           "yaml",
				Resources:        "packages",
				Concurrency:      1,
				DownloadPackages: true,
			})
			if err == nil {
				t.Errorf("backup reported success for refused server filename %q", tc.filename)
			}

			var escaped []string
			_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil || d.IsDir() {
					return nil
				}
				b, readErr := os.ReadFile(p)
				if readErr != nil || string(b) != string(marker) {
					return nil
				}
				if !strings.HasPrefix(p, filesDir+string(filepath.Separator)) {
					escaped = append(escaped, p)
				}
				return nil
			})
			if len(escaped) > 0 {
				t.Fatalf("server filename %q wrote attacker bytes outside %s:\n  %s", tc.filename, filesDir, strings.Join(escaped, "\n  "))
			}
		})
	}
}
