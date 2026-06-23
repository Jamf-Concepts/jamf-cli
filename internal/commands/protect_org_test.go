// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"encoding/base64"
	"os"
	"testing"
)

// TestExtractPKCS7Content verifies that a real CMS-signed configuration profile
// is unwrapped to its raw mobileconfig payload — the path behind
// `protect downloads <profile> --unsigned`, which lets the profile be
// re-uploaded to Jamf Pro (Jamf Pro rejects pre-signed input).
func TestExtractPKCS7Content(t *testing.T) {
	raw, err := os.ReadFile("testdata/signed_profile_sample.b64")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	der, err := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(raw)))
	if err != nil {
		t.Fatalf("decoding fixture base64: %v", err)
	}

	payload, err := extractPKCS7Content(der)
	if err != nil {
		t.Fatalf("extractPKCS7Content: %v", err)
	}

	if !bytes.HasPrefix(payload, []byte("<?xml")) {
		t.Errorf("payload does not start with XML declaration: %.40q", payload)
	}
	if !bytes.Contains(payload, []byte("</plist>")) {
		t.Error("payload does not contain </plist>")
	}
	if len(payload) >= len(der) {
		t.Errorf("stripped payload (%d) not smaller than signed envelope (%d)", len(payload), len(der))
	}
}

func TestExtractPKCS7Content_NotSigned(t *testing.T) {
	if _, err := extractPKCS7Content([]byte("<?xml version=\"1.0\"?><plist></plist>")); err == nil {
		t.Error("expected error for non-PKCS7 input, got nil")
	}
}
