// Copyright 2026, Jamf Software LLC

package commands

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func timeoutErr(hint string) error {
	return exitcode.Wrap(exitcode.General, fmt.Errorf("%w: gateway timeout", registry.ErrServerTimeout)).WithHint(hint)
}

func hintFrom(t *testing.T, err error) string {
	t.Helper()
	var e *exitcode.Error
	if !errors.As(err, &e) {
		t.Fatalf("%v is not an *exitcode.Error", err)
	}
	return e.Hint
}

// Issue 476: `pro patch-software-title-configurations list` has no paging or
// filter flag, and a timeout on it advised --page-size, --section and --filter.
func TestRefineTimeoutHint_NamesOnlyFlagsTheCommandDeclares(t *testing.T) {
	cases := []struct {
		name  string
		flags []string
		want  []string
		not   []string
	}{
		{"no narrowing flag", nil, []string{"no flag that narrows"}, []string{"--page-size", "--section", "--filter"}},
		{"page-size only", []string{"page-size"}, []string{"smaller --page-size"}, []string{"--section", "--filter"}},
		{"filter only", []string{"filter"}, []string{"narrow it with --filter"}, []string{"--page-size", "--section"}},
		{"all three", []string{"page-size", "section", "filter"}, []string{"smaller --page-size", "--section or --filter"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "x"}
			for _, f := range tc.flags {
				cmd.Flags().String(f, "", "")
			}
			got := hintFrom(t, RefineTimeoutHint(cmd, timeoutErr(registry.ServerTimeoutHint)))
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("hint %q lacks %q", got, w)
				}
			}
			for _, n := range tc.not {
				if strings.Contains(got, n) {
					t.Errorf("hint %q names %q, which the command does not declare", got, n)
				}
			}
		})
	}
}

func TestRefineTimeoutHint_LeavesOtherErrorsAlone(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	cmd.Flags().String("filter", "", "")

	// A write's hint is about state, not size.
	const applied = "the request reached the server and may have been applied — check the object's current state before retrying"
	if got := hintFrom(t, RefineTimeoutHint(cmd, timeoutErr(applied))); got != applied {
		t.Errorf("a write's hint was rewritten to %q", got)
	}
	// An error that is not a server timeout keeps whatever hint it has.
	other := exitcode.New(exitcode.General, "boom").WithHint(registry.ServerTimeoutHint)
	if got := hintFrom(t, RefineTimeoutHint(cmd, other)); got != registry.ServerTimeoutHint {
		t.Errorf("a non-timeout error's hint was rewritten to %q", got)
	}
	if RefineTimeoutHint(cmd, nil) != nil {
		t.Error("nil error must stay nil")
	}
}

// Whatever hint a timeout ends with, on any command in any namespace, may name
// only flags that command declares. Walks the whole assembled tree, so a new
// generated or hand-written command is covered without editing this test.
func TestEveryLeafTimeoutHintNamesOnlyItsOwnFlags(t *testing.T) {
	flagRef := regexp.MustCompile(`--[a-z][a-z-]*`)
	leaves := runnableLeaves(NewRootCmd("test", "none", "none", "none"))
	if len(leaves) < 500 {
		t.Fatalf("walked %d leaves; the tree walk is vacuous", len(leaves))
	}
	for _, l := range leaves {
		hint := hintFrom(t, RefineTimeoutHint(l.cmd, timeoutErr(registry.ServerTimeoutHint)))
		for _, ref := range flagRef.FindAllString(hint, -1) {
			if l.cmd.Flags().Lookup(strings.TrimPrefix(ref, "--")) == nil {
				t.Errorf("%s: timeout hint names %s, which the command does not declare: %q", l.path, ref, hint)
			}
		}
	}
}

// The reported command, pinned by name.
func TestTimeoutHint_PatchSoftwareTitleConfigurationsList(t *testing.T) {
	root := NewRootCmd("test", "none", "none", "none")
	cmd, _, err := root.Find([]string{"pro", "patch-software-title-configurations", "list"})
	if err != nil || cmd == nil || cmd.Name() != "list" {
		t.Fatalf("command not found: %v", err)
	}
	got := hintFrom(t, RefineTimeoutHint(cmd, timeoutErr(registry.ServerTimeoutHint)))
	for _, bad := range []string{"--page-size", "--section", "--filter"} {
		if cmd.Flags().Lookup(strings.TrimPrefix(bad, "--")) == nil && strings.Contains(got, bad) {
			t.Errorf("hint %q names %s", got, bad)
		}
	}
}
