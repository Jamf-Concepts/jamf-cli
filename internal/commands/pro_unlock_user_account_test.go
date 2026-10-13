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
	noDate   bool   // the POST answers without a Date header
	getBody  string // replaces the GET's body when set

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
		if !c.noDate {
			header.Set("Date", c.postedAt.UTC().Format(http.TimeFormat))
		}
		resp = `[{"id":"cmd-1","href":"https://example.invalid/api/v1/mdm/commands?uuids=cmd-1"}]`
	case method == http.MethodGet && strings.HasPrefix(path, mdmCommandsPath):
		if c.getErr != nil {
			return nil, c.getErr
		}
		if !strings.Contains(path, "uuid%3D%3Dcmd-1") {
			return nil, errors.New("GET did not filter on the returned uuid: " + path)
		}
		resp = `{"totalCount":1,"results":[{"uuid":"cmd-1","commandType":"UNLOCK_USER_ACCOUNT","commandState":"PENDING","dateSent":"` + c.dateSent + `"}]}`
		if c.getBody != "" {
			resp = c.getBody
		}
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

// recordingOutput keeps what the command prints, which mockOutput discards.
type recordingOutput struct {
	mockOutput
	raw []byte
}

func (o *recordingOutput) PrintRaw(b []byte) error { o.raw = b; return nil }

type unlockResult struct {
	err    error
	stdout string // what the command printed through the formatter
	stderr string
}

func runUnlockCapture(t *testing.T, client *unlockClient, newCmd func(*registry.CLIContext) *cobra.Command, args ...string) unlockResult {
	t.Helper()
	resetGlobals()
	out := &recordingOutput{}
	cmd := newCmd(&registry.CLIContext{Client: client, Output: out})
	cmd.PersistentFlags().BoolVar(&noInput, "no-input", false, "")
	cmd.SetArgs(append([]string{"--id", "7"}, args...))
	var stderr strings.Builder
	cmd.SetOut(io.Discard)
	cmd.SetErr(&stderr)
	err := cmd.Execute()
	return unlockResult{err: err, stdout: string(out.raw), stderr: stderr.String()}
}

func runUnlock(t *testing.T, client *unlockClient, newCmd func(*registry.CLIContext) *cobra.Command, args ...string) error {
	t.Helper()
	return runUnlockCapture(t, client, newCmd, args...).err
}

func TestUnlockUserAccount_SendsTheUserName(t *testing.T) {
	for _, c := range unlockCmds {
		t.Run(c.name, func(t *testing.T) {
			now := time.Now()
			client := &unlockClient{postedAt: now, dateSent: now.UTC().Format(time.RFC3339Nano)}
			res := runUnlockCapture(t, client, c.newCmd, "--user-name", "jsmith")
			if res.err != nil {
				t.Fatal(res.err)
			}
			if client.posts != 1 || client.userName != "jsmith" {
				t.Errorf("posts = %d, userName = %q; want one POST naming jsmith", client.posts, client.userName)
			}
			if !strings.Contains(res.stdout, "cmd-1") {
				t.Errorf("stdout = %q, want the queued command's UUID", res.stdout)
			}
			if res.stderr != "" {
				t.Errorf("stderr = %q, want none when the check ran and passed", res.stderr)
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
			res := runUnlockCapture(t, client, c.newCmd, "--user-name", "second-user")

			var ec *exitcode.Error
			if !errors.As(res.err, &ec) {
				t.Fatalf("err = %v, want an *exitcode.Error", res.err)
			}
			if ec.Code != exitcode.General {
				t.Errorf("code = %d, want exitcode.General (%d)", ec.Code, exitcode.General)
			}
			if !strings.Contains(ec.Message, "already has a pending unlock-user-account command (cmd-1") ||
				!strings.Contains(ec.Message, `It may name an account other than "second-user"`) {
				t.Errorf("message = %q, want the pending UUID and a hedge about which account it names", ec.Message)
			}
			if strings.Contains(ec.Message, "instead of queuing") {
				t.Errorf("message = %q, must not claim the pending command is for another account", ec.Message)
			}
			noun := "computers"
			if c.name == "mobile device" {
				noun = "mobile-devices"
			}
			for _, want := range []string{
				"If it was sent for this account, wait",
				"jamf-cli pro " + noun + " flush-commands --id 7 --status pending",
				"clears the whole pending queue",
			} {
				if !strings.Contains(ec.Hint, want) {
					t.Errorf("hint = %q, want it to contain %q", ec.Hint, want)
				}
			}
			if res.stdout != "" {
				t.Errorf("stdout = %q, want nothing printed for a refusal", res.stdout)
			}
		})
	}
}

// A command stamped more than the skew before the request was already there.
// Any skew of 3 s or more must fail this.
func TestUnlockUserAccount_SkewIsNarrow(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	client := &unlockClient{postedAt: now, dateSent: now.Add(-3 * time.Second).UTC().Format(time.RFC3339Nano)}
	var ec *exitcode.Error
	if err := runUnlock(t, client, newComputerUnlockUserAccountCmd, "--user-name", "jsmith"); !errors.As(err, &ec) {
		t.Errorf("err = %v, want a refusal for a command stamped 3s before the request", err)
	}
}

// The Date header has one-second resolution, so a command created during the
// request can carry a dateSent a little after it. That is a new command.
func TestUnlockUserAccount_ToleratesTheDateHeadersResolution(t *testing.T) {
	for _, c := range unlockCmds {
		t.Run(c.name, func(t *testing.T) {
			now := time.Now().Truncate(time.Second)
			client := &unlockClient{postedAt: now, dateSent: now.Add(-1500 * time.Millisecond).UTC().Format(time.RFC3339Nano)}
			if err := runUnlock(t, client, c.newCmd, "--user-name", "jsmith"); err != nil {
				t.Errorf("a command stamped within the skew was reported as pre-existing: %v", err)
			}
		})
	}
}

// A check that cannot be made must not turn a successful send into a failure,
// and must not pass for a check that succeeded: it warns on stderr.
func TestUnlockUserAccount_UnverifiableCheckWarnsAndStillSucceeds(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name   string
		client func() *unlockClient
		want   string
	}{
		{"read-back refused (403)", func() *unlockClient { return &unlockClient{postedAt: now, getErr: errors.New("403 BAD_PERMISSIONS")} }, "403 BAD_PERMISSIONS"},
		{"no Date header", func() *unlockClient { return &unlockClient{noDate: true} }, "Date header"},
		{"undecodable read-back", func() *unlockClient { return &unlockClient{postedAt: now, getBody: "<html>"} }, "could not be decoded"},
		{"read-back finds nothing", func() *unlockClient {
			return &unlockClient{postedAt: now, getBody: `{"totalCount":0,"results":[]}`}
		}, "read back 0 results"},
		{"unparseable dateSent", func() *unlockClient { return &unlockClient{postedAt: now, dateSent: "yesterday"} }, "dateSent"},
	}
	for _, c := range unlockCmds {
		for _, tc := range cases {
			t.Run(c.name+" "+tc.name, func(t *testing.T) {
				client := tc.client()
				res := runUnlockCapture(t, client, c.newCmd, "--user-name", "jsmith")
				if res.err != nil {
					t.Errorf("err = %v, want success when the check cannot be made", res.err)
				}
				if client.posts != 1 {
					t.Errorf("posts = %d, want 1", client.posts)
				}
				if !strings.Contains(res.stderr, "warning: unlock sent, but the pending-command check was skipped") ||
					!strings.Contains(res.stderr, tc.want) {
					t.Errorf("stderr = %q, want a skipped-check warning mentioning %q", res.stderr, tc.want)
				}
				if !strings.Contains(res.stdout, "cmd-1") {
					t.Errorf("stdout = %q, want the queued command's UUID still printed", res.stdout)
				}
			})
		}
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
