// Copyright 2026, Jamf Software LLC

package commands

import (
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

var namesASecret = regexp.MustCompile(`(?i)token|secret|credential|password|private.?key`)

// proseNamingACredential are sentences the generators add to many Longs. Each
// names a credential without the command printing one, so it is removed before
// a leaf is judged and the leaf is still judged on everything else it says.
var proseNamingACredential = map[string]string{
	"Requires a profile pointed at a Jamf Pro instance (auth-method oauth2 or token).": "names the auth methods a profile can use",
	"resolved from the access token":                  "says the gateway reads the organization from this server's own token",
	"The credential must also be organization-scoped": "names the scope level this server's credential needs",
}

const (
	exemptDeviceSecret     = "allowed by the operator's decision: a secret of the pinned tenant, readable on purpose"
	exemptSetsDeviceSecret = "allowed by the operator's decision: sets or clears a secret of the pinned tenant's devices, readable over MCP anyway"
	exemptJCDS             = "allowed by the operator's decision: upload credentials for the pinned tenant's JCDS bucket"
	exemptWriteOnly        = "sends a credential in its request body and prints none back: the spec declares that field writeOnly"
	exemptClassicRead      = "a Classic read: its credential fields print as <redacted> in an MCP child"
	exemptClassicWrite     = "a Classic write: --set refuses credential fields, --from-file is held to --input-dir, and the response is the record ID"
	exemptOpensURL         = "prints or opens the product's web URL and makes no API request"
)

// notACredentialPrinter are the leaves whose name or help names a token,
// secret, credential or password and that run over MCP anyway, each with the
// reason. Keyed by command path without the root name. A stale entry fails.
var notACredentialPrinter = map[string]string{
	"dashboard": "refused by the run_command handler; generate_report runs it and returns only a path",

	"pro computer-inventory view-recovery-lock-password": exemptDeviceSecret,
	"pro local-admin-password password":                  exemptDeviceSecret,
	"pro local-admin-password password-by-guid":          exemptDeviceSecret,
	"pro local-admin-password audit":                     exemptDeviceSecret,
	"pro local-admin-password audit-by-guid":             exemptDeviceSecret,

	"pro computer-inventory set-auto-admin-password": exemptSetsDeviceSecret,
	"pro computer-inventory set-recovery-lock":       exemptSetsDeviceSecret,
	"pro mobile-devices clear-passcode":              exemptSetsDeviceSecret,
	"pro local-admin-password set-password":          exemptSetsDeviceSecret,
	"pro mobile-devices clear-restrictions-password": exemptSetsDeviceSecret,
	"pro mobile-devices patch":                       exemptSetsDeviceSecret,

	"pro jamf-cloud-distribution-service renew-credentials": exemptJCDS,
	"pro jamf-cloud-distribution-service-files create":      exemptJCDS,

	"pro cloud-ldap update":                                      "sends the keystore and its password in the request body; the response's keystore is CloudLdapKeystore, which carries only its name, type and expiry",
	"pro adcs-settings patch":                                    exemptWriteOnly,
	"pro adcs-settings validate-client-certificate":              exemptWriteOnly,
	"pro computer-prestages update":                              exemptWriteOnly,
	"pro digicert patch":                                         exemptWriteOnly,
	"pro digicert validate-client-certificate":                   exemptWriteOnly,
	"pro distribution-point patch":                               exemptWriteOnly,
	"pro distribution-point update":                              exemptWriteOnly,
	"pro enrollment update":                                      exemptWriteOnly,
	"pro gsx-connection patch":                                   exemptWriteOnly,
	"pro gsx-connection update":                                  exemptWriteOnly,
	"pro smtp-server update":                                     exemptWriteOnly,
	"pro sso-settings cert update":                               exemptWriteOnly,
	"pro sso-settings-cert update":                               exemptWriteOnly,
	"pro sso-settings oidc-broker-config update":                 exemptWriteOnly,
	"pro team-viewer-remote-administration patch":                exemptWriteOnly,
	"pro venafi patch":                                           exemptWriteOnly,
	"pro volume-purchasing-locations create":                     exemptWriteOnly,
	"pro volume-purchasing-locations patch":                      exemptWriteOnly,
	"pro device-enrollments create":                              "uploads an Automated Device Enrollment server token; the response is the instance, which carries only the token's expiry",
	"pro jamf-pro-initialization initialize-database-connection": "sends the database password during first-run setup and prints nothing back",

	"pro adcs-settings get":                                            "the spec says the response carries no password information",
	"pro sso-settings oidc-broker-config get":                          "the spec says secret fields are never included in the response",
	"pro api-authentication current":                                   "prints the current token's authorization details, not the token",
	"pro api-authentication list":                                      "prints the current token's authorization details, not the token",
	"pro api-authentication invalidate-token":                          "invalidates this server's own token and prints nothing",
	"pro api-integrations update":                                      "names the access token lifetime setting; client-credentials, which prints a secret, is refused",
	"pro csa delete":                                                   "deletes the CSA token exchange and prints nothing",
	"pro csa token delete":                                             "deletes the CSA token exchange and prints nothing",
	"pro csa token get":                                                "prints the exchange's scopes, expiry and tenant; the spec has no token field",
	"pro local-admin-password history":                                 "who viewed or rotated the password and when; the spec has no password field",
	"pro local-admin-password settings update":                         "LAPS rotation settings, not a password",
	"pro local-admin-password update":                                  "LAPS rotation settings, not a password",
	"pro enrollment-languages update":                                  "the password field is the prompt label shown at enrollment",
	"pro sso-settings update":                                          "names a SAML token-expiry switch",
	"pro patch-software-title-configurations create":                   "names the subscription the title comes from; prints no token",
	"pro account-driven-user-enrollment-session-token-settings get":    "the session token's lifetime settings, not a token",
	"pro account-driven-user-enrollment-session-token-settings update": "the session token's lifetime settings, not a token",
	"pro enrollment adue-session-token-settings get":                   "the session token's lifetime settings, not a token",
	"pro enrollment adue-session-token-settings update":                "the session token's lifetime settings, not a token",
	"pro blueprints import-profile":                                    "names the passcode payload type it converts",
	"school blueprints import-profile":                                 "names the passcode payload type it converts",
	"platform sso-connections create":                                  "names the tokenEndpointAuthMethod enum; the client secret is writeOnly",
	"platform sso-connections update":                                  "names the tokenEndpointAuthMethod enum; the client secret is writeOnly",
	"security uem-connectors create":                                   "names the USERNAME_PASSWORD auth strategy; the password is writeOnly",
	"protect restore":                                                  "names the resources a restore skips because their secret cannot be restored",

	"pro classic-jwt-configs get":    exemptClassicRead,
	"pro classic-jwt-configs list":   exemptClassicRead,
	"pro classic-jwt-configs delete": "deletes by ID and prints nothing",

	"pro open":      exemptOpensURL,
	"protect open":  exemptOpensURL,
	"school open":   exemptOpensURL,
	"security open": exemptOpensURL,
}

// isClassicWrite reports whether c is a generated Classic create, update or
// apply, every one of which prints the record ID the server answers with.
func isClassicWrite(c *cobra.Command) bool {
	if c.Annotations["jamf:api"] != "pro-classic" {
		return false
	}
	switch c.Name() {
	case "create", "update", "apply":
		return true
	}
	return false
}

// TestMCPSecretNamingLeaves_AreClassified fails on any leaf whose name, Short
// or Long names a token, secret, credential, password or private key unless it
// is refused over MCP or carries a reason above. A new generated leaf that
// matches fails here until someone decides which.
func TestMCPSecretNamingLeaves_AreClassified(t *testing.T) {
	root := NewRootCmd("test", "t", "t", "t")
	usedProse := map[string]bool{}
	usedExempt := map[string]bool{}
	classicWrites := 0
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, s := range c.Commands() {
			walk(s)
		}
		if c.HasSubCommands() {
			return
		}
		long := c.Long
		for p := range proseNamingACredential {
			if strings.Contains(long, p) {
				usedProse[p] = true
				long = strings.ReplaceAll(long, p, "")
			}
		}
		if !namesASecret.MatchString(c.Name() + "\n" + c.Short + "\n" + long) {
			return
		}
		path := strings.TrimPrefix(c.CommandPath(), root.Name()+" ")
		refused := refuseOverMCP(childInvocation{path: c.CommandPath()}, "prod") != nil
		_, exempt := notACredentialPrinter[path]
		switch {
		case refused && exempt:
			t.Errorf("%q is refused over MCP and also exempted in notACredentialPrinter; drop the exemption", path)
		case refused:
		case exempt:
			usedExempt[path] = true
		case isClassicWrite(c):
			classicWrites++
		default:
			t.Errorf("%q names a token, secret, credential or password and is neither refused over MCP nor classified: add it to mcpRefusedCommands if it prints a credential that works outside the server, or to notACredentialPrinter with the reason it does not", path)
		}
	}
	walk(root)
	for path, why := range notACredentialPrinter {
		if !usedExempt[path] {
			t.Errorf("notACredentialPrinter names %q (%s), which is not a leaf the walk flags; remove the entry", path, why)
		}
	}
	for p, why := range proseNamingACredential {
		if !usedProse[p] {
			t.Errorf("proseNamingACredential names %q (%s), which no Long contains any more; remove it", p, why)
		}
	}
	if classicWrites == 0 {
		t.Error("no Classic write matched; isClassicWrite has gone stale")
	}
}

func TestMCP_RefusesCommandsThatPrintOrSetALoginCredential(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"pro", "sso-oauth-session-tokens", "list"}, "access token"},
		{[]string{"pro", "cloud-distribution-point", "create", "--from-file", "x"}, "private key"},
		{[]string{"pro", "cloud-distribution-point", "patch"}, "private key"},
		{[]string{"pro", "jamf-pro-user-account-settings", "change-password"}, "password"},
		{[]string{"pro", "accounts", "create"}, "password"},
		{[]string{"pro", "accounts", "update", "1"}, "password"},
		{[]string{"pro", "accounts", "apply"}, "password"},
	} {
		_, err := buildChildArgs("prod", tc.args)
		if !isMCPRefusal(err) {
			t.Errorf("run_command accepts %q (err %v); it hands the model a credential that works outside the server", tc.args, err)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("refusal of %q should name the %s: %v", tc.args, tc.want, err)
		}
	}
}

func TestMCP_RefusesProtectDownloadsThatWriteKeyMaterial(t *testing.T) {
	for _, args := range [][]string{
		{"protect", "downloads", "csr"},
		{"protect", "downloads", "websocket-auth"},
	} {
		_, err := buildChildArgs("prod", args)
		if !isMCPRefusal(err) {
			t.Errorf("run_command accepts %q (err %v); it writes a .p12 into the server's working directory", args, err)
			continue
		}
		if !strings.Contains(err.Error(), ".p12") {
			t.Errorf("refusal of %q should say it writes a .p12: %v", args, err)
		}
	}
}

func TestMCP_KeepsTheOperatorAllowedSecretsAvailable(t *testing.T) {
	for _, args := range [][]string{
		{"pro", "jamf-cloud-distribution-service", "renew-credentials"},
		{"pro", "jamf-cloud-distribution-service-files", "create"},
		{"pro", "local-admin-password", "password", "mgmt-1", "admin"},
		{"pro", "computer-inventory", "view-recovery-lock-password", "1"},
		{"protect", "downloads", "installer"},
	} {
		if _, err := buildChildArgs("prod", args); isMCPRefusal(err) {
			t.Errorf("run_command refuses %q: %v; the operator chose to leave it available", args, err)
		}
	}
}
