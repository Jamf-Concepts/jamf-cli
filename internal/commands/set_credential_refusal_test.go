// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	platformgen "github.com/Jamf-Concepts/jamf-cli/internal/commands/platform/generated"
	securitygen "github.com/Jamf-Concepts/jamf-cli/internal/commands/security/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/security"
)

const fakeSecret = "FAKE-SECRET-do-not-use-0000"

// TestGeneratedSetRefusesCredentialBodyFields holds the Platform and Security
// Cloud generated --set to the credential-input policy Classic --set already
// enforces: a secret-bearing body field must be refused before any request is
// built, with --from-file or stdin named as the route.
func TestGeneratedSetRefusesCredentialBodyFields(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cases := []struct {
		name   string
		newCmd func(t *testing.T, received *[]string) *cobra.Command
		args   []string
	}{
		{
			name: "security uem-connectors create deviceSyncAuth.clientSecret",
			newCmd: func(t *testing.T, received *[]string) *cobra.Command {
				sdk, mux := newTestPlatformSDK(t)
				mux.HandleFunc("/securitycloud/uem-connect/v1/connectors", recordBody(received))
				return platformgen.NewUemConnectorsCmd(&registry.CLIContext{PlatformSDKClient: sdk, Output: &captureOutput{}})
			},
			args: []string{
				"create", "--set", "vendor=JAMF_PRO", "--set", "authStrategy=JAMF_PRO_OAUTH",
				"--set", "deviceSyncAuth.clientId=id", "--set", "deviceSyncAuth.clientSecret=" + fakeSecret,
			},
		},
		{
			name: "security uem-connectors create deviceSyncAuth.password",
			newCmd: func(t *testing.T, received *[]string) *cobra.Command {
				sdk, mux := newTestPlatformSDK(t)
				mux.HandleFunc("/securitycloud/uem-connect/v1/connectors", recordBody(received))
				return platformgen.NewUemConnectorsCmd(&registry.CLIContext{PlatformSDKClient: sdk, Output: &captureOutput{}})
			},
			args: []string{
				"create", "--set", "vendor=JAMF_PRO", "--set", "authStrategy=USERNAME_PASSWORD",
				"--set", "deviceSyncAuth.username=svc", "--set", "deviceSyncAuth.password=" + fakeSecret,
			},
		},
		{
			name: "security uem-connectors create deviceSyncAuth as a JSON object",
			newCmd: func(t *testing.T, received *[]string) *cobra.Command {
				sdk, mux := newTestPlatformSDK(t)
				mux.HandleFunc("/securitycloud/uem-connect/v1/connectors", recordBody(received))
				return platformgen.NewUemConnectorsCmd(&registry.CLIContext{PlatformSDKClient: sdk, Output: &captureOutput{}})
			},
			args: []string{
				"create", "--set", "vendor=JAMF_PRO", "--set", "authStrategy=JAMF_PRO_OAUTH",
				"--set", `deviceSyncAuth={"clientId":"id","clientSecret":"` + fakeSecret + `"}`,
			},
		},
		{
			name: "security stream update delivery.authorization_header",
			newCmd: func(t *testing.T, received *[]string) *cobra.Command {
				mux := http.NewServeMux()
				mux.HandleFunc("/v1/login", func(w http.ResponseWriter, _ *http.Request) {
					claims := base64.RawURLEncoding.EncodeToString([]byte(`{"customer_id":"cust-1","exp":9999999999}`))
					writeJSON(w, map[string]any{"token": "h." + claims + ".s"})
				})
				mux.HandleFunc("/", recordBody(received))
				srv := httptest.NewServer(mux)
				t.Cleanup(srv.Close)
				sc := security.NewClient(
					security.WithAPIBaseURL(srv.URL),
					security.WithSSEBaseURL(srv.URL),
					security.WithRiskCredentials("id", "secret"),
					security.WithSSECredentials("id", "secret"),
				)
				return securitygen.NewStreamCmd(&registry.CLIContext{SecurityClient: sc, Output: &captureOutput{}})
			},
			args: []string{
				"update", "--set", "delivery.method=urn:ietf:rfc:8935",
				"--set", "delivery.authorization_header=Bearer " + fakeSecret,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var received []string
			cmd := tc.newCmd(t, &received)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tc.args)
			err := cmd.Execute()

			for _, b := range received {
				if strings.Contains(b, fakeSecret) {
					t.Errorf("server received the --set credential in the request body: %s", b)
				}
			}
			if err == nil {
				t.Fatalf("--set accepted a credential body field from argv; want a refusal naming --from-file")
			}
			if !strings.Contains(err.Error(), "--from-file") {
				t.Errorf("refusal = %q, want it to name --from-file as the route for the secret", err)
			}
		})
	}
}

// TestGeneratedSetStillAcceptsNonSecretSiblings is the control: the refusal
// must not reach deviceSyncAuth.clientId or username.
func TestGeneratedSetStillAcceptsNonSecretSiblings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var received []string
	sdk, mux := newTestPlatformSDK(t)
	mux.HandleFunc("/securitycloud/uem-connect/v1/connectors", recordBody(&received))
	cmd := platformgen.NewUemConnectorsCmd(&registry.CLIContext{PlatformSDKClient: sdk, Output: &captureOutput{}})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"create", "--set", "vendor=JAMF_PRO", "--set", "deviceSyncAuth.clientId=id", "--set", "deviceSyncAuth.username=svc"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("non-secret --set refused: %v", err)
	}
	if len(received) != 1 || !strings.Contains(received[0], `"clientId":"id"`) {
		t.Fatalf("server received %q, want one body carrying clientId", received)
	}
}

func recordBody(received *[]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r.Body)
		if buf.Len() > 0 {
			*received = append(*received, buf.String())
		}
		writeJSONStatus(w, http.StatusCreated, map[string]any{"id": "c-1", "href": "/x"})
	}
}
