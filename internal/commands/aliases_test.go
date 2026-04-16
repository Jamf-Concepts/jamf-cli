// Copyright 2026, Jamf Software LLC

package commands

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestApplyAliases(t *testing.T) {
	parent := &cobra.Command{Use: "pro"}
	parent.AddCommand(&cobra.Command{Use: "computers-inventory"})
	parent.AddCommand(&cobra.Command{Use: "mobile-devices"})
	parent.AddCommand(&cobra.Command{Use: "scripts"})
	parent.AddCommand(&cobra.Command{Use: "buildings"})
	parent.AddCommand(&cobra.Command{Use: "categories"})
	parent.AddCommand(&cobra.Command{Use: "departments"})
	parent.AddCommand(&cobra.Command{Use: "group-tools"})
	parent.AddCommand(&cobra.Command{Use: "version"}) // no alias expected

	applyAliases(parent)

	tests := []struct {
		name    string
		aliases []string
	}{
		{"computers-inventory", []string{"computers", "comp"}},
		{"mobile-devices", []string{"md"}},
		{"scripts", []string{"scr"}},
		{"buildings", []string{"bld"}},
		{"categories", []string{"cat"}},
		{"departments", []string{"dept"}},
		{"group-tools", []string{"gt"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, _, err := parent.Find([]string{tc.aliases[0]})
			if err != nil {
				t.Fatalf("alias %q not found: %v", tc.aliases[0], err)
			}
			if cmd.Name() != tc.name {
				t.Errorf("alias %q resolved to %q, want %q", tc.aliases[0], cmd.Name(), tc.name)
			}
		})
	}

	// version should have no aliases
	for _, cmd := range parent.Commands() {
		if cmd.Name() == "version" && len(cmd.Aliases) > 0 {
			t.Errorf("version should have no aliases, got %v", cmd.Aliases)
		}
	}
}

func TestApplyRootAliases(t *testing.T) {
	root := &cobra.Command{Use: "jamf-cli"}
	root.AddCommand(&cobra.Command{Use: "config"})
	root.AddCommand(&cobra.Command{Use: "version"})

	applyRootAliases(root)

	cmd, _, err := root.Find([]string{"cfg"})
	if err != nil {
		t.Fatalf("alias 'cfg' not found: %v", err)
	}
	if cmd.Name() != "config" {
		t.Errorf("alias 'cfg' resolved to %q, want 'config'", cmd.Name())
	}

	// version should have no aliases
	for _, cmd := range root.Commands() {
		if cmd.Name() == "version" && len(cmd.Aliases) > 0 {
			t.Errorf("version should have no aliases, got %v", cmd.Aliases)
		}
	}
}
