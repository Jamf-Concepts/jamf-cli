// Copyright 2026, Jamf Software LLC

package method

import (
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
)

type preview struct{}

type report struct{}

// Two methods share a name in one file. An exemption for one must not cover
// the other.
func (p *preview) render(rows []map[string]any) error {
	return output.New("table", false, false).Print(rows)
}

func (r report) render(rows []map[string]any) error {
	return output.New("json", false, false).Print(rows)
}
