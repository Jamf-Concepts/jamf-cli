// Copyright 2026, Jamf Software LLC

package commands

import (
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
)

// radarHalf is what `security setup` writes: the three scoped application pairs
// and the SSE host. Kept as a helper so both orderings assert the same set.
func radarHalf(p config.Profile) config.Profile {
	p.RiskClientID = "keychain:jamf-cli/p/risk-client-id"
	p.RiskClientSecret = "keychain:jamf-cli/p/risk-client-secret"
	p.LifecycleClientID = "keychain:jamf-cli/p/lifecycle-client-id"
	p.LifecycleClientSecret = "keychain:jamf-cli/p/lifecycle-client-secret"
	p.SSEClientID = "keychain:jamf-cli/p/sse-client-id"
	p.SSEClientSecret = "keychain:jamf-cli/p/sse-client-secret"
	p.SSEURL = "https://sse.jamf.com"
	return p
}

func gatewayCreds() *platformGatewayCredentials {
	return &platformGatewayCredentials{
		GatewayURL:    "https://eu.api.jamfcloud.com",
		ClientID:      "gw-id",
		ClientSecret:  "gw-secret",
		EnvironmentID: "env-1",
	}
}

// TestBothSetupsAgainstOneProfileKeepBothCredentialSets is the regression test
// for the finding that `platform setup` and `security setup` overwrote each
// other.
//
// Both assigned a freshly-constructed config.Profile literal into
// cfg.Profiles[name], and config.Profile carries both products' credentials —
// which is what made the loss silent. That was harmless while the two products
// used disjoint profiles; this change ends it, because `security setup` now
// closes by pointing at `platform setup` and securityPlatformSDKClient is built
// around a profile carrying both sets. Run in each order, because the merge has
// to be symmetric: whichever ran second used to win outright, and re-running
// the loser to fix it dropped the winner back out — a loop.
func TestBothSetupsAgainstOneProfileKeepBothCredentialSets(t *testing.T) {
	assertBoth := func(t *testing.T, p config.Profile) {
		t.Helper()
		// Gateway half.
		if p.URL != "https://eu.api.jamfcloud.com" {
			t.Errorf("gateway url lost: %q", p.URL)
		}
		if p.AuthMethod != "platform" {
			t.Errorf("auth-method = %q, want platform — ResolveAuthForProfile reads this to enter gateway auth", p.AuthMethod)
		}
		if p.EnvironmentID != "env-1" {
			t.Errorf("environment-id lost: %q", p.EnvironmentID)
		}
		if p.ClientID == "" || p.ClientSecret == "" {
			t.Errorf("gateway client credentials lost: id=%q secret=%q", p.ClientID, p.ClientSecret)
		}
		// Radar half — every field `security setup` owns.
		for name, got := range map[string]string{
			"risk-client-id":          p.RiskClientID,
			"risk-client-secret":      p.RiskClientSecret,
			"lifecycle-client-id":     p.LifecycleClientID,
			"lifecycle-client-secret": p.LifecycleClientSecret,
			"sse-client-id":           p.SSEClientID,
			"sse-client-secret":       p.SSEClientSecret,
			"sse-url":                 p.SSEURL,
		} {
			if got == "" {
				t.Errorf("%s lost", name)
			}
		}
	}

	t.Run("security setup then platform setup", func(t *testing.T) {
		p := radarHalf(mergeSecurityProfileBase(config.Profile{}))
		if p.Product != "security" || p.AuthMethod != "security" {
			t.Fatalf("security setup on an empty profile: product=%q auth-method=%q", p.Product, p.AuthMethod)
		}
		p = mergePlatformProfile(p, gatewayCreds(), "keychain:jamf-cli/p/client-id", "keychain:jamf-cli/p/client-secret")
		assertBoth(t, p)
	})

	t.Run("platform setup then security setup", func(t *testing.T) {
		p := mergePlatformProfile(config.Profile{}, gatewayCreds(), "keychain:jamf-cli/p/client-id", "keychain:jamf-cli/p/client-secret")
		p = radarHalf(mergeSecurityProfileBase(p))
		assertBoth(t, p)
		// The second setup must not demote the gateway profile: auth-method
		// "platform" is load-bearing and "security" is not read anywhere.
		if p.AuthMethod != "platform" {
			t.Errorf("security setup demoted auth-method to %q", p.AuthMethod)
		}
	})
}

// TestMergePlatformProfileReplacesTheScopeRatherThanUnioningIt: re-running
// `platform setup` and answering with a tenant where the profile held an
// environment has to leave one level set, not both — checkScopeConflict refuses
// a profile naming two, so a union would make the profile unusable.
func TestMergePlatformProfileReplacesTheScopeRatherThanUnioningIt(t *testing.T) {
	existing := config.Profile{AuthMethod: "platform", EnvironmentID: "env-old"}
	creds := &platformGatewayCredentials{GatewayURL: "https://us.api.jamfcloud.com", TenantID: "ten-new"}

	p := mergePlatformProfile(existing, creds, "id", "secret")
	if p.TenantID != "ten-new" {
		t.Errorf("tenant-id = %q, want ten-new", p.TenantID)
	}
	if p.EnvironmentID != "" {
		t.Errorf("environment-id = %q, want empty — two levels at once is refused by checkScopeConflict", p.EnvironmentID)
	}
}

// TestMergeSecurityProfileBaseIsIdempotent: re-running `security setup` against
// its own profile must not change the product or auth-method it already wrote.
func TestMergeSecurityProfileBaseIsIdempotent(t *testing.T) {
	first := mergeSecurityProfileBase(config.Profile{})
	second := mergeSecurityProfileBase(first)
	if first != second {
		t.Errorf("not idempotent: %+v then %+v", first, second)
	}
}
