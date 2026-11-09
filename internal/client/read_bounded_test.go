// Copyright 2026, Jamf Software LLC

package client

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestReadBounded_ExactlyAtLimitSucceeds(t *testing.T) {
	data, err := ReadBounded(bytes.NewReader(make([]byte, 100)), 100)
	if err != nil || len(data) != 100 {
		t.Fatalf("got len=%d err=%v, want 100 bytes and no error", len(data), err)
	}
}

// TestReadBounded_OverLimitFailsRatherThanTruncating guards #470: a bare
// io.LimitReader returned the first max bytes with a nil error, which surfaced
// later as "XML syntax error: unexpected EOF".
func TestReadBounded_OverLimitFailsRatherThanTruncating(t *testing.T) {
	data, err := ReadBounded(bytes.NewReader(make([]byte, 101)), 100)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("err = %v, want ErrBodyTooLarge", err)
	}
	if data != nil {
		t.Fatalf("returned %d bytes alongside the error; a caller could parse a truncated body", len(data))
	}
}

func TestReadResponseBody_PastTenMegabytesIsNotTruncated(t *testing.T) {
	body := strings.Repeat("x", 12<<20)
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(body))}
	data, err := ReadResponseBody(resp)
	if err != nil || len(data) != len(body) {
		t.Fatalf("got len=%d err=%v, want the full %d bytes", len(data), err, len(body))
	}
}
