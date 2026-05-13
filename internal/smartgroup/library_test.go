// Copyright 2026, Jamf Software LLC

package smartgroup

import (
	"sort"
	"testing"
)

func TestLibraryEmptyByDefault(t *testing.T) {
	_, ok := Lookup("nonexistent/slug")
	if ok {
		t.Fatal("expected Lookup of missing slug to return false")
	}
}

func TestRegisterDuplicateSlugPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate slug, got none")
		}
	}()
	tmpl := Template{Slug: "test/dup", Category: "test", Build: trivialBuild}
	Register(tmpl)
	defer Unregister("test/dup")
	Register(tmpl)
}

func TestCategoriesReturnsSortedUnique(t *testing.T) {
	Register(Template{Slug: "alpha/one", Category: "alpha", Build: trivialBuild})
	defer Unregister("alpha/one")
	Register(Template{Slug: "beta/one", Category: "beta", Build: trivialBuild})
	defer Unregister("beta/one")
	Register(Template{Slug: "alpha/two", Category: "alpha", Build: trivialBuild})
	defer Unregister("alpha/two")

	got := Categories()
	if !sort.StringsAreSorted(got) {
		t.Fatalf("categories not sorted: %v", got)
	}
	foundAlpha, foundBeta := false, false
	for _, c := range got {
		if c == "alpha" {
			foundAlpha = true
		}
		if c == "beta" {
			foundBeta = true
		}
	}
	if !foundAlpha || !foundBeta {
		t.Fatalf("expected alpha and beta in %v", got)
	}
}

func trivialBuild(_ map[string]any) (SmartGroupRequest, error) {
	return SmartGroupRequest{}, nil
}

// ─── Encryption category golden tests ──────────────────────────────────────

func TestEncryption_NotEncrypted_Golden(t *testing.T) {
	tmpl, ok := Lookup("encryption/not-encrypted")
	if !ok {
		t.Fatal("template encryption/not-encrypted not registered")
	}
	req, err := tmpl.Build(map[string]any{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if len(req.Criteria) != 1 {
		t.Fatalf("expected 1 criterion, got %d", len(req.Criteria))
	}
	c := req.Criteria[0]
	if c.Name != "FileVault 2 Status" || c.SearchType != "is" || c.Value != "Not Encrypted" {
		t.Fatalf("unexpected criterion: %+v", c)
	}
}

func TestEncryption_InvalidRecoveryKey_Golden(t *testing.T) {
	tmpl, _ := Lookup("encryption/invalid-recovery-key")
	req, err := tmpl.Build(map[string]any{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	c := req.Criteria[0]
	if c.Name != "FileVault 2 Individual Key Validation" || c.SearchType != "is" || c.Value != "Not Valid" {
		t.Fatalf("unexpected criterion: %+v", c)
	}
}

func TestEncryption_EscrowMissing_Golden(t *testing.T) {
	tmpl, _ := Lookup("encryption/escrow-missing")
	req, err := tmpl.Build(map[string]any{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	c := req.Criteria[0]
	if c.Name != "FileVault 2 Recovery Key Type" || c.SearchType != "is" || c.Value != "" {
		t.Fatalf("unexpected criterion: %+v", c)
	}
}

func TestEncryption_IRKOnlyDeprecated_Golden(t *testing.T) {
	tmpl, _ := Lookup("encryption/irk-only-deprecated")
	req, err := tmpl.Build(map[string]any{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	c := req.Criteria[0]
	if c.Name != "FileVault 2 Recovery Key Type" || c.SearchType != "is" || c.Value != "Institutional" {
		t.Fatalf("unexpected criterion: %+v", c)
	}
}

func TestEncryption_EncryptionStalled_GoldenDefault(t *testing.T) {
	tmpl, _ := Lookup("encryption/encryption-stalled")
	opts, err := tmpl.ResolveOpts(map[string]any{})
	if err != nil {
		t.Fatalf("ResolveOpts: %v", err)
	}
	req, err := tmpl.Build(opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(req.Criteria) != 2 {
		t.Fatalf("expected 2 criteria, got %d", len(req.Criteria))
	}
	if req.Criteria[1].Value != "7" {
		t.Fatalf("expected default 7 days, got %q", req.Criteria[1].Value)
	}
}

func TestEncryption_EncryptionStalled_GoldenCustom(t *testing.T) {
	tmpl, _ := Lookup("encryption/encryption-stalled")
	opts, err := tmpl.ResolveOpts(map[string]any{"stalled-after": 14})
	if err != nil {
		t.Fatalf("ResolveOpts: %v", err)
	}
	req, err := tmpl.Build(opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if req.Criteria[1].Value != "14" {
		t.Fatalf("expected 14 days, got %q", req.Criteria[1].Value)
	}
}

func TestEncryption_FVIneligible_Golden(t *testing.T) {
	tmpl, _ := Lookup("encryption/fv-ineligible")
	req, err := tmpl.Build(map[string]any{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	c := req.Criteria[0]
	if c.Name != "FileVault 2 Status" || c.SearchType != "is" || c.Value != "N/A" {
		t.Fatalf("unexpected criterion: %+v", c)
	}
}

// ─── Updates category golden tests ─────────────────────────────────────────

func TestUpdates_OSVersionBelow_Golden(t *testing.T) {
	tmpl, ok := Lookup("updates/os-version-below")
	if !ok {
		t.Fatal("template updates/os-version-below not registered")
	}
	if _, err := tmpl.ResolveOpts(map[string]any{}); err == nil {
		t.Fatal("expected error for missing required --below-version")
	}
	opts, err := tmpl.ResolveOpts(map[string]any{"below-version": "15.4"})
	if err != nil {
		t.Fatalf("ResolveOpts: %v", err)
	}
	req, err := tmpl.Build(opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	c := req.Criteria[0]
	if c.Name != "Operating System Version" || c.SearchType != "less than" || c.Value != "15.4" {
		t.Fatalf("unexpected criterion: %+v", c)
	}
}

func TestUpdates_MajorVersionBehind_Golden(t *testing.T) {
	tmpl, _ := Lookup("updates/major-version-behind")
	opts, err := tmpl.ResolveOpts(map[string]any{"major-below": 15})
	if err != nil {
		t.Fatalf("ResolveOpts: %v", err)
	}
	req, err := tmpl.Build(opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	c := req.Criteria[0]
	if c.Name != "Operating System Version" || c.SearchType != "less than" || c.Value != "15.0" {
		t.Fatalf("unexpected criterion: %+v", c)
	}
}

func TestUpdates_RSRNotApplied_Golden(t *testing.T) {
	tmpl, _ := Lookup("updates/rsr-not-applied")
	req, err := tmpl.Build(map[string]any{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	c := req.Criteria[0]
	if c.Name != "Operating System Rapid Security Response" || c.SearchType != "is" || c.Value != "" {
		t.Fatalf("unexpected criterion: %+v", c)
	}
}

func TestUpdates_BetaOS_Golden(t *testing.T) {
	tmpl, _ := Lookup("updates/beta-os")
	req, err := tmpl.Build(map[string]any{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	c := req.Criteria[0]
	if c.Name != "Operating System Version" || c.SearchType != "like" || c.Value != "Beta" {
		t.Fatalf("unexpected criterion: %+v", c)
	}
}
