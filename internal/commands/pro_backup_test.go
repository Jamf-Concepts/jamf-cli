// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"gopkg.in/yaml.v3"
)

// backupMockClient returns canned responses for backup tests.
type backupMockClient struct {
	responses map[string]overviewMockResponse
}

func (m *backupMockClient) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	// Try exact match first
	if resp, ok := m.responses[path]; ok {
		return &http.Response{
			StatusCode: resp.statusCode,
			Body:       io.NopCloser(strings.NewReader(resp.body)),
			Header:     make(http.Header),
		}, nil
	}
	// Try without query params
	if before, _, ok := strings.Cut(path, "?"); ok {
		base := before
		if resp, ok := m.responses[base]; ok {
			return &http.Response{
				StatusCode: resp.statusCode,
				Body:       io.NopCloser(strings.NewReader(resp.body)),
				Header:     make(http.Header),
			}, nil
		}
	}
	return nil, fmt.Errorf("no mock for %s %s", method, path)
}

func TestBackup_DirectoryStructure(t *testing.T) {
	mock := &backupMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/policies":      {200, `{"policies":[{"id":1,"name":"Deploy Chrome"},{"id":2,"name":"Install Rosetta"}]}`},
			"/JSSResource/policies/id/1": {200, `{"policy":{"general":{"id":1,"name":"Deploy Chrome","enabled":true},"scope":{"all_computers":true}}}`},
			"/JSSResource/policies/id/2": {200, `{"policy":{"general":{"id":2,"name":"Install Rosetta","enabled":false},"scope":{}}}`},
		},
	}

	oldURL := serverURL
	serverURL = "https://test.jamfcloud.com"
	defer func() { serverURL = oldURL }()

	outDir := t.TempDir()
	cliCtx := &registry.CLIContext{Client: mock}

	err := runBackup(context.Background(), cliCtx, backupOptions{
		OutputDir:   outDir,
		Format:      "yaml",
		Resources:   "policies",
		IncludeIDs:  false,
		Concurrency: 2,
	})
	if err != nil {
		t.Fatalf("runBackup error: %v", err)
	}

	// Verify directory exists
	policyDir := filepath.Join(outDir, "policies")
	if _, err := os.Stat(policyDir); os.IsNotExist(err) {
		t.Fatal("policies directory not created")
		return
	}

	// Verify files exist with slugified names
	entries, err := os.ReadDir(policyDir)
	if err != nil {
		t.Fatalf("reading directory: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 policy files, got %d", len(entries))
	}

	// Check file content
	chromePath := filepath.Join(policyDir, "deploy-chrome.yaml")
	content, err := os.ReadFile(chromePath)
	if err != nil {
		t.Fatalf("reading backup file: %v", err)
	}

	var parsed map[string]any
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("parsing YAML: %v", err)
	}

	// Should have _meta
	if _, ok := parsed["_meta"]; !ok {
		t.Error("backup file missing _meta block")
	}

	// id should be stripped (IncludeIDs=false)
	if _, ok := parsed["id"]; ok {
		t.Error("id should be stripped when IncludeIDs=false")
	}
}

func TestBackup_JSONFormat(t *testing.T) {
	mock := &backupMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/categories":   {200, `{"totalCount":1,"results":[{"id":"1","name":"Productivity"}]}`},
			"/v1/categories/1": {200, `{"id":"1","name":"Productivity","priority":5}`},
		},
	}

	oldURL := serverURL
	serverURL = "https://test.jamfcloud.com"
	defer func() { serverURL = oldURL }()

	outDir := t.TempDir()
	cliCtx := &registry.CLIContext{Client: mock}

	err := runBackup(context.Background(), cliCtx, backupOptions{
		OutputDir:   outDir,
		Format:      "json",
		Resources:   "categories",
		IncludeIDs:  true,
		Concurrency: 2,
	})
	if err != nil {
		t.Fatalf("runBackup error: %v", err)
	}

	catPath := filepath.Join(outDir, "categories", "productivity.json")
	content, err := os.ReadFile(catPath)
	if err != nil {
		t.Fatalf("reading JSON file: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("parsing JSON: %v", err)
	}

	// id should be present (IncludeIDs=true)
	if _, ok := parsed["id"]; !ok {
		t.Error("id should be present when IncludeIDs=true")
	}
}

func TestBackup_PartialFailure(t *testing.T) {
	mock := &backupMockClient{
		responses: map[string]overviewMockResponse{
			// Policies: list succeeds, one detail fails
			"/JSSResource/policies":      {200, `{"policies":[{"id":1,"name":"Good"},{"id":2,"name":"Bad"}]}`},
			"/JSSResource/policies/id/1": {200, `{"policy":{"general":{"id":1,"name":"Good"}}}`},
			"/JSSResource/policies/id/2": {403, `{"httpStatus":403}`},
		},
	}

	oldURL := serverURL
	serverURL = "https://test.jamfcloud.com"
	defer func() { serverURL = oldURL }()

	outDir := t.TempDir()
	cliCtx := &registry.CLIContext{Client: mock}

	err := runBackup(context.Background(), cliCtx, backupOptions{
		OutputDir:   outDir,
		Format:      "yaml",
		Resources:   "policies",
		Concurrency: 2,
	})
	// Partial failure: some exported, some failed -> exit code 7.
	if err == nil {
		t.Fatal("runBackup should return error when failures exist")
		return
	}
	if got := exitcode.CodeFrom(err); got != exitcode.PartialFailure {
		t.Fatalf("exit code = %d, want PartialFailure(%d)", got, exitcode.PartialFailure)
	}

	// Good policy should be exported
	if _, err := os.Stat(filepath.Join(outDir, "policies", "good.yaml")); os.IsNotExist(err) {
		t.Error("good.yaml should exist")
	}

	// Failures manifest should exist
	failPath := filepath.Join(outDir, "_failures.yaml")
	if _, err := os.Stat(failPath); os.IsNotExist(err) {
		t.Error("_failures.yaml should exist")
	}
}

func TestBackup_ResourceFilter(t *testing.T) {
	// Only policies mock — scripts should not be attempted
	mock := &backupMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/policies":      {200, `{"policies":[{"id":1,"name":"Test"}]}`},
			"/JSSResource/policies/id/1": {200, `{"policy":{"general":{"name":"Test"}}}`},
		},
	}

	oldURL := serverURL
	serverURL = "https://test.jamfcloud.com"
	defer func() { serverURL = oldURL }()

	outDir := t.TempDir()
	cliCtx := &registry.CLIContext{Client: mock}

	err := runBackup(context.Background(), cliCtx, backupOptions{
		OutputDir:   outDir,
		Format:      "yaml",
		Resources:   "policies",
		Concurrency: 2,
	})
	if err != nil {
		t.Fatalf("runBackup error: %v", err)
	}

	// Scripts directory should NOT exist (not requested)
	if _, err := os.Stat(filepath.Join(outDir, "scripts")); !os.IsNotExist(err) {
		t.Error("scripts directory should not be created when filtering to policies only")
	}
}

func TestBackup_DuplicateNames(t *testing.T) {
	mock := &backupMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/policies":      {200, `{"policies":[{"id":1,"name":"Test"},{"id":2,"name":"Test"}]}`},
			"/JSSResource/policies/id/1": {200, `{"policy":{"general":{"name":"Test","enabled":true}}}`},
			"/JSSResource/policies/id/2": {200, `{"policy":{"general":{"name":"Test","enabled":false}}}`},
		},
	}

	oldURL := serverURL
	serverURL = "https://test.jamfcloud.com"
	defer func() { serverURL = oldURL }()

	outDir := t.TempDir()
	cliCtx := &registry.CLIContext{Client: mock}

	err := runBackup(context.Background(), cliCtx, backupOptions{
		OutputDir:   outDir,
		Format:      "yaml",
		Resources:   "policies",
		Concurrency: 2,
	})
	if err != nil {
		t.Fatalf("runBackup error: %v", err)
	}

	entries, _ := os.ReadDir(filepath.Join(outDir, "policies"))
	if len(entries) != 2 {
		t.Errorf("expected 2 files for duplicate names, got %d", len(entries))
	}

	// Should have test.yaml and test-2.yaml
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name()] = true
	}
	if !names["test.yaml"] {
		t.Error("missing test.yaml")
	}
	if !names["test-2.yaml"] {
		t.Error("missing test-2.yaml (deduplicated)")
	}
}

func TestClampConcurrency(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, backupDefaultConcurrency},
		{-1, backupDefaultConcurrency},
		{1, 1},
		{3, 3},
		{5, 5},
		{10, backupMaxConcurrency},
		{11, backupMaxConcurrency},
		{50, backupMaxConcurrency},
	}
	for _, c := range cases {
		if got := clampConcurrency(c.in); got != c.want {
			t.Errorf("clampConcurrency(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestBackup_AccountsSubset(t *testing.T) {
	// Classic /JSSResource/accounts returns users + groups combined; the
	// subset-aware lister must fan each out into its own subdirectory and
	// fetch details via the user/group-specific endpoints.
	combinedAccountsXML := `<?xml version="1.0" encoding="UTF-8"?>
<accounts>
  <users>
    <user><id>1</id><name>alice</name></user>
    <user><id>2</id><name>bob</name></user>
  </users>
  <groups>
    <group><id>10</id><name>admins</name></group>
  </groups>
</accounts>`

	mock := &backupMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/accounts":            {200, combinedAccountsXML},
			"/JSSResource/accounts/userid/1":   {200, `<user><id>1</id><name>alice</name><email>alice@example.com</email></user>`},
			"/JSSResource/accounts/userid/2":   {200, `<user><id>2</id><name>bob</name><email>bob@example.com</email></user>`},
			"/JSSResource/accounts/groupid/10": {200, `<group><id>10</id><name>admins</name><privileges><jss_objects/></privileges></group>`},
		},
	}

	oldURL := serverURL
	serverURL = "https://test.jamfcloud.com"
	defer func() { serverURL = oldURL }()

	outDir := t.TempDir()
	cliCtx := &registry.CLIContext{Client: mock}

	if err := runBackup(context.Background(), cliCtx, backupOptions{
		OutputDir:   outDir,
		Format:      "yaml",
		Resources:   "accounts",
		Concurrency: 2,
	}); err != nil {
		t.Fatalf("runBackup error: %v", err)
	}

	// Two users, one group, in separate subdirs.
	wantFiles := []string{
		filepath.Join(outDir, "accounts", "users", "alice.yaml"),
		filepath.Join(outDir, "accounts", "users", "bob.yaml"),
		filepath.Join(outDir, "accounts", "groups", "admins.yaml"),
	}
	for _, path := range wantFiles {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected backup file %s: %v", path, err)
		}
	}
}

func TestBackup_SitesListOnly(t *testing.T) {
	// Sites have no per-ID detail endpoint — the list response is already the
	// complete record. ListOnly mode must write each list item directly
	// without attempting a per-ID GET (which would 404).
	mock := &backupMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/sites": {200, `[{"id":"1","name":"HQ"},{"id":"2","name":"Remote Office"}]`},
		},
	}

	oldURL := serverURL
	serverURL = "https://test.jamfcloud.com"
	defer func() { serverURL = oldURL }()

	outDir := t.TempDir()
	cliCtx := &registry.CLIContext{Client: mock}

	if err := runBackup(context.Background(), cliCtx, backupOptions{
		OutputDir:   outDir,
		Format:      "yaml",
		Resources:   "sites",
		Concurrency: 2,
	}); err != nil {
		t.Fatalf("runBackup error: %v", err)
	}

	for _, name := range []string{"hq.yaml", "remote-office.yaml"} {
		if _, err := os.Stat(filepath.Join(outDir, "sites", name)); err != nil {
			t.Errorf("expected %s: %v", name, err)
		}
	}
}

func TestExtractID(t *testing.T) {
	tests := []struct {
		input map[string]any
		want  string
	}{
		{map[string]any{"id": "42"}, "42"},
		{map[string]any{"id": float64(42)}, "42"},
		{map[string]any{"id": nil}, ""},
		{map[string]any{}, ""},
	}
	for _, tt := range tests {
		got := extractID(tt.input)
		if got != tt.want {
			t.Errorf("extractID(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBackupInventoryPreloadCSV_Success(t *testing.T) {
	csvBody := "Serial Number,Asset Tag\nC02XG0XXJHX2,ASSET001\nC02YH1ZZJHX3,ASSET002\n"
	mock := &backupMockClient{
		responses: map[string]overviewMockResponse{
			"/v2/inventory-preload/csv": {200, csvBody},
		},
	}

	outDir := t.TempDir()
	n, failures := backupInventoryPreloadCSV(context.Background(), mock, backupOptions{OutputDir: outDir})

	if len(failures) != 0 {
		t.Fatalf("expected no failures, got %v", failures)
	}
	if n != 1 {
		t.Errorf("expected 1 exported, got %d", n)
	}

	outPath := filepath.Join(outDir, "inventory-preloads", "inventory-preload-all.csv")
	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("expected CSV file at %s: %v", outPath, err)
	}
	if string(content) != csvBody {
		t.Errorf("CSV content mismatch: got %q, want %q", string(content), csvBody)
	}
}

func TestBackupInventoryPreloadCSV_HTTPError(t *testing.T) {
	mock := &backupMockClient{
		responses: map[string]overviewMockResponse{
			"/v2/inventory-preload/csv": {403, `{"httpStatus":403,"errors":[]}`},
		},
	}

	outDir := t.TempDir()
	n, failures := backupInventoryPreloadCSV(context.Background(), mock, backupOptions{OutputDir: outDir})

	if n != 0 {
		t.Errorf("expected 0 exported on error, got %d", n)
	}
	if len(failures) == 0 {
		t.Fatal("expected failures on HTTP 403")
	}
	if failures[0].Resource != "inventory-preloads" {
		t.Errorf("failure resource = %q, want %q", failures[0].Resource, "inventory-preloads")
	}
	if !strings.Contains(failures[0].Error, "403") {
		t.Errorf("failure error should mention 403, got %q", failures[0].Error)
	}
}

func TestBackup_InventoryPreloadFilter(t *testing.T) {
	csvBody := "Serial Number\nABC123\n"
	mock := &backupMockClient{
		responses: map[string]overviewMockResponse{
			"/v2/inventory-preload/csv": {200, csvBody},
		},
	}

	oldURL := serverURL
	serverURL = "https://test.jamfcloud.com"
	defer func() { serverURL = oldURL }()

	outDir := t.TempDir()
	cliCtx := &registry.CLIContext{Client: mock}

	err := runBackup(context.Background(), cliCtx, backupOptions{
		OutputDir:   outDir,
		Format:      "yaml",
		Resources:   "inventory-preloads",
		Concurrency: 2,
	})
	if err != nil {
		t.Fatalf("runBackup error: %v", err)
	}

	outPath := filepath.Join(outDir, "inventory-preloads", "inventory-preload-all.csv")
	if _, err := os.Stat(outPath); os.IsNotExist(err) {
		t.Error("inventory-preload-all.csv should exist")
	}
}

func TestBackup_DownloadPackages(t *testing.T) {
	fileContent := []byte("fake package binary")
	fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fileContent)
	}))
	defer fileSrv.Close()

	mock := &backupMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/packages":      {200, `{"packages":[{"id":1,"name":"Pkg"}]}`},
			"/JSSResource/packages/id/1": {200, `{"package":{"id":1,"name":"Pkg","filename":"pkg-a.pkg"}}`},
			"/v1/jcds/files":             {200, `[{"fileName":"pkg-a.pkg","length":20,"md5":"x","region":"us","sha3":"y"}]`},
			"/v1/jcds/files/pkg-a.pkg":   {200, fmt.Sprintf(`{"uri":%q}`, fileSrv.URL+"/pkg-a.pkg")},
		},
	}

	oldURL := serverURL
	serverURL = "https://test.jamfcloud.com"
	defer func() { serverURL = oldURL }()

	outDir := t.TempDir()
	cliCtx := &registry.CLIContext{Client: mock}

	err := runBackup(context.Background(), cliCtx, backupOptions{
		OutputDir:        outDir,
		Format:           "yaml",
		Resources:        "packages",
		Concurrency:      2,
		DownloadPackages: true,
	})
	if err != nil {
		t.Fatalf("runBackup error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(outDir, "packages", "files", "pkg-a.pkg"))
	if err != nil {
		t.Fatalf("reading downloaded package: %v", err)
	}
	if string(got) != string(fileContent) {
		t.Errorf("content mismatch: got %q, want %q", got, fileContent)
	}
}

func TestBackup_DownloadPackages_SkipsNonJCDS(t *testing.T) {
	mock := &backupMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/packages":      {200, `{"packages":[{"id":1,"name":"Pkg"}]}`},
			"/JSSResource/packages/id/1": {200, `{"package":{"id":1,"name":"Pkg","filename":"onprem.pkg"}}`},
			"/v1/jcds/files":             {200, `[]`},
		},
	}

	oldURL := serverURL
	serverURL = "https://test.jamfcloud.com"
	defer func() { serverURL = oldURL }()

	outDir := t.TempDir()
	cliCtx := &registry.CLIContext{Client: mock}

	err := runBackup(context.Background(), cliCtx, backupOptions{
		OutputDir:        outDir,
		Format:           "yaml",
		Resources:        "packages",
		Concurrency:      2,
		DownloadPackages: true,
	})
	if err != nil {
		t.Fatalf("runBackup error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outDir, "packages", "files", "onprem.pkg")); !os.IsNotExist(err) {
		t.Error("onprem.pkg should not have been downloaded — it has no JCDS match")
	}

	// Metadata for the package must still have been written even though the
	// binary download was skipped.
	if _, err := os.Stat(filepath.Join(outDir, "packages", "pkg.yaml")); err != nil {
		t.Errorf("package metadata file should still exist: %v", err)
	}
}

func TestBackup_DownloadPackages_DownloadFailure(t *testing.T) {
	mock := &backupMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/packages":      {200, `{"packages":[{"id":1,"name":"Pkg"}]}`},
			"/JSSResource/packages/id/1": {200, `{"package":{"id":1,"name":"Pkg","filename":"pkg-a.pkg"}}`},
			"/v1/jcds/files":             {200, `[{"fileName":"pkg-a.pkg","length":20,"md5":"x","region":"us","sha3":"y"}]`},
			// Pre-signed URL fetch fails — download should be recorded as a
			// failure, not crash the backup.
			"/v1/jcds/files/pkg-a.pkg": {500, `{"error":"server error"}`},
		},
	}

	oldURL := serverURL
	serverURL = "https://test.jamfcloud.com"
	defer func() { serverURL = oldURL }()

	outDir := t.TempDir()
	cliCtx := &registry.CLIContext{Client: mock}

	err := runBackup(context.Background(), cliCtx, backupOptions{
		OutputDir:        outDir,
		Format:           "yaml",
		Resources:        "packages",
		Concurrency:      2,
		DownloadPackages: true,
	})
	if err == nil {
		t.Fatal("runBackup should return error when a package download fails")
	}
	if got := exitcode.CodeFrom(err); got != exitcode.PartialFailure {
		t.Fatalf("exit code = %d, want PartialFailure(%d)", got, exitcode.PartialFailure)
	}

	// Metadata must still be written even though the binary download failed.
	if _, err := os.Stat(filepath.Join(outDir, "packages", "pkg.yaml")); err != nil {
		t.Errorf("package metadata file should still exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "packages", "files", "pkg-a.pkg")); !os.IsNotExist(err) {
		t.Error("pkg-a.pkg should not exist on disk after a failed download")
	}
	if _, err := os.Stat(filepath.Join(outDir, "_failures.yaml")); err != nil {
		t.Errorf("_failures.yaml should exist: %v", err)
	}
}

func TestBackup_DownloadPackages_ResourceFilteredOut(t *testing.T) {
	// --resources excludes "packages" entirely; --download-packages must not
	// touch /v1/jcds/files at all (no mock registered for it — a call would
	// surface as a failure and fail this test).
	mock := &backupMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/policies":      {200, `{"policies":[{"id":1,"name":"Good"}]}`},
			"/JSSResource/policies/id/1": {200, `{"policy":{"general":{"id":1,"name":"Good"}}}`},
		},
	}

	oldURL := serverURL
	serverURL = "https://test.jamfcloud.com"
	defer func() { serverURL = oldURL }()

	outDir := t.TempDir()
	cliCtx := &registry.CLIContext{Client: mock}

	err := runBackup(context.Background(), cliCtx, backupOptions{
		OutputDir:        outDir,
		Format:           "yaml",
		Resources:        "policies",
		Concurrency:      2,
		DownloadPackages: true,
	})
	if err != nil {
		t.Fatalf("runBackup error: %v", err)
	}
}

func TestBackup_NoDownloadPackagesByDefault(t *testing.T) {
	mock := &backupMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/packages":      {200, `{"packages":[{"id":1,"name":"Pkg"}]}`},
			"/JSSResource/packages/id/1": {200, `{"package":{"id":1,"name":"Pkg","filename":"pkg-a.pkg"}}`},
		},
	}

	oldURL := serverURL
	serverURL = "https://test.jamfcloud.com"
	defer func() { serverURL = oldURL }()

	outDir := t.TempDir()
	cliCtx := &registry.CLIContext{Client: mock}

	// DownloadPackages left false (default) — /v1/jcds/files must never be hit,
	// so no mock response is registered for it; a call would error the mock.
	err := runBackup(context.Background(), cliCtx, backupOptions{
		OutputDir:   outDir,
		Format:      "yaml",
		Resources:   "packages",
		Concurrency: 2,
	})
	if err != nil {
		t.Fatalf("runBackup error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outDir, "packages", "files")); !os.IsNotExist(err) {
		t.Error("packages/files directory should not exist when --download-packages is not set")
	}
}

func TestUnwrapClassicDetail(t *testing.T) {
	// Single-key wrapper
	wrapped := map[string]any{
		"policy": map[string]any{"name": "Test"},
	}
	result := unwrapClassicDetail(wrapped)
	if result["name"] != "Test" {
		t.Errorf("expected unwrapped name=Test, got %v", result)
	}

	// Non-wrapper (multiple keys)
	multi := map[string]any{
		"name":    "Test",
		"enabled": true,
	}
	result2 := unwrapClassicDetail(multi)
	if result2["name"] != "Test" || result2["enabled"] != true {
		t.Errorf("multi-key map should pass through, got %v", result2)
	}
}

// TestNoAuthAnnotation_ListResourcesSkipsAuth covers both halves of the
// annotation skip. `backup list-resources` reads two in-process tables and must
// run with no credentials at all; its sibling `backup` carries no annotation and
// must still resolve auth, because a skip that leaks onto a sibling is invisible
// — the command runs with no client and fails later with a message that sends
// the operator to fix credentials that were never the problem.
func TestNoAuthAnnotation_ListResourcesSkipsAuth(t *testing.T) {
	t.Run("list-resources runs with no credentials", func(t *testing.T) {
		resetGlobals()
		t.Setenv("JAMF_URL", "")
		t.Setenv("JAMF_TOKEN", "")
		t.Setenv("JAMF_CLIENT_ID", "")
		t.Setenv("JAMF_CLIENT_SECRET", "")
		t.Setenv("JAMF_PROFILE", "")
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
		root.SetArgs([]string{"pro", "backup", "list-resources"})
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)

		if err := root.Execute(); err != nil {
			t.Errorf("pro backup list-resources with no credentials: %v", err)
		}
	})

	t.Run("backup itself still resolves auth", func(t *testing.T) {
		resetGlobals()
		t.Setenv("JAMF_URL", "https://test.jamfcloud.com")
		t.Setenv("JAMF_TOKEN", "")
		t.Setenv("JAMF_CLIENT_ID", "")
		t.Setenv("JAMF_CLIENT_SECRET", "")
		t.Setenv("JAMF_PROFILE", "")
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
		root.SetArgs([]string{"pro", "backup", "--output", t.TempDir()})
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)

		err := root.Execute()
		if err == nil {
			t.Fatal("expected an auth error — the sibling skipped auth resolution")
		}
		if !strings.Contains(err.Error(), "authentication required") {
			t.Errorf("error = %q, want the auth-resolution error; any other message "+
				"here means auth was skipped and the command failed later", err.Error())
		}
	})
}

// TestBackupListResources_HonoursOutFile pins that list-resources prints
// through the shared formatter. It used to build its own with output.New, which
// writes to os.Stdout and applies none of the global flags PersistentPreRunE
// has already wired onto cliCtx.Output. --out-file created an empty file while
// the listing went to stdout, and --select, --field and --compact did nothing.
func TestBackupListResources_HonoursOutFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "tokens.json")

	root := NewRootCmd("test", "none", "none", "none")
	root.SetArgs([]string{"pro", "backup", "list-resources", "-o", "json", "--out-file", path})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	var err error
	stdout := captureStdout(t, func() { err = root.Execute() })
	if err != nil {
		t.Fatalf("pro backup list-resources: %v", err)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("--out-file was given, so stdout must be empty, got %d bytes: %q", len(stdout), stdout)
	}

	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("reading --out-file target: %v", readErr)
	}
	if len(data) == 0 {
		t.Fatal("--out-file target is empty: the listing went somewhere else")
	}

	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatalf("--out-file target is not the JSON listing: %v", err)
	}
	if len(rows) != len(BackupFilterNames()) {
		t.Errorf("--out-file target holds %d rows, want %d", len(rows), len(BackupFilterNames()))
	}
	if _, ok := rows[0]["objects"]; !ok {
		t.Error("json output must keep the objects key for anything parsing it")
	}
}

// TestBackupListResources_HonoursSelect covers the same regression through the
// projector, which output.New leaves unset.
func TestBackupListResources_HonoursSelect(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	root := NewRootCmd("test", "none", "none", "none")
	root.SetArgs([]string{"pro", "backup", "list-resources", "-o", "json", "--select", "resource"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	var err error
	stdout := captureStdout(t, func() { err = root.Execute() })
	if err != nil {
		t.Fatalf("pro backup list-resources: %v", err)
	}

	var rows []map[string]any
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("output is not JSON rows: %v (got %q)", err, stdout)
	}
	if len(rows) == 0 {
		t.Fatal("no rows returned")
	}
	for i, r := range rows {
		if len(r) != 1 {
			t.Fatalf("row %d has %d keys, want 1 after --select resource: %v", i, len(r), r)
		}
		if _, ok := r["resource"]; !ok {
			t.Fatalf("row %d does not carry the selected key: %v", i, r)
		}
	}
}

// TestBackupListResources_TableLeadsWithTheToken renders the real table and
// pins the two facts the narrowed row shape exists for: resource is the first
// column, and objects is not a column at all. sortedKeys floats only id and
// name, so the rest are alphabetical and objects would otherwise lead.
func TestBackupListResources_TableLeadsWithTheToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	root := NewRootCmd("test", "none", "none", "none")
	root.SetArgs([]string{"pro", "backup", "list-resources", "-o", "table", "--no-color"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	var err error
	stdout := captureStdout(t, func() { err = root.Execute() })
	if err != nil {
		t.Fatalf("pro backup list-resources: %v", err)
	}

	// The row-count banner precedes the column header, so find the header
	// rather than assuming it is the first line.
	header := ""
	for _, line := range strings.Split(stdout, "\n") {
		if strings.Contains(strings.ToLower(line), "resource") {
			header = strings.ToLower(line)
			break
		}
	}
	if header == "" {
		t.Fatalf("no column header naming resource in table output: %q", stdout)
	}
	if fields := strings.Fields(header); fields[0] != "resource" {
		t.Errorf("table columns = %v, want resource first", fields)
	}
	if strings.Contains(header, "objects") {
		t.Errorf("table must not carry the objects column, header = %q", header)
	}
}

// TestBackup_AdvancedComputerSearchDropsExecutedResults covers the reason
// BackupResource.DropKeys exists. A Classic advanced-search GET *runs* the
// search: `GET /JSSResource/advancedcomputersearches/id/{id}` returns the
// definition and the computers it currently matches, declared in
// specs/classic/schemas.json as advanced_computer_search.computers[] with
// {id, name, udid, Computer_Name}. It is the only curated Classic backup
// resource whose response embeds device membership.
//
// Three things go wrong if it is kept, and the first is why this asserts
// --include-ids too. StripServerFields drops ids and timestamps generically and
// is skipped under --include-ids, so it can never do this job. `pro diff` then
// reports `field: computers` modified for a search whose configuration is
// identical, on every cross-instance run, because two instances never share
// device membership. And a directory meant for version control carries device
// names and UDIDs, which no other resource in the default set contributes.
func TestBackup_AdvancedComputerSearchDropsExecutedResults(t *testing.T) {
	const listXML = `<?xml version="1.0" encoding="UTF-8"?>
<advanced_computer_searches>
  <advanced_computer_search><id>1</id><name>All Macs</name></advanced_computer_search>
</advanced_computer_searches>`
	// The shape the server really answers: config plus executed membership.
	const detailXML = `<?xml version="1.0" encoding="UTF-8"?>
<advanced_computer_search>
  <id>1</id>
  <name>All Macs</name>
  <view_as>Standard Web Page</view_as>
  <criteria><criterion><name>Operating System</name><priority>0</priority></criterion></criteria>
  <computers>
    <computer><id>3</id><name>Joes iMac</name><udid>55900BDC-347C-58B1-D249-F32244B11D30</udid><Computer_Name>Joes iMac</Computer_Name></computer>
  </computers>
</advanced_computer_search>`

	for _, tc := range []struct {
		name       string
		includeIDs bool
	}{
		{"default", false},
		{"include-ids", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock := &backupMockClient{responses: map[string]overviewMockResponse{
				"/JSSResource/advancedcomputersearches":      {200, listXML},
				"/JSSResource/advancedcomputersearches/id/1": {200, detailXML},
				"/v1/advanced-mobile-device-searches":        {200, `{"totalCount":0,"results":[]}`},
			}}

			oldURL := serverURL
			serverURL = "https://test.jamfcloud.com"
			defer func() { serverURL = oldURL }()

			outDir := t.TempDir()
			if err := runBackup(context.Background(), &registry.CLIContext{Client: mock}, backupOptions{
				OutputDir:   outDir,
				Format:      "yaml",
				Resources:   "advanced-searches",
				IncludeIDs:  tc.includeIDs,
				Concurrency: 2,
			}); err != nil {
				t.Fatalf("runBackup: %v", err)
			}

			// SlugifyName rather than a literal, so the filename convention
			// changing does not read as this rule failing.
			path := filepath.Join(outDir, "advanced-searches", "computers", SlugifyName("All Macs")+".yaml")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading %s: %v", path, err)
			}
			body := string(raw)

			// The membership and everything it carries must be gone.
			for _, leaked := range []string{"computers:", "Joes iMac", "55900BDC-347C-58B1-D249-F32244B11D30"} {
				if strings.Contains(body, leaked) {
					t.Errorf("exported search still carries %q, so device data reaches a version-controlled backup:\n%s", leaked, body)
				}
			}
			// The configuration must survive: a drop that took the whole
			// object would satisfy the assertions above.
			for _, want := range []string{"name:", "criteria:", "view_as:"} {
				if !strings.Contains(body, want) {
					t.Errorf("exported search lost its configuration key %q:\n%s", want, body)
				}
			}
		})
	}
}
