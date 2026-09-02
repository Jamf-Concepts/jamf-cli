// Copyright 2026, Jamf Software LLC

package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// The gateway host is the only signal an organization-scoped credential gives
// that platform auth is wanted: it names no tenant and no environment, so
// before this check JAMF_URL + client credentials fell through to oauth2
// against a URL that is not a Jamf Pro instance.
func TestIsPlatformGatewayURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://us.api.jamfcloud.com", true},
		{"https://eu.api.jamfcloud.com/", true},
		{"https://apac.api.jamfcloud.com", true},
		// A region Jamf adds later needs no code change.
		{"https://ca.api.jamfcloud.com", true},
		{"US.API.JAMFCLOUD.COM", true},
		{"https://us.api.jamfcloud.com:443/blueprints/v1", true},
		// Jamf Pro instances must not be read as the gateway.
		{"https://nmartin.jamfcloud.com", false},
		{"https://api.jamfcloud.com", false},
		{"https://eu.apigw.jamf.com", false},
		{"https://jamf.company.com", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := isPlatformGatewayURL(tc.url); got != tc.want {
			t.Errorf("isPlatformGatewayURL(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

// requireUSGateway must refuse before sending, because outside the US the
// gateway answers Tyk's bare "404 page not found" — no traceId, no envelope,
// nothing naming a region — which reads as a wrong path.
func TestRequireUSGateway(t *testing.T) {
	// A nil client is not this guard's business: --scaffold runs without auth,
	// and every other unauthenticated path has a clearer error waiting in
	// platform.RequirePlatformClient.
	if err := requireUSGateway(&registry.CLIContext{}); err != nil {
		t.Errorf("nil platform client should pass through, got %v", err)
	}
	if err := requireUSGateway(nil); err != nil {
		t.Errorf("nil context should pass through, got %v", err)
	}
}

func TestAnnotateDistributorScopeError(t *testing.T) {
	if got := annotateDistributorScopeError(nil); got != nil {
		t.Errorf("nil error should stay nil, got %v", got)
	}

	leaked := errors.New(`get: API request failed with status 400 Bad Request: {"error":"invalid_scope","error_description":"Invalid scopes: skyway-use2-product"}`)
	got := annotateDistributorScopeError(leaked)
	if !strings.Contains(got.Error(), "upstream fault") {
		t.Errorf("leaked invalid_scope should carry the note, got %q", got)
	}
	if !strings.Contains(got.Error(), "skyway-use2-product") {
		t.Errorf("note must not replace the original error, got %q", got)
	}

	// The second wire form, as of 2026-09-01: the account service's own
	// envelope, naming the service and carrying no scope. It replaced the form
	// above without the surface becoming usable, so it has to be matched too —
	// otherwise these commands answer a bare [UPSTREAM_ERROR] with no
	// explanation, which is what the annotation exists to prevent.
	newForm := errors.New(`get: API request failed with status 400 Bad Request: {"classification":"NONE","fields":[],"message":"[UPSTREAM_ERROR] Failed to retrieve distributor configuration via Skyway distributor service"}`)
	got = annotateDistributorScopeError(newForm)
	if !strings.Contains(got.Error(), "upstream fault") {
		t.Errorf("the UPSTREAM_ERROR form should carry the note, got %q", got)
	}
	if !strings.Contains(got.Error(), "Skyway distributor service") {
		t.Errorf("note must not replace the original error, got %q", got)
	}

	// Both markers are required for the OAuth form. A 400 from these endpoints
	// can legitimately mean a malformed purchase order, and a bare invalid_scope
	// from elsewhere is not this bug.
	for _, other := range []string{
		`400 [INVALID_FIELD] quoteNumber: must not be blank`,
		`400 {"error":"invalid_scope","error_description":"Invalid scopes: something-else"}`,
		`404 [DISTRIBUTOR_NOT_FOUND] not a registered distributor`,
		`400 [UPSTREAM_ERROR] Failed to reach some other service`,
	} {
		if got := annotateDistributorScopeError(errors.New(other)); got.Error() != other {
			t.Errorf("error %q should be untouched, got %q", other, got)
		}
	}
}

func TestAnnotateAuditScopeError(t *testing.T) {
	if got := annotateAuditScopeError(nil); got != nil {
		t.Errorf("nil error should stay nil, got %v", got)
	}

	missing := errors.New("sources: API request failed with status 400: [REQUEST_CONTEXT_NOT_PROVIDED] The request context could not be detected.")
	got := annotateAuditScopeError(missing)
	if !strings.Contains(got.Error(), "environment-scoped profile") {
		t.Errorf("missing scope should name the level that works, got %q", got)
	}

	// A tenant-scoped profile earns INVALID_REQUEST_CONTEXT_TYPE, which already
	// names both the level sent and the level expected — nothing to add.
	tenant := "sources: API request failed with status 400: [INVALID_REQUEST_CONTEXT_TYPE] Request context type 'tenant' is invalid. Expected any of 'environment'."
	if got := annotateAuditScopeError(errors.New(tenant)); got.Error() != tenant {
		t.Errorf("INVALID_REQUEST_CONTEXT_TYPE should be untouched, got %q", got)
	}
}

// Every Jamf Account leaf has to carry the US-only constraint in its own help,
// not only the parent's: `--help` on a leaf is reachable without a profile, and
// it is where someone works out which profile to make.
func TestAccountCommandsCarryUSOnlyHelp(t *testing.T) {
	cliCtx := &registry.CLIContext{}
	cmds := newAccountCmds(cliCtx)
	if len(cmds) != 7 {
		t.Fatalf("expected 7 Jamf Account resources, got %d", len(cmds))
	}
	var leaves int
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if !strings.Contains(c.Long, "US-only") {
			t.Errorf("%q Long does not mention the US-only constraint", c.CommandPath())
		}
		if c.RunE != nil {
			leaves++
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	for _, cmd := range cmds {
		walk(cmd)
	}
	if leaves == 0 {
		t.Fatal("no leaf commands found — the guard would be wrapping nothing")
	}
}

// The distributor note goes on the three distributor groups and nowhere else:
// licensing, deal registrations and SSO work today on an ordinary
// organization-scoped credential, and telling their users about a distributor
// registration would be noise.
func TestDistributorNoteOnlyOnDistributorCommands(t *testing.T) {
	for _, cmd := range newAccountCmds(&registry.CLIContext{}) {
		isDistributor := strings.HasPrefix(cmd.Name(), "distributor-")
		hasNote := strings.Contains(cmd.Long, "registered Jamf distributor")
		if isDistributor != hasNote {
			t.Errorf("%q: distributor=%v but note present=%v", cmd.Name(), isDistributor, hasNote)
		}
	}
}

// Audit is grouped and aliased apart from the account trio because it is
// environment-scoped and served in every region, so it must NOT carry the
// US-only guard.
func TestPlatformAuditIsNotUSOnly(t *testing.T) {
	cmd := newPlatformAuditCmd(&registry.CLIContext{})
	if strings.Contains(cmd.Long, "US-only") {
		t.Error("audit is served in every region; its help must not claim US-only")
	}
	if !strings.Contains(cmd.Long, "environment-scoped profile") {
		t.Error("audit help must name the one scope level that reaches it")
	}
}
