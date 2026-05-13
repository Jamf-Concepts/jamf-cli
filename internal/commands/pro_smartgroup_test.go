// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func runSmartGroupCmd(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cliCtx := &registry.CLIContext{}
	root := newSmartGroupCmd(cliCtx)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestTemplates_TableDefault(t *testing.T) {
	out, _, err := runSmartGroupCmd(t, "templates")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, want := range []string{
		"encryption/not-encrypted",
		"updates/os-version-below",
		"mdm/bootstrap-token-missing",
		"compliance/gatekeeper-disabled",
		"lifecycle/unsupervised",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
}

func TestTemplates_CategoryFilter(t *testing.T) {
	out, _, err := runSmartGroupCmd(t, "templates", "--category", "encryption")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "encryption/not-encrypted") {
		t.Errorf("expected encryption templates: %s", out)
	}
	if strings.Contains(out, "lifecycle/unsupervised") {
		t.Errorf("category filter should have excluded lifecycle: %s", out)
	}
}

func TestTemplates_JSONOutput(t *testing.T) {
	out, _, err := runSmartGroupCmd(t, "templates", "-o", "json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var parsed []map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("json output not parseable: %v\n%s", err, out)
	}
	if len(parsed) != 23 {
		t.Errorf("expected 23 templates in json, got %d", len(parsed))
	}
}

func TestTemplates_UnknownCategory(t *testing.T) {
	out, _, err := runSmartGroupCmd(t, "templates", "--category", "nonexistent")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "0 templates") && !strings.Contains(out, "No templates") {
		t.Errorf("expected empty-result message, got: %s", out)
	}
}

// Suppress unused-import warnings for context/http/io used by later tasks.
var (
	_           = context.Background
	_           = http.MethodGet
	_ io.Reader = nil
)

func TestPreview_ZeroParam(t *testing.T) {
	out, _, err := runSmartGroupCmd(t, "preview", "--template", "encryption/not-encrypted")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "POST /v2/computer-groups/smart-groups") {
		t.Errorf("expected POST header: %s", out)
	}
	if !strings.Contains(out, "FileVault 2 Status") || !strings.Contains(out, "Not Encrypted") {
		t.Errorf("expected criterion in JSON body: %s", out)
	}
}

func TestPreview_WithParam(t *testing.T) {
	out, _, err := runSmartGroupCmd(t, "preview", "--template", "encryption/encryption-stalled", "--stalled-after", "14")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, `"value": "14"`) {
		t.Errorf("expected stalled-after=14 in output: %s", out)
	}
}

func TestPreview_UnknownTemplate(t *testing.T) {
	_, _, err := runSmartGroupCmd(t, "preview", "--template", "encryption/typo")
	if err == nil {
		t.Fatal("expected error for unknown template, got nil")
	}
	if !strings.Contains(err.Error(), "encryption/") {
		t.Errorf("expected fuzzy-match suggestion mentioning encryption: %v", err)
	}
}

func TestPreview_RequiredParamMissing(t *testing.T) {
	_, _, err := runSmartGroupCmd(t, "preview", "--template", "updates/os-version-below")
	if err == nil {
		t.Fatal("expected error for missing --below-version, got nil")
	}
	if !strings.Contains(err.Error(), "below-version") {
		t.Errorf("expected error to mention required param: %v", err)
	}
}
