// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// unlockClient answers the device lookup, the unlock POST (with a Date header
// at postedAt) and the follow-up GET of the returned command, whose dateSent
// is dateSent. getErr fails that GET.
type unlockClient struct {
	postedAt time.Time
	dateSent string
	getErr   error

	posts    int
	userName string
}

func (c *unlockClient) Do(_ context.Context, method, path string, body io.Reader) (*http.Response, error) {
	header := make(http.Header)
	resp := `{}`
	switch {
	case method == http.MethodPost && strings.HasPrefix(path, mdmCommandsPath):
		c.posts++
		var sent struct {
			CommandData struct {
				CommandType string `json:"commandType"`
				UserName    string `json:"userName"`
			} `json:"commandData"`
		}
		b, _ := io.ReadAll(body)
		_ = json.Unmarshal(b, &sent)
		if sent.CommandData.CommandType != "UNLOCK_USER_ACCOUNT" {
			return nil, errors.New("unexpected commandType " + sent.CommandData.CommandType)
		}
		c.userName = sent.CommandData.UserName
		header.Set("Date", c.postedAt.UTC().Format(http.TimeFormat))
		resp = `[{"id":"cmd-1","href":"https://example.invalid/api/v1/mdm/commands?uuids=cmd-1"}]`
	case method == http.MethodGet && strings.HasPrefix(path, mdmCommandsPath):
		if c.getErr != nil {
			return nil, c.getErr
		}
		if !strings.Contains(path, "uuid%3D%3Dcmd-1") {
			return nil, errors.New("GET did not filter on the returned uuid: " + path)
		}
		resp = `{"totalCount":1,"results":[{"uuid":"cmd-1","commandType":"UNLOCK_USER_ACCOUNT","commandState":"PENDING","dateSent":"` + c.dateSent + `"}]}`
	case strings.Contains(path, "computers-inventory"):
		resp = `{"id":"7","udid":"u-7","general":{"name":"Mac","managementId":"bbbbbbbb-1111-2222-3333-444444444444"},"hardware":{"serialNumber":"C02X1234"}}`
	case strings.Contains(path, "mobile-devices"):
		resp = `{"id":"7","name":"iPad","managementId":"aaaaaaaa-1111-2222-3333-444444444444","udid":"u-7","serialNumber":"F4GH5678"}`
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(resp)), Header: header}, nil
}

var unlockCmds = []struct {
	name   string
	newCmd func(*registry.CLIContext) *cobra.Command
}{
	{"computer", newComputerUnlockUserAccountCmd},
	{"mobile device", newMobileUnlockUserAccountCmd},
}

func runUnlock(t *testing.T, client *unlockClient, newCmd func(*registry.CLIContext) *cobra.Command, args ...string) error {
	t.Helper()
	resetGlobals()
	cmd := newCmd(&registry.CLIContext{Client: client, Output: mockOutput{}})
	cmd.PersistentFlags().BoolVar(&noInput, "no-input", false, "")
	cmd.SetArgs(append([]string{"--id", "7"}, args...))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd.Execute()
}

func TestUnlockUserAccount_SendsTheUserName(t *testing.T) {
	for _, c := range unlockCmds {
		t.Run(c.name, func(t *testing.T) {
			now := time.Now()
			client := &unlockClient{postedAt: now, dateSent: now.UTC().Format(time.RFC3339Nano)}
			if err := runUnlock(t, client, c.newCmd, "--user-name", "jsmith"); err != nil {
				t.Fatal(err)
			}
			if client.posts != 1 || client.userName != "jsmith" {
				t.Errorf("posts = %d, userName = %q; want one POST naming jsmith", client.posts, client.userName)
			}
		})
	}
}

// Jamf Pro answers a second unlock for a device with the command already
// pending and queues nothing, whichever account the second one names.
func TestUnlockUserAccount_RefusesWhenAnUnlockIsAlreadyPending(t *testing.T) {
	for _, c := range unlockCmds {
		t.Run(c.name, func(t *testing.T) {
			now := time.Now()
			client := &unlockClient{postedAt: now, dateSent: now.Add(-time.Hour).UTC().Format(time.RFC3339Nano)}
			err := runUnlock(t, client, c.newCmd, "--user-name", "second-user")

			var ec *exitcode.Error
			if !errors.As(err, &ec) {
				t.Fatalf("err = %v, want an *exitcode.Error", err)
			}
			if !strings.Contains(ec.Message, "already has a pending unlock-user-account command (cmd-1") ||
				!strings.Contains(ec.Message, `"second-user"`) {
				t.Errorf("message = %q, want the pending UUID and the account that was not queued", ec.Message)
			}
			if !strings.Contains(ec.Hint, "flush-commands --status pending") {
				t.Errorf("hint = %q, want the flush-commands remedy", ec.Hint)
			}
		})
	}
}

// The Date header has one-second resolution, so a command created during the
// request can carry a dateSent a little after it. That is a new command.
func TestUnlockUserAccount_ToleratesTheDateHeadersResolution(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	client := &unlockClient{postedAt: now, dateSent: now.Add(-1500 * time.Millisecond).UTC().Format(time.RFC3339Nano)}
	if err := runUnlock(t, client, newComputerUnlockUserAccountCmd, "--user-name", "jsmith"); err != nil {
		t.Errorf("a command stamped within the skew was reported as pre-existing: %v", err)
	}
}

// A check that cannot be made must not turn a successful send into a failure.
func TestUnlockUserAccount_UnreadableCommandStillSucceeds(t *testing.T) {
	now := time.Now()
	client := &unlockClient{postedAt: now, getErr: errors.New("403 BAD_PERMISSIONS")}
	if err := runUnlock(t, client, newComputerUnlockUserAccountCmd, "--user-name", "jsmith"); err != nil {
		t.Errorf("err = %v, want success when the command cannot be read back", err)
	}
	if client.posts != 1 {
		t.Errorf("posts = %d, want 1", client.posts)
	}
}

func TestUnlockUserAccount_RefusesABlankUserName(t *testing.T) {
	for _, c := range unlockCmds {
		for _, name := range []string{"", "  "} {
			t.Run(c.name+" "+strings.ReplaceAll(name, " ", "_"), func(t *testing.T) {
				client := &unlockClient{postedAt: time.Now()}
				err := runUnlock(t, client, c.newCmd, "--user-name", name)
				var ec *exitcode.Error
				if !errors.As(err, &ec) || ec.Code != exitcode.Usage {
					t.Fatalf("err = %v, want a usage error", err)
				}
				if client.posts != 0 {
					t.Errorf("posts = %d, want none for a blank user name", client.posts)
				}
			})
		}
	}
}
