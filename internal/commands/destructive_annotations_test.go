// Copyright 2026, Jamf Software LLC

package commands

import (
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// destructiveVerbs maps the first whitespace-separated token of a command's
// `Use` field to whether the command is inherently destructive. Any command
// whose verb appears here MUST carry the `jamf:destructive` annotation
// (enforced by TestDestructiveVerbCommandsAreAnnotated below).
//
// This set must stay in lockstep with the generator's `isDestructiveAction`
// predicate (generator/parser/parser.go) so the contract is symmetric across
// generated (Platform device-actions) and hand-written (pro_device_actions.go)
// surfaces — otherwise the same verb ends up annotated on one side and
// silently exempt on the other, which is the exact gap the verifier exists
// to catch.
//
// Add new verbs here when introducing a destructive operation pattern. Do not
// add verbs that are merely *named* with a destructive word but operate on
// scope/policy/etc. without actually destroying user data — e.g. a `delete-rule`
// subcommand that edits a config file is not destructive in the MDM sense.
var destructiveVerbs = map[string]bool{
	"delete":            true,
	"delete-multiple":   true,
	"delete-user":       true,
	"bulk-delete":       true,
	"wipe":              true,
	"erase":             true,
	"flush-commands":    true,
	"remove-mdm":        true,
	"lock":              true,
	"restart":           true,
	"shutdown":          true,
	"unmanage":          true,
	"disable-lost-mode": true,
}

// hasYesFlag reports whether the command exposes a `--yes` confirmation flag
// via its own FlagSet, its inherited persistent flags, or a parent's
// persistent flags. The `--yes` convention is jamf-cli's established gate for
// suppressing the interactive confirmation prompt on destructive operations.
func hasYesFlag(cmd *cobra.Command) bool {
	if cmd.Flags().Lookup("yes") != nil {
		return true
	}
	if cmd.PersistentFlags().Lookup("yes") != nil {
		return true
	}
	if cmd.InheritedFlags().Lookup("yes") != nil {
		return true
	}
	return false
}

// firstWord extracts the leading token of a cobra `Use` string. `"delete <id>"`
// → `"delete"`. Used for verb-based destructive detection so commands declared
// as `"delete [name]"` or `"delete <id> [flags]"` all match consistently.
func firstWord(use string) string {
	use = strings.TrimSpace(use)
	if idx := strings.IndexAny(use, " \t"); idx > 0 {
		return use[:idx]
	}
	return use
}

// commandPath builds a space-separated path from the root down to cmd, e.g.
// "jamf-cli pro computers delete". Used in violation reports so a failing
// test names a path the developer can paste into their shell to reproduce.
func commandPath(cmd *cobra.Command) string {
	parts := []string{}
	for c := cmd; c != nil; c = c.Parent() {
		parts = append([]string{firstWord(c.Use)}, parts...)
	}
	return strings.Join(parts, " ")
}

// walkCommands invokes fn on every visible command in the tree rooted at
// root, including the root itself. Hidden commands are skipped from
// inspection but their children are still descended into — cobra lets a
// hidden parent's subcommands execute, so the destructive-annotation
// contract still applies to those subcommands.
func walkCommands(root *cobra.Command, fn func(*cobra.Command)) {
	if !root.Hidden {
		fn(root)
	}
	for _, child := range root.Commands() {
		walkCommands(child, fn)
	}
}

// TestDestructiveCommandsHaveYesFlag asserts the load-bearing half of the
// `jamf:destructive` annotation contract: any command marked destructive must
// expose `--yes` somewhere in its flag hierarchy so callers can suppress the
// interactive confirmation. A failure here means the annotation is decorative
// and the command's destructive action isn't gated against accidental use.
func TestDestructiveCommandsHaveYesFlag(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")

	// minDestructiveCommands is a floor sanity check — if the count ever
	// drops below this, the generator template's annotation emission likely
	// regressed (typo in key, predicate stopped firing, etc.) and the
	// "no violations" pass would be silent. Current count is ~280 across
	// generated delete operations + Platform action verbs + hand-written
	// destructive Pro/Protect/School commands; 100 leaves room for resource
	// churn without being so low that a major regression slips through.
	const minDestructiveCommands = 100

	var (
		violations []string
		annotated  int
	)
	walkCommands(root, func(cmd *cobra.Command) {
		if cmd.Annotations["jamf:destructive"] != "true" {
			return
		}
		annotated++
		if !hasYesFlag(cmd) {
			violations = append(violations, commandPath(cmd))
		}
	})

	if annotated < minDestructiveCommands {
		t.Errorf("only %d commands carry jamf:destructive annotation (expected >= %d) — the template emission likely regressed", annotated, minDestructiveCommands)
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Errorf("%d destructive command(s) missing --yes flag:\n  %s\n\nFix by adding a --yes flag to each, or remove the jamf:destructive annotation if the command isn't actually destructive.",
			len(violations), strings.Join(violations, "\n  "))
	}
}

// TestDestructiveVerbCommandsAreAnnotated asserts the inverse half: any
// command whose verb implies destruction (per destructiveVerbs) must carry
// the `jamf:destructive` annotation. A failure here means the command name
// promises destruction but the metadata that future verifiers (MCP host
// destructiveHint, future structural checks) consume isn't set — the
// command will be treated as benign by tooling that reads the annotation.
func TestDestructiveVerbCommandsAreAnnotated(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")

	var violations []string
	walkCommands(root, func(cmd *cobra.Command) {
		verb := firstWord(cmd.Use)
		if !destructiveVerbs[verb] {
			return
		}
		if cmd.Annotations["jamf:destructive"] != "true" {
			violations = append(violations, commandPath(cmd))
		}
	})

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Errorf("%d destructive-verb command(s) missing jamf:destructive annotation:\n  %s\n\nFix by adding Annotations: map[string]string{\"jamf:destructive\": \"true\"} to each cobra.Command. If a name collision (e.g. \"delete\" used on a non-destructive subcommand) is intentional, broaden the destructiveVerbs map exception list instead.",
			len(violations), strings.Join(violations, "\n  "))
	}
}
