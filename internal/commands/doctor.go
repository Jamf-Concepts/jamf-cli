// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// envVarsToProbe is the closed list of credential-bearing env vars that
// `doctor` reports on. Adding new auth methods means adding rows here so
// users see their state without having to remember the exact name.
var envVarsToProbe = []string{
	"JAMF_URL",
	"JAMF_PROFILE",
	"JAMF_TOKEN",
	"JAMF_CLIENT_ID",
	"JAMF_CLIENT_SECRET",
	"JAMF_TENANT_ID",
	"JAMFPROTECT_URL",
	"JAMFPROTECT_CLIENT_ID",
	"JAMFPROTECT_CLIENT_SECRET",
	"JAMFSCHOOL_URL",
	"JAMFSCHOOL_NETWORK_ID",
	"JAMFSCHOOL_API_KEY",
}

// secretEnvVars are the subset of envVarsToProbe whose values must never
// be printed in full — only fingerprinted.
var secretEnvVars = map[string]bool{
	"JAMF_TOKEN":                true,
	"JAMF_CLIENT_ID":            true,
	"JAMF_CLIENT_SECRET":        true,
	"JAMFPROTECT_CLIENT_ID":     true,
	"JAMFPROTECT_CLIENT_SECRET": true,
	"JAMFSCHOOL_API_KEY":        true,
}

// doctorReport is the JSON-shaped output used with `-o json`.
type doctorReport struct {
	Version       string            `json:"version"`
	ConfigPath    string            `json:"configPath"`
	ConfigPresent bool              `json:"configPresent"`
	Profile       *profileReport    `json:"profile,omitempty"`
	Env           []envEntry        `json:"env"`
	Connectivity  *connectivityInfo `json:"connectivity,omitempty"`
	Notes         []string          `json:"notes,omitempty"`
}

type profileReport struct {
	Name        string         `json:"name"`
	Source      string         `json:"source"`
	Product     string         `json:"product,omitempty"`
	URL         string         `json:"url"`
	AuthMethod  string         `json:"authMethod"`
	Credentials []credentialOK `json:"credentials,omitempty"`
}

type credentialOK struct {
	Field       string `json:"field"`
	Reference   string `json:"reference,omitempty"`
	Resolved    bool   `json:"resolved"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Error       string `json:"error,omitempty"`
}

type envEntry struct {
	Name        string `json:"name"`
	Set         bool   `json:"set"`
	Value       string `json:"value,omitempty"`       // only for non-secret vars
	Fingerprint string `json:"fingerprint,omitempty"` // only for secret vars
}

type connectivityInfo struct {
	URL        string `json:"url"`
	StatusCode int    `json:"statusCode,omitempty"`
	LatencyMS  int64  `json:"latencyMs,omitempty"`
	Error      string `json:"error,omitempty"`
}

// newDoctorCmd builds the `jamf-cli doctor` diagnostic command. It runs
// without auth (added to chainSkip in root.go) so it works even when
// credentials are misconfigured — that's the case it's most useful for.
func newDoctorCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor [profile]",
		Short: "Diagnose local config, credentials, and server reachability",
		Long: `Prints the resolved profile, env-var state (secrets fingerprinted),
and a read-only HEAD probe of the configured URL. Useful for diagnosing
why a 401 is firing or why no profile resolved.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			report := buildDoctorReport(cfg, args, cliVersion)
			return renderDoctorReport(cliCtx, report)
		},
	}
}

// buildDoctorReport assembles the diagnostic snapshot. Pure function
// against (cfg, args, version) so it's straightforward to test.
func buildDoctorReport(cfg *config.Config, args []string, version string) doctorReport {
	report := doctorReport{
		Version:    version,
		ConfigPath: config.ConfigPath(),
		Env:        probeEnvVars(),
	}
	if _, err := os.Stat(report.ConfigPath); err == nil {
		report.ConfigPresent = true
	}

	profileName, source := resolveProfileNameForDoctor(cfg, args)
	if profileName != "" {
		if p, ok := cfg.Profiles[profileName]; ok {
			pr := profileReport{
				Name:        profileName,
				Source:      source,
				Product:     p.Product,
				URL:         p.URL,
				AuthMethod:  defaultIfEmpty(p.AuthMethod, "token"),
				Credentials: probeProfileCredentials(p),
			}
			report.Profile = &pr

			// Connectivity probe — only when we have a URL to hit.
			if p.URL != "" {
				ci := probeConnectivity(p.URL)
				report.Connectivity = &ci
			}
		} else {
			report.Notes = append(report.Notes,
				fmt.Sprintf("profile %q not found in config — run `jamf-cli config list` to see available profiles", profileName))
		}
	} else if !report.ConfigPresent {
		report.Notes = append(report.Notes,
			"no config file present — run `jamf-cli pro setup` (or `protect setup` / `school setup`) to create a profile")
	}
	return report
}

// resolveProfileNameForDoctor mirrors the precedence used elsewhere in
// the CLI but reports back which signal won so the user can debug
// "why is this profile being picked?"
func resolveProfileNameForDoctor(cfg *config.Config, args []string) (name, source string) {
	if len(args) == 1 && args[0] != "" {
		return args[0], "positional argument"
	}
	if profile != "" {
		return profile, "--profile flag"
	}
	if env := os.Getenv("JAMF_PROFILE"); env != "" {
		return env, "JAMF_PROFILE env var"
	}
	if cfg.DefaultProfile != "" {
		return cfg.DefaultProfile, "config default-profile"
	}
	return "", ""
}

func probeEnvVars() []envEntry {
	entries := make([]envEntry, 0, len(envVarsToProbe))
	for _, name := range envVarsToProbe {
		val := os.Getenv(name)
		entry := envEntry{Name: name, Set: val != ""}
		if val != "" {
			if secretEnvVars[name] {
				entry.Fingerprint = fingerprint(val)
			} else {
				entry.Value = val
			}
		}
		entries = append(entries, entry)
	}
	return entries
}

func probeProfileCredentials(p config.Profile) []credentialOK {
	creds := []credentialOK{}
	if p.Token != "" {
		creds = append(creds, resolveCredential("token", p.Token))
	}
	if p.ClientID != "" {
		creds = append(creds, resolveCredential("client-id", p.ClientID))
	}
	if p.ClientSecret != "" {
		creds = append(creds, resolveCredential("client-secret", p.ClientSecret))
	}
	if p.APIKey != "" {
		creds = append(creds, resolveCredential("api-key", p.APIKey))
	}
	sort.Slice(creds, func(i, j int) bool { return creds[i].Field < creds[j].Field })
	return creds
}

// resolveCredential attempts to resolve a profile secret reference and
// returns a fingerprint of the resolved value. Errors (e.g., env var
// not set, keychain miss) are captured rather than returned so the
// report can show every credential's state in one pass.
func resolveCredential(field, ref string) credentialOK {
	out := credentialOK{Field: field, Reference: ref}
	resolved, err := config.ResolveSecret(ref)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	if resolved == "" {
		return out
	}
	out.Resolved = true
	out.Fingerprint = fingerprint(resolved)
	return out
}

// probeConnectivity does a minimal unauthenticated HEAD against the
// profile URL — pure reachability + TLS + DNS check, no auth context.
// 5s budget is small enough to surface obvious problems and large
// enough that a slow proxy doesn't trip it.
func probeConnectivity(url string) connectivityInfo {
	url = strings.TrimRight(url, "/")
	if !strings.Contains(url, "://") {
		url = "https://" + url
	}
	ci := connectivityInfo{URL: url}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		ci.Error = err.Error()
		return ci
	}
	resp, err := http.DefaultClient.Do(req)
	ci.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		ci.Error = err.Error()
		return ci
	}
	defer func() { _ = resp.Body.Close() }()
	ci.StatusCode = resp.StatusCode
	return ci
}

// fingerprint returns the first 4 characters of a value followed by a
// dotted ellipsis. Users can confirm which secret they're seeing without
// leaking it in shell history or screen recordings.
func fingerprint(s string) string {
	if len(s) < 4 {
		return strings.Repeat("•", len(s))
	}
	return s[:4] + "••••"
}

func defaultIfEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// renderDoctorReport prints either JSON (when -o json/yaml) or a
// human-readable summary. Routed through the formatter so --out-file
// and other global flags work as expected.
func renderDoctorReport(cliCtx *registry.CLIContext, report doctorReport) error {
	formatStr := outputFmt
	if formatStr == "json" || formatStr == "yaml" {
		data, err := json.Marshal(report)
		if err != nil {
			return err
		}
		return cliCtx.Output.PrintRaw(data)
	}
	return printDoctorHuman(os.Stdout, report)
}

// printDoctorHuman renders the report as a sectioned text block similar
// to `kubectl cluster-info`. We avoid reaching back into the formatter
// for table rendering because the data is keyed-by-row, not rows-of-
// records — table layout doesn't fit.
func printDoctorHuman(w *os.File, r doctorReport) error {
	p := func(format string, args ...any) {
		_, _ = fmt.Fprintf(w, format, args...)
	}

	p("jamf-cli %s\n\n", r.Version)

	p("CONFIG\n")
	p("  path:    %s\n", r.ConfigPath)
	p("  status:  %s\n", presentString(r.ConfigPresent))
	p("\n")

	if r.Profile != nil {
		p("ACTIVE PROFILE\n")
		p("  name:        %s\n", r.Profile.Name)
		p("  source:      %s\n", r.Profile.Source)
		if r.Profile.Product != "" {
			p("  product:     %s\n", r.Profile.Product)
		}
		p("  url:         %s\n", r.Profile.URL)
		p("  auth-method: %s\n", r.Profile.AuthMethod)
		for _, c := range r.Profile.Credentials {
			p("  %-12s %s\n", c.Field+":", credentialLine(c))
		}
		p("\n")
	}

	p("ENVIRONMENT\n")
	for _, e := range r.Env {
		p("  %-30s %s\n", e.Name, envLine(e))
	}
	p("\n")

	if r.Connectivity != nil {
		p("CONNECTIVITY\n")
		c := r.Connectivity
		switch {
		case c.Error != "":
			p("  HEAD %s  →  ERROR: %s (%dms)\n", c.URL, c.Error, c.LatencyMS)
		default:
			p("  HEAD %s  →  %d %s (%dms)\n",
				c.URL, c.StatusCode, http.StatusText(c.StatusCode), c.LatencyMS)
		}
		p("\n")
	}

	for _, n := range r.Notes {
		p("NOTE: %s\n", n)
	}
	return nil
}

func presentString(ok bool) string {
	if ok {
		return "present"
	}
	return "missing"
}

func envLine(e envEntry) string {
	if !e.Set {
		return "(unset)"
	}
	if e.Fingerprint != "" {
		return "set, fingerprint: " + e.Fingerprint
	}
	return e.Value
}

func credentialLine(c credentialOK) string {
	switch {
	case c.Error != "":
		return fmt.Sprintf("%s  (UNRESOLVABLE: %s)", c.Reference, c.Error)
	case c.Resolved:
		return fmt.Sprintf("%s  (resolved, fingerprint: %s)", c.Reference, c.Fingerprint)
	default:
		return c.Reference
	}
}
