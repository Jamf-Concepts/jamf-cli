// Copyright 2026, Jamf Software LLC

package parser

import (
	"sort"
	"strings"
)

// credentialNameMarkers are substrings of a normalized field name (lowercase,
// with "_" and "-" removed) that mark a string field as carrying a secret.
// Normalizing first is what lets one list serve Classic's snake_case and the
// modern specs' camelCase: `shared_secret` and `clientSecret` both contain
// "secret", `private_key` and `privateKeyJwt` both contain "privatekey".
var credentialNameMarkers = []string{
	"password",
	"passphrase",
	"secret",
	"privatekey",
	"apikey",
	"accesskey",
	"authorizationheader",
	// json_web_token_configuration.encryption_key is the JWT signing key:
	// whoever holds it can mint a token Jamf Pro will trust. Named in full
	// rather than as a bare "key", which would match key_type,
	// remediate_key_type and certificate_type, none of which is a secret.
	"encryptionkey",
	// A keystore is a base64 PKCS#12 carrying its private key, so the blob is
	// as secret as its password. keystoreFileName and keystoreSetupType are
	// kept settable by notCredentialSuffixes.
	"keystore",
}

// credentialNameSuffixes mark a field whose normalized name ends with them. A
// token is matched on the suffix alone, because "token" also opens the names of
// fields that only point at one: tokenUrl, tokenEndpoint, refreshTokenId.
//
// "pin" is the Find My or device-lock PIN that unlocks an erased or locked
// device, and is matched the same way so "mapping" cannot reach it.
var credentialNameSuffixes = []string{
	"token",
	"pin",
}

// notCredentialSuffixes name a field that refers to a credential without
// holding one: its identifier, where to fetch it, or what kind it is. Checked
// before the markers and the writeOnly flag, so `clientSecretId`, `apiKeyType`
// and a write-only `tenantId` stay settable.
var notCredentialSuffixes = []string{
	"id",
	"ids",
	"url",
	"uri",
	"endpoint",
	"type",
	"name",
	"label",
	"format",
	"expiration",
	"expirationdate",
}

// credentialPathSuffixes are matched against the whole dotted path, for fields
// whose leaf name alone is too generic to match safely.
//
// A Classic disk encryption configuration's institutional keystore is the case
// that needs it: `.key` and `.data` together are the base64 `.p12` and its key
// material, the private key that decrypts every institutionally-encrypted
// FileVault volume in the fleet, while the leaf names `key` and `data` are also
// worn by `key_type` and by base64 icon, `.ipa` and `.mobileconfig` blobs that
// are not credentials and must stay settable. Matched on a path suffix, so the
// same object is still refused if a future schema nests it.
//
// A Pro ADCS or DigiCert `clientCert.data` and a cloud LDAP
// `keystore.fileBytes` are a base64 `.p12` or `.pfx` carrying the client's
// private key. `serverCert.data` shares the ADCS schema and holds only a public
// certificate, so it stays settable.
var credentialPathSuffixes = []string{
	"institutional_recovery_key.key",
	"institutional_recovery_key.data",
	"clientcert.data",
	"keystore.filebytes",
}

// notCredentialFields are fields whose name marks a secret and whose content is
// not one, keyed "<schema name>.<field>", each with the reason. Name matching
// cannot tell these apart, so each is a claim about one field.
var notCredentialFields = map[string]string{
	"EnrollmentProcessTextObject.password": "the localized text of the enrollment page's password label, beside username and loginButton",
}

// IsCredentialField reports whether a request-body field carries a secret, and
// so must never be accepted as a `--set` value, where it would land in shell
// history, ps output and CI logs.
//
// kind is the field's JSON type. Only a string can hold a secret here: a
// distribution point declares `username_password_required`, a boolean whose
// name contains "password", and `secretExists` or `passcodePresent` report on a
// secret without carrying one. writeOnly is the spec's own marker for a value
// the server accepts and never returns, which is how an OpenAPI document spells
// "password" for a field whose name does not.
func IsCredentialField(path, name, kind string, writeOnly bool) bool {
	if kind != "string" {
		return false
	}
	n := normalizeFieldName(name)
	for _, s := range notCredentialSuffixes {
		if strings.HasSuffix(n, s) {
			return false
		}
	}
	if writeOnly {
		return true
	}
	for _, m := range credentialNameMarkers {
		if strings.Contains(n, m) {
			return true
		}
	}
	for _, s := range credentialNameSuffixes {
		if strings.HasSuffix(n, s) {
			return true
		}
	}
	lowerPath := strings.ToLower(path)
	for _, p := range credentialPathSuffixes {
		if lowerPath == p || strings.HasSuffix(lowerPath, "."+p) {
			return true
		}
	}
	return false
}

func normalizeFieldName(name string) string {
	return strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(name))
}

// CredentialPaths returns the dotted path of every credential field in a
// request-body schema, sorted. An array element is reached through a "[]"
// segment (`items[].password`), which a `--set` key cannot spell but a JSON
// object or array value passed to `--set` can carry.
//
// Every variant of a discriminated union is walked, not only the first one the
// scaffold renders: uem-connectors create keeps its Citrix and MTD secrets in
// the other variants. Read-only properties are skipped, since a body never
// carries them.
func CredentialPaths(s *Schema, schemas map[string]*Schema) []string {
	seen := map[string]bool{}
	collectCredentialPaths(s, schemas, "", 0, map[string]bool{}, seen)
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func collectCredentialPaths(s *Schema, schemas map[string]*Schema, prefix string, depth int, visited map[string]bool, out map[string]bool) {
	if s == nil || depth > maxSchemaDepth {
		return
	}
	if s.Items != nil {
		collectCredentialPaths(s.Items, schemas, joinFieldPath(prefix, "[]"), depth+1, visited, out)
	}
	for _, v := range s.VariantSchemas {
		collectCredentialPaths(v, schemas, prefix, depth+1, visited, out)
	}
	for name, prop := range s.Properties {
		if prop == nil || prop.ReadOnly {
			continue
		}
		path := joinFieldPath(prefix, name)
		if _, exempt := notCredentialFields[s.Name+"."+name]; exempt {
			continue
		}
		kind := prop.Type
		if prop.ByteArray {
			kind = "string"
		}
		if IsCredentialField(path, name, kind, prop.WriteOnly) {
			out[path] = true
			continue
		}
		nested := prop.Nested
		if nested == nil && prop.SchemaRef != "" {
			nested = schemas[prop.SchemaRef]
		}
		if nested != nil {
			if key := prop.SchemaRef; key != "" {
				if visited[key] {
					continue
				}
				visited[key] = true
				collectCredentialPaths(nested, schemas, path, depth+1, visited, out)
				delete(visited, key)
			} else {
				collectCredentialPaths(nested, schemas, path, depth+1, visited, out)
			}
		}
		if prop.Items != nil {
			collectCredentialPaths(prop.Items, schemas, path+"[]", depth+1, visited, out)
		}
	}
}

func joinFieldPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	if name == "[]" {
		return prefix + name
	}
	return prefix + "." + name
}

// RequestCredentialPaths is the union of CredentialPaths across the request
// bodies of ops, sorted; nil when none carries a credential. An apply command
// composes a create and an update, and must refuse what either one would.
func RequestCredentialPaths(schemas map[string]*Schema, ops ...*Operation) []string {
	seen := map[string]bool{}
	for _, op := range ops {
		if op == nil || op.RequestBody == nil || op.RequestBody.Schema == nil {
			continue
		}
		for _, p := range CredentialPaths(op.RequestBody.Schema, schemas) {
			seen[p] = true
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
