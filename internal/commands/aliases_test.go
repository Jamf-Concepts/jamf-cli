package commands

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestApplyAliases(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	root.AddCommand(&cobra.Command{Use: "computers"})
	root.AddCommand(&cobra.Command{Use: "mobile-devices"})
	root.AddCommand(&cobra.Command{Use: "scripts"})
	root.AddCommand(&cobra.Command{Use: "buildings"})
	root.AddCommand(&cobra.Command{Use: "categories"})
	root.AddCommand(&cobra.Command{Use: "departments"})
	root.AddCommand(&cobra.Command{Use: "config"})
	root.AddCommand(&cobra.Command{Use: "version"}) // no alias expected

	applyAliases(root)

	tests := []struct {
		name    string
		aliases []string
	}{
		{"computers", []string{"comp"}},
		{"mobile-devices", []string{"md"}},
		{"scripts", []string{"scr"}},
		{"buildings", []string{"bld"}},
		{"categories", []string{"cat"}},
		{"departments", []string{"dept"}},
		{"config", []string{"cfg"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, _, err := root.Find([]string{tc.aliases[0]})
			if err != nil {
				t.Fatalf("alias %q not found: %v", tc.aliases[0], err)
			}
			if cmd.Name() != tc.name {
				t.Errorf("alias %q resolved to %q, want %q", tc.aliases[0], cmd.Name(), tc.name)
			}
		})
	}

	// version should have no aliases
	for _, cmd := range root.Commands() {
		if cmd.Name() == "version" && len(cmd.Aliases) > 0 {
			t.Errorf("version should have no aliases, got %v", cmd.Aliases)
		}
	}
}
