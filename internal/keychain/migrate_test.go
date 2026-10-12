// Copyright 2026, Jamf Software LLC

package keychain

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// One keychain holding both kinds of item, the way the login keychain does:
// native entries carry the marker, legacy ones do not. Native Get and Delete
// see only marked items; Exists sees both.
type fakeKeychain struct {
	native, legacy map[string]string
	addErr         error
	legacyDelErr   error
	legacySetErr   error
}

func newFakeKeychain() *fakeKeychain {
	return &fakeKeychain{native: map[string]string{}, legacy: map[string]string{}}
}

func key(service, account string) string { return service + "\x00" + account }

type fakeNative struct{ kc *fakeKeychain }

func (f fakeNative) Get(s, a string) (string, error) {
	v, ok := f.kc.native[key(s, a)]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (f fakeNative) Exists(s, a string) (bool, error) {
	_, n := f.kc.native[key(s, a)]
	_, l := f.kc.legacy[key(s, a)]
	return n || l, nil
}

func (f fakeNative) Add(s, a, secret string) error {
	if f.kc.addErr != nil {
		return f.kc.addErr
	}
	if _, ok := f.kc.native[key(s, a)]; ok {
		return errors.New("duplicate item")
	}
	if _, ok := f.kc.legacy[key(s, a)]; ok {
		return errors.New("duplicate item")
	}
	f.kc.native[key(s, a)] = secret
	return nil
}

func (f fakeNative) Delete(s, a string) error {
	if _, ok := f.kc.native[key(s, a)]; !ok {
		return ErrNotFound
	}
	delete(f.kc.native, key(s, a))
	return nil
}

type fakeLegacy struct{ kc *fakeKeychain }

// Get mirrors /usr/bin/security: it reads whichever item matches, marked or not.
func (f fakeLegacy) Get(s, a string) (string, error) {
	if v, ok := f.kc.legacy[key(s, a)]; ok {
		return v, nil
	}
	if v, ok := f.kc.native[key(s, a)]; ok {
		return v, nil
	}
	return "", ErrNotFound
}

func (f fakeLegacy) Set(s, a, secret string) error {
	if f.kc.legacySetErr != nil {
		return f.kc.legacySetErr
	}
	f.kc.legacy[key(s, a)] = secret
	return nil
}

func (f fakeLegacy) Delete(s, a string) error {
	if f.kc.legacyDelErr != nil {
		return f.kc.legacyDelErr
	}
	if _, ok := f.kc.legacy[key(s, a)]; !ok {
		return ErrNotFound
	}
	delete(f.kc.legacy, key(s, a))
	return nil
}

func newTestStore(kc *fakeKeychain) (*migratingStore, *bytes.Buffer) {
	var warn bytes.Buffer
	return &migratingStore{native: fakeNative{kc}, legacy: fakeLegacy{kc}, warn: &warn}, &warn
}

const acct = "prod/client-secret"

func TestMigratingStore_GetReadsANativeItem(t *testing.T) {
	kc := newFakeKeychain()
	kc.native[key(DefaultService, acct)] = "native"
	store, warn := newTestStore(kc)

	got, err := store.Get(DefaultService, acct)
	if err != nil || got != "native" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if warn.Len() != 0 {
		t.Errorf("unexpected warning: %s", warn)
	}
}

func TestMigratingStore_GetMigratesALegacyItem(t *testing.T) {
	kc := newFakeKeychain()
	kc.legacy[key(DefaultService, acct)] = "legacy"
	store, warn := newTestStore(kc)

	got, err := store.Get(DefaultService, acct)
	if err != nil || got != "legacy" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if _, ok := kc.legacy[key(DefaultService, acct)]; ok {
		t.Error("legacy item still present after migration")
	}
	if kc.native[key(DefaultService, acct)] != "legacy" {
		t.Errorf("native item = %q, want the legacy value", kc.native[key(DefaultService, acct)])
	}
	if warn.Len() != 0 {
		t.Errorf("unexpected warning: %s", warn)
	}
}

// An item under another service is the user's own: read, never rewritten.
func TestMigratingStore_GetLeavesAnotherServicesItemAlone(t *testing.T) {
	kc := newFakeKeychain()
	kc.legacy[key("my-app", acct)] = "theirs"
	store, _ := newTestStore(kc)

	got, err := store.Get("my-app", acct)
	if err != nil || got != "theirs" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if _, ok := kc.legacy[key("my-app", acct)]; !ok {
		t.Error("custom-service item was migrated")
	}
	if len(kc.native) != 0 {
		t.Error("custom-service item was copied into a native item")
	}
}

func TestMigratingStore_GetNotFound(t *testing.T) {
	store, _ := newTestStore(newFakeKeychain())
	if _, err := store.Get(DefaultService, acct); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get error = %v, want ErrNotFound", err)
	}
}

// A failed write after the delete restores the legacy item, so a failed
// migration loses nothing and is retried on the next read.
func TestMigratingStore_FailedMigrationRestoresTheLegacyItem(t *testing.T) {
	kc := newFakeKeychain()
	kc.legacy[key(DefaultService, acct)] = "legacy"
	kc.addErr = errors.New("User interaction is not allowed")
	store, warn := newTestStore(kc)

	got, err := store.Get(DefaultService, acct)
	if err != nil || got != "legacy" {
		t.Fatalf("Get = %q, %v; a failed migration must still return the secret", got, err)
	}
	if kc.legacy[key(DefaultService, acct)] != "legacy" {
		t.Error("legacy item not restored after the native write failed")
	}
	if !strings.Contains(warn.String(), "re-creating it failed") {
		t.Errorf("warning = %q", warn)
	}
}

func TestMigratingStore_LostItemNamesTheRemedy(t *testing.T) {
	kc := newFakeKeychain()
	kc.legacy[key(DefaultService, acct)] = "legacy"
	kc.addErr = errors.New("add failed")
	kc.legacySetErr = errors.New("restore failed")
	store, warn := newTestStore(kc)

	if got, err := store.Get(DefaultService, acct); err != nil || got != "legacy" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if !strings.Contains(warn.String(), "config add-profile") {
		t.Errorf("warning does not say how to store the credential again: %q", warn)
	}
}

func TestMigratingStore_FailedDeleteLeavesTheItem(t *testing.T) {
	kc := newFakeKeychain()
	kc.legacy[key(DefaultService, acct)] = "legacy"
	kc.legacyDelErr = errors.New("delete failed")
	store, warn := newTestStore(kc)

	if got, err := store.Get(DefaultService, acct); err != nil || got != "legacy" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if len(kc.native) != 0 {
		t.Error("native item written although the legacy one could not be removed")
	}
	if !strings.Contains(warn.String(), "still trusts /usr/bin/security") {
		t.Errorf("warning = %q", warn)
	}
}

func TestMigratingStore_SetReplacesALegacyItem(t *testing.T) {
	kc := newFakeKeychain()
	kc.legacy[key(DefaultService, acct)] = "old"
	store, _ := newTestStore(kc)

	if err := store.Set(DefaultService, acct, "new"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, ok := kc.legacy[key(DefaultService, acct)]; ok {
		t.Error("legacy item survived Set")
	}
	if kc.native[key(DefaultService, acct)] != "new" {
		t.Errorf("native item = %q", kc.native[key(DefaultService, acct)])
	}
}

func TestMigratingStore_SetReplacesANativeItem(t *testing.T) {
	kc := newFakeKeychain()
	kc.native[key(DefaultService, acct)] = "old"
	store, _ := newTestStore(kc)

	if err := store.Set(DefaultService, acct, "new"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if kc.native[key(DefaultService, acct)] != "new" {
		t.Errorf("native item = %q", kc.native[key(DefaultService, acct)])
	}
}

func TestMigratingStore_DeleteRemovesBothKinds(t *testing.T) {
	kc := newFakeKeychain()
	kc.native[key(DefaultService, acct)] = "n"
	kc.legacy[key(DefaultService, acct)] = "l"
	store, _ := newTestStore(kc)

	if err := store.Delete(DefaultService, acct); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(kc.native)+len(kc.legacy) != 0 {
		t.Errorf("items left: native=%v legacy=%v", kc.native, kc.legacy)
	}
	if err := store.Delete(DefaultService, acct); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Delete = %v, want ErrNotFound", err)
	}
}

func TestMigratingStore_DeleteALegacyItemOnly(t *testing.T) {
	kc := newFakeKeychain()
	kc.legacy[key(DefaultService, acct)] = "l"
	store, _ := newTestStore(kc)

	if err := store.Delete(DefaultService, acct); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(kc.legacy) != 0 {
		t.Error("legacy item survived Delete")
	}
}
