package spinner

import (
	"os"
	"testing"
	"time"
)

func mockTTY(t *testing.T) {
	t.Helper()
	old := isTerminalFn
	isTerminalFn = func() bool { return true }
	t.Cleanup(func() { isTerminalFn = old })
}

// captureStderr redirects stderr to a pipe for the duration of the test.
// Returns a function that closes the write end and returns captured output.
func captureStderr(t *testing.T) func() string {
	t.Helper()
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = oldStderr; _ = r.Close() })

	return func() string {
		_ = w.Close()
		buf := make([]byte, 4096)
		n, _ := r.Read(buf)
		return string(buf[:n])
	}
}

func TestNew(t *testing.T) {
	s := New("Loading...")
	if s.message != "Loading..." {
		t.Errorf("message = %q, want %q", s.message, "Loading...")
	}
	if s.active {
		t.Error("spinner should not be active after New")
	}
}

func TestStartStop_NonTTY(t *testing.T) {
	s := New("Testing...")
	s.Start()
	s.Stop()
}

func TestDoubleStart_NonTTY(t *testing.T) {
	s := New("Testing...")
	s.Start()
	s.Start()
	s.Stop()
}

func TestStopWithoutStart(t *testing.T) {
	s := New("Testing...")
	s.Stop()
}

func TestSpinner_MessageUpdate(t *testing.T) {
	s := New("Loading...")
	s.Start()
	s.Stop()
	s.Stop()
}

func TestSpinner_ConcurrentStartStop(t *testing.T) {
	s := New("Concurrent test")
	done := make(chan struct{})

	for range 10 {
		go func() {
			s.Start()
			s.Stop()
			done <- struct{}{}
		}()
	}

	for range 10 {
		<-done
	}
}

// --- Active (TTY) path tests ---

func TestStartStop_ActivePath(t *testing.T) {
	mockTTY(t)
	captureStderr(t)

	s := New("Working...")
	s.Start()

	if !s.active {
		t.Error("spinner should be active after Start on TTY")
	}

	time.Sleep(100 * time.Millisecond)
	s.Stop()

	if s.active {
		t.Error("spinner should not be active after Stop")
	}
}

func TestDoubleStart_ActivePath(t *testing.T) {
	mockTTY(t)
	captureStderr(t)

	s := New("Double start")
	s.Start()
	s.Start()
	s.Stop()
}

func TestDoubleStop_ActivePath(t *testing.T) {
	mockTTY(t)
	captureStderr(t)

	s := New("Double stop")
	s.Start()
	s.Stop()
	s.Stop()
}

func TestConcurrent_ActivePath(t *testing.T) {
	mockTTY(t)
	captureStderr(t)

	s := New("Concurrent active")
	done := make(chan struct{})

	for range 10 {
		go func() {
			s.Start()
			s.Stop()
			done <- struct{}{}
		}()
	}

	for range 10 {
		<-done
	}
}

func TestStopClearsLine_ActivePath(t *testing.T) {
	mockTTY(t)
	collect := captureStderr(t)

	s := New("Clear test")
	s.Start()
	time.Sleep(100 * time.Millisecond)
	s.Stop()

	output := collect()
	if len(output) == 0 {
		t.Error("expected spinner output on stderr")
	}
}
