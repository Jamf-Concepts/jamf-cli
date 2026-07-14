// Copyright 2026, Jamf Software LLC

package classic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerate_ProducesFile(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := ClassicResource{
		Name:        "policies",
		Path:        "policies",
		CLIName:     "classic-policies",
		GoName:      "ClassicPolicies",
		Singular:    "policy",
		Description: "Deployment policies",
		Operations:  []string{"list", "get", "create", "update", "delete"},
		Lookups:     []string{"id", "name"},
		IDPath:      "id",
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if _, err := os.Stat(outPath); os.IsNotExist(err) {
		t.Fatalf("expected file %s to exist", outPath)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
		return
	}

	code := string(content)

	// Verify generated code contains expected elements
	checks := []string{
		"package generated",
		CodegenHeader,
		"NewClassicPoliciesCmd",
		`Use:   "classic-policies"`,
		"newClassicPoliciesListCmd",
		"newClassicPoliciesGetCmd",
		"newClassicPoliciesCreateCmd",
		"newClassicPoliciesUpdateCmd",
		"newClassicPoliciesDeleteCmd",
		"newClassicPoliciesApplyCmd",
		"/JSSResource/policies",
		`wrapper["policy"]`,
	}

	for _, check := range checks {
		if !strings.Contains(code, check) {
			t.Errorf("generated code missing %q", check)
		}
	}
}

func TestGenerate_ListOnly(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := ClassicResource{
		Name:        "patchavailabletitles",
		Path:        "patchavailabletitles",
		CLIName:     "classic-patch-available-titles",
		GoName:      "ClassicPatchAvailableTitles",
		Singular:    "patch_available_title",
		Description: "Available patch titles",
		Operations:  []string{"list", "get"},
		Lookups:     []string{"id"},
		IDPath:      "id",
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
		return
	}

	code := string(content)

	// Should have list and get but not create/update/delete
	if !strings.Contains(code, "ListCmd") {
		t.Error("expected list command")
	}
	if !strings.Contains(code, "GetCmd") {
		t.Error("expected get command")
	}
	if strings.Contains(code, "CreateCmd") {
		t.Error("unexpected create command for list-only resource")
	}
	if strings.Contains(code, "UpdateCmd") {
		t.Error("unexpected update command for list-only resource")
	}
	if strings.Contains(code, "DeleteCmd") {
		t.Error("unexpected delete command for list-only resource")
	}
}

func TestGenerate_ExtraLookups(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := ClassicResource{
		Name:        "policies",
		Path:        "policies",
		CLIName:     "classic-policies",
		GoName:      "ClassicPolicies",
		Singular:    "policy",
		Description: "Deployment policies",
		Operations:  []string{"list", "get"},
		Lookups:     []string{"id", "name"},
		IDPath:      "id",
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
		return
	}

	code := string(content)

	// name lookup should be folded into the get command as --name flag, not a separate subcommand
	if !strings.Contains(code, `StringVar(&flagName, "name"`) {
		t.Error("expected --name flag on get command for resource with name lookup")
	}
	if strings.Contains(code, "GetByNameCmd") {
		t.Error("should not generate a separate GetByNameCmd")
	}
}

func TestGenerateRegistry(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resources := []ClassicResource{
		{CLIName: "classic-policies", GoName: "ClassicPolicies"},
		{CLIName: "classic-packages", GoName: "ClassicPackages"},
	}

	outPath, err := gen.GenerateRegistry(resources)
	if err != nil {
		t.Fatalf("GenerateRegistry() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
		return
	}

	code := string(content)

	if !strings.Contains(code, "RegisterClassicCommands") {
		t.Error("expected RegisterClassicCommands function")
	}
	if !strings.Contains(code, "NewClassicPackagesCmd") {
		t.Error("expected NewClassicPackagesCmd registration")
	}
	if !strings.Contains(code, "NewClassicPoliciesCmd") {
		t.Error("expected NewClassicPoliciesCmd registration")
	}

	// Verify sorted order: packages before policies
	pkgIdx := strings.Index(code, "NewClassicPackagesCmd")
	polIdx := strings.Index(code, "NewClassicPoliciesCmd")
	if pkgIdx > polIdx {
		t.Error("expected classic-packages before classic-policies (sorted by CLIName)")
	}
}

func TestGenerate_ClassicExamples(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := ClassicResource{
		Name:        "policies",
		Path:        "policies",
		CLIName:     "classic-policies",
		GoName:      "ClassicPolicies",
		Singular:    "policy",
		Description: "Deployment policies",
		Operations:  []string{"list", "get", "create", "update", "delete"},
		Lookups:     []string{"id"},
		IDPath:      "id",
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
		return
	}
	code := string(content)

	// Each operation should have Example block
	if !strings.Contains(code, "Example:") {
		t.Error("expected Example: field in generated code")
	}

	// List example should contain --field
	if !strings.Contains(code, "--field id") {
		t.Error("expected list example to show --field usage")
	}
	// Get example should show -o yaml
	if !strings.Contains(code, "-o yaml") {
		t.Error("expected get example to show -o yaml usage")
	}
	// Delete example should show --yes
	if !strings.Contains(code, "--yes") {
		t.Error("expected delete example to show --yes usage")
	}
	// Create example should show XML input
	if !strings.Contains(code, "cat policy.xml") {
		t.Error("expected create example to show XML input pattern")
	}
}

func TestGenerate_ClassicExamples_ListOnly(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := ClassicResource{
		Name:        "patchreports",
		Path:        "patchreports",
		CLIName:     "classic-patch-reports",
		GoName:      "ClassicPatchReports",
		Singular:    "patch_report",
		Description: "Patch reports",
		Operations:  []string{"list"},
		Lookups:     []string{"id"},
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
		return
	}
	code := string(content)

	// List example should exist
	if !strings.Contains(code, "classic-patch-reports list") {
		t.Error("expected list example with resource name")
	}
	// No delete/update/create examples
	if strings.Contains(code, "classic-patch-reports delete") {
		t.Error("unexpected delete example for list-only resource")
	}
}

func TestGenerate_CustomIDPath(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := ClassicResource{
		Name:        "account-groups",
		Path:        "accounts",
		CLIName:     "classic-account-groups",
		GoName:      "ClassicAccountGroups",
		Singular:    "account_group",
		Description: "Account groups",
		Operations:  []string{"get", "create", "update", "delete"},
		Lookups:     []string{"id", "groupname"},
		IDPath:      "groupid",
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
		return
	}
	code := string(content)

	// All ID-based paths must use groupid, not id
	if strings.Contains(code, "/accounts/id/") {
		t.Error("generated code must not use /accounts/id/ — should use /accounts/groupid/")
	}
	checks := []string{
		"/JSSResource/accounts/groupid/",
		"newClassicAccountGroupsGetCmd",
		"newClassicAccountGroupsCreateCmd",
		"newClassicAccountGroupsUpdateCmd",
		"newClassicAccountGroupsDeleteCmd",
		`"net/url"`,
		`"io"`,
		// groupname lookup is folded into get as --groupname flag
		`StringVar(&flagGroupname, "groupname"`,
	}
	for _, check := range checks {
		if !strings.Contains(code, check) {
			t.Errorf("generated code missing %q", check)
		}
	}
}

func TestGenerate_ExtraLookupsOnlyResource(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := ClassicResource{
		Name:        "computerapplications",
		Path:        "computerapplications",
		CLIName:     "classic-computer-apps",
		GoName:      "ClassicComputerApps",
		Singular:    "computer_application",
		Description: "Computer applications inventory",
		Operations:  []string{},
		Lookups:     []string{"application"},
		IDPath:      "id",
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
		return
	}
	code := string(content)

	// Resource with only extra lookups and no standard operations produces
	// an empty command group (no subcommands — lookups are folded into get,
	// but there is no get operation to fold them into).
	for _, unexpected := range []string{"ListCmd", "GetCmd(", "CreateCmd", "UpdateCmd", "DeleteCmd", "GetByApplicationCmd"} {
		if strings.Contains(code, unexpected) {
			t.Errorf("unexpected %s for extra-lookups-only resource", unexpected)
		}
	}
}

// --- Template helper function tests ---

func TestGenerate_ListSubset(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := ClassicResource{
		Name:        "account-users",
		Path:        "accounts",
		CLIName:     "classic-account-users",
		GoName:      "ClassicAccountUsers",
		Singular:    "account_user",
		Description: "Account users",
		Operations:  []string{"list", "get"},
		Lookups:     []string{"id", "username"},
		IDPath:      "userid",
		ListSubset:  "users",
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
		return
	}
	code := string(content)

	// Subset branch must emit the helper call and its literal subset name.
	mustHave := []string{
		`extractClassicListSubset(body, "users")`,
		`sliceClassicListSubsetXML(body, "users")`,
		`"/JSSResource/accounts"`,
	}
	for _, s := range mustHave {
		if !strings.Contains(code, s) {
			t.Errorf("generated code missing %q", s)
		}
	}

	// Normal list branch must NOT fire when ListSubset is set.
	mustNotHave := []string{
		"xmlconv.ExtractListItems(body)",
		`wrapper["account-users"]`,
	}
	for _, s := range mustNotHave {
		if strings.Contains(code, s) {
			t.Errorf("subset list should not emit %q, but generated code contains it", s)
		}
	}
}

func TestGenerateRegistry_ListSubsetHelpers(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resources := []ClassicResource{
		{
			Name:       "account-users",
			Path:       "accounts",
			CLIName:    "classic-account-users",
			GoName:     "ClassicAccountUsers",
			Singular:   "account_user",
			Operations: []string{"list"},
			Lookups:    []string{"id"},
			IDPath:     "userid",
			ListSubset: "users",
		},
	}

	outPath, err := gen.GenerateRegistry(resources)
	if err != nil {
		t.Fatalf("GenerateRegistry() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
		return
	}
	code := string(content)

	mustHave := []string{
		"func sliceClassicListSubsetXML(",
		"func extractClassicListSubset(",
	}
	for _, s := range mustHave {
		if !strings.Contains(code, s) {
			t.Errorf("registry missing helper: %q", s)
		}
	}
}

func TestGenerateRegistry_NoListSubsetHelpers_WhenNotNeeded(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resources := []ClassicResource{
		{
			Name:       "policies",
			Path:       "policies",
			CLIName:    "classic-policies",
			GoName:     "ClassicPolicies",
			Singular:   "policy",
			Operations: []string{"list", "get"},
			Lookups:    []string{"id"},
			IDPath:     "id",
		},
	}

	outPath, err := gen.GenerateRegistry(resources)
	if err != nil {
		t.Fatalf("GenerateRegistry() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
		return
	}
	code := string(content)

	if strings.Contains(code, "func sliceClassicListSubsetXML(") {
		t.Error("registry should not emit sliceClassicListSubsetXML when no resource needs it")
	}
	if strings.Contains(code, "func extractClassicListSubset(") {
		t.Error("registry should not emit extractClassicListSubset when no resource needs it")
	}
}

func TestHasOp(t *testing.T) {
	ops := []string{"list", "get", "create"}
	if !hasOp(ops, "list") {
		t.Error("expected hasOp to find 'list'")
	}
	if hasOp(ops, "delete") {
		t.Error("expected hasOp to not find 'delete'")
	}
	if hasOp(nil, "list") {
		t.Error("expected hasOp to return false for nil ops")
	}
}

func TestHasLookup(t *testing.T) {
	lookups := []string{"id", "name", "serialnumber"}
	if !hasLookup(lookups, "name") {
		t.Error("expected hasLookup to find 'name'")
	}
	if hasLookup(lookups, "udid") {
		t.Error("expected hasLookup to not find 'udid'")
	}
	if hasLookup(nil, "id") {
		t.Error("expected hasLookup to return false for nil lookups")
	}
}

func TestExtraLookups(t *testing.T) {
	lookups := []string{"id", "name", "serialnumber"}
	extra := extraLookups(lookups)
	if len(extra) != 2 {
		t.Fatalf("expected 2 extra lookups, got %d", len(extra))
	}
	if extra[0] != "name" || extra[1] != "serialnumber" {
		t.Errorf("extra = %v, want [name, serialnumber]", extra)
	}

	// id-only should return nil
	idOnly := extraLookups([]string{"id"})
	if len(idOnly) != 0 {
		t.Errorf("expected no extra lookups for id-only, got %v", idOnly)
	}
}

// --- Error path tests ---

func TestGenerate_BadOutputDir(t *testing.T) {
	gen := NewGenerator("/nonexistent/path/that/does/not/exist")

	resource := ClassicResource{
		Name:       "policies",
		Path:       "policies",
		CLIName:    "classic-policies",
		GoName:     "ClassicPolicies",
		Singular:   "policy",
		Operations: []string{"list"},
		Lookups:    []string{"id"},
	}

	_, err := gen.Generate(resource)
	if err == nil {
		t.Fatal("expected error for nonexistent output dir")
		return
	}
	if !strings.Contains(err.Error(), "creating file") {
		t.Errorf("error = %q, want to contain 'creating file'", err.Error())
	}
}

func TestGenerateRegistry_BadOutputDir(t *testing.T) {
	gen := NewGenerator("/nonexistent/path/that/does/not/exist")

	resources := []ClassicResource{
		{CLIName: "classic-policies", GoName: "ClassicPolicies"},
	}

	_, err := gen.GenerateRegistry(resources)
	if err == nil {
		t.Fatal("expected error for nonexistent output dir")
		return
	}
	if !strings.Contains(err.Error(), "creating file") {
		t.Errorf("error = %q, want to contain 'creating file'", err.Error())
	}
}

func TestGenerate_FilenameDedup(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	// CLIName already starts with "classic-" — filename should not double-prefix
	resource := ClassicResource{
		Name:       "policies",
		Path:       "policies",
		CLIName:    "classic-policies",
		GoName:     "ClassicPolicies",
		Singular:   "policy",
		Operations: []string{"list"},
		Lookups:    []string{"id"},
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	expectedFile := filepath.Join(dir, "classic_policies.go")
	if outPath != expectedFile {
		t.Errorf("output path = %q, want %q (no classic_classic_ prefix)", outPath, expectedFile)
	}
}

// --- Apply command tests ---

func TestGenerate_ApplyCommand(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := ClassicResource{
		Name:        "policies",
		Path:        "policies",
		CLIName:     "classic-policies",
		GoName:      "ClassicPolicies",
		Singular:    "policy",
		Description: "Deployment policies",
		Operations:  []string{"list", "get", "create", "update", "delete"},
		Lookups:     []string{"id", "name"},
		IDPath:      "id",
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
		return
	}
	code := string(content)

	checks := []string{
		"newClassicPoliciesApplyCmd",
		`"apply"`,
		"Create or replace a policy by name",
		"--from-file",
		`"yes"`,
		"dry-run",
		"readApplyInput",
		"extractClassicName",
		"resolveClassicNameToIDForApply",
		`"policy"`,                    // singular key
		`/JSSResource/policies/id/0`,  // create path
		`/JSSResource/policies/id/%s`, // update path
		"bytes.NewReader",
		`Created policy`,
		`Replaced policy`,
		`[dry-run] Would create policy`,
		`[dry-run] Would replace policy`,
	}

	for _, check := range checks {
		if !strings.Contains(code, check) {
			t.Errorf("generated classic apply code missing %q", check)
		}
	}

	// Imports should include "bytes"
	if !strings.Contains(code, `"bytes"`) {
		t.Error("expected bytes import for apply command")
	}
}

func TestGenerate_NoApply_WithoutName(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := ClassicResource{
		Name:        "vppaccounts",
		Path:        "vppaccounts",
		CLIName:     "classic-vpp-accounts",
		GoName:      "ClassicVppAccounts",
		Singular:    "vpp_account",
		Description: "VPP accounts",
		Operations:  []string{"list", "get", "create", "update", "delete"},
		Lookups:     []string{"id"}, // No name lookup
		IDPath:      "id",
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
		return
	}
	code := string(content)

	if strings.Contains(code, "ApplyCmd") {
		t.Error("unexpected apply command for resource without name lookup")
	}
}

func TestGenerate_NoApply_WithoutCreateUpdate(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := ClassicResource{
		Name:        "accounts",
		Path:        "accounts",
		CLIName:     "classic-accounts",
		GoName:      "ClassicAccounts",
		Singular:    "account",
		Description: "Accounts",
		Operations:  []string{"list", "get"}, // No create/update
		Lookups:     []string{"id", "name"},
		IDPath:      "id",
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
		return
	}
	code := string(content)

	if strings.Contains(code, "ApplyCmd") {
		t.Error("unexpected apply command for read-only resource")
	}
}

func TestGenerate_ApplyExample(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := ClassicResource{
		Name:        "printers",
		Path:        "printers",
		CLIName:     "classic-printers",
		GoName:      "ClassicPrinters",
		Singular:    "printer",
		Description: "Printers",
		Operations:  []string{"list", "get", "create", "update", "delete"},
		Lookups:     []string{"id", "name"},
		IDPath:      "id",
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
		return
	}
	code := string(content)

	// Apply example should reference XML files and --yes
	if !strings.Contains(code, "printer.xml") {
		t.Error("expected apply example to reference printer.xml")
	}
	if !strings.Contains(code, "classic-printers apply") {
		t.Error("expected apply example to use correct command name")
	}
}

func TestGenerateRegistry_WithApplyHelpers(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resources := []ClassicResource{
		{
			CLIName:    "classic-policies",
			GoName:     "ClassicPolicies",
			Operations: []string{"list", "get", "create", "update", "delete"},
			Lookups:    []string{"id", "name"},
		},
	}

	outPath, err := gen.GenerateRegistry(resources)
	if err != nil {
		t.Fatalf("GenerateRegistry() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
		return
	}
	code := string(content)

	// Registry should contain apply helper functions
	checks := []string{
		"extractClassicName",
		"resolveClassicNameToIDForApply",
		"xmlconv.ToMap",
		"xmlconv.ExtractListItems",
		`"general"`,       // checks under general sub-element
		`"name"`,          // name field extraction
		"extractIDString", // shared helper from modern registry
	}

	for _, check := range checks {
		if !strings.Contains(code, check) {
			t.Errorf("classic registry missing apply helper %q", check)
		}
	}
}

func TestGenerate_DeleteByNameCommand(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := ClassicResource{
		Name:        "printers",
		Path:        "printers",
		CLIName:     "classic-printers",
		GoName:      "ClassicPrinters",
		Singular:    "printer",
		Description: "Printers",
		Operations:  []string{"list", "get", "create", "update", "delete"},
		Lookups:     []string{"id", "name"},
		IDPath:      "id",
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
		return
	}
	code := string(content)

	// delete command should have --name flag with inline name resolution
	checks := []string{
		`StringVar(&flagName, "name"`,
		"resolveClassicNameToIDForApply",
		`[dry-run] Would delete printer`,
		`"yes"`,
		"dry-run",
	}

	for _, check := range checks {
		if !strings.Contains(code, check) {
			t.Errorf("generated classic delete --name code missing %q", check)
		}
	}

	// should NOT generate a separate delete-by-name subcommand
	if strings.Contains(code, "DeleteByNameCmd") {
		t.Error("should not generate a separate DeleteByNameCmd")
	}
}

func TestGenerate_NoDeleteByName_WithoutNameLookup(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := ClassicResource{
		Name:        "vppaccounts",
		Path:        "vppaccounts",
		CLIName:     "classic-vpp-accounts",
		GoName:      "ClassicVppAccounts",
		Singular:    "vpp_account",
		Description: "VPP accounts",
		Operations:  []string{"list", "get", "create", "update", "delete"},
		Lookups:     []string{"id"}, // No name lookup
		IDPath:      "id",
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
		return
	}
	code := string(content)

	if strings.Contains(code, "DeleteByNameCmd") {
		t.Error("unexpected delete-by-name for resource without name lookup")
	}
}

func TestGenerateRegistry_NoApplyHelpers_WhenNotNeeded(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resources := []ClassicResource{
		{
			CLIName:    "classic-accounts",
			GoName:     "ClassicAccounts",
			Operations: []string{"list", "get"},
			Lookups:    []string{"id"},
		},
	}

	outPath, err := gen.GenerateRegistry(resources)
	if err != nil {
		t.Fatalf("GenerateRegistry() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
		return
	}
	code := string(content)

	// Registry should NOT contain apply helpers when no resource needs apply
	if strings.Contains(code, "extractClassicName") {
		t.Error("unexpected apply helpers in registry when no resource has apply")
	}
	if strings.Contains(code, "resolveClassicNameToIDForApply") {
		t.Error("unexpected apply helpers in registry when no resource has apply")
	}
}

func TestGenerate_SerialAlias(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := ClassicResource{
		Name:        "computerhistory",
		Path:        "computerhistory",
		CLIName:     "classic-computer-history",
		GoName:      "ClassicComputerHistory",
		Singular:    "computer_history",
		Description: "Computer history records",
		Operations:  []string{"get"},
		Lookups:     []string{"id", "name", "serialnumber"},
		IDPath:      "id",
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
		return
	}
	code := string(content)

	// canonical flag still registered
	if !strings.Contains(code, `StringVar(&flagSerialnumber, "serialnumber"`) {
		t.Error("expected canonical --serialnumber flag")
	}
	// alias registered, bound to the SAME variable
	if !strings.Contains(code, `StringVar(&flagSerialnumber, "serial"`) {
		t.Error("expected --serial alias bound to flagSerialnumber")
	}
	// canonical flag help advertises the alias (two-way discoverability)
	if !strings.Contains(code, `by serialnumber (alias: --serial)`) {
		t.Error("expected --serialnumber help to advertise the --serial alias")
	}
	// no-identifier error message lists the alias too
	if !strings.Contains(code, `--serialnumber, --serial`) {
		t.Error("expected the no-identifier error to mention --serial")
	}
}

func TestGenerate_Filename(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := ClassicResource{
		Name:        "policies",
		Path:        "policies",
		CLIName:     "classic-policies",
		GoName:      "ClassicPolicies",
		Singular:    "policy",
		Description: "Deployment policies",
		Operations:  []string{"list"},
		Lookups:     []string{"id"},
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	expectedFile := filepath.Join(dir, "classic_policies.go")
	if outPath != expectedFile {
		t.Errorf("output path = %q, want %q", outPath, expectedFile)
	}
}

func TestGenerate_Subset(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := ClassicResource{
		Name:        "computerhistory",
		Path:        "computerhistory",
		CLIName:     "classic-computer-history",
		GoName:      "ClassicComputerHistory",
		Singular:    "computer_history",
		Description: "Computer history records",
		Operations:  []string{"get"},
		Lookups:     []string{"id", "name", "serialnumber"},
		IDPath:      "id",
		Subsets:     []string{"General", "Commands"},
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
		return
	}
	code := string(content)

	if !strings.Contains(code, `StringVar(&flagSubset, "subset"`) {
		t.Error("expected --subset flag")
	}
	if !strings.Contains(code, `RegisterFlagCompletionFunc("subset"`) {
		t.Error("expected subset completion registration")
	}
	if !strings.Contains(code, `"General"`) || !strings.Contains(code, `"Commands"`) {
		t.Error("expected curated subset values in completion list")
	}
	if !strings.Contains(code, `path += "/subset/" + registry.EscapeClassicPathSegment(flagSubset)`) {
		t.Error("expected /subset/ path append using the classic path escaper")
	}
	// A non-id lookup combined with --subset must resolve to an id first, so the
	// request uses id/{id}/subset/ (the Platform Gateway 403s non-id + /subset/).
	if !strings.Contains(code, "resolveClassicRecordID(reqCtx, ctx.Client, path,") {
		t.Error("expected non-id lookup + subset to resolve to an id first")
	}
	if !strings.Contains(code, "pathByID") {
		t.Error("expected pathByID tracking to gate the id-resolution")
	}
}

func TestGenerate_NoSubsetWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := ClassicResource{
		Name:        "policies",
		Path:        "policies",
		CLIName:     "classic-policies",
		GoName:      "ClassicPolicies",
		Singular:    "policy",
		Description: "Deployment policies",
		Operations:  []string{"list", "get"},
		Lookups:     []string{"id", "name"},
		IDPath:      "id",
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
		return
	}
	code := string(content)

	if strings.Contains(code, "flagSubset") {
		t.Error("did not expect --subset wiring for a resource without subsets")
	}
}
