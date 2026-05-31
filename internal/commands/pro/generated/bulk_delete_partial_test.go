// Copyright 2026, Jamf Software LLC

package generated

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// A bulk delete where one ID succeeds and one fails must continue past the
// failure and return exit code 7 (partial failure), not abort at the first one.
func TestGeneratedBulkDelete_PartialFailure(t *testing.T) {
	dir := t.TempDir()
	listFile := filepath.Join(dir, "ids.txt")
	if err := os.WriteFile(listFile, []byte("1\n2\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	mock := &mockHTTPClient{responses: map[string]mockResponse{
		"/v1/buildings/1": {status: 204},
		"/v1/buildings/2": {status: 500, body: []byte("boom")},
	}}
	cmd := newBuildingsDeleteCmd(&registry.CLIContext{Client: mock})
	cmd.SetArgs([]string{"--from-file", listFile, "--yes"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected a partial-failure error, got nil")
	}
	if got := exitcode.CodeFrom(err); got != exitcode.PartialFailure {
		t.Fatalf("exit code = %d, want PartialFailure(%d) (err=%v)", got, exitcode.PartialFailure, err)
	}
}
