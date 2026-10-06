// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// generatedFileGuard returns the PreToolUse hook command in .claude/settings.json
// that refuses edits to generated code.
func generatedFileGuard(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../.claude/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	for _, entry := range settings.Hooks["PreToolUse"] {
		for _, h := range entry.Hooks {
			if strings.Contains(h.Command, "/generated/") {
				return h.Command
			}
		}
	}
	t.Fatal(".claude/settings.json has no PreToolUse hook guarding the generated trees")
	return ""
}

// guardBlocks runs the hook on an Edit payload for path and reports whether it
// refused the edit.
func guardBlocks(t *testing.T, command, path string) bool {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"tool_input": map[string]string{"file_path": path}})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-c", command)
	cmd.Stdin = strings.NewReader(string(payload))
	err = cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return false
	case errors.As(err, &exit) && exit.ExitCode() == 2:
		return true
	}
	t.Fatalf("hook on %s: %v", path, err)
	return false
}

// TestGeneratedFileGuardCoversEveryGeneratedTree runs the guard against every
// file in the trees `make generate` writes. It once named a directory that does
// not exist, and so refused nothing.
func TestGeneratedFileGuardCoversEveryGeneratedTree(t *testing.T) {
	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed; the hook itself needs it", tool)
		}
	}
	command := generatedFileGuard(t)
	trees, err := filepath.Glob("*/generated")
	if err != nil {
		t.Fatal(err)
	}
	if len(trees) < 3 {
		t.Fatalf("found generated trees %q; want pro, platform and security", trees)
	}
	for _, tree := range trees {
		entries, err := os.ReadDir(tree)
		if err != nil {
			t.Fatal(err)
		}
		// Every generated file exercises the same pattern, so one per tree is
		// enough; every hand-written file is checked, since those are what a
		// pattern too broad would catch.
		checkedGenerated := false
		for _, e := range entries {
			generated := strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go")
			if generated && checkedGenerated {
				continue
			}
			checkedGenerated = checkedGenerated || generated
			path, err := filepath.Abs(filepath.Join(tree, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if got := guardBlocks(t, command, path); got != generated {
				t.Errorf("guard blocks %s = %v, want %v: make generate overwrites generated Go code and nothing else", path, got, generated)
			}
		}
		if !checkedGenerated {
			t.Errorf("%s holds no generated Go file; the walk checked nothing", tree)
		}
	}
	future, err := filepath.Abs("school/generated/school_users.go")
	if err != nil {
		t.Fatal(err)
	}
	if !guardBlocks(t, command, future) {
		t.Errorf("guard does not block %s; a new generated tree must be covered without editing the hook", future)
	}
}
