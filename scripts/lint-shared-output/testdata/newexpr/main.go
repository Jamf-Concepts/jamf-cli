// Copyright 2026, Jamf Software LLC

package newexpr

import (
	"os"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
)

// new(T) builds the same working second formatter as &T{} and names no
// constructor, so a rule reading only composite literals and selectors misses
// it.
func printThings(rows []map[string]any) error {
	f := new(output.Formatter)
	f.SetWriter(os.Stdout)
	return f.Print(rows)
}

// new of some other type is not this rule's business.
func scratch() *[]byte {
	return new([]byte)
}
