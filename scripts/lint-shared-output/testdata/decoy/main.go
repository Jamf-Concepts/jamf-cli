// Copyright 2026, Jamf Software LLC

package decoy

import (
	"example.com/elsewhere/output"
)

func printThings(rows []map[string]any) error {
	return output.New().Print(rows)
}
