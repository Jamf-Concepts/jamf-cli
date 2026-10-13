// Copyright 2026, Jamf Software LLC

package keychain

import (
	"errors"

	gokeyring "github.com/zalando/go-keyring"
)

// zalandoStore implements Store using go-keyring. It is the store on Linux and
// Windows. On macOS it is only the way to read, and remove, an item written
// by an earlier jamf-cli: go-keyring there drives /usr/bin/security, and that
// tool is what such an item trusts.
type zalandoStore struct{}

func (zalandoStore) Get(service, account string) (string, error) {
	secret, err := gokeyring.Get(service, account)
	if err != nil {
		if errors.Is(err, gokeyring.ErrNotFound) {
			return "", ErrNotFound
		}
		return "", err
	}
	return secret, nil
}

func (zalandoStore) Set(service, account, secret string) error {
	return gokeyring.Set(service, account, secret)
}

func (zalandoStore) Delete(service, account string) error {
	err := gokeyring.Delete(service, account)
	if err != nil && errors.Is(err, gokeyring.ErrNotFound) {
		return ErrNotFound
	}
	return err
}
