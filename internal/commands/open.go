// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/browser"
	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// radarURL is the Jamf Security Cloud web console. Security Cloud has no
// per-tenant URL — tenancy lives in the JWT's customer_id claim — so unlike
// every other product this one is a constant and `security open` needs no
// credentials at all.
const radarURL = "https://radar.wandera.com/"

// proServerURLPath is the Jamf Pro API endpoint naming the instance's own web
// URL. It is only read on a gateway profile: an instance profile already holds
// the URL it authenticated against, and spending a request to be told what we
// dialled would make `pro open` fail for a reason unrelated to opening a
// browser. Served by the gateway (scope jss-url:read; "Read JSS URL" on a Jamf
// Pro API role).
const proServerURLPath = "/v1/jamf-pro-server-url"

// openLongTail documents the two behaviours a caller has to be able to predict:
// when the browser is skipped, and that the URL is always available as data.
const openLongTail = `

The URL is printed instead of opened when --print is passed, when stdin is
not interactive (--no-input), under --dry-run, or when stdout is not a
terminal — so
"jamf-cli ... open | pbcopy" and a CI job both give you the URL rather than
trying to launch a browser. Use -o json or --field url to consume it.

$BROWSER overrides the platform's default opener.`

// newProOpenCmd opens the Jamf Pro web interface.
//
// This is the one `open` that can need a request. It does not carry
// noAuthAnnotation for that reason: on a gateway profile the instance URL is
// not a credential input at all (a platform integration names a tenant, never
// a Jamf Pro host), so it has to be read from the API.
func newProOpenCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var printOnly bool
	cmd := &cobra.Command{
		Use:   "open",
		Short: "Open the Jamf Pro web interface in a browser",
		Long: `Open the Jamf Pro web interface in a browser.

On an instance profile (token or oauth2 auth) the configured URL is used and
no request is made. On a platform gateway profile the URL is read from the
Jamf Pro API, because a platform integration names a tenant rather than a
Jamf Pro host.` + openLongTail,
		Example: `  # Open Jamf Pro
  jamf-cli pro open

  # Print the URL instead
  jamf-cli pro open --print

  # Extract it for a script
  jamf-cli pro open -o json --field url`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			url, err := proWebURL(cmd.Context(), cliCtx)
			if err != nil {
				return err
			}
			return openOrPrint(cliCtx, url, printOnly)
		},
	}
	addPrintFlag(cmd, &printOnly)
	return cmd
}

// newProtectOpenCmd opens the Jamf Protect web interface, which is served at
// the root of the same host the API is on.
func newProtectOpenCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return newProfileURLOpenCmd(cliCtx, openTarget{
		product: "protect",
		label:   "Jamf Protect",
		envVars: []string{"JAMFPROTECT_URL", "JAMF_URL"},
		setup:   "protect setup",
	})
}

// newSchoolOpenCmd opens the Jamf School web interface.
func newSchoolOpenCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return newProfileURLOpenCmd(cliCtx, openTarget{
		product: "school",
		label:   "Jamf School",
		envVars: []string{"JAMFSCHOOL_URL"},
		setup:   "school setup",
	})
}

// newSecurityOpenCmd opens Jamf Security Cloud (Radar).
func newSecurityOpenCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var printOnly bool
	cmd := &cobra.Command{
		Use:   "open",
		Short: "Open the Jamf Security Cloud (Radar) web interface in a browser",
		Long: `Open the Jamf Security Cloud (Radar) web interface in a browser.

Jamf Security Cloud has no per-tenant URL — tenancy is carried inside the
JWT — so this command needs no credentials and makes no request.` + openLongTail,
		Example: `  jamf-cli security open`,
		// No credentials are read and no API is called, so auth resolution
		// would only be able to fail. The annotation rather than a name in
		// chainSkip: "open" is a plausible generated operation name, and a
		// name map would bypass auth for every command that came to share it.
		Annotations: map[string]string{noAuthAnnotation: "true"},
		RunE: func(_ *cobra.Command, _ []string) error {
			return openOrPrint(cliCtx, radarURL, printOnly)
		},
	}
	addPrintFlag(cmd, &printOnly)
	return cmd
}

// openTarget describes a product whose web interface is the base URL its
// profile already holds.
type openTarget struct {
	product string
	label   string
	// envVars are consulted in order after --url and before the profile,
	// mirroring each product's own credential ladder in root.go.
	envVars []string
	setup   string
}

// newProfileURLOpenCmd builds the `open` command for a product whose web
// interface is its configured base URL — no request, and so no credentials.
func newProfileURLOpenCmd(cliCtx *registry.CLIContext, t openTarget) *cobra.Command {
	var printOnly bool
	cmd := &cobra.Command{
		Use:   "open",
		Short: fmt.Sprintf("Open the %s web interface in a browser", t.label),
		Long: fmt.Sprintf(`Open the %s web interface in a browser.

The URL is the one the profile is configured with, so no request is made and
no credentials are needed.`, t.label) + openLongTail,
		Example: fmt.Sprintf("  jamf-cli %s open\n  jamf-cli %s open --print", t.product, t.product),
		// Reads a URL and calls no API — see newSecurityOpenCmd.
		Annotations: map[string]string{noAuthAnnotation: "true"},
		RunE: func(_ *cobra.Command, _ []string) error {
			url, err := configuredWebURL(t)
			if err != nil {
				return err
			}
			return openOrPrint(cliCtx, url, printOnly)
		},
	}
	addPrintFlag(cmd, &printOnly)
	return cmd
}

// addPrintFlag registers --print. One function so the four commands cannot
// drift in wording, and so a second spelling never appears: gh calls this
// --no-browser, and carrying both would leave two documented names for one
// behaviour.
func addPrintFlag(cmd *cobra.Command, printOnly *bool) {
	cmd.Flags().BoolVar(printOnly, "print", false, "Print the URL instead of opening a browser")
}

// configuredWebURL resolves a product's base URL without resolving auth:
// --url, then the product's own environment variables, then the profile.
//
// The ladder is duplicated from resolveProtectClient and resolveSchoolClient
// rather than shared with them, because those two resolve credentials in the
// same pass and fail when a credential is missing — which is the wrong answer
// for a command that only needs a hostname.
func configuredWebURL(t openTarget) (string, error) {
	if serverURL != "" {
		return serverURL, nil
	}
	for _, name := range t.envVars {
		if v := os.Getenv(name); v != "" {
			return v, nil
		}
	}
	cfg, err := config.Load()
	if err != nil {
		return "", fmt.Errorf("loading config: %w", err)
	}
	profileName := profile
	if profileName == "" {
		profileName = os.Getenv("JAMF_PROFILE")
	}
	if p, _, err := config.GetProfile(cfg, profileName); err == nil && p.URL != "" {
		return p.URL, nil
	}
	return "", exitcode.New(exitcode.Usage,
		fmt.Sprintf("no %s URL configured: use --url, %s, or run \"jamf-cli %s\"",
			t.label, envPhrase(t.envVars), t.setup))
}

// envPhrase names the environment variables for the not-configured message.
// A product with one of them gets a sentence about that variable; "one of
// JAMFSCHOOL_URL" reads as a truncated list, and this message is the whole
// answer when nothing is configured.
func envPhrase(names []string) string {
	if len(names) == 1 {
		return "the " + names[0] + " environment variable"
	}
	out := "one of "
	for i, n := range names {
		switch {
		case i == 0:
			out += n
		case i == len(names)-1:
			out += " or " + n
		default:
			out += ", " + n
		}
	}
	return out
}

// proWebURL answers the Jamf Pro web URL for the resolved auth method.
func proWebURL(ctx context.Context, cliCtx *registry.CLIContext) (string, error) {
	if !isGatewayProvider(cliCtx.AuthProvider) {
		// resolveAuth writes the resolved URL back to serverURL, so this is
		// the host the command authenticated against — including any context
		// path an on-premise instance is served under.
		if serverURL == "" {
			return "", exitcode.New(exitcode.Usage, "no Jamf Pro URL configured: use --url, JAMF_URL, or run \"jamf-cli pro setup\"")
		}
		return serverURL, nil
	}
	data, err := fetchJSON(ctx, cliCtx.Client, proServerURLPath)
	if err != nil {
		return "", fmt.Errorf("reading the Jamf Pro URL from %s: %w", proServerURLPath, err)
	}
	url, ok := data["url"].(string)
	if !ok || url == "" {
		return "", fmt.Errorf("%s returned no url field", proServerURLPath)
	}
	return url, nil
}

// openOrPrint launches a browser, or prints the URL when a browser cannot be
// the answer.
//
// The URL goes through printRows in the print case so --out-file, -o json,
// --field and --select all apply, which is what makes the command scriptable.
// In the launch case it is a one-line stderr note instead: a single-cell table
// around one URL is noise, and stdout stays empty so nothing downstream reads a
// launch as data.
func openOrPrint(cliCtx *registry.CLIContext, rawURL string, printOnly bool) error {
	url, err := browser.Validate(rawURL)
	if err != nil {
		return err
	}
	// --dry-run prints rather than launches: a browser launch is the whole
	// effect of this command, so running it under -n would make the flag a
	// documented no-op.
	if printOnly || noInput || cliCtx.DryRun || !output.IsTerminal(os.Stdout.Fd()) {
		return printRows(cliCtx, []map[string]any{{"url": url}})
	}
	if _, err := browser.Open(url, os.Getenv("BROWSER")); err != nil {
		return err
	}
	if !quiet {
		_, _ = fmt.Fprintf(os.Stderr, "Opening %s\n", url)
	}
	return nil
}
