// Copyright 2026, Jamf Software LLC

//go:build darwin

package keychain

import "os"

func newSystemStore() Store {
	return &migratingStore{native: securityFramework{}, legacy: zalandoStore{}, warn: os.Stderr}
}
