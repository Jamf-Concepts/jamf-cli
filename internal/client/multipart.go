// Copyright 2026, Jamf Software LLC

package client

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
)

// NewMultipartFileUpload builds a streaming multipart/form-data body wrapping
// a single file part and returns the body, Content-Type, and exact
// Content-Length.
//
// The returned io.ReadSeeker streams directly from the underlying *os.File —
// no in-memory copy of the file. Seek(0, SeekStart) rewinds the body so
// Client.Upload can retry on HTTP 429.
//
// The caller owns the file and must close it after Upload returns. filename
// in the multipart header is derived from filepath.Base(f.Name()).
func NewMultipartFileUpload(fieldName string, f *os.File) (body io.ReadSeeker, contentType string, contentLength int64, err error) {
	info, err := f.Stat()
	if err != nil {
		return nil, "", 0, fmt.Errorf("stat upload file: %w", err)
	}
	if info.IsDir() {
		return nil, "", 0, fmt.Errorf("upload source is a directory: %s", f.Name())
	}

	boundary := randomBoundary()
	filename := filepath.Base(f.Name())
	header := buildFilePartHeader(boundary, fieldName, filename)
	footer := []byte("\r\n--" + boundary + "--\r\n")

	body = &seekableMultipartBody{
		header:   header,
		file:     f,
		footer:   footer,
		fileSize: info.Size(),
	}
	contentType = "multipart/form-data; boundary=" + boundary
	contentLength = int64(len(header)) + info.Size() + int64(len(footer))
	return body, contentType, contentLength, nil
}

// buildFilePartHeader writes the opening boundary + Content-Disposition +
// Content-Type for a single file part. The Content-Type is sniffed from the
// filename extension: Jamf's image-upload endpoints (enrollment-customization,
// icon) reject the stdlib default "application/octet-stream" for PNG uploads,
// so a correct MIME type is required. Falls back to octet-stream for unknown
// extensions and strips any "; charset=..." parameter that mime.TypeByExtension
// attaches to text/* types (binary uploads should not carry a charset).
func buildFilePartHeader(boundary, fieldName, filename string) []byte {
	ct := mime.TypeByExtension(filepath.Ext(filename))
	if ct == "" {
		ct = "application/octet-stream"
	}
	if semi := strings.Index(ct, ";"); semi >= 0 {
		ct = strings.TrimSpace(ct[:semi])
	}
	h := fmt.Sprintf(
		"--%s\r\nContent-Disposition: form-data; name=%q; filename=%q\r\nContent-Type: %s\r\n\r\n",
		boundary, fieldName, filename, ct,
	)
	return []byte(h)
}

// randomBoundary reuses stdlib multipart.Writer's default 30-hex-char boundary
// generator via a throwaway writer — avoids reinventing RNG code.
func randomBoundary() string {
	return multipart.NewWriter(io.Discard).Boundary()
}

// seekableMultipartBody composes a byte-array header, a seekable file body,
// and a byte-array footer into a single streaming io.ReadSeeker. Only
// Seek(0, SeekStart) is supported — that's all Client.Upload needs for retry.
type seekableMultipartBody struct {
	header   []byte
	file     io.ReadSeeker
	footer   []byte
	fileSize int64
	pos      int64 // absolute position across header|file|footer
}

func (s *seekableMultipartBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	headerLen := int64(len(s.header))
	fileEnd := headerLen + s.fileSize
	totalLen := fileEnd + int64(len(s.footer))

	n := 0
	for n < len(p) {
		if s.pos >= totalLen {
			if n == 0 {
				return 0, io.EOF
			}
			return n, nil
		}
		switch {
		case s.pos < headerLen:
			m := copy(p[n:], s.header[s.pos:])
			s.pos += int64(m)
			n += m
		case s.pos < fileEnd:
			// Cap the read at the file boundary so a short Read on the
			// underlying file doesn't leak past the declared fileSize.
			remaining := fileEnd - s.pos
			dst := p[n:]
			if int64(len(dst)) > remaining {
				dst = dst[:remaining]
			}
			m, err := s.file.Read(dst)
			s.pos += int64(m)
			n += m
			if err != nil && !errors.Is(err, io.EOF) {
				return n, err
			}
			if m == 0 {
				// Defensive: the file ended early compared to Stat().Size().
				// Skip to the footer so we don't loop.
				s.pos = fileEnd
			}
		default: // footer
			offset := s.pos - fileEnd
			m := copy(p[n:], s.footer[offset:])
			s.pos += int64(m)
			n += m
		}
	}
	return n, nil
}

func (s *seekableMultipartBody) Seek(offset int64, whence int) (int64, error) {
	if offset != 0 || whence != io.SeekStart {
		return 0, fmt.Errorf("seekableMultipartBody: only Seek(0, SeekStart) is supported")
	}
	if _, err := s.file.Seek(0, io.SeekStart); err != nil {
		return 0, fmt.Errorf("rewinding underlying file: %w", err)
	}
	s.pos = 0
	return 0, nil
}
