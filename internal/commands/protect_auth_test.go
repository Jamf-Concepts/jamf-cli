// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

// ─── Minimal ProtectClient stub ──────────────────────────────────────────────
//
// stubProtectClientBase satisfies registry.ProtectClient with zero-value no-ops.
// Embed this in test clients and override only the methods you need.
//
// Embedding works because Go promotes the pointer-receiver methods of the
// embedded *stubProtectClientBase into the outer struct.

type stubProtectClientBase struct{}

func (s *stubProtectClientBase) ListPlans(_ context.Context) ([]jamfprotect.Plan, error) {
	return nil, nil
}

func (s *stubProtectClientBase) GetPlan(_ context.Context, _ string) (*jamfprotect.Plan, error) {
	return nil, nil
}

func (s *stubProtectClientBase) CreatePlan(_ context.Context, _ jamfprotect.PlanInput) (jamfprotect.Plan, error) {
	return jamfprotect.Plan{}, nil
}

func (s *stubProtectClientBase) UpdatePlan(_ context.Context, _ string, _ jamfprotect.PlanInput) (jamfprotect.Plan, error) {
	return jamfprotect.Plan{}, nil
}
func (s *stubProtectClientBase) DeletePlan(_ context.Context, _ string) error { return nil }
func (s *stubProtectClientBase) GetPlansConfigProfile(_ context.Context, _ string, _ *jamfprotect.PlanConfigProfileOptionsInput) (string, error) {
	return "", nil
}

func (s *stubProtectClientBase) ListComputers(_ context.Context) ([]jamfprotect.Computer, error) {
	return nil, nil
}

func (s *stubProtectClientBase) GetComputer(_ context.Context, _ string) (*jamfprotect.Computer, error) {
	return nil, nil
}
func (s *stubProtectClientBase) DeleteComputer(_ context.Context, _ string) error { return nil }
func (s *stubProtectClientBase) SetComputerPlan(_ context.Context, _ string, _ string) (*jamfprotect.Computer, error) {
	return nil, nil
}

func (s *stubProtectClientBase) UpdateComputer(_ context.Context, _ string, _ jamfprotect.ComputerUpdateInput) (*jamfprotect.Computer, error) {
	return nil, nil
}

func (s *stubProtectClientBase) ListAlerts(_ context.Context) ([]jamfprotect.Alert, error) {
	return nil, nil
}

func (s *stubProtectClientBase) GetAlert(_ context.Context, _ string) (*jamfprotect.Alert, error) {
	return nil, nil
}

func (s *stubProtectClientBase) GetAlertStatusCounts(_ context.Context) (jamfprotect.AlertStatusCounts, error) {
	return jamfprotect.AlertStatusCounts{}, nil
}

func (s *stubProtectClientBase) UpdateAlerts(_ context.Context, _ jamfprotect.AlertUpdateInput) ([]jamfprotect.Alert, error) {
	return nil, nil
}

func (s *stubProtectClientBase) ListInsights(_ context.Context) ([]jamfprotect.Insight, error) {
	return nil, nil
}

func (s *stubProtectClientBase) UpdateInsightStatus(_ context.Context, _ string, _ bool) (jamfprotect.Insight, error) {
	return jamfprotect.Insight{}, nil
}

func (s *stubProtectClientBase) ListInsightComputers(_ context.Context, _ string) ([]jamfprotect.InsightComputer, error) {
	return nil, nil
}

func (s *stubProtectClientBase) GetFleetComplianceScore(_ context.Context, _ string) (jamfprotect.ComplianceBaselineScore, error) {
	return jamfprotect.ComplianceBaselineScore{}, nil
}

func (s *stubProtectClientBase) ListAuditLogsByDate(_ context.Context, _ *jamfprotect.AuditLogDateRange) ([]jamfprotect.AuditLog, error) {
	return nil, nil
}

func (s *stubProtectClientBase) GetCount(_ context.Context) (jamfprotect.CountResponse, error) {
	return jamfprotect.CountResponse{}, nil
}

func (s *stubProtectClientBase) ListRiskiestComputers(_ context.Context, _ int, _ string) ([]jamfprotect.RiskyComputer, error) {
	return nil, nil
}

func (s *stubProtectClientBase) GetCurrentPermissions(_ context.Context) (jamfprotect.RolePermissions, error) {
	return jamfprotect.RolePermissions{}, nil
}

func (s *stubProtectClientBase) ListAnalytics(_ context.Context) ([]jamfprotect.Analytic, error) {
	return nil, nil
}

func (s *stubProtectClientBase) GetAnalytic(_ context.Context, _ string) (*jamfprotect.Analytic, error) {
	return nil, nil
}

func (s *stubProtectClientBase) CreateAnalytic(_ context.Context, _ jamfprotect.AnalyticInput) (jamfprotect.Analytic, error) {
	return jamfprotect.Analytic{}, nil
}

func (s *stubProtectClientBase) UpdateAnalytic(_ context.Context, _ string, _ jamfprotect.AnalyticInput) (jamfprotect.Analytic, error) {
	return jamfprotect.Analytic{}, nil
}

func (s *stubProtectClientBase) UpdateInternalAnalytic(_ context.Context, _ string, _ jamfprotect.InternalAnalyticInput) (jamfprotect.Analytic, error) {
	return jamfprotect.Analytic{}, nil
}
func (s *stubProtectClientBase) DeleteAnalytic(_ context.Context, _ string) error { return nil }

func (s *stubProtectClientBase) ListAnalyticSets(_ context.Context) ([]jamfprotect.AnalyticSet, error) {
	return nil, nil
}

func (s *stubProtectClientBase) GetAnalyticSet(_ context.Context, _ string) (*jamfprotect.AnalyticSet, error) {
	return nil, nil
}

func (s *stubProtectClientBase) CreateAnalyticSet(_ context.Context, _ jamfprotect.AnalyticSetInput) (jamfprotect.AnalyticSet, error) {
	return jamfprotect.AnalyticSet{}, nil
}

func (s *stubProtectClientBase) UpdateAnalyticSet(_ context.Context, _ string, _ jamfprotect.AnalyticSetInput) (jamfprotect.AnalyticSet, error) {
	return jamfprotect.AnalyticSet{}, nil
}
func (s *stubProtectClientBase) DeleteAnalyticSet(_ context.Context, _ string) error { return nil }

func (s *stubProtectClientBase) ListExceptionSets(_ context.Context) ([]jamfprotect.ExceptionSetListItem, error) {
	return nil, nil
}

func (s *stubProtectClientBase) GetExceptionSet(_ context.Context, _ string) (*jamfprotect.ExceptionSet, error) {
	return nil, nil
}

func (s *stubProtectClientBase) CreateExceptionSet(_ context.Context, _ jamfprotect.ExceptionSetInput) (jamfprotect.ExceptionSet, error) {
	return jamfprotect.ExceptionSet{}, nil
}

func (s *stubProtectClientBase) UpdateExceptionSet(_ context.Context, _ string, _ jamfprotect.ExceptionSetInput) (jamfprotect.ExceptionSet, error) {
	return jamfprotect.ExceptionSet{}, nil
}
func (s *stubProtectClientBase) DeleteExceptionSet(_ context.Context, _ string) error { return nil }

func (s *stubProtectClientBase) ListRemovableStorageControlSets(_ context.Context) ([]jamfprotect.RemovableStorageControlSet, error) {
	return nil, nil
}

func (s *stubProtectClientBase) GetRemovableStorageControlSet(_ context.Context, _ string) (*jamfprotect.RemovableStorageControlSet, error) {
	return nil, nil
}

func (s *stubProtectClientBase) CreateRemovableStorageControlSet(_ context.Context, _ jamfprotect.RemovableStorageControlSetInput) (jamfprotect.RemovableStorageControlSet, error) {
	return jamfprotect.RemovableStorageControlSet{}, nil
}

func (s *stubProtectClientBase) UpdateRemovableStorageControlSet(_ context.Context, _ string, _ jamfprotect.RemovableStorageControlSetInput) (jamfprotect.RemovableStorageControlSet, error) {
	return jamfprotect.RemovableStorageControlSet{}, nil
}

func (s *stubProtectClientBase) DeleteRemovableStorageControlSet(_ context.Context, _ string) error {
	return nil
}

func (s *stubProtectClientBase) ListActionConfigs(_ context.Context) ([]jamfprotect.ActionConfigListItem, error) {
	return nil, nil
}

func (s *stubProtectClientBase) GetActionConfig(_ context.Context, _ string) (*jamfprotect.ActionConfig, error) {
	return nil, nil
}

func (s *stubProtectClientBase) CreateActionConfig(_ context.Context, _ jamfprotect.ActionConfigInput) (jamfprotect.ActionConfig, error) {
	return jamfprotect.ActionConfig{}, nil
}

func (s *stubProtectClientBase) UpdateActionConfig(_ context.Context, _ string, _ jamfprotect.ActionConfigInput) (jamfprotect.ActionConfig, error) {
	return jamfprotect.ActionConfig{}, nil
}
func (s *stubProtectClientBase) DeleteActionConfig(_ context.Context, _ string) error { return nil }

func (s *stubProtectClientBase) ListTelemetriesV2(_ context.Context) ([]jamfprotect.TelemetryV2, error) {
	return nil, nil
}

func (s *stubProtectClientBase) GetTelemetryV2(_ context.Context, _ string) (*jamfprotect.TelemetryV2, error) {
	return nil, nil
}

func (s *stubProtectClientBase) CreateTelemetryV2(_ context.Context, _ jamfprotect.TelemetryV2Input) (jamfprotect.TelemetryV2, error) {
	return jamfprotect.TelemetryV2{}, nil
}

func (s *stubProtectClientBase) UpdateTelemetryV2(_ context.Context, _ string, _ jamfprotect.TelemetryV2Input) (jamfprotect.TelemetryV2, error) {
	return jamfprotect.TelemetryV2{}, nil
}
func (s *stubProtectClientBase) DeleteTelemetryV2(_ context.Context, _ string) error { return nil }

func (s *stubProtectClientBase) ListCustomPreventLists(_ context.Context) ([]jamfprotect.CustomPreventList, error) {
	return nil, nil
}

func (s *stubProtectClientBase) GetCustomPreventList(_ context.Context, _ string) (*jamfprotect.CustomPreventList, error) {
	return nil, nil
}

func (s *stubProtectClientBase) CreateCustomPreventList(_ context.Context, _ jamfprotect.CustomPreventListInput) (jamfprotect.CustomPreventList, error) {
	return jamfprotect.CustomPreventList{}, nil
}

func (s *stubProtectClientBase) UpdateCustomPreventList(_ context.Context, _ string, _ jamfprotect.CustomPreventListInput) (jamfprotect.CustomPreventList, error) {
	return jamfprotect.CustomPreventList{}, nil
}

func (s *stubProtectClientBase) DeleteCustomPreventList(_ context.Context, _ string) error {
	return nil
}

func (s *stubProtectClientBase) ListUnifiedLoggingFilters(_ context.Context) ([]jamfprotect.UnifiedLoggingFilter, error) {
	return nil, nil
}

func (s *stubProtectClientBase) GetUnifiedLoggingFilter(_ context.Context, _ string) (*jamfprotect.UnifiedLoggingFilter, error) {
	return nil, nil
}

func (s *stubProtectClientBase) CreateUnifiedLoggingFilter(_ context.Context, _ jamfprotect.UnifiedLoggingFilterInput) (jamfprotect.UnifiedLoggingFilter, error) {
	return jamfprotect.UnifiedLoggingFilter{}, nil
}

func (s *stubProtectClientBase) UpdateUnifiedLoggingFilter(_ context.Context, _ string, _ jamfprotect.UnifiedLoggingFilterInput) (jamfprotect.UnifiedLoggingFilter, error) {
	return jamfprotect.UnifiedLoggingFilter{}, nil
}

func (s *stubProtectClientBase) DeleteUnifiedLoggingFilter(_ context.Context, _ string) error {
	return nil
}

func (s *stubProtectClientBase) ListUnifiedLoggingFilterSets(_ context.Context) ([]jamfprotect.UnifiedLoggingFilterSet, error) {
	return nil, nil
}

func (s *stubProtectClientBase) GetUnifiedLoggingFilterSet(_ context.Context, _ string) (*jamfprotect.UnifiedLoggingFilterSet, error) {
	return nil, nil
}

func (s *stubProtectClientBase) CreateUnifiedLoggingFilterSet(_ context.Context, _ jamfprotect.UnifiedLoggingFilterSetInput) (jamfprotect.UnifiedLoggingFilterSet, error) {
	return jamfprotect.UnifiedLoggingFilterSet{}, nil
}

func (s *stubProtectClientBase) UpdateUnifiedLoggingFilterSet(_ context.Context, _ string, _ jamfprotect.UnifiedLoggingFilterSetInput) (jamfprotect.UnifiedLoggingFilterSet, error) {
	return jamfprotect.UnifiedLoggingFilterSet{}, nil
}

func (s *stubProtectClientBase) DeleteUnifiedLoggingFilterSet(_ context.Context, _ string) error {
	return nil
}

func (s *stubProtectClientBase) ListRoles(_ context.Context) ([]jamfprotect.Role, error) {
	return nil, nil
}

func (s *stubProtectClientBase) GetRole(_ context.Context, _ string) (*jamfprotect.Role, error) {
	return nil, nil
}

func (s *stubProtectClientBase) CreateRole(_ context.Context, _ jamfprotect.RoleInput) (jamfprotect.Role, error) {
	return jamfprotect.Role{}, nil
}

func (s *stubProtectClientBase) UpdateRole(_ context.Context, _ string, _ jamfprotect.RoleInput) (jamfprotect.Role, error) {
	return jamfprotect.Role{}, nil
}
func (s *stubProtectClientBase) DeleteRole(_ context.Context, _ string) error { return nil }

func (s *stubProtectClientBase) ListUsers(_ context.Context) ([]jamfprotect.User, error) {
	return nil, nil
}

func (s *stubProtectClientBase) GetUser(_ context.Context, _ string) (*jamfprotect.User, error) {
	return nil, nil
}

func (s *stubProtectClientBase) CreateUser(_ context.Context, _ jamfprotect.UserInput) (jamfprotect.User, error) {
	return jamfprotect.User{}, nil
}

func (s *stubProtectClientBase) UpdateUser(_ context.Context, _ string, _ jamfprotect.UserInput) (jamfprotect.User, error) {
	return jamfprotect.User{}, nil
}
func (s *stubProtectClientBase) DeleteUser(_ context.Context, _ string) error { return nil }

func (s *stubProtectClientBase) ListGroups(_ context.Context) ([]jamfprotect.Group, error) {
	return nil, nil
}

func (s *stubProtectClientBase) GetGroup(_ context.Context, _ string) (*jamfprotect.Group, error) {
	return nil, nil
}

func (s *stubProtectClientBase) CreateGroup(_ context.Context, _ jamfprotect.GroupInput) (jamfprotect.Group, error) {
	return jamfprotect.Group{}, nil
}

func (s *stubProtectClientBase) UpdateGroup(_ context.Context, _ string, _ jamfprotect.GroupInput) (jamfprotect.Group, error) {
	return jamfprotect.Group{}, nil
}
func (s *stubProtectClientBase) DeleteGroup(_ context.Context, _ string) error { return nil }

func (s *stubProtectClientBase) ListApiClients(_ context.Context) ([]jamfprotect.ApiClient, error) {
	return nil, nil
}

func (s *stubProtectClientBase) GetApiClient(_ context.Context, _ string) (*jamfprotect.ApiClient, error) {
	return nil, nil
}

func (s *stubProtectClientBase) CreateApiClient(_ context.Context, _ jamfprotect.ApiClientInput) (jamfprotect.ApiClient, error) {
	return jamfprotect.ApiClient{}, nil
}

func (s *stubProtectClientBase) UpdateApiClient(_ context.Context, _ string, _ jamfprotect.ApiClientInput) (jamfprotect.ApiClient, error) {
	return jamfprotect.ApiClient{}, nil
}
func (s *stubProtectClientBase) DeleteApiClient(_ context.Context, _ string) error { return nil }

func (s *stubProtectClientBase) GetDataForwarding(_ context.Context) (jamfprotect.DataForwardingResult, error) {
	return jamfprotect.DataForwardingResult{}, nil
}

func (s *stubProtectClientBase) UpdateDataForwarding(_ context.Context, _ jamfprotect.DataForwardingInput) (jamfprotect.DataForwardingResult, error) {
	return jamfprotect.DataForwardingResult{}, nil
}

func (s *stubProtectClientBase) GetDataRetention(_ context.Context) (jamfprotect.DataRetentionSettings, error) {
	return jamfprotect.DataRetentionSettings{}, nil
}

func (s *stubProtectClientBase) UpdateDataRetention(_ context.Context, _ jamfprotect.DataRetentionInput) (jamfprotect.DataRetentionSettings, error) {
	return jamfprotect.DataRetentionSettings{}, nil
}

func (s *stubProtectClientBase) GetOrganizationDownloads(_ context.Context) (jamfprotect.OrganizationDownloads, error) {
	return jamfprotect.OrganizationDownloads{}, nil
}

func (s *stubProtectClientBase) GetConfigFreeze(_ context.Context) (jamfprotect.ChangeManagementConfig, error) {
	return jamfprotect.ChangeManagementConfig{}, nil
}

func (s *stubProtectClientBase) UpdateOrganizationConfigFreeze(_ context.Context, _ bool) (jamfprotect.ChangeManagementConfig, error) {
	return jamfprotect.ChangeManagementConfig{}, nil
}

func (s *stubProtectClientBase) ListConnections(_ context.Context) ([]jamfprotect.Connection, error) {
	return nil, nil
}

func (s *stubProtectClientBase) BaseURL() string { return "" }
func (s *stubProtectClientBase) AccessToken(_ context.Context) (*jamfprotect.Token, error) {
	return nil, nil
}

// ─── Test client ─────────────────────────────────────────────────────────────

// protectTokenTestClient overrides AccessToken to return a controllable token.
type protectTokenTestClient struct {
	*stubProtectClientBase
	token *jamfprotect.Token
	err   error
}

func (c *protectTokenTestClient) AccessToken(_ context.Context) (*jamfprotect.Token, error) {
	return c.token, c.err
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// runProtectAuthToken builds a protect auth token command, runs RunE, and
// returns the parsed JSON output map.
func runProtectAuthToken(t *testing.T, pc registry.ProtectClient) (map[string]any, error) {
	t.Helper()
	out := &captureOutput{}
	cliCtx := &registry.CLIContext{
		Output:        out,
		ProtectClient: pc,
	}
	cmd := newProtectAuthTokenCmd(cliCtx)
	cmd.SetContext(context.Background())
	if err := cmd.RunE(cmd, nil); err != nil {
		return nil, err
	}
	return parseAuthTokenOutput(t, out.rawData), nil
}

// ─── Structure test ───────────────────────────────────────────────────────────

func TestProtectAuthTokenSubcommandExists(t *testing.T) {
	protect := findProtectCmd(t)
	authCmd := findSubcommand(protect, "auth")
	if authCmd == nil {
		t.Fatal("auth command not found under protect")
		return
	}
	if findSubcommand(authCmd, "token") == nil {
		t.Fatal("expected 'token' subcommand under 'protect auth'")
		return
	}
}

// ─── RunE tests ──────────────────────────────────────────────────────────────

func TestProtectAuthToken_Output(t *testing.T) {
	expiry := time.Now().Add(30 * time.Minute)
	pc := &protectTokenTestClient{
		stubProtectClientBase: &stubProtectClientBase{},
		token: &jamfprotect.Token{
			AccessToken: "protect-jwt",
			TokenType:   "Bearer",
			Expiry:      expiry,
		},
	}
	m, err := runProtectAuthToken(t, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["token"] != "protect-jwt" {
		t.Errorf("token = %v, want %q", m["token"], "protect-jwt")
	}
	expiresAt, ok := m["expires_at"]
	if !ok {
		t.Fatal("expires_at must be present in output")
		return
	}
	ts, ok := expiresAt.(string)
	if !ok {
		t.Fatalf("expires_at is not a string: %T", expiresAt)
	}
	if _, err := time.Parse(time.RFC3339, ts); err != nil {
		t.Errorf("expires_at %q is not valid RFC3339: %v", ts, err)
	}
}

func TestProtectAuthToken_AccessTokenError(t *testing.T) {
	pc := &protectTokenTestClient{
		stubProtectClientBase: &stubProtectClientBase{},
		err:                   fmt.Errorf("protect service unavailable"),
	}
	_, err := runProtectAuthToken(t, pc)
	if err == nil {
		t.Fatal("expected error from AccessToken failure, got nil")
		return
	}
}

// ─── --refresh flag ───────────────────────────────────────────────────────────

func TestProtectAuthToken_Refresh_ClearsCacheBeforeAccessToken(t *testing.T) {
	expiry := time.Now().Add(30 * time.Minute)
	pc := &protectTokenTestClient{
		stubProtectClientBase: &stubProtectClientBase{},
		token: &jamfprotect.Token{
			AccessToken: "fresh-protect-token",
			TokenType:   "Bearer",
			Expiry:      expiry,
		},
	}

	var clearCalled bool
	out := &captureOutput{}
	cliCtx := &registry.CLIContext{
		Output:            out,
		ProtectClient:     pc,
		ClearProtectToken: func() { clearCalled = true },
	}
	cmd := newProtectAuthTokenCmd(cliCtx)
	cmd.SetContext(context.Background())
	if err := cmd.Flags().Set("refresh", "true"); err != nil {
		t.Fatalf("setting --refresh flag: %v", err)
	}
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !clearCalled {
		t.Error("expected ClearProtectToken to be called when --refresh is set")
	}

	m := parseAuthTokenOutput(t, out.rawData)
	if m["token"] != "fresh-protect-token" {
		t.Errorf("token = %v, want %q", m["token"], "fresh-protect-token")
	}
}

func TestProtectAuthToken_Refresh_NilClearFunc_NoError(t *testing.T) {
	// When ClearProtectToken is nil (no disk cache), --refresh is silently ignored.
	expiry := time.Now().Add(30 * time.Minute)
	pc := &protectTokenTestClient{
		stubProtectClientBase: &stubProtectClientBase{},
		token: &jamfprotect.Token{
			AccessToken: "nocache-token",
			Expiry:      expiry,
		},
	}
	out := &captureOutput{}
	cliCtx := &registry.CLIContext{
		Output:            out,
		ProtectClient:     pc,
		ClearProtectToken: nil, // no cache configured
	}
	cmd := newProtectAuthTokenCmd(cliCtx)
	cmd.SetContext(context.Background())
	if err := cmd.Flags().Set("refresh", "true"); err != nil {
		t.Fatalf("setting --refresh flag: %v", err)
	}
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("unexpected error with nil ClearProtectToken: %v", err)
	}
	m := parseAuthTokenOutput(t, out.rawData)
	if m["token"] != "nocache-token" {
		t.Errorf("token = %v, want %q", m["token"], "nocache-token")
	}
}
