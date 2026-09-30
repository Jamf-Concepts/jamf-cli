// Copyright 2026, Jamf Software LLC

package commands

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestMCPRefusedCommands_EveryEntryNamesACommandInTheTree(t *testing.T) {
	root := NewRootCmd("test", "t", "t", "t")
	for _, path := range mcpRefusedCommands {
		args := strings.Fields(strings.TrimPrefix(path, root.Name()+" "))
		found, _, err := root.Find(args)
		if err != nil || found.CommandPath() != path {
			t.Errorf("refused command %q resolves to %v (err %v); a stale entry refuses nothing", path, found, err)
		}
	}
	for path := range mcpDirFlags {
		args := strings.Fields(strings.TrimPrefix(path, root.Name()+" "))
		found, _, err := root.Find(args)
		if err != nil || found.CommandPath() != path || found.LocalNonPersistentFlags().Lookup("dir") == nil {
			t.Errorf("mcpDirFlags names %q, which does not resolve to a command declaring --dir (got %v, err %v)", path, found, err)
		}
	}
}

func TestMCPRefusedCommands_CoverEverySetupCommand(t *testing.T) {
	root := NewRootCmd("test", "t", "t", "t")
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Name() == "setup" {
			if err := refuseOverMCP(childInvocation{path: c.CommandPath()}, "prod"); err == nil {
				t.Errorf("%q writes configuration and is not refused over MCP; add it to mcpRefusedCommands", c.CommandPath())
			}
		}
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk(root)
}

// A command that parses no flags gets no flag check from run_command, so only
// a stub that makes no request may be one.
func TestMCPRefusedCommands_EveryUnparsedCommandIsANoAuthStub(t *testing.T) {
	root := NewRootCmd("test", "t", "t", "t")
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.DisableFlagParsing && c.Annotations[noAuthAnnotation] != "true" &&
			refuseOverMCP(childInvocation{path: c.CommandPath()}, "prod") == nil {
			t.Errorf("%q parses no flags, calls an API, and is not refused over MCP", c.CommandPath())
		}
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk(root)
}

func TestBuildChildArgs_RefusesShellCompletion(t *testing.T) {
	for _, args := range [][]string{
		{"__complete", "--profile", "other", "pro", "computers", "get", ""},
		{"-q", "__completeNoDesc", "pro", "diff", "--source", "other", ""},
	} {
		if got, err := buildChildArgs("prod", args); err == nil {
			t.Errorf("buildChildArgs(%q) = %q; cobra's completion command parses no flags of its own", args, got)
		}
	}
}

// isPathShapedFlag is the naming a flag whose value is a local path tends to
// take; each one must be classified before it ships.
func isPathShapedFlag(root *cobra.Command, f *pflag.Flag) bool {
	n := f.Name
	switch {
	case n == "file", n == "dir", n == "save-to", n == "input":
		return true
	case strings.HasSuffix(n, "-file"), strings.HasSuffix(n, "-files"), strings.HasSuffix(n, "-dir"):
		return true
	case n == "output":
		return f != root.PersistentFlags().Lookup("output")
	}
	return false
}

func TestMCPLocalPathFlags_EveryPathShapedFlagIsRefused(t *testing.T) {
	root := NewRootCmd("test", "t", "t", "t")
	used := map[string]bool{}
	checked := 0
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		path := strings.Fields(strings.TrimPrefix(c.CommandPath(), root.Name()))
		refusedWhole := refuseOverMCP(childInvocation{path: c.CommandPath()}, "prod") != nil
		c.LocalNonPersistentFlags().VisitAll(func(f *pflag.Flag) {
			_, byName := mcpLocalPathFlags[f.Name]
			_, byCommand := mcpDirFlags[c.CommandPath()]
			classified := f.Name == "dir" && byCommand || f.Name != "dir" && byName
			switch {
			case classified:
				used[f.Name] = true
				if refusedWhole {
					return
				}
			case refusedWhole, !isPathShapedFlag(root, f), isBlockedChildFlag("--" + f.Name):
				return
			default:
				t.Errorf("--%s on %q looks like a local path and is not classified; add it to mcpLocalPathFlags or mcpDirFlags", f.Name, c.CommandPath())
				return
			}
			forms := [][]string{{"--" + f.Name, modelChosenPath}, {"--" + f.Name + "=" + modelChosenPath}}
			if f.Shorthand != "" {
				forms = append(forms, []string{"-" + f.Shorthand + modelChosenPath})
			}
			for _, form := range forms {
				checked++
				args := append(append([]string{}, path...), form...)
				if _, err := buildChildArgs("prod", args); err == nil {
					t.Errorf("run_command would forward %q", args)
				}
			}
		})
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk(root)
	for name := range mcpLocalPathFlags {
		if !used[name] {
			t.Errorf("mcpLocalPathFlags names --%s, which no command declares; remove the stale entry", name)
		}
	}
	if checked < 400 {
		t.Fatalf("walk checked only %d path-flag forms; it has stopped matching the tree", checked)
	}
}

func TestBuildChildArgs_RefusesEverySpellingOfAnotherProfile(t *testing.T) {
	for _, args := range [][]string{
		{"-pother", "pro", "computers", "list"},
		{"pro", "computers", "list", "-np", "other"},
		{"pro", "computers", "list", "-qp", "other"},
		{"--profile=other", "pro", "computers", "list"},
		{"--profile", "prod", "pro", "computers", "list"},
	} {
		if got, err := buildChildArgs("prod", args); err == nil {
			t.Errorf("buildChildArgs(%q) = %q; a model-supplied --profile must be refused, even one naming the pinned profile", args, got)
		}
	}
	// Ahead of the command path a clustered -p cannot take the next token:
	// cobra reads it as a command name and the child exits before running.
	front := []string{"--profile", "prod", "--no-input", "-np", "other", "pro", "computers", "list"}
	if _, _, err := NewRootCmd("test", "t", "t", "t").Find(front); err == nil {
		t.Errorf("cobra resolves %q; it must refuse it as an unknown command", front)
	}
}

func TestBuildChildArgs_KeepsDryRunVerboseHelpAndFormat(t *testing.T) {
	allowed := [][]string{
		{"pro", "classic-policies", "create", "--set", "general.name=x", "--dry-run"},
		{"pro", "classic-policies", "create", "--set", "general.name=x", "-n"},
		{"pro", "computers", "list", "-vvv"},
		{"pro", "computers", "list", "-v"},
		{"pro", "computers", "list", "--help"},
		{"pro", "computers", "list", "-oplain"},
		{"-o", "json", "pro", "computers", "list"},
		{"commands", "-o", "json"},
		{"help", "config", "set-default"},
		{"config", "list"},
		{"pro", "report", "software-installs", "--path"},
	}
	for _, args := range allowed {
		if _, err := buildChildArgs("prod", args); err != nil {
			t.Errorf("buildChildArgs(%q) = %v; this form must stay available over MCP", args, err)
		}
	}
}

func TestPinnedChildEnv_ReplacesAnInheritedPin(t *testing.T) {
	t.Setenv(mcpPinnedProfileEnvVar, "attacker")
	env := strings.Join(pinnedChildEnv("prod"), "\n")
	if strings.Contains(env, mcpPinnedProfileEnvVar+"=attacker") {
		t.Error("an inherited pin must not reach the child")
	}
	if !strings.Contains(env, mcpPinnedProfileEnvVar+"=prod") {
		t.Errorf("the child must be told the pinned profile: %s", env)
	}
}

// isMCPRefusal matches the refusal wording itself, since a temp path in an
// unrelated error carries the test's name.
func isMCPRefusal(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "over MCP") || strings.Contains(err.Error(), "the MCP server is pinned"))
}

// executeAsMCPChild runs args in-process as a child spawned by `mcp serve`
// pinned to pinned, without going through buildChildArgs.
func executeAsMCPChild(t *testing.T, pinned string, args ...string) error {
	t.Helper()
	resetGlobals()
	t.Cleanup(resetGlobals)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("JAMF_CLI_ARGS", "")
	t.Setenv(mcpChildEnvVar, "1")
	t.Setenv(mcpPinnedProfileEnvVar, pinned)
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	root.SetArgs(args)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	return root.Execute()
}

func TestMCPChild_RefusesWhatTheServerRefuses(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "hijacked.json")
	refused := [][]string{
		{"--profile", "prod", "--no-input", "cfg", "set-default", "x"},
		{"--profile", "prod", "--no-input", "-q", "multi", "--", "pro", "computers", "list"},
		{"--profile", "other", "--no-input", "config", "list"},
		{"--no-input", "--profile", "prod", "pro", "diff", "--source", "/var/empty", "--target", "other"},
		{"--profile", "prod", "--no-input", "pro", "scripts", "create", "--script-file", "/etc/passwd", "-n"},
		{"--profile", "prod", "--no-input", "pro", "computers", "list", "--out-file", outFile},
		{"--profile", "prod", "--no-input", "__complete", "--profile", "other", "pro", "computers", "get", ""},
	}
	for _, args := range refused {
		err := executeAsMCPChild(t, "prod", args...)
		if !isMCPRefusal(err) {
			t.Errorf("MCP child ran %q (err %v); it must refuse on its own parse", args, err)
		}
	}
	if _, err := os.Stat(outFile); err == nil {
		t.Error("--out-file was created before the refusal ran")
	}
	if err := executeAsMCPChild(t, "prod", "--profile", "prod", "--no-input", "config", "list"); err != nil {
		t.Errorf("the pinned profile's own --profile must not be refused in the child: %v", err)
	}
}

func TestMCPChild_JcdsSyncRefusedWithoutTheServer(t *testing.T) {
	victim := t.TempDir()
	if err := os.WriteFile(filepath.Join(victim, "id_ed25519"), []byte("operator data"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JAMF_URL", "http://127.0.0.1:1")
	t.Setenv("JAMF_TOKEN", "fake-token")
	for _, mount := range jcdsSyncMounts {
		args := append(append([]string{"--no-input"}, mount...), "--dir", victim, "--delete")
		if err := executeAsMCPChild(t, "", args...); !isMCPRefusal(err) {
			t.Errorf("MCP child ran %q (err %v)", args, err)
		}
	}
	if _, err := os.Stat(filepath.Join(victim, "id_ed25519")); err != nil {
		t.Errorf("operator file is gone: %v", err)
	}
}
