// Copyright 2026, Jamf Software LLC

package exempt

import (
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
)

func buildFormatter() *output.Formatter {
	return output.New("json", false, false)
}
