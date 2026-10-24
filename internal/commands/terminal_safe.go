// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
)

// termSafe renders v for a stderr line a human reads: an id, a name or an
// error a server supplied is printed with its control characters made visible.
func termSafe(v any) string {
	return output.SanitizeTerminal(fmt.Sprint(v))
}
