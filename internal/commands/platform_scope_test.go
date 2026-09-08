// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
)

// The scope levels come off the live specs, so this asserts the partition a
// real credential faces rather than a hand-made annotation.
//
// Build v2082 moved six Platform specs to environment-only while Security
// Cloud went tenant-and-environment, which is the split every message below
// depends on. Asserted by membership rather than by count, so a new resource
// arriving in either half does not fail the test for the wrong reason.
func TestPlatformResourcesByScopeSplitsOnTheDeclaredLevel(t *testing.T) {
	root := NewRootCmd("test", "", "", "")

	for _, tc := range []struct {
		level           string
		wantReachable   []string
		wantUnreachable []string
	}{
		{
			level: "tenant",
			// Security Cloud declares [tenant, environment].
			wantReachable: []string{"ztna-apps", "content-categories", "uem-connectors", "device-groups"},
			// The six Platform specs declare environment alone, and so do
			// audit and AI Governance.
			wantUnreachable: []string{
				"blueprints", "compliance-benchmarks", "platform-devices",
				"platform-device-groups", "ddm-reports", "audit", "ai-policies",
			},
		},
		{
			level:         "environment",
			wantReachable: []string{"blueprints", "audit", "ai-policies", "ztna-apps", "platform-devices"},
		},
		{
			level: "organization",
			// An organization-scoped credential sends no header, so nothing
			// declaring a level is in reach. Probed 2026-09-05 in US: AI
			// Governance answers 400 REQUEST_CONTEXT_NOT_PROVIDED with no
			// header, while /licensing/v1/licenses answers 200 in the same run.
			wantUnreachable: []string{"blueprints", "audit", "ai-policies", "ztna-apps"},
		},
	} {
		t.Run(tc.level, func(t *testing.T) {
			reachable, unreachable := platformResourcesByScope(root, tc.level)
			if len(reachable)+len(unreachable) == 0 {
				t.Fatal("no platform resource carries jamf:scopes — the annotation is not being stamped")
			}
			for _, want := range tc.wantReachable {
				if !contains(reachable, want) {
					t.Errorf("%s should be reachable at %s scope; unreachable=%v", want, tc.level, unreachable)
				}
			}
			for _, want := range tc.wantUnreachable {
				if !contains(unreachable, want) {
					t.Errorf("%s should be out of reach at %s scope; reachable=%v", want, tc.level, reachable)
				}
			}
		})
	}
}

// `platform setup`'s closing summary was hand-written prose and was wrong twice
// over: it told a tenant-scoped operator the profile served "the Pro API and
// Platform API commands" when thirteen platform-level resources declare
// environment scope, and it told an organization-scoped one that AI Governance
// was served when it answers 400 with no scope header. Both are asserted
// negatively, because the failure was a sentence that read fine.
func TestSetupSummarySaysWhatEachLevelActuallyReaches(t *testing.T) {
	root := NewRootCmd("test", "", "", "")

	render := func(c *platformGatewayCredentials) string {
		var b bytes.Buffer
		printScopeSummary(&b, root, c, false)
		return b.String()
	}

	tenant := render(&platformGatewayCredentials{TenantID: "t"})
	if !strings.Contains(tenant, "declare environment scope, which this credential is not at") {
		t.Errorf("tenant summary must say what it cannot reach, got:\n%s", tenant)
	}
	// No stronger than scopeLevelNote's "declares, does not require": the spec
	// is currently stricter than the gateway, and a tenant credential still
	// reaches platform-devices and platform-device-groups (probed 2026-09-05).
	// The summary used to assert those resources were "out of reach here",
	// which is a claim this data cannot support and which the runtime note
	// beside it deliberately declines to make.
	if strings.Contains(tenant, "out of reach") {
		t.Errorf("the summary is more certain than the data supports, got:\n%s", tenant)
	}
	if !strings.Contains(tenant, "Some answer on a tenant credential anyway") {
		t.Errorf("the summary should say the gateway has not followed the specs everywhere, got:\n%s", tenant)
	}
	// summariseResources truncates, so the summary has to point somewhere for
	// the rest; its own doc comment names this as the escape hatch.
	if !strings.Contains(tenant, "commands -o json") {
		t.Errorf("the truncated list should name where the whole set lives, got:\n%s", tenant)
	}
	if strings.Contains(tenant, "all 29") {
		t.Errorf("tenant summary must not claim every Platform resource, got:\n%s", tenant)
	}

	env := render(&platformGatewayCredentials{EnvironmentID: "e"})
	if !strings.Contains(env, "audit and AI Governance included") {
		t.Errorf("environment summary should claim the whole surface, got:\n%s", env)
	}
	// Asserted on the phrase the unreachable branch actually prints, not on a
	// phrase no branch prints any more: the tenant wording moved off "out of
	// reach", which would have left this assertion passing vacuously.
	if strings.Contains(env, "which this credential is not at") {
		t.Errorf("environment summary should exclude nothing, got:\n%s", env)
	}

	org := render(&platformGatewayCredentials{})
	if !strings.Contains(org, "Jamf Account commands") {
		t.Errorf("organization summary should name the Jamf Account commands, got:\n%s", org)
	}
	if strings.Contains(org, "It serves the Jamf Account commands\n(account-licenses") && strings.Contains(org, "AI Governance (ai-policies") {
		t.Errorf("organization summary must not claim AI Governance is served, got:\n%s", org)
	}
	if !strings.Contains(org, "It reaches no other Platform API resource") {
		t.Errorf("organization summary should say the platform surface is out of reach, got:\n%s", org)
	}

	// Only the tenant summary prints both lists; the environment one reaches
	// everything and lists nothing, which the helper reports as unproven.
	assertNoResourceIsBothReachedAndDisclaimed(t, tenant)

	// The summary reports the level and says where a permissions answer comes
	// from. It must claim nothing about any product's entitlement: it used to
	// probe Jamf Security Cloud and subtract sixteen resources on one 403,
	// which named a licensing gap the code could not distinguish from a single
	// missing grant. Wire-checked 2026-09-08, that mattered: a tenant
	// credential answered BAD_PERMISSIONS on /securitycloud/v1/categories while
	// an environment credential in the same organization answered 200.
	// Matched on the claim rather than on the product name: the organization
	// branch names Security Cloud as one of the surfaces an environment profile
	// drives, which is a reachability statement and stays.
	for _, summary := range []string{tenant, env, org} {
		for _, unwanted := range []string{"entitle", "check did not complete", "Jamf Security Cloud."} {
			if strings.Contains(summary, unwanted) {
				t.Errorf("the summary claims a product entitlement (%q):\n%s", unwanted, summary)
			}
		}
	}
	for _, summary := range []string{tenant, env} {
		if !strings.Contains(summary, "Setup does not check capability permissions") {
			t.Errorf("the summary should say where a permissions answer comes from:\n%s", summary)
		}
		if !strings.Contains(summary, "gatewayPermissions") {
			t.Errorf("the summary should name where the permissions are listed:\n%s", summary)
		}
	}
}

// assertNoResourceIsBothReachedAndDisclaimed fails when a resource named in the
// reachable list is also named in an out-of-reach clause. That contradiction is
// what finding (3) was: the summary is read top to bottom, so a name appearing
// twice under opposite headings is worse than either claim alone.
func assertNoResourceIsBothReachedAndDisclaimed(t *testing.T, summary string) {
	t.Helper()
	var reached, disclaimed []string
	target := &reached
	for _, line := range strings.Split(summary, "\n") {
		switch {
		case strings.Contains(line, "It also reaches") || strings.Contains(line, "It reaches none"):
			target = &reached
			continue
		case strings.Contains(line, "declare environment scope"):
			target = &disclaimed
			continue
		case !strings.HasPrefix(line, "  ") || strings.Contains(line, "commands -o json"):
			continue
		}
		for _, name := range strings.Split(strings.TrimSuffix(strings.TrimSpace(line), "."), ", ") {
			if name == "" || strings.HasSuffix(name, "more") {
				continue
			}
			*target = append(*target, name)
		}
	}
	if len(reached) == 0 && len(disclaimed) == 0 {
		t.Fatalf("neither list was parsed, so this assertion proved nothing:\n%s", summary)
	}
	for _, r := range reached {
		if slices.Contains(disclaimed, r) {
			t.Errorf("%q is listed as reachable and then disclaimed:\n%s", r, summary)
		}
	}
}

// The note is what an operator reads instead of the gateway's own message,
// which says a scope was not found without saying which kind is accepted.
func TestScopeLevelNoteNamesTheLevelsAndTheOneInUse(t *testing.T) {
	one := scopeLevelNote([]string{"environment"}, "tenant", noWithheldNote)
	if !strings.Contains(one, "declares environment scope") || !strings.Contains(one, "tenant-scoped") {
		t.Errorf("both halves must appear, got %q", one)
	}
	if !strings.Contains(one, "a different integration, not a different ID") {
		t.Errorf("a wrong-level credential cannot be fixed by editing an ID, got %q", one)
	}

	two := scopeLevelNote([]string{"environment", "tenant"}, "organization", noWithheldNote)
	if !strings.Contains(two, "environment or tenant scope") {
		t.Errorf("a two-level set should read as alternatives, got %q", two)
	}
	if !strings.Contains(two, "sent no scope header") {
		t.Errorf("organization scope has no ID to correct, so say so; got %q", two)
	}

	// "declares", never "requires": the spec is currently stricter than the
	// gateway, and a tenant credential still reaches platform-devices today.
	if strings.Contains(one, "requires") || strings.Contains(two, "requires") {
		t.Error("the note must say what the spec declares, not what it requires")
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// The setup summary is assembled from the scope levels the specs declare, and
// summariseResources is what keeps it readable: the two lists run to sixteen
// and twenty-nine entries. The truncation is worth pinning because it is silent
// — shortening it to one name still renders a grammatical sentence, and the
// count is the only thing that says how much was dropped.
func TestSummariseResources(t *testing.T) {
	for _, tc := range []struct {
		names []string
		want  string
	}{
		{nil, ""},
		{[]string{"a", "b", "c"}, "a, b, c"},
		{[]string{"a", "b", "c", "d"}, "a, b, c and 1 more"},
		{[]string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}, "a, b, c and 7 more"},
	} {
		if got := summariseResources(tc.names); got != tc.want {
			t.Errorf("summariseResources(%d names) = %q, want %q", len(tc.names), got, tc.want)
		}
	}
}

// newPlatformSDKClient is the one constructor every platform path calls, which
// is why the scope is recorded there: EnrichPrivilegeError and
// AnnotateScopeLevelError both run after Execute returns, handed a command and
// an error and nothing else.
//
// Deleting that one assignment leaves every scope note claiming the credential
// is organization-scoped — the zero value — with no test failing, because the
// notes are all rendered from the recorded value.
func TestNewPlatformSDKClientRecordsTheScopeItSent(t *testing.T) {
	saved := resolvedPlatformScope
	t.Cleanup(func() { resolvedPlatformScope = saved })

	for _, scope := range []auth.Scope{
		auth.TenantScope("a-tenant"),
		auth.EnvironmentScope("an-environment"),
		{}, // organization: no header, and the zero value is the right reading
	} {
		resolvedPlatformScope = auth.TenantScope("stale")
		if _, err := newPlatformSDKClient("https://eu.api.jamfcloud.com", "id", "secret", scope, false); err != nil {
			t.Fatalf("newPlatformSDKClient: %v", err)
		}
		if resolvedPlatformScope != scope {
			t.Errorf("recorded scope = {%v %q}, want {%v %q}",
				resolvedPlatformScope.Kind, resolvedPlatformScope.ID, scope.Kind, scope.ID)
		}
	}
}

// A scope ID the gateway refuses is the one probe answer that invalidates the
// whole summary, so it is the only thing the probe concludes.
//
// Both levels are covered, because the gateway spells the refusal differently
// per level and only one of the two spellings was matched at first. Wire-probed
// 2026-09-08 against GET /pro/v1/jamf-pro-version on tenant, environment and
// organization credentials: a bad X-Environment-Id answers 404
// ENVIRONMENT_NOT_FOUND and a bad X-Tenant-Id answers 403 OWNERSHIP_FORBIDDEN,
// each naming the value. There is no TENANT_NOT_FOUND, which is what the first
// version matched, so the tenant half of the mis-paste kept the claim.
func TestARejectedScopeIDStopsTheReachabilityClaim(t *testing.T) {
	srv := gatewayStub(t, http.StatusOK, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"httpStatus":404,"traceId":"t","errors":[{"code":"ENVIRONMENT_NOT_FOUND",`+
			`"description":"Environment 3f1c not found"}]}`)
	})

	var out bytes.Buffer
	creds := &platformGatewayCredentials{
		GatewayURL: srv.URL, ClientID: "id", ClientSecret: "secret",
		EnvironmentID: "3f1c",
	}
	scopeIDRejected, err := validatePlatformGatewayCredentials(context.Background(), &out, creds)
	if err != nil {
		t.Fatalf("a rejected scope ID is reported, not returned as an error: %v", err)
	}
	if !scopeIDRejected {
		t.Fatal("ENVIRONMENT_NOT_FOUND must be reported as a rejected scope ID — nothing else " +
			"catches it, and the summary is assembled from a level the gateway just refused")
	}
	if !strings.Contains(out.String(), "does not know this environment ID") {
		t.Errorf("the probe output does not say what was wrong:\n%s", out.String())
	}

	// And the summary makes no reachability claim from it.
	root := NewRootCmd("test", "", "", "")
	var summary bytes.Buffer
	printScopeSummary(&summary, root, creds, true)
	got := summary.String()
	if !strings.Contains(got, "does not recognise the environment ID") {
		t.Errorf("the summary should name the rejected level:\n%s", got)
	}
	for _, unwanted := range []string{"It also reaches", "declare environment scope, which this credential is not at"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("the summary still claims a reach for a rejected scope ID (%q):\n%s", unwanted, got)
		}
	}

	// The tenant half, which the first version of this got wrong. A bad
	// X-Tenant-Id is OWNERSHIP_FORBIDDEN and never TENANT_NOT_FOUND, so
	// matching the latter left the more likely mis-paste — an environment ID
	// typed at the tenant prompt — reported as a plain "no" with the summary
	// still claiming a reach.
	var tenantOut bytes.Buffer
	rejected := reportScopeIDProbe(&tenantOut, "tenant",
		fmt.Errorf("status 403: [OWNERSHIP_FORBIDDEN] Tenant 'aee3ec71' is not part of your organization"))
	if !rejected {
		t.Error("OWNERSHIP_FORBIDDEN must be reported as a rejected scope ID: the gateway will not " +
			"accept that tenant ID for these credentials, so no scoped request can work")
	}
	if !strings.Contains(tenantOut.String(), "will not accept this tenant ID") {
		t.Errorf("the tenant refusal does not say what was wrong:\n%s", tenantOut.String())
	}
	var tenantSummary bytes.Buffer
	printScopeSummary(&tenantSummary, root, &platformGatewayCredentials{TenantID: "aee3ec71"}, true)
	if got := tenantSummary.String(); !strings.Contains(got, "does not recognise the tenant ID") ||
		strings.Contains(got, "It also reaches") {
		t.Errorf("a rejected tenant ID must stop the reachability claim too:\n%s", got)
	}

	// The same code at environment level, which the old wording could not
	// express: the probe could not see the level, so it called a refused
	// environment ID a tenant ID and told the operator to use the prompt they
	// had just used, while printScopeSummary called the same value an
	// environment ID in the next breath. The two must agree.
	var envOwnership bytes.Buffer
	if rejected := reportScopeIDProbe(&envOwnership, "environment",
		fmt.Errorf("status 403: [OWNERSHIP_FORBIDDEN] not part of your organization")); !rejected {
		t.Error("OWNERSHIP_FORBIDDEN at environment level must reject the scope ID too")
	}
	if got := envOwnership.String(); !strings.Contains(got, "will not accept this environment ID") {
		t.Errorf("the refusal must name the level the operator supplied:\n%s", got)
	}
	if got := envOwnership.String(); strings.Contains(got, "this tenant ID") {
		t.Errorf("an environment-level refusal must not call the value a tenant ID:\n%s", got)
	}

	// A capability refusal is not a verdict on the ID: the gateway checks
	// ownership before capability, so BAD_PERMISSIONS means the header resolved.
	// Wire-checked 2026-09-08 — a correct tenant ID on a path requiring a grant
	// answers BAD_PERMISSIONS, and reading that as a bad ID would suppress the
	// summary for every profile whose integration lacks one permission.
	var capability bytes.Buffer
	if rejected := reportScopeIDProbe(&capability, "tenant",
		fmt.Errorf("status 403: [BAD_PERMISSIONS] forbidden")); rejected {
		t.Error("BAD_PERMISSIONS was treated as a rejected scope ID — it says the scope resolved")
	}
	if !strings.Contains(capability.String(), "could not confirm") {
		t.Errorf("an answer that is not about the ID must say so:\n%s", capability.String())
	}
}

// An unknown environment ID is a 404, so it reached neither
// EnrichPrivilegeError (403 only) nor the missing-scope arm, and arrived bare.
//
// It is the runtime half of what `platform setup`'s scope check catches: the
// mis-paste of a tenant ID into environment-id earns exactly this, wire-checked
// 2026-09-08 from both a tenant and an environment credential.
func TestAnUnknownEnvironmentIDIsExplained(t *testing.T) {
	cmd := &cobra.Command{Use: "list", Annotations: map[string]string{annotationScopes: "environment"}}
	err := AnnotateScopeLevelError(cmd, errors.New(
		`status 404: {"httpStatus":404,"errors":[{"code":"ENVIRONMENT_NOT_FOUND",`+
			`"description":"Environment '85f69825' not found."}]}`))

	for _, want := range []string{
		"does not know this platform environment ID",
		"come from different places in Jamf Account",
		"environment-id in this profile",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the annotated error is missing %q:\n%s", want, err)
		}
	}
	// The gateway already names the value, so the note adds only what it
	// cannot: which two IDs get confused for each other.
	if !strings.Contains(err.Error(), "Environment '85f69825' not found.") {
		t.Errorf("the gateway's own message must survive:\n%s", err)
	}

	// A command declaring nothing gets it too: the ID is wrong whatever the
	// endpoint declares.
	bare := AnnotateScopeLevelError(&cobra.Command{Use: "list"},
		errors.New(`status 404: [ENVIRONMENT_NOT_FOUND] not found`))
	if !strings.Contains(bare.Error(), "does not know this platform environment ID") {
		t.Errorf("a command with no declared scope still needs the note:\n%s", bare)
	}

	// And an unrelated 404 is left alone.
	other := errors.New(`status 404: [OBJECT_NOT_FOUND] no such blueprint`)
	if got := AnnotateScopeLevelError(cmd, other); got.Error() != other.Error() {
		t.Errorf("an unrelated 404 was annotated:\n%s", got)
	}
}
