// Copyright 2026, Jamf Software LLC

package smartgroup

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type fakeHTTPClient struct {
	resp *http.Response
	err  error
	url  string
}

func (f *fakeHTTPClient) Do(_ context.Context, _ string, url string, _ io.Reader) (*http.Response, error) {
	f.url = url
	return f.resp, f.err
}

func makeJSON(t *testing.T, v any) *http.Response {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(string(b))),
	}
}

func TestCountMembers_PopulatedGroup(t *testing.T) {
	resp := makeJSON(t, map[string]any{"members": []int{1, 2, 3, 4, 5}})
	client := &fakeHTTPClient{resp: resp}
	n, err := CountMembers(context.Background(), client, "287")
	if err != nil {
		t.Fatalf("CountMembers: %v", err)
	}
	if n != 5 {
		t.Fatalf("expected 5, got %d", n)
	}
	wantPath := "/v2/computer-groups/smart-group-membership/287"
	if !strings.Contains(client.url, wantPath) {
		t.Fatalf("expected URL to contain %q, got %q", wantPath, client.url)
	}
}

func TestCountMembers_EmptyGroup(t *testing.T) {
	resp := makeJSON(t, map[string]any{"members": []int{}})
	n, err := CountMembers(context.Background(), &fakeHTTPClient{resp: resp}, "1")
	if err != nil {
		t.Fatalf("CountMembers: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}

func TestCountMembers_HTTPError(t *testing.T) {
	resp := &http.Response{
		StatusCode: 404,
		Body:       io.NopCloser(strings.NewReader(`{"errors":["not found"]}`)),
	}
	_, err := CountMembers(context.Background(), &fakeHTTPClient{resp: resp}, "999")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}
