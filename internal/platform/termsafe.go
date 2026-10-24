// Copyright 2026, Jamf Software LLC

package platform

import "github.com/Jamf-Concepts/jamf-cli/internal/output"

// TermSafe renders a server-supplied id for a stderr line a human reads, with
// its control characters made visible. Generated commands call it.
func TermSafe(s string) string {
	return output.SanitizeTerminal(s)
}
