// Copyright 2026, Jamf Software LLC

package generated

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

// resolveNameMockClient returns a fixed body for every request, recording every
// request path so tests can assert how the lookup URL was constructed.
type resolveNameMockClient struct {
	body   string
	status int
	paths  []string
}

func (m *resolveNameMockClient) Do(_ context.Context, _, path string, _ io.Reader) (*http.Response, error) {
	m.paths = append(m.paths, path)
	status := m.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(m.body)),
	}, nil
}

// Two computer records sharing a serial number — the motherboard-swap scenario
// from issue #273.
const dupSerialBody = `{"totalCount":2,"results":[` +
	`{"id":"42","hardware":{"serialNumber":"C02X1234"}},` +
	`{"id":"88","hardware":{"serialNumber":"C02X1234"}}]}`

const oneSerialBody = `{"totalCount":1,"results":[` +
	`{"id":"42","hardware":{"serialNumber":"C02X1234"}}]}`

const noMatchBody = `{"totalCount":0,"results":[]}`

const (
	serialPath  = "/v3/computers-inventory"
	serialField = "hardware.serialNumber"
)

// resolveNameToID must never silently target one of several duplicate records:
// under --no-input a collision is a hard error naming every candidate ID.
func TestResolveNameToID_DuplicateSerialErrors(t *testing.T) {
	client := &resolveNameMockClient{body: dupSerialBody}
	_, err := resolveNameToID(context.Background(), client, serialPath, serialField, "id", "C02X1234", true)
	if err == nil {
		t.Fatal("expected error for duplicate serial, got nil")
	}
	for _, want := range []string{"multiple resources found", "42", "88"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
	// A collision is a fix-the-invocation condition, not a generic failure —
	// agents keying off the exit code must see Usage, not the General fallback.
	if code := exitcode.CodeFrom(err); code != exitcode.Usage {
		t.Errorf("exit code = %d, want %d (Usage)", code, exitcode.Usage)
	}
}

// Without --no-input, an ambiguous lookup must still fail fast when stdin isn't
// a terminal (CI, pipes, non-pty SSH) instead of blocking forever on a prompt
// read that will never be answered.
func TestPickMatchingID_NonTTYFailsFast(t *testing.T) {
	r, w, err := os.Pipe() // a pipe is never a terminal
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()
	orig := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = orig }()

	_, err = pickMatchingID([]string{"42", "88"}, "C02X1234", false)
	if err == nil {
		t.Fatal("expected error on non-TTY collision, got nil")
	}
	if !strings.Contains(err.Error(), "multiple resources found") {
		t.Errorf("error %q missing collision text", err)
	}
	if code := exitcode.CodeFrom(err); code != exitcode.Usage {
		t.Errorf("exit code = %d, want %d (Usage)", code, exitcode.Usage)
	}
}

// The RSQL filter must neutralize both quotes and backslashes so a value
// ending in a backslash can't break out of the string literal.
func TestLookupMatchingIDs_EscapesBackslashAndQuote(t *testing.T) {
	client := &resolveNameMockClient{body: noMatchBody}
	if _, err := lookupMatchingIDs(context.Background(), client, "/policies", "name", "id", `a\"b`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(client.paths) == 0 {
		t.Fatal("no request captured")
	}
	// url.QueryEscape encodes the fragment; decode the filter value back and
	// assert the backslash was doubled and the quote escaped before encoding.
	got := client.paths[0]
	// filter=name=="a\\\"b"  →  after QueryEscape the raw filter param must
	// contain the escaped sequence; assert the pre-encode form round-trips.
	const wantFilter = `name=="a\\\"b"`
	q := got[strings.Index(got, "filter=")+len("filter="):]
	if amp := strings.IndexByte(q, '&'); amp >= 0 {
		q = q[:amp]
	}
	dec, err := url.QueryUnescape(q)
	if err != nil {
		t.Fatalf("decoding filter: %v", err)
	}
	if dec != wantFilter {
		t.Errorf("filter = %q, want %q", dec, wantFilter)
	}
}

// A matching record whose ID can't be extracted must surface an error rather
// than silently shrink the candidate set (which would defeat collision
// detection — the exact failure mode this resolver exists to prevent).
func TestLookupMatchingIDs_UnextractableIDErrors(t *testing.T) {
	// Two records share the serial; the second has no id field.
	body := `{"totalCount":2,"results":[` +
		`{"id":"42","hardware":{"serialNumber":"C02X1234"}},` +
		`{"hardware":{"serialNumber":"C02X1234"}}]}`
	client := &resolveNameMockClient{body: body}
	ids, err := lookupMatchingIDs(context.Background(), client, serialPath, serialField, "id", "C02X1234")
	if err == nil {
		t.Fatal("expected error when a candidate has no extractable ID, got nil")
	}
	if !strings.Contains(err.Error(), "could not be resolved to an ID") {
		t.Errorf("error %q missing skip explanation", err)
	}
	// The well-formed candidate is still returned alongside the error.
	if len(ids) != 1 || ids[0] != "42" {
		t.Errorf("ids = %v, want [42]", ids)
	}
}

// When the filtered field is present in the response, the client-side safety
// net must narrow to exact matches — not blindly trust the server's filter.
func TestFilterResultsByName_NarrowsOnHardwareSection(t *testing.T) {
	results := []json.RawMessage{
		json.RawMessage(`{"id":"1","hardware":{"serialNumber":"C02X1234"}}`),
		json.RawMessage(`{"id":"2","hardware":{"serialNumber":"OTHER999"}}`),
	}
	filtered := filterResultsByName(results, "hardware.serialNumber", "C02X1234")
	if len(filtered) != 1 {
		t.Fatalf("got %d results, want 1 (mismatch excluded)", len(filtered))
	}
	if !strings.Contains(string(filtered[0]), `"id":"1"`) {
		t.Errorf("kept wrong record: %s", filtered[0])
	}
}

// A listPath that already carries a query string (e.g. ?section=HARDWARE) must
// join the filter with & rather than a second ?.
func TestLookupMatchingIDs_SectionQueryJoin(t *testing.T) {
	client := &resolveNameMockClient{body: oneSerialBody}
	if _, err := lookupMatchingIDs(context.Background(), client,
		"/v3/computers-inventory?section=HARDWARE", serialField, "id", "C02X1234"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := client.paths[0]
	if strings.Count(got, "?") != 1 {
		t.Errorf("path %q has %d '?' separators, want exactly 1", got, strings.Count(got, "?"))
	}
	if !strings.Contains(got, "section=HARDWARE&filter=") {
		t.Errorf("path %q did not join section and filter with &", got)
	}
}

func TestResolveNameToID_SingleMatch(t *testing.T) {
	client := &resolveNameMockClient{body: oneSerialBody}
	id, err := resolveNameToID(context.Background(), client, serialPath, serialField, "id", "C02X1234", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "42" {
		t.Errorf("id = %q, want 42", id)
	}
}

func TestResolveNameToID_NoMatchErrors(t *testing.T) {
	client := &resolveNameMockClient{body: noMatchBody}
	_, err := resolveNameToID(context.Background(), client, serialPath, serialField, "id", "NOPE", true)
	if err == nil || !strings.Contains(err.Error(), "no resource found") {
		t.Fatalf("expected 'no resource found' error, got %v", err)
	}
}

// resolveNameToIDForApply shares the same collision guard as resolveNameToID.
func TestResolveNameToIDForApply_DuplicateSerialErrors(t *testing.T) {
	client := &resolveNameMockClient{body: dupSerialBody}
	_, err := resolveNameToIDForApply(context.Background(), client, serialPath, serialField, "id", "C02X1234", true)
	if err == nil || !strings.Contains(err.Error(), "multiple resources found") {
		t.Fatalf("expected collision error, got %v", err)
	}
}

// The one behavioral difference: a no-match returns ("", nil) so apply creates.
func TestResolveNameToIDForApply_NoMatchReturnsEmpty(t *testing.T) {
	client := &resolveNameMockClient{body: noMatchBody}
	id, err := resolveNameToIDForApply(context.Background(), client, serialPath, serialField, "id", "NOPE", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "" {
		t.Errorf("id = %q, want empty (caller should create)", id)
	}
}

// The shared core surfaces every duplicate so callers can decide what to do.
func TestLookupMatchingIDs_ReturnsAllDuplicates(t *testing.T) {
	client := &resolveNameMockClient{body: dupSerialBody}
	ids, err := lookupMatchingIDs(context.Background(), client, serialPath, serialField, "id", "C02X1234")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 2 || ids[0] != "42" || ids[1] != "88" {
		t.Errorf("ids = %v, want [42 88]", ids)
	}
}
