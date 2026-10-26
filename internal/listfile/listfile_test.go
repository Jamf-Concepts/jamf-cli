// Copyright 2026, Jamf Software LLC

package listfile

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "list.txt")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRead(t *testing.T) {
	cases := map[string]struct {
		content string
		want    []Entry
	}{
		"one entry per line": {"123\n456\n789\n", []Entry{{"123", 1}, {"456", 2}, {"789", 3}}},
		"comments and blank lines skipped, lines still counted": {
			"# comment\n\n123\n# another\n456\n\n", []Entry{{"123", 3}, {"456", 5}},
		},
		"CRLF line endings":    {"123\r\n456\r\n", []Entry{{"123", 1}, {"456", 2}}},
		"names keep spaces":    {"42\nMy Policy\n", []Entry{{"42", 1}, {"My Policy", 2}}},
		"surrounding space":    {"  Main Office \t\n", []Entry{{"Main Office", 1}}},
		"empty file":           {"", nil},
		"a 255-character name": {strings.Repeat("é", MaxLen), []Entry{{strings.Repeat("é", MaxLen), 1}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := Read(write(t, tc.content))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRead_MissingFile(t *testing.T) {
	if _, err := Read(filepath.Join(t.TempDir(), "no-such-file.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want os.ErrNotExist", err)
	}
}

func TestRead_RefusesALineThatCannotBeAName(t *testing.T) {
	const secret = "s3cr3t-Token"
	for name, tc := range map[string]struct{ line, reason string }{
		"control character": {"a\x1b]52;c;" + secret + "\x07", "control character"},
		"tab inside":        {"a\t" + secret, "control character"},
		"invalid UTF-8":     {"\xff" + secret, "UTF-8"},
		"too long":          {secret + strings.Repeat("x", MaxLen), "characters long"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Read(write(t, "ok\n# skipped\n"+tc.line+"\n"))
			if err == nil {
				t.Fatal("want the file refused")
			}
			if !strings.Contains(err.Error(), "line 3") || !strings.Contains(err.Error(), tc.reason) {
				t.Errorf("err = %v, want line 3 and %q", err, tc.reason)
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("err = %v, want the line's content left out", err)
			}
		})
	}
}

func TestRead_IgnoresAControlCharacterInAComment(t *testing.T) {
	got, err := Read(write(t, "# \x1b[2J\nok\n"))
	if err != nil || !slices.Equal(got, []Entry{{"ok", 2}}) {
		t.Errorf("got %v, %v; want [ok on line 2]", got, err)
	}
}

func TestEntryLabelAndWrap(t *testing.T) {
	file := Entry{Value: "s3cr3t", Line: 4}
	typed := Entry{Value: "s3cr3t"}
	lookupErr := exitcode.New(exitcode.PermissionDenied, "permission denied (HTTP 403): GET /v1/x?filter=s3cr3t")

	t.Run("at a terminal", func(t *testing.T) {
		if got := file.Label(); got != `"s3cr3t"` {
			t.Errorf("Label = %s, want the quoted value", got)
		}
		err := file.Wrap("resolving "+file.Label(), lookupErr)
		if !errors.Is(err, lookupErr) {
			t.Errorf("Wrap = %v, want lookupErr kept in the chain", err)
		}
	})

	t.Run("in an MCP child", func(t *testing.T) {
		t.Setenv(registry.MCPChildEnvVar, "1")
		if got := file.Label(); got != "line 4" {
			t.Errorf("Label = %s, want line 4", got)
		}
		if got := typed.Label(); got != `"s3cr3t"` {
			t.Errorf("Label of a typed value = %s, want it quoted", got)
		}
		err := file.Wrap("resolving "+file.Label(), lookupErr)
		if strings.Contains(err.Error(), "s3cr3t") || errors.Is(err, lookupErr) {
			t.Errorf("Wrap = %v, want neither the value nor lookupErr's text", err)
		}
		if got := exitcode.CodeFrom(err); got != exitcode.PermissionDenied {
			t.Errorf("exit code = %d, want %d", got, exitcode.PermissionDenied)
		}
		if want := "resolving line 4: the request failed (permission_denied)"; err.Error() != want {
			t.Errorf("Wrap = %q, want %q", err, want)
		}
	})
}

func TestOpaqueKeepsTheHintAndNamesAnAmbiguity(t *testing.T) {
	denied := exitcode.New(exitcode.PermissionDenied, "permission denied (HTTP 403): GET /v1/x?filter=s3cr3t").
		WithHint("grant the Read Buildings privilege")
	var e *exitcode.Error
	if !errors.As(Opaque("resolving line 2", denied), &e) || e.Hint != "grant the Read Buildings privilege" {
		t.Errorf("Opaque lost the hint: %+v", e)
	}

	ambiguous := exitcode.New(exitcode.Usage, `multiple resources found matching "s3cr3t" (IDs: 1, 2)`)
	got := Opaque("resolving line 2", ambiguous)
	if want := "resolving line 2: it matches more than one record; give its numeric ID"; got.Error() != want {
		t.Errorf("Opaque = %q, want %q", got, want)
	}
	if exitcode.CodeFrom(got) != exitcode.Usage {
		t.Errorf("exit code = %d, want Usage", exitcode.CodeFrom(got))
	}
}

func TestEntryTargetAndHide(t *testing.T) {
	numeric := Entry{Value: "8675309", Line: 3}
	named := Entry{Value: "Main Office", Line: 4}
	failure := exitcode.New(exitcode.NotFound, "resource not found (HTTP 404): DELETE /v1/buildings/8675309")

	if got := numeric.Target("8675309"); got != `"8675309" (id: 8675309)` {
		t.Errorf("Target at a terminal = %s", got)
	}
	if got := numeric.Hide(failure); got != error(failure) {
		t.Errorf("Hide at a terminal = %v, want err unchanged", got)
	}

	t.Setenv(registry.MCPChildEnvVar, "1")
	if got := numeric.Target("8675309"); got != "line 3" {
		t.Errorf("Target of a hidden numeric entry = %s, want line 3", got)
	}
	if got := named.Target("12"); got != "line 4 (id: 12)" {
		t.Errorf("Target of a hidden name = %s, want line 4 (id: 12)", got)
	}
	hidden := numeric.Hide(failure)
	if strings.Contains(hidden.Error(), "8675309") || exitcode.CodeFrom(hidden) != exitcode.NotFound {
		t.Errorf("Hide = %v (code %d), want no value and NotFound", hidden, exitcode.CodeFrom(hidden))
	}
}
