package spinner

import (
	"testing"
)

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
