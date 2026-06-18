// Copyright 2026, Jamf Software LLC

package keychain

import (
	"errors"
	"fmt"
	"runtime"
	"strings"

	gokeyring "github.com/zalando/go-keyring"
)

// DefaultService is the keychain service name used by jamf-cli.
const DefaultService = "jamf-cli"

// ErrNotFound is returned when a keychain item does not exist.
var ErrNotFound = errors.New("keychain item not found")

// Store provides access to the system keychain.
type Store interface {
	Get(service, account string) (string, error)
	Set(service, account, secret string) error
	Delete(service, account string) error
}

// New returns a Store backed by the system keychain.
func New() Store {
	return &systemStore{}
}

// systemStore implements Store using go-keyring.
type systemStore struct{}

func (s *systemStore) Get(service, account string) (string, error) {
	secret, err := gokeyring.Get(service, account)
	if err != nil {
		if errors.Is(err, gokeyring.ErrNotFound) {
			return "", ErrNotFound
		}
		return "", err
	}
	return secret, nil
}

func (s *systemStore) Set(service, account, secret string) error {
	return gokeyring.Set(service, account, secret)
}

func (s *systemStore) Delete(service, account string) error {
	err := gokeyring.Delete(service, account)
	if err != nil && errors.Is(err, gokeyring.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

// ParseRef parses a keychain reference value into service and account.
// The input is the portion after the "keychain:" prefix.
//
// Format: "service/account/subkey" or just "account/subkey"
// If the value contains exactly two slashes, the first segment is the service.
// Otherwise DefaultService is used and the entire value is the account.
//
// Examples:
//
//	"jamf-cli/prod/client-secret" -> service="jamf-cli", account="prod/client-secret"
//	"prod/client-secret"             -> service="jamf-cli", account="prod/client-secret"
func ParseRef(value string) (service, account string) {
	// If the value starts with the default service name followed by a slash,
	// treat the first segment as the service.
	if after, ok := strings.CutPrefix(value, DefaultService+"/"); ok {
		return DefaultService, after
	}

	// Check for a custom service prefix: three segments means service/profile/field
	parts := strings.SplitN(value, "/", 3)
	if len(parts) == 3 {
		return parts[0], parts[1] + "/" + parts[2]
	}

	return DefaultService, value
}

// WriteError wraps a failed keychain write (Store.Set) with actionable,
// OS-specific guidance. The underlying go-keyring backends shell out to the
// system credential store and surface only the process exit code (e.g. macOS
// "exit status 154"), discarding the descriptive message — so on its own the
// raw error tells the user nothing about how to fix it. item is a short,
// human-readable label for what was being stored (e.g. "client ID").
func WriteError(item string, err error) error {
	// The guidance body is built separately and passed as a trailing %s so the
	// fmt.Errorf format string does not end in punctuation/newline (staticcheck
	// ST1005). The body is deliberately multi-line, user-facing text.
	var store, guidance string
	switch runtime.GOOS {
	case "darwin":
		store = "macOS keychain"
		guidance = "The login keychain rejected the write. It is usually locked, not the\n" +
			"default keychain, or unavailable in this session (e.g. over SSH).\n\n" +
			"Try unlocking it:\n" +
			"  security unlock-keychain ~/Library/Keychains/login.keychain-db\n\n" +
			"To see the underlying error directly:\n" +
			"  security add-generic-password -U -s " + DefaultService + " -a jamf-cli-test -w test\n\n" +
			"Alternatively, store secrets without the keychain using env: or file:\n" +
			"secret references in your config profile."
	case "linux":
		store = "system keyring"
		guidance = "The Secret Service backend (e.g. gnome-keyring) is usually locked or\n" +
			"unavailable in this session (e.g. a headless or SSH session with no\n" +
			"D-Bus / keyring daemon running).\n\n" +
			"Alternatively, store secrets without the keyring using env: or file:\n" +
			"secret references in your config profile."
	default:
		store = "system keychain"
		guidance = "The system credential store rejected the write. It may be locked or\n" +
			"unavailable in this session.\n\n" +
			"Alternatively, store secrets without the keychain using env: or file:\n" +
			"secret references in your config profile."
	}
	return fmt.Errorf("failed to store %s in the %s: %w\n\n%s", item, store, err, guidance)
}

// KeychainRef builds a keychain: reference string for use in config files.
// Example: KeychainRef("prod", "client-secret") returns "keychain:jamf-cli/prod/client-secret"
func KeychainRef(profile, field string) string {
	return "keychain:" + DefaultService + "/" + profile + "/" + field
}
