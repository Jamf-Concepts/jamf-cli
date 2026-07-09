// Copyright 2026, Jamf Software LLC

package generated

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// resolveNameMockClient returns a fixed body for every request, letting the
// name-resolution helpers be driven with synthetic list responses.
type resolveNameMockClient struct {
	body   string
	status int
}

func (m *resolveNameMockClient) Do(_ context.Context, _, _ string, _ io.Reader) (*http.Response, error) {
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
