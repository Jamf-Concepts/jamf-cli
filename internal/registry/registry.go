// Package registry defines the shared interfaces and types used by all
// product command packages (pro, protect, etc.) and the root command.
package registry

import (
	"context"
	"io"
	"net/http"

	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

// HTTPClient interface for making API requests.
type HTTPClient interface {
	Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error)
}

// OutputFormatter interface for formatting output.
type OutputFormatter interface {
	PrintResponse(resp *http.Response) error
	PrintRaw(data []byte) error
}

// ProtectClient defines the interface for Jamf Protect API operations.
// The SDK's *jamfprotect.Client satisfies this interface directly.
type ProtectClient interface {
	// Plans
	ListPlans(ctx context.Context) ([]jamfprotect.Plan, error)
	GetPlan(ctx context.Context, id string) (*jamfprotect.Plan, error)
	CreatePlan(ctx context.Context, input jamfprotect.PlanInput) (jamfprotect.Plan, error)
	UpdatePlan(ctx context.Context, id string, input jamfprotect.PlanInput) (jamfprotect.Plan, error)
	DeletePlan(ctx context.Context, id string) error
	GetPlansConfigProfile(ctx context.Context, id string, input *jamfprotect.PlanConfigProfileOptionsInput) (string, error)

	// Computers
	ListComputers(ctx context.Context) ([]jamfprotect.Computer, error)
	GetComputer(ctx context.Context, uuid string) (*jamfprotect.Computer, error)

	// Analytics
	ListAnalytics(ctx context.Context) ([]jamfprotect.Analytic, error)
	GetAnalytic(ctx context.Context, uuid string) (*jamfprotect.Analytic, error)
	CreateAnalytic(ctx context.Context, input jamfprotect.AnalyticInput) (jamfprotect.Analytic, error)
	UpdateAnalytic(ctx context.Context, uuid string, input jamfprotect.AnalyticInput) (jamfprotect.Analytic, error)
	DeleteAnalytic(ctx context.Context, uuid string) error

	// Analytic Sets
	ListAnalyticSets(ctx context.Context) ([]jamfprotect.AnalyticSet, error)
	GetAnalyticSet(ctx context.Context, uuid string) (*jamfprotect.AnalyticSet, error)
	CreateAnalyticSet(ctx context.Context, input jamfprotect.AnalyticSetInput) (jamfprotect.AnalyticSet, error)
	UpdateAnalyticSet(ctx context.Context, uuid string, input jamfprotect.AnalyticSetInput) (jamfprotect.AnalyticSet, error)
	DeleteAnalyticSet(ctx context.Context, uuid string) error

	// Exception Sets
	ListExceptionSets(ctx context.Context) ([]jamfprotect.ExceptionSetListItem, error)
	GetExceptionSet(ctx context.Context, uuid string) (*jamfprotect.ExceptionSet, error)
	CreateExceptionSet(ctx context.Context, input jamfprotect.ExceptionSetInput) (jamfprotect.ExceptionSet, error)
	UpdateExceptionSet(ctx context.Context, uuid string, input jamfprotect.ExceptionSetInput) (jamfprotect.ExceptionSet, error)
	DeleteExceptionSet(ctx context.Context, uuid string) error

	// Removable Storage Control Sets
	ListRemovableStorageControlSets(ctx context.Context) ([]jamfprotect.RemovableStorageControlSet, error)
	GetRemovableStorageControlSet(ctx context.Context, id string) (*jamfprotect.RemovableStorageControlSet, error)
	CreateRemovableStorageControlSet(ctx context.Context, input jamfprotect.RemovableStorageControlSetInput) (jamfprotect.RemovableStorageControlSet, error)
	UpdateRemovableStorageControlSet(ctx context.Context, id string, input jamfprotect.RemovableStorageControlSetInput) (jamfprotect.RemovableStorageControlSet, error)
	DeleteRemovableStorageControlSet(ctx context.Context, id string) error

	// Action Configs
	ListActionConfigs(ctx context.Context) ([]jamfprotect.ActionConfigListItem, error)
	GetActionConfig(ctx context.Context, id string) (*jamfprotect.ActionConfig, error)
	CreateActionConfig(ctx context.Context, input jamfprotect.ActionConfigInput) (jamfprotect.ActionConfig, error)
	UpdateActionConfig(ctx context.Context, id string, input jamfprotect.ActionConfigInput) (jamfprotect.ActionConfig, error)
	DeleteActionConfig(ctx context.Context, id string) error

	// Telemetry V2
	ListTelemetriesV2(ctx context.Context) ([]jamfprotect.TelemetryV2, error)
	GetTelemetryV2(ctx context.Context, id string) (*jamfprotect.TelemetryV2, error)
	CreateTelemetryV2(ctx context.Context, input jamfprotect.TelemetryV2Input) (jamfprotect.TelemetryV2, error)
	UpdateTelemetryV2(ctx context.Context, id string, input jamfprotect.TelemetryV2Input) (jamfprotect.TelemetryV2, error)
	DeleteTelemetryV2(ctx context.Context, id string) error

	// Custom Prevent Lists
	ListCustomPreventLists(ctx context.Context) ([]jamfprotect.CustomPreventList, error)
	GetCustomPreventList(ctx context.Context, id string) (*jamfprotect.CustomPreventList, error)
	CreateCustomPreventList(ctx context.Context, input jamfprotect.CustomPreventListInput) (jamfprotect.CustomPreventList, error)
	UpdateCustomPreventList(ctx context.Context, id string, input jamfprotect.CustomPreventListInput) (jamfprotect.CustomPreventList, error)
	DeleteCustomPreventList(ctx context.Context, id string) error

	// Unified Logging Filters
	ListUnifiedLoggingFilters(ctx context.Context) ([]jamfprotect.UnifiedLoggingFilter, error)
	GetUnifiedLoggingFilter(ctx context.Context, uuid string) (*jamfprotect.UnifiedLoggingFilter, error)
	CreateUnifiedLoggingFilter(ctx context.Context, input jamfprotect.UnifiedLoggingFilterInput) (jamfprotect.UnifiedLoggingFilter, error)
	UpdateUnifiedLoggingFilter(ctx context.Context, uuid string, input jamfprotect.UnifiedLoggingFilterInput) (jamfprotect.UnifiedLoggingFilter, error)
	DeleteUnifiedLoggingFilter(ctx context.Context, uuid string) error

	// Roles
	ListRoles(ctx context.Context) ([]jamfprotect.Role, error)
	GetRole(ctx context.Context, id string) (*jamfprotect.Role, error)
	CreateRole(ctx context.Context, input jamfprotect.RoleInput) (jamfprotect.Role, error)
	UpdateRole(ctx context.Context, id string, input jamfprotect.RoleInput) (jamfprotect.Role, error)
	DeleteRole(ctx context.Context, id string) error

	// Users
	ListUsers(ctx context.Context) ([]jamfprotect.User, error)
	GetUser(ctx context.Context, id string) (*jamfprotect.User, error)
	CreateUser(ctx context.Context, input jamfprotect.UserInput) (jamfprotect.User, error)
	UpdateUser(ctx context.Context, id string, input jamfprotect.UserInput) (jamfprotect.User, error)
	DeleteUser(ctx context.Context, id string) error

	// Groups
	ListGroups(ctx context.Context) ([]jamfprotect.Group, error)
	GetGroup(ctx context.Context, id string) (*jamfprotect.Group, error)
	CreateGroup(ctx context.Context, input jamfprotect.GroupInput) (jamfprotect.Group, error)
	UpdateGroup(ctx context.Context, id string, input jamfprotect.GroupInput) (jamfprotect.Group, error)
	DeleteGroup(ctx context.Context, id string) error

	// API Clients
	ListApiClients(ctx context.Context) ([]jamfprotect.ApiClient, error)
	GetApiClient(ctx context.Context, clientID string) (*jamfprotect.ApiClient, error)
	CreateApiClient(ctx context.Context, input jamfprotect.ApiClientInput) (jamfprotect.ApiClient, error)
	UpdateApiClient(ctx context.Context, clientID string, input jamfprotect.ApiClientInput) (jamfprotect.ApiClient, error)
	DeleteApiClient(ctx context.Context, clientID string) error

	// Organization
	GetDataForwarding(ctx context.Context) (jamfprotect.DataForwardingResult, error)
	UpdateDataForwarding(ctx context.Context, input jamfprotect.DataForwardingInput) (jamfprotect.DataForwardingResult, error)
	GetDataRetention(ctx context.Context) (jamfprotect.DataRetentionSettings, error)
	UpdateDataRetention(ctx context.Context, input jamfprotect.DataRetentionInput) (jamfprotect.DataRetentionSettings, error)
	GetOrganizationDownloads(ctx context.Context) (jamfprotect.OrganizationDownloads, error)
	GetConfigFreeze(ctx context.Context) (jamfprotect.ChangeManagementConfig, error)
	UpdateOrganizationConfigFreeze(ctx context.Context, configFreeze bool) (jamfprotect.ChangeManagementConfig, error)
	ListConnections(ctx context.Context) ([]jamfprotect.Connection, error)

	// Client metadata
	BaseURL() string
	AccessToken(ctx context.Context) (*jamfprotect.Token, error)
}

// CLIContext holds the shared client and output formatter for all commands.
// It is populated in PersistentPreRunE after token/URL resolution.
type CLIContext struct {
	Client        HTTPClient
	Output        OutputFormatter
	ProtectClient ProtectClient
}
