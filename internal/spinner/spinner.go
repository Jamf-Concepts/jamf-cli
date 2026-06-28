// Copyright 2026, Jamf Software LLC

package spinner

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/term"
)

type suppressKey struct{}

// WithSuppressed marks ctx so spinner wrappers skip the animation for requests
// made under it (used by paginated --all loops that show determinate progress).
func WithSuppressed(ctx context.Context) context.Context {
	return context.WithValue(ctx, suppressKey{}, true)
}

// IsSuppressed reports whether ctx was marked by WithSuppressed.
func IsSuppressed(ctx context.Context) bool {
	v, _ := ctx.Value(suppressKey{}).(bool)
	return v
}

var frames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// isTerminalFn controls the TTY check. Override in tests to force the active path.
var isTerminalFn = func() bool {
	return term.IsTerminal(int(os.Stderr.Fd()))
}

// Spinner displays a braille animation on stderr while work is in progress.
type Spinner struct {
	message string
	stop    chan struct{}
	done    chan struct{}
	mu      sync.Mutex
	started bool // true after first Start call (prevents double-close)
	active  bool // true only when the goroutine is running
}

// New creates a spinner with the given status message.
func New(message string) *Spinner {
	return &Spinner{
		message: message,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

// Start begins the spinner animation. It is a no-op if stderr is not a TTY
// or if Start was already called.
func (s *Spinner) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return
	}
	s.started = true

	// Only spin when stderr is a terminal
	if !isTerminalFn() {
		close(s.done)
		return
	}

	s.active = true
	go s.run()
}

// Stop halts the spinner and clears the line.
func (s *Spinner) Stop() {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return
	}
	s.active = false
	s.mu.Unlock()

	close(s.stop)
	<-s.done
	// Clear the spinner line
	fmt.Fprint(os.Stderr, "\r\033[K")
}

func (s *Spinner) run() {
	defer close(s.done)
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	i := 0
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			fmt.Fprintf(os.Stderr, "\r%c %s", frames[i%len(frames)], s.message)
			i++
		}
	}
}
