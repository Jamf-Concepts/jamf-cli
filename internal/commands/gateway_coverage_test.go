// Copyright 2026, Jamf Software LLC

package commands

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/gateway"
	"github.com/Jamf-Concepts/jamf-cli/internal/privileges"
)

func cmdWith(ann map[string]string) *cobra.Command {
	root := &cobra.Command{Use: "jamf-cli"}
	pro := &cobra.Command{Use: "pro"}
	leaf := &cobra.Command{Use: "list", Annotations: ann}
	pro.AddCommand(leaf)
	root.AddCommand(pro)
	return leaf
}

func TestCheckAPIMatchRefusesAProbedProEndpointOnAGatewayProfile(t *testing.T) {
	cmd := cmdWith(map[string]string{
		annotationAPI:           apiPro,
		annotationGateway:       string(gateway.Unserved),
		annotationGatewayBasis:  string(gateway.BasisProbe),
		annotationGatewayDetail: "wire-confirmed unserved on EU and US, 2026-08-28",
	})
	err := checkAPIMatch(cmd, &auth.PlatformOAuth2Provider{}, "platform-ga")
	if err == nil {
		t.Fatal("a wire-probed unserved endpoint was allowed through on a gateway profile")
	}
	// Usage, not NotFound: a script that iterates commands treats 4 as "no such
	// object, carry on" and would swallow this.
	var ec *exitcode.Error
	if !asExitcode(err, &ec) || ec.Code != exitcode.Usage {
		t.Errorf("exit code: got %v, want %d", err, exitcode.Usage)
	}
	if !strings.Contains(err.Error(), "pro list") {
		t.Errorf("the refusal does not name the command: %v", err)
	}
}

// An endpoint outside the published surface is refused too, and the message has
// the harder job: the gateway may still route it today, so a bare "not served"
// reads as a CLI bug to anyone whose command works. It has to say the routing is
// transitional and that is why it is being stopped now.
func TestCheckAPIMatchRefusesAnUnpublishedEndpointAndExplainsWhy(t *testing.T) {
	cmd := cmdWith(map[string]string{
		annotationAPI:           apiPro,
		annotationGateway:       string(gateway.Unserved),
		annotationGatewayBasis:  string(gateway.BasisUnpublished),
		annotationGatewayDetail: "not declared by the gateway's Jamf Pro API 11.31.0",
	})
	err := checkAPIMatch(cmd, &auth.PlatformOAuth2Provider{}, "platform-ga")
	if err == nil {
		t.Fatal("an endpoint outside the gateway's published API was allowed through")
	}
	for _, want := range []string{"published API", "may answer today", "transitional"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not explain %q: %v", want, err)
		}
	}
}

func TestCheckAPIMatchAllowsAServedProEndpointOnEitherProfile(t *testing.T) {
	cmd := cmdWith(map[string]string{annotationAPI: apiPro})
	for _, p := range []auth.Provider{&auth.PlatformOAuth2Provider{}, &auth.OAuth2Provider{}} {
		if err := checkAPIMatch(cmd, p, "p"); err != nil {
			t.Errorf("%T: %v", p, err)
		}
	}
}

func TestCheckAPIMatchRefusesAPlatformCommandOnAnInstanceProfile(t *testing.T) {
	cmd := cmdWith(map[string]string{annotationAPI: apiPlatformGateway})
	err := checkAPIMatch(cmd, &auth.OAuth2Provider{}, "pro-instance")
	if err == nil {
		t.Fatal("a Platform API command was allowed through on an instance profile")
	}
	// The generic RequirePlatformClient message explains how to set a platform
	// profile up but never says the profile in hand is an instance one, so an
	// operator with working credentials reads it as a credential problem.
	for _, want := range []string{`profile "pro-instance"`, "oauth2", "platform setup"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}
}

func TestCheckAPIMatchAllowsAPlatformCommandOnAGatewayProfile(t *testing.T) {
	cmd := cmdWith(map[string]string{annotationAPI: apiPlatformGateway})
	if err := checkAPIMatch(cmd, &auth.PlatformOAuth2Provider{}, "platform-ga"); err != nil {
		t.Fatalf("a Platform API command was refused on a gateway profile: %v", err)
	}
}

// An env-var invocation has no profile to blame, and "profile \"\"" reads as a
// bug rather than as a description of what happened.
func TestCheckAPIMatchDescribesEnvVarCredentials(t *testing.T) {
	cmd := cmdWith(map[string]string{annotationAPI: apiPlatformGateway})
	err := checkAPIMatch(cmd, &auth.TokenProvider{}, "")
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if strings.Contains(err.Error(), `""`) {
		t.Errorf("empty profile name leaked into the message: %v", err)
	}
	if !strings.Contains(err.Error(), "the active credentials") {
		t.Errorf("no description of where the credentials came from: %v", err)
	}
}

// resolveAuth's precedence is env-then-profile, but resolvedProfile is still
// whatever -p or default-profile names. Attributing the resolved auth method to
// that profile reported `profile "default" uses auth-method platform against the
// gateway` for a profile that is oauth2 against an instance — a message blaming
// the wrong thing, which is the failure this mechanism exists to remove.
func TestCredentialSourceNamesTheEnvironmentNotTheProfile(t *testing.T) {
	restore := func(id, sec, tok, tf string) func() {
		return func() { clientID, clientSecret, token, tokenFile = id, sec, tok, tf }
	}(clientID, clientSecret, token, tokenFile)
	defer restore()

	clientID, clientSecret, token, tokenFile = "", "", "", ""
	if got := credentialSource("default"); got != `profile "default"` {
		t.Errorf("profile-sourced: got %q", got)
	}
	if got := credentialSource(""); got != "the active credentials" {
		t.Errorf("nothing supplied: got %q", got)
	}

	clientID, clientSecret = "id", "secret"
	got := credentialSource("default")
	if strings.Contains(got, "default") {
		t.Errorf("env-sourced credentials were attributed to a profile: %q", got)
	}
	if !strings.Contains(got, "JAMF_CLIENT_ID") {
		t.Errorf("env-sourced: got %q", got)
	}

	clientID, clientSecret = "", ""
	token, tokenFile = "t", "/tmp/tok"
	if got := credentialSource("default"); !strings.Contains(got, "/tmp/tok") {
		t.Errorf("token-file-sourced: got %q", got)
	}
	tokenFile = ""
	if got := credentialSource("default"); !strings.Contains(got, "JAMF_TOKEN") {
		t.Errorf("token-env-sourced: got %q", got)
	}
}

// Both messages embed the credential source, which may be singular ("profile
// \"x\"") or plural ("the JAMF_* environment variables"), so neither may put a
// verb straight after it. Both did, and both read as broken English for the
// env-var case.
func TestMessagesReadCorrectlyForAPluralCredentialSource(t *testing.T) {
	restore := func(id, sec string) func() { return func() { clientID, clientSecret = id, sec } }(clientID, clientSecret)
	defer restore()
	clientID, clientSecret = "id", "secret"

	hint := profileHint("default", &auth.PlatformOAuth2Provider{})
	for _, bad := range []string{"variables selects", "variables uses"} {
		if strings.Contains(hint, bad) {
			t.Errorf("hint disagrees in number: %q", hint)
		}
	}

	err := checkAPIMatch(cmdWith(map[string]string{annotationAPI: apiPlatformGateway}),
		&auth.OAuth2Provider{}, "default")
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if strings.Contains(err.Error(), "variables authenticates") {
		t.Errorf("refusal disagrees in number: %v", err)
	}
}

func TestGatewayCoverageHelpTracksTheBasis(t *testing.T) {
	probe := gatewayCoverageHelp(cmdWith(map[string]string{
		annotationGateway:       string(gateway.Unserved),
		annotationGatewayBasis:  string(gateway.BasisProbe),
		annotationGatewayDetail: "wire-confirmed",
	}))
	if !strings.Contains(probe, gatewayHelpMarker) || !strings.Contains(probe, "not served") {
		t.Errorf("probe help text: %q", probe)
	}
	unpub := gatewayCoverageHelp(cmdWith(map[string]string{
		annotationGateway:       string(gateway.Unserved),
		annotationGatewayBasis:  string(gateway.BasisUnpublished),
		annotationGatewayDetail: "not declared",
	}))
	if !strings.Contains(unpub, gatewayHelpMarker) {
		t.Errorf("unpublished help text is missing the sentinel: %q", unpub)
	}
	// --help is where someone checks before writing a script, so it has to say
	// the command is refused rather than merely doubtful.
	for _, want := range []string{"refused", "transitional"} {
		if !strings.Contains(unpub, want) {
			t.Errorf("unpublished help text is missing %q: %q", want, unpub)
		}
	}
	if got := gatewayCoverageHelp(cmdWith(nil)); got != "" {
		t.Errorf("served command got a caveat: %q", got)
	}
}

// applyGatewayCoverageHelp must be idempotent: NewRootCmd is called more than
// once in tests and in the MCP server, and a doubled caveat is a visible defect.
func TestApplyGatewayCoverageHelpIsIdempotent(t *testing.T) {
	// Both bases, because the first sentinel matched only one of the two
	// wordings and let the other caveat double.
	for _, basis := range []gateway.Basis{gateway.BasisProbe, gateway.BasisUnpublished} {
		root := &cobra.Command{Use: "jamf-cli"}
		leaf := &cobra.Command{Use: "list", Short: "List things", Annotations: map[string]string{
			annotationGateway:       string(gateway.Unserved),
			annotationGatewayBasis:  string(basis),
			annotationGatewayDetail: "wire-confirmed",
		}}
		root.AddCommand(leaf)
		applyGatewayCoverageHelp(root)
		once := leaf.Long
		applyGatewayCoverageHelp(root)
		if leaf.Long != once {
			t.Errorf("%s: the caveat was appended twice:\n%s", basis, leaf.Long)
		}
	}
}

func asExitcode(err error, out **exitcode.Error) bool {
	e, ok := err.(*exitcode.Error)
	if ok {
		*out = e
	}
	return ok
}

// The catalog skipped every command named "commands", not just the root's own
// catalog command — so `pro mdm-commands commands` was absent from the
// machine-readable listing, gateway refusal and all.
func TestCommandsCatalogIncludesANestedCommandNamedCommands(t *testing.T) {
	root := NewRootCmd("test", "", "", "")
	entries := collectCommands(root, "", "", "")

	var found bool
	for _, e := range entries {
		if e.Command == "commands" {
			t.Error("the root catalog command listed itself")
		}
		if e.Command == "pro mdm-commands commands" {
			found = true
			if e.Gateway == "" {
				t.Errorf("%s listed without its gateway verdict", e.Command)
			}
		}
	}
	if !found {
		t.Error("pro mdm-commands commands is missing from the catalog")
	}
}

// The catalog carries both privilege vocabularies, because neither converts to
// the other: a Jamf Pro instance enforces API-role privilege names, the gateway
// enforces Jamf Account capability permissions, and which one an operator needs
// depends on the credential. Without both, sizing a Platform API integration
// from the catalog means provoking 403s.
func TestCommandsCatalogCarriesBothPrivilegeVocabularies(t *testing.T) {
	root := NewRootCmd("test", "", "", "")
	byName := map[string]commandEntry{}
	for _, e := range collectCommands(root, "", "", "") {
		byName[e.Command] = e
	}

	cases := []struct {
		command      string
		wantPro      []string
		wantGateway  []string
		wantNoGatewy bool
	}{
		// Modern Pro: both, and they do not resemble each other.
		{command: "pro categories delete", wantPro: []string{"Delete Categories"}, wantGateway: []string{"categories:delete"}},
		// Classic carries no Jamf Pro privilege data at all, and the gateway
		// scope is per method even though the coverage verdict is per resource.
		{command: "pro classic-account-groups update", wantGateway: []string{"accounts:update"}},
		{command: "pro classic-account-groups delete", wantGateway: []string{"accounts:delete"}},
		// apply has no spec operation of its own: it lists, then creates or
		// replaces, so it needs all three.
		{command: "pro scripts apply", wantGateway: []string{"scripts:create", "scripts:read", "scripts:update"}},
		// An endpoint the gateway does not publish declares no scope — the
		// absence is the honest answer, not "needs none".
		{command: "pro api-roles list", wantPro: []string{"Read API Roles"}, wantNoGatewy: true},
		// A Platform command's own privileges are already the capability
		// vocabulary, so it must not also claim a gateway set.
		{command: "pro blueprints list", wantNoGatewy: true},
	}

	for _, tc := range cases {
		e, ok := byName[tc.command]
		if !ok {
			t.Errorf("%s is missing from the catalog", tc.command)
			continue
		}
		if got := strings.Join(e.Privileges, ","); got != strings.Join(tc.wantPro, ",") && len(tc.wantPro) > 0 {
			t.Errorf("%s privileges = %q, want %q", tc.command, got, strings.Join(tc.wantPro, ","))
		}
		got := strings.Join(e.GatewayPrivileges, ",")
		if tc.wantNoGatewy {
			if got != "" {
				t.Errorf("%s gatewayPrivileges = %q, want none", tc.command, got)
			}
			continue
		}
		if got != strings.Join(tc.wantGateway, ",") {
			t.Errorf("%s gatewayPrivileges = %q, want %q", tc.command, got, strings.Join(tc.wantGateway, ","))
		}
	}
}

// Every gateway capability the catalog prints has to be renderable into the
// section and permission name Jamf Account shows, or the 403 hint built from the
// same slugs falls back to "no permission name recorded".
func TestCatalogGatewayPrivilegesAllHaveAPermissionName(t *testing.T) {
	root := NewRootCmd("test", "", "", "")
	seen := map[string]string{}
	for _, e := range collectCommands(root, "", "", "") {
		for _, slug := range e.GatewayPrivileges {
			if _, ok := seen[slug]; !ok {
				seen[slug] = e.Command
			}
		}
	}
	if len(seen) == 0 {
		t.Fatal("no gateway privileges in the catalog at all — the annotation is not being stamped")
	}
	for slug, command := range seen {
		reqs := privileges.Collect([]string{slug})
		if len(reqs) != 1 || reqs[0].Unknown {
			t.Errorf("%s (from %s) has no permission name recorded", slug, command)
		}
	}
}

// An API integration can only be created in Jamf Account's Platform API
// integrations UI, whose picker lists named permissions with a checkbox per
// action and shows a capability slug nowhere. So every command with a known
// capability requirement has to carry the wording that picker uses — including
// the Platform commands, whose own jamf:privileges already IS the capability
// vocabulary and so have no second slug list to render from.
func TestCommandsCatalogRendersPermissionsAsJamfAccountShowsThem(t *testing.T) {
	root := NewRootCmd("test", "", "", "")
	byName := map[string]commandEntry{}
	for _, e := range collectCommands(root, "", "", "") {
		byName[e.Command] = e
	}

	for _, tc := range []struct{ command, want string }{
		{"pro categories delete", "Organizational context > Categories: Delete (categories:delete)"},
		{"pro classic-account-groups delete", "Admin identity and access > Admin account: Delete (accounts:delete)"},
		// Platform-served, so its jamf:privileges is the capability list.
		{"pro blueprints list", "Deployment > Blueprints: Read (blueprints:read)"},
		{"pro platform-devices erase", "Device actions > Destructive device actions: Execute (destructive-device-actions:execute)"},
		{"security ztna-apps create", "Secure enterprise access > Zero-Trust Network Access (ZTNA): Create (ztna:create)"},
	} {
		e, ok := byName[tc.command]
		if !ok {
			t.Errorf("%s is missing from the catalog", tc.command)
			continue
		}
		if got := strings.Join(e.GatewayPermissions, " | "); got != tc.want {
			t.Errorf("%s gatewayPermissions = %q, want %q", tc.command, got, tc.want)
		}
	}

	// The actions of one permission collapse onto a single row, because that is
	// one row of the picker with several boxes ticked.
	if got := strings.Join(byName["pro scripts apply"].GatewayPermissions, " | "); got != "Deployment > Scripts: Create, Read, Update (scripts:create, scripts:read, scripts:update)" {
		t.Errorf("apply row = %q, want one row carrying all three actions", got)
	}
}

// A Classic resource the gateway still carries can have subcommands it no
// longer serves, and the whole-resource verdict reports those as fine.
//
// Classic API 11.28.0 is the case: it withdrew every read and write on
// patchsoftwaretitles while keeping POST /patchsoftwaretitles/id/{}, and
// withdrew GET /patchpolicies while keeping GET /patchpolicies/id/{}. So the
// resources are carried, and `classic-patch-titles list/get/update/delete` and
// `classic-patch-policies list` are dead. Each would otherwise pass the
// pre-flight refusal and go out to a bare 403 — the exact failure the refusal
// exists to pre-empt — while `jamf:gateway-privileges` named a permission that
// cannot make it work, because the subtree scope union still carries the read
// from the paths that survived.
func TestClassicSubcommandsRefusedWhenTheirMethodIsWithdrawn(t *testing.T) {
	root := NewRootCmd("test", "", "", "")
	byName := map[string]commandEntry{}
	for _, e := range collectCommands(root, "", "", "") {
		byName[e.Command] = e
	}

	refused := []string{
		"pro classic-patch-titles list",
		"pro classic-patch-titles get",
		"pro classic-patch-titles update",
		"pro classic-patch-titles delete",
		"pro classic-patch-titles apply",
		"pro classic-patch-policies list",
	}
	for _, name := range refused {
		e, ok := byName[name]
		if !ok {
			t.Errorf("%s is missing from the catalog", name)
			continue
		}
		if e.Gateway != string(gateway.Unserved) {
			t.Errorf("%s: gateway verdict %q, want %q", name, e.Gateway, gateway.Unserved)
		}
		if len(e.GatewayPrivileges) != 0 {
			t.Errorf("%s is refused and must name no permission, got %v", name, e.GatewayPrivileges)
		}
	}

	// The surviving method keeps working, and keeps its permission. Refusing
	// the whole resource would be the easy over-correction.
	served := []string{"pro classic-patch-titles create", "pro classic-patch-policies get"}
	for _, name := range served {
		e, ok := byName[name]
		if !ok {
			t.Errorf("%s is missing from the catalog", name)
			continue
		}
		if e.Gateway != "" {
			t.Errorf("%s: gateway verdict %q, want served", name, e.Gateway)
		}
		if len(e.GatewayPrivileges) == 0 {
			t.Errorf("%s is served and lost its gateway permission", name)
		}
	}
}
