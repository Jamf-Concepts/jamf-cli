// Copyright 2026, Jamf Software LLC

package commands

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
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

	applyPlatformGroups(cmd)

	return cmd
}

// platformGatewayRegions maps friendly names to gateway base URLs.
var platformGatewayRegions = []struct {
	key string
	url string
}{
	{"US", "https://us.apigw.jamf.com"},
	{"EU", "https://eu.apigw.jamf.com"},
	{"APAC", "https://apac.apigw.jamf.com"},
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

			cfg.Profiles[setupProfile] = config.Profile{
				URL:           creds.GatewayURL,
				AuthMethod:    "platform",
				TenantID:      creds.TenantID,
				EnvironmentID: creds.EnvironmentID,
				ClientID:      clientIDRef,
				ClientSecret:  clientSecretRef,
			}
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
				// Organization scope reaches SSO and AI Governance, neither of
				// which this CLI has commands for yet. Saying so beats implying
				// the profile drives Pro or Security Cloud.
				_, _ = fmt.Fprintln(w, "This is an organization-scoped credential. It covers organization resources")
				_, _ = fmt.Fprintln(w, "(SSO, AI Governance) — no jamf-cli command targets those yet, so set up a")
				_, _ = fmt.Fprintln(w, "profile with an environment or tenant ID to drive Pro, Platform or Security Cloud.")
			default:
				_, _ = fmt.Fprintln(w, "This scope serves the Pro API and Platform API commands.")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&setupProfile, "profile-name", "", "profile name (default: \"platform\")")

	return cmd
}
