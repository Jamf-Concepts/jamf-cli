// Copyright 2026, Jamf Software LLC

package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/term"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

// promptClientCredentials reads an OAuth2 client ID and secret from the
// terminal. The secret is read with term.ReadPassword so it is never echoed,
// and neither value is accepted via a flag, an environment variable or stdin —
// see the credential input policy in CLAUDE.md.
//
// Shared by "pro setup", "platform setup", "security setup" and "config
// add-profile" so they cannot drift on what they accept: a client ID may
// legitimately be an env: or file: reference in add-profile, and every command
// must reject an empty value rather than saving a profile that cannot
// authenticate.
//
// source is printed ahead of the first prompt and says where the pair comes
// from (#438). A bare "Client ID:" is ambiguous to an operator who holds a
// platform environment ID and no secret: an environment has no secret, and
// nothing said the prompt wanted an API integration's. The caller picks the
// wording because only it knows whether the pair belongs to a Jamf Pro API
// client or a Jamf Account integration.
func promptClientCredentials(w io.Writer, reader *bufio.Reader, source string) (clientID, clientSecret string, err error) {
	_, _ = fmt.Fprintln(w, source)
	_, _ = fmt.Fprint(w, "Client ID: ")
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", "", fmt.Errorf("reading client ID: %w", err)
	}
	clientID = strings.TrimSpace(line)
	if clientID == "" {
		return "", "", fmt.Errorf("client ID is required")
	}

	_, _ = fmt.Fprint(w, "Client Secret: ")
	secretBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return "", "", fmt.Errorf("reading client secret: %w", err)
	}
	_, _ = fmt.Fprintln(w) // newline after hidden input
	clientSecret = string(secretBytes)
	if clientSecret == "" {
		return "", "", fmt.Errorf("client secret is required")
	}

	return clientID, clientSecret, nil
}

// Where a client ID and secret come from, for promptClientCredentials. Jamf Pro
// names the page "API roles and clients"; Jamf Account's is the Platform API
// integrations UI (docs/guides/platform-api-ga.md).
const (
	jamfProClientSource = "Enter the client ID and secret of a Jamf Pro API client\n" +
		"(Settings > API roles and clients)."
	platformIntegrationSource = "Enter the client ID and secret of a Jamf Account Platform API integration.\n" +
		"Do not enter the platform environment ID or the tenant ID here."
)

// clientCredentialSource returns the promptClientCredentials lead-in for an
// auth method: a platform credential is issued by Jamf Account, anything else
// by the Jamf Pro instance.
func clientCredentialSource(authMethod string) string {
	if authMethod == "platform" {
		return platformIntegrationSource
	}
	return jamfProClientSource
}

// invalidPlatformClientHint is the remedy for a gateway refusing a client ID
// and secret outright. The token exchange cannot say which of these it is, so
// all three are named; each is a mistake an operator makes in setup, not a
// fault a retry fixes. Regions: the same pair answers 401 invalid_client at
// another region's /auth/token (internal/platform/CLAUDE.md).
const invalidPlatformClientHint = "the gateway refused this client ID and secret. Check that you entered " +
	"the integration's client ID and not an environment or tenant ID, and that the " +
	"gateway URL is in the region where you registered the integration. Jamf revoked " +
	"public-beta credentials at GA, so a beta integration needs a replacement in Jamf Account"

// isInvalidClient reports whether a token exchange failed because the server
// refused the client ID and secret, as opposed to a network fault, a wrong
// URL or a server error. Two sources: jamf-cli's own exchange
// (auth.ErrInvalidClientCredentials), and the platform SDK's, which passes
// x/oauth2's RetrieveError through — "invalid_client" in the body, or a bare
// 401, which RFC 6749 §5.2 makes the same answer.
func isInvalidClient(err error) bool {
	if errors.Is(err, auth.ErrInvalidClientCredentials) {
		return true
	}
	var re *oauth2.RetrieveError
	if !errors.As(err, &re) {
		return false
	}
	return re.ErrorCode == "invalid_client" ||
		(re.Response != nil && re.Response.StatusCode == http.StatusUnauthorized)
}

// verifyProfileCredentials proves a profile's client credentials work before
// they are written to disk.
//
// It is a client-credentials token exchange and nothing more, so it establishes
// that the pair is valid and says nothing about whether the credential's
// privileges — or, for platform auth, its scope level — reach any particular
// command. Those are answered by a 403 at the point of use, in wording setup
// cannot produce.
//
// Verification is skipped, with a line saying so, when either value is an env:
// or file: reference: config.ResolveSecret owns resolving those, and a
// reference is routinely written on a machine that cannot reach the server it
// names. It is also skipped for token auth, there being no exchange to make —
// a bearer token is only testable by spending it on a real request.
func verifyProfileCredentials(ctx context.Context, w io.Writer, authMethod, url, clientID, clientSecret, tenantID, environmentID string, noVerify bool) error {
	if noVerify {
		_, _ = fmt.Fprintln(w, "Skipping verification (--no-verify).")
		return nil
	}
	if authMethod != "oauth2" && authMethod != "platform" {
		return nil
	}
	if isSecretReference(clientID) || isSecretReference(clientSecret) {
		_, _ = fmt.Fprintln(w, "Client ID or secret is an env: or file: reference. Skipping verification.")
		return nil
	}

	_, _ = fmt.Fprint(w, "Verifying credentials... ")
	var err error
	if authMethod == "platform" {
		scope := auth.TenantScope(tenantID)
		if environmentID != "" {
			scope = auth.EnvironmentScope(environmentID)
		}
		err = auth.VerifyPlatformCredentials(ctx, url, clientID, clientSecret, scope)
	} else {
		err = auth.VerifyOAuth2Credentials(ctx, url, clientID, clientSecret)
	}
	if err != nil {
		_, _ = fmt.Fprintln(w, "✗")
		wrapped := fmt.Errorf("%w\n\njamf-cli did not write the profile. Add --no-verify to save it without checking", err)
		if authMethod == "platform" && isInvalidClient(err) {
			return exitcode.Wrap(exitcode.Authentication, wrapped).WithHint(invalidPlatformClientHint)
		}
		return wrapped
	}
	_, _ = fmt.Fprintln(w, "✓")
	return nil
}

// isSecretReference reports whether a value is one of the config's
// indirection forms rather than the secret itself. keychain: is not included:
// add-profile writes a bare value into the keychain and never accepts a
// keychain reference as input.
func isSecretReference(v string) bool {
	return strings.HasPrefix(v, "env:") || strings.HasPrefix(v, "file:")
}
