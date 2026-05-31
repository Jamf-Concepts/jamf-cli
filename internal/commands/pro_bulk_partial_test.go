// Copyright 2026, Jamf Software LLC

package commands

import (
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

// partialSendMock dispatches BlankPush to two computers; device 2's command
// POST returns 500 so one succeeds and one fails.
func partialSendMock() *bulkMockClient {
	return &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/computergroups":                           {200, `{"computer_groups":[{"id":100,"name":"Lab Macs"}]}`},
			"GET /JSSResource/computergroups/id/100":                    {200, staticGroupDetailJSON},
			"GET /JSSResource/computers/id/1":                           {200, `{"computer":{"id":1,"name":"Mac-01"}}`},
			"GET /JSSResource/computers/id/2":                           {200, `{"computer":{"id":2,"name":"Mac-02"}}`},
			"POST /JSSResource/computercommands/command/BlankPush/id/1": {200, `<computer_command/>`},
			"POST /JSSResource/computercommands/command/BlankPush/id/2": {500, `boom`},
		},
	}
}

func TestSendCommand_PartialFailure_ExitCode7(t *testing.T) {
	cmd := newBulkCmd(newBulkCLIContext(partialSendMock()))
	_, _, err := runCobraCmd(t, cmd, "send-command", "--command", "BlankPush", "--group", "Lab Macs", "--yes")
	if err == nil {
		t.Fatal("expected a partial-failure error, got nil")
	}
	if got := exitcode.CodeFrom(err); got != exitcode.PartialFailure {
		t.Fatalf("exit code = %d, want PartialFailure(%d) (err=%v)", got, exitcode.PartialFailure, err)
	}
}

func TestSendCommand_PartialFailure_AllowOptOut(t *testing.T) {
	allowPartialFailure = true
	defer func() { allowPartialFailure = false }()

	cmd := newBulkCmd(newBulkCLIContext(partialSendMock()))
	_, _, err := runCobraCmd(t, cmd, "send-command", "--command", "BlankPush", "--group", "Lab Macs", "--yes")
	if err != nil {
		t.Fatalf("--allow-partial-failure should downgrade to exit 0, got err=%v", err)
	}
}

func TestFinishBatch_TotalFailurePropagatesCode(t *testing.T) {
	// All failed -> propagate the underlying error's code, not PartialFailure.
	err := finishBatch(nil, "ops", 0, 3, exitcode.New(exitcode.Authentication, "401"))
	if got := exitcode.CodeFrom(err); got != exitcode.Authentication {
		t.Fatalf("total failure code = %d, want Authentication(%d)", got, exitcode.Authentication)
	}
}
