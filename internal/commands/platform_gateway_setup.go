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
	TenantID     string
	// SecurityCloudTenantID is optional: Jamf Security Cloud is a separate
	// product with its own tenant identifier, and plenty of platform tenants
	// have no Security Cloud entitlement at all.
	SecurityCloudTenantID string
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

	// Either tenant alone is a complete configuration. The Pro tenant is not
	// needed to reach Jamf Security Cloud — its paths carry their own tenant —
	// so someone who only has Security Cloud should not have to invent a value
	// they will never use.
	_, _ = fmt.Fprint(w, "Jamf Pro tenant ID (from Jamf Account portal; Enter to skip if you only use Security Cloud): ")
	line, _ = reader.ReadString('\n')
	creds.TenantID = strings.TrimSpace(line)

	_, _ = fmt.Fprint(w, "Jamf Security Cloud tenant ID (Enter to skip): ")
	line, _ = reader.ReadString('\n')
	creds.SecurityCloudTenantID = strings.TrimSpace(line)

	if creds.TenantID == "" && creds.SecurityCloudTenantID == "" {
		return nil, fmt.Errorf("a tenant ID is required: supply the Jamf Pro tenant ID, the Jamf Security Cloud tenant ID, or both")
	}

	return creds, nil
}

// validatePlatformGatewayCredentials checks the credentials against the gateway.
//
// Bad credentials are a hard error — nothing works without them. A rejected
// Security Cloud tenant is only a warning: the entitlement may not be
// provisioned yet, and refusing to save would block an otherwise valid
// Pro-only profile. Either way the caller saves.
func validatePlatformGatewayCredentials(ctx context.Context, w io.Writer, creds *platformGatewayCredentials) error {
	_, _ = fmt.Fprint(w, "\nValidating credentials... ")

	opts := []jamfplatform.Option{
		jamfplatform.WithTenantID(creds.TenantID),
		// No retries during setup. The default policy backs off for up to
		// ~90 seconds across three attempts, which for someone sitting at a
		// prompt reads as a hang; a mistyped secret or an unentitled tenant
		// should come back immediately and legibly.
		jamfplatform.WithRetryPolicy(0, 0, 0),
	}
	if creds.SecurityCloudTenantID != "" {
		opts = append(opts, jamfplatform.WithSecurityCloudTenantID(creds.SecurityCloudTenantID))
	}
	pc := jamfplatform.NewClient(creds.GatewayURL, creds.ClientID, creds.ClientSecret, opts...)

	if err := pc.ValidateCredentials(ctx); err != nil {
		_, _ = fmt.Fprintln(w, "failed")
		return fmt.Errorf("credential validation failed: %w", err)
	}
	_, _ = fmt.Fprintln(w, "ok")

	if creds.SecurityCloudTenantID == "" {
		return nil
	}

	// One cheap read against a Security Cloud collection every entitled tenant
	// has. The gateway distinguishes the two ways this fails, which is worth
	// passing on: the tenant being wrong is fixable here and now, the endpoint
	// not being routed is not.
	_, _ = fmt.Fprint(w, "Validating Security Cloud tenant... ")
	path := pc.Transport().TenantPrefix(securityCloudGatewayNamespace, "v1") + "/categories"
	var result any
	if err := pc.Transport().DoExpect(ctx, http.MethodGet, path, nil, http.StatusOK, &result); err != nil {
		_, _ = fmt.Fprintln(w, "failed")
		switch {
		case strings.Contains(err.Error(), "OWNERSHIP_FORBIDDEN"):
			_, _ = fmt.Fprintln(w, "  WARNING: the gateway rejected this tenant (OWNERSHIP_FORBIDDEN).")
			_, _ = fmt.Fprintln(w, "  Check the Security Cloud tenant ID in Jamf Account — note it differs")
			_, _ = fmt.Fprintln(w, "  from the Jamf Pro tenant ID, and is not the client ID.")
		case strings.Contains(err.Error(), "BAD_PERMISSIONS"):
			_, _ = fmt.Fprintln(w, "  WARNING: the gateway did not route the request (BAD_PERMISSIONS).")
			_, _ = fmt.Fprintln(w, "  Usually means this tenant has no Jamf Security Cloud entitlement.")
		default:
			_, _ = fmt.Fprintf(w, "  WARNING: %v\n", err)
		}
		_, _ = fmt.Fprintln(w, "  Saved anyway — the gateway-served security commands (dns-*, ztna-*,")
		_, _ = fmt.Fprintln(w, "  content-categories, device-groups, uem-*) will fail until it is corrected.")
		return nil
	}
	_, _ = fmt.Fprintln(w, "ok")
	return nil
}

// securityCloudGatewayNamespace is the gateway namespace Jamf Security Cloud is
// served under. It matches the namespace the SDK registers the Security Cloud
// tenant override against, so TenantPrefix resolves that tenant and not the Pro
// one.
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
