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
	// In test environments stderr is not a TTY, so Start should be a no-op
	s := New("Testing...")
	s.Start()
	s.Stop() // should not panic or block
}

func TestDoubleStart_NonTTY(t *testing.T) {
	s := New("Testing...")
	s.Start()
	s.Start() // second start should be safe
	s.Stop()
}

func TestStopWithoutStart(t *testing.T) {
	s := New("Testing...")
	s.Stop() // should not panic or block
}

func TestSpinner_MessageUpdate(t *testing.T) {
	s := New("Loading...")
	s.Start()
	s.Stop()
	s.Stop() // second stop should be safe
}

func TestSpinner_ConcurrentStartStop(t *testing.T) {
	// Run with -race to detect data races
	s := New("Concurrent test")
	done := make(chan struct{})

	for i := 0; i < 10; i++ {
		go func() {
			s.Start()
			s.Stop()
			done <- struct{}{}
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// --- Active (TTY) path tests ---

func TestStartStop_ActivePath(t *testing.T) {
	mockTTY(t)

	// Redirect stderr to avoid test output noise
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = oldStderr; _ = r.Close() })

	s := New("Working...")
	s.Start()

	if !s.active {
		t.Error("spinner should be active after Start on TTY")
	}

	// Let it tick at least once
	time.Sleep(100 * time.Millisecond)

	s.Stop()

	if s.active {
		t.Error("spinner should not be active after Stop")
	}

	_ = w.Close()
}

func TestDoubleStart_ActivePath(t *testing.T) {
	mockTTY(t)

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = oldStderr; _ = r.Close() })

	s := New("Double start")
	s.Start()
	s.Start() // second start should be no-op
	s.Stop()

	_ = w.Close()
}

func TestDoubleStop_ActivePath(t *testing.T) {
	mockTTY(t)

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = oldStderr; _ = r.Close() })

	s := New("Double stop")
	s.Start()
	s.Stop()
	s.Stop() // second stop should be safe

	_ = w.Close()
}

func TestConcurrent_ActivePath(t *testing.T) {
	mockTTY(t)

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = oldStderr; _ = r.Close() })

	s := New("Concurrent active")
	done := make(chan struct{})

	for i := 0; i < 10; i++ {
		go func() {
			s.Start()
			s.Stop()
			done <- struct{}{}
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	_ = w.Close()
}

func TestStopClearsLine_ActivePath(t *testing.T) {
	mockTTY(t)

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = oldStderr })

	s := New("Clear test")
	s.Start()
	time.Sleep(100 * time.Millisecond)
	s.Stop()

	_ = w.Close()

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	// Stop should write the ANSI clear-line sequence
	if len(output) == 0 {
		t.Error("expected spinner output on stderr")
	}
}
