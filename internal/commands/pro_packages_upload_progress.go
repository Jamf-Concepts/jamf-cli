// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"golang.org/x/term"
)

// uploadProgress reports how much of a file has been read, for an upload that
// can run for minutes. On a terminal it redraws one line in place, at most five
// times a second; anywhere else (a CI log, a pipe) it prints a plain line at
// each quarter, so a long job shows signs of life without flooding the log.
// --quiet silences both.
//
// It reports bytes read from the file, which is bytes handed to the transport:
// a value just under 100% means the last buffered writes have not reached the
// server yet, and the caller's own "complete" line follows only after they have.
type uploadProgress struct {
	r io.Reader
	w io.Writer

	// mu guards the counters: the JCDS transfer reads several parts of the file
	// at once through ReadAt.
	mu          sync.Mutex
	total, read int64
	interactive bool
	silent      bool
	now         func() time.Time
	last        time.Time
	nextStep    int // next quarter to print, as a percent (non-interactive)
}

// redrawInterval limits in-place redraws.
const redrawInterval = 200 * time.Millisecond

// newUploadProgress wraps r. total is the file's size; with total <= 0 nothing
// is reported, since a percentage of an unknown size would mislead.
func newUploadProgress(r io.Reader, total int64) *uploadProgress {
	return &uploadProgress{
		r: r, w: os.Stderr, total: total,
		interactive: term.IsTerminal(int(os.Stderr.Fd())),
		silent:      quiet,
		now:         time.Now,
		nextStep:    25,
	}
}

func (p *uploadProgress) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.add(int64(n))
	return n, err
}

// Seek makes the wrapper usable as an io.ReadSeeker, which the API route needs
// to rewind the body for a retry after HTTP 429. A rewind to the start resets
// the count, so a retried body is reported from 0 again rather than past 100%.
func (p *uploadProgress) Seek(offset int64, whence int) (int64, error) {
	sk, ok := p.r.(io.Seeker)
	if !ok {
		return 0, fmt.Errorf("upload body is not seekable")
	}
	pos, err := sk.Seek(offset, whence)
	if err == nil && pos == 0 {
		p.mu.Lock()
		p.read, p.nextStep = 0, 25
		p.mu.Unlock()
	}
	return pos, err
}

// add counts n more bytes and reports. The count is held at the total: a part
// the transfer reads again after a failed attempt is counted twice, and a
// progress line past 100% would be a wrong report rather than a rounding one.
func (p *uploadProgress) add(n int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.read += n
	if p.total > 0 && p.read > p.total {
		p.read = p.total
	}
	p.report()
}

// report prints the current state. The caller holds mu.
func (p *uploadProgress) report() {
	if p.silent || p.total <= 0 {
		return
	}
	pct := int(p.read * 100 / p.total)
	if p.interactive {
		now := p.now()
		if p.read < p.total && now.Sub(p.last) < redrawInterval {
			return
		}
		p.last = now
		_, _ = fmt.Fprintf(p.w, "\r  %s of %s (%d%%)\033[K", humanSize(p.read), humanSize(p.total), pct)
		return
	}
	for pct >= p.nextStep && p.nextStep <= 100 {
		_, _ = fmt.Fprintf(p.w, "  %d%% (%s of %s)\n", p.nextStep, humanSize(p.read), humanSize(p.total))
		p.nextStep += 25
	}
}

// finish erases an in-place line so the next message starts clean.
func (p *uploadProgress) finish() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.interactive && !p.silent && p.total > 0 {
		_, _ = fmt.Fprint(p.w, "\r\033[K")
	}
}

// progressFile is an *os.File that reports what is read from it. It embeds the
// file rather than wrapping a reader because the JCDS transfer sizes its parts
// from the file's length and reads them through Seek and ReadAt: a plain reader
// would hide both and change how the upload runs.
type progressFile struct {
	*os.File
	p *uploadProgress
}

func (f progressFile) Read(b []byte) (int, error) {
	n, err := f.File.Read(b)
	f.p.add(int64(n))
	return n, err
}

func (f progressFile) ReadAt(b []byte, off int64) (int, error) {
	n, err := f.File.ReadAt(b, off)
	f.p.add(int64(n))
	return n, err
}
