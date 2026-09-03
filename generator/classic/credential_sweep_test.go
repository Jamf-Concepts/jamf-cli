// Copyright 2026, Jamf Software LLC

package classic

import (
	"slices"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/generator/parser"
)

// credentialMarkers are the substrings that make a field name or path *look*
// like it carries a secret. Deliberately much broader than
// credentialFieldNames: this is the sweep's suspicion list, not the generator's
// refusal list, and its job is to fail when a spec refresh introduces a
// secret-bearing field the refusal list does not name.
var credentialMarkers = []string{
	"password",
	"passwd",
	"passphrase",
	"secret",
	"token",
	"credential",
	"key",
}

// notCredentials are the string fields that match a marker and are not secrets,
// each with the reason it is exempt. An entry is a claim about one field, so a
// new one has to be argued for rather than added to silence the sweep — and a
// stale entry fails the test below rather than sitting unnoticed.
//
// Every one of these matches through a *type discriminator* or through the name
// of the section it sits in, never through its own value. There is deliberately
// no entry for a field that holds a secret and is merely inconvenient to refuse.
var notCredentials = map[string]string{
	// Individual | Institutional — which recovery key a configuration uses.
	"diskencryptionconfigurations:key_type":                                    "enum naming which kind of recovery key, not a key",
	"policies:disk_encryption.remediate_key_type":                              "enum naming which kind of recovery key, not a key",
	"diskencryptionconfigurations:institutional_recovery_key.certificate_type": "enum naming the keystore's file type, not its contents",
	// Matches only through its parent section's name.
	"policies:account_maintenance.open_firmware_efi_password.of_mode": "command | none, the mode the EFI password is applied in",
}

// TestEverySecretBearingFieldIsRefusedForSet is the sweep, and it is the guard
// that cannot go stale: it walks every schema in the committed artifact rather
// than a list of resources someone remembered to add.
//
// It exists because three string-typed secrets shipped settable —
// json_web_token_configuration.encryption_key (the JWT signing key) and a disk
// encryption configuration's institutional_recovery_key.key and .data (the
// base64 .p12 that decrypts every institutionally-encrypted FileVault volume in
// the fleet, and its key material). All three matched none of
// credentialFieldNames, so --set accepted them and shell completion offered
// them, putting the value in shell history, in ps output and in the CI job log —
// exactly what the repo's credential policy exists to prevent.
//
// Only string-typed fields are considered, which is the same restriction
// isCredentialField applies and for the same reason: a distribution point
// declares username_password_required, a boolean switch whose name contains
// "password" and whose value is not one. Demanding that be refused would block a
// legitimate setting.
func TestEverySecretBearingFieldIsRefusedForSet(t *testing.T) {
	seen := map[string]bool{}
	var suspected int

	for _, r := range liveResources(t) {
		if !r.HasBodySchema() {
			continue
		}
		refused := r.CredentialFields()

		walkSchema(r.BodySchema, "", func(path string, s *parser.Schema) {
			for name, prop := range s.Properties {
				if prop == nil || prop.Type != "string" {
					continue
				}
				full := joinPath(path, name)
				if !namesASecret(full) {
					continue
				}
				suspected++
				key := r.Name + ":" + full
				seen[key] = true
				// continue, not return: a return here would abandon the rest of
				// this object's properties, and one exempt sibling would then
				// hide every unrefused secret beside it — which is exactly what
				// institutional_recovery_key.certificate_type did to .key and
				// .data on the first run of this test.
				if _, exempt := notCredentials[key]; exempt {
					if slices.Contains(refused, full) {
						t.Errorf("%s: %q is listed in notCredentials but the generator refuses it; drop the exemption", r.Name, full)
					}
					continue
				}
				if !slices.Contains(refused, full) {
					t.Errorf("%s: %q looks like a credential and --set accepts it. Add its name to credentialFieldNames (or its path to credentialFieldPaths), or exempt it in notCredentials with a reason.", r.Name, full)
				}
			}
		})
	}

	// A refactor that stopped the walk reaching anything would otherwise pass
	// vacuously. 36 fields match a marker in the 11.28.0 artifact; the floor is
	// well under that so a spec that drops a resource does not fail the build.
	if suspected < 25 {
		t.Errorf("the sweep only examined %d fields, which is too few to be walking the shipped artifact", suspected)
	}

	for key, why := range notCredentials {
		if !seen[key] {
			t.Errorf("notCredentials names %q (%s), which no longer matches any string field in the artifact; remove the entry", key, why)
		}
	}
}

// namesASecret reports whether a dotted path looks like it carries a secret.
//
// The whole path is tested, not just the leaf, because the two worst cases here
// are named `key` and `data` — indistinguishable from a type discriminator and
// from a base64 icon blob on their own, and identifiable only as
// `institutional_recovery_key.key` and `institutional_recovery_key.data`.
func namesASecret(path string) bool {
	lower := strings.ToLower(path)
	for _, m := range credentialMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// TestCredentialFields_RefusesTheJWTSigningKeyAndInstitutionalKeystore pins the
// three fields the sweep above was written for, by name, so a regression names
// the field rather than a count.
func TestCredentialFields_RefusesTheJWTSigningKeyAndInstitutionalKeystore(t *testing.T) {
	res := liveResources(t)
	for _, tc := range []struct{ resource, field, what string }{
		{"jsonwebtokenconfigurations", "encryption_key", "the JWT signing key"},
		{"diskencryptionconfigurations", "institutional_recovery_key.key", "the institutional FileVault keystore"},
		{"diskencryptionconfigurations", "institutional_recovery_key.data", "the institutional FileVault key material"},
	} {
		r := find(t, res, tc.resource)
		if got := r.CredentialFields(); !slices.Contains(got, tc.field) {
			t.Errorf("%s: %q is %s and --set accepts it; have %v", tc.resource, tc.field, tc.what, got)
		}
		for _, c := range r.SetCompletions() {
			if strings.TrimSuffix(c, "=") == tc.field {
				t.Errorf("%s: completion offers %q (%s)", tc.resource, c, tc.what)
			}
		}
	}
}

// TestCredentialFields_KeepsTheBase64BlobsSettable is the over-match guard on
// the path matching above. `data` is also the leaf name of the base64 icon,
// .ipa and .mobileconfig blobs on six resources, and `key_type` names which
// kind of recovery key a configuration uses. None is a secret, and refusing any
// of them would send a caller to --from-file for no reason.
func TestCredentialFields_KeepsTheBase64BlobsSettable(t *testing.T) {
	res := liveResources(t)
	for _, tc := range []struct{ resource, field string }{
		{"mobiledeviceapplications", "general.icon.data"},
		{"mobiledeviceapplications", "general.ipa.data"},
		{"mobiledeviceprovisioningprofiles", "general.profile.data"},
		{"diskencryptionconfigurations", "key_type"},
	} {
		r := find(t, res, tc.resource)
		if slices.Contains(r.CredentialFields(), tc.field) {
			t.Errorf("%s: %q is not a credential and must stay settable", tc.resource, tc.field)
		}
		if _, ok := r.SetFieldTypes()[tc.field]; !ok {
			t.Errorf("%s: %q is not settable at all; the fixture has gone stale", tc.resource, tc.field)
		}
	}
}
