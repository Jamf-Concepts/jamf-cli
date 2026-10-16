// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	return runValidateCmdWith(t, yamlContent, "json")
}

// runValidateCmdWith is runValidateCmd with an output format and flags, each
// flag given as name=value.
func runValidateCmdWith(t *testing.T, yamlContent, format string, flags ...string) (string, error) {
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
	cmd := newConfigValidateCmd(newTestCtx(buf, format))
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetContext(context.Background())
	for _, f := range flags {
		name, value, _ := strings.Cut(f, "=")
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatalf("setting --%s: %v", name, err)
		}
	}

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
	if !strings.Contains(out, `"name": "token",
    "status": "set"`) {
		t.Errorf("expected the token reported set in output:\n%s", out)
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
// TestConfigValidate_PlatformScopeLevels covers the three levels an integration
// can be created at. A platform profile naming neither ID is organization-scoped
// and valid — the gateway resolves it from the access token — so the only fault
// is naming both, where the credential can only match one.
func TestConfigValidate_PlatformScopeLevels(t *testing.T) {
	cases := []struct {
		name     string
		scope    string
		wantFail bool
		wantText string
	}{
		{name: "environment", scope: "    environment-id: env-1", wantText: "environment-id"},
		{name: "tenant", scope: "    tenant-id: ten-1", wantText: "tenant-id"},
		{name: "organization", scope: "", wantText: "organization-scoped"},
		{
			name:     "both levels",
			scope:    "    environment-id: env-1\n    tenant-id: ten-1",
			wantFail: true,
			wantText: "one level",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TEST_SCOPE_CLIENT_ID", "my-client")
			t.Setenv("TEST_SCOPE_CLIENT_SECRET", "my-secret")
			yaml := `
default-profile: scoped
profiles:
  scoped:
    url: https://eu.api.jamfcloud.com
    auth-method: platform
    client-id: "env:TEST_SCOPE_CLIENT_ID"
    client-secret: "env:TEST_SCOPE_CLIENT_SECRET"
` + tc.scope + "\n"

			out, _ := runValidateCmd(t, yaml)
			if got := strings.Contains(out, `"fail"`); got != tc.wantFail {
				t.Errorf("fail=%v, want %v; output:\n%s", got, tc.wantFail, out)
			}
			if !strings.Contains(out, tc.wantText) {
				t.Errorf("output missing %q:\n%s", tc.wantText, out)
			}
		})
	}
}

// TestConfigValidateFlagsTheRetiredGateway pins the base-URL migration being
// reported per profile.
//
// `config validate` is the command that answers "what is wrong with my config",
// and every platform profile written before 2026-08-28 names
// {region}.apigw.jamf.com — a host that is retired and that required an /api
// segment the GA gateway does not serve. An operator with several such profiles
// wants all of them named at once, not one failed command at a time.
func TestConfigValidateFlagsTheRetiredGateway(t *testing.T) {
	t.Setenv("TEST_RETIRED_CLIENT_ID", "my-client")
	t.Setenv("TEST_RETIRED_CLIENT_SECRET", "my-secret")
	yaml := `
default-profile: ga
profiles:
  ga:
    url: https://eu.api.jamfcloud.com
    auth-method: platform
    client-id: "env:TEST_RETIRED_CLIENT_ID"
    client-secret: "env:TEST_RETIRED_CLIENT_SECRET"
    tenant-id: t
  old:
    url: https://us.apigw.jamf.com
    auth-method: platform
    client-id: "env:TEST_RETIRED_CLIENT_ID"
    client-secret: "env:TEST_RETIRED_CLIENT_SECRET"
    tenant-id: t
`
	out, _ := runValidateCmd(t, yaml)
	if !strings.Contains(out, "retired Jamf Platform gateway") {
		t.Errorf("the retired host was not reported:\n%s", out)
	}
	// The replacement URL has to be in the message — naming the problem without
	// the fix leaves the operator to guess the new host.
	if !strings.Contains(out, "https://us.api.jamfcloud.com") {
		t.Errorf("the message does not name the replacement URL:\n%s", out)
	}
	// And the GA profile must not be swept up in it.
	if strings.Count(out, "retired Jamf Platform gateway") != 1 {
		t.Errorf("expected exactly one profile flagged, got:\n%s", out)
	}
}

// validateRows decodes config validate's JSON output.
func validateRows(t *testing.T, out string) []validateCheck {
	t.Helper()
	var rows []validateCheck
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("decoding output: %v\n%s", err, out)
	}
	return rows
}

// statusOf returns the status of the named row in a profile, or "" when the
// row is absent.
func statusOf(rows []validateCheck, scope, name string) string {
	for _, r := range rows {
		if r.Scope == scope && r.Name == name {
			return r.Status
		}
	}
	return ""
}

// messageOf returns the message of the named row in a profile.
func messageOf(rows []validateCheck, scope, name string) string {
	for _, r := range rows {
		if r.Scope == scope && r.Name == name {
			return r.Message
		}
	}
	return ""
}

// A credential row that only resolved its reference reports "set", never
// "pass" (#439): the keychain can hold a secret since rotated on the server,
// and "pass" beside rows that were genuinely checked read as "it works". Every
// credential row is covered: oauth2, platform, token, and Security Cloud's.
func TestConfigValidate_CredentialRowsReportSetNotPass(t *testing.T) {
	t.Setenv("TEST_SET_ID", "id")
	t.Setenv("TEST_SET_SECRET", "secret")
	yaml := `
profiles:
  inst:
    url: https://jamf.example.com
    auth-method: oauth2
    client-id: "env:TEST_SET_ID"
    client-secret: "env:TEST_SET_SECRET"
  gw:
    url: https://eu.api.jamfcloud.com
    auth-method: platform
    client-id: "env:TEST_SET_ID"
    client-secret: "env:TEST_SET_SECRET"
    environment-id: env-1
  tok:
    url: https://jamf.example.com
    auth-method: token
    token: "env:TEST_SET_SECRET"
  jsc:
    product: security
    auth-method: security
    risk-client-id: risk
`
	out, err := runValidateCmd(t, yaml)
	if err != nil {
		t.Fatalf("validate: %v\n%s", err, out)
	}
	rows := validateRows(t, out)
	for _, want := range []struct{ scope, name string }{
		{"inst", "client-id"},
		{"inst", "client-secret"},
		{"gw", "client-id"},
		{"gw", "client-secret"},
		{"tok", "token"},
		{"jsc", "credentials"},
	} {
		if got := statusOf(rows, want.scope, want.name); got != "set" {
			t.Errorf("%s %s: status = %q, want \"set\"", want.scope, want.name, got)
		}
	}
	// No token exchange happened, so nothing may claim one did.
	for _, r := range rows {
		if r.Name == "auth" {
			t.Errorf("an auth row was reported without --auth: %+v", r)
		}
	}
}

// A table takes its columns from the first row, and the first row
// (file-exists) never carries a message, so the table used to drop the column
// and the reason for every failure with it.
func TestConfigValidate_TableShowsTheFailureReason(t *testing.T) {
	yaml := `
profiles:
  a:
    url: https://jamf.example.com
    auth-method: oauth2
    client-id: "env:TEST_TABLE_UNSET_ID"
    client-secret: "env:TEST_TABLE_UNSET_SECRET"
`
	out, err := runValidateCmdWith(t, yaml, "table")
	if err == nil {
		t.Fatalf("expected a failure for unresolvable credentials:\n%s", out)
	}
	if !strings.Contains(out, "TEST_TABLE_UNSET_ID") {
		t.Errorf("the table does not say why client-id failed:\n%s", out)
	}
}

// JSON keeps the omitempty shape: a row with no message carries no message key,
// which is what a script already parsing this output reads.
func TestConfigValidate_JSONOmitsAnEmptyMessage(t *testing.T) {
	out, err := runValidateCmd(t, "profiles: {}\n")
	if err != nil {
		t.Fatalf("validate: %v\n%s", err, out)
	}
	if strings.Contains(out, `"message"`) {
		t.Errorf("an empty message was emitted in JSON:\n%s", out)
	}
}

// tokenServer answers the token endpoint at path with a token for wantSecret
// and 401 invalid_client for anything else, and counts requests.
func tokenServer(t *testing.T, path, wantSecret string) (*httptest.Server, *int) {
	t.Helper()
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		_ = r.ParseForm()
		if r.PostForm.Get("client_secret") != wantSecret {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprint(w, `{"error":"invalid_client"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"t","token_type":"Bearer","expires_in":900}`)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// --auth is what earns a credential a "pass": one exchange per profile, on a
// row of its own, failing with the server's answer when the secret is stale —
// the rotated-secret case #439 was reported from.
func TestConfigValidate_AuthExchangesEachProfile(t *testing.T) {
	inst, _ := tokenServer(t, "/api/oauth/token", "current")
	gw, _ := tokenServer(t, "/auth/token", "current")
	t.Setenv("TEST_AUTH_ID", "id")
	t.Setenv("TEST_AUTH_CURRENT", "current")
	t.Setenv("TEST_AUTH_ROTATED", "rotated")
	yaml := fmt.Sprintf(`
profiles:
  good:
    url: %[1]s
    auth-method: oauth2
    client-id: "env:TEST_AUTH_ID"
    client-secret: "env:TEST_AUTH_CURRENT"
  stale:
    url: %[1]s
    auth-method: oauth2
    client-id: "env:TEST_AUTH_ID"
    client-secret: "env:TEST_AUTH_ROTATED"
  gw:
    url: %[2]s
    auth-method: platform
    client-id: "env:TEST_AUTH_ID"
    client-secret: "env:TEST_AUTH_CURRENT"
    environment-id: env-1
  gw-refused:
    url: %[2]s
    auth-method: platform
    client-id: "env:TEST_AUTH_ID"
    client-secret: "env:TEST_AUTH_ROTATED"
    environment-id: env-1
`, inst.URL, gw.URL)

	out, err := runValidateCmdWith(t, yaml, "json", "auth=true")
	if err == nil {
		t.Fatalf("a stale secret must fail validation:\n%s", out)
	}
	rows := validateRows(t, out)
	for scope, want := range map[string]string{"good": "pass", "stale": "fail", "gw": "pass", "gw-refused": "fail"} {
		if got := statusOf(rows, scope, "auth"); got != want {
			t.Errorf("%s auth: status = %q, want %q", scope, got, want)
		}
	}
	// The gateway answers a wrong region exactly as it answers a wrong
	// secret, so a refused platform profile names the region among the
	// causes rather than "verify your client-secret" alone. A Jamf Pro
	// instance has no regions, so its refusal keeps the plain message.
	if got := messageOf(rows, "gw-refused", "auth"); got != invalidPlatformClientHint {
		t.Errorf("gw-refused auth message = %q, want the invalid-client hint", got)
	}
	if got := messageOf(rows, "stale", "auth"); strings.Contains(got, "region") {
		t.Errorf("an instance refusal mentions regions: %q", got)
	}
	// The credential rows themselves still only say "set": the exchange is
	// reported where it happened, not folded into them.
	if got := statusOf(rows, "good", "client-secret"); got != "set" {
		t.Errorf("good client-secret: status = %q, want \"set\"", got)
	}
}

// A credential that did not resolve has already failed its own row, so --auth
// sends nothing for it rather than exchanging an empty secret.
func TestConfigValidate_AuthSkipsAnUnresolvedCredential(t *testing.T) {
	srv, calls := tokenServer(t, "/api/oauth/token", "x")
	t.Setenv("TEST_SKIP_ID", "id")
	yaml := fmt.Sprintf(`
profiles:
  a:
    url: %s
    auth-method: oauth2
    client-id: "env:TEST_SKIP_ID"
    client-secret: "env:TEST_SKIP_UNSET_SECRET"
`, srv.URL)

	out, _ := runValidateCmdWith(t, yaml, "json", "auth=true")
	if *calls != 0 {
		t.Errorf("--auth sent %d request(s) for an unresolved secret", *calls)
	}
	if got := statusOf(validateRows(t, out), "a", "auth"); got != "" {
		t.Errorf("auth row reported for an unresolved credential: %q", got)
	}
}
