// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// classicProfileClient answers a Classic profile GET with a profile holding a
// Wi-Fi payload, and a group lookup with one platform group.
type classicProfileClient struct{ root, password, payloads string }

func (c classicProfileClient) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	var body string
	switch {
	case method == http.MethodGet && strings.HasPrefix(path, "/JSSResource/"):
		var esc strings.Builder
		payloads := c.payloads
		if payloads == "" {
			payloads = wifiProfilePlist(c.password)
		}
		_ = xml.EscapeText(&esc, []byte(payloads))
		body = `<` + c.root + `><general><id>7</id><name>Corp Wi-Fi</name><payloads>` + esc.String() + `</payloads></general>` +
			`<scope><computer_groups><computer_group><id>1</id><name>G</name></computer_group></computer_groups></scope></` + c.root + `>`
	case method == http.MethodGet && strings.HasPrefix(path, "/v2/groups"):
		body = `{"totalCount":1,"results":[{"groupPlatformId":"00000000-0000-0000-0000-000000000001","groupName":"G"}]}`
	default:
		return nil, fmt.Errorf("unexpected %s %s", method, path)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func runComponentsConfigProfile(t *testing.T, root string, args ...string) string {
	t.Helper()
	cliCtx, _, _ := newTestPlatformContext(t)
	cliCtx.Client = classicProfileClient{root: root, password: diffSecretPrefix + "psk"}
	cmd := newBlueprintsComponentsConfigProfileCmd(cliCtx)
	cmd.SetArgs(args)
	cmd.SetErr(io.Discard)
	var err error
	out := captureStdout(t, func() { err = cmd.Execute() })
	if err != nil {
		t.Fatalf("components configuration-profile %v: %v", args, err)
	}
	return out
}

var componentProfileCases = []struct {
	root string
	args []string
}{
	{"os_x_configuration_profile", []string{"--id", "7"}},
	{"configuration_profile", []string{"--id", "7", "--type", "mobile"}},
}

func TestComponentsConfigProfile_RedactsDownloadedPayloadSecretsInAnMCPChild(t *testing.T) {
	t.Setenv(mcpChildEnvVar, "1")
	for _, tc := range componentProfileCases {
		got := runComponentsConfigProfile(t, tc.root, tc.args...)
		if strings.Contains(got, diffSecretPrefix) {
			t.Errorf("components configuration-profile %v prints the Wi-Fi password over MCP:\n%s", tc.args, got)
		}
		for _, want := range []string{"redacted", "CorpWiFi", "SSID_STR"} {
			if !strings.Contains(got, want) {
				t.Errorf("components configuration-profile %v over MCP should still print %q:\n%s", tc.args, want, got)
			}
		}
	}
}

func TestComponentsConfigProfile_UndecodablePayloadFailsClosedInAnMCPChild(t *testing.T) {
	t.Setenv(mcpChildEnvVar, "1")
	cliCtx, _, _ := newTestPlatformContext(t)
	cliCtx.Client = classicProfileClient{root: "os_x_configuration_profile", payloads: "hello " + diffSecretPrefix + "psk"}
	cmd := newBlueprintsComponentsConfigProfileCmd(cliCtx)
	cmd.SetArgs([]string{"--id", "7"})
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	var err error
	out := captureStdout(t, func() { err = cmd.Execute() })
	if err == nil || !strings.Contains(err.Error(), "not printed over MCP") || strings.Contains(out, diffSecretPrefix) {
		t.Errorf("an undecodable payload should be refused over MCP, got err %v and output:\n%s", err, out)
	}
}

func TestComponentsConfigProfile_OutsideMCPPrintsTheDownloadedPayload(t *testing.T) {
	t.Setenv(mcpChildEnvVar, "")
	for _, tc := range componentProfileCases {
		if got := runComponentsConfigProfile(t, tc.root, tc.args...); !strings.Contains(got, diffSecretPrefix+"psk") || strings.Contains(got, "redacted") {
			t.Errorf("components configuration-profile %v outside MCP must print the payload unchanged:\n%s", tc.args, got)
		}
	}
}

// TestImportProfile_SendsTheRealPayloadInAnMCPChild holds the write side to the
// real value: import-profile copies the profile into a new blueprint, so a
// redacted copy would store the marker as the Wi-Fi password.
func TestImportProfile_SendsTheRealPayloadInAnMCPChild(t *testing.T) {
	t.Setenv(mcpChildEnvVar, "1")
	cliCtx, mux, _ := newTestPlatformContext(t)
	cliCtx.Client = classicProfileClient{root: "os_x_configuration_profile", password: diffSecretPrefix + "psk"}
	var mu sync.Mutex
	var created []string
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			created = append(created, string(b))
			mu.Unlock()
			writeJSONStatus(w, http.StatusCreated, map[string]any{"id": "bp-1", "href": "/bp-1"})
			return
		}
		writeJSON(w, map[string]any{"id": "bp-1", "name": "Corp Wi-Fi"})
	})
	cmd := newBlueprintsImportProfileCmd(cliCtx)
	cmd.SetArgs([]string{"7"})
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("import-profile: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(created) != 1 {
		t.Fatalf("import-profile sent %d create requests, want 1", len(created))
	}
	var decoded any
	if err := json.Unmarshal([]byte(created[0]), &decoded); err != nil {
		t.Fatalf("create body is not JSON: %v\n%s", err, created[0])
	}
	if !strings.Contains(created[0], diffSecretPrefix+"psk") || strings.Contains(created[0], "redacted") {
		t.Errorf("import-profile must send the real Wi-Fi password to the new blueprint:\n%s", created[0])
	}
}
