// Copyright 2026, Jamf Software LLC

package commands

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
)

// The MCP server pins the instance/credentials at launch (the profile passed
// to `mcp serve`). A connecting model must not be able to redirect to a
// different instance or swap credentials by smuggling those flags into the
// run_command args array. buildChildArgs enforces that boundary.

func TestBuildChildArgs_RejectsEmptyArgs(t *testing.T) {
	if _, err := buildChildArgs("prod", nil); err == nil {
		t.Fatal("expected an error for empty args, got nil")
	}
}

func TestBuildChildArgs_RejectsInstanceAndCredentialFlags(t *testing.T) {
	blocked := [][]string{
		{"-p", "other"},
		{"--profile", "other"},
		{"--profile=other"},
		{"--url", "https://evil.example.com"},
		{"--url=https://evil.example.com"},
		{"--token-file", "/tmp/tok"},
		{"--token-file=/tmp/tok"},
		{"--tenant-id", "999"},
		{"--tenant-id=999"},
	}
	for _, override := range blocked {
		args := append([]string{"pro", "computers", "list"}, override...)
		if _, err := buildChildArgs("prod", args); err == nil {
			t.Errorf("expected args %v to be rejected, got nil error", args)
		}
	}
}

func TestBuildChildArgs_InjectsServerProfileAndNoInput(t *testing.T) {
	got, err := buildChildArgs("prod", []string{"pro", "computers", "list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"--profile", "prod", "--no-input", "pro", "computers", "list"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildChildArgs_OmitsProfileWhenServerProfileEmpty(t *testing.T) {
	got, err := buildChildArgs("", []string{"pro", "computers", "list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"--no-input", "pro", "computers", "list"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildChildArgs_NormalizesModelNoInput(t *testing.T) {
	// A model-supplied --no-input is dropped and the server's own injected once,
	// so --no-input appears exactly once regardless of what the model passed.
	got, err := buildChildArgs("", []string{"pro", "computers", "list", "--no-input"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"--no-input", "pro", "computers", "list"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildChildArgs_EnforcesNoInputOverModelOverride(t *testing.T) {
	// A model must not be able to re-enable prompting by passing --no-input=false.
	got, err := buildChildArgs("", []string{"pro", "computers", "list", "--no-input=false"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var noInputCount int
	for _, a := range got {
		if a == "--no-input=false" {
			t.Errorf("--no-input=false should be dropped, got %v", got)
		}
		if a == "--no-input" {
			noInputCount++
		}
	}
	if noInputCount != 1 {
		t.Errorf("expected exactly one enforced --no-input, got %d in %v", noInputCount, got)
	}
}

func TestBuildChildArgs_RejectsProfileShorthandForms(t *testing.T) {
	// pflag accepts the -p shorthand attached (-pProd) or clustered after
	// value-less bool shorthands (-np Prod, -qpProd); all set --profile.
	blocked := [][]string{
		{"-pProd"},
		{"-np", "Prod"},
		{"-qpProd"},
	}
	for _, override := range blocked {
		args := append([]string{"pro", "computers", "list"}, override...)
		if _, err := buildChildArgs("prod", args); err == nil {
			t.Errorf("expected args %v to be rejected, got nil error", args)
		}
	}
}

func TestBuildChildArgs_AllowsBenignShortFlags(t *testing.T) {
	// Short flags that don't carry the profile shorthand must pass through.
	got, err := buildChildArgs("", []string{"pro", "computers", "list", "-q", "-o", "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(got, " ")
	for _, want := range []string{"-q", "-o", "json"} {
		if !strings.Contains(joined, want) {
			t.Errorf("benign flag %q should pass through, got %v", want, got)
		}
	}
}

func TestBuildChildArgs_RejectsOutFile(t *testing.T) {
	for _, override := range [][]string{{"--out-file", "/tmp/x"}, {"--out-file=/tmp/x"}} {
		args := append([]string{"pro", "computers", "list"}, override...)
		if _, err := buildChildArgs("prod", args); err == nil {
			t.Errorf("expected --out-file %v to be rejected, got nil error", args)
		}
	}
}

func TestBuildChildArgs_AllowsDestructiveWithYes(t *testing.T) {
	// Full surface is intentional: destructive commands are reachable, gated by
	// --yes (and --no-input makes an unconfirmed one fail fast rather than hang).
	// buildChildArgs must not block them.
	got, err := buildChildArgs("prod", []string{"pro", "computers", "delete", "--id", "5", "--yes"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "delete") || !strings.Contains(joined, "--yes") {
		t.Errorf("destructive command should pass through unchanged, got %v", got)
	}
}

// The MCP report path has no filename parameter. reportFileName is what makes
// that true in practice: it derives the name from the pinned profile and a UTC
// timestamp, and takes no title, so a model-supplied title — which may echo an
// admin-controlled device or policy name — cannot reach a path at all.

func TestReportFileName_DerivesFromProfileAndTimestamp(t *testing.T) {
	now := time.Date(2026, 8, 28, 10, 43, 0, 0, time.UTC)
	got := reportFileName("prod", now)
	want := "jamf-report-prod-20260828T104300Z.html"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReportFileName_UsesDefaultWhenNoProfilePinned(t *testing.T) {
	now := time.Date(2026, 8, 28, 10, 43, 0, 0, time.UTC)
	got := reportFileName("", now)
	want := "jamf-report-default-20260828T104300Z.html"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReportFileName_NormalizesToUTC(t *testing.T) {
	// The timestamp segment is UTC regardless of the server's local zone, so two
	// reports from different hosts sort together.
	zone := time.FixedZone("UTC+10", 10*60*60)
	got := reportFileName("prod", time.Date(2026, 8, 28, 20, 43, 0, 0, zone))
	want := "jamf-report-prod-20260828T104300Z.html"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReportFileName_StaysInsideReportDir(t *testing.T) {
	// A profile name is administrator-supplied, so it gets the same treatment a
	// Protect object name does: whatever it contains, the result is one path
	// segment that joins inside the report directory.
	now := time.Date(2026, 8, 28, 10, 43, 0, 0, time.UTC)
	for _, prof := range []string{"../../etc", "a/b", "..", ".", "with space", "nul\x00byte", "~", "-"} {
		name := reportFileName(prof, now)
		if strings.ContainsAny(name, `/\`) {
			t.Errorf("profile %q produced a name with a separator: %q", prof, name)
		}
		if strings.Contains(name, "\x00") {
			t.Errorf("profile %q produced a name with a NUL: %q", prof, name)
		}
		joined := filepath.Join("/reports", name)
		if filepath.Dir(joined) != "/reports" {
			t.Errorf("profile %q escaped the report dir: %q", prof, joined)
		}
		if !strings.HasSuffix(name, ".html") {
			t.Errorf("profile %q produced %q, want a .html suffix", prof, name)
		}
	}
}

// The MCP report path has no destination parameter, so an unusable report-dir is
// a refusal rather than something to work around. In particular a missing
// directory is not created: a typo'd report-dir silently materialising a
// directory tree is worse than an error, and `pro setup --report-dir` already
// does the MkdirAll when the administrator names one.

func TestResolveReportDir_RefusesWhenUnset(t *testing.T) {
	_, err := resolveReportDir(&config.Config{})
	if err == nil {
		t.Fatal("expected a refusal when report-dir is unset, got nil")
	}
	if !strings.Contains(err.Error(), "pro setup --report-dir") {
		t.Errorf("refusal must name the command that sets it, got: %v", err)
	}
	// `config` has no `set` subcommand; naming one that does not exist is worse
	// than naming none.
	if strings.Contains(err.Error(), "config set") {
		t.Errorf("refusal must not name a nonexistent command, got: %v", err)
	}
}

func TestResolveReportDir_RefusesNilConfig(t *testing.T) {
	if _, err := resolveReportDir(nil); err == nil {
		t.Fatal("expected a refusal for a nil config, got nil")
	}
}

func TestResolveReportDir_RefusesMissingDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-created")
	_, err := resolveReportDir(&config.Config{ReportDir: missing})
	if err == nil {
		t.Fatal("expected a refusal for a missing directory, got nil")
	}
	if _, statErr := os.Stat(missing); statErr == nil {
		t.Error("a missing report-dir must be refused, not created")
	}
}

func TestResolveReportDir_RefusesNonDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "report-dir")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := resolveReportDir(&config.Config{ReportDir: file})
	if err == nil {
		t.Fatal("expected a refusal when report-dir names a file, got nil")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("refusal should say what is wrong, got: %v", err)
	}
}

func TestResolveReportDir_AcceptsExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	got, err := resolveReportDir(&config.Config{ReportDir: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != dir {
		t.Errorf("got %q, want %q", got, dir)
	}
}

func TestCreateReportFile_CreatesInsideReportDir(t *testing.T) {
	dir := t.TempDir()
	f, err := createReportFile(dir, "jamf-report-prod-20260828T104300Z.html")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer f.Close()

	if filepath.Dir(f.Name()) != dir {
		t.Errorf("file created at %q, want it inside %q", f.Name(), dir)
	}
	info, err := os.Stat(f.Name())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}
}

func TestCreateReportFile_CollisionIsAnError(t *testing.T) {
	// O_EXCL: two reports generated inside the same second must not have the
	// second silently overwrite the first.
	dir := t.TempDir()
	name := "jamf-report-prod-20260828T104300Z.html"

	first, err := createReportFile(dir, name)
	if err != nil {
		t.Fatalf("unexpected error on first create: %v", err)
	}
	if _, err := first.WriteString("<html>first</html>"); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := createReportFile(dir, name); err == nil {
		t.Fatal("expected a collision to error, got nil")
	}

	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "<html>first</html>" {
		t.Errorf("the existing report was modified: %q", string(data))
	}
}
