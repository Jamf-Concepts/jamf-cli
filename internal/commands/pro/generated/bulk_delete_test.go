// Copyright 2026, Jamf Software LLC

package generated

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

// ── readDeleteFile ────────────────────────────────────────────────────────────

func TestReadDeleteFile(t *testing.T) {
	write := func(t *testing.T, content string) string {
		t.Helper()
		f, err := os.CreateTemp(t.TempDir(), "del-*.txt")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(content); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		return f.Name()
	}

	t.Run("basic entries", func(t *testing.T) {
		p := write(t, "123\n456\n789\n")
		got, err := readDeleteFile(p)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"123", "456", "789"}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("[%d] got %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("comment lines and blank lines stripped", func(t *testing.T) {
		p := write(t, "# comment\n\n123\n# another comment\n456\n\n")
		got, err := readDeleteFile(p)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"123", "456"}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("CRLF line endings", func(t *testing.T) {
		p := write(t, "123\r\n456\r\n789\r\n")
		got, err := readDeleteFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range got {
			if strings.Contains(e, "\r") {
				t.Errorf("entry %q still contains CR", e)
			}
		}
		if len(got) != 3 {
			t.Fatalf("got %d entries, want 3", len(got))
		}
	})

	t.Run("mixed numeric and name entries", func(t *testing.T) {
		p := write(t, "42\nMy Policy\n# skip\n100\nAnother Policy\n")
		got, err := readDeleteFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 4 {
			t.Fatalf("got %d entries, want 4", len(got))
		}
		if got[0] != "42" || got[1] != "My Policy" || got[2] != "100" || got[3] != "Another Policy" {
			t.Errorf("unexpected entries: %v", got)
		}
	})

	t.Run("empty file returns empty slice", func(t *testing.T) {
		p := write(t, "")
		got, err := readDeleteFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Errorf("got %v, want empty", got)
		}
	})

	t.Run("nonexistent file returns error", func(t *testing.T) {
		_, err := readDeleteFile(filepath.Join(t.TempDir(), "no-such-file.txt"))
		if err == nil {
			t.Fatal("expected error for missing file, got nil")
		}
	})
}

// ── isNumericID ───────────────────────────────────────────────────────────────

func TestIsNumericID(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"123", true},
		{"0", true},
		{"1", true},
		{"999999", true},
		{"", false},
		{"abc", false},
		{"12abc", false},
		{"abc12", false},
		{"-1", false},
		{"1.5", false},
		{"1 2", false},
		{"My Policy", false},
		{"FVFC41HCLYWP", false},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			if got := isNumericID(tc.input); got != tc.want {
				t.Errorf("isNumericID(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// ── classicFindIDByName ───────────────────────────────────────────────────────

func makeClassicListXML(items [][2]string) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><items>`)
	for _, item := range items {
		fmt.Fprintf(&b, `<item><id>%s</id><name>%s</name></item>`, item[0], item[1])
	}
	b.WriteString(`</items>`)
	return []byte(b.String())
}

func TestClassicFindIDByName(t *testing.T) {
	t.Run("exact match", func(t *testing.T) {
		body := makeClassicListXML([][2]string{{"1", "Alpha"}, {"2", "Beta"}})
		if got := classicFindIDByName(body, "Beta"); got != "2" {
			t.Errorf("got %q, want %q", got, "2")
		}
	})

	t.Run("case-insensitive match", func(t *testing.T) {
		body := makeClassicListXML([][2]string{{"10", "My Policy"}})
		if got := classicFindIDByName(body, "my policy"); got != "10" {
			t.Errorf("got %q, want %q", got, "10")
		}
	})

	t.Run("no match returns empty string", func(t *testing.T) {
		body := makeClassicListXML([][2]string{{"1", "Alpha"}})
		if got := classicFindIDByName(body, "Nonexistent"); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("first match returned when duplicates exist", func(t *testing.T) {
		body := makeClassicListXML([][2]string{{"5", "Dupe"}, {"6", "Dupe"}})
		if got := classicFindIDByName(body, "Dupe"); got != "5" {
			t.Errorf("got %q, want %q", got, "5")
		}
	})

	t.Run("malformed XML returns empty string", func(t *testing.T) {
		if got := classicFindIDByName([]byte(`<not valid xml`), "anything"); got != "" {
			t.Errorf("got %q, want empty on malformed XML", got)
		}
	})

	t.Run("empty body returns empty string", func(t *testing.T) {
		if got := classicFindIDByName([]byte{}, "anything"); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

// ── fetchClassicGroupMemberIDs ────────────────────────────────────────────────

type mockHTTPClient struct {
	responses map[string]mockResponse
}

type mockResponse struct {
	body   []byte
	status int
	err    error
}

func (m *mockHTTPClient) Do(_ context.Context, _, path string, _ io.Reader) (*http.Response, error) {
	r, ok := m.responses[path]
	if !ok {
		r = mockResponse{status: 404}
	}
	if r.err != nil {
		return nil, r.err
	}
	return &http.Response{
		StatusCode: r.status,
		Body:       io.NopCloser(bytes.NewReader(r.body)),
	}, nil
}

func makeGroupXML(membersKey, memberKey string, ids []string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="UTF-8"?><group><%s>`, membersKey)
	for _, id := range ids {
		fmt.Fprintf(&b, `<%s><id>%s</id></%s>`, memberKey, id, memberKey)
	}
	fmt.Fprintf(&b, `</%s></group>`, membersKey)
	return []byte(b.String())
}

func TestFetchClassicGroupMemberIDs(t *testing.T) {
	const groupsPath = "/JSSResource/computergroups"

	listXML := makeClassicListXML([][2]string{{"42", "My Group"}})
	memberXML := makeGroupXML("computers", "computer", []string{"5", "28", "31"})
	emptyMemberXML := makeGroupXML("computers", "computer", nil)

	t.Run("resolves group by name and returns member IDs", func(t *testing.T) {
		client := &mockHTTPClient{responses: map[string]mockResponse{
			groupsPath:            {body: listXML, status: 200},
			groupsPath + "/id/42": {body: memberXML, status: 200},
		}}
		ids, err := fetchClassicGroupMemberIDs(context.Background(), client, groupsPath, "computers", "computer", "My Group")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"5", "28", "31"}
		if len(ids) != len(want) {
			t.Fatalf("got %v, want %v", ids, want)
		}
		for i, w := range want {
			if ids[i] != w {
				t.Errorf("[%d] got %q, want %q", i, ids[i], w)
			}
		}
	})

	t.Run("resolves group by numeric ID directly (skips list)", func(t *testing.T) {
		client := &mockHTTPClient{responses: map[string]mockResponse{
			groupsPath + "/id/42": {body: memberXML, status: 200},
		}}
		ids, err := fetchClassicGroupMemberIDs(context.Background(), client, groupsPath, "computers", "computer", "42")
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) != 3 {
			t.Fatalf("got %v, want 3 members", ids)
		}
	})

	t.Run("group not found by name returns error", func(t *testing.T) {
		client := &mockHTTPClient{responses: map[string]mockResponse{
			groupsPath: {body: listXML, status: 200},
		}}
		_, err := fetchClassicGroupMemberIDs(context.Background(), client, groupsPath, "computers", "computer", "Nonexistent Group")
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Errorf("got %v, want 'not found' error", err)
		}
	})

	t.Run("empty group returns empty slice", func(t *testing.T) {
		client := &mockHTTPClient{responses: map[string]mockResponse{
			groupsPath:            {body: listXML, status: 200},
			groupsPath + "/id/42": {body: emptyMemberXML, status: 200},
		}}
		ids, err := fetchClassicGroupMemberIDs(context.Background(), client, groupsPath, "computers", "computer", "My Group")
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) != 0 {
			t.Errorf("got %v, want empty", ids)
		}
	})
}

// ── resolveClassicLookupToID ──────────────────────────────────────────────────

func makeClassicDetailXML(directID, generalID string) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><resource>`)
	if directID != "" {
		fmt.Fprintf(&b, `<id>%s</id>`, directID)
	}
	if generalID != "" {
		fmt.Fprintf(&b, `<general><id>%s</id></general>`, generalID)
	}
	b.WriteString(`</resource>`)
	return []byte(b.String())
}

func TestResolveClassicLookupToID(t *testing.T) {
	const basePath = "/JSSResource/mobiledevices/serialnumber"

	t.Run("direct id element", func(t *testing.T) {
		client := &mockHTTPClient{responses: map[string]mockResponse{
			basePath + "/ABC123": {body: makeClassicDetailXML("99", ""), status: 200},
		}}
		got, err := resolveClassicLookupToID(context.Background(), client, basePath, "ABC123")
		if err != nil {
			t.Fatal(err)
		}
		if got != "99" {
			t.Errorf("got %q, want %q", got, "99")
		}
	})

	t.Run("nested general>id element", func(t *testing.T) {
		client := &mockHTTPClient{responses: map[string]mockResponse{
			basePath + "/XYZ789": {body: makeClassicDetailXML("", "55"), status: 200},
		}}
		got, err := resolveClassicLookupToID(context.Background(), client, basePath, "XYZ789")
		if err != nil {
			t.Fatal(err)
		}
		if got != "55" {
			t.Errorf("got %q, want %q", got, "55")
		}
	})

	t.Run("404 returns empty string no error", func(t *testing.T) {
		client := &mockHTTPClient{responses: map[string]mockResponse{
			basePath + "/NOTFOUND": {err: exitcode.New(exitcode.NotFound, "not found")},
		}}
		got, err := resolveClassicLookupToID(context.Background(), client, basePath, "NOTFOUND")
		if err != nil {
			t.Fatalf("got error %v, want nil", err)
		}
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("malformed XML returns empty string no error", func(t *testing.T) {
		client := &mockHTTPClient{responses: map[string]mockResponse{
			basePath + "/BAD": {body: []byte(`<not valid xml`), status: 200},
		}}
		got, err := resolveClassicLookupToID(context.Background(), client, basePath, "BAD")
		if err != nil {
			t.Fatalf("got error %v, want nil", err)
		}
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}
