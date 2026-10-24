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
// intended, such as an error with its hint. It keeps \n and escapes \r, which
// could otherwise return to the start of a line and overwrite it.
func SanitizeTerminalLines(s string) string {
	return sanitize(s, true)
}

func sanitize(s string, keepNewline bool) string {
	if !needsSanitizing(s, keepNewline) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, `\x%02x`, s[i])
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
			if isTerminalControl(rune(c), keepNewline) {
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
