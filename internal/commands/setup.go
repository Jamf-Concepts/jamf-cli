package commands

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/ktn-jamf/jamfpro-cli/internal/auth"
	"github.com/ktn-jamf/jamfpro-cli/internal/config"
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

func (c *setupClient) do(ctx context.Context, method, path string, body interface{}) ([]byte, int, error) {
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
	defer resp.Body.Close()

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
	payload := map[string]interface{}{
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
	payload := map[string]interface{}{
		"displayName":         displayName,
		"authorizationScopes": scopes,
		"enabled":             true,
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

// filterPrivileges returns privileges matching any of the given prefixes.
func filterPrivileges(all []string, prefixes []string) []string {
	var result []string
	for _, p := range all {
		for _, prefix := range prefixes {
			if strings.HasPrefix(p, prefix) {
				result = append(result, p)
				break
			}
		}
	}
	return result
}

// scopePresets maps scope names to privilege prefixes. nil means all privileges.
var scopePresets = map[string][]string{
	"read-only":  {"Read "},
	"standard":   {"Read ", "Create ", "Update "},
	"full-admin": nil,
}

// normalizeURL ensures the URL has a scheme (defaults to https) and no trailing slash.
func normalizeURL(url string) string {
	url = strings.TrimRight(url, "/")
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}
	return url
}

func newConfigSetupCmd() *cobra.Command {
	var (
		setupURL     string
		setupUser    string
		setupPass    string
		setupScope   string
		setupProfile string
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Bootstrap OAuth2 credentials from username/password",
		Long: `Authenticates with a Jamf Pro admin account, creates an API role and
integration, generates OAuth2 client credentials, and saves them as a
config profile. The username and password are not stored.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			reader := bufio.NewReader(os.Stdin)

			// Gather inputs — prompt for anything not provided via flags
			if setupURL == "" {
				if noInput {
					return fmt.Errorf("--url is required when --no-input is set")
				}
				fmt.Fprint(cmd.OutOrStdout(), "Jamf Pro server URL: ")
				line, _ := reader.ReadString('\n')
				setupURL = strings.TrimSpace(line)
			}
			setupURL = normalizeURL(setupURL)

			if setupUser == "" {
				if noInput {
					return fmt.Errorf("--username is required when --no-input is set")
				}
				fmt.Fprint(cmd.OutOrStdout(), "Username: ")
				line, _ := reader.ReadString('\n')
				setupUser = strings.TrimSpace(line)
			}

			if setupPass == "" {
				if noInput {
					return fmt.Errorf("--password is required when --no-input is set")
				}
				fmt.Fprint(cmd.OutOrStdout(), "Password: ")
				passBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
				if err != nil {
					return fmt.Errorf("reading password: %w", err)
				}
				fmt.Fprintln(cmd.OutOrStdout()) // newline after hidden input
				setupPass = string(passBytes)
			}

			// Step 1: Authenticate with basic auth
			fmt.Fprint(cmd.OutOrStdout(), "\nAuthenticating... ")
			bearerToken, err := auth.BasicAuthExchange(ctx, setupURL, setupUser, setupPass)
			if err != nil {
				fmt.Fprintln(cmd.OutOrStdout(), "✗")
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "✓")

			client := newSetupClient(setupURL, bearerToken)

			// Step 2: Choose scope
			if setupScope == "" {
				if noInput {
					setupScope = "standard"
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "\nAPI scope:")
					fmt.Fprintln(cmd.OutOrStdout(), "  1. Read Only    — read access to all resources")
					fmt.Fprintln(cmd.OutOrStdout(), "  2. Standard     — read, create, and update (no deletes)")
					fmt.Fprintln(cmd.OutOrStdout(), "  3. Full Admin   — all privileges")
					fmt.Fprint(cmd.OutOrStdout(), "Choose [1-3] (default 2): ")
					line, _ := reader.ReadString('\n')
					choice := strings.TrimSpace(line)
					switch choice {
					case "1":
						setupScope = "read-only"
					case "3":
						setupScope = "full-admin"
					default:
						setupScope = "standard"
					}
				}
			}

			// Validate scope
			if _, ok := scopePresets[setupScope]; !ok {
				return fmt.Errorf("invalid --scope %q: must be read-only, standard, or full-admin", setupScope)
			}

			// Step 3: Fetch privileges and filter by scope
			allPrivileges, err := client.fetchPrivileges(ctx)
			if err != nil {
				return fmt.Errorf("fetching privileges: %w", err)
			}

			var rolePrivileges []string
			if prefixes := scopePresets[setupScope]; prefixes != nil {
				rolePrivileges = filterPrivileges(allPrivileges, prefixes)
			} else {
				rolePrivileges = allPrivileges
			}

			roleName := "jamfpro-cli-" + setupScope

			// Step 4: Create API role
			fmt.Fprintf(cmd.OutOrStdout(), "Creating API role %q... ", roleName)
			_, err = client.createAPIRole(ctx, roleName, rolePrivileges)
			if err != nil {
				fmt.Fprintln(cmd.OutOrStdout(), "✗")
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "✓")

			// Step 5: Create API integration
			integrationName := "jamfpro-cli"
			fmt.Fprintf(cmd.OutOrStdout(), "Creating API integration %q... ", integrationName)
			integrationID, err := client.createAPIIntegration(ctx, integrationName, []string{roleName})
			if err != nil {
				fmt.Fprintln(cmd.OutOrStdout(), "✗")
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "✓")

			// Step 6: Generate client credentials
			fmt.Fprint(cmd.OutOrStdout(), "Generating client credentials... ")
			clientID, clientSecret, err := client.generateClientCredentials(ctx, integrationID)
			if err != nil {
				fmt.Fprintln(cmd.OutOrStdout(), "✗")
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "✓")

			// Step 7: Save profile
			if setupProfile == "" {
				if noInput {
					setupProfile = "default"
				} else {
					fmt.Fprint(cmd.OutOrStdout(), "\nProfile name [default]: ")
					line, _ := reader.ReadString('\n')
					setupProfile = strings.TrimSpace(line)
					if setupProfile == "" {
						setupProfile = "default"
					}
				}
			}

			cfg, err := config.Load()
			if err != nil {
				return err
			}

			cfg.Profiles[setupProfile] = config.Profile{
				URL:          setupURL,
				AuthMethod:   "oauth2",
				ClientID:     clientID,
				ClientSecret: clientSecret,
			}
			cfg.DefaultProfile = setupProfile

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "\n✓ Profile %q saved and set as default.\n", setupProfile)
			fmt.Fprintf(cmd.OutOrStdout(), "  Client ID:     %s\n", clientID)
			fmt.Fprintf(cmd.OutOrStdout(), "  Client secret stored in %s\n", config.ConfigPath())
			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintln(cmd.OutOrStdout(), "  Note: For production use, consider moving the secret to an")
			fmt.Fprintln(cmd.OutOrStdout(), "  environment variable and updating the profile with:")
			fmt.Fprintf(cmd.OutOrStdout(), "    jamfpro-cli config add-profile %s --url %s --auth-method oauth2 --client-id %s --client-secret \"env:JAMF_CLIENT_SECRET\"\n", setupProfile, setupURL, clientID)

			return nil
		},
	}

	cmd.Flags().StringVar(&setupURL, "url", "", "Jamf Pro server URL")
	cmd.Flags().StringVar(&setupUser, "username", "", "admin username")
	cmd.Flags().StringVar(&setupPass, "password", "", "admin password (prompted if omitted)")
	cmd.Flags().StringVar(&setupScope, "scope", "", "API scope: read-only, standard, full-admin (default: standard)")
	cmd.Flags().StringVar(&setupProfile, "profile-name", "", "profile name (default: \"default\")")

	return cmd
}
