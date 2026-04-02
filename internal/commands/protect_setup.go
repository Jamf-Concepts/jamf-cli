// Copyright 2026, Jamf Software LLC

package commands

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/keychain"
)

func newProtectSetupCmd() *cobra.Command {
	var (
		setupURL     string
		setupCID     string
		setupSecret  string
		setupProfile string
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure a Jamf Protect profile with OAuth2 credentials",
		Long: `Prompts for Jamf Protect URL, client ID, and client secret, then saves
them as a config profile. Secrets are stored in the system keychain.

Create API client credentials in your Jamf Protect console under
Settings > API Clients before running this command.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			reader := bufio.NewReader(os.Stdin)

			// Profile name
			if setupProfile == "" {
				if noInput {
					setupProfile = "protect"
				} else {
					_, _ = fmt.Fprint(cmd.OutOrStdout(), "Profile name [protect]: ")
					line, _ := reader.ReadString('\n')
					setupProfile = strings.TrimSpace(line)
					if setupProfile == "" {
						setupProfile = "protect"
					}
				}
			}

			// URL
			if setupURL == "" {
				if noInput {
					return fmt.Errorf("--url is required when --no-input is set")
				}
				_, _ = fmt.Fprint(cmd.OutOrStdout(), "Jamf Protect URL (e.g. https://tenant.protect.jamfcloud.com): ")
				line, _ := reader.ReadString('\n')
				setupURL = strings.TrimSpace(line)
			}
			if setupURL == "" {
				return fmt.Errorf("URL is required")
			}
			setupURL = normalizeURL(setupURL)

			// Client ID
			if setupCID == "" {
				if noInput {
					return fmt.Errorf("--client-id is required when --no-input is set")
				}
				_, _ = fmt.Fprint(cmd.OutOrStdout(), "Client ID: ")
				line, _ := reader.ReadString('\n')
				setupCID = strings.TrimSpace(line)
			}
			if setupCID == "" {
				return fmt.Errorf("client ID is required")
			}

			// Client Secret (secure input)
			if setupSecret == "" {
				if noInput {
					return fmt.Errorf("--client-secret is required when --no-input is set")
				}
				_, _ = fmt.Fprint(cmd.OutOrStdout(), "Client Secret: ")
				secretBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
				if err != nil {
					return fmt.Errorf("reading client secret: %w", err)
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout()) // newline after hidden input
				setupSecret = string(secretBytes)
			}
			if setupSecret == "" {
				return fmt.Errorf("client secret is required")
			}

			// Store secrets in keychain
			store := config.GetKeychainStore()
			if err := store.Set(keychain.DefaultService, setupProfile+"/client-id", setupCID); err != nil {
				return fmt.Errorf("failed to store client ID in keychain: %w", err)
			}
			if err := store.Set(keychain.DefaultService, setupProfile+"/client-secret", setupSecret); err != nil {
				return fmt.Errorf("failed to store client secret in keychain: %w", err)
			}

			// Save profile to config
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			cfg.Profiles[setupProfile] = config.Profile{
				Product:      "protect",
				URL:          setupURL,
				AuthMethod:   "oauth2",
				ClientID:     keychain.KeychainRef(setupProfile, "client-id"),
				ClientSecret: keychain.KeychainRef(setupProfile, "client-secret"),
			}
			cfg.DefaultProfile = setupProfile

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\n✓ Profile %q saved and set as default.\n", setupProfile)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  Product:       Jamf Protect\n")
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  URL:           %s\n", setupURL)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  Client ID:     %s\n", setupCID)
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  Client secret stored in system keychain")

			return nil
		},
	}

	cmd.Flags().StringVar(&setupURL, "url", "", "Jamf Protect URL")
	cmd.Flags().StringVar(&setupCID, "client-id", "", "OAuth2 client ID")
	cmd.Flags().StringVar(&setupSecret, "client-secret", "", "OAuth2 client secret (omit to be prompted with hidden input)")
	cmd.Flags().StringVar(&setupProfile, "profile-name", "", "profile name (default: \"protect\")")

	return cmd
}
