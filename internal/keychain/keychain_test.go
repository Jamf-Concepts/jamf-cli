// Copyright 2026, Jamf Software LLC

package keychain

import (
	"testing"

	gokeyring "github.com/zalando/go-keyring"
)

func TestParseRef_DefaultServicePrefix(t *testing.T) {
	service, account := ParseRef("jamf-cli/prod/client-secret")
	if service != DefaultService {
		t.Errorf("service = %q, want %q", service, DefaultService)
	}
	if account != "prod/client-secret" {
		t.Errorf("account = %q, want %q", account, "prod/client-secret")
	}
}

func TestParseRef_BareProfileField(t *testing.T) {
	service, account := ParseRef("prod/client-secret")
	if service != DefaultService {
		t.Errorf("service = %q, want %q", service, DefaultService)
	}
	if account != "prod/client-secret" {
		t.Errorf("account = %q, want %q", account, "prod/client-secret")
	}
}

func TestParseRef_CustomService(t *testing.T) {
	service, account := ParseRef("my-app/prod/client-secret")
	if service != "my-app" {
		t.Errorf("service = %q, want %q", service, "my-app")
	}
	if account != "prod/client-secret" {
		t.Errorf("account = %q, want %q", account, "prod/client-secret")
	}
}

func TestParseRef_SingleSegment(t *testing.T) {
	service, account := ParseRef("just-a-key")
	if service != DefaultService {
		t.Errorf("service = %q, want %q", service, DefaultService)
	}
	if account != "just-a-key" {
		t.Errorf("account = %q, want %q", account, "just-a-key")
	}
}

func TestKeychainRef(t *testing.T) {
	ref := KeychainRef("prod", "client-secret")
	want := "keychain:jamf-cli/prod/client-secret"
	if ref != want {
		t.Errorf("KeychainRef = %q, want %q", ref, want)
	}
}

// --- Store (systemStore) tests using go-keyring mock backend ---

func TestSystemStore_SetAndGet(t *testing.T) {
	gokeyring.MockInit()

	store := New()
	if err := store.Set("test-service", "test-account", "s3cret"); err != nil {
		t.Fatalf("Set error: %v", err)
	}

	got, err := store.Get("test-service", "test-account")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if got != "s3cret" {
		t.Errorf("Get = %q, want %q", got, "s3cret")
	}
}

func TestSystemStore_GetNotFound(t *testing.T) {
	gokeyring.MockInit()

	store := New()
	_, err := store.Get("test-service", "nonexistent")
	if err != ErrNotFound {
		t.Errorf("Get error = %v, want ErrNotFound", err)
	}
}

func TestSystemStore_DeleteNotFound(t *testing.T) {
	gokeyring.MockInit()

	store := New()
	err := store.Delete("test-service", "nonexistent")
	if err != ErrNotFound {
		t.Errorf("Delete error = %v, want ErrNotFound", err)
	}
}

func TestSystemStore_DeleteSuccess(t *testing.T) {
	gokeyring.MockInit()

	store := New()
	if err := store.Set("test-service", "del-account", "value"); err != nil {
		t.Fatalf("Set error: %v", err)
	}
	if err := store.Delete("test-service", "del-account"); err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	_, err := store.Get("test-service", "del-account")
	if err != ErrNotFound {
		t.Errorf("Get after Delete: error = %v, want ErrNotFound", err)
	}
}

func TestKeychainRef_RoundTrip(t *testing.T) {
	ref := KeychainRef("myprofile", "token")
	// Strip the "keychain:" prefix to simulate what ResolveSecret does
	raw := ref[len("keychain:"):]
	service, account := ParseRef(raw)

	if service != DefaultService {
		t.Errorf("service = %q, want %q", service, DefaultService)
	}
	if account != "myprofile/token" {
		t.Errorf("account = %q, want %q", account, "myprofile/token")
	}
}
