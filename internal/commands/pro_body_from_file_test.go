// Copyright 2026, Jamf Software LLC

package commands

import (
	"strings"
	"testing"
)

// TestProWritesTakeABodyFromFile holds the generated Pro writes to the body
// flag every other product's writes take. They used to read stdin only, so
// `pro categories create --from-file body.json` answered "unknown flag". A
// destructive delete keeps --from-file as its list of IDs or names — cobra
// would panic on a second one, so one flag cannot mean both on a leaf.
func TestProWritesTakeABodyFromFile(t *testing.T) {
	root := NewRootCmd("test", "none", "none", "none")
	for _, path := range [][]string{
		{"pro", "categories", "create"},
		{"pro", "categories", "update"},
		{"pro", "computer-groups-static-groups", "create"},
		{"pro", "computer-groups-static-groups", "update"},
	} {
		cmd, _, err := root.Find(path)
		if err != nil || cmd.Name() != path[len(path)-1] {
			t.Fatalf("%v: not found", path)
		}
		f := cmd.Flags().Lookup("from-file")
		if f == nil || !strings.Contains(f.Usage, "request body") {
			t.Errorf("%v: no body --from-file (%v)", path, f)
		}
	}

	del, _, _ := root.Find([]string{"pro", "categories", "delete"})
	if f := del.Flags().Lookup("from-file"); f == nil || !strings.Contains(f.Usage, "IDs or names") {
		t.Errorf("pro categories delete: --from-file is no longer the ID list (%v)", f)
	}

	// --set builds the whole body, so a file beside it is refused by cobra
	// before anything is fetched.
	upd, _, _ := root.Find([]string{"pro", "categories", "update"})
	upd.SetArgs(nil)
	if err := upd.ParseFlags([]string{"--set", "priority=1", "--from-file", "x.json"}); err != nil {
		t.Fatal(err)
	}
	if err := upd.ValidateFlagGroups(); err == nil {
		t.Error("--set and --from-file were accepted together")
	}
}
