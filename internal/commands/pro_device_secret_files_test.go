// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// mdmCommandClient answers the device lookup and records the MDM command body.
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
	case strings.Contains(path, "computers-inventory"):
		resp = `{"id":"7","udid":"u-7","general":{"name":"Mac","managementId":"bbbbbbbb-1111-2222-3333-444444444444"},"hardware":{"serialNumber":"C02X1234"}}`
	case strings.Contains(path, "mobile-devices"):
		resp = `{"id":"7","name":"iPad","managementId":"aaaaaaaa-1111-2222-3333-444444444444","udid":"u-7","serialNumber":"F4GH5678"}`
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(resp)), Header: make(http.Header)}, nil
}

func runDeviceSecretCmd(t *testing.T, newCmd func(*registry.CLIContext) *cobra.Command, args ...string) (*mdmCommandClient, error) {
	t.Helper()
	resetGlobals()
	client := &mdmCommandClient{}
	cmd := newCmd(&registry.CLIContext{Client: client, Output: mockOutput{}})
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

func TestSetRecoveryLock_ReadsThePasswordFromAFile(t *testing.T) {
	client, err := runDeviceSecretCmd(t, newComputerSetRecoveryLockCmd, "--new-password-file", secretFile(t, "Fake-Recovery 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := client.commandData["newPassword"]; got != "Fake-Recovery 1" {
		t.Errorf("newPassword = %q, want the file's content without its line ending", got)
	}
}

// TestSetRecoveryLock_ClearSendsAnEmptyPassword keeps the clear request
// equivalent to the old "omit --new-password": an empty newPassword.
func TestSetRecoveryLock_ClearSendsAnEmptyPassword(t *testing.T) {
	client, err := runDeviceSecretCmd(t, newComputerSetRecoveryLockCmd, "--clear")
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := client.commandData["newPassword"]; !ok || got != "" {
		t.Errorf("newPassword = %#v (present %v), want an empty string", got, ok)
	}
}

func TestSetRecoveryLock_RefusesWithoutAnExplicitSource(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no-input and no file", []string{"--no-input"}, "--new-password-file is required"},
		{"empty file", []string{"--new-password-file", secretFile(t, "\n")}, "empty"},
		{"file and clear", []string{"--clear", "--new-password-file", secretFile(t, "x")}, "none of the others can be"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetGlobals()
			client := &mdmCommandClient{}
			cmd := newComputerSetRecoveryLockCmd(&registry.CLIContext{Client: client, Output: mockOutput{}})
			cmd.PersistentFlags().BoolVar(&noInput, "no-input", false, "")
			cmd.SetArgs(append([]string{"--id", "7", "--yes"}, tc.args...))
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
			if client.posts != 0 {
				t.Errorf("sent %d MDM command(s) after refusing", client.posts)
			}
		})
	}
}

func TestMobileLock_ReadsThePinFromAFile(t *testing.T) {
	client, err := runDeviceSecretCmd(t, newMobileLockCmd, "--pin-file", secretFile(t, "515151\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := client.commandData["pin"]; got != "515151" {
		t.Errorf("pin = %q, want 515151", got)
	}
}

func TestMobileLock_SendsNoPinWithoutAFile(t *testing.T) {
	client, err := runDeviceSecretCmd(t, newMobileLockCmd)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := client.commandData["pin"]; ok {
		t.Errorf("commandData carries a pin nobody supplied: %v", client.commandData)
	}
}

func TestClearPasscode_ReadsTheUnlockTokenFromStdin(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })
	_, _ = w.WriteString("RkFLRS1VTkxPQ0s=\n")
	_ = w.Close()

	client, err := runDeviceSecretCmd(t, newMobileClearPasscodeCmd, "--unlock-token-file", "-")
	if err != nil {
		t.Fatal(err)
	}
	if got := client.commandData["unlockToken"]; got != "RkFLRS1VTkxPQ0s=" {
		t.Errorf("unlockToken = %q, want the piped token", got)
	}
}

// TestRemovedSecretFlagsPointAtTheirFileFlag checks each rename hint fires on
// the command that gained the file flag.
func TestRemovedSecretFlagsPointAtTheirFileFlag(t *testing.T) {
	root := NewRootCmd("test", "none", "none", "none")
	for _, tc := range []struct {
		path []string
		from string
	}{
		{[]string{"pro", "computer-inventory", "set-recovery-lock"}, "new-password"},
		{[]string{"pro", "mobile-devices", "lock"}, "pin"},
		{[]string{"pro", "mobile-devices", "clear-passcode"}, "unlock-token"},
	} {
		cmd, _, err := root.Find(tc.path)
		if err != nil || cmd.Name() != tc.path[len(tc.path)-1] {
			t.Fatalf("%v: found %q, err %v", tc.path, cmd.Name(), err)
		}
		var known []string
		cmd.Flags().VisitAll(func(f *pflagFlag) { known = append(known, f.Name) })
		if got, want := suggestFlag(tc.from, known), renamedFlags[tc.from]; got != want {
			t.Errorf("%v --%s: suggestFlag = %q, want %q", tc.path, tc.from, got, want)
		}
	}
}
