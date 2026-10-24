// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// A policy's account maintenance carries a local account password, which the
// Classic API returns in full.
func credentialPolicyMock() *backupMockClient {
	return &backupMockClient{responses: map[string]overviewMockResponse{
		"/JSSResource/policies":      {200, `{"policies":[{"id":1,"name":"Create Admin"}]}`},
		"/JSSResource/policies/id/1": {200, `{"policy":{"general":{"id":1,"name":"Create Admin"},"account_maintenance":{"accounts":[{"username":"admin","password":"hunter2"}]}}}`},
	}}
}

func plainPolicyMock() *backupMockClient {
	return &backupMockClient{responses: map[string]overviewMockResponse{
		"/JSSResource/policies":      {200, `{"policies":[{"id":1,"name":"Deploy Chrome"}]}`},
		"/JSSResource/policies/id/1": {200, `{"policy":{"general":{"id":1,"name":"Deploy Chrome","enabled":true}}}`},
	}}
}

func runPolicyBackup(t *testing.T, mock *backupMockClient, outDir string) string {
	t.Helper()
	var runErr error
	stderr := captureStderr(t, func() {
		runErr = runBackup(context.Background(), &registry.CLIContext{Client: mock}, backupOptions{
			OutputDir:   outDir,
			Format:      "yaml",
			Resources:   "policies",
			Concurrency: 1,
		})
	})
	if runErr != nil {
		t.Fatalf("runBackup: %v", runErr)
	}
	return stderr
}

func assertPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != want {
		t.Errorf("%s: mode = %#o, want %#o", filepath.Base(path), perm, want)
	}
}

func skipModeTestOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports no Unix permission bits and needs privilege to create a symlink")
	}
}

// A backup holds credentials in cleartext, so no file or directory it creates
// is readable by another local user.
func TestBackup_FilesAre0600AndDirectories0700(t *testing.T) {
	skipModeTestOnWindows(t)
	outDir := filepath.Join(t.TempDir(), "backup")
	runPolicyBackup(t, credentialPolicyMock(), outDir)

	assertPerm(t, outDir, 0o700)
	assertPerm(t, filepath.Join(outDir, "policies"), 0o700)
	assertPerm(t, filepath.Join(outDir, "policies", "create-admin.yaml"), 0o600)
}

func TestBackup_PreExisting0644FileEndsUp0600(t *testing.T) {
	skipModeTestOnWindows(t)
	outDir := t.TempDir()
	dest := filepath.Join(outDir, "policies", "create-admin.yaml")
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	runPolicyBackup(t, credentialPolicyMock(), outDir)

	assertPerm(t, dest, 0o600)
	body, _ := os.ReadFile(dest)
	if !strings.Contains(string(body), "hunter2") {
		t.Errorf("destination was not rewritten: %q", body)
	}
}

func TestBackup_ReplacesASymlinkAtTheDestination(t *testing.T) {
	skipModeTestOnWindows(t)
	outDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "world-readable")
	if err := os.WriteFile(target, []byte("untouched"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(outDir, "policies", "create-admin.yaml")
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, dest); err != nil {
		t.Fatal(err)
	}

	runPolicyBackup(t, credentialPolicyMock(), outDir)

	info, err := os.Lstat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("destination is still a symlink")
	}
	got, _ := os.ReadFile(target)
	if string(got) != "untouched" {
		t.Errorf("backup wrote through the symlink into its target: %q", got)
	}
	assertPerm(t, dest, 0o600)
}

func TestBackupInventoryPreloadCSV_Is0600(t *testing.T) {
	skipModeTestOnWindows(t)
	mock := &backupMockClient{responses: map[string]overviewMockResponse{
		"/v2/inventory-preload/csv": {200, "Serial Number\nC02XG0XXJHX2\n"},
	}}
	outDir := t.TempDir()
	if _, failures := backupInventoryPreloadCSV(context.Background(), mock, backupOptions{OutputDir: outDir}); len(failures) != 0 {
		t.Fatalf("failures: %v", failures)
	}
	assertPerm(t, filepath.Join(outDir, "inventory-preloads"), 0o700)
	assertPerm(t, filepath.Join(outDir, "inventory-preloads", "inventory-preload-all.csv"), 0o600)
}

func TestBackup_WarnsWhenTheBackupCarriesCredentials(t *testing.T) {
	outDir := t.TempDir()
	stderr := runPolicyBackup(t, credentialPolicyMock(), outDir)

	if !strings.Contains(stderr, "WARNING: 1 resource(s) carry credentials in cleartext and were written 0600:") {
		t.Errorf("no credential warning in stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "  policies\n") {
		t.Errorf("warning does not name the resource:\n%s", stderr)
	}
	if !strings.Contains(stderr, outDir) {
		t.Errorf("warning does not name the backup directory:\n%s", stderr)
	}
}

func TestBackup_NoCredentialWarningWithoutCredentials(t *testing.T) {
	stderr := runPolicyBackup(t, plainPolicyMock(), t.TempDir())
	if strings.Contains(stderr, "carry credentials") {
		t.Errorf("warned about credentials on a backup that holds none:\n%s", stderr)
	}
}

func TestBackupCarriesCredential(t *testing.T) {
	profile := `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict>` +
		`<key>PayloadContent</key><array><dict><key>Password</key><string>wifi-pass</string></dict></array>` +
		`</dict></plist>`
	plainProfile := `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict>` +
		`<key>PayloadDisplayName</key><string>Dock</string></dict></plist>`

	cases := []struct {
		name string
		obj  map[string]any
		want bool
	}{
		{"nested password", map[string]any{"a": []any{map[string]any{"password": "x"}}}, true},
		{"empty password is not a credential", map[string]any{"password": ""}, false},
		{"boolean switch named after a password", map[string]any{"username_password_required": true}, false},
		{"profile payload secret", map[string]any{"general": map[string]any{"payloads": profile}}, true},
		{"profile payload without a secret", map[string]any{"general": map[string]any{"payloads": plainProfile}}, false},
		{"nothing", map[string]any{"name": "Deploy Chrome"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := backupCarriesCredential(tc.obj); got != tc.want {
				t.Errorf("backupCarriesCredential = %v, want %v", got, tc.want)
			}
		})
	}
}
