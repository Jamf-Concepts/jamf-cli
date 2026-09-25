// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// renderCatalogNDJSON renders entries the way `commands -o ndjson --select
// <fields>` prints them: one compact JSON object per row, projected to fields.
func renderCatalogNDJSON(t *testing.T, entries []commandEntry, fields string) string {
	t.Helper()
	keep := strings.Split(fields, ",")
	var b strings.Builder
	for _, row := range commandEntriesToMaps(entries, true) {
		projected := map[string]any{}
		for _, k := range keep {
			if v, ok := row[k]; ok {
				projected[k] = v
			}
		}
		line, err := json.Marshal(projected)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// TestQueryCatalog_ChildrenReachEveryCommandOnceUnderTheCeiling walks the tree
// the way list_commands does, one --children level at a time from the top.
func TestQueryCatalog_ChildrenReachEveryCommandOnceUnderTheCeiling(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	full := map[string]commandEntry{}
	for _, e := range collectCommands(root, "", "", "") {
		full[e.Command] = e
	}

	reached := map[string]int{}
	queue := []string{""}
	var levels, largest int
	for len(queue) > 0 {
		prefix := queue[0]
		queue = queue[1:]
		levels++

		entries, err := queryCatalog(root, catalogQuery{Prefix: prefix, Children: true})
		if err != nil {
			t.Fatalf("prefix %q: %v", prefix, err)
		}
		if size := len(renderCatalogNDJSON(t, entries, listCommandsBrowseFields)); size > maxListCommandsBytes {
			t.Errorf("prefix %q renders %d bytes, over the %d-byte list_commands ceiling", prefix, size, maxListCommandsBytes)
		} else if size > largest {
			largest = size
		}

		for _, e := range entries {
			if e.Subcommands > 0 && e.Command != prefix {
				queue = append(queue, e.Command)
			}
			want, ok := full[e.Command]
			if !ok {
				if e.Subcommands == 0 {
					t.Errorf("prefix %q: %q is neither a catalog command nor a group", prefix, e.Command)
				}
				continue
			}
			reached[e.Command]++
			got := e
			got.Subcommands = 0
			if !reflect.DeepEqual(got, want) {
				t.Errorf("prefix %q: %q differs from its full-catalog entry:\n got %+v\nwant %+v", prefix, e.Command, got, want)
			}
		}
	}

	for command := range full {
		if reached[command] != 1 {
			t.Errorf("%q reached %d times walking --children levels, want 1", command, reached[command])
		}
	}
	t.Logf("%d levels, largest %d bytes, %d commands", levels, largest, len(full))
}

func TestQueryCatalog_PrefixIsTheFullCatalogSubtreeAndTakesAliases(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")

	got, err := queryCatalog(root, catalogQuery{Prefix: "pro computers"})
	if err != nil {
		t.Fatal(err)
	}
	var want []commandEntry
	for _, e := range collectCommands(root, "", "", "") {
		if e.Command == "pro computer-inventory" || strings.HasPrefix(e.Command, "pro computer-inventory ") {
			want = append(want, e)
		}
	}
	if len(want) == 0 {
		t.Fatal("the full catalog has no pro computer-inventory commands; pick another resource")
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("--prefix \"pro computers\" returned %d entries, want the %d pro computer-inventory entries of the full catalog", len(got), len(want))
	}
}

func TestQueryCatalog_ChildrenOfACommandIsTheCommandItself(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	got, err := queryCatalog(root, catalogQuery{Prefix: "version", Children: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Command != "version" {
		t.Errorf("--prefix version --children must return the version command alone, got %+v", got)
	}
}

func TestQueryCatalog_RefusesAnUnknownPrefix(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	_, err := queryCatalog(root, catalogQuery{Prefix: "pro no-such-resource"})
	if err == nil || !strings.Contains(err.Error(), "no-such-resource") {
		t.Fatalf("an unknown prefix must be refused naming the unknown word, got %v", err)
	}
}

func TestQueryCatalog_SearchMatchesEveryWordAcrossPlurals(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")

	got, err := queryCatalog(root, catalogQuery{Search: "delete policy"})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range got {
		if e.Command == "pro classic-policies delete" {
			found = true
		}
		hay := strings.ToLower(e.Command + " " + e.Description)
		if !strings.Contains(hay, "delet") || !strings.Contains(hay, "polic") {
			t.Errorf("%q matched \"delete policy\" without both words", e.Command)
		}
	}
	if !found {
		t.Errorf("\"delete policy\" must find \"pro classic-policies delete\", got %d rows", len(got))
	}

	scoped, err := queryCatalog(root, catalogQuery{Prefix: "protect", Search: "list"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped) == 0 {
		t.Fatal("\"list\" under protect found nothing")
	}
	for _, e := range scoped {
		if !strings.HasPrefix(e.Command, "protect ") {
			t.Errorf("a search under --prefix protect returned %q", e.Command)
		}
	}
}

func TestCommandsCmd_ChildrenFlagPrintsSubcommandCounts(t *testing.T) {
	stdout, _, err := runRoot(t, "commands", "--children", "-o", "json")
	if err != nil {
		t.Fatalf("commands --children failed: %v", err)
	}
	counts := map[string]float64{}
	for _, row := range commandRows(t, stdout) {
		if n, ok := row["subcommands"].(float64); ok {
			counts[row["command"].(string)] = n
		}
	}
	if counts["pro"] == 0 {
		t.Errorf("commands --children must count the commands under pro, got %v", counts)
	}
}
