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

			// 5. Validate. A rejected Security Cloud tenant warns and continues.
			if err := validatePlatformGatewayCredentials(cmd.Context(), w, creds); err != nil {
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
				URL:                   creds.GatewayURL,
				AuthMethod:            "platform",
				TenantID:              creds.TenantID,
				SecurityCloudTenantID: creds.SecurityCloudTenantID,
				ClientID:              clientIDRef,
				ClientSecret:          clientSecretRef,
			}
			cfg.DefaultProfile = setupProfile

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			_, _ = fmt.Fprintf(w, "\nProfile %q saved and set as default.\n", setupProfile)
			_, _ = fmt.Fprintf(w, "  Gateway:     %s\n", creds.GatewayURL)
			if creds.TenantID != "" {
				_, _ = fmt.Fprintf(w, "  Jamf Pro tenant ID:    %s\n", creds.TenantID)
			}
			if creds.SecurityCloudTenantID != "" {
				_, _ = fmt.Fprintf(w, "  Security Cloud tenant: %s\n", creds.SecurityCloudTenantID)
			}
			_, _ = fmt.Fprintf(w, "  Client ID:   %s\n", creds.ClientID)
			_, _ = fmt.Fprintln(w, "  Secrets stored in system keychain")
			_, _ = fmt.Fprintln(w)
			// State what the profile actually enables. A Security-Cloud-only
			// profile claiming "Pro API and Platform API commands" would send
			// someone chasing a 404 that is really a missing tenant ID.
			switch {
			case creds.TenantID != "" && creds.SecurityCloudTenantID != "":
				_, _ = fmt.Fprintln(w, "This profile enables Pro API and Platform API commands, and the")
				_, _ = fmt.Fprintln(w, "gateway-served Jamf Security Cloud commands (dns-*, ztna-*,")
				_, _ = fmt.Fprintln(w, "content-categories, device-groups, uem-*).")
			case creds.SecurityCloudTenantID != "":
				_, _ = fmt.Fprintln(w, "This profile enables the gateway-served Jamf Security Cloud commands")
				_, _ = fmt.Fprintln(w, "(dns-*, ztna-*, content-categories, device-groups, uem-*).")
				_, _ = fmt.Fprintln(w, "Add a Jamf Pro tenant ID to enable Pro API and Platform API commands.")
			default:
				_, _ = fmt.Fprintln(w, "This profile enables both Pro API and Platform API commands.")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&setupProfile, "profile-name", "", "profile name (default: \"platform\")")

	return cmd
}
