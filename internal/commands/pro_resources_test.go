// Copyright 2026, Jamf Software LLC

package commands

import (
	"slices"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/commands/pro/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
)

func TestBackupResources_NotEmpty(t *testing.T) {
	if len(BackupResources) == 0 {
		t.Fatal("BackupResources should not be empty")
	}
}

// TestBackupResources_AllKeysRegistered is the drift-catching test: every
// curated entry must point at an endpoint that exists in the generated
// registry. A spec rename or deletion surfaces here after `make generate`
// rather than as a silent runtime failure.
func TestBackupResources_AllKeysRegistered(t *testing.T) {
	for _, r := range BackupResources {
		if r.Key == "" {
			t.Errorf("BackupResource with FilterName=%q has empty Key", r.FilterName)
			continue
		}
		ep, ok := generated.BackupEndpoints[r.Key]
		if !ok {
			t.Errorf("BackupResource %q (filter=%q) references unknown endpoint key", r.Key, r.FilterName)
			continue
		}
		if ep.ListPath == "" {
			t.Errorf("endpoint %q has empty ListPath", r.Key)
		}
		if ep.GetPath == "" && !r.ListOnly {
			t.Errorf("endpoint %q has empty GetPath but is not marked ListOnly", r.Key)
		}
		if ep.IsClassic && ep.WrapperKey == "" && ep.ListSubset == "" {
			t.Errorf("classic endpoint %q missing both WrapperKey and ListSubset", r.Key)
		}
		if r.SubDir == "" {
			t.Errorf("BackupResource %q has empty SubDir", r.Key)
		}
	}
}

func TestBackupResources_SubDirsUnique(t *testing.T) {
	seen := make(map[string]string, len(BackupResources))
	for _, r := range BackupResources {
		if prev, ok := seen[r.SubDir]; ok {
			t.Errorf("duplicate SubDir %q: %q and %q", r.SubDir, prev, r.Key)
		}
		seen[r.SubDir] = r.Key
	}
}

func TestBackupResources_PreferModernOverClassic(t *testing.T) {
	// Resources with modern equivalents that should NOT appear as classic in
	// the curated list. Keep this list in sync with the generator's modern
	// coverage — it documents the "prefer modern" policy in a checkable way.
	forbidden := map[string]string{
		"classic-computer-ext-attrs": "computer-extension-attributes",
	}
	for _, r := range BackupResources {
		if modern, ok := forbidden[r.Key]; ok {
			t.Errorf("BackupResource %q shadows modern %q — prefer modern API", r.Key, modern)
		}
	}
}

func TestResolveBackupResources_NoFilter(t *testing.T) {
	got, err := ResolveBackupResources(nil)
	if err != nil {
		t.Fatalf("ResolveBackupResources: %v", err)
	}
	if len(got) != len(BackupResources) {
		t.Errorf("expected %d resolved entries, got %d", len(BackupResources), len(got))
	}
}

func TestResolveBackupResources_WithFilter(t *testing.T) {
	got, err := ResolveBackupResources([]string{"policies", "scripts"})
	if err != nil {
		t.Fatalf("ResolveBackupResources: %v", err)
	}

	for _, r := range got {
		if r.FilterName != "policies" && r.FilterName != "scripts" {
			t.Errorf("unexpected FilterName %q in filtered results", r.FilterName)
		}
	}

	if len(got) == 0 {
		t.Error("expected at least one result for policies+scripts filter")
	}
}

func TestResolveBackupResources_NoMatch(t *testing.T) {
	got, err := ResolveBackupResources([]string{"nonexistent"})
	if err != nil {
		t.Fatalf("ResolveBackupResources: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 results for nonexistent filter, got %d", len(got))
	}
}

func TestResolveBackupResources_AccountsSplit(t *testing.T) {
	// The accounts filter must resolve to both the users + groups subsets so
	// backup doesn't silently drop half the admin config.
	got, err := ResolveBackupResources([]string{"accounts"})
	if err != nil {
		t.Fatalf("ResolveBackupResources: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 resolved entries for accounts filter, got %d", len(got))
	}
	subdirs := []string{got[0].SubDir, got[1].SubDir}
	slices.Sort(subdirs)
	want := []string{"accounts/groups", "accounts/users"}
	if !slices.Equal(subdirs, want) {
		t.Errorf("accounts subdirs = %v, want %v", subdirs, want)
	}
	for _, r := range got {
		if r.ListSubset == "" {
			t.Errorf("accounts entry %q missing ListSubset", r.Key)
		}
	}
}

func TestResolveBackupResources_PrestagesWithScope(t *testing.T) {
	// The prestages filter must resolve to both computer + mobile prestages,
	// each carrying a per-ID scope endpoint so device assignments are embedded.
	got, err := ResolveBackupResources([]string{"prestages"})
	if err != nil {
		t.Fatalf("ResolveBackupResources: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 resolved entries for prestages filter, got %d", len(got))
	}
	for _, r := range got {
		if r.ScopePath == "" {
			t.Errorf("prestages entry %q missing ScopePath", r.Key)
		}
		if !strings.Contains(r.ScopePath, "{id}") {
			t.Errorf("prestages entry %q ScopePath %q lacks {id} placeholder", r.Key, r.ScopePath)
		}
	}
}

func TestBackupFilterNames(t *testing.T) {
	names := BackupFilterNames()
	if len(names) == 0 {
		t.Fatal("BackupFilterNames should not be empty")
	}
	// Must contain both curated and non-standard filter tokens.
	required := []string{
		"policies", "profiles", "scripts", "extension-attributes", "accounts",
		"mac-apps", "mobile-apps",
		"inventory-preloads", "blueprints", "compliance-benchmarks",
	}
	for _, r := range required {
		if !slices.Contains(names, r) {
			t.Errorf("BackupFilterNames missing %q (got %v)", r, names)
		}
	}
}

// TestResolveBackupResources_AdvancedSearches pins the split across two APIs.
// There is no modern advanced-computer-searches spec, so the computer half must
// stay classic while the mobile half stays modern; a future modern spec is
// welcome to flip the computer key, but it must not silently drop either half.
func TestResolveBackupResources_AdvancedSearches(t *testing.T) {
	got, err := ResolveBackupResources([]string{"advanced-searches"})
	if err != nil {
		t.Fatalf("ResolveBackupResources: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 resolved entries for advanced-searches filter, got %d", len(got))
	}

	var keys, subdirs []string
	for _, r := range got {
		keys = append(keys, r.Key)
		subdirs = append(subdirs, r.SubDir)
	}
	slices.Sort(keys)
	slices.Sort(subdirs)

	wantKeys := []string{"advanced-mobile-device-searches", "classic-advanced-computer-searches"}
	if !slices.Equal(keys, wantKeys) {
		t.Errorf("advanced-searches keys = %v, want %v — fix BackupResources in pro_resources.go", keys, wantKeys)
	}
	wantSubDirs := []string{"advanced-searches/computers", "advanced-searches/mobile"}
	if !slices.Equal(subdirs, wantSubDirs) {
		t.Errorf("advanced-searches subdirs = %v, want %v", subdirs, wantSubDirs)
	}
}

// TestBackupResourceRows_EverySourceIsDerived sweeps the live tables so a token
// added with no source fails here rather than rendering a blank column. A
// curated token derives its source from BackupEndpoint.IsClassic; a
// non-standard one has no endpoint to read, so it states Source in
// nonStandardBackupFilters.
func TestBackupResourceRows_EverySourceIsDerived(t *testing.T) {
	rows, err := backupResourceRows()
	if err != nil {
		t.Fatalf("backupResourceRows: %v", err)
	}

	tokens := BackupFilterNames()
	if len(tokens) == 0 {
		t.Fatal("BackupFilterNames is empty — the sweep below would pass vacuously")
	}
	if len(rows) != len(tokens) {
		t.Fatalf("backupResourceRows returned %d rows for %d tokens", len(rows), len(tokens))
	}

	seen := make(map[string]bool, len(rows))
	for i, row := range rows {
		name, _ := row["resource"].(string)
		if name == "" {
			t.Fatalf("row %d has no resource token", i)
		}
		seen[name] = true
		if s, _ := row["source"].(string); s == "" {
			t.Errorf("token %q has no source — a curated token needs an endpoint in "+
				"generated.BackupEndpoints, a non-standard one needs Source set in "+
				"nonStandardBackupFilters (pro_resources.go)", name)
		}
		if o, _ := row["objects"].(string); o == "" {
			t.Errorf("token %q has no objects note (pro_resources.go)", name)
		}
	}
	for _, want := range tokens {
		if !seen[want] {
			t.Errorf("token %q is accepted by --resources but backupResourceRows does not list it", want)
		}
	}
}

// TestBackupResourceRows_MixedTokenNamesBothAPIs guards the derivation itself:
// advanced-searches and extension-attributes each span both APIs, so a source
// that reported only the first endpoint's API would still look plausible.
func TestBackupResourceRows_MixedTokenNamesBothAPIs(t *testing.T) {
	rows, err := backupResourceRows()
	if err != nil {
		t.Fatalf("backupResourceRows: %v", err)
	}

	sources := make(map[string]string, len(rows))
	for _, row := range rows {
		name, _ := row["resource"].(string)
		src, _ := row["source"].(string)
		sources[name] = src
	}

	for _, token := range []string{"advanced-searches", "extension-attributes"} {
		if got := sources[token]; got != "classic api, pro api" {
			t.Errorf("source for %q = %q, want %q", token, got, "classic api, pro api")
		}
	}
	if got := sources["scripts"]; got != "pro api" {
		t.Errorf("source for %q = %q, want %q", "scripts", got, "pro api")
	}
	if got := sources["policies"]; got != "classic api" {
		t.Errorf("source for %q = %q, want %q", "policies", got, "classic api")
	}
}

// TestBackupResourceRowsForFormat_TwoShapes pins both halves of the split. The
// column-based formats carry resource and source, so resource wins the
// alphabetical column order; the structured ones carry objects too, so nothing
// parsing the output loses the backing commands.
func TestBackupResourceRowsForFormat_TwoShapes(t *testing.T) {
	rows, err := backupResourceRows()
	if err != nil {
		t.Fatalf("backupResourceRows: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("no rows — the assertions below would pass vacuously")
	}

	for _, format := range []string{"table", "csv", "plain"} {
		got := backupResourceRowsForFormat(rows, format)
		if len(got) != len(rows) {
			t.Errorf("%s: %d rows, want %d", format, len(got), len(rows))
		}
		for i, r := range got {
			if len(r) != 2 {
				t.Fatalf("%s row %d has %d keys, want 2: %v", format, i, len(r), r)
			}
			if _, ok := r["objects"]; ok {
				t.Fatalf("%s row %d still carries objects: %v", format, i, r)
			}
			if r["resource"] == nil || r["source"] == nil {
				t.Fatalf("%s row %d dropped a kept key: %v", format, i, r)
			}
		}
		if first := sortedKeysOf(got[0]); first[0] != "resource" {
			t.Errorf("%s columns = %v, want resource first", format, first)
		}
	}

	for _, format := range []string{"json", "yaml", "ndjson"} {
		got := backupResourceRowsForFormat(rows, format)
		if len(got) != len(rows) {
			t.Errorf("%s: %d rows, want %d", format, len(got), len(rows))
		}
		for i, r := range got {
			if len(r) != 3 {
				t.Fatalf("%s row %d has %d keys, want all 3: %v", format, i, len(r), r)
			}
			if r["objects"] == nil {
				t.Fatalf("%s row %d lost objects: %v", format, i, r)
			}
		}
	}
}

// TestBackupResourceRowsForFormat_UnrecognisedFormatsAreNarrowed pins the
// polarity of the switch. internal/output normalises no case and its dispatch
// renders a table for every value it does not recognise, so matching table, csv
// and plain exactly sent each value below down the keep-everything arm: objects
// came back as the leading column of a 137-character table at exit 0, with no
// warning, which is the layout the split exists to remove.
func TestBackupResourceRowsForFormat_UnrecognisedFormatsAreNarrowed(t *testing.T) {
	rows, err := backupResourceRows()
	if err != nil {
		t.Fatalf("backupResourceRows: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("no rows — the assertions below would pass vacuously")
	}

	for _, format := range []string{"", "Table", "TABLE", "tabel", "plaintext", "bogus", "CSV", "Plain", "JSON"} {
		got := backupResourceRowsForFormat(rows, format)
		if len(got) != len(rows) {
			t.Errorf("%q: %d rows, want %d", format, len(got), len(rows))
		}
		for i, r := range got {
			if len(r) != 2 {
				t.Fatalf("%q row %d has %d keys, want 2: %v", format, i, len(r), r)
			}
			if _, ok := r["objects"]; ok {
				t.Fatalf("%q row %d still carries objects: %v", format, i, r)
			}
		}
	}
}

// TestBackupResourceRowsForFormat_DataFormatsKeepObjects sweeps the whole
// keep-everything arm, which adds xml and raw to the three the two-shapes test
// names. PrintRaw hands raw the bytes verbatim and xml straight to PrintBytes,
// neither of which runs the JSON-to-rows conversion, so a caller asking for
// either wants the field rather than a narrower table. The constants come from
// internal/output so the two sides of the same decision cannot drift.
func TestBackupResourceRowsForFormat_DataFormatsKeepObjects(t *testing.T) {
	rows, err := backupResourceRows()
	if err != nil {
		t.Fatalf("backupResourceRows: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("no rows — the assertions below would pass vacuously")
	}

	formats := []output.Format{
		output.FormatJSON, output.FormatYAML, output.FormatNDJSON,
		output.FormatXML, output.FormatRaw,
	}
	for _, format := range formats {
		got := backupResourceRowsForFormat(rows, string(format))
		if len(got) != len(rows) {
			t.Errorf("%s: %d rows, want %d", format, len(got), len(rows))
		}
		for i, r := range got {
			if len(r) != 3 {
				t.Fatalf("%s row %d has %d keys, want all 3: %v", format, i, len(r), r)
			}
			if r["objects"] == nil {
				t.Fatalf("%s row %d lost objects: %v", format, i, r)
			}
		}
	}
}

// TestBackupResourceRowsForFormat_JSONMultiIsNarrowed pins the one format whose
// name argues for the other arm. json-multi means JSON on the wire and a table
// on the screen: `multi` captures every display format as json-multi and
// re-renders the merged rows, and Print's switch has no case for it, so it
// reaches printTable. Keeping the column for it therefore put the buried
// 137-character layout back on a terminal by way of `multi`, which is the only
// route left after the narrowing above. selectTableColumns
// (generated/registry.go) excludes it from its own keep-set for this reason.
func TestBackupResourceRowsForFormat_JSONMultiIsNarrowed(t *testing.T) {
	rows, err := backupResourceRows()
	if err != nil {
		t.Fatalf("backupResourceRows: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("no rows — the assertions below would pass vacuously")
	}

	for i, r := range backupResourceRowsForFormat(rows, string(output.FormatJSONMulti)) {
		if len(r) != 2 {
			t.Fatalf("json-multi row %d has %d keys, want 2: %v", i, len(r), r)
		}
		if _, ok := r["objects"]; ok {
			t.Fatalf("json-multi row %d still carries objects: %v", i, r)
		}
	}
}

// sortedKeysOf mirrors the column order internal/output derives for a table
// row: id and name float, everything else is alphabetical. Neither key here is
// one of the two that float, so plain sorting is the same answer.
func sortedKeysOf(row map[string]any) []string {
	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
