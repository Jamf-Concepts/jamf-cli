// Copyright 2026, Jamf Software LLC

package parser

import (
	"regexp"
	"strings"
)

var htmlBreak = regexp.MustCompile(`(?i)<\s*/?\s*br\s*/?\s*>`)

// FlagUsage turns a spec's parameter description into a flag usage string.
//
// Two things in the specs' markup break `--help`. An HTML line break (`<br>`,
// `<br/>`, `</br>`) is printed literally. And pflag reads the first
// backquoted word of a usage string as the flag's value placeholder
// (pflag.UnquoteUsage): a filter description listing "`name`, `description`"
// rendered as `--filter name`, naming a placeholder that is not what the flag
// takes. Breaks become spaces, backquotes become single quotes, and runs of
// whitespace collapse, since a usage string is one line in the flag table.
func FlagUsage(desc string) string {
	s := htmlBreak.ReplaceAllString(desc, " ")
	s = strings.ReplaceAll(s, "`", "'")
	return strings.Join(strings.Fields(s), " ")
}
