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

type fakeSGClient struct {
	calls []recordedCall
	queue []*http.Response
}

type recordedCall struct {
	method, url, body string
}

func (f *fakeSGClient) Do(_ context.Context, method, url string, body io.Reader) (*http.Response, error) {
	b := ""
	if body != nil {
		buf, _ := io.ReadAll(body)
		b = string(buf)
	}
	f.calls = append(f.calls, recordedCall{method, url, b})
	if len(f.queue) == 0 {
		return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("queue empty"))}, nil
	}
	resp := f.queue[0]
	f.queue = f.queue[1:]
	return resp, nil
}

func newJSONResp(status int, payload any) *http.Response {
	b, _ := json.Marshal(payload)
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(b)))}
}

func runSmartGroupApply(t *testing.T, client *fakeSGClient, args ...string) (string, error) {
	t.Helper()
	cliCtx := &registry.CLIContext{Client: client}
	root := newSmartGroupCmd(cliCtx)
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"apply"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestApply_NewGroupCreated(t *testing.T) {
	client := &fakeSGClient{
		queue: []*http.Response{
			newJSONResp(200, map[string]any{"totalCount": 0, "results": []any{}}),
			newJSONResp(201, map[string]any{"id": "287", "href": "/.../287"}),
			newJSONResp(200, map[string]any{"members": []int{1, 2, 3, 4, 5}}),
		},
	}
	out, err := runSmartGroupApply(
		t, client,
		"--template", "encryption/not-encrypted",
		"--name", "Test FV Not Encrypted",
		"--yes",
	)
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	if len(client.calls) != 3 {
		t.Fatalf("expected 3 API calls, got %d", len(client.calls))
	}
	if client.calls[1].method != "POST" {
		t.Errorf("expected second call POST (create), got %s", client.calls[1].method)
	}
	if !strings.Contains(out, "Membership: 5") {
		t.Errorf("expected membership log in output: %s", out)
	}
}

func TestApply_ExistingGroupUpdated(t *testing.T) {
	client := &fakeSGClient{
		queue: []*http.Response{
			newJSONResp(200, map[string]any{"totalCount": 1, "results": []any{map[string]any{"id": "42", "name": "Test FV Not Encrypted"}}}),
			newJSONResp(204, map[string]any{}),
			newJSONResp(200, map[string]any{"members": []int{1, 2}}),
		},
	}
	out, err := runSmartGroupApply(
		t, client,
		"--template", "encryption/not-encrypted",
		"--name", "Test FV Not Encrypted",
		"--yes",
	)
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	if client.calls[1].method != "PUT" {
		t.Errorf("expected PUT on existing group, got %s", client.calls[1].method)
	}
	if !strings.Contains(client.calls[1].url, "/42") {
		t.Errorf("expected PUT URL with id=42: %s", client.calls[1].url)
	}
}

func TestApply_DryRunNoAPICalls(t *testing.T) {
	client := &fakeSGClient{}
	out, err := runSmartGroupApply(
		t, client,
		"--template", "encryption/not-encrypted",
		"--name", "Test",
		"--dry-run",
	)
	if err != nil {
		t.Fatalf("dry-run: %v\n%s", err, out)
	}
	if len(client.calls) != 0 {
		t.Fatalf("expected 0 API calls in dry-run, got %d", len(client.calls))
	}
	if !strings.Contains(out, "POST /v2/computer-groups/smart-groups") {
		t.Errorf("expected dry-run output to show what would POST: %s", out)
	}
}

func TestApply_ZeroMembershipWarning(t *testing.T) {
	client := &fakeSGClient{
		queue: []*http.Response{
			newJSONResp(200, map[string]any{"totalCount": 0, "results": []any{}}),
			newJSONResp(201, map[string]any{"id": "99"}),
			newJSONResp(200, map[string]any{"members": []int{}}),
		},
	}
	out, _ := runSmartGroupApply(
		t, client,
		"--template", "compliance/firewall-disabled",
		"--name", "Test FW Off",
		"--yes",
	)
	if !strings.Contains(out, "matched 0 devices") {
		t.Errorf("expected zero-match warning: %s", out)
	}
}

func TestApply_403MissingPrivilege(t *testing.T) {
	client := &fakeSGClient{
		queue: []*http.Response{
			newJSONResp(200, map[string]any{"totalCount": 0, "results": []any{}}),
			newJSONResp(403, map[string]any{"errors": []string{"forbidden"}}),
		},
	}
	_, err := runSmartGroupApply(
		t, client,
		"--template", "encryption/not-encrypted",
		"--name", "Test",
		"--yes",
	)
	if err == nil {
		t.Fatal("expected error on 403, got nil")
	}
	if !strings.Contains(err.Error(), "Create Smart Computer Groups") {
		t.Errorf("expected privilege name in error: %v", err)
	}
}

func TestVerifyTemplates_CategoryRuns(t *testing.T) {
	// Each template in the encryption category produces 4 HTTP calls
	// (POST create + recalc + membership + DELETE cleanup). We queue 6 templates * 4 = 24 responses.
	client := &fakeSGClient{}
	for i := 0; i < 6; i++ {
		client.queue = append(
			client.queue,
			newJSONResp(201, map[string]any{"id": "100"}),
			newJSONResp(200, map[string]any{}),
			newJSONResp(200, map[string]any{"members": []int{1, 2}}),
			newJSONResp(204, map[string]any{}),
		)
	}
	cliCtx := &registry.CLIContext{Client: client}
	root := newSmartGroupCmd(cliCtx)
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"verify-templates", "--category", "encryption"})
	if err := root.Execute(); err != nil {
		t.Fatalf("verify-templates: %v", err)
	}
	if !strings.Contains(out.String(), "Verifying 6 templates") {
		t.Errorf("expected '6 templates' in output: %s", out.String())
	}
	if !strings.Contains(out.String(), "Summary: 6 OK") {
		t.Errorf("expected summary line: %s", out.String())
	}
}
