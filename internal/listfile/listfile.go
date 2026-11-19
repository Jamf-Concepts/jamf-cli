// Copyright 2026, Jamf Software LLC

// Package listfile reads the files of IDs and names that bulk commands take
// through --list-file, one entry per line, and registers that flag.
//
// --list-file is the one flag that takes a list; --from-file is a request body
// and nothing else. The two used to share --from-file, so a reader could not
// tell from the name alone whether a command wanted a JSON body or a file of
// serial numbers. Commands that took a list through --from-file keep it as a
// hidden, deprecated alias (AddFlagWithLegacyAlias) until LegacyAliasRemoval.
package listfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// MaxLen is the longest entry, in characters, that Read accepts: the
// maxLength the Jamf Pro API spec declares for its name fields.
const MaxLen = 255

const (
	// FlagName is the flag every list-taking command registers.
	FlagName = "list-file"
	// LegacyFlagName is the name the list used to share with the request
	// body. It survives as a deprecated alias on the commands that took a
	// list through it, and nowhere else.
	LegacyFlagName = "from-file"
	// LegacyAliasRemoval is when the deprecated alias goes: six months after
	// the rename, the same window the command-name aliases get.
	LegacyAliasRemoval = "2027-05"
	// Stdin is the path that reads the list from standard input. Stdin is
	// never read unless asked for: a CI runner hands every process a
	// non-terminal stdin, so reading it whenever the flag is absent would turn
	// a single-ID call into an empty list.
	Stdin = "-"
)

// stdinOnce memoises standard input, which can be read only once while a
// command may read its list more than once (the package delete reads it to
// find file names, then the generated delete reads it again to delete).
var (
	stdinOnce sync.Once
	stdinData []byte
	stdinErr  error
	// stdinReader is os.Stdin, swappable in tests.
	stdinReader io.Reader = os.Stdin
)

func readSource(path string) ([]byte, error) {
	if path != Stdin {
		return os.ReadFile(path)
	}
	stdinOnce.Do(func() {
		stdinData, stdinErr = io.ReadAll(stdinReader)
	})
	return stdinData, stdinErr
}

// SourceName names path in a message: "stdin" for Stdin, else the path.
func SourceName(path string) string {
	if path == Stdin {
		return "stdin"
	}
	return path
}

// Option adjusts how AddFlag registers --list-file.
type Option func(*flagOpts)

type flagOpts struct {
	legacyAlias bool
	noStdin     bool
}

// WithLegacyAlias also registers --from-file as a hidden, deprecated alias
// bound to the same variable. Only a command that took its list through
// --from-file before the rename gets it; a command that gains a list now never
// had one to keep.
func WithLegacyAlias() Option { return func(o *flagOpts) { o.legacyAlias = true } }

// WithoutStdin leaves "- reads stdin" out of the usage, for a command whose
// stdin is already spoken for (pro setup prompts on it). The command refuses
// Stdin itself, through RefuseStdin.
func WithoutStdin() Option { return func(o *flagOpts) { o.noStdin = true } }

// AddFlag registers --list-file on cmd, bound to dst. what describes the
// entries, e.g. "computers to lock: ID, serial number, UDID, management ID or
// name". The usage is assembled here so the line format and the stdin form
// read the same on every command.
func AddFlag(cmd *cobra.Command, dst *string, what string, opts ...Option) {
	var o flagOpts
	for _, opt := range opts {
		opt(&o)
	}
	suffix := " (one per line, # comments ignored; - reads stdin)"
	if o.noStdin {
		suffix = " (one per line, # comments ignored)"
	}
	cmd.Flags().StringVar(dst, FlagName, "", "file listing "+what+suffix)
	if !o.legacyAlias {
		return
	}
	cmd.Flags().StringVar(dst, LegacyFlagName, "", "deprecated alias of --"+FlagName)
	_ = cmd.Flags().MarkDeprecated(LegacyFlagName,
		fmt.Sprintf("use --%s; --%s now means a request body only, and this alias is removed in %s", FlagName, LegacyFlagName, LegacyAliasRemoval))
	cmd.MarkFlagsMutuallyExclusive(FlagName, LegacyFlagName)
}

// RefuseStdin returns an error when path is Stdin, for a command registered
// WithoutStdin.
func RefuseStdin(path, why string) error {
	if path == Stdin {
		return exitcode.New(exitcode.Usage, fmt.Sprintf("--%s cannot read stdin here: %s", FlagName, why))
	}
	return nil
}

// Conflict is the refusal for --list-file given beside a positional or --name:
// a usage error (exit 2), like every other flag conflict.
func Conflict(msg string) error { return exitcode.New(exitcode.Usage, msg) }

// MarkExclusive marks --list-file, and its legacy alias where the command has
// one, mutually exclusive with each of others.
func MarkExclusive(cmd *cobra.Command, others ...string) {
	for _, name := range []string{FlagName, LegacyFlagName} {
		if !IsListFlag(cmd, name) {
			continue
		}
		for _, other := range others {
			cmd.MarkFlagsMutuallyExclusive(name, other)
		}
	}
}

// IsListFlag reports whether cmd's flag called name takes a list: --list-file
// always, and --from-file only where it is the deprecated alias.
func IsListFlag(cmd *cobra.Command, name string) bool {
	f := cmd.Flags().Lookup(name)
	if f == nil {
		return false
	}
	return name == FlagName || (name == LegacyFlagName && f.Deprecated != "")
}

// Changed reports whether the list was given, through either name.
func Changed(cmd *cobra.Command) bool {
	for _, name := range []string{FlagName, LegacyFlagName} {
		if IsListFlag(cmd, name) && cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}

// Entry is one ID or name from a list file, or one given on the command line.
type Entry struct {
	Value string
	// Line is the entry's 1-based line in its file, or 0 when it was not
	// read from a file.
	Line int
}

// Read returns the entries in the file at path, or on standard input when path
// is Stdin, skipping a leading byte order mark, blank lines and #-comments. It
// refuses the whole file when a line cannot be an ID or a name, and the
// refusal names only the line.
func Read(path string) ([]Entry, error) {
	data, err := readSource(path)
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for i, raw := range strings.Split(strings.TrimPrefix(string(data), "\ufeff"), "\n") {
		value := strings.TrimSpace(raw)
		if value == "" || strings.HasPrefix(value, "#") {
			continue
		}
		if reason := notAName(value); reason != "" {
			return nil, fmt.Errorf("%s line %d is not an ID or a name: %s", SourceName(path), i+1, reason)
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

// Target names the record the entry resolved to, as its quoted value and ID.
// A hidden entry is named by its line, with the ID only when it differs from
// the value: a numeric entry is its own ID until the server has answered.
func (e Entry) Target(id string) string {
	switch {
	case !e.Hidden():
		return fmt.Sprintf("%q (id: %s)", e.Value, id)
	case id == e.Value:
		return e.Label()
	default:
		return fmt.Sprintf("%s (id: %s)", e.Label(), id)
	}
}

// Wrap returns err after msg. When the entry is hidden it keeps only err's
// exit code and hint.
func (e Entry) Wrap(msg string, err error) error {
	if e.Hidden() {
		return Opaque(msg, err)
	}
	return fmt.Errorf("%s: %w", msg, err)
}

// Hide returns err, or for a hidden entry an error that keeps only err's exit
// code and hint.
func (e Entry) Hide(err error) error {
	if e.Hidden() {
		return Opaque("", err)
	}
	return err
}

// Opaque returns an error that has err's exit code and hint, and msg in place
// of err's text. A hint is the CLI's own remedy text and never quotes the
// request. A lookup that ends in a usage error is a name matching more than
// one record, and the message says so.
func Opaque(msg string, err error) error {
	code := exitcode.CodeFrom(err)
	reason := fmt.Sprintf("the request failed (%s)", exitcode.CodeName(code))
	if code == exitcode.Usage {
		reason = "it matches more than one record; give its numeric ID"
	}
	if msg != "" {
		reason = msg + ": " + reason
	}
	out := exitcode.New(code, reason)
	if e, ok := errors.AsType[*exitcode.Error](err); ok {
		out.Hint = e.Hint
	}
	return out
}
