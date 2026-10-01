// Copyright 2026, Jamf Software LLC

package parser

import (
	"slices"
	"strings"
	"testing"
)

func TestIsCredentialField(t *testing.T) {
	refused := []struct{ path, kind string }{
		{"password", "string"},
		{"deviceSyncAuth.clientSecret", "string"},
		{"delivery.authorization_header", "string"},
		{"apiKey", "string"},
		{"appAccessKey", "string"},
		{"refreshToken", "string"},
		{"serviceToken", "string"},
		{"token", "string"},
		{"privateKeyJwt", "string"},
		{"gsxKeystore.keystoreBytes", "string"},
		{"keystoreFile", "string"},
		{"recoveryLockPassword", "string"},
		{"shared_secret", "string"},
		{"json_web_token_configuration.encryption_key", "string"},
		{"institutional_recovery_key.key", "string"},
		{"pin", "string"},
	}
	for _, tc := range refused {
		name := tc.path[strings.LastIndex(tc.path, ".")+1:]
		if !IsCredentialField(tc.path, name, tc.kind, false) {
			t.Errorf("%s (%s) carries a secret and is not refused", tc.path, tc.kind)
		}
	}

	kept := []struct{ path, kind string }{
		{"tokenUrl", "string"},
		{"tokenEndpoint", "string"},
		{"connection.authorizationEndpoint", "string"},
		{"refreshTokenId", "string"},
		{"accessTokenId", "string"},
		{"tokenFileName", "string"},
		{"keystoreFileName", "string"},
		{"recoveryLockPasswordType", "string"},
		{"passwordLabel", "string"},
		{"key_type", "string"},
		{"general.icon.data", "string"},
		{"mapping", "string"},
		{"secretExists", "boolean"},
		{"passcodePresent", "boolean"},
		{"username_password_required", "boolean"},
		{"accessTokenLifetimeSeconds", "integer"},
		{"deviceSyncAuth", "object"},
	}
	for _, tc := range kept {
		name := tc.path[strings.LastIndex(tc.path, ".")+1:]
		if IsCredentialField(tc.path, name, tc.kind, false) {
			t.Errorf("%s (%s) is not a secret and must stay settable", tc.path, tc.kind)
		}
	}

	if !IsCredentialField("tenantCode", "tenantCode", "string", true) {
		t.Error("a write-only string is a credential whatever its name")
	}
	if IsCredentialField("tenantId", "tenantId", "string", true) {
		t.Error("a write-only identifier names a tenant and holds no secret")
	}
}

func TestCredentialPaths_WalksVariantsArraysAndRefs(t *testing.T) {
	str := func(name string) *Property { return &Property{Name: name, Type: "string"} }
	schemas := map[string]*Schema{
		"Creds": {Name: "Creds", Properties: map[string]*Property{"password": str("password"), "username": str("username")}},
	}
	body := &Schema{
		Properties: map[string]*Property{
			"name":    str("name"),
			"auth":    {Name: "auth", Type: "object", SchemaRef: "Creds"},
			"users":   {Name: "users", Type: "array", Items: &Schema{Properties: map[string]*Property{"secret": str("secret")}}},
			"hidden":  {Name: "hidden", Type: "string", ReadOnly: true},
			"apiKeyX": {Name: "apiKeyX", Type: "boolean"},
		},
		VariantSchemas: []*Schema{{Properties: map[string]*Property{"apiKey": str("apiKey")}}},
	}
	got := CredentialPaths(body, schemas)
	want := []string{"apiKey", "auth.password", "users[].secret"}
	if !slices.Equal(got, want) {
		t.Errorf("CredentialPaths = %v, want %v", got, want)
	}
}

func TestCredentialPaths_HonoursNotCredentialFields(t *testing.T) {
	body := &Schema{Name: "EnrollmentProcessTextObject", Properties: map[string]*Property{
		"password": {Name: "password", Type: "string"},
	}}
	if got := CredentialPaths(body, nil); len(got) != 0 {
		t.Errorf("CredentialPaths = %v; the enrollment page's password label is exempt", got)
	}
}

// TestShippedProSpecsRefuseEveryWriteOnlyString is the live-spec guard: the
// spec's own writeOnly marker is how it says "secret", so every write-only
// string a Pro request body carries must be refused by --set, and every
// notCredentialFields exemption must still name a field.
func TestShippedProSpecsRefuseEveryWriteOnlyString(t *testing.T) {
	var resources []*Resource
	var add func(r *Resource)
	add = func(r *Resource) {
		resources = append(resources, r)
		for _, sub := range r.SubResources {
			add(sub)
		}
	}
	for _, r := range loadShippedResources(t) {
		add(r)
	}

	writeOnly := 0
	exemptSeen := map[string]bool{}
	for _, r := range resources {
		for _, op := range r.Operations {
			if op.RequestBody == nil || op.RequestBody.Schema == nil {
				continue
			}
			refused := CredentialPaths(op.RequestBody.Schema, r.Schemas)
			walkForTest(op.RequestBody.Schema, r.Schemas, "", 0, func(s *Schema, path, name string, p *Property) {
				if _, ok := notCredentialFields[s.Name+"."+name]; ok {
					exemptSeen[s.Name+"."+name] = true
				}
				if !p.WriteOnly || p.ReadOnly || p.Type != "string" {
					return
				}
				writeOnly++
				if !slices.Contains(refused, path) {
					t.Errorf("%s %s %s: write-only %q is not refused by --set", r.CmdPath(), op.Method, op.Path, path)
				}
			})
		}
	}
	if writeOnly < 20 {
		t.Errorf("only %d write-only string fields found; the walk is not reaching the shipped specs", writeOnly)
	}
	for key, why := range notCredentialFields {
		if !exemptSeen[key] {
			t.Errorf("notCredentialFields names %q (%s), which no longer matches a request field; remove it", key, why)
		}
	}
}

func walkForTest(s *Schema, schemas map[string]*Schema, prefix string, depth int, fn func(*Schema, string, string, *Property)) {
	if s == nil || depth > maxSchemaDepth {
		return
	}
	for _, v := range s.VariantSchemas {
		walkForTest(v, schemas, prefix, depth+1, fn)
	}
	if s.Items != nil {
		walkForTest(s.Items, schemas, joinFieldPath(prefix, "[]"), depth+1, fn)
	}
	for name, p := range s.Properties {
		if p == nil {
			continue
		}
		path := joinFieldPath(prefix, name)
		fn(s, path, name, p)
		nested := p.Nested
		if nested == nil && p.SchemaRef != "" && depth < 6 {
			nested = schemas[p.SchemaRef]
		}
		walkForTest(nested, schemas, path, depth+1, fn)
		if p.Items != nil {
			walkForTest(p.Items, schemas, path+"[]", depth+1, fn)
		}
	}
}

// TestShippedProSpecsRefusePrivateKeyBlobs pins the PKCS#12 fields matched on
// their path: a byte array or string whose leaf name says nothing, beside a
// serverCert.data of the same schema that holds only a public certificate.
func TestShippedProSpecsRefusePrivateKeyBlobs(t *testing.T) {
	refused := map[string][]string{}
	var add func(r *Resource)
	add = func(r *Resource) {
		for _, op := range r.Operations {
			if op.RequestBody != nil && op.RequestBody.Schema != nil {
				refused[r.Name] = append(refused[r.Name], CredentialPaths(op.RequestBody.Schema, r.Schemas)...)
			}
		}
		for _, sub := range r.SubResources {
			add(sub)
		}
	}
	for _, r := range loadShippedResources(t) {
		add(r)
	}
	for resource, path := range map[string]string{
		"adcs-settings": "clientCert.data",
		"digicert":      "clientCert.data",
		"cloud-ldap":    "server.keystore.fileBytes",
	} {
		if !slices.Contains(refused[resource], path) {
			t.Errorf("%s: --set accepts the private key blob %q; refused: %v", resource, path, refused[resource])
		}
	}
	if slices.Contains(refused["adcs-settings"], "serverCert.data") {
		t.Error("adcs-settings: serverCert.data is a public certificate and must stay settable")
	}
}
