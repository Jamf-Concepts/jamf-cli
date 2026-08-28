// Copyright 2026, Jamf Software LLC

package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

func TestDo_ModernAPIPathPrefix(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))

	_, err := c.Do(context.Background(), "GET", "/v1/buildings", nil)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	if gotPath != "/api/v1/buildings" {
		t.Errorf("path = %q, want %q", gotPath, "/api/v1/buildings")
	}
}

func TestDo_ClassicAPIPathNoPrefix(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))

	_, err := c.Do(context.Background(), "GET", "/JSSResource/policies", nil)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	if gotPath != "/JSSResource/policies" {
		t.Errorf("path = %q, want %q", gotPath, "/JSSResource/policies")
	}
}

func TestDo_ExplicitAPIPrefix(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))

	_, err := c.Do(context.Background(), "GET", "/api/v1/buildings", nil)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	if gotPath != "/api/v1/buildings" {
		t.Errorf("path = %q, want %q", gotPath, "/api/v1/buildings")
	}
}

func TestDo_SetsJSONHeaders_ModernAPI(t *testing.T) {
	var gotAccept, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))

	body := strings.NewReader(`{"name":"test"}`)
	_, err := c.Do(context.Background(), "POST", "/v1/buildings", body)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want %q", gotAccept, "application/json")
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", gotContentType, "application/json")
	}
}

func TestDo_SetsXMLHeaders_ClassicAPI(t *testing.T) {
	var gotAccept, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<policy/>"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))

	body := strings.NewReader(`<policy><name>Test</name></policy>`)
	_, err := c.Do(context.Background(), "POST", "/JSSResource/policies/id/0", body)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	if gotAccept != "application/xml" {
		t.Errorf("Accept = %q, want %q", gotAccept, "application/xml")
	}
	if gotContentType != "application/xml" {
		t.Errorf("Content-Type = %q, want %q", gotContentType, "application/xml")
	}
}

func TestDo_SetsXMLHeaders_ClassicAPIGet(t *testing.T) {
	var gotAccept, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<policies/>"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))

	_, err := c.Do(context.Background(), "GET", "/JSSResource/policies", nil)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	if gotAccept != "application/xml" {
		t.Errorf("Accept = %q, want %q", gotAccept, "application/xml")
	}
	if gotContentType != "" {
		t.Errorf("Content-Type should be empty for GET, got %q", gotContentType)
	}
}

func TestDo_SetsXMLHeaders_PlatformGatewayClassic(t *testing.T) {
	var gotAccept, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<policy/>"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"), WithGatewayScope(auth.TenantScope("tid")))

	body := strings.NewReader(`<policy><name>Test</name></policy>`)
	_, err := c.Do(context.Background(), "POST", "/JSSResource/policies/id/0", body)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	// After gateway rewrite, path is /api/proclassic/... — should still get XML headers
	if gotAccept != "application/xml" {
		t.Errorf("Accept = %q, want %q", gotAccept, "application/xml")
	}
	if gotContentType != "application/xml" {
		t.Errorf("Content-Type = %q, want %q", gotContentType, "application/xml")
	}
}

func TestDo_SetsBearerAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("my-secret-token"))

	_, err := c.Do(context.Background(), "GET", "/JSSResource/policies", nil)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	if gotAuth != "Bearer my-secret-token" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer my-secret-token")
	}
}

func TestDo_ClassicAPIWithBody(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id": 1}`))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))

	input := `{"policy":{"name":"Test Policy"}}`
	resp, err := c.Do(context.Background(), "POST", "/JSSResource/policies/id/0", strings.NewReader(input))
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if gotBody != input {
		t.Errorf("body = %q, want %q", gotBody, input)
	}
}

// --- Error code mapping tests ---

func TestDo_Forbidden403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("insufficient privileges"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))
	_, err := c.Do(context.Background(), "GET", "/v1/buildings", nil)
	if err == nil {
		t.Fatal("expected error for 403")
		return
	}

	code := exitcode.CodeFrom(err)
	if code != exitcode.PermissionDenied {
		t.Errorf("exit code = %d, want %d (PermissionDenied)", code, exitcode.PermissionDenied)
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("error = %q, want to contain 'permission denied'", err.Error())
	}
}

func TestDo_NotFound404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))
	_, err := c.Do(context.Background(), "GET", "/v1/buildings/999", nil)
	if err == nil {
		t.Fatal("expected error for 404")
		return
	}

	code := exitcode.CodeFrom(err)
	if code != exitcode.NotFound {
		t.Errorf("exit code = %d, want %d (NotFound)", code, exitcode.NotFound)
	}
	if !strings.Contains(err.Error(), "GET") || !strings.Contains(err.Error(), "/buildings/999") {
		t.Errorf("404 error should contain method and path, got: %q", err.Error())
	}
}

func TestDo_ServerError500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))
	_, err := c.Do(context.Background(), "GET", "/v1/buildings", nil)
	if err == nil {
		t.Fatal("expected error for 500")
		return
	}

	code := exitcode.CodeFrom(err)
	if code != exitcode.General {
		t.Errorf("exit code = %d, want %d (General)", code, exitcode.General)
	}
	if !strings.Contains(err.Error(), "internal server error") {
		t.Errorf("500 error should contain body, got: %q", err.Error())
	}
}

func TestDo_Unauthorized401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("invalid token"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("bad-token"))
	_, err := c.Do(context.Background(), "GET", "/v1/buildings", nil)
	if err == nil {
		t.Fatal("expected error for 401")
		return
	}

	code := exitcode.CodeFrom(err)
	if code != exitcode.Authentication {
		t.Errorf("exit code = %d, want %d (Authentication)", code, exitcode.Authentication)
	}
}

func TestDo_RateLimited429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("too many requests"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))
	_, err := c.Do(context.Background(), "GET", "/v1/buildings", nil)
	if err == nil {
		t.Fatal("expected error for 429")
		return
	}

	code := exitcode.CodeFrom(err)
	if code != exitcode.RateLimited {
		t.Errorf("exit code = %d, want %d (RateLimited)", code, exitcode.RateLimited)
	}
}

// --- Verbose output tests ---

func TestDo_VerboseOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"), WithVerbose(1))

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	_, err := c.Do(context.Background(), "GET", "/v1/buildings", nil)

	_ = w.Close()
	os.Stderr = oldStderr

	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	output, _ := io.ReadAll(r)
	out := string(output)

	if !strings.Contains(out, "-->") {
		t.Errorf("verbose output should contain '-->' request line, got: %q", out)
	}
	if !strings.Contains(out, "<--") {
		t.Errorf("verbose output should contain '<--' response line, got: %q", out)
	}
	if !strings.Contains(out, "GET") {
		t.Errorf("verbose output should contain method, got: %q", out)
	}
}

func TestWithVerbose_SetsField(t *testing.T) {
	c := New("https://example.com", auth.NewTokenProvider("tok"))
	if c.verboseLevel != 0 {
		t.Error("verboseLevel should default to 0")
	}

	c = New("https://example.com", auth.NewTokenProvider("tok"), WithVerbose(1))
	if c.verboseLevel != 1 {
		t.Error("WithVerbose(1) should set verboseLevel to 1")
	}
}

// --- Retry logic tests ---

func TestDoWithRetry_SucceedsAfterFailure(t *testing.T) {
	var counter int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&counter, 1)
		if n == 1 {
			// First request: hang up (force connection reset)
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("server does not support hijacking")
				return
			}
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
			return
		}
		// Second request: succeed
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))
	resp, err := c.Do(context.Background(), "GET", "/v1/buildings", nil)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if atomic.LoadInt32(&counter) != 2 {
		t.Errorf("expected 2 attempts, got %d", counter)
	}
}

func TestDoWithRetry_RateLimitRetry(t *testing.T) {
	var counter int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&counter, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))
	resp, err := c.Do(context.Background(), "GET", "/v1/buildings", nil)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if atomic.LoadInt32(&counter) != 2 {
		t.Errorf("expected 2 attempts (429 then success), got %d", counter)
	}
}

func TestDoWithRetry_ExhaustsRetries(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow retry exhaustion test")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always hang up
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("server does not support hijacking")
			return
		}
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))
	_, err := c.Do(context.Background(), "GET", "/v1/buildings", nil)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
		return
	}

	if !strings.Contains(err.Error(), fmt.Sprintf("after %d retries", 3)) {
		t.Errorf("error should mention retry count, got: %q", err.Error())
	}
}

// --- Platform gateway path rewriting tests ---

func TestRewritePathForGateway_ClassicAPI(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/JSSResource/computers", "/proclassic/computers"},
		{"/JSSResource/policies/id/5", "/proclassic/policies/id/5"},
		{"/JSSResource/mobiledevices", "/proclassic/mobiledevices"},
	}
	for _, tt := range tests {
		got := rewritePathForGateway(tt.input)
		if got != tt.want {
			t.Errorf("rewritePathForGateway(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRewritePathForGateway_ModernAPI(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/api/v1/buildings", "/pro/v1/buildings"},
		{"/api/v2/mobile-devices", "/pro/v2/mobile-devices"},
		{"/api/v1/accounts/userid/1", "/pro/v1/accounts/userid/1"},
		{"/api/preview/computers", "/pro/preview/computers"},
	}
	for _, tt := range tests {
		got := rewritePathForGateway(tt.input)
		if got != tt.want {
			t.Errorf("rewritePathForGateway(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRewritePathForGateway_NoRewrite(t *testing.T) {
	// Paths that don't match /api/ or /JSSResource pass through unchanged
	tests := []string{
		"/auth/token",
		"/healthCheck.html",
	}
	for _, input := range tests {
		got := rewritePathForGateway(input)
		if got != input {
			t.Errorf("rewritePathForGateway(%q) = %q, want unchanged", input, got)
		}
	}
}

func TestDo_PlatformGateway_ClassicPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"), WithGatewayScope(auth.TenantScope("tenant-uuid")))
	_, err := c.Do(context.Background(), "GET", "/JSSResource/computers", nil)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	want := "/proclassic/computers"
	if gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestDo_PlatformGateway_ModernPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"), WithGatewayScope(auth.TenantScope("tenant-uuid")))
	// Path without /api prefix — gets /api prepended, then the gateway
	// rewrite replaces that segment with the namespace.
	_, err := c.Do(context.Background(), "GET", "/v1/buildings", nil)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	want := "/pro/v1/buildings"
	if gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestDo_PlatformGateway_ExplicitAPIPrefix(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"), WithGatewayScope(auth.TenantScope("tenant-uuid")))
	_, err := c.Do(context.Background(), "GET", "/api/v2/users", nil)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	want := "/pro/v2/users"
	if gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestWithGatewayScope_SetsScope(t *testing.T) {
	c := New("https://example.com", auth.NewTokenProvider("tok"))
	if c.gateway {
		t.Error("gateway mode should default off: a direct-to-instance client must not rewrite paths")
	}

	c = New("https://example.com", auth.NewTokenProvider("tok"), WithGatewayScope(auth.TenantScope("my-tenant")))
	if !c.gateway {
		t.Error("gateway mode should be on")
	}
	if name, value := c.scope.Header(); name != "X-Tenant-Id" || value != "my-tenant" {
		t.Errorf("scope header = (%q, %q), want (X-Tenant-Id, my-tenant)", name, value)
	}

	// Organization scope is gateway mode with no header at all — the gateway
	// resolves it from the access token — so the absence of an ID must not turn
	// gateway mode back off.
	c = New("https://example.com", auth.NewTokenProvider("tok"), WithGatewayScope(auth.Scope{}))
	if !c.gateway {
		t.Error("organization scope is still gateway mode")
	}
	if name, _ := c.scope.Header(); name != "" {
		t.Errorf("organization scope sent header %q, want none", name)
	}
}

// TestGatewayScopeHeaderPerKind pins which header each level travels in, and
// that an organization-scoped client sends neither. Crossing them over is a 403
// OWNERSHIP_FORBIDDEN even within one customer, so sending the wrong one is not
// a cosmetic error.
func TestGatewayScopeHeaderPerKind(t *testing.T) {
	cases := []struct {
		name       string
		scope      auth.Scope
		wantHeader string
	}{
		{name: "environment", scope: auth.EnvironmentScope("env-1"), wantHeader: "X-Environment-Id"},
		{name: "tenant", scope: auth.TenantScope("ten-1"), wantHeader: "X-Tenant-Id"},
		{name: "organization", scope: auth.Scope{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got http.Header
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Clone()
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			defer srv.Close()

			c := New(srv.URL, auth.NewTokenProvider("tok"), WithGatewayScope(tc.scope))
			if _, err := c.Do(context.Background(), "GET", "/v1/buildings", nil); err != nil {
				t.Fatalf("Do: %v", err)
			}
			for _, h := range []string{"X-Environment-Id", "X-Tenant-Id"} {
				want := ""
				if h == tc.wantHeader {
					want = tc.scope.ID
				}
				if got.Get(h) != want {
					t.Errorf("%s = %q, want %q", h, got.Get(h), want)
				}
			}
		})
	}
}

// --- Upload tests ---

func TestUpload_NonSeekable_NoRetry(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("slow down"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))

	// A strings.Reader is a Seeker, so wrap it to hide that interface.
	body := io.NopCloser(strings.NewReader("some-content"))
	_, err := c.Upload(context.Background(), "/v1/packages/1/upload", body, "application/octet-stream", 12)
	if err == nil {
		t.Fatal("expected error on 429")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 (non-seekable should not retry)", got)
	}
}

func TestUpload_Seekable_RetriesOn429(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Drain body so upload reader is exhausted each attempt.
		_, _ = io.Copy(io.Discard, r.Body)
		n := atomic.AddInt32(&attempts, 1)
		if n < 2 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("slow down"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))

	body := strings.NewReader("retry-me-please") // io.ReadSeeker
	resp, err := c.Upload(context.Background(), "/v1/packages/1/upload", body, "application/octet-stream", int64(body.Len()))
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Errorf("attempts = %d, want 2 (should retry seekable body)", got)
	}
}

func TestUpload_Seekable_RewindsBetweenAttempts(t *testing.T) {
	var bodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, b)
		if len(bodies) < 2 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))

	payload := "the-quick-brown-fox"
	body := strings.NewReader(payload)
	resp, err := c.Upload(context.Background(), "/v1/packages/1/upload", body, "application/octet-stream", int64(body.Len()))
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if len(bodies) != 2 {
		t.Fatalf("expected 2 request bodies, got %d", len(bodies))
	}
	if string(bodies[0]) != payload {
		t.Errorf("first body = %q, want %q", bodies[0], payload)
	}
	if string(bodies[1]) != payload {
		t.Errorf("second body = %q, want %q (rewind produced different bytes)", bodies[1], payload)
	}
}

func TestUpload_Seekable_MaxRetriesExhausted(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		atomic.AddInt32(&attempts, 1)
		// Retry-After "0" keeps the test fast; value is validated by
		// TestParseRetryAfter.
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("still rate limited"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))

	body := strings.NewReader("retry-me") // io.ReadSeeker
	_, err := c.Upload(context.Background(), "/v1/packages/1/upload", body, "application/octet-stream", int64(body.Len()))
	if err == nil {
		t.Fatal("expected error after all retries exhausted")
	}

	var ec *exitcode.Error
	if !errors.As(err, &ec) {
		t.Fatalf("expected *exitcode.Error, got %T: %v", err, err)
	}
	if ec.Code != exitcode.RateLimited {
		t.Errorf("exit code = %v, want %v", ec.Code, exitcode.RateLimited)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("attempts = %d, want 3 (all retries should fire)", got)
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		fallback time.Duration
		want     time.Duration
	}{
		{"numeric seconds", "5", time.Second, 5 * time.Second},
		{"numeric with whitespace", "  3 ", time.Second, 3 * time.Second},
		{"empty uses fallback", "", 2 * time.Second, 2 * time.Second},
		{"malformed uses fallback", "Wed, 21 Oct 2025 07:28:00 GMT", 2 * time.Second, 2 * time.Second},
		{"zero is valid", "0", time.Second, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseRetryAfter(tt.header, tt.fallback); got != tt.want {
				t.Errorf("parseRetryAfter(%q, %v) = %v, want %v", tt.header, tt.fallback, got, tt.want)
			}
		})
	}
}

// TestGatewayTenantTravelsInHeader pins the scope moving out of the URL. Until
// 2026-08-25 the tenant was a path segment and Tyk resolved the request context
// from the path; prod then gained `header` as an allowed source and the specs
// dropped the segment. A path carrying a tenant would still be routed during the
// transition window, so nothing fails loudly if this regresses — hence a test
// that asserts the header is sent AND that no tenant reaches the URL.
func TestGatewayTenantTravelsInHeader(t *testing.T) {
	var gotPath, gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Get("X-Tenant-Id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"), WithGatewayScope(auth.TenantScope("abc-123")))
	if _, err := c.Do(context.Background(), "GET", "/v1/buildings", nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if want := "/pro/v1/buildings"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotHeader != "abc-123" {
		t.Errorf("X-Tenant-Id = %q, want %q", gotHeader, "abc-123")
	}
	if strings.Contains(gotPath, "tenant") {
		t.Errorf("path %q still carries a tenant segment", gotPath)
	}
}

// TestDirectInstanceSendsNoTenantHeader guards the other direction: a
// direct-to-instance client has no tenant and must not invent one.
func TestDirectInstanceSendsNoTenantHeader(t *testing.T) {
	var gotHeader, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Tenant-Id")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))
	if _, err := c.Do(context.Background(), "GET", "/v1/buildings", nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotHeader != "" {
		t.Errorf("X-Tenant-Id = %q, want it absent", gotHeader)
	}
	if want := "/api/v1/buildings"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

// TestGatewayUnservedNote pins the app-installers explanation, and the
// false-positive containment around it.
//
// App installers are reachable only against a Jamf Pro instance directly, not
// through the platform gateway. The gateway's answer for a namespace it does not
// route is 403 BAD_PERMISSIONS or Tyk's bare "404 page not found" — 403
// BAD_PERMISSIONS being indistinguishable from a real missing privilege, which
// sends an operator looking for an API role that cannot help.
//
// The note is appended, never substituted, so the cases below also check it does
// NOT fire for a Jamf-Pro-issued 404 (a deployment ID that really is gone) or
// for a direct-to-instance path.
func TestGatewayUnservedNote(t *testing.T) {
	const proNotFound = `{"httpStatus":404,"errors":[]}`
	cases := []struct {
		name     string
		status   int
		path     string
		body     string
		wantNote bool
	}{
		{
			name:     "gateway 403 BAD_PERMISSIONS on app-installers",
			status:   http.StatusForbidden,
			path:     "/pro/v1/app-installers/titles",
			body:     `{"httpStatus":403,"errors":[{"code":"BAD_PERMISSIONS"}]}`,
			wantNote: true,
		},
		{
			name:     "Tyk unrouted 404 on app-installers",
			status:   http.StatusNotFound,
			path:     "/pro/v1/app-installers/deployments",
			body:     "404 page not found\n",
			wantNote: true,
		},
		{
			// The request reached Jamf Pro, which answered its own 404 for a
			// deployment that does not exist. That is not a gateway problem and
			// the note would be a red herring.
			name:     "Jamf Pro's own 404 for a missing deployment",
			status:   http.StatusNotFound,
			path:     "/pro/v1/app-installers/deployments/does-not-exist",
			body:     proNotFound,
			wantNote: false,
		},
		{
			// Same resource, no gateway involved: /api/ rather than /pro/.
			name:     "direct-to-instance path is untouched",
			status:   http.StatusForbidden,
			path:     "/api/v1/app-installers/titles",
			body:     `{"httpStatus":403,"errors":[{"code":"BAD_PERMISSIONS"}]}`,
			wantNote: false,
		},
		{
			// A different Pro namespace through the gateway is served; a 403
			// there is a genuine privilege problem.
			name:     "another gateway Pro namespace keeps the plain hint",
			status:   http.StatusForbidden,
			path:     "/pro/v1/categories",
			body:     `{"httpStatus":403,"errors":[{"code":"BAD_PERMISSIONS"}]}`,
			wantNote: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := StatusError(tc.status, "GET", tc.path, []byte(tc.body))
			var e *exitcode.Error
			if !errors.As(err, &e) {
				t.Fatalf("expected a structured exit error, got %T", err)
			}
			got := strings.Contains(e.Hint, "not exposed on the Jamf Platform gateway")
			if got != tc.wantNote {
				t.Errorf("note present = %v, want %v; hint = %q", got, tc.wantNote, e.Hint)
			}
			// The original remediation must survive in every case — the note is
			// additive, so replacing the hint would be a regression too.
			if e.Hint == "" {
				t.Error("hint was emptied")
			}
		})
	}
}

// TestUpload_PlatformGatewayPath covers the second call site of
// rewritePathForGateway.
//
// Client.Upload builds its own request rather than going through Do, so it is a
// separate path-rewriting and scope-header site — the same reason the tenant
// path->header migration had to be verified on it independently. Every other
// Upload test uses a direct-to-instance client, so nothing asserted its gateway
// shape until now.
//
// This one cannot be checked on the wire: the GA edge's WAF refuses .pkg uploads
// with a CloudFront 403 before the request reaches Jamf, so a live probe cannot
// distinguish a wrong path from the WAF. That makes the unit assertion the only
// coverage there is.
func TestUpload_PlatformGatewayPath(t *testing.T) {
	var gotPath, gotScope string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotScope = r.Header.Get("X-Tenant-Id")
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"), WithGatewayScope(auth.TenantScope("abc-123")))
	body := strings.NewReader("payload")
	if _, err := c.Upload(context.Background(), "/v1/packages/1/upload", body, "application/octet-stream", int64(body.Len())); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	if want := "/pro/v1/packages/1/upload"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if strings.HasPrefix(gotPath, "/api/") {
		t.Errorf("path is under /api, which the GA gateway does not serve: %q", gotPath)
	}
	if strings.Contains(gotPath, "tenant") {
		t.Errorf("path %q still carries a tenant segment", gotPath)
	}
	if gotScope != "abc-123" {
		t.Errorf("X-Tenant-Id = %q, want %q", gotScope, "abc-123")
	}
}

// TestEdgeBlockedNote pins the CloudFront/WAF refusal being reported as what it
// is rather than as a privilege problem.
//
// The GA gateway sits behind CloudFront, whose WAF refuses some requests before
// Jamf sees them — wire-established 2026-08-28: "file://" anywhere in the body
// earns a 403 "Request blocked", while the identical body with a POSIX path
// reaches Jamf. That is a legitimate value in Classic payloads (a dock item's
// path), and the untreated form of this error told an operator with a perfectly
// good credential to go and fix their API role.
//
// The hint must not assert WHICH rule fired: the same page is returned for a
// content match and for a volume block, with no traceId and nothing naming the
// rule.
func TestEdgeBlockedNote(t *testing.T) {
	const cloudfront = `<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01 Transitional//EN">
<HTML><HEAD><TITLE>ERROR: The request could not be satisfied</TITLE></HEAD>
<BODY><H1>403 ERROR</H1><H2>The request could not be satisfied.</H2>
Request blocked.</BODY></HTML>`

	err := StatusError(http.StatusForbidden, "POST", "/proclassic/dockitems/id/0", []byte(cloudfront))
	var e *exitcode.Error
	if !errors.As(err, &e) {
		t.Fatalf("expected a structured exit error, got %T", err)
	}
	if !strings.Contains(e.Error(), "blocked at the Jamf gateway edge") {
		t.Errorf("message does not identify the edge as the refuser: %q", e.Error())
	}
	// The whole point is that the operator is NOT sent to their API role.
	if strings.Contains(e.Hint, "check its API role") {
		t.Errorf("hint still blames the API role: %q", e.Hint)
	}
	if !strings.Contains(e.Hint, "file://") {
		t.Errorf("hint does not name the known file:// trigger: %q", e.Hint)
	}
	if !strings.Contains(e.Hint, "cannot say which one fired") {
		t.Errorf("hint claims to know which rule fired: %q", e.Hint)
	}
	// And the HTML page must not be pasted into the message.
	if strings.Contains(e.Error(), "DOCTYPE") {
		t.Errorf("the HTML page leaked into the message: %q", e.Error())
	}

	// A genuine Jamf 403 keeps the privilege hint.
	jamf := StatusError(http.StatusForbidden, "GET", "/pro/v1/categories",
		[]byte(`{"httpStatus":403,"errors":[{"code":"ACCESS_DENIED"}]}`))
	var je *exitcode.Error
	if !errors.As(jamf, &je) {
		t.Fatalf("expected a structured exit error, got %T", jamf)
	}
	if !strings.Contains(je.Hint, "API role") {
		t.Errorf("a real Jamf 403 lost its privilege hint: %q", je.Hint)
	}
}
