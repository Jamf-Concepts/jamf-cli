// Copyright 2026, Jamf Software LLC

package commands

import (
	"bufio"
	"fmt"
	"io"
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
console. Generate an API key at Organization > Settings > API.

Optionally configure Platform API access for blueprint and DDM report
commands. This requires API client credentials from the Jamf Account
portal (account.jamf.com).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			reader := bufio.NewReader(os.Stdin)

			// Profile name
			if setupProfile == "" {
				if noInput {
					setupProfile = "school"
				} else {
					_, _ = fmt.Fprint(w, "Profile name [school]: ")
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
				_, _ = fmt.Fprint(w, "Jamf School URL (e.g. https://tenant.jamfschool.com): ")
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
			_, _ = fmt.Fprint(w, "Network ID: ")
			line, _ := reader.ReadString('\n')
			networkID := strings.TrimSpace(line)
			if networkID == "" {
				return fmt.Errorf("network ID is required")
			}

			// API Key (secure input)
			_, _ = fmt.Fprint(w, "API Key: ")
			keyBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
			if err != nil {
				return fmt.Errorf("reading API key: %w", err)
			}
			_, _ = fmt.Fprintln(w) // newline after hidden input
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

			// Build profile
			prof := config.Profile{
				Product:    "school",
				URL:        setupURL,
				AuthMethod: "apikey",
				NetworkID:  keychain.KeychainRef(setupProfile, "network-id"),
				APIKey:     keychain.KeychainRef(setupProfile, "api-key"),
			}

			// Optional: Platform API credentials for blueprints + DDM reports
			_, _ = fmt.Fprint(w, "\nConfigure Platform API access for blueprints? (y/n) [n]: ")
			line, _ = reader.ReadString('\n')
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y") {
				platProf, err := collectPlatformCredentials(w, reader, store, setupProfile)
				if err != nil {
					return err
				}
				prof.PlatformURL = platProf.PlatformURL
				prof.ClientID = platProf.ClientID
				prof.ClientSecret = platProf.ClientSecret
				prof.TenantID = platProf.TenantID
			}

			// Save profile to config
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			cfg.Profiles[setupProfile] = prof
			cfg.DefaultProfile = setupProfile

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			_, _ = fmt.Fprintf(w, "\nProfile %q saved and set as default.\n", setupProfile)
			_, _ = fmt.Fprintf(w, "  Product:       Jamf School\n")
			_, _ = fmt.Fprintf(w, "  URL:           %s\n", setupURL)
			_, _ = fmt.Fprintf(w, "  Network ID:    %s\n", networkID)
			_, _ = fmt.Fprintln(w, "  API key stored in system keychain")
			if prof.PlatformURL != "" {
				_, _ = fmt.Fprintf(w, "  Platform URL:  %s\n", prof.PlatformURL)
				_, _ = fmt.Fprintf(w, "  Tenant ID:     %s\n", prof.TenantID)
				_, _ = fmt.Fprintln(w, "  Platform credentials stored in system keychain")
				_, _ = fmt.Fprintln(w, "  Blueprints and DDM report commands are enabled.")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&setupURL, "url", "", "Jamf School URL")
	cmd.Flags().StringVar(&setupProfile, "profile-name", "", "profile name (default: \"school\")")

	return cmd
}

// collectPlatformCredentials prompts for platform gateway credentials and
// stores them in the keychain. Returns partial Profile fields to merge.
func collectPlatformCredentials(w io.Writer, reader *bufio.Reader, store keychain.Store, profileName string) (config.Profile, error) {
	var prof config.Profile

	// Region / gateway URL
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
		_, _ = fmt.Fprint(w, "Gateway URL: ")
		line, _ = reader.ReadString('\n')
		gatewayURL = strings.TrimSpace(line)
	}
	if gatewayURL == "" {
		return prof, fmt.Errorf("gateway URL is required")
	}
	prof.PlatformURL = normalizeURL(gatewayURL)

	// Tenant ID
	_, _ = fmt.Fprint(w, "Tenant ID: ")
	line, _ = reader.ReadString('\n')
	prof.TenantID = strings.TrimSpace(line)
	if prof.TenantID == "" {
		return prof, fmt.Errorf("tenant ID is required")
	}

	// Client ID
	_, _ = fmt.Fprint(w, "Client ID: ")
	line, _ = reader.ReadString('\n')
	clientID := strings.TrimSpace(line)
	if clientID == "" {
		return prof, fmt.Errorf("client ID is required")
	}

	// Client Secret
	_, _ = fmt.Fprint(w, "Client Secret: ")
	secretBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return prof, fmt.Errorf("reading client secret: %w", err)
	}
	_, _ = fmt.Fprintln(w)
	clientSecret := string(secretBytes)
	if clientSecret == "" {
		return prof, fmt.Errorf("client secret is required")
	}

	// Store in keychain
	if err := store.Set(keychain.DefaultService, profileName+"/client-id", clientID); err != nil {
		return prof, fmt.Errorf("failed to store client ID in keychain: %w", err)
	}
	if err := store.Set(keychain.DefaultService, profileName+"/client-secret", clientSecret); err != nil {
		return prof, fmt.Errorf("failed to store client secret in keychain: %w", err)
	}

	prof.ClientID = keychain.KeychainRef(profileName, "client-id")
	prof.ClientSecret = keychain.KeychainRef(profileName, "client-secret")

	return prof, nil
}
