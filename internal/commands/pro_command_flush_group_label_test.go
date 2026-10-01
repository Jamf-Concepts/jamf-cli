// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// The prompt without --yes names the group the flush would hit by name and id,
// so the operator confirms the group that was resolved, not the one typed.
func TestFlushCommands_GroupPromptNamesTheResolvedID(t *testing.T) {
	cases := []struct {
		name   string
		list   map[string]flushMockResponse
		build  func(*registry.CLIContext) *cobra.Command
		group  string
		wantID string
	}{
		{"computer", map[string]flushMockResponse{"GET /JSSResource/computergroups": {200, `<computer_groups><size>1</size><computer_group><id>7</id><name>Lab Macs</name></computer_group></computer_groups>`}}, newComputerFlushCommandsCmd, "lab macs", `"Lab Macs" (id: 7)`},
		{"mobile", map[string]flushMockResponse{"GET /JSSResource/mobiledevicegroups": {200, `<mobile_device_groups><size>1</size><mobile_device_group><id>12</id><name>Lab iPads</name></mobile_device_group></mobile_device_groups>`}}, newMobileFlushCommandsCmd, "Lab iPads", `"Lab iPads" (id: 12)`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetGlobals()
			mock := &flushMockClient{responses: tc.list}
			cmd := tc.build(&registry.CLIContext{Client: mock, Output: &discardOutput{}})
			cmd.Flags().Bool("no-input", false, "")
			var stderr bytes.Buffer
			cmd.SetErr(&stderr)
			cmd.SetArgs([]string{"--group", tc.group, "--status", "failed"})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(mock.deletedPaths) != 0 {
				t.Errorf("flushed without --yes: %v", mock.deletedPaths)
			}
			if !strings.Contains(stderr.String(), tc.wantID) {
				t.Errorf("prompt %q does not name the resolved group %s", stderr.String(), tc.wantID)
			}
		})
	}
}
