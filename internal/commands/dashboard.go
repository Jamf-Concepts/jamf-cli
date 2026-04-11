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
		profiles    []string
		title       string
		outFile     string
		smartGroups []string
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
				Profiles:    profiles,
				Title:       title,
				OutFile:     outFile,
				SmartGroups: smartGroups,
			})
		},
	}

	cmd.Flags().StringArrayVar(&profiles, "profile", nil, "config profile(s) to include (repeatable, required)")
	cmd.Flags().StringVar(&title, "title", "Jamf Fleet Dashboard", "report title")
	cmd.Flags().StringVar(&outFile, "out-file", "", "write HTML to file instead of stdout")
	cmd.Flags().StringArrayVar(&smartGroups, "smart-groups", nil, "smart group names to visualize (repeatable)")

	return cmd
}

type dashboardOptions struct {
	Profiles    []string
	Title       string
	OutFile     string
	SmartGroups []string
}

type resolvedClients struct {
	profile  dashboardProfile
	pro      registry.HTTPClient
	protect  registry.ProtectClient
	platform registry.PlatformClient
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
			collectProData(ctx, rc.pro, data, opts.SmartGroups)
		case "protect":
			collectProtectData(ctx, rc.protect, data)
		case "platform":
			collectProData(ctx, rc.pro, data, opts.SmartGroups)
			collectPlatformData(ctx, rc.platform, data)
		}
	}

	// Phase 3: Render HTML
	if opts.OutFile != "" {
		f, createErr := os.Create(opts.OutFile)
		if createErr != nil {
			return fmt.Errorf("creating output file: %w", createErr)
		}
		if renderErr := renderDashboard(f, data); renderErr != nil {
			_ = f.Close()
			return renderErr
		}
		return f.Close()
	}

	return renderDashboard(os.Stdout, data)
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
