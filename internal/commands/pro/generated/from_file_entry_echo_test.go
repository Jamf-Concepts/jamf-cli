// Copyright 2026, Jamf Software LLC

package generated

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

const fileSecret = "s3cr3t-Token_0123456789abcdef"

func writeDeleteList(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "delete.txt")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// pathEchoClient fails every request with the request path in the message,
// as the real client's 404 and 504 errors do.
type pathEchoClient struct{ calls int }

func (c *pathEchoClient) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	c.calls++
	return nil, fmt.Errorf("resource not found (HTTP 404): %s %s", method, path)
}

// emptyListClient answers every request with an empty Jamf Pro API list.
type emptyListClient struct{}

func (emptyListClient) Do(context.Context, string, string, io.Reader) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte(`{"totalCount":0,"results":[]}`)))}, nil
}

func runDelete(t *testing.T, newCmd func(*registry.CLIContext) *cobra.Command, client registry.HTTPClient, list string) (string, error) {
	t.Helper()
	cmd := newCmd(&registry.CLIContext{Client: client})
	cmd.SetArgs([]string{"--from-file", list, "--yes"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	var err error
	r, w, perr := os.Pipe()
	if perr != nil {
		t.Fatal(perr)
	}
	orig := os.Stderr
	os.Stderr = w
	err = cmd.Execute()
	os.Stderr = orig
	_ = w.Close()
	stderr, _ := io.ReadAll(r)
	return string(stderr), err
}

func TestFromFileDelete_MCPChildNamesTheLineNotTheEntry(t *testing.T) {
	cases := map[string]struct {
		newCmd func(*registry.CLIContext) *cobra.Command
		client registry.HTTPClient
	}{
		"pro, no match":            {newBuildingsDeleteCmd, emptyListClient{}},
		"pro, lookup fails":        {newBuildingsDeleteCmd, &pathEchoClient{}},
		"classic, lookup fails":    {newClassicAdvancedComputerSearchesDeleteCmd, &pathEchoClient{}},
		"classic, by-field lookup": {newClassicMobileDevicesDeleteCmd, &pathEchoClient{}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv(registry.MCPChildEnvVar, "1")
			stderr, err := runDelete(t, tc.newCmd, tc.client, writeDeleteList(t, "# devices\n"+fileSecret+"\n"))
			if err == nil {
				t.Fatal("want an error for an entry that does not resolve")
			}
			if strings.Contains(err.Error(), "s3cr3t") || strings.Contains(stderr, "s3cr3t") {
				t.Errorf("a file line reached the MCP child's output:\nerr: %v\nstderr: %s", err, stderr)
			}
			if !strings.Contains(err.Error(), "line 2") {
				t.Errorf("err = %v, want it to name line 2", err)
			}
		})
	}
}

func TestFromFileDelete_OutsideMCPQuotesTheEntry(t *testing.T) {
	_, err := runDelete(t, newBuildingsDeleteCmd, emptyListClient{}, writeDeleteList(t, fileSecret+"\n"))
	if err == nil || !strings.Contains(err.Error(), strconv.Quote(fileSecret)) {
		t.Errorf("err = %v, want the entry quoted for a person at the terminal", err)
	}
}

func TestFromFileDelete_RefusesANonNameLineBeforeAnyRequest(t *testing.T) {
	for name, bad := range map[string]string{
		"control character": "bad\x1b]52;c;" + fileSecret + "\x07",
		"too long":          fileSecret + strings.Repeat("x", 256),
		"invalid UTF-8":     "bad\xff" + fileSecret,
	} {
		t.Run(name, func(t *testing.T) {
			client := &pathEchoClient{}
			_, err := runDelete(t, newBuildingsDeleteCmd, client, writeDeleteList(t, "Main Office\n"+bad+"\n"))
			if err == nil {
				t.Fatal("want the file refused")
			}
			if client.calls != 0 {
				t.Errorf("sent %d requests, want none before the file is refused", client.calls)
			}
			if !strings.Contains(err.Error(), "line 2") {
				t.Errorf("err = %v, want it to name line 2", err)
			}
			if strings.Contains(err.Error(), "s3cr3t") {
				t.Errorf("err = %v, want the line's content left out", err)
			}
		})
	}
}
