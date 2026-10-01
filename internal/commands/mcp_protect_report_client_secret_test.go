// Copyright 2026, Jamf Software LLC

package commands

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	fakeReportClientBearer = "Bearer report-client-secret-7f3a"
	fakeSentinelSharedKey  = "sentinel-shared-key-9c1d"
	fakeAPIClientPassword  = "protect-api-client-password-41be"
)

const fakeActionConfigJSON = `{"id":"ac-1","name":"siem","clients":[{"id":"c-1","type":"Http","supportedReports":["AlertV2"],"params":{"headers":[{"header":"Authorization","value":"` + fakeReportClientBearer + `"}],"method":"POST","url":"https://siem.example.invalid/ingest"}}]}`

const fakeDataForwardingJSON = `{"uuid":"org-1","forward":{"sentinel":{"enabled":true,"customerId":"cust-1","sharedKey":"` + fakeSentinelSharedKey + `","logType":"jamf","domain":"ods.opinsights.azure.com"}}}`

func newFakeProtectServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body, _ := io.ReadAll(r.Body)
		q := string(body)
		switch {
		case r.URL.Path == "/token":
			_, _ = io.WriteString(w, `{"access_token":"fake-protect-token","expires_in":3600,"token_type":"Bearer"}`)
		case strings.Contains(q, "updateActionConfigs"):
			_, _ = io.WriteString(w, `{"data":{"updateActionConfigs":`+fakeActionConfigJSON+`}}`)
		case strings.Contains(q, "listActionConfigs"):
			_, _ = io.WriteString(w, `{"data":{"listActionConfigs":{"items":[{"id":"ac-1","name":"siem"}],"pageInfo":{"next":null,"total":1}}}}`)
		case strings.Contains(q, "getActionConfigs"):
			_, _ = io.WriteString(w, `{"data":{"getActionConfigs":`+fakeActionConfigJSON+`}}`)
		case strings.Contains(q, "updateOrganizationForward"):
			_, _ = io.WriteString(w, `{"data":{"updateOrganizationForward":`+fakeDataForwardingJSON+`}}`)
		case strings.Contains(q, "sharedKey"):
			_, _ = io.WriteString(w, `{"data":{"getOrganization":`+fakeDataForwardingJSON+`}}`)
		case strings.Contains(q, "listApiClients"):
			_, _ = io.WriteString(w, `{"data":{"listApiClients":{"items":[{"clientId":"cid-1","name":"agent","created":"","assignedRoles":[],"password":""}],"pageInfo":{"next":null,"total":1}}}}`)
		case strings.Contains(q, "getApiClient"):
			_, _ = io.WriteString(w, `{"data":{"getApiClient":{"clientId":"cid-1","name":"agent","created":"","assignedRoles":[],"password":"`+fakeAPIClientPassword+`"}}}`)
		default:
			t.Errorf("unexpected Protect request %s: %s", r.URL.Path, q)
			_, _ = io.WriteString(w, `{"data":{}}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// useFakeProtect points the Protect credentials at the fake server with an
// isolated HOME, so the SDK's token cache (os.UserCacheDir ignores
// XDG_CACHE_HOME on darwin) stays out of the operator's home.
func useFakeProtect(t *testing.T) {
	t.Helper()
	srv := newFakeProtectServer(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("JAMFPROTECT_URL", srv.URL)
	t.Setenv("JAMFPROTECT_CLIENT_ID", "fake-client-id")
	t.Setenv("JAMFPROTECT_CLIENT_SECRET", "fake-client-secret")
}

func runProtectAsMCPChild(t *testing.T, args ...string) (string, error) {
	t.Helper()
	useFakeProtect(t)
	var err error
	out := captureStdout(t, func() {
		err = executeAsMCPChild(t, "", append([]string{"--no-input", "-o", "json"}, args...)...)
	})
	return out, err
}

// writeMCPInput writes body into a fresh directory the MCP child is told it may
// read from, and returns the file's path.
func writeMCPInput(t *testing.T, body string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "input.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(mcpInputDirEnvVar, dir)
	return path
}

func TestMCP_ProtectThirdPartySecretsDoNotReachTheModel(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   func(t *testing.T) []string
		secret string
	}{
		{"action-configs get", func(*testing.T) []string { return []string{"protect", "action-configs", "get", "siem"} }, fakeReportClientBearer},
		{"action-configs apply", func(t *testing.T) []string {
			return []string{"protect", "action-configs", "apply", "--yes", "--from-file", writeMCPInput(t, `{"name":"siem","description":""}`)}
		}, fakeReportClientBearer},
		{"data-forwarding get", func(*testing.T) []string { return []string{"protect", "data-forwarding", "get"} }, fakeSentinelSharedKey},
		{"data-forwarding update", func(t *testing.T) []string {
			return []string{"protect", "data-forwarding", "update", "--from-file", writeMCPInput(t, `{}`)}
		}, fakeSentinelSharedKey},
		{"api-clients get", func(*testing.T) []string { return []string{"protect", "api-clients", "get", "agent"} }, fakeAPIClientPassword},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := tc.args(t)
			// The server judges --from-file against its own --input-dir, which this test
			// does not start; the child judges it against the directory writeMCPInput set.
			if !slices.Contains(args, "--from-file") {
				if _, err := buildChildArgs("prod", args); err != nil {
					t.Fatalf("%q is an inspection path the operator keeps over MCP: %v", args, err)
				}
			}
			out, err := runProtectAsMCPChild(t, args...)
			if err != nil {
				t.Fatalf("%q failed in the MCP child: %v\n%s", args, err, out)
			}
			if strings.Contains(out, tc.secret) {
				t.Errorf("%q printed a credential to the model in an MCP child:\n%s", args, out)
			}
			if !strings.Contains(out, protectRedacted) {
				t.Errorf("%q should mark the withheld value as %s, so the model knows one is set:\n%s", args, protectRedacted, out)
			}
		})
	}

	out, err := runProtectAsMCPChild(t, "protect", "action-configs", "list")
	if err != nil || !strings.Contains(out, `"siem"`) {
		t.Errorf("control: `protect action-configs list` must still work in an MCP child (err %v):\n%s", err, out)
	}
}

func TestProtectActionConfigsGet_OutsideMCPPrintsTheHeaderValue(t *testing.T) {
	useFakeProtect(t)
	resetGlobals()
	t.Cleanup(resetGlobals)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("JAMF_CLI_ARGS", "")
	t.Setenv(mcpChildEnvVar, "")
	var err error
	out := captureStdout(t, func() {
		root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
		root.SetArgs([]string{"--no-input", "-o", "json", "protect", "action-configs", "get", "siem"})
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		err = root.Execute()
	})
	if err != nil || !strings.Contains(out, fakeReportClientBearer) {
		t.Errorf("outside an MCP child the operator's own output is unchanged and carries the header value (err %v):\n%s", err, out)
	}
}

func TestMCP_RefusesCommandsWhoseOutputIsACredential(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"protect", "action-configs", "export", "siem"}, "header"},
		{[]string{"protect", "ac", "export", "siem"}, "header"},
		{[]string{"pro", "api-integrations", "client-credentials", "7"}, "client secret"},
		{[]string{"pro", "ai", "client-credentials", "--name", "x"}, "client secret"},
		{[]string{"protect", "api-clients", "apply"}, "password"},
		{[]string{"-o", "json", "protect", "apic", "apply"}, "password"},
	} {
		_, err := buildChildArgs("prod", tc.args)
		if !isMCPRefusal(err) {
			t.Errorf("run_command accepts %q (err %v); its output carries a credential the model must not receive", tc.args, err)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("refusal of %q should name the %s it withholds: %v", tc.args, tc.want, err)
		}
	}
}
