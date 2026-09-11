// Copyright 2026, Jamf Software LLC

package parser

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// droppedOperationsFile is every operation the shipped surface loses to a name
// collision, pinned.
const droppedOperationsFile = "testdata/dropped-operations.tsv"

// TestTheDroppedOperationSetIsPinned is the guard TestParseMonolith_LosesNo-
// Endpoint is one pipeline stage short of being.
//
// That test compares the *parse* against a baseline of the 165-file layout, and
// the last drop happens after it: Generate calls dedupeOperations, which
// resolves a name still held by two operations by keeping one and **discarding
// the other** with a warning on stderr and exit 0. So "704 → 704, zero gained
// or lost" was true of parsing and not of the commands, and seven writes went
// missing under it — the whole of `enrollment-customizations` create, update and
// delete, and POST and PUT on both prestage `/scope` endpoints.
//
// Two assertions, and the second is the one that would have caught it.
//
// The set is pinned, so a drop appearing or disappearing is a diff to explain
// rather than a line in `make generate`'s output that nobody reads. And no
// dropped operation may address its resource's own root: a resource's plain
// verbs are its root's, so a root operation losing its name to a sub-path's is
// always the wrong resolution, whatever the two operations are. Fifteen drops
// survive and every one is a collision between two sub-paths.
func TestTheDroppedOperationSetIsPinned(t *testing.T) {
	resources := loadShippedResources(t)

	var got []string
	for _, r := range FlattenResources(resources) {
		root := "/" + strings.Join(r.Root, "/")
		kept := keptByName(r.Operations)
		for _, op := range droppedByDedupe(r.Operations) {
			got = append(got, strings.Join([]string{r.QualifiedName(), op.Name, op.Method, op.Path}, "\t"))
			if len(r.Root) == 0 || !addressesRoot(op, root) {
				continue
			}
			// A root operation losing its name to another root operation is a
			// different question — which of the collection and the item owns
			// `update` — and it has one live case, recorded in the fixture. What
			// is always wrong is a root operation losing to a sub-path's, since
			// a resource's plain verbs are its root's.
			if winner := kept[op.Name]; winner != nil && addressesRoot(winner, root) {
				continue
			}
			t.Errorf("%s: %s %s addresses the resource's own root %s and is dropped for the name %q, which "+
				"went to a sub-path — a root operation must outrank a sub-path's, so this is a naming bug "+
				"rather than a drop to record",
				r.QualifiedName(), op.Method, op.Path, root, op.Name)
		}
	}
	sort.Strings(got)

	want := readPinnedDrops(t)
	if len(want) == 0 {
		t.Fatalf("%s holds no rows, so this test cannot pass vacuously", droppedOperationsFile)
	}
	if diff := lineDiff(want, got); diff != "" {
		t.Errorf("the dropped-operation set moved. Every row is a capability the CLI does not ship, so a new "+
			"one needs a name that resolves the collision instead — see qualifyDuplicateVerbsOutsideTheRoot. "+
			"Update %s once the change is deliberate:\n%s", droppedOperationsFile, diff)
	}
}

// droppedByDedupe returns the operations dedupeOperations discards, computed as
// the difference between what it is given and what it keeps — so the rule lives
// in one place and this cannot drift from it.
func droppedByDedupe(ops []*Operation) []*Operation {
	kept := map[*Operation]bool{}
	// A copy, because dedupeOperations is also the pass Generate runs and this
	// test must not consume the resource it inspects.
	in := make([]*Operation, len(ops))
	copy(in, ops)
	for _, op := range dedupeOperations(in) {
		kept[op] = true
	}
	var dropped []*Operation
	for _, op := range ops {
		if !kept[op] {
			dropped = append(dropped, op)
		}
	}
	return dropped
}

// keptByName maps each surviving operation name to the operation that kept it.
func keptByName(ops []*Operation) map[string]*Operation {
	in := make([]*Operation, len(ops))
	copy(in, ops)
	out := map[string]*Operation{}
	for _, op := range dedupeOperations(in) {
		out[op.Name] = op
	}
	return out
}

func loadShippedResources(t *testing.T) []*Resource {
	t.Helper()
	specs, err := filepath.Glob("../../specs/*.yaml")
	if err != nil || len(specs) == 0 {
		t.Fatalf("specs/*.yaml is a required committed artifact: %v", err)
	}
	resources, _, err := LoadDocuments(specs)
	if err != nil {
		t.Fatalf("LoadDocuments: %v", err)
	}
	if len(resources) == 0 {
		t.Fatal("no resource parsed, so this test cannot pass vacuously")
	}
	return resources
}

func readPinnedDrops(t *testing.T) []string {
	t.Helper()
	f, err := os.Open(droppedOperationsFile)
	if err != nil {
		t.Fatalf("%s is a required committed artifact: %v", droppedOperationsFile, err)
	}
	defer func() { _ = f.Close() }()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("reading %s: %v", droppedOperationsFile, err)
	}
	sort.Strings(out)
	return out
}

func lineDiff(want, got []string) string {
	inWant := map[string]bool{}
	for _, l := range want {
		inWant[l] = true
	}
	inGot := map[string]bool{}
	for _, l := range got {
		inGot[l] = true
	}
	var b strings.Builder
	for _, l := range got {
		if !inWant[l] {
			fmt.Fprintf(&b, "  + %s\n", l)
		}
	}
	for _, l := range want {
		if !inGot[l] {
			fmt.Fprintf(&b, "  - %s\n", l)
		}
	}
	return b.String()
}
