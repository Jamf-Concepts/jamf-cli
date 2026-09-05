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
// operator meant. securityCloud says whether the gateway served a Security
// Cloud read, so the caller can describe what the profile enables instead of
// guessing.
//
// scopeIDRejected is the second verdict, and it exists because the first one
// threw away the only wire answer that says the profile cannot work at all.
// Every failure of the probe used to collapse into `false`, so "no Security
// Cloud entitlement" — a fine profile — was indistinguishable from "the gateway
// does not know this scope ID", which nothing else catches either:
// ValidateCredentials is a token exchange and sends no scope header. The
// summary then claimed a scope ID the gateway had just refused reached sixteen
// Platform API resources.
func validatePlatformGatewayCredentials(ctx context.Context, w io.Writer, creds *platformGatewayCredentials) (securityCloud, scopeIDRejected bool, err error) {
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
	// This is the one construction site that does not go through
	// newPlatformSDKClient — it wants no retries, no file token cache and none
	// of the dry-run/verbose/spinner transports — so it repeats the
	// retired-host refusal rather than inheriting it. The prompt path above
	// already refuses, and today every caller comes through it; the refusal is
	// here anyway because "today every caller does" is not a property a test
	// preserves, and it is what TestOnlyTheGuardedWrapperConstructsAPlatformClient
	// requires of a file it exempts.
	if err := refuseRetiredGatewayURL(creds.GatewayURL); err != nil {
		_, _ = fmt.Fprintln(w, "failed")
		return false, false, err
	}
	pc := jamfplatform.NewClient(creds.GatewayURL, creds.ClientID, creds.ClientSecret, opts...)

	if err := pc.ValidateCredentials(ctx); err != nil {
		_, _ = fmt.Fprintln(w, "failed")
		return false, false, fmt.Errorf("credential validation failed: %w", err)
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
		return false, false, nil
	}

	_, _ = fmt.Fprint(w, "Checking Jamf Security Cloud access... ")
	path := pc.Transport().APIPrefix(securityCloudGatewayNamespace, "v1") + "/categories"
	var result any
	probeErr := pc.Transport().DoExpect(ctx, http.MethodGet, path, nil, http.StatusOK, &result)
	securityCloud, scopeIDRejected = reportSecurityCloudProbe(w, probeErr)
	return securityCloud, scopeIDRejected, nil
}

// reportSecurityCloudProbe prints what the one Security Cloud read answered and
// returns the two things the caller can conclude from it.
//
// Split out from the probe so the wording of each branch is testable without an
// HTTP server: the branches are the whole value of the probe, and the one that
// matters most was missing.
//
// **A rejected scope ID is the answer that stops the summary, and the gateway
// spells it differently per level.** Wire-probed 2026-09-05 on an EU tenant
// credential and a US organization credential, with GET /devices/v1/devices
// alongside as a control returning identical codes — so these are gateway
// verdicts on the scope header rather than anything Security Cloud decides:
//
//   - X-Environment-Id the gateway does not know — an unknown UUID, or a tenant
//     ID pasted at the environment prompt, which is the mis-paste issue #354
//     reports — answers 404 ENVIRONMENT_NOT_FOUND naming the value.
//   - X-Tenant-Id the gateway will not accept answers 403 OWNERSHIP_FORBIDDEN
//     naming the value, "Tenant 'x' is not part of your organization", and it
//     does so for an unknown UUID, for an environment ID pasted at the tenant
//     prompt, and for a real tenant belonging to another organization alike.
//
// There is no TENANT_NOT_FOUND. An earlier version of this function matched one
// beside ENVIRONMENT_NOT_FOUND as the tenant-level twin, on the assumption the
// pair was symmetric. It is not: no code by that name is returned at either
// level, so the matcher was dead and the tenant half of the mis-paste fell
// through to the plain "no" below — leaving the reachability claim that this
// whole verdict exists to stop.
//
// So OWNERSHIP_FORBIDDEN rejects the ID. Its message already told the operator
// to check it; only the verdict was wrong. **BAD_PERMISSIONS is what an
// entitlement failure looks like, and that is the distinction the two codes
// carry:** the same credential answering BAD_PERMISSIONS for its own correct
// tenant answered 200 on /devices/v1/devices in the same run, so the profile is
// good and Security Cloud simply is not provisioned for it. CLAUDE.md used to
// record the two as indistinguishable in intent, which is what the earlier
// (false, false) rested on. Read on one credential and one tenant per level:
// enough to move the branch, not enough to claim it holds for every entitlement
// shape, so a future OWNERSHIP_FORBIDDEN on a demonstrably owned tenant is the
// thing that would send this back.
func reportSecurityCloudProbe(w io.Writer, err error) (securityCloud, scopeIDRejected bool) {
	if err == nil {
		_, _ = fmt.Fprintln(w, "yes")
		return true, false
	}
	switch {
	case strings.Contains(err.Error(), "ENVIRONMENT_NOT_FOUND"):
		_, _ = fmt.Fprintln(w, "no — the gateway does not know this environment ID")
		_, _ = fmt.Fprintln(w, "  A platform environment ID and a tenant ID are different values from different")
		_, _ = fmt.Fprintln(w, "  places in Jamf Account. Re-run setup and answer the prompt for the level this")
		_, _ = fmt.Fprintln(w, "  integration was created at.")
		return false, true
	case strings.Contains(err.Error(), "OWNERSHIP_FORBIDDEN"):
		// Not an entitlement answer: the gateway refuses this tenant ID for
		// this credential, so every scoped request will be refused too.
		_, _ = fmt.Fprintln(w, "no — the gateway will not accept this tenant ID for these credentials")
		_, _ = fmt.Fprintln(w, "  Either it belongs to another organization, or it is not a tenant ID at all —")
		_, _ = fmt.Fprintln(w, "  a platform environment ID goes in the environment prompt, and neither is the")
		_, _ = fmt.Fprintln(w, "  Jamf Pro tenant ID or the client ID. Check it in Jamf Account and re-run setup.")
		return false, true
	case strings.Contains(err.Error(), "BAD_PERMISSIONS"):
		// The scope ID is fine and this tenant has no Security Cloud
		// entitlement, which is a normal Jamf Pro profile. The summary stands.
		_, _ = fmt.Fprintln(w, "no (no Security Cloud entitlement)")
	default:
		_, _ = fmt.Fprintf(w, "no (%v)\n", err)
	}
	return false, false
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
