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

type seqClient struct {
	queue []*http.Response
	calls []string
}

func (s *seqClient) Do(_ context.Context, method, url string, _ io.Reader) (*http.Response, error) {
	s.calls = append(s.calls, method+" "+url)
	if len(s.queue) == 0 {
		return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("empty"))}, nil
	}
	r := s.queue[0]
	s.queue = s.queue[1:]
	return r, nil
}

func jsonResp(status int, payload any) *http.Response {
	b, _ := json.Marshal(payload)
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(b)))}
}

func TestVerify_RunOneTemplate_OK(t *testing.T) {
	tmpl, ok := Lookup("encryption/not-encrypted")
	if !ok {
		t.Fatal("template missing")
	}
	client := &seqClient{
		queue: []*http.Response{
			jsonResp(201, map[string]any{"id": "555"}),
			jsonResp(200, map[string]any{}),
			jsonResp(200, map[string]any{"members": []int{1, 2, 3}}),
			jsonResp(204, map[string]any{}),
		},
	}
	result := RunOneVerification(context.Background(), client, tmpl, true)
	if result.Outcome != VerifyOK {
		t.Errorf("expected OK outcome, got %v (%s)", result.Outcome, result.Error)
	}
	if result.MemberCount != 3 {
		t.Errorf("expected 3 members, got %d", result.MemberCount)
	}
}

func TestVerify_RunOneTemplate_ZeroMatch(t *testing.T) {
	tmpl, _ := Lookup("compliance/firewall-disabled")
	client := &seqClient{
		queue: []*http.Response{
			jsonResp(201, map[string]any{"id": "777"}),
			jsonResp(200, map[string]any{}),
			jsonResp(200, map[string]any{"members": []int{}}),
			jsonResp(204, map[string]any{}),
		},
	}
	result := RunOneVerification(context.Background(), client, tmpl, true)
	if result.Outcome != VerifyZeroMatch {
		t.Errorf("expected ZeroMatch, got %v", result.Outcome)
	}
}

func TestVerify_RunOneTemplate_CreateError(t *testing.T) {
	tmpl, _ := Lookup("encryption/not-encrypted")
	client := &seqClient{
		queue: []*http.Response{
			jsonResp(400, map[string]any{"errors": []string{"invalid criterion name"}}),
		},
	}
	result := RunOneVerification(context.Background(), client, tmpl, true)
	if result.Outcome != VerifyError {
		t.Errorf("expected Error, got %v", result.Outcome)
	}
	if result.Error == "" {
		t.Error("expected non-empty Error message")
	}
}

func TestRunOneVerification_DeleteFailure(t *testing.T) {
	// Create succeeds, recalc succeeds, count succeeds — verify is OK.
	// DELETE returns 500 — Outcome stays VerifyOK, but Error captures the
	// cleanup failure so the caller (the CLI) can surface it as a warning.
	tmpl, _ := Lookup("encryption/not-encrypted")
	client := &seqClient{
		queue: []*http.Response{
			jsonResp(201, map[string]any{"id": "999"}),
			jsonResp(200, map[string]any{}),
			jsonResp(200, map[string]any{"members": []int{1, 2}}),
			jsonResp(500, map[string]any{"errors": []string{"boom"}}),
		},
	}
	result := RunOneVerification(context.Background(), client, tmpl, true)
	if result.Outcome != VerifyOK {
		t.Errorf("expected Outcome=OK (verify succeeded; only cleanup failed), got %v", result.Outcome)
	}
	if result.Error == "" {
		t.Error("expected Error to capture cleanup failure")
	}
	if !strings.Contains(result.Error, "cleanup failed") {
		t.Errorf("expected 'cleanup failed' in Error, got: %q", result.Error)
	}
	if result.MemberCount != 2 {
		t.Errorf("expected MemberCount=2, got %d", result.MemberCount)
	}
}

func TestVerify_NoCleanupSkipsDelete(t *testing.T) {
	tmpl, _ := Lookup("encryption/not-encrypted")
	client := &seqClient{
		queue: []*http.Response{
			jsonResp(201, map[string]any{"id": "888"}),
			jsonResp(200, map[string]any{}),
			jsonResp(200, map[string]any{"members": []int{1}}),
		},
	}
	_ = RunOneVerification(context.Background(), client, tmpl, false)
	for _, c := range client.calls {
		if strings.HasPrefix(c, "DELETE") {
			t.Errorf("did not expect DELETE call with cleanup=false: %s", c)
		}
	}
}
