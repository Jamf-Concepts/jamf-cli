// Copyright 2026, Jamf Software LLC

package funcval

import (
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
)

// Taking the constructor as a value puts the call one hop away from the
// selector, so a rule matching only a call expression would miss it.
func printThings(rows []map[string]any) error {
	build := output.New
	return build("json", false, false).Print(rows)
}
