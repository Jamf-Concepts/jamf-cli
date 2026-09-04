// Copyright 2026, Jamf Software LLC

package twofiles

import (
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
)

// Same function name as a.go's, in the same package. An exemption naming one
// file must not cover the other.
func printThings(rows []map[string]any) error {
	return output.New("table", false, false).Print(rows)
}
