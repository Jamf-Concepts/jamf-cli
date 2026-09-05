// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"strings"
	"testing"
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
		printScopeSummary(&b, root, c, true)
		return b.String()
	}

	tenant := render(&platformGatewayCredentials{TenantID: "t"})
	if !strings.Contains(tenant, "declare environment scope and are out of reach here") {
		t.Errorf("tenant summary must say what it cannot reach, got:\n%s", tenant)
	}
	if strings.Contains(tenant, "all 29") {
		t.Errorf("tenant summary must not claim every Platform resource, got:\n%s", tenant)
	}

	env := render(&platformGatewayCredentials{EnvironmentID: "e"})
	if !strings.Contains(env, "audit and AI Governance included") {
		t.Errorf("environment summary should claim the whole surface, got:\n%s", env)
	}
	if strings.Contains(env, "out of reach") {
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
	printScopeSummary(&b, root, &platformGatewayCredentials{TenantID: "t"}, false)
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
	one := scopeLevelNote([]string{"environment"}, "tenant")
	if !strings.Contains(one, "declares environment scope") || !strings.Contains(one, "tenant-scoped") {
		t.Errorf("both halves must appear, got %q", one)
	}
	if !strings.Contains(one, "different integration rather than a different ID") {
		t.Errorf("a wrong-level credential cannot be fixed by editing an ID, got %q", one)
	}

	two := scopeLevelNote([]string{"environment", "tenant"}, "organization")
	if !strings.Contains(two, "environment or tenant scope") {
		t.Errorf("a two-level set should read as alternatives, got %q", two)
	}
	if !strings.Contains(two, "sends no scope header") {
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
