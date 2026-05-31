// Copyright 2026, Jamf Software LLC

package commands

import (
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestProofAllCommandsHaveShort asserts every command in the tree carries a
// non-empty Short description. Short is what `--help`, the generated site
// (`commands -o json`), and agent tooling surface as the one-line summary; an
// empty Short ships a blank, unusable command. Empirically 0 violations today,
// so this is a regression guard: it fails the moment a new hand-written or
// generated command lands without a Short.
func TestProofAllCommandsHaveShort(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")

	var (
		violations []string
		inspected  int
	)
	// walkCommands does not invoke fn on hidden commands (though it still
	// descends into their children) per its contract in
	// destructive_annotations_test.go, so a hidden command with an empty Short
	// is intentionally not flagged.
	walkCommands(root, func(cmd *cobra.Command) {
		inspected++
		if strings.TrimSpace(cmd.Short) == "" {
			violations = append(violations, commandPath(cmd))
		}
	})

	// Floor sanity check: if the walk inspects almost nothing, the tree wiring
	// regressed and a "no violations" pass would be silent. Current tree is
	// ~1299 commands; 100 leaves room for churn without masking a major break.
	// Fatalf (not Errorf): a broken walk yields an empty violations list too, so
	// there is nothing useful to report alongside the floor failure.
	const minInspected = 100
	if inspected < minInspected {
		t.Fatalf("walked only %d commands (expected >= %d) — command tree wiring likely regressed", inspected, minInspected)
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Errorf("%d command(s) missing a non-empty Short:\n  %s\n\nFix by setting Short on each cobra.Command. For generated commands, edit the generator template (generator/parser/generator.go resourceTemplate, generator/classic/generator.go classicResourceTemplate, or generator/platform/template.go resourceTemplate) and run `make generate` — do not edit files under internal/commands/pro/generated/ or internal/commands/platform/generated/.",
			len(violations), strings.Join(violations, "\n  "))
	}
}
