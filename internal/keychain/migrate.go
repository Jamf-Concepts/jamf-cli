// Copyright 2026, Jamf Software LLC

package keychain

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// processLock serializes keychain writes and migrations inside one process;
// lockFile does the same across processes.
var processLock sync.Mutex

// lockItem blocks until this process holds the right to change an item's
// storage. One lock covers every item: a migration is two keychain calls, and
// the contention is the first read after an upgrade.
func lockItem() func() {
	processLock.Lock()
	unlockFile := lockFile()
	return func() {
		unlockFile()
		processLock.Unlock()
	}
}

// nativeStore is the in-process keychain (securityFramework on macOS). Get and
// Delete see only items it wrote itself; Exists sees any item for the pair.
type nativeStore interface {
	Get(service, account string) (string, error)
	Exists(service, account string) (bool, error)
	Add(service, account, secret string) error
	Delete(service, account string) error
}

// migratingStore is the macOS Store. It writes every item through native, and
// replaces a jamf-cli item written by an earlier release the first time that
// item is read.
//
// An earlier release wrote through /usr/bin/security, so the item's access
// rule trusts that tool, and any process running as the user can read the item
// with it while the login keychain is unlocked, with no prompt (issue #430).
// Replacing the library does not change the rule on an item that already
// exists, which is why the item is re-created.
//
// The legacy item is read and removed through legacy — /usr/bin/security —
// because that is the program it trusts: reading it in-process would raise a
// prompt once per item for no gain. Only items under DefaultService are
// migrated. An item under another service is the user's own, and is read the
// old way and left as it is.
//
// After migration an earlier jamf-cli can still read the item, but only after
// the user approves the system prompt, because the item no longer trusts
// /usr/bin/security.
type migratingStore struct {
	native nativeStore
	legacy Store
	warn   io.Writer

	// lock serializes migration and writes. nil means lockItem.
	lock func() func()

	warned atomic.Bool
}

func (m *migratingStore) acquire() func() {
	if m.lock != nil {
		return m.lock()
	}
	return lockItem()
}

func (m *migratingStore) Get(service, account string) (string, error) {
	secret, err := m.native.Get(service, account)
	if !errors.Is(err, ErrNotFound) {
		return secret, err
	}

	// Another process may be migrating this item: it removes the legacy item
	// before it writes the native one, and in between neither is visible.
	// Take the lock, then look again, so that process finishes first.
	unlock := m.acquire()
	defer unlock()
	secret, err = m.native.Get(service, account)
	if !errors.Is(err, ErrNotFound) {
		return secret, err
	}

	exists, err := m.native.Exists(service, account)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", ErrNotFound
	}

	secret, err = m.legacy.Get(service, account)
	if errors.Is(err, ErrNotFound) {
		// Exists said yes and the legacy read says no: an earlier jamf-cli,
		// which takes no lock, migrated it in between.
		return m.native.Get(service, account)
	}
	if err != nil {
		return "", err
	}
	if service == DefaultService {
		m.migrate(service, account, secret)
	}
	return secret, nil
}

// migrate re-creates a legacy item in-process. A failure is reported and
// otherwise ignored: the caller already holds the secret, and the item is
// tried again on the next read. The caller holds the lock, which keeps other
// jamf-cli processes out of the window between the delete and the add.
//
// The lock cannot make the window safe against a kill. SecItemAdd refuses a
// second item for the same service and account while the legacy one exists, so
// the delete has to come first, and a process killed between the two calls
// loses the only copy. Store the credential again to recover.
func (m *migratingStore) migrate(service, account, secret string) {
	if err := m.legacy.Delete(service, account); err != nil {
		m.warnOnce("Warning: keychain item %s/%s still trusts /usr/bin/security; removing it for re-creation failed: %v\n", service, account, err)
		return
	}
	err := m.native.Add(service, account, secret)
	if err == nil {
		return
	}
	// The item is gone and the new one was not written. Put the old one back,
	// so a migration that fails but is not interrupted loses nothing.
	if rerr := m.legacy.Set(service, account, secret); rerr != nil {
		m.warnf("Warning: keychain item %s/%s was removed for re-creation and could not be written again (%v; restoring it failed: %v). Store the credential again with jamf-cli config add-profile or the product's setup command.\n", service, account, err, rerr)
		return
	}
	m.warnOnce("Warning: keychain item %s/%s still trusts /usr/bin/security; re-creating it failed: %v\n", service, account, err)
}

// Set replaces whatever is stored for the pair, so the item it leaves trusts
// the running binary and nothing else.
func (m *migratingStore) Set(service, account, secret string) error {
	unlock := m.acquire()
	defer unlock()
	rerr := m.removeAll(service, account)
	if rerr != nil && !errors.Is(rerr, ErrNotFound) {
		return rerr
	}
	err := m.native.Add(service, account, secret)
	if err != nil && rerr == nil {
		// SecItemAdd will not replace an item, so the old one went first.
		return fmt.Errorf("%w (the previous value was removed; store the credential again)", err)
	}
	return err
}

func (m *migratingStore) Delete(service, account string) error {
	unlock := m.acquire()
	defer unlock()
	return m.removeAll(service, account)
}

// removeAll deletes the native item and then any legacy one, answering
// ErrNotFound only when there was neither.
func (m *migratingStore) removeAll(service, account string) error {
	nerr := m.native.Delete(service, account)
	if nerr != nil && !errors.Is(nerr, ErrNotFound) {
		return nerr
	}

	exists, err := m.native.Exists(service, account)
	if err != nil {
		return err
	}
	if !exists {
		return nerr
	}
	if err := m.legacy.Delete(service, account); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return nil
}

// warnOnce prints the first of the "still trusts /usr/bin/security" warnings in
// a process. A client ID and a client secret that both fail say the same thing
// twice, and the next read tries again.
func (m *migratingStore) warnOnce(format string, args ...any) {
	if m.warned.CompareAndSwap(false, true) {
		m.warnf(format, args...)
	}
}

func (m *migratingStore) warnf(format string, args ...any) {
	if m.warn != nil {
		_, _ = fmt.Fprintf(m.warn, format, args...)
	}
}
