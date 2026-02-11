package keychain

import "testing"

func TestParseRef_DefaultServicePrefix(t *testing.T) {
	service, account := ParseRef("jamfpro-cli/prod/client-secret")
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
	want := "keychain:jamfpro-cli/prod/client-secret"
	if ref != want {
		t.Errorf("KeychainRef = %q, want %q", ref, want)
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
