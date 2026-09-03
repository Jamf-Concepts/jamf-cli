// Copyright 2026, Jamf Software LLC

package commands

import (
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
)

// TestResolveProductOnlyMatchesTheTopLevelNamespace is the regression test for
// `pro report security` panicking with SIGSEGV.
//
// resolveProduct walked from the leaf upward and returned on the first
// product-named ancestor, so the innermost name won and the command resolved to
// Jamf Security Cloud. PersistentPreRunE then returned early on the security
// branch without building cliCtx.Client, and runReportSecurity dereferenced a
// nil one. The early return used to fail safely — resolveSecurityClient found no
// Radar credentials and errored — until securityPlatformSDKClient made it
// succeed from the generic JAMF_* credentials.
//
// collectCommands guards the same hazard with rootOnlySkip and names this exact
// command in its comment. The catalog walk was fixed; the auth walk was not.
func TestResolveProductOnlyMatchesTheTopLevelNamespace(t *testing.T) {
	// Empty config with no default profile, so only the hierarchy can answer.
	cfg := &config.Config{Profiles: map[string]config.Profile{}}

	build := func(path ...string) *cobra.Command {
		root := &cobra.Command{Use: "jamf-cli"}
		parent := root
		var leaf *cobra.Command
		for _, name := range path {
			c := &cobra.Command{Use: name}
			parent.AddCommand(c)
			parent, leaf = c, c
		}
		return leaf
	}

	cases := []struct {
		path []string
		want string
	}{
		// The bug: a subcommand named after a namespace.
		{[]string{"pro", "report", "security"}, "pro"},
		{[]string{"pro", "report", "protect"}, "pro"},
		{[]string{"protect", "plans", "pro"}, "protect"},
		// The cases the old rule was written for still hold.
		{[]string{"pro", "computers", "list"}, "pro"},
		{[]string{"protect", "plans", "list"}, "protect"},
		{[]string{"school", "devices", "list"}, "school"},
		{[]string{"security", "risk", "list"}, "security"},
		{[]string{"pro"}, "pro"},
		{[]string{"security"}, "security"},
		// A non-product namespace falls through to the profile, which here
		// names nothing, so the default applies.
		{[]string{"platform", "ai-policies", "list"}, "pro"},
		{[]string{"config", "list"}, "pro"},
	}

	for _, tc := range cases {
		t.Run(joinPath(tc.path), func(t *testing.T) {
			if got := resolveProduct(build(tc.path...), cfg); got != tc.want {
				t.Errorf("resolveProduct(%v) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

// TestResolveProductFallsBackToTheProfile: a command outside every product
// namespace still has to reach the profile's product field, or a root-level
// command against a Protect profile resolves as Jamf Pro.
func TestResolveProductFallsBackToTheProfile(t *testing.T) {
	t.Setenv("JAMF_PROFILE", "")
	prev := profile
	t.Cleanup(func() { profile = prev })
	profile = "p"

	for _, product := range []string{"protect", "school", "security", "pro"} {
		cfg := &config.Config{
			DefaultProfile: "p",
			Profiles:       map[string]config.Profile{"p": {Product: product}},
		}
		root := &cobra.Command{Use: "jamf-cli"}
		leaf := &cobra.Command{Use: "doctor"}
		root.AddCommand(leaf)
		if got := resolveProduct(leaf, cfg); got != product {
			t.Errorf("profile product %q resolved as %q", product, got)
		}
	}
}

func joinPath(p []string) string {
	out := ""
	for i, s := range p {
		if i > 0 {
			out += "_"
		}
		out += s
	}
	return out
}
