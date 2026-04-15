// Copyright 2026, Jamf Software LLC

package commands

import (
	"testing"

	"github.com/spf13/cobra"
)

// findSchoolCmd returns the school command from a fresh root, failing the
// test if it does not exist.
func findSchoolCmd(t *testing.T) *cobra.Command {
	t.Helper()
	root := NewRootCmd("test", "abc123", "2024-01-01")
	cmd := findSubcommand(root, "school")
	if cmd == nil {
		t.Fatal("school command not found")
		return nil
	}
	return cmd
}

func TestSchoolCommandExists(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01")
	if findSubcommand(root, "school") == nil {
		t.Fatal("expected 'school' subcommand on root")
		return
	}
}

func TestSchoolGroups_AllCommandsGrouped(t *testing.T) {
	school := findSchoolCmd(t)

	for _, cmd := range school.Commands() {
		if cmd.Name() == "help" {
			continue
		}
		if cmd.GroupID == "" {
			t.Errorf("school command %q has no GroupID — add it to schoolGroupMap in groups.go", cmd.Name())
		}
	}
}

func TestSchoolGroups_GroupsRegistered(t *testing.T) {
	school := findSchoolCmd(t)

	groups := school.Groups()
	if len(groups) != len(schoolGroups) {
		t.Fatalf("expected %d school groups, got %d", len(schoolGroups), len(groups))
	}
}

func TestSchoolGroupMap_AllGroupIDsValid(t *testing.T) {
	validIDs := make(map[string]bool, len(schoolGroups))
	for _, g := range schoolGroups {
		validIDs[g.ID] = true
	}

	for cmdName, gid := range schoolGroupMap {
		if !validIDs[gid] {
			t.Errorf("schoolGroupMap[%q] = %q, which is not a defined group ID", cmdName, gid)
		}
	}
}

func TestSchoolAliases(t *testing.T) {
	school := findSchoolCmd(t)

	tests := []struct {
		alias   string
		command string
	}{
		{"dev", "devices"},
		{"dg", "device-groups"},
		{"cls", "classes"},
		{"loc", "locations"},
		{"dep", "dep-devices"},
		{"ib", "ibeacons"},
		{"bp", "blueprints"},
		{"ddm", "ddm-reports"},
	}

	for _, tc := range tests {
		t.Run(tc.alias, func(t *testing.T) {
			cmd, _, err := school.Find([]string{tc.alias})
			if err != nil {
				t.Fatalf("alias %q not found: %v", tc.alias, err)
			}
			if cmd.Name() != tc.command {
				t.Errorf("alias %q resolved to %q, want %q", tc.alias, cmd.Name(), tc.command)
			}
		})
	}
}

func TestSchoolSubcommands(t *testing.T) {
	school := findSchoolCmd(t)

	expected := []string{
		"overview",
		"setup",
		"devices",
		"device-groups",
		"users",
		"groups",
		"classes",
		"profiles",
		"apps",
		"locations",
		"ibeacons",
		"dep-devices",
		"blueprints",
		"ddm-reports",
	}

	for _, name := range expected {
		t.Run(name, func(t *testing.T) {
			if findSubcommand(school, name) == nil {
				t.Errorf("expected school subcommand %q", name)
			}
		})
	}
}

func TestSchoolDevicesSubcommands(t *testing.T) {
	school := findSchoolCmd(t)
	devices := findSubcommand(school, "devices")
	if devices == nil {
		t.Fatal("devices subcommand not found")
		return
	}

	expected := []string{"list", "get", "restart", "refresh", "unenroll", "erase", "clear-activation-lock", "trash", "restore"}
	for _, name := range expected {
		t.Run(name, func(t *testing.T) {
			if findSubcommand(devices, name) == nil {
				t.Errorf("expected devices subcommand %q", name)
			}
		})
	}
}

func TestSchoolUsersSubcommands(t *testing.T) {
	school := findSchoolCmd(t)
	users := findSubcommand(school, "users")
	if users == nil {
		t.Fatal("users subcommand not found")
		return
	}

	expected := []string{"list", "get", "apply", "delete", "export"}
	for _, name := range expected {
		t.Run(name, func(t *testing.T) {
			if findSubcommand(users, name) == nil {
				t.Errorf("expected users subcommand %q", name)
			}
		})
	}
}

func TestSchoolClassesSubcommands(t *testing.T) {
	school := findSchoolCmd(t)
	classes := findSubcommand(school, "classes")
	if classes == nil {
		t.Fatal("classes subcommand not found")
		return
	}

	expected := []string{"list", "get", "apply", "delete", "export", "assign-users", "devices"}
	for _, name := range expected {
		t.Run(name, func(t *testing.T) {
			if findSubcommand(classes, name) == nil {
				t.Errorf("expected classes subcommand %q", name)
			}
		})
	}
}

func TestSchoolDeviceGroupsSubcommands(t *testing.T) {
	school := findSchoolCmd(t)
	dg := findSubcommand(school, "device-groups")
	if dg == nil {
		t.Fatal("device-groups subcommand not found")
		return
	}

	expected := []string{"list", "get", "apply", "delete", "export", "members", "add-devices", "remove-devices"}
	for _, name := range expected {
		t.Run(name, func(t *testing.T) {
			if findSubcommand(dg, name) == nil {
				t.Errorf("expected device-groups subcommand %q", name)
			}
		})
	}
}

func TestSchoolIBeaconsSubcommands(t *testing.T) {
	school := findSchoolCmd(t)
	ib := findSubcommand(school, "ibeacons")
	if ib == nil {
		t.Fatal("ibeacons subcommand not found")
		return
	}

	expected := []string{"list", "get", "apply", "delete", "export"}
	for _, name := range expected {
		t.Run(name, func(t *testing.T) {
			if findSubcommand(ib, name) == nil {
				t.Errorf("expected ibeacons subcommand %q", name)
			}
		})
	}
}

func TestSchoolHelp_NoPanic(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01")
	root.SetArgs([]string{"school", "--help"})
	_ = root.Execute()
}
