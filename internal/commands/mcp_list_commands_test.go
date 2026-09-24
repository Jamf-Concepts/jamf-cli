// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

// runAsCLIEnv makes this test binary run the jamf-cli command tree instead of
// its tests, so os.Executable() can stand in for jamf-cli as an MCP child.
const runAsCLIEnv = "JAMF_CLI_TEST_RUN_AS_CLI"

func TestMain(m *testing.M) {
	if os.Getenv(runAsCLIEnv) == "1" {
		root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
		root.SetArgs(os.Args[1:])
		if err := root.Execute(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type listCommandsRow struct {
	Command     string `json:"command"`
	Destructive *bool  `json:"destructive"`
}

// decodeListCommands accepts one JSON array or a stream of JSON objects, and
// fails on anything that is not valid JSON.
func decodeListCommands(t *testing.T, text string) []listCommandsRow {
	t.Helper()
	var rows []listCommandsRow
	dec := json.NewDecoder(strings.NewReader(text))
	for {
		var raw json.RawMessage
		err := dec.Decode(&raw)
		if errors.Is(err, io.EOF) {
			return rows
		}
		if err != nil {
			tail := text[max(0, len(text)-200):]
			t.Fatalf("list_commands returned %d bytes that are not valid JSON (%v); it ends with:\n%s", len(text), err, tail)
		}
		if raw[0] == '[' {
			var batch []listCommandsRow
			if err := json.Unmarshal(raw, &batch); err != nil {
				t.Fatalf("list_commands array does not decode as catalog rows: %v", err)
			}
			rows = append(rows, batch...)
			continue
		}
		var row listCommandsRow
		if err := json.Unmarshal(raw, &row); err != nil {
			t.Fatalf("list_commands value %s does not decode as a catalog row: %v", raw, err)
		}
		rows = append(rows, row)
	}
}

// TestListCommands_ReturnsTheWholeCatalogAsValidJSON drives the list_commands
// handler against the real command tree. The catalog is fixed per build, so a
// catalog that outgrows the tool-result ceiling fails here, not at runtime.
func TestListCommands_ReturnsTheWholeCatalogAsValidJSON(t *testing.T) {
	t.Setenv(runAsCLIEnv, "1")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	res := listCommands(context.Background(), exe, "")
	text := mcpResultText(res)
	if res.IsError {
		t.Fatalf("list_commands failed: %s", text)
	}
	if len(text) > maxChildOutputBytes {
		t.Errorf("list_commands returned %d bytes, over the %d-byte ceiling for one tool result; "+
			"narrow the projection the handler requests", len(text), maxChildOutputBytes)
	}

	rows := decodeListCommands(t, text)
	got := make(map[string]listCommandsRow, len(rows))
	for _, r := range rows {
		got[r.Command] = r
	}

	want := collectCommands(NewRootCmd("test", "abc123", "2024-01-01", "unknown"), "", "", "")
	var missing []string
	for _, e := range want {
		r, ok := got[e.Command]
		if !ok {
			missing = append(missing, e.Command)
			continue
		}
		if r.Destructive == nil || *r.Destructive != e.Destructive {
			t.Errorf("%q: destructive must be %v in list_commands, got %v", e.Command, e.Destructive, r.Destructive)
		}
	}
	if len(missing) > 0 {
		t.Errorf("list_commands carries %d of the %d catalog commands; missing %d, first: %v",
			len(want)-len(missing), len(want), len(missing), missing[:min(5, len(missing))])
	}
	if len(rows) != len(want) {
		t.Errorf("list_commands returned %d rows for a catalog of %d commands", len(rows), len(want))
	}
}
