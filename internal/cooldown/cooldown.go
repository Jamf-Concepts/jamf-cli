// Copyright 2026, Jamf Software LLC

// Package cooldown enforces a minimum delay between destructive operations
// on a per-profile basis, protecting against runaway automation.
//
// Default delay is 10 seconds. Per-profile overrides are read from the config
// (destructive-cooldown field on the Profile struct). Setting the value to 0
// disables the cooldown for that profile. Passing noInput=true always skips
// the cooldown regardless of config, so CI/CD pipelines are never affected.
package cooldown

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const defaultCooldown = 10 * time.Second

// statePath returns the path to the cooldown state file.
func statePath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "jamf-cli", "cooldown.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "jamf-cli", "cooldown.json")
}

var mu sync.Mutex

// loadState reads the cooldown state file. Returns an empty map on any error
// (missing file, parse error) — callers treat a missing entry as no prior op.
func loadState() map[string]time.Time {
	data, err := os.ReadFile(statePath())
	if err != nil {
		return map[string]time.Time{}
	}
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return map[string]time.Time{}
	}
	state := make(map[string]time.Time, len(raw))
	for k, v := range raw {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err == nil {
			state[k] = t
		}
	}
	return state
}

// saveState writes the cooldown state file, creating parent directories as needed.
func saveState(state map[string]time.Time) error {
	raw := make(map[string]string, len(state))
	for k, v := range state {
		raw[k] = v.Format(time.RFC3339Nano)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	path := statePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Enforce checks whether a cooldown period has elapsed since the last
// destructive operation for profileName. If not, it sleeps for the remaining
// duration, printing a message to stderr.
//
// Rules:
//   - noInput=true → always returns nil immediately (CI/CD bypass)
//   - configured != nil && *configured == 0 → returns nil (explicitly disabled)
//   - configured == nil → uses defaultCooldown (10s)
//   - otherwise → uses *configured
func Enforce(profileName string, noInput bool, configured *time.Duration) error {
	if noInput {
		return nil
	}
	if configured != nil && *configured == 0 {
		return nil
	}

	effective := defaultCooldown
	if configured != nil {
		effective = *configured
	}

	if profileName == "" {
		// No profile — env-var auth, typically CI/CD; skip cooldown.
		return nil
	}

	mu.Lock()
	state := loadState()
	last, ok := state[profileName]
	mu.Unlock()

	if !ok {
		return nil
	}

	elapsed := time.Since(last)
	if elapsed >= effective {
		return nil
	}

	remaining := effective - elapsed
	fmt.Fprintf(os.Stderr, "Cooldown: waiting %s before destructive operation...\n",
		remaining.Round(time.Millisecond))
	time.Sleep(remaining)
	return nil
}

// Record saves the current time as the last destructive operation timestamp
// for profileName. Errors are silently ignored — a failed record just means
// the next operation won't benefit from the cooldown gap.
func Record(profileName string) {
	if profileName == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	state := loadState()
	state[profileName] = time.Now()
	_ = saveState(state)
}
