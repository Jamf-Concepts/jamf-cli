// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
)

type bodyRoundTripper struct {
	body          string
	contentLength int64 // -1: unannounced
}

func (b *bodyRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode:    200,
		Status:        "200 OK",
		Proto:         "HTTP/2.0",
		Header:        http.Header{},
		ContentLength: b.contentLength,
		Body:          io.NopCloser(strings.NewReader(b.body)),
		Request:       req,
	}, nil
}

func roundTripAndRead(t *testing.T, tr http.RoundTripper, method, body string) string {
	t.Helper()
	var reqBody io.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, "https://eu.api.jamfcloud.com/blueprints/v1/blueprints", reqBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return string(b)
}

// Every SDK-backed surface (Platform, Protect, School, Security Cloud) logs
// through verboseTransport, so this is the one place their call statistics are
// checked.
func TestVerboseTransportReportsSizesAndTimings(t *testing.T) {
	t.Run("announced length comes from the header", func(t *testing.T) {
		tr := &verboseTransport{inner: &bodyRoundTripper{body: "hello", contentLength: 5}, level: 1}
		out := captureStderr(t, func() { roundTripAndRead(t, tr, http.MethodPost, `{"a":1}`) })
		for _, want := range []string{"(req 7 B)", "<-- HTTP/2.0 200 OK (", "ms, resp 5 B)"} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q in:\n%s", want, out)
			}
		}
		if strings.Contains(out, "[resp body") {
			t.Errorf("a body whose length the server announced is not counted again:\n%s", out)
		}
	})

	t.Run("unannounced length is counted", func(t *testing.T) {
		tr := &verboseTransport{inner: &bodyRoundTripper{body: strings.Repeat("z", 3000), contentLength: -1}, level: 1}
		out := captureStderr(t, func() { roundTripAndRead(t, tr, http.MethodGet, "") })
		if strings.Contains(out, "(req") {
			t.Errorf("a request with no body has no size:\n%s", out)
		}
		if !strings.Contains(out, "[resp body 2.9 KB, ") {
			t.Errorf("missing counted size in:\n%s", out)
		}
	})
}

// At -vvv the transport reads a preview of the body for the log. It used to hand
// the command only that preview, so any response past the log limit reached the
// caller cut off.
func TestVerboseTransportBodyPreviewKeepsTheWholeBody(t *testing.T) {
	big := strings.Repeat("b", 200<<10)
	tr := &verboseTransport{inner: &bodyRoundTripper{body: big, contentLength: -1}, level: 3}
	var got string
	out := captureStderr(t, func() { got = roundTripAndRead(t, tr, http.MethodGet, "") })
	if len(got) != len(big) || !bytes.Equal([]byte(got), []byte(big)) {
		t.Fatalf("command received %d of %d bytes", len(got), len(big))
	}
	if !strings.Contains(out, "[resp body 200.0 KB, ") {
		t.Errorf("counted size must be the real one, got tail:\n%s", out[max(0, len(out)-200):])
	}
}
