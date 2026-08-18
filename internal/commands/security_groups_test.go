// Copyright 2026, Jamf Software LLC

package commands

import (
	"testing"

	"github.com/spf13/cobra"
)

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

	for _, name := range []string{
		"dns-zones", "dns-search-domains", "dns-custom-hostname-mappings",
		"ztna-apps", "ztna-gateways", "ztna-grouped-gateways",
		"ztna-shared-gateways", "ztna-predefined-apps",
		"content-categories", "device-groups",
		"uem-connectors", "uem-connector-enablement",
		"uem-sync-settings", "uem-sync", "uem-activation-profiles",
	} {
		if findSubcommand(security, name) == nil {
			t.Errorf("security command %q not wired — add it in security.go", name)
		}
	}
}
