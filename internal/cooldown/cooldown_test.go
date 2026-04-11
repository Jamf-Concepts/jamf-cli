// Copyright 2026, Jamf Software LLC

package cooldown

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeState writes a cooldown state file directly under the given config dir.
func writeState(t *testing.T, configDir string, state map[string]time.Time) {
	t.Helper()
	raw := make(map[string]string, len(state))
	for k, v := range state {
		raw[k] = v.Format(time.RFC3339Nano)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	dir := filepath.Join(configDir, "jamf-cli")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cooldown.json"), data, 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
}

// readStateFile reads the cooldown.json from the given config dir.
func readStateFile(t *testing.T, configDir string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(configDir, "jamf-cli", "cooldown.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal state file: %v", err)
	}
	return raw
}

func TestEnforce_NoInput(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	// Write a recent last-op so there would normally be a wait.
	writeState(t, dir, map[string]time.Time{"mypro": time.Now()})

	d := 10 * time.Second
	start := time.Now()
	if err := Enforce("mypro", true, &d); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("noInput=true should return immediately, took %s", elapsed)
	}
}

func TestEnforce_ZeroDuration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	writeState(t, dir, map[string]time.Time{"mypro": time.Now()})

	zero := time.Duration(0)
	start := time.Now()
	if err := Enforce("mypro", false, &zero); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("configured=0 should return immediately, took %s", elapsed)
	}
}

func TestEnforce_EmptyProfile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	start := time.Now()
	if err := Enforce("", false, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("empty profile should return immediately, took %s", elapsed)
	}
}

func TestEnforce_NoPriorOp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	// No state file at all.
	start := time.Now()
	if err := Enforce("mypro", false, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("no prior op should return immediately, took %s", elapsed)
	}
}

func TestEnforce_CooldownElapsed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	// Last op was 200ms ago; cooldown is 50ms — already elapsed.
	writeState(t, dir, map[string]time.Time{"mypro": time.Now().Add(-200 * time.Millisecond)})

	d := 50 * time.Millisecond
	start := time.Now()
	if err := Enforce("mypro", false, &d); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("elapsed cooldown should return immediately, took %s", elapsed)
	}
}

func TestEnforce_CooldownNotElapsed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	// Last op was 10ms ago; cooldown is 80ms — ~70ms remaining.
	writeState(t, dir, map[string]time.Time{"mypro": time.Now().Add(-10 * time.Millisecond)})

	d := 80 * time.Millisecond
	start := time.Now()
	if err := Enforce("mypro", false, &d); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	elapsed := time.Since(start)
	// Should have slept at least ~60ms (generous lower bound for slow CI).
	if elapsed < 50*time.Millisecond {
		t.Errorf("expected sleep of ~70ms, only waited %s", elapsed)
	}
}

func TestEnforce_DefaultDuration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	// Last op was 15 seconds ago — default 10s cooldown already elapsed.
	writeState(t, dir, map[string]time.Time{"mypro": time.Now().Add(-15 * time.Second)})

	start := time.Now()
	if err := Enforce("mypro", false, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("nil configured with elapsed default should return immediately, took %s", elapsed)
	}
}

func TestRecord_EmptyProfile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	Record("")

	// No file should have been written.
	_, err := os.Stat(filepath.Join(dir, "jamf-cli", "cooldown.json"))
	if !os.IsNotExist(err) {
		t.Errorf("expected no state file for empty profile, got: %v", err)
	}
}

func TestRecord_WritesState(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	before := time.Now()
	Record("mypro")
	after := time.Now()

	raw := readStateFile(t, dir)
	ts, ok := raw["mypro"]
	if !ok {
		t.Fatalf("state file missing 'mypro' key; got: %v", raw)
	}
	recorded, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t.Fatalf("parse recorded timestamp: %v", err)
	}
	if recorded.Before(before) || recorded.After(after) {
		t.Errorf("recorded time %s not in expected range [%s, %s]", recorded, before, after)
	}
}
