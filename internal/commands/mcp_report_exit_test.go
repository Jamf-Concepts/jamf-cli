// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// pflagFlag keeps the sweep below readable.
type pflagFlag = pflag.Flag

// writeFakeReportChild writes a shell script standing in for
// `jamf-cli dashboard`: it prints stdoutBody to stdout, stderrBody to stderr,
// and exits with code.
//
// A script rather than the real binary, because the behaviour under test is the
// *parent's* reading of an exit code, and driving a real collection would need
// a tenant. It also keeps the two streams distinguishable, which is the whole
// reason runReportChild cannot reuse runChild.
func writeFakeReportChild(t *testing.T, stdoutBody, stderrBody string, code int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-dashboard.sh")
	script := "#!/bin/sh\n"
	if stdoutBody != "" {
		script += fmt.Sprintf("printf '%%s' %s\n", shellQuote(stdoutBody))
	}
	if stderrBody != "" {
		script += fmt.Sprintf("printf '%%s' %s >&2\n", shellQuote(stderrBody))
	}
	script += fmt.Sprintf("exit %d\n", code)
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// reportDirConfig points config.Load() at a temp XDG home carrying a
// report-dir, and returns that directory.
func reportDirConfig(t *testing.T) string {
	t.Helper()
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	reportDir := t.TempDir()
	writeTestConfig(t, xdg, "report-dir: "+reportDir+"\n")
	return reportDir
}

// TestRunReportChild_KeepsTheReportWhenTheChildExits7 is the regression the
// round-2 review's finding (1) named.
//
// runDashboard returns exitcode.PartialFailure *after* writing the whole HTML,
// and the document carries its own incomplete-sections banner for exactly the
// reader who receives only the file. Branching on `runErr != nil` alone deleted
// that finished report and told the model "report generation failed: exit
// status 7" — making the banner undeliverable by the only path that produces
// one.
func TestRunReportChild_KeepsTheReportWhenTheChildExits7(t *testing.T) {
	reportDir := reportDirConfig(t)
	child := writeFakeReportChild(t,
		"<!DOCTYPE html><html><body>complete but partial</body></html>",
		"WARNING: security cloud ztna apps: 403\n", 7)

	res := runReportChild(context.Background(), child, "prod",
		generateReportInput{}, time.Date(2026, 8, 28, 10, 43, 0, 0, time.UTC))

	if res == nil {
		t.Fatal("expected a result")
	}
	if res.IsError {
		t.Fatalf("exit 7 means the document is complete; the result must not be an error: %s", mcpResultText(res))
	}

	path := filepath.Join(reportDir, "jamf-report-prod-20260828T104300Z.html")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the report must be left in place on exit 7: %v", err)
	}

	text := mcpResultText(res)
	if !strings.Contains(text, path) {
		t.Errorf("result must carry the path, got: %s", text)
	}
	if !strings.Contains(text, "incomplete") {
		t.Errorf("result must say the report is marked incomplete, got: %s", text)
	}
	if !strings.Contains(text, "ztna apps") {
		t.Errorf("result must carry the child's warnings, got: %s", text)
	}
	if strings.Contains(text, "<html") {
		t.Errorf("result must never carry the HTML, got: %s", text)
	}
}

// TestRunReportChild_DeletesTheReportOnAnyOtherNonZeroExit is the other half:
// only exit 7 means "complete but partial". Any other failure may have
// truncated the document mid-write, and a truncated HTML file is a corrupt file
// rather than a shorter report.
func TestRunReportChild_DeletesTheReportOnAnyOtherNonZeroExit(t *testing.T) {
	for _, code := range []int{1, 2, 3, 8} {
		t.Run(fmt.Sprintf("exit %d", code), func(t *testing.T) {
			reportDir := reportDirConfig(t)
			child := writeFakeReportChild(t, "<!DOCTYPE html><html><body>truncat", "boom\n", code)

			res := runReportChild(context.Background(), child, "prod",
				generateReportInput{}, time.Date(2026, 8, 28, 10, 43, 0, 0, time.UTC))

			if res == nil || !res.IsError {
				t.Fatalf("exit %d must be an error result, got %+v", code, res)
			}
			entries, err := os.ReadDir(reportDir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Errorf("a possibly-truncated report was left behind: %v", entries)
			}
		})
	}
}

// TestRunReportChild_TreatsAnEmptyReportAsAFailure: a zero-byte report reported
// as a success is what an inherited JAMF_CLI_ARGS --out-file produced — the
// child wrote its HTML elsewhere, the file the server opened stayed empty, and
// the model was told "Report written to … (0 bytes)".
func TestRunReportChild_TreatsAnEmptyReportAsAFailure(t *testing.T) {
	reportDir := reportDirConfig(t)
	child := writeFakeReportChild(t, "", "", 0)

	res := runReportChild(context.Background(), child, "prod",
		generateReportInput{}, time.Date(2026, 8, 28, 10, 43, 0, 0, time.UTC))

	if res == nil || !res.IsError {
		t.Fatalf("a zero-byte report must be an error result, got %+v", res)
	}
	if !strings.Contains(mcpResultText(res), "no output") {
		t.Errorf("the refusal must say the report was empty, got: %s", mcpResultText(res))
	}
	entries, err := os.ReadDir(reportDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("the empty report must be removed: %v", entries)
	}
}

// TestRunReportChild_ScrubsJAMFCLIARGSFromTheChild covers finding (17).
//
// cmd/jamf-cli/main.go reads JAMF_CLI_ARGS and prepends its shell-split
// contents ahead of everything buildChildArgs injects, so an inherited
// `--out-file` set the very flag blockedChildFlagPrefixes refuses. The fake
// child reports whether it saw the variable.
func TestRunReportChild_ScrubsJAMFCLIARGSFromTheChild(t *testing.T) {
	t.Setenv("JAMF_CLI_ARGS", "--out-file /tmp/hijacked.json")
	reportDir := reportDirConfig(t)

	// The child prints the variable it inherited into the report body, so an
	// inherited value is observable in the file the parent kept.
	path := filepath.Join(t.TempDir(), "echo-env.sh")
	script := "#!/bin/sh\nprintf 'JAMF_CLI_ARGS=[%s] JAMF_CLI_MCP=[%s]' \"$JAMF_CLI_ARGS\" \"$JAMF_CLI_MCP\"\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	res := runReportChild(context.Background(), path, "prod",
		generateReportInput{}, time.Date(2026, 8, 28, 10, 43, 0, 0, time.UTC))
	if res == nil || res.IsError {
		t.Fatalf("expected success, got %+v", res)
	}

	body, err := os.ReadFile(filepath.Join(reportDir, "jamf-report-prod-20260828T104300Z.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "JAMF_CLI_ARGS=[]") {
		t.Errorf("the child inherited JAMF_CLI_ARGS: %s", body)
	}
	if !strings.Contains(string(body), "JAMF_CLI_MCP=[1]") {
		t.Errorf("the child must still get JAMF_CLI_MCP=1: %s", body)
	}
}

// TestChildEnv_DropsOnlyJAMFCLIARGS keeps the scrub narrow: every other
// variable the operator set — a proxy, a CA bundle, JAMF_CLIENT_SECRET — has to
// reach the child, which is how the server's credentials get there at all.
func TestChildEnv_DropsOnlyJAMFCLIARGS(t *testing.T) {
	t.Setenv("JAMF_CLI_ARGS", "--out-file /tmp/x")
	t.Setenv("JAMF_CLIENT_SECRET", "s3cret")
	t.Setenv("HTTPS_PROXY", "http://proxy:3128")

	env := childEnv()
	joined := strings.Join(env, "\n")

	if strings.Contains(joined, "JAMF_CLI_ARGS=") {
		t.Error("JAMF_CLI_ARGS must be dropped")
	}
	for _, want := range []string{"JAMF_CLIENT_SECRET=s3cret", "HTTPS_PROXY=http://proxy:3128", "JAMF_CLI_MCP=1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("childEnv() must keep %q", want)
		}
	}
}

// TestBlockedChildFlags_CoverEveryCredentialSelectingFlagInTheTree is finding
// (2)'s "Fixed when": the blocked set is derived from the assembled command
// tree rather than checked against a hand-written literal.
//
// Every flag in the tree whose name names a profile, URL, token, tenant,
// environment or output file selects credentials or a destination, and must be
// refused. --include-profile is how this was found: it was added by the same
// change that added --environment-id to the list, and only the second one was
// added.
func TestBlockedChildFlags_CoverEveryCredentialSelectingFlagInTheTree(t *testing.T) {
	// Substrings that make a flag name credential- or destination-selecting.
	sensitive := []string{"profile", "url", "token", "tenant-id", "environment-id", "out-file", "client-id", "client-secret"}

	// Flags the substring sweep matches that do not select *this CLI's*
	// credentials or destination. Each needs a reason, and a stale entry fails
	// the test below — which is what keeps the exemption list from becoming the
	// place a real hole hides.
	exempt := map[string]string{
		"unlock-token":          "a device's own MDM unlock token, sent in the request body; it names no instance",
		"esim-server-url":       "the carrier's eSIM server, sent to the device; it names no Jamf instance",
		"no-keychain-client-id": "a payload-exclusion boolean on a config-profile download, not a credential",
		"no-token":              "a payload-exclusion boolean on a config-profile download, not a credential",
	}
	exercised := map[string]bool{}

	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	var checked int
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		cmd.Flags().VisitAll(func(f *pflagFlag) {
			name := strings.ToLower(f.Name)
			for _, s := range sensitive {
				if !strings.Contains(name, s) {
					continue
				}
				if _, ok := exempt[name]; ok {
					exercised[name] = true
					return
				}
				checked++
				if !isBlockedChildFlag("--" + f.Name) {
					t.Errorf("flag --%s (on %q) selects credentials or a destination and is not blocked for MCP children.\n"+
						"Add it to blockedChildFlagPrefixes, or exempt it here with the reason it is not credential-selecting.",
						f.Name, cmd.CommandPath())
				}
				if !isBlockedChildFlag("--" + f.Name + "=x") {
					t.Errorf("flag --%s=x (on %q) is not blocked in its =value form", f.Name, cmd.CommandPath())
				}
				return
			}
		})
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}
	walk(root)

	if checked == 0 {
		t.Fatal("the sweep matched no flags at all, so it proves nothing — has the flag naming changed?")
	}
	// A stale exemption is how a real hole would hide behind a list of reasons.
	for name, reason := range exempt {
		if !exercised[name] {
			t.Errorf("exemption --%s (%q) matches no flag in the tree any more; remove it", name, reason)
		}
	}
	t.Logf("checked %d credential- or destination-selecting flags, %d exempted", checked, len(exempt))
}

// TestBlockedChildFlags_LeaveOrdinaryFlagsAlone keeps the prefix rule from
// swallowing the tree: --from-file is declared by 362 commands and is the
// documented way to hand a body to a write, so a rule that caught it would
// disable most of the surface.
func TestBlockedChildFlags_LeaveOrdinaryFlagsAlone(t *testing.T) {
	for _, arg := range []string{
		"--from-file", "--from-file=/tmp/body.json", "--name", "--filter",
		"--field", "--select", "--yes", "--all", "--limit", "--section",
		"--scaffold", "--set", "--serial", "--udid", "--output", "-o", "json",
	} {
		if isBlockedChildFlag(arg) {
			t.Errorf("%q is an ordinary flag and must not be blocked", arg)
		}
	}
}

// TestBuildChildArgs_RefusesCommandsThatChooseTheirOwnTarget covers the half of
// finding (2) no flag list can hold: `multi` takes its own --profiles and fans
// out across instances, and the config write subcommands persist a new default
// profile or destination to disk, after which every later child is pointed
// somewhere the operator never chose.
func TestBuildChildArgs_RefusesCommandsThatChooseTheirOwnTarget(t *testing.T) {
	refused := [][]string{
		{"multi", "pro", "computers", "list"},
		{"config", "set-default", "other"},
		{"config", "add-profile", "other"},
		{"config", "remove-profile", "prod"},
		{"config", "set-report-dir", "/tmp"},
		{"pro", "backup", "--output", "/tmp/out"},
		{"protect", "backup", "--output", "/tmp/out"},
	}
	for _, args := range refused {
		if _, err := buildChildArgs("prod", args); err == nil {
			t.Errorf("expected %v to be refused", args)
		}
	}

	// A read-only config command is fine — it names no target.
	for _, args := range [][]string{{"config", "list"}, {"config", "show"}} {
		if _, err := buildChildArgs("prod", args); err != nil {
			t.Errorf("%v is read-only and must be allowed, got %v", args, err)
		}
	}
}

// TestRefuseReportThroughRunCommand covers finding (16): run_command returns
// stdout as tool text, and the dashboard writes a 320-800 KB HTML document
// there — the exact cost generate_report exists to avoid, with no file left to
// share. list_commands advertises `dashboard` like any other command, so the
// model reaches for it unless told.
func TestRefuseReportThroughRunCommand(t *testing.T) {
	for _, args := range [][]string{{"dashboard"}, {"dashboard", "--full"}, {"db"}, {"db", "--title", "x"}} {
		err := refuseReportThroughRunCommand(args)
		if err == nil {
			t.Fatalf("expected %v to be refused on the run_command path", args)
		}
		if !strings.Contains(err.Error(), "generate_report") {
			t.Errorf("the refusal must name generate_report, got %q", err)
		}
	}
	// The report path must still be able to spawn it.
	if err := refuseReportThroughRunCommand([]string{"pro", "computers", "list"}); err != nil {
		t.Errorf("an ordinary command must not be refused, got %v", err)
	}
	reportArgs, err := buildReportArgs(generateReportInput{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := buildChildArgs("prod", reportArgs); err != nil {
		t.Fatalf("generate_report must still be able to spawn the dashboard, got %v", err)
	}
}

// TestCapChildOutput bounds what one tool result may carry. The tree has
// commands whose output is unbounded, and run_command returns whatever the
// child printed.
func TestCapChildOutput(t *testing.T) {
	small := strings.Repeat("a", 100)
	if got := capChildOutput([]byte(small)); got != small {
		t.Error("output under the cap must pass through unchanged")
	}

	big := strings.Repeat("b", maxChildOutputBytes+5000)
	got := capChildOutput([]byte(big))
	if len(got) >= len(big) {
		t.Errorf("output of %d bytes was not truncated (got %d)", len(big), len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Error("truncation must be visible, not silent")
	}
}
