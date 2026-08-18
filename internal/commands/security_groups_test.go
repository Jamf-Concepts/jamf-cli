// Copyright 2026, Jamf Software LLC

package commands

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// gatewayServedSecurityResources are the Security Cloud resources reached
// through the platform gateway rather than api.wandera.com.
var gatewayServedSecurityResources = []string{
	"dns-zones", "dns-search-domains", "dns-custom-hostname-mappings",
	"ztna-apps", "ztna-gateways", "ztna-grouped-gateways",
	"ztna-shared-gateways", "ztna-predefined-apps",
	"content-categories", "device-groups",
	"uem-connectors", "uem-connector-enablement",
	"uem-sync-settings", "uem-sync", "uem-activation-profiles",
}

func findSecurityCmd(t *testing.T) *cobra.Command {
	t.Helper()
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	cmd := findSubcommand(root, "security")
	if cmd == nil {
		t.Fatal("security command not found")
		return nil
	}
	return cmd
}

// TestSecurityGroups_AllCommandsGrouped guards the spec-refresh loop: the
// gateway-served Security Cloud commands are generated from specs that change
// often, so a new tag becomes a new top-level subcommand without anyone
// editing this package. Ungrouped, it would vanish from the grouped `security
// --help` output while still being callable — the kind of drift nobody notices
// until a user cannot find the command.
func TestSecurityGroups_AllCommandsGrouped(t *testing.T) {
	security := findSecurityCmd(t)

	for _, cmd := range security.Commands() {
		if cmd.Name() == "help" {
			continue
		}
		if cmd.GroupID == "" {
			t.Errorf("security command %q has no GroupID — add it to securityGroupMap in groups.go", cmd.Name())
		}
	}
}

func TestSecurityGroups_GroupsRegistered(t *testing.T) {
	security := findSecurityCmd(t)

	groups := security.Groups()
	if len(groups) != len(securityGroups) {
		t.Fatalf("expected %d security groups, got %d", len(securityGroups), len(groups))
	}
}

// TestSecurityGatewayServedCommandsPresent pins the resources reached through
// the platform gateway rather than api.wandera.com. They are wired by hand in
// security.go — the generated platform package is shared across products, so
// nothing registers them automatically — and a spec refresh that renames or
// drops one should fail here rather than quietly shrink the command tree.
func TestSecurityGatewayServedCommandsPresent(t *testing.T) {
	security := findSecurityCmd(t)

	for _, name := range gatewayServedSecurityResources {
		if findSubcommand(security, name) == nil {
			t.Errorf("security command %q not wired — add it in security.go", name)
		}
	}
}

// TestSecurityCommandsDeclareTheirAPI asserts every command under `security`
// records which API serves it.
//
// The namespace mixes two transports with different credentials: the
// gateway-served resources take platform client-credentials plus a Security
// Cloud tenant ID, the Radar-served ones take per-API scoped pairs. Cobra shows
// Short as the shell-completion description, so that label is how someone
// typing `security <TAB>` learns which applies — and jamf:api is the
// machine-readable half of the same fact, consumed by the commands catalog.
//
// Both halves come from the generators, so a refresh that adds a resource gets
// them for free; this fails if one is ever emitted without the other.
func TestSecurityCommandsDeclareTheirAPI(t *testing.T) {
	security := findSecurityCmd(t)

	gatewayServed := make(map[string]bool, len(gatewayServedSecurityResources))
	for _, name := range gatewayServedSecurityResources {
		gatewayServed[name] = true
	}

	for _, cmd := range security.Commands() {
		switch cmd.Name() {
		case "help", "setup":
			// setup writes credentials to config and the keychain; it calls
			// neither API, so it declares neither.
			continue
		}

		want := "radar"
		wantLabel := "Radar API"
		if gatewayServed[cmd.Name()] {
			want = "platform-gateway"
			wantLabel = "platform gateway"
		}

		if got := cmd.Annotations["jamf:api"]; got != want {
			t.Errorf("security %s: jamf:api = %q, want %q", cmd.Name(), got, want)
		}
		if !strings.Contains(cmd.Short, wantLabel) {
			t.Errorf("security %s: Short %q does not name its API (%q) — shell completion shows this string",
				cmd.Name(), cmd.Short, wantLabel)
		}

		// Subcommands inherit nothing from the parent, and they are what a
		// 403 sends someone looking, so they carry the annotation too.
		for _, sub := range cmd.Commands() {
			if sub.Name() == "help" {
				continue
			}
			if got := sub.Annotations["jamf:api"]; got != want {
				t.Errorf("security %s %s: jamf:api = %q, want %q", cmd.Name(), sub.Name(), got, want)
			}
		}
	}
}
