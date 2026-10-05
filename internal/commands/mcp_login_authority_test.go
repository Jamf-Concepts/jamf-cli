// Copyright 2026, Jamf Software LLC

package commands

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// loginAuthorityResources are the resources whose writes decide who can log in
// to Jamf Pro: the account stores, the directories and identity providers an
// account or group is bound to, single sign-on, and the mail server that
// delivers password resets. Keyed by command path without the root name.
var loginAuthorityResources = []string{
	"pro accounts",
	"pro classic-account-users",
	"pro classic-account-groups",
	"pro classic-ldap-servers",
	"pro cloud-ldap",
	"pro cloud-azure",
	"pro cloud-idp",
	"pro classic-smtp-server",
	"pro smtp-server",
	"pro sso-settings",
	"pro sso-settings-cert",
	"platform sso-connections",
}

// loginAuthorityReadNames are leaf names that read, annotate or remove a login
// authority rather than set one, wherever they appear under the resources above.
var loginAuthorityReadNames = map[string]string{
	"get":              "reads the configuration",
	"list":             "reads the configuration",
	"history":          "reads the change history",
	"export":           "reads the configuration",
	"add-history-note": "adds a note to the change history",
	"delete":           "removes a login path; it adds none the model controls",
}

// loginAuthorityAllowedLeaves are the other leaves under those resources that
// run over MCP, each with the reason. A stale entry fails.
var loginAuthorityAllowedLeaves = map[string]string{
	"pro cloud-azure mappings":                "reads the default attribute mappings",
	"pro cloud-azure server-configuration":    "reads the default server configuration",
	"pro cloud-idp test-group":                "runs a test search against the configured provider",
	"pro cloud-idp test-user":                 "runs a test search against the configured provider",
	"pro cloud-idp test-user-membership":      "runs a test search against the configured provider",
	"pro cloud-ldap bind":                     "reads connection pool statistics",
	"pro cloud-ldap search":                   "reads connection pool statistics",
	"pro cloud-ldap defaults-mappings":        "reads the default attribute mappings",
	"pro cloud-ldap mappings":                 "reads the configured attribute mappings",
	"pro cloud-ldap server-configuration":     "reads the default server configuration",
	"pro cloud-ldap status":                   "tests the connection to the configured provider",
	"pro cloud-ldap verify":                   "validates a keystore and stores nothing",
	"pro smtp-server allowed-auth-types":      "reads the supported authentication types",
	"pro smtp-server test":                    "sends a test message through the configured server",
	"pro sso-settings dependencies":           "reads the enrollment customizations that use SSO",
	"pro sso-settings download":               "downloads Jamf Pro's own SAML metadata",
	"pro sso-settings failover":               "reads the failover URL, which reaches the password login page and grants no login",
	"pro sso-settings generate":               "replaces the failover URL, which reaches the password login page and grants no login",
	"pro sso-settings cert download":          "downloads the configured signing certificate",
	"pro sso-settings cert parse":             "parses a keystore and stores nothing",
	"pro sso-settings-cert download":          "downloads the configured signing certificate",
	"pro sso-settings-cert parse":             "parses a keystore and stores nothing",
	"pro sso-settings oidc-broker-config get": "reads the configuration",
}

// TestMCP_RefusesEveryWriteThatChangesWhoCanLogIn fails on any leaf under a
// login authority that runs over MCP without a reason above. A write the model
// can make there grants a Jamf Pro login that works outside this server: an
// account, a group bound to a directory, a directory or identity provider the
// model runs, an SSO identity provider, or the mail server that receives
// password resets.
func TestMCP_RefusesEveryWriteThatChangesWhoCanLogIn(t *testing.T) {
	root := NewRootCmd("test", "t", "t", "t")
	usedAllowed := map[string]bool{}
	for _, resource := range loginAuthorityResources {
		found, _, err := root.Find(strings.Fields(resource))
		if err != nil || found.CommandPath() != root.Name()+" "+resource {
			t.Errorf("loginAuthorityResources names %q, which resolves to %v (err %v)", resource, found, err)
			continue
		}
		leaves := 0
		var walk func(c *cobra.Command)
		walk = func(c *cobra.Command) {
			if c.HasSubCommands() {
				for _, s := range c.Commands() {
					walk(s)
				}
				return
			}
			if c.Annotations[noAuthAnnotation] == "true" {
				return
			}
			leaves++
			path := strings.TrimPrefix(c.CommandPath(), root.Name()+" ")
			_, allowed := loginAuthorityAllowedLeaves[path]
			if allowed {
				usedAllowed[path] = true
			}
			_, readName := loginAuthorityReadNames[c.Name()]
			refused := refuseOverMCP(childInvocation{path: c.CommandPath()}, "prod") != nil
			switch {
			case (allowed || readName) && refused:
				t.Errorf("%q is refused over MCP and also allowed here; drop the allowance", path)
			case !allowed && !readName && !refused:
				t.Errorf("%q writes a Jamf Pro login authority and runs over MCP: add it to mcpRefusedCommands, or to loginAuthorityAllowedLeaves with the reason it grants no login", path)
			}
		}
		walk(found)
		if leaves == 0 {
			t.Errorf("%q has no leaves; the walk checked nothing", resource)
		}
	}
	for path, why := range loginAuthorityAllowedLeaves {
		if !usedAllowed[path] {
			t.Errorf("loginAuthorityAllowedLeaves names %q (%s), which is not a leaf under a login authority; remove it", path, why)
		}
	}
}
