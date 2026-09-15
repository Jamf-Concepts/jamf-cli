// Copyright 2026, Jamf Software LLC

package parser

import (
	"strings"
	"testing"
)

// The registry registered `NewComputerGroupsCmd` on two consecutive lines for
// every release that carried the resource (#362), and nothing failed: the
// duplicate compiles, `make verify-generated` reproduces it faithfully, and the
// only symptom is a resource listed twice in `pro --help` whose second subtree
// cobra can never reach. So these assert the refusal rather than the absence of
// a duplicate in today's tree — the tree being clean is what makes the guard
// otherwise untested.
func TestCheckRegistryCollisions_RefusesARepeatedConstructor(t *testing.T) {
	resources := []*Resource{
		{Name: "computer-groups", GoName: "ComputerGroups"},
		{Name: "computer-groups", GoName: "ComputerGroups"},
	}

	err := CheckRegistryCollisions(resources)
	if err == nil {
		t.Fatal("two resources deriving one Go name were accepted; the registry would register NewComputerGroupsCmd twice")
	}
	if !strings.Contains(err.Error(), "ComputerGroups") {
		t.Errorf("the refusal does not name the colliding identifier, which is the whole diagnostic: %v", err)
	}
}

// A shared file name is the likelier cause of the duplicate registration and is
// worse on its own: Generate writes one file per resource, so the second write
// overwrites the first and a whole resource's operations leave the binary with
// nothing reporting it.
func TestCheckRegistryCollisions_RefusesASharedFileName(t *testing.T) {
	// Distinct Go names, identical FileBase: a nested `a b` and a top-level
	// `a-b` both stem to "a-b". The FileBase check has to be separate from the
	// Go-name one to see this.
	resources := []*Resource{
		{Name: "a-b", GoName: "AB"},
		{Name: "b", Parent: "a", GoName: "ANestedB"},
	}

	err := CheckRegistryCollisions(resources)
	if err == nil {
		t.Fatal("two resources deriving one file name were accepted; the second Generate would overwrite the first")
	}
	if !strings.Contains(err.Error(), "a-b") {
		t.Errorf("the refusal does not name the colliding file: %v", err)
	}
}

// A sub-resource is checked too: two resources in one package cannot both
// declare New<GoName>Cmd, and a nested one colliding with a top-level one is the
// same defect reported by the compiler instead of by cobra.
func TestCheckRegistryCollisions_ReachesSubResources(t *testing.T) {
	resources := []*Resource{
		{Name: "settings", GoName: "SsoSettingsSettings"},
		{
			Name: "sso-settings", GoName: "SsoSettings",
			SubResources: []*Resource{
				{Name: "settings", Parent: "sso-settings", GoName: "SsoSettingsSettings"},
			},
		},
	}

	if err := CheckRegistryCollisions(resources); err == nil {
		t.Fatal("a sub-resource colliding with a top-level resource was accepted")
	}
}

// The live resource set has to pass, or the guard is refusing the shipped tree.
func TestCheckRegistryCollisions_AcceptsDistinctResources(t *testing.T) {
	resources := []*Resource{
		{Name: "computer-groups", GoName: "ComputerGroups"},
		{Name: "computer-groups-smart-groups", GoName: "ComputerGroupsSmartGroups"},
		{
			Name: "sso-settings", GoName: "SsoSettings",
			SubResources: []*Resource{
				{Name: "cert", Parent: "sso-settings", GoName: "SsoSettingsCert"},
			},
		},
	}

	if err := CheckRegistryCollisions(resources); err != nil {
		t.Fatalf("a distinct resource set was refused: %v", err)
	}
}
