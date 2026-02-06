package commands

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/ktn-jamf/jamfpro-cli/internal/config"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage CLI configuration and profiles",
	}

	cmd.AddCommand(newConfigShowCmd())
	cmd.AddCommand(newConfigPathCmd())
	cmd.AddCommand(newConfigAddProfileCmd())
	cmd.AddCommand(newConfigRemoveProfileCmd())
	cmd.AddCommand(newConfigSetDefaultCmd())
	cmd.AddCommand(newConfigSetupCmd())
	cmd.AddCommand(newConfigValidateCmd())

	return cmd
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show resolved configuration with sources",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "# Config file: %s\n", config.ConfigPath())

			data, err := yaml.Marshal(cfg)
			if err != nil {
				return fmt.Errorf("marshalling config for display: %w", err)
			}

			fmt.Fprint(cmd.OutOrStdout(), string(data))
			return nil
		},
	}
}

func newConfigPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print config file path",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), config.ConfigPath())
		},
	}
}

func newConfigAddProfileCmd() *cobra.Command {
	var (
		profileURL       string
		authMethod       string
		profileTok       string
		profileClientID  string
		profileClientSec string
	)

	cmd := &cobra.Command{
		Use:   "add-profile <name>",
		Short: "Add or update a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			// Validate auth-method
			validMethods := map[string]bool{"token": true, "basic": true, "oauth2": true}
			if !validMethods[authMethod] {
				return fmt.Errorf("invalid --auth-method %q: must be token, basic, or oauth2", authMethod)
			}

			// Validate auth-method-specific requirements
			if authMethod == "oauth2" && profileClientID == "" {
				return fmt.Errorf("--client-id is required when --auth-method is oauth2")
			}
			if authMethod == "oauth2" && profileClientSec == "" {
				return fmt.Errorf("--client-secret is required when --auth-method is oauth2")
			}

			cfg, err := config.Load()
			if err != nil {
				return err
			}

			p := config.Profile{
				URL:          profileURL,
				AuthMethod:   authMethod,
				Token:        profileTok,
				ClientID:     profileClientID,
				ClientSecret: profileClientSec,
			}

			cfg.Profiles[name] = p

			// Auto-set as default if this is the first profile
			if len(cfg.Profiles) == 1 {
				cfg.DefaultProfile = name
			}

			if err := config.Save(cfg); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Profile %q saved.\n", name)
			if cfg.DefaultProfile == name {
				fmt.Fprintf(cmd.OutOrStdout(), "Set as default profile.\n")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&profileURL, "url", "", "Jamf Pro server URL")
	cmd.Flags().StringVar(&authMethod, "auth-method", "token", "authentication method: token, basic, oauth2")
	cmd.Flags().StringVar(&profileTok, "token", "", "API token (literal, env:VAR, or file:/path)")
	cmd.Flags().StringVar(&profileClientID, "client-id", "", "OAuth2 client ID")
	cmd.Flags().StringVar(&profileClientSec, "client-secret", "", "OAuth2 client secret (literal, env:VAR, or file:/path)")

	_ = cmd.MarkFlagRequired("url")

	return cmd
}

func newConfigRemoveProfileCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove-profile <name>",
		Short: "Remove a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if _, ok := cfg.Profiles[name]; !ok {
				return fmt.Errorf("profile %q not found", name)
			}

			delete(cfg.Profiles, name)

			// Clear default if we just removed it
			if cfg.DefaultProfile == name {
				cfg.DefaultProfile = ""
			}

			if err := config.Save(cfg); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Profile %q removed.\n", name)
			return nil
		},
	}
}

func newConfigSetDefaultCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-default <name>",
		Short: "Set the default profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if _, ok := cfg.Profiles[name]; !ok {
				return fmt.Errorf("profile %q not found in config", name)
			}

			cfg.DefaultProfile = name

			if err := config.Save(cfg); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Default profile set to %q.\n", name)
			return nil
		},
	}
}

func newConfigValidateCmd() *cobra.Command {
	var connectivity bool

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate config file and profile settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			path := config.ConfigPath()
			hasErrors := false

			pass := func(msg string) { fmt.Fprintf(w, "  \u2713 %s\n", msg) }
			fail := func(msg string) { fmt.Fprintf(w, "  \u2717 %s\n", msg); hasErrors = true }

			fmt.Fprintf(w, "Config file: %s\n", path)

			// 1. File exists
			data, err := os.ReadFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					fail("File does not exist")
					return fmt.Errorf("config file not found at %s", path)
				}
				fail(fmt.Sprintf("Cannot read file: %v", err))
				return err
			}
			pass("File exists")

			// 2. Valid YAML
			var cfg config.Config
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				fail(fmt.Sprintf("Invalid YAML: %v", err))
				return fmt.Errorf("config file is not valid YAML")
			}
			pass("Valid YAML")

			if cfg.Profiles == nil {
				cfg.Profiles = make(map[string]config.Profile)
			}

			// 3. default-output valid
			if cfg.DefaultOutput != "" {
				validFormats := map[string]bool{
					"table": true, "json": true, "csv": true, "yaml": true, "plain": true,
				}
				if validFormats[cfg.DefaultOutput] {
					pass(fmt.Sprintf("Default output format: %s", cfg.DefaultOutput))
				} else {
					fail(fmt.Sprintf("Invalid default-output %q (must be table, json, csv, yaml, or plain)", cfg.DefaultOutput))
				}
			}

			// 4. default-profile references existing profile
			if cfg.DefaultProfile != "" {
				if _, ok := cfg.Profiles[cfg.DefaultProfile]; ok {
					pass(fmt.Sprintf("Default profile: %s", cfg.DefaultProfile))
				} else {
					fail(fmt.Sprintf("Default profile %q not found in profiles", cfg.DefaultProfile))
				}
			}

			// 5-7. Validate each profile
			names := make([]string, 0, len(cfg.Profiles))
			for name := range cfg.Profiles {
				names = append(names, name)
			}
			sort.Strings(names)

			validAuthMethods := map[string]bool{"token": true, "oauth2": true, "basic": true}

			for _, name := range names {
				p := cfg.Profiles[name]
				fmt.Fprintf(w, "\nProfile %q:\n", name)

				// URL
				if p.URL != "" {
					pass(fmt.Sprintf("URL: %s", p.URL))
				} else {
					fail("Missing url")
				}

				// Auth method
				authMethod := p.AuthMethod
				if authMethod == "" {
					authMethod = "token"
				}
				if validAuthMethods[authMethod] {
					pass(fmt.Sprintf("Auth method: %s", authMethod))
				} else {
					fail(fmt.Sprintf("Invalid auth-method %q", authMethod))
					continue
				}

				// Auth-method-specific fields
				switch authMethod {
				case "oauth2":
					if p.ClientID == "" {
						fail("Missing client-id")
					} else {
						if _, err := config.ResolveSecret(p.ClientID); err != nil {
							fail(fmt.Sprintf("client-id not resolvable: %v", err))
						} else {
							pass("client-id resolvable")
						}
					}
					if p.ClientSecret == "" {
						fail("Missing client-secret")
					} else {
						if _, err := config.ResolveSecret(p.ClientSecret); err != nil {
							fail(fmt.Sprintf("client-secret not resolvable: %v", err))
						} else {
							pass("client-secret resolvable")
						}
					}
				case "token":
					if p.Token == "" {
						fail("Missing token")
					} else {
						if _, err := config.ResolveSecret(p.Token); err != nil {
							fail(fmt.Sprintf("token not resolvable: %v", err))
						} else {
							pass("token resolvable")
						}
					}
				case "basic":
					if p.Username == "" {
						fail("Missing username")
					} else {
						pass("username set")
					}
				}

				// Optional connectivity check
				if connectivity && p.URL != "" {
					httpClient := &http.Client{Timeout: 10 * time.Second}
					req, err := http.NewRequest("HEAD", p.URL, nil)
					if err != nil {
						fail(fmt.Sprintf("Connectivity: invalid URL: %v", err))
					} else {
						resp, err := httpClient.Do(req)
						if err != nil {
							fail(fmt.Sprintf("Connectivity: %v", err))
						} else {
							resp.Body.Close()
							pass(fmt.Sprintf("Connectivity: reachable (HTTP %d)", resp.StatusCode))
						}
					}
				}
			}

			fmt.Fprintln(w)
			if hasErrors {
				fmt.Fprintln(w, "\u2717 Validation completed with errors.")
				return fmt.Errorf("config validation failed")
			}
			fmt.Fprintln(w, "\u2713 All checks passed.")
			return nil
		},
	}

	cmd.Flags().BoolVar(&connectivity, "connectivity", false, "test server reachability for each profile")

	return cmd
}
