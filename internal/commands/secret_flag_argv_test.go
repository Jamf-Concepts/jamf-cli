package commands

import (
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// secretValueSegments name a flag whose value is a device or account secret.
// A "-file" suffix takes a path, not the secret, and is excluded.
var secretValueSegments = []string{"password", "passcode", "passphrase", "pin", "token", "secret", "credential"}

func flagTakesSecretOnArgv(f *pflag.Flag) bool {
	if f.Value.Type() == "bool" || strings.HasSuffix(f.Name, "-file") {
		return false
	}
	for _, seg := range strings.Split(f.Name, "-") {
		// "key" only as a whole segment: as a suffix it matches monkey and hotkey.
		if seg == "key" {
			return true
		}
		for _, w := range secretValueSegments {
			if seg == w || strings.HasSuffix(seg, w) {
				return true
			}
		}
	}
	return false
}

func TestNoFlagTakesASecretOnArgv(t *testing.T) {
	root := NewRootCmd("test", "none", "none", "none")
	var offenders []string
	walked := 0
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		walked++
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if flagTakesSecretOnArgv(f) {
				offenders = append(offenders, c.CommandPath()+" --"+f.Name)
			}
		})
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk(root)
	if walked < 1000 {
		t.Fatalf("walked only %d commands; the assembled tree is incomplete", walked)
	}
	sort.Strings(offenders)
	for _, o := range offenders {
		t.Errorf("%s takes a secret as a flag value (argv, ps, shell history, CI logs); read it from a --*-file path or an interactive prompt", o)
	}
}
