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
	_, _ = fmt.Fprintln(w, "Jamf Account creates an API integration at one level: organization, platform")
	_, _ = fmt.Fprintln(w, "environment, or tenant. Supply the ID for the level this one was created at.")

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
// Two questions. Are the credentials good — a hard error, since nothing works
// without them. And does the gateway recognise the scope ID just typed, which
// nothing else catches: ValidateCredentials is a token exchange and sends no
// scope header, so a mis-pasted ID saves cleanly and then refuses every
// command.
//
// **It checks no product's access or permissions, deliberately.** A capability
// permission is per operation, not per profile, and the gateway's 403 already
// names the one it wanted in Jamf Account's own words (EnrichPrivilegeError).
// A scope ID is agnostic about products.
func validatePlatformGatewayCredentials(ctx context.Context, w io.Writer, creds *platformGatewayCredentials) (scopeIDRejected bool, err error) {
	_, _ = fmt.Fprint(w, "\nValidating credentials... ")

	opts := []jamfplatform.Option{
		// No retries during setup. A mistyped secret should come back
		// immediately, not after the SDK's backoff ladder — bounded at ~22s,
		// which at an interactive prompt is 22s spent on an answer that will
		// not change.
		jamfplatform.WithRetryPolicy(0, 0, 0),
	}
	switch {
	case creds.EnvironmentID != "":
		opts = append(opts, jamfplatform.WithEnvironmentID(creds.EnvironmentID))
	case creds.TenantID != "":
		opts = append(opts, jamfplatform.WithTenantID(creds.TenantID))
	}
	// The one construction site that does not go through newPlatformSDKClient
	// — it wants no retries, no file token cache and none of the
	// dry-run/verbose/spinner transports — so it repeats the retired-host
	// refusal rather than inheriting it. "Every caller comes through the
	// prompt, which already refuses" is not a property a test preserves, and
	// TestOnlyTheGuardedWrapperConstructsAPlatformClient requires this of a
	// file it exempts.
	if err := refuseRetiredGatewayURL(creds.GatewayURL); err != nil {
		_, _ = fmt.Fprintln(w, "failed")
		return false, err
	}
	pc := jamfplatform.NewClient(creds.GatewayURL, creds.ClientID, creds.ClientSecret, opts...)

	if err := pc.ValidateCredentials(ctx); err != nil {
		_, _ = fmt.Fprintln(w, "failed")
		return false, fmt.Errorf("credential validation failed: %w", err)
	}
	_, _ = fmt.Fprintln(w, "ok")

	// Organization scope sends no scope header at all, so there is no ID to
	// check and the probe would report a failure that says nothing about the
	// profile. Wire-confirmed 2026-09-08: an organization credential answers
	// 200 on /licensing/v1/licenses and 400 REQUEST_CONTEXT_NOT_PROVIDED on
	// every scoped namespace, which is correct behaviour rather than a fault.
	if creds.EnvironmentID == "" && creds.TenantID == "" {
		return false, nil
	}

	_, _ = fmt.Fprint(w, "Checking the scope ID... ")
	path := pc.Transport().APIPrefix(scopeProbeNamespace, "v1") + scopeProbeResource
	var result any
	probeErr := pc.Transport().DoExpect(ctx, http.MethodGet, path, nil, http.StatusOK, &result)
	// The level is what the operator typed, and the probe cannot see it from an
	// error: the ownership refusal is worded from it so a refused environment ID
	// is not called a tenant ID.
	level := "tenant"
	if creds.EnvironmentID != "" {
		level = "environment"
	}
	return reportScopeIDProbe(w, level, probeErr), nil
}

// reportScopeIDProbe prints what the scope probe answered and reports whether
// the gateway refused the scope identifier itself. Split out so each branch's
// wording is testable without an HTTP server.
//
// **Only two codes reject an ID, and the gateway spells the refusal differently
// per level.** Wire-probed 2026-09-08 against GET /pro/v1/jamf-pro-version on
// an EU tenant, an EU platform environment and a US organization credential:
//
//   - An X-Environment-Id the gateway does not know — an unknown UUID, or a
//     tenant ID pasted at the environment prompt (issue #354) — answers 404
//     ENVIRONMENT_NOT_FOUND naming the value.
//   - An X-Tenant-Id it will not accept answers 403 OWNERSHIP_FORBIDDEN naming
//     the value, for an unknown UUID and for an environment or organization ID
//     pasted at the tenant prompt alike. There is no TENANT_NOT_FOUND.
//   - The right ID answers 200 at either level; no header answers 400
//     REQUEST_CONTEXT_NOT_PROVIDED.
//
// **Everything else leaves the ID unjudged**, because this probe asks about the
// scope header and nothing else: a BAD_PERMISSIONS, a 5xx or a timeout says
// nothing about the ID. That also covers the shape these credentials could not
// construct — a scope with no Jamf Pro behind it, where the path may answer
// something other than 200.
//
// level is a parameter because an error cannot answer it: OWNERSHIP_FORBIDDEN
// is reachable at either level, so a refused environment ID must not be called
// a tenant ID.
func reportScopeIDProbe(w io.Writer, level string, err error) (scopeIDRejected bool) {
	if err == nil {
		_, _ = fmt.Fprintf(w, "ok: the gateway accepts this %s ID\n", level)
		return false
	}
	switch {
	case strings.Contains(err.Error(), "ENVIRONMENT_NOT_FOUND"):
		_, _ = fmt.Fprintln(w, "rejected: the gateway does not know this environment ID")
		_, _ = fmt.Fprintln(w, "  A platform environment ID and a tenant ID come from different places in Jamf")
		_, _ = fmt.Fprintln(w, "  Account. Re-run setup and answer the prompt for the level this integration")
		_, _ = fmt.Fprintln(w, "  was created at.")
		return true
	case strings.Contains(err.Error(), "OWNERSHIP_FORBIDDEN"):
		_, _ = fmt.Fprintf(w, "rejected: the gateway will not accept this %s ID for these credentials\n", level)
		_, _ = fmt.Fprintln(w, "  This ID belongs to another organization, or it is the wrong kind of ID. A")
		_, _ = fmt.Fprintln(w, "  platform environment ID and a tenant ID come from different places in Jamf")
		_, _ = fmt.Fprintln(w, "  Account, and neither is the Jamf Pro tenant ID or the client ID. Check it in")
		_, _ = fmt.Fprintln(w, "  Jamf Account, then re-run setup.")
		return true
	}
	// Not an answer about the ID. Say so plainly rather than converting it into
	// a verdict: a capability refusal in particular means the scope header got
	// as far as being resolved, since the gateway checks ownership before
	// capability.
	_, _ = fmt.Fprintf(w, "could not confirm (%v)\n", err)
	return false
}

// The path the scope probe sends the header on. **It is a carrier, not a
// product access check** — the gateway resolves the scope at the edge, before
// routing and before capability, so the verdict does not depend on this
// endpoint existing or on the product being entitled. Wire-checked 2026-09-08:
// a wrong ID earns the same ENVIRONMENT_NOT_FOUND / OWNERSHIP_FORBIDDEN on
// /pro/v1/definitely-not-a-real-endpoint and on a Security Cloud path, and only
// an unrouted *namespace* escapes scope evaluation (Tyk's bare "404 page not
// found").
//
// This one is chosen because it needs no capability grant — one of the 44 Jamf
// Pro endpoints declaring none — so a correct ID answers a clean 200 instead of
// the BAD_PERMISSIONS every grant-bearing path returns, which would leave
// nothing to distinguish "accepted" from "refused on grants".
const (
	scopeProbeNamespace = "pro"
	scopeProbeResource  = "/jamf-pro-version"
)

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
