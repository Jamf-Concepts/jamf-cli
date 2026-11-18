// Copyright 2026, Jamf Software LLC

package client

import (
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// Verbose call statistics: how long a call took and how big its bodies were.
//
// None of it comes from the server. Wire-checked 2026-11-18 against a Jamf Pro
// instance, the Platform gateway and Jamf Protect: no response carries
// Server-Timing, X-Response-Time or X-Envoy-Upstream-Service-Time, so the
// elapsed time is measured here. Sizes use what the server announced where it
// announced one — a Content-Length header, which the Platform gateway and Jamf
// Protect send and Jamf Pro (chunked) does not — and count the bytes as the body
// drains where it did not. Go removes Content-Length when it decompresses a gzip
// response itself, so a compressed response is counted too, and the number is
// then the decoded size.
//
// The exported names are for the transports wrapped outside this package (the
// Platform, Protect, School and Security Cloud SDK clients).

// FormatSize renders a byte count as "512 B", "1.4 KB" or "2.3 MB".
func FormatSize(n int64) string {
	switch {
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	case n < 1<<30:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	}
}

// FormatElapsed renders a duration as whole milliseconds under a second and as
// seconds above it.
func FormatElapsed(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

// RequestNote is the suffix a verbose request line carries for its body: " (req
// 1.4 KB)", " (req chunked)" when the length is not declared, and "" when there
// is no body. contentLength is the length the request declares (-1 or 0 with a
// body means undeclared).
func RequestNote(contentLength int64, hasBody bool) string {
	if !hasBody {
		return ""
	}
	if contentLength <= 0 {
		return " (req chunked)"
	}
	return " (req " + FormatSize(contentLength) + ")"
}

// ResponseNote is the suffix a verbose response line carries: the time the
// server took to answer with its headers and, when it announced a body length,
// that length — " (112ms, resp 456 B)". A body without an announced length is
// reported afterwards by TrackBody.
func ResponseNote(elapsed time.Duration, resp *http.Response) string {
	note := FormatElapsed(elapsed)
	if resp != nil && resp.ContentLength >= 0 {
		note += ", resp " + FormatSize(resp.ContentLength)
	}
	return " (" + note + ")"
}

// TrackBody wraps resp.Body so that, when the body's length was not announced,
// its counted size and the total time since start are written to w once the body
// is drained or closed. start is when the request was sent, so the total covers
// the headers wait plus the transfer.
//
// A body with an announced length, or none, is left alone: ResponseNote already
// said everything there is to say. Call it before anything else reads the body,
// so the count includes the bytes a -vvv preview consumes.
func TrackBody(w io.Writer, resp *http.Response, start time.Time) {
	if resp == nil || resp.Body == nil || resp.Body == http.NoBody || resp.ContentLength >= 0 {
		return
	}
	resp.Body = &countingBody{ReadCloser: resp.Body, w: w, start: start}
}

type countingBody struct {
	io.ReadCloser
	w     io.Writer
	start time.Time
	n     atomic.Int64
	done  atomic.Bool
	eof   atomic.Bool
}

func (b *countingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.n.Add(int64(n))
	if err == io.EOF {
		b.eof.Store(true)
		b.report()
	}
	return n, err
}

func (b *countingBody) Close() error {
	err := b.ReadCloser.Close()
	b.report()
	return err
}

// report writes the line once. A body closed before EOF — a -vvv preview stops
// at the log limit, an error path stops reading — is reported as what was read.
func (b *countingBody) report() {
	if !b.done.CompareAndSwap(false, true) {
		return
	}
	size := FormatSize(b.n.Load())
	if !b.eof.Load() {
		size += " read, not drained"
	}
	_, _ = fmt.Fprintf(b.w, "    [resp body %s, %s total]\n", size, FormatElapsed(time.Since(b.start)))
}
