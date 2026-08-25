// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/keychain"
)

// runValidateCmd executes "config validate" with a temp config file and returns
// the combined output and any error.
func runValidateCmd(t *testing.T, yamlContent string) (string, error) {
	t.Helper()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if yamlContent != "" {
		if err := os.WriteFile(cfgPath, []byte(yamlContent), 0o600); err != nil {
			t.Fatalf("writing temp config: %v", err)
		}
	}

	// Point config at temp file
	t.Setenv("XDG_CONFIG_HOME", dir)
	// config.ConfigPath() uses XDG_CONFIG_HOME + "jamf-cli/config.yaml"
	jDir := filepath.Join(dir, "jamf-cli")
	if err := os.MkdirAll(jDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if yamlContent != "" {
		cfgPath = filepath.Join(jDir, "config.yaml")
		if err := os.WriteFile(cfgPath, []byte(yamlContent), 0o600); err != nil {
			t.Fatalf("writing temp config: %v", err)
		}
	}

	buf := &bytes.Buffer{}
	cmd := newConfigValidateCmd(newTestCtx(buf, "json"))
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.RunE(cmd, nil)
	return buf.String(), err
}

func TestConfigValidate_ValidConfig(t *testing.T) {
	t.Setenv("TEST_VALIDATE_CLIENT_ID", "my-client")
	t.Setenv("TEST_VALIDATE_CLIENT_SECRET", "my-secret")
	yaml := `
default-profile: prod
default-output: table
profiles:
  prod:
    url: https://jamf.example.com
    auth-method: oauth2
    client-id: "env:TEST_VALIDATE_CLIENT_ID"
    client-secret: "env:TEST_VALIDATE_CLIENT_SECRET"
`
	out, err := runValidateCmd(t, yaml)
	if err != nil {
		t.Fatalf("expected no error, got: %v\nOutput:\n%s", err, out)
	}
	if strings.Contains(out, `"fail"`) {
		t.Errorf("expected no failures, got:\n%s", out)
	}
}

func TestConfigValidate_MissingFile(t *testing.T) {
	// Don't write any file — just set XDG to an empty temp dir
	dir := t.TempDir()
	jDir := filepath.Join(dir, "jamf-cli")
	if err := os.MkdirAll(jDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)

	buf := &bytes.Buffer{}
	cmd := newConfigValidateCmd(newTestCtx(buf, "json"))
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error for missing config file")
		return
	}
	if !strings.Contains(buf.String(), "no file at") {
		t.Errorf("expected 'no file at' message in output:\n%s", buf.String())
	}
}

func TestConfigValidate_InvalidYAML(t *testing.T) {
	out, err := runValidateCmd(t, "{{not: yaml: at: all")
	if err == nil {
		t.Fatal("expected error for invalid YAML")
		return
	}
	if !strings.Contains(out, `"valid-yaml"`) || !strings.Contains(out, `"fail"`) {
		t.Errorf("expected valid-yaml failure in output:\n%s", out)
	}
}

func TestConfigValidate_InvalidOutputFormat(t *testing.T) {
	yaml := `
default-output: xml
profiles: {}
`
	out, err := runValidateCmd(t, yaml)
	if err == nil {
		t.Fatal("expected error for invalid output format")
		return
	}
	if !strings.Contains(out, `"default-output"`) || !strings.Contains(out, `"fail"`) {
		t.Errorf("expected default-output failure in output:\n%s", out)
	}
}

func TestConfigValidate_DefaultProfileMissing(t *testing.T) {
	t.Setenv("TEST_VALIDATE_TOKEN_DPM", "abc123")
	yaml := `
default-profile: ghost
profiles:
  prod:
    url: https://jamf.example.com
    auth-method: token
    token: "env:TEST_VALIDATE_TOKEN_DPM"
`
	out, err := runValidateCmd(t, yaml)
	if err == nil {
		t.Fatal("expected error for missing default profile")
		return
	}
	if !strings.Contains(out, "not found in profiles") {
		t.Errorf("expected 'not found in profiles' in output:\n%s", out)
	}
	if !strings.Contains(out, `"default-profile"`) {
		t.Errorf("expected default-profile key in output:\n%s", out)
	}
}

func TestConfigValidate_MissingURL(t *testing.T) {
	t.Setenv("TEST_VALIDATE_TOKEN_MU", "abc123")
	yaml := `
profiles:
  broken:
    auth-method: token
    token: "env:TEST_VALIDATE_TOKEN_MU"
`
	out, err := runValidateCmd(t, yaml)
	if err == nil {
		t.Fatal("expected error for missing URL")
		return
	}
	if !strings.Contains(out, `"url"`) || !strings.Contains(out, `"missing"`) {
		t.Errorf("expected missing url failure in output:\n%s", out)
	}
}

func TestConfigValidate_InvalidAuthMethod(t *testing.T) {
	yaml := `
profiles:
  broken:
    url: https://jamf.example.com
    auth-method: magic
`
	out, err := runValidateCmd(t, yaml)
	if err == nil {
		t.Fatal("expected error for invalid auth method")
		return
	}
	if !strings.Contains(out, `"auth-method"`) || !strings.Contains(out, `"invalid \"magic\""`) {
		t.Errorf("expected invalid auth-method failure in output:\n%s", out)
	}
}

func TestConfigValidate_OAuth2MissingCredentials(t *testing.T) {
	yaml := `
profiles:
  broken:
    url: https://jamf.example.com
    auth-method: oauth2
`
	out, err := runValidateCmd(t, yaml)
	if err == nil {
		t.Fatal("expected error for missing oauth2 credentials")
		return
	}
	if !strings.Contains(out, `"client-id"`) {
		t.Errorf("expected client-id check in output:\n%s", out)
	}
	if !strings.Contains(out, `"client-secret"`) {
		t.Errorf("expected client-secret check in output:\n%s", out)
	}
	if strings.Count(out, `"missing"`) < 2 {
		t.Errorf("expected both client-id and client-secret to be missing:\n%s", out)
	}
}

func TestConfigValidate_UnresolvableEnvSecret(t *testing.T) {
	yaml := `
profiles:
  broken:
    url: https://jamf.example.com
    auth-method: token
    token: "env:DEFINITELY_NOT_SET_12345"
`
	out, err := runValidateCmd(t, yaml)
	if err == nil {
		t.Fatal("expected error for unresolvable env secret")
		return
	}
	if !strings.Contains(out, "not resolvable") {
		t.Errorf("expected 'not resolvable' in output:\n%s", out)
	}
}

func TestConfigValidate_TokenProfile(t *testing.T) {
	t.Setenv("TEST_VALIDATE_TOKEN", "my-token-value")
	yaml := `
profiles:
  good:
    url: https://jamf.example.com
    auth-method: token
    token: "env:TEST_VALIDATE_TOKEN"
`
	out, err := runValidateCmd(t, yaml)
	if err != nil {
		t.Fatalf("expected no error, got: %v\nOutput:\n%s", err, out)
	}
	if !strings.Contains(out, `"name": "token"`) || !strings.Contains(out, `"pass"`) {
		t.Errorf("expected token pass in output:\n%s", out)
	}
}

func TestConfigValidate_LiteralTokenRejected(t *testing.T) {
	yaml := `
profiles:
  broken:
    url: https://jamf.example.com
    auth-method: token
    token: my-literal-token
`
	out, err := runValidateCmd(t, yaml)
	if err == nil {
		t.Fatal("expected error for literal token")
		return
	}
	if !strings.Contains(out, "not resolvable") {
		t.Errorf("expected 'not resolvable' in output:\n%s", out)
	}
}

// mockKeychainStore implements keychain.Store for testing.
type mockKeychainStore struct {
	items map[string]string
}

func newMockKeychainStore() *mockKeychainStore {
	return &mockKeychainStore{items: make(map[string]string)}
}

func (m *mockKeychainStore) Get(service, account string) (string, error) {
	key := service + "/" + account
	if v, ok := m.items[key]; ok {
		return v, nil
	}
	return "", keychain.ErrNotFound
}

func (m *mockKeychainStore) Set(service, account, secret string) error {
	key := service + "/" + account
	m.items[key] = secret
	return nil
}

func (m *mockKeychainStore) Delete(service, account string) error {
	key := service + "/" + account
	delete(m.items, key)
	return nil
}

func TestConfigValidate_KeychainSecret(t *testing.T) {
	mock := newMockKeychainStore()
	mock.items["jamf-cli/prod/client-secret"] = "resolved-secret"
	mock.items["jamf-cli/prod/client-id"] = "resolved-id"

	old := config.KeychainStore
	config.KeychainStore = mock
	defer func() { config.KeychainStore = old }()

	yaml := `
profiles:
  prod:
    url: https://jamf.example.com
    auth-method: oauth2
    client-id: "keychain:jamf-cli/prod/client-id"
    client-secret: "keychain:jamf-cli/prod/client-secret"
`
	out, err := runValidateCmd(t, yaml)
	if err != nil {
		t.Fatalf("expected no error, got: %v\nOutput:\n%s", err, out)
	}
	if !strings.Contains(out, `"client-id"`) || !strings.Contains(out, `"client-secret"`) {
		t.Errorf("expected client-id and client-secret checks in output:\n%s", out)
	}
	if strings.Contains(out, `"fail"`) {
		t.Errorf("expected no failures with keychain-backed secrets:\n%s", out)
	}
}

func TestConfigValidate_KeychainSecret_NotFound(t *testing.T) {
	mock := newMockKeychainStore()

	old := config.KeychainStore
	config.KeychainStore = mock
	defer func() { config.KeychainStore = old }()

	yaml := `
profiles:
  prod:
    url: https://jamf.example.com
    auth-method: oauth2
    client-id: "keychain:jamf-cli/prod/client-id"
    client-secret: "keychain:jamf-cli/prod/client-secret"
`
	out, err := runValidateCmd(t, yaml)
	if err == nil {
		t.Fatal("expected error for missing keychain items")
		return
	}
	if !strings.Contains(out, "not resolvable") {
		t.Errorf("expected 'not resolvable' in output:\n%s", out)
	}
}

// TestConfigValidate_PlatformSecurityCloudTenantOnly covers a platform profile
// scoped to Jamf Security Cloud alone.
//
// Security Cloud paths carry their own tenant, so reaching them needs no Jamf
// Pro tenant ID — every wire probe of that surface ran with a deliberately
// bogus one. Demanding tenant-id here would make `platform setup` a dead end
// for anyone who only has Security Cloud, forcing them to invent a value they
// never use.
// TestConfigValidate_PlatformNeedsATenant covers the one tenant a platform
// profile has to carry: without it the scope header is unset and the gateway
// answers 400 REQUEST_CONTEXT_NOT_PROVIDED for every request.
func TestConfigValidate_PlatformNeedsATenant(t *testing.T) {
	t.Setenv("TEST_NOTENANT_CLIENT_ID", "my-client")
	t.Setenv("TEST_NOTENANT_CLIENT_SECRET", "my-secret")
	yaml := `
default-profile: notenant
profiles:
  notenant:
    url: https://eu.apigw.jamf.com
    auth-method: platform
    client-id: "env:TEST_NOTENANT_CLIENT_ID"
    client-secret: "env:TEST_NOTENANT_CLIENT_SECRET"
`
	out, _ := runValidateCmd(t, yaml)
	if !strings.Contains(out, "tenant-id") || !strings.Contains(out, `"fail"`) {
		t.Errorf("a platform profile with no tenant should fail, got:\n%s", out)
	}
}
