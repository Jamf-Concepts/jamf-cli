// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// The global output flags are applied to one formatter in PersistentPreRunE, so
// a command that builds its own receives none of them and the flag is parsed
// and then discarded. These tests drive the real root command, because the
// defect is invisible at the level of the formatter: `--out-file` still creates
// the file and the payload still reaches standard output.
//
// `commands` is the command under test for five of the six flags. It needs no
// credentials, and it emits well over the 50 rows the advisory hint needs.

// runRoot executes the root command with args and returns what reached standard
// output and standard error. Both are read in goroutines: `commands -o json` is
// about a megabyte, which deadlocks a pipe that is drained after the write.
func runRoot(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	outR, outW, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("creating stdout pipe: %v", pipeErr)
	}
	errR, errW, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("creating stderr pipe: %v", pipeErr)
	}

	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	t.Cleanup(func() { os.Stdout, os.Stderr = origOut, origErr })

	outDone := make(chan string, 1)
	errDone := make(chan string, 1)
	go func() { b, _ := io.ReadAll(outR); outDone <- string(b) }()
	go func() { b, _ := io.ReadAll(errR); errDone <- string(b) }()

	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs(append([]string{"--no-update-check", "--no-version-check"}, args...))
	err = root.Execute()

	_ = outW.Close()
	_ = errW.Close()
	os.Stdout, os.Stderr = origOut, origErr
	return <-outDone, <-errDone, err
}

func commandRows(t *testing.T, data string) []map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal([]byte(data), &rows); err != nil {
		t.Fatalf("output is not a JSON array of objects: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("no rows: the command produced nothing to assert on")
	}
	return rows
}

// The measured signature of the defect: exit 0, a 0-byte file, and the whole
// payload on standard output.
func TestOutFileTakesTheOutputAndLeavesStdoutEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "commands.json")

	stdout, _, err := runRoot(t, "commands", "-o", "json", "--out-file", path)
	if err != nil {
		t.Fatalf("commands failed: %v", err)
	}

	written, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("reading --out-file target: %v", readErr)
	}
	if len(written) == 0 {
		t.Fatal("--out-file wrote 0 bytes")
	}
	commandRows(t, string(written))

	if stdout != "" {
		t.Errorf("standard output carried %d bytes; --out-file means the file gets them instead", len(stdout))
	}
}

func TestSelectKeepsOnlyTheNamedField(t *testing.T) {
	stdout, _, err := runRoot(t, "commands", "-o", "json", "--select", "command")
	if err != nil {
		t.Fatalf("commands failed: %v", err)
	}

	for _, row := range commandRows(t, stdout) {
		if len(row) != 1 {
			t.Fatalf("row has %d fields, want only the selected one: %v", len(row), row)
		}
		if _, ok := row["command"]; !ok {
			t.Fatalf("row is missing the selected field: %v", row)
		}
	}
}

// --compact drops arrays, so the privileges field is the observable one. A
// command with every field populated would show no difference, which is why the
// unprojected run is asserted first.
func TestCompactDropsTheArrayFields(t *testing.T) {
	plain, _, err := runRoot(t, "commands", "-o", "json")
	if err != nil {
		t.Fatalf("commands failed: %v", err)
	}
	var withPrivileges int
	for _, row := range commandRows(t, plain) {
		if _, ok := row["privileges"]; ok {
			withPrivileges++
		}
	}
	if withPrivileges == 0 {
		t.Skip("no command in the catalog declares privileges, so --compact has nothing to drop here")
	}

	compacted, _, err := runRoot(t, "commands", "-o", "json", "--compact")
	if err != nil {
		t.Fatalf("commands --compact failed: %v", err)
	}
	for _, row := range commandRows(t, compacted) {
		if _, ok := row["privileges"]; ok {
			t.Fatalf("--compact kept the privileges array: %v", row)
		}
	}
}

func TestFieldPrintsTheValuesAlone(t *testing.T) {
	stdout, _, err := runRoot(t, "commands", "-o", "json", "--field", "command")
	if err != nil {
		t.Fatalf("commands failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) < 50 {
		t.Fatalf("got %d lines, want one per command", len(lines))
	}
	if strings.Contains(stdout, "{") {
		t.Error("--field printed JSON rather than the field values")
	}
	if !strings.Contains(stdout, "config list") {
		t.Errorf("--field output does not name a known command: first line %q", lines[0])
	}
}

// --quiet and --no-hints both suppress the advisory hint the formatter writes to
// standard error above 50 rows. It is the only one of the six flags whose effect
// is on standard error, and the only reachable effect either flag has on this
// command.
func TestQuietAndNoHintsSuppressTheListHint(t *testing.T) {
	_, stderr, err := runRoot(t, "commands", "-o", "json")
	if err != nil {
		t.Fatalf("commands failed: %v", err)
	}
	if !strings.Contains(stderr, "hint:") {
		t.Fatalf("no hint on standard error, so neither flag has an observable effect here: %q", stderr)
	}

	for _, flag := range []string{"--quiet", "--no-hints"} {
		_, stderr, err := runRoot(t, "commands", "-o", "json", flag)
		if err != nil {
			t.Fatalf("commands %s failed: %v", flag, err)
		}
		if strings.Contains(stderr, "hint:") {
			t.Errorf("%s left the hint on standard error: %q", flag, stderr)
		}
	}
}

// A report that prints several sections has to send its section headers
// wherever its tables go. Otherwise --out-file splits one report across a file
// and a terminal, which is worse than the defect it replaces.
func TestSectionHeadersFollowTheFormatterWriter(t *testing.T) {
	oldFmt := outputFmt
	outputFmt = "table"
	t.Cleanup(func() { outputFmt = oldFmt })

	var buf bytes.Buffer
	formatter := output.New("table", true, false)
	formatter.SetWriter(&buf)
	cliCtx := &registry.CLIContext{Output: &cliOutput{formatter}}

	report := &securityReport{
		Summary: map[string]any{"total_devices": 1, "filevault_encrypted": 0},
		Devices: []map[string]any{
			{"name": "Mac-Bad", "serial": "S1", "filevault": "UNENCRYPTED", "gatekeeper": "DISABLED", "sip": "ENABLED", "firewall": false},
		},
		OSVersions: []map[string]any{{"os_version": "15.0", "count": 1, "pct": "100.0%"}},
	}

	stdout := captureStdout(t, func() {
		if err := printSecurityReport(cliCtx, report); err != nil {
			t.Fatalf("printSecurityReport error: %v", err)
		}
	})

	for _, want := range []string{"── Security Summary ──", "── Flagged Devices (1) ──", "── OS Version Distribution ──", "Mac-Bad"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("the formatter's writer did not receive %q", want)
		}
	}
	if stdout != "" {
		t.Errorf("standard output carried %q; the whole report belongs to the formatter's writer", stdout)
	}
}
