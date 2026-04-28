// Copyright 2026, Jamf Software LLC

// Package registry defines the shared interfaces and types used by all
// product command packages (pro, protect, etc.) and the root command.
package registry

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
	"github.com/Jamf-Concepts/jamfschool-go-sdk/jamfschool"
)

// HTTPClient interface for making API requests.
type HTTPClient interface {
	Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error)
}

// acceptKey is the unexported context key for the Accept header override.
type acceptKey struct{}

// WithAccept returns a new context that overrides the HTTP Accept header for the request.
// Use this for binary/non-JSON endpoints where the default "application/json" causes a 406.
func WithAccept(ctx context.Context, accept string) context.Context {
	return context.WithValue(ctx, acceptKey{}, accept)
}

// AcceptFromContext returns the Accept header override from the context, or "" if not set.
func AcceptFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(acceptKey{}).(string); ok {
		return v
	}
	return ""
}

// contentTypeKey is the unexported context key for the Content-Type header override.
type contentTypeKey struct{}

// WithContentType returns a new context that overrides the Content-Type header for the request.
// Use this for PATCH endpoints that require application/merge-patch+json.
func WithContentType(ctx context.Context, ct string) context.Context {
	return context.WithValue(ctx, contentTypeKey{}, ct)
}

// ContentTypeFromContext returns the Content-Type override from the context, or "" if not set.
func ContentTypeFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(contentTypeKey{}).(string); ok {
		return v
	}
	return ""
}

// FileUploader interface for streaming file uploads with custom Content-Type.
type FileUploader interface {
	Upload(ctx context.Context, path string, body io.Reader, contentType string, contentLength int64) (*http.Response, error)
}

// OutputFormatter interface for formatting output.
type OutputFormatter interface {
	PrintResponse(resp *http.Response) error
	PrintRaw(data []byte) error
	// PrintBytes writes raw bytes without any conversion or formatting.
	// Used by Classic API commands to emit raw XML.
	PrintBytes(data []byte) error
	// Format returns the current output format string (e.g. "json", "xml", "table").
	Format() string
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
	DeleteComputer(ctx context.Context, uuid string) error
	SetComputerPlan(ctx context.Context, uuid string, planID string) (*jamfprotect.Computer, error)
	UpdateComputer(ctx context.Context, uuid string, input jamfprotect.ComputerUpdateInput) (*jamfprotect.Computer, error)

	// Alerts
	ListAlerts(ctx context.Context) ([]jamfprotect.Alert, error)
	GetAlert(ctx context.Context, uuid string) (*jamfprotect.Alert, error)
	GetAlertStatusCounts(ctx context.Context) (jamfprotect.AlertStatusCounts, error)
	UpdateAlerts(ctx context.Context, input jamfprotect.AlertUpdateInput) ([]jamfprotect.Alert, error)

	// Insights
	ListInsights(ctx context.Context) ([]jamfprotect.Insight, error)
	UpdateInsightStatus(ctx context.Context, uuid string, enabled bool) (jamfprotect.Insight, error)
	ListInsightComputers(ctx context.Context, uuid string) ([]jamfprotect.InsightComputer, error)
	GetFleetComplianceScore(ctx context.Context, date string) (jamfprotect.ComplianceBaselineScore, error)

	// Audit Logs
	ListAuditLogsByDate(ctx context.Context, dateRange *jamfprotect.AuditLogDateRange) ([]jamfprotect.AuditLog, error)

	// Dashboard
	GetCount(ctx context.Context) (jamfprotect.CountResponse, error)
	ListRiskiestComputers(ctx context.Context, limit int, createdInterval string) ([]jamfprotect.RiskyComputer, error)

	// RBAC
	GetCurrentPermissions(ctx context.Context) (jamfprotect.RolePermissions, error)

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

// SchoolClient defines the interface for Jamf School API operations.
// The SDK's *jamfschool.Client satisfies this interface directly.
type SchoolClient interface {
	// Devices
	GetDevice(ctx context.Context, udid string) (*jamfschool.Device, error)
	GetDevices(ctx context.Context) ([]jamfschool.Device, error)
	EraseDevice(ctx context.Context, udid string, clearActivationLock bool) error
	RestartDevice(ctx context.Context, udid string, clearPasscode bool) error
	RefreshDevice(ctx context.Context, udid string, clearErrors bool) error
	UnenrollDevice(ctx context.Context, udid string) error
	ClearDeviceActivationLock(ctx context.Context, udid string) error
	TrashDevice(ctx context.Context, udid string) error
	RestoreDevice(ctx context.Context, udid string) error
	UpdateDeviceESIM(ctx context.Context, udid string, serverURL string, requiresNetworkTether bool) error

	// Users
	GetUser(ctx context.Context, id int64) (*jamfschool.User, error)
	GetUsers(ctx context.Context) ([]jamfschool.User, error)
	CreateUser(ctx context.Context, input jamfschool.UserCreateInput) (int64, error)
	UpdateUser(ctx context.Context, id int64, input jamfschool.UserUpdateInput) error
	MigrateUser(ctx context.Context, id, locationID int64, onlyUser bool) error
	DeleteUser(ctx context.Context, id int64) error

	// Profiles
	GetProfile(ctx context.Context, id int64) (*jamfschool.Profile, error)
	GetProfiles(ctx context.Context) ([]jamfschool.Profile, error)

	// Apps
	GetApp(ctx context.Context, id int64) (*jamfschool.App, error)
	GetApps(ctx context.Context) ([]jamfschool.App, error)
	CreateApp(ctx context.Context, input jamfschool.AppCreateInput) (int64, error)
	TrashApp(ctx context.Context, id int64) error

	// Classes
	GetClass(ctx context.Context, uuid string) (*jamfschool.Class, error)
	GetClasses(ctx context.Context) ([]jamfschool.Class, error)
	CreateClass(ctx context.Context, input jamfschool.ClassCreateInput) (string, error)
	UpdateClass(ctx context.Context, uuid string, input jamfschool.ClassUpdateInput) error
	DeleteClass(ctx context.Context, uuid string) error
	AssignClassUsers(ctx context.Context, uuid string, studentIDs, teacherIDs []int64) error
	GetClassDevices(ctx context.Context, uuid string) ([]jamfschool.ClassDevice, error)

	// User Groups
	GetGroup(ctx context.Context, id int64) (*jamfschool.Group, error)
	GetGroups(ctx context.Context) ([]jamfschool.Group, error)
	CreateGroup(ctx context.Context, input jamfschool.GroupCreateInput) (int64, error)
	UpdateGroup(ctx context.Context, id int64, input jamfschool.GroupUpdateInput) error
	DeleteGroup(ctx context.Context, id int64) error

	// Device Groups
	GetDeviceGroup(ctx context.Context, id int64) (*jamfschool.DeviceGroup, error)
	GetDeviceGroups(ctx context.Context) ([]jamfschool.DeviceGroup, error)
	CreateDeviceGroup(ctx context.Context, input jamfschool.DeviceGroupCreateInput) (int64, error)
	UpdateDeviceGroup(ctx context.Context, id int64, input jamfschool.DeviceGroupUpdateInput) error
	DeleteDeviceGroup(ctx context.Context, id int64) error
	AddDevicesToGroup(ctx context.Context, groupID int64, udids []string) error
	RemoveDevicesFromGroup(ctx context.Context, groupID int64, udids []string) error
	GetDeviceGroupMembers(ctx context.Context, groupID int64) ([]string, error)

	// Locations
	GetLocation(ctx context.Context, id int64) (*jamfschool.Location, error)
	GetLocations(ctx context.Context) ([]jamfschool.Location, error)

	// iBeacons
	GetIBeacon(ctx context.Context, id int64) (*jamfschool.IBeacon, error)
	GetIBeacons(ctx context.Context) ([]jamfschool.IBeacon, error)
	CreateIBeacon(ctx context.Context, input jamfschool.IBeaconCreateInput) (*jamfschool.IBeacon, error)
	UpdateIBeacon(ctx context.Context, id int64, input jamfschool.IBeaconUpdateInput) (*jamfschool.IBeacon, error)
	DeleteIBeacon(ctx context.Context, id int64) error

	// DEP Devices
	GetDEPDevice(ctx context.Context, serial string) (*jamfschool.DEPDevice, error)
	GetDEPDevices(ctx context.Context) ([]jamfschool.DEPDevice, error)

	// Client metadata
	BaseURL() string
}

// CLIContext holds the shared client and output formatter for all commands.
// It is populated in PersistentPreRunE after token/URL resolution.
type CLIContext struct {
	Client        HTTPClient
	Output        OutputFormatter
	AuthProvider  auth.Provider // resolved Pro auth provider (nil for Protect commands)
	ProtectClient ProtectClient
	// PlatformSDKClient is the raw *jamfplatform.Client used by all platform
	// command code (hand-written and spec-generated). Hand-written commands
	// construct subpackage clients per call (cheap — they share the
	// transport); generated commands call .Transport().Do() directly.
	PlatformSDKClient   *jamfplatform.Client
	SchoolClient        SchoolClient
	Uploader            FileUploader   // non-nil for Pro commands; supports streaming uploads
	ProfileName         string         // resolved profile name; empty when using env-var auth
	DestructiveCooldown *time.Duration // nil = use default (10s); 0 = disabled
	// ClearProtectToken, when non-nil, clears the Jamf Protect disk token cache
	// so the next AccessToken call exchanges fresh credentials. Used by
	// "protect auth token --refresh". Nil when no disk cache is configured.
	ClearProtectToken func()
}
