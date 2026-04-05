// Copyright 2026, Jamf Software LLC

package commands

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/keychain"
	"github.com/Jamf-Concepts/jamf-cli/internal/resolve"
)

// setupClient wraps a bearer token for making authenticated API calls during setup.
type setupClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func newSetupClient(baseURL, token string) *setupClient {
	return &setupClient{
		baseURL:    baseURL,
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *setupClient) do(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshalling request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB limit
	return respBody, resp.StatusCode, nil
}

// fetchPrivileges returns all available API role privileges from the Jamf Pro instance.
func (c *setupClient) fetchPrivileges(ctx context.Context) ([]string, error) {
	body, status, err := c.do(ctx, "GET", "/api/v1/api-role-privileges", nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("fetching privileges failed (HTTP %d): %s", status, string(body))
	}

	var result struct {
		Privileges []string `json:"privileges"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing privileges: %w", err)
	}
	return result.Privileges, nil
}

// createAPIRole creates an API role with the given display name and privileges.
// Returns the role ID.
func (c *setupClient) createAPIRole(ctx context.Context, displayName string, privileges []string) (string, error) {
	payload := map[string]any{
		"displayName": displayName,
		"privileges":  privileges,
	}

	body, status, err := c.do(ctx, "POST", "/api/v1/api-roles", payload)
	if err != nil {
		return "", err
	}

	if status == http.StatusOK || status == http.StatusCreated {
		var result struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return "", fmt.Errorf("parsing role response: %w", err)
		}
		return result.ID, nil
	}

	if status == http.StatusForbidden {
		return "", fmt.Errorf("your account lacks permission to create API roles")
	}

	return "", fmt.Errorf("creating API role failed (HTTP %d): %s", status, string(body))
}

// createAPIIntegration creates an API integration with the given display name and role scopes.
// Returns the integration ID.
func (c *setupClient) createAPIIntegration(ctx context.Context, displayName string, scopes []string) (int, error) {
	payload := map[string]any{
		"displayName":                displayName,
		"authorizationScopes":        scopes,
		"enabled":                    true,
		"accessTokenLifetimeSeconds": 300,
	}

	body, status, err := c.do(ctx, "POST", "/api/v1/api-integrations", payload)
	if err != nil {
		return 0, err
	}

	if status == http.StatusOK || status == http.StatusCreated {
		var result struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return 0, fmt.Errorf("parsing integration response: %w", err)
		}
		return result.ID, nil
	}

	if status == http.StatusForbidden {
		return 0, fmt.Errorf("your account lacks permission to create API integrations")
	}

	return 0, fmt.Errorf("creating API integration failed (HTTP %d): %s", status, string(body))
}

// generateClientCredentials generates new client credentials for the given integration ID.
// Returns clientID and clientSecret.
func (c *setupClient) generateClientCredentials(ctx context.Context, integrationID int) (string, string, error) {
	body, status, err := c.do(ctx, "POST", fmt.Sprintf("/api/v1/api-integrations/%d/client-credentials", integrationID), nil)
	if err != nil {
		return "", "", err
	}

	if status != http.StatusOK {
		return "", "", fmt.Errorf("generating credentials failed (HTTP %d): %s", status, string(body))
	}

	var result struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", fmt.Errorf("parsing credentials: %w", err)
	}
	return result.ClientID, result.ClientSecret, nil
}

// findAPIRoleByName searches for an API role by display name.
// Returns ("", nil) if not found, (id, nil) if exactly one match.
func (c *setupClient) findAPIRoleByName(ctx context.Context, displayName string) (string, error) {
	filter := url.QueryEscape(fmt.Sprintf(`displayName=="%s"`, resolve.EscapeRSQL(displayName)))
	path := "/api/v1/api-roles?page-size=2&filter=" + filter

	body, status, err := c.do(ctx, "GET", path, nil)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("searching for API role %q failed (HTTP %d): %s", displayName, status, string(body))
	}

	var result struct {
		TotalCount int `json:"totalCount"`
		Results    []struct {
			ID string `json:"id"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing role search response: %w", err)
	}

	switch result.TotalCount {
	case 0:
		return "", nil
	case 1:
		return result.Results[0].ID, nil
	default:
		return "", fmt.Errorf("multiple API roles named %q found (%d); remove duplicates before running setup", displayName, result.TotalCount)
	}
}

// updateAPIRole updates an existing API role's privileges.
func (c *setupClient) updateAPIRole(ctx context.Context, roleID, displayName string, privileges []string) error {
	payload := map[string]any{
		"displayName": displayName,
		"privileges":  privileges,
	}

	body, status, err := c.do(ctx, "PUT", "/api/v1/api-roles/"+roleID, payload)
	if err != nil {
		return err
	}
	if status == http.StatusOK {
		return nil
	}
	if status == http.StatusForbidden {
		return fmt.Errorf("your account lacks permission to update API roles")
	}
	return fmt.Errorf("updating API role failed (HTTP %d): %s", status, string(body))
}

// findAPIIntegrationByName searches for an API integration by display name.
// Returns (0, nil) if not found, (id, nil) if exactly one match.
func (c *setupClient) findAPIIntegrationByName(ctx context.Context, displayName string) (int, error) {
	filter := url.QueryEscape(fmt.Sprintf(`displayName=="%s"`, resolve.EscapeRSQL(displayName)))
	path := "/api/v1/api-integrations?page-size=2&filter=" + filter

	body, status, err := c.do(ctx, "GET", path, nil)
	if err != nil {
		return 0, err
	}
	if status != http.StatusOK {
		return 0, fmt.Errorf("searching for API integration %q failed (HTTP %d): %s", displayName, status, string(body))
	}

	var result struct {
		TotalCount int `json:"totalCount"`
		Results    []struct {
			ID int `json:"id"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("parsing integration search response: %w", err)
	}

	switch result.TotalCount {
	case 0:
		return 0, nil
	case 1:
		return result.Results[0].ID, nil
	default:
		return 0, fmt.Errorf("multiple API integrations named %q found (%d); remove duplicates before running setup", displayName, result.TotalCount)
	}
}

// updateAPIIntegration updates an existing API integration's authorization scopes.
func (c *setupClient) updateAPIIntegration(ctx context.Context, integrationID int, displayName string, scopes []string) error {
	payload := map[string]any{
		"displayName":                displayName,
		"authorizationScopes":        scopes,
		"enabled":                    true,
		"accessTokenLifetimeSeconds": 300,
	}

	body, status, err := c.do(ctx, "PUT", fmt.Sprintf("/api/v1/api-integrations/%d", integrationID), payload)
	if err != nil {
		return err
	}
	if status == http.StatusOK {
		return nil
	}
	if status == http.StatusForbidden {
		return fmt.Errorf("your account lacks permission to update API integrations")
	}
	return fmt.Errorf("updating API integration failed (HTTP %d): %s", status, string(body))
}

// filterPrivileges returns privileges from all that match any pattern.
// Two strategies are applied per pattern:
//   - Prefix: HasPrefix(p, pattern) — handles "Read Computers" style. Patterns
//     ending with a space (e.g. "Read ") prevent false matches on partial words.
//   - Verb suffix: for patterns ending with a space, the trimmed lowercase form
//     is also checked as a suffix — so "Read " matches "blueprints read" and
//     "compliance-benchmarks read", and any future platform-services privilege
//     whose name ends with " read". Derived as " "+ToLower(TrimRight(pattern," ")).
//
// Exact privilege names (no trailing space) match via prefix since
// HasPrefix(p, p) is always true. nil patterns returns an empty slice.
func filterPrivileges(all []string, patterns []string) []string {
	var result []string
	for _, p := range all {
		for _, pattern := range patterns {
			verbSuffix := " " + strings.ToLower(strings.TrimRight(pattern, " "))
			if strings.HasPrefix(p, pattern) || strings.HasSuffix(p, verbSuffix) {
				result = append(result, p)
				break
			}
		}
	}
	return result
}

// scopeOption describes a selectable scope tier in the setup wizard.
type scopeOption struct {
	key         string
	displayName string
	description string
	patterns    []string // privilege patterns for filterPrivileges; nil = all (full-admin)
}

// defaultScope is used when the user presses Enter without choosing.
const defaultScope = "standard"

// scopeOptions defines the ordered list of tiers shown in the interactive
// prompt. To add a new tier, add one entry here — the prompt, validation,
// flag help, and privilege filtering all derive from this slice automatically.
var scopeOptions = []scopeOption{
	{
		key: "read-only", displayName: "Read Only", description: "read access to all resources",
		// "Read " and "View " catch both the "Read Computers" prefix form and
		// the "blueprints read" / "compliance-benchmarks read" suffix form via
		// filterPrivileges' verb-suffix matching.
		patterns: []string{"Read ", "View "},
	},
	{
		key: "standard", displayName: "Standard", description: "read, create, and update (no deletes)",
		// "Read ", "Create ", "Update " also match Platform Services privileges
		// via verb-suffix (e.g. "blueprints create", "compliance-benchmarks update").
		// Excludes erase, wipe, lock, and remote wipe.
		patterns: []string{
			"Read ",
			"View ",
			"Create ",
			"Update ",
			// Operational privileges not covered by prefix/suffix patterns above
			"Allow User to Enroll",
			"Assign Users to Computers",
			"Assign Users to Mobile Devices",
			"Dismiss Notifications",
			"Edit Return To Service Configurations", // "Edit" not "Update"
			"Enroll Computers",
			"Enroll Mobile Devices",
			"Flush MDM Commands",
			"Flush Policy Logs",
			"Jamf Connect Deployment Retry",
			"Jamf Packages Action",
			"Jamf Protect Deployment Retry",
			// Non-destructive MDM send commands
			"Send Blank Pushes to Mobile Devices",
			"Send Command to Renew MDM Profile",
			"Send Declarative Management Command",
			"Send Device Information Command",
			"Send Inventory Requests to Mobile Devices",
			"Send MDM Check In Command",
		},
	},
	{
		key: "full-admin", displayName: "Full Admin", description: "all privileges",
		patterns: nil, // caller passes the full live privilege list unchanged
	},
}

// validScopeNames returns a comma-separated list of valid scope keys derived
// from scopeOptions — used in error messages and flag help text.
func validScopeNames() string {
	names := make([]string, len(scopeOptions))
	for i, opt := range scopeOptions {
		names[i] = opt.key
	}
	return strings.Join(names, ", ")
}

// scopePresets maps scope keys to privilege patterns for filterPrivileges.
// Derived from scopeOptions — do not edit directly; edit scopeOptions instead.
var scopePresets = func() map[string][]string {
	m := make(map[string][]string, len(scopeOptions))
	for _, opt := range scopeOptions {
		m[opt.key] = opt.patterns
	}
	return m
}()

// normalizeURL ensures the URL has a scheme (defaults to https) and no trailing slash.
func normalizeURL(rawURL string) string {
	rawURL = strings.TrimRight(rawURL, "/")
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}
	return rawURL
}

// extractSubdomain returns the hostname portion before the first dot.
// For "https://nmartin.jamfcloud.com" → "nmartin".
// Falls back to the full hostname if there are no dots.
func extractSubdomain(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		// Try adding a scheme and re-parsing
		parsed, err = url.Parse("https://" + rawURL)
		if err != nil || parsed.Host == "" {
			return rawURL
		}
	}
	host := parsed.Hostname() // strips port
	if idx := strings.Index(host, "."); idx > 0 {
		return host[:idx]
	}
	return host
}

// readURLsFromFile reads Jamf Pro URLs from a file, one per line.
// Blank lines and lines starting with # are ignored. Duplicate URLs are removed.
func readURLsFromFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening URL file: %w", err)
	}
	defer func() { _ = f.Close() }()

	seen := make(map[string]bool)
	var urls []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		normalized := normalizeURL(line)
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		urls = append(urls, normalized)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading URL file: %w", err)
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("no URLs found in %s", path)
	}
	return urls, nil
}

// setupInstance runs the full setup flow for a single Jamf Pro instance:
// authenticate, create/update role + integration, generate credentials,
// store secrets in keychain, and add the profile to cfg (caller saves).
func setupInstance(ctx context.Context, w io.Writer, cfg *config.Config, instanceURL, username, password, scope, profileName string, rotateCreds bool) error {
	// Authenticate
	_, _ = fmt.Fprintf(w, "  Authenticating... ")
	bearerToken, err := basicAuthExchange(ctx, instanceURL, username, password)
	if err != nil {
		_, _ = fmt.Fprintln(w, "✗")
		return err
	}
	_, _ = fmt.Fprintln(w, "✓")

	client := newSetupClient(instanceURL, bearerToken)

	// Fetch privileges and filter by scope
	allPrivileges, err := client.fetchPrivileges(ctx)
	if err != nil {
		return fmt.Errorf("fetching privileges: %w", err)
	}

	var rolePrivileges []string
	if prefixes := scopePresets[scope]; prefixes != nil {
		rolePrivileges = filterPrivileges(allPrivileges, prefixes)
	} else {
		rolePrivileges = allPrivileges
	}

	roleName := "jamf-cli-" + scope

	// Ensure API role exists (create or update)
	existingRoleID, err := client.findAPIRoleByName(ctx, roleName)
	if err != nil {
		return err
	}

	if existingRoleID != "" {
		_, _ = fmt.Fprintf(w, "  Updating existing API role %q... ", roleName)
		err = client.updateAPIRole(ctx, existingRoleID, roleName, rolePrivileges)
	} else {
		_, _ = fmt.Fprintf(w, "  Creating API role %q... ", roleName)
		_, err = client.createAPIRole(ctx, roleName, rolePrivileges)
	}
	if err != nil {
		_, _ = fmt.Fprintln(w, "✗")
		return err
	}
	_, _ = fmt.Fprintln(w, "✓")

	// Ensure API integration exists (create or update)
	integrationName := fmt.Sprintf("jamf-cli [%s]", username)
	existingIntID, err := client.findAPIIntegrationByName(ctx, integrationName)
	if err != nil {
		return err
	}

	var integrationID int
	if existingIntID != 0 {
		_, _ = fmt.Fprintf(w, "  Updating existing API integration %q... ", integrationName)
		err = client.updateAPIIntegration(ctx, existingIntID, integrationName, []string{roleName})
		integrationID = existingIntID
	} else {
		_, _ = fmt.Fprintf(w, "  Creating API integration %q... ", integrationName)
		integrationID, err = client.createAPIIntegration(ctx, integrationName, []string{roleName})
	}
	if err != nil {
		_, _ = fmt.Fprintln(w, "✗")
		return err
	}
	_, _ = fmt.Fprintln(w, "✓")

	// Generate client credentials (skip if integration already existed and --rotate-credentials not set)
	store := config.GetKeychainStore()
	var clientID string

	if existingIntID != 0 && !rotateCreds {
		_, _ = fmt.Fprintln(w, "  Credentials unchanged (use --rotate-credentials to regenerate)")
		// Retrieve existing client ID for the status message
		if cid, err := store.Get(keychain.DefaultService, profileName+"/client-id"); err == nil {
			clientID = cid
		}
	} else {
		_, _ = fmt.Fprint(w, "  Generating client credentials... ")
		var clientSecret string
		clientID, clientSecret, err = client.generateClientCredentials(ctx, integrationID)
		if err != nil {
			_, _ = fmt.Fprintln(w, "✗")
			return err
		}
		_, _ = fmt.Fprintln(w, "✓")

		if err := store.Set(keychain.DefaultService, profileName+"/client-id", clientID); err != nil {
			return fmt.Errorf("failed to store client ID in keychain: %w", err)
		}
		if err := store.Set(keychain.DefaultService, profileName+"/client-secret", clientSecret); err != nil {
			return fmt.Errorf("failed to store client secret in keychain: %w", err)
		}
	}

	// Add profile to config (caller is responsible for saving)
	cfg.Profiles[profileName] = config.Profile{
		URL:          instanceURL,
		AuthMethod:   "oauth2",
		ClientID:     keychain.KeychainRef(profileName, "client-id"),
		ClientSecret: keychain.KeychainRef(profileName, "client-secret"),
	}

	if clientID != "" {
		_, _ = fmt.Fprintf(w, "  ✓ Profile %q ready (client ID: %s)\n", profileName, clientID)
	} else {
		_, _ = fmt.Fprintf(w, "  ✓ Profile %q ready\n", profileName)
	}
	return nil
}

func newConfigSetupCmd() *cobra.Command {
	var (
		setupURL     string
		setupUser    string // populated by interactive prompt only
		setupPass    string // populated by interactive prompt only
		setupScope   string
		setupProfile string
		fromFile     string
		rotateCreds  bool
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Bootstrap OAuth2 credentials from username/password",
		Long: `Authenticates with a Jamf Pro admin account, creates an API role and
integration, generates OAuth2 client credentials, and saves them as a
config profile. The username and password are not stored.

For multi-instance setup (e.g., MSPs), use --from-file with a file
containing one Jamf Pro URL per line. Profiles are auto-named
pro-<subdomain> (e.g., pro-school1 for school1.jamfcloud.com).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := cmd.OutOrStdout()
			reader := bufio.NewReader(os.Stdin)

			// Determine URLs to process
			var urls []string
			if fromFile != "" {
				var err error
				urls, err = readURLsFromFile(fromFile)
				if err != nil {
					return err
				}
			} else {
				if setupURL == "" {
					if noInput {
						return fmt.Errorf("--url is required when --no-input is set")
					}
					_, _ = fmt.Fprint(w, "Jamf Pro server URL: ")
					line, _ := reader.ReadString('\n')
					setupURL = strings.TrimSpace(line)
				}
				urls = []string{setupURL}
			}

			// Normalize all URLs
			for i, u := range urls {
				urls[i] = normalizeURL(u)
			}

			// Gather credentials interactively — once for all instances.
			// Username and password are never accepted via flags or env vars
			// to prevent exposure in shell history and process listings.
			if noInput {
				return fmt.Errorf("setup requires interactive input for credentials; cannot use --no-input")
			}
			_, _ = fmt.Fprint(w, "Username: ")
			line, _ := reader.ReadString('\n')
			setupUser = strings.TrimSpace(line)

			_, _ = fmt.Fprint(w, "Password: ")
			passBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
			if err != nil {
				return fmt.Errorf("reading password: %w", err)
			}
			_, _ = fmt.Fprintln(w) // newline after hidden input
			setupPass = string(passBytes)

			// Choose scope — once for all instances
			if setupScope == "" {
				if noInput {
					setupScope = defaultScope
				} else {
					_, _ = fmt.Fprintln(w, "\nAPI scope:")
					for i, opt := range scopeOptions {
						marker := ""
						if opt.key == defaultScope {
							marker = " (default)"
						}
						_, _ = fmt.Fprintf(w, "  %d. %-12s — %s%s\n", i+1, opt.displayName, opt.description, marker)
					}
					_, _ = fmt.Fprintf(w, "Choose [1-%d]: ", len(scopeOptions))
					line, _ := reader.ReadString('\n')
					choice := strings.TrimSpace(line)

					setupScope = defaultScope
					if n, err := strconv.Atoi(choice); err == nil && n >= 1 && n <= len(scopeOptions) {
						setupScope = scopeOptions[n-1].key
					}
				}
			}

			if _, ok := scopePresets[setupScope]; !ok {
				return fmt.Errorf("invalid --scope %q: must be one of: %s", setupScope, validScopeNames())
			}

			// Load config once for all instances
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			multiInstance := len(urls) > 1 || fromFile != ""

			// Single-instance mode: prompt for profile name, set as default
			if !multiInstance {
				if setupProfile == "" {
					if noInput {
						setupProfile = "default"
					} else {
						_, _ = fmt.Fprint(w, "\nProfile name [default]: ")
						line, _ := reader.ReadString('\n')
						setupProfile = strings.TrimSpace(line)
						if setupProfile == "" {
							setupProfile = "default"
						}
					}
				}

				_, _ = fmt.Fprintf(w, "\n── %s ──\n", urls[0])
				if err := setupInstance(ctx, w, cfg, urls[0], setupUser, setupPass, setupScope, setupProfile, rotateCreds); err != nil {
					return err
				}

				cfg.DefaultProfile = setupProfile
				if err := config.Save(cfg); err != nil {
					return fmt.Errorf("saving config: %w", err)
				}
				_, _ = fmt.Fprintf(w, "\nProfile %q set as default.\n", setupProfile)
				return nil
			}

			// Multi-instance mode: auto-name profiles, continue on failure
			_, _ = fmt.Fprintf(w, "\nSetting up %d instance(s) with scope %q...\n", len(urls), setupScope)

			var succeeded, failed int
			var failures []string

			for _, instanceURL := range urls {
				profileName := "pro-" + extractSubdomain(instanceURL)
				_, _ = fmt.Fprintf(w, "\n── %s → profile %q ──\n", instanceURL, profileName)

				if err := setupInstance(ctx, w, cfg, instanceURL, setupUser, setupPass, setupScope, profileName, rotateCreds); err != nil {
					_, _ = fmt.Fprintf(w, "  ✗ FAILED: %v\n", err)
					failures = append(failures, fmt.Sprintf("%s: %v", instanceURL, err))
					failed++
				} else {
					succeeded++
				}
			}

			// Save config once after all instances
			if succeeded > 0 {
				if err := config.Save(cfg); err != nil {
					return fmt.Errorf("saving config: %w", err)
				}
			}

			// Summary
			_, _ = fmt.Fprintf(w, "\n── Summary ──\n")
			_, _ = fmt.Fprintf(w, "  Succeeded: %d\n", succeeded)
			if failed > 0 {
				_, _ = fmt.Fprintf(w, "  Failed:    %d\n", failed)
				for _, f := range failures {
					_, _ = fmt.Fprintf(w, "    - %s\n", f)
				}
				return fmt.Errorf("%d of %d instance(s) failed", failed, len(urls))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&setupURL, "url", "", "Jamf Pro server URL")
	cmd.Flags().StringVar(&fromFile, "from-file", "", "file containing one Jamf Pro URL per line (for multi-instance setup)")
	cmd.Flags().StringVar(&setupScope, "scope", "", fmt.Sprintf("API scope: %s (default: %s)", validScopeNames(), defaultScope))
	cmd.Flags().StringVar(&setupProfile, "profile-name", "", "profile name (default: \"default\"; ignored with --from-file)")
	cmd.Flags().BoolVar(&rotateCreds, "rotate-credentials", false, "regenerate client credentials for existing integrations")
	cmd.MarkFlagsMutuallyExclusive("url", "from-file")

	return cmd
}

// basicAuthExchange performs a one-shot basic auth token exchange during setup.
// Returns a bearer token. The username/password are not stored.
func basicAuthExchange(ctx context.Context, baseURL, username, password string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/api/v1/auth/token", nil)
	if err != nil {
		return "", fmt.Errorf("creating auth request: %w", err)
	}
	req.SetBasicAuth(username, password)

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("cannot reach server at %s: %w", baseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("invalid username or password")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("auth failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Token   string `json:"token"`
		Expires string `json:"expires"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parsing auth response: %w", err)
	}
	if result.Token == "" {
		return "", fmt.Errorf("basic auth exchange returned empty token, check that your account is not disabled or locked")
	}
	return result.Token, nil
}
