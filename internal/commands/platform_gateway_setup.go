// Copyright 2026, Jamf Software LLC

package commands

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/keychain"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
)

// platformGatewayCredentials is what `platform setup` collects. Gateway
// configuration lives there and only there: `security setup` owns the Radar
// application credentials and points at this command for the rest.
type platformGatewayCredentials struct {
	GatewayURL   string
	ClientID     string
	ClientSecret string
	// EnvironmentID and TenantID are the two scope identifiers, and at most one
	// is set. An API integration is created at one of three levels in Jamf
	// Account — organization, platform environment, or tenant — and its
	// credential only works with that level's header, so this is a choice
	// between integrations rather than between two IDs. Both empty is an
	// organization-scoped integration, which sends no scope header at all.
	EnvironmentID string
	TenantID      string
}

// promptPlatformGatewayCredentials runs the interactive gateway credential
// prompts. The client secret is read with term.ReadPassword and never echoed;
// per the credential policy it has no flag or env-var equivalent.
func promptPlatformGatewayCredentials(w io.Writer, reader *bufio.Reader) (*platformGatewayCredentials, error) {
	creds := &platformGatewayCredentials{}

	_, _ = fmt.Fprintln(w, "\nPlatform gateway region:")
	for i, r := range platformGatewayRegions {
		_, _ = fmt.Fprintf(w, "  %d. %s (%s)\n", i+1, r.key, r.url)
	}
	_, _ = fmt.Fprintf(w, "  %d. Custom URL\n", len(platformGatewayRegions)+1)
	_, _ = fmt.Fprintf(w, "Choose [1-%d]: ", len(platformGatewayRegions)+1)

	line, _ := reader.ReadString('\n')
	choice := strings.TrimSpace(line)

	n := 0
	if _, err := fmt.Sscanf(choice, "%d", &n); err == nil && n >= 1 && n <= len(platformGatewayRegions) {
		creds.GatewayURL = platformGatewayRegions[n-1].url
	} else {
		_, _ = fmt.Fprint(w, "Gateway URL: ")
		line, _ = reader.ReadString('\n')
		creds.GatewayURL = strings.TrimSpace(line)
	}
	if creds.GatewayURL == "" {
		return nil, fmt.Errorf("gateway URL is required")
	}
	normalized, err := normalizeURL(creds.GatewayURL)
	if err != nil {
		return nil, fmt.Errorf("invalid gateway URL: %w", err)
	}
	// The listed regions are GA hosts, so this can only come from the Custom URL
	// branch — someone pasting the URL out of an existing profile or an old
	// runbook. Caught here rather than at the validation call below, because the
	// retired host answers the token exchange with an edge-level 403 carrying an
	// HTML body: setup would report "invalid client credentials" for a URL
	// problem, and the operator would go and rotate a working secret.
	if ga := platformGatewayURLForRegion(normalized); ga != "" {
		return nil, fmt.Errorf("%s is the retired Jamf Platform gateway and does not serve the GA API paths; use %s", normalized, ga)
	}
	creds.GatewayURL = normalized

	_, _ = fmt.Fprint(w, "\nClient ID: ")
	line, _ = reader.ReadString('\n')
	creds.ClientID = strings.TrimSpace(line)
	if creds.ClientID == "" {
		return nil, fmt.Errorf("client ID is required")
	}

	_, _ = fmt.Fprint(w, "Client Secret: ")
	secretBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return nil, fmt.Errorf("reading client secret: %w", err)
	}
	_, _ = fmt.Fprintln(w)
	creds.ClientSecret = string(secretBytes)
	if creds.ClientSecret == "" {
		return nil, fmt.Errorf("client secret is required")
	}

	creds.EnvironmentID, creds.TenantID = promptScope(w, reader)

	return creds, nil
}

// promptScope asks which level this integration was created at, and returns the
// one identifier that answers it.
//
// Environment is asked first because it is the level to prefer, and an answer
// there ends the questioning: the levels are mutually exclusive, so asking for a
// tenant next would be offering a combination no credential can use. The tenant
// prompt is therefore only reached when environment is left blank, and leaving
// both blank is an organization-scoped integration, whose scope the gateway
// resolves from the access token.
//
// It returns at most one non-empty value, which is what lets the caller skip
// reconciling a pair it can never legitimately hold.
func promptScope(w io.Writer, reader *bufio.Reader) (environmentID, tenantID string) {
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "An API integration is created at one level: organization, platform environment,")
	_, _ = fmt.Fprintln(w, "or tenant. Supply the ID for the level this integration was created at.")

	_, _ = fmt.Fprint(w, "Platform environment ID (Enter to skip): ")
	line, _ := reader.ReadString('\n')
	if environmentID = strings.TrimSpace(line); environmentID != "" {
		return environmentID, ""
	}

	_, _ = fmt.Fprint(w, "Tenant ID (Enter to skip for an organization-scoped integration): ")
	line, _ = reader.ReadString('\n')
	return "", strings.TrimSpace(line)
}

// validatePlatformGatewayCredentials checks the credentials against the gateway.
//
// Bad credentials are a hard error — nothing works without them. Whether the
// tenant can reach Jamf Security Cloud is reported rather than enforced: a Jamf
// Pro tenant legitimately cannot, and nothing here knows which product the
// operator meant. The bool says whether the gateway served a Security Cloud
// read, so the caller can describe what the profile enables instead of guessing.
func validatePlatformGatewayCredentials(ctx context.Context, w io.Writer, creds *platformGatewayCredentials) (securityCloud bool, err error) {
	_, _ = fmt.Fprint(w, "\nValidating credentials... ")

	opts := []jamfplatform.Option{
		// No retries during setup. A mistyped secret or an unentitled tenant
		// should come back immediately and legibly, not after a backoff that
		// reads as a hang to someone sitting at a prompt.
		//
		// The earlier note here said the default policy backs off "~90 seconds
		// across three attempts". Both numbers were wrong: it was five attempts,
		// and the curve was RateLimitLinearJitterBackoff sampling uniformly over
		// the whole [1s,60s] window times the attempt number, so the first retry
		// alone averaged ~30s and a full sequence averaged over three minutes.
		// SDK 1529d60 replaced it with DefaultBackoff plus the intended clamp,
		// bounding the waits at 1+2+4+8 = 15s — measured at 22s wall clock for a
		// persistently-502 GET. Still worth disabling here: 22s at a prompt for
		// an answer that will not change is 22s wasted.
		jamfplatform.WithRetryPolicy(0, 0, 0),
	}
	switch {
	case creds.EnvironmentID != "":
		opts = append(opts, jamfplatform.WithEnvironmentID(creds.EnvironmentID))
	case creds.TenantID != "":
		opts = append(opts, jamfplatform.WithTenantID(creds.TenantID))
	}
	pc := jamfplatform.NewClient(creds.GatewayURL, creds.ClientID, creds.ClientSecret, opts...)

	if err := pc.ValidateCredentials(ctx); err != nil {
		_, _ = fmt.Fprintln(w, "failed")
		return false, fmt.Errorf("credential validation failed: %w", err)
	}
	_, _ = fmt.Fprintln(w, "ok")

	// One cheap read against a Security Cloud collection every entitled tenant
	// has. Its purpose is to tell the operator which half of `security` this
	// profile serves, not to pass or fail the profile: the two ways it fails are
	// indistinguishable from here — a Jamf Pro tenant has no Security Cloud
	// entitlement, and a mistyped tenant is not this organisation's.
	// Organization-scoped credentials do not reach product APIs at all, so the
	// probe would report a failure that says nothing about the profile.
	if creds.EnvironmentID == "" && creds.TenantID == "" {
		return false, nil
	}

	_, _ = fmt.Fprint(w, "Checking Jamf Security Cloud access... ")
	path := pc.Transport().APIPrefix(securityCloudGatewayNamespace, "v1") + "/categories"
	var result any
	if err := pc.Transport().DoExpect(ctx, http.MethodGet, path, nil, http.StatusOK, &result); err != nil {
		switch {
		case strings.Contains(err.Error(), "OWNERSHIP_FORBIDDEN"):
			_, _ = fmt.Fprintln(w, "no (tenant not owned by this organization)")
			_, _ = fmt.Fprintln(w, "  If this was meant to be a Security Cloud profile, check the tenant ID in Jamf")
			_, _ = fmt.Fprintln(w, "  Account — it differs from the Jamf Pro tenant ID, and is not the client ID.")
		case strings.Contains(err.Error(), "BAD_PERMISSIONS"):
			_, _ = fmt.Fprintln(w, "no (no Security Cloud entitlement)")
		default:
			_, _ = fmt.Fprintf(w, "no (%v)\n", err)
		}
		return false, nil
	}
	_, _ = fmt.Fprintln(w, "yes")
	return true, nil
}

// securityCloudGatewayNamespace is the gateway namespace Jamf Security Cloud is
// served under. It matches the namespace the SDK registers the Security Cloud
// tenant override against, so a Security Cloud request is scoped to that tenant
// and not the Pro one.
const securityCloudGatewayNamespace = "securitycloud"

// storePlatformGatewaySecrets writes the client credentials to the keychain and
// returns the profile references that stand in for them, so the plaintext never
// reaches the config file.
func storePlatformGatewaySecrets(profileName string, creds *platformGatewayCredentials) (clientIDRef, clientSecretRef string, err error) {
	store := config.GetKeychainStore()
	if err := store.Set(keychain.DefaultService, profileName+"/client-id", creds.ClientID); err != nil {
		return "", "", keychain.WriteError("client ID", err)
	}
	if err := store.Set(keychain.DefaultService, profileName+"/client-secret", creds.ClientSecret); err != nil {
		return "", "", keychain.WriteError("client secret", err)
	}
	return keychain.KeychainRef(profileName, "client-id"), keychain.KeychainRef(profileName, "client-secret"), nil
}
