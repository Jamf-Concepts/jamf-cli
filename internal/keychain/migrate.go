// Copyright 2026, Jamf Software LLC

package keychain

import (
	"errors"
	"fmt"
	"io"
)

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
}

func (m *migratingStore) Get(service, account string) (string, error) {
	secret, err := m.native.Get(service, account)
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
// tried again on the next read.
func (m *migratingStore) migrate(service, account, secret string) {
	if err := m.legacy.Delete(service, account); err != nil {
		m.warnf("Warning: keychain item %s/%s still trusts /usr/bin/security; removing it for re-creation failed: %v\n", service, account, err)
		return
	}
	err := m.native.Add(service, account, secret)
	if err == nil {
		return
	}
	// The item is gone and the new one was not written. Put the old one back,
	// so a failed migration costs nothing but the migration.
	if rerr := m.legacy.Set(service, account, secret); rerr != nil {
		m.warnf("Warning: keychain item %s/%s was removed for re-creation and could not be written again (%v; restoring it failed: %v). Store the credential again with jamf-cli config add-profile or the product's setup command.\n", service, account, err, rerr)
		return
	}
	m.warnf("Warning: keychain item %s/%s still trusts /usr/bin/security; re-creating it failed: %v\n", service, account, err)
}

// Set replaces whatever is stored for the pair, so the item it leaves trusts
// the running binary and nothing else.
func (m *migratingStore) Set(service, account, secret string) error {
	if err := m.removeAll(service, account); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return m.native.Add(service, account, secret)
}

func (m *migratingStore) Delete(service, account string) error {
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

func (m *migratingStore) warnf(format string, args ...any) {
	if m.warn != nil {
		_, _ = fmt.Fprintf(m.warn, format, args...)
	}
}
