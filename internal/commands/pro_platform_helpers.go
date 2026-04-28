// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"gopkg.in/yaml.v3"

	jamfclient "github.com/Jamf-Concepts/jamf-cli/internal/client"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
)

// platformVerboseTransport wraps an http.RoundTripper to mirror the verbose
// logging the Pro HTTP client (internal/client) emits — request/response
// lines at -v, headers at -vv, bodies at -vvv. Plumbed into the SDK via
// WithHTTPClient so spec-generated commands log the same way as hand-written
// Pro commands.
type platformVerboseTransport struct {
	inner http.RoundTripper
	level int
}

func (t *platformVerboseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.level >= 1 {
		fmt.Fprintf(os.Stderr, "--> %s %s\n", req.Method, req.URL)
	}
	if t.level >= 2 {
		jamfclient.LogHeaders(os.Stderr, req.Header, true)
	}
	if t.level >= 3 && req.Body != nil && req.Body != http.NoBody {
		raw, err := io.ReadAll(io.LimitReader(req.Body, jamfclient.BodyLogLimit))
		_ = req.Body.Close()
		if err == nil {
			jamfclient.LogBody(os.Stderr, raw)
			req.Body = io.NopCloser(bytes.NewReader(raw))
			req.ContentLength = int64(len(raw))
		}
	}

	resp, err := t.inner.RoundTrip(req)
	if err != nil {
		return resp, err
	}

	if t.level >= 1 {
		fmt.Fprintf(os.Stderr, "<-- %d %s\n", resp.StatusCode, resp.Status)
	}
	if t.level >= 2 {
		jamfclient.LogHeaders(os.Stderr, resp.Header, false)
	}
	if t.level >= 3 && resp.Body != nil {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, jamfclient.BodyLogLimit))
		_ = resp.Body.Close()
		jamfclient.LogBody(os.Stderr, jamfclient.RedactTokenBody(preview))
		resp.Body = io.NopCloser(bytes.NewReader(preview))
	}
	return resp, nil
}

// requirePlatformClient returns an error if the Platform SDK client is not
// available. Platform commands call this at the top of RunE so users get a
// clear message instead of a nil-pointer panic.
func requirePlatformClient(cliCtx *registry.CLIContext) error {
	if cliCtx.PlatformSDKClient == nil {
		return fmt.Errorf("this command requires platform gateway auth\n\n" +
			"Set up a platform profile:\n" +
			"  jamf-cli config add-profile <name> --auth-method platform --url <gateway-url> --tenant-id <id>\n\n" +
			"Or use environment variables:\n" +
			"  JAMF_URL, JAMF_CLIENT_ID, JAMF_CLIENT_SECRET, JAMF_TENANT_ID")
	}
	return nil
}

// newPlatformSDKClient constructs a *jamfplatform.Client used by all platform
// command code (both hand-written and spec-generated). The SDK transport
// handles auth, retry, and tenant injection; hand-written commands construct
// SDK subpackage clients per call (cheap — they share this transport).
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
	if verboseLevel > 0 {
		stdClient.Transport = &platformVerboseTransport{inner: stdClient.Transport, level: verboseLevel}
	}
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
