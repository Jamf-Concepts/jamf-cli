// Copyright 2026, Jamf Software LLC

// Package progress renders determinate progress for long, countable operations
// (e.g. --all pagination): an in-place count line on an interactive terminal, or
// newline-delimited JSON page_fetch events when piped.
package progress

import (
	"fmt"
	"io"
)

// Mode selects how a Reporter renders.
type Mode int

const (
	Silent      Mode = iota // emit nothing (e.g. --quiet)
	Interactive             // in-place count line on a TTY
	Events                  // NDJSON page_fetch events (piped / --no-color)
)

// Reporter renders determinate progress to w.
type Reporter struct {
	w       io.Writer
	mode    Mode
	stopped bool
}

// New returns a Reporter writing to w in the given mode.
func New(w io.Writer, mode Mode) *Reporter {
	return &Reporter{w: w, mode: mode}
}

// Update reports that fetched items of total have been retrieved. total <= 0
// means the total is unknown.
func (r *Reporter) Update(fetched, total int) {
	switch r.mode {
	case Interactive:
		if total > 0 {
			_, _ = fmt.Fprintf(r.w, "\rFetched %d / %d\033[K", fetched, total)
		} else {
			_, _ = fmt.Fprintf(r.w, "\rFetched %d\033[K", fetched)
		}
	case Events:
		_, _ = fmt.Fprintf(r.w, `{"event":"page_fetch","fetched":%d,"total":%d}`+"\n", fetched, total)
	}
}

// Stop finalizes the reporter. In interactive mode it clears the in-place line.
// Stop is idempotent: subsequent calls after the first are no-ops.
func (r *Reporter) Stop() {
	if r.stopped {
		return
	}
	r.stopped = true
	if r.mode == Interactive {
		_, _ = fmt.Fprint(r.w, "\r\033[K")
	}
}
