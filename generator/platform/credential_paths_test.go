// Copyright 2026, Jamf Software LLC

package platform

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/generator/parser"
)

// TestShippedSpecsRefuseTheKnownCredentialFields pins, by name, the secrets
// the Platform specs carry today, including the ones only a non-scaffolded
// union variant declares and the one behind an allOf-wrapped property. A
// regression names the field rather than a count.
func TestShippedSpecsRefuseTheKnownCredentialFields(t *testing.T) {
	specsDir, err := filepath.Abs("../../specs/platform")
	if err != nil {
		t.Fatalf("resolving specs dir: %v", err)
	}
	resources, _, err := LoadResources(specsDir)
	if err != nil {
		t.Fatalf("LoadResources: %v", err)
	}
	refused := map[string][]string{}
	for _, r := range resources {
		for _, op := range r.AllOperations() {
			refused[r.Name] = append(refused[r.Name], parser.RequestCredentialPaths(nil, op)...)
		}
	}
	for resource, fields := range map[string][]string{
		"uem-connectors": {
			"deviceSyncAuth.clientSecret", "deviceSyncAuth.password", "apiKey", "emmPassword",
			"apiSettings.clientSecret", "lcm.clientSecret", "mtd.clientSecret", "tag.clientSecret",
			"citrixCloudConfig.applicationSecret", "citrixCloudOauthConfig.applicationSecret",
			"tenantCode",
		},
		"device-actions": {"pin"},
		"blueprints": {
			"steps[].components[].configuration.Beta.Value.RequireProgram.Token",
			"steps[].components[].configuration.Beta.Value.OfferPrograms[].Token",
		},
		"ztna-gateways":             {"ipsec.left.secret"},
		"sso-connections":           {"connection.clientSecret"},
		"distributor-configuration": {"webhook.secretValue"},
	} {
		for _, f := range fields {
			if !slices.Contains(refused[resource], f) {
				t.Errorf("%s: --set accepts the credential %q; refused: %v", resource, f, refused[resource])
			}
		}
	}
	for resource, fields := range map[string][]string{
		"uem-connectors":  {"deviceSyncAuth.clientId", "deviceSyncAuth.username", "tenantId"},
		"sso-connections": {"connection.tokenEndpoint", "connection.authorizationEndpoint"},
	} {
		for _, f := range fields {
			if slices.Contains(refused[resource], f) {
				t.Errorf("%s: %q is not a secret and must stay settable", resource, f)
			}
		}
	}
}

// TestEmittedCommandsCarryTheirCredentialPaths reads what the emitter hands
// the template, not what the parser derives, so dropping either wiring line
// (an operation's paths or apply's union of create and update) fails here.
func TestEmittedCommandsCarryTheirCredentialPaths(t *testing.T) {
	specsDir, err := filepath.Abs("../../specs/platform")
	if err != nil {
		t.Fatalf("resolving specs dir: %v", err)
	}
	resources, _, err := LoadResources(specsDir)
	if err != nil {
		t.Fatalf("LoadResources: %v", err)
	}
	const secret = "ipsec.left.secret"
	found := false
	for _, r := range resources {
		if r.Name != "ztna-gateways" {
			continue
		}
		found = true
		tr, err := buildTemplateResource(r)
		if err != nil {
			t.Fatalf("buildTemplateResource(%s): %v", r.Name, err)
		}
		var ops []string
		for _, op := range tr.Operations {
			if slices.Contains(op.CredentialPaths, secret) {
				ops = append(ops, op.Method+" "+op.Path)
			}
		}
		if len(ops) == 0 {
			t.Errorf("no emitted ztna-gateways operation refuses %s", secret)
		}
		if tr.Apply == nil {
			t.Fatal("ztna-gateways emits no apply")
		}
		if !slices.Contains(tr.Apply.CredentialPaths, secret) {
			t.Errorf("ztna-gateways apply refuses %v, want %s", tr.Apply.CredentialPaths, secret)
		}
	}
	if !found {
		t.Fatal("no ztna-gateways resource in the shipped specs")
	}
}
