package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ktn-jamf/jamfpro-cli/internal/keychain"
)

// mockStore implements keychain.Store for testing.
type mockStore struct {
	items map[string]string
}

func newMockStore() *mockStore {
	return &mockStore{items: make(map[string]string)}
}

func (m *mockStore) Get(service, account string) (string, error) {
	key := service + "/" + account
	if v, ok := m.items[key]; ok {
		return v, nil
	}
	return "", keychain.ErrNotFound
}

func (m *mockStore) Set(service, account, secret string) error {
	key := service + "/" + account
	m.items[key] = secret
	return nil
}

func (m *mockStore) Delete(service, account string) error {
	key := service + "/" + account
	if _, ok := m.items[key]; !ok {
		return keychain.ErrNotFound
	}
	delete(m.items, key)
	return nil
}

func TestResolveSecret_Literal(t *testing.T) {
	val, err := ResolveSecret("my-plain-secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "my-plain-secret" {
		t.Errorf("got %q, want %q", val, "my-plain-secret")
	}
}

func TestResolveSecret_Env(t *testing.T) {
	t.Setenv("TEST_RESOLVE_SECRET_VAR", "env-value")
	val, err := ResolveSecret("env:TEST_RESOLVE_SECRET_VAR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "env-value" {
		t.Errorf("got %q, want %q", val, "env-value")
	}
}

func TestResolveSecret_Env_Missing(t *testing.T) {
	_, err := ResolveSecret("env:DEFINITELY_NOT_SET_98765")
	if err == nil {
		t.Fatal("expected error for missing env var")
	}
}

func TestResolveSecret_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(path, []byte("file-value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	val, err := ResolveSecret("file:" + path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "file-value" {
		t.Errorf("got %q, want %q", val, "file-value")
	}
}

func TestResolveSecret_File_Missing(t *testing.T) {
	_, err := ResolveSecret("file:/nonexistent/path/secret.txt")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestResolveSecret_Keychain(t *testing.T) {
	mock := newMockStore()
	mock.items["jamfpro-cli/prod/client-secret"] = "keychain-value"

	// Inject mock store
	old := KeychainStore
	KeychainStore = mock
	defer func() { KeychainStore = old }()

	val, err := ResolveSecret("keychain:jamfpro-cli/prod/client-secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "keychain-value" {
		t.Errorf("got %q, want %q", val, "keychain-value")
	}
}

func TestResolveSecret_Keychain_BareRef(t *testing.T) {
	mock := newMockStore()
	mock.items["jamfpro-cli/prod/token"] = "bare-value"

	old := KeychainStore
	KeychainStore = mock
	defer func() { KeychainStore = old }()

	val, err := ResolveSecret("keychain:prod/token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "bare-value" {
		t.Errorf("got %q, want %q", val, "bare-value")
	}
}

func TestResolveSecret_Keychain_NotFound(t *testing.T) {
	mock := newMockStore()

	old := KeychainStore
	KeychainStore = mock
	defer func() { KeychainStore = old }()

	_, err := ResolveSecret("keychain:jamfpro-cli/missing/secret")
	if err == nil {
		t.Fatal("expected error for missing keychain item")
	}
}

func TestResolveSecret_Empty(t *testing.T) {
	val, err := ResolveSecret("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "" {
		t.Errorf("got %q, want empty string", val)
	}
}
