package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runValidateCmd executes "config validate" with a temp config file and returns
// the combined output and any error.
func runValidateCmd(t *testing.T, yamlContent string) (string, error) {
	t.Helper()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if yamlContent != "" {
		if err := os.WriteFile(cfgPath, []byte(yamlContent), 0600); err != nil {
			t.Fatalf("writing temp config: %v", err)
		}
	}

	// Point config at temp file
	t.Setenv("XDG_CONFIG_HOME", dir)
	// config.ConfigPath() uses XDG_CONFIG_HOME + "jamfpro-cli/config.yaml"
	jDir := filepath.Join(dir, "jamfpro-cli")
	if err := os.MkdirAll(jDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if yamlContent != "" {
		cfgPath = filepath.Join(jDir, "config.yaml")
		if err := os.WriteFile(cfgPath, []byte(yamlContent), 0600); err != nil {
			t.Fatalf("writing temp config: %v", err)
		}
	}

	cmd := newConfigValidateCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.RunE(cmd, nil)
	return buf.String(), err
}

func TestConfigValidate_ValidConfig(t *testing.T) {
	yaml := `
default-profile: prod
default-output: table
profiles:
  prod:
    url: https://jamf.example.com
    auth-method: oauth2
    client-id: my-client
    client-secret: my-secret
`
	out, err := runValidateCmd(t, yaml)
	if err != nil {
		t.Fatalf("expected no error, got: %v\nOutput:\n%s", err, out)
	}
	if !strings.Contains(out, "All checks passed") {
		t.Errorf("expected 'All checks passed' in output:\n%s", out)
	}
}

func TestConfigValidate_MissingFile(t *testing.T) {
	// Don't write any file — just set XDG to an empty temp dir
	dir := t.TempDir()
	jDir := filepath.Join(dir, "jamfpro-cli")
	if err := os.MkdirAll(jDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)

	cmd := newConfigValidateCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error for missing config file")
	}
	if !strings.Contains(buf.String(), "does not exist") {
		t.Errorf("expected 'does not exist' in output:\n%s", buf.String())
	}
}

func TestConfigValidate_InvalidYAML(t *testing.T) {
	out, err := runValidateCmd(t, "{{not: yaml: at: all")
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	if !strings.Contains(out, "Invalid YAML") {
		t.Errorf("expected 'Invalid YAML' in output:\n%s", out)
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
	}
	if !strings.Contains(out, "Invalid default-output") {
		t.Errorf("expected 'Invalid default-output' in output:\n%s", out)
	}
}

func TestConfigValidate_DefaultProfileMissing(t *testing.T) {
	yaml := `
default-profile: ghost
profiles:
  prod:
    url: https://jamf.example.com
    auth-method: token
    token: abc123
`
	out, err := runValidateCmd(t, yaml)
	if err == nil {
		t.Fatal("expected error for missing default profile")
	}
	if !strings.Contains(out, "not found in profiles") {
		t.Errorf("expected 'not found in profiles' in output:\n%s", out)
	}
}

func TestConfigValidate_MissingURL(t *testing.T) {
	yaml := `
profiles:
  broken:
    auth-method: token
    token: abc123
`
	out, err := runValidateCmd(t, yaml)
	if err == nil {
		t.Fatal("expected error for missing URL")
	}
	if !strings.Contains(out, "Missing url") {
		t.Errorf("expected 'Missing url' in output:\n%s", out)
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
	}
	if !strings.Contains(out, "Invalid auth-method") {
		t.Errorf("expected 'Invalid auth-method' in output:\n%s", out)
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
	}
	if !strings.Contains(out, "Missing client-id") {
		t.Errorf("expected 'Missing client-id' in output:\n%s", out)
	}
	if !strings.Contains(out, "Missing client-secret") {
		t.Errorf("expected 'Missing client-secret' in output:\n%s", out)
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
	}
	if !strings.Contains(out, "not resolvable") {
		t.Errorf("expected 'not resolvable' in output:\n%s", out)
	}
}

func TestConfigValidate_TokenProfile(t *testing.T) {
	yaml := `
profiles:
  good:
    url: https://jamf.example.com
    auth-method: token
    token: my-literal-token
`
	out, err := runValidateCmd(t, yaml)
	if err != nil {
		t.Fatalf("expected no error, got: %v\nOutput:\n%s", err, out)
	}
	if !strings.Contains(out, "token resolvable") {
		t.Errorf("expected 'token resolvable' in output:\n%s", out)
	}
}
