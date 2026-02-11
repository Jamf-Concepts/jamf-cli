package keychain

import (
	"errors"
	"strings"

	gokeyring "github.com/zalando/go-keyring"
)

// DefaultService is the keychain service name used by jamfpro-cli.
const DefaultService = "jamfpro-cli"

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
//	"jamfpro-cli/prod/client-secret" -> service="jamfpro-cli", account="prod/client-secret"
//	"prod/client-secret"             -> service="jamfpro-cli", account="prod/client-secret"
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

// KeychainRef builds a keychain: reference string for use in config files.
// Example: KeychainRef("prod", "client-secret") returns "keychain:jamfpro-cli/prod/client-secret"
func KeychainRef(profile, field string) string {
	return "keychain:" + DefaultService + "/" + profile + "/" + field
}
