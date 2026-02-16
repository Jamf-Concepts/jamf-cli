package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/ktn-jamf/jamfpro-cli/internal/config"
	"github.com/ktn-jamf/jamfpro-cli/internal/keychain"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage CLI configuration and profiles",
	}

	cmd.AddCommand(newConfigShowCmd())
	cmd.AddCommand(newConfigPathCmd())
	cmd.AddCommand(newConfigListCmd())
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

// healthResult holds the outcome of a single health check.
type healthResult struct {
	Status  string
	Healthy bool
}

// checkHealth probes the Jamf Pro /healthCheck.html endpoint.
// Returns a status string and whether the instance is healthy.
func checkHealth(baseURL string) healthResult {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(strings.TrimRight(baseURL, "/") + "/healthCheck.html")
	if err != nil {
		return healthResult{"offline", false}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return healthResult{fmt.Sprintf("HTTP %d", resp.StatusCode), false}
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var checks []string
	if err := json.Unmarshal(body, &checks); err != nil {
		return healthResult{"unknown", false}
	}
	if len(checks) == 0 {
		return healthResult{"ok", true}
	}
	return healthResult{checks[0], false}
}

func newConfigListCmd() *cobra.Command {
	var status bool

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if len(cfg.Profiles) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No profiles configured. Run: jamfpro-cli config add-profile <name> --url <url>")
				return nil
			}

			// Determine active profile: flag > env > default
			active := profile
			if active == "" {
				active = os.Getenv("JAMF_PROFILE")
			}
			if active == "" {
				active = cfg.DefaultProfile
			}

			names := make([]string, 0, len(cfg.Profiles))
			for name := range cfg.Profiles {
				names = append(names, name)
			}
			sort.Strings(names)

			// Run health checks in parallel when --status is set
			var results map[string]healthResult
			if status {
				results = make(map[string]healthResult, len(names))
				var mu sync.Mutex
				var wg sync.WaitGroup
				for _, name := range names {
					wg.Add(1)
					go func(n string) {
						defer wg.Done()
						r := checkHealth(cfg.Profiles[n].URL)
						mu.Lock()
						results[n] = r
						mu.Unlock()
					}(name)
				}
				wg.Wait()
			}

			w := cmd.OutOrStdout()
			for _, name := range names {
				p := cfg.Profiles[name]
				marker := " "
				if name == active {
					marker = "*"
				}

				if !status {
					fmt.Fprintf(w, "  %s %-20s %-40s %s\n", marker, name, p.URL, p.AuthMethod)
					continue
				}

				r := results[name]
				var statusCol string
				if noColor {
					statusCol = r.Status
				} else if r.Healthy {
					statusCol = "\033[32m●\033[0m " + r.Status
				} else if r.Status == "offline" || strings.HasPrefix(r.Status, "HTTP") {
					statusCol = "\033[31m●\033[0m " + r.Status
				} else {
					statusCol = "\033[33m●\033[0m " + r.Status
				}
				fmt.Fprintf(w, "  %s %-20s %-40s %-8s %s\n", marker, name, p.URL, p.AuthMethod, statusCol)
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&status, "status", "s", false, "check instance health via /healthCheck.html")

	return cmd
}

func newConfigAddProfileCmd() *cobra.Command {
	var (
		profileURL       string
		authMethod       string
		profileTok       string
		profileClientID  string
		profileClientSec string
		touchID          bool
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
				URL:        profileURL,
				AuthMethod: authMethod,
				TouchID:    touchID,
			}

			// Store secrets: values with env: or file: prefix are written
			// directly to config; bare values go to the system keychain.
			store := config.GetKeychainStore()
			if err := storeOrRefSecret(store, name, "token", profileTok, &p.Token); err != nil {
				return err
			}
			if err := storeOrRefSecret(store, name, "client-id", profileClientID, &p.ClientID); err != nil {
				return err
			}
			if err := storeOrRefSecret(store, name, "client-secret", profileClientSec, &p.ClientSecret); err != nil {
				return err
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
	cmd.Flags().StringVar(&profileTok, "token", "", "API token (env:VAR, file:/path, or stored in keychain)")
	cmd.Flags().StringVar(&profileClientID, "client-id", "", "OAuth2 client ID")
	cmd.Flags().StringVar(&profileClientSec, "client-secret", "", "OAuth2 client secret (env:VAR, file:/path, or stored in keychain)")
	cmd.Flags().BoolVar(&touchID, "touch-id", false, "require Touch ID for keychain access (Phase 2, stored but not yet enforced)")

	_ = cmd.MarkFlagRequired("url")

	return cmd
}

// storeOrRefSecret writes a secret for the given field. If value has an env:
// or file: prefix it is stored directly as a reference. Otherwise the bare
// value is placed in the system keychain and a keychain: reference is written.
// Empty values are skipped.
func storeOrRefSecret(store keychain.Store, profile, field, value string, dest *string) error {
	if value == "" {
		return nil
	}
	if strings.HasPrefix(value, "env:") || strings.HasPrefix(value, "file:") {
		*dest = value
		return nil
	}
	account := profile + "/" + field
	if err := store.Set(keychain.DefaultService, account, value); err != nil {
		return fmt.Errorf("failed to store %s in keychain: %w", field, err)
	}
	*dest = keychain.KeychainRef(profile, field)
	return nil
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

			p, ok := cfg.Profiles[name]
			if !ok {
				return fmt.Errorf("profile %q not found", name)
			}

			// Clean up any keychain items referenced by this profile
			cleanupKeychainRefs(cmd, p)

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

// cleanupKeychainRefs scans a profile's secret fields for keychain: prefixes
// and deletes the corresponding keychain items. Failures are warned but not fatal.
func cleanupKeychainRefs(cmd *cobra.Command, p config.Profile) {
	fields := map[string]string{
		"token":         p.Token,
		"password":      p.Password,
		"client-id":     p.ClientID,
		"client-secret": p.ClientSecret,
	}

	store := keychain.New()
	for field, value := range fields {
		after, ok := strings.CutPrefix(value, "keychain:")
		if !ok {
			continue
		}
		service, account := keychain.ParseRef(after)
		if err := store.Delete(service, account); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to remove keychain item %s/%s (%s): %v\n", service, account, field, err)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Removed keychain item: %s/%s\n", service, account)
		}
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

				// Touch ID info
				if p.TouchID {
					fmt.Fprintf(w, "  ℹ touch-id is set; will be enforced in a future version\n")
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
