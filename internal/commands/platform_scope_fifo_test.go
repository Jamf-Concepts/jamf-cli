// Copyright 2026, Jamf Software LLC

//go:build unix

package commands

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A named pipe is the input readClientIDRefFile's IsRegular guard exists for,
// and the only one a downstream read cannot stand in for: os.Open blocks inside
// open(2) waiting for a writer and returns no error at all.
//
// The read runs in a goroutine against a deadline because the failure mode is a
// hang, not an error. Calling it directly would make a removed guard hang the
// package rather than fail it, which is why the timeout is the assertion.
func TestAClientIDReferenceThatIsAFIFOIsRefusedBeforeOpen(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "client-id")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo unsupported here: %v", err)
	}

	type result struct {
		id  string
		err error
	}
	done := make(chan result, 1)
	go func() {
		id, err := readClientIDRefFile(fifo)
		done <- result{id, err}
	}()

	select {
	case got := <-done:
		if got.err == nil {
			t.Errorf("readClientIDRefFile(FIFO) = %q, want a refusal", got.id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("readClientIDRefFile blocked on a named pipe: the IsRegular guard is what stops " +
			"open(2) hanging every invocation that resolves such a profile")
	}
}
