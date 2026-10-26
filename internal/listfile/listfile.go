// Copyright 2026, Jamf Software LLC

// Package listfile reads the files of IDs and names that bulk commands take
// through --from-file, one entry per line.
package listfile

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// MaxLen is the longest entry, in characters, that Read accepts: the
// maxLength the Jamf Pro API spec declares for its name fields.
const MaxLen = 255

// Entry is one ID or name from a list file, or one given on the command line.
type Entry struct {
	Value string
	// Line is the entry's 1-based line in its file, or 0 when it was not
	// read from a file.
	Line int
}

// Read returns the entries in the file at path, skipping blank lines and
// #-comments. It refuses the whole file when a line cannot be an ID or a name,
// and the refusal names only the line.
func Read(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for i, raw := range strings.Split(string(data), "\n") {
		value := strings.TrimSpace(raw)
		if value == "" || strings.HasPrefix(value, "#") {
			continue
		}
		if reason := notAName(value); reason != "" {
			return nil, fmt.Errorf("%s line %d is not an ID or a name: %s", path, i+1, reason)
		}
		entries = append(entries, Entry{Value: value, Line: i + 1})
	}
	return entries, nil
}

func notAName(value string) string {
	if !utf8.ValidString(value) {
		return "it is not valid UTF-8"
	}
	if strings.ContainsFunc(value, unicode.IsControl) {
		return "it holds a control character"
	}
	if n := utf8.RuneCountInString(value); n > MaxLen {
		return fmt.Sprintf("it is %d characters long, over the %d a Jamf name can be", n, MaxLen)
	}
	return ""
}

// Hidden reports whether messages must leave the entry's value out: true for
// an entry read from a file in an MCP child.
func (e Entry) Hidden() bool {
	return e.Line > 0 && registry.InMCPChild()
}

// Label names the entry in a message: its quoted value, or its line when the
// value is hidden.
func (e Entry) Label() string {
	if e.Hidden() {
		return fmt.Sprintf("line %d", e.Line)
	}
	return strconv.Quote(e.Value)
}

// Wrap returns err after msg. When the entry is hidden it keeps only err's
// exit code.
func (e Entry) Wrap(msg string, err error) error {
	if e.Hidden() {
		return Opaque(msg, err)
	}
	return fmt.Errorf("%s: %w", msg, err)
}

// Opaque returns an error that has err's exit code and msg, and none of err's
// text.
func Opaque(msg string, err error) error {
	code := exitcode.CodeFrom(err)
	return exitcode.New(code, fmt.Sprintf("%s: the lookup failed (%s)", msg, exitcode.CodeName(code)))
}
