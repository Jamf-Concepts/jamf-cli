// Copyright 2026, Jamf Software LLC

package offender

import (
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
)

func printThings(rows []map[string]any) error {
	formatter := output.New("json", false, false)
	return formatter.Print(rows)
}
