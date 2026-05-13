# pro smart-group templates — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a `pro smart-group` namespace with 4 commands (`templates`, `preview`, `apply`, `verify-templates`) backed by a curated, JSS-verified library of 23 smart-group templates across 5 categories.

**Architecture:** New handwritten `internal/smartgroup/` package owns the template library, criterion-name constants, builder functions, membership-check helper, and verify-runner. New `internal/commands/pro_smartgroup.go` is the cobra command surface that wraps the library and talks to the existing `/v2/computer-groups/smart-groups` endpoints via `cliCtx.Client.Do`. No generator changes, no spec changes.

**Tech Stack:** Go 1.21+, cobra, the project's existing `internal/registry.CLIContext` + `internal/output` packages, table-driven tests with golden JSON fixtures.

**Spec:** `docs/solutions/design-patterns/pro-smart-group-templates-spec-2026-05-12.md`

---

## File Structure

```
internal/smartgroup/
  types.go                    Template, ParamSpec, SmartGroupRequest, Criterion
  types_test.go               param validation
  criteria.go                 Go consts for JSS-verified criterion names
  criteria_test.go            assert every const is non-empty and unique
  library.go                  Library map + lookup + listing helpers
  library_test.go             registry sanity + per-template golden JSON
  encryption.go               6 encryption templates
  updates.go                  4 software-updates templates
  mdm.go                      5 MDM-health templates
  compliance.go               4 compliance templates
  lifecycle.go                4 lifecycle templates
  membership.go               post-apply membership-count helper
  membership_test.go          membership-helper unit tests
  verify.go                   verify-templates runner
  verify_test.go              verify-runner unit tests (mocked HTTP)

internal/commands/
  pro_smartgroup.go           namespace + 4 subcommands
  pro_smartgroup_test.go      golden output tests per command per format

internal/commands/pro.go      +1 line: cmd.AddCommand(newSmartGroupCmd(cliCtx))
internal/commands/groups.go   +1 entry in proGroupMap
internal/commands/aliases.go  +1 entry in commandAliases
```

---

## Task 1: Foundation types (Template, ParamSpec, request struct)

**Files:**
- Create: `internal/smartgroup/types.go`
- Create: `internal/smartgroup/types_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/smartgroup/types_test.go`:

```go
// Copyright 2026, Jamf Software LLC

package smartgroup

import (
	"testing"
)

func TestValidateOpts_RequiredMissing(t *testing.T) {
	spec := ParamSpec{Name: "below-version", Type: "string", Required: true}
	tmpl := Template{
		Slug:   "test/required",
		Params: []ParamSpec{spec},
	}
	_, err := tmpl.ResolveOpts(map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing required param, got nil")
	}
}

func TestValidateOpts_TypeMismatch(t *testing.T) {
	spec := ParamSpec{Name: "days", Type: "int"}
	tmpl := Template{
		Slug:   "test/typed",
		Params: []ParamSpec{spec},
	}
	_, err := tmpl.ResolveOpts(map[string]any{"days": "not-an-int"})
	if err == nil {
		t.Fatal("expected error for type mismatch, got nil")
	}
}

func TestValidateOpts_DefaultApplied(t *testing.T) {
	spec := ParamSpec{Name: "days", Type: "int", Default: 7}
	tmpl := Template{
		Slug:   "test/default",
		Params: []ParamSpec{spec},
	}
	opts, err := tmpl.ResolveOpts(map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts["days"] != 7 {
		t.Fatalf("expected default 7, got %v", opts["days"])
	}
}

func TestValidateOpts_NoParamsAccepted(t *testing.T) {
	tmpl := Template{Slug: "test/noparam", Params: nil}
	opts, err := tmpl.ResolveOpts(map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opts) != 0 {
		t.Fatalf("expected empty opts, got %v", opts)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/smartgroup/ -run TestValidateOpts -v`
Expected: FAIL with "undefined: ParamSpec" or "undefined: Template".

- [ ] **Step 3: Implement minimal types**

Create `internal/smartgroup/types.go`:

```go
// Copyright 2026, Jamf Software LLC

// Package smartgroup curates a library of Jamf Pro smart-group templates
// admins can instantiate via the CLI. Criterion-name strings are sourced from
// the JSS server (see criteria.go for citations); the library is exercised
// against a live tenant via pro smart-group verify-templates.
package smartgroup

import "fmt"

// ParamSpec describes a single named parameter on a parameterized template.
// Templates have at most one ParamSpec; multi-param templates should be split
// into discrete variants.
type ParamSpec struct {
	Name        string // CLI flag name, e.g. "stalled-after"
	Type        string // "int" | "string" | "version"
	Default     any    // applied when caller omits the param; nil iff Required
	Description string // for --help
	Required    bool
}

// Template is one curated smart-group recipe in the library.
type Template struct {
	Slug        string                                            // e.g. "encryption/not-encrypted"
	Category    string                                            // e.g. "encryption"
	Description string                                            // one-line for table listings
	Params      []ParamSpec                                       // zero or one entry
	Build       func(opts map[string]any) (SmartGroupRequest, error)
}

// SmartGroupRequest is the JSON body posted to /v2/computer-groups/smart-groups.
// We define our own type rather than importing the generated SmartComputerGroupV2
// so the smartgroup package can be tested in isolation.
type SmartGroupRequest struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Criteria    []Criterion `json:"criteria,omitempty"`
	SiteID      string      `json:"siteId,omitempty"`
}

// Criterion is one row in SmartGroupRequest.Criteria; matches the
// SmartSearchCriterion schema from specs/_MonolithLibrary.yaml.
type Criterion struct {
	AndOr        string `json:"andOr"`
	Name         string `json:"name"`
	SearchType   string `json:"searchType"`
	Value        string `json:"value"`
	Priority     int    `json:"priority"`
	OpeningParen bool   `json:"openingParen"`
	ClosingParen bool   `json:"closingParen"`
}

// ResolveOpts validates and normalises a caller-supplied opts map against the
// template's ParamSpec list. Required params must be present. Type mismatches
// return an error. Defaults are filled in when omitted.
func (t Template) ResolveOpts(in map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(t.Params))
	for _, p := range t.Params {
		raw, present := in[p.Name]
		if !present {
			if p.Required {
				return nil, fmt.Errorf("template %s requires param --%s", t.Slug, p.Name)
			}
			if p.Default != nil {
				out[p.Name] = p.Default
			}
			continue
		}
		val, err := coerceTo(p.Type, raw)
		if err != nil {
			return nil, fmt.Errorf("template %s: param --%s: %w", t.Slug, p.Name, err)
		}
		out[p.Name] = val
	}
	return out, nil
}

func coerceTo(typ string, raw any) (any, error) {
	switch typ {
	case "int":
		switch v := raw.(type) {
		case int:
			return v, nil
		case int64:
			return int(v), nil
		case float64:
			return int(v), nil
		case string:
			var n int
			if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
				return nil, fmt.Errorf("expected int, got %q", v)
			}
			return n, nil
		default:
			return nil, fmt.Errorf("expected int, got %T", raw)
		}
	case "string", "version":
		if s, ok := raw.(string); ok {
			return s, nil
		}
		return nil, fmt.Errorf("expected string, got %T", raw)
	default:
		return nil, fmt.Errorf("unknown param type %q", typ)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/smartgroup/ -run TestValidateOpts -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/smartgroup/types.go internal/smartgroup/types_test.go
git commit -m "feat(smartgroup): introduce Template + ParamSpec foundation types"
```

---

## Task 2: Criterion-name constants (JSS-verified)

**Files:**
- Create: `internal/smartgroup/criteria.go`
- Create: `internal/smartgroup/criteria_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/smartgroup/criteria_test.go`:

```go
// Copyright 2026, Jamf Software LLC

package smartgroup

import (
	"strings"
	"testing"
)

func TestCriterionConstsNotEmpty(t *testing.T) {
	consts := allCriterionConsts()
	for name, value := range consts {
		if strings.TrimSpace(value) == "" {
			t.Errorf("criterion const %s is empty", name)
		}
	}
}

func TestCriterionConstsUnique(t *testing.T) {
	consts := allCriterionConsts()
	seen := make(map[string]string)
	for name, value := range consts {
		if other, ok := seen[value]; ok {
			t.Errorf("criterion value %q used by both %s and %s", value, name, other)
		}
		seen[value] = name
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/smartgroup/ -run TestCriterion -v`
Expected: FAIL with "undefined: allCriterionConsts".

- [ ] **Step 3: Implement the criterion-name registry**

Create `internal/smartgroup/criteria.go`:

```go
// Copyright 2026, Jamf Software LLC

package smartgroup

// Smart-group criterion names. These strings must match Jamf Pro's smart-group
// criterion UI exactly. The canonical source is the JSS server repo
// (jamf/jss). Each const cites the file:line it was sourced from. Re-verify
// after any sync-specs pass that includes JSS source updates.

const (
	// FileVault2StatusMatcher.java:@Component("FileVault 2 Status")
	CriterionFV2Status = "FileVault 2 Status"

	// MatcherNameConstants.java:CD.FILE_VAULT_2_ENABLED
	CriterionFV2Enabled = "FileVault 2 Enabled"

	// ComputerInventoryValues.java:103
	CriterionFV2RecoveryKeyType = "FileVault 2 Recovery Key Type"

	// ComputerInventoryValues.java:104
	CriterionFV2IndividualKeyValidation = "FileVault 2 Individual Key Validation"

	// ComputerInventoryValues.java:106
	CriterionFV2PersonalRecoveryKey = "FileVault 2 Personal Recovery Key"

	// MatcherNameConstants.java:CD.OPERATING_SYSTEM_VERSION
	CriterionOSVersion = "Operating System Version"

	// MatcherNameConstants.java:CD.OPERATING_SYSTEM_BUILD
	CriterionOSBuild = "Operating System Build"

	// MatcherNameConstants.java:CD.OPERATING_SYSTEM_SUPPLEMENTAL_VERSION_EXTRA
	CriterionOSRapidSecurityResponse = "Operating System Rapid Security Response"

	// MatcherNameConstants.java:MDD.LAST_INVENTORY_UPDATE
	CriterionLastInventoryUpdate = "Last Inventory Update"

	// MatcherNameConstants.java:MDD.BOOTSTRAP_TOKEN_ESCROWED
	CriterionBootstrapTokenEscrowed = "Bootstrap Token Escrowed"

	// UserApprovedMdmMatcher.java:@Component("User Approved MDM")
	CriterionUserApprovedMDM = "User Approved MDM"

	// MatcherNameConstants.java:MDD.MDM_PROFILE_EXPIRATION_DATE
	CriterionMDMProfileExpirationDate = "MDM Profile Expiration Date"

	// MatcherNameConstants.java:CD.DECLARATIVE_DEVICE_MANAGEMENT_ENABLED
	CriterionDDMEnabled = "Declarative Device Management Enabled"

	// ComputerInventoryValues.java:118
	CriterionGatekeeper = "Gatekeeper"

	// ComputerInventoryValues.java:119
	CriterionSIP = "System Integrity Protection"

	// MatcherNameConstants.java:CD.FIREWALL_ENABLED
	CriterionFirewallEnabled = "Firewall Enabled"

	// MatcherNameConstants.java:MDD.SUPERVISED
	CriterionSupervised = "Supervised"

	// MatcherNameConstants.java:E.PRESTAGE
	CriterionEnrollmentMethodPrestage = "Enrollment Method: PreStage enrollment"

	// MatcherNameConstants.java:CD.APPLE_SILICON
	CriterionAppleSilicon = "Apple Silicon"

	// Parallel inventory criterion; pro smart-group verify-templates is the
	// empirical check against a live tenant.
	CriterionJamfBinaryVersion = "Jamf Binary Version"
)

// allCriterionConsts returns the full registry as a map for testing.
// Keep in sync with the const block above.
func allCriterionConsts() map[string]string {
	return map[string]string{
		"CriterionFV2Status":                  CriterionFV2Status,
		"CriterionFV2Enabled":                 CriterionFV2Enabled,
		"CriterionFV2RecoveryKeyType":         CriterionFV2RecoveryKeyType,
		"CriterionFV2IndividualKeyValidation": CriterionFV2IndividualKeyValidation,
		"CriterionFV2PersonalRecoveryKey":     CriterionFV2PersonalRecoveryKey,
		"CriterionOSVersion":                  CriterionOSVersion,
		"CriterionOSBuild":                    CriterionOSBuild,
		"CriterionOSRapidSecurityResponse":    CriterionOSRapidSecurityResponse,
		"CriterionLastInventoryUpdate":        CriterionLastInventoryUpdate,
		"CriterionBootstrapTokenEscrowed":     CriterionBootstrapTokenEscrowed,
		"CriterionUserApprovedMDM":            CriterionUserApprovedMDM,
		"CriterionMDMProfileExpirationDate":   CriterionMDMProfileExpirationDate,
		"CriterionDDMEnabled":                 CriterionDDMEnabled,
		"CriterionGatekeeper":                 CriterionGatekeeper,
		"CriterionSIP":                        CriterionSIP,
		"CriterionFirewallEnabled":            CriterionFirewallEnabled,
		"CriterionSupervised":                 CriterionSupervised,
		"CriterionEnrollmentMethodPrestage":   CriterionEnrollmentMethodPrestage,
		"CriterionAppleSilicon":               CriterionAppleSilicon,
		"CriterionJamfBinaryVersion":          CriterionJamfBinaryVersion,
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/smartgroup/ -run TestCriterion -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/smartgroup/criteria.go internal/smartgroup/criteria_test.go
git commit -m "feat(smartgroup): add JSS-verified criterion-name constants"
```

---

## Task 3: Library registry skeleton

**Files:**
- Create: `internal/smartgroup/library.go`
- Create: `internal/smartgroup/library_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/smartgroup/library_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/smartgroup/ -run "TestLibrary|TestRegister|TestCategories" -v`
Expected: FAIL with "undefined: Lookup" / "undefined: Register" / "undefined: Categories".

- [ ] **Step 3: Implement the registry**

Create `internal/smartgroup/library.go`:

```go
// Copyright 2026, Jamf Software LLC

package smartgroup

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// library is the in-memory registry of all curated templates.
// Concrete templates are registered via init() in their category files
// (encryption.go, updates.go, etc.).
var (
	libraryMu sync.RWMutex
	library   = make(map[string]Template)
)

// Register adds a template to the library. Panics on duplicate slug —
// duplicate slugs are a programming error, not a runtime condition.
func Register(t Template) {
	libraryMu.Lock()
	defer libraryMu.Unlock()
	if _, exists := library[t.Slug]; exists {
		panic(fmt.Sprintf("smartgroup: duplicate slug %q", t.Slug))
	}
	library[t.Slug] = t
}

// Unregister removes a template; used only in tests.
func Unregister(slug string) {
	libraryMu.Lock()
	defer libraryMu.Unlock()
	delete(library, slug)
}

// Lookup returns the template by slug. The second return value reports
// whether the slug exists in the library.
func Lookup(slug string) (Template, bool) {
	libraryMu.RLock()
	defer libraryMu.RUnlock()
	t, ok := library[slug]
	return t, ok
}

// All returns all templates ordered first by category, then by slug.
func All() []Template {
	libraryMu.RLock()
	defer libraryMu.RUnlock()
	out := make([]Template, 0, len(library))
	for _, t := range library {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Slug < out[j].Slug
	})
	return out
}

// ByCategory returns templates in one category, sorted by slug.
func ByCategory(category string) []Template {
	cat := strings.ToLower(category)
	out := make([]Template, 0)
	for _, t := range All() {
		if t.Category == cat {
			out = append(out, t)
		}
	}
	return out
}

// Categories returns the sorted, unique list of categories present in the library.
func Categories() []string {
	libraryMu.RLock()
	defer libraryMu.RUnlock()
	seen := make(map[string]struct{}, len(library))
	for _, t := range library {
		seen[t.Category] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// FuzzyMatch returns slugs that are similar to the input — used by the CLI
// to suggest corrections on unknown-slug errors. Returns at most 3 matches.
func FuzzyMatch(input string) []string {
	input = strings.ToLower(input)
	all := All()
	type scored struct {
		slug  string
		score int
	}
	cands := make([]scored, 0, len(all))
	for _, t := range all {
		score := simpleScore(strings.ToLower(t.Slug), input)
		if score > 0 {
			cands = append(cands, scored{t.Slug, score})
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	out := make([]string, 0, 3)
	for i := 0; i < len(cands) && i < 3; i++ {
		out = append(out, cands[i].slug)
	}
	return out
}

func simpleScore(a, b string) int {
	if strings.Contains(a, b) {
		return 100 - len(a)
	}
	common := 0
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] == b[i] {
			common++
		}
	}
	return common
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/smartgroup/ -run "TestLibrary|TestRegister|TestCategories" -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/smartgroup/library.go internal/smartgroup/library_test.go
git commit -m "feat(smartgroup): add template Library registry with Register/Lookup/All/ByCategory/Categories/FuzzyMatch"
```

---

## Task 4: Encryption category (6 templates) with golden tests

**Files:**
- Create: `internal/smartgroup/encryption.go`
- Modify: `internal/smartgroup/library_test.go` (append 6 golden tests + 1 default-opts variant)

- [ ] **Step 1: Write the failing tests**

Append to `internal/smartgroup/library_test.go`:

```go

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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/smartgroup/ -run TestEncryption -v`
Expected: FAIL — every test reports "template encryption/X not registered".

- [ ] **Step 3: Implement the encryption category**

Create `internal/smartgroup/encryption.go`:

```go
// Copyright 2026, Jamf Software LLC

package smartgroup

import "fmt"

func init() {
	Register(encryptionNotEncrypted())
	Register(encryptionInvalidRecoveryKey())
	Register(encryptionEscrowMissing())
	Register(encryptionIRKOnlyDeprecated())
	Register(encryptionEncryptionStalled())
	Register(encryptionFVIneligible())
}

func encryptionNotEncrypted() Template {
	return Template{
		Slug:        "encryption/not-encrypted",
		Category:    "encryption",
		Description: "Macs where FileVault 2 is not enabled",
		Build: func(_ map[string]any) (SmartGroupRequest, error) {
			return SmartGroupRequest{
				Description: "Auto-generated by jamf-cli (template: encryption/not-encrypted)",
				Criteria: []Criterion{
					{AndOr: "and", Priority: 0, Name: CriterionFV2Status, SearchType: "is", Value: "Not Encrypted"},
				},
			}, nil
		},
	}
}

func encryptionInvalidRecoveryKey() Template {
	return Template{
		Slug:        "encryption/invalid-recovery-key",
		Category:    "encryption",
		Description: "Macs with an INVALID escrowed recovery key (cannot unlock)",
		Build: func(_ map[string]any) (SmartGroupRequest, error) {
			return SmartGroupRequest{
				Description: "Auto-generated by jamf-cli (template: encryption/invalid-recovery-key)",
				Criteria: []Criterion{
					{AndOr: "and", Priority: 0, Name: CriterionFV2IndividualKeyValidation, SearchType: "is", Value: "Not Valid"},
				},
			}, nil
		},
	}
}

func encryptionEscrowMissing() Template {
	return Template{
		Slug:        "encryption/escrow-missing",
		Category:    "encryption",
		Description: "Macs without any escrowed recovery key",
		Build: func(_ map[string]any) (SmartGroupRequest, error) {
			return SmartGroupRequest{
				Description: "Auto-generated by jamf-cli (template: encryption/escrow-missing)",
				Criteria: []Criterion{
					{AndOr: "and", Priority: 0, Name: CriterionFV2RecoveryKeyType, SearchType: "is", Value: ""},
				},
			}, nil
		},
	}
}

func encryptionIRKOnlyDeprecated() Template {
	return Template{
		Slug:        "encryption/irk-only-deprecated",
		Category:    "encryption",
		Description: "Macs on the deprecated Institutional Recovery Key",
		Build: func(_ map[string]any) (SmartGroupRequest, error) {
			return SmartGroupRequest{
				Description: "Auto-generated by jamf-cli (template: encryption/irk-only-deprecated). IRK is deprecated for managed Macs; migrate to Personal Recovery Key escrow.",
				Criteria: []Criterion{
					{AndOr: "and", Priority: 0, Name: CriterionFV2RecoveryKeyType, SearchType: "is", Value: "Institutional"},
				},
			}, nil
		},
	}
}

func encryptionEncryptionStalled() Template {
	return Template{
		Slug:        "encryption/encryption-stalled",
		Category:    "encryption",
		Description: "Macs stuck mid-encryption (no inventory update in N days)",
		Params: []ParamSpec{
			{Name: "stalled-after", Type: "int", Default: 7, Description: "Days since last inventory update"},
		},
		Build: func(opts map[string]any) (SmartGroupRequest, error) {
			days, ok := opts["stalled-after"].(int)
			if !ok {
				return SmartGroupRequest{}, fmt.Errorf("expected int stalled-after, got %T", opts["stalled-after"])
			}
			return SmartGroupRequest{
				Description: fmt.Sprintf("Auto-generated by jamf-cli (template: encryption/encryption-stalled, stalled-after=%d)", days),
				Criteria: []Criterion{
					{AndOr: "and", Priority: 0, Name: CriterionFV2Status, SearchType: "is not", Value: "All Partitions Encrypted"},
					{AndOr: "and", Priority: 1, Name: CriterionLastInventoryUpdate, SearchType: "more than x days ago", Value: fmt.Sprintf("%d", days)},
				},
			}, nil
		},
	}
}

func encryptionFVIneligible() Template {
	return Template{
		Slug:        "encryption/fv-ineligible",
		Category:    "encryption",
		Description: `Macs reporting FileVault 2 Status of "N/A" (ineligible hardware or never collected)`,
		Build: func(_ map[string]any) (SmartGroupRequest, error) {
			return SmartGroupRequest{
				Description: "Auto-generated by jamf-cli (template: encryption/fv-ineligible)",
				Criteria: []Criterion{
					{AndOr: "and", Priority: 0, Name: CriterionFV2Status, SearchType: "is", Value: "N/A"},
				},
			}, nil
		},
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/smartgroup/ -run TestEncryption -v`
Expected: PASS (7 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/smartgroup/encryption.go internal/smartgroup/library_test.go
git commit -m "feat(smartgroup): add 6 encryption templates"
```

---

## Task 5: Software-updates category (4 templates) with golden tests

**Files:**
- Create: `internal/smartgroup/updates.go`
- Modify: `internal/smartgroup/library_test.go` (append golden tests)

- [ ] **Step 1: Write the failing tests**

Append to `internal/smartgroup/library_test.go`:

```go

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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/smartgroup/ -run TestUpdates -v`
Expected: FAIL.

- [ ] **Step 3: Implement the updates category**

Create `internal/smartgroup/updates.go`:

```go
// Copyright 2026, Jamf Software LLC

package smartgroup

import "fmt"

func init() {
	Register(updatesOSVersionBelow())
	Register(updatesMajorVersionBehind())
	Register(updatesRSRNotApplied())
	Register(updatesBetaOS())
}

func updatesOSVersionBelow() Template {
	return Template{
		Slug:        "updates/os-version-below",
		Category:    "updates",
		Description: "Macs running OS older than a specific version",
		Params: []ParamSpec{
			{Name: "below-version", Type: "version", Description: "macOS version threshold (e.g. 15.4)", Required: true},
		},
		Build: func(opts map[string]any) (SmartGroupRequest, error) {
			v, ok := opts["below-version"].(string)
			if !ok {
				return SmartGroupRequest{}, fmt.Errorf("expected string below-version, got %T", opts["below-version"])
			}
			return SmartGroupRequest{
				Description: fmt.Sprintf("Auto-generated by jamf-cli (template: updates/os-version-below, below-version=%s)", v),
				Criteria: []Criterion{
					{AndOr: "and", Priority: 0, Name: CriterionOSVersion, SearchType: "less than", Value: v},
				},
			}, nil
		},
	}
}

func updatesMajorVersionBehind() Template {
	return Template{
		Slug:        "updates/major-version-behind",
		Category:    "updates",
		Description: "Macs behind a major macOS version (e.g. all running <15.x)",
		Params: []ParamSpec{
			{Name: "major-below", Type: "int", Description: "Major macOS version threshold", Required: true},
		},
		Build: func(opts map[string]any) (SmartGroupRequest, error) {
			n, ok := opts["major-below"].(int)
			if !ok {
				return SmartGroupRequest{}, fmt.Errorf("expected int major-below, got %T", opts["major-below"])
			}
			return SmartGroupRequest{
				Description: fmt.Sprintf("Auto-generated by jamf-cli (template: updates/major-version-behind, major-below=%d)", n),
				Criteria: []Criterion{
					{AndOr: "and", Priority: 0, Name: CriterionOSVersion, SearchType: "less than", Value: fmt.Sprintf("%d.0", n)},
				},
			}, nil
		},
	}
}

func updatesRSRNotApplied() Template {
	return Template{
		Slug:        "updates/rsr-not-applied",
		Category:    "updates",
		Description: "Macs with no Rapid Security Response applied",
		Build: func(_ map[string]any) (SmartGroupRequest, error) {
			return SmartGroupRequest{
				Description: "Auto-generated by jamf-cli (template: updates/rsr-not-applied)",
				Criteria: []Criterion{
					{AndOr: "and", Priority: 0, Name: CriterionOSRapidSecurityResponse, SearchType: "is", Value: ""},
				},
			}, nil
		},
	}
}

func updatesBetaOS() Template {
	return Template{
		Slug:        "updates/beta-os",
		Category:    "updates",
		Description: `Macs whose OS version contains "Beta"`,
		Build: func(_ map[string]any) (SmartGroupRequest, error) {
			return SmartGroupRequest{
				Description: "Auto-generated by jamf-cli (template: updates/beta-os)",
				Criteria: []Criterion{
					{AndOr: "and", Priority: 0, Name: CriterionOSVersion, SearchType: "like", Value: "Beta"},
				},
			}, nil
		},
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/smartgroup/ -run TestUpdates -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/smartgroup/updates.go internal/smartgroup/library_test.go
git commit -m "feat(smartgroup): add 4 software-update templates"
```

---

## Task 6: MDM-health category (5 templates) with golden tests

**Files:**
- Create: `internal/smartgroup/mdm.go`
- Modify: `internal/smartgroup/library_test.go` (append golden tests)

- [ ] **Step 1: Write the failing tests**

Append to `internal/smartgroup/library_test.go`:

```go

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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/smartgroup/ -run TestMDM -v`
Expected: FAIL.

- [ ] **Step 3: Implement the MDM-health category**

Create `internal/smartgroup/mdm.go`:

```go
// Copyright 2026, Jamf Software LLC

package smartgroup

import "fmt"

func init() {
	Register(mdmBootstrapTokenMissing())
	Register(mdmUserApprovedMDMNo())
	Register(mdmStaleCheckin())
	Register(mdmMDMCertExpiring())
	Register(mdmDDMDisabled())
}

func mdmBootstrapTokenMissing() Template {
	return Template{
		Slug:        "mdm/bootstrap-token-missing",
		Category:    "mdm",
		Description: "Macs without an escrowed bootstrap token",
		Build: func(_ map[string]any) (SmartGroupRequest, error) {
			return SmartGroupRequest{
				Description: "Auto-generated by jamf-cli (template: mdm/bootstrap-token-missing)",
				Criteria: []Criterion{
					{AndOr: "and", Priority: 0, Name: CriterionBootstrapTokenEscrowed, SearchType: "is", Value: "No"},
				},
			}, nil
		},
	}
}

func mdmUserApprovedMDMNo() Template {
	return Template{
		Slug:        "mdm/user-approved-mdm-no",
		Category:    "mdm",
		Description: "Macs without User Approved MDM",
		Build: func(_ map[string]any) (SmartGroupRequest, error) {
			return SmartGroupRequest{
				Description: "Auto-generated by jamf-cli (template: mdm/user-approved-mdm-no)",
				Criteria: []Criterion{
					{AndOr: "and", Priority: 0, Name: CriterionUserApprovedMDM, SearchType: "is", Value: "No"},
				},
			}, nil
		},
	}
}

func mdmStaleCheckin() Template {
	return Template{
		Slug:        "mdm/stale-checkin",
		Category:    "mdm",
		Description: "Macs whose last inventory update is older than N days",
		Params: []ParamSpec{
			{Name: "days", Type: "int", Default: 7, Description: "Days since last inventory update"},
		},
		Build: func(opts map[string]any) (SmartGroupRequest, error) {
			n, ok := opts["days"].(int)
			if !ok {
				return SmartGroupRequest{}, fmt.Errorf("expected int days, got %T", opts["days"])
			}
			return SmartGroupRequest{
				Description: fmt.Sprintf("Auto-generated by jamf-cli (template: mdm/stale-checkin, days=%d)", n),
				Criteria: []Criterion{
					{AndOr: "and", Priority: 0, Name: CriterionLastInventoryUpdate, SearchType: "more than x days ago", Value: fmt.Sprintf("%d", n)},
				},
			}, nil
		},
	}
}

func mdmMDMCertExpiring() Template {
	return Template{
		Slug:        "mdm/mdm-cert-expiring",
		Category:    "mdm",
		Description: "Macs whose MDM profile expires within N days",
		Params: []ParamSpec{
			{Name: "within-days", Type: "int", Default: 30, Description: "Days from now"},
		},
		Build: func(opts map[string]any) (SmartGroupRequest, error) {
			n, ok := opts["within-days"].(int)
			if !ok {
				return SmartGroupRequest{}, fmt.Errorf("expected int within-days, got %T", opts["within-days"])
			}
			return SmartGroupRequest{
				Description: fmt.Sprintf("Auto-generated by jamf-cli (template: mdm/mdm-cert-expiring, within-days=%d)", n),
				Criteria: []Criterion{
					{AndOr: "and", Priority: 0, Name: CriterionMDMProfileExpirationDate, SearchType: "less than x days from now", Value: fmt.Sprintf("%d", n)},
				},
			}, nil
		},
	}
}

func mdmDDMDisabled() Template {
	return Template{
		Slug:        "mdm/declarative-management-disabled",
		Category:    "mdm",
		Description: "Macs without Declarative Device Management enabled",
		Build: func(_ map[string]any) (SmartGroupRequest, error) {
			return SmartGroupRequest{
				Description: "Auto-generated by jamf-cli (template: mdm/declarative-management-disabled)",
				Criteria: []Criterion{
					{AndOr: "and", Priority: 0, Name: CriterionDDMEnabled, SearchType: "is", Value: "No"},
				},
			}, nil
		},
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/smartgroup/ -run TestMDM -v`
Expected: PASS (5 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/smartgroup/mdm.go internal/smartgroup/library_test.go
git commit -m "feat(smartgroup): add 5 MDM-health templates"
```

---

## Task 7: Compliance category (4 templates) with golden tests

**Files:**
- Create: `internal/smartgroup/compliance.go`
- Modify: `internal/smartgroup/library_test.go` (append golden tests)

- [ ] **Step 1: Write the failing tests**

Append to `internal/smartgroup/library_test.go`:

```go

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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/smartgroup/ -run TestCompliance -v`
Expected: FAIL.

- [ ] **Step 3: Implement the compliance category**

Create `internal/smartgroup/compliance.go`:

```go
// Copyright 2026, Jamf Software LLC

package smartgroup

func init() {
	Register(complianceGatekeeperDisabled())
	Register(complianceSIPDisabled())
	Register(complianceFirewallDisabled())
	Register(complianceNonCompliantBaseline())
}

func complianceGatekeeperDisabled() Template {
	return Template{
		Slug:        "compliance/gatekeeper-disabled",
		Category:    "compliance",
		Description: "Macs with Gatekeeper disabled",
		Build: func(_ map[string]any) (SmartGroupRequest, error) {
			return SmartGroupRequest{
				Description: "Auto-generated by jamf-cli (template: compliance/gatekeeper-disabled)",
				Criteria: []Criterion{
					{AndOr: "and", Priority: 0, Name: CriterionGatekeeper, SearchType: "is", Value: "Disabled"},
				},
			}, nil
		},
	}
}

func complianceSIPDisabled() Template {
	return Template{
		Slug:        "compliance/sip-disabled",
		Category:    "compliance",
		Description: "Macs with System Integrity Protection disabled",
		Build: func(_ map[string]any) (SmartGroupRequest, error) {
			return SmartGroupRequest{
				Description: "Auto-generated by jamf-cli (template: compliance/sip-disabled)",
				Criteria: []Criterion{
					{AndOr: "and", Priority: 0, Name: CriterionSIP, SearchType: "is", Value: "Disabled"},
				},
			}, nil
		},
	}
}

func complianceFirewallDisabled() Template {
	return Template{
		Slug:        "compliance/firewall-disabled",
		Category:    "compliance",
		Description: "Macs with the application firewall disabled",
		Build: func(_ map[string]any) (SmartGroupRequest, error) {
			return SmartGroupRequest{
				Description: "Auto-generated by jamf-cli (template: compliance/firewall-disabled)",
				Criteria: []Criterion{
					{AndOr: "and", Priority: 0, Name: CriterionFirewallEnabled, SearchType: "is", Value: "No"},
				},
			}, nil
		},
	}
}

func complianceNonCompliantBaseline() Template {
	return Template{
		Slug:        "compliance/non-compliant-baseline",
		Category:    "compliance",
		Description: "Composite: any of FV2 disabled, SIP disabled, Gatekeeper disabled, Firewall disabled",
		Build: func(_ map[string]any) (SmartGroupRequest, error) {
			return SmartGroupRequest{
				Description: "Auto-generated by jamf-cli (template: compliance/non-compliant-baseline). OR-composite across four security primitives.",
				Criteria: []Criterion{
					{AndOr: "and", Priority: 0, Name: CriterionFV2Enabled, SearchType: "is", Value: "Not Enabled"},
					{AndOr: "or", Priority: 1, Name: CriterionSIP, SearchType: "is", Value: "Disabled"},
					{AndOr: "or", Priority: 2, Name: CriterionGatekeeper, SearchType: "is", Value: "Disabled"},
					{AndOr: "or", Priority: 3, Name: CriterionFirewallEnabled, SearchType: "is", Value: "No"},
				},
			}, nil
		},
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/smartgroup/ -run TestCompliance -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/smartgroup/compliance.go internal/smartgroup/library_test.go
git commit -m "feat(smartgroup): add 4 compliance-basics templates"
```

---

## Task 8: Lifecycle category (4 templates) with golden tests

**Files:**
- Create: `internal/smartgroup/lifecycle.go`
- Modify: `internal/smartgroup/library_test.go` (append golden tests)

- [ ] **Step 1: Write the failing tests**

Append to `internal/smartgroup/library_test.go`:

```go

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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/smartgroup/ -run TestLifecycle -v`
Expected: FAIL.

- [ ] **Step 3: Implement the lifecycle category**

Create `internal/smartgroup/lifecycle.go`:

```go
// Copyright 2026, Jamf Software LLC

package smartgroup

import "fmt"

func init() {
	Register(lifecycleUnsupervised())
	Register(lifecycleADEEnrolled())
	Register(lifecycleJamfBinaryOutdated())
	Register(lifecycleFVIneligibleHardware())
}

func lifecycleUnsupervised() Template {
	return Template{
		Slug:        "lifecycle/unsupervised",
		Category:    "lifecycle",
		Description: "Macs that are not supervised",
		Build: func(_ map[string]any) (SmartGroupRequest, error) {
			return SmartGroupRequest{
				Description: "Auto-generated by jamf-cli (template: lifecycle/unsupervised)",
				Criteria: []Criterion{
					{AndOr: "and", Priority: 0, Name: CriterionSupervised, SearchType: "is", Value: "No"},
				},
			}, nil
		},
	}
}

func lifecycleADEEnrolled() Template {
	return Template{
		Slug:        "lifecycle/ade-enrolled",
		Category:    "lifecycle",
		Description: "Macs enrolled via Automated Device Enrollment (PreStage)",
		Build: func(_ map[string]any) (SmartGroupRequest, error) {
			return SmartGroupRequest{
				Description: "Auto-generated by jamf-cli (template: lifecycle/ade-enrolled)",
				Criteria: []Criterion{
					{AndOr: "and", Priority: 0, Name: CriterionEnrollmentMethodPrestage, SearchType: "is", Value: "Yes"},
				},
			}, nil
		},
	}
}

func lifecycleJamfBinaryOutdated() Template {
	return Template{
		Slug:        "lifecycle/jamf-binary-outdated",
		Category:    "lifecycle",
		Description: "Macs running an outdated Jamf binary",
		Params: []ParamSpec{
			{Name: "below-version", Type: "version", Description: "Jamf binary version threshold (e.g. 11.0.0)", Required: true},
		},
		Build: func(opts map[string]any) (SmartGroupRequest, error) {
			v, ok := opts["below-version"].(string)
			if !ok {
				return SmartGroupRequest{}, fmt.Errorf("expected string below-version, got %T", opts["below-version"])
			}
			return SmartGroupRequest{
				Description: fmt.Sprintf("Auto-generated by jamf-cli (template: lifecycle/jamf-binary-outdated, below-version=%s)", v),
				Criteria: []Criterion{
					{AndOr: "and", Priority: 0, Name: CriterionJamfBinaryVersion, SearchType: "less than", Value: v},
				},
			}, nil
		},
	}
}

func lifecycleFVIneligibleHardware() Template {
	return Template{
		Slug:        "lifecycle/fv-ineligible-hardware",
		Category:    "lifecycle",
		Description: "Intel Macs reporting FileVault 2 Status N/A (hardware-refresh candidates)",
		Build: func(_ map[string]any) (SmartGroupRequest, error) {
			return SmartGroupRequest{
				Description: "Auto-generated by jamf-cli (template: lifecycle/fv-ineligible-hardware)",
				Criteria: []Criterion{
					{AndOr: "and", Priority: 0, Name: CriterionFV2Status, SearchType: "is", Value: "N/A"},
					{AndOr: "and", Priority: 1, Name: CriterionAppleSilicon, SearchType: "is", Value: "No"},
				},
			}, nil
		},
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/smartgroup/ -run TestLifecycle -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/smartgroup/lifecycle.go internal/smartgroup/library_test.go
git commit -m "feat(smartgroup): add 4 lifecycle-hygiene templates"
```

---

## Task 9: Whole-library integration tests

**Files:**
- Modify: `internal/smartgroup/library_test.go` (add 4 integration tests + import "strings")

- [ ] **Step 1: Write the failing tests**

Add `"strings"` to the imports at the top of `library_test.go` if not already present.

Append to `library_test.go`:

```go

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
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test ./internal/smartgroup/ -run "TestLibrary_" -v`
Expected: PASS (4 tests).

- [ ] **Step 3: Run the whole package test suite**

Run: `go test ./internal/smartgroup/ -v`
Expected: PASS — every test across types, criteria, library, and 5 categories.

- [ ] **Step 4: Commit**

```bash
git add internal/smartgroup/library_test.go
git commit -m "test(smartgroup): add full-library integration tests (23 templates, 5 categories, criterion-name registry)"
```

---

## Task 10: Membership-check helper

**Files:**
- Create: `internal/smartgroup/membership.go`
- Create: `internal/smartgroup/membership_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/smartgroup/membership_test.go`:

```go
// Copyright 2026, Jamf Software LLC

package smartgroup

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type fakeHTTPClient struct {
	resp *http.Response
	err  error
	url  string
}

func (f *fakeHTTPClient) Do(_ context.Context, _ string, url string, _ io.Reader) (*http.Response, error) {
	f.url = url
	return f.resp, f.err
}

func makeJSON(t *testing.T, v any) *http.Response {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(string(b))),
	}
}

func TestCountMembers_PopulatedGroup(t *testing.T) {
	resp := makeJSON(t, map[string]any{"members": []int{1, 2, 3, 4, 5}})
	client := &fakeHTTPClient{resp: resp}
	n, err := CountMembers(context.Background(), client, "287")
	if err != nil {
		t.Fatalf("CountMembers: %v", err)
	}
	if n != 5 {
		t.Fatalf("expected 5, got %d", n)
	}
	wantPath := "/v2/computer-groups/smart-group-membership/287"
	if !strings.Contains(client.url, wantPath) {
		t.Fatalf("expected URL to contain %q, got %q", wantPath, client.url)
	}
}

func TestCountMembers_EmptyGroup(t *testing.T) {
	resp := makeJSON(t, map[string]any{"members": []int{}})
	n, err := CountMembers(context.Background(), &fakeHTTPClient{resp: resp}, "1")
	if err != nil {
		t.Fatalf("CountMembers: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}

func TestCountMembers_HTTPError(t *testing.T) {
	resp := &http.Response{
		StatusCode: 404,
		Body:       io.NopCloser(strings.NewReader(`{"errors":["not found"]}`)),
	}
	_, err := CountMembers(context.Background(), &fakeHTTPClient{resp: resp}, "999")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/smartgroup/ -run TestCountMembers -v`
Expected: FAIL with "undefined: CountMembers".

- [ ] **Step 3: Implement the helper**

Create `internal/smartgroup/membership.go`:

```go
// Copyright 2026, Jamf Software LLC

package smartgroup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// HTTPDoer is the minimal HTTP interface required by membership/verify helpers.
// Matches registry.HTTPClient's Do signature so the same value can be passed.
type HTTPDoer interface {
	Do(ctx context.Context, method, url string, body io.Reader) (*http.Response, error)
}

// CountMembers calls GET /v2/computer-groups/smart-group-membership/{id} and
// returns the length of the members array.
func CountMembers(ctx context.Context, client HTTPDoer, groupID string) (int, error) {
	url := fmt.Sprintf("/v2/computer-groups/smart-group-membership/%s", groupID)
	resp, err := client.Do(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("smart-group membership: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("smart-group membership: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		Members []int `json:"members"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, fmt.Errorf("smart-group membership: decode: %w", err)
	}
	return len(out.Members), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/smartgroup/ -run TestCountMembers -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/smartgroup/membership.go internal/smartgroup/membership_test.go
git commit -m "feat(smartgroup): add CountMembers helper for post-apply membership check"
```

---

## Task 11: Command namespace skeleton + wire-up

**Files:**
- Create: `internal/commands/pro_smartgroup.go`
- Modify: `internal/commands/pro.go` (add 1 line)
- Modify: `internal/commands/groups.go` (add 1 entry to proGroupMap)
- Modify: `internal/commands/aliases.go` (add 1 entry to commandAliases)

- [ ] **Step 1: Write the skeleton**

Create `internal/commands/pro_smartgroup.go`:

```go
// Copyright 2026, Jamf Software LLC

package commands

import (
	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// newSmartGroupCmd is the entry point for the `pro smart-group` namespace.
// Subcommands are wired in subsequent tasks (templates, preview, apply,
// verify-templates).
func newSmartGroupCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "smart-group",
		Short: "Curated smart-group templates: list, preview, apply, verify",
		Long: `Create useful Jamf Pro smart groups from a curated library of templates.

Templates encode operationally-essential smart groups (devices not encrypted,
recovery keys invalid, OS versions behind, bootstrap tokens missing, etc.) so
admins don't have to assemble them by hand.

Templates are sourced from JSS canonical criterion-name strings. Run
'pro smart-group verify-templates' once against your tenant to confirm each
template matches as expected.`,
	}

	cmd.AddCommand(newSmartGroupTemplatesCmd(cliCtx))
	cmd.AddCommand(newSmartGroupPreviewCmd(cliCtx))
	cmd.AddCommand(newSmartGroupApplyCmd(cliCtx))
	cmd.AddCommand(newSmartGroupVerifyTemplatesCmd(cliCtx))

	return cmd
}

// Stubs so the skeleton compiles. Replaced in Tasks 12-15.

func newSmartGroupTemplatesCmd(_ *registry.CLIContext) *cobra.Command {
	return &cobra.Command{Use: "templates", Short: "List available smart-group templates (stub)"}
}

func newSmartGroupPreviewCmd(_ *registry.CLIContext) *cobra.Command {
	return &cobra.Command{Use: "preview", Short: "Preview a template (stub)"}
}

func newSmartGroupApplyCmd(_ *registry.CLIContext) *cobra.Command {
	return &cobra.Command{Use: "apply", Short: "Apply a template (stub)"}
}

func newSmartGroupVerifyTemplatesCmd(_ *registry.CLIContext) *cobra.Command {
	return &cobra.Command{Use: "verify-templates", Short: "Verify templates against the live tenant (stub)"}
}
```

- [ ] **Step 2: Wire into pro.go**

Edit `internal/commands/pro.go`. Find the line:

```go
	cmd.AddCommand(newDeviceCmd(cliCtx))
```

Insert immediately after it:

```go
	cmd.AddCommand(newSmartGroupCmd(cliCtx))
```

- [ ] **Step 3: Wire into groups.go**

Edit `internal/commands/groups.go`. Find the existing line in `proGroupMap`:

```go
		"smart-computer-groups":                  groupComputers,
```

Add this line immediately after it:

```go
		"smart-group":                            groupComputers,
```

- [ ] **Step 4: Wire into aliases.go**

Edit `internal/commands/aliases.go`. In `commandAliases`, append:

```go
	"smart-group": {"sg"},
```

- [ ] **Step 5: Build and smoke-check**

Run: `go build ./...`
Expected: builds clean.

Run: `go run ./cmd/jamf-cli pro smart-group --help`
Expected: prints "Curated smart-group templates: list, preview, apply, verify" plus four stub subcommands.

Run: `go run ./cmd/jamf-cli pro sg --help`
Expected: same output (alias works).

- [ ] **Step 6: Commit**

```bash
git add internal/commands/pro_smartgroup.go internal/commands/pro.go internal/commands/groups.go internal/commands/aliases.go
git commit -m "feat(commands): scaffold pro smart-group namespace (alias sg) with stub subcommands"
```

---

## Task 12: `templates` subcommand

**Files:**
- Modify: `internal/commands/pro_smartgroup.go` (replace `templates` stub)
- Create: `internal/commands/pro_smartgroup_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/commands/pro_smartgroup_test.go`:

```go
// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func runSmartGroupCmd(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cliCtx := &registry.CLIContext{}
	root := newSmartGroupCmd(cliCtx)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestTemplates_TableDefault(t *testing.T) {
	out, _, err := runSmartGroupCmd(t, "templates")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, want := range []string{
		"encryption/not-encrypted",
		"updates/os-version-below",
		"mdm/bootstrap-token-missing",
		"compliance/gatekeeper-disabled",
		"lifecycle/unsupervised",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
}

func TestTemplates_CategoryFilter(t *testing.T) {
	out, _, err := runSmartGroupCmd(t, "templates", "--category", "encryption")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "encryption/not-encrypted") {
		t.Errorf("expected encryption templates: %s", out)
	}
	if strings.Contains(out, "lifecycle/unsupervised") {
		t.Errorf("category filter should have excluded lifecycle: %s", out)
	}
}

func TestTemplates_JSONOutput(t *testing.T) {
	out, _, err := runSmartGroupCmd(t, "templates", "-o", "json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var parsed []map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("json output not parseable: %v\n%s", err, out)
	}
	if len(parsed) != 23 {
		t.Errorf("expected 23 templates in json, got %d", len(parsed))
	}
}

func TestTemplates_UnknownCategory(t *testing.T) {
	out, _, err := runSmartGroupCmd(t, "templates", "--category", "nonexistent")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "0 templates") && !strings.Contains(out, "No templates") {
		t.Errorf("expected empty-result message, got: %s", out)
	}
}

// Suppress unused-import warnings for context/http/io used by later tasks.
var _ = context.Background
var _ = http.MethodGet
var _ io.Reader = nil
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/commands/ -run TestTemplates -v`
Expected: FAIL — the current stub does nothing useful.

- [ ] **Step 3: Implement the `templates` subcommand**

Replace `newSmartGroupTemplatesCmd` in `internal/commands/pro_smartgroup.go` and add the helper functions. Replace the entire file with:

```go
// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/smartgroup"
)

func newSmartGroupCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "smart-group",
		Short: "Curated smart-group templates: list, preview, apply, verify",
		Long: `Create useful Jamf Pro smart groups from a curated library of templates.

Templates encode operationally-essential smart groups (devices not encrypted,
recovery keys invalid, OS versions behind, bootstrap tokens missing, etc.) so
admins don't have to assemble them by hand.

Templates are sourced from JSS canonical criterion-name strings. Run
'pro smart-group verify-templates' once against your tenant to confirm each
template matches as expected.`,
	}

	cmd.AddCommand(newSmartGroupTemplatesCmd(cliCtx))
	cmd.AddCommand(newSmartGroupPreviewCmd(cliCtx))
	cmd.AddCommand(newSmartGroupApplyCmd(cliCtx))
	cmd.AddCommand(newSmartGroupVerifyTemplatesCmd(cliCtx))

	return cmd
}

func newSmartGroupTemplatesCmd(_ *registry.CLIContext) *cobra.Command {
	var (
		category string
		format   string
	)
	cmd := &cobra.Command{
		Use:   "templates",
		Short: "List available smart-group templates",
		Long: `List all curated smart-group templates. Use --category to filter
to one of: encryption, updates, mdm, compliance, lifecycle.`,
		Example: `  # All templates grouped by category
  jamf-cli pro smart-group templates

  # Just encryption templates
  jamf-cli pro smart-group templates --category encryption

  # Machine-readable
  jamf-cli pro smart-group templates -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var tmpls []smartgroup.Template
			if category != "" {
				tmpls = smartgroup.ByCategory(category)
			} else {
				tmpls = smartgroup.All()
			}
			return renderTemplatesList(cmd, tmpls, format)
		},
	}
	cmd.Flags().StringVar(&category, "category", "", "Filter by category (encryption|updates|mdm|compliance|lifecycle)")
	cmd.Flags().StringVarP(&format, "output", "o", "table", "Output format: table|json")
	return cmd
}

func renderTemplatesList(cmd *cobra.Command, tmpls []smartgroup.Template, format string) error {
	out := cmd.OutOrStdout()
	if format == "json" {
		return writeTemplatesJSON(out, tmpls)
	}
	if len(tmpls) == 0 {
		fmt.Fprintln(out, "0 templates match the filter.")
		return nil
	}
	cats := uniqueCategories(tmpls)
	noun := "category"
	if len(cats) != 1 {
		noun = "categories"
	}
	fmt.Fprintf(out, "Smart Group Templates — %d available across %d %s\n\n", len(tmpls), len(cats), noun)
	for _, cat := range cats {
		bucket := filterByCategory(tmpls, cat)
		fmt.Fprintf(out, "Category: %s (%d)\n", cat, len(bucket))
		for _, t := range bucket {
			suffix := ""
			if len(t.Params) > 0 {
				suffix = fmt.Sprintf(" (params: --%s)", t.Params[0].Name)
			}
			fmt.Fprintf(out, "  %-40s %s%s\n", t.Slug, t.Description, suffix)
		}
		fmt.Fprintln(out)
	}
	return nil
}

func writeTemplatesJSON(out io.Writer, tmpls []smartgroup.Template) error {
	type paramOut struct {
		Name        string `json:"name"`
		Type        string `json:"type"`
		Default     any    `json:"default,omitempty"`
		Description string `json:"description"`
		Required    bool   `json:"required"`
	}
	type tmplOut struct {
		Slug        string     `json:"slug"`
		Category    string     `json:"category"`
		Description string     `json:"description"`
		Params      []paramOut `json:"params"`
	}
	rows := make([]tmplOut, 0, len(tmpls))
	for _, t := range tmpls {
		row := tmplOut{Slug: t.Slug, Category: t.Category, Description: t.Description, Params: []paramOut{}}
		for _, p := range t.Params {
			row.Params = append(row.Params, paramOut{
				Name: p.Name, Type: p.Type, Default: p.Default,
				Description: p.Description, Required: p.Required,
			})
		}
		rows = append(rows, row)
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

func uniqueCategories(tmpls []smartgroup.Template) []string {
	seen := make(map[string]struct{}, 5)
	out := make([]string, 0, 5)
	for _, t := range tmpls {
		if _, ok := seen[t.Category]; ok {
			continue
		}
		seen[t.Category] = struct{}{}
		out = append(out, t.Category)
	}
	sort.Strings(out)
	return out
}

func filterByCategory(tmpls []smartgroup.Template, cat string) []smartgroup.Template {
	out := make([]smartgroup.Template, 0)
	for _, t := range tmpls {
		if t.Category == cat {
			out = append(out, t)
		}
	}
	return out
}

// Stubs for the remaining subcommands. Replaced in Tasks 13-15.

func newSmartGroupPreviewCmd(_ *registry.CLIContext) *cobra.Command {
	return &cobra.Command{Use: "preview", Short: "Preview a template (stub)"}
}

func newSmartGroupApplyCmd(_ *registry.CLIContext) *cobra.Command {
	return &cobra.Command{Use: "apply", Short: "Apply a template (stub)"}
}

func newSmartGroupVerifyTemplatesCmd(_ *registry.CLIContext) *cobra.Command {
	return &cobra.Command{Use: "verify-templates", Short: "Verify templates against the live tenant (stub)"}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/commands/ -run TestTemplates -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/commands/pro_smartgroup.go internal/commands/pro_smartgroup_test.go
git commit -m "feat(commands): implement pro smart-group templates (list with --category, table/json output)"
```

---

## Task 13: `preview` subcommand

**Files:**
- Modify: `internal/commands/pro_smartgroup.go` (replace `preview` stub + helper funcs)
- Modify: `internal/commands/pro_smartgroup_test.go` (append preview tests)

- [ ] **Step 1: Write the failing tests**

Append to `internal/commands/pro_smartgroup_test.go`:

```go

func TestPreview_ZeroParam(t *testing.T) {
	out, _, err := runSmartGroupCmd(t, "preview", "--template", "encryption/not-encrypted")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "POST /v2/computer-groups/smart-groups") {
		t.Errorf("expected POST header: %s", out)
	}
	if !strings.Contains(out, "FileVault 2 Status") || !strings.Contains(out, "Not Encrypted") {
		t.Errorf("expected criterion in JSON body: %s", out)
	}
}

func TestPreview_WithParam(t *testing.T) {
	out, _, err := runSmartGroupCmd(t, "preview", "--template", "encryption/encryption-stalled", "--stalled-after", "14")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, `"value": "14"`) {
		t.Errorf("expected stalled-after=14 in output: %s", out)
	}
}

func TestPreview_UnknownTemplate(t *testing.T) {
	_, _, err := runSmartGroupCmd(t, "preview", "--template", "encryption/typo")
	if err == nil {
		t.Fatal("expected error for unknown template, got nil")
	}
	if !strings.Contains(err.Error(), "encryption/") {
		t.Errorf("expected fuzzy-match suggestion mentioning encryption: %v", err)
	}
}

func TestPreview_RequiredParamMissing(t *testing.T) {
	_, _, err := runSmartGroupCmd(t, "preview", "--template", "updates/os-version-below")
	if err == nil {
		t.Fatal("expected error for missing --below-version, got nil")
	}
	if !strings.Contains(err.Error(), "below-version") {
		t.Errorf("expected error to mention required param: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/commands/ -run TestPreview -v`
Expected: FAIL — preview stub does nothing useful.

- [ ] **Step 3: Implement `preview` and supporting helpers**

In `internal/commands/pro_smartgroup.go`, add `"strings"` to the imports (in addition to those already present). Replace the `newSmartGroupPreviewCmd` stub with the real implementation, and add the supporting helpers:

```go
func newSmartGroupPreviewCmd(_ *registry.CLIContext) *cobra.Command {
	var slug string
	cmd := &cobra.Command{
		Use:   "preview",
		Short: "Print the JSON body that would be POSTed (no API call)",
		Long: `Preview the JSON request that 'apply' would POST to
/v2/computer-groups/smart-groups for the chosen template. Use this to inspect
criteria before creating a group.`,
		Example: `  jamf-cli pro smart-group preview --template encryption/invalid-recovery-key
  jamf-cli pro smart-group preview --template encryption/encryption-stalled --stalled-after 14`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			tmpl, ok := smartgroup.Lookup(slug)
			if !ok {
				return unknownTemplateError(slug)
			}
			opts, err := collectParamValues(tmpl, cmd.Flags())
			if err != nil {
				return err
			}
			resolved, err := tmpl.ResolveOpts(opts)
			if err != nil {
				return err
			}
			req, err := tmpl.Build(resolved)
			if err != nil {
				return err
			}
			req.Name = "<--name required when running apply>"
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "POST /v2/computer-groups/smart-groups")
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			return enc.Encode(req)
		},
	}
	cmd.Flags().StringVar(&slug, "template", "", "Template slug (required) — e.g. encryption/invalid-recovery-key")
	_ = cmd.MarkFlagRequired("template")
	registerTemplateParamFlags(cmd)
	return cmd
}

// registerTemplateParamFlags declares the union of all per-template param
// flag names on the cobra command as generic string flags. collectParamValues
// reads only the flags the chosen template actually declares.
func registerTemplateParamFlags(cmd *cobra.Command) {
	seen := make(map[string]bool)
	for _, t := range smartgroup.All() {
		for _, p := range t.Params {
			if seen[p.Name] {
				continue
			}
			seen[p.Name] = true
			cmd.Flags().String(p.Name, "", p.Description)
		}
	}
}

// flagReader is the minimal flag-access interface used by collectParamValues
// — satisfied by *pflag.FlagSet returned by cobra's cmd.Flags().
type flagReader interface {
	GetString(string) (string, error)
	Changed(string) bool
}

func collectParamValues(tmpl smartgroup.Template, flags flagReader) (map[string]any, error) {
	out := make(map[string]any, len(tmpl.Params))
	for _, p := range tmpl.Params {
		if !flags.Changed(p.Name) {
			continue
		}
		v, err := flags.GetString(p.Name)
		if err != nil {
			return nil, err
		}
		out[p.Name] = v // ResolveOpts coerces strings to int when Type is "int".
	}
	return out, nil
}

func unknownTemplateError(slug string) error {
	suggestions := smartgroup.FuzzyMatch(slug)
	if len(suggestions) == 0 {
		return fmt.Errorf("unknown template %q (run 'pro smart-group templates' to list available)", slug)
	}
	return fmt.Errorf("unknown template %q — did you mean: %s?", slug, strings.Join(suggestions, ", "))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/commands/ -run TestPreview -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/commands/pro_smartgroup.go internal/commands/pro_smartgroup_test.go
git commit -m "feat(commands): implement pro smart-group preview with per-template param flags"
```

---

## Task 14: `apply` subcommand (idempotent create/update + membership check)

**Files:**
- Modify: `internal/commands/pro_smartgroup.go` (replace `apply` stub + add helpers)
- Modify: `internal/commands/pro_smartgroup_test.go` (append apply tests)

- [ ] **Step 1: Write the failing tests**

Append to `internal/commands/pro_smartgroup_test.go`:

```go

type fakeSGClient struct {
	calls []recordedCall
	queue []*http.Response
}

type recordedCall struct {
	method, url, body string
}

func (f *fakeSGClient) Do(_ context.Context, method, url string, body io.Reader) (*http.Response, error) {
	b := ""
	if body != nil {
		buf, _ := io.ReadAll(body)
		b = string(buf)
	}
	f.calls = append(f.calls, recordedCall{method, url, b})
	if len(f.queue) == 0 {
		return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("queue empty"))}, nil
	}
	resp := f.queue[0]
	f.queue = f.queue[1:]
	return resp, nil
}

func newJSONResp(status int, payload any) *http.Response {
	b, _ := json.Marshal(payload)
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(b)))}
}

func runSmartGroupApply(t *testing.T, client *fakeSGClient, args ...string) (string, error) {
	t.Helper()
	cliCtx := &registry.CLIContext{Client: client}
	root := newSmartGroupCmd(cliCtx)
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"apply"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestApply_NewGroupCreated(t *testing.T) {
	client := &fakeSGClient{
		queue: []*http.Response{
			newJSONResp(200, map[string]any{"totalCount": 0, "results": []any{}}),
			newJSONResp(201, map[string]any{"id": "287", "href": "/.../287"}),
			newJSONResp(200, map[string]any{"members": []int{1, 2, 3, 4, 5}}),
		},
	}
	out, err := runSmartGroupApply(t, client,
		"--template", "encryption/not-encrypted",
		"--name", "Test FV Not Encrypted",
		"--yes",
	)
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	if len(client.calls) != 3 {
		t.Fatalf("expected 3 API calls, got %d", len(client.calls))
	}
	if client.calls[1].method != "POST" {
		t.Errorf("expected second call POST (create), got %s", client.calls[1].method)
	}
	if !strings.Contains(out, "Membership: 5") {
		t.Errorf("expected membership log in output: %s", out)
	}
}

func TestApply_ExistingGroupUpdated(t *testing.T) {
	client := &fakeSGClient{
		queue: []*http.Response{
			newJSONResp(200, map[string]any{"totalCount": 1, "results": []any{map[string]any{"id": "42", "name": "Test FV Not Encrypted"}}}),
			newJSONResp(204, map[string]any{}),
			newJSONResp(200, map[string]any{"members": []int{1, 2}}),
		},
	}
	out, err := runSmartGroupApply(t, client,
		"--template", "encryption/not-encrypted",
		"--name", "Test FV Not Encrypted",
		"--yes",
	)
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	if client.calls[1].method != "PUT" {
		t.Errorf("expected PUT on existing group, got %s", client.calls[1].method)
	}
	if !strings.Contains(client.calls[1].url, "/42") {
		t.Errorf("expected PUT URL with id=42: %s", client.calls[1].url)
	}
}

func TestApply_DryRunNoAPICalls(t *testing.T) {
	client := &fakeSGClient{}
	out, err := runSmartGroupApply(t, client,
		"--template", "encryption/not-encrypted",
		"--name", "Test",
		"--dry-run",
	)
	if err != nil {
		t.Fatalf("dry-run: %v\n%s", err, out)
	}
	if len(client.calls) != 0 {
		t.Fatalf("expected 0 API calls in dry-run, got %d", len(client.calls))
	}
	if !strings.Contains(out, "POST /v2/computer-groups/smart-groups") {
		t.Errorf("expected dry-run output to show what would POST: %s", out)
	}
}

func TestApply_ZeroMembershipWarning(t *testing.T) {
	client := &fakeSGClient{
		queue: []*http.Response{
			newJSONResp(200, map[string]any{"totalCount": 0, "results": []any{}}),
			newJSONResp(201, map[string]any{"id": "99"}),
			newJSONResp(200, map[string]any{"members": []int{}}),
		},
	}
	out, _ := runSmartGroupApply(t, client,
		"--template", "compliance/firewall-disabled",
		"--name", "Test FW Off",
		"--yes",
	)
	if !strings.Contains(out, "matched 0 devices") {
		t.Errorf("expected zero-match warning: %s", out)
	}
}

func TestApply_403MissingPrivilege(t *testing.T) {
	client := &fakeSGClient{
		queue: []*http.Response{
			newJSONResp(200, map[string]any{"totalCount": 0, "results": []any{}}),
			newJSONResp(403, map[string]any{"errors": []string{"forbidden"}}),
		},
	}
	_, err := runSmartGroupApply(t, client,
		"--template", "encryption/not-encrypted",
		"--name", "Test",
		"--yes",
	)
	if err == nil {
		t.Fatal("expected error on 403, got nil")
	}
	if !strings.Contains(err.Error(), "Create Smart Computer Groups") {
		t.Errorf("expected privilege name in error: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/commands/ -run TestApply -v`
Expected: FAIL.

- [ ] **Step 3: Implement `apply` and the apply-flow helpers**

In `internal/commands/pro_smartgroup.go`, add these imports to the import block: `"bytes"`, `"context"`, `"net/http"`, `"net/url"`.

Replace the `newSmartGroupApplyCmd` stub with:

```go
func newSmartGroupApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		slug        string
		name        string
		recalculate bool
		dryRun      bool
		yes         bool
	)
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update a smart group from a template (idempotent by --name)",
		Long: `Apply a template against the live tenant. If a smart group with the
given --name already exists, it is updated (PUT); otherwise it is created
(POST). After apply, the membership endpoint is consulted and the count is
logged. Use --dry-run to inspect the request body without calling the API.`,
		Example: `  jamf-cli pro smart-group apply --template encryption/invalid-recovery-key --name "FV Invalid Recovery Keys"
  jamf-cli pro sg apply --template mdm/stale-checkin --name "Stale 30d" --days 30 --recalculate
  jamf-cli pro sg apply --template encryption/not-encrypted --name "Not Encrypted" --dry-run`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			tmpl, ok := smartgroup.Lookup(slug)
			if !ok {
				return unknownTemplateError(slug)
			}
			opts, err := collectParamValues(tmpl, cmd.Flags())
			if err != nil {
				return err
			}
			resolved, err := tmpl.ResolveOpts(opts)
			if err != nil {
				return err
			}
			req, err := tmpl.Build(resolved)
			if err != nil {
				return err
			}
			req.Name = name
			if dryRun {
				return printDryRun(cmd.OutOrStdout(), req)
			}
			if cliCtx.Client == nil {
				return fmt.Errorf("not authenticated to a Jamf Pro tenant; run 'jamf-cli pro setup' first")
			}
			return runApplyFlow(cmd.Context(), cmd.OutOrStdout(), cliCtx.Client, req, recalculate, yes)
		},
	}
	cmd.Flags().StringVar(&slug, "template", "", "Template slug (required)")
	cmd.Flags().StringVar(&name, "name", "", "Smart group name (required)")
	cmd.Flags().BoolVar(&recalculate, "recalculate", false, "After apply, force smart-group recalculation")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the request body without calling the API")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation when updating an existing group")
	_ = cmd.MarkFlagRequired("template")
	_ = cmd.MarkFlagRequired("name")
	registerTemplateParamFlags(cmd)
	return cmd
}

func printDryRun(out io.Writer, req smartgroup.SmartGroupRequest) error {
	fmt.Fprintln(out, "POST /v2/computer-groups/smart-groups")
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(req)
}

func runApplyFlow(ctx context.Context, out io.Writer, client registry.HTTPClient, req smartgroup.SmartGroupRequest, recalculate, yes bool) error {
	existingID, err := lookupSmartGroupByName(ctx, client, req.Name)
	if err != nil {
		return err
	}

	var id string
	switch {
	case existingID == "":
		newID, err := createSmartGroup(ctx, client, req)
		if err != nil {
			return err
		}
		id = newID
		fmt.Fprintf(out, "Created smart group %q (ID: %s)\n", req.Name, id)
	default:
		if !yes {
			return fmt.Errorf("smart group %q already exists (ID %s); pass --yes to replace", req.Name, existingID)
		}
		if err := updateSmartGroup(ctx, client, existingID, req); err != nil {
			return err
		}
		id = existingID
		fmt.Fprintf(out, "Updated smart group %q (ID: %s)\n", req.Name, id)
	}

	if recalculate {
		if err := recalculateSmartGroup(ctx, client, id); err != nil {
			fmt.Fprintf(out, "Warning: recalculate did not complete: %v\n", err)
		}
	}

	count, err := smartgroup.CountMembers(ctx, client, id)
	if err != nil {
		fmt.Fprintf(out, "Warning: membership check failed: %v\n", err)
		return nil
	}
	fmt.Fprintf(out, "Membership: %d devices.\n", count)
	if count == 0 {
		fmt.Fprintln(out, "This template matched 0 devices. Run 'pro sg verify-templates' to check criterion compatibility with your tenant.")
	}
	return nil
}

func lookupSmartGroupByName(ctx context.Context, client registry.HTTPClient, name string) (string, error) {
	filter := url.QueryEscape(fmt.Sprintf(`name=="%s"`, name))
	path := "/v2/computer-groups/smart-groups?filter=" + filter
	resp, err := client.Do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("lookup smart group: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		TotalCount int `json:"totalCount"`
		Results    []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	for _, r := range out.Results {
		if r.Name == name {
			return r.ID, nil
		}
	}
	return "", nil
}

func createSmartGroup(ctx context.Context, client registry.HTTPClient, req smartgroup.SmartGroupRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(ctx, http.MethodPost, "/v2/computer-groups/smart-groups", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 403 {
		return "", fmt.Errorf("permission denied: the OAuth role is missing the 'Create Smart Computer Groups' privilege")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create smart group: HTTP %d: %s", resp.StatusCode, string(buf))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func updateSmartGroup(ctx context.Context, client registry.HTTPClient, id string, req smartgroup.SmartGroupRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	resp, err := client.Do(ctx, http.MethodPut, "/v2/computer-groups/smart-groups/"+id, bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 403 {
		return fmt.Errorf("permission denied: the OAuth role is missing the 'Update Smart Computer Groups' privilege")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update smart group: HTTP %d: %s", resp.StatusCode, string(buf))
	}
	return nil
}

func recalculateSmartGroup(ctx context.Context, client registry.HTTPClient, id string) error {
	resp, err := client.Do(ctx, http.MethodPost, "/v1/smart-computer-groups/"+id+"/recalculate", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("recalculate: HTTP %d", resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/commands/ -run TestApply -v`
Expected: PASS (5 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/commands/pro_smartgroup.go internal/commands/pro_smartgroup_test.go
git commit -m "feat(commands): implement pro smart-group apply with idempotent name-based create/update and post-apply membership check"
```

---

## Task 15: `verify-templates` subcommand (live-tenant smoke test)

**Files:**
- Create: `internal/smartgroup/verify.go`
- Create: `internal/smartgroup/verify_test.go`
- Modify: `internal/commands/pro_smartgroup.go` (replace `verify-templates` stub)
- Modify: `internal/commands/pro_smartgroup_test.go` (append minimal subcommand test)

- [ ] **Step 1: Write the failing test for the verifier package**

Create `internal/smartgroup/verify_test.go`:

```go
// Copyright 2026, Jamf Software LLC

package smartgroup

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type seqClient struct {
	queue []*http.Response
	calls []string
}

func (s *seqClient) Do(_ context.Context, method, url string, _ io.Reader) (*http.Response, error) {
	s.calls = append(s.calls, method+" "+url)
	if len(s.queue) == 0 {
		return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("empty"))}, nil
	}
	r := s.queue[0]
	s.queue = s.queue[1:]
	return r, nil
}

func jsonResp(status int, payload any) *http.Response {
	b, _ := json.Marshal(payload)
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(b)))}
}

func TestVerify_RunOneTemplate_OK(t *testing.T) {
	tmpl, ok := Lookup("encryption/not-encrypted")
	if !ok {
		t.Fatal("template missing")
	}
	client := &seqClient{
		queue: []*http.Response{
			jsonResp(201, map[string]any{"id": "555"}),
			jsonResp(200, map[string]any{}),
			jsonResp(200, map[string]any{"members": []int{1, 2, 3}}),
			jsonResp(204, map[string]any{}),
		},
	}
	result := RunOneVerification(context.Background(), client, tmpl, true)
	if result.Outcome != VerifyOK {
		t.Errorf("expected OK outcome, got %v (%s)", result.Outcome, result.Error)
	}
	if result.MemberCount != 3 {
		t.Errorf("expected 3 members, got %d", result.MemberCount)
	}
}

func TestVerify_RunOneTemplate_ZeroMatch(t *testing.T) {
	tmpl, _ := Lookup("compliance/firewall-disabled")
	client := &seqClient{
		queue: []*http.Response{
			jsonResp(201, map[string]any{"id": "777"}),
			jsonResp(200, map[string]any{}),
			jsonResp(200, map[string]any{"members": []int{}}),
			jsonResp(204, map[string]any{}),
		},
	}
	result := RunOneVerification(context.Background(), client, tmpl, true)
	if result.Outcome != VerifyZeroMatch {
		t.Errorf("expected ZeroMatch, got %v", result.Outcome)
	}
}

func TestVerify_RunOneTemplate_CreateError(t *testing.T) {
	tmpl, _ := Lookup("encryption/not-encrypted")
	client := &seqClient{
		queue: []*http.Response{
			jsonResp(400, map[string]any{"errors": []string{"invalid criterion name"}}),
		},
	}
	result := RunOneVerification(context.Background(), client, tmpl, true)
	if result.Outcome != VerifyError {
		t.Errorf("expected Error, got %v", result.Outcome)
	}
	if result.Error == "" {
		t.Error("expected non-empty Error message")
	}
}

func TestVerify_NoCleanupSkipsDelete(t *testing.T) {
	tmpl, _ := Lookup("encryption/not-encrypted")
	client := &seqClient{
		queue: []*http.Response{
			jsonResp(201, map[string]any{"id": "888"}),
			jsonResp(200, map[string]any{}),
			jsonResp(200, map[string]any{"members": []int{1}}),
		},
	}
	_ = RunOneVerification(context.Background(), client, tmpl, false)
	for _, c := range client.calls {
		if strings.HasPrefix(c, "DELETE") {
			t.Errorf("did not expect DELETE call with cleanup=false: %s", c)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/smartgroup/ -run TestVerify -v`
Expected: FAIL with "undefined: RunOneVerification".

- [ ] **Step 3: Implement the verifier**

Create `internal/smartgroup/verify.go`:

```go
// Copyright 2026, Jamf Software LLC

package smartgroup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
)

// VerifyOutcome is the per-template result of a verify-templates pass.
type VerifyOutcome int

const (
	VerifyOK VerifyOutcome = iota
	VerifyZeroMatch
	VerifyError
)

func (o VerifyOutcome) String() string {
	switch o {
	case VerifyOK:
		return "OK"
	case VerifyZeroMatch:
		return "ZERO_MATCH"
	case VerifyError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// VerifyResult captures one template's verification outcome.
type VerifyResult struct {
	Slug        string
	Outcome     VerifyOutcome
	MemberCount int
	Error       string
}

// defaultVerifyOpts supplies sensible required-param values for verification.
// Update when adding new required-param templates.
var defaultVerifyOpts = map[string]map[string]any{
	"updates/os-version-below":       {"below-version": "15.0"},
	"updates/major-version-behind":   {"major-below": 15},
	"lifecycle/jamf-binary-outdated": {"below-version": "11.0.0"},
}

// RunOneVerification creates a temporary smart group from the template,
// recalculates membership, captures the count, and (if cleanup) deletes the
// temporary group.
func RunOneVerification(ctx context.Context, client HTTPDoer, tmpl Template, cleanup bool) VerifyResult {
	opts, err := tmpl.ResolveOpts(defaultVerifyOpts[tmpl.Slug])
	if err != nil {
		return VerifyResult{Slug: tmpl.Slug, Outcome: VerifyError, Error: fmt.Sprintf("ResolveOpts: %v", err)}
	}
	req, err := tmpl.Build(opts)
	if err != nil {
		return VerifyResult{Slug: tmpl.Slug, Outcome: VerifyError, Error: fmt.Sprintf("Build: %v", err)}
	}
	req.Name = fmt.Sprintf("__verify_%s_%06d", sanitizeSlug(tmpl.Slug), rand.Intn(1000000))

	id, err := createTempGroup(ctx, client, req)
	if err != nil {
		return VerifyResult{Slug: tmpl.Slug, Outcome: VerifyError, Error: err.Error()}
	}

	_ = recalcGroup(ctx, client, id) // recalc failure is non-fatal

	count, err := CountMembers(ctx, client, id)
	if err != nil {
		if cleanup {
			_ = deleteGroup(ctx, client, id)
		}
		return VerifyResult{Slug: tmpl.Slug, Outcome: VerifyError, Error: err.Error()}
	}

	if cleanup {
		_ = deleteGroup(ctx, client, id)
	}

	outcome := VerifyOK
	if count == 0 {
		outcome = VerifyZeroMatch
	}
	return VerifyResult{Slug: tmpl.Slug, Outcome: outcome, MemberCount: count}
}

func sanitizeSlug(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
			out = append(out, c)
		case c >= '0' && c <= '9':
			out = append(out, c)
		case c == '-' || c == '_':
			out = append(out, c)
		case c == '/':
			out = append(out, '_')
		}
	}
	return string(out)
}

func createTempGroup(ctx context.Context, client HTTPDoer, req SmartGroupRequest) (string, error) {
	body, _ := json.Marshal(req)
	resp, err := client.Do(ctx, http.MethodPost, "/v2/computer-groups/smart-groups", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(buf))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func recalcGroup(ctx context.Context, client HTTPDoer, id string) error {
	resp, err := client.Do(ctx, http.MethodPost, "/v1/smart-computer-groups/"+id+"/recalculate", nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func deleteGroup(ctx context.Context, client HTTPDoer, id string) error {
	resp, err := client.Do(ctx, http.MethodDelete, "/v2/computer-groups/smart-groups/"+id, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
```

- [ ] **Step 4: Run package tests to confirm verifier passes**

Run: `go test ./internal/smartgroup/ -run TestVerify -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Replace the `verify-templates` subcommand stub**

In `internal/commands/pro_smartgroup.go`, replace `newSmartGroupVerifyTemplatesCmd`:

```go
func newSmartGroupVerifyTemplatesCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		category   string
		noCleanup  bool
		jsonOutput bool
	)
	cmd := &cobra.Command{
		Use:   "verify-templates",
		Short: "Smoke-test every template against the live tenant",
		Long: `Create one temporary smart group per template (prefixed "__verify_"),
recalculate it, read the membership count, and report. Temporary groups are
deleted on completion unless --no-cleanup is set.

Use this on first run after install (and after any sync-specs that touches
JSS) to confirm criterion-name strings match your Jamf Pro version.`,
		Example: `  jamf-cli pro smart-group verify-templates
  jamf-cli pro sg verify-templates --category encryption
  jamf-cli pro sg verify-templates --no-cleanup`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cliCtx.Client == nil {
				return fmt.Errorf("not authenticated to a Jamf Pro tenant; run 'jamf-cli pro setup' first")
			}
			var tmpls []smartgroup.Template
			if category != "" {
				tmpls = smartgroup.ByCategory(category)
			} else {
				tmpls = smartgroup.All()
			}
			results := make([]smartgroup.VerifyResult, 0, len(tmpls))
			for _, t := range tmpls {
				results = append(results, smartgroup.RunOneVerification(cmd.Context(), cliCtx.Client, t, !noCleanup))
			}
			return renderVerifyResults(cmd.OutOrStdout(), results, jsonOutput)
		},
	}
	cmd.Flags().StringVar(&category, "category", "", "Verify only one category")
	cmd.Flags().BoolVar(&noCleanup, "no-cleanup", false, "Keep temporary groups instead of deleting them")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output JSON instead of human-readable summary")
	return cmd
}

func renderVerifyResults(out io.Writer, results []smartgroup.VerifyResult, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}
	ok, zero, errs := 0, 0, 0
	fmt.Fprintf(out, "Verifying %d templates...\n\n", len(results))
	for _, r := range results {
		switch r.Outcome {
		case smartgroup.VerifyOK:
			ok++
			fmt.Fprintf(out, "✓ %-40s — %d devices match\n", r.Slug, r.MemberCount)
		case smartgroup.VerifyZeroMatch:
			zero++
			fmt.Fprintf(out, "⚠ %-40s — 0 devices match (possible criterion mismatch)\n", r.Slug)
		case smartgroup.VerifyError:
			errs++
			fmt.Fprintf(out, "✗ %-40s — ERROR: %s\n", r.Slug, r.Error)
		}
	}
	fmt.Fprintf(out, "\nSummary: %d OK, %d zero-match warnings, %d errors.\n", ok, zero, errs)
	return nil
}
```

- [ ] **Step 6: Add a minimal end-to-end test of the subcommand**

Append to `internal/commands/pro_smartgroup_test.go`:

```go

func TestVerifyTemplates_CategoryRuns(t *testing.T) {
	// Each template in the encryption category produces 4 HTTP calls
	// (POST create + recalc + membership + DELETE cleanup). We queue 6 templates * 4 = 24 responses.
	client := &fakeSGClient{}
	for i := 0; i < 6; i++ {
		client.queue = append(client.queue,
			newJSONResp(201, map[string]any{"id": "100"}),
			newJSONResp(200, map[string]any{}),
			newJSONResp(200, map[string]any{"members": []int{1, 2}}),
			newJSONResp(204, map[string]any{}),
		)
	}
	cliCtx := &registry.CLIContext{Client: client}
	root := newSmartGroupCmd(cliCtx)
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"verify-templates", "--category", "encryption"})
	if err := root.Execute(); err != nil {
		t.Fatalf("verify-templates: %v", err)
	}
	if !strings.Contains(out.String(), "Verifying 6 templates") {
		t.Errorf("expected '6 templates' in output: %s", out.String())
	}
	if !strings.Contains(out.String(), "Summary: 6 OK") {
		t.Errorf("expected summary line: %s", out.String())
	}
}
```

- [ ] **Step 7: Run all subcommand tests**

Run: `go test ./internal/commands/ -run "TestTemplates|TestPreview|TestApply|TestVerifyTemplates" -v`
Expected: PASS for all.

- [ ] **Step 8: Commit**

```bash
git add internal/smartgroup/verify.go internal/smartgroup/verify_test.go internal/commands/pro_smartgroup.go internal/commands/pro_smartgroup_test.go
git commit -m "feat(commands): implement pro smart-group verify-templates with live-tenant smoke test"
```

---

## Task 16: Smoke seed integration (CI dispatch check)

**Files:**
- Modify (only if needed): `internal/commands/smoke_seed_test.go`

- [ ] **Step 1: Inspect the existing smoke seed**

Run: `head -60 internal/commands/smoke_seed_test.go`
Expected: a Go test that exercises `--help` against every registered command. If the harness auto-walks the tree, no changes are required.

- [ ] **Step 2: Run smoke tests**

Run: `go test ./internal/commands/ -run TestSmoke -v 2>&1 | tail -20`
Expected: PASS. If smoke tests fail because the new commands need explicit registration, add the four new subcommand names (`smart-group`, `templates`, `preview`, `apply`, `verify-templates`) to whichever seed list the smoke harness uses.

- [ ] **Step 3: Commit if changes were required**

```bash
git add internal/commands/smoke_seed_test.go
git commit -m "test(commands): wire pro smart-group into smoke harness"
```

If no smoke updates were needed, skip the commit.

---

## Task 17: Full build, lint, and final verification

**Files:** none modified directly

- [ ] **Step 1: Run the full test suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 2: Run the linter**

Run: `make lint`
Expected: PASS.

- [ ] **Step 3: Format**

Run: `make fmt`
Expected: any formatter fixes applied. Re-run tests after.

- [ ] **Step 4: Verify the binary builds and the new commands work end-to-end**

Run: `make build`
Expected: builds cleanly to `bin/jamf-cli`.

Run: `bin/jamf-cli pro smart-group --help`
Expected: prints the namespace help with four subcommands.

Run: `bin/jamf-cli pro sg templates --category encryption`
Expected: prints 6 encryption templates in category-grouped form.

Run: `bin/jamf-cli pro sg preview --template encryption/encryption-stalled --stalled-after 21`
Expected: prints "POST /v2/computer-groups/smart-groups" followed by a JSON body with value="21".

- [ ] **Step 5: Commit any formatter-applied fixes**

If `make fmt` produced changes:

```bash
git add -u
git commit -m "style: gofmt/gofumpt fixes for pro smart-group package"
```

If no formatter changes, skip the commit.

---

## Self-Review

**Spec coverage:** every spec section has a task.

| Spec section | Task(s) |
| --- | --- |
| Endpoint surface table | Tasks 10 (membership), 14 (apply), 15 (verify) |
| Request body schema (`SmartGroupRequest`, `Criterion`) | Task 1 |
| Verified Criterion-Name Registry | Task 2 |
| Verified enum value sets | Tasks 4-8 (templates use the correct value strings) |
| Template inventory (23 across 5 categories) | Tasks 4-8 |
| `pro smart-group templates` | Task 12 |
| `pro smart-group preview` | Task 13 |
| `pro smart-group apply` | Task 14 |
| `pro smart-group verify-templates` | Task 15 |
| Output and exit-code conventions | Tasks 12-15 (cobra error -> non-zero exit; explicit privilege messages) |
| Testing (golden JSON, output-flag matrix, smoke) | Tasks 4-9, 12-15, 16 |
| Wiki use policy | Implicit — no wiki content shipped in any file |

**Placeholder scan:** no `TBD`/`TODO`. Every step contains runnable code.

**Type consistency:** `SmartGroupRequest`, `Criterion`, `Template`, `ParamSpec`, `Library`, `Lookup`, `Register`, `All`, `ByCategory`, `Categories`, `FuzzyMatch`, `CountMembers`, `HTTPDoer`, `VerifyOutcome`, `VerifyResult`, `RunOneVerification` — all defined in their declaring task and referenced consistently in later tasks.

**Open spec questions surfaced by the plan:** the spec's 6 open questions (display values, searchType strings, Apple Silicon value, empty-value semantics, required-param verify values, encryption-stalled precision) are all empirically resolved by `verify-templates` (Task 15), which probes every unverified string against the live tenant.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-12-pro-smart-group-templates.md`. Two execution options:

**1. Subagent-Driven (recommended)** — fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — execute tasks in this session using executing-plans, batch execution with checkpoints.

**Which approach?**
