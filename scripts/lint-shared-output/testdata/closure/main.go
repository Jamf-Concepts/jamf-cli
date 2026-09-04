// Copyright 2026, Jamf Software LLC

package closure

import (
	"os"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
)

// An exemption states why one function cannot print through the shared
// formatter. That says nothing about a callback written inside it, so a
// construction in a closure is its own site and carries its own name.
func buildFormatter() *output.Formatter {
	sneak := func() *output.Formatter { return output.New("json", false, false) }
	alsoSneak := func() *output.Formatter {
		f := &output.Formatter{}
		f.SetWriter(os.Stdout)
		return f
	}
	_ = alsoSneak
	return sneak()
}
