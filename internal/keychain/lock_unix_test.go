// Copyright 2026, Jamf Software LLC

//go:build !windows

package keychain

import (
	"bufio"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestLockHelper is the child half of TestLockFile_BlocksAnotherProcess. It
// holds the lock until its stdin closes.
func TestLockHelper(t *testing.T) {
	if os.Getenv("JAMF_KEYCHAIN_LOCK_HELPER") != "1" {
		t.Skip("child of TestLockFile_BlocksAnotherProcess")
	}
	unlock := lockFile()
	_, _ = os.Stdout.WriteString("locked\n")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	unlock()
}

// The flock is what keeps two jamf-cli processes out of each other's migration;
// the in-process mutex cannot. A second lockFile must wait for a child that
// holds the lock, and proceed once the child lets go.
func TestLockFile_BlocksAnotherProcess(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	cmd := exec.Command(os.Args[0], "-test.run=^TestLockHelper$")
	cmd.Env = append(os.Environ(), "JAMF_KEYCHAIN_LOCK_HELPER=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = cmd.Wait() })

	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "locked\n" {
		t.Fatalf("child did not take the lock: %q, %v", line, err)
	}

	acquired := make(chan struct{})
	go func() {
		unlock := lockFile()
		close(acquired)
		unlock()
	}()

	select {
	case <-acquired:
		t.Fatal("lockFile returned while another process held the lock")
	case <-time.After(300 * time.Millisecond):
	}

	_ = stdin.Close()
	select {
	case <-acquired:
	case <-time.After(5 * time.Second):
		t.Fatal("lockFile still blocked after the other process released it")
	}
}
