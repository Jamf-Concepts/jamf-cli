// Copyright 2026, Jamf Software LLC

package commands

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// loginAuthorityResources are the resources whose writes decide who can log in
// to a Jamf product and what that login may do: the account and user stores,
// their groups and roles, the directories and identity providers they are
// bound to, single sign-on, and the mail server that delivers password resets.
// Keyed by command path without the root name.
var loginAuthorityResources = []string{
	"pro accounts",
	"pro account-groups",
	"pro classic-accounts",
	"pro classic-account-users",
	"pro classic-account-groups",
	"pro classic-ldap",
	"pro classic-ldap-servers",
	"pro ldap",
	"pro cloud-ldap",
	"pro cloud-azure",
	"pro cloud-idp",
	"pro classic-smtp-server",
	"pro smtp-server",
	"pro sso-settings",
	"pro sso-settings-cert",
	"pro oidc",
	"platform sso-connections",
	"protect users",
	"protect groups",
	"protect roles",
	"protect connections",
	"protect restore",
	"school users",
	"school groups",
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
	"pro ldap groups":                         "searches the configured access groups",
	"pro ldap ldap-servers":                   "reads the configured LDAP servers",
	"pro ldap servers":                        "reads the configured LDAP servers and cloud identity providers",
	"pro oidc direct-idp-login-url":           "reads the URL that logs in through the configured IdP",
	"pro oidc dispatch":                       "returns the IdP redirect URL for an e-mail address and stores nothing",
	"pro oidc generate-certificate":           "replaces the keystore Jamf Pro signs its own OIDC messages with; the key is generated server-side and never returned, so it grants no login",
	"pro oidc public-features":                "reads the public OIDC configuration",
	"pro oidc public-key":                     "reads the public key Jamf Pro signs OIDC messages with",
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
// can make there grants a login that works outside this server: an account or
// user, a group or role that confers privilege, a directory or identity
// provider the model runs, an SSO identity provider, or the mail server that
// receives password resets.
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
				t.Errorf("%q writes a login authority and runs over MCP: add it to mcpRefusedCommands, or to loginAuthorityAllowedLeaves with the reason it grants no login", path)
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

// TestMCPPolicyTexts_NameEveryLoginAuthorityRefusal requires the `mcp serve`
// help, the run_command description and the MCP section of agent_context.md to
// name the resource of every refusedChangesLoginAuthority entry, so a refusal
// added to mcpRefusedCommands cannot go unadvertised to the operator or the
// model.
func TestMCPPolicyTexts_NameEveryLoginAuthorityRefusal(t *testing.T) {
	guide, err := os.ReadFile("agent_context.md")
	if err != nil {
		t.Fatal(err)
	}
	section := string(guide)
	start := strings.Index(section, "\n## MCP\n")
	if start < 0 {
		t.Fatal("agent_context.md has no '## MCP' section")
	}
	section = section[start+1:]
	if end := strings.Index(section, "\n## "); end >= 0 {
		section = section[:end]
	}
	texts := map[string]string{
		"mcp serve --help":               newMCPServeCmd().Long,
		"the run_command description":    loginAuthorityToolNote,
		"agent_context.md's MCP section": section,
	}
	checked := 0
	for _, refused := range mcpRefusedCommands {
		if refused.why != refusedChangesLoginAuthority {
			continue
		}
		checked++
		fields := strings.Fields(strings.TrimPrefix(refused.path, "jamf-cli "))
		product, resource := fields[0], fields[1]
		named := regexp.MustCompile(`'(` + regexp.QuoteMeta(product) + ` )?` + regexp.QuoteMeta(resource) + `['\s]`)
		for name, text := range texts {
			flat := strings.Join(strings.Fields(strings.ReplaceAll(text, "`", "'")), " ")
			if !named.MatchString(flat) {
				t.Errorf("%s does not name '%s %s', which mcpRefusedCommands refuses as a login-authority write", name, product, resource)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no refusedChangesLoginAuthority entry in mcpRefusedCommands; the check is vacuous")
	}
}
