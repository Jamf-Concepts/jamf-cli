// Copyright 2026, Jamf Software LLC

package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type inputDirFixture struct {
	dir, inside, spaced, sub, outside string
	adjacentFile, adjacentDir         string
}

// newInputDirFixture installs a resolver allowing reads from a fresh directory
// holding two files, with a symlink inside it pointing at a file outside, and a
// sibling `<dir>-adjacent` whose name the input directory is a string prefix of.
func newInputDirFixture(t *testing.T) inputDirFixture {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	other, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fx := inputDirFixture{
		dir:     dir,
		inside:  filepath.Join(dir, "body.xml"),
		spaced:  filepath.Join(dir, "my payload.plist"),
		sub:     filepath.Join(dir, "imports"),
		outside: filepath.Join(other, "id_ed25519"),
	}
	adjacent := dir + "-adjacent"
	fx.adjacentFile = filepath.Join(adjacent, "body.xml")
	fx.adjacentDir = filepath.Join(adjacent, "imports")
	if err := os.MkdirAll(fx.adjacentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(adjacent) })
	for _, f := range []string{fx.inside, fx.spaced, fx.outside, fx.adjacentFile} {
		if err := os.WriteFile(f, []byte("<x/>"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(fx.sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fx.outside, filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}
	installMCPResolver(NewRootCmd("test", "t", "t", "t"), dir)
	t.Cleanup(func() { installMCPResolver(nil, "") })
	t.Cleanup(resetGlobals)
	return fx
}

func TestBuildChildArgs_AllowsReadPathsInsideTheInputDir(t *testing.T) {
	fx := newInputDirFixture(t)
	t.Chdir(fx.dir)
	for _, args := range [][]string{
		{"pro", "classic-policies", "create", "--from-file", fx.inside},
		{"pro", "classic-policies", "create", "--from-file=" + fx.inside},
		{"pro", "classic-policies", "create", "--from-file", "body.xml"},
		{"pro", "classic-macos-config-profiles", "create", "--custom-payload-file", fx.spaced, "--custom-payload-file=" + fx.inside},
		{"pro", "scripts", "create", "--script-file", "./body.xml"},
		{"protect", "analytics", "import", "--dir", fx.sub},
	} {
		if _, err := buildChildArgs("prod", args); err != nil {
			t.Errorf("buildChildArgs(%q) = %v; a read path inside the input directory is allowed", args, err)
		}
	}
}

func TestBuildChildArgs_RefusesReadPathsOutsideTheInputDir(t *testing.T) {
	fx := newInputDirFixture(t)
	cases := map[string][]string{
		"outside":             {"pro", "classic-policies", "create", "--from-file", fx.outside},
		"dot-dot":             {"pro", "classic-policies", "create", "--from-file", fx.dir + "/../" + filepath.Base(filepath.Dir(fx.outside)) + "/id_ed25519"},
		"symlink escape":      {"pro", "classic-policies", "create", "--from-file=" + filepath.Join(fx.dir, "escape")},
		"nonexistent":         {"pro", "classic-policies", "create", "--from-file", filepath.Join(fx.dir, "missing.xml")},
		"empty":               {"pro", "classic-policies", "create", "--from-file="},
		"one bad array value": {"pro", "classic-macos-config-profiles", "create", "--custom-payload-file", fx.inside, "--custom-payload-file", fx.outside},
		"password-file":       {"pro", "computer-inventory", "set-auto-admin-password", "--password-file", fx.inside},
		"save-to":             {"pro", "scripts", "download", "1", "--save-to", fx.inside},
		"sync --dir":          {"pro", "jcds", "sync", "--dir", fx.sub},
		"leaf --output":       {"protect", "downloads", "installer", "--output", fx.inside},
		"report-dir":          {"pro", "setup", "--report-dir", fx.sub},
	}
	for name, args := range cases {
		if got, err := buildChildArgs("prod", args); err == nil {
			t.Errorf("%s: buildChildArgs(%q) = %q; it must be refused even with an input directory", name, args, got)
		}
	}
}

func TestBuildChildArgs_ReadPathRefusalNamesTheRemedy(t *testing.T) {
	installMCPResolver(NewRootCmd("test", "t", "t", "t"), "")
	t.Cleanup(func() { installMCPResolver(nil, "") })
	t.Cleanup(resetGlobals)
	_, err := buildChildArgs("prod", []string{"pro", "classic-policies", "create", "--from-file", "/etc/hosts"})
	if err == nil || !strings.Contains(err.Error(), "--input-dir") {
		t.Errorf("with no input directory a read path must be refused, naming --input-dir: %v", err)
	}

	fx := newInputDirFixture(t)
	_, err = buildChildArgs("prod", []string{"pro", "classic-policies", "create", "--from-file", fx.outside})
	if err == nil || !strings.Contains(err.Error(), fx.dir) {
		t.Errorf("a read path outside the input directory must be refused, naming it: %v", err)
	}
}

func TestResolveMCPInputDir_RefusesADirectoryItCannotUse(t *testing.T) {
	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Join(t.TempDir(), "missing"), file} {
		if got, err := resolveMCPInputDir(dir); err == nil {
			t.Errorf("resolveMCPInputDir(%q) = %q; the server must not start on it", dir, got)
		}
	}
	link := filepath.Join(t.TempDir(), "link")
	target, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveMCPInputDir(link); err != nil || got != target {
		t.Errorf("resolveMCPInputDir(%q) = %q, %v; want the resolved %q", link, got, err, target)
	}
}

func TestPinnedChildEnv_CarriesOnlyTheInstalledInputDir(t *testing.T) {
	t.Setenv(mcpInputDirEnvVar, "/")
	installMCPResolver(nil, "/allowed")
	t.Cleanup(func() { installMCPResolver(nil, "") })
	env := pinnedChildEnv("prod")
	var got []string
	for _, kv := range env {
		if strings.HasPrefix(kv, mcpInputDirEnvVar+"=") {
			got = append(got, kv)
		}
	}
	if len(got) != 1 || got[0] != mcpInputDirEnvVar+"=/allowed" {
		t.Errorf("child input-dir env = %q; want only the installed directory", got)
	}
}

func TestMCPChild_EnforcesTheInputDir(t *testing.T) {
	fx := newInputDirFixture(t)
	installMCPResolver(nil, "")
	t.Setenv(mcpInputDirEnvVar, fx.dir)
	t.Setenv("JAMF_URL", "http://127.0.0.1:1")
	t.Setenv("JAMF_TOKEN", "fake-token")
	child := func(args ...string) error {
		return executeAsMCPChild(t, "", append([]string{"--no-input", "-n"}, args...)...)
	}

	for _, args := range [][]string{
		{"pro", "classic-policies", "create", "--from-file", fx.outside},
		{"pro", "classic-policies", "create", "--from-file", filepath.Join(fx.dir, "escape")},
		{"pro", "classic-macos-config-profiles", "create", "--custom-payload-file", fx.inside, "--custom-payload-file", fx.outside},
		{"pro", "computer-inventory", "set-auto-admin-password", "--password-file", fx.inside},
	} {
		if err := child(args...); !isMCPRefusal(err) {
			t.Errorf("MCP child ran %q (err %v); it must enforce the input directory on its own parse", args, err)
		}
	}
	if err := child("pro", "classic-policies", "create", "--from-file", fx.inside); isMCPRefusal(err) {
		t.Errorf("MCP child refused a read inside the input directory: %v", err)
	}

	t.Setenv(mcpInputDirEnvVar, "")
	if err := child("pro", "classic-policies", "create", "--from-file", fx.inside); err == nil || !strings.Contains(err.Error(), "--input-dir") {
		t.Errorf("with no input directory the child must refuse every read path: %v", err)
	}
}

func TestBuildChildArgs_RefusesDiffDirectoryOutsideTheInputDir(t *testing.T) {
	fx := newInputDirFixture(t)
	outsideDir := filepath.Dir(fx.outside)
	if err := os.Symlink(outsideDir, filepath.Join(fx.dir, "escape-dir")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", outsideDir)
	for name, args := range map[string][]string{
		"outside":         {"pro", "diff", "--source", outsideDir, "--target", "prod"},
		"inside vs out":   {"pro", "diff", "--source", fx.sub, "--target=" + outsideDir},
		"dot-dot":         {"pro", "diff", "--source", fx.dir + "/../" + filepath.Base(outsideDir), "--target", "prod"},
		"symlink escape":  {"pro", "diff", "--source", filepath.Join(fx.dir, "escape-dir"), "--target", "prod"},
		"home outside":    {"pro", "diff", "--source", "~/", "--target", "prod"},
		"nonexistent":     {"pro", "diff", "--source", filepath.Join(fx.dir, "missing"), "--target", "prod"},
		"foreign profile": {"pro", "diff", "--source", fx.sub, "--target", "other"},
	} {
		if got, err := buildChildArgs("prod", args); !isMCPRefusal(err) {
			t.Errorf("%s: buildChildArgs(%q) = %q, %v; a diff directory outside the input directory must be refused", name, args, got, err)
		}
	}
	if _, err := buildChildArgs("prod", []string{"pro", "diff", "--source", fx.sub, "--target", "prod"}); err != nil {
		t.Errorf("a diff directory inside the input directory must be allowed: %v", err)
	}
}

func TestBuildChildArgs_RefusesDiffDirectoryWithNoInputDir(t *testing.T) {
	installMCPResolver(NewRootCmd("test", "t", "t", "t"), "")
	t.Cleanup(func() { installMCPResolver(nil, "") })
	t.Cleanup(resetGlobals)
	_, err := buildChildArgs("prod", []string{"pro", "diff", "--source", t.TempDir(), "--target", "prod"})
	if !isMCPRefusal(err) || !strings.Contains(err.Error(), "--input-dir") {
		t.Errorf("with no input directory a diff directory must be refused, naming --input-dir: %v", err)
	}
}

func TestMCPChild_EnforcesTheInputDirOnDiffSides(t *testing.T) {
	fx := newInputDirFixture(t)
	installMCPResolver(nil, "")
	t.Setenv(mcpInputDirEnvVar, fx.dir)
	outside := filepath.Dir(fx.outside)
	if err := executeAsMCPChild(t, "prod", "--profile", "prod", "--no-input", "pro", "diff", "--source", outside, "--target", "prod"); !isMCPRefusal(err) {
		t.Errorf("MCP child diffed a directory outside the input directory (err %v)", err)
	}
	if err := executeAsMCPChild(t, "prod", "--profile", "prod", "--no-input", "pro", "diff", "--source", fx.sub, "--target", fx.sub); isMCPRefusal(err) {
		t.Errorf("MCP child refused a diff inside the input directory: %v", err)
	}
	t.Setenv(mcpInputDirEnvVar, "")
	if err := executeAsMCPChild(t, "prod", "--profile", "prod", "--no-input", "pro", "diff", "--source", fx.sub, "--target", "prod"); !isMCPRefusal(err) {
		t.Errorf("with no input directory the child must refuse a diff directory (err %v)", err)
	}
}

func TestBuildChildArgs_RefusesASiblingSharingTheInputDirPrefix(t *testing.T) {
	fx := newInputDirFixture(t)
	for name, args := range map[string][]string{
		"file":       {"pro", "classic-policies", "create", "--from-file", fx.adjacentFile},
		"import dir": {"protect", "analytics", "import", "--dir", fx.adjacentDir},
		"diff side":  {"pro", "diff", "--source", fx.adjacentDir, "--target", "prod"},
	} {
		if got, err := buildChildArgs("prod", args); !isMCPRefusal(err) {
			t.Errorf("%s: buildChildArgs(%q) = %q, %v; %s is outside the input directory %s", name, args, got, err, fx.adjacentFile, fx.dir)
		}
	}
}

func TestMCPChild_RefusesASiblingSharingTheInputDirPrefix(t *testing.T) {
	fx := newInputDirFixture(t)
	installMCPResolver(nil, "")
	t.Setenv(mcpInputDirEnvVar, fx.dir)
	t.Setenv("JAMF_URL", "http://127.0.0.1:1")
	t.Setenv("JAMF_TOKEN", "fake-token")
	for _, args := range [][]string{
		{"--no-input", "-n", "pro", "classic-policies", "create", "--from-file", fx.adjacentFile},
		{"--profile", "prod", "--no-input", "pro", "diff", "--source", fx.adjacentDir, "--target", "prod"},
	} {
		if err := executeAsMCPChild(t, "prod", args...); !isMCPRefusal(err) {
			t.Errorf("MCP child ran %q (err %v); a sibling sharing the input directory's prefix is outside it", args, err)
		}
	}
}

func TestInsideDir(t *testing.T) {
	for _, tc := range []struct {
		dir, path string
		want      bool
	}{
		{"/in", "/in", true},
		{"/in", "/in/a/b", true},
		{"/in", "/in/..hidden", true},
		{"/in", "/in-adjacent/a", false},
		{"/in", "/", false},
		{"/in", "/in/../out", false},
		{"/", "/etc/hosts", true},
		{"/", "/", true},
	} {
		if got := insideDir(tc.dir, tc.path); got != tc.want {
			t.Errorf("insideDir(%q, %q) = %v, want %v", tc.dir, tc.path, got, tc.want)
		}
	}
}

func TestBuildChildArgs_AllowsAnyExistingPathUnderARootInputDir(t *testing.T) {
	fx := newInputDirFixture(t)
	installMCPResolver(NewRootCmd("test", "t", "t", "t"), string(filepath.Separator))
	if _, err := buildChildArgs("prod", []string{"pro", "classic-policies", "create", "--from-file", fx.outside}); err != nil {
		t.Errorf("with --input-dir / every existing path is inside it: %v", err)
	}
}

// newDiffBackupWithSymlink returns a backup directory inside the fixture's
// input directory whose one resource file is a symlink to target.
func newDiffBackupWithSymlink(t *testing.T, fx inputDirFixture, target string) string {
	t.Helper()
	backup := filepath.Join(fx.sub, "backup")
	res := filepath.Join(backup, "custom-things")
	if err := os.MkdirAll(res, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(res, "leak.yaml")); err != nil {
		t.Fatal(err)
	}
	return backup
}

func TestLoadSnapshotFromDirectory_RefusesASymlinkOutOfTheInputDirInAnMCPChild(t *testing.T) {
	fx := newInputDirFixture(t)
	backup := newDiffBackupWithSymlink(t, fx, fx.outside)

	t.Setenv(mcpChildEnvVar, "1")
	t.Setenv(mcpInputDirEnvVar, fx.dir)
	if _, err := loadSnapshotFromDirectory(backup, nil); !errors.Is(err, errMCPChildReadRefused) || !strings.Contains(err.Error(), fx.outside) {
		t.Errorf("an MCP child diffed a file resolving to %s outside the input directory (err %v)", fx.outside, err)
	}

	t.Setenv(mcpChildEnvVar, "")
	if _, err := loadSnapshotFromDirectory(backup, nil); err != nil {
		t.Errorf("outside MCP a symlinked backup file must still be read as before: %v", err)
	}
}

func TestLoadSnapshotFromDirectory_FollowsASymlinkInsideTheInputDirInAnMCPChild(t *testing.T) {
	fx := newInputDirFixture(t)
	backup := newDiffBackupWithSymlink(t, fx, fx.inside)
	t.Setenv(mcpChildEnvVar, "1")
	t.Setenv(mcpInputDirEnvVar, fx.dir)
	if _, err := loadSnapshotFromDirectory(backup, nil); err != nil {
		t.Errorf("a symlink resolving inside the input directory must be read: %v", err)
	}
}

func TestRefuseMCPChildReadOutsideInputDir(t *testing.T) {
	fx := newInputDirFixture(t)
	escape := filepath.Join(fx.dir, "escape")
	t.Setenv(mcpChildEnvVar, "")
	t.Setenv(mcpInputDirEnvVar, fx.dir)
	if err := refuseMCPChildReadOutsideInputDir(escape); err != nil {
		t.Errorf("outside an MCP child nothing is refused: %v", err)
	}
	t.Setenv(mcpChildEnvVar, "1")
	for _, path := range []string{escape, fx.adjacentFile, filepath.Join(fx.dir, "missing.yaml")} {
		if err := refuseMCPChildReadOutsideInputDir(path); !errors.Is(err, errMCPChildReadRefused) {
			t.Errorf("an MCP child read %s, which is not inside %s (err %v)", path, fx.dir, err)
		}
	}
	if err := refuseMCPChildReadOutsideInputDir(fx.inside); err != nil {
		t.Errorf("a file inside the input directory must be readable: %v", err)
	}
}

func TestCollectProtectRestoreFiles_RefusesSymlinksOutOfTheInputDirInAnMCPChild(t *testing.T) {
	fx := newInputDirFixture(t)
	outsideDir := filepath.Dir(fx.outside)
	selected, err := protectSelectResources("", "")
	if err != nil {
		t.Fatal(err)
	}
	newBackup := func(t *testing.T, name string) string {
		t.Helper()
		backup := filepath.Join(fx.sub, name)
		if err := os.MkdirAll(filepath.Join(backup, "analytics"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(backup, "analytics", "Custom.yaml"), []byte("name: Custom\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return backup
	}
	t.Setenv(mcpChildEnvVar, "1")
	t.Setenv(mcpInputDirEnvVar, fx.dir)

	for name, link := range map[string]func(backup string) error{
		"analytic file": func(b string) error { return os.Symlink(fx.outside, filepath.Join(b, "analytics", "Leak.yaml")) },
		"singleton":     func(b string) error { return os.Symlink(fx.outside, filepath.Join(b, "insights.yaml")) },
		"resource dir": func(b string) error {
			if err := os.WriteFile(filepath.Join(outsideDir, "Stolen.yaml"), []byte("name: Stolen\n"), 0o600); err != nil {
				return err
			}
			return os.Symlink(outsideDir, filepath.Join(b, "plans"))
		},
	} {
		backup := newBackup(t, strings.ReplaceAll(name, " ", "-"))
		if err := link(backup); err != nil {
			t.Fatal(err)
		}
		if files, _, err := collectProtectRestoreFiles(backup, selected, true); !errors.Is(err, errMCPChildReadRefused) {
			t.Errorf("%s: an MCP child collected %v (err %v) through a symlink out of the input directory", name, files, err)
		}
	}

	backup := newBackup(t, "regular")
	files, _, err := collectProtectRestoreFiles(backup, selected, true)
	if err != nil || len(files) != 1 || files[0].Path != filepath.Join(backup, "analytics", "Custom.yaml") {
		t.Errorf("a regular file inside the input directory must still be collected: %v, %v", files, err)
	}
}
