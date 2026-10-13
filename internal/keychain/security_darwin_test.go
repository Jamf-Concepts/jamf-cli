// Copyright 2026, Jamf Software LLC

//go:build darwin

package keychain

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

// These tests use the real login keychain, so they run only when asked:
//
//	JAMF_CLI_KEYCHAIN_INTEGRATION=1 go test ./internal/keychain -run Integration
//
// Each item is created under DefaultService (the only service that migrates)
// with an account no profile can collide with, and removed afterwards.
func integrationAccount(t *testing.T) string {
	t.Helper()
	if os.Getenv("JAMF_CLI_KEYCHAIN_INTEGRATION") != "1" {
		t.Skip("set JAMF_CLI_KEYCHAIN_INTEGRATION=1 to run against the login keychain")
	}
	account := fmt.Sprintf("integration-test-%d/client-secret", time.Now().UnixNano())
	t.Cleanup(func() {
		_ = securityFramework{}.Delete(DefaultService, account)
		_ = zalandoStore{}.Delete(DefaultService, account)
	})
	return account
}

func TestIntegration_SetWritesANativeItem(t *testing.T) {
	account := integrationAccount(t)
	store := New()

	if err := store.Set(DefaultService, account, "first"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store.Set(DefaultService, account, "s3cret with spaces & ünïcode"); err != nil {
		t.Fatalf("Set over an existing item: %v", err)
	}

	got, err := securityFramework{}.Get(DefaultService, account)
	if err != nil {
		t.Fatalf("native Get: %v", err)
	}
	if got != "s3cret with spaces & ünïcode" {
		t.Errorf("native Get = %q", got)
	}

	if err := store.Delete(DefaultService, account); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(DefaultService, account); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete: %v, want ErrNotFound", err)
	}
	if err := store.Delete(DefaultService, account); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Delete: %v, want ErrNotFound", err)
	}
}

func TestIntegration_GetMigratesALegacyItem(t *testing.T) {
	account := integrationAccount(t)

	// Written exactly as jamf-cli 1.33 and earlier wrote it.
	if err := (zalandoStore{}).Set(DefaultService, account, "legacy-secret"); err != nil {
		t.Fatalf("legacy Set: %v", err)
	}
	if _, err := (securityFramework{}).Get(DefaultService, account); !errors.Is(err, ErrNotFound) {
		t.Fatalf("native Get before migration: %v, want ErrNotFound (the item carries no marker)", err)
	}

	got, err := New().Get(DefaultService, account)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "legacy-secret" {
		t.Errorf("Get = %q, want the decoded legacy value", got)
	}

	native, err := securityFramework{}.Get(DefaultService, account)
	if err != nil {
		t.Fatalf("native Get after migration: %v", err)
	}
	if native != "legacy-secret" {
		t.Errorf("migrated value = %q, want it stored without go-keyring's prefix", native)
	}
}

func TestOSStatusError_AccessRefused(t *testing.T) {
	for status, want := range map[int32]bool{
		errSecInteractionNotAllowed: true,
		errSecAuthFailed:            true,
		errSecUserCanceled:          true,
		-25299:                      false, // duplicate item
		-34018:                      false, // missing entitlement
	} {
		if got := (&osStatusError{status: status}).AccessRefused(); got != want {
			t.Errorf("status %d: AccessRefused = %v, want %v", status, got, want)
		}
	}
}
