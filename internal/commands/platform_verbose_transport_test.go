// Copyright 2026, Jamf Software LLC

package commands

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

type stubRoundTripper struct{ status int }

func (s *stubRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	rec.WriteHeader(s.status)
	return rec.Result(), nil
}

// captureStderr runs fn with os.Stderr redirected and returns what it wrote.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}
	os.Stderr = w

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()

	_ = w.Close()
	os.Stderr = orig
	return <-done
}

func mustRoundTrip(t *testing.T, tr http.RoundTripper, method, url string) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

// A retry sequence used to come out as N identical request/response pairs with
// nothing saying they were retries and no timing, so even a bounded 15s of
// backoff read as the CLI looping or hanging. This transport is the only place
// that can label them: it sits below retryablehttp, so it sees every attempt,
// while the SDK's own RequestLogHook can carry neither the attempt number nor
// the wait through the Logger interface it emits on.
func TestPlatformVerboseTransportLabelsRetries(t *testing.T) {
	tr := &platformVerboseTransport{inner: &stubRoundTripper{status: 502}, level: 1}

	out := captureStderr(t, func() {
		for range 3 {
			mustRoundTrip(t, tr, http.MethodGet, "https://us.api.jamfcloud.com/sso/v1/connections")
		}
	})

	if strings.Count(out, "--> GET") != 3 {
		t.Fatalf("expected three request lines, got:\n%s", out)
	}
	// The first attempt is not a retry and must not be labelled one.
	first := strings.SplitN(out, "\n", 2)[0]
	if strings.Contains(first, "retry") {
		t.Errorf("first attempt labelled as a retry: %q", first)
	}
	for _, want := range []string{"(retry 1, waited ", "(retry 2, waited "} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "retry 3") {
		t.Errorf("three attempts is two retries, not three:\n%s", out)
	}
}

// A repeat after a SUCCESS is a fresh call, not a retry — retryablehttp only
// retries a failure. Caught by the test above's sibling before it shipped: a
// command that legitimately re-issues one request (a poll, or a --name lookup
// followed by a list of the same collection) reported "(retry 1, waited 0s)".
func TestPlatformVerboseTransportDoesNotLabelRepeatAfterSuccess(t *testing.T) {
	tr := &platformVerboseTransport{inner: &stubRoundTripper{status: 200}, level: 1}
	out := captureStderr(t, func() {
		for range 3 {
			mustRoundTrip(t, tr, http.MethodGet, "https://us.api.jamfcloud.com/sso/v1/domains")
		}
	})
	if strings.Contains(out, "retry") {
		t.Errorf("a repeat after 200 must not be labelled a retry:\n%s", out)
	}
}

// Consecutive is the other half of the test. retryablehttp re-issues the same
// method and URL with nothing interleaved, so a different request in between
// means the repeat is a fresh call even when the earlier one failed.
func TestPlatformVerboseTransportResetsOnDifferentRequest(t *testing.T) {
	// 502 throughout, so only the consecutive guard can suppress the label.
	tr := &platformVerboseTransport{inner: &stubRoundTripper{status: 502}, level: 1}

	out := captureStderr(t, func() {
		mustRoundTrip(t, tr, http.MethodGet, "https://us.api.jamfcloud.com/sso/v1/domains")
		mustRoundTrip(t, tr, http.MethodGet, "https://us.api.jamfcloud.com/sso/v1/connections")
		mustRoundTrip(t, tr, http.MethodGet, "https://us.api.jamfcloud.com/sso/v1/domains")
	})
	if strings.Contains(out, "retry") {
		t.Errorf("interleaved requests must not be labelled retries:\n%s", out)
	}

	// A differing method on the same URL is also a different request.
	tr = &platformVerboseTransport{inner: &stubRoundTripper{status: 502}, level: 1}
	out = captureStderr(t, func() {
		mustRoundTrip(t, tr, http.MethodGet, "https://us.api.jamfcloud.com/sso/v1/domains")
		mustRoundTrip(t, tr, http.MethodDelete, "https://us.api.jamfcloud.com/sso/v1/domains")
	})
	if strings.Contains(out, "retry") {
		t.Errorf("a different method must not be labelled a retry:\n%s", out)
	}
}

// At -v off, the transport must stay silent — and must not accumulate retry
// state either, since the counter lives behind the same level check.
func TestPlatformVerboseTransportSilentAtLevelZero(t *testing.T) {
	tr := &platformVerboseTransport{inner: &stubRoundTripper{status: 502}, level: 0}
	out := captureStderr(t, func() {
		for range 3 {
			mustRoundTrip(t, tr, http.MethodGet, "https://us.api.jamfcloud.com/sso/v1/connections")
		}
	})
	if out != "" {
		t.Errorf("level 0 must log nothing, got:\n%s", out)
	}
}
