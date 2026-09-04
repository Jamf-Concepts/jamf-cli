// Copyright 2026, Jamf Software LLC

package testfile

import (
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
)

// A test builds a formatter to assemble a CLIContext, and no global output flag
// is set in a unit test, so the walk skips a _test.go file outright.
func printThings(rows []map[string]any) error {
	return output.New("json", false, false).Print(rows)
}
