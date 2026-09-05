// Copyright 2026, Jamf Software LLC

package commands

import (
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/config"
)

// withholdFixture is a config holding one platform profile that carries both a
// credential pair and a tenant ID — the shape every case below varies the
// environment against.
func withholdFixture() *config.Config {
	return &config.Config{
		DefaultProfile: "gw",
		Profiles: map[string]config.Profile{
			"gw": {
				URL:        "https://eu.api.jamfcloud.com",
				AuthMethod: "platform",
				// env: refs because config.ResolveSecret refuses a bare
				// value. Deliberately not the JAMF_CLIENT_* names: those are
				// what the withhold rule keys on, and a profile whose secrets
				// happen to live there must not read as an invocation-supplied
				// credential.
				ClientID:     "env:GW_TEST_CLIENT_ID",
				ClientSecret: "env:GW_TEST_CLIENT_SECRET",
				TenantID:     "profile-tenant",
			},
		},
	}
}

// isolateScopeVars clears the package-level flag vars a scope resolution reads
// and restores them afterwards. They are globals bound to root's persistent
// flags, so a case that set one would otherwise leak into every later test in
// the package.
func isolateScopeVars(t *testing.T) {
	t.Helper()
	cid, csec, tok := clientID, clientSecret, token
	eid, tid := environmentID, tenantID
	clientID, clientSecret, token = "", "", ""
	environmentID, tenantID = "", ""
	resetWithheldProfileScope()
	t.Cleanup(func() {
		clientID, clientSecret, token = cid, csec, tok
		environmentID, tenantID = eid, tid
		resetWithheldProfileScope()
	})
	t.Setenv("JAMF_CLIENT_ID", "")
	t.Setenv("JAMF_CLIENT_SECRET", "")
	t.Setenv("JAMF_TENANT_ID", "")
	t.Setenv("JAMF_ENVIRONMENT_ID", "")
}

// A profile's scope level belongs to the profile's own integration, so it is
// attached only when the client ID came from that profile.
//
// An integration is created at exactly one level in Jamf Account and its
// credential carries that choice, so a level from one integration cannot be
// spliced onto another's credential. The case that exposed it: supply
// JAMF_URL + JAMF_CLIENT_ID + JAMF_CLIENT_SECRET for an organization-scoped
// integration while any default profile happens to carry a tenant-id, and the
// request went out with an X-Tenant-Id that appeared in no flag, no variable
// and no command the operator typed — an organization-scoped credential must
// send no scope header at all.
//
// Every case is asserted on the resolved auth.Scope rather than on an error
// string, because the defect was a header on the wire.
func TestAProfileScopeIsUsedOnlyWithThatProfilesCredentials(t *testing.T) {
	for _, tc := range []struct {
		name         string
		env          map[string]string
		flagCID      string
		profile      string
		wantKind     auth.ScopeKind
		wantID       string
		wantWithheld bool
	}{
		{
			// The ordinary case, and the regression risk: an empty -p resolves
			// to default-profile inside GetProfile, and reading the *requested*
			// name here once made the rule read as "there is no profile" and
			// dropped the scope of every default-profile user.
			name:     "profile credentials, empty -p",
			profile:  "",
			wantKind: auth.ScopeTenant,
			wantID:   "profile-tenant",
		},
		{
			name:     "profile credentials, explicit -p",
			profile:  "gw",
			wantKind: auth.ScopeTenant,
			wantID:   "profile-tenant",
		},
		{
			// The splice. Organization scope has no ID, so the zero value is
			// the whole point: no header is sent.
			name:         "client id from the environment",
			env:          map[string]string{"JAMF_CLIENT_ID": "env-client-id", "JAMF_CLIENT_SECRET": "env-secret"},
			profile:      "gw",
			wantKind:     auth.ScopeOrganization,
			wantID:       "",
			wantWithheld: true,
		},
		{
			// The remedy the note names: supply the level for these
			// credentials and it is used, profile untouched.
			name: "client id and level both from the environment",
			env: map[string]string{
				"JAMF_CLIENT_ID": "env-client-id", "JAMF_CLIENT_SECRET": "env-secret",
				"JAMF_ENVIRONMENT_ID": "env-scope",
			},
			profile:  "gw",
			wantKind: auth.ScopeEnvironment,
			wantID:   "env-scope",
		},
		{
			// A secret alone does not move the integration's identity. The
			// profile names the client ID, so it is still the profile's
			// integration — the "profile plus injected secret" CI shape the
			// config's own env: references already serve.
			name:     "only the secret from the environment",
			env:      map[string]string{"JAMF_CLIENT_SECRET": "env-secret"},
			profile:  "gw",
			wantKind: auth.ScopeTenant,
			wantID:   "profile-tenant",
		},
		{
			name:         "client id from the --client-id flag",
			flagCID:      "flag-client-id",
			profile:      "gw",
			wantKind:     auth.ScopeOrganization,
			wantID:       "",
			wantWithheld: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateScopeVars(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			clientID = tc.flagCID

			got := resolveScope(withholdFixture(), tc.profile)
			if got.Kind != tc.wantKind || got.ID != tc.wantID {
				t.Errorf("resolveScope = {%v %q}, want {%v %q}", got.Kind, got.ID, tc.wantKind, tc.wantID)
			}
			if withheld := withheldProfileScope.Profile != ""; withheld != tc.wantWithheld {
				t.Errorf("withheld recorded = %v, want %v (profile=%q level=%q)",
					withheld, tc.wantWithheld, withheldProfileScope.Profile, withheldProfileScope.Level)
			}
			if tc.wantWithheld && withheldProfileScope.Profile != "gw" {
				t.Errorf("withheld profile = %q, want the resolved name %q — an empty -p resolves to "+
					"default-profile, and the note has to name it", withheldProfileScope.Profile, "gw")
			}
		})
	}
}

// The `security` product returns from PersistentPreRunE before resolveAuth
// folds JAMF_CLIENT_ID into the package var, so a rule reading only the folded
// var silently does not apply on the path serving the gateway-served Security
// Cloud commands. That is the same structural trap that once left --tenant-id
// unread there while --url was honoured.
//
// Asserted by leaving the flag var empty and setting only the environment,
// which is exactly the state that path is in.
func TestTheWithholdRuleAppliesBeforeTheEnvVarsAreFolded(t *testing.T) {
	isolateScopeVars(t)
	t.Setenv("JAMF_CLIENT_ID", "env-client-id")
	t.Setenv("JAMF_CLIENT_SECRET", "env-secret")
	if clientID != "" {
		t.Fatal("clientID should be unfolded for this case to mean anything")
	}

	got := resolveScope(withholdFixture(), "gw")
	if got.Kind != auth.ScopeOrganization || got.ID != "" {
		t.Errorf("resolveScope = {%v %q}, want organization scope with no ID — the rule has to read "+
			"JAMF_CLIENT_ID directly, because nothing folded it on this path", got.Kind, got.ID)
	}
}

// ResolveAuthForProfile is the other ladder, and it has to agree with
// resolveScope: one is the pro/platform path and the other the
// security/school one, and a rule that holds on one only is how the
// --tenant-id divergence happened.
func TestResolveAuthForProfileWithholdsAProfileScopeToo(t *testing.T) {
	isolateScopeVars(t)
	t.Setenv("GW_TEST_CLIENT_ID", "profile-client-id")
	t.Setenv("GW_TEST_CLIENT_SECRET", "profile-client-secret")

	_, provider, err := ResolveAuthForProfile(withholdFixture(), AuthParams{
		Profile:      "gw",
		ClientID:     "env-client-id",
		ClientSecret: "env-secret",
	})
	if err != nil {
		t.Fatalf("ResolveAuthForProfile: %v", err)
	}
	p, ok := provider.(*auth.PlatformOAuth2Provider)
	if !ok {
		t.Fatalf("provider is %T, want *auth.PlatformOAuth2Provider", provider)
	}
	if scope := p.Scope(); scope.Kind != auth.ScopeOrganization || scope.ID != "" {
		t.Errorf("scope = {%v %q}, want organization scope with no ID", scope.Kind, scope.ID)
	}
	if withheldProfileScope.Profile != "gw" || withheldProfileScope.Level != "tenant" {
		t.Errorf("withheld = {%q %q}, want {\"gw\" \"tenant\"}",
			withheldProfileScope.Profile, withheldProfileScope.Level)
	}

	// And the profile's own credentials still get the profile's level.
	isolateScopeVars(t)
	_, provider, err = ResolveAuthForProfile(withholdFixture(), AuthParams{Profile: "gw"})
	if err != nil {
		t.Fatalf("ResolveAuthForProfile: %v", err)
	}
	p, ok = provider.(*auth.PlatformOAuth2Provider)
	if !ok {
		t.Fatalf("provider is %T, want *auth.PlatformOAuth2Provider", provider)
	}
	if scope := p.Scope(); scope.Kind != auth.ScopeTenant || scope.ID != "profile-tenant" {
		t.Errorf("scope = {%v %q}, want the profile's tenant", scope.Kind, scope.ID)
	}
	if withheldProfileScope.Profile != "" {
		t.Errorf("nothing was withheld, got %q", withheldProfileScope.Profile)
	}
}

// The note is the whole reason the drop is usable rather than merely correct:
// without it the request carries no scope header and the gateway's 400 names no
// remedy, while the level note beside it would describe the credential as
// organization-scoped — a claim about the credential this side cannot make,
// since a gateway token is opaque and carries an empty scope.
func TestTheWithheldNoteNamesTheProfileTheLevelAndTheRemedy(t *testing.T) {
	isolateScopeVars(t)
	t.Setenv("JAMF_CLIENT_ID", "env-client-id")
	recordWithheldProfileScope("gw", "tenant", "profile-tenant")

	note := withheldScopeNote()
	for _, want := range []string{
		`"gw"`,
		"tenant ID",
		"JAMF_CLIENT_ID environment variable",
		"JAMF_TENANT_ID",
		"--tenant-id",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("note is missing %q:\n%s", want, note)
		}
	}
	// The ID itself is recorded but deliberately not printed: it is the value
	// being ignored, and quoting it invites the reader to think it was used.
	if strings.Contains(note, "profile-tenant") {
		t.Errorf("the withheld ID should not be quoted back:\n%s", note)
	}

	// An environment-level profile flips both the variable and the flag named.
	recordWithheldProfileScope("gw", "environment", "profile-env")
	note = withheldScopeNote()
	if !strings.Contains(note, "JAMF_ENVIRONMENT_ID") || !strings.Contains(note, "--environment-id") {
		t.Errorf("an environment-level withhold should name the environment inputs:\n%s", note)
	}

	// Nothing withheld, nothing said.
	resetWithheldProfileScope()
	if got := withheldScopeNote(); got != "" {
		t.Errorf("note = %q, want empty when nothing was withheld", got)
	}
}

// The level note hands its remedy to the withheld note when there is one.
// Rendering both produced "no scope header was sent" twice and then advised
// setting an ID on the very profile whose ID had just been passed over.
func TestTheLevelNoteDefersItsRemedyToTheWithheldNote(t *testing.T) {
	withRemedy := scopeLevelNote([]string{"environment"}, "organization", false)
	if !strings.Contains(withRemedy, "Set an environment ID on the profile") {
		t.Errorf("with nothing withheld the level note owns the remedy:\n%s", withRemedy)
	}
	deferred := scopeLevelNote([]string{"environment"}, "organization", true)
	if strings.Contains(deferred, "Set an environment ID on the profile") {
		t.Errorf("with a withheld note the level note must not advise the profile:\n%s", deferred)
	}
	if !strings.Contains(deferred, "declares environment scope") {
		t.Errorf("the declared level still has to be stated:\n%s", deferred)
	}
}
