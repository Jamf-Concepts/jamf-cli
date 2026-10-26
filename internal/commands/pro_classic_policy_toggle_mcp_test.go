// Copyright 2026, Jamf Software LLC

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func TestPolicyToggle_MCPChildNamesTheLineNotTheEntry(t *testing.T) {
	t.Setenv(registry.MCPChildEnvVar, "1")
	path := filepath.Join(t.TempDir(), "policies.txt")
	if err := os.WriteFile(path, []byte("# list\ns3cr3t-Token_0123456789abcdef\n8675309123\nLAB RESET\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stderr, err := runPolicyToggle(t, policyToggleFixture(), true, "--from-file", path, "--yes")
	if err == nil {
		t.Fatal("want an error for lines that resolve to no policy")
	}
	for _, secret := range []string{"s3cr3t", "8675309123", "LAB RESET"} {
		if strings.Contains(stderr, secret) || strings.Contains(err.Error(), secret) {
			t.Errorf("file line %q reached the MCP child's output:\nstderr: %s\nerr: %v", secret, stderr, err)
		}
	}
	for _, want := range []string{"no policy named line 2", "could not read policy line 3", "line 4 names 2 policies"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	}
}

func TestPolicyToggle_RefusesANonNameLineBeforeAnyRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policies.txt")
	if err := os.WriteFile(path, []byte("1\nbad\x1b]52;c;s3cr3t\x07\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mock := policyToggleFixture()

	_, err := runPolicyToggle(t, mock, true, "--from-file", path, "--yes")
	if err == nil || !strings.Contains(err.Error(), "line 2") || strings.Contains(err.Error(), "s3cr3t") {
		t.Errorf("err = %v, want line 2 refused without its content", err)
	}
	if calls := mock.callsMatching(""); len(calls) != 0 {
		t.Errorf("sent %v, want no request before the file is refused", calls)
	}
}
