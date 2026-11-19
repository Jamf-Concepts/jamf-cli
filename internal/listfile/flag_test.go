// Copyright 2026, Jamf Software LLC

package listfile

import (
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

// withStdin points Read's stdin at content for the duration of the test and
// forgets any stdin an earlier test memoised.
func withStdin(t *testing.T, content string) {
	t.Helper()
	prev := stdinReader
	stdinReader = strings.NewReader(content)
	stdinOnce, stdinData, stdinErr = sync.Once{}, nil, nil
	t.Cleanup(func() {
		stdinReader = prev
		stdinOnce, stdinData, stdinErr = sync.Once{}, nil, nil
	})
}

func TestRead_StdinIsReadOnceAndServedAgain(t *testing.T) {
	withStdin(t, "# header\n42\nC02X1234\n")

	for range 2 {
		entries, err := Read(Stdin)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 2 || entries[0].Value != "42" || entries[1].Value != "C02X1234" || entries[1].Line != 3 {
			t.Fatalf("Read(-) = %+v", entries)
		}
	}
}

func TestRead_StdinRefusalNamesStdin(t *testing.T) {
	withStdin(t, "ok\nbad\x01line\n")
	_, err := Read(Stdin)
	if err == nil || !strings.HasPrefix(err.Error(), "stdin line 2 ") {
		t.Fatalf("err = %v, want it to name stdin line 2", err)
	}
}

// TestRead_DashIsStdinNotAFile pins that "-" is never opened as a path, so a
// file called "-" in the working directory cannot stand in for stdin.
func TestRead_DashIsStdinNotAFile(t *testing.T) {
	withStdin(t, "7\n")
	t.Chdir(t.TempDir())
	if err := os.WriteFile("-", []byte("not-this\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := Read(Stdin)
	if err != nil || len(entries) != 1 || entries[0].Value != "7" {
		t.Fatalf("Read(-) = %+v, %v", entries, err)
	}
}

func TestSourceName(t *testing.T) {
	if got := SourceName(Stdin); got != "stdin" {
		t.Errorf("SourceName(-) = %q", got)
	}
	if got := SourceName("/tmp/x.txt"); got != "/tmp/x.txt" {
		t.Errorf("SourceName(path) = %q", got)
	}
}

func TestAddFlag_Usage(t *testing.T) {
	var v string
	cmd := &cobra.Command{Use: "x"}
	AddFlag(cmd, &v, "widgets: ID or name")
	f := cmd.Flags().Lookup(FlagName)
	if f == nil || f.Usage != "file listing widgets: ID or name (one per line, # comments ignored; - reads stdin)" {
		t.Fatalf("usage = %v", f)
	}
	if cmd.Flags().Lookup(LegacyFlagName) != nil {
		t.Error("AddFlag without WithLegacyAlias registered --from-file")
	}

	cmd = &cobra.Command{Use: "y"}
	AddFlag(cmd, &v, "URLs", WithoutStdin())
	if u := cmd.Flags().Lookup(FlagName).Usage; strings.Contains(u, "stdin") {
		t.Errorf("WithoutStdin usage still offers stdin: %q", u)
	}
}

func TestAddFlag_LegacyAliasSharesTheVariable(t *testing.T) {
	var v string
	cmd := &cobra.Command{Use: "x", RunE: func(*cobra.Command, []string) error { return nil }}
	AddFlag(cmd, &v, "widgets", WithLegacyAlias())

	alias := cmd.Flags().Lookup(LegacyFlagName)
	if alias == nil || alias.Deprecated == "" || !strings.Contains(alias.Deprecated, "--"+FlagName) {
		t.Fatalf("--from-file is not a deprecated alias naming --list-file: %+v", alias)
	}
	if !strings.Contains(alias.Deprecated, LegacyAliasRemoval) {
		t.Errorf("deprecation message does not say when the alias goes: %q", alias.Deprecated)
	}

	if err := cmd.ParseFlags([]string{"--from-file", "old.txt"}); err != nil {
		t.Fatal(err)
	}
	if v != "old.txt" {
		t.Errorf("alias did not set the shared variable: %q", v)
	}
	if !Changed(cmd) {
		t.Error("Changed is false after the alias was given")
	}

	cmd.SetArgs([]string{"--from-file", "a", "--list-file", "b"})
	if err := cmd.Execute(); err == nil {
		t.Error("--from-file and --list-file were accepted together")
	}
}

func TestMarkExclusive_CoversTheAlias(t *testing.T) {
	var v, name string
	newCmd := func() *cobra.Command {
		cmd := &cobra.Command{Use: "x", RunE: func(*cobra.Command, []string) error { return nil }}
		AddFlag(cmd, &v, "widgets", WithLegacyAlias())
		cmd.Flags().StringVar(&name, "name", "", "")
		MarkExclusive(cmd, "name")
		return cmd
	}
	for _, flag := range []string{"--list-file", "--from-file"} {
		cmd := newCmd()
		cmd.SetArgs([]string{flag, "a", "--name", "b"})
		if err := cmd.Execute(); err == nil {
			t.Errorf("%s and --name were accepted together", flag)
		}
	}
}

// TestIsListFlag_BodyFromFileIsNotAList pins that a command whose --from-file
// is a request body is never read as a list.
func TestIsListFlag_BodyFromFileIsNotAList(t *testing.T) {
	var v string
	cmd := &cobra.Command{Use: "create"}
	cmd.Flags().StringVar(&v, LegacyFlagName, "", "Path to the JSON or YAML request body")
	if IsListFlag(cmd, LegacyFlagName) {
		t.Error("a body --from-file reads as a list flag")
	}
	_ = cmd.Flags().Set(LegacyFlagName, "body.json")
	if Changed(cmd) {
		t.Error("Changed is true for a body --from-file")
	}
}

func TestRefuseStdin(t *testing.T) {
	if err := RefuseStdin("list.txt", "why"); err != nil {
		t.Errorf("RefuseStdin(path) = %v", err)
	}
	err := RefuseStdin(Stdin, "it prompts on stdin")
	if exitcode.CodeFrom(err) != exitcode.Usage || !strings.Contains(err.Error(), "it prompts on stdin") {
		t.Errorf("RefuseStdin(-) = %v (code %d)", err, exitcode.CodeFrom(err))
	}
}
