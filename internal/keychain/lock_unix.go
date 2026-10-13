// Copyright 2026, Jamf Software LLC

//go:build !windows

package keychain

import (
	"os"
	"path/filepath"
	"syscall"
)

// lockFile takes an exclusive advisory lock on a file under the user cache
// directory, so that two jamf-cli processes do not migrate one item at once.
// It is best effort: where the directory or the lock is unavailable the caller
// carries on holding only the in-process mutex, which is no worse than before.
func lockFile() func() {
	dir, err := os.UserCacheDir()
	if err != nil {
		return func() {}
	}
	dir = filepath.Join(dir, "jamf-cli")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return func() {}
	}
	f, err := os.OpenFile(filepath.Join(dir, "keychain.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return func() {}
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}
}
