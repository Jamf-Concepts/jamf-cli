// Copyright 2026, Jamf Software LLC

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func TestUploadMultiple_RefusesBeforeAnythingIsSent(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a", "Same.pkg")
	b := filepath.Join(dir, "b", "same.PKG")
	for _, p := range []string{a, b} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(files ...string) error {
		cmd := newPackagesUploadMultipleCmd(&registry.CLIContext{})
		cmd.SetArgs(files)
		cmd.SilenceUsage, cmd.SilenceErrors = true, true
		return cmd.Execute()
	}

	// Jamf Pro keys a package on its file name, case-insensitively: the second
	// upload would replace the first.
	if err := run(a, b); err == nil || !strings.Contains(err.Error(), "share the file name") {
		t.Errorf("same name in two folders: %v", err)
	}
	if err := run(a, filepath.Join(dir, "missing.pkg")); err == nil || !strings.Contains(err.Error(), "cannot access") {
		t.Errorf("missing file: %v", err)
	}
	if err := run(dir); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Errorf("directory: %v", err)
	}
	if err := run(); err == nil {
		t.Error("no files accepted")
	}
}
