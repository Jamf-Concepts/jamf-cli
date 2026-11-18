// Copyright 2026, Jamf Software LLC

package resolve

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/listfile"
)

func entries(values ...string) []listfile.Entry {
	out := make([]listfile.Entry, len(values))
	for i, v := range values {
		out[i] = listfile.Entry{Value: v, Line: i + 1}
	}
	return out
}

func targetIDs(targets []Target) string {
	s := make([]string, len(targets))
	for i, t := range targets {
		s[i] = t.ID
	}
	return strings.Join(s, ",")
}

// stubLookup answers every call with records and counts the calls.
type stubLookup struct {
	records []NamedRecord
	calls   int
	names   []string
}

func (s *stubLookup) lookup(_ context.Context, names []string) ([]NamedRecord, error) {
	s.calls++
	s.names = append(s.names, names...)
	return s.records, nil
}

func TestResolveNamedEntries_OneLookupForEveryName(t *testing.T) {
	stub := &stubLookup{records: []NamedRecord{{ID: "7", Name: "Main Office"}, {ID: "9", Name: "Annex"}}}
	targets, skipped, err := ResolveNamedEntries(context.Background(),
		NamedEntrySpec{Label: "building", Lookup: stub.lookup},
		entries("Annex", "42", "Main Office", "Annex"))
	if err != nil {
		t.Fatal(err)
	}
	if got := targetIDs(targets); got != "9,42,7" || skipped != 0 {
		t.Errorf("targets = %s, skipped = %d; want 9,42,7 in input order with the repeat dropped", got, skipped)
	}
	if stub.calls != 1 || strings.Join(stub.names, ",") != "Annex,Main Office" {
		t.Errorf("lookup called %d times with %v; want once, with each name once and no ID", stub.calls, stub.names)
	}
}

func TestResolveNamedEntries_AllDigitsIsAnIDNeverAName(t *testing.T) {
	stub := &stubLookup{records: []NamedRecord{{ID: "5", Name: "2024"}}}
	targets, _, err := ResolveNamedEntries(context.Background(),
		NamedEntrySpec{Label: "building", Lookup: stub.lookup}, entries("2024"))
	if err != nil || targetIDs(targets) != "2024" || stub.calls != 0 {
		t.Errorf("targets = %s, err = %v, lookups = %d; want 2024 taken as an ID with no lookup", targetIDs(targets), err, stub.calls)
	}
}

func TestResolveNamedEntries_SkipsAndCounts(t *testing.T) {
	stub := &stubLookup{records: []NamedRecord{
		{ID: "1", Name: "Dup"}, {ID: "2", Name: "Dup"}, {ID: "3", Name: "Lab"},
	}}
	targets, skipped, err := ResolveNamedEntries(context.Background(),
		NamedEntrySpec{Label: "building", Lookup: stub.lookup},
		entries("Dup", "Missing", "0", "Lab"))
	if err != nil {
		t.Fatal(err)
	}
	if targetIDs(targets) != "3" || skipped != 3 {
		t.Errorf("targets = %s, skipped = %d; want only Lab, with the ambiguous, missing and 0 lines skipped", targetIDs(targets), skipped)
	}
}

func TestResolveNamedEntries_NothingResolvedIsAnError(t *testing.T) {
	stub := &stubLookup{}
	_, skipped, err := ResolveNamedEntries(context.Background(),
		NamedEntrySpec{Label: "building", Lookup: stub.lookup}, entries("Gone"))
	if !errors.Is(err, errNothingResolved) || skipped != 1 {
		t.Errorf("err = %v, skipped = %d; want errNothingResolved, so a stale file is not a no-op success", err, skipped)
	}
}

func TestResolveNamedEntries_CaseRules(t *testing.T) {
	records := []NamedRecord{{ID: "1", Name: "Lab"}, {ID: "2", Name: "LAB"}, {ID: "3", Name: "Office"}}
	cases := []struct {
		fold  bool
		entry string
		want  string // "" = skipped
	}{
		{false, "Lab", "1"},   // exact
		{false, "lab", ""},    // Pro: no exact match, no fold
		{true, "Lab", "1"},    // Classic: two fold, one exact → the exact one
		{true, "lab", ""},     // Classic: two fold, no exact → ambiguous
		{true, "office", "3"}, // Classic: one fold
	}
	for _, tc := range cases {
		stub := &stubLookup{records: records}
		targets, _, _ := ResolveNamedEntries(context.Background(),
			NamedEntrySpec{Label: "x", Lookup: stub.lookup, FoldCase: tc.fold}, entries(tc.entry, "99"))
		got := ""
		if len(targets) == 2 {
			got = targets[0].ID
		}
		if got != tc.want {
			t.Errorf("fold=%v %q: resolved to %q, want %q", tc.fold, tc.entry, got, tc.want)
		}
	}
}

func TestResolveNamedEntries_NoLookupTakesIDsOnly(t *testing.T) {
	targets, skipped, err := ResolveNamedEntries(context.Background(),
		NamedEntrySpec{Label: "account"}, entries("12", "admin"))
	if err != nil || targetIDs(targets) != "12" || skipped != 1 {
		t.Errorf("targets = %s, skipped = %d, err = %v; want the name refused and the ID kept", targetIDs(targets), skipped, err)
	}
}

func TestProNameLookup_BatchesNamesIntoOneFilter(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		`filter=name=in=("Lab","Main Office")`: {status: 200, body: `{"totalCount":2,"results":[{"id":"7","name":"Lab"},{"id":8,"name":"Main Office"}]}`},
	}}
	records, err := ProNameLookup(client, "/v1/buildings", "name", "id")(context.Background(), []string{"Lab", "Main Office"})
	if err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 1 || len(records) != 2 || records[1].ID != "8" {
		t.Errorf("calls = %v, records = %+v; want one request and both records, a numeric id rendered as text", client.calls, records)
	}
}

func TestProNameLookup_FallsBackWhenInIsRefused(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		`filter=name=in=`:              {status: http.StatusBadRequest, body: `{"httpStatus":400}`},
		`filter=name=="Lab",name=="X"`: {status: 200, body: `{"totalCount":1,"results":[{"id":"7","name":"Lab"}]}`},
	}}
	records, err := ProNameLookup(client, "/v1/x", "name", "id")(context.Background(), []string{"Lab", "X"})
	if err != nil || len(records) != 1 || records[0].ID != "7" {
		t.Fatalf("records = %+v, err = %v, calls = %v", records, err, client.calls)
	}
}

func TestProNameLookup_ListsWholeWhenEveryFilterIsRefused(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		`filter=`:      {status: http.StatusBadRequest, body: `{"httpStatus":400}`},
		`/v1/x?page=0`: {status: 200, body: `{"totalCount":2,"results":[{"id":"1","name":"A"},{"id":"2","name":"B"}]}`},
	}}
	records, err := ProNameLookup(client, "/v1/x", "name", "id")(context.Background(), []string{"B"})
	if err != nil || len(records) != 2 {
		t.Fatalf("records = %+v, err = %v, calls = %v", records, err, client.calls)
	}
}

func TestProNameLookup_ReadsAPlainArrayAndADottedName(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		`/v1/x?filter=`: {status: 200, body: `[{"id":"3","general":{"name":"Deep"}}]`},
	}}
	records, err := ProNameLookup(client, "/v1/x", "general.name", "id")(context.Background(), []string{"Deep"})
	if err != nil || len(records) != 1 || records[0].Name != "Deep" || records[0].ID != "3" {
		t.Fatalf("records = %+v, err = %v", records, err)
	}
}

func TestProNameLookup_ChunksALongList(t *testing.T) {
	names := make([]string, 250)
	for i := range names {
		names[i] = fmt.Sprintf("n%03d", i)
	}
	client := &mockClient{responses: map[string]mockResponse{
		`filter=`: {status: 200, body: `{"totalCount":0,"results":[]}`},
	}}
	if _, err := ProNameLookup(client, "/v1/x", "name", "id")(context.Background(), names); err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 3 {
		t.Errorf("sent %d requests for 250 names, want 3 chunks of at most 100", len(client.calls))
	}
}
