// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// failingKeychainStore always returns an error on Set, simulating keychain unavailability.
type failingKeychainStore struct{}

func (f *failingKeychainStore) Get(_, _ string) (string, error) {
	return "", fmt.Errorf("keychain unavailable")
}

func (f *failingKeychainStore) Set(_, _, _ string) error {
	return fmt.Errorf("keychain unavailable")
}

func (f *failingKeychainStore) Delete(_, _ string) error {
	return fmt.Errorf("keychain unavailable")
}

// setupTempConfig creates a temp config directory and sets XDG_CONFIG_HOME.
func setupTempConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	jDir := filepath.Join(dir, "jamf-cli")
	if err := os.MkdirAll(jDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	return jDir
}

func TestAddProfile_NoInputRejected(t *testing.T) {
	setupTempConfig(t)

	oldNoInput := noInput
	noInput = true
	defer func() { noInput = oldNoInput }()

	cmd := newConfigAddProfileCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"test-profile",
		"--url", "https://example.jamfcloud.com",
		"--auth-method", "oauth2",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --no-input is set")
	}
	if !strings.Contains(err.Error(), "cannot use --no-input") {
		t.Errorf("error = %q, want to contain 'cannot use --no-input'", err.Error())
	}
}
