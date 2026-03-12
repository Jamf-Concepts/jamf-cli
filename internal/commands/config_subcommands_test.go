package commands

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jamf/jamfpro-cli/internal/config"
)

// --- config show tests ---

func TestConfigShow_ValidConfig(t *testing.T) {
	jDir := setupTempConfig(t)
	yaml := `default-profile: prod
profiles:
  prod:
    url: https://prod.jamfcloud.com
    auth-method: token
    token: env:MY_TOKEN
`
	os.WriteFile(filepath.Join(jDir, "config.yaml"), []byte(yaml), 0600)

	cmd := newConfigShowCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "# Config file:") {
		t.Error("expected config file header")
	}
	if !strings.Contains(out, "prod.jamfcloud.com") {
		t.Error("expected URL in output")
	}
}

func TestConfigShow_MissingFile(t *testing.T) {
	setupTempConfig(t)

	cmd := newConfigShowCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// Load returns empty config for missing file — no error
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- config path tests ---

func TestConfigPath_PrintsPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cmd := newConfigPathCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)

	cmd.Run(cmd, nil)

	out := strings.TrimSpace(buf.String())
	want := filepath.Join(dir, "jamfpro-cli", "config.yaml")
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

// --- config list tests ---

func TestConfigList_Empty(t *testing.T) {
	setupTempConfig(t)

	cmd := newConfigListCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(buf.String(), "No profiles configured") {
		t.Errorf("expected 'No profiles configured' message, got:\n%s", buf.String())
	}
}

func TestConfigList_WithProfiles(t *testing.T) {
	jDir := setupTempConfig(t)
	yaml := `default-profile: alpha
profiles:
  alpha:
    url: https://alpha.jamfcloud.com
    auth-method: token
  beta:
    url: https://beta.jamfcloud.com
    auth-method: oauth2
`
	os.WriteFile(filepath.Join(jDir, "config.yaml"), []byte(yaml), 0600)

	cmd := newConfigListCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	// Alpha should be marked as active (default)
	if !strings.Contains(out, "* alpha") {
		t.Error("expected alpha to be marked as default with *")
	}
	if !strings.Contains(out, "beta") {
		t.Error("expected beta in output")
	}
	if !strings.Contains(out, "alpha.jamfcloud.com") {
		t.Error("expected alpha URL in output")
	}
}

// --- config remove-profile tests ---

func TestConfigRemoveProfile_Exists(t *testing.T) {
	jDir := setupTempConfig(t)
	yaml := `default-profile: test
profiles:
  test:
    url: https://test.jamfcloud.com
    auth-method: token
    token: env:TOKEN
  other:
    url: https://other.jamfcloud.com
    auth-method: token
    token: env:TOKEN2
`
	os.WriteFile(filepath.Join(jDir, "config.yaml"), []byte(yaml), 0600)

	cmd := newConfigRemoveProfileCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"test"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(buf.String(), `Profile "test" removed`) {
		t.Errorf("expected removal confirmation, got:\n%s", buf.String())
	}

	// Verify config was updated
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	if _, ok := cfg.Profiles["test"]; ok {
		t.Error("expected 'test' profile to be removed")
	}
	if cfg.DefaultProfile != "" {
		t.Errorf("expected default-profile to be cleared, got %q", cfg.DefaultProfile)
	}
	if _, ok := cfg.Profiles["other"]; !ok {
		t.Error("expected 'other' profile to remain")
	}
}

func TestConfigRemoveProfile_NotFound(t *testing.T) {
	jDir := setupTempConfig(t)
	yaml := `profiles:
  existing:
    url: https://example.com
    auth-method: token
`
	os.WriteFile(filepath.Join(jDir, "config.yaml"), []byte(yaml), 0600)

	cmd := newConfigRemoveProfileCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"nonexistent"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent profile")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

// --- config set-default tests ---

func TestConfigSetDefault_Valid(t *testing.T) {
	jDir := setupTempConfig(t)
	yaml := `profiles:
  alpha:
    url: https://alpha.jamfcloud.com
    auth-method: token
  beta:
    url: https://beta.jamfcloud.com
    auth-method: token
`
	os.WriteFile(filepath.Join(jDir, "config.yaml"), []byte(yaml), 0600)

	cmd := newConfigSetDefaultCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"beta"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(buf.String(), `Default profile set to "beta"`) {
		t.Errorf("expected confirmation, got:\n%s", buf.String())
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	if cfg.DefaultProfile != "beta" {
		t.Errorf("default-profile = %q, want %q", cfg.DefaultProfile, "beta")
	}
}

func TestConfigSetDefault_NotFound(t *testing.T) {
	jDir := setupTempConfig(t)
	yaml := `profiles:
  alpha:
    url: https://alpha.jamfcloud.com
    auth-method: token
`
	os.WriteFile(filepath.Join(jDir, "config.yaml"), []byte(yaml), 0600)

	cmd := newConfigSetDefaultCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"nonexistent"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent profile")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

// --- checkHealth tests ---

func TestCheckHealth_Healthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthCheck.html" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]string{})
	}))
	defer srv.Close()

	result := checkHealth(srv.URL)
	if !result.Healthy {
		t.Error("expected healthy")
	}
	if result.Status != "ok" {
		t.Errorf("status = %q, want %q", result.Status, "ok")
	}
}

func TestCheckHealth_Unhealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]string{"DataLayer:UNHEALTHY"})
	}))
	defer srv.Close()

	result := checkHealth(srv.URL)
	if result.Healthy {
		t.Error("expected unhealthy")
	}
	if result.Status != "DataLayer:UNHEALTHY" {
		t.Errorf("status = %q, want %q", result.Status, "DataLayer:UNHEALTHY")
	}
}

func TestCheckHealth_ServerDown(t *testing.T) {
	result := checkHealth("http://127.0.0.1:1") // port 1 — will fail to connect
	if result.Healthy {
		t.Error("expected unhealthy for unreachable server")
	}
	if result.Status != "offline" {
		t.Errorf("status = %q, want %q", result.Status, "offline")
	}
}

func TestCheckHealth_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	result := checkHealth(srv.URL)
	if result.Healthy {
		t.Error("expected unhealthy for 503")
	}
	if result.Status != "HTTP 503" {
		t.Errorf("status = %q, want %q", result.Status, "HTTP 503")
	}
}

func TestCheckHealth_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	result := checkHealth(srv.URL)
	if result.Healthy {
		t.Error("expected unhealthy for invalid JSON")
	}
	if result.Status != "unknown" {
		t.Errorf("status = %q, want %q", result.Status, "unknown")
	}
}

// --- storeOrRefSecret tests ---

func TestStoreOrRefSecret_EmptyValue(t *testing.T) {
	var dest string
	err := storeOrRefSecret(newMockKeychainStore(), "prof", "field", "", &dest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dest != "" {
		t.Errorf("dest = %q, want empty", dest)
	}
}

func TestStoreOrRefSecret_EnvRef(t *testing.T) {
	var dest string
	err := storeOrRefSecret(newMockKeychainStore(), "prof", "field", "env:MY_VAR", &dest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dest != "env:MY_VAR" {
		t.Errorf("dest = %q, want %q", dest, "env:MY_VAR")
	}
}

func TestStoreOrRefSecret_FileRef(t *testing.T) {
	var dest string
	err := storeOrRefSecret(newMockKeychainStore(), "prof", "field", "file:/tmp/secret", &dest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dest != "file:/tmp/secret" {
		t.Errorf("dest = %q, want %q", dest, "file:/tmp/secret")
	}
}

func TestStoreOrRefSecret_BareValue(t *testing.T) {
	mock := newMockKeychainStore()
	var dest string
	err := storeOrRefSecret(mock, "myprof", "client-secret", "bare-secret-value", &dest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(dest, "keychain:") {
		t.Errorf("dest = %q, want keychain: prefix", dest)
	}
	// Verify the value is in the mock store
	key := "jamfpro-cli/myprof/client-secret"
	if v, ok := mock.items[key]; !ok || v != "bare-secret-value" {
		t.Errorf("keychain[%q] = %q, want %q", key, v, "bare-secret-value")
	}
}

func TestStoreOrRefSecret_KeychainFailure(t *testing.T) {
	var dest string
	err := storeOrRefSecret(&failingKeychainStore{}, "prof", "secret", "bare-value", &dest)
	if err == nil {
		t.Fatal("expected error for failing keychain")
	}
	if !strings.Contains(err.Error(), "keychain") {
		t.Errorf("expected keychain error, got: %v", err)
	}
}
