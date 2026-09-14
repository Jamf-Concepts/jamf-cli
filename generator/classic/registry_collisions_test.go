// Copyright 2026, Jamf Software LLC

package classic

import (
	"strings"
	"testing"
)

// The classic resources come from a hand-edited manifest, so a repeated
// cli_name is an edit rather than a derivation — and both of its symptoms are
// silent, the same two the modern guard exists for (#362).
func TestCheckRegistryCollisions_RefusesARepeatedCLIName(t *testing.T) {
	resources := []ClassicResource{
		{CLIName: "classic-policies", GoName: "ClassicPolicies"},
		{CLIName: "classic-policies", GoName: "ClassicPoliciesAgain"},
	}

	err := CheckRegistryCollisions(resources)
	if err == nil {
		t.Fatal("a repeated cli_name was accepted; the registry would register it twice")
	}
	if !strings.Contains(err.Error(), "classic-policies") {
		t.Errorf("the refusal does not name the collision: %v", err)
	}
}

func TestCheckRegistryCollisions_RefusesARepeatedGoName(t *testing.T) {
	resources := []ClassicResource{
		{CLIName: "classic-policies", GoName: "ClassicPolicies"},
		{CLIName: "classic-policy", GoName: "ClassicPolicies"},
	}

	if err := CheckRegistryCollisions(resources); err == nil {
		t.Fatal("a repeated Go name was accepted; the package cannot declare the constructor twice")
	}
}

func TestCheckRegistryCollisions_AcceptsDistinctResources(t *testing.T) {
	resources := []ClassicResource{
		{CLIName: "classic-policies", GoName: "ClassicPolicies"},
		{CLIName: "classic-computer-groups", GoName: "ClassicComputerGroups"},
	}

	if err := CheckRegistryCollisions(resources); err != nil {
		t.Fatalf("a distinct resource set was refused: %v", err)
	}
}
