// Copyright 2026, Jamf Software LLC

package commands

import (
	"testing"

	"github.com/spf13/cobra"
)

// findSubcommand returns the named child command, or nil if not found.
func findSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

// findProtectCmd returns the protect command from a fresh root, failing the
// test if it does not exist.
func findProtectCmd(t *testing.T) *cobra.Command {
	t.Helper()
	root := NewRootCmd("test", "abc123", "2024-01-01")
	cmd := findSubcommand(root, "protect")
	if cmd == nil {
		t.Fatal("protect command not found")
		return nil
	}
	return cmd
}

func TestProtectCommandExists(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01")
	if findSubcommand(root, "protect") == nil {
		t.Fatal("expected 'protect' subcommand on root")
		return
	}
}

func TestProtectGroups_AllCommandsGrouped(t *testing.T) {
	protect := findProtectCmd(t)

	for _, cmd := range protect.Commands() {
		if cmd.Name() == "help" {
			continue
		}
		if cmd.GroupID == "" {
			t.Errorf("protect command %q has no GroupID — add it to protectGroupMap in groups.go", cmd.Name())
		}
	}
}

func TestProtectGroups_GroupsRegistered(t *testing.T) {
	protect := findProtectCmd(t)

	groups := protect.Groups()
	if len(groups) != len(protectGroups) {
		t.Fatalf("expected %d protect groups, got %d", len(protectGroups), len(groups))
	}
}

func TestProtectAliases(t *testing.T) {
	protect := findProtectCmd(t)

	tests := []struct {
		alias   string
		command string
	}{
		{"comp", "computers"},
		{"ulf", "unified-logging-filters"},
		{"rscs", "removable-storage-control-sets"},
		{"es", "exception-sets"},
		{"as", "analytic-sets"},
		{"ac", "action-configs"},
		{"cpl", "custom-prevent-lists"},
		{"apic", "api-clients"},
		{"cf", "config-freeze"},
		{"df", "data-forwarding"},
		{"dr", "data-retention"},
		{"al", "audit-logs"},
		{"ins", "insights"},
	}

	for _, tc := range tests {
		t.Run(tc.alias, func(t *testing.T) {
			cmd, _, err := protect.Find([]string{tc.alias})
			if err != nil {
				t.Fatalf("alias %q not found: %v", tc.alias, err)
			}
			if cmd.Name() != tc.command {
				t.Errorf("alias %q resolved to %q, want %q", tc.alias, cmd.Name(), tc.command)
			}
		})
	}
}

func TestProtectSubcommands(t *testing.T) {
	protect := findProtectCmd(t)

	expected := []string{
		"overview",
		"setup",
		"auth",
		"plans",
		"computers",
		"alerts",
		"insights",
		"analytics",
		"analytic-sets",
		"exception-sets",
		"removable-storage-control-sets",
		"action-configs",
		"telemetry",
		"custom-prevent-lists",
		"unified-logging-filters",
		"roles",
		"users",
		"groups",
		"api-clients",
		"data-forwarding",
		"data-retention",
		"downloads",
		"config-freeze",
		"connections",
		"audit-logs",
		"permissions",
	}

	for _, name := range expected {
		t.Run(name, func(t *testing.T) {
			if findSubcommand(protect, name) == nil {
				t.Errorf("expected protect subcommand %q", name)
			}
		})
	}
}

func TestProtectAlertsSubcommands(t *testing.T) {
	protect := findProtectCmd(t)
	alerts := findSubcommand(protect, "alerts")
	if alerts == nil {
		t.Fatal("alerts subcommand not found")
		return
	}

	expected := []string{"list", "get", "update-status", "status-counts"}
	for _, name := range expected {
		t.Run(name, func(t *testing.T) {
			if findSubcommand(alerts, name) == nil {
				t.Errorf("expected alerts subcommand %q", name)
			}
		})
	}
}

func TestProtectInsightsSubcommands(t *testing.T) {
	protect := findProtectCmd(t)
	insights := findSubcommand(protect, "insights")
	if insights == nil {
		t.Fatal("insights subcommand not found")
		return
	}

	expected := []string{"list", "enable", "disable", "computers", "compliance-score"}
	for _, name := range expected {
		t.Run(name, func(t *testing.T) {
			if findSubcommand(insights, name) == nil {
				t.Errorf("expected insights subcommand %q", name)
			}
		})
	}
}

func TestProtectAuditLogsSubcommands(t *testing.T) {
	protect := findProtectCmd(t)
	al := findSubcommand(protect, "audit-logs")
	if al == nil {
		t.Fatal("audit-logs subcommand not found")
		return
	}

	expected := []string{"list"}
	for _, name := range expected {
		t.Run(name, func(t *testing.T) {
			if findSubcommand(al, name) == nil {
				t.Errorf("expected audit-logs subcommand %q", name)
			}
		})
	}
}

func TestProtectComputersSubcommands(t *testing.T) {
	protect := findProtectCmd(t)
	computers := findSubcommand(protect, "computers")
	if computers == nil {
		t.Fatal("computers subcommand not found")
		return
	}

	expected := []string{"list", "get", "delete", "set-plan", "update"}
	for _, name := range expected {
		t.Run(name, func(t *testing.T) {
			if findSubcommand(computers, name) == nil {
				t.Errorf("expected computers subcommand %q", name)
			}
		})
	}
}

func TestProtectPlansSubcommands(t *testing.T) {
	protect := findProtectCmd(t)
	plans := findSubcommand(protect, "plans")
	if plans == nil {
		t.Fatal("plans subcommand not found")
		return
	}

	expected := []string{"list", "get", "apply", "delete", "export", "config-profile"}
	for _, name := range expected {
		t.Run(name, func(t *testing.T) {
			if findSubcommand(plans, name) == nil {
				t.Errorf("expected plans subcommand %q", name)
			}
		})
	}
}

func TestProtectAnalyticsSubcommands(t *testing.T) {
	protect := findProtectCmd(t)
	analytics := findSubcommand(protect, "analytics")
	if analytics == nil {
		t.Fatal("analytics subcommand not found")
		return
	}

	expected := []string{"list", "get", "apply", "delete", "export", "import"}
	for _, name := range expected {
		t.Run(name, func(t *testing.T) {
			if findSubcommand(analytics, name) == nil {
				t.Errorf("expected analytics subcommand %q", name)
			}
		})
	}
}

func TestProtectRSCSSubcommands(t *testing.T) {
	protect := findProtectCmd(t)
	rscs := findSubcommand(protect, "removable-storage-control-sets")
	if rscs == nil {
		t.Fatal("removable-storage-control-sets subcommand not found")
		return
	}

	expected := []string{"list", "get", "apply", "delete", "export", "add-rule", "remove-rule"}
	for _, name := range expected {
		t.Run(name, func(t *testing.T) {
			if findSubcommand(rscs, name) == nil {
				t.Errorf("expected RSCS subcommand %q", name)
			}
		})
	}
}
