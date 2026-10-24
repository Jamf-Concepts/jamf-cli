// Copyright 2026, Jamf Software LLC

package output

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// SanitizeTerminal renders every C0 control character except tab, DEL, every
// C1 control character, and every byte that is not valid UTF-8 as a visible
// \xNN, so a value a server or a device user supplied cannot move the cursor,
// retitle the window, clear the screen or write the clipboard. Quotes and
// every printable rune are left alone, so a name keeps its shape.
//
// It is applied to values, never to a stream: the CLI's own colour and
// spinner sequences are written around sanitized values and must survive.
func SanitizeTerminal(s string) string {
	return sanitize(s, false)
}

// SanitizeTerminalLines is SanitizeTerminal for text whose line breaks are
// intended, such as an error with its hint. It keeps \n, and \r only directly
// before \n; any other \r could return to the start of a line and overwrite it.
func SanitizeTerminalLines(s string) string {
	return sanitize(s, true)
}

func sanitize(s string, keepNewline bool) string {
	if !needsSanitizing(s, keepNewline) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, `\x%02x`, s[i])
		case keepNewline && isCRLF(s, i):
			b.WriteByte('\r')
		case isTerminalControl(r, keepNewline):
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

func needsSanitizing(s string, keepNewline bool) bool {
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if isTerminalControl(rune(c), keepNewline) && (!keepNewline || !isCRLF(s, i)) {
				return true
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if (r == utf8.RuneError && size == 1) || isTerminalControl(r, keepNewline) {
			return true
		}
		i += size
	}
	return false
}

// isCRLF reports whether s[i] is a \r that ends a line, which moves nothing a
// line feed does not.
func isCRLF(s string, i int) bool {
	return s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n'
}

func isTerminalControl(r rune, keepNewline bool) bool {
	switch {
	case r == '\t':
		return false
	case r == '\n':
		return !keepNewline
	case r < 0x20, r == 0x7f:
		return true
	case r >= 0x80 && r <= 0x9f:
		return true
	}
	return false
}

// neutralizeCSVFormula prefixes ' to a cell a spreadsheet would evaluate as a
// formula (CWE-1236). A cell that parses as a number is left alone, so a
// negative number is still a number.
func neutralizeCSVFormula(cell string) string {
	if cell == "" {
		return cell
	}
	switch cell[0] {
	case '=', '+', '-', '@', '\t', '\r':
	default:
		return cell
	}
	if _, err := strconv.ParseFloat(cell, 64); err == nil {
		return cell
	}
	return "'" + cell
}

// escapeJSONControls rewrites DEL, the C1 controls and any byte that is not
// valid UTF-8 in an encoded JSON document as a \u escape. Valid JSON carries
// such a character only inside a string, where the escape means the same
// character, so the document stays valid and decodes to the same value.
func escapeJSONControls(data []byte) []byte {
	clean := true
	for i := 0; i < len(data); {
		r, size := utf8.DecodeRune(data[i:])
		if r == 0x7f || (r >= 0x80 && r <= 0x9f) || (r == utf8.RuneError && size == 1) {
			clean = false
			break
		}
		i += size
	}
	if clean {
		return data
	}
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); {
		r, size := utf8.DecodeRune(data[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			out = append(out, `\ufffd`...)
		case r == 0x7f || (r >= 0x80 && r <= 0x9f):
			out = fmt.Appendf(out, `\u%04x`, r)
		default:
			out = append(out, data[i:i+size]...)
		}
		i += size
	}
	return out
}
