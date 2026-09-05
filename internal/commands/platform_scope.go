// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"io"
	"os"
	"slices"
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

// withheldProfileScope records a profile's scope level that was deliberately
// not used, so an error can say why the request carried no scope header.
//
// Written by the only two ladders that resolve a scope — ResolveAuthForProfile
// and resolveScope — and read by the error note. Recorded rather than
// re-derived at error time for the reason credentialSource gives for reading
// the resolved package vars: a second copy of the precedence rules is a second
// thing to drift, and a message that blames the wrong source is the failure
// these notes exist to remove.
var withheldProfileScope struct {
	Profile string
	Level   string // "environment" or "tenant"
	ID      string
}

// recordWithheldProfileScope notes a profile level the resolution passed over.
func recordWithheldProfileScope(profileName, level, id string) {
	withheldProfileScope.Profile = profileName
	withheldProfileScope.Level = level
	withheldProfileScope.ID = id
}

// resetWithheldProfileScope clears the record at the top of a resolution.
// Called per resolution because NewRootCmd and ResolveAuthForProfile run more
// than once in one process — the tests and the MCP server both do it — and a
// stale answer would put a sentence about the wrong profile on a later error.
func resetWithheldProfileScope() {
	withheldProfileScope.Profile = ""
	withheldProfileScope.Level = ""
	withheldProfileScope.ID = ""
}

// clientIDFromInvocation returns the client ID this invocation supplied through
// JAMF_CLIENT_ID, or "" when it supplied none.
//
// **The environment is read directly here rather than through the folded
// package var, and that is load-bearing.** resolveAuth folds JAMF_CLIENT_ID
// into clientID, but PersistentPreRunE returns for the `security` product
// *before* it runs — so on the path that serves the 52 gateway-served Security
// Cloud commands the folded var is empty however the credentials were supplied,
// and a predicate reading it would conclude "the credentials came from the
// profile" and splice the profile's scope back in. That is the same structural
// trap that once left --tenant-id unread on this path while --url was honoured:
// a rule that depends on work a branch skips is a rule that silently does not
// apply there.
//
// There is no --client-id flag to consult beside it. The clientID package var
// is written in exactly one place — resolveAuth, from this variable — so the
// environment is the whole of the invocation's answer.
func clientIDFromInvocation() string {
	return os.Getenv("JAMF_CLIENT_ID")
}

// profileNamesTheInvocationClientID reports whether the profile's own client-id
// reference resolves to the client ID this invocation supplied.
//
// `client-id: env:JAMF_CLIENT_ID` is a first-class profile shape —
// config.ResolveSecret handles the `env:` form and README documents it on a
// platform profile — and on such a profile the variable *has to* be set for the
// profile to resolve its own credential at all. So the bare "was a client ID
// supplied?" test this replaces read every one of those profiles as a foreign
// credential and dropped both of its scope IDs, which broke a working platform
// profile: `pro platform-devices list` went out with no scope header and earned
// 400 REQUEST_CONTEXT_NOT_PROVIDED where it had exited 0 before. The note's
// remedy was unusable too, since dropping the variable it named stops the
// profile resolving its credential.
//
// Only an `env:` reference is compared, and it is compared without resolving
// anything else. Resolving a `keychain:` or `file:` reference here would touch
// the keychain — a prompt, on a system that asks — on a path that is by
// definition not using the profile's credentials; and `env:` is the only form
// whose value can also arrive as JAMF_CLIENT_ID, which is the coincidence being
// tested for. Matching on the value rather than on the variable name is
// deliberate: the same client ID is the same integration however it was spelled.
func profileNamesTheInvocationClientID(profileClientID, invocationID string) bool {
	if invocationID == "" {
		return false
	}
	after, ok := strings.CutPrefix(profileClientID, "env:")
	return ok && os.Getenv(after) == invocationID
}

// credentialIdentifiesTheProfilesIntegration reports whether the active client
// ID is the profile's own rather than one this invocation brought with it.
//
// The client ID is what names a Jamf Account integration, and an integration is
// created at exactly one scope level whose credential carries that choice — so
// whoever supplies the client ID supplies the level. A secret alone does not
// move that: a profile holding client-id with only JAMF_CLIENT_SECRET injected
// is still the profile's integration, which is a reasonable CI shape and one
// the config's own `env:` secret references already serve. This is therefore
// slightly narrower than "credentials came from the environment" and never
// wider: a JAMF_CLIENT_ID naming a *different* integration than the profile
// does always trips it.
//
// profileClientID is the profile's reference as written (`env:VAR`,
// `keychain:…`), not a resolved value, so this can answer without resolving a
// secret it has no business reading.
func credentialIdentifiesTheProfilesIntegration(profileName, profileClientID string) bool {
	if profileName == "" {
		return false
	}
	invocation := clientIDFromInvocation()
	return invocation == "" || profileNamesTheInvocationClientID(profileClientID, invocation)
}

// profileScopeAppliesTo reports whether a profile's scope level may be attached
// to the credentials in hand.
//
// It is false when the client ID came from the invocation and names a different
// integration than the profile's, and that is a behaviour change rather than a
// diagnostic one. resolveScope's ladder is per input — flag, then env var, then
// profile — which is right for a URL and wrong for the scope. Supply JAMF_URL +
// JAMF_CLIENT_ID + JAMF_CLIENT_SECRET for an organization-scoped integration
// while any default profile happens to carry a tenant-id, and the request went
// out with an X-Tenant-Id the operator never mentioned. **An organization-scoped
// credential must never send a scope header**, and there is nothing the CLI can
// do with a level belonging to some other integration: at best it is redundant,
// at worst it is a level the credential cannot use.
//
// So the level is dropped, not spliced, and the profile's is recorded so the
// resulting error can name it. Nothing silently reached the wrong tenant while
// the splice existed — wire-checked 2026-09-05, a tenant ID the credential does
// not own answers 403 OWNERSHIP_FORBIDDEN, with the owned ID at 200 and no
// header at 400 REQUEST_CONTEXT_NOT_PROVIDED in the same run — so this fixes a
// misattributed error rather than a wrong target. It was a bad error: the level
// named in it appeared in no flag, no variable and no command the operator
// typed.
func profileScopeAppliesTo(profileName, profileClientID string) bool {
	return credentialIdentifiesTheProfilesIntegration(profileName, profileClientID)
}

// withheldScopeNote explains a request that carried no scope header because the
// profile's level did not belong to the credentials in hand. "" when there is
// nothing withheld.
//
// This is what makes the drop actionable rather than merely correct: without
// it, an env-var tenant-scoped credential and no JAMF_TENANT_ID produce a bare
// 400 whose remedy is invisible, and the level note beside it would describe
// the credential as organization-scoped, which is a claim about the credential
// this side cannot make — a gateway token is opaque and carries an empty scope.
//
// levels is what the command's API declares, or nil when it declares nothing
// (every Pro and Classic command, since only generator/platform stamps
// jamf:scopes).
func withheldScopeNote(levels []string) string {
	if withheldProfileScope.Profile == "" {
		return ""
	}
	// The withheld level is only worth re-offering when the command can use it.
	// Naming JAMF_TENANT_ID under a sentence that has just said this API
	// declares environment scope reads as a remedy and is the next error:
	// supplying it earns INVALID_REQUEST_CONTEXT_TYPE. So when the two
	// disagree, say what the ID was and stop offering it.
	if len(levels) > 0 && !slices.Contains(levels, withheldProfileScope.Level) {
		return fmt.Sprintf("Profile %q carries a %s ID, which was not used because the client ID came "+
			"from %s — and would not have worked here anyway, since this command's API declares %s. "+
			"Use an integration created at a declared level.",
			withheldProfileScope.Profile, withheldProfileScope.Level, clientIDSource, renderScopeLevels(levels))
	}
	envVar := "JAMF_TENANT_ID"
	flagName := "--tenant-id"
	if withheldProfileScope.Level == "environment" {
		envVar, flagName = "JAMF_ENVIRONMENT_ID", "--environment-id"
	}
	return fmt.Sprintf("Profile %q carries a %s ID, which was not used because the client ID came "+
		"from %s: an integration is created at one level in Jamf Account and its credential carries "+
		"that choice, so that ID belongs to the profile's own integration. Supply the level for "+
		"these credentials with %s or %s, or use the profile's own client ID as well as its level.",
		withheldProfileScope.Profile, withheldProfileScope.Level, clientIDSource, envVar, flagName)
}

// clientIDSource names where an invocation-supplied client ID came from. A
// constant rather than a branch: there is no --client-id flag in this CLI, so
// JAMF_CLIENT_ID is the only way one arrives.
const clientIDSource = "the JAMF_CLIENT_ID environment variable"

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
// OWNERSHIP_FORBIDDEN is a 403 that already routes to scopeMismatchHint, which
// names the declared levels itself — one answer beats two overlapping ones.
const gatewayMissingScope = "REQUEST_CONTEXT_NOT_PROVIDED"

// INVALID_REQUEST_CONTEXT_TYPE gets nothing, and after the withhold rule it
// cannot arise from a profile at all: a level now only reaches the wire when
// the caller named it on this invocation, and the gateway's own message already
// names both the level sent and the levels accepted. That error was the symptom
// that found the splice — an organization-scoped credential from JAMF_*
// variables, with a default profile carrying a tenant-id, produced
// "Request context type 'tenant' is invalid" naming a level the operator never
// chose. See profileScopeAppliesTo.

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
		// A Pro or Classic command declares no level — only generator/platform
		// stamps jamf:scopes — but a withheld profile scope is still the reason
		// this request went out with no header, and the note is what makes the
		// withhold actionable. The withhold rule drops the scope for a Pro or
		// Classic request identically, so those commands hit this same 400 with
		// the record fully populated; without this arm the explanation reached
		// only the platform commands.
		if withheld := withheldScopeNote(nil); withheld != "" && strings.Contains(err.Error(), gatewayMissingScope) {
			return fmt.Errorf("%w\n\nnote: %s", err, withheld)
		}
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
	withheld := withheldScopeNote(levels)
	return fmt.Errorf("%w\n\nnote: %s", err, joinNotes(scopeLevelNote(levels, have, withheld != ""), withheld))
}

// joinNotes runs the sentences together, skipping the empty ones.
func joinNotes(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " ")
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
func scopeLevelNote(levels []string, have string, withheld bool) string {
	note := fmt.Sprintf("this command's API declares %s, and this invocation is %s-scoped.",
		renderScopeLevels(levels), have)
	if withheld {
		// The remedy belongs to withheldScopeNote, which knows the profile and
		// the level. Repeating it here produced a note that said "no scope
		// header was sent" twice and then advised setting an ID on the very
		// profile whose ID had just been passed over.
		return note
	}
	if have == "organization" {
		// An organization-scoped credential sends no header at all, so there is
		// no ID to correct — the answer is a different integration or a profile
		// carrying one.
		which := "one of those levels"
		if len(levels) == 1 {
			which = "that level"
		}
		// "organization-scoped" describes the request, not the credential: a
		// gateway token is opaque and carries an empty scope, so nothing here
		// can read the level a credential was minted at. Sending no header is
		// what organization scope is on the wire, and it is also what a
		// credential whose level was never supplied ends up sending — which is
		// why withheldScopeNote runs beside this one.
		return note + " No scope header was sent, which is what organization scope is on the wire. " +
			"Set an environment ID on the profile (or JAMF_ENVIRONMENT_ID), or use an integration " +
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
//
// scopeIDRejected is the one answer that invalidates the whole summary: the
// gateway refused the scope identifier itself, so no reachability claim can be
// made from it and none is made. Without that the summary told an operator who
// had pasted a tenant UUID at the environment prompt that the profile reached
// sixteen Platform API resources, every one of which answers the same 404.
func printScopeSummary(w io.Writer, root *cobra.Command, creds *platformGatewayCredentials, securityCloud, scopeIDRejected bool) {
	level := "organization"
	switch {
	case creds.EnvironmentID != "":
		level = "environment"
	case creds.TenantID != "":
		level = "tenant"
	}
	if scopeIDRejected {
		// The probe is skipped for an organization-scoped credential, so level
		// here is always the one that was typed in. Say what was rejected and
		// stop: a reachability list assembled from a scope the gateway does not
		// know describes a profile that reaches nothing.
		_, _ = fmt.Fprintf(w, "The gateway does not recognise the %s ID in this profile, so nothing here can\n", level)
		_, _ = fmt.Fprintln(w, "say what it reaches — every scoped request will be refused until the ID is right.")
		_, _ = fmt.Fprintln(w, "A platform environment ID and a tenant ID are different values from different")
		_, _ = fmt.Fprintln(w, "places in Jamf Account. Re-run `jamf-cli platform setup` and answer the prompt")
		_, _ = fmt.Fprintln(w, "for the level this integration was created at.")
		return
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
		_, _ = fmt.Fprintf(w, "The other %d declare environment scope, which this credential is not at:\n", len(unreachable))
		_, _ = fmt.Fprintf(w, "  %s.\n", summariseResources(unreachable))
		_, _ = fmt.Fprintln(w, "  (jamf-cli commands -o json lists every command's declared scope under \"scopes\".)")
		// "declares", never "is out of reach": the spec is currently stricter
		// than the gateway — build v2082 moved six Platform specs to
		// environment-only and a tenant credential still reaches
		// platform-devices and platform-device-groups (probed 2026-09-05) — and
		// this summary must not be more certain than AnnotateScopeLevelError,
		// which annotates a refusal the gateway has already returned and
		// deliberately pre-empts none.
		_, _ = fmt.Fprintln(w, "Some still answer on a tenant credential — the gateway has not followed the specs")
		_, _ = fmt.Fprintln(w, "everywhere, and nothing here refuses on this. A 400 or 403 naming the scope is the")
		_, _ = fmt.Fprintln(w, "signal to create an environment-scoped integration; environment is the level to")
		_, _ = fmt.Fprintln(w, "prefer for a new one either way.")
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
