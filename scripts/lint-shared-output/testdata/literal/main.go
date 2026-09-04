// Copyright 2026, Jamf Software LLC

package literal

import (
	"os"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
)

// Every field of output.Formatter is unexported, but the type and its setters
// are not, so this builds a working second formatter without naming New.
func printThings(rows []map[string]any) error {
	f := &output.Formatter{}
	f.SetWriter(os.Stdout)
	return f.Print(rows)
}

// A reference to the type that constructs nothing must stay clean.
func passItAlong(f *output.Formatter) *output.Formatter {
	return f
}
