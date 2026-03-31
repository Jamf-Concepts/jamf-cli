package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
)

func TestCommandsSubcommand_JSON(t *testing.T) {
	// Reset global state
	outputFmt = "json"
	noColor = true
	wide = false

	root := NewRootCmd("test", "abc123", "2024-01-01")

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"commands", "-o", "json"})

	if err := root.Execute(); err != nil {
		t.Fatalf("commands subcommand failed: %v", err)
	}

	// The output goes to stdout via the formatter, capture it
	// Since the formatter writes to os.Stdout, we need to check
	// that the command ran without error. For a proper output check,
	// we'll verify the collectCommands logic directly.
}

func TestCollectCommands(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01")
	entries := collectCommands(root, "")

	if len(entries) == 0 {
		t.Fatal("expected at least one command entry")
	}

	// Verify version command is present (it has a Run func)
	found := false
	for _, e := range entries {
		if e.Command == "version" {
			found = true
			if e.Description != "Print version information" {
				t.Errorf("version description = %q, want %q", e.Description, "Print version information")
			}
			break
		}
	}
	if !found {
		t.Error("expected 'version' command in entries")
	}

	// Verify a generated command like "pro computers list" is present
	found = false
	for _, e := range entries {
		if e.Command == "pro computers list" {
			found = true
			if len(e.Aliases) == 0 {
				t.Error("expected pro computers list to have aliases (e.g., 'comp')")
			}
			if len(e.Flags) == 0 {
				t.Error("expected pro computers list to have flags")
			}
			break
		}
	}
	if !found {
		t.Error("expected 'pro computers list' command in entries")
	}

	// Verify 'commands' and 'help' are excluded
	for _, e := range entries {
		if e.Command == "commands" {
			t.Error("'commands' should be excluded from its own output")
		}
		if e.Command == "help" {
			t.Error("'help' should be excluded from output")
		}
	}
}

func TestCommandEntriesToMaps_Full(t *testing.T) {
	entries := []commandEntry{
		{
			Command:     "computers list",
			Description: "List computers",
			Aliases:     []string{"comp"},
			Flags:       []string{"--page", "--sort"},
		},
		{
			Command:     "version",
			Description: "Print version",
		},
	}

	maps := commandEntriesToMaps(entries, true)
	if len(maps) != 2 {
		t.Fatalf("expected 2 maps, got %d", len(maps))
	}

	if maps[0]["command"] != "computers list" {
		t.Errorf("command = %q, want %q", maps[0]["command"], "computers list")
	}
	if maps[0]["aliases"] != "comp" {
		t.Errorf("aliases = %q, want %q", maps[0]["aliases"], "comp")
	}
	if maps[0]["flags"] != "--page, --sort" {
		t.Errorf("flags = %q, want %q", maps[0]["flags"], "--page, --sort")
	}

	// Entry without aliases/flags should have empty strings
	if maps[1]["aliases"] != "" {
		t.Errorf("version aliases = %q, want empty", maps[1]["aliases"])
	}
	if maps[1]["flags"] != "" {
		t.Errorf("version flags = %q, want empty", maps[1]["flags"])
	}
}

func TestCommandEntriesToMaps_Compact(t *testing.T) {
	entries := []commandEntry{
		{
			Command:     "computers list",
			Description: "List computers",
			Aliases:     []string{"comp"},
			Flags:       []string{"--page"},
		},
	}

	maps := commandEntriesToMaps(entries, false)

	if maps[0]["command"] != "computers list" {
		t.Errorf("command = %q, want %q", maps[0]["command"], "computers list")
	}
	if _, ok := maps[0]["aliases"]; ok {
		t.Error("compact mode should not include aliases key")
	}
	if _, ok := maps[0]["flags"]; ok {
		t.Error("compact mode should not include flags key")
	}
}

func TestFormatError_JSON(t *testing.T) {
	// Set outputFmt to json
	outputFmt = "json"

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := exitcode.New(exitcode.Authentication, "authentication failed (HTTP 401)")
	handled := FormatError(err)

	_ = w.Close()
	os.Stdout = oldStdout

	if !handled {
		t.Fatal("FormatError should return true for json format")
	}

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	var envelope map[string]any
	if jsonErr := json.Unmarshal(buf.Bytes(), &envelope); jsonErr != nil {
		t.Fatalf("failed to parse JSON output: %v\nraw: %s", jsonErr, buf.String())
	}

	if envelope["error"] != "authentication" {
		t.Errorf("error = %q, want %q", envelope["error"], "authentication")
	}
	if envelope["message"] != "authentication failed (HTTP 401)" {
		t.Errorf("message = %q, want %q", envelope["message"], "authentication failed (HTTP 401)")
	}
	// JSON numbers decode as float64
	if envelope["exitCode"] != float64(3) {
		t.Errorf("exitCode = %v, want %v", envelope["exitCode"], 3)
	}
}

func TestFormatError_NonJSON(t *testing.T) {
	outputFmt = "table"
	err := exitcode.New(exitcode.General, "something broke")
	if FormatError(err) {
		t.Error("FormatError should return false for non-json format")
	}
}

// mockHTTPClient records whether Do was called and with what method.
type mockHTTPClient struct {
	called bool
	method string
	path   string
}

func (m *mockHTTPClient) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	m.called = true
	m.method = method
	m.path = path
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"id":1}`)),
		Header:     make(http.Header),
	}, nil
}

func TestDryRunClient_GET_PassesThrough(t *testing.T) {
	mock := &mockHTTPClient{}
	client := &dryRunClient{inner: mock}

	resp, err := client.Do(context.Background(), "GET", "/v1/computers", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mock.called {
		t.Fatal("GET should pass through to inner client")
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestDryRunClient_HEAD_PassesThrough(t *testing.T) {
	mock := &mockHTTPClient{}
	client := &dryRunClient{inner: mock}

	_, err := client.Do(context.Background(), "HEAD", "/v1/computers", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mock.called {
		t.Fatal("HEAD should pass through to inner client")
	}
}

func TestDryRunClient_POST_Intercepted(t *testing.T) {
	mock := &mockHTTPClient{}
	client := &dryRunClient{inner: mock}

	body := strings.NewReader(`{"name":"Test"}`)
	resp, err := client.Do(context.Background(), "POST", "/v1/categories", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.called {
		t.Fatal("POST should NOT pass through to inner client")
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	// Verify synthetic body is valid JSON
	data, _ := io.ReadAll(resp.Body)
	if string(data) != "{}" {
		t.Errorf("body = %q, want %q", string(data), "{}")
	}
}

func TestDryRunClient_PUT_Intercepted(t *testing.T) {
	mock := &mockHTTPClient{}
	client := &dryRunClient{inner: mock}

	_, err := client.Do(context.Background(), "PUT", "/v1/categories/1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.called {
		t.Fatal("PUT should NOT pass through to inner client")
	}
}

func TestDryRunClient_PATCH_Intercepted(t *testing.T) {
	mock := &mockHTTPClient{}
	client := &dryRunClient{inner: mock}

	_, err := client.Do(context.Background(), "PATCH", "/v1/categories/1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.called {
		t.Fatal("PATCH should NOT pass through to inner client")
	}
}

func TestDryRunClient_DELETE_Intercepted(t *testing.T) {
	mock := &mockHTTPClient{}
	client := &dryRunClient{inner: mock}

	_, err := client.Do(context.Background(), "DELETE", "/v1/categories/1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.called {
		t.Fatal("DELETE should NOT pass through to inner client")
	}
}

func TestDryRunClient_StderrOutput(t *testing.T) {
	mock := &mockHTTPClient{}
	client := &dryRunClient{inner: mock}

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	body := strings.NewReader(`{"name":"Test"}`)
	_, _ = client.Do(context.Background(), "POST", "/v1/categories", body)

	_ = w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "[dry-run] POST /v1/categories") {
		t.Errorf("expected dry-run method/path in stderr, got: %s", output)
	}
	if !strings.Contains(output, `{"name":"Test"}`) {
		t.Errorf("expected request body in stderr, got: %s", output)
	}
}

// resetGlobals resets all package-level flag variables to zero values.
// Must be called before each PersistentPreRunE test to avoid state leaking.
func resetGlobals() {
	profile = ""
	outputFmt = "json"
	quiet = false
	verbose = false
	noInput = false
	noColor = false
	dryRun = false
	wide = false
	outFile = ""
	fieldName = ""
	serverURL = ""
	token = ""
	tokenFile = ""
	tokenStdin = false
	clientID = ""
	clientSecret = ""
}

// clearAuthEnv clears all auth-related env vars so tests start from a clean slate.
func clearAuthEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"JAMF_URL", "JAMF_TOKEN", "JAMF_CLIENT_ID", "JAMF_CLIENT_SECRET",
		"JAMF_PROFILE",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

func TestPersistentPreRunE_MissingURL(t *testing.T) {
	resetGlobals()
	// Clear any env vars that could provide a URL
	t.Setenv("JAMF_URL", "")
	t.Setenv("JAMF_TOKEN", "my-token")
	t.Setenv("JAMF_PROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // empty config dir — no profiles

	root := NewRootCmd("test", "abc123", "2024-01-01")
	root.SetArgs([]string{"pro", "computers", "list"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when URL is missing")
	}
	if !strings.Contains(err.Error(), "server URL is required") {
		t.Errorf("error = %q, want to contain 'server URL is required'", err.Error())
	}
}

func TestPersistentPreRunE_MissingAuth(t *testing.T) {
	resetGlobals()
	t.Setenv("JAMF_URL", "https://test.jamfcloud.com")
	t.Setenv("JAMF_TOKEN", "")
	t.Setenv("JAMF_CLIENT_ID", "")
	t.Setenv("JAMF_CLIENT_SECRET", "")
	t.Setenv("JAMF_PROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	root := NewRootCmd("test", "abc123", "2024-01-01")
	root.SetArgs([]string{"pro", "computers", "list"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when auth is missing")
	}
	if !strings.Contains(err.Error(), "authentication required") {
		t.Errorf("error = %q, want to contain 'authentication required'", err.Error())
	}
}

func TestPersistentPreRunE_PartialOAuth2_MissingSecret(t *testing.T) {
	resetGlobals()
	t.Setenv("JAMF_URL", "https://test.jamfcloud.com")
	t.Setenv("JAMF_TOKEN", "")
	t.Setenv("JAMF_CLIENT_ID", "my-client-id")
	t.Setenv("JAMF_CLIENT_SECRET", "")
	t.Setenv("JAMF_PROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	root := NewRootCmd("test", "abc123", "2024-01-01")
	root.SetArgs([]string{"pro", "computers", "list"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for partial OAuth2 credentials")
	}
	if !strings.Contains(err.Error(), "--client-secret is required") {
		t.Errorf("error = %q, want to contain '--client-secret is required'", err.Error())
	}
}

func TestPersistentPreRunE_PartialOAuth2_MissingID(t *testing.T) {
	resetGlobals()
	t.Setenv("JAMF_URL", "https://test.jamfcloud.com")
	t.Setenv("JAMF_TOKEN", "")
	t.Setenv("JAMF_CLIENT_ID", "")
	t.Setenv("JAMF_CLIENT_SECRET", "my-secret")
	t.Setenv("JAMF_PROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	root := NewRootCmd("test", "abc123", "2024-01-01")
	root.SetArgs([]string{"pro", "computers", "list"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for partial OAuth2 credentials")
	}
	if !strings.Contains(err.Error(), "--client-id is required") {
		t.Errorf("error = %q, want to contain '--client-id is required'", err.Error())
	}
}

func TestPersistentPreRunE_SkipsForConfigCommand(t *testing.T) {
	resetGlobals()
	// No URL, no auth — but config commands should skip validation
	t.Setenv("JAMF_URL", "")
	t.Setenv("JAMF_TOKEN", "")
	t.Setenv("JAMF_PROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	root := NewRootCmd("test", "abc123", "2024-01-01")
	root.SetArgs([]string{"config", "validate"})

	err := root.Execute()
	// config validate may fail for its own reasons, but not from PersistentPreRunE
	if err != nil && strings.Contains(err.Error(), "server URL is required") {
		t.Error("config commands should skip PersistentPreRunE validation")
	}
}

func TestPersistentPreRunE_SkipsForVersionCommand(t *testing.T) {
	resetGlobals()
	t.Setenv("JAMF_URL", "")
	t.Setenv("JAMF_TOKEN", "")
	t.Setenv("JAMF_PROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	root := NewRootCmd("test", "abc123", "2024-01-01")
	root.SetArgs([]string{"version"})

	err := root.Execute()
	if err != nil {
		t.Errorf("version command should not require auth, got: %v", err)
	}
}

// --- --field flag (cliOutput.PrintRaw) tests ---

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = oldStdout })

	fn()

	_ = w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	return buf.String()
}

func TestCLIOutputPrintRaw_FieldExtractArray(t *testing.T) {
	fieldName = "id"
	t.Cleanup(func() { fieldName = "" })

	formatter := output.New("json", true, false)
	o := &cliOutput{formatter}

	out := captureStdout(t, func() {
		err := o.PrintRaw([]byte(`[{"id":1,"name":"A"},{"id":2,"name":"B"}]`))
		if err != nil {
			t.Fatalf("PrintRaw error: %v", err)
		}
	})

	if out != "1\n2\n" {
		t.Errorf("output = %q, want %q", out, "1\n2\n")
	}
}

func TestCLIOutputPrintRaw_FieldExtractSingleObject(t *testing.T) {
	fieldName = "name"
	t.Cleanup(func() { fieldName = "" })

	formatter := output.New("json", true, false)
	o := &cliOutput{formatter}

	out := captureStdout(t, func() {
		err := o.PrintRaw([]byte(`{"id":1,"name":"HQ"}`))
		if err != nil {
			t.Fatalf("PrintRaw error: %v", err)
		}
	})

	if out != "HQ\n" {
		t.Errorf("output = %q, want %q", out, "HQ\n")
	}
}

func TestCLIOutputPrintRaw_FieldNotSet(t *testing.T) {
	fieldName = ""

	var buf bytes.Buffer
	formatter := output.New("json", true, false)
	formatter.SetWriter(&buf)
	o := &cliOutput{formatter}

	err := o.PrintRaw([]byte(`{"id":1}`))
	if err != nil {
		t.Fatalf("PrintRaw error: %v", err)
	}

	// Should delegate to Formatter.PrintRaw (JSON pretty-print)
	if !strings.Contains(buf.String(), `"id"`) {
		t.Errorf("expected JSON output, got: %s", buf.String())
	}
}

func TestCLIOutputPrintRaw_FieldNotFound(t *testing.T) {
	fieldName = "bogus"
	t.Cleanup(func() { fieldName = "" })

	formatter := output.New("json", true, false)
	o := &cliOutput{formatter}

	out := captureStdout(t, func() {
		err := o.PrintRaw([]byte(`[{"id":1},{"id":2}]`))
		if err != nil {
			t.Fatalf("PrintRaw error: %v", err)
		}
	})

	if out != "" {
		t.Errorf("output = %q, want empty", out)
	}
}

func TestCLIOutputPrintRaw_NonJSON(t *testing.T) {
	fieldName = "id"
	t.Cleanup(func() { fieldName = "" })

	formatter := output.New("json", true, false)
	o := &cliOutput{formatter}

	// Use plain text that is neither valid JSON nor valid XML.
	err := o.PrintRaw([]byte(`this is plain text, not json or xml`))
	if err == nil {
		t.Fatal("expected error for non-JSON input")
	}
	if !strings.Contains(err.Error(), "cannot extract field from non-JSON response") {
		t.Errorf("error = %q, want to contain 'cannot extract field from non-JSON response'", err.Error())
	}
}

func TestCLIOutputPrintRaw_ScalarJSON(t *testing.T) {
	fieldName = "id"
	t.Cleanup(func() { fieldName = "" })

	formatter := output.New("json", true, false)
	o := &cliOutput{formatter}

	err := o.PrintRaw([]byte(`"hello"`))
	if err == nil {
		t.Fatal("expected error for scalar JSON")
	}
	if !strings.Contains(err.Error(), "cannot extract field") {
		t.Errorf("error = %q, want to contain 'cannot extract field'", err.Error())
	}
}

// --- resolveAuth tests ---

func TestResolveAuth_EnvCredentials(t *testing.T) {
	tests := []struct {
		name   string
		envs   map[string]string
		wantOK bool
	}{
		{
			name:   "token auth",
			envs:   map[string]string{"JAMF_URL": "https://test.jamfcloud.com", "JAMF_TOKEN": "my-token"},
			wantOK: true,
		},
		{
			name:   "oauth2",
			envs:   map[string]string{"JAMF_URL": "https://test.jamfcloud.com", "JAMF_CLIENT_ID": "my-client", "JAMF_CLIENT_SECRET": "my-secret"},
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetGlobals()
			clearAuthEnv(t)
			for k, v := range tt.envs {
				t.Setenv(k, v)
			}

			cfg, _ := config.Load()
			url, provider, err := resolveAuth(cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if url != "https://test.jamfcloud.com" {
				t.Errorf("url = %q, want %q", url, "https://test.jamfcloud.com")
			}
			if provider == nil {
				t.Fatal("expected non-nil auth provider")
			}
		})
	}
}

func TestResolveAuth_TokenFromFile(t *testing.T) {
	resetGlobals()
	clearAuthEnv(t)
	t.Setenv("JAMF_URL", "https://test.jamfcloud.com")

	tokenPath := filepath.Join(t.TempDir(), "token.txt")
	_ = os.WriteFile(tokenPath, []byte("file-token\n"), 0o600)
	tokenFile = tokenPath

	cfg, _ := config.Load()
	_, provider, err := resolveAuth(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil auth provider from token file")
	}
}

func TestResolveAuth_TokenFileMissing(t *testing.T) {
	resetGlobals()
	clearAuthEnv(t)
	t.Setenv("JAMF_URL", "https://test.jamfcloud.com")

	tokenFile = "/nonexistent/token.txt"

	cfg, _ := config.Load()
	_, _, err := resolveAuth(cfg)
	if err == nil {
		t.Fatal("expected error for missing token file")
	}
	if !strings.Contains(err.Error(), "reading token file") {
		t.Errorf("error = %q, want to contain 'reading token file'", err.Error())
	}
}

// setupConfigProfile creates a temp config with the given YAML and clears auth env vars.
func setupConfigProfile(t *testing.T, cfgYaml string) {
	t.Helper()
	resetGlobals()
	clearAuthEnv(t)

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	jDir := filepath.Join(dir, "jamf-cli")
	_ = os.MkdirAll(jDir, 0o700)
	_ = os.WriteFile(filepath.Join(jDir, "config.yaml"), []byte(cfgYaml), 0o600)
}

func TestResolveAuth_ProfileFallback(t *testing.T) {
	t.Setenv("TEST_RESOLVE_TOKEN", "profile-token")
	setupConfigProfile(t, `default-profile: myprofile
profiles:
  myprofile:
    url: https://profile.jamfcloud.com
    auth-method: token
    token: "env:TEST_RESOLVE_TOKEN"
`)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	url, provider, err := resolveAuth(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://profile.jamfcloud.com" {
		t.Errorf("url = %q, want from profile", url)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider from profile")
	}
}

func TestResolveAuth_ProfileOAuth2(t *testing.T) {
	t.Setenv("TEST_OAUTH_ID", "from-env-id")
	t.Setenv("TEST_OAUTH_SECRET", "from-env-secret")
	setupConfigProfile(t, `default-profile: oauthprofile
profiles:
  oauthprofile:
    url: https://oauth.jamfcloud.com
    auth-method: oauth2
    client-id: "env:TEST_OAUTH_ID"
    client-secret: "env:TEST_OAUTH_SECRET"
`)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	url, provider, err := resolveAuth(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://oauth.jamfcloud.com" {
		t.Errorf("url = %q", url)
	}
	if provider == nil {
		t.Fatal("expected non-nil oauth2 provider")
	}
}

func TestResolveAuth_ProfileNotFound(t *testing.T) {
	setupConfigProfile(t, `profiles:
  existing:
    url: https://example.com
    auth-method: token
`)
	t.Setenv("JAMF_PROFILE", "nonexistent")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	_, _, err = resolveAuth(cfg)
	if err == nil {
		t.Fatal("expected error for nonexistent profile")
	}
	if !strings.Contains(err.Error(), "loading profile") {
		t.Errorf("error = %q, want to contain 'loading profile'", err.Error())
	}
}

// --- cliOutput.PrintResponse test ---

func TestCLIOutputPrintResponse(t *testing.T) {
	fieldName = ""

	var buf bytes.Buffer
	formatter := output.New("json", true, false)
	formatter.SetWriter(&buf)
	o := &cliOutput{formatter}

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"id":42}`)),
		Header:     make(http.Header),
	}

	err := o.PrintResponse(resp)
	if err != nil {
		t.Fatalf("PrintResponse error: %v", err)
	}
	if !strings.Contains(buf.String(), "42") {
		t.Errorf("expected response body in output, got: %s", buf.String())
	}
}

func TestPersistentPreRunE_NOCOLOREnv(t *testing.T) {
	resetGlobals()
	t.Setenv("NO_COLOR", "1")
	t.Setenv("JAMF_URL", "")
	t.Setenv("JAMF_TOKEN", "")
	t.Setenv("JAMF_PROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	root := NewRootCmd("test", "abc123", "2024-01-01")

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root.SetArgs([]string{"version"})
	_ = root.Execute()

	_ = w.Close()
	os.Stdout = oldStdout

	out, _ := io.ReadAll(r)
	output := string(out)

	// Verify no ANSI escape sequences
	if strings.Contains(output, "\033[") {
		t.Errorf("output with NO_COLOR should not contain ANSI codes, got: %q", output)
	}
}

// --- spinnerClient tests ---

func TestSpinnerClient_PassesThrough(t *testing.T) {
	mock := &mockHTTPClient{}
	sc := &spinnerClient{inner: mock}

	resp, err := sc.Do(context.Background(), "GET", "/v1/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mock.called {
		t.Fatal("expected inner client to be called")
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// --- cliClient.Do test ---

func TestCLIOutputPrintResponse_ReadError(t *testing.T) {
	fieldName = ""
	t.Cleanup(func() { fieldName = "" })

	formatter := output.New("json", true, false)
	var buf bytes.Buffer
	formatter.SetWriter(&buf)
	o := &cliOutput{formatter}

	// Create a response with a body that's already closed
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(&failReader{}),
		Header:     make(http.Header),
	}

	err := o.PrintResponse(resp)
	if err == nil {
		t.Fatal("expected error from failed body read")
	}
}

// failReader always returns an error on Read.
type failReader struct{}

func (f *failReader) Read(p []byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}
