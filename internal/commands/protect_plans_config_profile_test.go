// Copyright 2026, Jamf Software LLC

package commands

import (
	"strings"
	"testing"
)

func TestPlanConfigProfileFileName_StaysInTheWorkingDirectory(t *testing.T) {
	for _, plan := range []string{"Default", "My Plan: v2", "..hidden", "a.b"} {
		if got, err := planConfigProfileFileName(plan); err != nil || got != plan+".mobileconfig" {
			t.Errorf("planConfigProfileFileName(%q) = %q, %v; an ordinary plan name must be kept byte-for-byte", plan, got, err)
		}
	}
	for _, plan := range []string{"../../.ssh/authorized_keys", "/etc/passwd", "/", "a/b", `a\b`, `..\x`, "", ".", ".."} {
		if got, err := planConfigProfileFileName(plan); err == nil || !strings.Contains(err.Error(), "-O/--output") {
			t.Errorf("planConfigProfileFileName(%q) = %q, %v; a name that is not one path segment must be refused, naming -O/--output", plan, got, err)
		}
	}
}
