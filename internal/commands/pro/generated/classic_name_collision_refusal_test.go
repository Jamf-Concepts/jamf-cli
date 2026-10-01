// Copyright 2026, Jamf Software LLC

package generated

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/spf13/cobra"
)

// TestClassicNameCollisionRefusalIsAUsageErrorWithAHint: the refusal exits 2,
// keeps the typed collision reachable, and its hint tells the caller to rerun
// the same command with an ID as <id> rather than the name.
func TestClassicNameCollisionRefusalIsAUsageErrorWithAHint(t *testing.T) {
	cases := []struct {
		verb     string
		newCmd   func(*registry.CLIContext) *cobra.Command
		args     []string
		wantHint string
	}{
		{"update", newClassicPoliciesUpdateCmd, []string{"update", "--no-input", "--name", "Baseline", "--from-file", ""}, "run update again with one of these IDs as <id> in place of --name"},
		{"delete", newClassicPoliciesDeleteCmd, []string{"delete", "--no-input", "--yes", "--name", "Baseline"}, "run delete again with one of these IDs as <id> in place of --name"},
		{"apply", newClassicPoliciesApplyCmd, []string{"apply", "--no-input", "--yes", "--from-file", ""}, "rename one of the records so the name is unique, or run update with one of these IDs as <id>"},
	}
	for _, tc := range cases {
		t.Run(tc.verb, func(t *testing.T) {
			server := &duplicateNameClassicServer{apiPath: "policies", root: "policy", listRoot: "policies", ids: []string{"11", "22"}, serverPick: "22"}
			stdinFromDevNull(t)
			for i, a := range tc.args {
				if a == "" {
					tc.args[i] = writeXML(t, "<policy><general><name>Baseline</name></general></policy>")
				}
			}
			err := runClassicCmd(tc.newCmd, server, tc.args...)
			if err == nil {
				t.Fatalf("%s on a shared name succeeded; writes %v", tc.verb, server.writes())
			}
			if code := exitcode.CodeFrom(err); code != exitcode.Usage {
				t.Errorf("exit code = %d, want %d (Usage); err %v", code, exitcode.Usage, err)
			}
			var collision *ClassicNameCollisionError
			if !errors.As(err, &collision) || strings.Join(collision.IDs, ",") != "11,22" {
				t.Errorf("err %v should wrap a collision naming 11 and 22", err)
			}
			var e *exitcode.Error
			if !errors.As(err, &e) || e.Hint != tc.wantHint {
				t.Errorf("hint = %q, want %q", hintOf(err), tc.wantHint)
			}
			if w := server.writes(); len(w) != 0 {
				t.Errorf("no write may be sent on a name collision, got %v", w)
			}
		})
	}
}

func hintOf(err error) string {
	var e *exitcode.Error
	if errors.As(err, &e) {
		return e.Hint
	}
	return ""
}

// TestClassicNameCollisionChooserShowsTheName: each candidate the operator is
// asked to pick from carries its name beside its ID.
func TestClassicNameCollisionChooserShowsTheName(t *testing.T) {
	server := &duplicateNameClassicServer{apiPath: "policies", root: "policy", listRoot: "policies", ids: []string{"11", "22"}, serverPick: "22"}
	pipeStdin(t, "0\n")
	stdinIsTerminal(t, true)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	runErr := runClassicCmd(newClassicPoliciesDeleteCmd, server, "delete", "--yes", "--name", "Baseline")
	os.Stderr = orig
	_ = w.Close()
	out, _ := io.ReadAll(r)

	if runErr == nil {
		t.Fatalf("cancelling the pick should abort; writes %v", server.writes())
	}
	for _, want := range []string{"[1] ID: 11  Name: Baseline", "[2] ID: 22  Name: Baseline"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("chooser output missing %q:\n%s", want, out)
		}
	}
}
