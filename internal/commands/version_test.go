// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintVersion_DefaultBanner(t *testing.T) {
	var buf bytes.Buffer
	printVersion(&buf, "v1.0.0", "abc1234", "2026-05-08T00:00:00Z", false)
	out := buf.String()

	for _, want := range []string{"v1.0.0", "abc1234", "2026-05-08T00:00:00Z"} {
		if !strings.Contains(out, want) {
			t.Errorf("default banner missing %q, got %q", want, out)
		}
	}
	if strings.Contains(out, "spec sources") {
		t.Errorf("default banner should not include provenance, got %q", out)
	}
}

func TestPrintVersion_VerboseShowsProvenance(t *testing.T) {
	var buf bytes.Buffer
	printVersion(&buf, "v1.0.0", "abc1234", "2026-05-08T00:00:00Z", true)
	out := buf.String()

	for _, want := range []string{"Pro spec sources:", "Platform spec sources:"} {
		if !strings.Contains(out, want) {
			t.Errorf("verbose output missing %q, got %q", want, out)
		}
	}
}

func TestShortHash(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"dca326f3407f53b3c7f000c598e28badf70cd608e51250396d0e35eabde0ab5b", "dca326f3407f"},
		{"abc", "abc"}, // shorter than 12 → returned as-is
		{"", ""},
	}
	for _, tc := range tests {
		if got := shortHash(tc.in); got != tc.want {
			t.Errorf("shortHash(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNewVersionCmd_HelpMentionsVerbose(t *testing.T) {
	cmd := newVersionCmd("v1.0.0", "deadbeef", "2026-05-08")
	if !cmd.Flags().HasFlags() {
		t.Fatal("version command should declare a -v flag")
	}
	flag := cmd.Flags().Lookup("verbose")
	if flag == nil {
		t.Fatal("--verbose flag missing")
	}
	if flag.Shorthand != "v" {
		t.Errorf("expected shorthand 'v', got %q", flag.Shorthand)
	}
}
