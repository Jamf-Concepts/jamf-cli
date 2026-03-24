package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Jamf-Concepts/jamfpro-cli/internal/auth"
	"github.com/Jamf-Concepts/jamfpro-cli/internal/exitcode"
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

func TestDo_SetsJSONHeaders(t *testing.T) {
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
	_, err := c.Do(context.Background(), "POST", "/JSSResource/policies/id/0", body)
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

	c := New(srv.URL, auth.NewTokenProvider("test-token"), WithVerbose(true))

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
	if c.verbose {
		t.Error("verbose should default to false")
	}

	c = New("https://example.com", auth.NewTokenProvider("tok"), WithVerbose(true))
	if !c.verbose {
		t.Error("WithVerbose(true) should set verbose to true")
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
		}
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))
	_, err := c.Do(context.Background(), "GET", "/v1/buildings", nil)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}

	if !strings.Contains(err.Error(), fmt.Sprintf("after %d retries", 3)) {
		t.Errorf("error should mention retry count, got: %q", err.Error())
	}
}
