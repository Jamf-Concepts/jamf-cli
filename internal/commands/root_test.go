// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/spf13/cobra"
)

func TestIsFullDetailFormat(t *testing.T) {
	// Structured machine formats carry full per-command detail in `commands`.
	for _, f := range []string{"json", "ndjson", "yaml", "csv"} {
		if !isFullDetailFormat(f) {
			t.Errorf("isFullDetailFormat(%q) = false, want true", f)
		}
	}
	// Human/compact formats stay compact unless --wide.
	for _, f := range []string{"table", "plain", "xml", "raw", ""} {
		if isFullDetailFormat(f) {
			t.Errorf("isFullDetailFormat(%q) = true, want false", f)
		}
	}
}

func TestCommandsSubcommand_JSON(t *testing.T) {
	// Reset global state
	outputFmt = "json"
	noColor = true
	wide = false

	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")

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
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	entries := collectCommands(root, "", "", "")

	if len(entries) == 0 {
		t.Fatal("expected at least one command entry")
		return
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

	// Verify a generated command like "pro computers-inventory list" is present
	found = false
	for _, e := range entries {
		if e.Command == "pro computers-inventory list" {
			found = true
			if len(e.Aliases) == 0 {
				t.Error("expected pro computers-inventory list to have aliases (e.g., 'comp')")
			}
			if len(e.Flags) == 0 {
				t.Error("expected pro computers-inventory list to have flags")
			}
			break
		}
	}
	if !found {
		t.Error("expected 'pro computers-inventory list' command in entries")
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

// TestCollectCommands_NestedNamespaceNameNotReclassified guards a regression
// where a nested command named after a top-level namespace (e.g.
// "pro report security") was mis-tagged with that namespace's product. Only
// top-level namespaces should set the product.
func TestCollectCommands_NestedNamespaceNameNotReclassified(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	entries := collectCommands(root, "", "", "")

	byCommand := make(map[string]commandEntry, len(entries))
	for _, e := range entries {
		byCommand[e.Command] = e
	}

	e, ok := byCommand["pro report security"]
	if !ok {
		t.Fatal("expected 'pro report security' command in entries")
	}
	if e.Product != "pro" {
		t.Errorf("'pro report security' product = %q, want %q", e.Product, "pro")
	}

	// Every top-level namespace command should still be tagged with its own product.
	for _, ns := range []string{"pro", "protect", "school", "security", "platform"} {
		if got := byCommand[ns].Product; got != ns {
			t.Errorf("top-level %q product = %q, want %q", ns, got, ns)
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
			Product:     "pro",
			Group:       "Computer Management",
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
	if maps[0]["product"] != "pro" {
		t.Errorf("product = %q, want %q", maps[0]["product"], "pro")
	}
	if maps[0]["group"] != "Computer Management" {
		t.Errorf("group = %q, want %q", maps[0]["group"], "Computer Management")
	}

	// Entry without product/group/aliases/flags should have empty strings
	if maps[1]["aliases"] != "" {
		t.Errorf("version aliases = %q, want empty", maps[1]["aliases"])
	}
	if maps[1]["flags"] != "" {
		t.Errorf("version flags = %q, want empty", maps[1]["flags"])
	}
	if maps[1]["product"] != "" {
		t.Errorf("version product = %q, want empty", maps[1]["product"])
	}
	if maps[1]["group"] != "" {
		t.Errorf("version group = %q, want empty", maps[1]["group"])
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
	if _, ok := maps[0]["product"]; ok {
		t.Error("compact mode should not include product key")
	}
	if _, ok := maps[0]["group"]; ok {
		t.Error("compact mode should not include group key")
	}
}

func TestCollectCommands_ProductAndGroup(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	entries := collectCommands(root, "", "", "")

	var found *commandEntry
	for i := range entries {
		if entries[i].Command == "pro computers-inventory list" {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected 'pro computers-inventory list' in entries")
		return
	}
	if found.Product != "pro" {
		t.Errorf("product = %q, want %q", found.Product, "pro")
	}
	if found.Group != "Computer Management" {
		t.Errorf("group = %q, want %q", found.Group, "Computer Management")
	}
}

func TestCollectCommands_ProtectProductAndGroup(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	entries := collectCommands(root, "", "", "")

	var found *commandEntry
	for i := range entries {
		if entries[i].Command == "protect analytics list" {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected 'protect analytics list' in entries")
		return
	}
	if found.Product != "protect" {
		t.Errorf("product = %q, want %q", found.Product, "protect")
	}
	if found.Group != "Security Configuration" {
		t.Errorf("group = %q, want %q", found.Group, "Security Configuration")
	}
}

func TestCollectCommands_RootCommandsNoProduct(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	entries := collectCommands(root, "", "", "")

	var found *commandEntry
	for i := range entries {
		if entries[i].Command == "version" {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected 'version' in entries")
		return
	}
	if found.Product != "" {
		t.Errorf("version product = %q, want empty", found.Product)
	}
	if found.Group != "Core Commands" {
		t.Errorf("version group = %q, want %q", found.Group, "Core Commands")
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
		return
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
		return
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
		return
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
		return
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
		return
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
		return
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
		return
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
	noHints = false
	verboseLevel = 0
	noInput = false
	noColor = false
	dryRun = false
	wide = false
	outFile = ""
	fieldName = ""
	serverURL = ""
	token = ""
	tokenFile = ""
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

	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	root.SetArgs([]string{"pro", "computers", "list"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when URL is missing")
		return
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

	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	root.SetArgs([]string{"pro", "computers", "list"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when auth is missing")
		return
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

	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	root.SetArgs([]string{"pro", "computers", "list"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for partial OAuth2 credentials")
		return
	}
	if !strings.Contains(err.Error(), "client secret is required") {
		t.Errorf("error = %q, want to contain 'client secret is required'", err.Error())
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

	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	root.SetArgs([]string{"pro", "computers", "list"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for partial OAuth2 credentials")
		return
	}
	if !strings.Contains(err.Error(), "client ID is required") {
		t.Errorf("error = %q, want to contain 'client ID is required'", err.Error())
	}
}

func TestPersistentPreRunE_SkipsForConfigCommand(t *testing.T) {
	resetGlobals()
	// No URL, no auth — but config commands should skip validation
	t.Setenv("JAMF_URL", "")
	t.Setenv("JAMF_TOKEN", "")
	t.Setenv("JAMF_PROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
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

	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
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

	// The values are asserted against the formatter's WRITER, not stdout.
	// --field used to write to os.Stdout itself, which split one report between
	// two destinations: a multi-section report's headers followed --out-file
	// through the formatter while the values went to the terminal.
	var buf bytes.Buffer
	formatter := output.New("json", true, false)
	formatter.SetWriter(&buf)
	o := &cliOutput{formatter}

	stdout := captureStdout(t, func() {
		err := o.PrintRaw([]byte(`[{"id":1,"name":"A"},{"id":2,"name":"B"}]`))
		if err != nil {
			t.Fatalf("PrintRaw error: %v", err)
		}
	})

	if buf.String() != "1\n2\n" {
		t.Errorf("writer got %q, want %q", buf.String(), "1\n2\n")
	}
	if stdout != "" {
		t.Errorf("--field left %q on standard output, so --out-file receives only part of the report", stdout)
	}
}

func TestCLIOutputPrintRaw_FieldExtractSingleObject(t *testing.T) {
	fieldName = "name"
	t.Cleanup(func() { fieldName = "" })

	// The values are asserted against the formatter's WRITER, not stdout.
	// --field used to write to os.Stdout itself, which split one report between
	// two destinations: a multi-section report's headers followed --out-file
	// through the formatter while the values went to the terminal.
	var buf bytes.Buffer
	formatter := output.New("json", true, false)
	formatter.SetWriter(&buf)
	o := &cliOutput{formatter}

	stdout := captureStdout(t, func() {
		err := o.PrintRaw([]byte(`{"id":1,"name":"HQ"}`))
		if err != nil {
			t.Fatalf("PrintRaw error: %v", err)
		}
	})

	if buf.String() != "HQ\n" {
		t.Errorf("writer got %q, want %q", buf.String(), "HQ\n")
	}
	if stdout != "" {
		t.Errorf("--field left %q on standard output, so --out-file receives only part of the report", stdout)
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
		return
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
		return
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
				return
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
		return
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
		return
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
		return
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
		return
	}
}

func TestResolveAuth_ExplicitTokenSkipsProfileCredentials(t *testing.T) {
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
	// Explicit token should override the oauth2 profile credentials
	token = "explicit-bearer-token"

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	url, provider, err := resolveAuth(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Profile URL should still be used (url resolution is outside the explicitToken guard)
	if url != "https://oauth.jamfcloud.com" {
		t.Errorf("url = %q, want profile URL", url)
	}
	// Provider should be token, NOT oauth2
	if _, ok := provider.(*auth.TokenProvider); !ok {
		t.Errorf("expected *auth.TokenProvider, got %T", provider)
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
		return
	}
	if !strings.Contains(err.Error(), "loading profile") {
		t.Errorf("error = %q, want to contain 'loading profile'", err.Error())
	}
}

func TestResolveAuth_URLNormalization(t *testing.T) {
	tests := []struct {
		name    string
		cfgYaml string
		envURL  string
		wantURL string
	}{
		{
			name: "trailing slash in stored profile stripped",
			cfgYaml: `default-profile: test
profiles:
  test:
    url: https://example.jamfcloud.com/
    auth-method: token
    token: "env:TEST_URL_NORM_TOKEN"
`,
			wantURL: "https://example.jamfcloud.com",
		},
		{
			name:    "bare hostname in JAMF_URL gets https",
			cfgYaml: `profiles: {}`,
			envURL:  "example.jamfcloud.com",
			wantURL: "https://example.jamfcloud.com",
		},
		{
			name:    "JAMF_URL trailing slash stripped",
			cfgYaml: `profiles: {}`,
			envURL:  "https://example.jamfcloud.com/",
			wantURL: "https://example.jamfcloud.com",
		},
	}

	t.Setenv("TEST_URL_NORM_TOKEN", "test-token")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupConfigProfile(t, tt.cfgYaml)
			if tt.envURL != "" {
				t.Setenv("JAMF_URL", tt.envURL)
				t.Setenv("JAMF_TOKEN", "tok")
			}

			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("Load error: %v", err)
			}

			url, _, err := resolveAuth(cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if url != tt.wantURL {
				t.Errorf("url = %q, want %q", url, tt.wantURL)
			}
		})
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

	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")

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

func TestPersistentPreRunE_NoHintsEnv(t *testing.T) {
	cases := []struct {
		name        string
		envVal      string
		wantNoHints bool
	}{
		{"1 enables", "1", true},
		{"true enables", "true", true},
		{"TRUE enables", "TRUE", true},
		{"0 leaves hints on", "0", false},
		{"false leaves hints on", "false", false},
		{"empty leaves hints on", "", false},
		{"garbage leaves hints on", "yarp", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetGlobals()
			clearAuthEnv(t)
			t.Setenv("JAMF_CLI_NO_HINTS", tc.envVal)

			root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")

			// version is in chainSkip, but the JAMF_CLI_NO_HINTS parse runs
			// before the skip return — same as the NO_COLOR handling — so the
			// global reflects the env regardless of the command.
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w
			root.SetArgs([]string{"version"})
			_ = root.Execute()
			_ = w.Close()
			os.Stdout = oldStdout
			_, _ = io.ReadAll(r)

			if noHints != tc.wantNoHints {
				t.Errorf("JAMF_CLI_NO_HINTS=%q: noHints=%v, want %v", tc.envVal, noHints, tc.wantNoHints)
			}
		})
	}
}

// --- spinnerClient tests ---

func TestShouldShowSpinner(t *testing.T) {
	cases := []struct {
		name    string
		quiet   bool
		noColor bool
		verbose int
		want    bool
	}{
		{"defaults show spinner", false, false, 0, true},
		{"quiet suppresses", true, false, 0, false},
		{"no-color suppresses", false, true, 0, false},
		{"verbose suppresses", false, false, 1, false},
		{"quiet wins over no-color", true, true, 0, false},
		{"verbose wins alone", false, false, 2, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(resetGlobals)
			quiet = tc.quiet
			noColor = tc.noColor
			verboseLevel = tc.verbose
			if got := shouldShowSpinner(); got != tc.want {
				t.Errorf("shouldShowSpinner()=%v want=%v (quiet=%v noColor=%v verbose=%d)",
					got, tc.want, tc.quiet, tc.noColor, tc.verbose)
			}
		})
	}
}

// TestShouldShowSpinner_IgnoresNoHints guards the defining invariant of #214:
// --no-hints / JAMF_CLI_NO_HINTS suppresses advisory hints but must NOT touch
// the spinner. If noHints ever leaks into shouldShowSpinner(), this fails.
func TestShouldShowSpinner_IgnoresNoHints(t *testing.T) {
	t.Cleanup(resetGlobals)
	resetGlobals()
	noHints = true
	if !shouldShowSpinner() {
		t.Error("shouldShowSpinner()=false with noHints=true; --no-hints must not suppress the spinner")
	}
}

func TestSpinnerClient_PassesThrough(t *testing.T) {
	mock := &mockHTTPClient{}
	sc := &spinnerClient{inner: mock}

	resp, err := sc.Do(context.Background(), "GET", "/v1/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mock.called {
		t.Fatal("expected inner client to be called")
		return
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
		return
	}
}

// failReader always returns an error on Read.
type failReader struct{}

func (f *failReader) Read(p []byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func TestClassifyError(t *testing.T) {
	// Unknown-command errors (plain, from cobra) become usage errors (2).
	unknownCmd := errors.New(`unknown command "proo" for "jamf-cli"`)
	if got := exitcode.CodeFrom(ClassifyError(unknownCmd)); got != exitcode.Usage {
		t.Errorf("unknown command -> exit %d, want %d (usage)", got, exitcode.Usage)
	}
	// Errors that already carry a code are left untouched.
	authErr := exitcode.New(exitcode.Authentication, "nope")
	if got := exitcode.CodeFrom(ClassifyError(authErr)); got != exitcode.Authentication {
		t.Errorf("pre-coded error -> exit %d, want %d", got, exitcode.Authentication)
	}
	// A coded error whose message opens on a usage prefix keeps its own code.
	// The errors.As early return is the stated basis for matching prefixes at
	// all — it is what confines the loop to plain errors this repository built —
	// so the case where the two disagree has to be pinned. Deleting that return,
	// or moving it after the loop, passes every other test in this file.
	for _, p := range usageErrorPrefixes {
		coded := exitcode.New(exitcode.Authentication, p+`"token" was rejected`)
		if got := exitcode.CodeFrom(ClassifyError(coded)); got != exitcode.Authentication {
			t.Errorf("coded error opening on %q -> exit %d, want %d (authentication)",
				p, got, exitcode.Authentication)
		}
	}
	// Unrelated plain errors stay general (1).
	if got := exitcode.CodeFrom(ClassifyError(errors.New("boom"))); got != exitcode.General {
		t.Errorf("generic error -> exit %d, want %d (general)", got, exitcode.General)
	}
	if ClassifyError(nil) != nil {
		t.Error("ClassifyError(nil) should return nil")
	}
}

// TestClassifyError_MissingRequiredFlagIsAUsageError drives a real command
// through Execute rather than asserting on a literal, because the string
// ClassifyError matches is cobra's own format and nothing on this side owns it.
// A cobra upgrade that reworded the message would otherwise silently return
// every command with a required flag to exit 1, the generic-failure code, which
// is exactly the state this fixes: `pro backup --nosuchflag` exited 2 while
// `pro backup` with no --output exited 1.
func TestClassifyError_MissingRequiredFlagIsAUsageError(t *testing.T) {
	// Credentials have to resolve first: cobra runs PersistentPreRunE before
	// ValidateRequiredFlags, so without them the auth error arrives instead and
	// the required-flag path is never reached.
	resetGlobals()
	t.Setenv("JAMF_URL", "https://test.jamfcloud.com")
	t.Setenv("JAMF_TOKEN", "test-token")
	t.Setenv("JAMF_CLIENT_ID", "")
	t.Setenv("JAMF_CLIENT_SECRET", "")
	t.Setenv("JAMF_PROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// The spec version must stay "unknown". checkTenantVersion returns early
	// only on that value, and a completed PersistentPreRunE calls it, so any
	// other value probes the tenant over the network from a unit test.
	root := NewRootCmd("test", "none", "none", "unknown")
	root.SetArgs([]string{"pro", "backup"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	err := root.Execute()
	if err == nil {
		t.Fatal("pro backup with no --output should fail")
	}
	if !strings.HasPrefix(err.Error(), usagePrefixRequiredFlags) {
		t.Fatalf("cobra reworded the message; %q in ClassifyError's prefix list needs updating: %q",
			usagePrefixRequiredFlags, err.Error())
	}
	if got := exitcode.CodeFrom(ClassifyError(err)); got != exitcode.Usage {
		t.Errorf("missing required flag -> exit %d, want %d (usage)", got, exitcode.Usage)
	}
}

// TestClassifyError_UnrelatedPlainErrorStaysGeneral guards the widening: the
// prefix list must not swallow a request failure into a usage code.
//
// Every prefix gets two neighbours, because a single unrelated string catches
// neither way the matcher can go wrong. One opens on a truncation of the prefix
// and then diverges, so a shortened prefix fails here when the shortening
// reaches that far. One carries the whole prefix as a mid-sentence substring, so
// relaxing strings.HasPrefix to strings.Contains fails here. The earlier three
// strings satisfied neither shape for five of the six prefixes.
func TestClassifyError_UnrelatedPlainErrorStaysGeneral(t *testing.T) {
	for _, msg := range []string{
		"the request required a flag we could not send",
		"boom",

		"unknown field in the response body",
		"the response body carried an unknown command reference",

		"required inventory data was absent from the response",
		`the server rejected the body: required flag(s) "name" not set`,

		"if any device is offline the report is partial",
		"the server said if any flags in the group [a b] are set none of the others can be",

		"at least one device must be enrolled",
		"the server said at least one of the flags in the group [a b] is required",

		"requires a serial number",
		"the server said requires at least 2 arg(s), only received 1",

		"accept header was rejected",
		"the endpoint accepts 2 arg(s), received 0",

		// The two messages that made "accepts " and "requires at least " untenable
		// as prefixes, verbatim from the binary. Both are a caller-supplied path at
		// character zero, so re-adding either prefix fails here.
		"accepts x is a directory, not a file",
		"requires at least a dir holds backup documents but its _meta cannot be read",
	} {
		if got := exitcode.CodeFrom(ClassifyError(errors.New(msg))); got != exitcode.General {
			t.Errorf("%q -> exit %d, want %d (general)", msg, got, exitcode.General)
		}
	}

	// The strings above are hand-picked, so they cover only the prefixes that
	// existed when someone wrote them and a seventh would arrive unguarded. These
	// two shapes come off the list itself, one per prefix, so it cannot.
	//
	// Neither shape can catch a *shortened* prefix, and it is worth being clear
	// about why: both are computed from the const, so a shortening moves the
	// guard with it. Mutating usagePrefixFlagGroup to "if any flags" leaves this
	// whole suite green, before and after these two shapes were added. What they
	// do catch is a relaxed comparison, which is the other way the matcher goes
	// wrong. Shortening is covered only by the hand-picked strings above, and only
	// where one of them happens to collide — the durable answer is the audit
	// written above the const block, not a test.
	for _, p := range usageErrorPrefixes {
		// All but the last character, so the comparison has to be anchored on the
		// whole prefix rather than on a near miss.
		nearMiss := p[:len(p)-1] + "zzz was not the problem"
		// The whole prefix, carried mid-sentence, which fails if HasPrefix is ever
		// relaxed to Contains.
		nested := "the server rejected the body: " + p + " and then stopped"
		for _, msg := range []string{nearMiss, nested} {
			if strings.HasPrefix(msg, p) {
				t.Fatalf("derived guard %q still opens on %q, so it guards nothing", msg, p)
			}
			if got := exitcode.CodeFrom(ClassifyError(errors.New(msg))); got != exitcode.General {
				t.Errorf("derived from %q: %q -> exit %d, want %d (general)",
					p, msg, got, exitcode.General)
			}
		}
	}
}

// TestUsageErrorPrefixesMatchCobrasOwnMessages drives cobra's validators
// directly so the literals cannot drift underneath the prefix list. Asserting
// the prefix as well as the exit code is what makes an upstream reword name the
// const that needs updating rather than just flipping a code.
//
// It calls the validators rather than Execute because MarkFlagsRequiredTogether
// has no call site in this CLI, so no real command can produce that message.
//
// The argument-count validators used to be here too. They moved to
// TestClassifyArgsErrors_CodesEveryCobraArgumentValidator, which asserts the
// same exit code through classifyArgsErrors and reads none of cobra's text —
// pinning a literal that nothing matches on any more would assert nothing.
func TestUsageErrorPrefixesMatchCobrasOwnMessages(t *testing.T) {
	probe := func() *cobra.Command {
		cmd := &cobra.Command{Use: "probe", Run: func(*cobra.Command, []string) {}}
		cmd.Flags().String("alpha", "", "")
		cmd.Flags().String("beta", "", "")
		return cmd
	}
	setFlags := func(t *testing.T, cmd *cobra.Command, names ...string) {
		t.Helper()
		for _, n := range names {
			if err := cmd.Flags().Set(n, "x"); err != nil {
				t.Fatalf("set --%s: %v", n, err)
			}
		}
	}

	for _, tc := range []struct {
		name    string
		prefix  string
		produce func(t *testing.T) error
	}{
		{
			name:   "MarkFlagRequired unset",
			prefix: usagePrefixRequiredFlags,
			produce: func(t *testing.T) error {
				cmd := probe()
				if err := cmd.MarkFlagRequired("alpha"); err != nil {
					t.Fatalf("MarkFlagRequired: %v", err)
				}
				return cmd.ValidateRequiredFlags()
			},
		},
		{
			name:   "MarkFlagsRequiredTogether with one set",
			prefix: usagePrefixFlagGroup,
			produce: func(t *testing.T) error {
				cmd := probe()
				cmd.MarkFlagsRequiredTogether("alpha", "beta")
				setFlags(t, cmd, "alpha")
				return cmd.ValidateFlagGroups()
			},
		},
		{
			name:   "MarkFlagsMutuallyExclusive with both set",
			prefix: usagePrefixFlagGroup,
			produce: func(t *testing.T) error {
				cmd := probe()
				cmd.MarkFlagsMutuallyExclusive("alpha", "beta")
				setFlags(t, cmd, "alpha", "beta")
				return cmd.ValidateFlagGroups()
			},
		},
		{
			name:   "MarkFlagsOneRequired with neither set",
			prefix: usagePrefixFlagGroupOneOf,
			produce: func(t *testing.T) error {
				cmd := probe()
				cmd.MarkFlagsOneRequired("alpha", "beta")
				return cmd.ValidateFlagGroups()
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.produce(t)
			if err == nil {
				t.Fatal("cobra accepted an invocation it used to reject")
			}
			if !strings.HasPrefix(err.Error(), tc.prefix) {
				t.Errorf("cobra reworded the message; %q in ClassifyError's prefix list needs updating: %q",
					tc.prefix, err.Error())
			}
			if got := exitcode.CodeFrom(ClassifyError(err)); got != exitcode.Usage {
				t.Errorf("%q -> exit %d, want %d (usage)", err.Error(), got, exitcode.Usage)
			}
		})
	}
}

// TestClassifyError_ExclusiveFlagGroupIsAUsageError covers the flag-group half
// of the widening through a real command. `pro comp erase` marks serial, name,
// id, group and from-file mutually exclusive, and two of them together exited 1
// before the group prefixes were listed.
func TestClassifyError_ExclusiveFlagGroupIsAUsageError(t *testing.T) {
	resetGlobals()
	t.Setenv("JAMF_URL", "https://test.jamfcloud.com")
	t.Setenv("JAMF_TOKEN", "test-token")
	t.Setenv("JAMF_CLIENT_ID", "")
	t.Setenv("JAMF_CLIENT_SECRET", "")
	t.Setenv("JAMF_PROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	root := NewRootCmd("test", "none", "none", "unknown")
	root.SetArgs([]string{"pro", "comp", "erase", "--serial", "ABC", "--name", "foo"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	err := root.Execute()
	if err == nil {
		t.Fatal("pro comp erase with --serial and --name should fail")
	}
	if !strings.HasPrefix(err.Error(), usagePrefixFlagGroup) {
		t.Fatalf("cobra reworded the message; %q in ClassifyError's prefix list needs updating: %q",
			usagePrefixFlagGroup, err.Error())
	}
	if got := exitcode.CodeFrom(ClassifyError(err)); got != exitcode.Usage {
		t.Errorf("exclusive flag group -> exit %d, want %d (usage)", got, exitcode.Usage)
	}
}

// TestClassifyError_OneRequiredFlagGroupIsAUsageError covers MarkFlagsOneRequired
// through a real command.
//
// The credentials are the Jamf Protect ones on purpose. `protect analytics
// import` resolves no auth from JAMF_URL and JAMF_TOKEN, so with those alone it
// fails earlier with a credentials error that already exits 2, and the test
// passes without ever reaching the flag group. JAMF_URL and JAMF_TOKEN are
// cleared so a developer's own environment cannot change which error arrives.
func TestClassifyError_OneRequiredFlagGroupIsAUsageError(t *testing.T) {
	resetGlobals()
	t.Setenv("JAMFPROTECT_URL", "https://test.protect.jamfcloud.com")
	t.Setenv("JAMFPROTECT_CLIENT_ID", "test-client")
	t.Setenv("JAMFPROTECT_CLIENT_SECRET", "test-secret")
	t.Setenv("JAMF_URL", "")
	t.Setenv("JAMF_TOKEN", "")
	t.Setenv("JAMF_CLIENT_ID", "")
	t.Setenv("JAMF_CLIENT_SECRET", "")
	t.Setenv("JAMF_PROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	root := NewRootCmd("test", "none", "none", "unknown")
	root.SetArgs([]string{"protect", "analytics", "import"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	err := root.Execute()
	if err == nil {
		t.Fatal("protect analytics import with neither --file nor --dir should fail")
	}
	if !strings.HasPrefix(err.Error(), usagePrefixFlagGroupOneOf) {
		t.Fatalf("cobra reworded the message; %q in ClassifyError's prefix list needs updating: %q",
			usagePrefixFlagGroupOneOf, err.Error())
	}
	if got := exitcode.CodeFrom(ClassifyError(err)); got != exitcode.Usage {
		t.Errorf("one-required flag group -> exit %d, want %d (usage)", got, exitcode.Usage)
	}
}

// TestArgCountErrorIsAUsageError covers the argument-count half through a real
// command. `pro blueprints clone` is cobra.ExactArgs(2).
//
// The error is read straight off Execute, with no ClassifyError in between,
// because classifyArgsErrors codes it inside the validator: the code has to be
// on the error the moment it leaves cobra, or the hand-written platform commands
// that return their own errors would still depend on the text.
func TestArgCountErrorIsAUsageError(t *testing.T) {
	resetGlobals()
	t.Setenv("JAMF_URL", "https://test.jamfcloud.com")
	t.Setenv("JAMF_TOKEN", "test-token")
	t.Setenv("JAMF_CLIENT_ID", "")
	t.Setenv("JAMF_CLIENT_SECRET", "")
	t.Setenv("JAMF_PROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	root := NewRootCmd("test", "none", "none", "unknown")
	root.SetArgs([]string{"pro", "blueprints", "clone"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	err := root.Execute()
	if err == nil {
		t.Fatal("pro blueprints clone with no arguments should fail")
	}
	if got := exitcode.CodeFrom(err); got != exitcode.Usage {
		t.Errorf("wrong argument count -> exit %d, want %d (usage): %v", got, exitcode.Usage, err)
	}
}

// TestClassifyArgsErrors_CodesEveryCobraArgumentValidator drives each cobra
// validator this CLI can reach through the walk that NewRootCmd runs, so the
// exit code no longer depends on how cobra words the message.
//
// MinimumNArgs and RangeArgs have no call site in this CLI. They are covered
// anyway because the walk is indiscriminate, so a command adopting either one
// gets exit 2 without a second edit.
func TestClassifyArgsErrors_CodesEveryCobraArgumentValidator(t *testing.T) {
	for _, tc := range []struct {
		name string
		args cobra.PositionalArgs
		give []string
	}{
		{"ExactArgs", cobra.ExactArgs(2), nil},
		{"MaximumNArgs", cobra.MaximumNArgs(1), []string{"a", "b"}},
		{"MinimumNArgs", cobra.MinimumNArgs(1), nil},
		{"RangeArgs", cobra.RangeArgs(1, 2), nil},
		{"NoArgs", cobra.NoArgs, []string{"a"}},
		// The shape both generators emit for a command whose --scaffold relaxes
		// the count: a closure of this repository's own around a cobra validator.
		{"a scaffold-relaxing closure", func(c *cobra.Command, args []string) error {
			return cobra.ExactArgs(1)(c, args)
		}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			leaf := &cobra.Command{Use: "leaf", Args: tc.args, Run: func(*cobra.Command, []string) {}}
			root := &cobra.Command{Use: "root"}
			root.AddCommand(&cobra.Command{Use: "mid", Short: "mid"})
			root.Commands()[0].AddCommand(leaf)

			classifyArgsErrors(root)

			err := leaf.Args(leaf, tc.give)
			if err == nil {
				t.Fatal("cobra accepted an invocation it used to reject")
			}
			if got := exitcode.CodeFrom(err); got != exitcode.Usage {
				t.Errorf("%v -> exit %d, want %d (usage)", err, got, exitcode.Usage)
			}
		})
	}
}

// TestClassifyArgsErrors_LeavesANilValidatorNil pins the invariant the walk
// depends on. Cobra's Find consults Args only for nil, and takes the nil branch
// to run legacyArgs — which is what rejects an unknown command at the root and
// what guardUnknownSubcommands relies on staying in place for every group parent
// below it. Assigning a validator to a nil field switches that off silently.
func TestClassifyArgsErrors_LeavesANilValidatorNil(t *testing.T) {
	parent := &cobra.Command{Use: "parent"}
	parent.AddCommand(&cobra.Command{Use: "leaf", Run: func(*cobra.Command, []string) {}})

	classifyArgsErrors(parent)

	if parent.Args != nil {
		t.Error("parent gained an Args validator, which disables legacyArgs in Find")
	}
	if parent.Commands()[0].Args != nil {
		t.Error("leaf gained an Args validator")
	}

	// And through the real tree: the root's unknown-command rejection is the
	// behaviour that breaks if the walk ever stops honouring nil.
	resetGlobals()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := NewRootCmd("test", "none", "none", "unknown")
	root.SetArgs([]string{"nosuchcommand"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.Execute()
	if err == nil {
		t.Fatal("an unknown root command should fail")
	}
	if !strings.HasPrefix(err.Error(), usagePrefixUnknownCommand) {
		t.Fatalf("legacyArgs no longer runs at the root: %q", err.Error())
	}
}

// TestClassifyArgsErrors_KeepsACodedErrorsOwnCode covers the idempotence the
// wrap needs. exitcode.Wrap builds a fresh *exitcode.Error and CodeFrom reads
// the outermost one, so a validator returning its own code would have it
// overwritten with Usage. Running the walk twice must also not change anything,
// since nothing stops a future caller from walking a subtree that was already
// walked.
func TestClassifyArgsErrors_KeepsACodedErrorsOwnCode(t *testing.T) {
	refused := exitcode.New(exitcode.Unsupported, "this command is refused on a gateway profile")
	leaf := &cobra.Command{
		Use:  "leaf",
		Args: func(*cobra.Command, []string) error { return refused },
		Run:  func(*cobra.Command, []string) {},
	}
	root := &cobra.Command{Use: "root"}
	root.AddCommand(leaf)

	classifyArgsErrors(root)
	classifyArgsErrors(root)

	if got := exitcode.CodeFrom(leaf.Args(leaf, nil)); got != exitcode.Unsupported {
		t.Errorf("coded Args error -> exit %d, want %d (unsupported)", got, exitcode.Unsupported)
	}
}

func TestGuardUnknownSubcommands(t *testing.T) {
	// A typo at a group-parent level must error with a usage code and a
	// suggestion, not silently print help and exit 0 (cobra's default).
	root := NewRootCmd("test", "none", "none", "none")
	root.SetArgs([]string{"pro", "buildings", "lst"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for unknown subcommand, got nil")
	}
	if code := exitcode.CodeFrom(err); code != exitcode.Usage {
		t.Errorf("exit code = %d, want %d (usage)", code, exitcode.Usage)
	}
	if !strings.Contains(err.Error(), "list") {
		t.Errorf("error should suggest 'list', got %q", err.Error())
	}

	// A bare parent with no subcommand still shows help and succeeds.
	root = NewRootCmd("test", "none", "none", "none")
	root.SetArgs([]string{"pro", "buildings"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	if err := root.Execute(); err != nil {
		t.Errorf("bare parent should not error, got %v", err)
	}
}

// TestGuardUnknownSubcommands_CoversAllGroupParents walks the entire command
// tree and asserts every group parent (a command that owns subcommands) is
// guarded: it must carry the group-parent annotation and reject an unknown
// subcommand with a usage exit code. Because it walks the whole tree, a new
// command group — generated or hand-written, at any depth — is covered
// automatically; if one is ever left unguarded, this fails.
func TestGuardUnknownSubcommands_CoversAllGroupParents(t *testing.T) {
	root := NewRootCmd("test", "none", "none", "none")

	const bogus = "zzdefinitelynotacommand"
	parents := 0

	var walk func(c *cobra.Command, path string)
	walk = func(c *cobra.Command, path string) {
		for _, sub := range c.Commands() {
			subPath := strings.TrimSpace(path + " " + sub.Name())
			if sub.HasSubCommands() {
				parents++
				switch {
				case sub.Annotations[groupParentAnnotation] != "true":
					t.Errorf("group parent %q is not guarded: a typo would print help and exit 0", subPath)
				case sub.RunE == nil:
					t.Errorf("group parent %q is annotated but has no RunE", subPath)
				default:
					err := sub.RunE(sub, []string{bogus})
					if err == nil {
						t.Errorf("group parent %q accepted unknown subcommand without error", subPath)
					} else if code := exitcode.CodeFrom(err); code != exitcode.Usage {
						t.Errorf("group parent %q: unknown subcommand exit %d, want %d (usage)", subPath, code, exitcode.Usage)
					}
				}
			}
			walk(sub, subPath)
		}
	}
	walk(root, "")

	if parents < 10 {
		t.Fatalf("found only %d group parents — tree walk likely broken", parents)
	}
	t.Logf("verified %d group parents reject unknown subcommands", parents)
}

func TestCollectCommands_DestructiveFlag(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	entries := collectCommands(root, "", "", "")

	byCmd := make(map[string]commandEntry, len(entries))
	for _, e := range entries {
		byCmd[e.Command] = e
	}

	// A delete command must be marked destructive (annotation is enforced
	// elsewhere by TestDestructiveVerbCommandsAreAnnotated).
	// "computers" is an alias for "computers-inventory"; collectCommands uses
	// the canonical command name, not aliases.
	del, ok := byCmd["pro computers-inventory delete"]
	if !ok {
		t.Fatal("expected 'pro computers-inventory delete' in entries")
	}
	if !del.Destructive {
		t.Error("pro computers-inventory delete: Destructive = false, want true")
	}

	// A read command must not be marked destructive.
	list, ok := byCmd["pro computers-inventory list"]
	if !ok {
		t.Fatal("expected 'pro computers-inventory list' in entries")
	}
	if list.Destructive {
		t.Error("pro computers-inventory list: Destructive = true, want false")
	}
}

func TestCommandEntriesToMaps_Destructive(t *testing.T) {
	// Synthetic entries — command names are arbitrary for this unit test.
	entries := []commandEntry{
		{Command: "x delete", Description: "Delete", Destructive: true},
		{Command: "x list", Description: "List"},
	}
	maps := commandEntriesToMaps(entries, true)

	if maps[0]["destructive"] != true {
		t.Errorf("destructive command: destructive = %v, want true", maps[0]["destructive"])
	}
	// Emitted unconditionally in full mode (present, false) so CSV/table output —
	// which derive columns from the first row — carry the field for every row.
	if maps[1]["destructive"] != false {
		t.Errorf("non-destructive command: destructive = %v, want false", maps[1]["destructive"])
	}
}

func TestCollectCommands_Privileges(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	entries := collectCommands(root, "", "", "")

	found := false
	for _, e := range entries {
		if len(e.Privileges) > 0 {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one command with Privileges populated from the jamf:privileges annotation")
	}

	// 'version' carries no privilege annotation.
	for _, e := range entries {
		if e.Command == "version" && len(e.Privileges) > 0 {
			t.Errorf("version should have no privileges, got %v", e.Privileges)
		}
	}
}

func TestCommandEntriesToMaps_PrivilegesPositiveOnly(t *testing.T) {
	entries := []commandEntry{
		// Synthetic entries — command names are arbitrary for this unit test.
		{Command: "x get", Description: "Get", Privileges: []string{"Read Computers"}},
		{Command: "x noop", Description: "Noop"},
	}
	maps := commandEntriesToMaps(entries, true)

	if _, ok := maps[0]["privileges"]; !ok {
		t.Error("entry with privileges should emit the 'privileges' key")
	}
	if _, ok := maps[1]["privileges"]; ok {
		t.Error("entry without privileges must omit the 'privileges' key entirely")
	}
}

// TestChainSkip_RootOnlyNamesDoNotSkipNestedCommands asserts that a subcommand
// named "version" or "commands" still resolves auth.
//
// chainSkip matches by command name anywhere in the chain, which is right for
// "config" and "setup" but wrong for ordinary English words a resource can use
// as an operation name. AI Governance's GET /policies/{id}/versions/{n} is
// generated as `platform ai-policies version`, and while "version" was matched
// anywhere that command silently skipped auth and then failed at its own gate
// with "this command requires platform gateway auth" — sending the operator to
// fix credentials that were already correct. The failure is invisible from the
// generator side: nothing about the spec or the emitted code is wrong.
func TestChainSkip_RootOnlyNamesDoNotSkipNestedCommands(t *testing.T) {
	for _, args := range [][]string{
		{"platform", "ai-policies", "version", "some-id", "1"},
		{"pro", "mdm-commands", "commands"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			resetGlobals()
			t.Setenv("JAMF_URL", "https://test.jamfcloud.com")
			t.Setenv("JAMF_TOKEN", "")
			t.Setenv("JAMF_CLIENT_ID", "")
			t.Setenv("JAMF_CLIENT_SECRET", "")
			t.Setenv("JAMF_PROFILE", "")
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())

			root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
			root.SetArgs(args)

			err := root.Execute()
			if err == nil {
				t.Fatal("expected an auth error — the command skipped auth resolution")
			}
			if !strings.Contains(err.Error(), "authentication required") {
				t.Errorf("error = %q, want the auth-resolution error; a product gate's own "+
					"message here means auth was skipped", err.Error())
			}
		})
	}
}

// TestChainSkip_RootLevelVersionAndCommandsStillSkip is the other half: the
// root-level `version` and `commands` must keep working with no credentials at
// all, which is the whole reason they are in the skip set.
func TestChainSkip_RootLevelVersionAndCommandsStillSkip(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"commands"}} {
		t.Run(args[0], func(t *testing.T) {
			resetGlobals()
			t.Setenv("JAMF_URL", "")
			t.Setenv("JAMF_TOKEN", "")
			t.Setenv("JAMF_CLIENT_ID", "")
			t.Setenv("JAMF_CLIENT_SECRET", "")
			t.Setenv("JAMF_PROFILE", "")
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())

			root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
			root.SetArgs(args)
			root.SetOut(io.Discard)
			root.SetErr(io.Discard)

			if err := root.Execute(); err != nil {
				t.Errorf("%v with no credentials: %v", args, err)
			}
		})
	}
}
