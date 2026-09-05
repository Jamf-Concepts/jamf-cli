// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
)

// annotationScopes holds the Jamf Platform API scope levels the published spec
// declares a credential must be created at, comma-separated and widest first,
// stamped by generator/platform from the spec-root x-scope-types extension.
//
// This is the one requirement a 403 cannot teach. A Platform API integration is
// created at exactly one level in Jamf Account — organization, platform
// environment or tenant — and the credential only works with that level, but the
// gateway's refusal names a capability permission and says nothing about the
// level, so an operator with a correctly-granted credential at the wrong level
// is sent to tick a box that changes nothing.
const annotationScopes = "jamf:scopes"

// resolvedPlatformScope is the scope level the active credential sends,
// recorded by newPlatformSDKClient — the one constructor every platform path
// calls, for the same reason refuseRetiredGatewayURL lives there rather than
// beside its callers.
//
// A package var rather than a CLIContext field because the reader is
// EnrichPrivilegeError, which main.go calls after Execute returns and which is
// handed a *cobra.Command and an error and nothing else. Its zero value is
// organization scope, which is also the right reading when no ID resolved: an
// organization-scoped credential sends no header, so "nothing was resolved" and
// "organization" are the same state on the wire.
var resolvedPlatformScope auth.Scope

// scopesOf returns the scope levels a command's annotation declares, or nil.
//
// nil means the spec is silent, not that any level works: the three Jamf
// Account specs declare no x-scope-types at all despite being
// organization-scoped, and a Pro or Classic command routed through the gateway
// carries none because its scope is a property of the gateway route rather than
// of the endpoint.
func scopesOf(cmd *cobra.Command) []string {
	if cmd == nil {
		return nil
	}
	raw := cmd.Annotations[annotationScopes]
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ",")
}

// gatewayMissingScope is the gateway's code for a request carrying no scope
// header on an endpoint that needs one — what an organization-scoped credential
// earns. A 400, so EnrichPrivilegeError never sees it and this is the only
// place the declared levels can reach it.
//
// Two sibling codes are deliberately handled elsewhere or not at all.
// OWNERSHIP_FORBIDDEN is a 403 that already routes to scopeMismatchHint, which
// names the declared levels itself — one answer beats two overlapping ones.
// INVALID_REQUEST_CONTEXT_TYPE already names both the level sent and the levels
// accepted ("Request context type 'tenant' is invalid. Expected any of
// 'environment'"), so the annotation adds nothing and a second list risks
// disagreeing with the gateway's own.
const gatewayMissingScope = "REQUEST_CONTEXT_NOT_PROVIDED"

// AnnotateScopeLevelError appends the levels a platform command declares to a
// gateway scope error, when the credential in hand is not at one of them.
//
// It replaces annotateAuditScopeError, which spelled the same fact for one
// command by hand. Audit is environment-only, and the note saying so had
// already gone stale once — it used to add that the spec listed organization as
// allowed while the gateway refused it, which build v2056 made false. Reading
// x-scope-types means the sentence cannot disagree with the artifact, and every
// platform command gets it rather than the one whose gap someone hit.
//
// It says "declares" rather than "requires" on purpose. The spec is currently
// STRICTER than the gateway: build v2082 moved six Platform specs to
// environment-only, and a tenant credential still reaches platform-devices and
// platform-device-groups today (probed 2026-09-05). So this annotates a failure
// the gateway has already returned and never pre-empts one — a refusal keyed on
// this data would refuse working commands.
//
// Silent when the credential's level is among the declared ones: the scope is
// then not the story, and a note about scope would send the reader away from
// whatever is.
func AnnotateScopeLevelError(cmd *cobra.Command, err error) error {
	if err == nil {
		return err
	}
	levels := scopesOf(cmd)
	if len(levels) == 0 {
		return err
	}
	have := resolvedPlatformScope.Kind.String()
	for _, l := range levels {
		if l == have {
			return err
		}
	}
	if !strings.Contains(err.Error(), gatewayMissingScope) {
		return err
	}
	// Wrapped with %w, not %s. This runs before exitcode.CodeFrom and
	// EnrichPrivilegeError's errors.As checks, and formatting with %s flattens
	// the chain — which is how an annotated error lost its classification and
	// fell back to exit 1.
	return fmt.Errorf("%w\n\nnote: %s", err, scopeLevelNote(levels, have))
}

// renderScopeLevels renders a declared level set as prose. Shared with
// scopeMismatchHint so the 400 note and the 403 hint cannot describe the same
// annotation two different ways.
func renderScopeLevels(levels []string) string {
	if len(levels) == 1 {
		return levels[0] + " scope"
	}
	return strings.Join(levels[:len(levels)-1], ", ") + " or " + levels[len(levels)-1] + " scope"
}

// scopeLevelNote renders the sentence. Split out so a test can assert the
// wording for each level without standing up a gateway error.
func scopeLevelNote(levels []string, have string) string {
	note := fmt.Sprintf("this command's API declares %s, and the credential in use is %s-scoped.",
		renderScopeLevels(levels), have)
	if have == "organization" {
		// An organization-scoped credential sends no header at all, so there is
		// no ID to correct — the answer is a different integration or a profile
		// carrying one.
		which := "one of those levels"
		if len(levels) == 1 {
			which = "that level"
		}
		return note + " An organization-scoped credential sends no scope header. Set an " +
			"environment ID on the profile (or JAMF_ENVIRONMENT_ID), or use an integration " +
			"created at " + which + "."
	}
	return note + " An integration is created at one level in Jamf Account and only works " +
		"with that level, so this needs a different integration rather than a different ID. " +
		"jamf-cli config list shows each profile's scope."
}

// platformResourcesByScope walks the command tree and partitions the
// platform-served resource groups into those a credential at have can reach and
// those it cannot, by their declared levels. Names are the resource groups as
// typed (`blueprints`, `ai-policies`), deduplicated and sorted.
//
// Derived rather than listed by hand because `platform setup`'s closing summary
// used to be a hand-written sentence and it was wrong in two ways at once: it
// told a tenant-scoped operator the profile "serves the Pro API and Platform
// API commands" when six Platform specs are declared environment-only, and it
// told an organization-scoped one that AI Governance was served when
// GET /ai/governance/policies/v1/policies answers 400 REQUEST_CONTEXT_NOT_PROVIDED
// with no header (probed 2026-09-05 in US, with /licensing/v1/licenses at 200
// in the same run as the control). A sentence assembled from the annotations
// cannot drift from the specs the commands were generated from.
//
// A group is counted as unreachable only when NO leaf under it declares have,
// so a resource whose operations disagree — two specs can merge into one
// resource — is reported reachable rather than excluded wholesale.
func platformResourcesByScope(root *cobra.Command, have string) (reachable, unreachable []string) {
	yes := map[string]bool{}
	seen := map[string]bool{}
	var walk func(cmd *cobra.Command, group string)
	walk = func(cmd *cobra.Command, group string) {
		for _, sub := range cmd.Commands() {
			if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
				continue
			}
			g := group
			if g == "" && cmd.Parent() != nil {
				// Depth 2 is the resource group: root > product > resource.
				g = sub.Name()
			}
			if levels := scopesOf(sub); len(levels) > 0 && g != "" {
				seen[g] = true
				for _, l := range levels {
					if l == have {
						yes[g] = true
					}
				}
			}
			walk(sub, g)
		}
	}
	walk(root, "")

	for g := range seen {
		if yes[g] {
			reachable = append(reachable, g)
		} else {
			unreachable = append(unreachable, g)
		}
	}
	sort.Strings(reachable)
	sort.Strings(unreachable)
	return reachable, unreachable
}

// printScopeSummary closes `platform setup` by saying what the profile just
// written can actually reach, derived from the scope levels the specs declare.
//
// securityCloud is what the tenant answered to one probe against
// content-categories, and it qualifies the derived list rather than replacing
// it: a Jamf Pro tenant legitimately has no Security Cloud entitlement, and the
// gateway's two rejections for that are indistinguishable in intent from a
// scope problem — so the probe answers "entitled?" while jamf:scopes answers
// "right level?", and both matter.
func printScopeSummary(w io.Writer, root *cobra.Command, creds *platformGatewayCredentials, securityCloud bool) {
	level := "organization"
	switch {
	case creds.EnvironmentID != "":
		level = "environment"
	case creds.TenantID != "":
		level = "tenant"
	}
	reachable, unreachable := platformResourcesByScope(root, level)

	if level == "organization" {
		// Naming the surfaces beats implying the profile drives a product API,
		// which it cannot: an organization-scoped credential sends no scope
		// header, and every platform resource that declares a level declares
		// one this credential is not at.
		_, _ = fmt.Fprintln(w, "This is an organization-scoped credential. It serves the Jamf Account commands")
		_, _ = fmt.Fprintln(w, "(account-licenses, deal-registrations, distributor-*, sso-connections, sso-domains),")
		_, _ = fmt.Fprintln(w, "which are US-only.")
		_, _ = fmt.Fprintf(w, "It reaches no other Platform API resource: all %d declare environment or tenant\n", len(unreachable))
		_, _ = fmt.Fprintln(w, "scope, and the Pro and Classic APIs need a scope header too. Set up a profile with")
		_, _ = fmt.Fprintln(w, "an environment ID to drive Pro, Platform, Security Cloud, audit or AI Governance.")
		return
	}

	_, _ = fmt.Fprintln(w, "This scope serves the Pro API and Classic API commands.")
	if len(unreachable) == 0 {
		_, _ = fmt.Fprintf(w, "It also reaches all %d Platform API resources, audit and AI Governance included.\n", len(reachable))
	} else {
		_, _ = fmt.Fprintf(w, "It also reaches %d of the %d Platform API resources:\n", len(reachable), len(reachable)+len(unreachable))
		_, _ = fmt.Fprintf(w, "  %s.\n", summariseResources(reachable))
		_, _ = fmt.Fprintf(w, "The other %d declare environment scope and are out of reach here:\n", len(unreachable))
		_, _ = fmt.Fprintf(w, "  %s.\n", summariseResources(unreachable))
		_, _ = fmt.Fprintln(w, "Create an environment-scoped integration in Jamf Account to reach those — tenant")
		_, _ = fmt.Fprintln(w, "IDs are the legacy level, and environment is the one to prefer for a new one.")
	}
	if !securityCloud {
		_, _ = fmt.Fprintln(w, "This tenant answered no to the Jamf Security Cloud check, so the dns-*, ztna-*,")
		_, _ = fmt.Fprintln(w, "content-categories, device-groups and uem-* commands need an entitlement it lacks.")
	}
	_, _ = fmt.Fprintln(w, "The Jamf Account commands need an organization-scoped integration.")
}

// summariseResources renders at most three names plus a count, because the
// full list runs to sixteen and twenty-nine entries and a wall of names in a
// setup summary is skipped rather than read. `commands -o json` carries every
// one under `scopes` for anything that needs the whole set.
func summariseResources(names []string) string {
	const show = 3
	if len(names) <= show {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(names[:show], ", "), len(names)-show)
}
