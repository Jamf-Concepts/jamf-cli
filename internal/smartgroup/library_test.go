// Copyright 2026, Jamf Software LLC

package smartgroup

import (
	"sort"
	"strings"
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
	if c.Name != "FileVault 2 Status" || c.SearchType != "is" || c.Value != "No Partitions Encrypted" {
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

// ─── MDM-health category golden tests ──────────────────────────────────────

func TestMDM_BootstrapTokenMissing_Golden(t *testing.T) {
	tmpl, _ := Lookup("mdm/bootstrap-token-missing")
	req, err := tmpl.Build(map[string]any{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	c := req.Criteria[0]
	if c.Name != "Bootstrap Token Escrowed" || c.SearchType != "is" || c.Value != "No" {
		t.Fatalf("unexpected criterion: %+v", c)
	}
}

func TestMDM_UserApprovedMDMNo_Golden(t *testing.T) {
	tmpl, _ := Lookup("mdm/user-approved-mdm-no")
	req, err := tmpl.Build(map[string]any{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	c := req.Criteria[0]
	if c.Name != "User Approved MDM" || c.SearchType != "is" || c.Value != "No" {
		t.Fatalf("unexpected criterion: %+v", c)
	}
}

func TestMDM_StaleCheckin_GoldenDefault(t *testing.T) {
	tmpl, _ := Lookup("mdm/stale-checkin")
	opts, err := tmpl.ResolveOpts(map[string]any{})
	if err != nil {
		t.Fatalf("ResolveOpts: %v", err)
	}
	req, err := tmpl.Build(opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	c := req.Criteria[0]
	if c.Name != "Last Inventory Update" || c.SearchType != "more than x days ago" || c.Value != "7" {
		t.Fatalf("unexpected criterion: %+v", c)
	}
}

func TestMDM_MDMCertExpiring_GoldenDefault(t *testing.T) {
	tmpl, _ := Lookup("mdm/mdm-cert-expiring")
	opts, err := tmpl.ResolveOpts(map[string]any{})
	if err != nil {
		t.Fatalf("ResolveOpts: %v", err)
	}
	req, err := tmpl.Build(opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	c := req.Criteria[0]
	if c.Name != "MDM Profile Expiration Date" || c.SearchType != "less than x days from now" || c.Value != "30" {
		t.Fatalf("unexpected criterion: %+v", c)
	}
}

func TestMDM_DDMDisabled_Golden(t *testing.T) {
	tmpl, _ := Lookup("mdm/declarative-management-disabled")
	req, err := tmpl.Build(map[string]any{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	c := req.Criteria[0]
	if c.Name != "Declarative Device Management Enabled" || c.SearchType != "is" || c.Value != "No" {
		t.Fatalf("unexpected criterion: %+v", c)
	}
}

// ─── Compliance category golden tests ──────────────────────────────────────

func TestCompliance_GatekeeperDisabled_Golden(t *testing.T) {
	tmpl, _ := Lookup("compliance/gatekeeper-disabled")
	req, err := tmpl.Build(map[string]any{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	c := req.Criteria[0]
	if c.Name != "Gatekeeper" || c.SearchType != "is" || c.Value != "Disabled" {
		t.Fatalf("unexpected criterion: %+v", c)
	}
}

func TestCompliance_SIPDisabled_Golden(t *testing.T) {
	tmpl, _ := Lookup("compliance/sip-disabled")
	req, err := tmpl.Build(map[string]any{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	c := req.Criteria[0]
	if c.Name != "System Integrity Protection" || c.SearchType != "is" || c.Value != "Disabled" {
		t.Fatalf("unexpected criterion: %+v", c)
	}
}

func TestCompliance_FirewallDisabled_Golden(t *testing.T) {
	tmpl, _ := Lookup("compliance/firewall-disabled")
	req, err := tmpl.Build(map[string]any{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	c := req.Criteria[0]
	if c.Name != "Firewall Enabled" || c.SearchType != "is" || c.Value != "No" {
		t.Fatalf("unexpected criterion: %+v", c)
	}
}

func TestCompliance_NonCompliantBaseline_Golden(t *testing.T) {
	tmpl, _ := Lookup("compliance/non-compliant-baseline")
	req, err := tmpl.Build(map[string]any{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(req.Criteria) != 4 {
		t.Fatalf("expected 4 criteria, got %d", len(req.Criteria))
	}
	for i, c := range req.Criteria {
		if i == 0 && c.AndOr != "and" {
			t.Errorf("first criterion should be 'and', got %q", c.AndOr)
		}
		if i > 0 && c.AndOr != "or" {
			t.Errorf("criterion %d should be 'or', got %q", i, c.AndOr)
		}
	}
}

// ─── Lifecycle category golden tests ───────────────────────────────────────

func TestLifecycle_Unsupervised_Golden(t *testing.T) {
	tmpl, _ := Lookup("lifecycle/unsupervised")
	req, err := tmpl.Build(map[string]any{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	c := req.Criteria[0]
	if c.Name != "Supervised" || c.SearchType != "is" || c.Value != "No" {
		t.Fatalf("unexpected criterion: %+v", c)
	}
}

func TestLifecycle_ADEEnrolled_Golden(t *testing.T) {
	tmpl, _ := Lookup("lifecycle/ade-enrolled")
	req, err := tmpl.Build(map[string]any{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	c := req.Criteria[0]
	if c.Name != "Enrollment Method: PreStage enrollment" || c.SearchType != "is" || c.Value != "Yes" {
		t.Fatalf("unexpected criterion: %+v", c)
	}
}

func TestLifecycle_JamfBinaryOutdated_Golden(t *testing.T) {
	tmpl, _ := Lookup("lifecycle/jamf-binary-outdated")
	if _, err := tmpl.ResolveOpts(map[string]any{}); err == nil {
		t.Fatal("expected error for missing required --below-version")
	}
	opts, err := tmpl.ResolveOpts(map[string]any{"below-version": "11.0.0"})
	if err != nil {
		t.Fatalf("ResolveOpts: %v", err)
	}
	req, err := tmpl.Build(opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	c := req.Criteria[0]
	if c.Name != "Jamf Binary Version" || c.SearchType != "less than" || c.Value != "11.0.0" {
		t.Fatalf("unexpected criterion: %+v", c)
	}
}

func TestLifecycle_FVIneligibleHardware_Golden(t *testing.T) {
	tmpl, _ := Lookup("lifecycle/fv-ineligible-hardware")
	req, err := tmpl.Build(map[string]any{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(req.Criteria) != 2 {
		t.Fatalf("expected 2 criteria, got %d", len(req.Criteria))
	}
	if req.Criteria[0].Name != "FileVault 2 Status" || req.Criteria[0].Value != "N/A" {
		t.Fatalf("unexpected criterion 0: %+v", req.Criteria[0])
	}
	if req.Criteria[1].Name != "Apple Silicon" || req.Criteria[1].Value != "No" {
		t.Fatalf("unexpected criterion 1: %+v", req.Criteria[1])
	}
}

// ─── Whole-library integration tests ───────────────────────────────────────

func TestLibrary_ExactlyTwentyThreeTemplates(t *testing.T) {
	got := len(All())
	const want = 23
	if got != want {
		t.Fatalf("expected %d templates registered, got %d", want, got)
	}
}

func TestLibrary_AllCategoriesPresent(t *testing.T) {
	want := map[string]int{
		"encryption": 6,
		"updates":    4,
		"mdm":        5,
		"compliance": 4,
		"lifecycle":  4,
	}
	got := make(map[string]int)
	for _, tt := range All() {
		got[tt.Category]++
	}
	for cat, n := range want {
		if got[cat] != n {
			t.Errorf("category %s: expected %d templates, got %d", cat, n, got[cat])
		}
	}
}

func TestLibrary_EveryTemplateProducesValidCriteria(t *testing.T) {
	known := allCriterionConsts()
	knownValues := make(map[string]struct{}, len(known))
	for _, v := range known {
		knownValues[v] = struct{}{}
	}
	for _, tmpl := range All() {
		t.Run(tmpl.Slug, func(t *testing.T) {
			opts, err := tmpl.ResolveOpts(defaultOptsForTest(tmpl))
			if err != nil {
				t.Fatalf("ResolveOpts: %v", err)
			}
			req, err := tmpl.Build(opts)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if len(req.Criteria) == 0 {
				t.Fatal("template produced zero criteria")
			}
			for i, c := range req.Criteria {
				if _, ok := knownValues[c.Name]; !ok {
					t.Errorf("criterion %d uses unregistered name %q (must be one of the criteria.go consts)", i, c.Name)
				}
				if c.SearchType == "" {
					t.Errorf("criterion %d has empty searchType", i)
				}
				if c.AndOr != "and" && c.AndOr != "or" {
					t.Errorf("criterion %d has invalid andOr %q", i, c.AndOr)
				}
			}
		})
	}
}

// defaultOptsForTest supplies sensible values for required params during the
// whole-library scan. Keep this in sync with required-param templates.
func defaultOptsForTest(t Template) map[string]any {
	out := make(map[string]any, len(t.Params))
	for _, p := range t.Params {
		if !p.Required {
			continue
		}
		switch t.Slug {
		case "updates/os-version-below":
			out["below-version"] = "15.0"
		case "updates/major-version-behind":
			out["major-below"] = 15
		case "lifecycle/jamf-binary-outdated":
			out["below-version"] = "11.0.0"
		}
	}
	return out
}

func TestLibrary_AllSlugsUseCategoryPrefix(t *testing.T) {
	for _, tmpl := range All() {
		prefix := tmpl.Category + "/"
		if !strings.HasPrefix(tmpl.Slug, prefix) {
			t.Errorf("template %q (category %q): slug should start with %q", tmpl.Slug, tmpl.Category, prefix)
		}
	}
}
