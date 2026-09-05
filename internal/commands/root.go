// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/client"
	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/security"
	"github.com/Jamf-Concepts/jamf-cli/internal/spinner"
	"github.com/Jamf-Concepts/jamf-cli/internal/xmlconv"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
	"github.com/Jamf-Concepts/jamfschool-go-sdk/jamfschool"
)

// Global flags
var (
	profile             string
	outputFmt           string
	quiet               bool
	noHints             bool
	verboseLevel        int
	noInput             bool
	noColor             bool
	environmentID       string
	dryRun              bool
	wide                bool
	compact             bool
	allowPartialFailure bool
	selectFields        []string
	outFile             string
	fieldName           string
	serverURL           string
	token               string
	tokenFile           string
	clientID            string
	clientSecret        string
	tenantID            string
	cliVersion          string // set by NewRootCmd for use by power commands
	noVersionCheck      bool   // skip tenant version compatibility probe
	noUpdateCheck       bool   // skip the newer-jamf-cli-release advisory
)

// cliClient wraps our client to implement registry.HTTPClient
type cliClient struct {
	*client.Client
}

func (c *cliClient) Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	return c.Client.Do(ctx, method, path, body)
}

// cliOutput wraps our output formatter to implement registry.OutputFormatter
type cliOutput struct {
	*output.Formatter
}

func (o *cliOutput) PrintResponse(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 100<<20)) // 100 MB limit
	if err != nil {
		return err
	}
	return o.PrintRaw(body)
}

func (o *cliOutput) PrintRaw(data []byte) error {
	// Convert XML to JSON if needed (Classic API responses).
	if xmlconv.IsXML(data) {
		if converted, err := xmlconv.ToJSON(data); err == nil {
			data = converted
		}
	}

	if fieldName == "" {
		return o.Formatter.PrintRaw(data)
	}

	// Parse JSON and extract the named field
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("cannot extract field from non-JSON response")
	}

	var objects []map[string]any
	switch v := parsed.(type) {
	case []any:
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				objects = append(objects, m)
			}
		}
	case map[string]any:
		objects = []map[string]any{v}
	default:
		return fmt.Errorf("cannot extract field %q from scalar value", fieldName)
	}

	parts := strings.Split(fieldName, ".")
	for _, obj := range objects {
		val, ok := walkFieldPath(obj, parts)
		if !ok {
			continue
		}
		if _, err := fmt.Fprintln(os.Stdout, output.FormatValue(val)); err != nil {
			return err
		}
	}
	return nil
}

// walkFieldPath traverses a dot-separated path through nested maps.
// "general.id" on {"general": {"id": 42}} returns (42, true).
func walkFieldPath(obj map[string]any, parts []string) (any, bool) {
	current := any(obj)
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

// shouldShowSpinner reports whether HTTP clients should be wrapped to display
// the loading spinner. The spinner is suppressed when the user has asked for
// quiet output, has opted out of ANSI escapes via NO_COLOR or --no-color, or
// has requested verbose logging (which would interleave with the animation).
func shouldShowSpinner() bool {
	return !quiet && !noColor && verboseLevel == 0
}

// spinnerClient wraps an HTTPClient to show a loading spinner during requests.
type spinnerClient struct {
	inner registry.HTTPClient
}

func (c *spinnerClient) Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	if spinner.IsSuppressed(ctx) {
		return c.inner.Do(ctx, method, path, body)
	}
	s := spinner.New("Loading...")
	s.Start()
	defer s.Stop()
	return c.inner.Do(ctx, method, path, body)
}

// spinnerTransport wraps an http.RoundTripper to show a loading spinner
// during requests. Used to add spinner support to SDK HTTP clients that
// manage their own transport (e.g., Platform SDK, Protect SDK).
type spinnerTransport struct {
	inner http.RoundTripper
}

func (t *spinnerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if spinner.IsSuppressed(req.Context()) {
		return t.inner.RoundTrip(req)
	}
	s := spinner.New("Loading...")
	s.Start()
	defer s.Stop()
	return t.inner.RoundTrip(req)
}

// dryRunClient wraps an HTTPClient to intercept mutating requests.
// GET/HEAD pass through; POST/PUT/PATCH/DELETE print what would happen
// and return a synthetic empty response.
type dryRunClient struct {
	inner registry.HTTPClient
}

func (c *dryRunClient) Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	if method == "GET" || method == "HEAD" {
		return c.inner.Do(ctx, method, path, body)
	}

	fmt.Fprintf(os.Stderr, "[dry-run] %s %s\n", method, path)

	if body != nil {
		data, err := io.ReadAll(io.LimitReader(body, 10<<20)) // 10 MB limit
		if err == nil && len(data) > 0 {
			fmt.Fprintf(os.Stderr, "[dry-run] Request body:\n%s\n", string(data))
		}
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("{}")),
		Header:     make(http.Header),
	}, nil
}

// versionCacheEntry holds a single cached tenant version probe result.
type versionCacheEntry struct {
	Version   string    `json:"version"`
	CheckedAt time.Time `json:"checked_at"`
}

const versionCacheTTL = 24 * time.Hour

func versionCachePath() string {
	return filepath.Join(filepath.Dir(config.ConfigPath()), ".version-cache.json")
}

func readVersionCache() map[string]versionCacheEntry {
	data, err := os.ReadFile(versionCachePath())
	if err != nil {
		return nil
	}
	var cache map[string]versionCacheEntry
	if json.Unmarshal(data, &cache) != nil {
		return nil
	}
	return cache
}

func writeVersionCache(profileName, version string) {
	cache := readVersionCache()
	if cache == nil {
		cache = make(map[string]versionCacheEntry)
	}
	cache[profileName] = versionCacheEntry{Version: version, CheckedAt: time.Now().UTC()}
	data, err := json.Marshal(cache)
	if err != nil {
		return
	}
	// Atomic write: temp file then rename to avoid corrupt reads from parallel invocations.
	tmp := versionCachePath() + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, versionCachePath())
	}
}

// checkTenantVersion probes /v1/jamf-pro-version and emits a one-line warning
// to w when the tenant is running an older Jamf Pro than the spec version this
// CLI was generated from. Results are cached per named profile for 24 hours so
// that fast single-shot commands don't pay an extra round-trip on every call.
// Any error (auth failure, timeout, unparseable response) is silently ignored —
// this check must never break a command.
func checkTenantVersion(c registry.HTTPClient, specVersion, profileName string, w io.Writer) {
	if specVersion == "unknown" {
		return
	}

	// Use cached version when available and fresh to avoid a round-trip per command.
	if profileName != "" {
		if cache := readVersionCache(); cache != nil {
			if entry, ok := cache[profileName]; ok && time.Since(entry.CheckedAt) < versionCacheTTL {
				if compareProVersions(entry.Version, specVersion) < 0 {
					_, _ = fmt.Fprintf(w, "warning: tenant is on Jamf Pro %s; this CLI was built against %s — some commands may not be available\n", entry.Version, specVersion)
				}
				return
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.Do(ctx, "GET", "/v1/jamf-pro-version", nil)
	if err != nil {
		return
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Version string `json:"version"`
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	if err != nil || json.Unmarshal(body, &result) != nil || result.Version == "" {
		return
	}

	if profileName != "" {
		writeVersionCache(profileName, result.Version)
	}

	if compareProVersions(result.Version, specVersion) < 0 {
		_, _ = fmt.Fprintf(w, "warning: tenant is on Jamf Pro %s; this CLI was built against %s — some commands may not be available\n", result.Version, specVersion)
	}
}

// compareProVersions compares two Jamf Pro version strings (e.g. "11.28.0").
// Returns -1 if a < b, 0 if equal, 1 if a > b. Any non-numeric suffix is
// stripped before comparison (e.g. "11.28.0-t1234" → "11.28.0").
func compareProVersions(a, b string) int {
	parse := func(v string) [3]int {
		// Strip build suffix at first non-digit/dot character.
		end := len(v)
		for i, c := range v {
			if c != '.' && (c < '0' || c > '9') {
				end = i
				break
			}
		}
		v = v[:end]
		parts := strings.SplitN(v, ".", 3)
		var n [3]int
		for i, p := range parts {
			if i >= 3 {
				break
			}
			for _, c := range p {
				if c >= '0' && c <= '9' {
					n[i] = n[i]*10 + int(c-'0')
				}
			}
		}
		return n
	}
	av, bv := parse(a), parse(b)
	for i := range av {
		if av[i] < bv[i] {
			return -1
		}
		if av[i] > bv[i] {
			return 1
		}
	}
	return 0
}

// AuthParams holds all auth-related inputs for profile resolution.
// Enables callers (like diff) to resolve multiple profiles independently.
type AuthParams struct {
	Profile      string
	ServerURL    string
	Token        string
	TokenFile    string
	TokenStdin   bool
	ClientID     string
	ClientSecret string
	// TenantID and EnvironmentID are the two scope identifiers a platform
	// integration can name, and they are mutually exclusive — see the scope
	// handling in ResolveAuthForProfile. Neither set means organization scope.
	TenantID      string
	EnvironmentID string
}

// ResolveAuthForProfile determines the server URL and auth provider for a
// specific profile name using the given config. Unlike resolveAuth, it does
// not read or mutate package-level variables, making it safe to call multiple
// times for different profiles (e.g., in the diff command).
func ResolveAuthForProfile(cfg *config.Config, params AuthParams) (string, auth.Provider, error) {
	profileName := params.Profile
	url := params.ServerURL
	tok := params.Token
	cid := params.ClientID
	csecret := params.ClientSecret
	tid := params.TenantID
	eid := params.EnvironmentID
	// An explicitly supplied scope — flag or env var — settles the level, so the
	// profile's other level must not be merged in beside it. Without this, a
	// profile carrying tenant-id plus JAMF_ENVIRONMENT_ID read as "both levels"
	// and was refused, when the caller had in fact overridden the profile the way
	// every other env var here overrides it.
	scopeFromParams := tid != "" || eid != ""
	isPlatform := false

	// Config profile: fill remaining gaps.
	// Skip profile credential resolution when a token was explicitly provided
	// via flags/env — the explicit token should take priority over profile
	// credentials to support ad-hoc bearer token usage (e.g., basic auth
	// bootstrap scripts).
	explicitToken := tok != ""
	if len(cfg.Profiles) > 0 {
		p, _, err := config.GetProfile(cfg, profileName)
		if err == nil {
			if url == "" {
				url = p.URL
			}
			if !explicitToken {
				switch p.AuthMethod {
				case "platform":
					isPlatform = true
					if cid == "" && p.ClientID != "" {
						resolved, err := config.ResolveSecret(p.ClientID)
						if err != nil {
							return "", nil, fmt.Errorf("resolving client-id from profile: %w", err)
						}
						cid = resolved
					}
					if csecret == "" && p.ClientSecret != "" {
						resolved, err := config.ResolveSecret(p.ClientSecret)
						if err != nil {
							return "", nil, fmt.Errorf("resolving client-secret from profile: %w", err)
						}
						csecret = resolved
					}
					if !scopeFromParams && p.EnvironmentID != "" {
						eid = p.EnvironmentID
					}
					if !scopeFromParams && tid == "" && p.TenantID != "" {
						tid = p.TenantID
					}
				case "oauth2":
					if cid == "" && p.ClientID != "" {
						resolved, err := config.ResolveSecret(p.ClientID)
						if err != nil {
							return "", nil, fmt.Errorf("resolving client-id from profile: %w", err)
						}
						cid = resolved
					}
					if csecret == "" && p.ClientSecret != "" {
						resolved, err := config.ResolveSecret(p.ClientSecret)
						if err != nil {
							return "", nil, fmt.Errorf("resolving client-secret from profile: %w", err)
						}
						csecret = resolved
					}
				default: // "token" or empty
					if tok == "" && p.Token != "" {
						resolved, err := config.ResolveSecret(p.Token)
						if err != nil {
							return "", nil, fmt.Errorf("resolving token from profile: %w", err)
						}
						tok = resolved
					}
				}
			}
		}
		if err != nil && profileName != "" {
			return "", nil, fmt.Errorf("loading profile: %w", err)
		}
	}

	// Token from file
	if tok == "" && params.TokenFile != "" {
		data, err := os.ReadFile(params.TokenFile)
		if err != nil {
			return "", nil, fmt.Errorf("reading token file %s: %w", params.TokenFile, err)
		}
		tok = strings.TrimSpace(string(data))
	}

	// Normalize: strip trailing slash (silently fixes profiles saved before this
	// check was added) and add https:// to bare hostnames in env/flag inputs.
	url = strings.TrimRight(url, "/")
	if url != "" && !strings.Contains(url, "://") {
		url = "https://" + url
	}

	// Validate
	if url == "" {
		return "", nil, exitcode.New(exitcode.Usage, "server URL is required: use --url, JAMF_URL env var, or jamf-cli config add-profile")
	}
	if strings.HasPrefix(url, "http://") {
		fmt.Fprintln(os.Stderr, "WARNING: using HTTP (not HTTPS) — credentials will be sent in plaintext")
	}
	if (cid != "") != (csecret != "") {
		if cid != "" {
			return "", nil, exitcode.New(exitcode.Usage, "client secret is required when client ID is provided: set JAMF_CLIENT_SECRET env var or use a config profile")
		}
		return "", nil, exitcode.New(exitcode.Usage, "client ID is required when client secret is provided: set JAMF_CLIENT_ID env var or use a config profile")
	}

	// Platform gateway auth. Client credentials are required; a scope is not.
	// An integration is created at one of three levels and its credential
	// carries that choice: organization-scoped credentials name no ID at all
	// (the gateway reads the scope from the access token), while environment-
	// and tenant-scoped ones name theirs. Both IDs at once cannot be honoured,
	// since the credential only works with one level's header.
	//
	// The gateway host counts as a request for platform auth in its own right.
	// Without that, the only env-var route into this branch was a tenant or
	// environment ID — so an *organization*-scoped credential, which names no
	// ID at all by definition, could only be used from a saved profile and
	// JAMF_URL + JAMF_CLIENT_ID + JAMF_CLIENT_SECRET fell through to oauth2
	// against a URL that is not a Jamf Pro instance. Nothing exercised it until
	// the Jamf Account commands arrived: they are the first surface that is
	// organization-scoped only, and CI/CD is exactly the env-var case.
	if isPlatform || tid != "" || eid != "" || isPlatformGatewayURL(url) {
		if cid == "" || csecret == "" {
			return "", nil, exitcode.New(exitcode.Usage, "client ID and client secret are required for platform gateway auth: set JAMF_CLIENT_ID/JAMF_CLIENT_SECRET env vars or use a config profile")
		}
		// Refuse the retired gateway host by name, before the token exchange.
		// Shared with newPlatformSDKClient rather than spelled twice: the
		// wording is the whole value of the refusal, and two copies drift.
		if err := refuseRetiredGatewayURL(url); err != nil {
			return "", nil, err
		}
		// Both set can now only mean both were supplied together — the profile
		// cannot contribute a second level past scopeFromParams — so this is a
		// genuine "you asked for two levels at once" rather than an override.
		if tid != "" && eid != "" {
			return "", nil, exitcode.New(exitcode.Usage,
				"--environment-id and --tenant-id are mutually exclusive: an API integration is created at one level, and its credential only works with that level's header")
		}
		scope := auth.TenantScope(tid)
		if eid != "" {
			scope = auth.EnvironmentScope(eid)
		}
		return url, auth.NewPlatformOAuth2Provider(url, cid, csecret, scope), nil
	}

	// Construct auth provider
	switch {
	case cid != "" && csecret != "":
		return url, auth.NewOAuth2Provider(url, cid, csecret), nil
	case tok != "":
		return url, auth.NewTokenProvider(tok), nil
	default:
		return "", nil, exitcode.New(exitcode.Usage, "authentication required: use JAMF_TOKEN/JAMF_CLIENT_ID/JAMF_CLIENT_SECRET env vars, or jamf-cli config add-profile")
	}
}

// resolveAuth determines the server URL and auth provider using the priority
// chain: CLI flags > environment variables > config profile. It reads and
// fills gaps in the package-level flag variables. Thin wrapper around
// ResolveAuthForProfile.
func resolveAuth(cfg *config.Config) (string, auth.Provider, error) {
	// Environment variable fallbacks
	if profile == "" {
		profile = os.Getenv("JAMF_PROFILE")
	}
	if serverURL == "" {
		serverURL = os.Getenv("JAMF_URL")
	}
	if token == "" {
		token = os.Getenv("JAMF_TOKEN")
	}
	if clientID == "" {
		clientID = os.Getenv("JAMF_CLIENT_ID")
	}
	if clientSecret == "" {
		clientSecret = os.Getenv("JAMF_CLIENT_SECRET")
	}
	// A scope flag settles the level, so the OTHER level must not be backfilled
	// from the environment beside it. Backfilling both independently made
	// `--tenant-id X` with JAMF_ENVIRONMENT_ID exported arrive at
	// ResolveAuthForProfile as two levels supplied together, which it refuses —
	// while the same pair on the `security` path let the flag win, because
	// PersistentPreRunE returns before this function and checkScopeConflict
	// reads the flag vars unbackfilled. Measured on the built binary: exit 2
	// "mutually exclusive" on `pro blueprints list`, and a request against the
	// flag's tenant on `security device-groups list`. One documented rule, two
	// answers. The env var still overrides the profile; what it no longer does
	// is contradict a flag on one path only.
	if tenantID == "" && environmentID == "" {
		tenantID = os.Getenv("JAMF_TENANT_ID")
		environmentID = os.Getenv("JAMF_ENVIRONMENT_ID")
	}

	// Token from file
	if token == "" && tokenFile != "" {
		data, err := os.ReadFile(tokenFile)
		if err != nil {
			return "", nil, fmt.Errorf("reading token file %s: %w", tokenFile, err)
		}
		token = strings.TrimSpace(string(data))
	}

	url, provider, err := ResolveAuthForProfile(cfg, AuthParams{
		Profile:       profile,
		ServerURL:     serverURL,
		Token:         token,
		TokenFile:     tokenFile,
		ClientID:      clientID,
		ClientSecret:  clientSecret,
		TenantID:      tenantID,
		EnvironmentID: environmentID,
	})
	if err != nil {
		return "", nil, err
	}

	// Write back resolved URL so other code (overview health check) sees it
	serverURL = url
	return url, provider, nil
}

func NewRootCmd(version, commit, date, specProVersion string) *cobra.Command {
	cliVersion = version
	// CLIContext is populated in PersistentPreRunE after token/URL resolution
	cliCtx := &registry.CLIContext{}
	var outFileHandle *os.File

	cmd := &cobra.Command{
		Use:     "jamf-cli",
		Short:   "CLI for the Jamf platform",
		Version: version,
		Long: `jamf-cli is a command-line interface for the Jamf platform.

Use "jamf-cli pro" for Jamf Pro commands (device management, inventory,
configuration, reporting, and API automation).
Use "jamf-cli protect" for Jamf Protect commands (endpoint security,
analytics, threat prevention, and configuration).
Use "jamf-cli school" for Jamf School commands (education device management,
users, classes, and apps).
Use "jamf-cli security" for Jamf Security Cloud commands (device risk,
device lifecycle, and Shared Signals & Events stream configuration).

Set JAMF_CLI_ARGS to prepend default flags to every invocation:
  export JAMF_CLI_ARGS='--quiet --no-input'
  export JAMF_CLI_ARGS='--profile "My CI Profile"'

Set JAMF_CLI_NO_HINTS=1 to suppress advisory hints while keeping the
spinner and progress output (narrower than --quiet).

Once a day, an interactive release build checks whether a newer jamf-cli
exists and prints a one-line hint to stderr. Silence it with
--no-update-check, JAMF_CLI_NO_UPDATE_CHECK=1, or "update-check: false"
in the config file. It never runs in CI, when output is piped, or under
--quiet / --no-hints.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Respect NO_COLOR env var (https://no-color.org)
			if _, ok := os.LookupEnv("NO_COLOR"); ok {
				noColor = true
			}

			// JAMF_CLI_NO_HINTS disables advisory hints (parallels NO_COLOR,
			// but value-parsed so JAMF_CLI_NO_HINTS=0 leaves hints on).
			if b, err := strconv.ParseBool(os.Getenv("JAMF_CLI_NO_HINTS")); err == nil && b {
				noHints = true
			}

			// Load config up-front so the formatter honours default-output
			// for every command, including those that skip auth.
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			// Resolve effective output format: explicit --output wins, then
			// config default_output, then auto (TTY -> table, piped -> json).
			// Color is disabled whenever stdout is not an interactive terminal
			// so ANSI never leaks into a pipe.
			stdoutTTY := output.IsTerminal(os.Stdout.Fd())
			outputFmt = output.ResolveFormat(
				cmd.Flags().Changed("output"), outputFmt, cfg.DefaultOutput,
				stdoutTTY, outFile != "",
			)
			// noColor at this point reflects only explicit sources (--no-color
			// flag or NO_COLOR env), before stdout-piping / out-file auto-disable.
			explicitNoColor := noColor
			if !stdoutTTY {
				noColor = true
			}

			if outputFmt == string(output.FormatJSONMulti) && os.Getenv("JAMF_CLI_MULTI_CAPTURE") == "" {
				return fmt.Errorf("output format %q is reserved for internal use", outputFmt)
			}

			// Build output formatter (shared by all products and by skipped
			// commands like config/completion/version).
			if outFile != "" {
				f, err := os.Create(outFile)
				if err != nil {
					return fmt.Errorf("opening output file: %w", err)
				}
				outFileHandle = f
				noColor = true
			}
			formatter := output.New(outputFmt, noColor, wide)
			if outFileHandle != nil {
				formatter.SetWriter(outFileHandle)
			}
			formatter.SetProjector(output.Projector{Compact: compact, Select: selectFields})
			formatter.SetQuiet(quiet)
			formatter.SetNoHints(noHints)
			formatter.SetExplicitNoColor(explicitNoColor)
			cliCtx.Output = &cliOutput{formatter}
			// Set before any product branch: the Protect, School and Security
			// Cloud paths return from here directly, so an assignment made
			// alongside the Pro client would leave DryRun false for exactly the
			// commands whose transports cannot be wrapped by dryRunClient.
			cliCtx.DryRun = dryRun

			// Release advisory — probes at most once per 24 h, in the
			// background, and prints in PersistentPostRunE so it can neither
			// delay nor interleave with the command's own output. Started
			// before the auth-skip returns below so it covers auth-free
			// commands too. Every suppression rule lives in the gate; see
			// startUpdateCheck.
			pendingUpdateCheck = startUpdateCheck(cmd, cfg, version, noUpdateCheck)

			// Group parent commands (made runnable only to reject unknown
			// subcommands) never call an API; skip auth so `pro buildings`
			// help and typos work without credentials.
			if cmd.Annotations[groupParentAnnotation] == "true" {
				return nil
			}

			// Skip auth for commands that don't need it. Most are matched
			// anywhere in the chain (e.g. "config" covers all subcommands,
			// "setup" covers both "pro setup" and "protect setup").
			//
			// rootOnlySkip names the ones that must match a direct child of the
			// root and nothing else, because they are ordinary English words a
			// resource can legitimately use for an operation. Matching them
			// anywhere is a silent auth bypass: the command runs with no client
			// and fails at its own gate with "this command requires platform
			// gateway auth", which sends the operator to fix credentials that
			// were never the problem. "commands" was already here because
			// "pro mdm-commands commands" must not skip; "version" joined it
			// when AI Governance's GET /policies/{id}/versions/{n} became
			// "platform ai-policies version" and stopped authenticating.
			chainSkip := map[string]bool{
				"completion":    true,
				"help":          true,
				"config":        true,
				"diff":          true,
				"setup":         true,
				"multi":         true,
				"doctor":        true,
				"mcp":           true,
				"agent-context": true,
			}
			rootOnlySkip := map[string]bool{
				"commands": true,
				"version":  true,
			}
			for c := cmd; c != nil; c = c.Parent() {
				if chainSkip[c.Name()] {
					return nil
				}
				if rootOnlySkip[c.Name()] && c.Parent() != nil && c.Parent().Parent() == nil {
					return nil
				}
			}

			// A command that opts out for itself, wherever it sits in the tree.
			if cmd.Annotations[noAuthAnnotation] == "true" {
				return nil
			}

			// --scaffold just prints a JSON template — no auth needed.
			if scaffold, _ := cmd.Flags().GetBool("scaffold"); scaffold {
				return nil
			}

			// Determine product type from command hierarchy or profile
			product := resolveProduct(cmd, cfg)

			if product == "protect" {
				return resolveProtectClient(cfg, cliCtx)
			}
			if product == "school" {
				return resolveSchoolClient(cfg, cliCtx)
			}
			if product == "security" {
				return resolveSecurityClient(cfg, cliCtx)
			}

			// Default: Jamf Pro auth flow
			resolvedURL, authProvider, err := resolveAuth(cfg)
			if err != nil {
				return err
			}

			// Build HTTP client with decorators.
			// jarProvider is satisfied by OAuth2Provider and PlatformOAuth2Provider.
			type jarProvider interface {
				Jar() http.CookieJar
			}
			clientOpts := []client.Option{client.WithVerbose(verboseLevel)}
			if p, ok := authProvider.(*auth.PlatformOAuth2Provider); ok {
				clientOpts = append(clientOpts, client.WithGatewayScope(p.Scope()))
			}
			if jp, ok := authProvider.(jarProvider); ok {
				clientOpts = append(clientOpts, client.WithCookieJar(jp.Jar()))
			}
			proClient := &cliClient{client.New(resolvedURL, authProvider, clientOpts...)}
			cliCtx.Uploader = proClient // set before wrapping with decorators
			var httpClient registry.HTTPClient = proClient
			if dryRun {
				httpClient = &dryRunClient{inner: httpClient}
			}
			if shouldShowSpinner() {
				httpClient = &spinnerClient{inner: httpClient}
			}
			cliCtx.Client = httpClient
			cliCtx.AuthProvider = authProvider

			// Resolve effective profile name and per-profile cooldown setting.
			resolvedProfile := profile
			if resolvedProfile == "" {
				resolvedProfile = os.Getenv("JAMF_PROFILE")
			}
			if resolvedProfile == "" {
				resolvedProfile = cfg.DefaultProfile
			}
			cliCtx.ProfileName = resolvedProfile
			if resolvedProfile != "" {
				if p, ok := cfg.Profiles[resolvedProfile]; ok {
					cliCtx.DestructiveCooldown = p.DestructiveCooldown
				}
			}

			// Refuse an API the resolved credentials cannot reach, before any
			// request goes out — a Pro or Classic endpoint the gateway does not
			// expose, or a Platform command on an instance profile. Placed
			// after the clients are built but before anything is sent (the
			// version check below is the first request), because it needs the
			// resolved profile name to say which credentials are in play.
			if err := checkAPIMatch(cmd, authProvider, resolvedProfile); err != nil {
				return err
			}

			// When platform gateway auth is active, also construct the
			// Platform SDK client for platform-native commands (blueprints,
			// compliance-benchmarks, etc.). The SDK manages its own OAuth2
			// token lifecycle independently from the Pro HTTP client.
			if p, ok := authProvider.(*auth.PlatformOAuth2Provider); ok {
				if err := checkScopeConflict(cfg, resolvedProfile); err != nil {
					return err
				}
				sdk, err := newPlatformSDKClient(
					resolvedURL, p.ClientID(), p.ClientSecret(), p.Scope(),
					shouldShowSpinner(),
				)
				if err != nil {
					return err
				}
				cliCtx.PlatformSDKClient = sdk
			}

			// Tenant version check — probes once per 24 h per profile and warns if
			// the tenant is running an older Jamf Pro than the spec. Non-fatal: any
			// error is silently ignored. Suppressed by --no-version-check, --quiet,
			// or JAMF_NO_VERSION_CHECK. The "unknown" guard is inside the function.
			if !noVersionCheck && !quiet && os.Getenv("JAMF_NO_VERSION_CHECK") == "" {
				checkTenantVersion(proClient, specProVersion, profile, os.Stderr)
			}

			return nil
		},
		PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
			// Only on the success path: a user staring at an error does not
			// need to hear about a release too.
			pendingUpdateCheck.notify(os.Stderr)
			if outFileHandle != nil {
				return outFileHandle.Close()
			}
			return nil
		},
	}

	// Custom version template so --version matches the `version` subcommand output
	cmd.SetVersionTemplate(fmt.Sprintf("jamf-cli %s\n  commit: %s\n  built:  %s\n", version, commit, date))

	// Suggest the nearest command for typos ("did you mean ...").
	cmd.SuggestionsMinimumDistance = 2
	// Suggest the nearest flag for unknown-flag typos, then classify as a usage
	// error (exit 2) so the exit code matches the helpers.go contract.
	cmd.SetFlagErrorFunc(func(c *cobra.Command, ferr error) error {
		const marker = "unknown flag: --"
		if i := strings.Index(ferr.Error(), marker); i >= 0 {
			bad := strings.SplitN(ferr.Error()[i+len(marker):], " ", 2)[0]
			var known []string
			c.Flags().VisitAll(func(f *pflag.Flag) { known = append(known, f.Name) })
			if s := suggestFlag(bad, known); s != "" {
				fmt.Fprintf(os.Stderr, "hint: did you mean --%s?\n", s)
			}
		}
		return exitcode.Wrap(exitcode.Usage, ferr)
	})

	// Global flags
	cmd.PersistentFlags().StringVarP(&profile, "profile", "p", "", "config profile to use (or JAMF_PROFILE env)")
	// Empty means "not resolved yet": PersistentPreRunE fills it from the flag,
	// the profile's default-output or the TTY, and ResolveFormat ignores this
	// value unless the flag was changed. So the only readers of the default are
	// the paths that run BEFORE resolution — an Args refusal (cobra validates
	// args first) and a flag error — and defaulting it to "json" made both
	// render the machine envelope on stdout for a caller who never asked for
	// JSON, while a refusal raised from RunE printed plain text on stderr.
	cmd.PersistentFlags().StringVarP(&outputFmt, "output", "o", "", "output format: table, json, ndjson, csv, yaml, plain, xml (pretty), raw (classic commands default to xml)")
	cmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "suppress non-error output")
	cmd.PersistentFlags().BoolVar(&noHints, "no-hints", false, "suppress advisory hints (e.g. large-result narrowing tips); keeps spinner and progress output (or JAMF_CLI_NO_HINTS env)")
	cmd.PersistentFlags().CountVarP(&verboseLevel, "verbose", "v", "show HTTP requests/responses (-vv adds headers, -vvv adds bodies)")
	cmd.PersistentFlags().BoolVar(&noInput, "no-input", false, "never prompt; fail if input required")
	cmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable colored output")
	cmd.PersistentFlags().BoolVarP(&dryRun, "dry-run", "n", false, "preview changes without executing")
	cmd.PersistentFlags().BoolVarP(&wide, "wide", "w", false, "show all columns in table output")
	cmd.PersistentFlags().BoolVar(&compact, "compact", false, "keep only high-signal scalar fields (identity + fields common across rows); smaller payloads for agents; ignored when --field is set")
	cmd.PersistentFlags().StringSliceVar(&selectFields, "select", nil, "project output to these dot-path fields only, e.g., --select id,general.name,udid (ignored when --field is set)")
	cmd.PersistentFlags().StringVar(&outFile, "out-file", "", "write output to file instead of stdout")
	cmd.PersistentFlags().StringVar(&fieldName, "field", "", "extract a single field from JSON response (e.g., --field id)")
	cmd.PersistentFlags().BoolVar(&allowPartialFailure, "allow-partial-failure", false, "downgrade a partial batch failure (some items failed) to a warning and exit 0")

	// Connection flags
	cmd.PersistentFlags().StringVar(&serverURL, "url", "", "Jamf Pro server URL (or JAMF_URL env)")
	cmd.PersistentFlags().StringVar(&tokenFile, "token-file", "", "path to file containing API token")
	cmd.PersistentFlags().StringVar(&tenantID, "tenant-id", "", "tenant ID for platform gateway auth (or JAMF_TENANT_ID env); mutually exclusive with --environment-id")
	cmd.PersistentFlags().StringVar(&environmentID, "environment-id", "", "platform environment ID for platform gateway auth (or JAMF_ENVIRONMENT_ID env); mutually exclusive with --tenant-id")
	cmd.PersistentFlags().BoolVar(&noVersionCheck, "no-version-check", false, "skip tenant version compatibility check (also: JAMF_NO_VERSION_CHECK env)")
	cmd.PersistentFlags().BoolVar(&noUpdateCheck, "no-update-check", false, "skip the daily check for a newer jamf-cli release (also: JAMF_CLI_NO_UPDATE_CHECK env, or update-check: false in config)")

	// Version command (extracted to version.go so it can pull in provenance).
	cmd.AddCommand(newVersionCmd(cliCtx, version, commit, date, specProVersion))

	// Config command group
	cmd.AddCommand(newConfigCmd(cliCtx))

	// Doctor — diagnostic command, no auth required.
	cmd.AddCommand(newDoctorCmd(cliCtx))

	// Completion command
	cmd.AddCommand(newCompletionCmd())

	// Commands discovery subcommand
	cmd.AddCommand(newCommandsCmd(cmd))

	// Agent operating guide (auth, exit codes, flags, MCP)
	cmd.AddCommand(newAgentContextCmd())

	// Multi-profile command runner
	cmd.AddCommand(newMultiCmd())

	// MCP server (exposes the command tree to AI clients over stdio)
	cmd.AddCommand(newMCPCmd())

	// Jamf Pro product namespace
	cmd.AddCommand(newProCmd(cliCtx))

	// Jamf Protect product namespace
	cmd.AddCommand(newProtectCmd(cliCtx))

	// Jamf School product namespace
	cmd.AddCommand(newSchoolCmd(cliCtx))

	// Jamf Security Cloud product namespace
	cmd.AddCommand(newSecurityCmd(cliCtx))

	// Jamf Platform namespace
	cmd.AddCommand(newPlatformCmd(cliCtx))

	// Apply root-level aliases and groups for --help output
	applyRootAliases(cmd)
	applyRootGroups(cmd)

	// Say in --help which Pro and Classic commands the platform gateway does
	// not carry, so it is discoverable without running one and failing.
	applyGatewayCoverageHelp(cmd)

	// cobra only rejects unknown subcommands at the root; extend that to every
	// non-runnable parent so typos like `pro buildings lst` error with a hint
	// instead of silently printing help and exiting 0.
	guardUnknownSubcommands(cmd)

	// A sibling walk rather than part of the one above, which returns early for
	// every command that has an argument validator — a leaf has no subcommands.
	// Both have to run after the last AddCommand, which is the only thing they
	// share.
	classifyArgsErrors(cmd)

	return cmd
}

// commandEntry represents a single leaf command for structured output.
type commandEntry struct {
	Command     string   `json:"command"`
	Description string   `json:"description"`
	Aliases     []string `json:"aliases,omitempty"`
	Flags       []string `json:"flags,omitempty"`
	Product     string   `json:"product,omitempty"`
	Group       string   `json:"group,omitempty"`
	Destructive bool     `json:"destructive,omitempty"`
	Privileges  []string `json:"privileges,omitempty"`
	// API is which Jamf API serves the command, and so which credentials it
	// needs — "platform-gateway" or "radar". It matters most under `security`,
	// where both appear side by side and take different credentials.
	API string `json:"api,omitempty"`
	// Gateway is set to "unserved" when the Jamf Platform gateway's published API
	// does not carry this Pro or Classic endpoint, in which case a gateway
	// profile is refused before a request is sent. GatewayBasis is the evidence
	// — "probe" (wire-confirmed unrouted) or "unpublished" (absent from the
	// published surface; the gateway may still route it today, transitionally) —
	// and GatewayDetail spells it out. Absent when the gateway serves it.
	// GatewaySuccessor names a command that ships in this binary, does the same
	// job, and is served by the gateway — the answer to "then what do I run".
	// Present only for a refused command that has one, from the curated table
	// in internal/gateway; the runtime refusal and the --help caveat render the
	// same entry, so a script and an operator get the same answer.
	Gateway          string `json:"gateway,omitempty"`
	GatewayBasis     string `json:"gatewayBasis,omitempty"`
	GatewayDetail    string `json:"gatewayDetail,omitempty"`
	GatewaySuccessor string `json:"gatewaySuccessor,omitempty"`
	// GatewayPrivileges are the Jamf Account capability permissions the gateway
	// requires for this Pro or Classic command — a different vocabulary from
	// Privileges above, not a translation of it, since the GA consolidation
	// folded several Jamf Pro privileges into one capability and Jamf Account no
	// longer offers the old names. Both are carried so a Platform API
	// integration can be sized from this catalog rather than by provoking 403s.
	//
	// Absent for an unserved endpoint (the published spec declares no scope for
	// what it does not publish), for the 44 unauthenticated Jamf Pro endpoints,
	// and for hand-written commands, which send no single endpoint. A --name,
	// --serial or --udid lookup additionally resolves the identifier through the
	// resource's collection, so those invocations also need its read permission.
	GatewayPrivileges []string `json:"gatewayPrivileges,omitempty"`
	// GatewayPermissions is the same requirement in the words Jamf Account
	// prints — "Organizational context > Categories: Read (categories:read)",
	// one row per permission, deduplicated across actions.
	//
	// It is not a convenience over GatewayPrivileges, it is the only actionable
	// form: an integration can only be created in the Jamf Account UI, whose
	// picker is a list of named permissions with a checkbox per action and shows
	// the capability slug nowhere. A reader handed only slugs has to go to Jamf's
	// permissions-map article to act, and the names differ enough that guessing
	// fails — computer-inventory-collection-settings is "Device inventory
	// collection settings". The slugs stay beside it because that is what the
	// gateway's own errors and the specs use, so a script matching on them keeps
	// a stable key.
	GatewayPermissions []string `json:"gatewayPermissions,omitempty"`
}

// isFullDetailFormat reports whether an output format carries the full
// per-command detail (aliases, flags, product, group, destructive) in the
// `commands` catalog. Structured machine formats get full detail; table/plain
// stay compact unless --wide is set.
func isFullDetailFormat(format string) bool {
	switch output.Format(format) {
	case output.FormatJSON, output.FormatNDJSON, output.FormatYAML, output.FormatCSV:
		return true
	default:
		return false
	}
}

// newCommandsCmd creates the "commands" subcommand that outputs the full
// command tree in a machine-readable format.
func newCommandsCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "commands",
		Short: "List all available commands",
		Long:  `List all available commands in a structured format for discovery by scripts and AI agents.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries := collectCommands(root, "", "", "")
			formatter := output.New(outputFmt, noColor, wide)
			// Structured formats always get full detail; table/plain
			// show only command+description unless --wide is set.
			full := wide || isFullDetailFormat(outputFmt)
			return formatter.Print(commandEntriesToMaps(entries, full))
		},
	}
}

// collectCommands recursively walks the command tree and returns leaf commands.
// product and group are inherited from parent context and updated as we descend.
func collectCommands(cmd *cobra.Command, prefix, product, group string) []commandEntry {
	var entries []commandEntry
	for _, child := range cmd.Commands() {
		// "commands" is skipped only at the root, where it is this catalog
		// command itself. Matching the name at any depth is the same mistake
		// chainSkip made with "version": it silently dropped
		// `pro mdm-commands commands` — a real generated operation, and one the
		// gateway refuses, so the catalog was missing exactly the entry a reader
		// consults it for. "help" stays unconditional: cobra gives every command
		// one.
		if child.Hidden || child.Name() == "help" || (child.Name() == "commands" && prefix == "") {
			continue
		}

		fullPath := child.Name()
		if prefix != "" {
			fullPath = prefix + " " + child.Name()
		}

		// Determine product for this child's subtree. Only top-level namespaces
		// set the product: product is empty only at the root, so gating on it
		// prevents a nested command that happens to be named after a namespace
		// (e.g. "pro report security") from being re-tagged.
		childProduct := product
		if product == "" && (child.Name() == "pro" || child.Name() == "protect" || child.Name() == "school" || child.Name() == "security" || child.Name() == "platform") {
			childProduct = child.Name()
		}

		// Determine group for this child's subtree.
		childGroup := group
		if child.GroupID != "" {
			childGroup = groupTitle(child.GroupID)
		}

		// Leaf command: has RunE or Run
		if child.RunE != nil || child.Run != nil {
			var privileges []string
			if p := child.Annotations["jamf:privileges"]; p != "" {
				privileges = strings.Split(p, ",")
			}
			entry := commandEntry{
				Command:     fullPath,
				Description: child.Short,
				Product:     childProduct,
				Group:       childGroup,
				Destructive: child.Annotations["jamf:destructive"] == "true",
				Privileges:  privileges,
				API:         child.Annotations["jamf:api"],

				Gateway:          child.Annotations[annotationGateway],
				GatewayBasis:     child.Annotations[annotationGatewayBasis],
				GatewayDetail:    child.Annotations[annotationGatewayDetail],
				GatewaySuccessor: gatewaySuccessorOf(child, fullPath),

				GatewayPrivileges:  gatewayPrivilegesOf(child),
				GatewayPermissions: gatewayPermissionsOf(child),
			}

			// Collect aliases: for leaf commands under a top-level group
			// (e.g., "computers list"), expose the group's aliases ("comp")
			// so agents know "comp list" also works.
			if len(child.Aliases) > 0 {
				entry.Aliases = child.Aliases
			} else if len(cmd.Aliases) > 0 {
				entry.Aliases = cmd.Aliases
			}

			// Collect non-hidden local flags
			var flags []string
			child.LocalFlags().VisitAll(func(f *pflag.Flag) {
				if !f.Hidden {
					flags = append(flags, "--"+f.Name)
				}
			})
			entry.Flags = flags

			entries = append(entries, entry)
		}

		// Recurse into subcommands
		if child.HasSubCommands() {
			entries = append(entries, collectCommands(child, fullPath, childProduct, childGroup)...)
		}
	}
	return entries
}

// commandEntriesToMaps converts command entries to the []map[string]interface{}
// format expected by the output formatter. When full is true, aliases and flags
// columns are included; otherwise only command and description are emitted
// for a compact table.
func commandEntriesToMaps(entries []commandEntry, full bool) []map[string]any {
	result := make([]map[string]any, len(entries))
	for i, e := range entries {
		m := map[string]any{
			"command":     e.Command,
			"description": e.Description,
		}
		if full {
			aliases := ""
			if len(e.Aliases) > 0 {
				aliases = strings.Join(e.Aliases, ", ")
			}
			flags := ""
			if len(e.Flags) > 0 {
				flags = strings.Join(e.Flags, ", ")
			}
			m["aliases"] = aliases
			m["flags"] = flags
			m["product"] = e.Product
			m["group"] = e.Group
			// Emit unconditionally (not just when true) so CSV and table output,
			// which derive their columns from the first row, carry the field for
			// every row rather than dropping it when the first row isn't destructive.
			m["destructive"] = e.Destructive
			// Privileges, unlike destructive above, is positive-only: an empty
			// array would falsely assert "needs no privileges" for commands that
			// simply don't declare them (classic, platform, handwritten), so the
			// key is omitted when absent. It is primarily a JSON/agent + 403-hint
			// signal; CSV/table may not surface it (column set derives from row 0).
			if len(e.Privileges) > 0 {
				m["privileges"] = e.Privileges
			}
			// Positive-only for the same reason as privileges: hand-written
			// commands declare no API, and an empty string in every row would
			// read as a claim rather than an absence.
			if e.API != "" {
				m["api"] = e.API
			}
			// Positive-only, likewise: an empty "gateway" on every row would
			// read as "known to be served", which is a stronger claim than the
			// coverage manifest supports — most commands are simply not
			// annotated. The detail travels with it so a reader can weigh the
			// evidence rather than take the level on faith.
			if e.Gateway != "" {
				m["gateway"] = e.Gateway
				m["gatewayBasis"] = e.GatewayBasis
				m["gatewayDetail"] = e.GatewayDetail
				// Positive-only again: most refused commands have no
				// successor, and an empty string on every row would read as
				// "no replacement exists" rather than "none is recorded".
				if e.GatewaySuccessor != "" {
					m["gatewaySuccessor"] = e.GatewaySuccessor
				}
			}
			// Positive-only, as privileges is, and for the same reason: absent
			// means "no capability recorded", which is not "needs none".
			if len(e.GatewayPrivileges) > 0 {
				m["gatewayPrivileges"] = e.GatewayPrivileges
			}
			// Emitted independently of the slugs above: a Platform command's own
			// privileges are already the capability vocabulary, so it has a
			// Jamf Account rendering without a second slug list to carry.
			if len(e.GatewayPermissions) > 0 {
				m["gatewayPermissions"] = e.GatewayPermissions
			}
		}
		result[i] = m
	}
	return result
}

// resolveProduct determines the product type. The command's own namespace is
// definitive when it has one; otherwise the config profile's product field
// decides.
//
// Only the top-level namespace counts, and matching at any depth is what made
// `pro report security` resolve as Jamf Security Cloud: the walk started at the
// leaf, so the innermost name won. PersistentPreRunE then returned early on the
// security branch without building cliCtx.Client, and the command dereferenced
// a nil one — a SIGSEGV where the same input used to be a clean error, because
// securityPlatformSDKClient now succeeds from the generic JAMF_* credentials
// where resolveSecurityClient used to fail for want of Radar ones. This is the
// same hazard collectCommands guards with rootOnlySkip; the catalog walk was
// fixed and the auth walk was not.
func resolveProduct(cmd *cobra.Command, cfg *config.Config) string {
	if ns := topLevelNamespace(cmd); ns != "" {
		return ns
	}

	// Fall back to profile product field
	profileName := profile
	if profileName == "" {
		profileName = os.Getenv("JAMF_PROFILE")
	}
	if profileName == "" {
		profileName = cfg.DefaultProfile
	}
	if profileName == "" {
		return "pro"
	}
	p, ok := cfg.Profiles[profileName]
	if !ok {
		return "pro"
	}
	switch p.Product {
	case "protect":
		return "protect"
	case "school":
		return "school"
	case "security":
		return "security"
	default:
		return "pro"
	}
}

// topLevelNamespace returns the product namespace cmd sits under — the name of
// its ancestor that is a direct child of the root — or "" when cmd is the root
// itself or its namespace is not a product.
//
// Deliberately not a match at any depth: a subcommand may legitimately be named
// after a namespace (`pro report security`), and only the top level says which
// product's credentials and client the invocation needs.
func topLevelNamespace(cmd *cobra.Command) string {
	if cmd == nil {
		return ""
	}
	top := cmd
	for top.Parent() != nil && top.Parent().Parent() != nil {
		top = top.Parent()
	}
	if top.Parent() == nil {
		return "" // cmd is the root
	}
	switch top.Name() {
	case "protect", "school", "security", "pro":
		return top.Name()
	}
	return ""
}

// clearableProtectCache is a jamfprotect.TokenCache that stores tokens on disk
// (using the same file format as the SDK's built-in FileTokenCache) and
// supports forced invalidation via Clear(). Clear() marks the cache as bypassed
// so the next Load call returns nothing, forcing the SDK to exchange fresh
// credentials. Store resets the bypass flag and persists the new token.
//
// The SDK calls Load/Store with a pre-computed key (sha256 of baseURL+clientID),
// so we use it directly as the filename suffix — no key derivation needed here.
type clearableProtectCache struct {
	dir     string
	mu      sync.Mutex
	cleared bool
	lastKey string
}

func (c *clearableProtectCache) Load(key string) (string, time.Time, bool) {
	c.mu.Lock()
	cleared := c.cleared
	c.lastKey = key
	c.mu.Unlock()
	if cleared {
		_ = os.Remove(filepath.Join(c.dir, "jamfprotect-token-"+key))
		return "", time.Time{}, false
	}
	data, err := os.ReadFile(filepath.Join(c.dir, "jamfprotect-token-"+key))
	if err != nil {
		return "", time.Time{}, false
	}
	var entry struct {
		AccessToken string    `json:"access_token"`
		ExpiresAt   time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(data, &entry); err != nil || entry.AccessToken == "" {
		return "", time.Time{}, false
	}
	return entry.AccessToken, entry.ExpiresAt, true
}

func (c *clearableProtectCache) Store(key string, token string, expiresAt time.Time) error {
	c.mu.Lock()
	c.cleared = false
	c.mu.Unlock()
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return fmt.Errorf("creating protect token cache dir: %w", err)
	}
	data, err := json.Marshal(struct {
		AccessToken string    `json:"access_token"`
		ExpiresAt   time.Time `json:"expires_at"`
	}{token, expiresAt})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.dir, "jamfprotect-token-"+key), data, 0o600)
}

// Clear removes any cached token from disk (if the key is known) and marks the
// cache as bypassed so the next Load returns nothing, forcing the SDK to
// exchange fresh credentials. Store resets the bypass automatically.
func (c *clearableProtectCache) Clear() {
	c.mu.Lock()
	c.cleared = true
	key := c.lastKey
	c.mu.Unlock()
	if key != "" {
		_ = os.Remove(filepath.Join(c.dir, "jamfprotect-token-"+key))
	}
}

// resolveProtectClient constructs a Jamf Protect SDK client from config/flags/env
// and assigns it to cliCtx.ProtectClient.
func resolveProtectClient(cfg *config.Config, cliCtx *registry.CLIContext) error {
	profileName := profile
	if profileName == "" {
		profileName = os.Getenv("JAMF_PROFILE")
	}

	url := serverURL
	cid := clientID
	csecret := clientSecret

	// Environment variable fallbacks (Protect-specific)
	if url == "" {
		url = os.Getenv("JAMFPROTECT_URL")
	}
	if cid == "" {
		cid = os.Getenv("JAMFPROTECT_CLIENT_ID")
	}
	if csecret == "" {
		csecret = os.Getenv("JAMFPROTECT_CLIENT_SECRET")
	}

	// Fill from config profile
	if p, _, err := config.GetProfile(cfg, profileName); err == nil {
		if url == "" {
			url = p.URL
		}
		if cid == "" && p.ClientID != "" {
			resolved, err := config.ResolveSecret(p.ClientID)
			if err != nil {
				return fmt.Errorf("resolving client-id from profile: %w", err)
			}
			cid = resolved
		}
		if csecret == "" && p.ClientSecret != "" {
			resolved, err := config.ResolveSecret(p.ClientSecret)
			if err != nil {
				return fmt.Errorf("resolving client-secret from profile: %w", err)
			}
			csecret = resolved
		}
	}

	// Also check generic env vars as fallback
	if url == "" {
		url = os.Getenv("JAMF_URL")
	}
	if cid == "" {
		cid = os.Getenv("JAMF_CLIENT_ID")
	}
	if csecret == "" {
		csecret = os.Getenv("JAMF_CLIENT_SECRET")
	}

	if url == "" {
		return exitcode.New(exitcode.Usage, "server URL is required: use --url, JAMFPROTECT_URL env var, or configure a protect profile")
	}
	if cid == "" || csecret == "" {
		return exitcode.New(exitcode.Usage, "client-id and client-secret are required for Jamf Protect: use JAMFPROTECT_CLIENT_ID/JAMFPROTECT_CLIENT_SECRET env vars, or configure a protect profile")
	}

	// Build a retryablehttp-backed HTTP client with a cookie jar so that:
	//   - sticky session affinity cookies (APBALANCEID) persist across requests
	//   - the SDK's retry behavior (3 retries, exponential backoff) is preserved
	// Injecting a plain *http.Client would replace the SDK's default retryablehttp
	// transport, so we reconstruct it here with the jar set on the inner client.
	jar, _ := cookiejar.New(nil)
	rc := retryablehttp.NewClient()
	rc.RetryMax = 3
	rc.RetryWaitMin = 1 * time.Second
	rc.RetryWaitMax = 30 * time.Second
	rc.Logger = nil
	rc.CheckRetry = retryablehttp.ErrorPropagatedRetryPolicy
	rc.HTTPClient.Timeout = 60 * time.Second
	rc.HTTPClient.Jar = jar

	stdClient := rc.StandardClient()
	if shouldShowSpinner() {
		stdClient.Transport = &spinnerTransport{inner: stdClient.Transport}
	}

	protectOpts := []jamfprotect.Option{
		jamfprotect.WithUserAgent("jamf-cli/" + cliVersion),
		jamfprotect.WithHTTPClient(stdClient),
	}
	if cacheDir, err := os.UserCacheDir(); err == nil {
		cache := &clearableProtectCache{dir: filepath.Join(cacheDir, "jamf-cli")}
		protectOpts = append(protectOpts, jamfprotect.WithTokenCache(cache))
		cliCtx.ClearProtectToken = cache.Clear
	}
	sdkClient := jamfprotect.NewClient(url, cid, csecret, protectOpts...)
	cliCtx.ProtectClient = sdkClient
	cliCtx.ProtectURL = url
	return nil
}

// resolveSchoolClient constructs a Jamf School SDK client from config/flags/env
// and assigns it to cliCtx.SchoolClient.
func resolveSchoolClient(cfg *config.Config, cliCtx *registry.CLIContext) error {
	profileName := profile
	if profileName == "" {
		profileName = os.Getenv("JAMF_PROFILE")
	}

	url := serverURL
	networkID := os.Getenv("JAMFSCHOOL_NETWORK_ID")
	apiKey := os.Getenv("JAMFSCHOOL_API_KEY")

	// Environment variable fallbacks (School-specific)
	if url == "" {
		url = os.Getenv("JAMFSCHOOL_URL")
	}

	// Platform API credentials (optional — enables blueprints + DDM reports)
	platformURL := os.Getenv("JAMFSCHOOL_PLATFORM_URL")
	cid := os.Getenv("JAMF_CLIENT_ID")
	csecret := os.Getenv("JAMF_CLIENT_SECRET")
	tid := os.Getenv("JAMF_TENANT_ID")

	// Fill from config profile
	if p, _, err := config.GetProfile(cfg, profileName); err == nil {
		if url == "" {
			url = p.URL
		}
		if networkID == "" && p.NetworkID != "" {
			resolved, err := config.ResolveSecret(p.NetworkID)
			if err != nil {
				return fmt.Errorf("resolving network-id from profile: %w", err)
			}
			networkID = resolved
		}
		if apiKey == "" && p.APIKey != "" {
			resolved, err := config.ResolveSecret(p.APIKey)
			if err != nil {
				return fmt.Errorf("resolving api-key from profile: %w", err)
			}
			apiKey = resolved
		}
		// Platform credentials from profile (env vars take precedence)
		if platformURL == "" && p.PlatformURL != "" {
			platformURL = p.PlatformURL
		}
		if cid == "" && p.ClientID != "" {
			resolved, err := config.ResolveSecret(p.ClientID)
			if err != nil {
				return fmt.Errorf("resolving client-id from profile: %w", err)
			}
			cid = resolved
		}
		if csecret == "" && p.ClientSecret != "" {
			resolved, err := config.ResolveSecret(p.ClientSecret)
			if err != nil {
				return fmt.Errorf("resolving client-secret from profile: %w", err)
			}
			csecret = resolved
		}
		if tid == "" && p.TenantID != "" {
			tid = p.TenantID
		}
	}

	// Also check generic env vars as fallback
	if url == "" {
		url = os.Getenv("JAMF_URL")
	}

	if url == "" {
		return exitcode.New(exitcode.Usage, "server URL is required: use --url, JAMFSCHOOL_URL env var, or configure a school profile")
	}
	if networkID == "" || apiKey == "" {
		return exitcode.New(exitcode.Usage, "network-id and api-key are required for Jamf School: use JAMFSCHOOL_NETWORK_ID/JAMFSCHOOL_API_KEY env vars, or configure a school profile")
	}

	// Build a retryablehttp-backed HTTP client for retry behavior.
	jar, _ := cookiejar.New(nil)
	rc := retryablehttp.NewClient()
	rc.RetryMax = 3
	rc.RetryWaitMin = 1 * time.Second
	rc.RetryWaitMax = 30 * time.Second
	rc.Logger = nil
	rc.CheckRetry = retryablehttp.ErrorPropagatedRetryPolicy
	rc.HTTPClient.Timeout = 60 * time.Second
	rc.HTTPClient.Jar = jar

	stdClient := rc.StandardClient()
	if shouldShowSpinner() {
		stdClient.Transport = &spinnerTransport{inner: stdClient.Transport}
	}

	schoolOpts := []jamfschool.Option{
		jamfschool.WithUserAgent("jamf-cli/" + cliVersion),
		jamfschool.WithHTTPClient(stdClient),
	}
	cliCtx.SchoolClient = jamfschool.NewClient(url, networkID, apiKey, schoolOpts...)

	// When platform credentials are present, also construct the Platform SDK
	// client for blueprint and DDM report commands.
	if platformURL != "" && cid != "" && csecret != "" && tid != "" {
		// School reaches the Platform API for blueprints and DDM reports only;
		// Security Cloud is not part of that surface, so it has no tenant here.
		sdk, err := newPlatformSDKClient(
			platformURL, cid, csecret, auth.TenantScope(tid),
			shouldShowSpinner(),
		)
		if err != nil {
			return err
		}
		cliCtx.PlatformSDKClient = sdk
	}

	return nil
}

// resolveSecurityClient constructs a Jamf Security Cloud client from
// config/flags/env and assigns it to cliCtx.SecurityClient. Unlike Pro/
// Protect/School, Security Cloud has one shared production host
// (api.wandera.com) across all customers — tenancy is carried inside the
// login JWT's customer_id claim, not the URL — so a URL is not required
// here: security.NewClient falls back to its own defaults (api.wandera.com /
// sse.jamf.com) when both are empty.
//
// Also unlike those products, Security provisions a separate application
// ID/secret per API (Risk, Device Lifecycle, SSE) — any subset may be
// configured, and only commands that touch an unconfigured API fail (with a
// "run security setup" hint), rather than failing here for the whole product.
func resolveSecurityClient(cfg *config.Config, cliCtx *registry.CLIContext) error {
	profileName := profile
	if profileName == "" {
		profileName = os.Getenv("JAMF_PROFILE")
	}

	url := serverURL
	if url == "" {
		url = os.Getenv("JAMFSECURITY_URL")
	}
	sseURL := os.Getenv("JAMFSECURITY_SSE_URL")

	riskID, riskSecret := os.Getenv("JAMFSECURITY_RISK_CLIENT_ID"), os.Getenv("JAMFSECURITY_RISK_CLIENT_SECRET")
	lifecycleID, lifecycleSecret := os.Getenv("JAMFSECURITY_LIFECYCLE_CLIENT_ID"), os.Getenv("JAMFSECURITY_LIFECYCLE_CLIENT_SECRET")
	sseID, sseSecret := os.Getenv("JAMFSECURITY_SSE_CLIENT_ID"), os.Getenv("JAMFSECURITY_SSE_CLIENT_SECRET")

	// Fill any still-empty values from the config profile.
	if p, _, err := config.GetProfile(cfg, profileName); err == nil {
		if url == "" {
			url = p.URL
		}
		if sseURL == "" {
			sseURL = p.SSEURL
		}

		var resolveErr error
		fillPair := func(scope string, id, secret *string, profileID, profileSecret string) {
			if resolveErr != nil {
				return
			}
			// Treat id/secret as an atomic pair: if one half already came
			// from the environment and the other would be backfilled from
			// the profile, refuse rather than risk splicing together a
			// mismatched credential pair that surfaces as an opaque 401.
			idFromEnv, secretFromEnv := *id != "", *secret != ""
			if idFromEnv && !secretFromEnv && profileSecret != "" {
				resolveErr = fmt.Errorf("JAMFSECURITY_%s_CLIENT_ID is set via env but the client secret would come from the config profile; set both via env or neither", scope)
				return
			}
			if secretFromEnv && !idFromEnv && profileID != "" {
				resolveErr = fmt.Errorf("JAMFSECURITY_%s_CLIENT_SECRET is set via env but the client ID would come from the config profile; set both via env or neither", scope)
				return
			}
			if *id == "" && profileID != "" {
				resolved, err := config.ResolveSecret(profileID)
				if err != nil {
					resolveErr = fmt.Errorf("resolving credential from profile: %w", err)
					return
				}
				*id = resolved
			}
			if *secret == "" && profileSecret != "" {
				resolved, err := config.ResolveSecret(profileSecret)
				if err != nil {
					resolveErr = fmt.Errorf("resolving credential from profile: %w", err)
					return
				}
				*secret = resolved
			}
		}
		fillPair("RISK", &riskID, &riskSecret, p.RiskClientID, p.RiskClientSecret)
		fillPair("LIFECYCLE", &lifecycleID, &lifecycleSecret, p.LifecycleClientID, p.LifecycleClientSecret)
		fillPair("SSE", &sseID, &sseSecret, p.SSEClientID, p.SSEClientSecret)
		if resolveErr != nil {
			return resolveErr
		}
	}

	// Part of Jamf Security Cloud is served on the platform gateway
	// (/api/securitycloud — DNS, ZTNA, content categories, device groups, UEM
	// Connect) rather than on api.wandera.com, and reached with platform
	// client-credentials plus a Security Cloud tenant ID instead of the scoped
	// pairs above. A profile may carry either set or both, so this is resolved
	// independently and neither half is required.
	if err := checkScopeConflict(cfg, profileName); err != nil {
		return err
	}
	platformSDK, err := securityPlatformSDKClient(cfg, profileName)
	if err != nil {
		return err
	}
	cliCtx.PlatformSDKClient = platformSDK

	if riskID == "" && lifecycleID == "" && sseID == "" && cliCtx.PlatformSDKClient == nil {
		return exitcode.New(exitcode.Usage, "no Jamf Security Cloud credentials configured: run 'jamf-cli security setup', or set JAMFSECURITY_RISK_CLIENT_ID/SECRET, JAMFSECURITY_LIFECYCLE_CLIENT_ID/SECRET, and/or JAMFSECURITY_SSE_CLIENT_ID/SECRET env vars. For the gateway-served commands (dns-*, ztna-*, content-categories, device-groups, uem-*) configure a platform profile: 'jamf-cli config add-profile <name> --auth-method platform --tenant-id <id>'")
	}

	if strings.HasPrefix(url, "http://") || strings.HasPrefix(sseURL, "http://") {
		fmt.Fprintln(os.Stderr, "WARNING: using HTTP (not HTTPS) — credentials will be sent in plaintext")
	}

	stdClient := &http.Client{Timeout: 60 * time.Second}
	if shouldShowSpinner() {
		stdClient.Transport = &spinnerTransport{inner: http.DefaultTransport}
	}

	cliCtx.SecurityClient = security.NewClient(
		security.WithUserAgent("jamf-cli/"+cliVersion),
		security.WithHTTPClient(stdClient),
		security.WithAPIBaseURL(url),
		security.WithSSEBaseURL(sseURL),
		security.WithRiskCredentials(riskID, riskSecret),
		security.WithLifecycleCredentials(lifecycleID, lifecycleSecret),
		security.WithSSECredentials(sseID, sseSecret),
	)
	return nil
}

// FormatError writes a structured JSON error to stdout when the output format
// is "json". Returns true if the error was handled, false otherwise (caller
// should fall back to plain stderr).
func FormatError(err error) bool {
	return formatErrorTo(os.Stdout, err)
}

// errorFormat answers which format an error should be rendered in.
//
// resolved is outputFmt, empty until PersistentPreRunE fills it. Cobra
// validates args and parses flags before that runs, so errors from those two
// paths arrive unresolved, and this reproduces the answer PersistentPreRunE
// would have given. Without it a piped run got the envelope for a RunE error
// and plain text for an Args refusal: two answers to one question, which is
// worse than either alone.
//
// Split out from formatErrorTo because the interesting input is whether stdout
// is a terminal, and a test writing to a pipe can never be one.
//
// The profile's default-output is deliberately not consulted. Loading config on
// the path that prints an error is not worth it, and it differs only for a
// profile that pins a format while on a terminal.
func errorFormat(resolved string, stdoutTTY, hasOutFile bool) string {
	if resolved != "" {
		return resolved
	}
	return output.ResolveFormat(false, "", "", stdoutTTY, hasOutFile)
}

// formatErrorTo writes the JSON error envelope to w when output is "json",
// including the remediation hint and any structured details when present.
func formatErrorTo(w io.Writer, err error) bool {
	if errorFormat(outputFmt, output.IsTerminal(os.Stdout.Fd()), outFile != "") != "json" {
		return false
	}
	code := exitcode.CodeFrom(err)
	envelope := map[string]any{
		"error":        exitcode.CodeName(code),
		"message":      err.Error(),
		"exitCode":     code,
		"exitCodeName": exitcode.CodeName(code),
	}
	var e *exitcode.Error
	if errors.As(err, &e) {
		if e.Hint != "" {
			envelope["hint"] = e.Hint
		}
		for k, v := range e.Details {
			envelope[k] = v
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(envelope); err != nil {
		return false // stdout broken (e.g. SIGPIPE); fall back to stderr
	}
	return true
}

// FprintError writes a human-facing error (and a "hint:" line when present) to
// w. Used by main when the JSON envelope path does not apply.
func FprintError(w io.Writer, err error) {
	_, _ = fmt.Fprintln(w, err)
	var e *exitcode.Error
	if errors.As(err, &e) && e.Hint != "" {
		_, _ = fmt.Fprintf(w, "hint: %s\n", e.Hint)
	}
}

// ClassifyError normalizes framework errors that carry no explicit exit code so
// they map to the documented codes. Cobra's flag-group and required-flag
// validators report a wrong invocation as a plain error, which defaults to exit
// 1; classify those as a usage error (2) to match the unknown-flag path handled
// by SetFlagErrorFunc. Errors that already carry an exit code pass through
// unchanged — including the unknown-flag errors and every argument-count error,
// which classifyArgsErrors codes at its own call site.
//
// Only two prefixes were listed at first, so `pro backup` with no --output
// exited 1 while `pro backup --nosuchflag` exited 2, the two halves of one
// mistake answering differently on every command in the CLI that calls
// MarkFlagRequired. Exit 1 is this CLI's generic failure, so a wrapper could not
// tell "you invoked it wrong" from "the request failed", which is the whole
// distinction exitcode.Usage exists to draw.
func ClassifyError(err error) error {
	if err == nil {
		return nil
	}
	var e *exitcode.Error
	if errors.As(err, &e) {
		return err
	}
	for _, p := range usageErrorPrefixes {
		if strings.HasPrefix(err.Error(), p) {
			return exitcode.Wrap(exitcode.Usage, err)
		}
	}
	return err
}

// Cobra's own format literals for an invocation that is wrong rather than a
// request that failed. Prefix matching is forced, not chosen. Cobra returns
// these as plain errors with no type to assert on, and SetFlagErrorFunc is
// reached only from ParseFlags, never from ValidateRequiredFlags or
// ValidateFlagGroups. Each const is cobra's literal verbatim, so an upstream
// reword fails TestUsageErrorPrefixesMatchCobrasOwnMessages by name instead of
// silently returning those invocations to exit 1.
//
// Argument counts are deliberately not on this list. classifyArgsErrors codes
// them where cobra calls the validator, so the two prefixes they used to need,
// "accepts " and "requires at least ", are gone entirely. Both matched a
// user-supplied path at character zero: `pro packages upload --file "accepts x"`
// and `protect backup --output "requires at least a dir"` reported a plain
// failure as exit 2, while the same commands reported the identical failure as
// exit 1 for a directory named anything else.
//
// So a candidate prefix has to be judged against the messages this repository
// can assemble, not against the literals it declares. errors.As returns early
// for anything internal/client built, which leaves only errors from this
// repository's own Go code — and several of those interpolate a caller-supplied
// path or URL at character zero (grep `Errorf("%[sv]`; a `%q` site cannot
// collide, its first character being a quote). A prefix is safe when landing on
// it at character zero would take a whole cobra clause reproduced verbatim, as
// all four below would. A prefix that opens on one ordinary English word is not
// safe however long the rest of its literal is.
//
// `invalid argument ` (cobra's OnlyValidArgs) is absent for a third reason. No
// command here sets ValidArgs, and syscall.EINVAL.Error() is exactly "invalid
// argument", so the prefix would match an OS error verbatim. pflag's own
// invalid-argument error already reaches exit 2 through SetFlagErrorFunc.
const (
	usagePrefixUnknownCommand = "unknown command"
	usagePrefixRequiredFlags  = "required flag(s)"
	// Covers MarkFlagsRequiredTogether and MarkFlagsMutuallyExclusive both,
	// because cobra opens the two messages with the same clause.
	usagePrefixFlagGroup      = "if any flags in the group ["
	usagePrefixFlagGroupOneOf = "at least one of the flags in the group ["
)

var usageErrorPrefixes = []string{
	usagePrefixUnknownCommand,
	usagePrefixRequiredFlags,
	usagePrefixFlagGroup,
	usagePrefixFlagGroupOneOf,
}

// asUsageError codes err as a usage error unless it already carries a code.
//
// exitcode.Wrap builds a fresh *exitcode.Error around whatever it is handed and
// CodeFrom reads the outermost one, so wrapping an already-coded error would
// overwrite its code with Usage. The guard is what makes the wrap idempotent.
func asUsageError(err error) error {
	var e *exitcode.Error
	if err == nil || errors.As(err, &e) {
		return err
	}
	return exitcode.Wrap(exitcode.Usage, err)
}

// classifyArgsErrors wraps every positional-argument validator in the tree so a
// wrong argument count carries exitcode.Usage from the one place cobra reports
// it, command.go's single `c.Args(c, args)` call site.
//
// The alternative was two more entries in usageErrorPrefixes, and those two had
// to be "accepts " and "requires at least " — broad enough to match a
// user-supplied path at character zero, which is documented above the const
// block. Coding the validator instead removes the guess: the error is known to
// be an argument-count error because of where it came from, not because of how
// it reads.
//
// A command whose Args is nil must stay nil. Find consults that field to decide
// whether to run legacyArgs, which is what rejects an unknown command at the
// root and what guardUnknownSubcommands leaves in place for every group parent
// below it; assigning a validator to a nil field would silently switch that off.
//
// Cobra adds three commands of its own after this walk, from ExecuteC: the help
// command and the default completion command (which this CLI pre-empts with its
// own) both leave Args nil, so neither is affected. __complete sets
// MinimumNArgs(1) and so keeps exit 1 for a malformed shell-completion probe,
// which no shell reads — completion is consumed from stdout.
func classifyArgsErrors(cmd *cobra.Command) {
	for _, c := range cmd.Commands() {
		classifyArgsErrors(c)
	}
	if cmd.Args == nil {
		return
	}
	declared := cmd.Args
	cmd.Args = func(c *cobra.Command, args []string) error {
		return asUsageError(declared(c, args))
	}
}

// noAuthAnnotation marks a single command that calls no API, so PersistentPreRunE
// skips auth for it. An annotation travels with the one command it is set on,
// which a third name map could not: matching a name is what makes chainSkip a
// silent auth bypass for any other command that happens to share it, and what
// rootOnlySkip exists to contain.
const noAuthAnnotation = "jamf:no-auth"

// groupParentAnnotation marks a parent command that guardUnknownSubcommands made
// runnable solely to reject unknown subcommands. PersistentPreRunE skips auth for
// these — a group parent never calls an API itself.
const groupParentAnnotation = "jamfcli/group-parent"

// guardUnknownSubcommands makes every non-root group parent reject an unknown
// subcommand with a "did you mean" hint and a usage exit code. Cobra applies
// this only to the root command (via legacyArgs in Find); a child parent would
// otherwise silently print help and exit 0.
//
// We attach a RunE rather than an Args validator because cobra short-circuits a
// non-runnable command to help before arguments are ever validated, so an Args
// validator on a pure group never runs. Making the parent runnable routes it
// through RunE, where args are available — and the annotation lets the auth
// pre-run skip it.
func guardUnknownSubcommands(cmd *cobra.Command) {
	for _, c := range cmd.Commands() {
		guardUnknownSubcommands(c)
	}
	if !cmd.HasParent() || !cmd.HasSubCommands() || cmd.Runnable() {
		return
	}
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[groupParentAnnotation] = "true"
	cmd.RunE = func(c *cobra.Command, args []string) error {
		if len(args) == 0 {
			return c.Help() // bare parent (e.g. `pro buildings`) shows help
		}
		return unknownSubcommandError(c, args[0])
	}
}

// refuseStrayArgs rejects any positional argument with the same message builder
// and usage exit code guardUnknownSubcommands gives a group parent. It covers
// the case that guard cannot: a command that is already runnable, which cobra
// routes straight to RunE with the stray positional silently discarded. `pro
// backup` and `pro diff` both take their whole input as flags, and `pro backup`
// owns a subcommand, so a subcommand typo there would otherwise start a full
// backup.
//
// A runnable command must NOT be given groupParentAnnotation instead:
// PersistentPreRunE reads that annotation to skip auth, so annotating one that
// calls an API would run it with a nil client.
func refuseStrayArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	return unknownSubcommandError(cmd, args[0])
}

// unknownSubcommandError reports arg as an unresolvable positional of cmd, in
// the exit code cobra's own root-level handling uses.
//
// The wording follows whether cmd owns subcommands, because "unknown command"
// is only true when there was a command to get wrong. `pro diff` owns none and
// takes its whole input as flags, so reporting a stray positional as an unknown
// command read as though diff were a command group, when the mistake is almost
// always a missing --source or --target.
//
// Either wording then names any required flag that was not supplied, and
// carries a hint saying where to find the flags — #360's tree-wide guard holds
// every zero-arity leaf to that, and two wordings for one mistake is what it
// exists to prevent.
func unknownSubcommandError(cmd *cobra.Command, arg string) error {
	var msg, hint, suggestions string
	if cmd.HasSubCommands() {
		// SuggestionsFor reads this directly with no default, and a child
		// command leaves it at 0, which suppresses all but exact matches.
		// Cobra's own findSuggestions defaults it the same way at the same
		// point.
		if cmd.SuggestionsMinimumDistance <= 0 {
			cmd.SuggestionsMinimumDistance = 2
		}
		msg = fmt.Sprintf("unknown command %q for %q", arg, cmd.CommandPath())
		hint = fmt.Sprintf("run %s --help to list its subcommands", cmd.CommandPath())
		if s := cmd.SuggestionsFor(arg); len(s) > 0 {
			suggestions = "\n\nDid you mean this?\n\t" + strings.Join(s, "\n\t")
		}
	} else {
		msg = fmt.Sprintf("%q takes no positional arguments, but got %q", cmd.CommandPath(), arg)
		hint = fmt.Sprintf("run %s --help for the flags it accepts", cmd.CommandPath())
	}
	// Cobra runs ValidateArgs before ValidateRequiredFlags, so this refusal
	// pre-empts the required-flag error and would otherwise be the last word.
	// Its own exported validator states the missing set, in the wording the CLI
	// already prints for a missing required flag everywhere else — so `pro diff
	// staging production` names --source and --target again. Re-deriving the
	// rule here meant copying cobra's completion-annotation marker, which is
	// internal in spirit and would name nothing if cobra ever changed it.
	if err := cmd.ValidateRequiredFlags(); err != nil {
		msg += " (" + err.Error() + ")"
	}
	return &exitcode.Error{Code: exitcode.Usage, Message: msg + suggestions, Hint: hint}
}

// suggestFlag returns the closest known flag name to unknown, or "" when none
// is a plausible match. It anchors on a shared first letter and keeps the edit
// distance small relative to the typo length, so a short typo resolves to the
// intended flag (--fld -> --field) rather than an unrelated one at the same
// distance (--all), and a typo with no real match (--id) yields no hint at all.
func suggestFlag(unknown string, known []string) string {
	if unknown == "" {
		return ""
	}
	best, bestDist := "", 0
	for _, k := range known {
		if k == "" || k[0] != unknown[0] {
			continue
		}
		d := levenshtein(unknown, k)
		if d > 2 || d >= len(unknown) {
			continue
		}
		if best == "" || d < bestDist {
			best, bestDist = k, d
		}
	}
	return best
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := []int{i}
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur = append(cur, min(min(prev[j]+1, cur[j-1]+1), prev[j-1]+cost))
		}
		prev = cur
	}
	return prev[len(b)]
}
