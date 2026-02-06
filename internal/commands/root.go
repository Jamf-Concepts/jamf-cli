package commands

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ktn-jamf/jamfpro-cli/internal/auth"
	"github.com/ktn-jamf/jamfpro-cli/internal/client"
	"github.com/ktn-jamf/jamfpro-cli/internal/commands/generated"
	"github.com/ktn-jamf/jamfpro-cli/internal/config"
	"github.com/ktn-jamf/jamfpro-cli/internal/output"
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
	serverURL    string
	token        string
	tokenFile    string
	tokenStdin   bool
	clientID     string
	clientSecret string
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

func NewRootCmd(version, commit, date string) *cobra.Command {
	// CLIContext is populated in PersistentPreRunE after token/URL resolution
	cliCtx := &generated.CLIContext{}

	cmd := &cobra.Command{
		Use:   "jamfpro-cli",
		Short: "CLI tool for Jamf Pro Server API automation",
		Long: `jamfpro-cli is a command-line interface for the Jamf Pro Server API.

It provides full API coverage for admin automation workflows including
device management, inventory/reporting, and configuration management.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Skip for completion, help, version, and config commands
			skipCommands := map[string]bool{
				"completion": true,
				"help":       true,
				"version":    true,
				"config":     true,
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
						return fmt.Errorf("basic authentication is not yet implemented")
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
				return fmt.Errorf("server URL is required: use --url, JAMF_URL env var, or jamfpro-cli config add-profile")
			}

			// Detect partial OAuth2 credentials
			if (clientID != "") != (clientSecret != "") {
				if clientID != "" {
					return fmt.Errorf("--client-secret is required when --client-id is provided")
				}
				return fmt.Errorf("--client-id is required when --client-secret is provided")
			}

			// Determine auth provider
			var authProvider auth.Provider
			if clientID != "" && clientSecret != "" {
				// OAuth2 client credentials
				authProvider = auth.NewOAuth2Provider(serverURL, clientID, clientSecret)
			} else if token != "" {
				// Static bearer token
				authProvider = auth.NewTokenProvider(token)
			} else {
				return fmt.Errorf("authentication required: use --client-id/--client-secret, --token, --token-file, JAMF_TOKEN/JAMF_CLIENT_ID env vars, or jamfpro-cli config add-profile")
			}

			// Create client and formatter now that auth is resolved
			cliCtx.Client = &cliClient{client.New(serverURL, authProvider, client.WithVerbose(verbose))}
			cliCtx.Output = &cliOutput{output.New(outputFmt, noColor, wide)}

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

	// Connection flags
	cmd.PersistentFlags().StringVar(&serverURL, "url", "", "Jamf Pro server URL (or JAMF_URL env)")
	cmd.PersistentFlags().StringVar(&token, "token", "", "API token (or JAMF_TOKEN env)")
	cmd.PersistentFlags().StringVar(&tokenFile, "token-file", "", "path to file containing API token")
	cmd.PersistentFlags().BoolVar(&tokenStdin, "token-stdin", false, "read API token from stdin")
	cmd.PersistentFlags().StringVar(&clientID, "client-id", "", "OAuth2 client ID (or JAMF_CLIENT_ID env)")
	cmd.PersistentFlags().StringVar(&clientSecret, "client-secret", "", "OAuth2 client secret (or JAMF_CLIENT_SECRET env)")

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

	// Register generated resource commands with CLIContext
	generated.RegisterCommands(cmd, cliCtx)

	return cmd
}
