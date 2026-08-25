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
		Long: `Guided setup for platform gateway authentication. Prompts for region,
API client credentials, tenant ID, and optionally a Jamf Security Cloud tenant
ID, validates them against the gateway, and saves the profile. This profile
enables the Pro API, the Platform API, and — with a Security Cloud tenant — the
gateway-served Jamf Security Cloud commands.

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
				URL:          creds.GatewayURL,
				AuthMethod:   "platform",
				TenantID:     creds.TenantID,
				ClientID:     clientIDRef,
				ClientSecret: clientSecretRef,
			}
			cfg.DefaultProfile = setupProfile

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			_, _ = fmt.Fprintf(w, "\nProfile %q saved and set as default.\n", setupProfile)
			_, _ = fmt.Fprintf(w, "  Gateway:     %s\n", creds.GatewayURL)
			_, _ = fmt.Fprintf(w, "  Tenant ID:   %s\n", creds.TenantID)
			_, _ = fmt.Fprintf(w, "  Client ID:   %s\n", creds.ClientID)
			_, _ = fmt.Fprintln(w, "  Secrets stored in system keychain")
			_, _ = fmt.Fprintln(w)
			// State what the profile actually enables, from what the gateway
			// answered rather than from which prompt was filled in: one tenant
			// belongs to one product, and claiming a surface it cannot reach
			// sends someone chasing a 403 that is really a missing entitlement.
			if securityCloud {
				_, _ = fmt.Fprintln(w, "This tenant serves the gateway-served Jamf Security Cloud commands")
				_, _ = fmt.Fprintln(w, "(dns-*, ztna-*, content-categories, device-groups, uem-*).")
			} else {
				_, _ = fmt.Fprintln(w, "This tenant serves the Pro API and Platform API commands.")
				_, _ = fmt.Fprintln(w, "For Jamf Security Cloud, set up a second profile with its own tenant ID:")
				_, _ = fmt.Fprintln(w, "  jamf-cli platform setup --profile-name jsc")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&setupProfile, "profile-name", "", "profile name (default: \"platform\")")

	return cmd
}
