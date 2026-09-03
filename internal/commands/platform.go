// Copyright 2026, Jamf Software LLC

package commands

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	platformgen "github.com/Jamf-Concepts/jamf-cli/internal/commands/platform/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newPlatformCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "platform",
		Short: "Jamf Platform commands",
		Long:  "Setup and administration for the Jamf Platform Gateway.",
	}

	cmd.AddCommand(newPlatformSetupCmd())
	cmd.AddCommand(newPlatformAuthCmd(cliCtx))

	// Spec-generated Jamf AI Governance commands. These live here rather than
	// under `pro` — where the other generated Platform resources sit — because
	// AI Governance is not a Jamf Pro surface: it is scoped at the organization
	// or platform-environment level, its privileges are its own
	// (ai-policies:read and friends), and a credential reaching it need name no
	// Jamf Pro tenant at all. `pro ai-policies` would have implied otherwise.
	cmd.AddCommand(platformgen.NewAiPoliciesCmd(cliCtx))
	cmd.AddCommand(platformgen.NewAiToolsCmd(cliCtx))

	// Spec-generated Jamf Account commands — licensing, partners and SSO. Also
	// here rather than under `pro`, and for a stronger reason than AI
	// Governance: these are organization-level services that send no scope
	// header at all, so the credential reaching them names no Jamf Pro, School,
	// Protect or Security Cloud tenant. They are also US-only; newAccountCmds
	// applies that guard.
	cmd.AddCommand(newAccountCmds(cliCtx)...)

	// Spec-generated platform audit commands. Environment-scoped, and served in
	// every region — unlike the account trio above.
	cmd.AddCommand(newPlatformAuditCmd(cliCtx))

	applyPlatformGroups(cmd)
	applyPlatformAliases(cmd)

	return cmd
}

// platformGatewayRegions maps friendly names to gateway base URLs.
//
// These are the GA gateway hosts. The previous gateway,
// {region}.apigw.jamf.com, is retired at Platform API GA (2026-09-01) and
// required an /api segment the new host does not serve — see
// retiredGatewayHost, which turns a profile still naming it into an
// instruction rather than an authentication failure.
var platformGatewayRegions = []struct {
	key string
	url string
}{
	{"US", "https://us.api.jamfcloud.com"},
	{"EU", "https://eu.api.jamfcloud.com"},
	{"APAC", "https://apac.api.jamfcloud.com"},
}

// platformGatewayHostSuffix is the host suffix every GA platform gateway shares
// ("us.api.jamfcloud.com", "eu.api.jamfcloud.com", ...).
//
// Matched as a suffix rather than against platformGatewayRegions so a region
// Jamf adds later needs no code change here. It cannot collide with a Jamf Pro
// instance URL: an instance is "<tenant>.jamfcloud.com", so reaching this
// two-label suffix would take a tenant literally named "api" *and* a subdomain
// beneath it, which the instance naming does not produce.
const platformGatewayHostSuffix = ".api.jamfcloud.com"

// isPlatformGatewayURL reports whether a URL names a GA platform gateway, which
// is a request for platform gateway auth whether or not a scope ID accompanies
// it. Organization-scoped credentials name no scope at all, so the host is the
// only signal they give.
func isPlatformGatewayURL(rawURL string) bool {
	host := strings.TrimSuffix(strings.TrimSpace(rawURL), "/")
	if _, after, ok := strings.Cut(host, "://"); ok {
		host = after
	}
	host, _, _ = strings.Cut(host, "/")
	host, _, _ = strings.Cut(host, ":")
	return strings.HasSuffix(strings.ToLower(host), platformGatewayHostSuffix)
}

// retiredGatewayHost is the host of the pre-GA platform gateway. Every profile
// written before 2026-08-28 names it, and it does not serve the GA path shape.
const retiredGatewayHost = "apigw.jamf.com"

// platformGatewayURLForRegion maps a retired-gateway URL onto its GA
// replacement, preserving the region. Returns "" when the URL is not a retired
// gateway URL.
//
// This exists so the failure names the fix. A profile pointing at
// {region}.apigw.jamf.com fails during the token exchange, before the command
// the user typed is ever sent, and the gateway's answer there is an edge-level
// 403 with an HTML body — nothing in it says "your base URL is a host that no
// longer serves this API". Rewriting silently was the other option and is
// worse: the profile on disk stays wrong, so every other tool reading it keeps
// failing, and a URL the user did not type is a bad thing to send a credential to.
func platformGatewayURLForRegion(rawURL string) string {
	trimmed := strings.TrimSuffix(strings.TrimSpace(rawURL), "/")
	host := trimmed
	if _, after, ok := strings.Cut(host, "://"); ok {
		host = after
	}
	host, _, _ = strings.Cut(host, "/")
	region, rest, ok := strings.Cut(host, ".")
	if !ok || rest != retiredGatewayHost {
		return ""
	}
	return "https://" + region + ".api.jamfcloud.com"
}

// refuseRetiredGatewayURL rejects the pre-GA gateway host by name, or returns
// nil for any other URL.
//
// It is checked before a client is built rather than at the request, because
// the failure the retired host produces lands in the *token exchange* — before
// the command the user typed is sent — as an edge-level 403 with an HTML body
// that names neither the host nor the reason. An operator reads that as an
// authentication failure and rotates a working client secret.
//
// Rewriting the URL silently was the other option and is worse: the profile on
// disk stays wrong for every other tool reading it, and a URL the user did not
// type is a bad thing to send a credential to. So it names the replacement and
// the user edits the profile.
func refuseRetiredGatewayURL(rawURL string) error {
	ga := platformGatewayURLForRegion(rawURL)
	if ga == "" {
		return nil
	}
	return exitcode.New(exitcode.Usage, fmt.Sprintf(
		"%s is the retired Jamf Platform gateway and does not serve the GA API paths.\nSet url: %s in this profile (jamf-cli config path prints the file), or re-run `jamf-cli platform setup`.",
		rawURL, ga))
}

// mergePlatformProfile writes the gateway half of a profile onto whatever the
// profile already holds, leaving every field this command does not own alone.
//
// A fresh config.Profile literal was the bug: the struct carries every
// product's credentials, so replacing the entry silently zeroed the Radar
// pairs, SSEURL and Product that `security setup` writes. That was harmless
// while the two products used disjoint profiles and is not any more — `security
// setup` now closes by pointing here, and securityPlatformSDKClient is built
// around a profile carrying both credential sets. Replacing meant running the
// two setups against one profile name dropped whichever ran first, and
// re-running that one to fix it dropped the other back out. The keychain
// entries survive either way, so nothing looks deleted: the operator just sees
// credentials they know they entered reported as absent.
func mergePlatformProfile(existing config.Profile, creds *platformGatewayCredentials, clientIDRef, clientSecretRef string) config.Profile {
	p := existing
	p.URL = creds.GatewayURL
	// auth-method platform is what ResolveAuthForProfile reads to enter
	// gateway auth, so it is set unconditionally: it is the whole point of
	// running this command, and the `security` tree is selected by the command
	// namespace rather than by this field.
	p.AuthMethod = "platform"
	p.TenantID = creds.TenantID
	p.EnvironmentID = creds.EnvironmentID
	p.ClientID = clientIDRef
	p.ClientSecret = clientSecretRef
	return p
}

func newPlatformSetupCmd() *cobra.Command {
	var setupProfile string

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure a Jamf Platform Gateway profile",
		Long: `Guided setup for platform gateway authentication. Prompts for region, API client
credentials, and the scope the integration was created at, validates them against
the gateway, and saves the profile.

An API integration is created at one of three levels in Jamf Account, and its
credential only works with that level:

  organization           SSO, AI Governance — supply neither ID
  platform environment   a group of tenants across product types (preferred)
  tenant                 a single Jamf Pro, School, Protect or Security Cloud
                         tenant (legacy)

Environment and tenant are mutually exclusive; a customer holding several tenants
without a platform environment makes a profile per tenant.

Create API client credentials in the Jamf Account portal
(account.jamf.com) before running this command.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			reader := bufio.NewReader(os.Stdin)

			if noInput {
				return fmt.Errorf("setup requires interactive input; cannot use --no-input")
			}

			// 1. Profile name
			if setupProfile == "" {
				_, _ = fmt.Fprint(w, "Profile name [platform]: ")
				line, _ := reader.ReadString('\n')
				setupProfile = strings.TrimSpace(line)
				if setupProfile == "" {
					setupProfile = "platform"
				}
			}

			// 2-4. Gateway credentials (shared with `security setup`)
			creds, err := promptPlatformGatewayCredentials(w, reader)
			if err != nil {
				return err
			}

			// 5. Validate. Security Cloud access is reported, not enforced.
			securityCloud, err := validatePlatformGatewayCredentials(cmd.Context(), w, creds)
			if err != nil {
				return err
			}

			// 6. Store secrets in keychain
			clientIDRef, clientSecretRef, err := storePlatformGatewaySecrets(setupProfile, creds)
			if err != nil {
				return err
			}

			// 7. Save profile
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			cfg.Profiles[setupProfile] = mergePlatformProfile(
				cfg.Profiles[setupProfile], creds, clientIDRef, clientSecretRef)
			cfg.DefaultProfile = setupProfile

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			_, _ = fmt.Fprintf(w, "\nProfile %q saved and set as default.\n", setupProfile)
			_, _ = fmt.Fprintf(w, "  Gateway:     %s\n", creds.GatewayURL)
			switch {
			case creds.EnvironmentID != "":
				_, _ = fmt.Fprintf(w, "  Scope:       platform environment %s\n", creds.EnvironmentID)
			case creds.TenantID != "":
				_, _ = fmt.Fprintf(w, "  Scope:       tenant %s\n", creds.TenantID)
			default:
				_, _ = fmt.Fprintln(w, "  Scope:       organization (resolved from the credential)")
			}
			_, _ = fmt.Fprintf(w, "  Client ID:   %s\n", creds.ClientID)
			_, _ = fmt.Fprintln(w, "  Secrets stored in system keychain")
			_, _ = fmt.Fprintln(w)
			// State what the profile actually enables, from what the gateway
			// answered rather than from which prompt was filled in: one tenant
			// belongs to one product, and claiming a surface it cannot reach
			// sends someone chasing a 403 that is really a missing entitlement.
			switch {
			case securityCloud:
				_, _ = fmt.Fprintln(w, "This scope serves the gateway-served Jamf Security Cloud commands")
				_, _ = fmt.Fprintln(w, "(dns-*, ztna-*, content-categories, device-groups, uem-*).")
			case creds.EnvironmentID == "" && creds.TenantID == "":
				// Naming the surfaces beats implying the profile drives Pro or
				// Security Cloud, which it cannot: an organization-scoped
				// credential sends no scope header and reaches no product API.
				_, _ = fmt.Fprintln(w, "This is an organization-scoped credential. It serves the Jamf Account commands")
				_, _ = fmt.Fprintln(w, "(account-licenses, deal-registrations, distributor-*, sso-connections, sso-domains)")
				_, _ = fmt.Fprintln(w, "and AI Governance (ai-policies, ai-tools). The Jamf Account ones are US-only.")
				_, _ = fmt.Fprintln(w, "Set up a profile with an environment or tenant ID to drive Pro, Platform,")
				_, _ = fmt.Fprintln(w, "Security Cloud or audit.")
			default:
				_, _ = fmt.Fprintln(w, "This scope serves the Pro API and Platform API commands.")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&setupProfile, "profile-name", "", "profile name (default: \"platform\")")

	return cmd
}
