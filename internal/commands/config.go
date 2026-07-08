// Copyright 2026, Jamf Software LLC

package commands

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/keychain"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newConfigCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage CLI configuration and profiles",
	}

	cmd.AddCommand(newConfigShowCmd(cliCtx))
	cmd.AddCommand(newConfigPathCmd())
	cmd.AddCommand(newConfigListCmd(cliCtx))
	cmd.AddCommand(newConfigAddProfileCmd())
	cmd.AddCommand(newConfigRemoveProfileCmd())
	cmd.AddCommand(newConfigSetDefaultCmd())
	cmd.AddCommand(newConfigValidateCmd(cliCtx))

	return cmd
}

// configProfileRow is the per-profile row shape emitted by `show` and `list`.
// Fields use JSON tags so the output formatter renders stable column names
// across table/json/yaml/csv.
type configProfileRow struct {
	Name         string `json:"name"`
	URL          string `json:"url"`
	AuthMethod   string `json:"auth-method"`
	TenantID     string `json:"tenant-id,omitempty"`
	Default      bool   `json:"default,omitempty"`
	Token        string `json:"token,omitempty"`
	ClientID     string `json:"client-id,omitempty"`
	ClientSecret string `json:"client-secret,omitempty"`
	Status       string `json:"status,omitempty"`
	Healthy      *bool  `json:"healthy,omitempty"`
}

// activeProfileName returns the profile currently in effect: flag > env > default.
func activeProfileName(cfg *config.Config) string {
	active := profile
	if active == "" {
		active = os.Getenv("JAMF_PROFILE")
	}
	if active == "" {
		active = cfg.DefaultProfile
	}
	return active
}

func newConfigShowCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show resolved configuration with sources",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			active := activeProfileName(cfg)
			names := sortedProfileNames(cfg)
			profiles := make([]configProfileRow, 0, len(names))
			for _, name := range names {
				p := cfg.Profiles[name]
				profiles = append(profiles, configProfileRow{
					Name:         name,
					URL:          p.URL,
					AuthMethod:   p.AuthMethod,
					TenantID:     p.TenantID,
					Default:      name == active,
					Token:        p.Token,
					ClientID:     p.ClientID,
					ClientSecret: p.ClientSecret,
				})
			}

			out := map[string]any{
				"config-file":     config.ConfigPath(),
				"default-profile": cfg.DefaultProfile,
				"default-output":  cfg.DefaultOutput,
				"profiles":        profiles,
			}

			data, err := json.Marshal(out)
			if err != nil {
				return fmt.Errorf("marshalling config for display: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

func newConfigPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print config file path",
		Run: func(cmd *cobra.Command, args []string) {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), config.ConfigPath())
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
	defer func() { _ = resp.Body.Close() }()

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

func newConfigListCmd(cliCtx *registry.CLIContext) *cobra.Command {
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
				// No rows to format — emit a structured empty list and a
				// helper hint to stderr for interactive users.
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "No profiles configured. Run: jamf-cli config add-profile <name> --url <url>")
				return cliCtx.Output.PrintRaw([]byte("[]"))
			}

			active := activeProfileName(cfg)
			names := sortedProfileNames(cfg)

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

			rows := make([]configProfileRow, 0, len(names))
			for _, name := range names {
				p := cfg.Profiles[name]
				row := configProfileRow{
					Name:       name,
					URL:        p.URL,
					AuthMethod: p.AuthMethod,
					TenantID:   p.TenantID,
					Default:    name == active,
				}
				if status {
					r := results[name]
					row.Status = r.Status
					healthy := r.Healthy
					row.Healthy = &healthy
				}
				rows = append(rows, row)
			}

			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling profiles: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
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
		profileTenantID  string
	)

	cmd := &cobra.Command{
		Use:   "add-profile <name>",
		Short: "Add or update a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			// Validate auth-method
			validMethods := map[string]bool{"token": true, "oauth2": true, "platform": true}
			if !validMethods[authMethod] {
				return fmt.Errorf("invalid --auth-method %q: must be token, oauth2, or platform", authMethod)
			}

			// Validate and normalize URL before prompting for credentials.
			normalizedURL, err := normalizeURL(profileURL)
			if err != nil {
				return fmt.Errorf("invalid --url: %w", err)
			}

			w := cmd.OutOrStdout()
			reader := bufio.NewReader(os.Stdin)

			// Credentials are always collected interactively to prevent
			// exposure in shell history and process listings.
			if noInput {
				return fmt.Errorf("add-profile requires interactive input for credentials; cannot use --no-input")
			}

			// oauth2 and platform both require client-id + client-secret
			if authMethod == "oauth2" || authMethod == "platform" {
				_, _ = fmt.Fprint(w, "Client ID: ")
				line, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("reading client ID: %w", err)
				}
				profileClientID = strings.TrimSpace(line)
				if profileClientID == "" {
					return fmt.Errorf("client ID is required")
				}

				_, _ = fmt.Fprint(w, "Client Secret: ")
				secretBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
				if err != nil {
					return fmt.Errorf("reading client secret: %w", err)
				}
				_, _ = fmt.Fprintln(w)
				profileClientSec = string(secretBytes)
				if profileClientSec == "" {
					return fmt.Errorf("client secret is required")
				}
			}

			// platform additionally requires tenant-id
			if authMethod == "platform" && profileTenantID == "" {
				_, _ = fmt.Fprint(w, "Tenant ID: ")
				line, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("reading tenant ID: %w", err)
				}
				profileTenantID = strings.TrimSpace(line)
				if profileTenantID == "" {
					return fmt.Errorf("tenant ID is required")
				}
			}

			// token auth requires a bearer token
			if authMethod == "token" {
				_, _ = fmt.Fprint(w, "API Token: ")
				tokenBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
				if err != nil {
					return fmt.Errorf("reading token: %w", err)
				}
				_, _ = fmt.Fprintln(w)
				profileTok = string(tokenBytes)
				if profileTok == "" {
					return fmt.Errorf("token is required")
				}
			}

			cfg, err := config.Load()
			if err != nil {
				return err
			}

			p := config.Profile{
				URL:        normalizedURL,
				AuthMethod: authMethod,
				TenantID:   profileTenantID,
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

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Profile %q saved.\n", name)
			if cfg.DefaultProfile == name {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Set as default profile.\n")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&profileURL, "url", "", "Jamf Pro server URL (instance URL or platform gateway URL)")
	cmd.Flags().StringVar(&authMethod, "auth-method", "token", "authentication method: token, oauth2, platform")
	cmd.Flags().StringVar(&profileTenantID, "tenant-id", "", "Jamf Pro tenant ID (required for platform auth)")
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
		return keychain.WriteError(field, err)
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

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Profile %q removed.\n", name)
			return nil
		},
	}
}

// cleanupKeychainRefs scans a profile's secret fields for keychain: prefixes
// and deletes the corresponding keychain items. Failures are warned but not fatal.
func cleanupKeychainRefs(cmd *cobra.Command, p config.Profile) {
	fields := map[string]string{
		"token":         p.Token,
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
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to remove keychain item %s/%s (%s): %v\n", service, account, field, err)
		} else {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed keychain item: %s/%s\n", service, account)
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

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Default profile set to %q.\n", name)
			return nil
		},
	}
}

// validateCheck is one line of validation output.
type validateCheck struct {
	Scope   string `json:"scope"` // "config" or profile name
	Name    string `json:"name"`
	Status  string `json:"status"` // "pass" or "fail"
	Message string `json:"message,omitempty"`
}

func newConfigValidateCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var connectivity bool

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate config file and profile settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := config.ConfigPath()
			var checks []validateCheck
			pass := func(scope, name string) {
				checks = append(checks, validateCheck{Scope: scope, Name: name, Status: "pass"})
			}
			fail := func(scope, name, msg string) {
				checks = append(checks, validateCheck{Scope: scope, Name: name, Status: "fail", Message: msg})
			}

			// 1. File exists
			data, err := os.ReadFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					fail("config", "file-exists", fmt.Sprintf("no file at %s", path))
					return emitValidateAndFail(cliCtx, checks, fmt.Errorf("config file not found at %s", path))
				}
				fail("config", "file-readable", err.Error())
				return emitValidateAndFail(cliCtx, checks, err)
			}
			pass("config", "file-exists")

			// 2. Valid YAML
			var cfg config.Config
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				fail("config", "valid-yaml", err.Error())
				return emitValidateAndFail(cliCtx, checks, fmt.Errorf("config file is not valid YAML"))
			}
			pass("config", "valid-yaml")

			if cfg.Profiles == nil {
				cfg.Profiles = make(map[string]config.Profile)
			}

			// 3. default-output valid
			if cfg.DefaultOutput != "" {
				validFormats := map[string]bool{
					"table": true, "json": true, "csv": true, "yaml": true, "plain": true,
				}
				if validFormats[cfg.DefaultOutput] {
					pass("config", "default-output")
				} else {
					fail("config", "default-output", fmt.Sprintf("invalid %q (must be table, json, csv, yaml, or plain)", cfg.DefaultOutput))
				}
			}

			// 4. default-profile references existing profile
			if cfg.DefaultProfile != "" {
				if _, ok := cfg.Profiles[cfg.DefaultProfile]; ok {
					pass("config", "default-profile")
				} else {
					fail("config", "default-profile", fmt.Sprintf("%q not found in profiles", cfg.DefaultProfile))
				}
			}

			// 5-7. Validate each profile
			validAuthMethods := map[string]bool{"token": true, "oauth2": true, "platform": true, "security": true}

			for _, name := range sortedProfileNames(&cfg) {
				p := cfg.Profiles[name]

				// URL — required for every product except Security, which has
				// one shared global host (api.wandera.com) and treats URL as an
				// optional override rather than a per-tenant address.
				if p.URL != "" {
					pass(name, "url")
				} else if p.Product != "security" {
					fail(name, "url", "missing")
				}

				// Auth method
				authMethod := p.AuthMethod
				if authMethod == "" {
					authMethod = "token"
				}
				if !validAuthMethods[authMethod] {
					fail(name, "auth-method", fmt.Sprintf("invalid %q", authMethod))
					continue
				}
				pass(name, "auth-method")

				// Auth-method-specific fields
				switch authMethod {
				case "platform":
					checkSecretField(&checks, name, "client-id", p.ClientID)
					checkSecretField(&checks, name, "client-secret", p.ClientSecret)
					if p.TenantID == "" {
						fail(name, "tenant-id", "missing")
					} else {
						pass(name, "tenant-id")
					}
				case "oauth2":
					checkSecretField(&checks, name, "client-id", p.ClientID)
					checkSecretField(&checks, name, "client-secret", p.ClientSecret)
				case "token":
					checkSecretField(&checks, name, "token", p.Token)
				case "security":
					// Any subset of the three Risk/Lifecycle/SSE credential pairs
					// may be configured; only require that at least one is.
					if p.RiskClientID == "" && p.LifecycleClientID == "" && p.SSEClientID == "" {
						fail(name, "credentials", "no risk-client-id, lifecycle-client-id, or sse-client-id configured")
					} else {
						pass(name, "credentials")
					}
				}

				// Optional connectivity check
				if connectivity && p.URL != "" {
					httpClient := &http.Client{Timeout: 10 * time.Second}
					req, err := http.NewRequestWithContext(cmd.Context(), "HEAD", p.URL, nil)
					if err != nil {
						fail(name, "connectivity", fmt.Sprintf("invalid URL: %v", err))
					} else {
						resp, err := httpClient.Do(req)
						if err != nil {
							fail(name, "connectivity", err.Error())
						} else {
							_ = resp.Body.Close()
							checks = append(checks, validateCheck{
								Scope:   name,
								Name:    "connectivity",
								Status:  "pass",
								Message: fmt.Sprintf("HTTP %d", resp.StatusCode),
							})
						}
					}
				}
			}

			if err := emitValidate(cliCtx, checks); err != nil {
				return err
			}
			for _, c := range checks {
				if c.Status == "fail" {
					return fmt.Errorf("config validation failed")
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&connectivity, "connectivity", false, "test server reachability for each profile")

	return cmd
}

// checkSecretField fails when the value is empty or not resolvable, otherwise passes.
func checkSecretField(checks *[]validateCheck, profileName, field, value string) {
	if value == "" {
		*checks = append(*checks, validateCheck{Scope: profileName, Name: field, Status: "fail", Message: "missing"})
		return
	}
	if _, err := config.ResolveSecret(value); err != nil {
		*checks = append(*checks, validateCheck{Scope: profileName, Name: field, Status: "fail", Message: fmt.Sprintf("not resolvable: %v", err)})
		return
	}
	*checks = append(*checks, validateCheck{Scope: profileName, Name: field, Status: "pass"})
}

func emitValidate(cliCtx *registry.CLIContext, checks []validateCheck) error {
	data, err := json.Marshal(checks)
	if err != nil {
		return fmt.Errorf("marshalling validation results: %w", err)
	}
	return cliCtx.Output.PrintRaw(data)
}

func emitValidateAndFail(cliCtx *registry.CLIContext, checks []validateCheck, cause error) error {
	if err := emitValidate(cliCtx, checks); err != nil {
		return err
	}
	return cause
}
