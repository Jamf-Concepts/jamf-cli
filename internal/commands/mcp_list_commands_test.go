// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
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
	Command     string  `json:"command"`
	Destructive *bool   `json:"destructive"`
	Subcommands int     `json:"subcommands"`
	Flags       *string `json:"flags"`
	Truncated   bool    `json:"truncated"`
}

// decodeListCommands requires one valid JSON object on every line.
func decodeListCommands(t *testing.T, text string) []listCommandsRow {
	t.Helper()
	var rows []listCommandsRow
	for i, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		var row listCommandsRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("list_commands line %d is not a JSON object (%v): %q", i+1, err, line)
		}
		rows = append(rows, row)
	}
	return rows
}

func runListCommands(t *testing.T, in listCommandsInput) []listCommandsRow {
	t.Helper()
	t.Setenv(runAsCLIEnv, "1")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	res := listCommands(context.Background(), exe, "", in)
	text := mcpResultText(res)
	if res.IsError {
		t.Fatalf("list_commands %+v failed: %s", in, text)
	}
	if len(text) > maxListCommandsBytes {
		t.Errorf("list_commands %+v returned %d bytes, over the %d-byte ceiling", in, len(text), maxListCommandsBytes)
	}
	return decodeListCommands(t, text)
}

func TestListCommands_OpensTheTopLevelThenAResource(t *testing.T) {
	top := map[string]listCommandsRow{}
	for _, r := range runListCommands(t, listCommandsInput{}) {
		top[r.Command] = r
	}
	for _, product := range []string{"pro", "protect", "school", "security", "platform"} {
		if top[product].Subcommands == 0 {
			t.Errorf("the top level must list %q with a subcommand count, got %+v", product, top[product])
		}
	}

	rows := runListCommands(t, listCommandsInput{Prefix: "pro computers"})
	var list *listCommandsRow
	for i := range rows {
		if rows[i].Command == "pro computer-inventory list" {
			list = &rows[i]
		}
	}
	if list == nil {
		t.Fatalf("prefix \"pro computers\" must list pro computer-inventory list, got %+v", rows)
	}
	if list.Destructive == nil || list.Flags == nil {
		t.Errorf("a command row must carry destructive and flags, got %+v", *list)
	}
}

func TestListCommands_SearchesUnderAPrefix(t *testing.T) {
	rows := runListCommands(t, listCommandsInput{Prefix: "pro", Query: "delete policy"})
	var found bool
	for _, r := range rows {
		if !strings.HasPrefix(r.Command, "pro ") {
			t.Errorf("a search under prefix pro returned %q", r.Command)
		}
		if r.Command == "pro classic-policies delete" {
			found = true
			if r.Destructive == nil || !*r.Destructive {
				t.Errorf("pro classic-policies delete must be marked destructive, got %+v", r)
			}
		}
	}
	if !found {
		t.Errorf("query \"delete policy\" must find pro classic-policies delete, got %+v", rows)
	}
}

func TestListCommands_RefusesAnUnknownPrefix(t *testing.T) {
	t.Setenv(runAsCLIEnv, "1")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	res := listCommands(context.Background(), exe, "", listCommandsInput{Prefix: "pro no-such-resource"})
	if !res.IsError || !strings.Contains(mcpResultText(res), "no-such-resource") {
		t.Errorf("an unknown prefix must be an error naming it, got %+v: %s", res.IsError, mcpResultText(res))
	}
}

func TestListCommands_CutsAtALineAndSaysHowManyItOmitted(t *testing.T) {
	line := `{"command":"pro example","description":"` + strings.Repeat("x", 100) + `","destructive":false}` + "\n"
	child := writeFakeReportChild(t, strings.Repeat(line, 2000), "", 0)

	res := listCommands(context.Background(), child, "", listCommandsInput{Query: "example"})
	text := mcpResultText(res)
	if len(text) > maxListCommandsBytes {
		t.Errorf("a cut result is %d bytes, over the %d-byte ceiling", len(text), maxListCommandsBytes)
	}
	rows := decodeListCommands(t, text)
	last := rows[len(rows)-1]
	if !last.Truncated {
		t.Fatalf("a cut result must end with a truncated line, got %q", text[len(text)-200:])
	}
	if !strings.Contains(text, fmt.Sprintf(`"omitted":%d`, 2000-(len(rows)-1))) {
		t.Errorf("the truncated line must count the omitted rows, got %q", text[len(text)-200:])
	}
}

func TestListCommands_KeepsStderrOutOfTheCatalog(t *testing.T) {
	row := `{"command":"pro computers list","description":"List computers","destructive":false}` + "\n"
	hint := "hint: 1766 results returned. Narrow with --select=<fields>\n"

	ok := mcpResultText(listCommands(context.Background(), writeFakeReportChild(t, row, hint, 0), "", listCommandsInput{}))
	if ok != row {
		t.Errorf("a successful catalog must be the child's stdout alone, got:\n%s", ok)
	}

	res := listCommands(context.Background(), writeFakeReportChild(t, "", "Error: config unreadable\n", 1), "", listCommandsInput{})
	if !res.IsError {
		t.Fatal("a failed catalog child must be an error result")
	}
	if !strings.Contains(mcpResultText(res), "config unreadable") {
		t.Errorf("a failed catalog must carry the child's stderr, got:\n%s", mcpResultText(res))
	}
}

func TestListCommandsArgs_KeepModelTextAsFlagValues(t *testing.T) {
	args := listCommandsArgs(listCommandsInput{Prefix: "--profile other", Query: "-p prod"})
	for _, a := range args {
		if a == "--profile" || a == "-p" {
			t.Errorf("model text reached the child as its own argument: %q in %v", a, args)
		}
	}
	if _, err := buildChildArgs("prod", args); err != nil {
		t.Errorf("list_commands arguments must pass buildChildArgs, got %v", err)
	}
}

func TestListCommands_NamesTheEmptyResultByMode(t *testing.T) {
	empty := writeFakeReportChild(t, "", "", 0)

	browse := mcpResultText(listCommands(context.Background(), empty, "", listCommandsInput{Prefix: "pro"}))
	if !strings.Contains(browse, `"matches":0`) || strings.Contains(browse, "words") {
		t.Errorf("an empty browse must say nothing is listed and mention no words, got %q", browse)
	}
	search := mcpResultText(listCommands(context.Background(), empty, "", listCommandsInput{Query: "zzz"}))
	if !strings.Contains(search, `"matches":0`) || !strings.Contains(search, "words") {
		t.Errorf("an empty search must suggest other words, got %q", search)
	}
}

func TestListCommands_RefusesAQueryWithNoWords(t *testing.T) {
	t.Setenv(runAsCLIEnv, "1")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	res := listCommands(context.Background(), exe, "", listCommandsInput{Query: "-"})
	if !res.IsError || !strings.Contains(mcpResultText(res), "no letters or digits") {
		t.Errorf("a query with no words must be an error, got %v: %.300s", res.IsError, mcpResultText(res))
	}
}
