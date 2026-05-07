// Copyright 2026, Jamf Software LLC

package commands

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/keychain"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
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
API client credentials, and tenant ID, validates them against the gateway,
and saves the profile. This profile enables both Pro API and Platform API
commands.

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

			// 2. Region / gateway URL
			_, _ = fmt.Fprintln(w, "\nPlatform gateway region:")
			for i, r := range platformGatewayRegions {
				_, _ = fmt.Fprintf(w, "  %d. %s (%s)\n", i+1, r.key, r.url)
			}
			_, _ = fmt.Fprintf(w, "  %d. Custom URL\n", len(platformGatewayRegions)+1)
			_, _ = fmt.Fprintf(w, "Choose [1-%d]: ", len(platformGatewayRegions)+1)

			line, _ := reader.ReadString('\n')
			choice := strings.TrimSpace(line)

			var gatewayURL string
			n := 0
			if _, err := fmt.Sscanf(choice, "%d", &n); err == nil && n >= 1 && n <= len(platformGatewayRegions) {
				gatewayURL = platformGatewayRegions[n-1].url
			} else {
				// Custom URL
				_, _ = fmt.Fprint(w, "Gateway URL: ")
				line, _ = reader.ReadString('\n')
				gatewayURL = strings.TrimSpace(line)
			}
			if gatewayURL == "" {
				return fmt.Errorf("gateway URL is required")
			}
			gatewayURL, err := normalizeURL(gatewayURL)
			if err != nil {
				return fmt.Errorf("invalid gateway URL: %w", err)
			}

			// 3. Client credentials (interactive only)
			_, _ = fmt.Fprint(w, "\nClient ID: ")
			line, _ = reader.ReadString('\n')
			clientID := strings.TrimSpace(line)
			if clientID == "" {
				return fmt.Errorf("client ID is required")
			}

			_, _ = fmt.Fprint(w, "Client Secret: ")
			secretBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
			if err != nil {
				return fmt.Errorf("reading client secret: %w", err)
			}
			_, _ = fmt.Fprintln(w)
			clientSecret := string(secretBytes)
			if clientSecret == "" {
				return fmt.Errorf("client secret is required")
			}

			// 4. Tenant ID
			_, _ = fmt.Fprint(w, "Tenant ID (from Jamf Account portal): ")
			line, _ = reader.ReadString('\n')
			tenantID := strings.TrimSpace(line)
			if tenantID == "" {
				return fmt.Errorf("tenant ID is required")
			}

			// 5. Validate credentials
			_, _ = fmt.Fprint(w, "\nValidating credentials... ")
			pc := jamfplatform.NewClient(
				gatewayURL, clientID, clientSecret,
				jamfplatform.WithTenantID(tenantID),
			)
			if err := pc.ValidateCredentials(context.Background()); err != nil {
				_, _ = fmt.Fprintln(w, "failed")
				return fmt.Errorf("credential validation failed: %w", err)
			}
			_, _ = fmt.Fprintln(w, "ok")

			// 6. Store secrets in keychain
			store := config.GetKeychainStore()
			if err := store.Set(keychain.DefaultService, setupProfile+"/client-id", clientID); err != nil {
				return fmt.Errorf("failed to store client ID in keychain: %w", err)
			}
			if err := store.Set(keychain.DefaultService, setupProfile+"/client-secret", clientSecret); err != nil {
				return fmt.Errorf("failed to store client secret in keychain: %w", err)
			}

			// 7. Save profile
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			cfg.Profiles[setupProfile] = config.Profile{
				URL:          gatewayURL,
				AuthMethod:   "platform",
				TenantID:     tenantID,
				ClientID:     keychain.KeychainRef(setupProfile, "client-id"),
				ClientSecret: keychain.KeychainRef(setupProfile, "client-secret"),
			}
			cfg.DefaultProfile = setupProfile

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			_, _ = fmt.Fprintf(w, "\nProfile %q saved and set as default.\n", setupProfile)
			_, _ = fmt.Fprintf(w, "  Gateway:     %s\n", gatewayURL)
			_, _ = fmt.Fprintf(w, "  Tenant ID:   %s\n", tenantID)
			_, _ = fmt.Fprintf(w, "  Client ID:   %s\n", clientID)
			_, _ = fmt.Fprintln(w, "  Secrets stored in system keychain")
			_, _ = fmt.Fprintln(w)
			_, _ = fmt.Fprintln(w, "This profile enables both Pro API and Platform API commands.")

			return nil
		},
	}

	cmd.Flags().StringVar(&setupProfile, "profile-name", "", "profile name (default: \"platform\")")

	return cmd
}
