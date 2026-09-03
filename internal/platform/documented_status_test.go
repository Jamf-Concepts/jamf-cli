// Copyright 2026, Jamf Software LLC

package platform

import (
	"testing"

	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
)

const searchDomainNotSet = `{"httpStatus":404,"traceId":"abc123","errors":[{"code":"SEARCH_DOMAIN_NOT_SET","description":"No search domain configured."}]}`

func notSetErr() *jamfplatform.APIResponseError {
	return &jamfplatform.APIResponseError{
		StatusCode: 404,
		Body:       searchDomainNotSet,
		TraceID:    "abc123",
		Errors:     []jamfplatform.ErrorDetail{{Code: "SEARCH_DOMAIN_NOT_SET", Description: "No search domain configured."}},
	}
}

func TestMatchesDocumented(t *testing.T) {
	tests := []struct {
		name       string
		err        *jamfplatform.APIResponseError
		documented []DocumentedStatus
		want       bool
	}{
		{
			name:       "status and error code both match",
			err:        notSetErr(),
			documented: []DocumentedStatus{{Code: 404, ErrorCode: "SEARCH_DOMAIN_NOT_SET", Empty: true}},
			want:       true,
		},
		{
			// The reason the error code is part of the match: a 404 that
			// arrives for any other reason must still be a failure, or a
			// mistyped path renders as an empty answer with exit 0.
			name: "same status, different error code, stays a failure",
			err: &jamfplatform.APIResponseError{
				StatusCode: 404,
				Errors:     []jamfplatform.ErrorDetail{{Code: "ZONE_NOT_FOUND"}},
			},
			documented: []DocumentedStatus{{Code: 404, ErrorCode: "SEARCH_DOMAIN_NOT_SET", Empty: true}},
			want:       false,
		},
		{
			name: "right code under a different status stays a failure",
			err: &jamfplatform.APIResponseError{
				StatusCode: 500,
				Errors:     []jamfplatform.ErrorDetail{{Code: "SEARCH_DOMAIN_NOT_SET"}},
			},
			documented: []DocumentedStatus{{Code: 404, ErrorCode: "SEARCH_DOMAIN_NOT_SET"}},
			want:       false,
		},
		{
			name:       "an empty error code matches on status alone",
			err:        &jamfplatform.APIResponseError{StatusCode: 404},
			documented: []DocumentedStatus{{Code: 404}},
			want:       true,
		},
		{
			name:       "no documented statuses never matches",
			err:        notSetErr(),
			documented: nil,
			want:       false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, got := matchesDocumented(tt.err, tt.documented); got != tt.want {
				t.Errorf("matchesDocumented() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMatchesDocumented_ReturnsTheMatchedEntry checks the caller can tell an
// empty state from a status whose body is the answer — they render differently.
func TestMatchesDocumented_ReturnsTheMatchedEntry(t *testing.T) {
	documented := []DocumentedStatus{
		{Code: 403, ErrorCode: "MISSING_PRIVILEGES"},
		{Code: 404, ErrorCode: "SEARCH_DOMAIN_NOT_SET", Empty: true},
	}
	match, ok := matchesDocumented(notSetErr(), documented)
	if !ok {
		t.Fatal("expected the 404 entry to match")
	}
	if !match.Empty {
		t.Error("expected the matched entry to be the Empty one, so an empty object renders rather than the error envelope")
	}
}

// TestDocumentedEmptyStateRendersEmptyObject pins the rendering decision: the
// server says "not configured" with an error envelope carrying a per-call
// traceId. Rendering that envelope would make one command emit two unrelated
// schemas depending on state, and make two identical reads differ.
func TestDocumentedEmptyStateRendersEmptyObject(t *testing.T) {
	var result any
	if err := renderDocumented(notSetErr(), DocumentedStatus{Code: 404, ErrorCode: "SEARCH_DOMAIN_NOT_SET", Empty: true}, &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected an empty object, got %#v", result)
	}
	if len(m) != 0 {
		t.Errorf("expected the object to be empty, got %#v", m)
	}
}

// TestDocumentedAnswerBodyIsDecoded covers the other kind of documented status —
// one whose body genuinely is the answer, as the Jamf Pro privilege-check
// endpoint's 403 is. There the body must survive.
func TestDocumentedAnswerBodyIsDecoded(t *testing.T) {
	apiErr := &jamfplatform.APIResponseError{
		StatusCode: 403,
		Body:       `{"missing":["read:foo"]}`,
		Errors:     []jamfplatform.ErrorDetail{{Code: "MISSING_PRIVILEGES"}},
	}
	var result any
	if err := renderDocumented(apiErr, DocumentedStatus{Code: 403, ErrorCode: "MISSING_PRIVILEGES"}, &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected the body decoded into an object, got %#v", result)
	}
	if _, ok := m["missing"]; !ok {
		t.Errorf("expected the answer body preserved, got %#v", m)
	}
}
