// Copyright 2026, Jamf Software LLC

//go:build !darwin

package keychain

func newSystemStore() Store {
	return zalandoStore{}
}
