// Copyright 2026, Jamf Software LLC

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/profileconvert"
)

// TestMCPPolicyTexts_NameEveryPayloadSecretSuffix requires each text that
// tells an operator which payload values are redacted to name exactly the
// suffixes profileconvert matches, in its order, and to say the rest are shown.
func TestMCPPolicyTexts_NameEveryPayloadSecretSuffix(t *testing.T) {
	s := profileconvert.SecretPayloadKeySuffixes
	want := strings.Join(s[:len(s)-1], ", ") + " or " + s[len(s)-1]
	texts := map[string]string{
		"mcp serve --help":            newMCPServeCmd().Long,
		"the run_command description": payloadRedactionToolNote,
	}
	for _, f := range []string{"agent_context.md", filepath.Join("..", "..", "CHANGELOG.md")} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		texts[filepath.Base(f)] = string(b)
	}
	for name, text := range texts {
		flat := strings.Join(strings.Fields(strings.ReplaceAll(text, "`", "")), " ")
		if !strings.Contains(flat, want) {
			t.Errorf("%s should name the redacted payload key suffixes as %q", name, want)
		}
		if !strings.Contains(flat, "Challenge") || !strings.Contains(strings.ToLower(flat), "every other payload value is shown") {
			t.Errorf("%s should name Challenge and say every other payload value is shown", name)
		}
	}
}
