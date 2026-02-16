package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/ktn-jamf/jamfpro-cli/internal/config"
)

func TestWarnPlaintextSecrets_Plaintext(t *testing.T) {
	cmd := &cobra.Command{}
	stderr := &bytes.Buffer{}
	cmd.SetErr(stderr)

	p := &config.Profile{
		URL:          "https://example.com",
		AuthMethod:   "oauth2",
		ClientSecret: "my-literal-secret",
	}

	warnPlaintextSecrets(cmd, "prod", p)

	out := stderr.String()
	if !strings.Contains(out, "Warning:") {
		t.Errorf("expected warning, got: %s", out)
	}
	if !strings.Contains(out, "client-secret") {
		t.Errorf("expected field name in warning, got: %s", out)
	}
	if !strings.Contains(out, "prod") {
		t.Errorf("expected profile name in warning, got: %s", out)
	}
}

func TestWarnPlaintextSecrets_SecureRef(t *testing.T) {
	cmd := &cobra.Command{}
	stderr := &bytes.Buffer{}
	cmd.SetErr(stderr)

	p := &config.Profile{
		URL:          "https://example.com",
		AuthMethod:   "oauth2",
		ClientSecret: "keychain:jamfpro-cli/prod/client-secret",
	}

	warnPlaintextSecrets(cmd, "prod", p)

	if stderr.Len() != 0 {
		t.Errorf("expected no warning for keychain ref, got: %s", stderr.String())
	}
}

func TestWarnPlaintextSecrets_MultipleFields(t *testing.T) {
	cmd := &cobra.Command{}
	stderr := &bytes.Buffer{}
	cmd.SetErr(stderr)

	p := &config.Profile{
		URL:          "https://example.com",
		AuthMethod:   "basic",
		Password:     "plaintext-pass",
		Token:        "plaintext-token",
		ClientSecret: "keychain:ref",
	}

	warnPlaintextSecrets(cmd, "test", p)

	out := stderr.String()
	if !strings.Contains(out, "password") {
		t.Errorf("expected password in warning, got: %s", out)
	}
	if !strings.Contains(out, "token") {
		t.Errorf("expected token in warning, got: %s", out)
	}
}

func TestWarnPlaintextSecrets_Empty(t *testing.T) {
	cmd := &cobra.Command{}
	stderr := &bytes.Buffer{}
	cmd.SetErr(stderr)

	p := &config.Profile{
		URL:        "https://example.com",
		AuthMethod: "token",
	}

	warnPlaintextSecrets(cmd, "empty", p)

	if stderr.Len() != 0 {
		t.Errorf("expected no warning for empty fields, got: %s", stderr.String())
	}
}

func TestWarnFlagSecrets_NoFlags(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("token", "", "")
	cmd.Flags().String("client-secret", "", "")
	cmd.Flags().String("password", "", "")
	stderr := &bytes.Buffer{}
	cmd.SetErr(stderr)

	warnFlagSecrets(cmd)

	if stderr.Len() != 0 {
		t.Errorf("expected no warning when flags not set, got: %s", stderr.String())
	}
}

func TestWarnFlagSecrets_TokenSet(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("token", "", "")
	cmd.Flags().String("client-secret", "", "")
	cmd.Flags().String("password", "", "")
	stderr := &bytes.Buffer{}
	cmd.SetErr(stderr)

	// Simulate flag being set on command line
	cmd.Flags().Set("token", "my-secret-token")

	warnFlagSecrets(cmd)

	out := stderr.String()
	if !strings.Contains(out, "--token") {
		t.Errorf("expected --token in warning, got: %s", out)
	}
	if !strings.Contains(out, "process listings") {
		t.Errorf("expected process listing warning, got: %s", out)
	}
}

func TestWarnFlagSecrets_MultipleFlags(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("token", "", "")
	cmd.Flags().String("client-secret", "", "")
	cmd.Flags().String("password", "", "")
	stderr := &bytes.Buffer{}
	cmd.SetErr(stderr)

	cmd.Flags().Set("client-secret", "secret")
	cmd.Flags().Set("password", "pass")

	warnFlagSecrets(cmd)

	out := stderr.String()
	if !strings.Contains(out, "--client-secret") {
		t.Errorf("expected --client-secret in warning, got: %s", out)
	}
	if !strings.Contains(out, "--password") {
		t.Errorf("expected --password in warning, got: %s", out)
	}
}
