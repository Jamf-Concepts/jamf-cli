// Copyright 2026, Jamf Software LLC

package commands

import (
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

// newPlatformSDKClient constructs a jamfplatform.Client with retry, spinner,
// and token cache — the same configuration used by the main PersistentPreRunE
// in root.go. This avoids bare SDK clients that lack retry and progress indication.
func newPlatformSDKClient(url, clientID, clientSecret, tenantID string, showSpinner bool) *jamfplatform.Client {
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

	return jamfplatform.NewClient(url, clientID, clientSecret, opts...)
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
