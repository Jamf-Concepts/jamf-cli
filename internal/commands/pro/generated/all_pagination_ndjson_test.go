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

// ── paginated fake HTTP client ────────────────────────────────────────────────

// paginatedClient serves a two-page computers-inventory response keyed on the
// "page" query parameter. Page 0 → 100 results; page 1 → 50 results (150 total).
type paginatedClient struct {
	totalCount int
	pageSize   int
	// pagePrefix is the path prefix to match (without query string)
	pathPrefix string
}

func newComputersInventoryClient() *paginatedClient {
	return &paginatedClient{
		totalCount: 150,
		pageSize:   100,
		pathPrefix: "/v3/computers-inventory",
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

	// Page 0: first 100 results. Page 1: remaining 50. Any other page: empty.
	var results []json.RawMessage
	start := pageNum * c.pageSize
	total := c.totalCount
	for i := start; i < start+c.pageSize && i < total; i++ {
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

// ── TEST 5a: --all pagination produces 150 NDJSON lines ──────────────────────

func TestAllPagination_NDJSON_PerRecord(t *testing.T) {
	out := newNDJSONOutput()
	cliCtx := &registry.CLIContext{
		Client: newComputersInventoryClient(),
		Output: out,
	}

	// --all is the default (true), so no explicit flag needed; we just run list.
	cmd := NewComputersInventoryCmd(cliCtx)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list execute: %v", err)
	}

	lines := nonEmptyNDJSONLines(out.buf.String())
	if len(lines) != 150 {
		t.Errorf("expected 150 NDJSON lines (100+50 pages), got %d", len(lines))
	}
	assertNDJSONObjects(t, lines)
}

// ── TEST 5b: --limit 120 truncates to 120 lines ───────────────────────────────

func TestAllPagination_NDJSON_Limit(t *testing.T) {
	out := newNDJSONOutput()
	cliCtx := &registry.CLIContext{
		Client: newComputersInventoryClient(),
		Output: out,
	}

	cmd := NewComputersInventoryCmd(cliCtx)
	cmd.SetArgs([]string{"list", "--limit", "120"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list execute: %v", err)
	}

	lines := nonEmptyNDJSONLines(out.buf.String())
	if len(lines) != 120 {
		t.Errorf("expected 120 NDJSON lines (--limit 120), got %d", len(lines))
	}
	assertNDJSONObjects(t, lines)
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
