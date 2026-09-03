// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"io"
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
	// Unsupported, not Usage and not NotFound: the command is real and correctly
	// invoked, so 2 would be indistinguishable from a flag error, and a script
	// that iterates commands treats 4 as "no such object, carry on" and would
	// swallow this.
	var ec *exitcode.Error
	if !asExitcode(err, &ec) || ec.Code != exitcode.Unsupported {
		t.Errorf("exit code: got %v, want %d", err, exitcode.Unsupported)
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

// unservedLeaf is a refused leaf under a named resource, so a test can exercise
// the successor table against a real command path.
func unservedLeaf(resource, leafName string, basis gateway.Basis) (root, group, leaf *cobra.Command) {
	root = &cobra.Command{Use: "jamf-cli"}
	pro := &cobra.Command{Use: "pro"}
	group = &cobra.Command{Use: resource, Short: "Manage " + resource}
	leaf = &cobra.Command{Use: leafName, Short: "List things", Annotations: map[string]string{
		annotationAPI:           apiPro,
		annotationGateway:       string(gateway.Unserved),
		annotationGatewayBasis:  string(basis),
		annotationGatewayDetail: "not declared by the gateway's Jamf Pro API 11.31.0",
		// A leaf with no RunE is not Runnable, and everyLeafRefused ignores
		// those — a group of them would otherwise earn a caveat for nothing.
	}, RunE: func(*cobra.Command, []string) error { return nil }}
	group.AddCommand(leaf)
	pro.AddCommand(group)
	root.AddCommand(pro)
	return root, group, leaf
}

// A policy refusal has to be distinguishable from a malformed invocation. Both
// were exit 2, which is also every cobra flag error, unknown subcommand, missing
// URL, missing credential, retired host and scope conflict — so a wrapper script
// could not tell "this credential cannot reach this API" (degrade and carry on)
// from "you typed it wrong" (stop).
func TestBothDirectionsOfTheRefusalExitUnsupportedNotUsage(t *testing.T) {
	proOnGateway := cmdWith(map[string]string{
		annotationAPI:           apiPro,
		annotationGateway:       string(gateway.Unserved),
		annotationGatewayBasis:  string(gateway.BasisUnpublished),
		annotationGatewayDetail: "not declared by the gateway's Jamf Pro API 11.31.0",
	})
	platformOnInstance := cmdWith(map[string]string{annotationAPI: apiPlatformGateway})

	for _, tc := range []struct {
		name     string
		cmd      *cobra.Command
		provider auth.Provider
	}{
		{"Pro command on a gateway profile", proOnGateway, &auth.PlatformOAuth2Provider{}},
		{"Platform command on an instance profile", platformOnInstance, &auth.OAuth2Provider{}},
	} {
		err := checkAPIMatch(tc.cmd, tc.provider, "p")
		var ec *exitcode.Error
		if !asExitcode(err, &ec) {
			t.Fatalf("%s: not an exitcode error: %v", tc.name, err)
		}
		if ec.Code != exitcode.Unsupported {
			t.Errorf("%s: exit %d, want %d (unsupported)", tc.name, ec.Code, exitcode.Unsupported)
		}
		if ec.Code == exitcode.Usage {
			t.Errorf("%s: still exit 2, which a script cannot tell from a flag error", tc.name)
		}
	}
	if got := exitcode.CodeName(exitcode.Unsupported); got != "unsupported" {
		t.Errorf("CodeName(%d) = %q — the JSON envelope reports this name", exitcode.Unsupported, got)
	}
}

// The escape hatch exists because forceServed is a compile-time table: a
// customer whose pro policy-properties demonstrably answers 200 had no route at
// all after upgrading. It downgrades the refusal, and it has to be loud about it.
func TestAllowUnpublishedDowngradesTheRefusalToALoudWarning(t *testing.T) {
	t.Setenv(envAllowUnpublished, "1")
	_, _, leaf := unservedLeaf("api-roles", "list", gateway.BasisUnpublished)
	var stderr bytes.Buffer
	leaf.SetErr(&stderr)

	if err := checkAPIMatch(leaf, &auth.PlatformOAuth2Provider{}, "platform-ga"); err != nil {
		t.Fatalf("the override did not let the request proceed: %v", err)
	}
	warn := stderr.String()
	for _, want := range []string{
		"warning:",                    // it is a warning, not an aside
		"jamf-cli pro api-roles list", // which endpoint
		envAllowUnpublished,           // and why it was allowed
		"transitional",                // the route is not committed to
		"without notice",              // and can stop at any time
		"stopgap",                     // this is not a supported mode
	} {
		if !strings.Contains(warn, want) {
			t.Errorf("the warning does not say %q:\n%s", want, warn)
		}
	}
}

// Value-parsed, matching JAMF_CLI_NO_HINTS, so a CI runner that exports the
// variable unconditionally can turn it off without unsetting it.
func TestAllowUnpublishedIsValueParsed(t *testing.T) {
	for _, v := range []string{"0", "false", "", "maybe"} {
		t.Setenv(envAllowUnpublished, v)
		_, _, leaf := unservedLeaf("api-roles", "list", gateway.BasisUnpublished)
		leaf.SetErr(io.Discard)
		if err := checkAPIMatch(leaf, &auth.PlatformOAuth2Provider{}, "p"); err == nil {
			t.Errorf("%s=%q allowed the request through", envAllowUnpublished, v)
		}
	}
}

// A probed endpoint has no route at all, so allowing it buys the operator a bare
// 403 and nothing else. The override is for endpoints the gateway still answers.
func TestAllowUnpublishedDoesNotApplyToAProbedEndpoint(t *testing.T) {
	t.Setenv(envAllowUnpublished, "true")
	_, _, leaf := unservedLeaf("app-installer-titles", "list", gateway.BasisProbe)
	leaf.SetErr(io.Discard)
	err := checkAPIMatch(leaf, &auth.PlatformOAuth2Provider{}, "p")
	if err == nil {
		t.Fatal("a wire-probed unrouted endpoint was allowed through — the override only covers unpublished ones")
	}
}

// The refusal used to offer one remedy — provision a second credential against a
// Jamf Pro instance — even where swapping one command name on the profile in hand
// would do. Where a replacement ships in this same binary, say so first.
func TestTheRefusalNamesAWorkingSuccessorWhereOneShips(t *testing.T) {
	_, _, leaf := unservedLeaf("static-computer-groups", "list", gateway.BasisUnpublished)
	err := checkAPIMatch(leaf, &auth.PlatformOAuth2Provider{}, "platform-ga")
	if err == nil {
		t.Fatal("expected a refusal")
	}
	msg := err.Error()
	if !strings.Contains(msg, "jamf-cli pro computer-groups-static-groups") {
		t.Errorf("the refusal does not name the successor:\n%s", msg)
	}
	// The instance remedy stays, demoted: it is still the answer for anyone who
	// wants the withdrawn endpoint itself.
	if !strings.Contains(msg, "Failing that, run it against a Jamf Pro instance") {
		t.Errorf("the instance remedy went missing or was not demoted:\n%s", msg)
	}
	// A refused command with no curated successor must not gain a sentence.
	_, _, plain := unservedLeaf("api-roles", "list", gateway.BasisUnpublished)
	if err := checkAPIMatch(plain, &auth.PlatformOAuth2Provider{}, "p"); err == nil {
		t.Fatal("expected a refusal")
	} else if strings.Contains(err.Error(), "instead") {
		t.Errorf("a successor was invented for a command that has none:\n%s", err)
	}
}

// gatewayCoverageHelp fires only for a command carrying the verdict itself, and
// the verdict is stamped per operation — so the group node, which is the natural
// first `--help`, listed every subcommand with no caveat when all of them are
// refused.
func TestGroupHelpCarriesTheCaveatWhenEveryLeafIsRefused(t *testing.T) {
	root, group, _ := unservedLeaf("static-computer-groups", "list", gateway.BasisUnpublished)
	applyGatewayCoverageHelp(root)
	if !strings.Contains(group.Long, gatewayHelpMarker) {
		t.Fatalf("the group carries no caveat though every leaf is refused:\n%q", group.Long)
	}
	for _, want := range []string{"every subcommand here is refused", "jamf-cli pro computer-groups-static-groups"} {
		if !strings.Contains(group.Long, want) {
			t.Errorf("the group caveat does not say %q:\n%s", want, group.Long)
		}
	}
	// Idempotent, like the leaf note: NewRootCmd is built more than once.
	once := group.Long
	applyGatewayCoverageHelp(root)
	if group.Long != once {
		t.Errorf("the group caveat was appended twice:\n%s", group.Long)
	}
}

// A partially-refused group must stay quiet. That shape is real and growing — the
// gateway publishes POST /patchsoftwaretitles/id/{id} and nothing else on that
// resource — and a group-level caveat would read as covering the subcommands that
// work.
func TestGroupHelpStaysQuietWhenOnlySomeLeavesAreRefused(t *testing.T) {
	root, group, _ := unservedLeaf("classic-patch-titles", "list", gateway.BasisUnpublished)
	group.AddCommand(&cobra.Command{
		Use: "create", Short: "Create one",
		Annotations: map[string]string{annotationAPI: apiProClassic},
		RunE:        func(*cobra.Command, []string) error { return nil },
	})
	applyGatewayCoverageHelp(root)
	if strings.Contains(group.Long, gatewayHelpMarker) {
		t.Errorf("a group with a served subcommand was labelled refused:\n%s", group.Long)
	}
}

// A leafless grouping node earns nothing: everyLeafRefused is false with no
// runnable leaf beneath, or a bare namespace node would advertise a refusal it
// knows nothing about.
func TestGroupHelpNeedsARunnableLeaf(t *testing.T) {
	root := &cobra.Command{Use: "jamf-cli"}
	empty := &cobra.Command{Use: "shell", Short: "A group with nothing in it"}
	root.AddCommand(empty)
	applyGatewayCoverageHelp(root)
	if strings.Contains(empty.Long, gatewayHelpMarker) {
		t.Errorf("a leafless group earned a caveat:\n%s", empty.Long)
	}
}

// The successor table is hand-curated and nothing derives it, so the guard that
// it has not gone stale is this test. It fails three ways, and each is a real way
// an entry rots: the refused command disappears or is renamed, the replacement
// disappears or is renamed, or the gateway starts serving the refused command
// again and the entry becomes advice to abandon a working command.
func TestGatewaySuccessorsNameCommandsTheBinaryShips(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	for refused, replacement := range gateway.SuccessorTable() {
		refusedCmd := findCommandPath(t, root, refused)
		if refusedCmd == nil {
			t.Errorf("successor entry %q names no command this binary ships — remove it, or fix the path", refused)
			continue
		}
		replacementCmd := findCommandPath(t, root, replacement)
		if replacementCmd == nil {
			t.Errorf("the successor for %q is %q, which this binary does not ship — a refusal would send an operator to a command that does not exist", refused, replacement)
		} else if n, total := refusedLeafCount(replacementCmd); n > 0 {
			// SuccessorNote renders "It ships in this binary AND IS SERVED BY
			// THE GATEWAY", and until this check only the first half was
			// guarded: the test read the refused side's annotation and never
			// the replacement's. The one live entry is correct today, so this
			// closes a guard gap rather than a wrong answer — but the event
			// that withdrew the v2 endpoint is the same kind of event that
			// would withdraw v3, and that would leave the refusal recommending
			// a refused command inside a sentence promising it is served, with
			// every test green. note.go's own argument is that a wrong answer
			// here is worse than no answer, so the property that makes an
			// answer right is the one that has to be asserted.
			t.Errorf("the successor for %q is %q, and %d of its %d subcommands are themselves refused on a gateway profile — SuccessorNote claims the replacement \"is served by the gateway\", so this entry now renders a false promise. Point it at a served command or remove it",
				refused, replacement, n, total)
		}
		// "Any leaf", not "every leaf", and the difference is a live gap rather
		// than laxity: the synthesized `apply` carries no verdict in the
		// committed generated code, so a resource whose every spec operation is
		// withdrawn still ships one unannotated subcommand until the tree is
		// regenerated. An entry is stale when *nothing* under it is refused any
		// more — that is the state in which the advice becomes "abandon a working
		// command". Whether a group is refused in full is what the group-note
		// tests assert, over a synthetic tree that cannot drift with a regenerate.
		if n, total := refusedLeafCount(refusedCmd); n == 0 {
			t.Errorf("none of %q's %d subcommands are refused on a gateway profile any more, so the successor entry pointing at %q is now advice to abandon a working command — remove the entry", refused, total, replacement)
		}
	}
}

// refusedLeafCount counts the runnable leaves beneath cmd and how many of them
// the gateway refuses.
func refusedLeafCount(cmd *cobra.Command) (refused, total int) {
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		subs := c.Commands()
		if len(subs) == 0 {
			if !c.Runnable() {
				return
			}
			total++
			if gateway.Level(c.Annotations[annotationGateway]) == gateway.Unserved {
				refused++
			}
			return
		}
		for _, sub := range subs {
			walk(sub)
		}
	}
	walk(cmd)
	return refused, total
}

// findCommandPath resolves a space-separated command path under root, or nil.
// cobra's Find is used so an alias resolves the same way a user's invocation
// would, and the result is checked to be the command actually named rather than
// the deepest ancestor Find fell back to.
func findCommandPath(t *testing.T, root *cobra.Command, path string) *cobra.Command {
	t.Helper()
	args := strings.Fields(path)
	found, _, err := root.Find(args)
	if err != nil || found == nil {
		return nil
	}
	if !strings.HasSuffix(found.CommandPath(), " "+path) && found.CommandPath() != path {
		return nil // Find fell back to an ancestor
	}
	return found
}

// TestCatalogCarriesTheSuccessorForARefusedCommand: the JSON catalog is what a
// script or an agent reads instead of running --help, so the successor has to
// reach it too. Without this the catalog told a consumer the command was
// unserved and nothing more, while the runtime refusal named a working
// replacement — two answers to the same question.
func TestCatalogCarriesTheSuccessorForARefusedCommand(t *testing.T) {
	root := NewRootCmd("test", "commit", "date", "11.31.0")
	entries := collectCommands(root, "jamf-cli", "", "")

	var refused, withSuccessor int
	for _, e := range entries {
		if e.Gateway != string(gateway.Unserved) {
			// A served command must never advertise a replacement: that would
			// read as a deprecation this CLI is not making.
			if e.GatewaySuccessor != "" {
				t.Errorf("%s is served but names successor %q", e.Command, e.GatewaySuccessor)
			}
			continue
		}
		refused++
		if e.GatewaySuccessor == "" {
			continue
		}
		withSuccessor++
		// The successor must be a command the binary actually ships, or the
		// catalog sends a consumer to something that does not exist.
		fields := strings.Fields(e.GatewaySuccessor)
		if len(fields) < 2 {
			t.Errorf("%s: successor %q is not a command path", e.Command, e.GatewaySuccessor)
			continue
		}
		if _, _, err := root.Find(fields[1:]); err != nil {
			t.Errorf("%s: successor %q is not a shipped command: %v", e.Command, e.GatewaySuccessor, err)
		}
	}

	if refused == 0 {
		t.Fatal("no refused command in the catalog — the coverage manifest is not being read")
	}
	if withSuccessor == 0 {
		t.Fatal("no refused command carries a successor; gateway.Successor is not reaching the catalog")
	}
}
