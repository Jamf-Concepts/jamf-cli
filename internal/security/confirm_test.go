// Copyright 2026, Jamf Software LLC

package security

import "testing"

func TestConfirmAction_YesBypassesPrompt(t *testing.T) {
	if err := ConfirmAction("purge", "device-lifecycle", true); err != nil {
		t.Fatalf("ConfirmAction() error = %v, want nil", err)
	}
}

func TestConfirmAction_NonTerminalWithoutYesErrors(t *testing.T) {
	// go test's stdin is not a terminal, so this exercises the
	// non-interactive (CI) path without needing to fake a TTY.
	err := ConfirmAction("purge", "device-lifecycle", false)
	if err == nil {
		t.Fatal("ConfirmAction() error = nil, want error when stdin is not a terminal and --yes is unset")
	}
}
