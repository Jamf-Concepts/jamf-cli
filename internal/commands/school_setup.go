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

func newSchoolSetupCmd() *cobra.Command {
	var (
		setupURL     string
		setupProfile string
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure a Jamf School profile with API credentials",
		Long: `Prompts for Jamf School URL, network ID, and API key, then saves them
as a config profile. Secrets are stored in the system keychain.

Find your network ID at Devices > Enroll Device(s) in your Jamf School
console. Generate an API key at Organization > Settings > API.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			reader := bufio.NewReader(os.Stdin)

			// Profile name
			if setupProfile == "" {
				if noInput {
					setupProfile = "school"
				} else {
					_, _ = fmt.Fprint(cmd.OutOrStdout(), "Profile name [school]: ")
					line, _ := reader.ReadString('\n')
					setupProfile = strings.TrimSpace(line)
					if setupProfile == "" {
						setupProfile = "school"
					}
				}
			}

			// URL
			if setupURL == "" {
				if noInput {
					return fmt.Errorf("--url is required when --no-input is set")
				}
				_, _ = fmt.Fprint(cmd.OutOrStdout(), "Jamf School URL (e.g. https://tenant.jamfschool.com): ")
				line, _ := reader.ReadString('\n')
				setupURL = strings.TrimSpace(line)
			}
			if setupURL == "" {
				return fmt.Errorf("URL is required")
			}
			setupURL = normalizeURL(setupURL)

			// Credentials are always collected interactively to prevent
			// exposure in shell history and process listings.
			if noInput {
				return fmt.Errorf("setup requires interactive input for credentials; cannot use --no-input")
			}

			// Network ID
			_, _ = fmt.Fprint(cmd.OutOrStdout(), "Network ID: ")
			line, _ := reader.ReadString('\n')
			networkID := strings.TrimSpace(line)
			if networkID == "" {
				return fmt.Errorf("network ID is required")
			}

			// API Key (secure input)
			_, _ = fmt.Fprint(cmd.OutOrStdout(), "API Key: ")
			keyBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
			if err != nil {
				return fmt.Errorf("reading API key: %w", err)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout()) // newline after hidden input
			apiKey := string(keyBytes)
			if apiKey == "" {
				return fmt.Errorf("API key is required")
			}

			// Store secrets in keychain
			store := config.GetKeychainStore()
			if err := store.Set(keychain.DefaultService, setupProfile+"/network-id", networkID); err != nil {
				return fmt.Errorf("failed to store network ID in keychain: %w", err)
			}
			if err := store.Set(keychain.DefaultService, setupProfile+"/api-key", apiKey); err != nil {
				return fmt.Errorf("failed to store API key in keychain: %w", err)
			}

			// Save profile to config
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			cfg.Profiles[setupProfile] = config.Profile{
				Product:    "school",
				URL:        setupURL,
				AuthMethod: "apikey",
				NetworkID:  keychain.KeychainRef(setupProfile, "network-id"),
				APIKey:     keychain.KeychainRef(setupProfile, "api-key"),
			}
			cfg.DefaultProfile = setupProfile

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\n✓ Profile %q saved and set as default.\n", setupProfile)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  Product:       Jamf School\n")
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  URL:           %s\n", setupURL)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  Network ID:    %s\n", networkID)
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  API key stored in system keychain")

			return nil
		},
	}

	cmd.Flags().StringVar(&setupURL, "url", "", "Jamf School URL")
	cmd.Flags().StringVar(&setupProfile, "profile-name", "", "profile name (default: \"school\")")

	return cmd
}
