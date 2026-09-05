// Copyright 2026, Jamf Software LLC

package commands

import (
	"go/ast"
	"go/parser"
	gotoken "go/token"
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

	// #341 asks for these by default, and everything above is the
	// explicit-filter path. TestResolveBackupResources_NoFilter only compares
	// lengths, which is a tautology against whatever the table holds, so
	// without this a default-set exclusion would pass the whole suite.
	all, err := ResolveBackupResources(nil)
	if err != nil {
		t.Fatalf("ResolveBackupResources(nil): %v", err)
	}
	for _, want := range wantKeys {
		if !slices.ContainsFunc(all, func(r ResolvedBackupResource) bool { return r.Key == want }) {
			t.Errorf("%q is absent from the default (unfiltered) backup set", want)
		}
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

// TestNonStandardBackupFilters_SourcesArePinned holds each hand-written Source
// to its filter name. These three are the only source values in the listing
// with no derivation behind them, so nothing else in the suite would notice one
// moving: TestBackupResourceRows_EverySourceIsDerived only asserts they are
// non-empty, and TestBackupResourceRows_MixedTokenNamesBothAPIs pins exact
// strings only for the derived tokens. Swapping "csv download" and
// "platform sdk" between inventory-preloads and blueprints passed the whole
// suite.
//
// Each value names the real mechanism a resource is read through, so a swap
// misreports it in the one place an operator goes to look it up.
func TestNonStandardBackupFilters_SourcesArePinned(t *testing.T) {
	want := map[string]string{
		"inventory-preloads":    "csv download", // /v2/inventory-preload/csv
		"blueprints":            "platform sdk", // blueprintToExport
		"compliance-benchmarks": "platform sdk", // benchmarkToExport
	}

	if len(nonStandardBackupFilters) != len(want) {
		t.Fatalf("%d non-standard filters, %d pinned — add the new one here with the mechanism it reads through", len(nonStandardBackupFilters), len(want))
	}
	for _, n := range nonStandardBackupFilters {
		w, ok := want[n.FilterName]
		if !ok {
			t.Errorf("filter %q is not pinned here", n.FilterName)
			continue
		}
		if n.Source != w {
			t.Errorf("filter %q source = %q, want %q", n.FilterName, n.Source, w)
		}
	}
}

// TestBackupAndDiffDropTheSameKeys holds the two halves of DropKeys together.
// A resource whose backup omits a key while its diff keeps it reports that key
// as permanently modified — the exact symptom DropKeys exists to remove — so
// the drop is only correct if both call sites have it.
//
// Structural rather than behavioural, deliberately. The backup half is covered
// end to end by TestBackup_AdvancedComputerSearchDropsExecutedResults, but the
// diff half lives in loadSnapshotFromProfile, which loads config and resolves
// real auth before it fetches anything; standing that up to observe one deleted
// map key costs far more than it proves. Deleting the diff call site left the
// whole suite green, which is what this closes.
//
// The rule reads off StripServerFields rather than a file list: that call marks
// every place a fetched object is normalised for writing, so a fourth site
// added later is covered without editing this test.
func TestBackupAndDiffDropTheSameKeys(t *testing.T) {
	fset := gotoken.NewFileSet()
	found := 0
	for _, name := range []string{"pro_backup.go", "pro_diff.go"} {
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			if !isFunc {
				continue
			}
			strips, drops := false, false
			ast.Inspect(fn, func(n ast.Node) bool {
				call, isCall := n.(*ast.CallExpr)
				if !isCall {
					return true
				}
				if id, ok := call.Fun.(*ast.Ident); ok {
					switch id.Name {
					case "StripServerFields":
						strips = true
					case "dropResponseKeys":
						drops = true
					}
				}
				return true
			})
			if !strips {
				continue
			}
			found++
			if !drops {
				t.Errorf("%s: %s normalises a fetched object with StripServerFields but never calls dropResponseKeys, so a resource's executed output survives here and is reported as a permanent change", name, fn.Name.Name)
			}
		}
	}
	if found < 2 {
		t.Errorf("found %d normalising functions across pro_backup.go and pro_diff.go, want at least 2 — the walk is not seeing them, so it proves nothing", found)
	}
}
