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

// pathCaptureClient records every request path and returns a canned Classic
// record whose <general><id> is 42, so tests can assert how a command assembles
// its URL — including the id-resolution round-trip — without a live instance.
type pathCaptureClient struct {
	calls []string // "METHOD /path" in order
}

func (c *pathCaptureClient) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	c.calls = append(c.calls, method+" "+path)
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader("<computer_history><general><id>42</id></general></computer_history>")),
		Header:     make(http.Header),
	}, nil
}

func newCaptureCtx(c *pathCaptureClient) *registry.CLIContext {
	formatter := output.New("json", true, false)
	formatter.SetWriter(&bytes.Buffer{})
	return &registry.CLIContext{Client: c, Output: &cliOutput{formatter}}
}

// TestClassicComputerHistoryGet_PathComposition locks the runtime behavior of
// the generated --serial alias and --subset flag:
//   - --serial resolves to the same /serialnumber/ path as --serialnumber;
//   - --subset with an id appends /subset/<v> directly (one call);
//   - --subset with a non-id lookup resolves to an id first, then requests
//     id/{id}/subset/<v> (two calls) — the Platform Gateway 403s the direct
//     non-id + /subset/ form, so the request must never be sent that way.
func TestClassicComputerHistoryGet_PathComposition(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantCalls []string
	}{
		{
			name:      "id with subset appends directly, no resolve",
			args:      []string{"get", "42", "--subset", "Commands"},
			wantCalls: []string{"GET /JSSResource/computerhistory/id/42/subset/Commands"},
		},
		{
			name: "serialnumber with subset resolves to id first",
			args: []string{"get", "--serialnumber", "ABC123", "--subset", "General"},
			wantCalls: []string{
				"GET /JSSResource/computerhistory/serialnumber/ABC123",
				"GET /JSSResource/computerhistory/id/42/subset/General",
			},
		},
		{
			name: "serial alias behaves identically to serialnumber",
			args: []string{"get", "--serial", "ABC123", "--subset", "General"},
			wantCalls: []string{
				"GET /JSSResource/computerhistory/serialnumber/ABC123",
				"GET /JSSResource/computerhistory/id/42/subset/General",
			},
		},
		{
			name:      "serial without subset is a single direct lookup",
			args:      []string{"get", "--serial", "ABC123"},
			wantCalls: []string{"GET /JSSResource/computerhistory/serialnumber/ABC123"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &pathCaptureClient{}
			cmd := generated.NewClassicComputerHistoryCmd(newCaptureCtx(client))
			if _, _, err := runCobraCmd(t, cmd, tc.args...); err != nil {
				t.Fatalf("command error: %v", err)
			}
			if len(client.calls) != len(tc.wantCalls) {
				t.Fatalf("calls = %v, want %v", client.calls, tc.wantCalls)
			}
			for i, want := range tc.wantCalls {
				if client.calls[i] != want {
					t.Errorf("call[%d] = %q, want %q", i, client.calls[i], want)
				}
			}
		})
	}
}
