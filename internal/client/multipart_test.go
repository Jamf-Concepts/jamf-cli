// Copyright 2026, Jamf Software LLC

package client

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempFile(t *testing.T, name string, payload []byte) *os.File {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening temp file: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestNewMultipartFileUpload_StreamsCorrectBody(t *testing.T) {
	payload := []byte("hello multipart world")
	f := writeTempFile(t, "test.bin", payload)

	body, contentType, contentLength, err := NewMultipartFileUpload("file", f)
	if err != nil {
		t.Fatalf("NewMultipartFileUpload: %v", err)
	}

	// Read the whole streamed body
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if int64(len(got)) != contentLength {
		t.Errorf("body length = %d, Content-Length = %d; should match", len(got), contentLength)
	}

	// Parse the multipart body back and verify the file part
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatalf("parse content-type: %v", err)
	}
	boundary := params["boundary"]
	if boundary == "" {
		t.Fatal("no boundary in content-type")
	}

	mr := multipart.NewReader(bytes.NewReader(got), boundary)
	part, err := mr.NextPart()
	if err != nil {
		t.Fatalf("reading part: %v", err)
	}
	if part.FormName() != "file" {
		t.Errorf("form name = %q, want %q", part.FormName(), "file")
	}
	if part.FileName() != "test.bin" {
		t.Errorf("file name = %q, want %q", part.FileName(), "test.bin")
	}
	partBytes, err := io.ReadAll(part)
	if err != nil {
		t.Fatalf("reading part body: %v", err)
	}
	if !bytes.Equal(partBytes, payload) {
		t.Errorf("part body = %q, want %q", partBytes, payload)
	}

	if _, err := mr.NextPart(); err != io.EOF {
		t.Errorf("expected EOF after single part, got %v", err)
	}
}

func TestNewMultipartFileUpload_ContentLengthExact(t *testing.T) {
	payload := make([]byte, 5*1024*1024) // 5 MiB
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	f := writeTempFile(t, "big.pkg", payload)

	body, _, contentLength, err := NewMultipartFileUpload("file", f)
	if err != nil {
		t.Fatalf("NewMultipartFileUpload: %v", err)
	}

	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if int64(len(got)) != contentLength {
		t.Errorf("streamed bytes = %d, Content-Length = %d", len(got), contentLength)
	}
}

func TestSeekableMultipartBody_RewindForRetry(t *testing.T) {
	payload := []byte("retry me")
	f := writeTempFile(t, "retry.bin", payload)

	body, _, _, err := NewMultipartFileUpload("file", f)
	if err != nil {
		t.Fatalf("NewMultipartFileUpload: %v", err)
	}

	first, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("reading body first time: %v", err)
	}

	if _, err := body.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("seek rewind: %v", err)
	}

	second, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("reading body second time: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Errorf("rewind produced different bytes:\nfirst:  %q\nsecond: %q", first, second)
	}
}

func TestSeekableMultipartBody_ShortFileErrors(t *testing.T) {
	// Fake the mismatch between declared fileSize and what the reader actually
	// yields: stat said 100, reader delivers 5. Content-Length has already
	// been committed, so Read must surface io.ErrUnexpectedEOF instead of
	// silently sending a short body.
	body := &seekableMultipartBody{
		header:   []byte("--b\r\n\r\n"),
		file:     strings.NewReader("short"), // only 5 bytes
		footer:   []byte("\r\n--b--\r\n"),
		fileSize: 100, // lie about the size
	}

	buf := make([]byte, 200)
	_, err := io.ReadFull(body, buf)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("expected io.ErrUnexpectedEOF, got %v", err)
	}
}

func TestSeekableMultipartBody_UnsupportedSeekRejected(t *testing.T) {
	f := writeTempFile(t, "x.bin", []byte("x"))
	body, _, _, err := NewMultipartFileUpload("file", f)
	if err != nil {
		t.Fatalf("NewMultipartFileUpload: %v", err)
	}

	if _, err := body.Seek(10, io.SeekStart); err == nil {
		t.Error("Seek(10, SeekStart) should fail")
	}
	if _, err := body.Seek(0, io.SeekCurrent); err == nil {
		t.Error("Seek(0, SeekCurrent) should fail")
	}
	if _, err := body.Seek(0, io.SeekEnd); err == nil {
		t.Error("Seek(0, SeekEnd) should fail")
	}
}

func TestBuildFilePartHeader_SniffsContentType(t *testing.T) {
	h := string(buildFilePartHeader("b", "file", "icon.png"))

	var mh textproto.MIMEHeader
	// The header is raw bytes ending with CRLFCRLF; fake parse by splitting on CRLF
	lines := strings.Split(h, "\r\n")
	mh = make(textproto.MIMEHeader)
	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		k, v, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		mh.Add(k, v)
	}

	if got := mh.Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want %q", got, "image/png")
	}
	// Binary extension falls through to octet-stream
	h2 := string(buildFilePartHeader("b", "file", "blob.bin"))
	if !strings.Contains(h2, "Content-Type: application/octet-stream") {
		t.Errorf("blob.bin should get octet-stream, got: %s", h2)
	}
}

func TestBuildFilePartHeader_StripsCharset(t *testing.T) {
	// Text extensions default to "text/plain; charset=utf-8" — the charset
	// param must be stripped for multipart uploads.
	h := string(buildFilePartHeader("b", "file", "notes.txt"))
	if strings.Contains(h, "charset=") {
		t.Errorf("charset param should be stripped, got: %s", h)
	}
}

func TestRandomBoundary_UniquePerCall(t *testing.T) {
	b1 := randomBoundary()
	b2 := randomBoundary()
	if b1 == b2 {
		t.Errorf("two consecutive boundaries are equal (%q); expected random", b1)
	}
}
