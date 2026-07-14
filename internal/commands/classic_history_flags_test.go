// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/commands/pro/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// pathCaptureClient records the request path of the last Do call and returns a
// canned Classic XML body, so tests can assert how a command assembles its URL
// without a live Jamf Pro instance.
type pathCaptureClient struct {
	lastMethod string
	lastPath   string
}

func (c *pathCaptureClient) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	c.lastMethod = method
	c.lastPath = path
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader("<computer_history/>")),
		Header:     make(http.Header),
	}, nil
}

func newCaptureCtx(c *pathCaptureClient) *registry.CLIContext {
	formatter := output.New("json", true, false)
	formatter.SetWriter(&bytes.Buffer{})
	return &registry.CLIContext{Client: c, Output: &cliOutput{formatter}}
}

// TestClassicComputerHistoryGet_PathComposition locks the runtime behavior of
// the generated --serial alias and --subset flag: that --serial resolves to the
// same /serialnumber/ path as --serialnumber, and that --subset appends a
// /subset/<value> segment after the resolved identifier. Without this the path
// logic is only exercised against a live instance.
func TestClassicComputerHistoryGet_PathComposition(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "id with subset",
			args: []string{"get", "42", "--subset", "Commands"},
			want: "/JSSResource/computerhistory/id/42/subset/Commands",
		},
		{
			name: "serialnumber with subset",
			args: []string{"get", "--serialnumber", "ABC123", "--subset", "General"},
			want: "/JSSResource/computerhistory/serialnumber/ABC123/subset/General",
		},
		{
			name: "serial alias resolves identically to serialnumber",
			args: []string{"get", "--serial", "ABC123", "--subset", "General"},
			want: "/JSSResource/computerhistory/serialnumber/ABC123/subset/General",
		},
		{
			name: "no subset omits the subset segment",
			args: []string{"get", "--serial", "ABC123"},
			want: "/JSSResource/computerhistory/serialnumber/ABC123",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &pathCaptureClient{}
			cmd := generated.NewClassicComputerHistoryCmd(newCaptureCtx(client))
			if _, _, err := runCobraCmd(t, cmd, tc.args...); err != nil {
				t.Fatalf("command error: %v", err)
			}
			if client.lastMethod != "GET" {
				t.Errorf("method = %q, want GET", client.lastMethod)
			}
			if client.lastPath != tc.want {
				t.Errorf("path = %q, want %q", client.lastPath, tc.want)
			}
		})
	}
}
