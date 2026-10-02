// Copyright 2026, Jamf Software LLC

package commands

import (
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

// TestSecretFileFlags_RefuseStdin pins the credential policy: a device secret
// is never read from stdin, so "-" is a usage error even with a secret piped.
func TestSecretFileFlags_RefuseStdin(t *testing.T) {
	for _, c := range secretFileCmds {
		t.Run(c.name, func(t *testing.T) {
			client, err := runSecretCmd(t, c.newCmd, ptr("Fake-Piped-3\n"), c.flag, "-")
			assertRefused(t, client, err, c.flag+" does not read stdin")
			if got := exitcode.CodeFrom(err); got != exitcode.Usage {
				t.Errorf("exit code = %d, want %d", got, exitcode.Usage)
			}
		})
	}
}
