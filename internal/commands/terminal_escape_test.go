// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/resolve"
)

// escapePayload carries an erase-line CSI, an OSC 52 clipboard write ended by
// BEL, a screen clear and a C1 CSI: what a 403 body and a device-authored
// computer name were seen writing to the terminal.
const escapePayload = "Evil\x1b[2K\x1b]52;c;aGVsbG8=\x07\x1b[2J\u009bFAKE"

func assertTerminalInert(t *testing.T, where, out string) {
	t.Helper()
	if strings.ContainsRune(out, 0x1b) || strings.ContainsRune(out, 0x07) || strings.ContainsRune(out, 0x9b) {
		t.Errorf("%s wrote a raw control character: %q", where, out)
	}
	if !strings.Contains(out, `\x1b[2K`) {
		t.Errorf("%s did not render ESC visibly: %q", where, out)
	}
}

func TestFprintErrorSanitizesErrorAndHint(t *testing.T) {
	var buf bytes.Buffer
	err := exitcode.New(exitcode.PermissionDenied, "permission denied (HTTP 403): "+escapePayload+"\nsecond line").
		WithHint("Required privilege(s): " + escapePayload)
	FprintError(&buf, err)
	out := buf.String()
	assertTerminalInert(t, "FprintError", out)
	if !strings.Contains(out, "\nsecond line\n") {
		t.Errorf("FprintError collapsed an intended line break: %q", out)
	}
	if !strings.Contains(out, "\nhint: Required privilege(s): Evil") {
		t.Errorf("FprintError lost the hint line: %q", out)
	}
}

func TestFprintErrorSanitizesAPlainError(t *testing.T) {
	var buf bytes.Buffer
	FprintError(&buf, errors.New("API login failed: HTTP 500: "+escapePayload))
	assertTerminalInert(t, "FprintError", buf.String())
}

func escapeSections() []overviewSection {
	return []overviewSection{{
		Name: "Section " + escapePayload,
		Items: []overviewItem{
			{Resource: "Computer " + escapePayload, Value: escapePayload},
			{Resource: "Plain", Value: "ok"},
		},
	}}
}

func TestOverviewRenderersSanitize(t *testing.T) {
	renderers := map[string]func(*bytes.Buffer){
		"printOverviewTable": func(b *bytes.Buffer) {
			printOverviewTable(b, escapeSections(), true, "DEVICE "+escapePayload)
		},
		"printProtectOverviewTable": func(b *bytes.Buffer) { printProtectOverviewTable(b, escapeSections(), true) },
		"printSchoolOverviewTable":  func(b *bytes.Buffer) { printSchoolOverviewTable(b, escapeSections(), true) },
	}
	for name, render := range renderers {
		var buf bytes.Buffer
		render(&buf)
		out := buf.String()
		// The renderer's own colour must survive: it is the CLI's escape, not the server's.
		withoutOwnColour := out
		for _, sgr := range []string{"\033[0m", "\033[1m", "\033[2m", "\033[31m", "\033[32m", "\033[33m"} {
			withoutOwnColour = strings.ReplaceAll(withoutOwnColour, sgr, "")
		}
		assertTerminalInert(t, name, withoutOwnColour)
		if !strings.Contains(out, "\033[1m") {
			t.Errorf("%s lost its own bold heading: %q", name, out)
		}
	}
}

func TestOverviewAlignsOnTheSanitizedValue(t *testing.T) {
	var buf bytes.Buffer
	printSchoolOverviewTable(&buf, []overviewSection{{Name: "S", Items: []overviewItem{
		{Resource: "a", Value: "x\x1by"},
		{Resource: "b", Value: `x\x1by`},
	}}}, false)
	lines := strings.Split(buf.String(), "\n")
	var got []string
	for _, l := range lines {
		if strings.HasPrefix(l, "  a ") || strings.HasPrefix(l, "  b ") {
			got = append(got, l)
		}
	}
	if len(got) != 2 || len(got[0]) != len(got[1]) {
		t.Errorf("sanitized value is not right-aligned with its equal: %q", got)
	}
}

func TestBulkLogSanitizesTargetAndResult(t *testing.T) {
	var buf bytes.Buffer
	bulkLogW(&buf, "send-command", escapePayload, "ERROR: "+escapePayload)
	assertTerminalInert(t, "bulkLogW", buf.String())
}

func TestPolicyDescSanitizesTheName(t *testing.T) {
	p := policyEntry{id: "7", detail: map[string]any{"general": map[string]any{"name": escapePayload}}}
	assertTerminalInert(t, "policyDesc", policyDesc(p))
}

func TestFormatDeviceDescSanitizesIDAndSerial(t *testing.T) {
	got := resolve.FormatDeviceDesc(&resolve.DeviceIdentifiers{ID: escapePayload, SerialNumber: escapePayload, Name: "Mac"})
	assertTerminalInert(t, "FormatDeviceDesc", got)
}

func TestFieldValuesSanitizeOnlyOnATerminal(t *testing.T) {
	objects := []map[string]any{{"name": escapePayload}}

	var tty bytes.Buffer
	if err := printFieldValues(&tty, objects, "name", true); err != nil {
		t.Fatal(err)
	}
	assertTerminalInert(t, "--field on a terminal", tty.String())

	var pipe bytes.Buffer
	if err := printFieldValues(&pipe, objects, "name", false); err != nil {
		t.Fatal(err)
	}
	if pipe.String() != escapePayload+"\n" {
		t.Errorf("--field to a pipe must stay byte-faithful: %q", pipe.String())
	}
}

func TestTermSafe(t *testing.T) {
	assertTerminalInert(t, "termSafe", termSafe(escapePayload))
	assertTerminalInert(t, "termSafe(error)", termSafe(errors.New(escapePayload)))
	if got := termSafe("12"); got != "12" {
		t.Errorf("termSafe changed a clean id: %q", got)
	}
}

func TestJCDSLocalPathRefusesControlCharacters(t *testing.T) {
	if _, err := jcdsLocalPath(t.TempDir(), "pkg\x1b]0;x\x07.pkg"); err == nil {
		t.Fatal("jcdsLocalPath accepted a file name carrying terminal escapes")
	} else if strings.ContainsRune(err.Error(), 0x1b) {
		t.Errorf("the refusal itself carries the raw escape: %q", err.Error())
	}
	if _, err := jcdsLocalPath(t.TempDir(), "Firefox 126.0.pkg"); err != nil {
		t.Errorf("jcdsLocalPath refused an ordinary name: %v", err)
	}
}

func TestCheckTenantVersionDropsAnEscapeCarryingVersion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	evil := "0.0.0\x1b]0;PWNED-TITLE\x07\x1b[2K"
	body, _ := json.Marshal(map[string]string{"version": evil})

	var buf bytes.Buffer
	checkTenantVersion(&versionMockClient{body: string(body)}, "11.28.0", "p", &buf)
	if buf.Len() != 0 {
		t.Errorf("a malformed version was printed: %q", buf.String())
	}
	if cache := readVersionCache(); cache != nil {
		if _, ok := cache["p"]; ok {
			t.Error("a malformed version was cached")
		}
	}
}

func TestCheckTenantVersionDropsAnEscapeCarryingCachedVersion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	data, _ := json.Marshal(map[string]versionCacheEntry{
		"p": {Version: "0.0.0\x1b]0;PWNED\x07", CheckedAt: time.Now().UTC()},
	})
	cacheFile := filepath.Join(dir, "jamf-cli", ".version-cache.json")
	_ = os.MkdirAll(filepath.Dir(cacheFile), 0o755)
	_ = os.WriteFile(cacheFile, data, 0o600)

	var buf bytes.Buffer
	checkTenantVersion(&versionMockClient{body: `{"version":"11.28.0"}`}, "11.28.0", "p", &buf)
	if strings.ContainsRune(buf.String(), 0x1b) {
		t.Errorf("a malformed cached version was replayed: %q", buf.String())
	}
}

func TestProVersionPattern(t *testing.T) {
	for _, v := range []string{"11.28.0", "11.28.0-t1729188960", "11.33", "10.52.0.1"} {
		if !proVersionPattern.MatchString(v) {
			t.Errorf("%q is a real Jamf Pro version and was refused", v)
		}
	}
	for _, v := range []string{"", "11", "11.28.0\x1b[2K", "11.28.0 extra", "v11.28.0", strings.Repeat("1", 7) + ".0"} {
		if proVersionPattern.MatchString(v) {
			t.Errorf("%q was accepted as a version", v)
		}
	}
}

// terminalChokePoints are the functions every caller of a class of output goes
// through. Each must still reach a sanitizing call, or the class reopens with
// no test noticing.
var terminalChokePoints = map[string]string{
	"FprintError":               "root.go",
	"printFieldValues":          "root.go",
	"printOverviewTable":        "pro_overview.go",
	"printProtectOverviewTable": "protect_overview.go",
	"printSchoolOverviewTable":  "school_overview.go",
	"bulkLogW":                  "pro_bulk.go",
	"policyDesc":                "pro_classic_policy_toggle.go",
	"jcdsLocalPath":             "pro_jcds.go",
	"checkTenantVersion":        "root.go",
}

var sanitizingIdents = map[string]bool{
	"SanitizeTerminal":         true,
	"SanitizeTerminalLines":    true,
	"termSafe":                 true,
	"sanitizeOverviewSections": true,
	"proVersionPattern":        true,
}

func TestTerminalChokePointsSanitize(t *testing.T) {
	for fn, file := range terminalChokePoints {
		fset := gotoken.NewFileSet()
		f, err := goparser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		found, sanitizes := false, false
		for _, decl := range f.Decls {
			d, ok := decl.(*ast.FuncDecl)
			if !ok || d.Name.Name != fn || d.Recv != nil {
				continue
			}
			found = true
			ast.Inspect(d.Body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.Ident:
					sanitizes = sanitizes || sanitizingIdents[n.Name]
				case *ast.SelectorExpr:
					sanitizes = sanitizes || sanitizingIdents[n.Sel.Name]
				}
				return true
			})
		}
		if !found {
			t.Errorf("%s no longer declares %s. Point terminalChokePoints at where it moved", file, fn)
			continue
		}
		if !sanitizes {
			t.Errorf("%s (%s) writes server-supplied text to a terminal and no longer sanitizes it", fn, file)
		}
	}
}

// TestNoStderrLinePrintsARawID refuses an id handed raw to a %s or %v verb of
// an fmt.Fprintf to stderr, in hand-written and generated commands alike. An id
// a server returns is a string it chose, so it reaches a consent prompt or a
// progress line with whatever bytes it carries. Wrap it in termSafe
// (platform.TermSafe in a platform template), or print it with %q.
//
// It keys on spelling: an argument whose name ends in "id" (id, resolvedID,
// e.id, group.ID). An id held under another name, or a format string that is
// not a literal, passes unseen.
func TestNoStderrLinePrintsARawID(t *testing.T) {
	for _, root := range []string{".", "../resolve"} {
		walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			fset := gotoken.NewFileSet()
			file, parseErr := goparser.ParseFile(fset, path, nil, 0)
			if parseErr != nil {
				t.Errorf("parsing %s: %v", path, parseErr)
				return nil
			}
			ast.Inspect(file, func(n ast.Node) bool {
				for _, a := range rawIDArgs(n) {
					t.Errorf("%s: %s reaches a stderr line raw. Wrap it in termSafe", fset.Position(a.Pos()), argName(a))
				}
				return true
			})
			return nil
		})
		if walkErr != nil {
			t.Fatal(walkErr)
		}
	}
}

func argName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	}
	return ""
}

// isStderrWriter recognises the writers a stderr line is written through:
// os.Stderr, cmd.ErrOrStderr(), and the stderr/log/w parameters the
// hand-written commands thread through for testability.
func isStderrWriter(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.SelectorExpr:
		id, ok := e.X.(*ast.Ident)
		return ok && id.Name == "os" && e.Sel.Name == "Stderr"
	case *ast.CallExpr:
		s, ok := e.Fun.(*ast.SelectorExpr)
		return ok && s.Sel.Name == "ErrOrStderr"
	case *ast.Ident:
		return e.Name == "stderr" || e.Name == "log" || e.Name == "w"
	}
	return false
}

func rawIDArgs(n ast.Node) []ast.Expr {
	call, ok := n.(*ast.CallExpr)
	if !ok || len(call.Args) < 2 {
		return nil
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Fprintf" {
		return nil
	}
	if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "fmt" || !isStderrWriter(call.Args[0]) {
		return nil
	}
	lit, ok := call.Args[1].(*ast.BasicLit)
	if !ok {
		return nil
	}
	format, err := strconv.Unquote(lit.Value)
	if err != nil {
		return nil
	}
	var hits []ast.Expr
	argi := 2
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		i++
		for i < len(format) && strings.IndexByte("+-# 0123456789.*", format[i]) >= 0 {
			if format[i] == '*' {
				argi++
			}
			i++
		}
		if i >= len(format) || format[i] == '%' {
			continue
		}
		if argi < len(call.Args) && (format[i] == 's' || format[i] == 'v') {
			if a := call.Args[argi]; strings.HasSuffix(strings.ToLower(argName(a)), "id") {
				hits = append(hits, a)
			}
		}
		argi++
	}
	return hits
}
