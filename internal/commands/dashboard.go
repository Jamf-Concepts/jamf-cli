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
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/client"
	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

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
		Long: `Generate a self-contained HTML report aggregating fleet health, security
posture, audit findings, and more across Jamf Pro, Protect, and Platform
products.

By default the report runs a fast collection (~20 API calls) that covers
fleet counts, security posture, OS distribution, check-in compliance, audit
findings, and environment object counts — a comprehensive overview regardless
of instance size.

Add --full to also collect patch compliance (2 calls per patch title), hardware
models, cleanup analysis (1 API call per policy and profile), and org structure.
On a large instance with 30 patch titles and 300 config objects, --full adds
roughly 350 additional API calls and may take 60-120 seconds.

The report covers the profile selected by the global -p/--profile flag
(or JAMF_PROFILE, or the configured default). Add --include-profile to
pull a second product into the same report. Each profile's product type
(pro, protect, or platform) determines which sections are populated, and
all profiles are authenticated before any data collection begins.

HTML goes to stdout; redirect it, or use the global --out-file.

Examples:
  jamf-cli dashboard -p prod-pro --out-file report.html
  jamf-cli dashboard -p prod-pro --full --out-file full-report.html
  jamf-cli dashboard -p prod-pro --include-profile prod-protect > report.html
  jamf-cli dashboard -p my-platform --include-profile my-pro --title "Q2 Fleet Report"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := config.Load()

			names := dashboardProfileNames(cfg, extraProfiles)
			if len(names) == 0 {
				var available []string
				if cfg != nil {
					for name := range cfg.Profiles {
						available = append(available, name)
					}
				}
				if len(available) > 0 {
					sort.Strings(available)
					return fmt.Errorf("no profile selected: pass -p/--profile\n\nAvailable profiles: %v\nList all: jamf-cli config list", available)
				}
				return fmt.Errorf("no profile selected: pass -p/--profile\n\nConfigure one first: jamf-cli config add-profile")
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
	cmd.Flags().BoolVar(&full, "full", false, "collect additional sections (patch compliance, hardware models, cleanup analysis, org structure) — significantly more API calls on large instances")

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

type dashboardOptions struct {
	Profiles    []string
	Title       string
	SmartGroups []string
	Full        bool
}

type resolvedClients struct {
	profile        dashboardProfile
	pro            registry.HTTPClient
	protect        registry.ProtectClient
	platform       *jamfplatform.Client
	hasSecurityCloud bool
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
			collectProData(ctx, rc.pro, data, opts.SmartGroups, opts.Full)
		case "protect":
			collectProtectData(ctx, rc.protect, data)
		case "platform":
			collectProData(ctx, rc.pro, data, opts.SmartGroups, opts.Full)
			collectPlatformData(ctx, rc.platform, data)
			if rc.hasSecurityCloud {
				collectSecurityCloudData(ctx, rc.platform, data)
			}
		}
	}

	// Phase 3: Render HTML. The writer comes from the output formatter, so
	// the global --out-file already points it at the file it opened; opening
	// the path a second time here would truncate what root is holding.
	return renderDashboard(w, data)
}

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
		if err := checkScopeConflict(cfg, profileName); err != nil {
			return resolvedClients{}, err
		}
		rc.pro = &cliClient{client.New(resolvedURL, authProvider, client.WithGatewayScope(pp.Scope()))}
		sdk, err := newPlatformSDKClient(resolvedURL, pp.ClientID(), pp.ClientSecret(), pp.Scope(), shouldShowSpinner())
		if err != nil {
			return resolvedClients{}, err
		}
		rc.platform = sdk
		rc.hasSecurityCloud = true
		rc.profile.URL = resolvedURL

	default: // "pro"
		resolvedURL, authProvider, err := ResolveAuthForProfile(cfg, AuthParams{Profile: profileName})
		if err != nil {
			return resolvedClients{}, err
		}
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

	return rc, nil
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
