package commands

import (
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// secretValueSegments name a flag whose value is a device or account secret,
// matched as a whole hyphen-delimited segment or as its suffix (newpassword,
// apikey). A "-file" suffix takes a path, not the secret, and is excluded.
var secretValueSegments = []string{
	"password", "passwd", "passcode", "passphrase", "pin", "otp",
	"token", "secret", "credential", "credentials", "key",
}

// notASecretSegment are segments a suffix match catches that name no secret.
var notASecretSegment = map[string]bool{"monkey": true, "hotkey": true}

func flagTakesSecretOnArgv(f *pflag.Flag) bool {
	if f.Value.Type() == "bool" || strings.HasSuffix(f.Name, "-file") {
		return false
	}
	for _, seg := range strings.Split(f.Name, "-") {
		if notASecretSegment[seg] {
			continue
		}
		for _, w := range secretValueSegments {
			if strings.HasSuffix(seg, w) {
				return true
			}
		}
	}
	return false
}

func TestFlagTakesSecretOnArgv_Matcher(t *testing.T) {
	for name, want := range map[string]bool{
		"new-password": true, "passwd": true, "otp": true, "api-key": true, "apikey": true,
		"credentials": true, "client-credential": true, "pin": true,
		"monkey": false, "hotkey": false, "keychain": false, "mapping": false, "pin-file": false,
		"token-file": false, "keys": false,
	} {
		f := &pflag.Flag{Name: name, Value: newStringValue()}
		if got := flagTakesSecretOnArgv(f); got != want {
			t.Errorf("flagTakesSecretOnArgv(--%s) = %v, want %v", name, got, want)
		}
	}
}

func newStringValue() pflag.Value {
	fs := pflag.NewFlagSet("probe", pflag.ContinueOnError)
	fs.String("probe", "", "")
	return fs.Lookup("probe").Value
}

// namedLikeASecret are flags the matcher catches whose value is not a secret.
// A stale entry fails the walk, so the list cannot hide a real one.
var namedLikeASecret = map[string]string{
	"jamf-cli pro setup --credentials": "picks where setup reads the client credentials from (a fixed set of source names), not a credential",
}

func TestNoFlagTakesASecretOnArgv(t *testing.T) {
	root := NewRootCmd("test", "none", "none", "none")
	var offenders []string
	exempted := map[string]bool{}
	walked := 0
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		walked++
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if !flagTakesSecretOnArgv(f) {
				return
			}
			key := c.CommandPath() + " --" + f.Name
			if _, ok := namedLikeASecret[key]; ok {
				exempted[key] = true
				return
			}
			offenders = append(offenders, key)
		})
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk(root)
	if walked < 1000 {
		t.Fatalf("walked only %d commands; the assembled tree is incomplete", walked)
	}
	for key, reason := range namedLikeASecret {
		if !exempted[key] {
			t.Errorf("exemption %q (%s) matches no flag any more; remove it", key, reason)
		}
	}
	sort.Strings(offenders)
	for _, o := range offenders {
		t.Errorf("%s takes a secret as a flag value (argv, ps, shell history, CI logs); read it from a --*-file path or an interactive prompt", o)
	}
}
