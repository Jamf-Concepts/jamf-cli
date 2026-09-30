// Copyright 2026, Jamf Software LLC

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type inputDirFixture struct {
	dir, inside, spaced, sub, outside string
}

// newInputDirFixture installs a resolver allowing reads from a fresh directory
// holding two files, with a symlink inside it pointing at a file outside.
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
	for _, f := range []string{fx.inside, fx.spaced, fx.outside} {
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
