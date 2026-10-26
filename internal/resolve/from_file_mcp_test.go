// Copyright 2026, Jamf Software LLC

package resolve

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

const fileSecret = "s3cr3t-Token_0123456789abcdef"

func writeEntryFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "entries.txt")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

type pathEchoClient struct{ calls int }

func (c *pathEchoClient) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	c.calls++
	return nil, fmt.Errorf("resource not found (HTTP 404): %s %s", method, path)
}

func TestResolveComputersFromFile_MCPChildNamesTheLineNotTheEntry(t *testing.T) {
	t.Setenv(registry.MCPChildEnvVar, "1")
	path := writeEntryFile(t, fileSecret+"\n"+fileSecret+"*\n")
	client := &mockClient{responses: map[string]mockResponse{
		"computers-inventory": {200, `{"totalCount":0,"results":[]}`},
	}}

	var err error
	stderr := captureStderr(t, func() {
		_, _, err = ResolveComputersFromFile(context.Background(), client, path)
	})
	if err == nil {
		t.Fatal("want an error when no entry resolves")
	}
	if strings.Contains(stderr, "s3cr3t") || strings.Contains(err.Error(), "s3cr3t") {
		t.Errorf("a file line reached the MCP child's output:\nstderr: %s\nerr: %v", stderr, err)
	}
	for _, want := range []string{"line 1", "line 2"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to name %s", stderr, want)
		}
	}
}

func TestResolveComputersFromFile_OutsideMCPQuotesTheEntry(t *testing.T) {
	path := writeEntryFile(t, fileSecret+"\n")
	client := &mockClient{responses: map[string]mockResponse{
		"computers-inventory": {200, `{"totalCount":0,"results":[]}`},
	}}

	stderr := captureStderr(t, func() {
		_, _, _ = ResolveComputersFromFile(context.Background(), client, path)
	})
	if !strings.Contains(stderr, strconv.Quote(fileSecret)) {
		t.Errorf("stderr = %q, want the entry quoted for a person at the terminal", stderr)
	}
}

func TestResolveComputersFromFile_MCPChildLookupFailureOmitsTheRequest(t *testing.T) {
	t.Setenv(registry.MCPChildEnvVar, "1")
	path := writeEntryFile(t, fileSecret+"\n")

	_, _, err := ResolveComputersFromFile(context.Background(), &pathEchoClient{}, path)
	if err == nil {
		t.Fatal("want the lookup failure returned")
	}
	if strings.Contains(err.Error(), "s3cr3t") {
		t.Errorf("err = %v, want no file line in an MCP child's error", err)
	}
}

func TestResolveComputersFromFile_RefusesANonNameLineBeforeAnyRequest(t *testing.T) {
	for name, bad := range map[string]string{
		"control character": "bad\x1b]52;c;" + fileSecret + "\x07",
		"too long":          fileSecret + strings.Repeat("x", 256),
		"invalid UTF-8":     "bad\xff" + fileSecret,
	} {
		t.Run(name, func(t *testing.T) {
			path := writeEntryFile(t, "C02OK\n"+bad+"\n")
			client := &pathEchoClient{}

			_, _, err := ResolveComputersFromFile(context.Background(), client, path)
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
