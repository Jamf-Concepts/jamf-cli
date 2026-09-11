// Copyright 2026, Jamf Software LLC

package commands

import (
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestApplyAliases(t *testing.T) {
	parent := &cobra.Command{Use: "pro"}
	parent.AddCommand(&cobra.Command{Use: "computer-inventory"})
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
		{"computer-inventory", []string{"computers", "comp"}},
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

// No command may answer to one alias twice.
//
// applyDeprecatedNames runs before applyAliases and appends aliases of its own,
// so two tables can name one alias — `jcds` did, from both, until it was
// promoted to a curated alias and dropped from deprecatedNames. A duplicate
// resolves fine and then prints twice in the `Aliases:` line of --help and
// twice in the `commands -o json` catalog, which reads as a defect in the
// listing rather than in a table.
func TestNoCommandAnswersToAnAliasTwice(t *testing.T) {
	root := NewRootCmd("test", "test", "test", "test")
	var dupes []string
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		seen := map[string]int{}
		for _, a := range cmd.Aliases {
			seen[a]++
		}
		for a, n := range seen {
			if n > 1 {
				dupes = append(dupes, cmd.CommandPath()+" answers to "+a+" "+strconv.Itoa(n)+" times")
			}
		}
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}
	walk(root)
	sort.Strings(dupes)
	if len(dupes) > 0 {
		t.Errorf("%d duplicated alias(es):\n  %s", len(dupes), strings.Join(dupes, "\n  "))
	}
}

// An alias must not collide with a live command name under the same parent, in
// either direction: cobra resolves the name first, so the alias is dead, and
// nothing reports it.
func TestNoAliasShadowsASiblingName(t *testing.T) {
	root := NewRootCmd("test", "test", "test", "test")
	var bad []string
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		names := map[string]bool{}
		for _, sub := range cmd.Commands() {
			names[sub.Name()] = true
		}
		for _, sub := range cmd.Commands() {
			for _, a := range sub.Aliases {
				if names[a] {
					bad = append(bad, cmd.CommandPath()+": alias "+a+" on "+sub.Name()+" is also a sibling's name")
				}
			}
		}
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}
	walk(root)
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Errorf("%d alias(es) shadowed by a live name:\n  %s", len(bad), strings.Join(bad, "\n  "))
	}
}
