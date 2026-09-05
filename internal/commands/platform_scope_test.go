// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

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
		printScopeSummary(&b, root, c, true, false)
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
	if !strings.Contains(tenant, "Some still answer on a tenant credential") {
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

	// The Security Cloud probe qualifies the derived list rather than replacing
	// it: a Jamf Pro tenant with no Security Cloud entitlement still drives the
	// Pro API, and the old summary printed only the Security Cloud sentence.
	var b bytes.Buffer
	printScopeSummary(&b, root, &platformGatewayCredentials{TenantID: "t"}, false, false)
	unentitled := b.String()
	if !strings.Contains(unentitled, "Pro API and Classic API") {
		t.Errorf("an unentitled tenant still drives Pro; got:\n%s", unentitled)
	}
	if !strings.Contains(unentitled, "answered no to the Jamf Security Cloud check") {
		t.Errorf("the entitlement answer should be reported; got:\n%s", unentitled)
	}
}

// The note is what an operator reads instead of the gateway's own message,
// which says a scope was not found without saying which kind is accepted.
func TestScopeLevelNoteNamesTheLevelsAndTheOneInUse(t *testing.T) {
	one := scopeLevelNote([]string{"environment"}, "tenant", false)
	if !strings.Contains(one, "declares environment scope") || !strings.Contains(one, "tenant-scoped") {
		t.Errorf("both halves must appear, got %q", one)
	}
	if !strings.Contains(one, "different integration rather than a different ID") {
		t.Errorf("a wrong-level credential cannot be fixed by editing an ID, got %q", one)
	}

	two := scopeLevelNote([]string{"environment", "tenant"}, "organization", false)
	if !strings.Contains(two, "environment or tenant scope") {
		t.Errorf("a two-level set should read as alternatives, got %q", two)
	}
	if !strings.Contains(two, "No scope header was sent") {
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
// whole summary, and it used to collapse into the same `false` as "no Security
// Cloud entitlement" — so setup reported the profile reached sixteen Platform
// API resources, every one of which answers the same refusal.
//
// Both levels are covered, because the gateway spells the refusal differently
// per level and only one of the two spellings was matched at first. Wire-probed
// 2026-09-05, with GET /devices/v1/devices alongside returning identical codes:
// a bad X-Environment-Id answers 404 ENVIRONMENT_NOT_FOUND, and a bad
// X-Tenant-Id answers 403 OWNERSHIP_FORBIDDEN, each naming the value. There is
// no TENANT_NOT_FOUND, which is what the first version matched — so the tenant
// half of the mis-paste fell through to a plain "no" and kept the claim.
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
	securityCloud, scopeIDRejected, err := validatePlatformGatewayCredentials(context.Background(), &out, creds)
	if err != nil {
		t.Fatalf("a rejected scope ID is reported, not returned as an error: %v", err)
	}
	if securityCloud {
		t.Error("a 404 on the probe is not Security Cloud access")
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
	printScopeSummary(&summary, root, creds, false, true)
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
	_, rejected := reportSecurityCloudProbe(&tenantOut,
		fmt.Errorf("status 403: [OWNERSHIP_FORBIDDEN] Tenant 'aee3ec71' is not part of your organization"))
	if !rejected {
		t.Error("OWNERSHIP_FORBIDDEN must be reported as a rejected scope ID: the gateway will not " +
			"accept that tenant ID for these credentials, so no scoped request can work")
	}
	if !strings.Contains(tenantOut.String(), "will not accept this tenant ID") {
		t.Errorf("the tenant refusal does not say what was wrong:\n%s", tenantOut.String())
	}
	var tenantSummary bytes.Buffer
	printScopeSummary(&tenantSummary, root, &platformGatewayCredentials{TenantID: "aee3ec71"}, false, true)
	if got := tenantSummary.String(); !strings.Contains(got, "does not recognise the tenant ID") ||
		strings.Contains(got, "It also reaches") {
		t.Errorf("a rejected tenant ID must stop the reachability claim too:\n%s", got)
	}

	// BAD_PERMISSIONS stays a plain report, and it is the only one that may:
	// probed on a credential whose own correct tenant answered BAD_PERMISSIONS
	// on Security Cloud and 200 on /devices/v1/devices, so the scope ID is good
	// and the entitlement is simply absent. That is a normal Jamf Pro profile
	// and the summary stands.
	var entitlement bytes.Buffer
	if _, rejected := reportSecurityCloudProbe(&entitlement,
		fmt.Errorf("status 403: [BAD_PERMISSIONS] forbidden")); rejected {
		t.Error("BAD_PERMISSIONS was treated as a rejected scope ID — it is an entitlement answer, " +
			"and treating it as a bad ID would suppress the summary for every unentitled Jamf Pro tenant")
	}
}
