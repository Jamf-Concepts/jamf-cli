// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// versionMockClient returns a fixed JSON body for every call.
type versionMockClient struct {
	body   string
	called bool
	err    error
}

func (m *versionMockClient) Do(_ context.Context, _, _ string, _ io.Reader) (*http.Response, error) {
	m.called = true
	if m.err != nil {
		return nil, m.err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(m.body)),
		Header:     make(http.Header),
	}, nil
}

func TestCheckTenantVersion_WarnsBelowSpec(t *testing.T) {
	mock := &versionMockClient{body: `{"version":"11.27.0"}`}
	var buf bytes.Buffer
	checkTenantVersion(mock, "11.28.0", "", &buf)
	if !strings.Contains(buf.String(), "11.27.0") {
		t.Errorf("expected warning mentioning tenant version; got %q", buf.String())
	}
}

func TestCheckTenantVersion_SilentAtSpec(t *testing.T) {
	mock := &versionMockClient{body: `{"version":"11.28.0"}`}
	var buf bytes.Buffer
	checkTenantVersion(mock, "11.28.0", "", &buf)
	if buf.Len() > 0 {
		t.Errorf("expected no warning for matching version; got %q", buf.String())
	}
}

func TestCheckTenantVersion_SilentAboveSpec(t *testing.T) {
	mock := &versionMockClient{body: `{"version":"11.29.0"}`}
	var buf bytes.Buffer
	checkTenantVersion(mock, "11.28.0", "", &buf)
	if buf.Len() > 0 {
		t.Errorf("expected no warning for newer tenant; got %q", buf.String())
	}
}

func TestCheckTenantVersion_SilentOnUnknownSpec(t *testing.T) {
	mock := &versionMockClient{body: `{"version":"11.27.0"}`}
	var buf bytes.Buffer
	checkTenantVersion(mock, "unknown", "", &buf)
	if mock.called {
		t.Error("should not probe when specVersion is unknown")
	}
	if buf.Len() > 0 {
		t.Errorf("expected no output; got %q", buf.String())
	}
}

func TestCheckTenantVersion_SilentOnHTTPError(t *testing.T) {
	mock := &versionMockClient{err: &mockHTTPError{}}
	var buf bytes.Buffer
	checkTenantVersion(mock, "11.28.0", "", &buf)
	if buf.Len() > 0 {
		t.Errorf("expected no output on HTTP error; got %q", buf.String())
	}
}

func TestCheckTenantVersion_UsesCache(t *testing.T) {
	dir := t.TempDir()
	// Point the cache at a temp dir by overriding XDG_CONFIG_HOME.
	t.Setenv("XDG_CONFIG_HOME", dir)

	// Seed the cache with a fresh, below-spec entry.
	profile := "test-profile"
	cache := map[string]versionCacheEntry{
		profile: {Version: "11.27.0", CheckedAt: time.Now().UTC()},
	}
	data, _ := json.Marshal(cache)
	cacheFile := filepath.Join(dir, "jamf-cli", ".version-cache.json")
	_ = os.MkdirAll(filepath.Dir(cacheFile), 0o755)
	_ = os.WriteFile(cacheFile, data, 0o600)

	mock := &versionMockClient{body: `{"version":"11.99.0"}`}
	var buf bytes.Buffer
	checkTenantVersion(mock, "11.28.0", profile, &buf)

	if mock.called {
		t.Error("expected cache hit; HTTP client should not have been called")
	}
	if !strings.Contains(buf.String(), "11.27.0") {
		t.Errorf("expected warning from cached version; got %q", buf.String())
	}
}

func TestCheckTenantVersion_SkipsStaleCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	// Seed the cache with a stale entry (25 hours old).
	profile := "test-profile"
	cache := map[string]versionCacheEntry{
		profile: {Version: "11.28.0", CheckedAt: time.Now().UTC().Add(-25 * time.Hour)},
	}
	data, _ := json.Marshal(cache)
	cacheFile := filepath.Join(dir, "jamf-cli", ".version-cache.json")
	_ = os.MkdirAll(filepath.Dir(cacheFile), 0o755)
	_ = os.WriteFile(cacheFile, data, 0o600)

	// Return a below-spec version to confirm a live probe fires.
	mock := &versionMockClient{body: `{"version":"11.27.0"}`}
	var buf bytes.Buffer
	checkTenantVersion(mock, "11.28.0", profile, &buf)

	if !mock.called {
		t.Error("expected stale cache to trigger a live probe")
	}
	if !strings.Contains(buf.String(), "11.27.0") {
		t.Errorf("expected warning after live probe; got %q", buf.String())
	}
}

// mockHTTPError is a minimal error type for HTTP failure simulation.
type mockHTTPError struct{}

func (e *mockHTTPError) Error() string { return "mock HTTP error" }

func TestCompareProVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"11.28.0", "11.28.0", 0},
		{"11.27.0", "11.28.0", -1},
		{"11.29.0", "11.28.0", 1},
		{"10.50.0", "11.0.0", -1},
		{"11.28.1", "11.28.0", 1},
		{"11.28.0-t1234", "11.28.0", 0}, // suffix stripped
		{"11.28.0", "11.28.0-t1234", 0}, // suffix stripped
		{"11.27.0-t9", "11.28.0", -1},   // suffix stripped before compare
		{"unknown", "11.28.0", -1},      // unparseable treats as 0.0.0
		{"11.28.0", "unknown", 1},
	}
	for _, tc := range tests {
		got := compareProVersions(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("compareProVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
