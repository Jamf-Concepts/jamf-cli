// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"gopkg.in/yaml.v3"
)

// backupMockClient returns canned responses for backup tests.
type backupMockClient struct {
	responses map[string]overviewMockResponse
}

func (m *backupMockClient) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	// Try exact match first
	if resp, ok := m.responses[path]; ok {
		return &http.Response{
			StatusCode: resp.statusCode,
			Body:       io.NopCloser(strings.NewReader(resp.body)),
			Header:     make(http.Header),
		}, nil
	}
	// Try without query params
	if before, _, ok := strings.Cut(path, "?"); ok {
		base := before
		if resp, ok := m.responses[base]; ok {
			return &http.Response{
				StatusCode: resp.statusCode,
				Body:       io.NopCloser(strings.NewReader(resp.body)),
				Header:     make(http.Header),
			}, nil
		}
	}
	return nil, fmt.Errorf("no mock for %s %s", method, path)
}

func TestBackup_DirectoryStructure(t *testing.T) {
	mock := &backupMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/policies":      {200, `{"policies":[{"id":1,"name":"Deploy Chrome"},{"id":2,"name":"Install Rosetta"}]}`},
			"/JSSResource/policies/id/1": {200, `{"policy":{"general":{"id":1,"name":"Deploy Chrome","enabled":true},"scope":{"all_computers":true}}}`},
			"/JSSResource/policies/id/2": {200, `{"policy":{"general":{"id":2,"name":"Install Rosetta","enabled":false},"scope":{}}}`},
		},
	}

	oldURL := serverURL
	serverURL = "https://test.jamfcloud.com"
	defer func() { serverURL = oldURL }()

	outDir := t.TempDir()
	cliCtx := &registry.CLIContext{Client: mock}

	err := runBackup(context.Background(), cliCtx, backupOptions{
		OutputDir:   outDir,
		Format:      "yaml",
		Resources:   "policies",
		IncludeIDs:  false,
		Concurrency: 2,
	})
	if err != nil {
		t.Fatalf("runBackup error: %v", err)
	}

	// Verify directory exists
	policyDir := filepath.Join(outDir, "policies")
	if _, err := os.Stat(policyDir); os.IsNotExist(err) {
		t.Fatal("policies directory not created")
	}

	// Verify files exist with slugified names
	entries, err := os.ReadDir(policyDir)
	if err != nil {
		t.Fatalf("reading directory: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 policy files, got %d", len(entries))
	}

	// Check file content
	chromePath := filepath.Join(policyDir, "deploy-chrome.yaml")
	content, err := os.ReadFile(chromePath)
	if err != nil {
		t.Fatalf("reading backup file: %v", err)
	}

	var parsed map[string]any
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("parsing YAML: %v", err)
	}

	// Should have _meta
	if _, ok := parsed["_meta"]; !ok {
		t.Error("backup file missing _meta block")
	}

	// id should be stripped (IncludeIDs=false)
	if _, ok := parsed["id"]; ok {
		t.Error("id should be stripped when IncludeIDs=false")
	}
}

func TestBackup_JSONFormat(t *testing.T) {
	mock := &backupMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/categories":   {200, `{"totalCount":1,"results":[{"id":"1","name":"Productivity"}]}`},
			"/v1/categories/1": {200, `{"id":"1","name":"Productivity","priority":5}`},
		},
	}

	oldURL := serverURL
	serverURL = "https://test.jamfcloud.com"
	defer func() { serverURL = oldURL }()

	outDir := t.TempDir()
	cliCtx := &registry.CLIContext{Client: mock}

	err := runBackup(context.Background(), cliCtx, backupOptions{
		OutputDir:   outDir,
		Format:      "json",
		Resources:   "categories",
		IncludeIDs:  true,
		Concurrency: 2,
	})
	if err != nil {
		t.Fatalf("runBackup error: %v", err)
	}

	catPath := filepath.Join(outDir, "categories", "productivity.json")
	content, err := os.ReadFile(catPath)
	if err != nil {
		t.Fatalf("reading JSON file: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("parsing JSON: %v", err)
	}

	// id should be present (IncludeIDs=true)
	if _, ok := parsed["id"]; !ok {
		t.Error("id should be present when IncludeIDs=true")
	}
}

func TestBackup_PartialFailure(t *testing.T) {
	mock := &backupMockClient{
		responses: map[string]overviewMockResponse{
			// Policies: list succeeds, one detail fails
			"/JSSResource/policies":      {200, `{"policies":[{"id":1,"name":"Good"},{"id":2,"name":"Bad"}]}`},
			"/JSSResource/policies/id/1": {200, `{"policy":{"general":{"id":1,"name":"Good"}}}`},
			"/JSSResource/policies/id/2": {403, `{"httpStatus":403}`},
		},
	}

	oldURL := serverURL
	serverURL = "https://test.jamfcloud.com"
	defer func() { serverURL = oldURL }()

	outDir := t.TempDir()
	cliCtx := &registry.CLIContext{Client: mock}

	err := runBackup(context.Background(), cliCtx, backupOptions{
		OutputDir:   outDir,
		Format:      "yaml",
		Resources:   "policies",
		Concurrency: 2,
	})
	// Should return error indicating failures occurred
	if err == nil {
		t.Fatal("runBackup should return error when failures exist")
	}

	// Good policy should be exported
	if _, err := os.Stat(filepath.Join(outDir, "policies", "good.yaml")); os.IsNotExist(err) {
		t.Error("good.yaml should exist")
	}

	// Failures manifest should exist
	failPath := filepath.Join(outDir, "_failures.yaml")
	if _, err := os.Stat(failPath); os.IsNotExist(err) {
		t.Error("_failures.yaml should exist")
	}
}

func TestBackup_ResourceFilter(t *testing.T) {
	// Only policies mock — scripts should not be attempted
	mock := &backupMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/policies":      {200, `{"policies":[{"id":1,"name":"Test"}]}`},
			"/JSSResource/policies/id/1": {200, `{"policy":{"general":{"name":"Test"}}}`},
		},
	}

	oldURL := serverURL
	serverURL = "https://test.jamfcloud.com"
	defer func() { serverURL = oldURL }()

	outDir := t.TempDir()
	cliCtx := &registry.CLIContext{Client: mock}

	err := runBackup(context.Background(), cliCtx, backupOptions{
		OutputDir:   outDir,
		Format:      "yaml",
		Resources:   "policies",
		Concurrency: 2,
	})
	if err != nil {
		t.Fatalf("runBackup error: %v", err)
	}

	// Scripts directory should NOT exist (not requested)
	if _, err := os.Stat(filepath.Join(outDir, "scripts")); !os.IsNotExist(err) {
		t.Error("scripts directory should not be created when filtering to policies only")
	}
}

func TestBackup_DuplicateNames(t *testing.T) {
	mock := &backupMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/policies":      {200, `{"policies":[{"id":1,"name":"Test"},{"id":2,"name":"Test"}]}`},
			"/JSSResource/policies/id/1": {200, `{"policy":{"general":{"name":"Test","enabled":true}}}`},
			"/JSSResource/policies/id/2": {200, `{"policy":{"general":{"name":"Test","enabled":false}}}`},
		},
	}

	oldURL := serverURL
	serverURL = "https://test.jamfcloud.com"
	defer func() { serverURL = oldURL }()

	outDir := t.TempDir()
	cliCtx := &registry.CLIContext{Client: mock}

	err := runBackup(context.Background(), cliCtx, backupOptions{
		OutputDir:   outDir,
		Format:      "yaml",
		Resources:   "policies",
		Concurrency: 2,
	})
	if err != nil {
		t.Fatalf("runBackup error: %v", err)
	}

	entries, _ := os.ReadDir(filepath.Join(outDir, "policies"))
	if len(entries) != 2 {
		t.Errorf("expected 2 files for duplicate names, got %d", len(entries))
	}

	// Should have test.yaml and test-2.yaml
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name()] = true
	}
	if !names["test.yaml"] {
		t.Error("missing test.yaml")
	}
	if !names["test-2.yaml"] {
		t.Error("missing test-2.yaml (deduplicated)")
	}
}

func TestBackup_ComputerGroups(t *testing.T) {
	mock := &backupMockClient{
		responses: map[string]overviewMockResponse{
			// v2 smart group list (paginated)
			"/v2/computer-groups/smart-groups": {200, `{"totalCount":1,"results":[{"id":"10","name":"All Laptops","membershipCount":42}]}`},
			// v2 smart group detail
			"/v2/computer-groups/smart-groups/10": {200, `{"name":"All Laptops","description":"Laptops only","criteria":[{"name":"Model","priority":0,"andOr":"and","searchType":"like","value":"MacBook"}],"siteId":"-1"}`},
			// v2 static group list (paginated)
			"/v2/computer-groups/static-groups": {200, `{"totalCount":1,"results":[{"id":"20","name":"Lab Macs","count":5}]}`},
			// v2 static group detail
			"/v2/computer-groups/static-groups/20": {200, `{"id":"20","name":"Lab Macs","description":"Computer lab","siteId":"-1"}`},
			// mobile smart/static (empty — same Resource Name as computer groups; FilterResources runs all)
			"/v1/mobile-device-groups/smart-groups":  {200, `[]`},
			"/v1/mobile-device-groups/static-groups": {200, `{"totalCount":0,"results":[]}`},
		},
	}

	oldURL := serverURL
	serverURL = "https://test.jamfcloud.com"
	defer func() { serverURL = oldURL }()

	outDir := t.TempDir()
	cliCtx := &registry.CLIContext{Client: mock}

	err := runBackup(context.Background(), cliCtx, backupOptions{
		OutputDir:   outDir,
		Format:      "yaml",
		Resources:   "smart-groups,static-groups",
		IncludeIDs:  false,
		Concurrency: 2,
	})
	if err != nil {
		t.Fatalf("runBackup error: %v", err)
	}

	// Smart group should be in smart-groups/computers/
	smartDir := filepath.Join(outDir, "smart-groups", "computers")
	entries, err := os.ReadDir(smartDir)
	if err != nil {
		t.Fatalf("reading smart-groups/computers: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 smart group file, got %d", len(entries))
	}

	smartPath := filepath.Join(smartDir, "all-laptops.yaml")
	content, err := os.ReadFile(smartPath)
	if err != nil {
		t.Fatalf("reading smart group file: %v", err)
	}
	var smartParsed map[string]any
	if err := yaml.Unmarshal(content, &smartParsed); err != nil {
		t.Fatalf("parsing smart group YAML: %v", err)
	}
	if smartParsed["name"] != "All Laptops" {
		t.Errorf("expected smart group name 'All Laptops', got %v", smartParsed["name"])
	}
	criteria, ok := smartParsed["criteria"].([]any)
	if !ok || len(criteria) == 0 {
		t.Error("smart group should contain criteria")
	}

	// Static group should be in static-groups/computers/
	staticDir := filepath.Join(outDir, "static-groups", "computers")
	entries, err = os.ReadDir(staticDir)
	if err != nil {
		t.Fatalf("reading static-groups/computers: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 static group file, got %d", len(entries))
	}

	staticPath := filepath.Join(staticDir, "lab-macs.yaml")
	content, err = os.ReadFile(staticPath)
	if err != nil {
		t.Fatalf("reading static group file: %v", err)
	}
	var staticParsed map[string]any
	if err := yaml.Unmarshal(content, &staticParsed); err != nil {
		t.Fatalf("parsing static group YAML: %v", err)
	}
	if staticParsed["name"] != "Lab Macs" {
		t.Errorf("expected static group name 'Lab Macs', got %v", staticParsed["name"])
	}
}

func TestBackup_MobileDeviceGroups(t *testing.T) {
	mock := &backupMockClient{
		responses: map[string]overviewMockResponse{
			// Computer smart/static: empty (this test targets mobile list shape groupId/groupName)
			"/v2/computer-groups/smart-groups":  {200, `{"totalCount":0,"results":[]}`},
			"/v2/computer-groups/static-groups": {200, `{"totalCount":0,"results":[]}`},
			// Mobile smart: list uses groupId + groupName per Jamf API
			"/v1/mobile-device-groups/smart-groups": {200, `{"totalCount":1,"results":[{"groupId":"5","groupName":"iPad Lab","siteId":"-1","count":3}]}`},
			"/v1/mobile-device-groups/smart-groups/5": {200, `{"groupId":"5","groupName":"iPad Lab","criteria":[{"name":"Model","andOr":"and","searchType":"is","value":"iPad"}],"siteId":"-1"}`},
			// Mobile static
			"/v1/mobile-device-groups/static-groups": {200, `{"totalCount":1,"results":[{"groupId":"7","groupName":"Loaner iPads","siteId":"-1","count":0}]}`},
			"/v1/mobile-device-groups/static-groups/7": {200, `{"groupId":"7","groupName":"Loaner iPads","groupDescription":"desc","siteId":"-1"}`},
		},
	}

	oldURL := serverURL
	serverURL = "https://test.jamfcloud.com"
	defer func() { serverURL = oldURL }()

	outDir := t.TempDir()
	cliCtx := &registry.CLIContext{Client: mock}

	err := runBackup(context.Background(), cliCtx, backupOptions{
		OutputDir:   outDir,
		Format:      "yaml",
		Resources:   "smart-groups,static-groups",
		IncludeIDs:  false,
		Concurrency: 2,
	})
	if err != nil {
		t.Fatalf("runBackup error: %v", err)
	}

	smartMobileDir := filepath.Join(outDir, "smart-groups", "mobile")
	entries, err := os.ReadDir(smartMobileDir)
	if err != nil {
		t.Fatalf("reading smart-groups/mobile: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 mobile smart group file, got %d", len(entries))
	}

	staticMobileDir := filepath.Join(outDir, "static-groups", "mobile")
	entries, err = os.ReadDir(staticMobileDir)
	if err != nil {
		t.Fatalf("reading static-groups/mobile: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 mobile static group file, got %d", len(entries))
	}
}

func TestExtractID(t *testing.T) {
	tests := []struct {
		input map[string]any
		want  string
	}{
		{map[string]any{"id": "42"}, "42"},
		{map[string]any{"id": float64(42)}, "42"},
		{map[string]any{"id": nil}, ""},
		{map[string]any{}, ""},
	}
	for _, tt := range tests {
		got := extractID(tt.input)
		if got != tt.want {
			t.Errorf("extractID(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestUnwrapClassicDetail(t *testing.T) {
	// Single-key wrapper
	wrapped := map[string]any{
		"policy": map[string]any{"name": "Test"},
	}
	result := unwrapClassicDetail(wrapped)
	if result["name"] != "Test" {
		t.Errorf("expected unwrapped name=Test, got %v", result)
	}

	// Non-wrapper (multiple keys)
	multi := map[string]any{
		"name":    "Test",
		"enabled": true,
	}
	result2 := unwrapClassicDetail(multi)
	if result2["name"] != "Test" || result2["enabled"] != true {
		t.Errorf("multi-key map should pass through, got %v", result2)
	}
}
