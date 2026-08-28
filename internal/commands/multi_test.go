// Copyright 2026, Jamf Software LLC

package commands

import (
	"os"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
)

func testConfig() *config.Config {
	return &config.Config{
		Profiles: map[string]config.Profile{
			"default":      {URL: "https://main.jamfcloud.com", AuthMethod: "oauth2"},
			"pro-school1":  {URL: "https://school1.jamfcloud.com", AuthMethod: "oauth2"},
			"pro-school2":  {URL: "https://school2.jamfcloud.com", AuthMethod: "oauth2"},
			"pro-school3":  {URL: "https://school3.jamfcloud.com", AuthMethod: "oauth2"},
			"protect-test": {URL: "https://test.protect.jamfcloud.com", AuthMethod: "oauth2", Product: "protect"},
			"staging":      {URL: "https://staging.jamfcloud.com", AuthMethod: "oauth2"},
		},
	}
}

func TestSortedProfileNames(t *testing.T) {
	cfg := testConfig()
	names := sortedProfileNames(cfg)
	if len(names) != 6 {
		t.Fatalf("got %d names, want 6", len(names))
	}
	// Should be sorted
	for i := 1; i < len(names); i++ {
		if names[i] < names[i-1] {
			t.Errorf("names not sorted: %v", names)
			break
		}
	}
}

func TestFilterProfiles_GlobMatch(t *testing.T) {
	names := []string{"default", "pro-school1", "pro-school2", "pro-school3", "protect-test", "staging"}

	matched, err := filterProfiles(names, "pro-*")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matched) != 3 {
		t.Fatalf("got %d matches, want 3", len(matched))
	}
	for _, m := range matched {
		if !strings.HasPrefix(m, "pro-") {
			t.Errorf("unexpected match: %q", m)
		}
	}
}

func TestFilterProfiles_NoMatch(t *testing.T) {
	names := []string{"default", "pro-school1"}

	_, err := filterProfiles(names, "missing-*")
	if err == nil {
		t.Fatal("expected error for no matches")
		return
	}
	if !strings.Contains(err.Error(), "no profiles match") {
		t.Errorf("error = %q, want it to contain 'no profiles match'", err.Error())
	}
}

func TestFilterProfiles_InvalidPattern(t *testing.T) {
	names := []string{"default"}

	_, err := filterProfiles(names, "[invalid")
	if err == nil {
		t.Fatal("expected error for invalid pattern")
		return
	}
}

func TestValidateProfileNames_AllValid(t *testing.T) {
	cfg := testConfig()

	names, err := validateProfileNames(cfg, []string{"default", "pro-school1", "staging"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 3 {
		t.Fatalf("got %d names, want 3", len(names))
	}
}

func TestValidateProfileNames_Unknown(t *testing.T) {
	cfg := testConfig()

	_, err := validateProfileNames(cfg, []string{"default", "nonexistent"})
	if err == nil {
		t.Fatal("expected error for unknown profile")
		return
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want it to contain 'not found'", err.Error())
	}
}

func TestValidateProfileNames_Dedup(t *testing.T) {
	cfg := testConfig()

	names, err := validateProfileNames(cfg, []string{"default", "default", "pro-school1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("got %d names, want 2 (deduped)", len(names))
	}
}

func TestResolveURLToProfile_Found(t *testing.T) {
	cfg := testConfig()

	name, err := resolveURLToProfile(cfg, "https://school1.jamfcloud.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "pro-school1" {
		t.Errorf("got %q, want %q", name, "pro-school1")
	}
}

func TestResolveURLToProfile_FoundWithoutScheme(t *testing.T) {
	cfg := testConfig()

	name, err := resolveURLToProfile(cfg, "school2.jamfcloud.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "pro-school2" {
		t.Errorf("got %q, want %q", name, "pro-school2")
	}
}

func TestResolveURLToProfile_NotFound(t *testing.T) {
	cfg := testConfig()

	_, err := resolveURLToProfile(cfg, "https://unknown.jamfcloud.com")
	if err == nil {
		t.Fatal("expected error for unknown URL")
		return
	}
	if !strings.Contains(err.Error(), "no profile found") {
		t.Errorf("error = %q, want it to contain 'no profile found'", err.Error())
	}
}

func TestReadProfilesFromFile_ProfileNames(t *testing.T) {
	cfg := testConfig()

	f, err := os.CreateTemp("", "profiles-*.txt")
	if err != nil {
		t.Fatal(err)
		return
	}
	defer func() { _ = os.Remove(f.Name()) }()

	_, _ = f.WriteString("pro-school1\n")
	_, _ = f.WriteString("# comment\n")
	_, _ = f.WriteString("\n")
	_, _ = f.WriteString("pro-school2\n")
	_ = f.Close()

	names, err := readProfilesFromFile(cfg, f.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("got %d names, want 2", len(names))
	}
	if names[0] != "pro-school1" || names[1] != "pro-school2" {
		t.Errorf("names = %v, want [pro-school1 pro-school2]", names)
	}
}

func TestReadProfilesFromFile_URLs(t *testing.T) {
	cfg := testConfig()

	f, err := os.CreateTemp("", "urls-*.txt")
	if err != nil {
		t.Fatal(err)
		return
	}
	defer func() { _ = os.Remove(f.Name()) }()

	_, _ = f.WriteString("school1.jamfcloud.com\n")
	_, _ = f.WriteString("https://school3.jamfcloud.com\n")
	_ = f.Close()

	names, err := readProfilesFromFile(cfg, f.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("got %d names, want 2", len(names))
	}
	if names[0] != "pro-school1" || names[1] != "pro-school3" {
		t.Errorf("names = %v, want [pro-school1 pro-school3]", names)
	}
}

func TestReadProfilesFromFile_Dedup(t *testing.T) {
	cfg := testConfig()

	f, err := os.CreateTemp("", "dup-*.txt")
	if err != nil {
		t.Fatal(err)
		return
	}
	defer func() { _ = os.Remove(f.Name()) }()

	_, _ = f.WriteString("pro-school1\n")
	_, _ = f.WriteString("school1.jamfcloud.com\n") // same profile via URL
	_, _ = f.WriteString("pro-school1\n")           // exact duplicate
	_ = f.Close()

	names, err := readProfilesFromFile(cfg, f.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 1 {
		t.Fatalf("got %d names, want 1 (deduped)", len(names))
	}
}

func TestReadProfilesFromFile_UnknownURL(t *testing.T) {
	cfg := testConfig()

	f, err := os.CreateTemp("", "unknown-*.txt")
	if err != nil {
		t.Fatal(err)
		return
	}
	defer func() { _ = os.Remove(f.Name()) }()

	_, _ = f.WriteString("https://unknown.jamfcloud.com\n")
	_ = f.Close()

	_, err = readProfilesFromFile(cfg, f.Name())
	if err == nil {
		t.Fatal("expected error for unknown URL")
		return
	}
	if !strings.Contains(err.Error(), "no profile found") {
		t.Errorf("error = %q, want it to contain 'no profile found'", err.Error())
	}
}

func TestLooksLikeURL(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"https://school1.jamfcloud.com", true},
		{"school1.jamfcloud.com", true},
		{"http://localhost:8080", true},
		{"tenant.protect.jamfcloud.com", true},
		{"us.api.jamfcloud.com", true},
		{"pro-school1", false},
		{"default", false},
		{"v2.prod", false},          // dotted profile name — not a URL
		{"staging.internal", false}, // dotted profile name — not a URL
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := looksLikeURL(tt.input)
			if got != tt.want {
				t.Errorf("looksLikeURL(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeURLForMatch(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://School1.JamfCloud.com", "https://school1.jamfcloud.com"},
		{"school1.jamfcloud.com/", "https://school1.jamfcloud.com"},
		{"  school1.jamfcloud.com  ", "https://school1.jamfcloud.com"},
		{"http://localhost:8080", "http://localhost:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeURLForMatch(tt.input)
			if got != tt.want {
				t.Errorf("normalizeURLForMatch(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSplitAndTrim(t *testing.T) {
	got := splitAndTrim(" a , b , c , ", ",")
	if len(got) != 3 {
		t.Fatalf("got %d parts, want 3", len(got))
	}
	if got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("got %v, want [a b c]", got)
	}
}

func TestTokenizeSelection(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"1 2 3", []string{"1", "2", "3"}},
		{"1,2,3", []string{"1", "2", "3"}},
		{"1, 2, 3", []string{"1", "2", "3"}},
		{"1 3 5-8", []string{"1", "3", "5-8"}},
		{"1,3,5-8", []string{"1", "3", "5-8"}},
		{"  1  2  ", []string{"1", "2"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := tokenizeSelection(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("token[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseRange(t *testing.T) {
	start, end, ok := parseRange("3-7")
	if !ok {
		t.Fatal("expected ok for valid range")
		return
	}
	if start != 3 || end != 7 {
		t.Errorf("got %d-%d, want 3-7", start, end)
	}

	_, _, ok = parseRange("5")
	if ok {
		t.Error("expected !ok for non-range")
	}

	_, _, ok = parseRange("a-b")
	if ok {
		t.Error("expected !ok for non-numeric range")
	}
}

func TestDetectProduct(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"pro", "comp", "list"}, "pro"},
		{[]string{"protect", "plans", "list"}, "protect"},
		{[]string{"school", "devices", "list"}, "school"},
		{[]string{"config", "list"}, ""},
		{[]string{}, ""},
	}

	for _, tt := range tests {
		got := detectProduct(tt.args)
		if got != tt.want {
			t.Errorf("detectProduct(%v) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

func TestFilterByProduct_Pro(t *testing.T) {
	cfg := testConfig()
	all := sortedProfileNames(cfg)

	proOnly := filterByProduct(cfg, all, "pro")
	for _, name := range proOnly {
		if cfg.Profiles[name].Product == "protect" {
			t.Errorf("pro filter included protect profile %q", name)
		}
	}
	// Should exclude protect-test
	for _, name := range proOnly {
		if name == "protect-test" {
			t.Error("pro filter should not include protect-test")
		}
	}
}

func TestFilterByProduct_Protect(t *testing.T) {
	cfg := testConfig()
	all := sortedProfileNames(cfg)

	protectOnly := filterByProduct(cfg, all, "protect")
	if len(protectOnly) != 1 {
		t.Fatalf("got %d protect profiles, want 1", len(protectOnly))
	}
	if protectOnly[0] != "protect-test" {
		t.Errorf("got %q, want protect-test", protectOnly[0])
	}
}

func TestFilterByProduct_Empty(t *testing.T) {
	cfg := testConfig()
	all := sortedProfileNames(cfg)

	// Empty product should return all
	result := filterByProduct(cfg, all, "")
	if len(result) != len(all) {
		t.Errorf("got %d profiles, want %d (empty product should return all)", len(result), len(all))
	}
}

// --- resolveMultiProfiles integration tests ---

func TestResolveMultiProfiles_FilterWithProductFilter(t *testing.T) {
	cfg := testConfig()

	// --filter '*' with product=pro should exclude protect-test
	names, err := resolveMultiProfiles(cfg, "*", "", "", "pro", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, name := range names {
		if name == "protect-test" {
			t.Error("product filter should have excluded protect-test")
		}
	}
	// Should have 5 pro profiles (all except protect-test)
	if len(names) != 5 {
		t.Errorf("got %d profiles, want 5", len(names))
	}
}

func TestResolveMultiProfiles_FilterWithProductProtect(t *testing.T) {
	cfg := testConfig()

	// --filter '*' with product=protect should only return protect profiles
	names, err := resolveMultiProfiles(cfg, "*", "", "", "protect", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 1 || names[0] != "protect-test" {
		t.Errorf("got %v, want [protect-test]", names)
	}
}

func TestResolveMultiProfiles_ExplicitProfilesSkipProductFilter(t *testing.T) {
	cfg := testConfig()

	// --profiles with explicit names should NOT be filtered by product
	names, err := resolveMultiProfiles(cfg, "", "protect-test,pro-school1", "", "pro", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should include both — explicit profiles are trusted
	if len(names) != 2 {
		t.Errorf("got %d profiles, want 2 (explicit profiles skip product filter)", len(names))
	}
}

func TestResolveMultiProfiles_NoMatchingProduct(t *testing.T) {
	// Config with only pro profiles
	cfg := &config.Config{
		Profiles: map[string]config.Profile{
			"default": {URL: "https://main.jamfcloud.com", AuthMethod: "oauth2"},
		},
	}

	_, err := resolveMultiProfiles(cfg, "*", "", "", "protect", true)
	if err == nil {
		t.Fatal("expected error when no profiles match product")
		return
	}
	if !strings.Contains(err.Error(), "no matching") {
		t.Errorf("error = %q, want it to contain 'no matching'", err.Error())
	}
}

func TestResolveMultiProfiles_EmptyConfig(t *testing.T) {
	cfg := &config.Config{Profiles: map[string]config.Profile{}}

	_, err := resolveMultiProfiles(cfg, "*", "", "", "", true)
	if err == nil {
		t.Fatal("expected error for empty config")
		return
	}
	if !strings.Contains(err.Error(), "no profiles configured") {
		t.Errorf("error = %q, want it to contain 'no profiles configured'", err.Error())
	}
}

func TestResolveMultiProfiles_NoInput(t *testing.T) {
	cfg := testConfig()

	// No flags + noInput=true should error
	_, err := resolveMultiProfiles(cfg, "", "", "", "", true)
	if err == nil {
		t.Fatal("expected error for no flags with --no-input")
		return
	}
	if !strings.Contains(err.Error(), "--filter") {
		t.Errorf("error = %q, want it to mention --filter", err.Error())
	}
}
