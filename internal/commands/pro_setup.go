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

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
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
// Three pattern forms are supported:
//
//   - "*suffix"  — leading asterisk: matches any privilege ending with suffix,
//     e.g. "*Retry" matches "Jamf Connect Deployment Retry".
//
//   - "Verb "    — trailing space: matches by prefix ("Read Computers") AND by
//     lowercase verb suffix ("blueprints read"). The space prevents false matches
//     on partial words; the verb-suffix derives as " "+ToLower(TrimRight(p," ")).
//
//   - exact name — no asterisk, no trailing space: matched via HasPrefix since
//     HasPrefix(p, p) is always true.
//
// nil patterns returns an empty slice (full-admin is handled by the caller).
func filterPrivileges(all []string, patterns []string) []string {
	var result []string
	for _, p := range all {
		for _, pattern := range patterns {
			var matched bool
			if strings.HasPrefix(pattern, "*") {
				matched = strings.HasSuffix(p, pattern[1:])
			} else {
				verbSuffix := " " + strings.ToLower(strings.TrimRight(pattern, " "))
				matched = strings.HasPrefix(p, pattern) || strings.HasSuffix(p, verbSuffix)
			}
			if matched {
				result = append(result, p)
				break
			}
		}
	}
	return result
}

// scopeOption describes a selectable scope tier in the setup wizard.
// include == nil means start from all privileges (apply exclude if set).
// exclude == nil means no exclusions. Both nil = pass everything through (full-admin).
type scopeOption struct {
	key         string
	displayName string
	description string
	include     []string // allowlist patterns; nil = include all
	exclude     []string // denylist patterns applied after include; nil = exclude nothing
}

// defaultScope is used when the user presses Enter without choosing.
const defaultScope = "standard"

// scopeOptions defines the ordered list of tiers shown in the interactive
// prompt. To add a new tier, add one entry here — the prompt, validation,
// flag help, and privilege filtering all derive from this slice automatically.
var scopeOptions = []scopeOption{
	{
		key: "read-only", displayName: "Read Only", description: "read access to all resources",
		// "Read " and "View " match both "Read Computers" (prefix) and
		// "blueprints read" (verb suffix) via filterPrivileges.
		include: []string{"Read ", "View "},
	},
	{
		key: "standard", displayName: "Standard", description: "read, create, update — no deletes, wipes, or audit destruction",
		// Denylist: all privileges except destructive/irreversible operations.
		// "Delete " covers "Delete Computers" (prefix) and "blueprints delete" (verb suffix).
		// "Flush " covers "Flush MDM Commands" / "Flush Policy Logs" (destroys audit data).
		// "Dismiss " covers "Dismiss Notifications" (irreversible).
		// "*Remote Wipe Command" / "*Remote Lock Command" use the *suffix form to match
		// "Send Computer/Mobile Device Remote Wipe/Lock Command" without listing each variant.
		exclude: []string{"Delete ", "Flush ", "Dismiss ", "*Remote Wipe Command", "*Remote Lock Command"},
	},
	{
		key: "full-admin", displayName: "Full Admin", description: "all privileges",
		// Both nil: applyPrivilegeFilter returns all privileges unchanged.
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

// scopeOptionByKey returns the scopeOption whose key matches key.
// Returns the zero value if not found; callers should validate scope upstream.
func scopeOptionByKey(key string) scopeOption {
	for _, opt := range scopeOptions {
		if opt.key == key {
			return opt
		}
	}
	return scopeOption{}
}

// applyPrivilegeFilter applies a scope's include/exclude rules to all privileges.
// include == nil: start with all. include set: start with matching subset.
// exclude is then subtracted from the result. Both nil returns all unchanged.
func applyPrivilegeFilter(all []string, opt scopeOption) []string {
	base := all
	if opt.include != nil {
		base = filterPrivileges(all, opt.include)
	}
	if len(opt.exclude) == 0 {
		return base
	}
	excluded := make(map[string]bool, len(base))
	for _, p := range filterPrivileges(all, opt.exclude) {
		excluded[p] = true
	}
	result := make([]string, 0, len(base))
	for _, p := range base {
		if !excluded[p] {
			result = append(result, p)
		}
	}
	return result
}

// normalizeURL ensures the URL has an https scheme (added when absent) and no
// trailing slash. Returns an error for explicit http:// — credentials must not
// travel in plaintext.
func normalizeURL(rawURL string) (string, error) {
	rawURL = strings.TrimRight(rawURL, "/")
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	switch parsed.Scheme {
	case "https":
		return rawURL, nil
	case "http":
		return "", fmt.Errorf("http:// is not allowed; use https:// to protect credentials in transit")
	default:
		return "", fmt.Errorf("URL scheme %q is not supported; use https://", parsed.Scheme)
	}
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
		normalized, err := normalizeURL(line)
		if err != nil {
			return nil, fmt.Errorf("invalid URL %q: %w", line, err)
		}
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

	rolePrivileges := applyPrivilegeFilter(allPrivileges, scopeOptionByKey(scope))

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

	// Generate client credentials (skip if integration already existed and --rotate-credentials not set,
	// but only if both keychain items are actually present — if they were deleted the profile would be
	// saved with broken keychain references, so fall through to regenerate in that case).
	store := config.GetKeychainStore()
	var clientID string

	existingCID, cidErr := store.Get(keychain.DefaultService, profileName+"/client-id")
	_, csErr := store.Get(keychain.DefaultService, profileName+"/client-secret")
	keychainPresent := cidErr == nil && csErr == nil

	if existingIntID != 0 && !rotateCreds && keychainPresent {
		_, _ = fmt.Fprintln(w, "  Credentials unchanged (use --rotate-credentials to regenerate)")
		clientID = existingCID
	} else {
		if existingIntID != 0 && !keychainPresent {
			_, _ = fmt.Fprintln(w, "  Keychain credentials missing — regenerating...")
		}
		_, _ = fmt.Fprint(w, "  Generating client credentials... ")
		var clientSecret string
		clientID, clientSecret, err = client.generateClientCredentials(ctx, integrationID)
		if err != nil {
			_, _ = fmt.Fprintln(w, "✗")
			return err
		}
		_, _ = fmt.Fprintln(w, "✓")

		if err := store.Set(keychain.DefaultService, profileName+"/client-id", clientID); err != nil {
			return keychain.WriteError("client ID", err)
		}
		if err := store.Set(keychain.DefaultService, profileName+"/client-secret", clientSecret); err != nil {
			return keychain.WriteError("client secret", err)
		}
	}

	// Add profile to config (caller is responsible for saving)
	cfg.Profiles[profileName] = config.Profile{
		URL:          instanceURL,
		AuthMethod:   "oauth2",
		ClientID:     keychain.KeychainRef(profileName, "client-id"),
		ClientSecret: keychain.KeychainRef(profileName, "client-secret"),
	}

	// Clear any cached token and cookies for this profile so the next invocation
	// fetches a fresh token rather than potentially using a stale one from before setup.
	auth.ClearTokenCache(instanceURL, clientID)
	auth.ClearCookieCache(instanceURL, clientID)

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
		reportDir    string
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Bootstrap OAuth2 credentials from username/password",
		Long: `Authenticates with a Jamf Pro admin account, creates an API role and
integration, generates OAuth2 client credentials, and saves them as a
config profile. The username and password are not stored.

For multi-instance setup (e.g., MSPs), use --from-file with a file
containing one Jamf Pro URL per line. Profiles are auto-named
pro-<subdomain> (e.g., pro-school1 for school1.jamfcloud.com).

Setup also asks for an HTML report directory. It is a single global
setting rather than a per-profile one, and it is what the MCP server
writes reports into — the server names the file itself and will not
write anywhere else. Change it later with --report-dir.`,
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
				normalized, err := normalizeURL(u)
				if err != nil {
					return fmt.Errorf("invalid URL %q: %w", u, err)
				}
				urls[i] = normalized
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

			if scopeOptionByKey(setupScope).key == "" {
				return fmt.Errorf("invalid --scope %q: must be one of: %s", setupScope, validScopeNames())
			}

			// Load config once for all instances
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			// Report directory — a single global setting, so it is asked once
			// regardless of how many instances follow. Skipped when the config
			// already carries one, since re-running setup to add a profile
			// should not re-litigate it; pass --report-dir to change it.
			if reportDir == "" && cfg.ReportDir == "" {
				_, _ = fmt.Fprint(w, "\nHTML report directory (optional, press Enter to skip): ")
				line, _ := reader.ReadString('\n')
				reportDir = strings.TrimSpace(line)
			}
			if reportDir != "" {
				cfg.ReportDir = reportDir
				resolved := cfg.ReportDirPath()
				if err := os.MkdirAll(resolved, 0o700); err != nil {
					return fmt.Errorf("creating report directory %s: %w", resolved, err)
				}
				_, _ = fmt.Fprintf(w, "  ✓ Reports will be written to %s\n", resolved)
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
	cmd.Flags().StringVar(&reportDir, "report-dir", "", "directory HTML reports are written to (the MCP server writes only here)")
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
