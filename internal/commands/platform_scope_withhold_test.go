// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/platform"
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

// withholdFixtureWithRef is withholdFixture with the client-id reference
// replaced, so a case can say which reference form the profile carries. That is
// the axis the predicate turns on: the comparison is on the resolved *value*,
// and the only form it declines to resolve is keychain:, because resolving one
// can prompt on a path that by definition is not using the profile's
// credentials.
func withholdFixtureWithRef(clientIDRef func(*testing.T) string) func(*testing.T) *config.Config {
	return func(t *testing.T) *config.Config {
		cfg := withholdFixture()
		p := cfg.Profiles["gw"]
		p.ClientID = clientIDRef(t)
		cfg.Profiles["gw"] = p
		return cfg
	}
}

// fileRef writes id to a file under the subtest's temp dir and returns the
// `file:` reference naming it.
func fileRef(id string) func(*testing.T) string {
	return func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "client-id")
		if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
			t.Fatalf("writing the client-id file: %v", err)
		}
		return "file:" + path
	}
}

// literalRef is for a reference with nothing to write, like keychain:.
func literalRef(ref string) func(*testing.T) string {
	return func(*testing.T) string { return ref }
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
	resetPlatformScopeRecords()
	t.Cleanup(func() {
		clientID, clientSecret, token = cid, csec, tok
		environmentID, tenantID = eid, tid
		resetPlatformScopeRecords()
	})
	t.Setenv("JAMF_CLIENT_ID", "")
	t.Setenv("JAMF_CLIENT_SECRET", "")
	t.Setenv("JAMF_TENANT_ID", "")
	t.Setenv("JAMF_ENVIRONMENT_ID", "")
}

// Both per-resolution records are cleared together, and only one of them ever
// was. resolvedPlatformScope is written by newPlatformSDKClient and read by
// AnnotateScopeLevelError, so a second resolution that builds no client — a
// profile with no credentials, or school's tenant-less path — used to keep the
// first one's level and the note said "this invocation is tenant-scoped" about
// a credential this invocation never used.
func TestBothPlatformScopeRecordsAreClearedPerResolution(t *testing.T) {
	isolateScopeVars(t)
	saved := resolvedPlatformScope
	t.Cleanup(func() { resolvedPlatformScope = saved })

	// Stand in for a first resolution that built a tenant-scoped client and
	// withheld a profile's level.
	resolvedPlatformScope = auth.TenantScope("first-tenant")
	recordWithheldProfileScope("first", "tenant", "first-tenant")

	// A second resolution over a config with nothing to resolve.
	_, _, err := ResolveAuthForProfile(&config.Config{Profiles: map[string]config.Profile{}}, AuthParams{})
	if err == nil {
		t.Fatal("a config with no profile and no credentials should not resolve")
	}
	if got := resolvedPlatformScope.Kind; got != auth.ScopeOrganization {
		t.Errorf("resolvedPlatformScope.Kind = %v, want the zero value — no client was built, so "+
			"the previous resolution's level must not stand", got)
	}
	if resolvedPlatformScope.ID != "" {
		t.Errorf("resolvedPlatformScope.ID = %q, want empty", resolvedPlatformScope.ID)
	}
	if withheldProfileScope.Profile != "" {
		t.Errorf("withheldProfileScope = %q, want cleared", withheldProfileScope.Profile)
	}

	// The Security Cloud ladder is the third path resetPlatformScopeRecords'
	// doc comment covers, and it was the one that skipped the reset: it returns
	// nil, nil for a profile with no credentials *before* reaching resolveScope,
	// which is the only reset on that path. The 52 gateway-served Security
	// Cloud commands therefore inherited the previous resolution's level.
	resolvedPlatformScope = auth.TenantScope("first-tenant")
	recordWithheldProfileScope("first", "tenant", "first-tenant")
	saveURL := serverURL
	serverURL = ""
	t.Cleanup(func() { serverURL = saveURL })
	t.Setenv("JAMF_URL", "")

	client, err := securityPlatformSDKClient(&config.Config{Profiles: map[string]config.Profile{}}, "")
	if err != nil || client != nil {
		t.Fatalf("securityPlatformSDKClient = (%v, %v), want (nil, nil) for a config with no credentials", client, err)
	}
	if resolvedPlatformScope.Kind != auth.ScopeOrganization || resolvedPlatformScope.ID != "" {
		t.Errorf("resolvedPlatformScope = %+v, want the zero value — no client was built", resolvedPlatformScope)
	}
	if withheldProfileScope.Profile != "" {
		t.Errorf("withheldProfileScope = %q, want cleared on the security path too", withheldProfileScope.Profile)
	}
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
		name string
		// Takes a T because two rows write a client-id file, and a table
		// literal is evaluated before any subtest exists to own the temp dir.
		cfg          func(*testing.T) *config.Config
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
			cfg:      func(*testing.T) *config.Config { return withholdFixtureNamingJAMFClientID() },
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
			// A file: reference is a plain read, so it can answer the
			// same-integration question and the profile keeps its level. It was
			// lumped in with keychain: at first, which made a file-referencing
			// profile behave like a keychain one for no reason the code could
			// state — the reason for withholding is the prompt, not the
			// indirection.
			name:     "the profile's client-id is a file: reference naming the same integration",
			cfg:      withholdFixtureWithRef(fileRef("env-client-id")),
			env:      map[string]string{"JAMF_CLIENT_ID": "env-client-id", "JAMF_CLIENT_SECRET": "s"},
			profile:  "gw",
			wantKind: auth.ScopeTenant,
			wantID:   "profile-tenant",
		},
		{
			// The same form naming a different integration: values differ, so
			// the level is still withheld. Without this row the row above
			// passes for a predicate that returns true for any file: reference.
			name:         "a file: reference naming a different integration",
			cfg:          withholdFixtureWithRef(fileRef("some-other-id")),
			env:          map[string]string{"JAMF_CLIENT_ID": "env-client-id", "JAMF_CLIENT_SECRET": "s"},
			profile:      "gw",
			wantKind:     auth.ScopeOrganization,
			wantID:       "",
			wantWithheld: true,
		},
		{
			// keychain: is the shape `platform setup` writes, and it is
			// undecidable here: resolving it can prompt. So it reads as a
			// foreign integration and the level is withheld — a real cost,
			// documented in the CHANGELOG, and the reason withheldScopeNote has
			// to carry a usable remedy rather than merely being correct.
			//
			// This row is what makes the predicate's final `return false`
			// load-bearing: returning true from it left the whole
			// internal/commands suite green.
			name:         "the profile's client-id is a keychain: reference",
			cfg:          withholdFixtureWithRef(literalRef("keychain:gw/client-id")),
			env:          map[string]string{"JAMF_CLIENT_ID": "env-client-id", "JAMF_CLIENT_SECRET": "s"},
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
				newCfg = func(*testing.T) *config.Config { return withholdFixture() }
			}

			got := resolveScope(newCfg(t), tc.profile)
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

// Both levels read as English. "a %s ID" printed "carries a environment ID"
// for the level Jamf wants integrations created at — a note about a careful
// precedence rule, misspelling its own common case, and it shipped to the wire
// before anything read it back.
func TestTheWithheldNoteGetsTheArticleRight(t *testing.T) {
	isolateScopeVars(t)
	for level, want := range map[string]string{
		"environment": "carries an environment ID",
		"tenant":      "carries a tenant ID",
	} {
		recordWithheldProfileScope("gw", level, "x")
		if got := withheldScopeNote(nil); !strings.Contains(got, want) {
			t.Errorf("level %q: note should read %q:\n%s", level, want, got)
		}
		// And in the disagreeing branch, which builds the phrase separately.
		if got := withheldScopeNote([]string{"organization"}); !strings.Contains(got, want) {
			t.Errorf("level %q: the mismatch branch should read %q too:\n%s", level, want, got)
		}
	}
}

// The level note hands its remedy to the withheld note when there is one.
// Rendering both produced "no scope header was sent" twice and then advised
// setting an ID on the very profile whose ID had just been passed over.
func TestTheLevelNoteDefersItsRemedyToTheWithheldNote(t *testing.T) {
	withRemedy := scopeLevelNote([]string{"environment"}, "organization", noWithheldNote)
	if !strings.Contains(withRemedy, "Set an environment ID on the profile") {
		t.Errorf("with nothing withheld the level note owns the remedy:\n%s", withRemedy)
	}
	deferred := scopeLevelNote([]string{"environment"}, "organization", withheldNoteBeside)
	if strings.Contains(deferred, "Set an environment ID on the profile") {
		t.Errorf("with a withheld note the level note must not advise the profile:\n%s", deferred)
	}
	if !strings.Contains(deferred, "declares environment scope") {
		t.Errorf("the declared level still has to be stated:\n%s", deferred)
	}
	// And when the withheld note names the declared levels itself — its
	// disagreeing branch does — the level note has nothing left to add. Both
	// rendered opened with "this command's API declares ...", so joinNotes ran
	// the same fact together twice.
	if got := scopeLevelNote([]string{"environment"}, "organization", withheldNoteNamesLevels); got != "" {
		t.Errorf("the level note should defer the whole sentence, got %q", got)
	}
}

// withheldNoteState has to agree with withheldScopeNote about which branch is
// rendering, or the suppression fires for the wrong one — dropping the declared
// level entirely instead of de-duplicating it.
func TestWithheldNoteStateTracksWhichBranchRenders(t *testing.T) {
	isolateScopeVars(t)
	if got := withheldNoteState([]string{"environment"}); got != noWithheldNote {
		t.Errorf("state = %v, want noWithheldNote when nothing is withheld", got)
	}

	recordWithheldProfileScope("gw", "tenant", "t")
	// The withheld level is one the command declares, so withheldScopeNote
	// renders its remedy branch and says nothing about declared levels.
	if got := withheldNoteState([]string{"environment", "tenant"}); got != withheldNoteBeside {
		t.Errorf("state = %v, want withheldNoteBeside when the level is declared", got)
	}
	if note := withheldScopeNote([]string{"environment", "tenant"}); strings.Contains(note, "declares") {
		t.Errorf("that branch must not name declared levels, or the suppression above drops them:\n%s", note)
	}
	// And when it is not declared, the withheld note names them and the level
	// note stands down.
	if got := withheldNoteState([]string{"environment"}); got != withheldNoteNamesLevels {
		t.Errorf("state = %v, want withheldNoteNamesLevels when the level is not declared", got)
	}
	if note := withheldScopeNote([]string{"environment"}); !strings.Contains(note, "declares environment scope") {
		t.Errorf("that branch has to name the declared levels, since the level note defers:\n%s", note)
	}
}

// A composed note ranks the durable answer over the one that works today, and
// prints both — without claiming what the gateway would have done with a header
// this invocation never sent.
//
// Two defects in sequence produced this shape. First, scopeLevelNote defers its
// remedy to withheldScopeNote and withheldScopeNote knew only which level had
// been withheld, so a tenant-carrying profile on an environment-only command
// advised JAMF_TENANT_ID under a sentence saying the API declares environment
// scope. The fix for that swung too far: it said the withheld ID "would not
// have worked here anyway", which is a *requires* claim
// AnnotateScopeLevelError's own doc comment forbids — a tenant credential
// answers 200 on pro platform-devices list, which declares environment scope
// (probed 2026-09-05) — and it returned before the remedy, leaving an operator
// whose one working input was the withheld level with nothing to try.
func TestTheWithheldNoteRanksTheDurableAnswerWithoutOverClaiming(t *testing.T) {
	isolateScopeVars(t)
	t.Setenv("JAMF_CLIENT_ID", "env-client-id")
	recordWithheldProfileScope("gw", "tenant", "profile-tenant")

	note := withheldScopeNote([]string{"environment"})
	for _, want := range []string{
		`"gw"`,
		"tenant ID",
		"declares environment scope",
		// The durable answer is named first, and the level that may still work
		// is named too — the whole of what an operator can act on.
		"integration created at a declared level",
		"JAMF_TENANT_ID",
		"--tenant-id",
		"may still work today",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("the note is missing %q:\n%s", want, note)
		}
	}
	// No sentence may state what the gateway would have done with a level it
	// was never sent. CLAUDE.md records the counter-example.
	for _, unwanted := range []string{"would not have worked", "will not work"} {
		if strings.Contains(note, unwanted) {
			t.Errorf("the note asserts a gateway verdict it cannot know (%q):\n%s", unwanted, note)
		}
	}

	// When the command does declare the withheld level there is no ranking to
	// do: the withheld level is simply the answer.
	agreeing := withheldScopeNote([]string{"tenant", "environment"})
	if !strings.Contains(agreeing, "JAMF_TENANT_ID") {
		t.Errorf("a declared level should still be offered:\n%s", agreeing)
	}
	if strings.Contains(agreeing, "may still work today") {
		t.Errorf("a declared level is not a maybe:\n%s", agreeing)
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

	// Recording it is not enough on this path, and that is what separates
	// school from the other two ladders: resolveSchoolClient requires a tenant
	// ID before it builds a platform client, so a withheld level leaves the
	// client nil and NO REQUEST IS SENT. The 400 every other arm of
	// AnnotateScopeLevelError keys on never arrives, so the note had nowhere to
	// surface and the operator was told to supply credentials — the profile,
	// the client ID and the secret — that were all already present.
	//
	// Asserted through AnnotateScopeLevelError rather than by calling
	// withheldScopeNote, because the record and the rendering being correct
	// while nothing joined them is exactly the state this covers.
	annotated := AnnotateScopeLevelError(nil, platform.RequirePlatformClient(nil))
	for _, want := range []string{"sch", "tenant ID", "JAMF_TENANT_ID"} {
		if !strings.Contains(annotated.Error(), want) {
			t.Errorf("the no-client error should name %q:\n%s", want, annotated)
		}
	}
	// And the classification survives, which takes a %w wrap: the gate's error
	// is what exitcode and the platform hint match on.
	if !errors.Is(annotated, platform.ErrNoPlatformClient) {
		t.Error("annotating the no-client error flattened the chain")
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

// The school no-client note must be usable on the school path, which is the
// only path in this CLI where AnnotateScopeLevelError fires with no gateway
// response behind it.
//
// The arm used to pass scopesOf(cmd). `school blueprints list` declares
// environment scope and the withheld level is tenant, so that selected
// withheldScopeNote's disagreeing branch — which talks about a gateway this
// arm never reached, and ranks "an integration created at a declared level"
// first. resolveSchoolClient has no environment code path at all: it reads
// JAMF_TENANT_ID and the profile's tenant-id and builds auth.TenantScope, so
// following that advice returns the operator to the identical error.
//
// Asserted through the *cobra.Command main.go actually passes rather than
// through nil. TestSchoolWithholdsAProfileScopeFromForeignCredentials passes
// nil, which selects the other branch and is why the defect read as covered:
// replacing scopesOf(cmd) with nil survived the whole suite.
func TestTheSchoolNoClientNoteOffersTheOneLevelThatPathAccepts(t *testing.T) {
	isolateScopeVars(t)
	root := NewRootCmd("test", "", "", "")
	cmd := findCommandPath(t, root, "school blueprints list")
	if cmd == nil {
		t.Fatal("school blueprints list is not in the tree — this test can prove nothing")
	}
	// The premise: the command declares a level the school resolver cannot
	// build. If that ever stops being true this test is vacuous, so it is
	// asserted rather than assumed.
	if levels := scopesOf(cmd); !slices.Contains(levels, "environment") || slices.Contains(levels, "tenant") {
		t.Fatalf("scopes = %v, want environment without tenant — the branch this covers needs the "+
			"declared level to disagree with the withheld one", levels)
	}

	t.Setenv("JAMF_CLIENT_ID", "env-client-id")
	recordWithheldProfileScope("sch", "tenant", "profile-tenant")

	got := AnnotateScopeLevelError(cmd, platform.RequirePlatformClient(nil)).Error()
	for _, want := range []string{`"sch"`, "tenant ID", "JAMF_TENANT_ID", "--tenant-id"} {
		if !strings.Contains(got, want) {
			t.Errorf("the no-client note is missing %q — that is the input this path accepts:\n%s", want, got)
		}
	}
	// No request was sent, so the note has no opinion on the declared level and
	// must not rank an integration change ahead of the input that works.
	for _, unwanted := range []string{"declares environment scope", "may still work today"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("the note talks about a gateway this arm never reached (%q):\n%s", unwanted, got)
		}
	}
}

// An unreadable `file:` client-id reference is named, not silently blamed on
// the environment.
//
// The comparison fails closed either way — the level is withheld, which is
// right, since nothing could establish that the profile names the same
// integration. What was wrong was the sentence: it said the level "was not
// used because the client ID came from the JAMF_CLIENT_ID environment
// variable", when the real cause was a path that could not be read. And the
// read error surfaces nowhere else: this branch is reachable only when the
// invocation supplied a client ID, and in exactly that case both ladders skip
// ResolveSecret(p.ClientID), so no later step ever opens the file.
func TestAnUnreadableClientIDFileIsNamedRatherThanBlamedOnTheEnvironment(t *testing.T) {
	isolateScopeVars(t)
	missing := filepath.Join(t.TempDir(), "not-mounted", "client-id")

	if profileNamesTheInvocationClientID("file:"+missing, "env-client-id") {
		t.Fatal("an unreadable reference must not read as the same integration")
	}
	if unreadableClientIDRef.Path != missing {
		t.Fatalf("unreadableClientIDRef.Path = %q, want %q", unreadableClientIDRef.Path, missing)
	}
	if unreadableClientIDRef.Err == nil {
		t.Fatal("the read error was discarded")
	}

	recordWithheldProfileScope("gw", "tenant", "profile-tenant")
	note := withheldScopeNote(nil)
	if !strings.Contains(note, missing) {
		t.Errorf("the note should name the path it could not read:\n%s", note)
	}
	if strings.Contains(note, "the client ID came from") {
		t.Errorf("the note blames the environment variable for a file read failure:\n%s", note)
	}
	// The remedy still has to be there — the whole point of the note.
	if !strings.Contains(note, "JAMF_TENANT_ID") {
		t.Errorf("the note dropped its remedy:\n%s", note)
	}

	// A readable file still answers the question it was asked, and leaves no
	// record behind for the next resolution to misreport.
	resetPlatformScopeRecords()
	path := filepath.Join(t.TempDir(), "client-id")
	if err := os.WriteFile(path, []byte("env-client-id\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !profileNamesTheInvocationClientID("file:"+path, "env-client-id") {
		t.Error("a readable matching file should read as the same integration")
	}
	if unreadableClientIDRef.Path != "" {
		t.Errorf("a successful read recorded a failure: %q", unreadableClientIDRef.Path)
	}
}

// A `file:` reference the config names but the OS will not let us read cheaply
// is refused rather than opened. os.ReadFile on a FIFO blocks inside open()
// with no timeout, which would hang every invocation carrying such a profile on
// a path whose only job is to answer a yes/no question; /dev/zero Stats as size
// 0 and then reads forever.
func TestAClientIDReferenceThatIsNotARegularFileIsRefused(t *testing.T) {
	isolateScopeVars(t)
	dir := t.TempDir()
	if _, err := readClientIDRefFile(dir); err == nil {
		t.Error("a directory should not read as a client ID")
	}
	// The size cap, asserted on the boundary rather than on a device file so
	// the test runs the same everywhere.
	path := filepath.Join(dir, "big")
	if err := os.WriteFile(path, bytes.Repeat([]byte("a"), maxClientIDRefBytes*2), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readClientIDRefFile(path)
	if err != nil {
		t.Fatalf("readClientIDRefFile: %v", err)
	}
	if len(got) != maxClientIDRefBytes {
		t.Errorf("read %d bytes, want the read capped at %d", len(got), maxClientIDRefBytes)
	}
}
