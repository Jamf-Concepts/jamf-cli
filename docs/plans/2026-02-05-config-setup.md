# `config setup` — OAuth2 Bootstrap Command

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a `config setup` command that authenticates with username/password, creates an API role + integration in Jamf Pro, generates OAuth2 client credentials, and saves them as a config profile — so users go from zero to working CLI in one command.

**Architecture:** Interactive command with flag overrides. Makes 4 sequential API calls: basic auth → create role → create integration → generate credentials. Password is never stored. Privileges are fetched at runtime from the Jamf Pro instance.

**Tech Stack:** Go stdlib (`net/http`, `encoding/json`, `bufio`, `os`, `strings`, `fmt`), plus `golang.org/x/term` for hidden password input.

---

### Task 1: Implement BasicAuthProvider and setup API helpers

**Files:**
- Modify: `internal/auth/auth.go`
- Create: `internal/commands/setup.go`

**Step 1: Add BasicAuthProvider to auth.go**

The existing `BasicProvider` is stubbed. We don't need to change it — we need a simpler helper that does a one-shot basic auth token exchange. Add this to `internal/auth/auth.go`:

```go
// BasicAuthExchange performs a one-shot basic auth token exchange.
// It returns a bearer token. The username/password are not stored.
func BasicAuthExchange(ctx context.Context, baseURL, username, password string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/api/v1/auth/token", nil)
	if err != nil {
		return "", fmt.Errorf("creating auth request: %w", err)
	}
	req.SetBasicAuth(username, password)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("cannot reach server at %s: %w", baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("invalid username or password")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("auth failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Token   string `json:"token"`
		Expires string `json:"expires"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parsing auth response: %w", err)
	}
	return result.Token, nil
}
```

**Step 2: Create setup.go with the API helper functions**

Create `internal/commands/setup.go` with the scaffolding and API helpers:

```go
package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// setupClient wraps a bearer token for making authenticated API calls during setup.
type setupClient struct {
	baseURL string
	token   string
}

func (c *setupClient) do(ctx context.Context, method, path string, body interface{}) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshalling request: %w", err)
		}
		reqBody = strings.NewReader(string(data))
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
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
// Returns the role ID. If a role with the same name already exists, returns its ID.
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
```

**Step 3: Build**

```bash
cd /Users/keaton.svoma/Projects/jamfpro-cli && go build ./...
```

**Step 4: Commit**

```bash
git add internal/auth/auth.go internal/commands/setup.go
git commit -m "feat: add BasicAuthExchange and setup API helpers

- One-shot basic auth token exchange for bootstrapping
- setupClient with helpers for API roles, integrations, credentials
- filterPrivileges for scope-based privilege selection

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 2: Implement the `config setup` command with interactive prompts

**Files:**
- Modify: `internal/commands/setup.go`
- Modify: `internal/commands/config.go`

**Step 1: Add `golang.org/x/term` dependency**

```bash
go get golang.org/x/term
```

**Step 2: Add the newConfigSetupCmd function to setup.go**

Add to `internal/commands/setup.go`:

```go
import (
	"bufio"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/ktn-jamf/jamfpro-cli/internal/auth"
	"github.com/ktn-jamf/jamfpro-cli/internal/config"
)

var scopePresets = map[string][]string{
	"read-only": {"Read "},
	"standard":  {"Read ", "Create ", "Update "},
	"full-admin": nil, // nil means all privileges
}

func newConfigSetupCmd() *cobra.Command {
	var (
		setupURL      string
		setupUser     string
		setupPass     string
		setupScope    string
		setupProfile  string
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
			setupURL = strings.TrimRight(setupURL, "/")

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

			client := &setupClient{baseURL: setupURL, token: bearerToken}

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
			if _, ok := scopePresets[setupScope]; !ok && setupScope != "full-admin" {
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
				rolePrivileges = allPrivileges // full-admin
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
```

**Step 3: Register the command in config.go**

In `newConfigCmd()`, add:

```go
cmd.AddCommand(newConfigSetupCmd())
```

**Step 4: Build and verify help**

```bash
go build ./...
go run ./cmd/jamfpro-cli config setup --help
```

**Step 5: Commit**

```bash
git add internal/commands/setup.go internal/commands/config.go go.mod go.sum
git commit -m "feat: add config setup command for OAuth2 bootstrapping

- Interactive prompts with flag overrides for scripting
- Three scope presets: read-only, standard, full-admin
- Fetches privileges at runtime from Jamf Pro instance
- Creates API role + integration + client credentials
- Saves OAuth2 profile and sets as default
- Hidden password input via golang.org/x/term
- Respects --no-input flag for CI/CD usage

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 3: Integration test against local Jamf Pro

**Files:**
- No new files — verification only

**Step 1: Run all unit tests**

```bash
go test ./... -v
```

**Step 2: Test interactive flow against local instance**

```bash
# Interactive setup
echo -e "http://localhost:8080\nadmin\njamf123456\n2\ntest-setup" | go run ./cmd/jamfpro-cli config setup
```

Note: The hidden password prompt via `term.ReadPassword` won't work with piped input. For the interactive test, run it manually. For CI testing, use flags:

```bash
go run ./cmd/jamfpro-cli config setup \
  --url http://localhost:8080 \
  --username admin \
  --password jamf123456 \
  --scope standard \
  --profile-name test-setup
```

**Step 3: Verify the profile was saved**

```bash
go run ./cmd/jamfpro-cli config show
```

**Step 4: Verify the credentials work end-to-end**

```bash
# Use the profile we just created
go run ./cmd/jamfpro-cli buildings list --profile test-setup
```

Expected: Either a successful response or a 404 (buildings not configured), but NOT an auth error.

**Step 5: Clean up**

```bash
go run ./cmd/jamfpro-cli config remove-profile test-setup
```

Clean up the API integration and role from Jamf Pro:

```bash
TOKEN=$(curl -s -u admin:jamf123456 -X POST http://localhost:8080/api/v1/auth/token | python3 -c "import sys,json;print(json.load(sys.stdin)['token'])")
# List integrations to find the ID
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/api-integrations | python3 -m json.tool
# Delete by ID (adjust ID as needed)
curl -s -X DELETE -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/api-integrations/{id}
curl -s -X DELETE -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/api-roles/{id}
```

**Step 6: Commit (if fixes were needed)**

---

### Task 4: Build, install, and push

**Step 1: Full test suite**

```bash
go test ./... -v
```

**Step 2: Build and install**

```bash
go build ./... && go install ./...
```

**Step 3: Smoke test installed binary**

```bash
~/go/bin/jamfpro-cli config setup --help
```

**Step 4: Push**

```bash
git push
```

---

## Summary of changes

| File | Change |
|------|--------|
| `internal/auth/auth.go` | Add `BasicAuthExchange` for one-shot basic auth token exchange |
| `internal/commands/setup.go` | New: `setupClient` with API helpers, `newConfigSetupCmd` with interactive prompts, scope presets, credential generation, profile saving |
| `internal/commands/config.go` | Register `setup` subcommand |
| `go.mod` / `go.sum` | Add `golang.org/x/term` dependency |

## User-facing result

```bash
# Interactive (recommended for first-time setup)
jamfpro-cli config setup

# Scripted (CI/CD)
jamfpro-cli config setup \
  --url https://myorg.jamfcloud.com \
  --username admin \
  --password "$JAMF_ADMIN_PASSWORD" \
  --scope standard \
  --profile-name prod
```
