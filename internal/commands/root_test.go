package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/jamf/jamfpro-cli/internal/exitcode"
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

	// Verify a generated command like "computers list" is present
	found = false
	for _, e := range entries {
		if e.Command == "computers list" {
			found = true
			if len(e.Aliases) == 0 {
				t.Error("expected computers list to have aliases (e.g., 'comp')")
			}
			if len(e.Flags) == 0 {
				t.Error("expected computers list to have flags")
			}
			break
		}
	}
	if !found {
		t.Error("expected 'computers list' command in entries")
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

	w.Close()
	os.Stdout = oldStdout

	if !handled {
		t.Fatal("FormatError should return true for json format")
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)

	var envelope map[string]interface{}
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
	client.Do(context.Background(), "POST", "/v1/categories", body)

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
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
	serverURL = ""
	token = ""
	tokenFile = ""
	tokenStdin = false
	clientID = ""
	clientSecret = ""
	username = ""
	password = ""
}

func TestPersistentPreRunE_MissingURL(t *testing.T) {
	resetGlobals()
	// Clear any env vars that could provide a URL
	t.Setenv("JAMF_URL", "")
	t.Setenv("JAMF_TOKEN", "my-token")
	t.Setenv("JAMF_PROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // empty config dir — no profiles

	root := NewRootCmd("test", "abc123", "2024-01-01")
	root.SetArgs([]string{"computers", "list"})

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
	t.Setenv("JAMF_USERNAME", "")
	t.Setenv("JAMF_PASSWORD", "")
	t.Setenv("JAMF_PROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	root := NewRootCmd("test", "abc123", "2024-01-01")
	root.SetArgs([]string{"computers", "list"})

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
	t.Setenv("JAMF_USERNAME", "")
	t.Setenv("JAMF_PASSWORD", "")
	t.Setenv("JAMF_PROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	root := NewRootCmd("test", "abc123", "2024-01-01")
	root.SetArgs([]string{"computers", "list"})

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
	t.Setenv("JAMF_USERNAME", "")
	t.Setenv("JAMF_PASSWORD", "")
	t.Setenv("JAMF_PROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	root := NewRootCmd("test", "abc123", "2024-01-01")
	root.SetArgs([]string{"computers", "list"})

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
	root.Execute()

	w.Close()
	os.Stdout = oldStdout

	out, _ := io.ReadAll(r)
	output := string(out)

	// Verify no ANSI escape sequences
	if strings.Contains(output, "\033[") {
		t.Errorf("output with NO_COLOR should not contain ANSI codes, got: %q", output)
	}
}
