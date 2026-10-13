// Copyright 2026, Jamf Software LLC

//go:build darwin

package keychain

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// securityFramework reads and writes generic passwords through
// Security.framework in this process, which is the whole point of it: an item
// created here trusts the program that created it — the signed jamf-cli binary
// — where an item created by /usr/bin/security trusts /usr/bin/security, so
// that any process running as the user could read it back through that tool
// without a prompt (issue #430).
//
// The calls go through purego rather than cgo so the release build keeps
// CGO_ENABLED=0 and can still be cross-compiled.
//
// It sees only items carrying itemMarker, except for Exists. The marker is how
// an item written here is told from one written by /usr/bin/security, and it is
// readable without decrypting anything, so the test never raises a prompt.
type securityFramework struct{}

// itemMarker is stored in each item's kSecAttrDescription, which Keychain
// Access shows as the item's Kind.
const itemMarker = "jamf-cli credential"

// OSStatus values from <Security/SecBase.h>.
const (
	errSecSuccess      = 0
	errSecItemNotFound = -25300

	errSecUserCanceled          = -128
	errSecAuthFailed            = -25293
	errSecInteractionNotAllowed = -25308
)

const kCFStringEncodingUTF8 = 0x08000100

var (
	loadOnce sync.Once
	errLoad  error

	cfRelease               func(cf uintptr)
	cfStringCreateWithBytes func(alloc uintptr, bytes *byte, n int, enc uint32, external bool) uintptr
	cfStringGetLength       func(s uintptr) int
	cfStringGetCString      func(s uintptr, buf *byte, size int, enc uint32) bool
	cfDataCreate            func(alloc uintptr, bytes *byte, n int) uintptr
	cfDataGetLength         func(d uintptr) int
	cfDataGetBytePtr        func(d uintptr) *byte
	cfDictionaryCreate      func(alloc uintptr, keys, values *uintptr, n int, keyCB, valueCB uintptr) uintptr

	secItemAdd                func(attrs, result uintptr) int32
	secItemCopyMatching       func(query uintptr, result *uintptr) int32
	secItemDelete             func(query uintptr) int32
	secCopyErrorMessageString func(status int32, reserved uintptr) uintptr

	// Addresses of the two callback structs, passed as-is.
	cfTypeDictionaryKeyCallBacks   uintptr
	cfTypeDictionaryValueCallBacks uintptr

	// CFStringRef / CFBooleanRef constants, dereferenced at load.
	cfBooleanTrue            uintptr
	kSecClass                uintptr
	kSecClassGenericPassword uintptr
	kSecAttrService          uintptr
	kSecAttrAccount          uintptr
	kSecAttrLabel            uintptr
	kSecAttrDescription      uintptr
	kSecValueData            uintptr
	kSecReturnData           uintptr
	kSecReturnAttributes     uintptr
	kSecMatchLimit           uintptr
	kSecMatchLimitOne        uintptr
)

func load() error {
	loadOnce.Do(func() { errLoad = loadFrameworks() })
	return errLoad
}

func loadFrameworks() error {
	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("loading CoreFoundation: %w", err)
	}
	sec, err := purego.Dlopen("/System/Library/Frameworks/Security.framework/Security", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("loading Security.framework: %w", err)
	}

	purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
	purego.RegisterLibFunc(&cfStringCreateWithBytes, cf, "CFStringCreateWithBytes")
	purego.RegisterLibFunc(&cfStringGetLength, cf, "CFStringGetLength")
	purego.RegisterLibFunc(&cfStringGetCString, cf, "CFStringGetCString")
	purego.RegisterLibFunc(&cfDataCreate, cf, "CFDataCreate")
	purego.RegisterLibFunc(&cfDataGetLength, cf, "CFDataGetLength")
	purego.RegisterLibFunc(&cfDataGetBytePtr, cf, "CFDataGetBytePtr")
	purego.RegisterLibFunc(&cfDictionaryCreate, cf, "CFDictionaryCreate")
	purego.RegisterLibFunc(&secItemAdd, sec, "SecItemAdd")
	purego.RegisterLibFunc(&secItemCopyMatching, sec, "SecItemCopyMatching")
	purego.RegisterLibFunc(&secItemDelete, sec, "SecItemDelete")
	purego.RegisterLibFunc(&secCopyErrorMessageString, sec, "SecCopyErrorMessageString")

	for _, s := range []struct {
		lib   uintptr
		name  string
		dst   *uintptr
		deref bool
	}{
		{cf, "kCFTypeDictionaryKeyCallBacks", &cfTypeDictionaryKeyCallBacks, false},
		{cf, "kCFTypeDictionaryValueCallBacks", &cfTypeDictionaryValueCallBacks, false},
		{cf, "kCFBooleanTrue", &cfBooleanTrue, true},
		{sec, "kSecClass", &kSecClass, true},
		{sec, "kSecClassGenericPassword", &kSecClassGenericPassword, true},
		{sec, "kSecAttrService", &kSecAttrService, true},
		{sec, "kSecAttrAccount", &kSecAttrAccount, true},
		{sec, "kSecAttrLabel", &kSecAttrLabel, true},
		{sec, "kSecAttrDescription", &kSecAttrDescription, true},
		{sec, "kSecValueData", &kSecValueData, true},
		{sec, "kSecReturnData", &kSecReturnData, true},
		{sec, "kSecReturnAttributes", &kSecReturnAttributes, true},
		{sec, "kSecMatchLimit", &kSecMatchLimit, true},
		{sec, "kSecMatchLimitOne", &kSecMatchLimitOne, true},
	} {
		addr, err := purego.Dlsym(s.lib, s.name)
		if err != nil {
			return fmt.Errorf("resolving %s: %w", s.name, err)
		}
		if s.deref {
			addr = derefSymbol(addr)
		}
		*s.dst = addr
	}
	return nil
}

// derefSymbol reads the pointer-sized value stored at a C global's address.
// The uintptr is reinterpreted through its own address rather than converted,
// which is the form go vet's unsafeptr check accepts.
func derefSymbol(addr uintptr) uintptr {
	p := *(*unsafe.Pointer)(unsafe.Pointer(&addr))
	return *(*uintptr)(p)
}

// osStatusError is a Security.framework failure, carrying the framework's own
// description of the code.
type osStatusError struct {
	status  int32
	message string
}

func (e *osStatusError) Error() string {
	if e.message == "" {
		return fmt.Sprintf("Security.framework error %d", e.status)
	}
	return fmt.Sprintf("%s (OSStatus %d)", e.message, e.status)
}

// AccessRefused reports a status that means the item's access rule refused the
// read: the prompt it needs cannot be shown, failed, or was dismissed.
func (e *osStatusError) AccessRefused() bool {
	switch e.status {
	case errSecInteractionNotAllowed, errSecAuthFailed, errSecUserCanceled:
		return true
	}
	return false
}

func statusError(status int32) error {
	if status == errSecItemNotFound {
		return ErrNotFound
	}
	msg := ""
	if s := secCopyErrorMessageString(status, 0); s != 0 {
		msg = goString(s)
		cfRelease(s)
	}
	return &osStatusError{status: status, message: msg}
}

func cfString(s string) uintptr {
	b := []byte(s)
	var p *byte
	if len(b) > 0 {
		p = &b[0]
	}
	return cfStringCreateWithBytes(0, p, len(b), kCFStringEncodingUTF8, false)
}

func goString(s uintptr) string {
	// UTF-8 needs at most four bytes per UTF-16 unit, plus the terminator.
	buf := make([]byte, cfStringGetLength(s)*4+1)
	if !cfStringGetCString(s, &buf[0], len(buf), kCFStringEncodingUTF8) {
		return ""
	}
	for i, c := range buf {
		if c == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}

// query builds a CFDictionary from key/value pairs. str and data create the CF
// value they add; ref adds a CF constant. build releases what str and data
// created once the dictionary has retained it.
type query struct {
	keys, values []uintptr
	owned        []uintptr
}

func (q *query) str(key uintptr, value string) *query {
	v := cfString(value)
	q.owned = append(q.owned, v)
	return q.ref(key, v)
}

func (q *query) data(key uintptr, value []byte) *query {
	var p *byte
	if len(value) > 0 {
		p = &value[0]
	}
	v := cfDataCreate(0, p, len(value))
	q.owned = append(q.owned, v)
	return q.ref(key, v)
}

func (q *query) ref(key, value uintptr) *query {
	q.keys = append(q.keys, key)
	q.values = append(q.values, value)
	return q
}

// build returns the dictionary; the caller releases it.
func (q *query) build() uintptr {
	d := cfDictionaryCreate(0, &q.keys[0], &q.values[0], len(q.keys), cfTypeDictionaryKeyCallBacks, cfTypeDictionaryValueCallBacks)
	for _, o := range q.owned {
		cfRelease(o)
	}
	q.owned = nil
	return d
}

// item starts a query for one generic password. marked restricts it to items
// written by securityFramework.
func item(service, account string, marked bool) *query {
	q := (&query{}).
		ref(kSecClass, kSecClassGenericPassword).
		str(kSecAttrService, service).
		str(kSecAttrAccount, account)
	if marked {
		q.str(kSecAttrDescription, itemMarker)
	}
	return q
}

var errNotData = errors.New("keychain returned a value that is not data")

// Get returns the secret of the marked item, or ErrNotFound. Reading an item
// that trusts a different build of jamf-cli raises the system's own prompt.
func (securityFramework) Get(service, account string) (string, error) {
	if err := load(); err != nil {
		return "", err
	}
	q := item(service, account, true).
		ref(kSecReturnData, cfBooleanTrue).
		ref(kSecMatchLimit, kSecMatchLimitOne).
		build()
	defer cfRelease(q)

	var result uintptr
	if status := secItemCopyMatching(q, &result); status != errSecSuccess {
		return "", statusError(status)
	}
	if result == 0 {
		return "", errNotData
	}
	defer cfRelease(result)

	n := cfDataGetLength(result)
	if n == 0 {
		return "", nil
	}
	return string(unsafe.Slice(cfDataGetBytePtr(result), n)), nil
}

// Exists reports whether any item — marked or not — is stored for the pair.
// It asks for attributes only, which needs no decryption and raises no prompt.
func (securityFramework) Exists(service, account string) (bool, error) {
	if err := load(); err != nil {
		return false, err
	}
	q := item(service, account, false).
		ref(kSecReturnAttributes, cfBooleanTrue).
		ref(kSecMatchLimit, kSecMatchLimitOne).
		build()
	defer cfRelease(q)

	var result uintptr
	status := secItemCopyMatching(q, &result)
	if result != 0 {
		cfRelease(result)
	}
	switch status {
	case errSecSuccess:
		return true, nil
	case errSecItemNotFound:
		return false, nil
	}
	return false, statusError(status)
}

// Add creates a marked item. Its default access trusts this executable only.
func (securityFramework) Add(service, account, secret string) error {
	if err := load(); err != nil {
		return err
	}
	q := item(service, account, true).
		str(kSecAttrLabel, service).
		data(kSecValueData, []byte(secret)).
		build()
	defer cfRelease(q)

	if status := secItemAdd(q, 0); status != errSecSuccess {
		return statusError(status)
	}
	return nil
}

// Delete removes the marked item, or answers ErrNotFound.
func (securityFramework) Delete(service, account string) error {
	if err := load(); err != nil {
		return err
	}
	q := item(service, account, true).build()
	defer cfRelease(q)

	if status := secItemDelete(q); status != errSecSuccess {
		return statusError(status)
	}
	return nil
}
