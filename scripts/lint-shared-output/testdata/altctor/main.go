// Copyright 2026, Jamf Software LLC

package altctor

import (
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
)

// A second constructor in internal/output is still a second formatter. The
// package exports one today, so this fixture names one it does not: an exact
// match on New would report the tree clean the day another is added.
func printThings(rows []map[string]any) error {
	return output.NewPlain("json").Print(rows)
}
