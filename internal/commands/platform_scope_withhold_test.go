// Copyright 2026, Jamf Software LLC

package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// withholdFixture is a config holding one platform profile that carries both a
// credential pair and a tenant ID — the shape every case below varies the
// environment against.
//
// Its client-id names a variable of its own, so a JAMF_CLIENT_ID in the
// environment is unambiguously a *different* integration's. The profile that
// names JAMF_CLIENT_ID itself is withholdFixtureNamingJAMFClientID below, and
// it is the case this fixture's shape hid: the rule used to key on a client ID
// merely being present, which read every profile using that documented shape as
// a foreign credential and dropped its scope.
func withholdFixture() *config.Config {
	return &config.Config{
		DefaultProfile: "gw",
		Profiles: map[string]config.Profile{
			"gw": {
				URL:        "https://eu.api.jamfcloud.com",
				AuthMethod: "platform",
				// env: refs because config.ResolveSecret refuses a bare value.
				ClientID:     "env:GW_TEST_CLIENT_ID",
				ClientSecret: "env:GW_TEST_CLIENT_SECRET",
				TenantID:     "profile-tenant",
			},
		},
	}
}

// withholdFixtureNamingJAMFClientID is the shape README documents on a platform
// profile: the credential lives in JAMF_CLIENT_ID/JAMF_CLIENT_SECRET and the
// profile references it. Those variables *have to* be set for the profile to
// resolve its own credential — config.ResolveSecret fails outright when they
// are not — so this profile is indistinguishable from a foreign credential to
// any rule that only asks whether a client ID was supplied, and its scope must
// still be used.
func withholdFixtureNamingJAMFClientID() *config.Config {
	return &config.Config{
		DefaultProfile: "gw",
		Profiles: map[string]config.Profile{
			"gw": {
				URL:          "https://eu.api.jamfcloud.com",
				AuthMethod:   "platform",
				ClientID:     "env:JAMF_CLIENT_ID",
				ClientSecret: "env:JAMF_CLIENT_SECRET",
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
		cfg          func() *config.Config
		env          map[string]string
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
			// The regression this rule broke, and the reason it cannot key on a
			// client ID merely being present: the profile names JAMF_CLIENT_ID
			// as its own client-id reference, so the variable is set on every
			// invocation that uses the profile at all, and dropping the scope
			// here sent `pro platform-devices list` out with no header for a
			// profile that had been working.
			//
			// Whatever that variable holds *is* this profile's client ID, by
			// its own declaration, so there is no foreign-credential variant of
			// this row to write: a profile naming JAMF_CLIENT_ID and an
			// invocation supplying it are the same integration by construction.
			name:     "the profile's own client-id reference is JAMF_CLIENT_ID",
			cfg:      withholdFixtureNamingJAMFClientID,
			env:      map[string]string{"JAMF_CLIENT_ID": "profile-client-id", "JAMF_CLIENT_SECRET": "s"},
			profile:  "gw",
			wantKind: auth.ScopeTenant,
			wantID:   "profile-tenant",
		},
		{
			// A profile referencing a variable of its own, with a foreign
			// client ID in JAMF_CLIENT_ID: the values differ, so the level is
			// still withheld. This is the pair that shows the rule compares
			// values rather than merely counting them — the case above and this
			// one differ only in which variable the profile names.
			name:         "JAMF_CLIENT_ID names a different integration than the profile",
			env:          map[string]string{"JAMF_CLIENT_ID": "other-client-id", "JAMF_CLIENT_SECRET": "s"},
			profile:      "gw",
			wantKind:     auth.ScopeOrganization,
			wantID:       "",
			wantWithheld: true,
		},
		{
			// The withheld record has to name the resolved profile even when
			// none was asked for: deleting the resolvedName plumbing leaves
			// `Profile "" carries a tenant ID` for every default-profile user,
			// and the note names nothing.
			name:         "client id from the environment, empty -p",
			env:          map[string]string{"JAMF_CLIENT_ID": "env-client-id", "JAMF_CLIENT_SECRET": "env-secret"},
			profile:      "",
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
			newCfg := tc.cfg
			if newCfg == nil {
				newCfg = withholdFixture
			}

			got := resolveScope(newCfg(), tc.profile)
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

	// And a profile whose own client-id reference is env:JAMF_CLIENT_ID keeps
	// its level. resolveAuth folds that variable into params.ClientID, so on
	// this ladder the documented profile shape arrives looking exactly like a
	// foreign credential — which is what broke it, and is why this half of the
	// rule has to compare against the profile too rather than testing
	// params.ClientID for emptiness.
	isolateScopeVars(t)
	t.Setenv("JAMF_CLIENT_ID", "profile-client-id")
	t.Setenv("JAMF_CLIENT_SECRET", "profile-client-secret")
	_, provider, err = ResolveAuthForProfile(withholdFixtureNamingJAMFClientID(), AuthParams{
		Profile:      "gw",
		ClientID:     "profile-client-id",
		ClientSecret: "profile-client-secret",
	})
	if err != nil {
		t.Fatalf("ResolveAuthForProfile: %v", err)
	}
	p, ok = provider.(*auth.PlatformOAuth2Provider)
	if !ok {
		t.Fatalf("provider is %T, want *auth.PlatformOAuth2Provider", provider)
	}
	if scope := p.Scope(); scope.Kind != auth.ScopeTenant || scope.ID != "profile-tenant" {
		t.Errorf("scope = {%v %q}, want the profile's tenant — the credential is the profile's own",
			scope.Kind, scope.ID)
	}
	if withheldProfileScope.Profile != "" {
		t.Errorf("nothing should have been withheld, got %q", withheldProfileScope.Profile)
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

	note := withheldScopeNote([]string{"tenant", "environment"})
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
	note = withheldScopeNote(nil)
	if !strings.Contains(note, "JAMF_ENVIRONMENT_ID") || !strings.Contains(note, "--environment-id") {
		t.Errorf("an environment-level withhold should name the environment inputs:\n%s", note)
	}

	// Nothing withheld, nothing said.
	resetWithheldProfileScope()
	if got := withheldScopeNote(nil); got != "" {
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

// A composed note must not recommend the level it has just said the command's
// API does not declare.
//
// scopeLevelNote defers its remedy to withheldScopeNote, and withheldScopeNote
// knew only which level had been withheld — so a tenant-carrying profile on an
// environment-only command produced "...declares environment scope, and this
// invocation is organization-scoped. Profile "gw" carries a tenant ID ... Supply
// the level for these credentials with JAMF_TENANT_ID or --tenant-id", and
// following that earns INVALID_REQUEST_CONTEXT_TYPE. Reproduced verbatim on
// `pro blueprints list`.
func TestTheWithheldNoteDoesNotOfferALevelTheCommandDoesNotDeclare(t *testing.T) {
	isolateScopeVars(t)
	t.Setenv("JAMF_CLIENT_ID", "env-client-id")
	recordWithheldProfileScope("gw", "tenant", "profile-tenant")

	note := withheldScopeNote([]string{"environment"})
	for _, unwanted := range []string{"JAMF_TENANT_ID", "--tenant-id"} {
		if strings.Contains(note, unwanted) {
			t.Errorf("the note offers %q for a command declaring environment scope only:\n%s", unwanted, note)
		}
	}
	for _, want := range []string{`"gw"`, "tenant ID", "declares environment scope"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note is missing %q:\n%s", want, note)
		}
	}

	// When the command does declare the withheld level, re-offering it is the
	// right answer and the remedy comes back.
	if note := withheldScopeNote([]string{"tenant", "environment"}); !strings.Contains(note, "JAMF_TENANT_ID") {
		t.Errorf("a declared level should still be offered:\n%s", note)
	}
}

// The withheld explanation has to reach a Pro or Classic command too.
//
// Only generator/platform stamps jamf:scopes, so AnnotateScopeLevelError used
// to return early for every one of the 700 Pro and 589 Classic operations —
// while the withhold rule drops their scope identically, so they hit the same
// 400 with the record fully populated and no note rendered. Reproduced: a
// default profile carrying tenant-id plus organization-scoped JAMF_* credentials
// gave `pro categories list` a bare REQUEST_CONTEXT_NOT_PROVIDED and
// `pro blueprints list` the same 400 plus the full note.
func TestAWithheldScopeIsExplainedOnACommandThatDeclaresNoLevels(t *testing.T) {
	isolateScopeVars(t)
	t.Setenv("JAMF_CLIENT_ID", "env-client-id")
	recordWithheldProfileScope("gw", "tenant", "profile-tenant")

	pro := &cobra.Command{Use: "list"} // no jamf:scopes — a Pro command
	gatewayErr := errors.New("API request failed with status 400: [REQUEST_CONTEXT_NOT_PROVIDED] no scope")

	got := AnnotateScopeLevelError(pro, gatewayErr)
	if !strings.Contains(got.Error(), `Profile "gw" carries a tenant ID`) {
		t.Errorf("a Pro command got no withheld note:\n%s", got)
	}
	if !errors.Is(got, gatewayErr) {
		t.Error("the note must wrap with %w — errors.As downstream is what classifies the exit code")
	}

	// Nothing withheld, nothing appended: the gateway's own message stands.
	resetWithheldProfileScope()
	if got := AnnotateScopeLevelError(pro, gatewayErr); got.Error() != gatewayErr.Error() {
		t.Errorf("an unannotated command with nothing withheld was rewritten:\n%s", got)
	}
	// And an unrelated error is left alone even with a withheld record.
	recordWithheldProfileScope("gw", "tenant", "profile-tenant")
	other := errors.New("404 not found")
	if got := AnnotateScopeLevelError(pro, other); got.Error() != other.Error() {
		t.Errorf("a non-scope error was annotated:\n%s", got)
	}
}

// resolveSchoolClient was the third copy of the ladder, and the one that still
// spliced: it filled the client credentials from JAMF_CLIENT_ID/SECRET and the
// tenant from the profile independently of one another, then built the platform
// client with auth.TenantScope. Nothing consulted profileScopeAppliesTo, and
// because the path recorded nothing, no withheld note could fire either.
//
// Asserted on PlatformSDKClient being nil rather than on an error: with the
// tenant withheld there is no scope to send, and school only builds the platform
// client when it has one.
func TestSchoolWithholdsAProfileScopeFromForeignCredentials(t *testing.T) {
	isolateScopeVars(t)
	saved := profile
	t.Cleanup(func() { profile = saved })
	profile = "sch"
	t.Setenv("JAMFSCHOOL_NETWORK_ID", "net")
	t.Setenv("JAMFSCHOOL_API_KEY", "key")
	t.Setenv("JAMFSCHOOL_PLATFORM_URL", "")
	t.Setenv("JAMF_URL", "")

	cfg := &config.Config{
		DefaultProfile: "sch",
		Profiles: map[string]config.Profile{
			"sch": {
				URL:          "https://school.jamfcloud.com",
				Product:      "school",
				PlatformURL:  "https://eu.api.jamfcloud.com",
				ClientID:     "env:SCHOOL_TEST_CLIENT_ID",
				ClientSecret: "env:SCHOOL_TEST_CLIENT_SECRET",
				TenantID:     "profile-tenant",
			},
		},
	}
	t.Setenv("SCHOOL_TEST_CLIENT_ID", "profile-client-id")
	t.Setenv("SCHOOL_TEST_CLIENT_SECRET", "profile-client-secret")

	// A different integration's credentials in the environment: the profile's
	// tenant is not theirs to carry.
	t.Setenv("JAMF_CLIENT_ID", "env-client-id")
	t.Setenv("JAMF_CLIENT_SECRET", "env-secret")

	cliCtx := &registry.CLIContext{}
	if err := resolveSchoolClient(cfg, cliCtx); err != nil {
		t.Fatalf("resolveSchoolClient: %v", err)
	}
	if cliCtx.PlatformSDKClient != nil {
		t.Error("the platform client was built with the profile's tenant and the environment's credentials")
	}
	if withheldProfileScope.Profile != "sch" || withheldProfileScope.Level != "tenant" {
		t.Errorf("withheld = {%q %q}, want {\"sch\" \"tenant\"} — a path that records nothing can render no note",
			withheldProfileScope.Profile, withheldProfileScope.Level)
	}

	// The profile's own credentials still get the profile's level.
	isolateScopeVars(t)
	cliCtx = &registry.CLIContext{}
	if err := resolveSchoolClient(cfg, cliCtx); err != nil {
		t.Fatalf("resolveSchoolClient: %v", err)
	}
	if cliCtx.PlatformSDKClient == nil {
		t.Error("the profile's own credentials should still reach the Platform API")
	}
	if withheldProfileScope.Profile != "" {
		t.Errorf("nothing should have been withheld, got %q", withheldProfileScope.Profile)
	}
}
