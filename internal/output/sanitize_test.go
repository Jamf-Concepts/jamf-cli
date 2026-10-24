// Copyright 2026, Jamf Software LLC

package output

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// escapePayload carries an erase-line CSI, an OSC 52 clipboard write ended by
// BEL, and a C1 CSI, which is everything a device-authored name was seen
// smuggling into a table cell.
const escapePayload = "Evil\x1b[2K\x1b]52;c;aGVsbG8=\x07\u009bFAKE"

// assertInert fails if out still carries a byte a terminal would act on.
func assertInert(t *testing.T, where, out string) {
	t.Helper()
	if strings.ContainsRune(out, 0x1b) || strings.ContainsRune(out, 0x07) || strings.ContainsRune(out, 0x9b) {
		t.Errorf("%s wrote a raw control character: %q", where, out)
	}
	if !strings.Contains(out, `\x1b[2K`) {
		t.Errorf("%s did not render ESC visibly: %q", where, out)
	}
}

func TestSanitizeTerminal(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain name", "plain name"},
		{`quoted "name" and \ backslash`, `quoted "name" and \ backslash`},
		{"tab\tkept", "tab\tkept"},
		{"Bücher – ünïcode ✓", "Bücher – ünïcode ✓"},
		{"a\x1b[31mb", `a\x1b[31mb`},
		{"line\nbreak", `line\x0abreak`},
		{"cr\roverwrite", `cr\x0doverwrite`},
		{"del\x7f", `del\x7f`},
		{"c1\u009bx", `c1\x9bx`},
		{"bad\x9bbyte", `bad\x9bbyte`},
		{"nul\x00", `nul\x00`},
	}
	for _, c := range cases {
		if got := SanitizeTerminal(c.in); got != c.want {
			t.Errorf("SanitizeTerminal(%q) = %q, want %q", c.in, got, c.want)
		}
		if !utf8.ValidString(SanitizeTerminal(c.in)) {
			t.Errorf("SanitizeTerminal(%q) is not valid UTF-8", c.in)
		}
	}
}

func TestSanitizeTerminalLinesKeepsNewlinesOnly(t *testing.T) {
	got := SanitizeTerminalLines("first\nsecond\r\x1b]0;title\x07")
	want := `first` + "\n" + `second\x0d\x1b]0;title\x07`
	if got != want {
		t.Errorf("SanitizeTerminalLines = %q, want %q", got, want)
	}
}

func TestSanitizeTerminalLinesKeepsALineEndingCR(t *testing.T) {
	cases := map[string]string{
		"a\r\nb\r\n": "a\r\nb\r\n",
		"a\rb\r\n":   `a\x0db` + "\r\n",
		"end\r":      `end\x0d`,
		"a\r\r\nb":   `a\x0d` + "\r\nb",
		"x\x1b\r\ny": `x\x1b` + "\r\ny",
	}
	for in, want := range cases {
		if got := SanitizeTerminalLines(in); got != want {
			t.Errorf("SanitizeTerminalLines(%q) = %q, want %q", in, got, want)
		}
	}
	if got := SanitizeTerminal("a\r\nb"); got != `a\x0d\x0ab` {
		t.Errorf("SanitizeTerminal kept a line break: %q", got)
	}
}

func TestSanitizeTerminalReturnsCleanInputUnchanged(t *testing.T) {
	in := "Mac-01 (Bücher)"
	if got := SanitizeTerminal(in); got != in {
		t.Errorf("clean input changed: %q", got)
	}
	if allocs := testing.AllocsPerRun(100, func() { _ = SanitizeTerminal(in) }); allocs != 0 {
		t.Errorf("clean input allocated %v times; every table cell takes this path", allocs)
	}
}

func bufferFormatter(format string, terminal bool) (*Formatter, *bytes.Buffer) {
	var buf bytes.Buffer
	f := New(format, true, false)
	f.SetWriter(&buf)
	f.SetTerminal(terminal)
	return f, &buf
}

func TestPrintTableSanitizesCellsAndHeaders(t *testing.T) {
	// Not a terminal: a table is for a human wherever it goes.
	f, buf := bufferFormatter("table", false)
	rows := []map[string]any{{"id": "1", "name": escapePayload, "evil\x1bkey": "x"}}
	if err := f.Print(rows); err != nil {
		t.Fatal(err)
	}
	assertInert(t, "printTable", buf.String())
	if strings.Contains(buf.String(), "evil\x1bkey") {
		t.Errorf("printTable wrote a raw header: %q", buf.String())
	}
}

func TestPrintTableAlignsOnTheSanitizedWidth(t *testing.T) {
	f, buf := bufferFormatter("table", false)
	rows := []map[string]any{{"id": "1", "name": "a\x1bb"}, {"id": "2", "name": "plain"}}
	if err := f.Print(rows); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	last := lines[len(lines)-2:]
	if len(last[0]) != len(last[1]) {
		t.Errorf("rows are not aligned:\n%s\n%s", last[0], last[1])
	}
}

func TestPrintTableFallbackSanitizes(t *testing.T) {
	f, buf := bufferFormatter("table", false)
	if err := f.Print(escapePayload); err != nil {
		t.Fatal(err)
	}
	assertInert(t, "printTable fallback", buf.String())
}

func TestPrintDetailSanitizesValuesAndKeys(t *testing.T) {
	f, buf := bufferFormatter("table", false)
	if err := f.Print(map[string]any{"name": escapePayload, "k\x1b[1A": "v"}); err != nil {
		t.Fatal(err)
	}
	assertInert(t, "printDetail", buf.String())
	if strings.Contains(buf.String(), "k\x1b[1A") {
		t.Errorf("printDetail wrote a raw key: %q", buf.String())
	}
}

func TestPlainIsSanitizedOnlyOnATerminal(t *testing.T) {
	rows := []map[string]any{{"id": "1", "name": escapePayload}}

	f, buf := bufferFormatter("plain", true)
	if err := f.Print(rows); err != nil {
		t.Fatal(err)
	}
	assertInert(t, "printPlain on a terminal", buf.String())

	f, buf = bufferFormatter("plain", false)
	if err := f.Print(rows); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), escapePayload) {
		t.Errorf("printPlain to a pipe must stay byte-faithful: %q", buf.String())
	}
}

func TestCSVIsSanitizedOnlyOnATerminal(t *testing.T) {
	rows := []map[string]any{{"id": "1", "name": escapePayload, "k\x1b[1A": "v"}}

	f, buf := bufferFormatter("csv", true)
	if err := f.Print(rows); err != nil {
		t.Fatal(err)
	}
	assertInert(t, "printCSV on a terminal", buf.String())
	if strings.Contains(buf.String(), "k\x1b[1A") {
		t.Errorf("printCSV wrote a raw header on a terminal: %q", buf.String())
	}

	f, buf = bufferFormatter("csv", false)
	if err := f.Print(rows); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), escapePayload) {
		t.Errorf("printCSV to a file must stay byte-faithful: %q", buf.String())
	}
}

func TestCSVNeutralizesFormulas(t *testing.T) {
	cases := map[string]string{
		`=HYPERLINK("http://x","y")`: `'=HYPERLINK("http://x","y")`,
		"+cmd":                       "'+cmd",
		"-2+3":                       "'-2+3",
		"@SUM(A1)":                   "'@SUM(A1)",
		"\tlead":                     "'\tlead",
		"\rlead":                     "'\rlead",
		"-42":                        "-42",
		"-1.5e3":                     "-1.5e3",
		"+7":                         "+7",
		"plain":                      "plain",
		"":                           "",
		"a=b":                        "a=b",
	}
	for in, want := range cases {
		if got := neutralizeCSVFormula(in); got != want {
			t.Errorf("neutralizeCSVFormula(%q) = %q, want %q", in, got, want)
		}
	}

	f, buf := bufferFormatter("csv", false)
	if err := f.Print([]map[string]any{{"id": "1", "name": `=HYPERLINK("http://x","y")`, "delta": float64(-3)}}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"'=HYPERLINK(""http://x"",""y"")"`) {
		t.Errorf("formula cell not neutralized: %q", out)
	}
	if !strings.Contains(out, ",-3") && !strings.HasPrefix(strings.Split(out, "\n")[1], "-3,") {
		t.Errorf("negative number was altered: %q", out)
	}
}

func TestPrintRawFallbacksSanitizeOnATerminal(t *testing.T) {
	notJSON := []byte("<<not json " + escapePayload + "\nsecond line")

	for _, format := range []string{"json", "table"} {
		f, buf := bufferFormatter(format, true)
		if err := f.PrintRaw(notJSON); err != nil {
			t.Fatal(err)
		}
		assertInert(t, "PrintRaw "+format+" fallback", buf.String())
		if !strings.Contains(buf.String(), "\nsecond line") {
			t.Errorf("PrintRaw %s fallback lost its line break: %q", format, buf.String())
		}

		f, buf = bufferFormatter(format, false)
		if err := f.PrintRaw(notJSON); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(buf.Bytes(), notJSON) {
			t.Errorf("PrintRaw %s fallback to a pipe must stay byte-faithful: %q", format, buf.String())
		}
	}
}

func TestPrintBytesSanitizesOnATerminalExceptRaw(t *testing.T) {
	data := []byte("text " + escapePayload)

	f, buf := bufferFormatter("xml", true)
	if err := f.PrintBytes(data); err != nil {
		t.Fatal(err)
	}
	assertInert(t, "PrintBytes", buf.String())

	f, buf = bufferFormatter("raw", true)
	if err := f.PrintBytes(data); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), escapePayload) {
		t.Errorf("-o raw promises exact wire bytes: %q", buf.String())
	}
}

// writerSanctioned names the Formatter methods that write to f.writer without
// sanitizing, each with its reason. Every other method that writes there must
// reach one of sanitizingCalls.
var writerSanctioned = map[string]string{
	"printYAML":  "yaml.v3 quotes and escapes control characters",
	"PrintRaw":   "-o raw is an explicit request for exact wire bytes; its other writes go through writeBytes and writeJSON",
	"writeBytes": "is the sanitizing writer",
	"SetWriter":  "assigns the writer and writes nothing",
	"Writer":     "returns the writer; a caller rendering its own text owns sanitizing it",
}

var sanitizingCalls = map[string]bool{
	"SanitizeTerminal":      true,
	"SanitizeTerminalLines": true,
	"forTerminal":           true,
	"writeBytes":            true,
	"escapeJSONControls":    true,
}

// TestEveryRendererSanitizes keeps the human renderers sanitizing: a method
// that writes to f.writer and reaches no sanitizing call is a renderer a
// device-authored name can drive the terminal through.
func TestEveryRendererSanitizes(t *testing.T) {
	fset := token.NewFileSet()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var decls []ast.Decl
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		decls = append(decls, file.Decls...)
	}
	seen := map[string]bool{}
	for _, decl := range decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Body == nil {
			continue
		}
		writes, sanitizes := false, false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.SelectorExpr:
				if id, ok := n.X.(*ast.Ident); ok && id.Name == "f" && n.Sel.Name == "writer" {
					writes = true
				}
				if sanitizingCalls[n.Sel.Name] {
					sanitizes = true
				}
			case *ast.Ident:
				if sanitizingCalls[n.Name] {
					sanitizes = true
				}
			}
			return true
		})
		name := fn.Name.Name
		if _, ok := writerSanctioned[name]; ok {
			seen[name] = true
			continue
		}
		if writes && !sanitizes {
			t.Errorf("%s writes to f.writer without sanitizing. Pass each value through SanitizeTerminal (or forTerminal for a format a parser reads), or add it to writerSanctioned with its reason", name)
		}
	}
	for name := range writerSanctioned {
		if !seen[name] {
			t.Errorf("writerSanctioned names %s, which internal/output no longer declares", name)
		}
	}
}

func TestPlainScalarsAreSanitizedOnlyOnATerminal(t *testing.T) {
	type named struct{ Name string }
	for what, data := range map[string]any{"string": escapePayload, "struct": named{escapePayload}} {
		f, buf := bufferFormatter("plain", true)
		if err := f.Print(data); err != nil {
			t.Fatal(err)
		}
		assertInert(t, "printPlain "+what+" on a terminal", buf.String())

		f, buf = bufferFormatter("plain", false)
		if err := f.Print(data); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), escapePayload) {
			t.Errorf("printPlain %s to a pipe must stay byte-faithful: %q", what, buf.String())
		}
	}
}

func TestJSONEscapesC1AndDELOnlyOnATerminal(t *testing.T) {
	value := "a\u009b[2Jb\x7fc"
	wire := []byte(`{"name":"` + value + `","bad":"x` + "\xff" + `y"}`)
	runs := map[string]func(*Formatter) error{
		"printJSON":         func(f *Formatter) error { return f.Print(map[string]any{"name": value}) },
		"printNDJSON":       func(f *Formatter) error { return f.Print([]map[string]any{{"name": value}}) },
		"PrintRaw fastpath": func(f *Formatter) error { return f.PrintRaw(wire) },
	}
	formats := map[string]string{"printJSON": "json", "printNDJSON": "ndjson", "PrintRaw fastpath": "json"}
	for name, run := range runs {
		f, buf := bufferFormatter(formats[name], true)
		if err := run(f); err != nil {
			t.Fatal(err)
		}
		out := buf.String()
		if strings.ContainsRune(out, 0x9b) || strings.ContainsRune(out, 0x7f) || !utf8.ValidString(out) {
			t.Errorf("%s on a terminal wrote a raw C1, DEL or invalid byte: %q", name, out)
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &got); err != nil {
			t.Errorf("%s on a terminal is no longer valid JSON: %v: %q", name, err, out)
		} else if got["name"] != value {
			t.Errorf("%s on a terminal changed the value: %q", name, got["name"])
		}

		f, buf = bufferFormatter(formats[name], false)
		if err := run(f); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), value) {
			t.Errorf("%s to a pipe must stay byte-faithful: %q", name, buf.String())
		}
	}
}
