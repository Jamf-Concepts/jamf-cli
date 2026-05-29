// Copyright 2026, Jamf Software LLC

package generated

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// versionFallbackMockClient records calls and returns pre-programmed responses.
type versionFallbackMockClient struct {
	calls    []string
	response func(path string) (*http.Response, error)
}

func (m *versionFallbackMockClient) Do(_ context.Context, _, path string, _ io.Reader) (*http.Response, error) {
	m.calls = append(m.calls, path)
	return m.response(path)
}

func ok200() (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{}`)),
	}, nil
}

func err404() (*http.Response, error) {
	return nil, exitcode.New(exitcode.NotFound, "resource not found (HTTP 404)")
}

func errOther() (*http.Response, error) {
	return nil, exitcode.New(exitcode.General, "server error (HTTP 500)")
}

// TestVersionFallback_PrimarySucceeds verifies no fallback attempt when primary works.
func TestVersionFallback_PrimarySucceeds(t *testing.T) {
	mock := &versionFallbackMockClient{response: func(path string) (*http.Response, error) {
		return ok200()
	}}
	vft := newVersionFallback("/v3/foo")
	resp, err := vft.do(mock, context.Background(), "GET", "/v3/foo?page=0", nil, []string{"/v2/foo"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()
	if len(mock.calls) != 1 {
		t.Errorf("expected 1 call, got %d: %v", len(mock.calls), mock.calls)
	}
	if vft.activePath != "/v3/foo" {
		t.Errorf("activePath should be primary, got %q", vft.activePath)
	}
}

// TestVersionFallback_FallsBackOn404 verifies retry on 404 with correct warning.
func TestVersionFallback_FallsBackOn404(t *testing.T) {
	mock := &versionFallbackMockClient{response: func(path string) (*http.Response, error) {
		if strings.HasPrefix(path, "/v3/") {
			return err404()
		}
		return ok200()
	}}
	vft := newVersionFallback("/v3/foo")
	resp, err := vft.do(mock, context.Background(), "GET", "/v3/foo?page=0", nil, []string{"/v2/foo"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()
	if len(mock.calls) != 2 {
		t.Errorf("expected 2 calls (primary + fallback), got %d: %v", len(mock.calls), mock.calls)
	}
	if !strings.HasPrefix(mock.calls[1], "/v2/") {
		t.Errorf("fallback call should use /v2/, got %q", mock.calls[1])
	}
	if vft.activePath != "/v2/foo" {
		t.Errorf("activePath should be fallback after resolution, got %q", vft.activePath)
	}
	if !vft.warned {
		t.Error("warned should be true after successful fallback")
	}
}

// TestVersionFallback_ReusesResolvedPath verifies pagination reuse (no re-probe).
func TestVersionFallback_ReusesResolvedPath(t *testing.T) {
	callCount := 0
	mock := &versionFallbackMockClient{response: func(path string) (*http.Response, error) {
		callCount++
		if strings.HasPrefix(path, "/v3/") {
			return err404()
		}
		return ok200()
	}}
	vft := newVersionFallback("/v3/foo")
	fallbacks := []string{"/v2/foo"}

	// First call: resolves to /v2
	resp, err := vft.do(mock, context.Background(), "GET", "/v3/foo?page=0&page-size=100", nil, fallbacks)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	_ = resp.Body.Close()

	// Second call (next page): should go directly to /v2, no /v3 probe
	resp2, err := vft.do(mock, context.Background(), "GET", "/v3/foo?page=1&page-size=100", nil, fallbacks)
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	_ = resp2.Body.Close()

	// Calls: 1 (v3 probe) + 1 (v2 success) + 1 (v2 direct for page 1) = 3
	if callCount != 3 {
		t.Errorf("expected 3 HTTP calls, got %d: %v", callCount, mock.calls)
	}
	if !strings.Contains(mock.calls[2], "/v2/foo?page=1") {
		t.Errorf("second page should go directly to /v2, got %q", mock.calls[2])
	}
}

// TestVersionFallback_AllFallbacksExhausted returns original 404 when none work.
func TestVersionFallback_AllFallbacksExhausted(t *testing.T) {
	mock := &versionFallbackMockClient{response: func(_ string) (*http.Response, error) {
		return err404()
	}}
	vft := newVersionFallback("/v3/foo")
	_, err := vft.do(mock, context.Background(), "GET", "/v3/foo", nil, []string{"/v2/foo", "/v1/foo"})
	if err == nil {
		t.Fatal("expected error when all paths return 404")
	}
	if !versionFallbackIs404(err) {
		t.Errorf("expected 404 error, got %v", err)
	}
	if len(mock.calls) != 3 {
		t.Errorf("expected 3 calls (primary + 2 fallbacks), got %d: %v", len(mock.calls), mock.calls)
	}
}

// TestVersionFallback_NonNotFoundErrorStopsImmediately verifies non-404 halts chain.
func TestVersionFallback_NonNotFoundErrorStopsImmediately(t *testing.T) {
	mock := &versionFallbackMockClient{response: func(path string) (*http.Response, error) {
		if strings.HasPrefix(path, "/v3/") {
			return err404()
		}
		return errOther()
	}}
	vft := newVersionFallback("/v3/foo")
	_, err := vft.do(mock, context.Background(), "GET", "/v3/foo", nil, []string{"/v2/foo"})
	if err == nil {
		t.Fatal("expected error from non-404 fallback")
	}
	if versionFallbackIs404(err) {
		t.Errorf("expected non-404 error to propagate, got 404")
	}
}

// TestVersionFallbackApply verifies version prefix replacement.
func TestVersionFallbackApply(t *testing.T) {
	tests := []struct {
		primaryTemplate  string
		fallbackTemplate string
		actualPath       string
		want             string
	}{
		{"/v3/foo/{id}", "/v2/foo", "/v3/foo/123", "/v2/foo/123"},
		{"/v3/foo", "/v2/foo", "/v3/foo?page=0&page-size=100", "/v2/foo?page=0&page-size=100"},
		{"/v1/auth", "/auth", "/v1/auth", "/auth"},
		{"/v2/groups/{id}", "/v1/groups", "/v2/groups/456", "/v1/groups/456"},
	}
	for _, tc := range tests {
		got := versionFallbackApply(tc.primaryTemplate, tc.fallbackTemplate, tc.actualPath)
		if got != tc.want {
			t.Errorf("versionFallbackApply(%q, %q, %q) = %q, want %q",
				tc.primaryTemplate, tc.fallbackTemplate, tc.actualPath, got, tc.want)
		}
	}
}

// Compile-time check: versionFallbackMockClient satisfies registry.HTTPClient.
var _ registry.HTTPClient = (*versionFallbackMockClient)(nil)
