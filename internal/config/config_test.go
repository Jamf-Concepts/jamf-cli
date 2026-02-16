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

func TestIsSecretRef(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"env:MY_VAR", true},
		{"file:/path/to/secret", true},
		{"keychain:jamfpro-cli/prod/token", true},
		{"keychain:prod/token", true},
		{"my-plain-secret", false},
		{"", false},
		{"ENV:MY_VAR", false},       // case-sensitive
		{"envoy-service", false},    // prefix must be exact
		{"filecoin-token", false},   // not a file: prefix
		{"keychainref-foo", false},  // not a keychain: prefix
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got := IsSecretRef(tt.value)
			if got != tt.want {
				t.Errorf("IsSecretRef(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestMaskedProfile(t *testing.T) {
	p := Profile{
		URL:          "https://example.com",
		AuthMethod:   "oauth2",
		ClientID:     "my-client-id",
		ClientSecret: "super-secret",
		Token:        "keychain:jamfpro-cli/prod/token",
		Password:     "env:JAMF_PASSWORD",
	}

	masked := MaskedProfile(p)

	// URL and AuthMethod should not be masked
	if masked.URL != p.URL {
		t.Errorf("URL should not be masked, got %q", masked.URL)
	}
	if masked.AuthMethod != p.AuthMethod {
		t.Errorf("AuthMethod should not be masked, got %q", masked.AuthMethod)
	}

	// ClientID is not a secret field, should not be masked
	if masked.ClientID != "my-client-id" {
		t.Errorf("ClientID should not be masked, got %q", masked.ClientID)
	}

	// Plaintext secret should be masked
	if masked.ClientSecret != "***" {
		t.Errorf("ClientSecret should be masked, got %q", masked.ClientSecret)
	}

	// Secret references should be preserved
	if masked.Token != "keychain:jamfpro-cli/prod/token" {
		t.Errorf("Token keychain ref should be preserved, got %q", masked.Token)
	}
	if masked.Password != "env:JAMF_PASSWORD" {
		t.Errorf("Password env ref should be preserved, got %q", masked.Password)
	}
}

func TestMaskedProfile_EmptyFields(t *testing.T) {
	p := Profile{
		URL:        "https://example.com",
		AuthMethod: "token",
	}

	masked := MaskedProfile(p)

	if masked.Token != "" {
		t.Errorf("empty Token should stay empty, got %q", masked.Token)
	}
	if masked.Password != "" {
		t.Errorf("empty Password should stay empty, got %q", masked.Password)
	}
	if masked.ClientSecret != "" {
		t.Errorf("empty ClientSecret should stay empty, got %q", masked.ClientSecret)
	}
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
