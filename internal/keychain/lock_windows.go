// Copyright 2026, Jamf Software LLC

//go:build windows

package keychain

// lockFile has nothing to take on Windows: the Windows store never migrates.
func lockFile() func() { return func() {} }
