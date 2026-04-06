// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"testing"
	"time"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func TestCheckFailedMDMCommands(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v2/mdm/commands": {200, `{"totalCount":15,"results":[]}`},
		},
	}

	result, err := checkFailedMDMCommands(context.Background(), client, 14)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
		return
	}
	if result.AffectedCount != 15 {
		t.Errorf("affected = %d, want 15", result.AffectedCount)
	}
	if result.Severity != severityWarning {
		t.Errorf("severity = %q, want %q", result.Severity, severityWarning)
	}
}

func TestCheckFailedMDMCommands_Critical(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v2/mdm/commands": {200, `{"totalCount":150,"results":[]}`},
		},
	}

	result, err := checkFailedMDMCommands(context.Background(), client, 14)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Severity != severityCritical {
		t.Errorf("severity = %q, want %q for >100 failed commands", result.Severity, severityCritical)
	}
}

func TestCheckFailedMDMCommands_None(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v2/mdm/commands": {200, `{"totalCount":0,"results":[]}`},
		},
	}

	result, err := checkFailedMDMCommands(context.Background(), client, 14)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result when no failed commands")
	}
}

func TestCheckDEPTokenExpiry(t *testing.T) {
	// Set timeNow to a fixed time for predictable test
	oldTimeNow := timeNow
	timeNow = func() time.Time { return time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC) }
	defer func() { timeNow = oldTimeNow }()

	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/device-enrollments": {200, `{
				"totalCount": 2,
				"results": [
					{"id": "1", "tokenExpirationDate": "2026-04-01"},
					{"id": "2", "tokenExpirationDate": "2027-12-31"}
				]
			}`},
		},
	}

	result, err := checkDEPTokenExpiry(context.Background(), client, 14)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected finding for expiring token")
		return
	}
	if result.AffectedCount != 1 {
		t.Errorf("affected = %d, want 1 (only the 2026-04-01 token)", result.AffectedCount)
	}
}

func TestCheckDEPTokenExpiry_SingleToken_Warning(t *testing.T) {
	oldTimeNow := timeNow
	timeNow = func() time.Time { return time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC) }
	defer func() { timeNow = oldTimeNow }()

	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/device-enrollments": {200, `{
				"totalCount": 1,
				"results": [
					{"id": "1", "tokenExpirationDate": "2026-04-01"}
				]
			}`},
		},
	}

	result, err := checkDEPTokenExpiry(context.Background(), client, 14)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected finding for expiring token")
		return
	}
	if result.Severity != severityWarning {
		t.Errorf("severity = %q, want %q for single expiring token", result.Severity, severityWarning)
	}
}

func TestCheckDEPTokenExpiry_MultipleTokens_Critical(t *testing.T) {
	oldTimeNow := timeNow
	timeNow = func() time.Time { return time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC) }
	defer func() { timeNow = oldTimeNow }()

	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/device-enrollments": {200, `{
				"totalCount": 2,
				"results": [
					{"id": "1", "tokenExpirationDate": "2026-04-01"},
					{"id": "2", "tokenExpirationDate": "2026-03-20"}
				]
			}`},
		},
	}

	result, err := checkDEPTokenExpiry(context.Background(), client, 14)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected finding for expiring tokens")
		return
	}
	if result.AffectedCount != 2 {
		t.Errorf("affected = %d, want 2", result.AffectedCount)
	}
	if result.Severity != severityCritical {
		t.Errorf("severity = %q, want %q for multiple expiring tokens", result.Severity, severityCritical)
	}
}

func TestCheckPrestageCoverage_NoPresetages(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/device-enrollments": {200, `{"totalCount":2,"results":[{"id":"1"},{"id":"2"}]}`},
			"/v3/computer-prestages": {200, `{"totalCount":0,"results":[]}`},
		},
	}

	result, err := checkPrestageCoverage(context.Background(), client, 14)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected finding when no prestages configured")
		return
	}
	if result.Severity != severityWarning {
		t.Errorf("severity = %q, want %q", result.Severity, severityWarning)
	}
}

func TestCheckPrestageCoverage_HasPrestages(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/device-enrollments": {200, `{"totalCount":1,"results":[{"id":"1"}]}`},
			"/v3/computer-prestages": {200, `{"totalCount":3,"results":[]}`},
		},
	}

	result, err := checkPrestageCoverage(context.Background(), client, 14)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected no finding when prestages are configured")
	}
}

func TestCheckEmptySmartGroups(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/computer-groups": {200, `[
				{"id":"1","name":"All Macs","smartGroup":true,"memberCount":50},
				{"id":"2","name":"Empty Smart","smartGroup":true,"memberCount":0},
				{"id":"3","name":"Empty Static","smartGroup":false,"memberCount":0},
				{"id":"4","name":"Also Empty Smart","smartGroup":true,"memberCount":0}
			]`},
		},
	}

	result, err := checkEmptySmartGroups(context.Background(), client, 14)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected finding for empty smart groups")
		return
	}
	if result.AffectedCount != 2 {
		t.Errorf("affected = %d, want 2 (only smart groups)", result.AffectedCount)
	}
}

// --- Security check tests ---

func TestCheckUnencryptedDevices_Found(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v3/computers-inventory": {200, `{"totalCount":3,"results":[
				{"id":"1","diskEncryption":{"bootPartitionEncryptionDetails":{"partitionFileVault2State":"ENCRYPTED"}}},
				{"id":"2","diskEncryption":{"bootPartitionEncryptionDetails":{"partitionFileVault2State":"UNENCRYPTED"}}},
				{"id":"3","diskEncryption":{"bootPartitionEncryptionDetails":{"partitionFileVault2State":"DECRYPTED"}}}
			]}`},
		},
	}

	result, err := checkUnencryptedDevices(context.Background(), client, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected finding for unencrypted devices")
		return
	}
	if result.AffectedCount != 2 {
		t.Errorf("affected = %d, want 2 (UNENCRYPTED + DECRYPTED)", result.AffectedCount)
	}
	if result.Severity != severityCritical {
		t.Errorf("severity = %q, want %q", result.Severity, severityCritical)
	}
}

func TestCheckUnencryptedDevices_AllClean(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v3/computers-inventory": {200, `{"totalCount":2,"results":[
				{"id":"1","diskEncryption":{"bootPartitionEncryptionDetails":{"partitionFileVault2State":"ENCRYPTED"}}},
				{"id":"2","diskEncryption":{"bootPartitionEncryptionDetails":{"partitionFileVault2State":"ENCRYPTED"}}}
			]}`},
		},
	}

	result, err := checkUnencryptedDevices(context.Background(), client, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result when all devices are encrypted")
	}
}

func TestCheckUnencryptedDevices_NoDiskEncryptionSection(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v3/computers-inventory": {200, `{"totalCount":1,"results":[
				{"id":"1","general":{"name":"Mac1"}}
			]}`},
		},
	}

	result, err := checkUnencryptedDevices(context.Background(), client, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil when diskEncryption section missing (graceful skip)")
	}
}

func TestCheckUnencryptedDevices_EmptyDiskEncryptionSection(t *testing.T) {
	// diskEncryption present but bootPartitionEncryptionDetails absent — fileVaultStatus
	// returns "" which must not be counted as unencrypted.
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v3/computers-inventory": {200, `{"totalCount":1,"results":[
				{"id":"1","diskEncryption":{}}
			]}`},
		},
	}

	result, err := checkUnencryptedDevices(context.Background(), client, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil when bootPartitionEncryptionDetails absent (graceful skip, not counted as unencrypted)")
	}
}

func TestCheckGatekeeper_Found(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v3/computers-inventory": {200, `{"totalCount":3,"results":[
				{"id":"1","security":{"gatekeeperStatus":"ENABLED"}},
				{"id":"2","security":{"gatekeeperStatus":"DISABLED"}},
				{"id":"3","security":{"gatekeeperStatus":"Disabled"}}
			]}`},
		},
	}

	result, err := checkGatekeeper(context.Background(), client, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected finding for disabled Gatekeeper")
		return
	}
	if result.AffectedCount != 2 {
		t.Errorf("affected = %d, want 2 (DISABLED + Disabled)", result.AffectedCount)
	}
	if result.Severity != severityWarning {
		t.Errorf("severity = %q, want %q", result.Severity, severityWarning)
	}
}

func TestCheckGatekeeper_AllEnabled(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v3/computers-inventory": {200, `{"totalCount":1,"results":[
				{"id":"1","security":{"gatekeeperStatus":"ENABLED"}}
			]}`},
		},
	}

	result, err := checkGatekeeper(context.Background(), client, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil when all devices have Gatekeeper enabled")
	}
}

// --- Policies no scope test ---

func TestCheckPoliciesNoScope(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/policies": {200, `{"policies":[
				{"id":1,"name":"Scoped"},
				{"id":2,"name":"Unscoped"},
				{"id":3,"name":"AllComputers"}
			]}`},
			"/JSSResource/policies/id/1": {200, `{"policy":{"scope":{"all_computers":false,"computer_groups":[{"id":5}]}}}`},
			"/JSSResource/policies/id/2": {200, `{"policy":{"scope":{"all_computers":false}}}`},
			"/JSSResource/policies/id/3": {200, `{"policy":{"scope":{"all_computers":true}}}`},
		},
	}

	result, err := checkPoliciesNoScope(context.Background(), client, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected finding for unscoped policy")
		return
	}
	if result.AffectedCount != 1 {
		t.Errorf("affected = %d, want 1 (only id=2 has empty scope)", result.AffectedCount)
	}
}

func TestCheckPoliciesNoScope_AllScoped(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/policies":      {200, `{"policies":[{"id":1,"name":"P1"}]}`},
			"/JSSResource/policies/id/1": {200, `{"policy":{"scope":{"all_computers":true}}}`},
		},
	}

	result, err := checkPoliciesNoScope(context.Background(), client, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil when all policies are scoped")
	}
}

func TestCheckPoliciesNoScope_NoScopeKey(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/policies":      {200, `{"policies":[{"id":1,"name":"NoScope"}]}`},
			"/JSSResource/policies/id/1": {200, `{"policy":{"general":{"name":"NoScope"}}}`},
		},
	}

	result, err := checkPoliciesNoScope(context.Background(), client, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected finding when scope key is missing entirely")
		return
	}
	if result.AffectedCount != 1 {
		t.Errorf("affected = %d, want 1", result.AffectedCount)
	}
}

func TestCheckPoliciesNoScope_DetailFetchError(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/policies": {200, `{"policies":[{"id":1,"name":"P1"},{"id":2,"name":"P2"}]}`},
			// id=1 fails (403), id=2 succeeds with scope
			"/JSSResource/policies/id/1": {403, `{}`},
			"/JSSResource/policies/id/2": {200, `{"policy":{"scope":{"all_computers":true}}}`},
		},
	}

	result, err := checkPoliciesNoScope(context.Background(), client, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// id=1 errors are skipped (continue), id=2 is scoped → nil result
	if result != nil {
		t.Error("expected nil: failed fetches are skipped, remaining policy is scoped")
	}
}

// --- Empty categories test ---

func TestCheckEmptyCategories_None(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/categories": {200, `{"totalCount":0,"results":[]}`},
		},
	}

	result, err := checkEmptyCategories(context.Background(), client, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil when no categories exist")
	}
}

func TestRunAudit_FilterByCategory(t *testing.T) {
	// Only set up compliance mocks
	mock := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v3/computers-inventory": {200, `{"totalCount":0,"results":[]}`},
			"/v2/mdm/commands":        {200, `{"totalCount":5,"results":[]}`},
		},
	}

	oldFmt := outputFmt
	outputFmt = "json"
	defer func() { outputFmt = oldFmt }()

	cliCtx := &registry.CLIContext{Client: mock}
	// Should not error — security checks won't run
	err := runAudit(context.Background(), cliCtx, auditOptions{
		Checks: "compliance",
		Days:   14,
	})
	if err != nil {
		t.Fatalf("runAudit error: %v", err)
	}
}

func TestRunAudit_InvalidCategory(t *testing.T) {
	cliCtx := &registry.CLIContext{Client: &overviewMockClient{}}
	err := runAudit(context.Background(), cliCtx, auditOptions{
		Checks: "bogus",
		Days:   14,
	})
	if err == nil {
		t.Fatal("expected error for invalid category")
	}
}

func TestAllAuditChecks_Coverage(t *testing.T) {
	checks := allAuditChecks()
	categories := map[string]int{}
	for _, c := range checks {
		categories[c.Category]++
	}

	for _, cat := range []string{"security", "compliance", "hygiene", "enrollment"} {
		if categories[cat] == 0 {
			t.Errorf("expected at least one check in category %q", cat)
		}
	}
}
