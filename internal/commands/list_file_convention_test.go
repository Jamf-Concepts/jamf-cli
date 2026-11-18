// Copyright 2026, Jamf Software LLC

package commands

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/listfile"
)

// listUsageWords are what a usage string says when the flag takes a list of
// identifiers rather than a request body.
var listUsageWords = []string{"listing", "one per line", "IDs or names", "ID or name"}

// TestFromFileIsAlwaysARequestBody holds the split issue #342 made: --list-file
// takes a list, --from-file takes a request body, and a reader can tell which
// from the name alone.
//
// The two used to share --from-file — a body on ~460 leaves, a list of IDs,
// serials or names on ~120 — so an agent could not tell from the flag whether a
// command wanted JSON or a file of serial numbers, and `computer-inventory
// erase`, which needs both, had to rename its body to --body-file. Every leaf
// that took a list through --from-file now keeps it only as listfile's hidden,
// deprecated alias of --list-file.
func TestFromFileIsAlwaysARequestBody(t *testing.T) {
	root := NewRootCmd("test", "none", "none", "none")
	var bodies, aliases, lists int
	walkCommands(root, func(cmd *cobra.Command) {
		path := cmd.CommandPath()
		if f := cmd.LocalNonPersistentFlags().Lookup(listfile.LegacyFlagName); f != nil {
			if f.Deprecated != "" {
				aliases++
				if !listfile.IsListFlag(cmd, listfile.LegacyFlagName) || cmd.Flags().Lookup(listfile.FlagName) == nil {
					t.Errorf("%s: deprecated --from-file with no --list-file beside it", path)
				}
			} else {
				bodies++
				for _, w := range listUsageWords {
					if strings.Contains(f.Usage, w) {
						t.Errorf("%s: --from-file %q describes a list; a list is --list-file (listfile.AddFlag)", path, f.Usage)
						break
					}
				}
			}
		}
		if f := cmd.LocalNonPersistentFlags().Lookup(listfile.FlagName); f != nil {
			lists++
			if !strings.HasPrefix(f.Usage, "file listing ") || !strings.Contains(f.Usage, "(one per line, # comments ignored") {
				t.Errorf("%s: --list-file usage %q was not registered through listfile.AddFlag", path, f.Usage)
			}
		}
	})
	t.Logf("%d body --from-file, %d deprecated aliases, %d --list-file", bodies, aliases, lists)
	// Floors, so a walk that stopped reaching the tree fails rather than passes
	// with nothing checked.
	if bodies < 400 || aliases < 100 || lists < aliases {
		t.Fatalf("walk saw %d body --from-file, %d deprecated aliases, %d --list-file", bodies, aliases, lists)
	}
}

// TestListFileAliasIsTheSameList pins that the deprecated --from-file on a list
// command sets the very variable --list-file does, on a hand-written command
// and on a generated one.
func TestListFileAliasIsTheSameList(t *testing.T) {
	root := NewRootCmd("test", "none", "none", "none")
	for _, path := range [][]string{
		{"pro", "computer-inventory", "blank-push"},
		{"pro", "classic-computer-groups", "add-members"},
		{"pro", "classic-policies", "disable"},
		{"pro", "buildings", "delete"},
		{"pro", "classic-printers", "delete"},
		{"multi"},
		{"pro", "setup"},
	} {
		cmd, _, err := root.Find(path)
		if err != nil || cmd.Name() != path[len(path)-1] {
			t.Fatalf("%v: not found", path)
		}
		if err := cmd.Flags().Set(listfile.LegacyFlagName, "ids.txt"); err != nil {
			t.Fatalf("%v: %v", path, err)
		}
		if got := cmd.Flags().Lookup(listfile.FlagName).Value.String(); got != "ids.txt" {
			t.Errorf("%v: --from-file did not reach --list-file's variable (got %q)", path, got)
		}
	}
}

// listFileGetExemptions are the `get` leaves that name one record and still
// take no --list-file, each with the reason. A stale entry fails the test.
var listFileGetExemptions = map[string]string{
	"jamf-cli pro mobile-device-groups get": "a paginated membership read: one group's members walked page by page, not one record",
	// The scope commands read and print one resource's scope as a table of
	// categories; a list of resources has no single scope to show.
	"jamf-cli pro classic-ebooks scope get":                 "one resource's scope table",
	"jamf-cli pro classic-mac-apps scope get":               "one resource's scope table",
	"jamf-cli pro classic-macos-config-profiles scope get":  "one resource's scope table",
	"jamf-cli pro classic-mobile-apps scope get":            "one resource's scope table",
	"jamf-cli pro classic-mobile-config-profiles scope get": "one resource's scope table",
	"jamf-cli pro classic-policies scope get":               "one resource's scope table",
	"jamf-cli pro classic-restricted-software scope get":    "one resource's scope table",
	"jamf-cli pro classic-vpp-assignments scope get":        "one resource's scope table",
	"jamf-cli pro classic-vpp-invitations scope get":        "one resource's scope table",
	"jamf-cli pro blueprints get":                           "Platform API: hand-written, not yet migrated",
	"jamf-cli pro blueprint-components get":                 "Platform API: hand-written, not yet migrated",
	"jamf-cli pro platform-device-groups get":               "Platform API: hand-written, not yet migrated",
	"jamf-cli pro compliance-benchmarks get":                "Platform API generator: not yet migrated",
}

// TestEveryNamedGetTakesAListFile holds issue #342's promise: a `get` that
// can find one record by name or ID can read a list of them with --list-file.
// Every Classic `get`, and every Pro `get` that declares --name, has it or is
// exempted above with a reason.
func TestEveryNamedGetTakesAListFile(t *testing.T) {
	root := NewRootCmd("test", "none", "none", "none")
	seen := map[string]bool{}
	checked := 0
	walkCommands(root, func(cmd *cobra.Command) {
		if cmd.Name() != "get" || cmd.HasSubCommands() {
			return
		}
		path := cmd.CommandPath()
		classic := cmd.Annotations[annotationAPI] == apiProClassic
		named := cmd.LocalNonPersistentFlags().Lookup("name") != nil
		if !strings.HasPrefix(path, "jamf-cli pro ") || (!classic && !named) {
			return
		}
		if _, ok := listFileGetExemptions[path]; ok {
			seen[path] = true
			return
		}
		checked++
		if cmd.LocalNonPersistentFlags().Lookup(listfile.FlagName) == nil {
			t.Errorf("%s: names one record but takes no --list-file (exempt it in listFileGetExemptions with a reason, or add it)", path)
		}
	})
	for path := range listFileGetExemptions {
		if !seen[path] {
			t.Errorf("listFileGetExemptions: %q is stale; no such named get remains without --list-file", path)
		}
	}
	if checked < 90 {
		t.Fatalf("walk checked %d gets; expected the Pro and Classic tree", checked)
	}
}
