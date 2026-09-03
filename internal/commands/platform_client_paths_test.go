// Copyright 2026, Jamf Software LLC

package commands

import (
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

// TestEveryPlatformClientPathRefusesTheRetiredHost drives every code path that
// constructs a *jamfplatform.Client with a retired-gateway URL.
//
// The refusal used to live beside one caller (ResolveAuthForProfile) and not the
// others. `security` and `school` both return early from PersistentPreRunE, so
// neither reached it: a profile still naming {region}.apigw.jamf.com built a
// client against the retired host and failed inside the *token exchange*, with
// an edge-level 403 and an HTML body naming neither the host nor the reason —
// exactly the symptom the refusal exists to replace. The guard now sits in
// newPlatformSDKClient, which every path passes through, so the test drives the
// callers and asserts each surfaces it.
func TestEveryPlatformClientPathRefusesTheRetiredHost(t *testing.T) {
	const retired = "https://eu.apigw.jamf.com"

	assertRefusal := func(t *testing.T, label string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: retired host accepted; a client was built against a host that does not serve the GA paths", label)
		}
		msg := err.Error()
		// The whole value of the refusal is that it names the host and the
		// replacement, so assert both rather than only that it failed.
		for _, want := range []string{retired, "https://eu.api.jamfcloud.com", "retired"} {
			if !strings.Contains(msg, want) {
				t.Errorf("%s: refusal does not mention %q: %v", label, want, err)
			}
		}
		if code := exitcode.CodeFrom(err); code != exitcode.Usage {
			t.Errorf("%s: exit code = %d, want %d (usage)", label, code, exitcode.Usage)
		}
	}

	t.Run("newPlatformSDKClient", func(t *testing.T) {
		c, err := newPlatformSDKClient(retired, "id", "secret", auth.TenantScope("t"), false)
		if c != nil {
			t.Error("a client was returned alongside the refusal")
		}
		assertRefusal(t, "newPlatformSDKClient", err)
	})

	t.Run("securityPlatformSDKClient", func(t *testing.T) {
		t.Setenv("JAMF_URL", "")
		t.Setenv("JAMF_CLIENT_ID", "id")
		t.Setenv("JAMF_CLIENT_SECRET", "secret")
		t.Setenv("JAMF_TENANT_ID", "")
		t.Setenv("JAMF_ENVIRONMENT_ID", "")
		setScopeFlags(t, "", "")
		prevURL := serverURL
		t.Cleanup(func() { serverURL = prevURL })
		serverURL = ""

		cfg := &config.Config{
			DefaultProfile: "p",
			Profiles: map[string]config.Profile{
				"p": {Product: "security", AuthMethod: "platform", URL: retired},
			},
		}
		c, err := securityPlatformSDKClient(cfg, "p")
		if c != nil {
			t.Error("a client was returned alongside the refusal")
		}
		assertRefusal(t, "securityPlatformSDKClient", err)
	})

	t.Run("securityPlatformSDKClient via platform-url", func(t *testing.T) {
		t.Setenv("JAMF_URL", "")
		t.Setenv("JAMF_CLIENT_ID", "id")
		t.Setenv("JAMF_CLIENT_SECRET", "secret")
		t.Setenv("JAMF_TENANT_ID", "")
		t.Setenv("JAMF_ENVIRONMENT_ID", "")
		setScopeFlags(t, "", "")
		prevURL := serverURL
		t.Cleanup(func() { serverURL = prevURL })
		serverURL = ""

		// A profile carrying both credential sets keeps the gateway URL in
		// platform-url, because url is the Radar host. That is a second way in.
		cfg := &config.Config{
			DefaultProfile: "p",
			Profiles: map[string]config.Profile{
				"p": {Product: "security", AuthMethod: "security", PlatformURL: retired},
			},
		}
		_, err := securityPlatformSDKClient(cfg, "p")
		assertRefusal(t, "securityPlatformSDKClient (platform-url)", err)
	})

	t.Run("ResolveAuthForProfile", func(t *testing.T) {
		_, _, err := ResolveAuthForProfile(&config.Config{}, AuthParams{
			ServerURL:    retired,
			ClientID:     "id",
			ClientSecret: "secret",
			TenantID:     "t",
		})
		assertRefusal(t, "ResolveAuthForProfile", err)
	})
}

// TestSecurityPlatformSDKClientReportsAFailedSecretRead: a failed keychain or
// file: read used to be discarded (`if idErr == nil && secretErr == nil`), so a
// deleted keychain item, a locked login keychain or a moved path was reported by
// the caller as "no Jamf Security Cloud credentials configured" — sending the
// operator to re-enter credentials that were already stored.
func TestSecurityPlatformSDKClientReportsAFailedSecretRead(t *testing.T) {
	t.Setenv("JAMF_URL", "")
	t.Setenv("JAMF_CLIENT_ID", "")
	t.Setenv("JAMF_CLIENT_SECRET", "")
	t.Setenv("JAMF_TENANT_ID", "")
	t.Setenv("JAMF_ENVIRONMENT_ID", "")
	setScopeFlags(t, "", "")
	prevURL := serverURL
	t.Cleanup(func() { serverURL = prevURL })
	serverURL = ""

	cfg := &config.Config{
		DefaultProfile: "p",
		Profiles: map[string]config.Profile{
			"p": {
				Product:      "security",
				AuthMethod:   "platform",
				URL:          "https://eu.api.jamfcloud.com",
				ClientID:     "file:/nonexistent/does-not-exist-client-id",
				ClientSecret: "file:/nonexistent/does-not-exist-client-secret",
			},
		},
	}
	c, err := securityPlatformSDKClient(cfg, "p")
	if err == nil {
		t.Fatal("a failed secret read must be reported, not degraded into \"no credentials configured\"")
	}
	if c != nil {
		t.Error("a client was returned alongside the error")
	}
	// The message has to name the profile and the field, or the operator
	// cannot tell which of the two references is broken.
	for _, want := range []string{"client-id", `"p"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
}

// TestSecurityPlatformSDKClientReturnsNilForNoCredentials keeps the other half
// honest: a profile configured only for Risk/Device Lifecycle/SSE still gets a
// working `security` tree, so no credentials is (nil, nil) and not an error.
func TestSecurityPlatformSDKClientReturnsNilForNoCredentials(t *testing.T) {
	t.Setenv("JAMF_URL", "")
	t.Setenv("JAMF_CLIENT_ID", "")
	t.Setenv("JAMF_CLIENT_SECRET", "")
	t.Setenv("JAMF_TENANT_ID", "")
	t.Setenv("JAMF_ENVIRONMENT_ID", "")
	setScopeFlags(t, "", "")
	prevURL := serverURL
	t.Cleanup(func() { serverURL = prevURL })
	serverURL = ""

	cfg := &config.Config{
		DefaultProfile: "p",
		Profiles:       map[string]config.Profile{"p": {Product: "security", AuthMethod: "security"}},
	}
	c, err := securityPlatformSDKClient(cfg, "p")
	if err != nil {
		t.Fatalf("no credentials must not be an error: %v", err)
	}
	if c != nil {
		t.Error("a client was built from no credentials")
	}
}
