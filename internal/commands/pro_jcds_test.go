// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"crypto/sha3"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// jcdsTestClient implements registry.HTTPClient backed by an httptest.Server.
type jcdsTestClient struct {
	srv *httptest.Server
}

func (c *jcdsTestClient) Do(_ context.Context, method, path string, body io.Reader) (*http.Response, error) {
	url := c.srv.URL + path
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequest(method, url, body)
	} else {
		req, err = http.NewRequest(method, url, nil)
	}
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}

// sha3hex returns the SHA3-512 hex of data.
func sha3hex(data []byte) string {
	h := sha3.New512()
	_, _ = h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func TestJcdsFileSHA3(t *testing.T) {
	content := []byte("hello jcds package")
	f, err := os.CreateTemp(t.TempDir(), "*.pkg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := jcdsFileSHA3(f.Name())
	if err != nil {
		t.Fatalf("jcdsFileSHA3: %v", err)
	}
	want := sha3hex(content)
	if got != want {
		t.Errorf("SHA3 mismatch: got %s, want %s", got, want)
	}
}

func TestJcdsDownloadFile(t *testing.T) {
	fileContent := []byte("fake package content")

	// Serve the pre-signed URL file content.
	fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fileContent)
	}))
	defer fileSrv.Close()

	// Serve the JCDS API returning the pre-signed URL.
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jcds/files/test.pkg", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"uri": fileSrv.URL + "/download"})
	})
	apiSrv := httptest.NewServer(mux)
	defer apiSrv.Close()

	client := &jcdsTestClient{srv: apiSrv}
	outPath := filepath.Join(t.TempDir(), "test.pkg")

	if err := jcdsDownloadFile(context.Background(), client, "test.pkg", outPath); err != nil {
		t.Fatalf("jcdsDownloadFile: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if string(got) != string(fileContent) {
		t.Errorf("content mismatch: got %q, want %q", got, fileContent)
	}
}

func TestJcdsDownloadFile_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jcds/files/missing.pkg", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &jcdsTestClient{srv: srv}
	outPath := filepath.Join(t.TempDir(), "missing.pkg")
	err := jcdsDownloadFile(context.Background(), client, "missing.pkg", outPath)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 in error, got: %v", err)
	}
}

func TestJcdsDownloadFile_403Retry(t *testing.T) {
	fileContent := []byte("retry test content")

	hits := 0
	fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fileContent)
	}))
	defer fileSrv.Close()

	apiCalls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jcds/files/retry.pkg", func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		_ = json.NewEncoder(w).Encode(map[string]string{"uri": fileSrv.URL + "/file"})
	})
	apiSrv := httptest.NewServer(mux)
	defer apiSrv.Close()

	outPath := filepath.Join(t.TempDir(), "retry.pkg")
	if err := jcdsDownloadFile(context.Background(), &jcdsTestClient{srv: apiSrv}, "retry.pkg", outPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// URL must be fetched twice — initial + after 403.
	if apiCalls != 2 {
		t.Errorf("expected 2 API calls (initial + retry), got %d", apiCalls)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(fileContent) {
		t.Errorf("got %q, want %q", got, fileContent)
	}
}

func TestJcdsSyncCmd_Download(t *testing.T) {
	fileContent := []byte("pkg content for sync test")
	fileHash := sha3hex(fileContent)

	fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fileContent)
	}))
	defer fileSrv.Close()

	mux := http.NewServeMux()
	// List endpoint.
	mux.HandleFunc("/v1/jcds/files", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_ = json.NewEncoder(w).Encode([]jcdsFileData{
			{FileName: "pkg-a.pkg", SHA3: fileHash},
		})
	})
	// Get pre-signed URL.
	mux.HandleFunc("/v1/jcds/files/pkg-a.pkg", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"uri": fileSrv.URL + "/pkg-a.pkg"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	ctx := &registry.CLIContext{Client: &jcdsTestClient{srv: srv}, Output: &captureOutput{}}
	cmd := newJcdsSyncCmd(ctx)
	cmd.SetArgs([]string{"--dir", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "pkg-a.pkg"))
	if err != nil {
		t.Fatalf("reading downloaded file: %v", err)
	}
	if string(got) != string(fileContent) {
		t.Errorf("content mismatch: got %q, want %q", got, fileContent)
	}
}

func TestJcdsSyncCmd_SkipUpToDate(t *testing.T) {
	fileContent := []byte("already correct content")
	fileHash := sha3hex(fileContent)

	dir := t.TempDir()
	// Pre-populate local file with matching content.
	if err := os.WriteFile(filepath.Join(dir, "pkg-b.pkg"), fileContent, 0o644); err != nil {
		t.Fatal(err)
	}

	downloadCalled := false
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jcds/files", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]jcdsFileData{
			{FileName: "pkg-b.pkg", SHA3: fileHash},
		})
	})
	mux.HandleFunc("/v1/jcds/files/pkg-b.pkg", func(w http.ResponseWriter, r *http.Request) {
		downloadCalled = true
		_ = json.NewEncoder(w).Encode(map[string]string{"uri": "http://should-not-be-called"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := &registry.CLIContext{Client: &jcdsTestClient{srv: srv}, Output: &captureOutput{}}
	cmd := newJcdsSyncCmd(ctx)
	cmd.SetArgs([]string{"--dir", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if downloadCalled {
		t.Error("download endpoint called for up-to-date file — should have been skipped")
	}
}

func TestJcdsSyncCmd_UpdateChanged(t *testing.T) {
	oldContent := []byte("old package content")
	newContent := []byte("new package content")
	newHash := sha3hex(newContent)

	dir := t.TempDir()
	// Pre-populate with old content (different hash).
	if err := os.WriteFile(filepath.Join(dir, "pkg-c.pkg"), oldContent, 0o644); err != nil {
		t.Fatal(err)
	}

	fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(newContent)
	}))
	defer fileSrv.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jcds/files", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]jcdsFileData{
			{FileName: "pkg-c.pkg", SHA3: newHash},
		})
	})
	mux.HandleFunc("/v1/jcds/files/pkg-c.pkg", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"uri": fileSrv.URL + "/pkg-c.pkg"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := &registry.CLIContext{Client: &jcdsTestClient{srv: srv}, Output: &captureOutput{}}
	cmd := newJcdsSyncCmd(ctx)
	cmd.SetArgs([]string{"--dir", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "pkg-c.pkg"))
	if err != nil {
		t.Fatalf("reading updated file: %v", err)
	}
	if string(got) != string(newContent) {
		t.Errorf("file not updated: got %q, want %q", got, newContent)
	}
}

func TestJcdsSyncCmd_DeleteOrphans(t *testing.T) {
	dir := t.TempDir()
	// Local file not on JCDS.
	orphanPath := filepath.Join(dir, "orphan.pkg")
	if err := os.WriteFile(orphanPath, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jcds/files", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]jcdsFileData{}) // empty — nothing on JCDS
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := &registry.CLIContext{Client: &jcdsTestClient{srv: srv}, Output: &captureOutput{}}
	cmd := newJcdsSyncCmd(ctx)
	cmd.SetArgs([]string{"--dir", dir, "--delete"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Error("orphan file should have been deleted but still exists")
	}
}

func TestJcdsSyncCmd_DeleteOrphans_DryRun(t *testing.T) {
	dir := t.TempDir()
	orphanPath := filepath.Join(dir, "orphan.pkg")
	if err := os.WriteFile(orphanPath, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jcds/files", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]jcdsFileData{})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := &registry.CLIContext{Client: &jcdsTestClient{srv: srv}, Output: &captureOutput{}}
	cmd := newJcdsSyncCmd(ctx)
	cmd.SetArgs([]string{"--dir", dir, "--delete", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	if _, err := os.Stat(orphanPath); os.IsNotExist(err) {
		t.Error("dry-run should not delete orphan file")
	}
}

func TestJcdsSyncCmd_InvalidConcurrency(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jcds/files", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]jcdsFileData{})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := &registry.CLIContext{Client: &jcdsTestClient{srv: srv}, Output: &captureOutput{}}
	cmd := newJcdsSyncCmd(ctx)
	cmd.SetArgs([]string{"--dir", t.TempDir(), "--concurrency", "0"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for --concurrency 0, got nil")
	}
	if !strings.Contains(err.Error(), "concurrency") {
		t.Errorf("expected concurrency error, got: %v", err)
	}
}

func TestJcdsSyncCmd_NoSHA3_NoRedownload(t *testing.T) {
	fileContent := []byte("existing content")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pkg-d.pkg"), fileContent, 0o644); err != nil {
		t.Fatal(err)
	}

	downloadCalled := false
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jcds/files", func(w http.ResponseWriter, r *http.Request) {
		// SHA3 field absent — API didn't return a hash.
		_ = json.NewEncoder(w).Encode([]jcdsFileData{
			{FileName: "pkg-d.pkg", SHA3: ""},
		})
	})
	mux.HandleFunc("/v1/jcds/files/pkg-d.pkg", func(w http.ResponseWriter, r *http.Request) {
		downloadCalled = true
		_, _ = fmt.Fprint(w, `{"uri":"http://should-not-be-called"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := &registry.CLIContext{Client: &jcdsTestClient{srv: srv}, Output: &captureOutput{}}
	cmd := newJcdsSyncCmd(ctx)
	cmd.SetArgs([]string{"--dir", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if downloadCalled {
		t.Error("should not download when JCDS returns no SHA3 hash")
	}
}

func TestJcdsSyncCmd_FailedDownload(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jcds/files", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]jcdsFileData{
			{FileName: "bad.pkg", SHA3: "somehash"},
		})
	})
	// Return an API error for the presigned-URL fetch so the download fails.
	mux.HandleFunc("/v1/jcds/files/bad.pkg", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"server error"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out := &captureOutput{}
	ctx := &registry.CLIContext{Client: &jcdsTestClient{srv: srv}, Output: out}
	cmd := newJcdsSyncCmd(ctx)
	cmd.SetArgs([]string{"--dir", t.TempDir()})

	// Sync must complete without error — failed files are skipped, not fatal.
	if err := cmd.Execute(); err != nil {
		t.Fatalf("sync should not fail on download error (skip-on-failure): %v", err)
	}

	var report jcdsSyncReport
	if err := json.Unmarshal(out.rawData, &report); err != nil {
		t.Fatalf("parsing report: %v (raw: %s)", err, out.rawData)
	}
	if report.Summary.Failed != 1 {
		t.Errorf("failed: got %d, want 1", report.Summary.Failed)
	}
	if len(report.Files) != 1 {
		t.Fatalf("files: got %d, want 1", len(report.Files))
	}
	if report.Files[0].Status != jcdsStatusFailed {
		t.Errorf("status: got %q, want %q", report.Files[0].Status, jcdsStatusFailed)
	}
	if report.Files[0].Error == "" {
		t.Error("error field should be non-empty for failed file")
	}
}

func TestJcdsSyncCmd_StructuredResult(t *testing.T) {
	fileContent := []byte("structured result test content")
	fileHash := sha3hex(fileContent)

	fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(fileContent)
	}))
	defer fileSrv.Close()

	dir := t.TempDir()
	// One existing up-to-date file, one missing file to download.
	if err := os.WriteFile(filepath.Join(dir, "existing.pkg"), fileContent, 0o644); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jcds/files", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]jcdsFileData{
			{FileName: "existing.pkg", SHA3: fileHash},
			{FileName: "new.pkg", SHA3: "irrelevant-for-missing"},
		})
	})
	mux.HandleFunc("/v1/jcds/files/new.pkg", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"uri": fileSrv.URL + "/new.pkg"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out := &captureOutput{}
	ctx := &registry.CLIContext{Client: &jcdsTestClient{srv: srv}, Output: out}
	cmd := newJcdsSyncCmd(ctx)
	cmd.SetArgs([]string{"--dir", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	var report jcdsSyncReport
	if err := json.Unmarshal(out.rawData, &report); err != nil {
		t.Fatalf("parsing sync report: %v (raw: %s)", err, out.rawData)
	}

	// Check summary counts.
	if report.Summary.Downloaded != 1 {
		t.Errorf("downloaded: got %d, want 1", report.Summary.Downloaded)
	}
	if report.Summary.UpToDate != 1 {
		t.Errorf("upToDate: got %d, want 1", report.Summary.UpToDate)
	}
	if report.Summary.Updated != 0 || report.Summary.Deleted != 0 || report.Summary.Failed != 0 {
		t.Errorf("unexpected counts: updated=%d deleted=%d failed=%d", report.Summary.Updated, report.Summary.Deleted, report.Summary.Failed)
	}

	// Check per-file entries.
	if len(report.Files) != 2 {
		t.Fatalf("files: got %d entries, want 2", len(report.Files))
	}
	filesByName := make(map[string]jcdsSyncFileResult, len(report.Files))
	for _, f := range report.Files {
		filesByName[f.FileName] = f
	}
	if filesByName["existing.pkg"].Status != jcdsStatusSkipped {
		t.Errorf("existing.pkg: got status %q, want %q", filesByName["existing.pkg"].Status, jcdsStatusSkipped)
	}
	if filesByName["new.pkg"].Status != jcdsStatusDownloaded {
		t.Errorf("new.pkg: got status %q, want %q", filesByName["new.pkg"].Status, jcdsStatusDownloaded)
	}
}
