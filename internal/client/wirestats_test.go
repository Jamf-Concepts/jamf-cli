// Copyright 2026, Jamf Software LLC

package client

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
)

func TestFormatSize(t *testing.T) {
	for n, want := range map[int64]string{
		0: "0 B", 1023: "1023 B", 1024: "1.0 KB", 1536: "1.5 KB",
		1<<20 - 1: "1024.0 KB", 1 << 20: "1.0 MB", 5 << 30: "5.0 GB",
	} {
		if got := FormatSize(n); got != want {
			t.Errorf("FormatSize(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestFormatElapsed(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0: "0ms", 112 * time.Millisecond: "112ms", 999 * time.Millisecond: "999ms",
		time.Second: "1.00s", 2350 * time.Millisecond: "2.35s",
	} {
		if got := FormatElapsed(d); got != want {
			t.Errorf("FormatElapsed(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestRequestNote(t *testing.T) {
	for _, tc := range []struct {
		n       int64
		hasBody bool
		want    string
	}{
		{0, false, ""},
		{-1, false, ""},
		{45, true, " (req 45 B)"},
		{1500, true, " (req 1.5 KB)"},
		// A body whose length is not declared is chunked, not empty.
		{-1, true, " (req chunked)"},
		{0, true, " (req chunked)"},
	} {
		if got := RequestNote(tc.n, tc.hasBody); got != tc.want {
			t.Errorf("RequestNote(%d, %v) = %q, want %q", tc.n, tc.hasBody, got, tc.want)
		}
	}
}

func TestResponseNote_UsesTheAnnouncedLength(t *testing.T) {
	got := ResponseNote(112*time.Millisecond, &http.Response{ContentLength: 456})
	if got != " (112ms, resp 456 B)" {
		t.Errorf("got %q", got)
	}
	// An unannounced length is left out rather than reported as zero.
	if got := ResponseNote(112*time.Millisecond, &http.Response{ContentLength: -1}); got != " (112ms)" {
		t.Errorf("unannounced length: got %q", got)
	}
	if got := ResponseNote(time.Second, &http.Response{ContentLength: 0}); got != " (1.00s, resp 0 B)" {
		t.Errorf("empty body: got %q", got)
	}
}

func TestTrackBody_CountsAnUnannouncedBodyOnDrain(t *testing.T) {
	var out bytes.Buffer
	resp := &http.Response{ContentLength: -1, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 2048)))}
	TrackBody(&out, resp, time.Now())

	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	got := out.String()
	if !regexp.MustCompile(`^    \[resp body 2\.0 KB, \d+ms total\]\n$`).MatchString(got) {
		t.Errorf("unexpected line (and it must be written exactly once): %q", got)
	}
}

func TestTrackBody_ReportsABodyClosedBeforeEOF(t *testing.T) {
	var out bytes.Buffer
	resp := &http.Response{ContentLength: -1, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 100)))}
	TrackBody(&out, resp, time.Now())

	buf := make([]byte, 10)
	if _, err := resp.Body.Read(buf); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	if got := out.String(); !strings.Contains(got, "10 B read, not drained") {
		t.Errorf("a partial read must say so, got %q", got)
	}
}

func TestTrackBody_LeavesAnAnnouncedOrEmptyBodyAlone(t *testing.T) {
	var out bytes.Buffer
	announced := &http.Response{ContentLength: 5, Body: io.NopCloser(strings.NewReader("hello"))}
	body := announced.Body
	TrackBody(&out, announced, time.Now())
	if announced.Body != body {
		t.Error("a body with an announced length must not be wrapped: the header already gave its size")
	}

	empty := &http.Response{ContentLength: -1, Body: http.NoBody}
	TrackBody(&out, empty, time.Now())
	if empty.Body != http.NoBody {
		t.Error("http.NoBody must not be wrapped")
	}
	TrackBody(&out, nil, time.Now())
	TrackBody(&out, &http.Response{ContentLength: -1}, time.Now())
	if out.Len() != 0 {
		t.Errorf("nothing should have been written, got %q", out.String())
	}
}

// Pro answers a list chunked, with no Content-Length: the size has to be
// counted. The request side is known up front.
func TestDo_VerboseReportsSizesAndTimings(t *testing.T) {
	const payload = `{"results":[1,2,3]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if strings.HasSuffix(r.URL.Path, "/chunked") {
			w.Header().Set("Content-Type", "application/json")
			fl := w.(http.Flusher)
			_, _ = w.Write([]byte(payload[:5]))
			fl.Flush() // forces chunked: no Content-Length can be set after this
			_, _ = w.Write([]byte(payload[5:]))
			return
		}
		w.Header().Set("Content-Length", "19")
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	for _, tc := range []struct {
		name, path, body string
		want             []string
		notWant          []string
	}{
		{
			name: "chunked response is counted", path: "/chunked", body: `{"a":1}`,
			want:    []string{"--> POST " + srv.URL + "/api/chunked (req 7 B)", " OK (", "ms)", "[resp body 19 B, "},
			notWant: []string{"resp 19 B)"},
		},
		{
			name: "announced response uses the header", path: "/fixed",
			want:    []string{"--> GET " + srv.URL + "/api/fixed\n", ", resp 19 B)"},
			notWant: []string{"(req", "[resp body"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New(srv.URL, auth.NewTokenProvider("tok"), WithVerbose(1))
			method, body := http.MethodGet, io.Reader(nil)
			if tc.body != "" {
				method, body = http.MethodPost, strings.NewReader(tc.body)
			}
			out := captureStderrConcurrently(t, func() {
				resp, err := c.Do(context.Background(), method, tc.path, body)
				if err != nil {
					t.Fatalf("Do: %v", err)
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			})
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("missing %q in:\n%s", w, out)
				}
			}
			for _, w := range tc.notWant {
				if strings.Contains(out, w) {
					t.Errorf("unexpected %q in:\n%s", w, out)
				}
			}
		})
	}
}

// -vvv previews the body for the log; the command must still receive all of it,
// and the counted size must be the real one, not the preview's.
func TestDo_VerboseBodyPreviewKeepsTheWholeBody(t *testing.T) {
	big := strings.Repeat("a", bodyLogLimit*2+17)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fl := w.(http.Flusher)
		_, _ = w.Write([]byte(big[:10]))
		fl.Flush()
		_, _ = w.Write([]byte(big[10:]))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("tok"), WithVerbose(3))
	var got string
	out := captureStderrConcurrently(t, func() {
		resp, err := c.Do(context.Background(), http.MethodGet, "/big", nil)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		got = string(b)
	})
	if got != big {
		t.Errorf("command received %d bytes, server sent %d", len(got), len(big))
	}
	if !strings.Contains(out, "[resp body 128.0 KB, ") {
		t.Errorf("counted size must cover the whole body, got:\n%s", out[max(0, len(out)-300):])
	}
}
