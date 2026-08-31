// Copyright 2026, Jamf Software LLC

package commands

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/gateway"
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
