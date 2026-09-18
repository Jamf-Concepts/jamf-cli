# HTML Reports over MCP — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a third MCP tool, `generate_report`, that writes a cross-product HTML fleet report into an administrator-designated directory and returns its path — and rewrite the `jamf-report` skill so it is followable in Claude Desktop, which has no Bash tool.

**Architecture:** `generate_report` re-invokes this same binary as `jamf-cli dashboard`, exactly as `run_command` does, but keeps the child's two streams apart: stdout is an `*os.File` opened inside the configured `report-dir`, stderr is a `bytes.Buffer` whose 4 KB tail becomes the tool result. The tool input carries no path, no filename and no profile — the filename is derived server-side from the pinned profile and a UTC timestamp — so the `--out-file` prohibition in `blockedChildFlags` is not widened, and there is no path parameter for a prompt-injected device name to steer.

**Tech Stack:** Go, `github.com/modelcontextprotocol/go-sdk/mcp`, `github.com/spf13/cobra`, stdlib `os`/`os/exec`/`bytes`/`time`/`path/filepath`.

**Spec:** `docs/superpowers/specs/2026-08-28-mcp-html-reports-design.md`

## Global Constraints

Every task's requirements implicitly include this section.

- **The tool input carries no path, no filename, and no profile.** The model chooses what the report says, never where it lands or which tenant it reads.
- **The filename is server-derived:** `jamf-report-<profile>-<UTC>.html`, timestamp layout exactly `20060102T150405Z`, e.g. `jamf-report-prod-20260828T104300Z.html`. The profile segment goes through `protectFileNameSafe` (`internal/commands/protect_backup.go:899`) and is `default` when the server was launched with no profile pinned.
- **The title never reaches the filename.** `reportFileName` takes no title parameter — the injection path is closed by construction, not sanitised.
- **The report file is opened `O_CREATE|O_EXCL`,** so a collision errors rather than overwriting.
- **The child is launched through `buildChildArgs`,** inheriting the pinned `--profile` and the enforced `--no-input` the model cannot disable. `blockedChildFlags` is unchanged; no code path may pass `--out-file`.
- **No `--include-profile` in the report arg vector.** An MCP report covers the pinned profile only.
- **stderr is truncated to its last 4 KB** before it reaches the result — the tail, not the head, because the last line explains the exit.
- **The result never contains the HTML.** Only path, byte size, and warnings.
- **A missing or non-directory `report-dir` is refused, never created.** `pro setup` does the `MkdirAll` when the administrator names it.
- **Refusals name `jamf-cli pro setup --report-dir <dir>`.** Never `config set report-dir` — `config` has no `set` subcommand.
- **`internal/commands/pro/generated/`, `platform/generated/` and `security/generated/` are never edited** (CLAUDE.md: Generated Code Boundary). This work touches none of them.
- **Never declare a local flag whose name matches a root persistent one** (CLAUDE.md: Conventions). This work adds no flags.
- **Never accept credentials via CLI flags or stdin** (CLAUDE.md: Credential Input Policy). `report-dir` is a directory path, not a credential, so `pro setup --report-dir` is permitted — already implemented in `bebf384`.

### Toolchain gap — read before Step 1 of any task

There is no working Go toolchain on this machine: `/usr/local/go/bin/go` is 1.13.4 and `go.mod` requires 1.26.6 (`malformed module path "cmp"`), and that gofmt cannot parse generics. Sections 1 and 2 of the spec (`bebf384`, `f6fda1c`, `c1a5033`) are already committed but compiler-unverified for this reason.

**Section 3 is boundary-enforcement code, which must be test-driven rather than eyeballed.** Before starting Task 1, run:

```bash
go version && go build ./... && go test ./internal/commands/... ./internal/config/...
```

If that does not succeed, **stop and report** — do not implement Tasks 1–6 blind. Install a Go ≥ 1.26.6 (e.g. `brew install go`, or a `golang.org/dl` shim) and confirm the baseline builds and the existing tests pass first. Task 7 (the skill rewrite) touches only Markdown and JSON and may proceed regardless.

---

## File Structure

| File | Disposition | Responsibility |
|---|---|---|
| `internal/commands/mcp.go` | Modify | Adds `generateReportInput`, `reportFileName`, `resolveReportDir`, `createReportFile`, `buildReportArgs`, `tailWarnings`, `runReportChild`, and the third `mcp.AddTool` registration. Existing `runChild`/`buildChildArgs`/`blockedChildFlags` are untouched. |
| `internal/commands/mcp_test.go` | Modify | Extends the output-boundary test file with the five new cases the spec's Testing section names. Existing ten `TestBuildChildArgs_*` cases retained verbatim. |
| `skills/skills/jamf-report/SKILL.md` | Rewrite Rule #1 + add a dashboard section | Two-branch route to `jamf-cli` (MCP tools if present, else Bash); the dashboard as a distinct shareable-artifact capability; the two MCP limits. |
| `skills/.claude-plugin/plugin.json` | Modify | `version` 0.2.0 → 0.3.0. |

`mcp.go` grows from 213 to roughly 350 lines and keeps one responsibility — the MCP server and its child-process boundary — so it is not split.

---

### Task 1: The filename deriver

The smallest unit, and the one carrying the injection defence. A pure function, so it is fully testable without a child process or a filesystem.

**Files:**
- Modify: `internal/commands/mcp.go` (add function + imports)
- Test: `internal/commands/mcp_test.go`

**Interfaces:**
- Consumes: `protectFileNameSafe(name string) string` from `internal/commands/protect_backup.go:899` — same package, no import needed.
- Produces: `reportFileName(serverProfile string, now time.Time) string`

- [ ] **Step 1: Write the failing test**

Append to `internal/commands/mcp_test.go`:

```go
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
```

Add to the `mcp_test.go` import block (which currently holds `reflect`, `strings`, `testing`):

```go
import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/commands/ -run 'TestReportFileName' -v`
Expected: FAIL — `undefined: reportFileName`

- [ ] **Step 3: Write the minimal implementation**

In `internal/commands/mcp.go`, add after `buildChildArgs` (end of file):

```go
// reportFileName derives the file name for a generated report from the profile
// the server was pinned to and the moment of generation.
//
// It deliberately takes no title. Device, policy and group names are
// administrator-controlled free text that flows into report content, so a model
// that could name the output file could be induced by a device called
// "../../.zshrc" to name one. Closing that by having no parameter is stronger
// than sanitising one. The profile segment still goes through
// protectFileNameSafe, since a profile name is free text too.
func reportFileName(serverProfile string, now time.Time) string {
	name := serverProfile
	if name == "" {
		// A name is what makes two reports in one directory tellable apart.
		name = "default"
	}
	return fmt.Sprintf("jamf-report-%s-%s.html",
		protectFileNameSafe(name), now.UTC().Format("20060102T150405Z"))
}
```

Add `"time"` to `mcp.go`'s import block, in the stdlib group:

```go
import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/commands/ -run 'TestReportFileName' -v`
Expected: PASS (4 tests)

- [ ] **Step 5: Run the whole package to check nothing regressed**

Run: `go test ./internal/commands/ && gofmt -l internal/commands/mcp.go internal/commands/mcp_test.go`
Expected: PASS, and `gofmt -l` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/commands/mcp.go internal/commands/mcp_test.go
git commit -m "feat(mcp): derive report file names from profile and timestamp

The name takes no title, so a model-supplied string cannot reach a path."
```

---

### Task 2: The report-directory refusals

All refusals happen before any child process starts. The handler has no `CLIContext` to read — `newMCPCmd()` and `newMCPServeCmd()` take no arguments — so it loads config itself.

**Files:**
- Modify: `internal/commands/mcp.go`
- Test: `internal/commands/mcp_test.go`

**Interfaces:**
- Consumes: `(*config.Config).ReportDirPath() string` from `internal/config/config.go:82` — nil-receiver safe, expands a leading `~`.
- Produces: `resolveReportDir(cfg *config.Config) (string, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/commands/mcp_test.go`:

```go
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
```

Add the config import to `mcp_test.go`:

```go
import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
)
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/commands/ -run 'TestResolveReportDir' -v`
Expected: FAIL — `undefined: resolveReportDir`

- [ ] **Step 3: Write the minimal implementation**

In `internal/commands/mcp.go`, add after `reportFileName`:

```go
// reportDirHint is the one way to set a report directory. `config` has no `set`
// subcommand (show, path, list, add-profile, remove-profile, set-default,
// validate), and naming a command that does not exist is worse than naming none.
const reportDirHint = "Set one with: jamf-cli pro setup --report-dir <dir>"

// resolveReportDir returns the directory reports are written to, or a refusal.
// Every failure here is returned before any child process starts. A missing
// directory is refused rather than created: `pro setup` does the MkdirAll when
// the administrator names one, and a typo'd report-dir silently materialising a
// directory tree is worse than an error.
func resolveReportDir(cfg *config.Config) (string, error) {
	dir := cfg.ReportDirPath()
	if dir == "" {
		return "", fmt.Errorf("no report directory is configured, and the MCP server has no destination parameter to fall back on. %s", reportDirHint)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("report directory %s is not accessible: %w. Create it, or choose another. %s", dir, err, reportDirHint)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("report directory %s is not a directory. %s", dir, reportDirHint)
	}
	return dir, nil
}
```

Add the config import to `mcp.go`:

```go
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/commands/ -run 'TestResolveReportDir' -v`
Expected: PASS (5 tests)

- [ ] **Step 5: Run the whole package**

Run: `go test ./internal/commands/ && gofmt -l internal/commands/mcp.go internal/commands/mcp_test.go`
Expected: PASS, no gofmt output.

- [ ] **Step 6: Commit**

```bash
git add internal/commands/mcp.go internal/commands/mcp_test.go
git commit -m "feat(mcp): refuse a report when report-dir is unset or unusable

Refused before any child starts, and a missing directory is never created."
```

---

### Task 3: Exclusive file creation

**Files:**
- Modify: `internal/commands/mcp.go`
- Test: `internal/commands/mcp_test.go`

**Interfaces:**
- Produces: `createReportFile(dir, name string) (*os.File, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/commands/mcp_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/commands/ -run 'TestCreateReportFile' -v`
Expected: FAIL — `undefined: createReportFile`

- [ ] **Step 3: Write the minimal implementation**

In `internal/commands/mcp.go`, add after `resolveReportDir`:

```go
// createReportFile opens the report file with O_EXCL, so a name collision — two
// reports generated inside the same second — errors rather than overwriting a
// report the administrator may already have shared. 0600 matches the 0700
// report directory `pro setup` creates.
func createReportFile(dir, name string) (*os.File, error) {
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("creating report file %s: %w", path, err)
	}
	return f, nil
}
```

Add `"path/filepath"` to `mcp.go`'s stdlib import group (between `"os/exec"` and `"strings"`).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/commands/ -run 'TestCreateReportFile' -v`
Expected: PASS (2 tests)

- [ ] **Step 5: Run the whole package**

Run: `go test ./internal/commands/ && gofmt -l internal/commands/mcp.go internal/commands/mcp_test.go`
Expected: PASS, no gofmt output.

- [ ] **Step 6: Commit**

```bash
git add internal/commands/mcp.go internal/commands/mcp_test.go
git commit -m "feat(mcp): open the report file O_EXCL so a collision cannot overwrite"
```

---

### Task 4: The report arg vector

The tool input type and the `dashboard` invocation it produces. This is where the spec's "the report child's arg vector carries the pinned `--profile` and `--no-input`, and no `--out-file`" is pinned.

**Files:**
- Modify: `internal/commands/mcp.go`
- Test: `internal/commands/mcp_test.go`

**Interfaces:**
- Consumes: `buildChildArgs(serverProfile string, args []string) ([]string, error)` from `internal/commands/mcp.go:188`. The `dashboard` command's flags are `--include-profile`, `--title`, `--smart-groups` (`internal/commands/dashboard.go:77-79`); `--smart-groups` is a repeatable `StringArrayVar`.
- Produces: `type generateReportInput struct { Title string; SmartGroups []string }` and `buildReportArgs(in generateReportInput) []string`

- [ ] **Step 1: Write the failing test**

Append to `internal/commands/mcp_test.go`:

```go
func TestBuildReportArgs_BareInvocation(t *testing.T) {
	// dashboard's --title already defaults to "Jamf Fleet Dashboard", so an
	// omitted title means "use the default", not "pass an empty one".
	got := buildReportArgs(generateReportInput{})
	want := []string{"dashboard"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildReportArgs_TitleAndRepeatedSmartGroups(t *testing.T) {
	got := buildReportArgs(generateReportInput{
		Title:       "Q3 Fleet Review",
		SmartGroups: []string{"All Laptops", "Executives"},
	})
	want := []string{
		"dashboard",
		"--title", "Q3 Fleet Review",
		"--smart-groups", "All Laptops",
		"--smart-groups", "Executives",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildReportArgs_SkipsBlankValues(t *testing.T) {
	got := buildReportArgs(generateReportInput{
		Title:       "   ",
		SmartGroups: []string{"", "  ", "All Laptops"},
	})
	want := []string{"dashboard", "--smart-groups", "All Laptops"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildReportArgs_NeverRedirectsOutput(t *testing.T) {
	// The whole design rests on the report child never being handed a
	// destination: stdout is a file the server opened, not a path the model
	// named. An --include-profile would also widen an MCP report past the pinned
	// profile.
	got := buildReportArgs(generateReportInput{
		Title:       "--out-file /etc/passwd",
		SmartGroups: []string{"--include-profile", "--out-file=/etc/passwd"},
	})
	for i, a := range got {
		if a == "--out-file" || strings.HasPrefix(a, "--out-file=") || a == "--include-profile" {
			t.Errorf("arg %d is a redirect flag: %v", i, got)
		}
	}
	// A model-supplied string that looks like a flag arrives as a flag *value*,
	// which is why it cannot become one: it is always preceded by its own flag.
	for i, a := range got {
		if strings.HasPrefix(a, "-") && i > 0 && !strings.HasPrefix(got[i-1], "--") {
			t.Errorf("arg %d %q is not positioned as a flag value: %v", i, a, got)
		}
	}
}

func TestBuildReportArgs_ThroughBuildChildArgsKeepsBoundary(t *testing.T) {
	// The report child goes through the same gate run_command does, so it
	// inherits the pinned profile and the enforced --no-input, and a title that
	// looks like a blocked flag is rejected rather than smuggled through.
	got, err := buildChildArgs("prod", buildReportArgs(generateReportInput{Title: "Q3 Fleet Review"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"--profile", "prod", "--no-input", "dashboard", "--title", "Q3 Fleet Review"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	for _, a := range got {
		if strings.HasPrefix(a, "--out-file") {
			t.Errorf("--out-file must never reach the report child: %v", got)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/commands/ -run 'TestBuildReportArgs' -v`
Expected: FAIL — `undefined: buildReportArgs`, `undefined: generateReportInput`

- [ ] **Step 3: Write the minimal implementation**

In `internal/commands/mcp.go`, add beside `runCommandInput` (after line 117):

```go
// generateReportInput is the whole of what a connecting model may choose about a
// report: what it says. No path, no filename, no profile — the destination and
// the tenant are pinned by the administrator at launch.
type generateReportInput struct {
	Title       string   `json:"title,omitempty" jsonschema:"report title shown in the HTML heading"`
	SmartGroups []string `json:"smart_groups,omitempty" jsonschema:"smart group names to visualize"`
}
```

And add after `createReportFile`:

```go
// buildReportArgs is the dashboard invocation for a model-requested report.
//
// No --include-profile: the server pins one profile at launch, so an MCP report
// covers that profile. Cross-product reports stay a CLI capability. No
// --out-file either — stdout is a file this server opened, which is what keeps
// the flag on blockedChildFlags.
func buildReportArgs(in generateReportInput) []string {
	args := []string{"dashboard"}
	// dashboard's --title already defaults, so an empty title means "use it".
	if strings.TrimSpace(in.Title) != "" {
		args = append(args, "--title", in.Title)
	}
	// --smart-groups is a repeatable StringArrayVar (dashboard.go:79).
	for _, g := range in.SmartGroups {
		if strings.TrimSpace(g) == "" {
			continue
		}
		args = append(args, "--smart-groups", g)
	}
	return args
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/commands/ -run 'TestBuildReportArgs' -v`
Expected: PASS (5 tests)

- [ ] **Step 5: Run the whole package**

Run: `go test ./internal/commands/ && gofmt -l internal/commands/mcp.go internal/commands/mcp_test.go`
Expected: PASS, no gofmt output.

- [ ] **Step 6: Commit**

```bash
git add internal/commands/mcp.go internal/commands/mcp_test.go
git commit -m "feat(mcp): build the report child's dashboard arg vector

Pinned profile only, no destination flag, through the same gate run_command uses."
```

---

### Task 5: Running the child with separated streams

This is why `generate_report` cannot reuse `runChild`: `runChild` calls `child.CombinedOutput()` (`internal/commands/mcp.go:131`), which would interleave `WARNING: patch compliance: …` into the middle of the HTML document and corrupt it.

**Files:**
- Modify: `internal/commands/mcp.go`
- Test: `internal/commands/mcp_test.go`

**Interfaces:**
- Consumes: `errorResult(text string) *mcp.CallToolResult` (`mcp.go:150`), plus Tasks 1–4's four functions and `config.Load()`.
- Produces: `tailWarnings(b []byte) string` and
  `runReportChild(ctx context.Context, executable, serverProfile string, in generateReportInput, now time.Time) *mcp.CallToolResult`

- [ ] **Step 1: Write the failing test**

Append to `internal/commands/mcp_test.go`:

```go
func TestTailWarnings_KeepsShortOutputVerbatim(t *testing.T) {
	in := []byte("WARNING: patch compliance: 403\n")
	if got := tailWarnings(in); got != string(in) {
		t.Errorf("got %q, want %q", got, string(in))
	}
}

func TestTailWarnings_KeepsTheTailNotTheHead(t *testing.T) {
	// A child that fails once per device must not spend the conversation's
	// context on repeated warnings — and the last line is the one that explains
	// the exit, so the tail is what survives.
	head := strings.Repeat("WARNING: device skipped\n", 1000)
	in := []byte(head + "FATAL: gateway refused the connection\n")
	got := tailWarnings(in)

	if len(got) > reportWarningTailBytes+128 {
		t.Errorf("truncated output is %d bytes, want ~%d", len(got), reportWarningTailBytes)
	}
	if !strings.Contains(got, "FATAL: gateway refused the connection") {
		t.Error("the last line must survive truncation")
	}
	if !strings.Contains(got, "omitted") {
		t.Error("truncation must be visible, not silent")
	}
}

func TestRunReportChild_RefusesBeforeStartingAChildWhenReportDirUnset(t *testing.T) {
	// An empty XDG_CONFIG_HOME means config.Load() returns a config with no
	// report-dir, which is the unconfigured administrator's state.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	res := runReportChild(context.Background(), "/nonexistent/jamf-cli", "prod",
		generateReportInput{}, time.Date(2026, 8, 28, 10, 43, 0, 0, time.UTC))

	if res == nil || !res.IsError {
		t.Fatalf("expected an error result, got %+v", res)
	}
	if !strings.Contains(mcpResultText(res), "pro setup --report-dir") {
		t.Errorf("refusal must name the command that sets it, got: %s", mcpResultText(res))
	}
}

func TestRunReportChild_RemovesThePartialFileWhenTheChildFails(t *testing.T) {
	// A non-zero exit leaves no half-written HTML behind: a truncated report is
	// not a smaller report, it is a corrupt file. Pointing `executable` at a
	// path that does not exist is the cheapest deterministic failure.
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	reportDir := t.TempDir()
	writeTestConfig(t, xdg, "report-dir: "+reportDir+"\n")

	res := runReportChild(context.Background(), filepath.Join(t.TempDir(), "absent-binary"),
		"prod", generateReportInput{}, time.Date(2026, 8, 28, 10, 43, 0, 0, time.UTC))

	if res == nil || !res.IsError {
		t.Fatalf("expected an error result, got %+v", res)
	}
	entries, err := os.ReadDir(reportDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("partial report left behind: %v", entries)
	}
}

func TestRunReportChild_ReportsPathAndSizeNeverTheHTML(t *testing.T) {
	// `/usr/bin/true` stands in for `jamf-cli dashboard`: it ignores the arg
	// vector, writes nothing, and exits 0 — so the report file is created and
	// left in place at zero bytes. The point under test is that the result
	// carries the path and the size and never the document.
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	reportDir := t.TempDir()
	writeTestConfig(t, xdg, "report-dir: "+reportDir+"\n")

	res := runReportChild(context.Background(), "/usr/bin/true", "",
		generateReportInput{}, time.Date(2026, 8, 28, 10, 43, 0, 0, time.UTC))

	if res == nil {
		t.Fatal("expected a result")
	}
	if res.IsError {
		t.Fatalf("expected success, got error result: %s", mcpResultText(res))
	}
	text := mcpResultText(res)
	want := filepath.Join(reportDir, "jamf-report-default-20260828T104300Z.html")
	if !strings.Contains(text, want) {
		t.Errorf("result must carry the path %q, got: %s", want, text)
	}
	if strings.Contains(text, "<html") || strings.Contains(text, "<!DOCTYPE") {
		t.Errorf("result must never carry the HTML, got: %s", text)
	}
}

// writeTestConfig writes a config.yaml into an XDG_CONFIG_HOME the test owns.
func writeTestConfig(t *testing.T, xdgDir, body string) {
	t.Helper()
	dir := filepath.Join(xdgDir, "jamf-cli")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// mcpResultText flattens a tool result's text content for assertions.
func mcpResultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
```

Add `"context"` and the mcp SDK to `mcp_test.go`'s imports:

```go
import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
)
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/commands/ -run 'TestTailWarnings|TestRunReportChild' -v`
Expected: FAIL — `undefined: tailWarnings`, `undefined: reportWarningTailBytes`, `undefined: runReportChild`

- [ ] **Step 3: Write the minimal implementation**

In `internal/commands/mcp.go`, add after `buildReportArgs`:

```go
// reportWarningTailBytes caps how much of the child's stderr reaches the model.
// The dashboard warns per partial failure, so a child failing once per device
// could otherwise spend the whole conversation's context on repeated warnings.
const reportWarningTailBytes = 4 << 10

// tailWarnings truncates the child's stderr to its last reportWarningTailBytes.
// The tail rather than the head, because the last line is the one that explains
// the exit.
func tailWarnings(b []byte) string {
	if len(b) <= reportWarningTailBytes {
		return string(b)
	}
	return "(earlier warnings omitted)\n" + string(b[len(b)-reportWarningTailBytes:])
}

// runReportChild generates an HTML report by re-invoking this binary as
// `jamf-cli dashboard` and returns its path, size, and warnings — never the
// HTML, which at 320–800 KB is 80k–200k tokens, and which truncation would
// turn into a corrupt file rather than a shorter report.
//
// This cannot reuse runChild: that calls CombinedOutput(), which would
// interleave the dashboard's partial-failure warnings into the middle of the
// HTML document. The two streams are kept apart — stdout is the report file,
// stderr is a buffer.
//
// It loads config itself because newMCPCmd and newMCPServeCmd take no
// arguments, so there is no CLIContext to thread.
func runReportChild(ctx context.Context, executable, serverProfile string, in generateReportInput, now time.Time) *mcp.CallToolResult {
	cfg, err := config.Load()
	if err != nil {
		return errorResult(fmt.Sprintf("reading config: %v", err))
	}
	dir, err := resolveReportDir(cfg)
	if err != nil {
		return errorResult(err.Error())
	}
	childArgs, err := buildChildArgs(serverProfile, buildReportArgs(in))
	if err != nil {
		return errorResult(err.Error())
	}

	f, err := createReportFile(dir, reportFileName(serverProfile, now))
	if err != nil {
		return errorResult(err.Error())
	}
	path := f.Name()

	var stderr bytes.Buffer
	child := exec.CommandContext(ctx, executable, childArgs...)
	child.Env = append(os.Environ(), "JAMF_CLI_MCP=1")
	child.Stdout = f
	child.Stderr = &stderr

	runErr := child.Run()
	closeErr := f.Close()
	warnings := strings.TrimSpace(tailWarnings(stderr.Bytes()))

	if runErr != nil {
		// A partial HTML document is not a smaller report.
		_ = os.Remove(path)
		text := fmt.Sprintf("report generation failed: %v", runErr)
		if warnings != "" {
			text += "\n\n" + warnings
		}
		return errorResult(text)
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return errorResult(fmt.Sprintf("writing report %s: %v", path, closeErr))
	}

	var size int64
	if info, statErr := os.Stat(path); statErr == nil {
		size = info.Size()
	}

	text := fmt.Sprintf("Report written to %s (%d bytes). Open or share that file; its contents are not returned here.", path, size)
	if warnings != "" {
		text += "\n\nWarnings during generation (some sections may be incomplete):\n" + warnings
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}
```

Add `"bytes"` to `mcp.go`'s stdlib import group, before `"context"`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/commands/ -run 'TestTailWarnings|TestRunReportChild' -v`
Expected: PASS (5 tests)

If `TestRunReportChild_ReportsPathAndSizeNeverTheHTML` fails because `/usr/bin/true` is absent on the platform, replace the executable argument with a path resolved at test time — `p, err := exec.LookPath("true")`, skipping via `t.Skipf` when it is not found — rather than weakening the assertion.

- [ ] **Step 5: Run the whole package**

Run: `go test ./internal/commands/ && gofmt -l internal/commands/mcp.go internal/commands/mcp_test.go`
Expected: PASS, no gofmt output.

- [ ] **Step 6: Commit**

```bash
git add internal/commands/mcp.go internal/commands/mcp_test.go
git commit -m "feat(mcp): write the report to a file and return its path, not its HTML

Keeps stdout and stderr apart so warnings cannot corrupt the document."
```

---

### Task 6: Register the third tool

**Files:**
- Modify: `internal/commands/mcp.go:31-41` (the `mcp` command's `Long`) and `internal/commands/mcp.go:88-99` (after the `run_command` registration)
- Test: `internal/commands/mcp_test.go`

**Interfaces:**
- Consumes: `runReportChild` from Task 5; `executable` and `serverProfile`, both already in scope inside `newMCPServeCmd`'s `RunE` (`mcp.go:65` and `mcp.go:71`).
- Produces: no new exported surface — the `generate_report` tool registration.

- [ ] **Step 1: Write the failing test**

Append to `internal/commands/mcp_test.go`:

```go
func TestNewMCPCmd_DocumentsGenerateReport(t *testing.T) {
	// The help text is the only place an administrator learns the tool exists
	// before wiring the server into a client, and it must not promise a
	// destination the tool does not accept.
	cmd := newMCPCmd()
	long := cmd.Long
	for _, want := range []string{"generate_report", "list_commands", "run_command", "report-dir"} {
		if !strings.Contains(long, want) {
			t.Errorf("mcp --help should mention %q, got:\n%s", want, long)
		}
	}

	var serve *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "serve" {
			serve = sub
		}
	}
	if serve == nil {
		t.Fatal("mcp serve subcommand missing")
	}
}
```

Add `"github.com/spf13/cobra"` to `mcp_test.go`'s third-party import group.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/commands/ -run 'TestNewMCPCmd_DocumentsGenerateReport' -v`
Expected: FAIL — `mcp --help should mention "generate_report"`

- [ ] **Step 3: Write the minimal implementation**

Replace `newMCPCmd`'s `Long` (`internal/commands/mcp.go:31-41`) with:

```go
		Long: `Serve jamf-cli's command tree to MCP-capable AI clients over stdio.

The connecting AI gets three tools:
  - list_commands   : the full command catalog (names, descriptions, flags)
  - run_command     : execute any jamf-cli command and get its output back
  - generate_report : write a shareable HTML fleet report and return its path

Commands run as child processes of this binary using the same profile this
'mcp serve' was started with (-p/--profile or JAMF_PROFILE), so credentials are
never passed over the protocol. Children run with --no-input, so commands that
would prompt (setup, unconfirmed destructive ops) fail fast instead of hanging;
the model must pass --yes to confirm a destructive command.

generate_report needs a report directory, which the connecting AI cannot
choose. Set one with: jamf-cli pro setup --report-dir <dir>`,
```

Insert after the `run_command` registration (after `mcp.go:99`, before the disconnect comment at line 101):

```go
			mcp.AddTool(server, &mcp.Tool{
				Name: "generate_report",
				Description: "Generate a shareable, self-contained HTML fleet report across " +
					"Jamf Pro, Protect, and Platform, and write it into the report directory " +
					"the administrator configured. Returns the file path, its size, and any " +
					"warnings — never the HTML, which is far too large for a conversation. " +
					"Use this when the administrator wants something to share or action " +
					"rather than read now; use run_command for questions answered by a table. " +
					"The destination and file name are server-derived and cannot be set per " +
					"call, and the report covers only the profile this server was started " +
					"with. After calling it, tell the administrator the path and summarize " +
					"what the report says.",
			}, func(ctx context.Context, _ *mcp.CallToolRequest, in generateReportInput) (*mcp.CallToolResult, any, error) {
				return runReportChild(ctx, executable, serverProfile, in, time.Now()), nil, nil
			})
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/commands/ -run 'TestNewMCPCmd_DocumentsGenerateReport' -v`
Expected: PASS

- [ ] **Step 5: Run the full suite and the build**

Run: `go build ./... && go test ./internal/commands/ ./internal/config/ && make lint`
Expected: build succeeds, all tests pass, lint clean.

- [ ] **Step 6: Smoke-test the tool end to end**

```bash
make build
mkdir -p /tmp/jamf-reports-smoke
XDG_CONFIG_HOME=$(mktemp -d) sh -c '
  mkdir -p "$XDG_CONFIG_HOME/jamf-cli"
  printf "report-dir: /tmp/jamf-reports-smoke\n" > "$XDG_CONFIG_HOME/jamf-cli/config.yaml"
  printf "%s\n%s\n" \
    "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2024-11-05\",\"capabilities\":{},\"clientInfo\":{\"name\":\"smoke\",\"version\":\"0\"}}}" \
    "{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}" \
  | bin/jamf-cli mcp serve
'
```

Expected: the `tools/list` response names all three tools, and `generate_report`'s input schema has exactly two properties — `title` and `smart_groups` — and no path, filename, or profile property. A `tools/call` without credentials configured is expected to come back as an error result naming the auth problem, not to hang.

- [ ] **Step 7: Commit**

```bash
git add internal/commands/mcp.go internal/commands/mcp_test.go
git commit -m "feat(mcp): register the generate_report tool

Third tool beside list_commands and run_command; input carries no destination."
```

---

### Task 7: The `jamf-report` skill

Rule #1 currently reads "Never call the Jamf API directly. Always use `jamf-cli` via the Bash tool." Claude Desktop exposes MCP tools and no Bash tool, so the skill's single hard rule is unfollowable in precisely the environment this work targets. This task is independent of the Go toolchain and may proceed even if Tasks 1–6 are blocked.

**Files:**
- Modify: `skills/skills/jamf-report/SKILL.md`
- Modify: `skills/.claude-plugin/plugin.json`

**Interfaces:**
- Consumes: the `generate_report` tool name and its two input fields from Tasks 4 and 6; `jamf-cli dashboard --title` / `--out-file` from `internal/commands/dashboard.go`.
- Produces: no code surface.

- [ ] **Step 1: Rewrite Rule #1 and add the dashboard capability**

Replace lines 9–14 of `skills/skills/jamf-report/SKILL.md` with:

```markdown
## Rules

1. **Never call the Jamf API directly.** Every report goes through `jamf-cli`.
   Two routes, depending on what this environment gives you:
   - **MCP tools present** (`list_commands`, `run_command`, `generate_report`) —
     use them. There is no Bash tool in Claude Desktop. For an HTML report,
     `generate_report` returns a file path; surface that path to the user, since
     it is the shareable artifact.
   - **Otherwise** — use `jamf-cli` via the Bash tool, as below.
2. **Default to table output** for interactive review, CSV for export.
3. **Offer narrative summaries** at the appropriate level (executive, detailed, raw).
4. **Highlight trends** if previous reports exist for comparison.
```

- [ ] **Step 2: Add the dashboard as a distinct capability**

Insert after the "Extension Attribute Results" section (currently ending at line 49), before `## Presentation Workflow`:

The block below is fenced with four backticks because the content it carries has
a three-backtick fence of its own — that inner fence is part of the text to
write into SKILL.md.

````markdown
### Cross-Product HTML Dashboard

The five reports above answer questions in the conversation, where a table beats
a file. The dashboard is different: reach for it when the user wants something to
**share or action** rather than read now — a self-contained HTML file covering
fleet health, security posture, patch compliance, and Platform blueprints and
benchmarks, with no CDN dependency, so it renders offline.

```
MCP:  generate_report { "title": "Q3 Fleet Review" }
CLI:  jamf-cli dashboard --title "Q3 Fleet Review" --out-file ~/Reports/q3.html
```

Add smart groups to visualize with `"smart_groups": ["All Laptops"]` over MCP, or
a repeatable `--smart-groups "All Laptops"` on the CLI.

Two limits apply to the MCP route only:

- **One profile.** The server pins a profile at launch, so an MCP report covers
  that profile. For a report spanning two products, use the CLI form with
  `--include-profile`.
- **No choice of destination.** The file name and directory are server-derived —
  the administrator sets the directory once with
  `jamf-cli pro setup --report-dir <dir>`, and `generate_report` refuses until
  they have. For a specific path, use the CLI form with `--out-file`.
````

- [ ] **Step 3: Fork the presentation workflow**

Replace the `## Presentation Workflow` section (currently lines 51–60) with:

```markdown
## Presentation Workflow

For a report the user reads now:

1. **Run the appropriate report command** with `-o json` for processing
2. **Summarize key findings** in plain language
3. **Offer export** in CSV format for spreadsheet use
4. **If requested, generate executive summary:**
   - Overall health metrics
   - Key risk areas
   - Recommended actions
   - Comparison to previous report (if available)

For a shareable artifact:

1. **Generate the dashboard** (`generate_report` over MCP, `jamf-cli dashboard`
   otherwise)
2. **Report the file path** — that is the thing the user sends on
3. **Summarize the highlights in the conversation**, so they know what they are
   about to share without opening it
4. **Relay any warnings** the generation returned: a warned section is
   incomplete, and the recipient cannot tell that from the file
```

- [ ] **Step 4: Bump the plugin version**

In `skills/.claude-plugin/plugin.json`, change `"version": "0.2.0"` to `"version": "0.3.0"`. This is the only section of this work that changes a skill.

- [ ] **Step 5: Verify the edits**

Run: `git diff --stat skills/ && grep -n "Bash tool" skills/skills/jamf-report/SKILL.md`
Expected: two files changed. The only remaining "Bash tool" mentions are inside Rule #1's two-branch route — one saying Claude Desktop has none, one naming Bash as the fallback. If any other skill's line 11 still reads the old unconditional rule, leave it: correcting the other six is explicitly out of scope (spec, Non-goals).

Confirm the frontmatter still parses and `name`/`description`/`user_invocable` are unchanged.

- [ ] **Step 6: Commit**

```bash
git add skills/skills/jamf-report/SKILL.md skills/.claude-plugin/plugin.json
git commit -m "feat(skills): route jamf-report through MCP tools when Bash is absent

Adds the cross-product dashboard as a shareable-artifact capability."
```

---

## Verification against the spec's Testing section

After Task 7, confirm each case the spec names has a test:

| Spec requirement | Test |
|---|---|
| filename derives from profile + timestamp, unaffected by a title with `../`, `/`, or NUL | `TestReportFileName_DerivesFromProfileAndTimestamp`, `TestReportFileName_StaysInsideReportDir` (title has no parameter to carry) |
| a title with path separators still yields a filename inside `report-dir` | `TestReportFileName_StaysInsideReportDir`, `TestBuildReportArgs_NeverRedirectsOutput`, `TestRunReportChild_ReportsPathAndSizeNeverTheHTML` |
| refusal when `report-dir` is unset, and when it names a non-directory | `TestResolveReportDir_RefusesWhenUnset`, `TestResolveReportDir_RefusesNonDirectory`, `TestRunReportChild_RefusesBeforeStartingAChildWhenReportDirUnset` |
| the report child's arg vector carries `--profile` and `--no-input`, and no `--out-file` | `TestBuildReportArgs_ThroughBuildChildArgsKeepsBoundary` |
| an `O_EXCL` collision is an error, not an overwrite | `TestCreateReportFile_CollisionIsAnError` |
| existing coverage retained | the ten `TestBuildChildArgs_*` cases are untouched; `TestDashboardInheritsRootFlags`, `TestDashboardProfileNames`, `TestReportDirPath` unchanged |

Then run the whole suite once: `make test && make lint && make verify-generated`.
