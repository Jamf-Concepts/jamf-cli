// Copyright 2026, Jamf Software LLC

package keychain

import (
	"bytes"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// One keychain holding both kinds of item, the way the login keychain does:
// native entries carry the marker, legacy ones do not. Native Get and Delete
// see only marked items; Exists sees both.
type fakeKeychain struct {
	mu             sync.Mutex
	native, legacy map[string]string
	addErr         error
	legacyDelErr   error
	legacySetErr   error
	existsErr      error
	getErr         error

	// staleLegacyReads counts /usr/bin/security reads or deletes that landed on
	// an item already re-created natively. On a real keychain that item no
	// longer trusts the tool, so each one is a prompt or a failure.
	staleLegacyReads int
}

func newFakeKeychain() *fakeKeychain {
	return &fakeKeychain{native: map[string]string{}, legacy: map[string]string{}}
}

func key(service, account string) string { return service + "\x00" + account }

type fakeNative struct{ kc *fakeKeychain }

func (f fakeNative) Get(s, a string) (string, error) {
	f.kc.mu.Lock()
	defer f.kc.mu.Unlock()
	runtime.Gosched()
	if f.kc.getErr != nil {
		return "", f.kc.getErr
	}
	v, ok := f.kc.native[key(s, a)]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (f fakeNative) Exists(s, a string) (bool, error) {
	f.kc.mu.Lock()
	defer f.kc.mu.Unlock()
	runtime.Gosched()
	if f.kc.existsErr != nil {
		return false, f.kc.existsErr
	}
	_, n := f.kc.native[key(s, a)]
	_, l := f.kc.legacy[key(s, a)]
	return n || l, nil
}

func (f fakeNative) Add(s, a, secret string) error {
	time.Sleep(2 * time.Millisecond) // widen the gap after the legacy delete
	f.kc.mu.Lock()
	defer f.kc.mu.Unlock()
	runtime.Gosched()
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
	f.kc.mu.Lock()
	defer f.kc.mu.Unlock()
	runtime.Gosched()
	if _, ok := f.kc.native[key(s, a)]; !ok {
		return ErrNotFound
	}
	delete(f.kc.native, key(s, a))
	return nil
}

type fakeLegacy struct{ kc *fakeKeychain }

// Get mirrors /usr/bin/security: it reads whichever item matches, marked or not.
func (f fakeLegacy) Get(s, a string) (string, error) {
	time.Sleep(time.Millisecond) // widen the window between Exists and Delete
	f.kc.mu.Lock()
	defer f.kc.mu.Unlock()
	runtime.Gosched()
	if v, ok := f.kc.legacy[key(s, a)]; ok {
		return v, nil
	}
	if v, ok := f.kc.native[key(s, a)]; ok {
		f.kc.staleLegacyReads++
		return v, nil
	}
	return "", ErrNotFound
}

func (f fakeLegacy) Set(s, a, secret string) error {
	f.kc.mu.Lock()
	defer f.kc.mu.Unlock()
	runtime.Gosched()
	if f.kc.legacySetErr != nil {
		return f.kc.legacySetErr
	}
	f.kc.legacy[key(s, a)] = secret
	return nil
}

// Delete mirrors `security delete-generic-password`: it removes whichever item
// matches the service and account, marked or not.
func (f fakeLegacy) Delete(s, a string) error {
	f.kc.mu.Lock()
	defer f.kc.mu.Unlock()
	runtime.Gosched()
	if f.kc.legacyDelErr != nil {
		return f.kc.legacyDelErr
	}
	if _, ok := f.kc.legacy[key(s, a)]; ok {
		delete(f.kc.legacy, key(s, a))
		return nil
	}
	if _, ok := f.kc.native[key(s, a)]; ok {
		f.kc.staleLegacyReads++
		delete(f.kc.native, key(s, a))
		return nil
	}
	return ErrNotFound
}

func newTestStore(kc *fakeKeychain) (*migratingStore, *bytes.Buffer) {
	var warn bytes.Buffer
	warned.Store(false)
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
	if !strings.Contains(warn.String(), "setup command") || !strings.Contains(warn.String(), `"prod"`) {
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

// Right after an upgrade several processes read one legacy item. Each must get
// the secret, and one native item must be left. The fake's Delete removes
// whichever item matches, as `security` does, so a reader that migrates after
// another has finished would delete the item the other just wrote.
func TestMigratingStore_ConcurrentGetsOfALegacyItem(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	kc := newFakeKeychain()
	kc.legacy[key(DefaultService, acct)] = "legacy"
	store, warn := newTestStore(kc)

	const readers = 16
	var wg sync.WaitGroup
	errs := make(chan error, readers)
	for range readers {
		wg.Go(func() {
			got, err := store.Get(DefaultService, acct)
			if err != nil || got != "legacy" {
				errs <- errors.New("Get = " + got + ": " + errString(err))
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	if len(kc.legacy) != 0 || len(kc.native) != 1 || kc.native[key(DefaultService, acct)] != "legacy" {
		t.Errorf("items left: native=%v legacy=%v", kc.native, kc.legacy)
	}
	if warn.Len() != 0 {
		t.Errorf("unexpected warning: %s", warn)
	}
	if kc.staleLegacyReads != 0 {
		t.Errorf("%d reads went to /usr/bin/security for an item already re-created natively", kc.staleLegacyReads)
	}
}

func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// A client ID and a secret that both fail to migrate print one warning.
func TestMigratingStore_RepeatedMigrationFailuresWarnOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	kc := newFakeKeychain()
	kc.legacy[key(DefaultService, "prod/client-id")] = "id"
	kc.legacy[key(DefaultService, "prod/client-secret")] = "secret"
	kc.legacyDelErr = errors.New("delete failed")
	store, warn := newTestStore(kc)
	// ResolveSecret builds a store per secret, so the flag cannot live on one.
	other := &migratingStore{native: fakeNative{kc}, legacy: fakeLegacy{kc}, warn: warn}

	if _, err := store.Get(DefaultService, "prod/client-id"); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Get(DefaultService, "prod/client-secret"); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(warn.String(), "Warning:"); n != 1 {
		t.Errorf("%d warnings, want 1: %s", n, warn)
	}
}

// SecItemAdd does not replace an item, so a failed Set has already removed the
// old value and the error has to say so.
func TestMigratingStore_FailedSetSaysTheOldValueWasRemoved(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	kc := newFakeKeychain()
	kc.native[key(DefaultService, acct)] = "old"
	kc.addErr = errors.New("User interaction is not allowed")
	store, _ := newTestStore(kc)

	err := store.Set(DefaultService, acct, "new")
	if err == nil || !strings.Contains(err.Error(), "removed") {
		t.Errorf("Set error = %v, want one saying the old value was removed", err)
	}
}

// A first-time Set that fails removed nothing, and must not claim it did.
func TestMigratingStore_FailedFirstSetDoesNotClaimARemoval(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	kc := newFakeKeychain()
	kc.addErr = errors.New("User interaction is not allowed")
	store, _ := newTestStore(kc)

	err := store.Set(DefaultService, acct, "new")
	if err == nil || strings.Contains(err.Error(), "removed") {
		t.Errorf("Set error = %v, want the bare failure", err)
	}
}

// removeAll deletes the native item and then fails to look for a legacy one.
// The credential is gone, and the error has to say so.
func TestMigratingStore_SetSaysTheOldValueWasRemovedWhenTheLookupFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	kc := newFakeKeychain()
	kc.native[key(DefaultService, acct)] = "old"
	kc.existsErr = errors.New("lookup failed")
	store, _ := newTestStore(kc)

	err := store.Set(DefaultService, acct, "new")
	if err == nil || !strings.Contains(err.Error(), "previous value was removed") {
		t.Errorf("Set error = %v, want one saying the old value was removed", err)
	}
	if len(kc.native) != 0 {
		t.Errorf("native item still present: %v", kc.native)
	}
}

type refusedErr struct{}

func (refusedErr) Error() string       { return "User interaction is not allowed. (OSStatus -25308)" }
func (refusedErr) AccessRefused() bool { return true }

// A read the item's access rule refuses names the other-build cause and both
// remedies; any other failure stays bare.
func TestMigratingStore_RefusedReadNamesTheCauseAndRemedies(t *testing.T) {
	kc := newFakeKeychain()
	kc.getErr = refusedErr{}
	store, _ := newTestStore(kc)

	_, err := store.Get(DefaultService, acct)
	if !errors.Is(err, kc.getErr) {
		t.Fatalf("Get error %v does not wrap the framework error", err)
	}
	for _, want := range []string{"OSStatus -25308", "different jamf-cli build", `"prod"`, "setup command", "env: or file:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}

	kc.getErr = errors.New("something else")
	if _, err := store.Get(DefaultService, acct); err == nil || strings.Contains(err.Error(), "different jamf-cli build") {
		t.Errorf("an unrelated failure was wrapped: %v", err)
	}
}

// An item under another service is never migrated, so reading it takes no lock.
func TestMigratingStore_AnotherServiceReadsWithoutTheLock(t *testing.T) {
	kc := newFakeKeychain()
	kc.legacy[key("corp", acct)] = "mine"
	store, _ := newTestStore(kc)
	store.lock = func() func() { t.Error("lock taken for a service that never migrates"); return func() {} }

	if got, err := store.Get("corp", acct); err != nil || got != "mine" {
		t.Fatalf("Get = %q, %v", got, err)
	}
}
