// Copyright 2026, Jamf Software LLC

package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

// The Order field is the whole correctness argument for restore: a set applied
// before its members, or a plan before its sets, fails name resolution. Assert
// the chain rather than the numbers, so the table can be renumbered freely.
func TestProtectResourceOrderEncodesDependencies(t *testing.T) {
	order := map[string]int{}
	for _, r := range protectResources() {
		order[r.Name] = r.Order
	}

	mustPrecede := [][2]string{
		{"analytics", "analytic-sets"},
		{"unified-logging-filters", "unified-logging-filter-sets"},
		{"analytic-sets", "plans"},
		{"exception-sets", "plans"},
		{"action-configs", "plans"},
		{"telemetry", "plans"},
		{"removable-storage-control-sets", "plans"},
		{"unified-logging-filter-sets", "plans"},
		{"roles", "groups"},
		{"groups", "users"},
		{"roles", "api-clients"},
		{"analytics", "analytic-overrides"},
	}
	for _, pair := range mustPrecede {
		before, after := pair[0], pair[1]
		b, ok := order[before]
		if !ok {
			t.Fatalf("resource %q missing from the table", before)
		}
		a, ok := order[after]
		if !ok {
			t.Fatalf("resource %q missing from the table", after)
		}
		if b >= a {
			t.Errorf("%s (order %d) must be applied before %s (order %d)", before, b, after, a)
		}
	}
}

// Every resource must either be restorable or say why not. A nil Restore with no
// reason would be a silent no-op at restore time.
func TestProtectResourcesAreRestorableOrExplained(t *testing.T) {
	for _, r := range protectResources() {
		if r.Export == nil {
			t.Errorf("%s: Export is nil", r.Name)
		}
		if r.Restore == nil && r.RestoreSkipReason == "" {
			t.Errorf("%s: no Restore and no RestoreSkipReason — restore would silently skip it", r.Name)
		}
		if r.Restore != nil && r.RestoreSkipReason != "" {
			t.Errorf("%s: has both a Restore and a RestoreSkipReason", r.Name)
		}
	}
}

func TestProtectSelectResources(t *testing.T) {
	t.Run("no filters returns everything in order", func(t *testing.T) {
		got, err := protectSelectResources("", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != len(protectResources()) {
			t.Errorf("got %d resources, want all %d", len(got), len(protectResources()))
		}
		for i := 1; i < len(got); i++ {
			if got[i-1].Order > got[i].Order {
				t.Fatalf("not sorted: %s (%d) before %s (%d)", got[i-1].Name, got[i-1].Order, got[i].Name, got[i].Order)
			}
		}
	})

	t.Run("include is an allowlist", func(t *testing.T) {
		got, err := protectSelectResources("plans,roles", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d resources, want 2", len(got))
		}
	})

	t.Run("exclude removes", func(t *testing.T) {
		got, err := protectSelectResources("", "users,api-clients")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, r := range got {
			if r.Name == "users" || r.Name == "api-clients" {
				t.Errorf("%s should have been excluded", r.Name)
			}
		}
		if len(got) != len(protectResources())-2 {
			t.Errorf("got %d resources, want %d", len(got), len(protectResources())-2)
		}
	})

	t.Run("include and exclude compose", func(t *testing.T) {
		got, err := protectSelectResources("plans,roles,users", "users")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d resources, want 2 (users excluded from the allowlist)", len(got))
		}
	})

	t.Run("unknown include is rejected", func(t *testing.T) {
		_, err := protectSelectResources("nonsense", "")
		if err == nil {
			t.Fatal("expected an error naming the unknown resource")
		}
	})

	t.Run("unknown exclude is rejected", func(t *testing.T) {
		_, err := protectSelectResources("", "nonsense")
		if err == nil {
			t.Fatal("expected an error naming the unknown resource")
		}
	})

	t.Run("filters that select nothing are rejected", func(t *testing.T) {
		_, err := protectSelectResources("plans", "plans")
		if err == nil {
			t.Fatal("expected an error rather than a silent no-op run")
		}
	})
}

func TestProtectFileNameSafe(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Managed Protect - Standard", "Managed_Protect_-_Standard"},
		{"Default Analytic Set", "Default_Analytic_Set"},
		{"neil.martin@jamf.com", "neil.martin@jamf.com"},
		{"a/b:c", "a_b_c"},
		{"", "unnamed"},
		{"///", "unnamed"},
	}
	for _, tc := range tests {
		if got := protectFileNameSafe(tc.in); got != tc.want {
			t.Errorf("protectFileNameSafe(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsProtectDefaultObject(t *testing.T) {
	if !isProtectDefaultObject("roles", "Full Admin") {
		t.Error("Full Admin should be a tenant default")
	}
	if !isProtectDefaultObject("analytic-sets", "Default Analytic Set") {
		t.Error("Default Analytic Set should be a tenant default")
	}
	if isProtectDefaultObject("roles", "Jamf-Insights-Integration") {
		t.Error("a custom role must not be treated as a default")
	}
	if isProtectDefaultObject("plans", "Default") {
		t.Error("plans have no defaults registered — a tenant may legitimately name a plan Default")
	}
}

// The file name is lossy (protectFileNameSafe rewrites spaces), so the default
// check must read the name out of the document body.
func TestProtectObjectNameFromFile(t *testing.T) {
	dir := t.TempDir()

	rolePath := filepath.Join(dir, "Full_Admin.yaml")
	if err := os.WriteFile(rolePath, []byte("name: Full Admin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := protectObjectNameFromFile(rolePath); got != "Full Admin" {
		t.Errorf("got %q, want %q from the document body", got, "Full Admin")
	}

	userPath := filepath.Join(dir, "u.yaml")
	if err := os.WriteFile(userPath, []byte("email: a@b.c\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := protectObjectNameFromFile(userPath); got != "a@b.c" {
		t.Errorf("got %q, want the email", got)
	}

	if got := protectObjectNameFromFile(filepath.Join(dir, "missing.yaml")); got != "" {
		t.Errorf("got %q, want empty for an unreadable file", got)
	}
}

// The action config params blob comes back as a union across every client type,
// fully populated with zero values. Sending those back is refused.
func TestPruneEmptyValues(t *testing.T) {
	got := pruneEmptyValues(map[string]any{
		"host":     "",
		"port":     float64(0),
		"scheme":   "",
		"headers":  nil,
		"backups":  float64(0),
		"url":      "https://example.com",
		"maxSizeM": float64(5),
		"enabled":  false,
		"empty":    []any{},
		"tags":     []any{"a"},
		"nested":   map[string]any{"a": "", "b": float64(0)},
		"kept":     map[string]any{"a": "value"},
	})

	for _, gone := range []string{"host", "port", "scheme", "headers", "backups", "empty", "nested"} {
		if _, ok := got[gone]; ok {
			t.Errorf("%q should have been pruned", gone)
		}
	}
	if got["url"] != "https://example.com" {
		t.Error("a non-empty string must survive")
	}
	if got["maxSizeM"] != float64(5) {
		t.Error("a non-zero number must survive")
	}
	// false is a setting, not an absence.
	if v, ok := got["enabled"]; !ok || v != false {
		t.Error("boolean false must be kept — unlike an unset port, it is meaningful")
	}
	if kept, ok := got["kept"].(map[string]any); !ok || kept["a"] != "value" {
		t.Error("a nested map with content must survive")
	}
}

func TestPruneEmptyValuesOnNilMap(t *testing.T) {
	if got := pruneEmptyValues(nil); len(got) != 0 {
		t.Errorf("got %v, want an empty map", got)
	}
}

// The retention response is nested and the update input is flat, so a backup
// storing the response replayed as zeros, which the server rejects.
func TestDataRetentionToInput(t *testing.T) {
	got := dataRetentionToInput(jamfprotect.DataRetentionSettings{
		Database: &jamfprotect.DataRetentionDatabase{
			Log:   &jamfprotect.DataRetentionDays{NumberOfDays: 180},
			Alert: &jamfprotect.DataRetentionDays{NumberOfDays: 90},
		},
		Cold: &jamfprotect.DataRetentionCold{
			Alert: &jamfprotect.DataRetentionColdDays{NumberOfDays: 30},
		},
	})

	if got.DatabaseLogDays != 180 {
		t.Errorf("DatabaseLogDays = %d, want 180", got.DatabaseLogDays)
	}
	if got.DatabaseAlertDays != 90 {
		t.Errorf("DatabaseAlertDays = %d, want 90", got.DatabaseAlertDays)
	}
	if got.ColdAlertDays != 30 {
		t.Errorf("ColdAlertDays = %d, want 30", got.ColdAlertDays)
	}
}

func TestDataRetentionToInputHandlesNilSections(t *testing.T) {
	got := dataRetentionToInput(jamfprotect.DataRetentionSettings{})
	if got.DatabaseLogDays != 0 || got.DatabaseAlertDays != 0 || got.ColdAlertDays != 0 {
		t.Errorf("got %+v, want zeros without panicking", got)
	}
}

// A restore must walk resources in dependency order and skip tenant defaults.
func TestCollectProtectRestoreFilesOrderAndDefaults(t *testing.T) {
	dir := t.TempDir()
	write := func(sub, file, body string) {
		if sub != "" {
			if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, sub, file), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("plans", "My_Plan.yaml", "name: My Plan\n")
	write("analytics", "Custom.yaml", "name: Custom\n")
	write("roles", "Full_Admin.yaml", "name: Full Admin\n")
	write("roles", "Custom_Role.yaml", "name: Custom Role\n")

	selected, err := protectSelectResources("", "")
	if err != nil {
		t.Fatal(err)
	}

	files, skipped, err := collectProtectRestoreFiles(dir, selected, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var gotOrder []string
	for _, f := range files {
		gotOrder = append(gotOrder, f.Resource.Name+"/"+filepath.Base(f.Path))
	}
	want := []string{"analytics/Custom.yaml", "plans/My_Plan.yaml", "roles/Custom_Role.yaml"}
	if len(gotOrder) != len(want) {
		t.Fatalf("got %v, want %v", gotOrder, want)
	}
	for i := range want {
		if gotOrder[i] != want[i] {
			t.Errorf("position %d = %q, want %q", i, gotOrder[i], want[i])
		}
	}

	var sawDefaultSkip bool
	for _, s := range skipped {
		if filepath.Base(s) != "" && contains(s, "Full Admin") {
			sawDefaultSkip = true
		}
	}
	if !sawDefaultSkip {
		t.Errorf("expected the built-in role to be reported as skipped, got %v", skipped)
	}

	// With --include-defaults it comes back.
	files, _, err = collectProtectRestoreFiles(dir, selected, true)
	if err != nil {
		t.Fatal(err)
	}
	var sawFullAdmin bool
	for _, f := range files {
		if filepath.Base(f.Path) == "Full_Admin.yaml" {
			sawFullAdmin = true
		}
	}
	if !sawFullAdmin {
		t.Error("--include-defaults must apply the built-in role")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		len(needle) == 0 || indexOf(haystack, needle) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
