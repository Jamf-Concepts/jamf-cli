// Copyright 2026, Jamf Software LLC

package aliased

import (
	render "github.com/Jamf-Concepts/jamf-cli/internal/output"
)

func printThings(rows []map[string]any) error {
	return render.New("json", false, false).Print(rows)
}
