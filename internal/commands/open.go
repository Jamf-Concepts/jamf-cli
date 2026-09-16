// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

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

// newProOpenCmd opens the Jamf Pro web interface, at a named section of it.
//
// This is the one `open` that can need a request. It does not carry
// noAuthAnnotation for that reason: on a gateway profile the instance URL is
// not a credential input at all (a platform integration names a tenant, never
// a Jamf Pro host), so it has to be read from the API.
//
// The section is a positional rather than 139 subcommands. Both spell the same
// invocation — `pro open policies` either way — and the choice only shows up
// in the two places that matter: `pro open --help` stays one screen instead of
// listing the whole interface, and `pro --help` keeps one entry. It is also
// what `gh browse <path>` and `stripe open <section>` do. Discoverability
// comes from completion and --list rather than from the help body.
func newProOpenCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var printOnly, list bool
	cmd := &cobra.Command{
		Use:   "open [<section>]",
		Short: "Open the Jamf Pro web interface in a browser",
		Long: `Open the Jamf Pro web interface in a browser, at a named section.

With no section the dashboard is opened. Run with --list to see every section
name, or press tab: completion offers each name with the heading the interface
uses for it.

An argument that is not a section name but looks like a path ("policies.html",
"view/settings/system-settings/sso") is opened as-is, so a page this CLI does
not name — a record detail page, an enrollment wizard — is still reachable.

On an instance profile (token or oauth2 auth) the configured URL is used and
no request is made. On a platform gateway profile the URL is read from the
Jamf Pro API, because a platform integration names a tenant rather than a
Jamf Pro host.` + openLongTail,
		Example: `  # Open the dashboard
  jamf-cli pro open

  # Open a section
  jamf-cli pro open policies
  jamf-cli pro open smart-computer-groups
  jamf-cli pro open settings/api-roles-and-clients

  # List every section name
  jamf-cli pro open --list

  # Print a URL instead of opening it
  jamf-cli pro open computers --print
  jamf-cli pro open computers -o json --field url

  # Open a page this CLI does not name
  jamf-cli pro open computers.html?id=42`,
		Args: cobra.MaximumNArgs(1),
		// --list answers from the table in this binary, so it must not require
		// credentials or a reachable instance. See noAuthWhenFlagAnnotation.
		Annotations: map[string]string{noAuthWhenFlagAnnotation: "list"},
		ValidArgsFunction: func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return completeProSections(toComplete), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Before the URL is resolved: --list is a question about this
			// binary's own table, so it must not need credentials, a reachable
			// instance or a gateway round trip.
			if list {
				return printProSections(cliCtx)
			}
			base, err := proWebURL(cmd.Context(), cliCtx)
			if err != nil {
				return err
			}
			section := ""
			if len(args) == 1 {
				section = args[0]
			}
			target, err := proSectionURL(base, section)
			if err != nil {
				return err
			}
			return openOrPrint(cliCtx, target, printOnly)
		},
	}
	addPrintFlag(cmd, &printOnly)
	cmd.Flags().BoolVar(&list, "list", false, "List the section names this command accepts and exit")
	return cmd
}

// proSectionURL joins a section onto the instance's base URL.
//
// A section name wins over a path, so a name carrying a slash
// ("settings/sso") is never read as a path. An unrecognised argument is a path
// only when it looks like one; anything else is a typo, and answering a typo
// by opening the base URL with junk appended would put the caller on Jamf
// Pro's own error page with nothing saying why.
//
// The result is parsed back and the host compared, because the argument is
// user text being glued onto a URL: "//evil.example.com" is a host-relative
// URL that would leave the instance entirely, and a browser would follow it.
func proSectionURL(base, section string) (string, error) {
	base = strings.TrimRight(base, "/")
	if section == "" {
		return base, nil
	}
	path := ""
	if s, ok := proUISections[section]; ok {
		path = s.Path
	} else if looksLikeUIPath(section) {
		path = section
	} else if strings.Contains(section, "://") {
		// Refused rather than opened: `pro open <url>` is someone expecting a
		// different command, and opening an arbitrary site from a Jamf Pro
		// subcommand is not what this one does.
		return "", exitcode.New(exitcode.Usage,
			fmt.Sprintf("%q is a URL, not a section: this command opens a page of this Jamf Pro instance", section)).
			WithHint("open the URL yourself, or pass a section name — \"jamf-cli pro open --list\"")
	} else {
		return "", unknownSectionError(section)
	}
	if path == "" { // the dashboard section
		return base, nil
	}
	target := base + "/" + strings.TrimLeft(path, "/")

	parsed, err := url.Parse(target)
	if err != nil {
		return "", fmt.Errorf("building a URL for %q: %w", section, err)
	}
	baseParsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("invalid base URL %q: %w", base, err)
	}
	if parsed.Host != baseParsed.Host || parsed.Scheme != baseParsed.Scheme {
		return "", fmt.Errorf("refusing to open %q: it resolves to %s rather than to this Jamf Pro instance", section, parsed.Host)
	}
	return target, nil
}

// looksLikeUIPath reports whether an unrecognised argument should be opened
// verbatim. A scheme is refused outright rather than treated as a path: `pro
// open https://example.com` is someone expecting a different command, and
// opening an arbitrary site from a Jamf Pro subcommand is not it.
func looksLikeUIPath(arg string) bool {
	if strings.Contains(arg, "://") {
		return false
	}
	// No page is reached through a parent segment, and a browser resolves one
	// before sending, so ".." can only obscure where the caller is being sent.
	for _, seg := range strings.Split(arg, "/") {
		if seg == ".." {
			return false
		}
	}
	before, _, _ := strings.Cut(arg, "?")
	return strings.HasSuffix(before, ".html") || strings.HasPrefix(arg, "view/")
}

// unknownSectionError names the nearest sections rather than only refusing.
// The table is large enough that a near miss is the likely mistake, and a bare
// refusal leaves the caller guessing at a name they cannot see.
func unknownSectionError(section string) error {
	near := nearestProSections(section, 5)
	msg := fmt.Sprintf("unknown section %q", section)
	hint := "run \"jamf-cli pro open --list\" to see every section name, or pass a path such as \"policies.html\""
	if len(near) > 0 {
		msg += "\n\nDid you mean this?\n\t" + strings.Join(near, "\n\t")
	}
	return exitcode.New(exitcode.Usage, msg).WithHint(hint)
}

// nearestProSections ranks candidates by substring match first and edit
// distance second. Substring first because these names are long and
// compound — someone reaching for the SSO settings types "sso", which is three
// edits from nothing and a substring of exactly the right answer.
func nearestProSections(section string, limit int) []string {
	type scored struct {
		name string
		rank int
		dist int
	}
	var out []scored
	for name := range proUISections {
		switch {
		case strings.Contains(name, section):
			out = append(out, scored{name, 0, len(name)})
		case levenshtein(section, name) <= 3:
			out = append(out, scored{name, 1, levenshtein(section, name)})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].rank != out[j].rank {
			return out[i].rank < out[j].rank
		}
		if out[i].dist != out[j].dist {
			return out[i].dist < out[j].dist
		}
		return out[i].name < out[j].name
	})
	names := make([]string, 0, limit)
	for _, s := range out {
		if len(names) == limit {
			break
		}
		names = append(names, s.name)
	}
	return names
}

// completeProSections offers each section with the interface's own heading as
// the description, so a name can be found by what it is called on screen.
func completeProSections(toComplete string) []string {
	out := make([]string, 0, len(proUISections))
	for name, s := range proUISections {
		if !strings.HasPrefix(name, toComplete) {
			continue
		}
		out = append(out, name+"\t"+s.Label)
	}
	sort.Strings(out)
	return out
}

// printProSections renders the table through the shared formatter, so --list
// is consumable as data (-o json, --out-file) rather than only readable.
func printProSections(cliCtx *registry.CLIContext) error {
	rows := make([]map[string]any, 0, len(proUISections))
	for name, s := range proUISections {
		rows = append(rows, map[string]any{"name": name, "page": s.Label, "path": s.Path})
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i]["name"].(string) < rows[j]["name"].(string)
	})
	return printRows(cliCtx, rows)
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
