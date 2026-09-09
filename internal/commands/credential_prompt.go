// Copyright 2026, Jamf Software LLC

package commands

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
)

// promptClientCredentials reads an OAuth2 client ID and secret from the
// terminal. The secret is read with term.ReadPassword so it is never echoed,
// and neither value is accepted via a flag, an environment variable or stdin —
// see the credential input policy in CLAUDE.md.
//
// Shared by "pro setup" and "config add-profile" so the two cannot drift on
// what they accept: a client ID may legitimately be an env: or file: reference
// in add-profile, and both commands must reject an empty value rather than
// saving a profile that cannot authenticate.
func promptClientCredentials(w io.Writer, reader *bufio.Reader) (clientID, clientSecret string, err error) {
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
		return fmt.Errorf("%w\n\njamf-cli did not write the profile. Add --no-verify to save it without checking", err)
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
