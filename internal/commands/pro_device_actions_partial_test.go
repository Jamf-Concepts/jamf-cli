// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/resolve"
)

// runActionOver runs a bulk device action whose execSingle fails for the IDs
// in failing, against devices resolved from a file with unresolved entries
// that never matched.
func runActionOver(t *testing.T, ids []string, failing map[string]bool, unresolved int) error {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetOut(&bytes.Buffer{})
	devices := make([]*resolve.DeviceIdentifiers, len(ids))
	for i, id := range ids {
		devices[i] = &resolve.DeviceIdentifiers{ID: id, SerialNumber: "S" + id}
	}
	dt := &deviceTarget{fromFile: "devices.txt"}
	return executeAction(cmd, dt, devices, unresolved, true, false, deviceActionConfig{
		actionName: "restart",
		deviceType: "computer",
		execSingle: func(d *resolve.DeviceIdentifiers, _ io.Reader) error {
			if failing[d.ID] {
				return exitcode.New(exitcode.PermissionDenied, "denied")
			}
			return nil
		},
	})
}

func TestDeviceAction_PartialFailureExitsSeven(t *testing.T) {
	t.Cleanup(func() { allowPartialFailure = false })

	err := runActionOver(t, []string{"1", "2", "3"}, map[string]bool{"2": true}, 0)
	if exitcode.CodeFrom(err) != exitcode.PartialFailure {
		t.Errorf("one of three failed: err = %v (code %d), want exit 7", err, exitcode.CodeFrom(err))
	}

	// An entry that never resolved is a failure too.
	err = runActionOver(t, []string{"1", "2"}, nil, 1)
	if exitcode.CodeFrom(err) != exitcode.PartialFailure {
		t.Errorf("all sent, one unresolved: err = %v, want exit 7", err)
	}
	err = runActionOver(t, []string{"1"}, nil, 2)
	if exitcode.CodeFrom(err) != exitcode.PartialFailure {
		t.Errorf("one sent, two unresolved: err = %v, want exit 7", err)
	}

	allowPartialFailure = true
	if err := runActionOver(t, []string{"1", "2", "3"}, map[string]bool{"2": true}, 1); err != nil {
		t.Errorf("--allow-partial-failure: err = %v, want nil", err)
	}
}

// A total failure keeps the code of the error that caused it.
func TestDeviceAction_TotalFailurePropagates(t *testing.T) {
	err := runActionOver(t, []string{"1", "2"}, map[string]bool{"1": true, "2": true}, 0)
	var ee *exitcode.Error
	if !errors.As(err, &ee) || ee.Code != exitcode.PermissionDenied {
		t.Errorf("err = %v (code %d), want the permission-denied code propagated", err, exitcode.CodeFrom(err))
	}
	if err := runActionOver(t, []string{"1", "2"}, nil, 0); err != nil {
		t.Errorf("every device succeeded: err = %v, want nil", err)
	}
}
