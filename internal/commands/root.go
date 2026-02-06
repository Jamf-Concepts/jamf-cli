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

	"github.com/ktn-jamf/jamfpro-cli/internal/auth"
	"github.com/ktn-jamf/jamfpro-cli/internal/client"
	"github.com/ktn-jamf/jamfpro-cli/internal/commands/generated"
	"github.com/ktn-jamf/jamfpro-cli/internal/config"
	"github.com/ktn-jamf/jamfpro-cli/internal/exitcode"
	"github.com/ktn-jamf/jamfpro-cli/internal/output"
	"github.com/ktn-jamf/jamfpro-cli/internal/spinner"
)

// Global flags
var (
	profile      string
	outputFmt    string
	quiet        bool
	verbose      bool
	noInput      bool
	noColor      bool
	dryRun       bool
	wide         bool
	outFile      string
	serverURL    string
	token        string
	tokenFile    string
	tokenStdin   bool
	clientID     string
	clientSecret string
	username     string
	password     string
)

// cliClient wraps our client to implement generated.HTTPClient
type cliClient struct {
	*client.Client
}

func (c *cliClient) Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	return c.Client.Do(ctx, method, path, body)
}

// cliOutput wraps our output formatter to implement generated.OutputFormatter
type cliOutput struct {
	*output.Formatter
}

func (o *cliOutput) PrintResponse(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return o.Formatter.PrintRaw(body)
}

// spinnerClient wraps an HTTPClient to show a loading spinner during requests.
type spinnerClient struct {
	inner generated.HTTPClient
}

func (c *spinnerClient) Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	s := spinner.New("Loading...")
	s.Start()
	defer s.Stop()
	return c.inner.Do(ctx, method, path, body)
}

func NewRootCmd(version, commit, date string) *cobra.Command {
	// CLIContext is populated in PersistentPreRunE after token/URL resolution
	cliCtx := &generated.CLIContext{}
	var outFileHandle *os.File

	cmd := &cobra.Command{
		Use:   "jamfpro-cli",
		Short: "CLI tool for Jamf Pro Server API automation",
		Long: `jamfpro-cli is a command-line interface for the Jamf Pro Server API.

It provides full API coverage for admin automation workflows including
device management, inventory/reporting, and configuration management.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Skip for completion, help, version, config, and commands
			skipCommands := map[string]bool{
				"completion": true,
				"help":       true,
				"version":    true,
				"config":     true,
				"commands":   true,
			}
			// Check command and all parents
			for c := cmd; c != nil; c = c.Parent() {
				if skipCommands[c.Name()] {
					return nil
				}
			}

			// Environment variable fallback for server URL
			if serverURL == "" {
				serverURL = os.Getenv("JAMF_URL")
			}

			// Environment variable fallback for token
			if token == "" {
				token = os.Getenv("JAMF_TOKEN")
			}

			// Environment variable fallback for OAuth2 client credentials
			if clientID == "" {
				clientID = os.Getenv("JAMF_CLIENT_ID")
			}
			if clientSecret == "" {
				clientSecret = os.Getenv("JAMF_CLIENT_SECRET")
			}

			// Environment variable fallback for basic auth credentials
			if username == "" {
				username = os.Getenv("JAMF_USERNAME")
			}
			if password == "" {
				password = os.Getenv("JAMF_PASSWORD")
			}

			// Config profile fallback: fill gaps from profile
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			// Only attempt profile resolution if config has profiles
			if len(cfg.Profiles) > 0 {
				p, _, err := config.GetProfile(cfg, profile)
				if err == nil {
					// Fill in server URL from profile if not set by flag or env
					if serverURL == "" {
						serverURL = p.URL
					}

					// Handle auth method from profile
					switch p.AuthMethod {
					case "oauth2":
						if clientID == "" && p.ClientID != "" {
							resolved, err := config.ResolveSecret(p.ClientID)
							if err != nil {
								return fmt.Errorf("resolving client-id from profile: %w", err)
							}
							clientID = resolved
						}
						if clientSecret == "" && p.ClientSecret != "" {
							resolved, err := config.ResolveSecret(p.ClientSecret)
							if err != nil {
								return fmt.Errorf("resolving client-secret from profile: %w", err)
							}
							clientSecret = resolved
						}
					case "basic":
						if username == "" && p.Username != "" {
							username = p.Username
						}
						if password == "" && p.Password != "" {
							resolved, err := config.ResolveSecret(p.Password)
							if err != nil {
								return fmt.Errorf("resolving password from profile: %w", err)
							}
							password = resolved
						}
					default: // "token" or empty
						// Fill in token from profile if not set by flag or env
						if token == "" && p.Token != "" {
							resolved, err := config.ResolveSecret(p.Token)
							if err != nil {
								return fmt.Errorf("resolving token from profile: %w", err)
							}
							token = resolved
						}
					}
				}
				// If profile not found but was explicitly requested, error out
				if err != nil && profile != "" {
					return fmt.Errorf("loading profile: %w", err)
				}
			}

			// Apply default output from config if flag not explicitly set
			if !cmd.Flags().Changed("output") && cfg.DefaultOutput != "" {
				outputFmt = cfg.DefaultOutput
			}

			// Token file fallback: read token from a file
			if token == "" && tokenFile != "" {
				data, err := os.ReadFile(tokenFile)
				if err != nil {
					return fmt.Errorf("reading token file %s: %w", tokenFile, err)
				}
				token = strings.TrimSpace(string(data))
			}

			// Token stdin fallback: read token from stdin
			if token == "" && tokenStdin {
				data, err := io.ReadAll(os.Stdin)
				if err != nil {
					return fmt.Errorf("reading token from stdin: %w", err)
				}
				token = strings.TrimSpace(string(data))
			}

			// Validate we have server URL and some form of auth
			if serverURL == "" {
				return exitcode.New(exitcode.Usage, "server URL is required: use --url, JAMF_URL env var, or jamfpro-cli config add-profile")
			}

			// Detect partial OAuth2 credentials
			if (clientID != "") != (clientSecret != "") {
				if clientID != "" {
					return exitcode.New(exitcode.Usage, "--client-secret is required when --client-id is provided")
				}
				return exitcode.New(exitcode.Usage, "--client-id is required when --client-secret is provided")
			}

			// Determine auth provider
			var authProvider auth.Provider
			if clientID != "" && clientSecret != "" {
				// OAuth2 client credentials
				authProvider = auth.NewOAuth2Provider(serverURL, clientID, clientSecret)
			} else if token != "" {
				// Static bearer token
				authProvider = auth.NewTokenProvider(token)
			} else if username != "" && password != "" {
				// Basic auth token exchange
				authProvider = auth.NewBasicProvider(serverURL, username, password)
			} else {
				return exitcode.New(exitcode.Usage, "authentication required: use --client-id/--client-secret, --token, --username/--password, JAMF_TOKEN/JAMF_CLIENT_ID env vars, or jamfpro-cli config add-profile")
			}

			// Create client and formatter now that auth is resolved
			var httpClient generated.HTTPClient = &cliClient{client.New(serverURL, authProvider, client.WithVerbose(verbose))}

			// Wrap with spinner unless --quiet or --verbose suppresses it
			if !quiet && !verbose {
				httpClient = &spinnerClient{inner: httpClient}
			}

			cliCtx.Client = httpClient

			// Redirect output to file if --out-file is set (disables color)
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
	cmd.PersistentFlags().StringVarP(&profile, "profile", "p", "", "config profile to use")
	cmd.PersistentFlags().StringVarP(&outputFmt, "output", "o", "json", "output format: table, json, csv, yaml, plain")
	cmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "suppress non-error output")
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "show debug info")
	cmd.PersistentFlags().BoolVar(&noInput, "no-input", false, "never prompt; fail if input required")
	cmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable colored output")
	cmd.PersistentFlags().BoolVarP(&dryRun, "dry-run", "n", false, "preview changes without executing")
	cmd.PersistentFlags().BoolVarP(&wide, "wide", "w", false, "show all columns in table output")
	cmd.PersistentFlags().StringVar(&outFile, "out-file", "", "write output to file instead of stdout")

	// Connection flags
	cmd.PersistentFlags().StringVar(&serverURL, "url", "", "Jamf Pro server URL (or JAMF_URL env)")
	cmd.PersistentFlags().StringVar(&token, "token", "", "API token (or JAMF_TOKEN env)")
	cmd.PersistentFlags().StringVar(&tokenFile, "token-file", "", "path to file containing API token")
	cmd.PersistentFlags().BoolVar(&tokenStdin, "token-stdin", false, "read API token from stdin")
	cmd.PersistentFlags().StringVar(&clientID, "client-id", "", "OAuth2 client ID (or JAMF_CLIENT_ID env)")
	cmd.PersistentFlags().StringVar(&clientSecret, "client-secret", "", "OAuth2 client secret (or JAMF_CLIENT_SECRET env)")
	cmd.PersistentFlags().StringVar(&username, "username", "", "basic auth username (or JAMF_USERNAME env)")
	cmd.PersistentFlags().StringVar(&password, "password", "", "basic auth password (or JAMF_PASSWORD env)")

	// Version command
	cmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("jamfpro-cli %s\n", version)
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

	// Register generated resource commands with CLIContext
	generated.RegisterCommands(cmd, cliCtx)

	// Apply short aliases (e.g., "comp" for "computers")
	applyAliases(cmd)

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
	enc.Encode(envelope)
	return true
}
