// Copyright 2026, Jamf Software LLC

package safewrite

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports no Unix permission bits and needs privilege to create a symlink")
	}
}

func TestFile_WritesAtTheRequestedMode(t *testing.T) {
	skipOnWindows(t)
	path := filepath.Join(t.TempDir(), "out.yaml")
	if err := File(path, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %#o, want 0600", perm)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "hello\n" {
		t.Errorf("content = %q", got)
	}
}

func TestFile_PreExistingLooserModeDoesNotSurvive(t *testing.T) {
	skipOnWindows(t)
	path := filepath.Join(t.TempDir(), "out.yaml")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := File(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %#o, want 0600 — an os.WriteFile over an existing file keeps its old mode", perm)
	}
}

func TestFile_ReplacesASymlinkRatherThanFollowingIt(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere")
	if err := os.WriteFile(target, []byte("untouched"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "out.yaml")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}

	if err := File(path, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("destination is still a symlink")
	}
	got, _ := os.ReadFile(target)
	if string(got) != "untouched" {
		t.Errorf("link target was written through: %q", got)
	}
}

func TestFile_ReplacesAHardLinkRatherThanWritingIntoIt(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	other := filepath.Join(dir, "other")
	if err := os.WriteFile(other, []byte("untouched"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "out.yaml")
	if err := os.Link(other, path); err != nil {
		t.Fatal(err)
	}

	if err := File(path, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(other)
	if string(got) != "untouched" {
		t.Errorf("hard-linked file was written through: %q", got)
	}
}

func TestFile_LeavesNoTempFileBehind(t *testing.T) {
	dir := t.TempDir()
	if err := File(filepath.Join(dir, "out.yaml"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only the destination", len(entries))
	}
}
