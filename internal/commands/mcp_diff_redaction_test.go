// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

const diffSecretPrefix = "S3CRET-"

// writeDiffSides writes two backup directories whose Classic credential fields
// differ, and nothing else does.
func writeDiffSides(t *testing.T) (src, tgt string) {
	t.Helper()
	src, tgt = t.TempDir(), t.TempDir()
	for _, side := range []struct{ dir, v string }{{src, "old"}, {tgt, "new"}} {
		writeBackupFileForTest(t, filepath.Join(side.dir, "policies", "p.yaml"), map[string]any{
			"general": map[string]any{"name": "P"},
			"account_maintenance": map[string]any{"accounts": []any{
				map[string]any{"account": map[string]any{"username": "admin", "password": diffSecretPrefix + "policy-" + side.v}},
			}},
		}, "yaml")
		writeBackupFileForTest(t, filepath.Join(side.dir, "disk-encryption", "d.yaml"), map[string]any{
			"name":                       "D",
			"institutional_recovery_key": map[string]any{"key": diffSecretPrefix + "key-" + side.v, "data": diffSecretPrefix + "data-" + side.v},
		}, "yaml")
		writeBackupFileForTest(t, filepath.Join(side.dir, "accounts", "users", "u.yaml"), map[string]any{
			"name":            "U",
			"password_sha256": diffSecretPrefix + "hash-" + side.v,
		}, "yaml")
	}
	return src, tgt
}

func runDiffToString(t *testing.T, src, tgt string) string {
	t.Helper()
	var buf bytes.Buffer
	f := output.New("json", true, false)
	f.SetWriter(&buf)
	cliCtx := &registry.CLIContext{Output: &cliOutput{f}}
	if err := runDiff(context.Background(), cliCtx, diffOptions{Source: src, Target: tgt}); err != nil {
		t.Fatalf("runDiff: %v", err)
	}
	return buf.String()
}

func TestRunDiff_RedactsClassicCredentialFieldsInAnMCPChild(t *testing.T) {
	t.Setenv(mcpChildEnvVar, "1")
	src, tgt := writeDiffSides(t)
	got := runDiffToString(t, src, tgt)
	if strings.Contains(got, diffSecretPrefix) {
		t.Errorf("pro diff prints a Classic credential field over MCP:\n%s", got)
	}
	for _, field := range []string{"account_maintenance", "institutional_recovery_key", "password_sha256"} {
		if !strings.Contains(got, field) {
			t.Errorf("a changed credential must still be reported as a modified %s:\n%s", field, got)
		}
	}
	if !strings.Contains(got, `redacted`) {
		t.Errorf("the masked values should print the redaction marker:\n%s", got)
	}
}

func TestRunDiff_PrintsClassicCredentialFieldsOutsideMCP(t *testing.T) {
	t.Setenv(mcpChildEnvVar, "")
	src, tgt := writeDiffSides(t)
	got := runDiffToString(t, src, tgt)
	for _, v := range []string{"policy-old", "policy-new", "key-new", "data-old", "hash-new"} {
		if !strings.Contains(got, diffSecretPrefix+v) {
			t.Errorf("outside MCP pro diff must print %s unchanged:\n%s", v, got)
		}
	}
}
