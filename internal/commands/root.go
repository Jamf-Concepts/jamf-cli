package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/client"
	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/spinner"
)

// Global flags
var (
	profile           string
	outputFmt         string
	quiet             bool
	verbose           bool
	noInput           bool
	noColor           bool
	dryRun            bool
	wide              bool
	outFile           string
	fieldName         string
	serverURL         string
	token             string
	tokenFile         string
	tokenStdin        bool
	clientID          string
	clientSecret      string
	clientSecretStdin bool
	tenantID          string
	cliVersion        string // set by NewRootCmd for use by power commands
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
	if fieldName == "" {
		return o.Formatter.PrintRaw(data)
	}

	// Parse JSON and extract the named field
	var parsed interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("cannot extract field from non-JSON response")
	}

	var objects []map[string]interface{}
	switch v := parsed.(type) {
	case []interface{}:
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				objects = append(objects, m)
			}
		}
	case map[string]interface{}:
		objects = []map[string]interface{}{v}
	default:
		return fmt.Errorf("cannot extract field %q from scalar value", fieldName)
	}

	for _, obj := range objects {
		val, ok := obj[fieldName]
		if !ok {
			continue
		}
		if _, err := fmt.Fprintln(os.Stdout, output.FormatValue(val)); err != nil {
			return err
		}
	}
	return nil
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

	// Config profile: fill remaining gaps
	if len(cfg.Profiles) > 0 {
		p, _, err := config.GetProfile(cfg, profileName)
		if err == nil {
			if url == "" {
				url = p.URL
			}
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

	// Validate
	if url == "" {
		return "", nil, exitcode.New(exitcode.Usage, "server URL is required: use --url, JAMF_URL env var, or jamf-cli config add-profile")
	}
	if strings.HasPrefix(url, "http://") {
		fmt.Fprintln(os.Stderr, "WARNING: using HTTP (not HTTPS) — credentials will be sent in plaintext")
	}
	if (cid != "") != (csecret != "") {
		if cid != "" {
			return "", nil, exitcode.New(exitcode.Usage, "--client-secret is required when --client-id is provided")
		}
		return "", nil, exitcode.New(exitcode.Usage, "--client-id is required when --client-secret is provided")
	}

	// Platform gateway auth: requires client credentials + tenant ID
	if isPlatform || tid != "" {
		if cid == "" || csecret == "" {
			return "", nil, exitcode.New(exitcode.Usage, "--client-id and --client-secret are required for platform gateway auth")
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
		return "", nil, exitcode.New(exitcode.Usage, "authentication required: use --client-id/--client-secret, --token, JAMF_TOKEN/JAMF_CLIENT_ID env vars, or jamf-cli config add-profile")
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

	// Token from file or stdin (before delegating to ResolveAuthForProfile
	// which does not handle stdin)
	if token == "" && tokenFile != "" {
		data, err := os.ReadFile(tokenFile)
		if err != nil {
			return "", nil, fmt.Errorf("reading token file %s: %w", tokenFile, err)
		}
		token = strings.TrimSpace(string(data))
	}
	if tokenStdin && clientSecretStdin {
		return "", nil, exitcode.New(exitcode.Usage, "--token-stdin and --client-secret-stdin are mutually exclusive (both read from stdin)")
	}
	if token == "" && tokenStdin {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
		if err != nil {
			return "", nil, fmt.Errorf("reading token from stdin: %w", err)
		}
		token = strings.TrimSpace(string(data))
	}
	if clientSecret == "" && clientSecretStdin {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
		if err != nil {
			return "", nil, fmt.Errorf("reading client secret from stdin: %w", err)
		}
		clientSecret = strings.TrimSpace(string(data))
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

func NewRootCmd(version, commit, date string) *cobra.Command {
	cliVersion = version
	// CLIContext is populated in PersistentPreRunE after token/URL resolution
	cliCtx := &registry.CLIContext{}
	var outFileHandle *os.File

	cmd := &cobra.Command{
		Use:   "jamf-cli",
		Short: "CLI for the Jamf platform",
		Long: `jamf-cli is a command-line interface for the Jamf platform.

Use "jamf-cli pro" for Jamf Pro commands (device management, inventory,
configuration, reporting, and API automation).`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Respect NO_COLOR env var (https://no-color.org)
			if _, ok := os.LookupEnv("NO_COLOR"); ok {
				noColor = true
			}

			// Skip for completion, help, version, config, and commands
			skipCommands := map[string]bool{
				"completion": true,
				"help":       true,
				"version":    true,
				"config":     true,
				"commands":   true,
				"diff":       true,
				"setup":      true,
			}
			for c := cmd; c != nil; c = c.Parent() {
				if skipCommands[c.Name()] {
					return nil
				}
			}

			// Load config and resolve auth
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			resolvedURL, authProvider, err := resolveAuth(cfg)
			if err != nil {
				return err
			}

			// Apply default output from config if flag not explicitly set
			if !cmd.Flags().Changed("output") && cfg.DefaultOutput != "" {
				outputFmt = cfg.DefaultOutput
			}

			// Build HTTP client with decorators
			clientOpts := []client.Option{client.WithVerbose(verbose)}
			if p, ok := authProvider.(*auth.PlatformOAuth2Provider); ok {
				clientOpts = append(clientOpts, client.WithTenantID(p.TenantID()))
			}
			var httpClient registry.HTTPClient = &cliClient{client.New(resolvedURL, authProvider, clientOpts...)}
			if dryRun {
				httpClient = &dryRunClient{inner: httpClient}
			}
			if !quiet && !verbose {
				httpClient = &spinnerClient{inner: httpClient}
			}
			cliCtx.Client = httpClient

			// Build output formatter
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
			cliCtx.Output = &cliOutput{formatter}

			return nil
		},
		PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
			if outFileHandle != nil {
				return outFileHandle.Close()
			}
			return nil
		},
	}

	// Global flags
	cmd.PersistentFlags().StringVarP(&profile, "profile", "p", "", "config profile to use (or JAMF_PROFILE env)")
	cmd.PersistentFlags().StringVarP(&outputFmt, "output", "o", "json", "output format: table, json, csv, yaml, plain")
	cmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "suppress non-error output")
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "show debug info")
	cmd.PersistentFlags().BoolVar(&noInput, "no-input", false, "never prompt; fail if input required")
	cmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable colored output")
	cmd.PersistentFlags().BoolVarP(&dryRun, "dry-run", "n", false, "preview changes without executing")
	cmd.PersistentFlags().BoolVarP(&wide, "wide", "w", false, "show all columns in table output")
	cmd.PersistentFlags().StringVar(&outFile, "out-file", "", "write output to file instead of stdout")
	cmd.PersistentFlags().StringVar(&fieldName, "field", "", "extract a single field from JSON response (e.g., --field id)")

	// Connection flags
	cmd.PersistentFlags().StringVar(&serverURL, "url", "", "Jamf Pro server URL (or JAMF_URL env)")
	cmd.PersistentFlags().StringVar(&token, "token", "", "API token (visible in ps; prefer JAMF_TOKEN env or --token-stdin)")
	cmd.PersistentFlags().StringVar(&tokenFile, "token-file", "", "path to file containing API token")
	cmd.PersistentFlags().BoolVar(&tokenStdin, "token-stdin", false, "read API token from stdin")
	cmd.PersistentFlags().StringVar(&clientID, "client-id", "", "OAuth2 client ID (visible in ps; prefer JAMF_CLIENT_ID env)")
	cmd.PersistentFlags().StringVar(&clientSecret, "client-secret", "", "OAuth2 client secret (visible in ps; prefer JAMF_CLIENT_SECRET env or --client-secret-stdin)")
	cmd.PersistentFlags().BoolVar(&clientSecretStdin, "client-secret-stdin", false, "read OAuth2 client secret from stdin")
	cmd.PersistentFlags().StringVar(&tenantID, "tenant-id", "", "Jamf Pro tenant ID for platform gateway auth (or JAMF_TENANT_ID env)")

	// Version command
	cmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("jamf-cli %s\n", version)
			fmt.Printf("  commit: %s\n", commit)
			fmt.Printf("  built:  %s\n", date)
		},
	})

	// Config command group
	cmd.AddCommand(newConfigCmd())

	// Completion command
	cmd.AddCommand(newCompletionCmd())

	// Commands discovery subcommand
	cmd.AddCommand(newCommandsCmd(cmd))

	// Jamf Pro product namespace
	cmd.AddCommand(newProCmd(cliCtx))

	// Apply root-level aliases and groups for --help output
	applyRootAliases(cmd)
	applyRootGroups(cmd)

	return cmd
}

// commandEntry represents a single leaf command for structured output.
type commandEntry struct {
	Command     string   `json:"command"`
	Description string   `json:"description"`
	Aliases     []string `json:"aliases,omitempty"`
	Flags       []string `json:"flags,omitempty"`
}

// newCommandsCmd creates the "commands" subcommand that outputs the full
// command tree in a machine-readable format.
func newCommandsCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "commands",
		Short: "List all available commands",
		Long:  `List all available commands in a structured format for discovery by scripts and AI agents.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries := collectCommands(root, "")
			formatter := output.New(outputFmt, noColor, wide)
			// Structured formats always get full detail; table/plain
			// show only command+description unless --wide is set.
			full := wide || outputFmt == "json" || outputFmt == "yaml" || outputFmt == "csv"
			return formatter.Print(commandEntriesToMaps(entries, full))
		},
	}
}

// collectCommands recursively walks the command tree and returns leaf commands.
func collectCommands(cmd *cobra.Command, prefix string) []commandEntry {
	var entries []commandEntry
	for _, child := range cmd.Commands() {
		if child.Hidden || child.Name() == "help" || child.Name() == "commands" {
			continue
		}

		fullPath := child.Name()
		if prefix != "" {
			fullPath = prefix + " " + child.Name()
		}

		// Leaf command: has RunE or Run
		if child.RunE != nil || child.Run != nil {
			entry := commandEntry{
				Command:     fullPath,
				Description: child.Short,
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
			entries = append(entries, collectCommands(child, fullPath)...)
		}
	}
	return entries
}

// commandEntriesToMaps converts command entries to the []map[string]interface{}
// format expected by the output formatter. When full is true, aliases and flags
// columns are included; otherwise only command and description are emitted
// for a compact table.
func commandEntriesToMaps(entries []commandEntry, full bool) []map[string]interface{} {
	result := make([]map[string]interface{}, len(entries))
	for i, e := range entries {
		m := map[string]interface{}{
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
		}
		result[i] = m
	}
	return result
}

// FormatError writes a structured JSON error to stdout when the output format
// is "json". Returns true if the error was handled, false otherwise (caller
// should fall back to plain stderr).
func FormatError(err error) bool {
	if outputFmt != "json" {
		return false
	}
	code := exitcode.CodeFrom(err)
	envelope := map[string]interface{}{
		"error":    exitcode.CodeName(code),
		"message":  err.Error(),
		"exitCode": code,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(envelope); err != nil {
		return false // stdout broken (e.g. SIGPIPE); fall back to stderr
	}
	return true
}
