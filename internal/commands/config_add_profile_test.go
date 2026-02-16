package commands

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ktn-jamf/jamfpro-cli/internal/config"
	"github.com/ktn-jamf/jamfpro-cli/internal/keychain"
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
	if err := os.MkdirAll(jDir, 0700); err != nil {
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

func TestAddProfile_NoKeychainFlag(t *testing.T) {
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
		"--no-keychain",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}

	p := cfg.Profiles["test-profile"]
	if p.ClientID != "my-id" {
		t.Errorf("client-id should be plaintext %q, got %q", "my-id", p.ClientID)
	}
	if p.ClientSecret != "my-secret" {
		t.Errorf("client-secret should be plaintext %q, got %q", "my-secret", p.ClientSecret)
	}

	// Keychain should be empty
	if len(mock.items) != 0 {
		t.Errorf("expected no keychain items, got %d", len(mock.items))
	}
}

func TestAddProfile_KeychainFailureFallback(t *testing.T) {
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

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have warnings on stderr
	if !strings.Contains(stderr.String(), "Warning:") {
		t.Errorf("expected warning on stderr, got: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Falling back") {
		t.Errorf("expected fallback message on stderr, got: %s", stderr.String())
	}

	// Secrets should be stored as plaintext
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}

	p := cfg.Profiles["test-profile"]
	if p.ClientID != "my-id" {
		t.Errorf("client-id should fall back to plaintext %q, got %q", "my-id", p.ClientID)
	}
	if p.ClientSecret != "my-secret" {
		t.Errorf("client-secret should fall back to plaintext %q, got %q", "my-secret", p.ClientSecret)
	}
}
