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

// restoreOutputFlags saves every global output flag var and puts it back when
// the test ends. They are package-level and cobra parses into them, so any test
// that drives the root command leaks its flags into the next one.
func restoreOutputFlags(t *testing.T) {
	t.Helper()
	oFmt, oColor, oWide := outputFmt, noColor, wide
	oFile, oSelect, oCompact := outFile, selectFields, compact
	oField, oQuiet, oHints := fieldName, quiet, noHints
	t.Cleanup(func() {
		outputFmt, noColor, wide = oFmt, oColor, oWide
		outFile, selectFields, compact = oFile, oSelect, oCompact
		fieldName, quiet, noHints = oField, oQuiet, oHints
	})
}

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

	// root.Execute() parses into the package flag vars, so without this a value
	// set here leaks into whatever test runs next. `-shuffle=4` failed on it.
	restoreOutputFlags(t)

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

// The signature of the defect is exit 0, a 0-byte file, and the whole payload
// on standard output.
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
// command with every field populated shows no difference, so the unprojected
// run is asserted first.
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

// --quiet and --no-hints both suppress the advisory hint the formatter writes
// to standard error above 50 rows. That is the only reachable effect either
// flag has on this command.
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
// own text and reach the destination through writerFor. This covers the helper.
// TestOverviewRenderersTakeTheFormattersWriter covers their call sites.
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

// Print has no case for raw or xml, so its default arm renders the table.
// Routing through PrintRaw instead reaches PrintRaw's own FormatRaw and
// FormatXML arms and emits marshalled JSON, which is a different answer under a
// flag documented as "exact wire bytes" for a report that has none.
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

// A multi-section report sends its section headers wherever its tables go, or
// --out-file splits one report across a file and a terminal.
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

// An empty list prints `[]`, never `null`, which is a CLAUDE.md convention:
// `null` breaks a jq pipeline on the tenants where a collection is empty.
// overviewToRows declares `var rows []map[string]any` and appends only inside a
// loop, so a report whose sections all came back empty reaches printRows nil.
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

// Every setter the formatter exposes carries one global flag to the output, and
// buildOutputFormatter is the only place they are applied. A test that observes
// the flag holds four of the five. SetExplicitNoColor has no such test: its
// only reader is PaginationProgress, which needs stderr to be a terminal, and
// isStderrTTY is unexported.
//
// So this pins the wiring, and reads the set off the formatter rather than
// listing it. A sixth setter buildOutputFormatter never calls is the same
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

// formatterFor's fallback covers a caller reached with a test double, or one
// reached before PersistentPreRunE ran. It calls the shared builder, so the
// flags a fresh output.New discards apply there too.
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

// The two callers that ask the shared formatter for a format their own argument
// names rather than the global -o value. The TestGroupToolsExport_* cases assert
// against marshalGroupsJSON and marshalGroupsYAML, which runGroupToolsExport
// never calls, so nothing else reaches WithFormat through a non-global format.
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
	// The shared formatter is reused for the rest of the invocation, so an
	// aliasing WithFormat would leave every later command rendering YAML.
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

	// json and yaml return early from printAggregated, so neither reaches the
	// section helper. "" is the arm `multi` takes with no -o at all. csv and
	// plain are the only machine-rendered formats that reach the helper, so
	// they are what exercises its suppression.
	for _, format := range []string{"json", "yaml", "table", "", "csv", "plain"} {
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
			// They must not travel into a stream a parser reads. A `──` line
			// makes csv.Reader yield a one-field row.
			if format == "csv" || format == "plain" {
				if strings.Contains(buf.String(), "──") {
					t.Errorf("-o %s put box-drawing lines into a machine-read stream: %q", name, shortened(buf.String()))
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
// derived from the signature every one of them shares, a writer followed by the
// sections to render, so a fifth overview command's renderer is covered without
// an edit here. A hardcoded list of names cannot see one.
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
				// printSection counts as routing. It writes the header through
				// writerFor itself.
				if node.Name == "writerFor" || node.Name == "printSection" {
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
				case "Fprint", "Fprintf", "Fprintln":
					// A write guarded on a nil CLIContext is writerFor's own
					// fallback, unreachable in a real run. Exempted by name so
					// the rule still sees every other write in the file.
					if stdoutFallbackSites[name] {
						return true
					}
					// fmt.Fprintln(os.Stdout, …) is the same bug spelled with a
					// writer. printSchoolOverviewTable is 0% unit-covered, so
					// this guard is its only feedback loop.
					if len(node.Args) > 0 && isStdout(node.Args[0]) {
						bare = append(bare, fmt.Sprintf("%s:%d", name, fset.Position(node.Lparen).Line))
					}
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

// stdoutFallbackSites are files whose only os.Stdout write is the fallback for
// a nil CLIContext. That is the shape writerFor carries, and it is unreachable
// once PersistentPreRunE has run.
//
// renderVersion (version.go): `if cliCtx != nil && cliCtx.Output != nil` routes
// through the formatter, and the os.Stdout line below it exists for a caller
// reached with a test double.
var stdoutFallbackSites = map[string]bool{
	"version.go": true,
}

// isStdout reports whether e is os.Stdout.
func isStdout(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Stdout" && isPackageIdent(sel.X, "os")
}

func isPackageIdent(expr ast.Expr, name string) bool {
	id, ok := expr.(*ast.Ident)
	return ok && id.Name == name
}

// sanctionedFormatterSites is the whole set of places an output.Formatter may
// be built, keyed by file and enclosing function. A construction anywhere else
// is a finding. Each reason is recorded at the site itself.
var sanctionedFormatterSites = map[string]string{
	// The shared formatter. Every global output flag is applied here.
	"commands/root.go": "buildOutputFormatter",
	// newBulkCmd's Long documents the contract: preview table on stdout,
	// mutation log on stderr. It stays a table whatever -o says, so it must not
	// follow --out-file into the data file.
	"commands/pro_bulk.go": "bulkPreviewTable",
	// The bulk-targeting preview, keeping the same documented contract.
	"commands/pro_device_actions.go": "deviceActionPreviewTable",
}

// buildsFormatter reports whether n produces or captures an output.Formatter,
// where local is the name internal/output is bound to in the file being read.
//
// New is that package's only exported constructor and Formatter's fields are
// unexported, so three shapes reach one: a reference to New, a composite
// literal of the type, and new() of it.
//
// It matches the selector local.New rather than a call to it, so it covers both
// `output.New(…)` and `mk := output.New`. Matching the call alone misses the
// second, because a constructor taken as a value puts the call one hop away.
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
		// The literal's own type, or the element type of the collection it
		// belongs to. An inner literal that elides its element type has
		// Type == nil, so only the outer literal names the type at all.
		return namesFormatterType(v.Type, local)
	case *ast.CallExpr:
		if id, ok := v.Fun.(*ast.Ident); ok && id.Name == "new" && len(v.Args) == 1 {
			return isSel(v.Args[0], "Formatter")
		}
	}
	return false
}

// namesFormatterType reports whether e denotes output.Formatter, or a pointer,
// slice, array or map whose element does.
//
// It is what catches a composite literal with an elided element type. A
// go/types walk would collapse every construction shape into one rule and close
// the unexported-constructor hole too; this stays syntactic, so a construction
// whose type is only inferrable, a factory returning an interface for example,
// is
// still out of reach.
func namesFormatterType(e ast.Expr, local string) bool {
	switch t := e.(type) {
	case *ast.StarExpr:
		return namesFormatterType(t.X, local)
	case *ast.ArrayType:
		return namesFormatterType(t.Elt, local)
	case *ast.MapType:
		return namesFormatterType(t.Value, local)
	case *ast.SelectorExpr:
		if t.Sel.Name != "Formatter" {
			return false
		}
		id, isIdent := t.X.(*ast.Ident)
		return isIdent && id.Name == local
	}
	return false
}

// TestNoFileBuildsItsOwnOutputFormatter refuses an output.Formatter built
// outside the sanctioned sites. A command that builds its own receives none of
// the six global output flags, so each one is parsed and then discarded.
//
// The rule resolves the import rather than matching construction syntax, so a
// file that cannot name internal/output cannot trip it in any form.
//
// Attribution is to the enclosing top-level function, so a construction inside
// a closure inherits that function's exemption. Keep a sanctioned function
// small for that reason.
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
		// A file that does not import the package cannot construct one, whatever
		// syntax it reaches for. internal/output's own files are not importers.
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
			t.Errorf("%s dot-imports internal/output, which puts New and Formatter beyond this rule. Import it normally", path)
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
				t.Errorf("%s: %s builds or captures its own output.Formatter, so every global output flag is inert on it. Print through printRows or formatterFor instead", rel, site)
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
			t.Errorf("sanctioned site %s:%s builds no formatter any more. Remove the entry, or point it at the function that does", file, fn)
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
// of values on stdout at exit 0. With no -o, f was 0 bytes, which is the
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

	if err := printRows(cliCtx, []map[string]any{{"id": "1", "status": "ok"}}); err != nil {
		t.Fatalf("printRows: %v", err)
	}
	if got := buf.String(); got != "" {
		t.Errorf("a section carrying none of the --select paths rendered %q, which reads as a broken renderer rather than an absent field", got)
	}

	// A section that does carry it must still render.
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
// `── Flagged Devices (5) ──`, at exit 0. A -o csv consumer received a
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
		t.Errorf("a skipped section still wrote %q, so the banner outlives the body it announced", got)
	}

	// A section that does carry the field keeps its banner and its body.
	buf.Reset()
	selectFields = []string{"id"}
	formatter.SetProjector(output.Projector{Select: selectFields})
	if err := printSection(cliCtx, "── Flagged Devices (1) ──\n", []map[string]any{{"id": "1"}}); err != nil {
		t.Fatalf("printSection: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"── Flagged Devices (1) ──", "RESULTS (1 total)"} {
		if !strings.Contains(out, want) {
			t.Errorf("a rendered section is missing %q: %q", want, out)
		}
	}
}

// TestSelectReachesAWideOnlyFieldOnANarrowFormat pins the row-set clause of
// --select, in newCommandsCmd rather than in the formatter.
//
// `full := wide || isFullDetailFormat(outputFmt) || len(selectFields) > 0` puts
// a wide-only field into the rows at all. Deleting the --select clause leaves
// the whole package green without this test.
// TestSelectBypassesTheDefaultColumnHeuristicBeyondItsThreshold covers the
// formatter clause instead, and either clause alone renders nothing.
//
// table, plain, xml and raw are the four narrow formats. All four are listed,
// because isFullDetailFormat naming a format takes it off this path.
func TestSelectReachesAWideOnlyFieldOnANarrowFormat(t *testing.T) {
	for _, format := range []string{"table", "plain", "xml", "raw"} {
		t.Run(format, func(t *testing.T) {
			restoreOutputFlags(t)
			// `api` is wide-only, and platform-gateway is a value it carries.
			stdout, stderr, err := runRoot(t, "commands", "-o", format, "--select", "api", "--quiet")
			if err != nil {
				t.Fatalf("commands -o %s --select api: %v", format, err)
			}
			if !strings.Contains(stdout, "platform-gateway") {
				t.Errorf("-o %s --select api rendered no api value. The narrow row set withheld the field the caller named.\nstdout: %q\nstderr: %q",
					format, shortened(stdout), shortened(stderr))
			}
		})
	}
}

// TestFieldMissIsReportedAndSurvivesQuiet pins reportFieldMiss.
//
// The note goes to stderr, and the only other test driving a real field miss
// reads stdout, so both mutations pass without this. Deleting the call leaves
// `commands --field nosuchfield --out-file f` at 0 bytes with both streams
// empty at exit 0. Restoring the quiet || noHints suppression does the same
// under the flags a CI job passes.
func TestFieldMissIsReportedAndSurvivesQuiet(t *testing.T) {
	rows := []map[string]any{{"id": "1"}, {"id": "2"}}

	for _, tc := range []struct {
		name         string
		quiet, hints bool
	}{
		{name: "plain"},
		{name: "quiet", quiet: true},
		{name: "no-hints", hints: true},
		{name: "both", quiet: true, hints: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			restoreOutputFlags(t)
			fieldName, quiet, noHints = "nosuchfield", tc.quiet, tc.hints

			var buf bytes.Buffer
			formatter := output.New("json", true, false)
			formatter.SetWriter(&buf)
			cliCtx := &registry.CLIContext{Output: &cliOutput{formatter}}

			stderr := captureStderr(t, func() {
				if err := printRows(cliCtx, rows); err != nil {
					t.Fatalf("printRows: %v", err)
				}
			})
			if !strings.Contains(stderr, "--field nosuchfield matched no field in 2 row(s)") {
				t.Errorf("stderr = %q, want the field-miss note. A --field miss writes nothing at all, so nothing else reports it", stderr)
			}
		})
	}
}

// TestProjectionMissIsReportedAndSurvivesQuiet is the same guard for --select
// and --compact.
//
// json, yaml and ndjson emit a document of empty objects for a projection that
// matches nothing. table, csv, plain and detail decline the empty column set
// and write nothing, so `commands -o table --select nosuchfield` gave 0 bytes
// on both streams at exit 0, which is issue #349's signature.
func TestProjectionMissIsReportedAndSurvivesQuiet(t *testing.T) {
	rows := []map[string]any{{"id": "1"}, {"id": "2"}}
	// --compact keeps a scalar whose key appears in at least 80% of rows, so
	// only a row set of rare keys empties every row.
	rare := []map[string]any{{"a": "1"}, {"b": "2"}, {"c": "3"}, {"d": "4"}, {"e": "5"}}

	for _, tc := range []struct {
		name         string
		rows         []map[string]any
		selectFields []string
		compact      bool
		want         string
	}{
		{name: "select", rows: rows, selectFields: []string{"nosuchfield"}, want: "--select nosuchfield matched no field in 2 row(s)"},
		{name: "compact", rows: rare, compact: true, want: "--compact matched no field in 5 row(s)"},
	} {
		for _, silent := range []bool{false, true} {
			name := tc.name
			if silent {
				name += "/quiet+no-hints"
			}
			t.Run(name, func(t *testing.T) {
				restoreOutputFlags(t)
				outputFmt = "table"
				selectFields, compact = tc.selectFields, tc.compact
				quiet, noHints = silent, silent

				var buf bytes.Buffer
				formatter := output.New("table", true, false)
				formatter.SetWriter(&buf)
				formatter.SetProjector(output.Projector{Select: tc.selectFields, Compact: tc.compact})
				cliCtx := &registry.CLIContext{Output: &cliOutput{formatter}}

				stderr := captureStderr(t, func() {
					if err := printRows(cliCtx, tc.rows); err != nil {
						t.Fatalf("printRows: %v", err)
					}
				})
				if !strings.Contains(stderr, tc.want) {
					t.Errorf("stderr = %q, want %q. The renderer declines an empty column set, so nothing else reports it", stderr, tc.want)
				}
				if buf.Len() != 0 {
					t.Errorf("the table rendered %q over no columns", buf.String())
				}
			})
		}
	}

	t.Run("match is silent", func(t *testing.T) {
		restoreOutputFlags(t)
		outputFmt = "table"
		selectFields = []string{"id"}

		var buf bytes.Buffer
		formatter := output.New("table", true, false)
		formatter.SetWriter(&buf)
		formatter.SetProjector(output.Projector{Select: selectFields})
		cliCtx := &registry.CLIContext{Output: &cliOutput{formatter}}

		stderr := captureStderr(t, func() {
			if err := printRows(cliCtx, rows); err != nil {
				t.Fatalf("printRows: %v", err)
			}
		})
		if strings.Contains(stderr, "matched no field") {
			t.Errorf("a projection that matched reported a miss: %q", stderr)
		}
	})
}

// TestSectionBannerIsWithheldForAMachineFormat pins printSection's second rule.
//
// The test above sets outputFmt = "table" for both cases, so it never reaches
// the IsMachineRendered condition with rows present and a header to write.
// Dropping that condition puts box-drawing lines back into a CSV stream with
// the suite green.
func TestSectionBannerIsWithheldForAMachineFormat(t *testing.T) {
	restoreOutputFlags(t)
	selectFields, quiet = nil, true

	for _, tc := range []struct {
		format string
		banner bool
	}{
		{"csv", false},
		{"plain", false},
		{"ndjson", false},
		// xml and raw have no case in Print's switch, so both render tables and
		// both take a banner.
		{"xml", true},
		{"table", true},
	} {
		t.Run(tc.format, func(t *testing.T) {
			outputFmt = tc.format
			var buf bytes.Buffer
			formatter := output.New(tc.format, true, false)
			formatter.SetWriter(&buf)
			cliCtx := &registry.CLIContext{Output: &cliOutput{formatter}}

			if err := printSection(cliCtx, "── Flagged Devices (1) ──\n", []map[string]any{{"id": "1"}}); err != nil {
				t.Fatalf("printSection: %v", err)
			}
			out := buf.String()
			if got := strings.Contains(out, "──"); got != tc.banner {
				t.Errorf("-o %s wrote a banner = %v, want %v: %q", tc.format, got, tc.banner, out)
			}
			if !strings.Contains(out, "1") {
				t.Errorf("-o %s wrote no body at all: %q", tc.format, out)
			}
		})
	}
}

// TestSelectMatchingNothingRendersConsistently pins what a projection matching
// no field produces, per format.
//
// A projection leaves each row with no fields. It does not remove the rows. So
// a machine format emits a document of empty objects, matching the 200+
// generated commands, and a table or CSV emits nothing rather than a banner
// above a blank header.
//
// An earlier revision dropped the emptied rows and forced `[]` here. That made
// the hand-written commands disagree with the generated ones, and its
// heterogeneous survivors moved the original defect into the column set, then
// into every renderer and every `multi` arm.
func TestSelectMatchingNothingRendersConsistently(t *testing.T) {
	oldSelect, oldFmt := selectFields, outputFmt
	t.Cleanup(func() { selectFields, outputFmt = oldSelect, oldFmt })
	selectFields = []string{"nosuchfield"}

	for _, tc := range []struct {
		format string
		want   string
	}{
		// The rows are still there, with no fields.
		{"json", "[\n  {}\n]"},
		{"yaml", "- {}"},
		{"ndjson", "{}"},
		// No column set, so no table and no CSV.
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
			if got := strings.TrimSpace(buf.String()); got != tc.want {
				t.Errorf("-o %s wrote %q, want %q", tc.format, got, tc.want)
			}
		})
	}
}

// TestNoReportWritesABannerThenPrintRows pins the call sites rather than the
// helper.
//
// printSection decides the drop and the format before writing the header. A
// site that writes its own banner and then calls printRows skips that decision:
// `pro report profile-status -o table --select nosuchfield` wrote 117 bytes of
// box-drawing lines and no rows, and -o csv handed a CSV consumer the same
// stream.
//
// TestSelectMatchingNothingLeavesNoOrphanBanner calls printSection directly and
// asserts nothing about its callers. The report functions are 0% covered, so
// the call site is the only thing that can hold this.
func TestNoReportWritesABannerThenPrintRows(t *testing.T) {
	_, files := packageFiles(t)

	checked := 0
	for name, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			block, ok := n.(*ast.BlockStmt)
			if !ok {
				return true
			}
			for i, stmt := range block.List {
				if !writesASectionBanner(stmt) {
					continue
				}
				checked++
				// Every later statement in the block, not just the next one. A
				// single statement between a banner and its printRows hid the
				// site this rule was written over.
				for _, later := range block.List[i+1:] {
					if writesASectionBanner(later) {
						break // the next section starts; this one is settled
					}
					if callsPrintRows(later) {
						t.Errorf("%s: a section banner is followed by printRows in the same block. Use printSection, which decides the drop and the format before the header", name)
						break
					}
				}
			}
			return true
		})
	}
	if checked == 0 {
		t.Error("no section banner was found anywhere in the package, so this test proves nothing")
	}
	t.Logf("checked %d section banners across %d files", checked, len(files))
}

// writesASectionBanner reports whether stmt writes a `── … ──` header itself,
// as opposed to handing one to printSection.
//
// A banner passed to printSection is correct by definition, which is what the
// helper is for, so counting it made every converted report look like a
// violation the moment any later statement in the block called printRows.
func writesASectionBanner(stmt ast.Stmt) bool {
	if callsNamed(stmt, "printSection") {
		return false
	}
	found := false
	ast.Inspect(stmt, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if ok && strings.Contains(lit.Value, "──") {
			found = true
		}
		return !found
	})
	return found
}

// callsNamed reports whether stmt calls the named package-level function.
func callsNamed(stmt ast.Stmt, name string) bool {
	found := false
	ast.Inspect(stmt, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, isIdent := call.Fun.(*ast.Ident); isIdent && id.Name == name {
			found = true
		}
		return !found
	})
	return found
}

// callsPrintRows reports whether stmt calls printRows.
func callsPrintRows(stmt ast.Stmt) bool {
	return callsNamed(stmt, "printRows")
}
