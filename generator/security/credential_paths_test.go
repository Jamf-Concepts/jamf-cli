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
