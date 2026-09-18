# Dashboard Command Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `jamf-cli dashboard` — a top-level command that authenticates against multiple config profiles, collects fleet data in parallel, and renders a self-contained HTML report with Chart.js visualizations and interactive elements.

**Architecture:** The command handles its own multi-profile auth (like `diff`), builds product-specific clients per profile, collects data in parallel within each profile, and feeds a `DashboardData` struct into a Go `html/template` that produces a single self-contained HTML file. Each product's data collection is isolated in its own file.

**Tech Stack:** Go, `html/template`, Chart.js (embedded), vanilla JS, `spf13/cobra`, existing `registry.HTTPClient` / `ProtectClient` / `PlatformClient` interfaces.

---

## File Map

| File | Responsibility |
|------|---------------|
| `internal/commands/dashboard.go` | Command definition, `--profile` flag, auth orchestration loop, data collection coordinator |
| `internal/commands/dashboard_data.go` | All data types (`DashboardData`, `FleetSummary`, `SecurityPosture`, etc.) |
| `internal/commands/dashboard_pro.go` | Pro data collection: fleet counts, security posture, audit, patch, device compliance, OS distribution |
| `internal/commands/dashboard_protect.go` | Protect data collection: plans, analytics, endpoints, configuration sets |
| `internal/commands/dashboard_platform.go` | Platform data collection: blueprints, compliance benchmarks, DDM status |
| `internal/commands/dashboard_html.go` | HTML template constant, `renderDashboard` function, Chart.js embed |
| `internal/commands/dashboard_test.go` | Tests for data collection functions and template rendering |
| `internal/commands/root.go` | Wire `newDashboardCmd()`, add `"dashboard"` to `chainSkip` |
| `internal/commands/groups.go` | Add `"dashboard"` to `rootGroupMap` |
| `internal/commands/aliases.go` | Add `"dashboard": {"db"}` to `rootAliases` |

---

### Task 1: Create feature branch

**Files:** None (git only)

- [ ] **Step 1: Create and switch to feature branch**

```bash
git checkout -b feat/dashboard main
```

- [ ] **Step 2: Verify branch**

```bash
git branch --show-current
```

Expected: `feat/dashboard`

---

### Task 2: Data types

**Files:**
- Create: `internal/commands/dashboard_data.go`

- [ ] **Step 1: Create the data types file**

```go
// Copyright 2026, Jamf Software LLC

package commands

import "time"

// DashboardData holds all collected data for the HTML report template.
type DashboardData struct {
	Title       string
	GeneratedAt time.Time
	CLIVersion  string
	Profiles    []dashboardProfile

	// Conditional sections — nil means "don't render this section"
	Fleet    *fleetSummary
	Security *securityPosture
	Audit    *auditSummary
	Patch    *patchCompliance
	Devices  *deviceCompliance
	OSDist   *osDistribution
	Protect  *protectCoverage
	Platform *platformStatus
}

// dashboardProfile records which profile contributed data and what product it serves.
type dashboardProfile struct {
	Name    string
	Product string // "pro", "protect", or "platform"
	URL     string
}

// fleetSummary holds device and user counts from Jamf Pro.
type fleetSummary struct {
	ManagedComputers   int
	UnmanagedComputers int
	ManagedMobile      int
	UnmanagedMobile    int
	Users              int
}

// securityPosture holds security compliance percentages.
type securityPosture struct {
	Total             int
	FileVaultEnabled  int
	FirewallEnabled   int
	GatekeeperEnabled int
	SIPEnabled        int
}

// Pct returns a 0-100 float for the given count against the total.
func (s *securityPosture) Pct(count int) float64 {
	if s.Total == 0 {
		return 0
	}
	return float64(count) / float64(s.Total) * 100
}

// auditSummary holds categorized audit findings.
type auditSummary struct {
	Results []auditResult
}

// CriticalCount returns the number of CRITICAL findings.
func (a *auditSummary) CriticalCount() int {
	n := 0
	for _, r := range a.Results {
		if r.Severity == severityCritical {
			n++
		}
	}
	return n
}

// WarningCount returns the number of WARNING findings.
func (a *auditSummary) WarningCount() int {
	n := 0
	for _, r := range a.Results {
		if r.Severity == severityWarning {
			n++
		}
	}
	return n
}

// InfoCount returns the number of INFO findings.
func (a *auditSummary) InfoCount() int {
	n := 0
	for _, r := range a.Results {
		if r.Severity == severityInfo {
			n++
		}
	}
	return n
}

// patchCompliance holds per-title patch compliance data.
type patchCompliance struct {
	Titles []patchTitle
}

// patchTitle represents one software title's patch status.
type patchTitle struct {
	Name          string
	LatestVersion string
	UpToDate      int
	OutOfDate     int
	Total         int
	CompliancePct float64
}

// deviceCompliance holds stale/failing device counts.
type deviceCompliance struct {
	StaleDevices      int
	FailedMDMCommands int
	StaleThresholdDays int
}

// osDistribution holds OS version counts for charting.
type osDistribution struct {
	Versions []osVersionCount
}

// osVersionCount pairs an OS version string with its device count.
type osVersionCount struct {
	Version string
	Count   int
}

// protectCoverage holds Jamf Protect overview data.
type protectCoverage struct {
	Plans           int
	AnalyticsTotal  int
	AnalyticsActive int
	Endpoints       int
	AnalyticSets    int
	ExceptionSets   int
}

// platformStatus holds Jamf Platform data (blueprints + compliance).
type platformStatus struct {
	Blueprints []blueprintEntry
	Benchmarks []benchmarkEntry
}

// blueprintEntry represents one blueprint's deployment status.
type blueprintEntry struct {
	Name            string
	DeploymentState string
}

// benchmarkEntry represents one compliance benchmark.
type benchmarkEntry struct {
	Title         string
	CompliancePct float64
	FailingRules  int
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/keaton.svoma/Projects/jamf-cli && go build ./internal/commands/...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/commands/dashboard_data.go
git commit -m "feat(dashboard): add data types for cross-product HTML dashboard"
```

---

### Task 3: Command skeleton and wiring

**Files:**
- Create: `internal/commands/dashboard.go`
- Modify: `internal/commands/root.go`
- Modify: `internal/commands/groups.go`
- Modify: `internal/commands/aliases.go`

- [ ] **Step 1: Create the command file with auth orchestration**

```go
// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/client"
	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

func newDashboardCmd() *cobra.Command {
	var (
		profiles []string
		title    string
		outFile  string
	)

	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Generate a cross-product HTML fleet dashboard",
		Long: `Generate a self-contained HTML report aggregating fleet health, security
posture, audit findings, patch compliance, and more across Jamf Pro,
Protect, and Platform products.

Each --profile flag specifies a config profile to pull data from. The
profile's product type (pro, protect, or platform) determines which
sections are populated. All profiles are authenticated before any data
collection begins.

Examples:
  jamf-cli dashboard --profile prod-pro --out-file report.html
  jamf-cli dashboard --profile prod-pro --profile prod-protect > report.html
  jamf-cli dashboard --profile my-platform --profile my-pro --title "Q2 Fleet Report"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(profiles) == 0 {
				cfg, _ := config.Load()
				var available []string
				if cfg != nil {
					for name := range cfg.Profiles {
						available = append(available, name)
					}
				}
				if len(available) > 0 {
					return fmt.Errorf("at least one --profile is required\n\nAvailable profiles: %v\nList all: jamf-cli config list", available)
				}
				return fmt.Errorf("at least one --profile is required\n\nConfigure one first: jamf-cli config add-profile")
			}
			return runDashboard(cmd.Context(), dashboardOptions{
				Profiles: profiles,
				Title:    title,
				OutFile:  outFile,
			})
		},
	}

	cmd.Flags().StringArrayVar(&profiles, "profile", nil, "config profile(s) to include (repeatable, required)")
	cmd.Flags().StringVar(&title, "title", "Jamf Fleet Dashboard", "report title")
	cmd.Flags().StringVar(&outFile, "out-file", "", "write HTML to file instead of stdout")

	return cmd
}

type dashboardOptions struct {
	Profiles []string
	Title    string
	OutFile  string
}

// resolvedClients holds the authenticated clients for a single profile.
type resolvedClients struct {
	profile  dashboardProfile
	pro      registry.HTTPClient   // non-nil for pro/platform profiles
	protect  registry.ProtectClient // non-nil for protect profiles
	platform registry.PlatformClient // non-nil for platform profiles
}

func runDashboard(ctx context.Context, opts dashboardOptions) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Phase 1: Authenticate all profiles (fail fast)
	var clients []resolvedClients
	for _, profileName := range opts.Profiles {
		rc, err := resolveDashboardProfile(cfg, profileName)
		if err != nil {
			return fmt.Errorf("profile %q: %w", profileName, err)
		}
		clients = append(clients, rc)
	}

	// Phase 2: Collect data
	data := &DashboardData{
		Title:       opts.Title,
		GeneratedAt: time.Now(),
		CLIVersion:  cliVersion,
	}

	for _, rc := range clients {
		data.Profiles = append(data.Profiles, rc.profile)

		switch rc.profile.Product {
		case "pro":
			collectProData(ctx, rc.pro, data)
		case "protect":
			collectProtectData(ctx, rc.protect, data)
		case "platform":
			collectProData(ctx, rc.pro, data)
			collectPlatformData(ctx, rc.platform, data)
		}
	}

	// Phase 3: Render HTML
	var w *os.File
	if opts.OutFile != "" {
		w, err = os.Create(opts.OutFile)
		if err != nil {
			return fmt.Errorf("creating output file: %w", err)
		}
		defer w.Close()
	} else {
		w = os.Stdout
	}

	return renderDashboard(w, data)
}

// resolveDashboardProfile authenticates a single config profile and returns
// the appropriate client(s) for its product type.
func resolveDashboardProfile(cfg *config.Config, profileName string) (resolvedClients, error) {
	p, _, err := config.GetProfile(cfg, profileName)
	if err != nil {
		return resolvedClients{}, fmt.Errorf("unknown profile — run 'jamf-cli config list' to see available profiles")
	}

	product := p.Product
	if product == "" {
		product = "pro"
	}
	if p.AuthMethod == "platform" {
		product = "platform"
	}

	rc := resolvedClients{
		profile: dashboardProfile{
			Name:    profileName,
			Product: product,
			URL:     p.URL,
		},
	}

	switch product {
	case "protect":
		protectClient, err := buildProtectClient(cfg, profileName)
		if err != nil {
			return resolvedClients{}, err
		}
		rc.protect = protectClient

	case "platform":
		resolvedURL, authProvider, err := ResolveAuthForProfile(cfg, AuthParams{Profile: profileName})
		if err != nil {
			return resolvedClients{}, err
		}
		pp, ok := authProvider.(*auth.PlatformOAuth2Provider)
		if !ok {
			return resolvedClients{}, fmt.Errorf("profile %q has platform product but non-platform auth", profileName)
		}
		rc.pro = &cliClient{client.New(resolvedURL, authProvider, client.WithTenantID(pp.TenantID()))}
		rc.platform = newPlatformSDKClient(resolvedURL, pp.ClientID(), pp.ClientSecret(), pp.TenantID(), false)
		rc.profile.URL = resolvedURL

	default: // "pro"
		resolvedURL, authProvider, err := ResolveAuthForProfile(cfg, AuthParams{Profile: profileName})
		if err != nil {
			return resolvedClients{}, err
		}
		clientOpts := []client.Option{client.WithVerbose(false)}
		if pp, ok := authProvider.(*auth.PlatformOAuth2Provider); ok {
			clientOpts = append(clientOpts, client.WithTenantID(pp.TenantID()))
		}
		type jarProvider interface {
			Jar() http.CookieJar
		}
		if jp, ok := authProvider.(jarProvider); ok {
			clientOpts = append(clientOpts, client.WithCookieJar(jp.Jar()))
		}
		rc.pro = &cliClient{client.New(resolvedURL, authProvider, clientOpts...)}
		rc.profile.URL = resolvedURL
	}

	return rc, nil
}

// buildProtectClient constructs a Jamf Protect SDK client from a config profile.
func buildProtectClient(cfg *config.Config, profileName string) (registry.ProtectClient, error) {
	p, _, err := config.GetProfile(cfg, profileName)
	if err != nil {
		return nil, err
	}

	url := p.URL
	cid := ""
	csecret := ""

	if p.ClientID != "" {
		cid, err = config.ResolveSecret(p.ClientID)
		if err != nil {
			return nil, fmt.Errorf("resolving client-id: %w", err)
		}
	}
	if p.ClientSecret != "" {
		csecret, err = config.ResolveSecret(p.ClientSecret)
		if err != nil {
			return nil, fmt.Errorf("resolving client-secret: %w", err)
		}
	}

	if url == "" {
		return nil, fmt.Errorf("URL is required for protect profile")
	}
	if cid == "" || csecret == "" {
		return nil, fmt.Errorf("client-id and client-secret are required for protect profile")
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

	protectOpts := []jamfprotect.Option{
		jamfprotect.WithUserAgent("jamf-cli/" + cliVersion),
		jamfprotect.WithHTTPClient(rc.StandardClient()),
	}
	if cacheDir, err := os.UserCacheDir(); err == nil {
		protectOpts = append(protectOpts, jamfprotect.WithFileTokenCache(cacheDir+"/jamf-cli"))
	}

	return jamfprotect.NewClient(url, cid, csecret, protectOpts...), nil
}
```

- [ ] **Step 2: Wire the command into root.go**

In `root.go`, add `"dashboard"` to the `chainSkip` map (around line 412):

```go
"dashboard": true,
```

And add the command registration (around line 567, near the other `AddCommand` calls):

```go
// Cross-product dashboard
cmd.AddCommand(newDashboardCmd())
```

- [ ] **Step 3: Add to groups.go**

Add `"dashboard"` to `rootGroupMap`:

```go
"dashboard": "core",
```

- [ ] **Step 4: Add alias in aliases.go**

Add to `rootAliases`:

```go
"dashboard": {"db"},
```

- [ ] **Step 5: Verify it compiles**

Run: `cd /Users/keaton.svoma/Projects/jamf-cli && go build ./internal/commands/...`

This will fail because `collectProData`, `collectProtectData`, `collectPlatformData`, and `renderDashboard` don't exist yet. Create stub files:

Create `internal/commands/dashboard_pro.go`:
```go
// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func collectProData(ctx context.Context, client registry.HTTPClient, data *DashboardData) {
	// Implemented in Task 4
}
```

Create `internal/commands/dashboard_protect.go`:
```go
// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func collectProtectData(ctx context.Context, client registry.ProtectClient, data *DashboardData) {
	// Implemented in Task 5
}
```

Create `internal/commands/dashboard_platform.go`:
```go
// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func collectPlatformData(ctx context.Context, client registry.PlatformClient, data *DashboardData) {
	// Implemented in Task 6
}
```

Create `internal/commands/dashboard_html.go`:
```go
// Copyright 2026, Jamf Software LLC

package commands

import (
	"io"
)

func renderDashboard(w io.Writer, data *DashboardData) error {
	// Implemented in Task 7
	_, err := w.Write([]byte("<html><body>placeholder</body></html>"))
	return err
}
```

- [ ] **Step 6: Verify it compiles**

Run: `cd /Users/keaton.svoma/Projects/jamf-cli && go build ./internal/commands/...`
Expected: no errors

- [ ] **Step 7: Verify the command appears in help**

Run: `cd /Users/keaton.svoma/Projects/jamf-cli && go run ./cmd/jamf-cli dashboard --help`
Expected: shows usage with `--profile`, `--title`, `--out-file` flags

- [ ] **Step 8: Commit**

```bash
git add internal/commands/dashboard.go internal/commands/dashboard_pro.go \
       internal/commands/dashboard_protect.go internal/commands/dashboard_platform.go \
       internal/commands/dashboard_html.go internal/commands/root.go \
       internal/commands/groups.go internal/commands/aliases.go
git commit -m "feat(dashboard): add command skeleton with multi-profile auth orchestration"
```

---

### Task 4: Pro data collection

**Files:**
- Modify: `internal/commands/dashboard_pro.go`

This task implements the Jamf Pro data collection. It reuses the existing `fetchJSON`, `fetchPaginatedCount`, `FetchAllPaginated`, and `allAuditChecks()` functions that are already package-level in the `commands` package.

- [ ] **Step 1: Implement collectProData**

Replace the contents of `dashboard_pro.go` with:

```go
// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// collectProData fetches Jamf Pro data in parallel and populates the
// corresponding sections of DashboardData. Individual fetch failures
// are logged to stderr but do not stop other fetches.
func collectProData(ctx context.Context, client registry.HTTPClient, data *DashboardData) {
	var wg sync.WaitGroup
	var mu sync.Mutex

	fleet := &fleetSummary{}
	security := &securityPosture{}
	audit := &auditSummary{}
	patch := &patchCompliance{}
	devices := &deviceCompliance{StaleThresholdDays: 14}
	osDist := &osDistribution{}

	// Fleet counts
	wg.Add(1)
	go func() {
		defer wg.Done()
		collectFleetCounts(ctx, client, fleet, &mu)
	}()

	// Security posture
	wg.Add(1)
	go func() {
		defer wg.Done()
		collectSecurityPosture(ctx, client, security, &mu)
	}()

	// Audit findings (sequential internally, as each check is lightweight)
	wg.Add(1)
	go func() {
		defer wg.Done()
		collectAuditFindings(ctx, client, audit, &mu)
	}()

	// Patch compliance
	wg.Add(1)
	go func() {
		defer wg.Done()
		collectPatchCompliance(ctx, client, patch, &mu)
	}()

	// Device compliance (stale check-ins, failed MDM)
	wg.Add(1)
	go func() {
		defer wg.Done()
		collectDeviceCompliance(ctx, client, devices, &mu)
	}()

	// OS distribution
	wg.Add(1)
	go func() {
		defer wg.Done()
		collectOSDistribution(ctx, client, osDist, &mu)
	}()

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	data.Fleet = fleet
	data.Security = security
	data.Audit = audit
	data.Patch = patch
	data.Devices = devices
	data.OSDist = osDist
}

func collectFleetCounts(ctx context.Context, client registry.HTTPClient, fleet *fleetSummary, mu *sync.Mutex) {
	info, err := fetchJSON(ctx, client, "/v1/inventory-information")
	if err != nil {
		fmt.Fprintf(os.Stderr, "dashboard: fleet counts: %v\n", err)
		return
	}

	mu.Lock()
	defer mu.Unlock()
	if v, ok := info["managedComputers"].(float64); ok {
		fleet.ManagedComputers = int(v)
	}
	if v, ok := info["unmanagedComputers"].(float64); ok {
		fleet.UnmanagedComputers = int(v)
	}
	if v, ok := info["managedDevices"].(float64); ok {
		fleet.ManagedMobile = int(v)
	}
	if v, ok := info["unmanagedDevices"].(float64); ok {
		fleet.UnmanagedMobile = int(v)
	}

	// User count from paginated endpoint
	userCount, err := fetchPaginatedCount(ctx, client, "/v1/users")
	if err == nil {
		// Parse the formatted count back; simpler to just re-fetch as int
		userData, err2 := fetchJSON(ctx, client, "/v1/users?page-size=1")
		if err2 == nil {
			if tc, ok := userData["totalCount"].(float64); ok {
				fleet.Users = int(tc)
			}
		}
	}
	_ = userCount // used the raw int path instead
}

func collectSecurityPosture(ctx context.Context, client registry.HTTPClient, security *securityPosture, mu *sync.Mutex) {
	computers, err := FetchAllPaginated(ctx, client,
		"/v3/computers-inventory?section=SECURITY&section=DISK_ENCRYPTION", 500)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dashboard: security posture: %v\n", err)
		return
	}

	total := len(computers)
	var fv, fw, gk, sip int

	for _, c := range computers {
		// FileVault
		if de, ok := c["diskEncryption"].(map[string]any); ok {
			if boot, ok := de["bootPartitionEncryptionDetails"].(map[string]any); ok {
				if state, ok := boot["partitionFileVault2State"].(string); ok && state == "ENCRYPTED" {
					fv++
				}
			}
		}

		// Firewall, Gatekeeper, SIP
		if sec, ok := c["security"].(map[string]any); ok {
			if enabled, ok := sec["firewallEnabled"].(bool); ok && enabled {
				fw++
			}
			if status, ok := sec["gatekeeperStatus"].(string); ok && status != "DISABLED" && status != "Disabled" {
				gk++
			}
			if status, ok := sec["sipStatus"].(string); ok && (status == "ENABLED" || status == "Enabled") {
				sip++
			}
		}
	}

	mu.Lock()
	defer mu.Unlock()
	security.Total = total
	security.FileVaultEnabled = fv
	security.FirewallEnabled = fw
	security.GatekeeperEnabled = gk
	security.SIPEnabled = sip
}

func collectAuditFindings(ctx context.Context, client registry.HTTPClient, audit *auditSummary, mu *sync.Mutex) {
	checks := allAuditChecks()
	var results []auditResult

	for _, check := range checks {
		result, err := check.Run(ctx, client, 14)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: audit check %q: %v\n", check.Name, err)
			continue
		}
		if result != nil {
			results = append(results, *result)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	audit.Results = results
}

func collectPatchCompliance(ctx context.Context, client registry.HTTPClient, patch *patchCompliance, mu *sync.Mutex) {
	titles, err := FetchAllPaginated(ctx, client, "/v2/patch-software-title-configurations", 200)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dashboard: patch titles: %v\n", err)
		return
	}

	type titleResult struct {
		name          string
		latestVersion string
		upToDate      int
		outOfDate     int
	}

	results, _ := BoundedParallelFetch(ctx, titles, 5,
		func(ctx context.Context, t map[string]any) (titleResult, error) {
			titleID := extractID(t)
			titleName, _ := t["displayName"].(string)
			if titleName == "" {
				titleName, _ = t["softwareTitleName"].(string)
			}

			path := fmt.Sprintf("/v2/patch-software-title-configurations/%s/patch-summary", titleID)
			summary, err := fetchJSON(ctx, client, path)
			if err != nil {
				return titleResult{name: titleName}, err
			}

			r := titleResult{name: titleName}
			if v, ok := summary["upToDate"].(float64); ok {
				r.upToDate = int(v)
			}
			if v, ok := summary["outOfDate"].(float64); ok {
				r.outOfDate = int(v)
			}
			if v, ok := summary["latestVersion"].(string); ok {
				r.latestVersion = v
			}
			return r, nil
		})

	mu.Lock()
	defer mu.Unlock()
	for _, r := range results {
		total := r.upToDate + r.outOfDate
		pct := 0.0
		if total > 0 {
			pct = float64(r.upToDate) / float64(total) * 100
		}
		patch.Titles = append(patch.Titles, patchTitle{
			Name:          r.name,
			LatestVersion: r.latestVersion,
			UpToDate:      r.upToDate,
			OutOfDate:     r.outOfDate,
			Total:         total,
			CompliancePct: pct,
		})
	}
}

func collectDeviceCompliance(ctx context.Context, client registry.HTTPClient, devices *deviceCompliance, mu *sync.Mutex) {
	// Stale check-ins
	cutoff := time.Now().AddDate(0, 0, -devices.StaleThresholdDays).UTC().Format("2006-01-02T15:04:05.000Z")
	stalePath := fmt.Sprintf("/v3/computers-inventory?section=GENERAL&page-size=1&filter=general.lastContactTime%%3C%%3D%s", cutoff)
	staleData, err := fetchJSON(ctx, client, stalePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dashboard: stale devices: %v\n", err)
	} else {
		mu.Lock()
		if tc, ok := staleData["totalCount"].(float64); ok {
			devices.StaleDevices = int(tc)
		}
		mu.Unlock()
	}

	// Failed MDM commands
	failedPath := "/v2/mdm/commands?filter=status%3D%3DError&page-size=1"
	failedData, err := fetchJSON(ctx, client, failedPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dashboard: failed MDM commands: %v\n", err)
	} else {
		mu.Lock()
		if tc, ok := failedData["totalCount"].(float64); ok {
			devices.FailedMDMCommands = int(tc)
		}
		mu.Unlock()
	}
}

func collectOSDistribution(ctx context.Context, client registry.HTTPClient, osDist *osDistribution, mu *sync.Mutex) {
	computers, err := FetchAllPaginated(ctx, client,
		"/v3/computers-inventory?section=OPERATING_SYSTEM", 500)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dashboard: OS distribution: %v\n", err)
		return
	}

	counts := make(map[string]int)
	for _, c := range computers {
		if osInfo, ok := c["operatingSystem"].(map[string]any); ok {
			if v, ok := osInfo["version"].(string); ok && v != "" {
				counts[v]++
			}
		}
	}

	mu.Lock()
	defer mu.Unlock()
	for version, count := range counts {
		osDist.Versions = append(osDist.Versions, osVersionCount{
			Version: version,
			Count:   count,
		})
	}
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/keaton.svoma/Projects/jamf-cli && go build ./internal/commands/...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/commands/dashboard_pro.go
git commit -m "feat(dashboard): implement Jamf Pro data collection (fleet, security, audit, patch, compliance, OS)"
```

---

### Task 5: Protect data collection

**Files:**
- Modify: `internal/commands/dashboard_protect.go`

- [ ] **Step 1: Implement collectProtectData**

Replace the contents of `dashboard_protect.go` with:

```go
// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// collectProtectData fetches Jamf Protect data in parallel and populates
// the Protect section of DashboardData.
func collectProtectData(ctx context.Context, client registry.ProtectClient, data *DashboardData) {
	var wg sync.WaitGroup
	var mu sync.Mutex

	protect := &protectCoverage{}

	// Plans
	wg.Add(1)
	go func() {
		defer wg.Done()
		plans, err := client.ListPlans(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: protect plans: %v\n", err)
			return
		}
		mu.Lock()
		protect.Plans = len(plans)
		mu.Unlock()
	}()

	// Analytics
	wg.Add(1)
	go func() {
		defer wg.Done()
		analytics, err := client.ListAnalytics(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: protect analytics: %v\n", err)
			return
		}
		total := len(analytics)
		active := 0
		for _, a := range analytics {
			if a.Enabled {
				active++
			}
		}
		mu.Lock()
		protect.AnalyticsTotal = total
		protect.AnalyticsActive = active
		mu.Unlock()
	}()

	// Endpoints
	wg.Add(1)
	go func() {
		defer wg.Done()
		computers, err := client.ListComputers(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: protect endpoints: %v\n", err)
			return
		}
		mu.Lock()
		protect.Endpoints = len(computers)
		mu.Unlock()
	}()

	// Analytic sets
	wg.Add(1)
	go func() {
		defer wg.Done()
		sets, err := client.ListAnalyticSets(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: protect analytic sets: %v\n", err)
			return
		}
		mu.Lock()
		protect.AnalyticSets = len(sets)
		mu.Unlock()
	}()

	// Exception sets
	wg.Add(1)
	go func() {
		defer wg.Done()
		sets, err := client.ListExceptionSets(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: protect exception sets: %v\n", err)
			return
		}
		mu.Lock()
		protect.ExceptionSets = len(sets)
		mu.Unlock()
	}()

	wg.Wait()

	data.Protect = protect
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/keaton.svoma/Projects/jamf-cli && go build ./internal/commands/...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/commands/dashboard_protect.go
git commit -m "feat(dashboard): implement Jamf Protect data collection"
```

---

### Task 6: Platform data collection

**Files:**
- Modify: `internal/commands/dashboard_platform.go`

- [ ] **Step 1: Implement collectPlatformData**

Replace the contents of `dashboard_platform.go` with:

```go
// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// collectPlatformData fetches Jamf Platform data in parallel and populates
// the Platform section of DashboardData.
// Note: DDM reports are omitted — they require per-device iteration which is
// too expensive for a summary dashboard. Blueprints + benchmarks cover the
// Platform section. DDM can be added later with a device-count summary endpoint.
func collectPlatformData(ctx context.Context, client registry.PlatformClient, data *DashboardData) {
	var wg sync.WaitGroup
	var mu sync.Mutex

	platform := &platformStatus{}

	// Blueprints
	wg.Add(1)
	go func() {
		defer wg.Done()
		bps, err := client.ListBlueprints(ctx, nil, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: platform blueprints: %v\n", err)
			return
		}
		mu.Lock()
		for _, bp := range bps {
			platform.Blueprints = append(platform.Blueprints, blueprintEntry{
				Name:            bp.Name,
				DeploymentState: bp.DeploymentState.State,
			})
		}
		mu.Unlock()
	}()

	// Compliance benchmarks
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := client.ListBenchmarks(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dashboard: platform benchmarks: %v\n", err)
			return
		}

		for _, bm := range resp.Benchmarks {
			entry := benchmarkEntry{
				Title: bm.Title,
			}

			// Fetch compliance percentage per benchmark
			pctResp, err := client.GetBenchmarkCompliancePercentage(ctx, bm.ID)
			if err == nil && pctResp != nil {
				entry.CompliancePct = pctResp.CompliancePercentage
			}

			// Fetch failing rules count
			rules, err := client.ListBenchmarkRulesStats(ctx, bm.ID, "", "")
			if err == nil {
				for _, rule := range rules {
					if rule.FailedCount > 0 {
						entry.FailingRules++
					}
				}
			}

			mu.Lock()
			platform.Benchmarks = append(platform.Benchmarks, entry)
			mu.Unlock()
		}
	}()

	wg.Wait()

	data.Platform = platform
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/keaton.svoma/Projects/jamf-cli && go build ./internal/commands/...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/commands/dashboard_platform.go
git commit -m "feat(dashboard): implement Jamf Platform data collection (blueprints, compliance)"
```

---

### Task 7: HTML template and rendering

**Files:**
- Modify: `internal/commands/dashboard_html.go`

This is the largest task. The HTML template includes embedded Chart.js, CSS, and vanilla JS for interactivity.

- [ ] **Step 1: Implement the HTML template and render function**

Replace the contents of `dashboard_html.go`. The file is large (~500 lines), so here is the structure:

```go
// Copyright 2026, Jamf Software LLC

package commands

import (
	"html/template"
	"io"
	"sort"
)

// renderDashboard executes the HTML template with the given data and writes
// the result to w.
func renderDashboard(w io.Writer, data *DashboardData) error {
	// Sort OS distribution by count descending for charts
	if data.OSDist != nil {
		sort.Slice(data.OSDist.Versions, func(i, j int) bool {
			return data.OSDist.Versions[i].Count > data.OSDist.Versions[j].Count
		})
		// Cap at top 10 for readability
		if len(data.OSDist.Versions) > 10 {
			other := 0
			for _, v := range data.OSDist.Versions[10:] {
				other += v.Count
			}
			data.OSDist.Versions = append(data.OSDist.Versions[:10], osVersionCount{
				Version: "Other",
				Count:   other,
			})
		}
	}

	// Sort patch titles by compliance ascending (worst first)
	if data.Patch != nil {
		sort.Slice(data.Patch.Titles, func(i, j int) bool {
			return data.Patch.Titles[i].CompliancePct < data.Patch.Titles[j].CompliancePct
		})
	}

	// Sort audit results: CRITICAL first, then WARNING, then INFO
	if data.Audit != nil {
		sevOrder := map[string]int{severityCritical: 0, severityWarning: 1, severityInfo: 2}
		sort.Slice(data.Audit.Results, func(i, j int) bool {
			return sevOrder[data.Audit.Results[i].Severity] < sevOrder[data.Audit.Results[j].Severity]
		})
	}

	tmpl, err := template.New("dashboard").Parse(dashboardTemplate)
	if err != nil {
		return err
	}
	return tmpl.Execute(w, data)
}

const dashboardTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{.Title}}</title>
<script>` + chartJSMinified + `</script>
<style>
` + dashboardCSS + `
</style>
</head>
<body>

<header>
  <h1>{{.Title}}</h1>
  <p class="meta">Generated {{.GeneratedAt.Format "Jan 02, 2006 at 3:04 PM"}} · jamf-cli {{.CLIVersion}}</p>
  <div class="profiles">
    {{range .Profiles}}<span class="profile-badge profile-{{.Product}}">{{.Name}} ({{.Product}})</span>{{end}}
  </div>
  <div class="view-toggle">
    <button id="btn-summary" class="active" onclick="toggleView('summary')">Summary</button>
    <button id="btn-detail" onclick="toggleView('detail')">Detail</button>
  </div>
</header>

<main>

{{if .Fleet}}
<section class="dashboard-section" id="fleet">
  <h2 class="section-header" onclick="toggleSection(this)">
    <span class="chevron">▾</span> Fleet Summary
  </h2>
  <div class="section-body">
    <div class="card-grid">
      <div class="stat-card">
        <div class="stat-value">{{.Fleet.ManagedComputers}}</div>
        <div class="stat-label">Managed Computers</div>
      </div>
      <div class="stat-card">
        <div class="stat-value">{{.Fleet.ManagedMobile}}</div>
        <div class="stat-label">Managed Mobile</div>
      </div>
      <div class="stat-card">
        <div class="stat-value">{{.Fleet.Users}}</div>
        <div class="stat-label">Users</div>
      </div>
      <div class="stat-card muted">
        <div class="stat-value">{{.Fleet.UnmanagedComputers}}</div>
        <div class="stat-label">Unmanaged Computers</div>
      </div>
      <div class="stat-card muted">
        <div class="stat-value">{{.Fleet.UnmanagedMobile}}</div>
        <div class="stat-label">Unmanaged Mobile</div>
      </div>
    </div>
  </div>
</section>
{{end}}

{{if .Security}}
<section class="dashboard-section" id="security">
  <h2 class="section-header" onclick="toggleSection(this)">
    <span class="chevron">▾</span> Security Posture
    <span class="section-badge">{{.Security.Total}} devices</span>
  </h2>
  <div class="section-body">
    <div class="chart-grid">
      <div class="chart-card">
        <canvas id="chart-fv"></canvas>
        <div class="chart-label">FileVault</div>
      </div>
      <div class="chart-card">
        <canvas id="chart-fw"></canvas>
        <div class="chart-label">Firewall</div>
      </div>
      <div class="chart-card">
        <canvas id="chart-gk"></canvas>
        <div class="chart-label">Gatekeeper</div>
      </div>
      <div class="chart-card">
        <canvas id="chart-sip"></canvas>
        <div class="chart-label">SIP</div>
      </div>
    </div>
  </div>
</section>
{{end}}

{{if .Audit}}
<section class="dashboard-section" id="audit">
  <h2 class="section-header" onclick="toggleSection(this)">
    <span class="chevron">▾</span> Audit Findings
    {{if gt (.Audit.CriticalCount) 0}}<span class="severity-badge critical">{{.Audit.CriticalCount}} Critical</span>{{end}}
    {{if gt (.Audit.WarningCount) 0}}<span class="severity-badge warning">{{.Audit.WarningCount}} Warning</span>{{end}}
    {{if gt (.Audit.InfoCount) 0}}<span class="severity-badge info">{{.Audit.InfoCount}} Info</span>{{end}}
  </h2>
  <div class="section-body">
    <div class="filter-bar detail-only">
      <button class="filter-btn active" onclick="filterAudit('all')">All</button>
      <button class="filter-btn" onclick="filterAudit('CRITICAL')">Critical</button>
      <button class="filter-btn" onclick="filterAudit('WARNING')">Warning</button>
      <button class="filter-btn" onclick="filterAudit('INFO')">Info</button>
    </div>
    <table class="data-table detail-only">
      <thead><tr><th>Severity</th><th>Category</th><th>Check</th><th>Affected</th><th>Recommendation</th></tr></thead>
      <tbody>
        {{range .Audit.Results}}
        <tr data-severity="{{.Severity}}">
          <td><span class="severity-dot {{toLower .Severity}}"></span> {{.Severity}}</td>
          <td>{{.Category}}</td>
          <td>{{.Name}}</td>
          <td>{{.AffectedCount}}</td>
          <td>{{.Recommendation}}</td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>
</section>
{{end}}

{{if .Patch}}
<section class="dashboard-section" id="patch">
  <h2 class="section-header" onclick="toggleSection(this)">
    <span class="chevron">▾</span> Patch Compliance
    <span class="section-badge">{{len .Patch.Titles}} titles</span>
  </h2>
  <div class="section-body">
    <div class="chart-container">
      <canvas id="chart-patch"></canvas>
    </div>
    <table class="data-table detail-only">
      <thead><tr><th>Title</th><th>Latest</th><th>Up to Date</th><th>Out of Date</th><th>Compliance</th></tr></thead>
      <tbody>
        {{range .Patch.Titles}}
        <tr>
          <td>{{.Name}}</td>
          <td>{{.LatestVersion}}</td>
          <td>{{.UpToDate}}</td>
          <td>{{.OutOfDate}}</td>
          <td><div class="pct-bar"><div class="pct-fill" style="width:{{printf "%.0f" .CompliancePct}}%"></div><span>{{printf "%.0f" .CompliancePct}}%</span></div></td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>
</section>
{{end}}

{{if .Devices}}
<section class="dashboard-section" id="devices">
  <h2 class="section-header" onclick="toggleSection(this)">
    <span class="chevron">▾</span> Device Compliance
  </h2>
  <div class="section-body">
    <div class="card-grid">
      <div class="stat-card {{if gt .Devices.StaleDevices 0}}alert{{end}}">
        <div class="stat-value">{{.Devices.StaleDevices}}</div>
        <div class="stat-label">Stale Devices (>{{.Devices.StaleThresholdDays}}d)</div>
      </div>
      <div class="stat-card {{if gt .Devices.FailedMDMCommands 0}}alert{{end}}">
        <div class="stat-value">{{.Devices.FailedMDMCommands}}</div>
        <div class="stat-label">Failed MDM Commands</div>
      </div>
    </div>
  </div>
</section>
{{end}}

{{if .OSDist}}
<section class="dashboard-section" id="os-dist">
  <h2 class="section-header" onclick="toggleSection(this)">
    <span class="chevron">▾</span> OS Distribution
  </h2>
  <div class="section-body">
    <div class="chart-container">
      <canvas id="chart-os"></canvas>
    </div>
  </div>
</section>
{{end}}

{{if .Protect}}
<section class="dashboard-section" id="protect">
  <h2 class="section-header" onclick="toggleSection(this)">
    <span class="chevron">▾</span> Jamf Protect
  </h2>
  <div class="section-body">
    <div class="card-grid">
      <div class="stat-card"><div class="stat-value">{{.Protect.Plans}}</div><div class="stat-label">Plans</div></div>
      <div class="stat-card"><div class="stat-value">{{.Protect.Endpoints}}</div><div class="stat-label">Endpoints</div></div>
      <div class="stat-card"><div class="stat-value">{{.Protect.AnalyticsActive}}/{{.Protect.AnalyticsTotal}}</div><div class="stat-label">Analytics Active</div></div>
      <div class="stat-card"><div class="stat-value">{{.Protect.AnalyticSets}}</div><div class="stat-label">Analytic Sets</div></div>
      <div class="stat-card"><div class="stat-value">{{.Protect.ExceptionSets}}</div><div class="stat-label">Exception Sets</div></div>
    </div>
  </div>
</section>
{{end}}

{{if .Platform}}
<section class="dashboard-section" id="platform">
  <h2 class="section-header" onclick="toggleSection(this)">
    <span class="chevron">▾</span> Jamf Platform
  </h2>
  <div class="section-body">
    {{if .Platform.Blueprints}}
    <h3>Blueprints</h3>
    <table class="data-table">
      <thead><tr><th>Name</th><th>Deployment State</th></tr></thead>
      <tbody>
        {{range .Platform.Blueprints}}
        <tr>
          <td>{{.Name}}</td>
          <td><span class="deploy-badge deploy-{{toLower .DeploymentState}}">{{.DeploymentState}}</span></td>
        </tr>
        {{end}}
      </tbody>
    </table>
    {{end}}
    {{if .Platform.Benchmarks}}
    <h3>Compliance Benchmarks</h3>
    <table class="data-table">
      <thead><tr><th>Benchmark</th><th>Compliance</th><th>Failing Rules</th></tr></thead>
      <tbody>
        {{range .Platform.Benchmarks}}
        <tr>
          <td>{{.Title}}</td>
          <td><div class="pct-bar"><div class="pct-fill" style="width:{{printf "%.0f" .CompliancePct}}%"></div><span>{{printf "%.0f" .CompliancePct}}%</span></div></td>
          <td>{{.FailingRules}}</td>
        </tr>
        {{end}}
      </tbody>
    </table>
    {{end}}
  </div>
</section>
{{end}}

</main>

<footer>
  <p>Generated by <strong>jamf-cli {{.CLIVersion}}</strong> · {{.GeneratedAt.Format "2006-01-02T15:04:05Z07:00"}}</p>
</footer>

<script>
` + dashboardJS + `
</script>

{{if .Security}}
<script>
const secData = {
  fv: [{{.Security.FileVaultEnabled}}, {{.Security.Total}} - {{.Security.FileVaultEnabled}}],
  fw: [{{.Security.FirewallEnabled}}, {{.Security.Total}} - {{.Security.FirewallEnabled}}],
  gk: [{{.Security.GatekeeperEnabled}}, {{.Security.Total}} - {{.Security.GatekeeperEnabled}}],
  sip: [{{.Security.SIPEnabled}}, {{.Security.Total}} - {{.Security.SIPEnabled}}]
};
createDoughnut('chart-fv', secData.fv);
createDoughnut('chart-fw', secData.fw);
createDoughnut('chart-gk', secData.gk);
createDoughnut('chart-sip', secData.sip);
</script>
{{end}}

{{if .OSDist}}
<script>
createBarChart('chart-os',
  [{{range .OSDist.Versions}}'{{.Version}}',{{end}}],
  [{{range .OSDist.Versions}}{{.Count}},{{end}}]
);
</script>
{{end}}

{{if .Patch}}
<script>
createHorizontalBar('chart-patch',
  [{{range .Patch.Titles}}'{{.Name}}',{{end}}],
  [{{range .Patch.Titles}}{{printf "%.1f" .CompliancePct}},{{end}}]
);
</script>
{{end}}

</body>
</html>`

// toLower is registered as a template function for CSS class generation.
func init() {
	// This will be used in the template parse — we need to register it.
	// Actually, we'll use template.FuncMap in renderDashboard instead.
}

// Update renderDashboard to use FuncMap:
func init() {}
```

Wait — the template needs a `toLower` function. Let me restructure `renderDashboard` to register it:

Update `renderDashboard` to:

```go
func renderDashboard(w io.Writer, data *DashboardData) error {
	// Sort OS distribution by count descending for charts
	if data.OSDist != nil {
		sort.Slice(data.OSDist.Versions, func(i, j int) bool {
			return data.OSDist.Versions[i].Count > data.OSDist.Versions[j].Count
		})
		if len(data.OSDist.Versions) > 10 {
			other := 0
			for _, v := range data.OSDist.Versions[10:] {
				other += v.Count
			}
			data.OSDist.Versions = append(data.OSDist.Versions[:10], osVersionCount{
				Version: "Other",
				Count:   other,
			})
		}
	}

	if data.Patch != nil {
		sort.Slice(data.Patch.Titles, func(i, j int) bool {
			return data.Patch.Titles[i].CompliancePct < data.Patch.Titles[j].CompliancePct
		})
	}

	if data.Audit != nil {
		sevOrder := map[string]int{severityCritical: 0, severityWarning: 1, severityInfo: 2}
		sort.Slice(data.Audit.Results, func(i, j int) bool {
			return sevOrder[data.Audit.Results[i].Severity] < sevOrder[data.Audit.Results[j].Severity]
		})
	}

	funcMap := template.FuncMap{
		"toLower": strings.ToLower,
	}

	tmpl, err := template.New("dashboard").Funcs(funcMap).Parse(dashboardTemplate)
	if err != nil {
		return err
	}
	return tmpl.Execute(w, data)
}
```

And add `"strings"` to the imports.

The full file should be written as one coherent unit. The CSS and JS constants are defined separately for readability:

```go
const dashboardCSS = `
* { margin: 0; padding: 0; box-sizing: border-box; }
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #f8f9fa; color: #1a1a2e; line-height: 1.6; }
header { background: #0c101a; color: #f1f5f9; padding: 2rem; text-align: center; }
header h1 { font-size: 1.75rem; font-weight: 700; margin-bottom: 0.25rem; }
.meta { color: #8b89a3; font-size: 0.85rem; }
.profiles { margin-top: 0.75rem; display: flex; gap: 0.5rem; justify-content: center; flex-wrap: wrap; }
.profile-badge { padding: 0.25rem 0.75rem; border-radius: 1rem; font-size: 0.8rem; font-weight: 500; }
.profile-pro { background: #0073EC22; color: #0073EC; border: 1px solid #0073EC44; }
.profile-protect { background: #0E9F6E22; color: #0E9F6E; border: 1px solid #0E9F6E44; }
.profile-platform { background: #8B5CF622; color: #8B5CF6; border: 1px solid #8B5CF644; }
.view-toggle { margin-top: 1rem; display: flex; gap: 0.25rem; justify-content: center; }
.view-toggle button { background: #1e2740; color: #8b89a3; border: 1px solid #2d3654; padding: 0.375rem 1rem; border-radius: 0.375rem; cursor: pointer; font-size: 0.8rem; }
.view-toggle button.active { background: #0073EC; color: white; border-color: #0073EC; }
main { max-width: 1200px; margin: 2rem auto; padding: 0 1rem; }
.dashboard-section { background: white; border-radius: 0.75rem; margin-bottom: 1.5rem; box-shadow: 0 1px 3px rgba(0,0,0,0.08); overflow: hidden; }
.section-header { padding: 1rem 1.5rem; font-size: 1.1rem; font-weight: 600; cursor: pointer; display: flex; align-items: center; gap: 0.5rem; user-select: none; border-bottom: 1px solid #e5e7eb; }
.section-header:hover { background: #f9fafb; }
.chevron { transition: transform 0.2s; font-size: 0.8rem; }
.collapsed .chevron { transform: rotate(-90deg); }
.collapsed .section-body { display: none; }
.section-body { padding: 1.5rem; }
.section-badge { font-size: 0.75rem; color: #6b7280; font-weight: 400; margin-left: auto; }
.card-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 1rem; }
.stat-card { background: #f3f7ff; border-radius: 0.5rem; padding: 1.25rem; text-align: center; }
.stat-card.muted { background: #f9fafb; }
.stat-card.muted .stat-value { color: #9ca3af; }
.stat-card.alert { background: #fef2f2; border: 1px solid #fecaca; }
.stat-card.alert .stat-value { color: #dc2626; }
.stat-value { font-size: 2rem; font-weight: 700; color: #0c101a; }
.stat-label { font-size: 0.8rem; color: #6b7280; margin-top: 0.25rem; }
.chart-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 1.5rem; }
.chart-card { text-align: center; }
.chart-card canvas { max-width: 160px; margin: 0 auto; }
.chart-label { font-size: 0.85rem; font-weight: 500; margin-top: 0.5rem; color: #374151; }
.chart-container { max-width: 700px; margin: 0 auto; }
.data-table { width: 100%; border-collapse: collapse; font-size: 0.85rem; margin-top: 1rem; }
.data-table th { text-align: left; padding: 0.5rem 0.75rem; border-bottom: 2px solid #e5e7eb; color: #6b7280; font-weight: 600; font-size: 0.75rem; text-transform: uppercase; }
.data-table td { padding: 0.5rem 0.75rem; border-bottom: 1px solid #f3f4f6; }
.data-table tr:hover { background: #f9fafb; }
.severity-badge { font-size: 0.7rem; padding: 0.15rem 0.5rem; border-radius: 1rem; font-weight: 600; }
.severity-badge.critical { background: #fef2f2; color: #dc2626; }
.severity-badge.warning { background: #fffbeb; color: #d97706; }
.severity-badge.info { background: #eff6ff; color: #2563eb; }
.severity-dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; margin-right: 0.25rem; }
.severity-dot.critical { background: #dc2626; }
.severity-dot.warning { background: #d97706; }
.severity-dot.info { background: #2563eb; }
.pct-bar { position: relative; background: #e5e7eb; border-radius: 0.25rem; height: 1.25rem; overflow: hidden; min-width: 80px; }
.pct-fill { height: 100%; border-radius: 0.25rem; background: #0073EC; transition: width 0.3s; }
.pct-bar span { position: absolute; right: 0.5rem; top: 50%; transform: translateY(-50%); font-size: 0.7rem; font-weight: 600; color: #374151; }
.deploy-badge { font-size: 0.75rem; padding: 0.15rem 0.5rem; border-radius: 0.25rem; font-weight: 500; }
.deploy-deployed { background: #d1fae5; color: #065f46; }
.deploy-draft { background: #e5e7eb; color: #374151; }
.deploy-undeployed { background: #fef3c7; color: #92400e; }
.filter-bar { display: flex; gap: 0.25rem; margin-bottom: 0.75rem; }
.filter-btn { background: #f3f4f6; border: 1px solid #e5e7eb; padding: 0.25rem 0.75rem; border-radius: 0.25rem; cursor: pointer; font-size: 0.8rem; }
.filter-btn.active { background: #0073EC; color: white; border-color: #0073EC; }
footer { text-align: center; padding: 2rem; color: #9ca3af; font-size: 0.8rem; }
@media print { header { background: #333; } .view-toggle, .filter-bar { display: none; } .dashboard-section { break-inside: avoid; } }
@media (max-width: 768px) { .chart-grid { grid-template-columns: repeat(2, 1fr); } .card-grid { grid-template-columns: repeat(2, 1fr); } }
`

const dashboardJS = `
function toggleSection(header) {
  header.parentElement.classList.toggle('collapsed');
}

function toggleView(mode) {
  document.querySelectorAll('.view-toggle button').forEach(b => b.classList.remove('active'));
  document.getElementById('btn-' + mode).classList.add('active');
  document.querySelectorAll('.detail-only').forEach(el => {
    el.style.display = mode === 'detail' ? '' : 'none';
  });
}

function filterAudit(severity) {
  document.querySelectorAll('#audit .filter-btn').forEach(b => b.classList.remove('active'));
  event.target.classList.add('active');
  document.querySelectorAll('#audit tbody tr').forEach(row => {
    if (severity === 'all' || row.dataset.severity === severity) {
      row.style.display = '';
    } else {
      row.style.display = 'none';
    }
  });
}

function pctColor(pct) {
  if (pct >= 90) return '#10b981';
  if (pct >= 70) return '#f59e0b';
  return '#ef4444';
}

function createDoughnut(id, values) {
  var el = document.getElementById(id);
  if (!el) return;
  var pct = values[0] + values[1] > 0 ? Math.round(values[0] / (values[0] + values[1]) * 100) : 0;
  new Chart(el, {
    type: 'doughnut',
    data: {
      labels: ['Enabled', 'Disabled'],
      datasets: [{ data: values, backgroundColor: [pctColor(pct), '#e5e7eb'], borderWidth: 0 }]
    },
    options: {
      cutout: '70%',
      plugins: {
        legend: { display: false },
        tooltip: { enabled: true },
      },
      responsive: true,
    },
    plugins: [{
      id: 'centerText',
      afterDraw: function(chart) {
        var ctx = chart.ctx;
        ctx.save();
        ctx.font = 'bold 1.25rem -apple-system, sans-serif';
        ctx.fillStyle = pctColor(pct);
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';
        var cx = (chart.chartArea.left + chart.chartArea.right) / 2;
        var cy = (chart.chartArea.top + chart.chartArea.bottom) / 2;
        ctx.fillText(pct + '%', cx, cy);
        ctx.restore();
      }
    }]
  });
}

function createBarChart(id, labels, values) {
  var el = document.getElementById(id);
  if (!el) return;
  new Chart(el, {
    type: 'bar',
    data: {
      labels: labels,
      datasets: [{ data: values, backgroundColor: '#0073EC', borderRadius: 4 }]
    },
    options: {
      indexAxis: 'y',
      plugins: { legend: { display: false } },
      scales: { x: { beginAtZero: true, grid: { display: false } }, y: { grid: { display: false } } },
      responsive: true,
    }
  });
}

function createHorizontalBar(id, labels, values) {
  var el = document.getElementById(id);
  if (!el) return;
  var colors = values.map(function(v) { return pctColor(v); });
  new Chart(el, {
    type: 'bar',
    data: {
      labels: labels,
      datasets: [{ data: values, backgroundColor: colors, borderRadius: 4 }]
    },
    options: {
      indexAxis: 'y',
      plugins: { legend: { display: false } },
      scales: { x: { beginAtZero: true, max: 100, grid: { display: false }, ticks: { callback: function(v) { return v + '%'; } } }, y: { grid: { display: false } } },
      responsive: true,
    }
  });
}

// Start in summary mode
toggleView('summary');
`

// chartJSMinified holds the Chart.js v4 UMD bundle, minified.
// To update: download from https://cdn.jsdelivr.net/npm/chart.js@4/dist/chart.umd.min.js
// and paste the contents here.
const chartJSMinified = ` /* Chart.js will be embedded here — see Step 2 */ `
```

- [ ] **Step 2: Embed Chart.js**

Download Chart.js v4 UMD minified bundle and embed it in the `chartJSMinified` constant. Run:

```bash
curl -sL "https://cdn.jsdelivr.net/npm/chart.js@4.4.7/dist/chart.umd.min.js" > /tmp/chart.min.js
wc -c /tmp/chart.min.js
```

Expected: ~200KB file. Then embed it as the value of `chartJSMinified` in `dashboard_html.go`. Due to the file size, use `go:embed` instead of a string constant:

Create `internal/commands/chartjs.min.js` with the downloaded content, then change the embed approach:

```go
import "embed"

//go:embed chartjs.min.js
var chartJSMinified string
```

This is cleaner than a massive string constant. Update the template to use the embedded variable.

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/keaton.svoma/Projects/jamf-cli && go build ./internal/commands/...`
Expected: no errors

- [ ] **Step 4: Test template rendering with mock data**

Create a simple test in `internal/commands/dashboard_test.go`:

```go
// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestRenderDashboard_ProOnly(t *testing.T) {
	data := &DashboardData{
		Title:       "Test Dashboard",
		GeneratedAt: time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC),
		CLIVersion:  "1.0.0-test",
		Profiles:    []dashboardProfile{{Name: "test-pro", Product: "pro", URL: "https://example.jamfcloud.com"}},
		Fleet:       &fleetSummary{ManagedComputers: 150, ManagedMobile: 75, Users: 200, UnmanagedComputers: 10},
		Security:    &securityPosture{Total: 150, FileVaultEnabled: 140, FirewallEnabled: 130, GatekeeperEnabled: 145, SIPEnabled: 148},
		Audit:       &auditSummary{Results: []auditResult{{Category: "security", Severity: "WARNING", Name: "Test check", AffectedCount: 5, Recommendation: "Fix it"}}},
		Patch:       &patchCompliance{Titles: []patchTitle{{Name: "Chrome", LatestVersion: "120.0", UpToDate: 90, OutOfDate: 10, Total: 100, CompliancePct: 90}}},
		Devices:     &deviceCompliance{StaleDevices: 3, FailedMDMCommands: 1, StaleThresholdDays: 14},
		OSDist:      &osDistribution{Versions: []osVersionCount{{Version: "15.2", Count: 100}, {Version: "14.7", Count: 50}}},
	}

	var buf bytes.Buffer
	if err := renderDashboard(&buf, data); err != nil {
		t.Fatalf("renderDashboard: %v", err)
	}

	html := buf.String()

	// Verify key content is present
	checks := []string{
		"Test Dashboard",
		"test-pro",
		"150",                // managed computers
		"FileVault",
		"Gatekeeper",
		"WARNING",
		"Chrome",
		"Stale Devices",
		"15.2",               // OS version
		"jamf-cli 1.0.0-test",
	}
	for _, check := range checks {
		if !strings.Contains(html, check) {
			t.Errorf("expected HTML to contain %q", check)
		}
	}
}

func TestRenderDashboard_ProtectSection(t *testing.T) {
	data := &DashboardData{
		Title:       "Test",
		GeneratedAt: time.Now(),
		Profiles:    []dashboardProfile{{Name: "test-protect", Product: "protect"}},
		Protect:     &protectCoverage{Plans: 5, AnalyticsTotal: 100, AnalyticsActive: 85, Endpoints: 500, AnalyticSets: 3, ExceptionSets: 2},
	}

	var buf bytes.Buffer
	if err := renderDashboard(&buf, data); err != nil {
		t.Fatalf("renderDashboard: %v", err)
	}

	html := buf.String()
	if !strings.Contains(html, "Jamf Protect") {
		t.Error("expected Protect section")
	}
	if !strings.Contains(html, "85/100") {
		t.Error("expected analytics active count")
	}
}

func TestRenderDashboard_NoSectionsWhenNil(t *testing.T) {
	data := &DashboardData{
		Title:       "Empty",
		GeneratedAt: time.Now(),
		Profiles:    []dashboardProfile{{Name: "test", Product: "pro"}},
	}

	var buf bytes.Buffer
	if err := renderDashboard(&buf, data); err != nil {
		t.Fatalf("renderDashboard: %v", err)
	}

	html := buf.String()
	if strings.Contains(html, "Fleet Summary") {
		t.Error("Fleet section should not render when Fleet is nil")
	}
	if strings.Contains(html, "Jamf Protect") {
		t.Error("Protect section should not render when Protect is nil")
	}
}

func TestSecurityPosturePct(t *testing.T) {
	s := &securityPosture{Total: 200, FileVaultEnabled: 180}
	got := s.Pct(s.FileVaultEnabled)
	if got != 90.0 {
		t.Errorf("Pct = %v, want 90.0", got)
	}

	zero := &securityPosture{Total: 0}
	if zero.Pct(0) != 0 {
		t.Error("Pct with zero total should be 0")
	}
}
```

Run: `cd /Users/keaton.svoma/Projects/jamf-cli && go test -v -run TestRenderDashboard ./internal/commands/...`
Expected: all tests pass (3 render tests + 1 Pct test)

- [ ] **Step 5: Commit**

```bash
git add internal/commands/dashboard_html.go internal/commands/chartjs.min.js \
       internal/commands/dashboard_test.go
git commit -m "feat(dashboard): add HTML template with Chart.js, CSS, interactive JS, and tests"
```

---

### Task 8: Build verification and lint

**Files:** None new

- [ ] **Step 1: Full build**

```bash
cd /Users/keaton.svoma/Projects/jamf-cli && make build
```

Expected: binary at `bin/jamf-cli`

- [ ] **Step 2: Run all tests**

```bash
cd /Users/keaton.svoma/Projects/jamf-cli && make test
```

Expected: all tests pass

- [ ] **Step 3: Run lint**

```bash
cd /Users/keaton.svoma/Projects/jamf-cli && make lint
```

Expected: no new lint errors. Fix any that appear.

- [ ] **Step 4: Verify command registration**

```bash
bin/jamf-cli dashboard --help
bin/jamf-cli db --help
```

Expected: both show the dashboard help with `--profile`, `--title`, `--out-file` flags.

- [ ] **Step 5: Verify command appears in help**

```bash
bin/jamf-cli --help
```

Expected: `dashboard` appears under "Core Commands:" group.

- [ ] **Step 6: Verify error on no profiles**

```bash
bin/jamf-cli dashboard
```

Expected: error message about `--profile` being required.

- [ ] **Step 7: Commit any fixes**

```bash
git add -A
git commit -m "fix(dashboard): address lint and build issues"
```

---

### Task 9: Manual smoke test with mock rendering

**Files:** None new (testing only)

- [ ] **Step 1: Generate a test report with mock data**

Create a quick Go test that generates a full report to a file, then open it:

```bash
cd /Users/keaton.svoma/Projects/jamf-cli
go test -v -run TestRenderDashboard_ProOnly ./internal/commands/... 2>&1
```

To actually see the HTML, add a one-off test or tweak the existing test to write to a temp file. Alternatively, if you have a configured profile, test end-to-end:

```bash
bin/jamf-cli dashboard --profile <your-pro-profile> --out-file /tmp/dashboard.html
open /tmp/dashboard.html
```

- [ ] **Step 2: Visual inspection**

Open the HTML file in a browser and verify:
- Header shows title, timestamp, CLI version, profile badges
- Summary/Detail toggle works
- Collapsible sections work (click headers)
- Security posture doughnut charts render with percentage center text
- Audit findings table shows severity dots and filtering works
- Patch compliance horizontal bar chart renders
- OS distribution bar chart renders
- Stat cards show correct numbers
- Print preview looks clean (`Cmd+P`)

- [ ] **Step 3: Fix any visual issues**

If charts don't render, CSS is broken, or interactivity doesn't work — fix in `dashboard_html.go` and re-test.

- [ ] **Step 4: Commit any fixes**

```bash
git add -A
git commit -m "fix(dashboard): polish HTML template after visual review"
```

---

### Task 10: Final cleanup and PR readiness

**Files:** None new

- [ ] **Step 1: Run full verification suite**

```bash
cd /Users/keaton.svoma/Projects/jamf-cli
make build && make test && make lint
```

Expected: all green.

- [ ] **Step 2: Review git log**

```bash
git log --oneline main..HEAD
```

Expected: clean commit history on `feat/dashboard` branch.

- [ ] **Step 3: Verify no untracked files**

```bash
git status
```

Expected: clean working tree (or only expected untracked files like screenshots).
