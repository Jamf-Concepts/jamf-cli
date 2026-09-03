// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/gateway"
	"github.com/Jamf-Concepts/jamf-cli/internal/privileges"
)

// Cobra annotations the generators stamp. jamf:api names the API that serves a
// command; jamf:gateway and jamf:gateway-detail are set only on a Pro or
// Classic command the Jamf Platform gateway does not serve; jamf:gateway-basis
// is the evidence behind that, and selects the wording only.
const (
	annotationAPI           = "jamf:api"
	annotationGateway       = "jamf:gateway"
	annotationGatewayBasis  = "jamf:gateway-basis"
	annotationGatewayDetail = "jamf:gateway-detail"
	// annotationGatewayPrivs holds the gateway capability permissions the
	// endpoint requires, comma-separated, stamped from
	// specs/gateway/coverage.json. Distinct from jamf:privileges, which is the
	// Jamf Pro API-role vocabulary for the same endpoint.
	annotationGatewayPrivs = "jamf:gateway-privileges"
	apiPro                 = "pro"
	apiProClassic          = "pro-classic"
	apiPlatformGateway     = "platform-gateway"
)

// checkAPIMatch refuses, before anything is sent, a command whose API the
// resolved credentials cannot reach. Both directions are refused, because both
// used to fail with an error pointing at the wrong thing.
//
// Pro or Classic command on a gateway profile. The gateway does not expose every
// Jamf Pro endpoint. Its answer for a path it does not route is 403
// BAD_PERMISSIONS, byte-for-byte what a missing API-role privilege answers, so
// the operator goes hunting for a grant that cannot help.
//
// Every Unserved command is refused, and absence from the gateway's published
// spec is enough to produce that verdict — the whole table in
// internal/gateway/coverage_gen.go is BasisUnpublished today. Absence refusing
// is the deliberate choice, not an oversight: the gateway currently routes more
// than it publishes, and that is transitional, so an endpoint that answers today
// is precisely the one worth stopping. Every day a workflow keeps running
// against a route that is going away, the eventual failure gets more expensive,
// and it arrives as a bare 403 with nothing saying a withdrawal caused it. Basis
// records which evidence produced the verdict and selects the wording; it does
// not change whether the command is refused. See internal/gateway and
// docs/solutions/logic-errors/gateway-spec-absence-is-not-unrouted-2026-08-31.md,
// which records the two superseded designs this replaced.
//
// JAMF_CLI_ALLOW_UNPUBLISHED downgrades an unpublished refusal to a warning; see
// allowUnpublishedGatewayEndpoints.
//
// Platform command on an instance profile. Every generated platform command
// already gates its own RunE on platform.RequirePlatformClient, but that error
// describes how to set a platform profile up without saying that the profile in
// hand is an instance one — so an operator with a working oauth2 profile reads
// it as a setup problem with their credentials rather than as the wrong profile
// for this command. Checked here so it names both.
func checkAPIMatch(cmd *cobra.Command, provider auth.Provider, profileName string) error {
	api := cmd.Annotations[annotationAPI]
	gatewayMode := isGatewayProvider(provider)

	switch {
	case gatewayMode && (api == apiPro || api == apiProClassic):
		if cmd.Annotations[annotationGateway] != string(gateway.Unserved) {
			return nil
		}
		basis := gateway.Basis(cmd.Annotations[annotationGatewayBasis])
		detail := cmd.Annotations[annotationGatewayDetail]
		// The escape hatch applies to an unpublished endpoint only. A probed one
		// is not refused by policy — a recorded wire probe found no route at all,
		// so letting it through buys the operator a 403 and nothing else.
		if basis == gateway.BasisUnpublished && allowUnpublishedGatewayEndpoints() {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), gateway.UnpublishedOverrideWarning(cmd.CommandPath(), detail))
			return nil
		}
		// Unsupported (8), not Usage (2): the command is real and correctly
		// invoked, and only the credentials cannot reach it. Usage is also every
		// cobra flag error, unknown subcommand, missing URL, missing credential,
		// retired host and scope conflict, so a wrapper script could not tell a
		// policy refusal from a malformed invocation — the distinction a pipeline
		// needs in order to degrade rather than fail. Not NotFound (4) either: a
		// script iterating commands treats 4 as "no such thing, carry on" and
		// would swallow it. Same code as the reverse direction below, because it
		// is the same mistake in the other direction.
		return exitcode.New(exitcode.Unsupported,
			gateway.Refusal(cmd.CommandPath(), basis, detail)).
			WithHint(profileHint(profileName, provider))

	case !gatewayMode && api == apiPlatformGateway:
		return exitcode.New(exitcode.Unsupported, fmt.Sprintf(
			"%s is served by the Jamf Platform API, which the active credentials do not reach\n\n"+
				"The resolved credentials authenticate against a Jamf Pro instance (auth-method %s, from %s). Platform API commands need a platform gateway credential: a client ID and secret from a Jamf Platform API integration, against https://{region}.api.jamfcloud.com.\n\n"+
				"Set one up with `jamf-cli platform setup`, then re-run with -p <that profile>.",
			cmd.CommandPath(), authMethodName(provider), credentialSource(profileName)))
	}
	return nil
}

// envAllowUnpublished names the escape hatch for an endpoint that is absent
// from the gateway's published API but demonstrably still answers — the
// policyProperties pair is the live case: build v2051 withdrew
// GET/PUT /settings/obj/policyProperties from the published spec while
// GET /pro/settings/obj/policyProperties still returns real data.
//
// It exists because the alternative was no route at all: forceServed is a
// compile-time table in the generator, so a customer whose workflow the
// withdrawal breaks had to wait for a release. It is deliberately an
// environment variable and deliberately not a config key or a flag — a stopgap
// an operator sets for one job, not a mode a profile settles into — and the
// warning it substitutes for the refusal cannot be silenced by --quiet or
// --no-hints, because being told is the whole of what it trades away.
const envAllowUnpublished = "JAMF_CLI_ALLOW_UNPUBLISHED"

// allowUnpublishedGatewayEndpoints reports whether the operator has opted in to
// sending an endpoint outside the gateway's published API.
//
// Value-parsed rather than presence-tested, matching JAMF_CLI_NO_HINTS, so
// JAMF_CLI_ALLOW_UNPUBLISHED=0 leaves the refusal in place — a CI runner that
// exports the variable unconditionally can turn it off without unsetting it.
func allowUnpublishedGatewayEndpoints() bool {
	b, err := strconv.ParseBool(os.Getenv(envAllowUnpublished))
	return err == nil && b
}

// isGatewayProvider reports whether the resolved credentials authenticate
// against the platform gateway. Type assertion rather than reading the
// profile's auth-method, because the method can also arrive from the URL alone
// (isPlatformGatewayURL) or from env vars with no profile at all.
func isGatewayProvider(p auth.Provider) bool {
	_, ok := p.(*auth.PlatformOAuth2Provider)
	return ok
}

// authMethodName names the resolved auth method for a message.
func authMethodName(p auth.Provider) string {
	switch p.(type) {
	case *auth.PlatformOAuth2Provider:
		return "platform"
	case *auth.OAuth2Provider:
		return "oauth2"
	case *auth.TokenProvider:
		return "token"
	default:
		return "unknown"
	}
}

// credentialSource names where the resolved credentials actually came from.
//
// It exists because naming the profile was wrong. resolveAuth's precedence is
// env-then-profile, and resolvedProfile is still whatever -p or the config's
// default-profile names even when every credential came from a JAMF_* variable —
// so the hint reported `profile "default" uses auth-method platform against the
// gateway` for a profile that is oauth2 against an instance. A message that
// blames the wrong thing is the failure this whole mechanism exists to remove.
//
// The package-level vars are read rather than re-derived: resolveAuth has already
// folded the env vars into them by the time this runs, so they are the resolved
// answer and not a second copy of the precedence rules.
func credentialSource(profileName string) string {
	switch {
	case clientID != "" || clientSecret != "":
		return "the JAMF_CLIENT_ID / JAMF_CLIENT_SECRET environment variables"
	case token != "" && tokenFile != "":
		return fmt.Sprintf("the token in %s", tokenFile)
	case token != "":
		return "the JAMF_TOKEN environment variable"
	case profileName != "":
		return fmt.Sprintf("profile %q", profileName)
	default:
		return "the active credentials"
	}
}

func profileHint(name string, p auth.Provider) string {
	// Phrased with the source last so it reads correctly whether that source is
	// singular ("profile \"x\"") or plural ("the JAMF_* environment variables").
	return fmt.Sprintf("auth-method %s against the gateway, from %s", authMethodName(p), credentialSource(name))
}

// gatewayHelpMarker opens every coverage caveat. It doubles as the
// already-applied sentinel for applyGatewayCoverageHelp, which must be
// idempotent: NewRootCmd is built more than once in tests and by the MCP server,
// and a doubled caveat is a visible defect. Matching on wording instead was the
// first attempt and had a hole — the two levels word the rest of the sentence
// differently, so the Undeclared text failed a check written against the
// Unserved one and appended twice.
const gatewayHelpMarker = "Through the Jamf Platform gateway:"

// capitaliseFirst upper-cases the first rune, for a detail used as a sentence.
func capitaliseFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// gatewayCoverageHelp appends the coverage note to a command's Long text, so
// `--help` says it without the operator having to run the command and fail.
// Applied by applyGatewayCoverageHelp over the whole tree, once, at wiring time.
func gatewayCoverageHelp(cmd *cobra.Command) string {
	if gateway.Level(cmd.Annotations[annotationGateway]) != gateway.Unserved {
		return ""
	}
	detail := cmd.Annotations[annotationGatewayDetail]
	if gateway.Basis(cmd.Annotations[annotationGatewayBasis]) == gateway.BasisProbe {
		return fmt.Sprintf("\n\n%s not served. %s. Requires a profile pointed at a Jamf Pro instance (auth-method oauth2 or token).%s",
			gatewayHelpMarker, capitaliseFirst(detail), successorHelp(cmd))
	}
	return fmt.Sprintf("\n\n%s outside the published API and refused. %s. The gateway may still route it today, but that is transitional. Requires a profile pointed at a Jamf Pro instance (auth-method oauth2 or token).%s",
		gatewayHelpMarker, capitaliseFirst(detail), successorHelp(cmd))
}

// successorHelp renders the curated replacement, if any, as a trailing sentence
// for a coverage caveat. Same wording as the runtime refusal, from the same
// table, so --help and the failure cannot disagree.
func successorHelp(cmd *cobra.Command) string {
	if note := gateway.SuccessorNote(cmd.CommandPath()); note != "" {
		return " " + note
	}
	return ""
}

// gatewayGroupCoverageHelp returns the caveat for a parent command whose every
// leaf is refused.
//
// gatewayCoverageHelp alone fires only on a command carrying the verdict itself,
// and the verdict is stamped per operation — so `pro static-computer-groups
// --help`, the natural first command, listed six subcommands with no caveat when
// all six are refused, and the operator learned it only by picking one and
// running it.
//
// Only the all-refused case gets a note. A partially-refused group is a real and
// growing shape — the gateway still publishes POST /patchsoftwaretitles/id/{id}
// and nothing else on that resource — and a caveat on the group would read as
// covering subcommands that work.
func gatewayGroupCoverageHelp(cmd *cobra.Command) string {
	// A command carrying its own verdict already has the leaf note.
	if gateway.Level(cmd.Annotations[annotationGateway]) == gateway.Unserved {
		return ""
	}
	if !everyLeafRefused(cmd) {
		return ""
	}
	return fmt.Sprintf("\n\n%s every subcommand here is refused, so nothing under this command works on a gateway profile. Requires a profile pointed at a Jamf Pro instance (auth-method oauth2 or token).%s",
		gatewayHelpMarker, successorHelp(cmd))
}

// everyLeafRefused reports whether cmd has at least one runnable leaf beneath it
// and every one of them is refused.
//
// Judged over leaves rather than direct children so an intermediate group node
// counts as refused when everything under it is, and false for a leafless
// command so a pure grouping node with no operations never earns a note.
func everyLeafRefused(cmd *cobra.Command) bool {
	leaves := 0
	var walk func(*cobra.Command) bool
	walk = func(c *cobra.Command) bool {
		subs := c.Commands()
		if len(subs) == 0 {
			if !c.Runnable() {
				return true // not a leaf that can be refused; ignore it
			}
			leaves++
			return gateway.Level(c.Annotations[annotationGateway]) == gateway.Unserved
		}
		all := true
		for _, sub := range subs {
			if !walk(sub) {
				all = false
			}
		}
		return all
	}
	if !walk(cmd) {
		return false
	}
	return leaves > 0
}

// applyGatewayCoverageHelp walks the command tree and appends the coverage note
// to every leaf that carries one. Done as a walk rather than in the templates
// because the note is the same sentence for both generators and for whatever
// hand-written command inherits an annotation later.
func applyGatewayCoverageHelp(root *cobra.Command) {
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		note := gatewayCoverageHelp(c)
		if note == "" {
			note = gatewayGroupCoverageHelp(c)
		}
		if note != "" && !strings.Contains(c.Long, gatewayHelpMarker) {
			if c.Long == "" {
				c.Long = c.Short
			}
			c.Long += note
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
}

// gatewayPrivilegesOf reads a command's gateway capability permissions, or nil
// when it declares none. Separate from the Jamf Pro privileges the catalog
// already carries: the two are independent sets, so a reader has to be able to
// tell which console a name belongs to.
func gatewayPrivilegesOf(cmd *cobra.Command) []string {
	privs := cmd.Annotations[annotationGatewayPrivs]
	if privs == "" {
		return nil
	}
	return strings.Split(privs, ",")
}

// gatewayPermissionsOf renders a command's gateway permissions as Jamf Account
// prints them, one row per permission. Nil when the command declares none.
//
// Rendered here rather than stamped by the generator so the catalogue is the
// only place the names live: a generated annotation would freeze whatever the
// transcription said at generate time, and re-verifying it against Jamf's
// article would then mean a regenerate rather than an edit.
func gatewayPermissionsOf(cmd *cobra.Command) []string {
	scopes := gatewayPrivilegesOf(cmd)
	if len(scopes) == 0 && cmd.Annotations[annotationAPI] == apiPlatformGateway {
		// A Platform command's own jamf:privileges IS the capability
		// vocabulary — it is served by the gateway and nothing else — so there
		// is no second annotation to read. Without this the commands whose
		// permissions are *only* ever granted in Jamf Account were the ones with
		// no Jamf Account wording in the catalog.
		if p := cmd.Annotations["jamf:privileges"]; p != "" {
			scopes = strings.Split(p, ",")
		}
	}
	if len(scopes) == 0 {
		return nil
	}
	reqs := privileges.Collect(scopes)
	out := make([]string, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, r.String())
	}
	return out
}

// gatewaySuccessorOf returns the shipped, gateway-served replacement for a
// refused command, or "" when the command is served or has no recorded
// successor.
//
// Read from the same curated table the runtime refusal and the --help caveat
// use, so the catalog cannot say something different from what an operator is
// told. A served command never carries one: naming a replacement for a working
// command would read as a deprecation this CLI is not making.
func gatewaySuccessorOf(cmd *cobra.Command, fullPath string) string {
	if cmd.Annotations[annotationGateway] != string(gateway.Unserved) {
		return ""
	}
	command, _, ok := gateway.Successor(fullPath)
	if !ok {
		return ""
	}
	return command
}
