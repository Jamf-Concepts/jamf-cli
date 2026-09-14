// Copyright 2026, Jamf Software LLC

package commands

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/scope"
)

// findScopeLeaf walks the assembled tree for one resource's scope subcommand.
func findScopeLeaf(t *testing.T, root *cobra.Command, path ...string) *cobra.Command {
	t.Helper()
	cur := root
	for _, name := range path {
		var next *cobra.Command
		for _, c := range cur.Commands() {
			if c.Name() == name {
				next = c
				break
			}
		}
		if next == nil {
			t.Fatalf("no %q under %q", name, cur.CommandPath())
		}
		cur = next
	}
	return cur
}

// TestScopeLeavesCarryTheirCategoryVocabulary pins the annotation the
// unknown-flag hint depends on. Registering only a resource's own categories
// is what keeps --help and completion honest, and the cost is that a category
// from another device family arrives as cobra's "unknown flag" before the
// scope matrix can explain it — so the vocabulary has to travel on the
// command.
func TestScopeLeavesCarryTheirCategoryVocabulary(t *testing.T) {
	root := NewRootCmd("test", "test", "test", "test")

	cases := []struct {
		resource string
		wants    []string
		lacks    []string
	}{
		{"classic-policies", []string{"--computer-group", "--ibeacon"}, []string{"--mobile-device-group", "--class"}},
		{"classic-mobile-config-profiles", []string{"--mobile-device-group", "--ibeacon"}, []string{"--computer-group", "--class"}},
		{"classic-ebooks", []string{"--class", "--computer-group", "--mobile-device-group"}, []string{"--ibeacon"}},
		{"classic-restricted-software", []string{"--computer-group", "--user"}, []string{"--network-segment", "--ibeacon", "--jss-user"}},
		{"classic-vpp-assignments", []string{"--jss-user-group", "--user-group"}, []string{"--computer-group", "--building"}},
	}

	for _, tc := range cases {
		for _, verb := range []string{"add", "remove"} {
			leaf := findScopeLeaf(t, root, "pro", tc.resource, "scope", verb)
			cats := leaf.Annotations[scope.AnnotationCategories]
			if cats == "" {
				t.Errorf("%s scope %s: no %s annotation, so an unknown flag gets no hint", tc.resource, verb, scope.AnnotationCategories)
				continue
			}
			for _, want := range tc.wants {
				if !strings.Contains(cats, want) {
					t.Errorf("%s scope %s: vocabulary %q is missing %s", tc.resource, verb, cats, want)
				}
				if leaf.Flags().Lookup(strings.TrimPrefix(want, "--")) == nil {
					t.Errorf("%s scope %s: %s is in the vocabulary but not registered as a flag", tc.resource, verb, want)
				}
			}
			for _, lacks := range tc.lacks {
				if strings.Contains(cats, lacks) {
					t.Errorf("%s scope %s: vocabulary %q should not offer %s", tc.resource, verb, cats, lacks)
				}
				if leaf.Flags().Lookup(strings.TrimPrefix(lacks, "--")) != nil {
					t.Errorf("%s scope %s: %s is registered but is not a category of this resource", tc.resource, verb, lacks)
				}
			}
		}
	}
}

// TestScopeLeavesTakeAnIDOrAName pins the identifier convention across the
// whole scope surface: an optional `<id>` positional plus --name, the same as
// every other Classic command. These leaves used to take the name as a bare
// required positional — the only commands in the binary that did.
func TestScopeLeavesTakeAnIDOrAName(t *testing.T) {
	root := NewRootCmd("test", "test", "test", "test")
	resources := []string{
		"classic-policies", "classic-macos-config-profiles", "classic-mobile-config-profiles",
		"classic-mac-apps", "classic-mobile-apps", "classic-ebooks",
		"classic-restricted-software", "classic-vpp-assignments", "classic-vpp-invitations",
	}
	for _, resource := range resources {
		for _, verb := range []string{"get", "add", "remove"} {
			leaf := findScopeLeaf(t, root, "pro", resource, "scope", verb)
			if !strings.Contains(leaf.Use, "[<id>]") {
				t.Errorf("%s scope %s: Use = %q, want an optional <id> positional", resource, verb, leaf.Use)
			}
			if leaf.Flags().Lookup("name") == nil {
				t.Errorf("%s scope %s: no --name flag", resource, verb)
			}
			// One positional at most: the guard in root.go clamps a leaf whose
			// Use documents no placeholder, and two would mean the command
			// silently discards the second.
			if err := leaf.Args(leaf, []string{"1", "2"}); err == nil {
				t.Errorf("%s scope %s: accepted two positionals", resource, verb)
			}
		}
	}
}
