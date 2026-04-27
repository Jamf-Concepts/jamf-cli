// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"gopkg.in/yaml.v3"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/blueprints"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/compliancebenchmarks"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/ddmreport"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/deviceactions"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/devicegroups"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/devices"
)

// requirePlatformClient returns an error if the Platform SDK client is not
// available. Platform commands call this at the top of RunE so users get a
// clear message instead of a nil-pointer panic.
func requirePlatformClient(cliCtx *registry.CLIContext) error {
	if cliCtx.PlatformClient == nil {
		return fmt.Errorf("this command requires platform gateway auth\n\n" +
			"Set up a platform profile:\n" +
			"  jamf-cli config add-profile <name> --auth-method platform --url <gateway-url> --tenant-id <id>\n\n" +
			"Or use environment variables:\n" +
			"  JAMF_URL, JAMF_CLIENT_ID, JAMF_CLIENT_SECRET, JAMF_TENANT_ID")
	}
	return nil
}

// platformClientWrapper composes subpackage clients from the v0.7+ SDK into the
// single registry.PlatformClient interface expected by all command code.
type platformClientWrapper struct {
	base *jamfplatform.Client
	bp   *blueprints.Client
	cb   *compliancebenchmarks.Client
	ddm  *ddmreport.Client
	da   *deviceactions.Client
	dg   *devicegroups.Client
	dev  *devices.Client
}

func newPlatformWrapper(base *jamfplatform.Client) *platformClientWrapper {
	return &platformClientWrapper{
		base: base,
		bp:   blueprints.New(base),
		cb:   compliancebenchmarks.New(base),
		ddm:  ddmreport.New(base),
		da:   deviceactions.New(base),
		dg:   devicegroups.New(base),
		dev:  devices.New(base),
	}
}

func (w *platformClientWrapper) BaseURL() string { return w.base.BaseURL() }
func (w *platformClientWrapper) ValidateCredentials(ctx context.Context) error {
	return w.base.ValidateCredentials(ctx)
}

// Blueprints
func (w *platformClientWrapper) ListBlueprints(ctx context.Context, sort []string, search string) ([]blueprints.BlueprintOverview, error) {
	return w.bp.ListBlueprints(ctx, sort, search)
}

func (w *platformClientWrapper) GetBlueprint(ctx context.Context, id string) (*blueprints.BlueprintDetail, error) {
	return w.bp.GetBlueprint(ctx, id)
}

func (w *platformClientWrapper) CreateBlueprint(ctx context.Context, req *blueprints.CreateBlueprintRequest) (*blueprints.CreateResponse, error) {
	return w.bp.CreateBlueprint(ctx, req)
}

func (w *platformClientWrapper) UpdateBlueprint(ctx context.Context, id string, req *blueprints.UpdateBlueprintRequest) error {
	return w.bp.UpdateBlueprint(ctx, id, req)
}

func (w *platformClientWrapper) DeleteBlueprint(ctx context.Context, id string) error {
	return w.bp.DeleteBlueprint(ctx, id)
}

func (w *platformClientWrapper) DeployBlueprint(ctx context.Context, id string) error {
	return w.bp.DeployBlueprint(ctx, id)
}

func (w *platformClientWrapper) UndeployBlueprint(ctx context.Context, id string) error {
	return w.bp.UndeployBlueprint(ctx, id)
}

func (w *platformClientWrapper) GetBlueprintReport(ctx context.Context, id string) (*blueprints.BlueprintStatusDetail, error) {
	return w.bp.GetBlueprintReport(ctx, id)
}

func (w *platformClientWrapper) ListBlueprintComponents(ctx context.Context) ([]blueprints.ComponentDescription, error) {
	return w.bp.ListBlueprintComponents(ctx)
}

func (w *platformClientWrapper) GetBlueprintComponent(ctx context.Context, id string) (*blueprints.ComponentDescription, error) {
	return w.bp.GetBlueprintComponent(ctx, id)
}

// Compliance Benchmarks
func (w *platformClientWrapper) ListBaselines(ctx context.Context) (*compliancebenchmarks.BaselinesResponse, error) {
	return w.cb.ListBaselines(ctx)
}

func (w *platformClientWrapper) ListBenchmarks(ctx context.Context) (*compliancebenchmarks.BenchmarksResponseV2, error) {
	return w.cb.ListBenchmarks(ctx)
}

func (w *platformClientWrapper) GetBenchmark(ctx context.Context, id string) (*compliancebenchmarks.BenchmarkResponseV2, error) {
	return w.cb.GetBenchmark(ctx, id)
}

func (w *platformClientWrapper) CreateBenchmark(ctx context.Context, req *compliancebenchmarks.BenchmarkRequestV2) (*compliancebenchmarks.BenchmarkResponseV2, error) {
	return w.cb.CreateBenchmark(ctx, req)
}

func (w *platformClientWrapper) DeleteBenchmark(ctx context.Context, id string) error {
	return w.cb.DeleteBenchmark(ctx, id)
}

func (w *platformClientWrapper) GetBaselineRules(ctx context.Context, baselineID string) (*compliancebenchmarks.SourcedRules, error) {
	return w.cb.GetBaselineRules(ctx, baselineID)
}

func (w *platformClientWrapper) ListBenchmarkRulesStats(ctx context.Context, benchmarkID string, sort string, ruleSearch string) ([]compliancebenchmarks.RuleResult, error) {
	return w.cb.ListBenchmarkRulesStats(ctx, benchmarkID, sort, ruleSearch)
}

func (w *platformClientWrapper) ListBenchmarkRuleDevices(ctx context.Context, benchmarkID string, ruleID string, sort string, deviceSearch string, ruleResult string) ([]compliancebenchmarks.DeviceRuleResult, error) {
	return w.cb.ListBenchmarkRuleDevices(ctx, benchmarkID, ruleID, sort, deviceSearch, ruleResult)
}

func (w *platformClientWrapper) GetBenchmarkCompliancePercentage(ctx context.Context, benchmarkID string) (*compliancebenchmarks.CompliancePercentage, error) {
	return w.cb.GetBenchmarkCompliancePercentage(ctx, benchmarkID)
}

// Devices
func (w *platformClientWrapper) ListDevices(ctx context.Context, sort []string, filter string) ([]devices.DeviceListReadRepresentationV1, error) {
	return w.dev.ListDevices(ctx, sort, filter)
}

func (w *platformClientWrapper) GetDevice(ctx context.Context, id string) (*devices.DeviceReadRepresentationV1, error) {
	return w.dev.GetDevice(ctx, id)
}

func (w *platformClientWrapper) UpdateDevice(ctx context.Context, id string, payload *devices.DeviceUpdateRepresentationV1) error {
	return w.dev.UpdateDevice(ctx, id, payload)
}

func (w *platformClientWrapper) DeleteDevice(ctx context.Context, id string) error {
	return w.dev.DeleteDevice(ctx, id)
}

func (w *platformClientWrapper) ListDeviceApplications(ctx context.Context, deviceID string, sort []string, filter string) ([]devices.DeviceInstalledApplicationReadRepresentationV1, error) {
	return w.dev.ListDeviceApplications(ctx, deviceID, sort, filter)
}

func (w *platformClientWrapper) ListDevicesForUser(ctx context.Context, userID string, sort []string, filter string) ([]devices.DeviceListReadRepresentationV1, error) {
	return w.dev.ListDevicesForUser(ctx, userID, sort, filter)
}

// Device Groups
func (w *platformClientWrapper) ListDeviceGroups(ctx context.Context, sort []string, filter string) ([]devicegroups.DeviceGroupListReadRepresentationV1, error) {
	return w.dg.ListDeviceGroups(ctx, sort, filter)
}

func (w *platformClientWrapper) GetDeviceGroup(ctx context.Context, id string) (*devicegroups.DeviceGroupReadRepresentationV1, error) {
	return w.dg.GetDeviceGroup(ctx, id)
}

func (w *platformClientWrapper) CreateDeviceGroup(ctx context.Context, req *devicegroups.DeviceGroupCreateRepresentationV1) (*devicegroups.HrefRepresentation, error) {
	return w.dg.CreateDeviceGroup(ctx, req)
}

func (w *platformClientWrapper) UpdateDeviceGroup(ctx context.Context, id string, req *devicegroups.DeviceGroupUpdateRepresentationV1) error {
	return w.dg.UpdateDeviceGroup(ctx, id, req)
}

func (w *platformClientWrapper) DeleteDeviceGroup(ctx context.Context, id string) error {
	return w.dg.DeleteDeviceGroup(ctx, id)
}

func (w *platformClientWrapper) ListDeviceGroupMembers(ctx context.Context, id string) ([]string, error) {
	return w.dg.ListDeviceGroupMembers(ctx, id)
}

func (w *platformClientWrapper) UpdateDeviceGroupMembers(ctx context.Context, id string, patch *devicegroups.DeviceGroupMemberPatchRepresentationV1) error {
	return w.dg.UpdateDeviceGroupMembers(ctx, id, patch)
}

func (w *platformClientWrapper) ListDeviceGroupsForDevice(ctx context.Context, deviceID string) ([]devicegroups.DeviceGroupMemberOfRepresentationV1, error) {
	return w.dg.ListDeviceGroupsForDevice(ctx, deviceID)
}

// Device Actions
func (w *platformClientWrapper) CheckInDevice(ctx context.Context, id string) error {
	return w.da.CheckInDevice(ctx, id)
}

func (w *platformClientWrapper) EraseDevice(ctx context.Context, id string, req *deviceactions.EraseDeviceRequest) ([]deviceactions.DeviceCommandResponse, error) {
	return w.da.EraseDevice(ctx, id, req)
}

func (w *platformClientWrapper) RestartDevice(ctx context.Context, id string) ([]deviceactions.DeviceCommandResponse, error) {
	return w.da.RestartDevice(ctx, id)
}

func (w *platformClientWrapper) ShutdownDevice(ctx context.Context, id string) ([]deviceactions.DeviceCommandResponse, error) {
	return w.da.ShutdownDevice(ctx, id)
}

func (w *platformClientWrapper) UnmanageDevice(ctx context.Context, id string) ([]deviceactions.DeviceCommandResponse, error) {
	return w.da.UnmanageDevice(ctx, id)
}

// DDM Declaration Reports
func (w *platformClientWrapper) GetDeviceDeclarationReport(ctx context.Context, deviceID string) (*ddmreport.DeviceReportDto, error) {
	return w.ddm.GetDeviceDeclarationReport(ctx, deviceID)
}

func (w *platformClientWrapper) ListDeclarationReportClients(ctx context.Context, declarationIdentifier string, sort []string) ([]ddmreport.DeclarationReportClientDto, error) {
	return w.ddm.ListDeclarationReportClients(ctx, declarationIdentifier, sort)
}

// newPlatformSDKClient constructs a registry.PlatformClient with retry, spinner,
// and token cache — the same configuration used by the main PersistentPreRunE
// in root.go. This avoids bare SDK clients that lack retry and progress indication.
func newPlatformSDKClient(url, clientID, clientSecret, tenantID string, showSpinner bool) registry.PlatformClient {
	opts := []jamfplatform.Option{
		jamfplatform.WithTenantID(tenantID),
		jamfplatform.WithUserAgent("jamf-cli/" + cliVersion),
	}

	if cacheDir, _ := os.UserCacheDir(); cacheDir != "" {
		opts = append(opts, jamfplatform.WithFileTokenCache(filepath.Join(cacheDir, "jamf-cli")))
	}

	jar, _ := cookiejar.New(nil)
	rc := retryablehttp.NewClient()
	rc.RetryMax = 3
	rc.RetryWaitMin = 1 * time.Second
	rc.RetryWaitMax = 30 * time.Second
	rc.Logger = nil
	rc.CheckRetry = retryablehttp.ErrorPropagatedRetryPolicy
	rc.HTTPClient.Timeout = 60 * time.Second
	rc.HTTPClient.Jar = jar
	stdClient := rc.StandardClient()
	if showSpinner {
		stdClient.Transport = &spinnerTransport{inner: stdClient.Transport}
	}
	opts = append(opts, jamfplatform.WithHTTPClient(stdClient))

	return newPlatformWrapper(jamfplatform.NewClient(url, clientID, clientSecret, opts...))
}

// printScaffold marshals the given value to stdout, respecting the -o flag.
// Used by apply commands with --scaffold to show the expected input structure.
func printScaffold(v any) error {
	switch outputFmt {
	case "yaml":
		enc := yaml.NewEncoder(os.Stdout)
		enc.SetIndent(2)
		if err := enc.Encode(v); err != nil {
			return err
		}
		return enc.Close()
	default:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(v)
	}
}
