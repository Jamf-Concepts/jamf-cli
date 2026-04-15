// Copyright 2026, Jamf Software LLC

package commands

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestApplyRootGroups_AllCommandsGrouped(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01")

	for _, cmd := range root.Commands() {
		if cmd.Name() == "help" {
			continue
		}
		if cmd.GroupID == "" {
			t.Errorf("root command %q has no GroupID — add it to rootGroupMap in groups.go", cmd.Name())
		}
	}
}

func TestApplyRootGroups_GroupsRegistered(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01")

	groups := root.Groups()
	if len(groups) != len(rootGroups) {
		t.Fatalf("expected %d root groups, got %d", len(rootGroups), len(groups))
	}
}

func TestApplyProGroups_AllCommandsGrouped(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01")

	var pro *cobra.Command
	for _, cmd := range root.Commands() {
		if cmd.Name() == "pro" {
			pro = cmd
			break
		}
	}
	if pro == nil {
		t.Fatal("expected 'pro' subcommand on root")
		return
	}

	for _, cmd := range pro.Commands() {
		if cmd.Name() == "help" {
			continue
		}
		if cmd.GroupID == "" {
			t.Errorf("pro command %q has no GroupID — add it to proGroupMap in groups.go", cmd.Name())
		}
	}
}

func TestApplyProGroups_GroupsRegistered(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01")

	var pro *cobra.Command
	for _, cmd := range root.Commands() {
		if cmd.Name() == "pro" {
			pro = cmd
			break
		}
	}
	if pro == nil {
		t.Fatal("expected 'pro' subcommand on root")
		return
	}

	groups := pro.Groups()
	if len(groups) != len(proGroups) {
		t.Fatalf("expected %d pro groups, got %d", len(proGroups), len(groups))
	}
}

func TestProGroupMap_AllGroupIDsValid(t *testing.T) {
	validIDs := make(map[string]bool, len(proGroups))
	for _, g := range proGroups {
		validIDs[g.ID] = true
	}

	for cmdName, gid := range proGroupMap {
		if !validIDs[gid] {
			t.Errorf("proGroupMap[%q] = %q, which is not a defined group ID", cmdName, gid)
		}
	}
}

func TestApplyGroups_NoPanic(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01")
	root.SetArgs([]string{"--help"})
	_ = root.Execute()
}

func TestApplyProGroups_NoPanic(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01")
	root.SetArgs([]string{"pro", "--help"})
	_ = root.Execute()
}
