package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jamf/jamfpro-cli/internal/keychain"
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

func TestResolveSecret_Literal_Rejected(t *testing.T) {
	_, err := ResolveSecret("my-plain-secret")
	if err == nil {
		t.Fatal("expected error for literal secret, got nil")
	}
	if !strings.Contains(err.Error(), "unrecognized secret format") {
		t.Errorf("unexpected error message: %v", err)
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

// --- ConfigPath tests ---

func TestConfigPath_XDG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	got := ConfigPath()
	want := filepath.Join(dir, "jamfpro-cli", "config.yaml")
	if got != want {
		t.Errorf("ConfigPath() = %q, want %q", got, want)
	}
}

func TestConfigPath_XDGDefault(t *testing.T) {
	// Unset XDG_CONFIG_HOME to test the default ~/.config path
	t.Setenv("XDG_CONFIG_HOME", "")

	// Since we can't predict HOME, just verify it ends with the expected suffix
	got := ConfigPath()
	if !strings.HasSuffix(got, filepath.Join("jamfpro-cli", "config.yaml")) {
		t.Errorf("ConfigPath() = %q, expected to end with jamfpro-cli/config.yaml", got)
	}
}


// --- GetKeychainStore tests ---

func TestGetKeychainStore_Override(t *testing.T) {
	mock := newMockStore()
	old := KeychainStore
	KeychainStore = mock
	defer func() { KeychainStore = old }()

	got := GetKeychainStore()
	if got != mock {
		t.Error("expected mock store when override is set")
	}
}

func TestGetKeychainStore_Default(t *testing.T) {
	old := KeychainStore
	KeychainStore = nil
	defer func() { KeychainStore = old }()

	got := GetKeychainStore()
	if got == nil {
		t.Error("expected non-nil store when override is nil")
	}
}

// --- Load tests ---

func TestLoad_MissingFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("Load() returned nil config")
	}
	if len(cfg.Profiles) != 0 {
		t.Errorf("expected empty profiles, got %d", len(cfg.Profiles))
	}
	if cfg.DefaultProfile != "" {
		t.Errorf("expected empty default-profile, got %q", cfg.DefaultProfile)
	}
}

func TestLoad_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	configDir := filepath.Join(dir, "jamfpro-cli")
	_ = os.MkdirAll(configDir, 0700)

	yaml := `default-profile: prod
profiles:
  prod:
    url: https://prod.jamfcloud.com
    auth-method: oauth2
    client-id: env:PROD_CLIENT_ID
    client-secret: keychain:prod/secret
  staging:
    url: https://staging.jamfcloud.com
    auth-method: token
    token: env:STAGING_TOKEN
`
	_ = os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(yaml), 0600)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.DefaultProfile != "prod" {
		t.Errorf("DefaultProfile = %q, want %q", cfg.DefaultProfile, "prod")
	}
	if len(cfg.Profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(cfg.Profiles))
	}

	prod := cfg.Profiles["prod"]
	if prod.URL != "https://prod.jamfcloud.com" {
		t.Errorf("prod.URL = %q, want %q", prod.URL, "https://prod.jamfcloud.com")
	}
	if prod.AuthMethod != "oauth2" {
		t.Errorf("prod.AuthMethod = %q, want %q", prod.AuthMethod, "oauth2")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	configDir := filepath.Join(dir, "jamfpro-cli")
	_ = os.MkdirAll(configDir, 0700)
	_ = os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("{{not valid yaml:::"), 0600)

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	if !strings.Contains(err.Error(), "parsing config") {
		t.Errorf("error = %q, want to contain 'parsing config'", err.Error())
	}
}

// --- Save tests ---

func TestSave_ReadOnlyDir(t *testing.T) {
	dir := t.TempDir()
	readOnlyDir := filepath.Join(dir, "readonly")
	_ = os.MkdirAll(readOnlyDir, 0500)
	t.Setenv("XDG_CONFIG_HOME", readOnlyDir)

	cfg := &Config{
		Profiles: map[string]Profile{
			"test": {URL: "https://test.com"},
		},
	}

	err := Save(cfg)
	if err == nil {
		t.Fatal("expected error writing to read-only directory")
	}
}

func TestLoad_NilProfiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	configDir := filepath.Join(dir, "jamfpro-cli")
	_ = os.MkdirAll(configDir, 0700)
	// YAML with no profiles key at all
	_ = os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("default-profile: test\n"), 0600)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	// Profiles map should be initialized even if missing from YAML
	if cfg.Profiles == nil {
		t.Fatal("expected non-nil Profiles map")
	}
}

func TestSave_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg := &Config{
		DefaultProfile: "test",
		Profiles: map[string]Profile{
			"test": {URL: "https://test.jamfcloud.com", AuthMethod: "token"},
		},
	}

	if err := Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	path := ConfigPath()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("config file not created: %v", err)
	}

	// Verify 0600 permissions
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}
}

func TestSave_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	original := &Config{
		DefaultProfile: "production",
		DefaultOutput:  "table",
		Profiles: map[string]Profile{
			"production": {
				URL:          "https://prod.jamfcloud.com",
				AuthMethod:   "oauth2",
				ClientID:     "env:PROD_ID",
				ClientSecret: "keychain:prod/secret",
			},
			"dev": {
				URL:        "https://dev.jamfcloud.com",
				AuthMethod: "token",
				Token:      "env:DEV_TOKEN",
			},
		},
	}

	if err := Save(original); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.DefaultProfile != original.DefaultProfile {
		t.Errorf("DefaultProfile = %q, want %q", loaded.DefaultProfile, original.DefaultProfile)
	}
	if loaded.DefaultOutput != original.DefaultOutput {
		t.Errorf("DefaultOutput = %q, want %q", loaded.DefaultOutput, original.DefaultOutput)
	}
	if len(loaded.Profiles) != len(original.Profiles) {
		t.Fatalf("profiles count = %d, want %d", len(loaded.Profiles), len(original.Profiles))
	}

	prod := loaded.Profiles["production"]
	if prod.URL != "https://prod.jamfcloud.com" {
		t.Errorf("prod.URL = %q, want %q", prod.URL, "https://prod.jamfcloud.com")
	}
	if prod.AuthMethod != "oauth2" {
		t.Errorf("prod.AuthMethod = %q, want %q", prod.AuthMethod, "oauth2")
	}
	if prod.ClientID != "env:PROD_ID" {
		t.Errorf("prod.ClientID = %q, want %q", prod.ClientID, "env:PROD_ID")
	}
	if prod.ClientSecret != "keychain:prod/secret" {
		t.Errorf("prod.ClientSecret = %q, want %q", prod.ClientSecret, "keychain:prod/secret")
	}

	dev := loaded.Profiles["dev"]
	if dev.Token != "env:DEV_TOKEN" {
		t.Errorf("dev.Token = %q, want %q", dev.Token, "env:DEV_TOKEN")
	}
}

// --- GetProfile tests ---

func TestGetProfile_ByName(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]Profile{
			"alpha": {URL: "https://alpha.example.com"},
			"beta":  {URL: "https://beta.example.com"},
		},
	}

	p, name, err := GetProfile(cfg, "beta")
	if err != nil {
		t.Fatalf("GetProfile() error = %v", err)
	}
	if name != "beta" {
		t.Errorf("name = %q, want %q", name, "beta")
	}
	if p.URL != "https://beta.example.com" {
		t.Errorf("URL = %q, want %q", p.URL, "https://beta.example.com")
	}
}

func TestGetProfile_Default(t *testing.T) {
	cfg := &Config{
		DefaultProfile: "alpha",
		Profiles: map[string]Profile{
			"alpha": {URL: "https://alpha.example.com"},
		},
	}

	p, name, err := GetProfile(cfg, "")
	if err != nil {
		t.Fatalf("GetProfile() error = %v", err)
	}
	if name != "alpha" {
		t.Errorf("name = %q, want %q", name, "alpha")
	}
	if p.URL != "https://alpha.example.com" {
		t.Errorf("URL = %q, want %q", p.URL, "https://alpha.example.com")
	}
}

func TestGetProfile_Missing(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]Profile{
			"alpha": {URL: "https://alpha.example.com"},
		},
	}

	_, _, err := GetProfile(cfg, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want to contain 'not found'", err.Error())
	}
}

func TestGetProfile_NoDefault(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]Profile{
			"alpha": {URL: "https://alpha.example.com"},
		},
	}

	_, _, err := GetProfile(cfg, "")
	if err == nil {
		t.Fatal("expected error when no default profile and empty name")
	}
	if !strings.Contains(err.Error(), "no profile specified") {
		t.Errorf("error = %q, want to contain 'no profile specified'", err.Error())
	}
}
