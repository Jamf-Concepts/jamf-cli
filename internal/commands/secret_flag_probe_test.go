// Copyright 2026, Jamf Software LLC

package commands

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// carriesASecretFlag reports whether cmd registers a string flag whose name
// names a credential. Tests use it to check that a probe leaf carries none.
func carriesASecretFlag(cmd *cobra.Command) bool {
	found := false
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if found || f.Value.Type() != "string" || strings.HasSuffix(f.Name, "-file") {
			return
		}
		for _, seg := range strings.Split(f.Name, "-") {
			if secretFlagSegments[seg] {
				found = true
				return
			}
		}
	})
	return found
}
