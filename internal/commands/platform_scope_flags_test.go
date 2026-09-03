// Copyright 2026, Jamf Software LLC

package commands

import (
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/config"
)

// setScopeFlags sets the package-level vars the root --tenant-id and
// --environment-id flags bind to, restoring them afterwards. They are globals
// because every generated command reads them through CLIContext, so a test that
// leaves one set poisons every later test in the package.
func setScopeFlags(t *testing.T, tenant, environment string) {
	t.Helper()
	prevT, prevE := tenantID, environmentID
	t.Cleanup(func() { tenantID, environmentID = prevT, prevE })
	tenantID, environmentID = tenant, environment
}

// TestResolveScopeHonoursTheFlags is the regression test for the finding that
// --tenant-id was silently discarded on the whole `security` product path.
//
// resolveScope read the env vars and the profile and never the flag vars, and
// the security path never reaches ResolveAuthForProfile — which is where those
// vars are otherwise consumed — so nothing on that path read them. --url *was*
// honoured there, so the two documented ways to override a profile's scope
// disagreed and only the failing one was silent: `-p tenant-a --tenant-id B
// security device-groups delete <id>` deleted in tenant A and exited 0.
func TestResolveScopeHonoursTheFlags(t *testing.T) {
	cases := []struct {
		name     string
		flagT    string
		flagE    string
		env      map[string]string
		profile  config.Profile
		wantKind auth.ScopeKind
		wantID   string
	}{
		{
			name:     "--tenant-id retargets a tenant profile",
			flagT:    "ten-flag",
			profile:  config.Profile{AuthMethod: "platform", TenantID: "ten-profile"},
			wantKind: auth.ScopeTenant,
			wantID:   "ten-flag",
		},
		{
			name:     "--environment-id retargets a tenant profile",
			flagE:    "env-flag",
			profile:  config.Profile{AuthMethod: "platform", TenantID: "ten-profile"},
			wantKind: auth.ScopeEnvironment,
			wantID:   "env-flag",
		},
		{
			name:     "--tenant-id beats JAMF_TENANT_ID",
			flagT:    "ten-flag",
			env:      map[string]string{"JAMF_TENANT_ID": "ten-env"},
			profile:  config.Profile{AuthMethod: "platform"},
			wantKind: auth.ScopeTenant,
			wantID:   "ten-flag",
		},
		{
			name:     "--tenant-id beats JAMF_ENVIRONMENT_ID",
			flagT:    "ten-flag",
			env:      map[string]string{"JAMF_ENVIRONMENT_ID": "env-env"},
			profile:  config.Profile{AuthMethod: "platform"},
			wantKind: auth.ScopeTenant,
			wantID:   "ten-flag",
		},
		{
			name:     "no flag falls through to the env var",
			env:      map[string]string{"JAMF_TENANT_ID": "ten-env"},
			profile:  config.Profile{AuthMethod: "platform", TenantID: "ten-profile"},
			wantKind: auth.ScopeTenant,
			wantID:   "ten-env",
		},
		{
			name:     "no flag and no env falls through to the profile",
			profile:  config.Profile{AuthMethod: "platform", EnvironmentID: "env-profile"},
			wantKind: auth.ScopeEnvironment,
			wantID:   "env-profile",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("JAMF_ENVIRONMENT_ID", tc.env["JAMF_ENVIRONMENT_ID"])
			t.Setenv("JAMF_TENANT_ID", tc.env["JAMF_TENANT_ID"])
			setScopeFlags(t, tc.flagT, tc.flagE)

			cfg := &config.Config{
				DefaultProfile: "p",
				Profiles:       map[string]config.Profile{"p": tc.profile},
			}
			got := resolveScope(cfg, "p")
			if got.Kind != tc.wantKind || got.ID != tc.wantID {
				t.Fatalf("resolveScope = {%v %q}, want {%v %q}", got.Kind, got.ID, tc.wantKind, tc.wantID)
			}
			// The header is the only signal the gateway reads, so assert it
			// rather than only the resolved struct.
			_, value := got.Header()
			if value != tc.wantID {
				t.Errorf("scope header value = %q, want %q", value, tc.wantID)
			}
		})
	}
}

// TestCheckScopeConflictRefusesBothFlags: the refusal has to cover the flags for
// the same reason it covers the env vars. Without it `--tenant-id X
// --environment-id Y` was accepted on a `security` command, and one of the two
// is guaranteed wrong for the credential.
func TestCheckScopeConflictRefusesBothFlags(t *testing.T) {
	t.Setenv("JAMF_ENVIRONMENT_ID", "")
	t.Setenv("JAMF_TENANT_ID", "")
	cfg := &config.Config{
		DefaultProfile: "p",
		Profiles:       map[string]config.Profile{"p": {AuthMethod: "platform"}},
	}

	setScopeFlags(t, "ten-flag", "env-flag")
	err := checkScopeConflict(cfg, "p")
	if err == nil {
		t.Fatal("--tenant-id and --environment-id together must be refused")
	}
	for _, want := range []string{"ten-flag", "env-flag", "one level"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}

	// One flag alone settles the level and must not be refused, even against a
	// profile naming the other one — that is the override, not a conflict.
	for _, tc := range []struct{ flagT, flagE string }{{"ten-flag", ""}, {"", "env-flag"}} {
		setScopeFlags(t, tc.flagT, tc.flagE)
		both := &config.Config{
			DefaultProfile: "p",
			Profiles: map[string]config.Profile{
				"p": {AuthMethod: "platform", EnvironmentID: "env-p", TenantID: "ten-p"},
			},
		}
		if err := checkScopeConflict(both, "p"); err != nil {
			t.Errorf("flag {%q %q} against a two-level profile: unexpected refusal: %v", tc.flagT, tc.flagE, err)
		}
	}
}
