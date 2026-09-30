// Copyright 2026, Jamf Software LLC

package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// slowServer answers every request only after release is closed, counting the
// requests that reached it.
func slowServer(t *testing.T) (srv *httptest.Server, hits *int32) {
	t.Helper()
	release := make(chan struct{})
	hits = new(int32)
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	return srv, hits
}

// withHeaderTimeout gives c's transport a header timeout short enough for a
// test to trip.
func withHeaderTimeout(t *testing.T, c *Client, d time.Duration) {
	t.Helper()
	tr, ok := c.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport is %T, want *http.Transport", c.httpClient.Transport)
	}
	tr.ResponseHeaderTimeout = d
}

func hintOf(t *testing.T, err error) string {
	t.Helper()
	var ee *exitcode.Error
	if !errors.As(err, &ee) {
		t.Fatalf("error %v is not an *exitcode.Error", err)
	}
	return ee.Hint
}

// The old policy re-sent a timed-out request three times, so the reporter of
// issue 392 waited three full timeouts for one error. A timeout means the
// server is still working on the request; the identical request only times
// out again.
func TestDoWithRetry_TimeoutIsNotRetried(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			srv, hits := slowServer(t)
			c := New(srv.URL, auth.NewTokenProvider("test-token"))
			withHeaderTimeout(t, c, 50*time.Millisecond)

			var body io.Reader
			if method != http.MethodGet {
				body = strings.NewReader(`{"name":"x"}`)
			}
			_, err := c.Do(context.Background(), method, "/v1/things", body)
			if !errors.Is(err, registry.ErrServerTimeout) {
				t.Fatalf("error = %v, want one wrapping registry.ErrServerTimeout", err)
			}
			if n := atomic.LoadInt32(hits); n != 1 {
				t.Errorf("server saw %d requests, want 1", n)
			}
			hint := hintOf(t, err)
			if method == http.MethodGet && !strings.Contains(hint, "--page-size") {
				t.Errorf("GET hint = %q, want it to name --page-size", hint)
			}
			if method != http.MethodGet && hint != mayHaveAppliedHint {
				t.Errorf("%s hint = %q, want the may-have-been-applied hint", method, hint)
			}
		})
	}
}

// A write whose connection dropped after it was sent may already have been
// applied. Re-sending it is how one command creates two objects.
func TestDoWithRetry_WriteIsNotResentAfterItReachedTheServer(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))
	_, err := c.Do(context.Background(), http.MethodPost, "/v1/things", strings.NewReader(`{"name":"x"}`))
	if err == nil {
		t.Fatal("expected an error")
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Errorf("server saw %d POSTs, want 1", n)
	}
	if hint := hintOf(t, err); hint != mayHaveAppliedHint {
		t.Errorf("hint = %q, want the may-have-been-applied hint", hint)
	}
}

// failingTransport fails every request without sending it, the way a refused
// dial does: no WroteHeaders, so nothing reached a server.
type failingTransport struct{ calls int32 }

func (f *failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	atomic.AddInt32(&f.calls, 1)
	return nil, errors.New("dial tcp: connection refused")
}

// A request that never left the client is safe to retry whatever its method.
func TestDoWithRetry_UnsentWriteIsRetried(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out the retry backoff")
	}
	ft := &failingTransport{}
	c := New("http://jamf.invalid", auth.NewTokenProvider("test-token"))
	c.httpClient.Transport = ft

	_, err := c.Do(context.Background(), http.MethodPost, "/v1/things", strings.NewReader(`{}`))
	if err == nil || !strings.Contains(err.Error(), "after 3 retries") {
		t.Fatalf("error = %v, want retries exhausted", err)
	}
	if n := atomic.LoadInt32(&ft.calls); n != 3 {
		t.Errorf("transport saw %d attempts, want 3", n)
	}
}

// CloudFront answers 504 after 90s on the gateway. It must read as a server
// timeout — so a paginated walk can shrink its page — and must not repeat the
// edge's HTML page, which says nothing about the request.
func TestDo_GatewayTimeout504(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusGatewayTimeout)
		_, _ = w.Write([]byte(`<HTML><TITLE>ERROR: The request could not be satisfied</TITLE>Generated by cloudfront</HTML>`))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))
	_, err := c.Do(context.Background(), http.MethodGet, "/v4/computers-inventory?page=0&page-size=2000", nil)
	if !errors.Is(err, registry.ErrServerTimeout) {
		t.Fatalf("error = %v, want one wrapping registry.ErrServerTimeout", err)
	}
	if strings.Contains(err.Error(), "cloudfront") {
		t.Errorf("error repeats the edge's page: %v", err)
	}
	if !strings.Contains(err.Error(), "HTTP 504") {
		t.Errorf("error does not name the status: %v", err)
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Errorf("server saw %d requests, want 1", n)
	}
}

// The gateway speaks HTTP/2, whose header timeout is a different error from
// HTTP/1.1's ("http2: timeout awaiting response headers", the one issue 392
// reported). It must read as a server timeout too.
func TestDoWithRetry_HTTP2TimeoutIsAServerTimeout(t *testing.T) {
	release := make(chan struct{})
	var hits int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.ProtoMajor != 2 {
			t.Errorf("request arrived over %s, want HTTP/2", r.Proto)
		}
		<-release
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(func() { close(release); srv.Close() })

	c := New(srv.URL, auth.NewTokenProvider("test-token"))
	withHeaderTimeout(t, c, 50*time.Millisecond)
	c.httpClient.Transport.(*http.Transport).TLSClientConfig = srv.Client().Transport.(*http.Transport).TLSClientConfig

	_, err := c.Do(context.Background(), http.MethodGet, "/v4/computers-inventory", nil)
	if !errors.Is(err, registry.ErrServerTimeout) {
		t.Fatalf("error = %v, want one wrapping registry.ErrServerTimeout", err)
	}
	if !strings.Contains(err.Error(), "http2") {
		t.Errorf("error = %v, want the HTTP/2 timeout", err)
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Errorf("server saw %d requests, want 1", n)
	}
}
