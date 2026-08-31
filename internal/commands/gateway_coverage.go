// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/gateway"
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
	apiPro                  = "pro"
	apiProClassic           = "pro-classic"
	apiPlatformGateway      = "platform-gateway"
)

// checkAPIMatch refuses, before anything is sent, a command whose API the
// resolved credentials cannot reach. Both directions are refused, because both
// used to fail with an error pointing at the wrong thing.
//
// Pro or Classic command on a gateway profile. The gateway does not expose every
// Jamf Pro endpoint — app installers are the established case. Its answer for a
// path it does not route is 403 BAD_PERMISSIONS, byte-for-byte what a missing
// API-role privilege answers, so the operator goes hunting for a grant that
// cannot help. Refused only at the Unserved level, which only a recorded wire
// probe produces: the gateway routes more than its published spec declares, so
// absence from the spec annotates and hints but never refuses. See
// internal/gateway.
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
		// Usage (2), not NotFound (4): the mistake is the profile, not a
		// missing object. A script that iterates commands treats 4 as "no such
		// thing, carry on" and would swallow this, where 2 says the invocation
		// itself was wrong. Same code as the reverse direction below, because
		// it is the same mistake in the other direction.
		return exitcode.New(exitcode.Usage,
			gateway.Refusal(cmd.CommandPath(),
				gateway.Basis(cmd.Annotations[annotationGatewayBasis]),
				cmd.Annotations[annotationGatewayDetail])).
			WithHint(profileHint(profileName, provider))

	case !gatewayMode && api == apiPlatformGateway:
		return exitcode.New(exitcode.Usage, fmt.Sprintf(
			"%s is served by the Jamf Platform API, which the active credentials do not reach\n\n"+
				"The resolved credentials authenticate against a Jamf Pro instance (auth-method %s, from %s). Platform API commands need a platform gateway credential: a client ID and secret from a Jamf Platform API integration, against https://{region}.api.jamfcloud.com.\n\n"+
				"Set one up with `jamf-cli platform setup`, then re-run with -p <that profile>.",
			cmd.CommandPath(), authMethodName(provider), credentialSource(profileName)))
	}
	return nil
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
		return fmt.Sprintf("\n\n%s not served. %s. Requires a profile pointed at a Jamf Pro instance (auth-method oauth2 or token).",
			gatewayHelpMarker, capitaliseFirst(detail))
	}
	return fmt.Sprintf("\n\n%s outside the published API and refused. %s. The gateway may still route it today, but that is transitional. Requires a profile pointed at a Jamf Pro instance (auth-method oauth2 or token).",
		gatewayHelpMarker, capitaliseFirst(detail))
}

// applyGatewayCoverageHelp walks the command tree and appends the coverage note
// to every leaf that carries one. Done as a walk rather than in the templates
// because the note is the same sentence for both generators and for whatever
// hand-written command inherits an annotation later.
func applyGatewayCoverageHelp(root *cobra.Command) {
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if note := gatewayCoverageHelp(c); note != "" && !strings.Contains(c.Long, gatewayHelpMarker) {
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
