// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// mdmCommandClient answers the device and LAPS lookups and records the MDM
// command body.
type mdmCommandClient struct {
	commandData map[string]any
	posts       int
}

func (c *mdmCommandClient) Do(_ context.Context, method, path string, body io.Reader) (*http.Response, error) {
	resp := `{}`
	switch {
	case method == http.MethodPost && strings.Contains(path, "/mdm/commands"):
		c.posts++
		var sent struct {
			CommandData map[string]any `json:"commandData"`
		}
		b, _ := io.ReadAll(body)
		_ = json.Unmarshal(b, &sent)
		c.commandData = sent.CommandData
		resp = `[{"id":"cmd-1"}]`
	case strings.Contains(path, "local-admin-password"):
		resp = `{"results":[{"guid":"guid-mdm","username":"jamfadmin","userSource":"MDM"}]}`
	case strings.Contains(path, "computers-inventory"):
		resp = `{"id":"7","udid":"u-7","general":{"name":"Mac","managementId":"bbbbbbbb-1111-2222-3333-444444444444"},"hardware":{"serialNumber":"C02X1234"}}`
	case strings.Contains(path, "mobile-devices"):
		resp = `{"id":"7","name":"iPad","managementId":"aaaaaaaa-1111-2222-3333-444444444444","udid":"u-7","serialNumber":"F4GH5678"}`
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(resp)), Header: make(http.Header)}, nil
}

type secretFileCmd struct {
	name   string
	newCmd func(*registry.CLIContext) *cobra.Command
	flag   string
	field  string
}

var secretFileCmds = []secretFileCmd{
	{"set-recovery-lock", newComputerSetRecoveryLockCmd, "--new-password-file", "newPassword"},
	{"set-auto-admin-password", newComputerSetAutoAdminPasswordCmd, "--password-file", "password"},
	{"lock", newMobileLockCmd, "--pin-file", "pin"},
	{"clear-passcode", newMobileClearPasscodeCmd, "--unlock-token-file", "unlockToken"},
}

// runSecretCmd runs one device-secret command against --id 7 --yes. A non-nil
// stdin is piped to the command.
func runSecretCmd(t *testing.T, newCmd func(*registry.CLIContext) *cobra.Command, stdin *string, args ...string) (*mdmCommandClient, error) {
	t.Helper()
	resetGlobals()
	if stdin != nil {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		orig := os.Stdin
		os.Stdin = r
		t.Cleanup(func() { os.Stdin = orig; _ = r.Close() })
		_, _ = w.WriteString(*stdin)
		_ = w.Close()
	}
	client := &mdmCommandClient{}
	cmd := newCmd(&registry.CLIContext{Client: client, Output: mockOutput{}})
	cmd.PersistentFlags().BoolVar(&noInput, "no-input", false, "")
	cmd.SetArgs(append([]string{"--id", "7", "--yes"}, args...))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return client, cmd.Execute()
}

func secretFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func ptr(s string) *string { return &s }

func TestSecretFileFlags_SendTheFileContent(t *testing.T) {
	for _, c := range secretFileCmds {
		t.Run(c.name+" named file", func(t *testing.T) {
			client, err := runSecretCmd(t, c.newCmd, nil, c.flag, secretFile(t, "Fake Secret 1\r\n"))
			if err != nil {
				t.Fatal(err)
			}
			if got := client.commandData[c.field]; got != "Fake Secret 1" {
				t.Errorf("%s = %q, want the file's content without its line ending", c.field, got)
			}
		})
	}
}

func TestSecretFileFlags_RefuseAnEmptySource(t *testing.T) {
	for _, c := range secretFileCmds {
		for _, content := range []string{"", "\n"} {
			t.Run(c.name+" named file "+strings.ReplaceAll(content, "\n", `\n`), func(t *testing.T) {
				client, err := runSecretCmd(t, c.newCmd, nil, c.flag, secretFile(t, content))
				assertRefused(t, client, err, c.flag+" names an empty file")
			})
		}
	}
}

// TestSecretFileFlags_MissingFileNamesNoPath pins that the error leaves the
// path out: a secret typed where its path belongs would otherwise be echoed.
func TestSecretFileFlags_MissingFileNamesNoPath(t *testing.T) {
	for _, c := range secretFileCmds {
		t.Run(c.name, func(t *testing.T) {
			missing := filepath.Join(t.TempDir(), "FAKE-515151")
			client, err := runSecretCmd(t, c.newCmd, nil, c.flag, missing)
			assertRefused(t, client, err, "reading "+c.flag+": ")
			if !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("want a not-exist error, got %v", err)
			}
			if strings.Contains(err.Error(), "FAKE-515151") {
				t.Errorf("the path reached the error: %v", err)
			}
		})
	}
}

func assertRefused(t *testing.T, client *mdmCommandClient, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("err = %v, want it to contain %q", err, want)
	}
	if client.posts != 0 {
		t.Errorf("sent %d MDM command(s) after refusing", client.posts)
	}
}

// TestSetRecoveryLock_ClearSendsAnEmptyPassword keeps the clear request
// equivalent to the old "omit --new-password": an empty newPassword.
func TestSetRecoveryLock_ClearSendsAnEmptyPassword(t *testing.T) {
	client, err := runSecretCmd(t, newComputerSetRecoveryLockCmd, nil, "--clear")
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := client.commandData["newPassword"]; !ok || got != "" {
		t.Errorf("newPassword = %#v (present %v), want an empty string", got, ok)
	}
}

func TestSecretFileFlags_RefuseUnderNoInputWithoutAFile(t *testing.T) {
	for _, c := range secretFileCmds[:2] {
		t.Run(c.name, func(t *testing.T) {
			client, err := runSecretCmd(t, c.newCmd, nil, "--no-input")
			assertRefused(t, client, err, c.flag+" is required when --no-input is set")
		})
	}
}

func TestSetRecoveryLock_RefusesAFileWithClear(t *testing.T) {
	client, err := runSecretCmd(t, newComputerSetRecoveryLockCmd, nil, "--clear", "--new-password-file", secretFile(t, "x"))
	assertRefused(t, client, err, "none of the others can be")
}

func TestClearPasscode_RequiresTheTokenFile(t *testing.T) {
	client, err := runSecretCmd(t, newMobileClearPasscodeCmd, nil)
	assertRefused(t, client, err, `required flag(s) "unlock-token-file" not set`)
}

func TestMobileLock_SendsNoPinWithoutAFile(t *testing.T) {
	client, err := runSecretCmd(t, newMobileLockCmd, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := client.commandData["pin"]; ok {
		t.Errorf("commandData carries a pin nobody supplied: %v", client.commandData)
	}
}

// TestRemovedSecretFlagsPointAtTheirFileFlag checks each rename hint fires on
// the command that gained the file flag.
func TestRemovedSecretFlagsPointAtTheirFileFlag(t *testing.T) {
	root := NewRootCmd("test", "none", "none", "none")
	for _, tc := range []struct {
		path     []string
		from, to string
	}{
		{[]string{"pro", "computer-inventory", "set-recovery-lock"}, "new-password", "new-password-file"},
		{[]string{"pro", "mobile-devices", "lock"}, "pin", "pin-file"},
		{[]string{"pro", "mobile-devices", "clear-passcode"}, "unlock-token", "unlock-token-file"},
	} {
		cmd, _, err := root.Find(tc.path)
		if err != nil || cmd.Name() != tc.path[len(tc.path)-1] {
			t.Fatalf("%v: found %q, err %v", tc.path, cmd.Name(), err)
		}
		var known []string
		cmd.Flags().VisitAll(func(f *pflagFlag) { known = append(known, f.Name) })
		if got := suggestFlag(tc.from, known); got != tc.to {
			t.Errorf("%v --%s: suggestFlag = %q, want %q", tc.path, tc.from, got, tc.to)
		}
	}
}

// TestStrayPositionalOnASecretFileCommandIsRedacted covers the typo the
// file flags invite: the secret given as a positional instead of a path.
func TestStrayPositionalOnASecretFileCommandIsRedacted(t *testing.T) {
	root := NewRootCmd("test", "none", "none", "none")
	for _, path := range [][]string{
		{"pro", "computer-inventory", "set-recovery-lock"},
		{"pro", "computer-inventory", "set-auto-admin-password"},
		{"pro", "mobile-devices", "lock"},
		{"pro", "mobile-devices", "clear-passcode"},
	} {
		cmd, _, err := root.Find(path)
		if err != nil || cmd.Name() != path[len(path)-1] {
			t.Fatalf("%v: found %q, err %v", path, cmd.Name(), err)
		}
		err = cmd.Args(cmd, []string{"S3cur3P@ss123"})
		if err == nil || strings.Contains(err.Error(), "S3cur3P@ss123") || !strings.Contains(err.Error(), "<redacted>") {
			t.Errorf("%v: want the stray positional refused and redacted, got %v", path, err)
		}
	}
	cmd, _, _ := root.Find([]string{"pro", "computer-inventory", "restart"})
	if err := cmd.Args(cmd, []string{"junkarg"}); err == nil || !strings.Contains(err.Error(), "junkarg") {
		t.Errorf("a command with no secret flag must still name the typo: %v", err)
	}
}

// TestStrayPositionalOnEraseIsRedacted covers erase, whose Find My PIN goes in
// a body with no flag of its own, so no flag name marks the command.
func TestStrayPositionalOnEraseIsRedacted(t *testing.T) {
	root := NewRootCmd("test", "none", "none", "none")
	for _, path := range [][]string{
		{"pro", "computer-inventory", "erase"},
		{"pro", "mobile-devices", "erase"},
	} {
		cmd, _, err := root.Find(path)
		if err != nil || cmd.Name() != path[len(path)-1] {
			t.Fatalf("%v: found %q, err %v", path, cmd.Name(), err)
		}
		err = cmd.Args(cmd, []string{"515151"})
		if err == nil || strings.Contains(err.Error(), "515151") || !strings.Contains(err.Error(), "<redacted>") {
			t.Errorf("%v: want the stray positional refused and redacted, got %v", path, err)
		}
	}
}
