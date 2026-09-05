// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	gotoken "go/token" // the package name collides with root.go's token flag var
	"io"
	"io/fs"
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

// Every setter the formatter exposes is one global flag reaching the output,
// and buildOutputFormatter is the only place they are applied. Four of the five
// are held by a test that observes the flag; SetExplicitNoColor is not, its only
// reader being PaginationProgress's choice between the interactive counter and
// NDJSON events, which needs stderr to be a terminal — something no test in this
// package can arrange, isStderrTTY being unexported. Deleting that one line left
// the whole package passing.
//
// So the wiring is what is pinned, and the set is read off the formatter rather
// than listed: a sixth setter that buildOutputFormatter never calls is the same
// defect as a fifth that stopped being called.
func TestTheSharedFormatterAppliesEverySetterTheFormatterExposes(t *testing.T) {
	setters := formatterSetterNames(t)
	if len(setters) < 5 {
		t.Fatalf("found %d setters on output.Formatter, want at least the 5 that carry the global flags", len(setters))
	}

	fset := gotoken.NewFileSet()
	file, err := parser.ParseFile(fset, "root.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing root.go: %v", err)
	}

	applied := map[string]bool{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "buildOutputFormatter" {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			if sel, isSel := n.(*ast.SelectorExpr); isSel {
				applied[sel.Sel.Name] = true
			}
			return true
		})
	}
	if len(applied) == 0 {
		t.Fatal("root.go declares no buildOutputFormatter, so nothing assembles the shared formatter")
	}

	for _, setter := range setters {
		if !applied[setter] {
			t.Errorf("buildOutputFormatter never calls %s, so the flag it carries is parsed and discarded", setter)
		}
	}
}

// formatterSetterNames returns every setter output.Formatter exposes.
func formatterSetterNames(t *testing.T) []string {
	t.Helper()

	dir := filepath.Join("..", "output")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the output package directory: %v", err)
	}

	var setters []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := gotoken.NewFileSet()
		file, parseErr := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", name, parseErr)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
				continue
			}
			if !strings.HasPrefix(fn.Name.Name, "Set") || receiverTypeName(fn.Recv.List[0].Type) != "Formatter" {
				continue
			}
			setters = append(setters, fn.Name.Name)
		}
	}
	return setters
}

func receiverTypeName(expr ast.Expr) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if id, ok := expr.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// formatterFor's fallback is for a caller reached with a test double or before
// PersistentPreRunE ran. It goes through the shared builder, so the flags a
// fresh output.New discards apply there too — which also removes the second
// construction site the lint had to carve out.
func TestTheFormatterFallbackStillCarriesTheProjection(t *testing.T) {
	oldSelect := selectFields
	selectFields = []string{"name"}
	t.Cleanup(func() { selectFields = oldSelect })

	stdout := captureStdout(t, func() {
		if err := formatterFor(nil, "json").Print([]map[string]any{{"name": "keep", "drop": "me"}}); err != nil {
			t.Fatalf("Print error: %v", err)
		}
	})

	if !strings.Contains(stdout, "keep") {
		t.Errorf("the fallback formatter printed nothing usable: %q", shortened(stdout))
	}
	if strings.Contains(stdout, "drop") {
		t.Errorf("the fallback formatter ignored --select: %q", shortened(stdout))
	}
}

// The two callers that ask the shared formatter for a format their own
// argument names, rather than the global -o value. Both were referenced
// exactly once in the package outside their own definition, and the three
// TestGroupToolsExport_* cases assert against marshalGroupsJSON and
// marshalGroupsYAML, helpers runGroupToolsExport does not call. So WithFormat
// returning an alias instead of a copy was held by its own unit test alone,
// and a clone defect reachable only through a non-global format argument would
// have shipped green.
func TestExportPrintsTheNamedFormatToTheFormattersWriter(t *testing.T) {
	oldFmt := outputFmt
	outputFmt = "table"
	t.Cleanup(func() { outputFmt = oldFmt })

	var buf bytes.Buffer
	formatter := output.New("table", true, false)
	formatter.SetWriter(&buf)
	cliCtx := &registry.CLIContext{
		Client: groupToolsMockClient(),
		Output: &cliOutput{formatter},
	}

	stdout := captureStdout(t, func() {
		if err := runGroupToolsExport(context.Background(), cliCtx, "yaml"); err != nil {
			t.Fatalf("runGroupToolsExport error: %v", err)
		}
	})

	if !strings.Contains(buf.String(), "name: All Computers") {
		t.Errorf("--format yaml did not reach the formatter's writer as YAML: %q", shortened(buf.String()))
	}
	if strings.Contains(buf.String(), "───") {
		t.Errorf("the export rendered the global -o table rather than its own --format: %q", shortened(buf.String()))
	}
	if stdout != "" {
		t.Errorf("standard output carried %q; the export belongs to the formatter's writer", shortened(stdout))
	}
	// The shared formatter is reused for the rest of the invocation, so a
	// WithFormat that aliased instead of copying would leave every later
	// command rendering YAML.
	if formatter.Format() != "table" {
		t.Errorf("the shared formatter now renders %q; asking it for one format must not change it for everyone", formatter.Format())
	}
}

// multi aggregates its children's output and renders it in the format the
// inner command named, so it reaches the shared formatter by the same route as
// the export above. printAggregated was named only in a comment.
func TestAggregatedReportPrintsToTheFormattersWriter(t *testing.T) {
	oldFmt := outputFmt
	outputFmt = "table"
	t.Cleanup(func() { outputFmt = oldFmt })

	// A summary dict beside the list, because the two take different arms of
	// printAggregated's switch and each prints its own section header.
	merged := map[string]any{
		"summary": map[string]any{"profiles": "2", "total": "1"},
		mergedListKey: map[string]map[string]any{
			"1": {"id": "1", "name": "Mac-01"},
		},
	}

	// "table" and "" are the arm this PR changed, and the arm `multi` takes
	// with no -o at all. json and yaml both return early from printAggregated,
	// so a loop of those two never executed the section-header code: reverting
	// the header print to fmt.Printf passed the test this replaced.
	for _, format := range []string{"json", "yaml", "table", ""} {
		name := format
		if name == "" {
			name = "(no -o)"
		}
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			formatter := output.New("table", true, false)
			formatter.SetWriter(&buf)
			cliCtx := &registry.CLIContext{Output: &cliOutput{formatter}}

			stdout := captureStdout(t, func() {
				if err := printAggregated(cliCtx, newMultiCmd(cliCtx), merged, format); err != nil {
					t.Fatalf("printAggregated(%s) error: %v", name, err)
				}
			})

			if !strings.Contains(buf.String(), "Mac-01") {
				t.Errorf("-o %s did not reach the formatter's writer: %q", name, shortened(buf.String()))
			}
			if stdout != "" {
				t.Errorf("-o %s left %q on standard output", name, shortened(stdout))
			}
			if formatter.Format() != "table" {
				t.Errorf("the shared formatter now renders %q after -o %s", formatter.Format(), name)
			}
			// The section headers have to travel with the tables, or --out-file
			// takes the rows and the terminal takes the headings.
			if format == "table" || format == "" {
				if !strings.Contains(buf.String(), "──") {
					t.Errorf("-o %s: no section header reached the writer, so the headings and the rows go to different places: %q", name, shortened(buf.String()))
				}
			}
		})
	}
}

// shortened keeps a failure message readable when the value is a whole report.
func shortened(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// overviewRenderers are the whole-output text renderers that take a writer
// rather than printing through the formatter, so the writer their caller
// chooses is the only thing that sends their output to --out-file. The set is
// derived from the signature every one of them shares — a writer, then the
// sections to render — so a fifth overview command's renderer is covered
// without an edit here. A hardcoded list of names could not see one.
func overviewRenderers(files map[string]*ast.File) map[string]bool {
	found := map[string]bool{}
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || len(fn.Type.Params.List) < 2 {
				continue
			}
			if !isIOWriterType(fn.Type.Params.List[0].Type) || !isOverviewSectionsType(fn.Type.Params.List[1].Type) {
				continue
			}
			found[fn.Name.Name] = true
		}
	}
	return found
}

func isIOWriterType(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Writer" && isPackageIdent(sel.X, "io")
}

func isOverviewSectionsType(expr ast.Expr) bool {
	arr, ok := expr.(*ast.ArrayType)
	if !ok || arr.Len != nil {
		return false
	}
	id, ok := arr.Elt.(*ast.Ident)
	return ok && id.Name == "overviewSection"
}

// A revert of any one of those renderers' writer arguments to cmd.OutOrStdout()
// restores the defect for that command and breaks no other test: writerFor
// keeps answering correctly, and nothing else reads the argument. So the
// argument itself is what has to be pinned.
func TestOverviewRenderersTakeTheFormattersWriter(t *testing.T) {
	fset, files := packageFiles(t)
	renderers := overviewRenderers(files)
	if len(renderers) < 3 {
		t.Fatalf("derived %d overview renderers, want at least the 3 in the tree; the signature they share has moved", len(renderers))
	}

	called := map[string]int{}
	for name, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			callee, ok := call.Fun.(*ast.Ident)
			if !ok || !renderers[callee.Name] || len(call.Args) == 0 {
				return true
			}
			called[callee.Name]++
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

	for renderer := range renderers {
		if called[renderer] == 0 {
			t.Errorf("%s renders a whole overview and nothing calls it, so no call site is pinned", renderer)
		}
	}
}

// packageFiles parses every non-test file of this package, keyed by file name,
// against one FileSet so the positions of any two of them are comparable.
func packageFiles(t *testing.T) (*gotoken.FileSet, map[string]*ast.File) {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading package directory: %v", err)
	}

	fset := gotoken.NewFileSet()
	files := map[string]*ast.File{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, name, nil, 0)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", name, parseErr)
		}
		files[name] = file
	}
	return fset, files
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

// sanctionedFormatterSites is the whole set of places an output.Formatter may
// be built, keyed by file and enclosing function. A construction anywhere else
// is a finding. Each reason is recorded at the site itself.
var sanctionedFormatterSites = map[string]string{
	// The shared formatter itself. The setters carrying --out-file, --select,
	// --compact, --quiet and --no-hints are applied here.
	"commands/root.go": "buildOutputFormatter",
	// newBulkCmd's Long documents the contract this preview keeps: preview
	// table on stdout, mutation log on stderr. It stays a table whatever -o
	// says, so it cannot come from the shared formatter, and it must not follow
	// --out-file into the data file.
	"commands/pro_bulk.go": "bulkPreviewTable",
	// The bulk-targeting preview, keeping the same documented contract.
	"commands/pro_device_actions.go": "deviceActionPreviewTable",
}

// buildsFormatter reports whether n produces or captures an output.Formatter,
// where local is the name internal/output is bound to in the file being read.
//
// New is that package's only exported constructor and Formatter's fields are
// unexported, so the surface is: a reference to New, a composite literal of the
// type, and new() of it.
//
// It matches the SELECTOR local.New rather than a call to it, which covers both
// `output.New(…)` and `mk := output.New` in one rule. Matching the call alone
// missed the second — the constructor taken as a value puts the call one hop
// away — and the linter this test replaced carried a fixture for exactly that
// shape, so an earlier version of this rule was a regression against it.
func buildsFormatter(n ast.Node, local string) bool {
	isSel := func(e ast.Expr, sel string) bool {
		s, ok := e.(*ast.SelectorExpr)
		if !ok || s.Sel.Name != sel {
			return false
		}
		id, isIdent := s.X.(*ast.Ident)
		return isIdent && id.Name == local
	}
	switch v := n.(type) {
	case *ast.SelectorExpr:
		return isSel(v, "New")
	case *ast.CompositeLit:
		return isSel(v.Type, "Formatter")
	case *ast.CallExpr:
		if id, ok := v.Fun.(*ast.Ident); ok && id.Name == "new" && len(v.Args) == 1 {
			return isSel(v.Args[0], "Formatter")
		}
	}
	return false
}

// TestNoFileBuildsItsOwnOutputFormatter refuses an output.Formatter built
// outside the sanctioned sites. Twenty-four commands called output.New
// directly, so --out-file, --select, --field, --compact, --quiet and --no-hints
// were parsed and then discarded on every one of them: --out-file created a
// 0-byte file while 2934 bytes went to stdout.
//
// It replaced a 976-line program under scripts/ with its own Makefile target
// and gating CI step. That program enumerated construction syntax — dot
// imports, elided composite-literal element types, generic receivers — and was
// still blind to a shape this same change introduces, Formatter.WithFormat's
// dereference-copy. This resolves the import instead, so a file that cannot
// name the package cannot trip any form of the rule, known or not.
//
// Attribution is to the enclosing top-level function, so a construction inside
// a closure inside a sanctioned function is exempt with it. That is why
// deviceActionPreviewTable was extracted from executeAction: the exemption
// covers three lines rather than a whole function body where a later
// result-printing path would have been exempt in advance.
func TestNoFileBuildsItsOwnOutputFormatter(t *testing.T) {
	const outputPkg = `"github.com/Jamf-Concepts/jamf-cli/internal/output"`

	found := map[string]string{}
	walkErr := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		fset := gotoken.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Errorf("parsing %s: %v", path, parseErr)
			return nil
		}
		// A file that does not import the package cannot construct one,
		// whatever syntax it reaches for. internal/output's own files are
		// skipped here, being the package rather than an importer of it.
		local := ""
		for _, imp := range file.Imports {
			if imp.Path.Value != outputPkg {
				continue
			}
			local = "output"
			if imp.Name != nil {
				local = imp.Name.Name
			}
		}
		if local == "" || local == "_" {
			return nil
		}
		// A dot import binds New and Formatter as bare identifiers, which no
		// selector-based rule can see. Refuse the import rather than grow a
		// second matcher for it: nothing here needs one.
		if local == "." {
			t.Errorf("%s dot-imports internal/output, which puts New and Formatter beyond this rule — import it normally", path)
			return nil
		}

		rel := filepath.ToSlash(strings.TrimPrefix(path, "../"))
		for _, decl := range file.Decls {
			// Every declaration, not only functions. A package-level
			// `var f = output.New(…)` is an *ast.GenDecl, so skipping
			// non-functions let the whole shape through: the flags go inert on
			// that command, --out-file writes 0 bytes at exit 0, and the guard
			// reports green.
			site := "package scope"
			if fn, isFunc := decl.(*ast.FuncDecl); isFunc {
				site = fn.Name.Name
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				if !buildsFormatter(n, local) {
					return true
				}
				// "package scope" is not a Go identifier, so it can never
				// match a sanctioned entry: a construction outside a function
				// is always a finding.
				if sanctionedFormatterSites[rel] == site {
					found[rel] = site
					return true
				}
				t.Errorf("%s: %s builds or captures its own output.Formatter, so every global output flag is inert on it — print through printRows or formatterFor instead", rel, site)
				return true
			})
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking internal/: %v", walkErr)
	}

	// A sanctioned site that moved or was renamed must fail here rather than
	// silently leaving the rule enforcing nothing.
	for file, fn := range sanctionedFormatterSites {
		if found[file] != fn {
			t.Errorf("sanctioned site %s:%s builds no formatter any more — remove the entry, or point it at the function that does", file, fn)
		}
	}
}

// TestFieldFollowsTheFormatterWriter is the --field half of
// TestSectionHeadersFollowTheFormatterWriter. printRows sends the section
// headers of a multi-section report to the formatter's writer, so --field
// sending its values anywhere else splits one report between two destinations.
//
// Wire-measured before the fix: `pro report ddm-status -o table --field source
// --out-file f` left f holding 28 bytes, the header alone, and put 3917 bytes
// of values on stdout at exit 0. With no -o, f was 0 bytes — verbatim the
// signature issue #349 reports and this PR closes.
func TestFieldFollowsTheFormatterWriter(t *testing.T) {
	fieldName = "name"
	t.Cleanup(func() { fieldName = "" })

	var buf bytes.Buffer
	formatter := output.New("table", true, false)
	formatter.SetWriter(&buf)
	cliCtx := &registry.CLIContext{Output: &cliOutput{formatter}}

	rows := []map[string]any{{"id": "1", "name": "Mac-01"}, {"id": "2", "name": "Mac-02"}}

	stdout := captureStdout(t, func() {
		if err := printRows(cliCtx, rows); err != nil {
			t.Fatalf("printRows: %v", err)
		}
	})

	if got := buf.String(); got != "Mac-01\nMac-02\n" {
		t.Errorf("writer got %q, want both values", got)
	}
	if stdout != "" {
		t.Errorf("--field left %q on standard output, so --out-file receives only the headers", stdout)
	}
}

// TestSelectMatchingNothingPrintsNothing covers a section whose rows carry none
// of the --select paths. --select was inert on the multi-section reports until
// this change, so the symptom arrives with it: projectSelect omits a missing
// path silently and keeps len(rows), so printTable read sortedKeys(rows[0]),
// got nothing, and still printed "RESULTS (18 total)" above a blank header.
//
// A report's sections do not share a schema, so this is the normal case rather
// than an edge one: `--select reason` is carried by the errors section of
// `pro report ddm-status` and by none of the others.
func TestSelectMatchingNothingPrintsNothing(t *testing.T) {
	oldSelect, oldFmt := selectFields, outputFmt
	selectFields, outputFmt = []string{"reason"}, "table"
	t.Cleanup(func() { selectFields, outputFmt = oldSelect, oldFmt })

	var buf bytes.Buffer
	formatter := output.New("table", true, false)
	formatter.SetWriter(&buf)
	formatter.SetProjector(output.Projector{Select: selectFields})
	cliCtx := &registry.CLIContext{Output: &cliOutput{formatter}}

	// No row carries "reason".
	if err := printRows(cliCtx, []map[string]any{{"id": "1", "status": "ok"}}); err != nil {
		t.Fatalf("printRows: %v", err)
	}
	if got := buf.String(); got != "" {
		t.Errorf("a section carrying none of the --select paths rendered %q, which reads as a broken renderer rather than an absent field", got)
	}

	// A section that does carry it must still render, or the skip is just a
	// mute button.
	buf.Reset()
	if err := printRows(cliCtx, []map[string]any{{"id": "1", "reason": "expired"}}); err != nil {
		t.Fatalf("printRows: %v", err)
	}
	if !strings.Contains(buf.String(), "expired") {
		t.Errorf("a section carrying the --select path rendered %q, want the value", buf.String())
	}
}

// TestSelectMatchingNothingLeavesNoOrphanBanner covers the caller-side half of
// the --select skip. Each hand-written multi-section report wrote its own
// banner and then called printRows, so suppressing the body left the banner on
// the writer: `pro report security -o table --select nosuchfield` produced 105
// bytes of nothing but three box-drawing lines, one reading
// `── Flagged Devices (5) ──`, at exit 0 — and a -o csv consumer received a
// stream of box-drawing characters. printSection decides before the header.
func TestSelectMatchingNothingLeavesNoOrphanBanner(t *testing.T) {
	oldSelect, oldFmt, oldQuiet := selectFields, outputFmt, quiet
	selectFields, outputFmt, quiet = []string{"nosuchfield"}, "table", true
	t.Cleanup(func() { selectFields, outputFmt, quiet = oldSelect, oldFmt, oldQuiet })

	var buf bytes.Buffer
	formatter := output.New("table", true, false)
	formatter.SetWriter(&buf)
	formatter.SetProjector(output.Projector{Select: selectFields})
	cliCtx := &registry.CLIContext{Output: &cliOutput{formatter}}

	if err := printSection(cliCtx, "── Flagged Devices (5) ──\n", []map[string]any{{"id": "1"}}); err != nil {
		t.Fatalf("printSection: %v", err)
	}
	if got := buf.String(); got != "" {
		t.Errorf("a skipped section still wrote %q — the banner outlives the body it announced", got)
	}

	// A section that does carry the field keeps its banner AND its body.
	buf.Reset()
	selectFields = []string{"id"}
	formatter.SetProjector(output.Projector{Select: selectFields})
	if err := printSection(cliCtx, "── Flagged Devices (1) ──\n", []map[string]any{{"id": "1"}}); err != nil {
		t.Fatalf("printSection: %v", err)
	}
	out := buf.String()
	// The table upper-cases its column names, so assert the rendered value.
	for _, want := range []string{"── Flagged Devices (1) ──", "RESULTS (1 total)"} {
		if !strings.Contains(out, want) {
			t.Errorf("a rendered section is missing %q: %q", want, out)
		}
	}
}

// TestSelectMatchingNothingStillEmitsAStructuredDocument keeps the
// empty-collection contract printRows documents for a nil slice: `null` breaks
// a jq pipeline, and zero bytes is worse — it is not a document at all.
// `pro report security -o json --select nosuchfield` wrote 0 bytes at exit 0,
// which raises JSONDecodeError, where main emitted a parseable document.
func TestSelectMatchingNothingStillEmitsAStructuredDocument(t *testing.T) {
	oldSelect, oldFmt, oldQuiet := selectFields, outputFmt, quiet
	t.Cleanup(func() { selectFields, outputFmt, quiet = oldSelect, oldFmt, oldQuiet })
	selectFields, quiet = []string{"nosuchfield"}, true

	for _, tc := range []struct {
		format string
		want   string
	}{
		{"json", "[]"},
		{"ndjson", ""},
		{"yaml", "[]"},
		// A table or CSV has no empty-document form, and its banner is gone.
		{"table", ""},
		{"csv", ""},
	} {
		t.Run(tc.format, func(t *testing.T) {
			outputFmt = tc.format
			var buf bytes.Buffer
			formatter := output.New(tc.format, true, false)
			formatter.SetWriter(&buf)
			formatter.SetProjector(output.Projector{Select: selectFields})
			cliCtx := &registry.CLIContext{Output: &cliOutput{formatter}}

			if err := printRows(cliCtx, []map[string]any{{"id": "1"}}); err != nil {
				t.Fatalf("printRows: %v", err)
			}
			got := strings.TrimSpace(buf.String())
			if tc.want == "" {
				if got != "" {
					t.Errorf("-o %s wrote %q, want nothing", tc.format, got)
				}
				return
			}
			if got != tc.want {
				t.Errorf("-o %s wrote %q, want %q — a structured consumer needs a document, not zero bytes", tc.format, got, tc.want)
			}
		})
	}
}
