// Copyright 2026, Jamf Software LLC

package generated

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/progress"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// ── ndjson output wrapper ─────────────────────────────────────────────────────

// ndjsonOutput wraps output.Formatter and satisfies registry.OutputFormatter
// with a real ndjson formatter writing to a captured buffer.
type ndjsonOutput struct {
	f   *output.Formatter
	buf *bytes.Buffer
	// ignoredBy records the (requested, used) pair of the last dropped
	// --page-size notice, so a test can assert the drop was reported.
	ignoredBy [2]int
	clamped   [2]int
}

func newNDJSONOutput() *ndjsonOutput {
	buf := &bytes.Buffer{}
	f := output.New("ndjson", true, false)
	f.SetWriter(buf)
	return &ndjsonOutput{f: f, buf: buf}
}

func (o *ndjsonOutput) PrintResponse(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return o.f.PrintRaw(body)
}

func (o *ndjsonOutput) PrintRaw(data []byte) error {
	return o.f.PrintRaw(data)
}

func (o *ndjsonOutput) PrintBytes(data []byte) error {
	return o.f.PrintBytes(data)
}

func (o *ndjsonOutput) Format() string {
	return o.f.Format()
}

func (o *ndjsonOutput) PaginationProgress() *progress.Reporter {
	return progress.New(io.Discard, progress.Silent)
}

func (o *ndjsonOutput) NotePageSizeIgnoredByAll(requested, used int) {
	o.ignoredBy = [2]int{requested, used}
}

func (o *ndjsonOutput) NotePageSizeClamped(requested, ceiling int) {
	o.clamped = [2]int{requested, ceiling}
}

// ── paginated fake HTTP client ────────────────────────────────────────────────

// paginatedClient serves a computers-inventory collection of totalCount rows,
// HONOURING the page-size the command asked for, the way the Jamf Pro API does
// up to its 2000 ceiling. It used to serve a fixed 100 rows a page whatever was
// requested, which is a server that clamps — and that hid the defect in issue
// 385 rather than exposing it, because the command only ever asked for 100.
//
// pageSizes records every page size seen, so a test can assert what went on the
// wire and not just how many rows came back.
type paginatedClient struct {
	totalCount int
	// pagePrefix is the path prefix to match (without query string)
	pathPrefix string
	pageSizes  []int
}

func newComputersInventoryClient() *paginatedClient {
	return &paginatedClient{
		// Deliberately more than two full pages at the endpoint's 2000
		// ceiling, so the walk is still a multi-page walk after the page size
		// went up: 2000 + 2000 + 500.
		totalCount: 4500,
		pathPrefix: "/v4/computers-inventory",
	}
}

func (c *paginatedClient) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	if method != "GET" || !strings.HasPrefix(path, c.pathPrefix) {
		return &http.Response{
			StatusCode: 404,
			Body:       io.NopCloser(strings.NewReader(`{"error":"not found"}`)),
		}, nil
	}

	// Parse page number from query string.
	pageNum := 0
	if _, after, ok := strings.Cut(path, "page="); ok {
		rest := after
		// Read digits until non-digit
		end := 0
		for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
			end++
		}
		_, _ = fmt.Sscanf(rest[:end], "%d", &pageNum)
	}

	pageSize := queryInt(path, "page-size=", 100)
	c.pageSizes = append(c.pageSizes, pageSize)

	var results []json.RawMessage
	start := pageNum * pageSize
	total := c.totalCount
	for i := start; i < start+pageSize && i < total; i++ {
		obj := json.RawMessage(fmt.Sprintf(`{"id":"%d","general":{"name":"computer-%d"}}`, i+1, i+1))
		results = append(results, obj)
	}

	body := struct {
		TotalCount int               `json:"totalCount"`
		Results    []json.RawMessage `json:"results"`
	}{
		TotalCount: total,
		Results:    results,
	}
	bodyBytes, _ := json.Marshal(body)

	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewReader(bodyBytes)),
	}, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// queryInt reads the integer value of a query parameter from a raw path,
// returning fallback when the parameter is absent.
func queryInt(path, key string, fallback int) int {
	_, after, ok := strings.Cut(path, key)
	if !ok {
		return fallback
	}
	end := 0
	for end < len(after) && after[end] >= '0' && after[end] <= '9' {
		end++
	}
	n := fallback
	if _, err := fmt.Sscanf(after[:end], "%d", &n); err != nil {
		return fallback
	}
	return n
}

// nonEmptyNDJSONLines splits output on newlines, dropping blank trailing lines.
func nonEmptyNDJSONLines(s string) []string {
	var out []string
	for ln := range strings.SplitSeq(s, "\n") {
		if ln != "" {
			out = append(out, ln)
		}
	}
	return out
}

// assertNDJSONObjects checks that every line is a valid JSON object (not an array).
func assertNDJSONObjects(t *testing.T, lines []string) {
	t.Helper()
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "[") {
			t.Errorf("line %d starts with '[' (array wrapper leaked): %q", i, ln)
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(ln), &obj); err != nil {
			t.Errorf("line %d is not a valid JSON object: %q (%v)", i, ln, err)
		}
	}
}

// ── TEST 5a: --all pagination produces one NDJSON line per record ────────────

func TestAllPagination_NDJSON_PerRecord(t *testing.T) {
	out := newNDJSONOutput()
	client := newComputersInventoryClient()
	cliCtx := &registry.CLIContext{
		Client: client,
		Output: out,
	}

	// --all is the default (true), so no explicit flag needed; we just run list.
	cmd := NewComputerInventoryCmd(cliCtx)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list execute: %v", err)
	}

	lines := nonEmptyNDJSONLines(out.buf.String())
	if len(lines) != 4500 {
		t.Errorf("expected 4500 NDJSON lines (2000+2000+500 pages), got %d", len(lines))
	}
	assertNDJSONObjects(t, lines)

	// The page size, not just the row count: issue 385's whole symptom was a
	// correct row count reached in 45 requests instead of 3.
	if len(client.pageSizes) != 3 {
		t.Errorf("expected 3 requests for 4500 rows at page-size 2000, got %d: %v", len(client.pageSizes), client.pageSizes)
	}
	for i, ps := range client.pageSizes {
		if ps != 2000 {
			t.Errorf("request %d asked for page-size=%d, want 2000 (the endpoint maximum)", i, ps)
		}
	}
}

// ── TEST 5a2: --page-size is ignored by --all, and said to be ────────────────

func TestAllPagination_PageSizeIgnoredWithANotice(t *testing.T) {
	out := newNDJSONOutput()
	client := newComputersInventoryClient()
	cliCtx := &registry.CLIContext{
		Client: client,
		Output: out,
	}

	cmd := NewComputerInventoryCmd(cliCtx)
	cmd.SetArgs([]string{"list", "--page-size", "100"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list execute: %v", err)
	}

	for i, ps := range client.pageSizes {
		if ps != 2000 {
			t.Errorf("request %d asked for page-size=%d; --all must use the endpoint maximum 2000, not --page-size", i, ps)
		}
	}
	if len(nonEmptyNDJSONLines(out.buf.String())) != 4500 {
		t.Error("--page-size alongside --all must not change which records are returned")
	}
	if out.ignoredBy != [2]int{100, 2000} {
		t.Errorf("expected a dropped-flag notice of (requested 100, used 2000), got %v — a silently dropped flag is the defect", out.ignoredBy)
	}
}

// ── TEST 5a3: --page 0 asks for the first page alone ─────────────────────────

func TestAllPagination_ExplicitPageZeroIsASinglePage(t *testing.T) {
	out := newNDJSONOutput()
	client := newComputersInventoryClient()
	cliCtx := &registry.CLIContext{
		Client: client,
		Output: out,
	}

	cmd := NewComputerInventoryCmd(cliCtx)
	cmd.SetArgs([]string{"list", "--page", "0", "--page-size", "10"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list execute: %v", err)
	}

	if len(client.pageSizes) != 1 {
		t.Errorf("--page 0 must fetch one page, got %d requests: %v", len(client.pageSizes), client.pageSizes)
	}
	if got := len(nonEmptyNDJSONLines(out.buf.String())); got != 10 {
		t.Errorf("--page 0 --page-size 10 returned %d records, want 10", got)
	}
}

// ── TEST 5b: --limit 120 truncates to 120 lines ───────────────────────────────

func TestAllPagination_NDJSON_Limit(t *testing.T) {
	out := newNDJSONOutput()
	client := newComputersInventoryClient()
	cliCtx := &registry.CLIContext{
		Client: client,
		Output: out,
	}

	cmd := NewComputerInventoryCmd(cliCtx)
	cmd.SetArgs([]string{"list", "--limit", "120"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list execute: %v", err)
	}

	lines := nonEmptyNDJSONLines(out.buf.String())
	if len(lines) != 120 {
		t.Errorf("expected 120 NDJSON lines (--limit 120), got %d", len(lines))
	}
	assertNDJSONObjects(t, lines)

	// A small --limit must not pull a full 2000-row page to satisfy it.
	if len(client.pageSizes) != 1 || client.pageSizes[0] != 120 {
		t.Errorf("--limit 120 should have asked for one page of 120, got %v", client.pageSizes)
	}
}

// ── TEST 5c: classic list produces per-record NDJSON lines ───────────────────

// classicUsersClient returns an XML /JSSResource/accounts response with N users.
type classicUsersClient struct {
	count int
}

func (c *classicUsersClient) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	if method != "GET" || path != "/JSSResource/accounts" {
		return &http.Response{
			StatusCode: 404,
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	}

	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?><accounts><users>`)
	for i := 1; i <= c.count; i++ {
		fmt.Fprintf(&sb, `<user><id>%d</id><name>user-%d</name></user>`, i, i)
	}
	sb.WriteString(`</users><groups/></accounts>`)

	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(sb.String())),
	}, nil
}

func TestClassicList_NDJSON_PerRecord(t *testing.T) {
	out := newNDJSONOutput()
	cliCtx := &registry.CLIContext{
		Client: &classicUsersClient{count: 5},
		Output: out,
	}

	// The formatter's Format() returns "ndjson", so the classic list code will
	// skip its "default to pretty-printed XML" branch and take the structured path.
	cmd := NewClassicAccountUsersCmd(cliCtx)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list execute: %v", err)
	}

	lines := nonEmptyNDJSONLines(out.buf.String())
	if len(lines) != 5 {
		t.Errorf("expected 5 NDJSON lines (one per user), got %d\noutput:\n%s", len(lines), out.buf.String())
	}
	assertNDJSONObjects(t, lines)
	// Each line should contain the user name field.
	for i, ln := range lines {
		if !strings.Contains(ln, fmt.Sprintf("user-%d", i+1)) {
			t.Errorf("line %d does not contain expected user name 'user-%d': %q", i, i+1, ln)
		}
	}
}
