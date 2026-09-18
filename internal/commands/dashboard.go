// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/client"
	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
	"github.com/jamf/jamfplatform-go-sdk/jamfplatform"
)

// dashboardCostNote is the one statement of what each tier costs. Every other
// surface that mentions the cost — this command's Long, the generate_report
// tool description, the jamf-report skill — quotes this rather than restating
// it, because the three used to give three different numbers and all of them
// were wrong.
//
// A formula, not a constant. The fast tier was documented as "~20 API calls
// total, independent of instance size" while four of its audit checks swept the
// fleet or issued a request per policy: on a 50,000-device, 500-policy instance
// that tier cost roughly 2,222 requests, about 2,000 of them strictly serial.
// Those four checks are in the full tier now, and the fast tier's cost really
// is fixed apart from the one inventory page sequence it shares.
const dashboardCostNote = "The fast tier costs ⌈C/500⌉ + ~22 requests for C computers. " +
	"--full adds 2 per patch title, 1 per policy, 1 per configuration profile, " +
	"1 per Mac or mobile app, mobile profile and printer (the category tally), " +
	"and reuses the fast tier's inventory pass — so on a 10,000-device instance " +
	"with 30 patch titles and 300 configuration objects it is roughly 500 further " +
	"requests and 60-120 seconds."

func newDashboardCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		extraProfiles []string
		title         string
		smartGroups   []string
		full          bool
	)

	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Generate a cross-product HTML fleet dashboard",
		Annotations: map[string]string{
			noAuthAnnotation: "true",
		},
		Long: `Generate a self-contained HTML report aggregating fleet health, security
posture, audit findings, and more across Jamf Pro, Protect, and Platform
products.

By default the report runs a fixed-cost collection covering fleet counts,
security posture, OS distribution, check-in compliance, audit findings, and
environment object counts.

Add --full to also collect patch compliance, hardware models, cleanup analysis,
org structure with per-category object counts, and the four audit checks whose
cost grows with the fleet or the policy count.

` + dashboardCostNote + `

The report covers the profile selected by the global -p/--profile flag (or
JAMF_PROFILE, or the configured default); with no profile named it falls back to
--url plus JAMF_TOKEN or JAMF_CLIENT_ID/JAMF_CLIENT_SECRET, the same chain every
other command walks. Add --include-profile to pull a second product into the
same report — the profiles must be of different product types, since one report
holds one set of each product's sections. Note a platform profile collects the
Jamf Pro sections too, so it cannot be combined with a pro profile. All profiles
are authenticated before any data collection begins.

Destination: prefer the global --out-file. HTML goes to stdout, so a shell
redirect also captures the error envelope a partially-collected run prints
there, which then lands inside the report file itself.

Inherited global flags this command does not honour: -o/--output, --field,
--select and --compact shape tabular output and have no effect on an HTML
document. --allow-partial-failure is honoured, and downgrades a run with
missing sections from exit 7 to exit 0. -n/--dry-run is not honoured: the
report is read-only, so there is nothing to preview.

Related: 'pro overview' shows the same fleet data as a terminal table, and
'pro dashboard' manages the Jamf Pro interface's own dashboard objects.

Examples:
  jamf-cli dashboard -p prod-pro --out-file report.html
  jamf-cli dashboard -p prod-pro --full --out-file full-report.html
  jamf-cli dashboard -p prod-pro --include-profile prod-protect --out-file report.html
  jamf-cli dashboard -p my-platform --include-profile my-protect --title "Q2 Fleet Report"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := config.Load()

			names := dashboardProfileNames(cfg, extraProfiles)
			if len(names) == 0 {
				// No profile named anywhere. Before refusing, check for the
				// flag/env credential route every other command reaches through
				// ResolveAuthForProfile: a CI runner holding JAMF_URL plus
				// client credentials and no config file is the documented
				// CI/CD shape, and this command used to be the only one that
				// could not run there — advising the operator to write a config
				// file on a runner that already had working credentials.
				if dashboardHasInvocationCredentials() {
					names = []string{""}
				} else {
					var available []string
					if cfg != nil {
						for name := range cfg.Profiles {
							available = append(available, name)
						}
					}
					if len(available) > 0 {
						sort.Strings(available)
						return fmt.Errorf("no profile selected: pass -p/--profile, or set JAMF_URL with JAMF_TOKEN or JAMF_CLIENT_ID/JAMF_CLIENT_SECRET\n\nAvailable profiles: %s\nList all: jamf-cli config list", quoteNames(available))
					}
					return fmt.Errorf("no credentials: pass -p/--profile, or set JAMF_URL with JAMF_TOKEN or JAMF_CLIENT_ID/JAMF_CLIENT_SECRET\n\nOr configure a profile: jamf-cli config add-profile")
				}
			}

			return runDashboard(cmd.Context(), writerFor(cliCtx), dashboardOptions{
				Profiles:    names,
				Title:       title,
				SmartGroups: smartGroups,
				Full:        full,
			})
		},
	}

	cmd.Flags().StringArrayVar(&extraProfiles, "include-profile", nil, "additional config profile(s) to pull into the same report (repeatable)")
	cmd.Flags().StringVar(&title, "title", "Jamf Fleet Dashboard", "report title")
	cmd.Flags().StringArrayVar(&smartGroups, "smart-groups", nil, "smart group names to visualize (repeatable)")
	cmd.Flags().BoolVar(&full, "full", false, "collect additional sections (patch compliance, hardware models, cleanup analysis, org structure, fleet-scaled audit checks) — cost grows with instance size; see --help")

	return cmd
}

// dashboardProfileNames resolves the set of profiles the report covers: the
// globally selected one first (-p, then JAMF_PROFILE, then the configured
// default — the same chain resolveAuth walks), then each --include-profile,
// de-duplicated so naming the primary again does not collect it twice.
func dashboardProfileNames(cfg *config.Config, extra []string) []string {
	primary := profile
	if primary == "" {
		primary = os.Getenv("JAMF_PROFILE")
	}
	if primary == "" && cfg != nil {
		primary = cfg.DefaultProfile
	}

	var names []string
	seen := map[string]bool{}
	for _, name := range append([]string{primary}, extra...) {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

// dashboardInvocationParams folds this invocation's flag vars and environment
// variables into AuthParams, the same ladder resolveAuth walks — flag first,
// then the JAMF_* variable.
//
// It has to fold them here rather than passing the flag vars through:
// ResolveAuthForProfile reads params only, deliberately, so that its result
// does not depend on global state. resolveAuth is the function that folds, and
// this path does not go through it — a profile name is what it keys on.
//
// Mirroring resolveAuth's scope rule: a scope flag settles the level, so the
// other level must not be backfilled from the environment beside it, or the two
// arrive as "both levels supplied together" and are refused.
func dashboardInvocationParams() AuthParams {
	env := func(flagVal, name string) string {
		if flagVal != "" {
			return flagVal
		}
		return os.Getenv(name)
	}

	params := AuthParams{
		ServerURL:    env(serverURL, "JAMF_URL"),
		Token:        env(token, "JAMF_TOKEN"),
		TokenFile:    tokenFile,
		ClientID:     env(clientID, "JAMF_CLIENT_ID"),
		ClientSecret: env(clientSecret, "JAMF_CLIENT_SECRET"),
	}
	if tenantID != "" || environmentID != "" {
		params.TenantID, params.EnvironmentID = tenantID, environmentID
	} else {
		params.TenantID = os.Getenv("JAMF_TENANT_ID")
		params.EnvironmentID = os.Getenv("JAMF_ENVIRONMENT_ID")
	}
	return params
}

// dashboardHasInvocationCredentials reports whether this invocation carries
// enough to authenticate without naming a config profile. It admits exactly
// what ResolveAuthForProfile can then resolve from the same params.
func dashboardHasInvocationCredentials() bool {
	p := dashboardInvocationParams()
	if p.ServerURL == "" {
		return false
	}
	if p.Token != "" || p.TokenFile != "" {
		return true
	}
	return p.ClientID != "" && p.ClientSecret != ""
}

type dashboardOptions struct {
	Profiles    []string
	Title       string
	SmartGroups []string
	Full        bool
}

type resolvedClients struct {
	profile  dashboardProfile
	pro      registry.HTTPClient
	protect  registry.ProtectClient
	platform *jamfplatform.Client
}

// Reader-facing section names. The tally is keyed on these rather than on a
// fetch, because the banner speaks to whoever opens the HTML: one section built
// from five calls that all failed is one missing section, not five, and the
// reader can check a named section against the document in front of them where
// a bare count is unverifiable.
const (
	sectionFleet         = "Fleet"
	sectionSecurity      = "Security Posture"
	sectionAudit         = "Audit Findings"
	sectionCheckin       = "Check-in Status"
	sectionOSDist        = "OS Distribution"
	sectionEnvironment   = "Environment"
	sectionSmartGroups   = "Smart Groups"
	sectionPatch         = "Patch Compliance"
	sectionHardware      = "Hardware Models"
	sectionCleanup       = "Cleanup"
	sectionOrgStructure  = "Org Structure"
	sectionProtect       = "Jamf Protect"
	sectionPlatform      = "Platform"
	sectionSecurityCloud = "Security Cloud"
)

// collectStatus is the shared per-section outcome the collectors write to. A
// section that cannot be fetched still prints its warning to stderr and leaves
// its data field nil or marked missing, and it records the miss here so
// runDashboard can name it in the banner and exit non-zero — a report with
// silently-dropped sections must not report success.
//
// Both halves are recorded. Failures alone cannot distinguish "one product was
// unreachable" from "nothing worked at all", and those want different exit
// codes: the first is a partial run, the second is whatever error stopped it.
// Its methods are safe to call from the collector goroutines.
type collectStatus struct {
	mu       sync.Mutex
	failed   map[string]bool
	ok       map[string]bool
	firstErr error
}

// recordFailure marks section as having lost at least one fetch. Every stderr
// warning in a collector must be paired with one of these: a warning the reader
// of the file never sees is not a signal, and an unrecorded miss is what lets a
// zero render as good news.
func (s *collectStatus) recordFailure(section string) {
	s.recordFailureErr(section, nil)
}

// recordFailureErr is recordFailure carrying the error that caused the miss, so
// a run where nothing succeeded can propagate that error's own exit code rather
// than reporting a partial failure over an empty document.
func (s *collectStatus) recordFailureErr(section string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed == nil {
		s.failed = map[string]bool{}
	}
	s.failed[section] = true
	if err != nil && s.firstErr == nil {
		s.firstErr = err
	}
}

// recordSuccess marks section as having landed at least one fetch.
func (s *collectStatus) recordSuccess(section string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ok == nil {
		s.ok = map[string]bool{}
	}
	s.ok[section] = true
}

// clearSection forgets everything recorded for one section. It exists for the
// one case where a failure turns out not to be one: a product the tenant does
// not own answers 403/404 on every call, which is an absent section rather than
// a failed one, and only the collector that made all the calls can tell.
func (s *collectStatus) clearSection(section string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.failed, section)
	delete(s.ok, section)
}

// failedSections returns the names of the sections that lost a fetch, sorted so
// the banner reads the same way twice.
func (s *collectStatus) failedSections() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.failed))
	for name := range s.failed {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// failures counts sections, not fetches.
func (s *collectStatus) failures() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.failed)
}

// successes counts the sections that produced data. A section that both failed
// and succeeded counts in both: it rendered something, and something is missing
// from it.
func (s *collectStatus) successes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.ok)
}

// collectError returns the first error recorded with recordFailureErr, for
// exitcode.PartialOrPropagate to read when nothing succeeded.
func (s *collectStatus) collectError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.firstErr
}

// dashboardProductOf names the set of report sections a profile populates. Two
// profiles answering the same name overwrite each other, because every
// collector writes the same DashboardData fields.
func dashboardProductOf(rc resolvedClients) string {
	// A platform profile collects the Pro sections too, so it collides with a
	// pro profile as well as with another platform one.
	if rc.profile.Product == "platform" {
		return "pro"
	}
	return rc.profile.Product
}

// refuseOverlappingProducts refuses a profile set whose members would populate
// the same sections.
//
// runDashboard hands one *DashboardData to each profile's collectors in turn
// and every collector assigns unconditionally, so the last profile wins and
// every earlier one is silently discarded — while data.Profiles still gains an
// entry per profile and the header still badges each of them. Two pro (or
// platform) profiles therefore produced one tenant's figures presented as
// covering both.
//
// Refused rather than rendered, because a report that silently covers one of
// the two tenants it names is worse than no report; the alternative is
// per-profile sections, which the data model cannot hold today.
func refuseOverlappingProducts(clients []resolvedClients) error {
	seen := map[string][]string{}
	for _, rc := range clients {
		product := dashboardProductOf(rc)
		seen[product] = append(seen[product], rc.profile.Name)
	}
	products := make([]string, 0, len(seen))
	for product := range seen {
		products = append(products, product)
	}
	sort.Strings(products)
	for _, product := range products {
		names := seen[product]
		if len(names) < 2 {
			continue
		}
		return fmt.Errorf("profiles %s both populate the %s sections, and one report can hold only one set of them\n\n"+
			"Run one report per profile, or name profiles of different product types (pro, protect, platform).\n"+
			"Note a platform profile collects the Jamf Pro sections too, so it collides with a pro profile",
			quoteNames(names), product)
	}
	return nil
}

// quoteNames renders a name list readably. Profile names may contain spaces, so
// %v on the slice gives no way to see where one ends.
func quoteNames(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = fmt.Sprintf("%q", n)
	}
	return strings.Join(quoted, " and ")
}

func runDashboard(ctx context.Context, w io.Writer, opts dashboardOptions) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Phase 1: Authenticate all profiles (fail fast)
	var clients []resolvedClients
	for _, profileName := range opts.Profiles {
		rc, err := resolveDashboardProfile(cfg, profileName)
		if err != nil {
			if profileName == "" {
				return err
			}
			return fmt.Errorf("profile %q: %w", profileName, err)
		}
		clients = append(clients, rc)
	}
	if err := refuseOverlappingProducts(clients); err != nil {
		return err
	}

	// Phase 2: Collect data
	data := &DashboardData{
		Title:       opts.Title,
		GeneratedAt: time.Now(),
		CLIVersion:  cliVersion,
	}

	status := &collectStatus{}
	for _, rc := range clients {
		data.Profiles = append(data.Profiles, rc.profile)

		switch rc.profile.Product {
		case "pro":
			collectProData(ctx, rc.pro, data, opts.SmartGroups, opts.Full, status)
		case "protect":
			collectProtectData(ctx, rc.protect, data, status)
		case "platform":
			collectProData(ctx, rc.pro, data, opts.SmartGroups, opts.Full, status)
			collectPlatformData(ctx, rc.platform, data, status)
			collectSecurityCloudData(ctx, rc.platform, data, status)
		}
	}

	return finishDashboard(w, data, status)
}

// finishDashboard renders the report and decides the exit code. It is a
// function of its own so both can be tested without a tenant: the exit contract
// is the part a pipeline depends on, and runDashboard has no seam a test can
// reach.
//
// The writer comes from the output formatter, so the global --out-file already
// points it at the file it opened; opening the path a second time here would
// truncate what root is holding.
func finishDashboard(w io.Writer, data *DashboardData, status *collectStatus) error {
	failed := status.failedSections()
	succeeded := status.successes()

	data.IncompleteSections = failed
	data.TotalSections = succeeded + len(failed)

	if err := renderDashboard(w, data); err != nil {
		return err
	}

	if len(failed) == 0 {
		return nil
	}

	// The report is written whether or not every section was collected, but a
	// run that dropped sections must not exit 0 by default: a pipeline treating
	// the report as authoritative needs to know it is incomplete. The
	// per-section warnings already named what failed on stderr; this is the
	// machine-readable signal.
	msg := fmt.Sprintf("dashboard rendered without %d of %d section(s): %s",
		len(failed), data.TotalSections, strings.Join(failed, ", "))

	// --allow-partial-failure is a root persistent flag this command advertises
	// in its own --help, so it has to be read. A documented flag that does
	// nothing is worse than an absent one.
	if allowPartialFailure && succeeded > 0 {
		fmt.Fprintf(os.Stderr, "warning: %s; continuing (--allow-partial-failure)\n", msg)
		return nil
	}

	// Nothing succeeded means the report is an empty shell, and "partial" is
	// the wrong answer for it: the truthful code is whatever stopped the first
	// fetch — an auth failure exits 3, not 7.
	err := exitcode.PartialOrPropagate(succeeded, len(failed), status.collectError(), msg)
	if e, ok := err.(*exitcode.Error); ok {
		return e.WithDetails(map[string]any{
			"failed_sections": failed,
			"total_sections":  data.TotalSections,
		})
	}
	return err
}

func resolveDashboardProfile(cfg *config.Config, profileName string) (resolvedClients, error) {
	// An empty name is the flag/env credential route, not a lookup failure:
	// there is no profile to read a product from, so the resolved provider's
	// type decides it — the gateway provider means platform, anything else pro.
	if profileName == "" {
		return resolveDashboardFromInvocation(cfg)
	}

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
		if err := checkScopeConflict(cfg, profileName); err != nil {
			return resolvedClients{}, err
		}
		if err := attachPlatformClients(&rc, resolvedURL, authProvider, pp); err != nil {
			return resolvedClients{}, err
		}

	default: // "pro"
		resolvedURL, authProvider, err := ResolveAuthForProfile(cfg, AuthParams{Profile: profileName})
		if err != nil {
			return resolvedClients{}, err
		}
		attachProClient(&rc, resolvedURL, authProvider)
	}

	return rc, nil
}

// resolveDashboardFromInvocation builds the clients from the flags and
// environment variables of this invocation, with no config profile named. It is
// the route a CI runner takes: JAMF_URL plus a token or client credentials, and
// no config file on disk.
//
// The product comes from the resolved provider rather than from a profile
// field, because there is no profile to read one from — the gateway provider is
// a platform credential and anything else is a Jamf Pro instance. Jamf Protect
// is not reachable this way: its credentials live under JAMFPROTECT_* and are
// read per profile, so a Protect section still needs one.
func resolveDashboardFromInvocation(cfg *config.Config) (resolvedClients, error) {
	resolvedURL, authProvider, err := ResolveAuthForProfile(cfg, dashboardInvocationParams())
	if err != nil {
		return resolvedClients{}, err
	}

	rc := resolvedClients{
		profile: dashboardProfile{
			// No profile was named, so the badge names the credential source
			// rather than inventing a profile name the operator never typed.
			Name:    credentialSource(""),
			Product: "pro",
			URL:     resolvedURL,
		},
	}

	if pp, ok := authProvider.(*auth.PlatformOAuth2Provider); ok {
		rc.profile.Product = "platform"
		if err := attachPlatformClients(&rc, resolvedURL, authProvider, pp); err != nil {
			return resolvedClients{}, err
		}
		return rc, nil
	}

	attachProClient(&rc, resolvedURL, authProvider)
	return rc, nil
}

// attachPlatformClients builds the gateway-routed Pro client and the Platform
// SDK client for a platform credential.
//
// WithVerbose is passed here as well as on the pro path: the platform branch
// used to omit it, so `dashboard -p my-platform -vvv` traced none of the Pro
// calls it makes through the gateway — on the profile type the MCP server's own
// help calls the richest.
func attachPlatformClients(rc *resolvedClients, resolvedURL string, authProvider auth.Provider, pp *auth.PlatformOAuth2Provider) error {
	rc.pro = &cliClient{client.New(resolvedURL, authProvider,
		client.WithVerbose(verboseLevel),
		client.WithGatewayScope(pp.Scope()))}
	sdk, err := newPlatformSDKClient(resolvedURL, pp.ClientID(), pp.ClientSecret(), pp.Scope(), shouldShowSpinner())
	if err != nil {
		return err
	}
	rc.platform = sdk
	rc.profile.URL = resolvedURL
	return nil
}

// attachProClient builds the Jamf Pro client for an instance credential.
func attachProClient(rc *resolvedClients, resolvedURL string, authProvider auth.Provider) {
	clientOpts := []client.Option{client.WithVerbose(verboseLevel)}
	if pp, ok := authProvider.(*auth.PlatformOAuth2Provider); ok {
		clientOpts = append(clientOpts, client.WithGatewayScope(pp.Scope()))
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
