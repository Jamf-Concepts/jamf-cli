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
	TenantID     string
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
					if tid == "" && p.TenantID != "" {
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

	// Platform gateway auth: requires client credentials + tenant ID
	if isPlatform || tid != "" {
		if cid == "" || csecret == "" {
			return "", nil, exitcode.New(exitcode.Usage, "client ID and client secret are required for platform gateway auth: set JAMF_CLIENT_ID/JAMF_CLIENT_SECRET env vars or use a config profile")
		}
		if tid == "" {
			return "", nil, exitcode.New(exitcode.Usage, "--tenant-id is required for platform gateway auth")
		}
		return url, auth.NewPlatformOAuth2Provider(url, cid, csecret, tid), nil
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
	if tenantID == "" {
		tenantID = os.Getenv("JAMF_TENANT_ID")
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
		Profile:      profile,
		ServerURL:    serverURL,
		Token:        token,
		TokenFile:    tokenFile,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TenantID:     tenantID,
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

Set JAMF_CLI_ARGS to prepend default flags to every invocation:
  export JAMF_CLI_ARGS='--quiet --no-input'
  export JAMF_CLI_ARGS='--profile "My CI Profile"'

Set JAMF_CLI_NO_HINTS=1 to suppress advisory hints while keeping the
spinner and progress output (narrower than --quiet).`,
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
			cliCtx.Output = &cliOutput{formatter}

			// Group parent commands (made runnable only to reject unknown
			// subcommands) never call an API; skip auth so `pro buildings`
			// help and typos work without credentials.
			if cmd.Annotations[groupParentAnnotation] == "true" {
				return nil
			}

			// Skip auth for commands that don't need it. Most are matched
			// anywhere in the chain (e.g. "config" covers all subcommands,
			// "setup" covers both "pro setup" and "protect setup").
			// "commands" is intentionally root-child-only: the root-level
			// "jamf-cli commands" listing command must be skipped, but
			// "pro mdm-commands commands" must NOT be skipped.
			chainSkip := map[string]bool{
				"completion":    true,
				"help":          true,
				"version":       true,
				"config":        true,
				"diff":          true,
				"setup":         true,
				"multi":         true,
				"doctor":        true,
				"mcp":           true,
				"agent-context": true,
			}
			for c := cmd; c != nil; c = c.Parent() {
				if chainSkip[c.Name()] {
					return nil
				}
				// "commands" only skips when it is a direct child of the root.
				if c.Name() == "commands" && c.Parent() != nil && c.Parent().Parent() == nil {
					return nil
				}
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
				clientOpts = append(clientOpts, client.WithTenantID(p.TenantID()))
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

			// When platform gateway auth is active, also construct the
			// Platform SDK client for platform-native commands (blueprints,
			// compliance-benchmarks, etc.). The SDK manages its own OAuth2
			// token lifecycle independently from the Pro HTTP client.
			if p, ok := authProvider.(*auth.PlatformOAuth2Provider); ok {
				cliCtx.PlatformSDKClient = newPlatformSDKClient(
					resolvedURL, p.ClientID(), p.ClientSecret(), p.TenantID(),
					shouldShowSpinner(),
				)
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
	cmd.PersistentFlags().StringVarP(&outputFmt, "output", "o", "json", "output format: table, json, csv, yaml, plain, xml (pretty), raw (classic commands default to xml)")
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
	cmd.PersistentFlags().StringVar(&tenantID, "tenant-id", "", "Jamf Pro tenant ID for platform gateway auth (or JAMF_TENANT_ID env)")
	cmd.PersistentFlags().BoolVar(&noVersionCheck, "no-version-check", false, "skip tenant version compatibility check (also: JAMF_NO_VERSION_CHECK env)")

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

	// Jamf Platform namespace
	cmd.AddCommand(newPlatformCmd(cliCtx))

	// Apply root-level aliases and groups for --help output
	applyRootAliases(cmd)
	applyRootGroups(cmd)

	// cobra only rejects unknown subcommands at the root; extend that to every
	// non-runnable parent so typos like `pro buildings lst` error with a hint
	// instead of silently printing help and exiting 0.
	guardUnknownSubcommands(cmd)

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
			full := wide || outputFmt == "json" || outputFmt == "yaml" || outputFmt == "csv"
			return formatter.Print(commandEntriesToMaps(entries, full))
		},
	}
}

// collectCommands recursively walks the command tree and returns leaf commands.
// product and group are inherited from parent context and updated as we descend.
func collectCommands(cmd *cobra.Command, prefix, product, group string) []commandEntry {
	var entries []commandEntry
	for _, child := range cmd.Commands() {
		if child.Hidden || child.Name() == "help" || child.Name() == "commands" {
			continue
		}

		fullPath := child.Name()
		if prefix != "" {
			fullPath = prefix + " " + child.Name()
		}

		// Determine product for this child's subtree.
		childProduct := product
		if child.Name() == "pro" || child.Name() == "protect" || child.Name() == "school" || child.Name() == "platform" {
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
			if len(e.Privileges) > 0 {
				m["privileges"] = e.Privileges
			}
		}
		result[i] = m
	}
	return result
}

// resolveProduct determines the product type from the profile. Returns "pro" or "protect".
// resolveProduct determines the product type. It checks the command hierarchy
// first (a command under "protect" is always protect), then falls back to the
// config profile's product field.
func resolveProduct(cmd *cobra.Command, cfg *config.Config) string {
	// Check command hierarchy: if any parent is a product, that's definitive
	for c := cmd; c != nil; c = c.Parent() {
		switch c.Name() {
		case "protect":
			return "protect"
		case "school":
			return "school"
		case "pro":
			return "pro"
		}
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
	default:
		return "pro"
	}
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
		cliCtx.PlatformSDKClient = newPlatformSDKClient(
			platformURL, cid, csecret, tid,
			shouldShowSpinner(),
		)
	}

	return nil
}

// FormatError writes a structured JSON error to stdout when the output format
// is "json". Returns true if the error was handled, false otherwise (caller
// should fall back to plain stderr).
func FormatError(err error) bool {
	return formatErrorTo(os.Stdout, err)
}

// formatErrorTo writes the JSON error envelope to w when output is "json",
// including the remediation hint and any structured details when present.
func formatErrorTo(w io.Writer, err error) bool {
	if outputFmt != "json" {
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
// they map to the documented codes. Cobra reports an unknown command as a plain
// error (default exit 1); classify it as a usage error (2) to match the
// unknown-flag path handled by SetFlagErrorFunc. Errors that already carry an
// exit code (including wrapped unknown-flag errors) pass through unchanged.
func ClassifyError(err error) error {
	if err == nil {
		return nil
	}
	var e *exitcode.Error
	if errors.As(err, &e) {
		return err
	}
	if strings.HasPrefix(err.Error(), "unknown command") {
		return exitcode.Wrap(exitcode.Usage, err)
	}
	return err
}

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
	// SuggestionsFor reads this directly with no default; child commands leave
	// it at 0, which would suppress all but exact matches.
	if cmd.SuggestionsMinimumDistance <= 0 {
		cmd.SuggestionsMinimumDistance = 2
	}
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[groupParentAnnotation] = "true"
	cmd.RunE = func(c *cobra.Command, args []string) error {
		if len(args) == 0 {
			return c.Help() // bare parent (e.g. `pro buildings`) shows help
		}
		msg := fmt.Sprintf("unknown command %q for %q", args[0], c.CommandPath())
		if s := c.SuggestionsFor(args[0]); len(s) > 0 {
			msg += "\n\nDid you mean this?\n\t" + strings.Join(s, "\n\t")
		}
		return exitcode.Wrap(exitcode.Usage, errors.New(msg))
	}
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
