// Copyright 2026, Jamf Software LLC

package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/platform"
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

// resetPlatformScopeRecords clears both per-resolution records together.
//
// resolvedPlatformScope had no reset while its sibling did, and the asymmetry
// is not survivable in a process that resolves twice: a second resolution that
// builds no platform client — a profile with no credentials, or school's
// tenant-less path — leaves the first one's level standing, and
// AnnotateScopeLevelError then says "this invocation is tenant-scoped" about a
// credential this invocation never used. Resetting them in one call is what
// stops the next var added here from being forgotten the same way.
func resetPlatformScopeRecords() {
	resetWithheldProfileScope()
	resolvedPlatformScope = auth.Scope{}
	unreadableClientIDRef.Path = ""
	unreadableClientIDRef.Err = nil
}

// unreadableClientIDRef records a profile `client-id: file:/path` reference the
// withhold rule could not read, so the note can name the real cause instead of
// blaming the environment variable.
//
// It exists because nothing downstream reads that file. This branch is reached
// only when the invocation supplied a client ID, and in exactly that case both
// resolution ladders skip config.ResolveSecret(p.ClientID) — so an unmounted
// secrets path, a permission error or a typo'd path is observable here and
// nowhere else. Without it the level was withheld and the note said it "was
// not used because the client ID came from the JAMF_CLIENT_ID environment
// variable", which is not why: with the same file readable the same invocation
// keeps its level and succeeds.
var unreadableClientIDRef struct {
	Path string
	Err  error
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
// The comparison is on the *value*, not on how it was spelled — the same client
// ID is the same integration however the profile refers to it — so every
// non-prompting reference form is resolved and compared:
//
//   - `env:VAR`, the form the README documents on a platform profile;
//   - `file:/path`, a plain file read.
//
// `keychain:` is the one form withheld, and only because resolving it can
// prompt on a system that asks — on a path which by definition is not using the
// profile's credentials. So a keychain reference is undecidable here and reads
// as a foreign integration, which means the profile's level is dropped and the
// note has to carry the remedy. That is the shape `platform setup` writes
// (`keychain:<profile>/client-id`), so it is the common case rather than an
// exotic one: `jamf-cli -p gw pro platform-devices list` in a shell that also
// exports JAMF_CLIENT_ID for that same integration sends no scope header. The
// note names the profile, the level and both ways to supply one, which is what
// keeps it actionable; supplying JAMF_ENVIRONMENT_ID / JAMF_TENANT_ID, or
// dropping JAMF_CLIENT_ID so the profile resolves its own, both work.
//
// Resolving `file:` here rather than lumping it in with `keychain:` matters
// because the reason for withholding is the *prompt*, not the indirection — an
// earlier version withheld both, which made a file-referencing profile behave
// like a keychain one for no reason it could state.
func profileNamesTheInvocationClientID(profileClientID, invocationID string) bool {
	if invocationID == "" || profileClientID == "" {
		return false
	}
	if after, ok := strings.CutPrefix(profileClientID, "env:"); ok {
		return os.Getenv(after) == invocationID
	}
	if path, ok := strings.CutPrefix(profileClientID, "file:"); ok {
		id, err := readClientIDRefFile(path)
		if err != nil {
			// Recorded rather than discarded. "It will fail with a better
			// message further along" was the earlier justification and it is
			// false: this branch is reachable only when the invocation supplied
			// a client ID, and in that case both ladders skip
			// ResolveSecret(p.ClientID), so nothing downstream ever opens this
			// file. Undecidable still reads as foreign — the level is withheld,
			// which is the fail-closed answer — but the note now names the path
			// and the read error instead of blaming JAMF_CLIENT_ID.
			unreadableClientIDRef.Path = path
			unreadableClientIDRef.Err = err
			return false
		}
		return id == invocationID
	}
	// keychain:, and nothing else — config.ResolveSecret rejects any other
	// form outright ("must use env:, file:, or keychain: prefix"), so a bare
	// value is not a shape a profile can carry and there is nothing else to
	// compare. Undecidable reads as foreign, and withheldScopeNote is what
	// makes that usable rather than merely correct.
	return false
}

// maxClientIDRefBytes bounds a `file:` client-id read. A client ID is a short
// opaque string, so the cap costs nothing legitimate and is what stops a
// config naming /dev/zero from growing the read without limit — os.ReadFile
// pre-sizes from Stat, which reports 0 for a character device and then reads
// until EOF that never comes.
const maxClientIDRefBytes = 4096

// readClientIDRefFile reads a profile's `file:` client-id reference for the
// same-integration comparison, refusing anything that is not a regular file.
//
// The refusal is the point: os.ReadFile on a config-named FIFO blocks inside
// open() with no timeout, which would hang every invocation carrying such a
// profile on a path that only wants to answer a yes/no question. Stat reports a
// named pipe's mode without opening it, so the check is cheap and comes first.
// This deliberately does not reuse config.ResolveSecret, which has neither
// guard and is called where a blocking read is the caller's own problem.
func readClientIDRefFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular file (%s)", info.Mode().Type())
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	// Read one byte past the cap so "at the cap" and "over it" are
	// distinguishable, and refuse both an over-cap and an empty file rather
	// than returning a value that cannot be the client ID. Silently comparing
	// a truncated prefix, or "" against a guaranteed non-empty invocation ID,
	// would withhold the level and then let withheldReasonClause blame
	// JAMF_CLIENT_ID for a file problem — the misattribution
	// unreadableClientIDRef exists to remove. Zero length is what an
	// interrupted write or a secrets volume mounted ahead of its content
	// leaves.
	b, err := io.ReadAll(io.LimitReader(f, maxClientIDRefBytes+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxClientIDRefBytes {
		return "", fmt.Errorf("larger than %d bytes, so it cannot name a client ID", maxClientIDRefBytes)
	}
	id := strings.TrimSpace(string(b))
	if id == "" {
		return "", errors.New("empty")
	}
	return id, nil
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
	envVar := "JAMF_TENANT_ID"
	flagName := "--tenant-id"
	if withheldProfileScope.Level == "environment" {
		envVar, flagName = "JAMF_ENVIRONMENT_ID", "--environment-id"
	}
	// An unreadable reference means the comparison was never made, so nothing
	// here may claim whose integration the profile's ID names —
	// profileNamesTheInvocationClientID returning false was a fail-closed
	// default, not a determination. The remedy is the file, and dropping
	// JAMF_CLIENT_ID is specifically the wrong advice: without it
	// ResolveAuthForProfile calls ResolveSecret(p.ClientID), opens this same
	// file and fails outright. The declared levels are left to scopeLevelNote,
	// which runs beside this (see withheldNoteState).
	if unreadableClientIDRef.Path != "" {
		return fmt.Sprintf("Profile %q carries %s, %s Supply the level for these credentials with "+
			"%s or %s, or make file:%s readable so the profile resolves its own client ID. Do not "+
			"drop %s while that file is unreadable: the profile cannot then resolve a client ID at all.",
			withheldProfileScope.Profile, withheldLevelPhrase(), withheldReasonClause(),
			envVar, flagName, unreadableClientIDRef.Path, clientIDSource)
	}
	// When the command's API declares levels that do not include the withheld
	// one, say so — and stop there, without saying what the gateway would have
	// done with a header this invocation never sent. The spec is currently
	// stricter than the gateway (a tenant credential answers 200 on
	// pro platform-devices list, which declares environment scope), so both
	// halves are stated in order of durability rather than one being ruled out.
	if len(levels) > 0 && !slices.Contains(levels, withheldProfileScope.Level) {
		return fmt.Sprintf("Profile %q carries %s, %s This command's API declares %s, so an "+
			"integration created at a declared level is the lasting fix; the gateway does not "+
			"follow the specs everywhere, so %s or %s may still work today.",
			withheldProfileScope.Profile, withheldLevelPhrase(), withheldReasonClause(),
			renderScopeLevels(levels), envVar, flagName)
	}
	return fmt.Sprintf("Profile %q carries %s, %s Jamf Account creates an integration at one level "+
		"and its credential carries that choice, so that ID belongs to the profile's own "+
		"integration. Supply the level for these credentials with %s or %s, or use the profile's "+
		"own client ID as well as its level.",
		withheldProfileScope.Profile, withheldLevelPhrase(), withheldReasonClause(), envVar, flagName)
}

// withheldReasonClause says why the profile's level was passed over, and there
// are two reasons rather than one.
//
// The usual one is that the invocation supplied the client ID, which names a
// different integration. The other is that the profile's own `file:` client-id
// reference could not be read, so the comparison could not be made at all —
// and blaming JAMF_CLIENT_ID for that sends the operator to look at a variable
// that is not the problem. The path and the read error are the whole of the
// signal on that branch, since nothing downstream opens the file.
func withheldReasonClause() string {
	if unreadableClientIDRef.Path != "" {
		return fmt.Sprintf("which this invocation did not use: it could not read the profile's own "+
			"client-id reference file:%s (%v), so nothing here can tell whether that reference names "+
			"the same integration as the credentials in hand.",
			unreadableClientIDRef.Path, unreadableClientIDRef.Err)
	}
	return fmt.Sprintf("which this invocation did not use, because the client ID came from %s.", clientIDSource)
}

// withheldLevelPhrase names the withheld level with the right article. The
// levels are "environment" and "tenant", so a literal "a %s ID" printed
// "carries a environment ID" for exactly the level Jamf wants integrations
// created at — a note about a careful precedence rule, misspelling the common
// case.
func withheldLevelPhrase() string {
	if withheldProfileScope.Level == "environment" {
		return "an environment ID"
	}
	return "a " + withheldProfileScope.Level + " ID"
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

// gatewayUnknownEnvironment is the gateway's code for an X-Environment-Id it
// does not know, and it is a 404 — so neither EnrichPrivilegeError (403 only)
// nor the missing-scope arm above sees it, and it reached the operator bare.
//
// It is the runtime half of what `platform setup`'s scope check catches, and
// the mis-paste issue #354 reports earns exactly this: wire-checked 2026-09-08,
// a valid tenant ID sent as X-Environment-Id answers
// 404 ENVIRONMENT_NOT_FOUND naming the value, from both a tenant and an
// environment credential.
const gatewayUnknownEnvironment = "ENVIRONMENT_NOT_FOUND"

// unknownEnvironmentNote explains a 404 the gateway returns for an environment
// ID it does not know. The gateway names the value, so this adds only the part
// it cannot: the two IDs are different values from different places, which is
// how the wrong one gets pasted in.
const unknownEnvironmentNote = "The gateway does not know this platform environment ID. A platform " +
	"environment ID and a tenant ID come from different places in Jamf Account, and neither is the " +
	"Jamf Pro tenant ID or the client ID. Check environment-id in this profile (jamf-cli config " +
	"list shows the scope; jamf-cli config path prints the file), or re-run jamf-cli platform setup."

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
// command by hand and had already gone stale once. Reading x-scope-types means
// the sentence cannot disagree with the artifact, and every platform command
// gets it rather than the one whose gap someone hit.
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
	// An unknown environment ID is a 404, so it reaches neither
	// EnrichPrivilegeError nor the arms below and arrived bare. Checked first
	// because the ID is the whole problem: the declared levels are beside the
	// point when the gateway does not recognise the value.
	if strings.Contains(err.Error(), gatewayUnknownEnvironment) {
		return fmt.Errorf("%w\n\nnote: %s", err, unknownEnvironmentNote)
	}
	// The one path that never reaches the gateway, and therefore never gets the
	// 400 every other arm here keys on. resolveSchoolClient requires a tenant
	// ID before it builds a platform client, so a withheld level leaves the
	// client nil and the operator is told to supply credentials that are all
	// already present — the profile, the client ID and the secret — with only
	// the level dropped. Annotating the gate's own error is what puts the real
	// cause where the wrong one is printed; a warning at resolution time would
	// fire on every `school` command, including the ones that never touch the
	// Platform API.
	if errors.Is(err, platform.ErrNoPlatformClient) {
		// nil rather than scopesOf(cmd), for two independent reasons.
		//
		// No request was sent, so nothing here has an opinion on whether the
		// declared level would have been accepted — and passing the declared
		// levels selects withheldScopeNote's disagreeing branch, which talks
		// about a gateway this arm never reached.
		//
		// And school's resolver accepts one level only: resolveSchoolClient
		// reads JAMF_TENANT_ID and the profile's tenant-id and builds
		// auth.TenantScope, with no environment code path at all. `school
		// blueprints list` declares environment scope, so the disagreeing
		// branch advised an integration created at a declared level — advice
		// that returns the operator to this identical error, while omitting the
		// one input that works. Offering the withheld level back is the whole
		// remedy on this path.
		if withheld := withheldScopeNote(nil); withheld != "" {
			return fmt.Errorf("%w\n\nnote: %s", err, withheld)
		}
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
	return fmt.Errorf("%w\n\nnote: %s", err, joinNotes(scopeLevelNote(levels, have, withheldNoteState(levels)), withheld))
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

// withheldState says what the withheld note running beside this one already
// covers, which decides how much of the level note is left to say. Three
// states rather than a bool because two separate things can be duplicated: the
// remedy, and the declared-level sentence itself.
type withheldState int

const (
	// noWithheldNote: nothing was withheld, so the level note carries the
	// whole answer including its remedy.
	noWithheldNote withheldState = iota
	// withheldNoteBeside: a withheld note runs beside this one and owns the
	// remedy, because it knows the profile and the level.
	withheldNoteBeside
	// withheldNoteNamesLevels: the withheld note also names the declared
	// levels, which leaves the level note nothing to add.
	withheldNoteNamesLevels
)

// withheldNoteState reports which of those three the current record produces
// for a command declaring levels. It mirrors withheldScopeNote's own branch, so
// the two cannot disagree about which sentence is being rendered where.
func withheldNoteState(levels []string) withheldState {
	if withheldProfileScope.Profile == "" {
		return noWithheldNote
	}
	// The unreadable-reference branch returns before the levels check and names
	// none, so scopeLevelNote is what carries the declared levels there.
	if unreadableClientIDRef.Path != "" {
		return withheldNoteBeside
	}
	if len(levels) > 0 && !slices.Contains(levels, withheldProfileScope.Level) {
		return withheldNoteNamesLevels
	}
	return withheldNoteBeside
}

// scopeLevelNote renders the sentence. Split out so a test can assert the
// wording for each level without standing up a gateway error.
func scopeLevelNote(levels []string, have string, withheld withheldState) string {
	if withheld == withheldNoteNamesLevels {
		// Nothing left to say: withheldScopeNote's disagreeing branch already
		// names the declared levels, and the remedy was suppressed below. Two
		// sentences each opening "this command's API declares ..." is what
		// joinNotes produced before this.
		return ""
	}
	note := fmt.Sprintf("this command's API declares %s, and this invocation is %s-scoped.",
		renderScopeLevels(levels), have)
	if withheld == withheldNoteBeside {
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
		return note + " This invocation sent no scope header, which is what organization scope " +
			"looks like on the wire. Set an environment ID on the profile (or " +
			"JAMF_ENVIRONMENT_ID), or use an integration created at " + which + "."
	}
	return note + " Jamf Account creates an integration at one level and it only works with " +
		"that level, so fix this with a different integration, not a different ID. " +
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
// written can reach, derived from the scope levels the specs declare.
//
// It reports the *level*, never a product's entitlement or grants: a capability
// permission is per operation and is named by the 403 that wants it, so a
// summary guessing at 29 resources' permissions can only be less accurate than
// the error the operator will get anyway.
//
// scopeIDRejected is the one answer that invalidates the whole summary — the
// gateway refused the scope identifier itself, so no reachability claim can be
// made from it and none is.
func printScopeSummary(w io.Writer, root *cobra.Command, creds *platformGatewayCredentials, scopeIDRejected bool) {
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
		_, _ = fmt.Fprintf(w, "The gateway does not recognise the %s ID in this profile. Every scoped\n", level)
		_, _ = fmt.Fprintln(w, "request will be refused until you fix it, so this cannot say what the profile")
		_, _ = fmt.Fprintln(w, "reaches. A platform environment ID and a tenant ID come from different places")
		_, _ = fmt.Fprintln(w, "in Jamf Account. Re-run `jamf-cli platform setup` and answer the prompt for the")
		_, _ = fmt.Fprintln(w, "level this integration was created at.")
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
	total := len(reachable) + len(unreachable)
	switch {
	case len(unreachable) == 0:
		_, _ = fmt.Fprintf(w, "It also reaches all %d Platform API resources, audit and AI Governance included.\n", total)
	case len(reachable) == 0:
		_, _ = fmt.Fprintf(w, "It reaches none of the %d Platform API resources.\n", total)
	default:
		_, _ = fmt.Fprintf(w, "It also reaches %d of the %d Platform API resources:\n", len(reachable), total)
		_, _ = fmt.Fprintf(w, "  %s.\n", summariseResources(reachable))
	}
	if len(unreachable) > 0 {
		_, _ = fmt.Fprintf(w, "%d declare environment scope, which this credential is not at:\n", len(unreachable))
		_, _ = fmt.Fprintf(w, "  %s.\n", summariseResources(unreachable))
		// "declares", never "is out of reach": the spec is currently stricter
		// than the gateway — build v2082 moved six Platform specs to
		// environment-only and a tenant credential still reaches
		// platform-devices and platform-device-groups (probed 2026-09-05) — so
		// this summary must not be more certain than AnnotateScopeLevelError,
		// which pre-empts nothing.
		_, _ = fmt.Fprintln(w, "  Some answer on a tenant credential anyway: the gateway does not follow the")
		_, _ = fmt.Fprintln(w, "  specs everywhere, and nothing here refuses on this. Treat a 400 or 403 naming")
		_, _ = fmt.Fprintln(w, "  the scope as the signal to create an environment-scoped integration. Prefer")
		_, _ = fmt.Fprintln(w, "  environment for a new one either way.")
		_, _ = fmt.Fprintln(w, "  (jamf-cli commands -o json lists every command's declared scope under \"scopes\".)")
	}
	printPermissionsNote(w)
	_, _ = fmt.Fprintln(w, "The Jamf Account commands need an organization-scoped integration.")
}

// printPermissionsNote says where a permissions answer comes from, since setup
// does not give one.
//
// This is what a per-product entitlement probe was worth: a capability
// permission is granted per operation when the integration is created, so no
// single read establishes anything about the other 28 resources — and a wrong
// guess is expensive, because the gateway spells "no entitlement" and "this
// grant is missing" identically. The 403 that wants a permission names it, in
// the section and picker wording Jamf Account searches by.
func printPermissionsNote(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Setup does not check capability permissions. You grant those per command when")
	_, _ = fmt.Fprintln(w, "you create the integration, and a 403 names the one it wanted in the words Jamf")
	_, _ = fmt.Fprintln(w, "Account uses. `jamf-cli commands -o json` lists them under \"gatewayPermissions\".")
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
