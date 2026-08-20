// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"

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
		{"analytics", "exception-sets"},
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
		if strings.Contains(s, "Full Admin") {
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

// protectFileNameSafe is lossy in two directions: it collapses runs of illegal
// characters, and a case-insensitive filesystem (the macOS default) folds names
// that differ only in case. Both used to mean one object silently overwrote
// another while the count still reported two.
func TestProtectNameAllocatorDisambiguatesCollisions(t *testing.T) {
	t.Run("punctuation collapsing to the same name", func(t *testing.T) {
		a := newProtectNameAllocator()
		first, wasFirstDisambiguated := a.allocate("Alert: High")
		second, wasSecondDisambiguated := a.allocate("Alert/High")

		if first != "Alert_High" {
			t.Errorf("first = %q, want the plain safe name", first)
		}
		if wasFirstDisambiguated {
			t.Error("the first claimant must keep the plain name")
		}
		if !wasSecondDisambiguated {
			t.Error("the second object must be reported as disambiguated")
		}
		if second == first {
			t.Fatalf("both objects got %q — one would overwrite the other", first)
		}
		if !strings.HasPrefix(second, "Alert_High-") {
			t.Errorf("second = %q, want the safe name plus a discriminator", second)
		}
	})

	t.Run("case-only difference", func(t *testing.T) {
		a := newProtectNameAllocator()
		first, _ := a.allocate("MyPlan")
		second, disambiguated := a.allocate("myplan")
		if !disambiguated {
			t.Error("a case-only difference collides on a case-insensitive filesystem")
		}
		if strings.EqualFold(first, second) {
			t.Fatalf("%q and %q are the same file on APFS", first, second)
		}
	})

	t.Run("distinct names are untouched", func(t *testing.T) {
		a := newProtectNameAllocator()
		for _, name := range []string{"One", "Two", "Three"} {
			got, disambiguated := a.allocate(name)
			if got != name || disambiguated {
				t.Errorf("allocate(%q) = %q (disambiguated=%v), want the name unchanged", name, got, disambiguated)
			}
		}
	})

	// A backup directory under version control must diff cleanly, so the
	// discriminator has to be derived from the name rather than from iteration.
	t.Run("allocation is stable across runs", func(t *testing.T) {
		run := func() []string {
			a := newProtectNameAllocator()
			var out []string
			for _, n := range []string{"Alert: High", "Alert/High", "Alert|High"} {
				got, _ := a.allocate(n)
				out = append(out, got)
			}
			return out
		}
		first, second := run(), run()
		for i := range first {
			if first[i] != second[i] {
				t.Errorf("position %d: %q then %q — not reproducible", i, first[i], second[i])
			}
		}
	})

	// Re-offering the same object name must return the same file, not a new one.
	t.Run("the same object name is idempotent", func(t *testing.T) {
		a := newProtectNameAllocator()
		first, _ := a.allocate("Plan A")
		second, disambiguated := a.allocate("Plan A")
		if first != second || disambiguated {
			t.Errorf("re-allocating %q gave %q then %q", "Plan A", first, second)
		}
	})
}

// The forwarding settings are captured for reference and never replayed, so the
// one credential the API returns in cleartext has no reason to be in the file.
func TestRedactDataForwarding(t *testing.T) {
	got := redactDataForwarding(jamfprotect.DataForwardingResult{
		UUID: "u1",
		Forward: &jamfprotect.DataForwardingSettings{
			Sentinel: &jamfprotect.ForwardSentinel{
				Enabled:    true,
				CustomerID: "cust-1",
				SharedKey:  "super-secret-shared-key",
				LogType:    "JamfProtect",
			},
			S3: &jamfprotect.ForwardS3{Bucket: "b", Enabled: true},
		},
	})

	if got.Forward.Sentinel.SharedKey != protectRedacted {
		t.Errorf("SharedKey = %q, want it redacted", got.Forward.Sentinel.SharedKey)
	}
	// Everything else must survive — the document is still a useful record.
	if got.Forward.Sentinel.CustomerID != "cust-1" || !got.Forward.Sentinel.Enabled {
		t.Errorf("non-secret Sentinel fields were lost: %+v", got.Forward.Sentinel)
	}
	if got.Forward.S3 == nil || got.Forward.S3.Bucket != "b" {
		t.Error("the S3 section must be untouched")
	}
	if got.UUID != "u1" {
		t.Error("the organization uuid must be untouched")
	}
}

func TestRedactDataForwardingDoesNotMutateItsInput(t *testing.T) {
	sentinel := &jamfprotect.ForwardSentinel{SharedKey: "keep-me"}
	in := jamfprotect.DataForwardingResult{
		Forward: &jamfprotect.DataForwardingSettings{Sentinel: sentinel},
	}
	_ = redactDataForwarding(in)
	if sentinel.SharedKey != "keep-me" {
		t.Errorf("the caller's value was overwritten: %q", sentinel.SharedKey)
	}
}

func TestRedactDataForwardingHandlesNilSections(t *testing.T) {
	if got := redactDataForwarding(jamfprotect.DataForwardingResult{}); got.Forward != nil {
		t.Error("a nil Forward must pass through without panicking")
	}
	got := redactDataForwarding(jamfprotect.DataForwardingResult{
		Forward: &jamfprotect.DataForwardingSettings{},
	})
	if got.Forward.Sentinel != nil {
		t.Error("a nil Sentinel must pass through")
	}
}

// Every resource that can carry a credential must say so, because that string is
// what turns the file mode down and what the operator is shown before they commit
// the tree to git.
func TestProtectSensitiveResourcesAreDeclared(t *testing.T) {
	want := map[string]bool{"action-configs": true, "data-forwarding": true}
	for _, r := range protectResources() {
		if want[r.Name] && r.SensitiveReason == "" {
			t.Errorf("%s can carry a third-party credential but declares no SensitiveReason", r.Name)
		}
		if !want[r.Name] && r.SensitiveReason != "" {
			t.Errorf("%s declares a SensitiveReason; add it to this test's list deliberately", r.Name)
		}
	}
}

// A backup that exits 0 with a resource missing is indistinguishable from a good
// one to the scheduled job that runs it. 'pro backup' returns a partial-failure
// code for the same condition; this asserts the two products agree.
func TestRunProtectBackupExitsNonZeroOnPartialFailure(t *testing.T) {
	newCtx := func() *registry.CLIContext {
		return &registry.CLIContext{
			ProtectClient: &mockProtectClient{
				analyticsErr: errors.New("listAnalytics: 502 bad gateway"),
				ulfFilters:   []jamfprotect.UnifiedLoggingFilter{{UUID: "f1", Name: "zz-filter"}},
			},
		}
	}

	t.Run("failure is reported and the run exits non-zero", func(t *testing.T) {
		dir := t.TempDir()
		err := runProtectBackup(context.Background(), newCtx(), dir,
			"yaml", "analytics,unified-logging-filters", "", false)
		if err == nil {
			t.Fatal("expected a non-zero exit; a silently incomplete backup is the failure mode this guards")
		}
		if !strings.Contains(err.Error(), "failure") {
			t.Errorf("error %q should name the failure count", err)
		}
		// The failure manifest must still be on disk, and the healthy resource
		// must still have been captured — partial failures stay tolerated.
		if _, statErr := os.Stat(filepath.Join(dir, "_failures.yaml")); statErr != nil {
			t.Errorf("_failures.yaml not written: %v", statErr)
		}
		if _, statErr := os.Stat(filepath.Join(dir, "unified-logging-filters", "zz-filter.yaml")); statErr != nil {
			t.Errorf("the healthy resource was not captured: %v", statErr)
		}
	})

	// --allow-partial-failure is the root persistent flag, read from the package
	// var the same way pro_backup.go reads it. A local flag of the same name would
	// shadow it and silently ignore the global position.
	t.Run("--allow-partial-failure downgrades to success", func(t *testing.T) {
		prev := allowPartialFailure
		allowPartialFailure = true
		defer func() { allowPartialFailure = prev }()

		dir := t.TempDir()
		err := runProtectBackup(context.Background(), newCtx(), dir,
			"yaml", "analytics,unified-logging-filters", "", false)
		if err != nil {
			t.Fatalf("--allow-partial-failure should exit 0, got %v", err)
		}
	})

	t.Run("a clean run still exits zero", func(t *testing.T) {
		dir := t.TempDir()
		ctx := &registry.CLIContext{ProtectClient: &mockProtectClient{
			ulfFilters: []jamfprotect.UnifiedLoggingFilter{{UUID: "f1", Name: "zz-filter"}},
		}}
		if err := runProtectBackup(context.Background(), ctx, dir,
			"yaml", "unified-logging-filters", "", false); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, statErr := os.Stat(filepath.Join(dir, "_failures.yaml")); statErr == nil {
			t.Error("_failures.yaml must not exist for a clean run")
		}
	})
}

// A document that can carry a credential must not be world-readable, because the
// documented workflow for a backup directory is to commit it to version control.
func TestRunProtectBackupTightensPermissionsOnSensitiveResources(t *testing.T) {
	dir := t.TempDir()
	ctx := &registry.CLIContext{ProtectClient: &mockProtectClient{
		actionConfigs: []jamfprotect.ActionConfigListItem{{ID: "a1", Name: "zz-http-forwarder"}},
		actionConfig: &jamfprotect.ActionConfig{
			ID:   "a1",
			Name: "zz-http-forwarder",
			Clients: []jamfprotect.ReportClient{{
				ID:   "c1",
				Type: "Http",
				Params: jamfprotect.ReportClientParams{
					URL:    "https://siem.example.com/ingest",
					Method: "POST",
					Headers: []jamfprotect.ReportClientHeader{
						{Header: "Authorization", Value: "Bearer super-secret-token"},
					},
				},
			}},
		},
		ulfFilters: []jamfprotect.UnifiedLoggingFilter{{UUID: "f1", Name: "zz-filter"}},
	}}

	if err := runProtectBackup(context.Background(), ctx, dir,
		"yaml", "action-configs,unified-logging-filters", "", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sensitive := filepath.Join(dir, "action-configs", "zz-http-forwarder.yaml")
	info, err := os.Stat(sensitive)
	if err != nil {
		t.Fatalf("action config not written: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %#o, want 0600 — this file contains the request headers verbatim", perm)
	}

	// The header value really is in there, which is why the mode matters. If a
	// future change redacts it, this assertion is the one to revisit.
	body, err := os.ReadFile(sensitive)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "super-secret-token") {
		t.Log("note: header values are no longer captured verbatim — the 0600 mode may now be unnecessary")
	}

	// A resource that cannot carry a credential keeps the ordinary mode, so the
	// tightening is targeted rather than blanket.
	ordinary, err := os.Stat(filepath.Join(dir, "unified-logging-filters", "zz-filter.yaml"))
	if err != nil {
		t.Fatalf("filter not written: %v", err)
	}
	if perm := ordinary.Mode().Perm(); perm != 0o644 {
		t.Errorf("mode = %#o, want 0644 for a non-sensitive resource", perm)
	}
}

// Retention updates are rate-limited to once per 24 hours, so an unconditional
// write made a re-run report a failure for a resource already in the desired
// state. config-freeze and insights compare first; this must too.
func TestRestoreDataRetentionSkipsAnUnchangedWrite(t *testing.T) {
	settings := jamfprotect.DataRetentionSettings{
		Database: &jamfprotect.DataRetentionDatabase{
			Log:   &jamfprotect.DataRetentionDays{NumberOfDays: 180},
			Alert: &jamfprotect.DataRetentionDays{NumberOfDays: 90},
		},
		Cold: &jamfprotect.DataRetentionCold{
			Alert: &jamfprotect.DataRetentionColdDays{NumberOfDays: 30},
		},
	}

	var retention protectResource
	for _, r := range protectResources() {
		if r.Name == "data-retention" {
			retention = r
		}
	}
	if retention.Restore == nil {
		t.Fatal("data-retention has no Restore")
	}

	doc, err := protectMarshal(dataRetentionToInput(settings), "yaml")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("already matching: no write", func(t *testing.T) {
		mock := &mockProtectClient{retention: settings}
		msg, err := retention.Restore(context.Background(), mock, protect.NewResolver(mock), doc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.retentionWrites != 0 {
			t.Errorf("UpdateDataRetention called %d time(s); a re-run inside 24h would fail on a no-op",
				mock.retentionWrites)
		}
		if !strings.Contains(msg, "already") {
			t.Errorf("message = %q, want it to say the value already matches", msg)
		}
	})

	t.Run("differing: writes once", func(t *testing.T) {
		different := settings
		different.Database = &jamfprotect.DataRetentionDatabase{
			Log:   &jamfprotect.DataRetentionDays{NumberOfDays: 30},
			Alert: &jamfprotect.DataRetentionDays{NumberOfDays: 90},
		}
		mock := &mockProtectClient{retention: different}
		if _, err := retention.Restore(context.Background(), mock, protect.NewResolver(mock), doc); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.retentionWrites != 1 {
			t.Errorf("UpdateDataRetention called %d time(s), want 1 — a real change must still be sent",
				mock.retentionWrites)
		}
	})
}

// Only "absent" may become a create. A lookup that failed for any other reason
// must abort the object, or a transient blip mid-restore mutates the tenant a
// different way than the backup describes.
func TestUpsertByNameOnlyCreatesWhenGenuinelyAbsent(t *testing.T) {
	type input struct{ Name string }

	t.Run("not found creates", func(t *testing.T) {
		var created, updated int
		name, err := upsertByName(context.Background(), "thing", input{Name: "thing"},
			func(context.Context, string) (string, error) {
				return "", fmt.Errorf("thing %q not found; use 'protect things list': %w", "thing", protect.ErrNotFound)
			},
			func(context.Context, input) (any, error) { created++; return nil, nil },
			func(context.Context, string, input) (any, error) { updated++; return nil, nil },
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if created != 1 || updated != 0 {
			t.Errorf("created=%d updated=%d, want a single create", created, updated)
		}
		if name != "thing" {
			t.Errorf("name = %q", name)
		}
	})

	t.Run("found updates", func(t *testing.T) {
		var created, updated int
		if _, err := upsertByName(context.Background(), "thing", input{Name: "thing"},
			func(context.Context, string) (string, error) { return "id-1", nil },
			func(context.Context, input) (any, error) { created++; return nil, nil },
			func(context.Context, string, input) (any, error) { updated++; return nil, nil },
		); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if created != 0 || updated != 1 {
			t.Errorf("created=%d updated=%d, want a single update", created, updated)
		}
	})

	t.Run("a transient lookup failure creates nothing", func(t *testing.T) {
		var created, updated int
		_, err := upsertByName(context.Background(), "thing", input{Name: "thing"},
			func(context.Context, string) (string, error) {
				return "", errors.New("listThings: 502 bad gateway")
			},
			func(context.Context, input) (any, error) { created++; return nil, nil },
			func(context.Context, string, input) (any, error) { updated++; return nil, nil },
		)
		if err == nil {
			t.Fatal("expected the lookup failure to abort the object")
		}
		if created != 0 || updated != 0 {
			t.Errorf("created=%d updated=%d — a 502 must not be read as \"absent, so create it\"", created, updated)
		}
		if !strings.Contains(err.Error(), "502") {
			t.Errorf("error %q should carry the real cause, not a duplicate-name message", err)
		}
	})
}

// The resource vocabulary was discoverable only two ways: shell completion on
// --resources, and passing a wrong value to read the error. --help never listed
// it and --exclude did not complete at all, despite taking the same vocabulary.
// These pin the fix so the help cannot drift from the table.
func TestProtectResourceListHelpCoversEveryResource(t *testing.T) {
	for _, forRestore := range []bool{false, true} {
		name := "backup"
		if forRestore {
			name = "restore"
		}
		t.Run(name, func(t *testing.T) {
			help := protectResourceListHelp(forRestore)
			for _, r := range protectResources() {
				if !strings.Contains(help, r.Name) {
					t.Errorf("%s is in protectResources() but missing from --help", r.Name)
				}
			}
			if forRestore {
				// A user asking to restore one of these should be able to see
				// from --help why nothing happens.
				for _, r := range protectResources() {
					if r.RestoreSkipReason == "" {
						continue
					}
					line := ""
					for _, l := range strings.Split(help, "\n") {
						if strings.HasPrefix(strings.TrimSpace(l), r.Name) {
							line = l
						}
					}
					if !strings.Contains(line, "never replayed") {
						t.Errorf("%s is backup-only but restore --help does not say so (line: %q)", r.Name, line)
					}
				}
			}
		})
	}
}

// Both flags take the same vocabulary and are documented as composing, so both
// must complete.
func TestProtectBackupAndRestoreCompleteBothResourceFlags(t *testing.T) {
	protectCmd := findProtectCmd(t)
	for _, name := range []string{"backup", "restore"} {
		t.Run(name, func(t *testing.T) {
			cmd := findSubcommand(protectCmd, name)
			if cmd == nil {
				t.Fatalf("%s subcommand not found", name)
			}
			for _, flag := range []string{"resources", "exclude"} {
				if cmd.Flags().Lookup(flag) == nil {
					t.Fatalf("--%s not defined on %s", flag, name)
				}
				got, _ := cmd.GetFlagCompletionFunc(flag)
				if got == nil {
					t.Errorf("--%s on %s has no completion function", flag, name)
				}
			}
		})
	}
}

// createGroup accepts accessGroup: true on a connection-less local group but
// updateGroup refuses it — even when true is already stored. Verified on the wire.
// So a restore of such a group succeeded once and failed on every re-run.
func TestRestoreGroupHandlesTheAccessGroupAsymmetry(t *testing.T) {
	local := func(access bool) jamfprotect.GroupInput {
		return jamfprotect.GroupInput{Name: "zz-group", AccessGroup: access, RoleIDs: []string{}}
	}

	t.Run("absent: created", func(t *testing.T) {
		m := &mockProtectClient{}
		got, err := restoreGroup(context.Background(), m, local(true))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "zz-group" {
			t.Errorf("got %q", got)
		}
		if m.createdGroups != 1 || m.updatedGroups != 0 {
			t.Errorf("created=%d updated=%d, want a single create", m.createdGroups, m.updatedGroups)
		}
	})

	// The re-run that used to fail.
	t.Run("present and already an access group: no update attempted", func(t *testing.T) {
		m := &mockProtectClient{groups: []jamfprotect.Group{{ID: "g-1", Name: "zz-group", AccessGroup: true}}}
		got, err := restoreGroup(context.Background(), m, local(true))
		if err != nil {
			t.Fatalf("re-applying an unchanged access group must not fail: %v", err)
		}
		if m.updatedGroups != 0 {
			t.Errorf("updateGroup was called %d time(s); the server refuses it for a local group", m.updatedGroups)
		}
		if !strings.Contains(got, "already an access group") {
			t.Errorf("message = %q, want it to say the desired state already holds", got)
		}
	})

	// Genuinely impossible via update: say so instead of passing through the
	// server's "Local groups cannot be designated as access groups".
	t.Run("present but access disabled: actionable error", func(t *testing.T) {
		m := &mockProtectClient{groups: []jamfprotect.Group{{ID: "g-1", Name: "zz-group", AccessGroup: false}}}
		_, err := restoreGroup(context.Background(), m, local(true))
		if err == nil {
			t.Fatal("expected an error explaining that update cannot do this")
		}
		for _, want := range []string{"zz-group", "only at create"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q should mention %q", err, want)
			}
		}
		if m.updatedGroups != 0 {
			t.Error("must not spend a call the server will refuse")
		}
	})

	// A group with a connection is unaffected; so is accessGroup: false.
	t.Run("ordinary updates still happen", func(t *testing.T) {
		for _, in := range []jamfprotect.GroupInput{
			local(false),
			{Name: "zz-group", AccessGroup: true, ConnectionID: strp("conn-1"), RoleIDs: []string{}},
		} {
			m := &mockProtectClient{groups: []jamfprotect.Group{{ID: "g-1", Name: "zz-group"}}}
			if _, err := restoreGroup(context.Background(), m, in); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if m.updatedGroups != 1 {
				t.Errorf("updateGroup called %d time(s), want 1 for %+v", m.updatedGroups, in)
			}
		}
	})
}

// A backup directory used to be the union of every run that wrote to it, and
// because restore applies whatever it finds, an object deleted from the tenant was
// silently recreated by the next restore.
func TestBackupPrunesDocumentsThatNoLongerMatchTheTenant(t *testing.T) {
	twoFilters := []jamfprotect.UnifiedLoggingFilter{
		{UUID: "f1", Name: "zz-keep"},
		{UUID: "f2", Name: "zz-remove-me"},
	}

	t.Run("an object deleted from the tenant loses its file", func(t *testing.T) {
		dir := t.TempDir()
		ctx := &registry.CLIContext{ProtectClient: &mockProtectClient{ulfFilters: twoFilters}}
		if err := runProtectBackup(context.Background(), ctx, dir, "yaml", "unified-logging-filters", "", false); err != nil {
			t.Fatal(err)
		}
		for _, n := range []string{"zz-keep.yaml", "zz-remove-me.yaml"} {
			if _, err := os.Stat(filepath.Join(dir, "unified-logging-filters", n)); err != nil {
				t.Fatalf("first backup did not write %s: %v", n, err)
			}
		}

		// Second run: the tenant now holds only one of them.
		ctx = &registry.CLIContext{ProtectClient: &mockProtectClient{ulfFilters: twoFilters[:1]}}
		if err := runProtectBackup(context.Background(), ctx, dir, "yaml", "unified-logging-filters", "", false); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "unified-logging-filters", "zz-keep.yaml")); err != nil {
			t.Errorf("the surviving object lost its file: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "unified-logging-filters", "zz-remove-me.yaml")); err == nil {
			t.Error("the deleted object kept its file; a restore would recreate it")
		}
	})

	t.Run("--no-prune keeps it", func(t *testing.T) {
		dir := t.TempDir()
		ctx := &registry.CLIContext{ProtectClient: &mockProtectClient{ulfFilters: twoFilters}}
		if err := runProtectBackup(context.Background(), ctx, dir, "yaml", "unified-logging-filters", "", false); err != nil {
			t.Fatal(err)
		}
		ctx = &registry.CLIContext{ProtectClient: &mockProtectClient{ulfFilters: twoFilters[:1]}}
		if err := runProtectBackup(context.Background(), ctx, dir, "yaml", "unified-logging-filters", "", true); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "unified-logging-filters", "zz-remove-me.yaml")); err != nil {
			t.Errorf("--no-prune must keep the stale document: %v", err)
		}
	})

	// Switching format used to leave both sets on disk, and restore reads .yaml,
	// .yml and .json alike — so every object would be applied twice.
	t.Run("switching format removes the other format's documents", func(t *testing.T) {
		dir := t.TempDir()
		ctx := &registry.CLIContext{ProtectClient: &mockProtectClient{ulfFilters: twoFilters[:1]}}
		if err := runProtectBackup(context.Background(), ctx, dir, "yaml", "unified-logging-filters", "", false); err != nil {
			t.Fatal(err)
		}
		if err := runProtectBackup(context.Background(), ctx, dir, "json", "unified-logging-filters", "", false); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "unified-logging-filters", "zz-keep.json")); err != nil {
			t.Errorf("json document missing: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "unified-logging-filters", "zz-keep.yaml")); err == nil {
			t.Error("the yaml document survived a switch to json; restore would apply the object twice")
		}
	})

	// The rail that matters: a failed export must never delete anything, because
	// the true object set is unknown.
	t.Run("a resource whose export failed is never pruned", func(t *testing.T) {
		dir := t.TempDir()
		ctx := &registry.CLIContext{ProtectClient: &mockProtectClient{ulfFilters: twoFilters}}
		if err := runProtectBackup(context.Background(), ctx, dir, "yaml", "unified-logging-filters", "", false); err != nil {
			t.Fatal(err)
		}

		// Now the same resource fails to list.
		ctx = &registry.CLIContext{ProtectClient: &mockProtectClient{
			ulfErr: errors.New("listUnifiedLoggingFilters: 502 bad gateway"),
		}}
		// The run exits non-zero: the only selected resource failed. What matters
		// here is what it did NOT do to the directory.
		if err := runProtectBackup(context.Background(), ctx, dir, "yaml", "unified-logging-filters", "", false); err == nil {
			t.Fatal("expected a non-zero exit when the only selected resource failed")
		}
		for _, n := range []string{"zz-keep.yaml", "zz-remove-me.yaml"} {
			if _, statErr := os.Stat(filepath.Join(dir, "unified-logging-filters", n)); statErr != nil {
				t.Errorf("%s was deleted after a failed export — that turns an outage into data loss", n)
			}
		}
	})

	// Pruning must not reach outside the directories this command writes.
	t.Run("files the command could not have written are untouched", func(t *testing.T) {
		dir := t.TempDir()
		ctx := &registry.CLIContext{ProtectClient: &mockProtectClient{ulfFilters: twoFilters[:1]}}
		if err := runProtectBackup(context.Background(), ctx, dir, "yaml", "unified-logging-filters", "", false); err != nil {
			t.Fatal(err)
		}

		bystanders := map[string]string{
			filepath.Join(dir, "README.md"):                            "notes",
			filepath.Join(dir, "unified-logging-filters", "notes.txt"): "not a backup document",
			filepath.Join(dir, "plans.yaml"):                           "a singleton name this run never captured",
		}
		for path, body := range bystanders {
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := runProtectBackup(context.Background(), ctx, dir, "yaml", "unified-logging-filters", "", false); err != nil {
			t.Fatal(err)
		}
		for path := range bystanders {
			if _, err := os.Stat(path); err != nil {
				t.Errorf("%s was deleted; pruning must only consider files it could have written", path)
			}
		}
	})
}

// os.WriteFile applies its perm argument only when it creates the file, so a
// re-run into an existing tree used to keep whatever mode was already there while
// the summary still claimed 0600. A git clone of a backup repo hands every file
// back 0644 — git records no non-exec permissions — which is exactly the workflow
// the jamf-backup skill recommends.
func TestRunProtectBackupTightensPermissionsOnAnExistingFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "action-configs"), 0o755); err != nil {
		t.Fatal(err)
	}
	sensitive := filepath.Join(dir, "action-configs", "zz-http-forwarder.yaml")
	// Stand in for the checkout: the file already exists, world-readable.
	if err := os.WriteFile(sensitive, []byte("name: zz-http-forwarder\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := &registry.CLIContext{ProtectClient: &mockProtectClient{
		actionConfigs: []jamfprotect.ActionConfigListItem{{ID: "a1", Name: "zz-http-forwarder"}},
		actionConfig: &jamfprotect.ActionConfig{
			ID:   "a1",
			Name: "zz-http-forwarder",
			Clients: []jamfprotect.ReportClient{{
				ID:   "c1",
				Type: "Http",
				Params: jamfprotect.ReportClientParams{
					URL:    "https://siem.example.com/ingest",
					Method: "POST",
					Headers: []jamfprotect.ReportClientHeader{
						{Header: "Authorization", Value: "Bearer super-secret-token"},
					},
				},
			}},
		},
	}}
	if err := runProtectBackup(context.Background(), ctx, dir, "yaml", "action-configs", "", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, err := os.Stat(sensitive)
	if err != nil {
		t.Fatalf("action config not written: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %#o, want 0600 — the run reports 0600 and must deliver it on a re-run too", perm)
	}
}

// Pruning is keyed on "the object set of the tenant being backed up now", so
// running it against a directory holding a different tenant's backup would delete
// that tenant's documents for every object this one does not have — reported only
// as a prune count, with no confirmation prompt and no destructive annotation.
func TestBackupRefusesToPruneAnotherTenantsDirectory(t *testing.T) {
	twoFilters := []jamfprotect.UnifiedLoggingFilter{
		{UUID: "f1", Name: "zz-staging-only"},
		{UUID: "f2", Name: "zz-shared"},
	}
	staging := func() *registry.CLIContext {
		return &registry.CLIContext{
			ProtectURL:    "https://staging.protect.jamfcloud.com",
			ProtectClient: &mockProtectClient{ulfFilters: twoFilters},
		}
	}
	production := func() *registry.CLIContext {
		return &registry.CLIContext{
			ProtectURL:    "https://prod.protect.jamfcloud.com",
			ProtectClient: &mockProtectClient{ulfFilters: twoFilters[1:]},
		}
	}

	t.Run("a second tenant is refused before anything is deleted", func(t *testing.T) {
		dir := t.TempDir()
		if err := runProtectBackup(context.Background(), staging(), dir, "yaml", "unified-logging-filters", "", false); err != nil {
			t.Fatal(err)
		}

		err := runProtectBackup(context.Background(), production(), dir, "yaml", "unified-logging-filters", "", false)
		if err == nil {
			t.Fatal("expected a refusal; pruning would delete the other tenant's documents")
		}
		for _, want := range []string{"staging.protect.jamfcloud.com", "prod.protect.jamfcloud.com", "--no-prune"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q should mention %q", err, want)
			}
		}
		if _, statErr := os.Stat(filepath.Join(dir, "unified-logging-filters", "zz-staging-only.yaml")); statErr != nil {
			t.Errorf("the other tenant's document was deleted anyway: %v", statErr)
		}
	})

	t.Run("--no-prune writes alongside them", func(t *testing.T) {
		dir := t.TempDir()
		if err := runProtectBackup(context.Background(), staging(), dir, "yaml", "unified-logging-filters", "", false); err != nil {
			t.Fatal(err)
		}
		if err := runProtectBackup(context.Background(), production(), dir, "yaml", "unified-logging-filters", "", true); err != nil {
			t.Fatalf("--no-prune must be allowed: %v", err)
		}
		if _, statErr := os.Stat(filepath.Join(dir, "unified-logging-filters", "zz-staging-only.yaml")); statErr != nil {
			t.Errorf("--no-prune must keep the other tenant's document: %v", statErr)
		}
	})

	t.Run("the same tenant still prunes", func(t *testing.T) {
		dir := t.TempDir()
		if err := runProtectBackup(context.Background(), staging(), dir, "yaml", "unified-logging-filters", "", false); err != nil {
			t.Fatal(err)
		}
		shrunk := &registry.CLIContext{
			ProtectURL:    "https://staging.protect.jamfcloud.com",
			ProtectClient: &mockProtectClient{ulfFilters: twoFilters[1:]},
		}
		if err := runProtectBackup(context.Background(), shrunk, dir, "yaml", "unified-logging-filters", "", false); err != nil {
			t.Fatal(err)
		}
		if _, statErr := os.Stat(filepath.Join(dir, "unified-logging-filters", "zz-staging-only.yaml")); statErr == nil {
			t.Error("the deleted object kept its file; the guard must not disable pruning for the owning tenant")
		}
	})
}

// One refused override must not abandon the document: the entries before it are
// already written, and aborting left them unreported and the rest never attempted.
// This is the behaviour 'overrides apply' has, and whose comment claims restore
// matches it.
func TestRestoreAnalyticOverridesReportsAndContinues(t *testing.T) {
	var overrides protectResource
	for _, r := range protectResources() {
		if r.Name == "analytic-overrides" {
			overrides = r
		}
	}
	if overrides.Restore == nil {
		t.Fatal("analytic-overrides has no Restore")
	}

	doc, err := protectMarshal(analyticOverridesDoc{Overrides: []analyticOverride{
		{Analytic: "zz-first", Severity: "High"},
		{Analytic: "zz-second", Actions: []analyticOverrideAction{{Name: "Bogus"}}},
		{Analytic: "zz-third", Severity: "Low"},
	}}, "yaml")
	if err != nil {
		t.Fatal(err)
	}

	mock := &mockProtectClient{
		analytics: []jamfprotect.Analytic{
			{UUID: "a1", Name: "zz-first", Jamf: true},
			{UUID: "a2", Name: "zz-second", Jamf: true},
			{UUID: "a3", Name: "zz-third", Jamf: true},
		},
		internalAnalyticFailFor: "a2",
	}

	_, err = overrides.Restore(context.Background(), mock, protect.NewResolver(mock), doc)
	if err == nil {
		t.Fatal("expected an error so the resource still counts as failed and the run exits non-zero")
	}
	for _, want := range []string{"1 of 3", "2 applied"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should report both counts (%q)", err, want)
		}
	}
	if got, want := mock.internalAnalyticWrites, []string{"a1", "a3"}; !slices.Equal(got, want) {
		t.Errorf("applied %v, want %v — the entries after the failure must still be attempted", got, want)
	}
}

// Cobra's AddFlagSet skips an inherited flag whose name is already taken, and the
// shorthand goes with it. A local --dry-run on 'protect restore' therefore removed
// the root -n entirely, so 'restore -n' failed with "unknown shorthand flag" and
// neither form appeared in the command's Global Flags list.
func TestProtectRestoreUsesTheRootDryRunFlag(t *testing.T) {
	restore := findSubcommand(findProtectCmd(t), "restore")
	if restore == nil {
		t.Fatal("restore subcommand not found")
	}
	if restore.Flags().Lookup("dry-run") != nil {
		t.Error("--dry-run is declared locally; that shadows the root persistent flag and drops -n")
	}
	// After ParseFlags the merged set is what the user actually gets.
	if err := restore.ParseFlags([]string{"-n"}); err != nil {
		t.Fatalf("restore -n must parse: %v", err)
	}
	if f := restore.Flags().ShorthandLookup("n"); f == nil || f.Name != "dry-run" {
		t.Errorf("-n resolves to %v, want the root --dry-run", f)
	}
}

// --output is this command's destination directory, which costs it the root -o.
// 'pro backup' made the same trade, so the name stays — but the help has to say so,
// because the documented CI mechanism JAMF_CLI_ARGS='-o json' exits 2 here.
func TestProtectBackupHelpExplainsTheLostOutputShorthand(t *testing.T) {
	backup := findSubcommand(findProtectCmd(t), "backup")
	if backup == nil {
		t.Fatal("backup subcommand not found")
	}
	if backup.Flags().Lookup("output") == nil {
		t.Fatal("--output not defined on backup")
	}
	if !strings.Contains(backup.Long, "-o") {
		t.Error("backup's help must say the global -o output format flag is unavailable here")
	}
}

// A restore that half-worked must be observable the same way a backup that
// half-worked is: PartialFailure (7) rather than General (1), and downgradable
// with the same --allow-partial-failure. Returning a plain error ignored both.
func TestRunProtectRestoreExitsPartialOnMixedResults(t *testing.T) {
	seed := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		sub := filepath.Join(dir, "unified-logging-filters")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sub, "zz-good.yaml"),
			[]byte("name: zz-good\npredicate: 'subsystem == \"com.apple.TimeMachine\"'\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		// Not a document at all, so its Restore fails at decode.
		if err := os.WriteFile(filepath.Join(sub, "zz-bad.yaml"), []byte("\t- : ][\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	newCtx := func() *registry.CLIContext {
		return &registry.CLIContext{ProtectClient: &mockProtectClient{}}
	}

	t.Run("mixed results exit PartialFailure", func(t *testing.T) {
		err := runProtectRestore(context.Background(), newCtx(), seed(t), "unified-logging-filters", "", false, false, true)
		if err == nil {
			t.Fatal("expected a non-zero exit when a document failed to restore")
		}
		if got := exitcode.CodeFrom(err); got != exitcode.PartialFailure {
			t.Errorf("exit code = %v, want PartialFailure (%v) — 'protect backup' reports the same condition that way",
				got, exitcode.PartialFailure)
		}
	})

	t.Run("--allow-partial-failure downgrades to success", func(t *testing.T) {
		prev := allowPartialFailure
		allowPartialFailure = true
		defer func() { allowPartialFailure = prev }()

		if err := runProtectRestore(context.Background(), newCtx(), seed(t), "unified-logging-filters", "", false, false, true); err != nil {
			t.Fatalf("--allow-partial-failure should exit 0, got %v", err)
		}
	})
}

// Pruning reads .yaml, .yml and .json alike, so restore must too: a singleton
// written insights.yml was deleted by a backup run and ignored by a restore.
func TestCollectProtectRestoreFilesAcceptsYmlForSingletons(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "insights.yml"), []byte("enabled: []\ndisabled: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	selected, err := protectSelectResources("insights", "")
	if err != nil {
		t.Fatal(err)
	}
	files, _, err := collectProtectRestoreFiles(dir, selected, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("collected %d file(s), want 1 — a .yml singleton is prunable, so it must be restorable", len(files))
	}
}

// The accessGroup asymmetry is a property of the API, not of restore, so
// 'groups apply' has to honour it too — otherwise 'groups export | groups apply'
// still fails for a local access group with the raw server message the guard
// exists to replace.
func TestGroupsApplySharesTheAccessGroupGuard(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "group.yaml")
	if err := os.WriteFile(doc, []byte("name: zz-group\nconnection: \"\"\naccessGroup: true\nroles: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("already an access group: no update attempted", func(t *testing.T) {
		m := &mockProtectClient{groups: []jamfprotect.Group{{ID: "g-1", Name: "zz-group", AccessGroup: true}}}
		cmd := newProtectGroupsApplyCmd(&registry.CLIContext{ProtectClient: m, Output: mockOutput{}})
		cmd.SetArgs([]string{"--from-file", doc, "--yes"})
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("re-applying an unchanged local access group must not fail: %v", err)
		}
		if m.updatedGroups != 0 {
			t.Errorf("updateGroup was called %d time(s); the server refuses it for a local group", m.updatedGroups)
		}
	})

	t.Run("access disabled in the target: actionable error", func(t *testing.T) {
		m := &mockProtectClient{groups: []jamfprotect.Group{{ID: "g-1", Name: "zz-group", AccessGroup: false}}}
		cmd := newProtectGroupsApplyCmd(&registry.CLIContext{ProtectClient: m, Output: mockOutput{}})
		cmd.SetArgs([]string{"--from-file", doc, "--yes"})
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected the guard's error rather than the raw server message")
		}
		if !strings.Contains(err.Error(), "only at create") {
			t.Errorf("error %q should explain that update cannot do this", err)
		}
		if m.updatedGroups != 0 {
			t.Error("must not spend a call the server will refuse")
		}
	})
}
