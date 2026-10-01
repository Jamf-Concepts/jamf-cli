// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	platformgen "github.com/Jamf-Concepts/jamf-cli/internal/commands/platform/generated"
	progen "github.com/Jamf-Concepts/jamf-cli/internal/commands/pro/generated"
	securitygen "github.com/Jamf-Concepts/jamf-cli/internal/commands/security/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

const dryRunSecret = "DRYRUN-CANARY-s3cr3t"

func writeBodyFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func assertPreviewRedacted(t *testing.T, stderr string) {
	t.Helper()
	if !strings.Contains(stderr, "[dry-run]") {
		t.Fatalf("no dry-run preview printed:\n%s", stderr)
	}
	if strings.Contains(stderr, dryRunSecret) {
		t.Errorf("--dry-run preview printed the credential verbatim:\n%s", stderr)
	}
}

func TestDryRunPreviewRedactsCredentials_ProAndClassic(t *testing.T) {
	cases := []struct {
		name string
		args func(t *testing.T) []string
	}{
		{
			name: "pro smtp-server update",
			args: func(t *testing.T) []string {
				return []string{"smtp-server", "update", "--set", "graphApiCredentials.clientSecret=" + dryRunSecret}
			},
		},
		{
			name: "classic-ldap-servers create",
			args: func(t *testing.T) []string {
				f := writeBodyFile(t, "ldap.xml", `<ldap_server><connection><name>corp</name><account><distinguished_username>cn=svc</distinguished_username><password>`+dryRunSecret+`</password></account></connection></ldap_server>`)
				return []string{"classic-ldap-servers", "create", "--from-file", f}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &registry.CLIContext{Client: &dryRunClient{inner: &mockHTTPClient{}}, Output: &captureOutput{}, DryRun: true}
			root := &cobra.Command{Use: "jamf-cli"}
			root.AddCommand(progen.NewSmtpServerCmd(ctx))
			progen.RegisterClassicCommands(root, ctx)
			root.SetArgs(tc.args(t))
			var execErr error
			stderr := captureStderr(t, func() { execErr = root.Execute() })
			if execErr != nil {
				t.Fatalf("execute: %v\n%s", execErr, stderr)
			}
			assertPreviewRedacted(t, stderr)
		})
	}
}

func TestDryRunPreviewRedactsCredentials_PlatformGateway(t *testing.T) {
	cases := []struct {
		name   string
		newCmd func(*registry.CLIContext) *cobra.Command
		body   string
	}{
		{
			name:   "pro uem-connectors create",
			newCmd: platformgen.NewUemConnectorsCmd,
			body:   `{"vendor":"JAMF_PRO","authStrategy":"JAMF_PRO_OAUTH","url":"https://x.jamfcloud.com","deviceSyncAuth":{"clientId":"c","clientSecret":"` + dryRunSecret + `"}}`,
		},
		{
			name:   "security ztna-gateways create (gateway-served)",
			newCmd: platformgen.NewZtnaGatewaysCmd,
			body:   `{"name":"gw","datacenter":"eu-west-1","ipsec":{"left":{"id":"a","secret":"` + dryRunSecret + `"}}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, _ := newTestPlatformSDK(t)
			ctx := &registry.CLIContext{PlatformSDKClient: sdk, Output: &captureOutput{}, DryRun: true}
			cmd := tc.newCmd(ctx)
			var stderr bytes.Buffer
			cmd.SetErr(&stderr)
			cmd.SetArgs([]string{"create", "--from-file", writeBodyFile(t, "body.json", tc.body)})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("execute: %v\n%s", err, stderr.String())
			}
			assertPreviewRedacted(t, stderr.String())
		})
	}
}

func TestDryRunPreviewRedactsCredentials_SecurityRadar(t *testing.T) {
	sc, hits := newDryRunSecurityClient(t)
	ctx := &registry.CLIContext{SecurityClient: sc, Output: &captureOutput{}, DryRun: true}
	cmd := securitygen.NewStreamCmd(ctx)
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	body := `{"aud":"https://r.example.com","delivery":{"method":"https://schemas.openid.net/secevent/risc/delivery-method/push","endpoint_url":"https://r.example.com/events","authorization_header":"Bearer ` + dryRunSecret + `"}}`
	cmd.SetArgs([]string{"update", "--from-file", writeBodyFile(t, "stream.json", body)})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, stderr.String())
	}
	if *hits != 0 {
		t.Errorf("server saw %d request(s) under --dry-run, want 0", *hits)
	}
	assertPreviewRedacted(t, stderr.String())
}
