// Copyright 2026, Jamf Software LLC

package twofiles

import (
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
)

func printThings(rows []map[string]any) error {
	return output.New("json", false, false).Print(rows)
}
