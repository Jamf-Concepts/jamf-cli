// Copyright 2026, Jamf Software LLC

package parser

import (
	"errors"
	"go/ast"
	goparser "go/parser"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"text/template"
)

func TestValidateResourceNamesRefusesEverySpecDerivedName(t *testing.T) {
	valid := func() *Resource {
		return &Resource{
			Name:       "widgets",
			Parent:     "gadgets",
			Operations: []*Operation{{Name: "get", Path: "/v1/widgets/{id}"}},
		}
	}
	if err := ValidateResourceNames(valid()); err != nil {
		t.Fatalf("baseline resource refused: %v", err)
	}
	cases := map[string]struct {
		mutate func(*Resource)
		want   string
	}{
		"resource name":        {func(r *Resource) { r.Name = `w"+x+"` }, "resource name"},
		"parent resource name": {func(r *Resource) { r.Parent = "Gadgets" }, "parent resource name"},
		"operation name":       {func(r *Resource) { r.Operations[0].Name = "get widget" }, "operation name"},
		"newline in path":      {func(r *Resource) { r.Operations[0].Path = "/v1/widgets\nfunc init() {}" }, "control character"},
		"carriage return in path": {
			func(r *Resource) { r.Operations[0].Path = "/v1/widgets\r/{id}" }, "control character",
		},
		"newline in bulk action path": {
			func(r *Resource) { r.Operations[0].BulkActionPath = "/v1/widgets\n/retry" }, "control character",
		},
		"newline in update token path": {
			func(r *Resource) { r.UpdateTokenOp = &Operation{Name: "upload-token", Path: "/v1/widgets/{id}\n"} }, "control character",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := valid()
			tc.mutate(r)
			err := ValidateResourceNames(r)
			if err == nil {
				t.Fatal("ValidateResourceNames accepted the resource")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestWriteGoSourceWritesNothingWhenOutputIsNotGo(t *testing.T) {
	invalid := template.Must(template.New("bad").Parse("package x\n\nfunc {{ . }}"))

	t.Run("absent file stays absent", func(t *testing.T) {
		outPath := filepath.Join(t.TempDir(), "bad.go")
		err := WriteGoSource(outPath, invalid, "(")
		if err == nil || !strings.Contains(err.Error(), "not valid Go") {
			t.Fatalf("WriteGoSource error = %v, want one saying the output is not valid Go", err)
		}
		if _, err := os.Stat(outPath); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("WriteGoSource left %s behind: %v", outPath, err)
		}
	})

	t.Run("existing file is untouched", func(t *testing.T) {
		outPath := filepath.Join(t.TempDir(), "bad.go")
		committed := []byte("package x\n\nfunc Committed() {}\n")
		if err := os.WriteFile(outPath, committed, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := WriteGoSource(outPath, invalid, "("); err == nil {
			t.Fatal("WriteGoSource accepted invalid Go")
		}
		got, err := os.ReadFile(outPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(committed) {
			t.Errorf("WriteGoSource rewrote the existing file:\n%s", got)
		}
	})
}

func TestGoRawStringFallsBackWhenARawLiteralCannotHoldTheValue(t *testing.T) {
	cases := map[string]struct {
		in      string
		wantRaw bool
	}{
		"plain":           {"Manage widgets\nin Jamf Pro.", true},
		"backquote":       {"a ` b", false},
		"carriage return": {"a\r\nb", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := GoRawString(tc.in)
			if isRaw := strings.HasPrefix(got, "`"); isRaw != tc.wantRaw {
				t.Errorf("GoRawString(%q) = %s, raw = %v, want raw = %v", tc.in, got, isRaw, tc.wantRaw)
			}
			assertGoStringLiteralOf(t, got, tc.in)
		})
	}
}

func TestDefaultValRefusesADefaultOfTheWrongType(t *testing.T) {
	refused := map[string]struct {
		typ string
		val any
	}{
		"string default on a boolean":  {"boolean", "true"},
		"number default on a boolean":  {"boolean", float64(1)},
		"non-integral integer default": {"integer", 1.5},
		"string default on an integer": {"integer", `func() int { panic("pwned") }()`},
	}
	for name, tc := range refused {
		t.Run(name, func(t *testing.T) {
			got, err := defaultVal(tc.typ, tc.val)
			if err == nil {
				t.Fatalf("defaultVal(%q, %#v) = %s; want a refusal", tc.typ, tc.val, got)
			}
			if !strings.Contains(err.Error(), "does not match the declared "+tc.typ+" type") {
				t.Errorf("error %q does not name the type mismatch", err)
			}
		})
	}

	t.Run("hostile string default stays a string literal", func(t *testing.T) {
		hostile := `x" + func() string { panic("pwned") }() + "`
		got, err := defaultVal("string", hostile)
		if err != nil {
			t.Fatal(err)
		}
		assertGoStringLiteralOf(t, got, hostile)
	})
}

func assertGoStringLiteralOf(t *testing.T, src, want string) {
	t.Helper()
	expr, err := goparser.ParseExpr(src)
	if err != nil {
		t.Fatalf("%s does not parse as Go: %v", src, err)
	}
	lit, ok := expr.(*ast.BasicLit)
	if !ok {
		t.Fatalf("%s parses as a %T, not a single string literal", src, expr)
	}
	got, err := strconv.Unquote(lit.Value)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("literal %s holds %q, want %q", src, got, want)
	}
}
