// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// deviceResolveMockClient routes responses based on the full path (including
// query params) so we can differentiate serial-number vs name RSQL searches.
type deviceResolveMockClient struct {
	// handler is called with the full path; return statusCode + body.
	handler func(method, path string) (int, string, error)
}

func (m *deviceResolveMockClient) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	code, body, err := m.handler(method, path)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: code,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

func TestResolveDeviceByIdentifier_ByID(t *testing.T) {
	client := &deviceResolveMockClient{
		handler: func(_, path string) (int, string, error) {
			if path == "/v1/computers-inventory-detail/42" {
				return 200, `{"id":"42","general":{"name":"MacBook-Lab1"}}`, nil
			}
			return 0, "", fmt.Errorf("unexpected path: %s", path)
		},
	}

	id, name, err := resolveDeviceByIdentifier(context.Background(), client, "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "42" {
		t.Errorf("id = %q, want %q", id, "42")
	}
	if name != "MacBook-Lab1" {
		t.Errorf("name = %q, want %q", name, "MacBook-Lab1")
	}
}

func TestResolveDeviceByIdentifier_BySerial(t *testing.T) {
	client := &deviceResolveMockClient{
		handler: func(_, path string) (int, string, error) {
			if strings.HasPrefix(path, "/v1/computers-inventory-detail/") {
				return 404, `{"errors":[]}`, nil
			}
			if strings.Contains(path, "hardware.serialNumber") {
				return 200, `{"totalCount":1,"results":[{"id":"99","general":{"name":"MacBook-Serial"}}]}`, nil
			}
			return 0, "", fmt.Errorf("unexpected path: %s", path)
		},
	}

	id, name, err := resolveDeviceByIdentifier(context.Background(), client, "C02X1234")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "99" {
		t.Errorf("id = %q, want %q", id, "99")
	}
	if name != "MacBook-Serial" {
		t.Errorf("name = %q, want %q", name, "MacBook-Serial")
	}
}

func TestResolveDeviceByIdentifier_ByName(t *testing.T) {
	client := &deviceResolveMockClient{
		handler: func(_, path string) (int, string, error) {
			if strings.HasPrefix(path, "/v1/computers-inventory-detail/") {
				return 404, `{"errors":[]}`, nil
			}
			if strings.Contains(path, "hardware.serialNumber") {
				return 200, `{"totalCount":0,"results":[]}`, nil
			}
			if strings.Contains(path, "general.name") {
				return 200, `{"totalCount":1,"results":[{"id":"7","general":{"name":"MacBook-Lab1"}}]}`, nil
			}
			return 0, "", fmt.Errorf("unexpected path: %s", path)
		},
	}

	id, name, err := resolveDeviceByIdentifier(context.Background(), client, "MacBook-Lab1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "7" {
		t.Errorf("id = %q, want %q", id, "7")
	}
	if name != "MacBook-Lab1" {
		t.Errorf("name = %q, want %q", name, "MacBook-Lab1")
	}
}

func TestResolveDeviceByIdentifier_NotFound(t *testing.T) {
	client := &deviceResolveMockClient{
		handler: func(_, path string) (int, string, error) {
			if strings.HasPrefix(path, "/v1/computers-inventory-detail/") {
				return 404, `{"errors":[]}`, nil
			}
			// Both serial and name searches return 0 results
			return 200, `{"totalCount":0,"results":[]}`, nil
		},
	}

	_, _, err := resolveDeviceByIdentifier(context.Background(), client, "ghost-machine")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no device found") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "no device found")
	}
}

func TestResolveDeviceByIdentifier_MultipleMatches(t *testing.T) {
	client := &deviceResolveMockClient{
		handler: func(_, path string) (int, string, error) {
			if strings.HasPrefix(path, "/v1/computers-inventory-detail/") {
				return 404, `{"errors":[]}`, nil
			}
			if strings.Contains(path, "hardware.serialNumber") {
				return 200, `{"totalCount":0,"results":[]}`, nil
			}
			if strings.Contains(path, "general.name") {
				return 200, `{"totalCount":3,"results":[{"id":"1","general":{"name":"MacBook"}},{"id":"2","general":{"name":"MacBook"}},{"id":"3","general":{"name":"MacBook"}}]}`, nil
			}
			return 0, "", fmt.Errorf("unexpected path: %s", path)
		},
	}

	_, _, err := resolveDeviceByIdentifier(context.Background(), client, "MacBook")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "multiple devices match") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "multiple devices match")
	}
}
