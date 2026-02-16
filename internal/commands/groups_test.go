package commands

import (
	"testing"
)

func TestApplyGroups_AllCommandsGrouped(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01")

	for _, cmd := range root.Commands() {
		if cmd.Name() == "help" {
			continue
		}
		if cmd.GroupID == "" {
			t.Errorf("command %q has no GroupID — add it to commandGroupMap in groups.go", cmd.Name())
		}
	}
}

func TestApplyGroups_GroupsRegistered(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01")

	groups := root.Groups()
	if len(groups) != len(commandGroups) {
		t.Fatalf("expected %d groups, got %d", len(commandGroups), len(groups))
	}

	for i, g := range groups {
		if g.ID != commandGroups[i].ID {
			t.Errorf("group[%d].ID = %q, want %q", i, g.ID, commandGroups[i].ID)
		}
		if g.Title != commandGroups[i].Title {
			t.Errorf("group[%d].Title = %q, want %q", i, g.Title, commandGroups[i].Title)
		}
	}
}

func TestCommandGroupMap_AllGroupIDsValid(t *testing.T) {
	validIDs := make(map[string]bool, len(commandGroups))
	for _, g := range commandGroups {
		validIDs[g.ID] = true
	}

	for cmdName, gid := range commandGroupMap {
		if !validIDs[gid] {
			t.Errorf("commandGroupMap[%q] = %q, which is not a defined group ID", cmdName, gid)
		}
	}
}

func TestApplyGroups_NoPanic(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01")
	root.SetArgs([]string{"--help"})
	// Execute triggers Cobra's internal group validation; it panics if a
	// command references a GroupID that was never registered via AddGroup.
	_ = root.Execute()
}
