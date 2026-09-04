// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	gotoken "go/token" // the package name collides with root.go's token flag var
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

// The four overview commands and the five multi-section reports render their
// own text and reach the destination through writerFor, so this covers the
// helper every one of them depends on. It does not cover their call sites;
// TestOverviewRenderersTakeTheFormattersWriter does.
func TestWriterForAnswersWithTheFormattersWriter(t *testing.T) {
	var buf bytes.Buffer
	formatter := output.New("table", true, false)
	formatter.SetWriter(&buf)

	if got := writerFor(&registry.CLIContext{Output: &cliOutput{formatter}}); got != &buf {
		t.Errorf("writerFor returned %T, want the writer the formatter was given", got)
	}
	if got := writerFor(&registry.CLIContext{}); got != os.Stdout {
		t.Errorf("writerFor with no formatter returned %T, want os.Stdout", got)
	}
}

// Two formats have no rendering of their own for a report the CLI assembles,
// so Print's switch has no case for either and its default arm renders the
// table. Routing the rows through PrintRaw instead would reach PrintRaw's own
// FormatRaw and FormatXML arms and emit the report's marshalled JSON, which is
// a different answer under a flag documented as "exact wire bytes" on a report
// that has none. Pinned against the table so the equivalence is the assertion,
// rather than a byte count that says nothing about why.
func TestRawAndXMLRenderTheSameAsTable(t *testing.T) {
	table, _, err := runRoot(t, "commands", "-o", "table")
	if err != nil {
		t.Fatalf("commands -o table failed: %v", err)
	}
	if table == "" {
		t.Fatal("commands -o table produced nothing to compare against")
	}

	for _, format := range []string{"raw", "xml"} {
		stdout, _, err := runRoot(t, "commands", "-o", format)
		if err != nil {
			t.Fatalf("commands -o %s failed: %v", format, err)
		}
		if stdout != table {
			t.Errorf("-o %s rendered %d bytes against the table's %d; it reached a different renderer", format, len(stdout), len(table))
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

// CLAUDE.md records this as a convention with a past regression behind it: an
// empty list prints `[]`, never `null`, because `null` breaks a jq pipeline on
// exactly the tenants where the collection is empty. overviewToRows declares
// `var rows []map[string]any` and only appends inside a loop, so a report whose
// every section came back empty reaches printRows with a nil slice.
func TestPrintRowsRendersANilSliceAsAnEmptyList(t *testing.T) {
	oldFmt := outputFmt
	outputFmt = "json"
	t.Cleanup(func() { outputFmt = oldFmt })

	var buf bytes.Buffer
	formatter := output.New("json", true, false)
	formatter.SetWriter(&buf)

	if err := printRows(&registry.CLIContext{Output: &cliOutput{formatter}}, nil); err != nil {
		t.Fatalf("printRows error: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "[]" {
		t.Errorf("printRows(nil) rendered %q, want []", got)
	}
}

// formatterFor exists to keep the writer and the projector that a fresh
// formatter drops, so a change back to output.New would restore the defect for
// `multi`, `group-tools export` and every command that prints rows.
func TestWithFormatKeepsTheWriterAndTheProjector(t *testing.T) {
	var buf bytes.Buffer
	formatter := output.New("table", true, false)
	formatter.SetWriter(&buf)
	formatter.SetProjector(output.Projector{Select: []string{"name"}})

	clone := formatter.WithFormat("json")
	if clone.Format() != "json" {
		t.Errorf("clone format = %q, want json", clone.Format())
	}
	if formatter.Format() != "table" {
		t.Errorf("the original's format changed to %q; WithFormat must copy", formatter.Format())
	}
	if err := clone.Print([]map[string]any{{"name": "keep", "drop": "me"}}); err != nil {
		t.Fatalf("clone Print error: %v", err)
	}
	if !strings.Contains(buf.String(), "keep") {
		t.Errorf("the clone did not write to the original's writer: %q", buf.String())
	}
	if strings.Contains(buf.String(), "drop") {
		t.Errorf("the clone dropped the projector: %q", buf.String())
	}
}

// overviewRenderers are the whole-output text renderers that take a writer
// rather than printing through the formatter, so the writer their caller
// chooses is the only thing that sends their output to --out-file.
var overviewRenderers = map[string]bool{
	"printOverviewTable":        true,
	"printProtectOverviewTable": true,
	"printSchoolOverviewTable":  true,
}

// A revert of any one of these four arguments to cmd.OutOrStdout() restores the
// defect for that command and breaks no other test: writerFor keeps answering
// correctly, and nothing else reads the argument. So the argument itself is
// what has to be pinned.
func TestOverviewRenderersTakeTheFormattersWriter(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading package directory: %v", err)
	}

	checked := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		fset := gotoken.NewFileSet()
		file, parseErr := parser.ParseFile(fset, name, nil, 0)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", name, parseErr)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			callee, ok := call.Fun.(*ast.Ident)
			if !ok || !overviewRenderers[callee.Name] || len(call.Args) == 0 {
				return true
			}
			checked++
			pos := fset.Position(call.Lparen)
			arg, isCall := call.Args[0].(*ast.CallExpr)
			if !isCall {
				t.Errorf("%s:%d %s takes %T as its writer, want a writerFor call", name, pos.Line, callee.Name, call.Args[0])
				return true
			}
			if writer, isIdent := arg.Fun.(*ast.Ident); !isIdent || writer.Name != "writerFor" {
				t.Errorf("%s:%d %s takes a call to something other than writerFor as its writer", name, pos.Line, callee.Name)
			}
			return true
		})
	}

	if checked != 4 {
		t.Errorf("checked %d overview renderer calls, want the 4 this rule exists for; update overviewRenderers", checked)
	}
}

// The five multi-section reports read writerFor into a local and then write
// their section headers to it. A revert of one header to fmt.Printf splits that
// report between --out-file and the terminal, and no test in the package calls
// four of the five functions, so the header itself has nothing holding it. A
// file that has learned to route its writer must not backslide, which also
// covers the next file to adopt writerFor.
func TestFilesThatRouteTheirWriterDoNotPrintToStdout(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading package directory: %v", err)
	}

	routed := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		fset := gotoken.NewFileSet()
		file, parseErr := parser.ParseFile(fset, name, nil, 0)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", name, parseErr)
		}

		var routes bool
		var bare []string
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.Ident:
				if node.Name == "writerFor" {
					routes = true
				}
			case *ast.CallExpr:
				sel, ok := node.Fun.(*ast.SelectorExpr)
				if !ok || !isPackageIdent(sel.X, "fmt") {
					return true
				}
				switch sel.Sel.Name {
				case "Print", "Printf", "Println":
					bare = append(bare, fmt.Sprintf("%s:%d", name, fset.Position(node.Lparen).Line))
				}
			}
			return true
		})

		if !routes {
			continue
		}
		routed++
		for _, at := range bare {
			t.Errorf("%s writes to standard output directly, but this file routes its writer through writerFor", at)
		}
	}

	if routed < 10 {
		t.Errorf("only %d files route through writerFor, want at least the 10 this rule was written over", routed)
	}
}

func isPackageIdent(expr ast.Expr, name string) bool {
	id, ok := expr.(*ast.Ident)
	return ok && id.Name == name
}
