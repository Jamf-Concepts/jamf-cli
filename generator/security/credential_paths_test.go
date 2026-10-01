// Copyright 2026, Jamf Software LLC

package security

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/generator/parser"
)

// TestShippedSpecsRefuseTheStreamAuthorizationHeader pins the one secret the
// Security Cloud specs carry today: the bearer value the SSE transmitter
// presents to the receiver.
func TestShippedSpecsRefuseTheStreamAuthorizationHeader(t *testing.T) {
	specsDir, err := filepath.Abs("../../specs/security")
	if err != nil {
		t.Fatalf("resolving specs dir: %v", err)
	}
	resources, _, _, err := LoadResources(specsDir)
	if err != nil {
		t.Fatalf("LoadResources: %v", err)
	}
	var refused []string
	for _, r := range resources {
		for _, op := range r.AllOperations() {
			refused = append(refused, parser.RequestCredentialPaths(nil, op)...)
		}
	}
	if !slices.Contains(refused, "delivery.authorization_header") {
		t.Errorf("--set accepts delivery.authorization_header; refused: %v", refused)
	}
	if slices.Contains(refused, "delivery.endpoint_url") {
		t.Error("delivery.endpoint_url is not a secret and must stay settable")
	}
}

// TestEmittedStreamCarriesItsCredentialPath reads what the emitter hands the
// template, so dropping its wiring line fails here.
func TestEmittedStreamCarriesItsCredentialPath(t *testing.T) {
	specsDir, err := filepath.Abs("../../specs/security")
	if err != nil {
		t.Fatalf("resolving specs dir: %v", err)
	}
	resources, scopeOf, _, err := LoadResources(specsDir)
	if err != nil {
		t.Fatalf("LoadResources: %v", err)
	}
	for _, r := range resources {
		tr, err := buildTemplateResource(r, scopeOf[r.Name])
		if err != nil {
			t.Fatalf("buildTemplateResource(%s): %v", r.Name, err)
		}
		for _, op := range tr.Operations {
			if slices.Contains(op.CredentialPaths, "delivery.authorization_header") {
				return
			}
		}
	}
	t.Error("no emitted Security Cloud operation refuses delivery.authorization_header")
}
