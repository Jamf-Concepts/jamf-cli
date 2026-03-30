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
	}
	return cmd
}

func TestProtectCommandExists(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01")
	if findSubcommand(root, "protect") == nil {
		t.Fatal("expected 'protect' subcommand on root")
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
	}

	for _, name := range expected {
		t.Run(name, func(t *testing.T) {
			if findSubcommand(protect, name) == nil {
				t.Errorf("expected protect subcommand %q", name)
			}
		})
	}
}

func TestProtectPlansSubcommands(t *testing.T) {
	protect := findProtectCmd(t)
	plans := findSubcommand(protect, "plans")
	if plans == nil {
		t.Fatal("plans subcommand not found")
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
