// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
)

func TestMCPRefusedCommands_EveryEntryNamesACommandInTheTree(t *testing.T) {
	root := NewRootCmd("test", "t", "t", "t")
	for _, refused := range mcpRefusedCommands {
		path := refused.path
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
	case f.Value.Type() == "bool":
		return false
	}
	return pathShapedUsage.MatchString(f.Usage)
}

var pathShapedUsage = regexp.MustCompile(`(?i)\b(path|file|directory)\b`)

// notALocalPathFlags are flags whose usage text mentions a path, file or
// directory without taking a path on this machine, each with the reason.
var notALocalPathFlags = map[string]string{
	"set":                   "a body field assignment; its usage names --from-file as the exclusive alternative",
	"custom-payload-domain": "a preference domain; its usage names --custom-payload-file",
	"name":                  "a Jamf object or remote file name, looked up on the server",
	"file-name":             "a file name in a distribution point, looked up on the server",
	"type":                  "an enum naming a file or exception type",
	"export-labels":         "column labels for a server-side export",
	"user":                  "a directory-service username in a scope",
	"user-group":            "a directory-service group in a scope",
	"prefix":                "a command path in the catalog",
	"search":                "words matched against command paths",
	"source":                "a pro diff side, judged by refuseDiffSide",
	"target":                "a pro diff side, judged by refuseDiffSide",
}

func TestMCPLocalPathFlags_EveryPathShapedFlagIsRefused(t *testing.T) {
	root := NewRootCmd("test", "t", "t", "t")
	used := map[string]bool{}
	exempted := map[string]bool{}
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
			case notALocalPathFlags[f.Name] != "":
				exempted[f.Name] = true
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
	for name := range notALocalPathFlags {
		if !exempted[name] {
			t.Errorf("notALocalPathFlags exempts --%s, which no longer looks like a local path; remove the stale entry", name)
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
		{"pro", "computers", "list", "-vv"},
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

func TestMCPChild_RefusesCredentialReaders(t *testing.T) {
	for _, args := range [][]string{
		{"--profile", "prod", "--no-input", "config", "validate"},
		{"--profile", "prod", "--no-input", "doctor"},
		{"--profile", "prod", "--no-input", "doctor", "other"},
	} {
		if err := executeAsMCPChild(t, "prod", args...); !isMCPRefusal(err) {
			t.Errorf("MCP child ran %q (err %v); it prints or probes other profiles' credentials", args, err)
		}
	}
}

func TestMCPChild_RefusesMCPServe(t *testing.T) {
	done := make(chan error, 1)
	go func() {
		done <- executeAsMCPChild(t, "prod", "--profile", "prod", "--no-input", "mcp", "serve")
	}()
	select {
	case err := <-done:
		if !isMCPRefusal(err) {
			t.Errorf("MCP child ran `mcp serve` (err %v)", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("MCP child started `mcp serve` and is serving on stdin")
	}
}

func TestRunChild_TellsTheChildItsPinAndInputDir(t *testing.T) {
	t.Setenv(mcpChildEnvVar, "0")
	t.Setenv(mcpPinnedProfileEnvVar, "attacker")
	t.Setenv(mcpInputDirEnvVar, "/")
	inputDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	installMCPResolver(NewRootCmd("test", "t", "t", "t"), inputDir)
	t.Cleanup(func() { installMCPResolver(nil, "") })
	t.Cleanup(resetGlobals)

	path := filepath.Join(t.TempDir(), "echo-env.sh")
	script := "#!/bin/sh\nprintf 'MCP=[%s] PROFILE=[%s] INPUT=[%s]' \"$JAMF_CLI_MCP\" \"$JAMF_CLI_MCP_PROFILE\" \"$JAMF_CLI_MCP_INPUT_DIR\"\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	res := runChild(context.Background(), path, "prod", []string{"pro", "computers", "list"})
	if res == nil || res.IsError {
		t.Fatalf("expected success, got %+v", res)
	}
	got := mcpResultText(res)
	want := "MCP=[1] PROFILE=[prod] INPUT=[" + inputDir + "]"
	if got != want {
		t.Errorf("child saw %q, want %q", got, want)
	}
}

func TestConfigShowRows_RedactsCredentialsInAnMCPChild(t *testing.T) {
	const marker = "F5-MARKER-literal-client-secret"
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"prod":  {URL: "https://prod.example", ClientID: marker + "-id", ClientSecret: marker},
		"other": {URL: "https://other.example", Token: marker + "-token"},
		"empty": {URL: "https://empty.example"},
	}}

	t.Setenv(mcpChildEnvVar, "1")
	for _, r := range configShowRows(cfg, "prod") {
		for field, v := range map[string]string{"token": r.Token, "client-id": r.ClientID, "client-secret": r.ClientSecret} {
			if strings.Contains(v, marker) {
				t.Errorf("config show in an MCP child printed profile %q's %s: %q", r.Name, field, v)
			}
		}
		if r.Name == "prod" && r.ClientSecret != "<redacted>" {
			t.Errorf("profile prod's client-secret = %q; want it marked <redacted>", r.ClientSecret)
		}
		if r.Name == "empty" && r.Token+r.ClientID+r.ClientSecret != "" {
			t.Errorf("an unset credential must stay unset, got %+v", r)
		}
	}

	t.Setenv(mcpChildEnvVar, "")
	for _, r := range configShowRows(cfg, "prod") {
		if r.Name == "prod" && r.ClientSecret != marker {
			t.Errorf("outside MCP config show must print the configured value, got %q", r.ClientSecret)
		}
	}
}

func TestConfigListStatus_ProbesOnlyThePinnedProfileInAnMCPChild(t *testing.T) {
	newServer := func() (*httptest.Server, *atomic.Int32) {
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			_, _ = w.Write([]byte("[]"))
		}))
		t.Cleanup(srv.Close)
		return srv, &hits
	}
	pinnedSrv, pinnedHits := newServer()
	otherSrv, otherHits := newServer()

	run := func(t *testing.T, mcpChild string) {
		t.Helper()
		resetGlobals()
		t.Cleanup(resetGlobals)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("XDG_CACHE_HOME", t.TempDir())
		t.Setenv("JAMF_CLI_ARGS", "")
		t.Setenv(mcpChildEnvVar, mcpChild)
		t.Setenv(mcpPinnedProfileEnvVar, "prod")
		if err := config.Save(&config.Config{Profiles: map[string]config.Profile{
			"prod":  {URL: pinnedSrv.URL},
			"other": {URL: otherSrv.URL},
		}}); err != nil {
			t.Fatal(err)
		}
		root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
		root.SetArgs([]string{"--profile", "prod", "--no-input", "-o", "json", "config", "list", "--status"})
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		if err := root.Execute(); err != nil {
			t.Fatalf("config list --status: %v", err)
		}
	}

	run(t, "1")
	if pinnedHits.Load() != 1 || otherHits.Load() != 0 {
		t.Errorf("in an MCP child: pinned server hit %d times, other %d; only the pinned profile may be probed", pinnedHits.Load(), otherHits.Load())
	}

	pinnedHits.Store(0)
	run(t, "")
	if pinnedHits.Load() != 1 || otherHits.Load() != 1 {
		t.Errorf("outside MCP every profile is probed once, got pinned %d, other %d", pinnedHits.Load(), otherHits.Load())
	}
}
