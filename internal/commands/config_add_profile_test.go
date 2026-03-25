package commands

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamfpro-cli/internal/config"
	"github.com/Jamf-Concepts/jamfpro-cli/internal/keychain"
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
	jDir := filepath.Join(dir, "jamfpro-cli")
	if err := os.MkdirAll(jDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	return jDir
}

func TestAddProfile_DefaultsToKeychain(t *testing.T) {
	setupTempConfig(t)

	mock := newMockKeychainStore()
	old := config.KeychainStore
	config.KeychainStore = mock
	defer func() { config.KeychainStore = old }()

	cmd := newConfigAddProfileCmd()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{
		"test-profile",
		"--url", "https://example.jamfcloud.com",
		"--auth-method", "oauth2",
		"--client-id", "my-id",
		"--client-secret", "my-secret",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr.String())
	}

	// Verify secrets were stored in keychain
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}

	p := cfg.Profiles["test-profile"]
	if !strings.HasPrefix(p.ClientID, "keychain:") {
		t.Errorf("client-id should be a keychain ref, got %q", p.ClientID)
	}
	if !strings.HasPrefix(p.ClientSecret, "keychain:") {
		t.Errorf("client-secret should be a keychain ref, got %q", p.ClientSecret)
	}

	// Verify the actual values are in the mock keychain
	expectedRef := keychain.DefaultService + "/test-profile/client-secret"
	if v, ok := mock.items[expectedRef]; !ok || v != "my-secret" {
		t.Errorf("expected keychain item %q = %q, got %q (exists: %v)", expectedRef, "my-secret", v, ok)
	}
}

func TestAddProfile_EnvRefSkipsKeychain(t *testing.T) {
	setupTempConfig(t)

	mock := newMockKeychainStore()
	old := config.KeychainStore
	config.KeychainStore = mock
	defer func() { config.KeychainStore = old }()

	cmd := newConfigAddProfileCmd()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{
		"test-profile",
		"--url", "https://example.jamfcloud.com",
		"--auth-method", "oauth2",
		"--client-id", "my-id",
		"--client-secret", "env:MY_SECRET",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr.String())
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}

	p := cfg.Profiles["test-profile"]
	// client-secret should be written as env: ref, not keychain
	if p.ClientSecret != "env:MY_SECRET" {
		t.Errorf("client-secret should be env ref %q, got %q", "env:MY_SECRET", p.ClientSecret)
	}
	// client-id (bare value) should be in keychain
	if !strings.HasPrefix(p.ClientID, "keychain:") {
		t.Errorf("client-id should be a keychain ref, got %q", p.ClientID)
	}
}

func TestAddProfile_KeychainFailureReturnsError(t *testing.T) {
	setupTempConfig(t)

	old := config.KeychainStore
	config.KeychainStore = &failingKeychainStore{}
	defer func() { config.KeychainStore = old }()

	cmd := newConfigAddProfileCmd()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{
		"test-profile",
		"--url", "https://example.jamfcloud.com",
		"--auth-method", "oauth2",
		"--client-id", "my-id",
		"--client-secret", "my-secret",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when keychain fails, got nil")
	}
	if !strings.Contains(err.Error(), "keychain") {
		t.Errorf("expected keychain error, got: %v", err)
	}
}
