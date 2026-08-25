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
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	jamfclient "github.com/Jamf-Concepts/jamf-cli/internal/client"
	"github.com/Jamf-Concepts/jamf-cli/internal/config"
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

// identityEncodingOnWrites asks for an uncompressed response on mutating
// requests.
//
// The gateway has a bug worth working around rather than living with: whenever
// a create response is gzipped it answers `"href": null` AND drops the Location
// header, and returns both when it is not — deterministic, wire-verified 3/3
// each way on POST /ztna/apps. Go's net/http sets Accept-Encoding: gzip on
// every request and decompresses transparently, so every create through this
// CLI saw null and the id was the only usable field in a response whose schema
// declares href required. Setting the header explicitly opts out of Go's
// transparent gzip (it only compresses/decompresses when it owns the header),
// which is safe here because a mutation's response body is a handful of bytes.
// Reads keep gzip: a full list is exactly where it earns its keep.
type identityEncodingOnWrites struct{ inner http.RoundTripper }

func (t *identityEncodingOnWrites) RoundTrip(req *http.Request) (*http.Response, error) {
	switch req.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		if req.Header.Get("Accept-Encoding") == "" {
			req = req.Clone(req.Context())
			req.Header.Set("Accept-Encoding", "identity")
		}
	}
	return t.inner.RoundTrip(req)
}

// dryRunGuardTransport refuses to send a mutating platform request when
// --dry-run is set.
//
// It is a backstop, not the mechanism: generated commands report the request and
// return before reaching the transport (see platform.ReportDryRun), because only
// the command knows the success status its caller asserts. What is left are the
// hand-written platform commands — blueprints apply/clone/deploy/import-profile
// and friends — which orchestrate several calls through SDK subpackages and have
// no such gate. Before this they wrote for real under a flag that promised a
// preview, so failing loudly is the conservative reading: nothing is sent, the
// exit code is non-zero, and the message says which request was refused.
//
// The token endpoint is exempt: authenticating is not a change, and refusing it
// would make -n fail before it could report anything.
type dryRunGuardTransport struct{ inner http.RoundTripper }

func (t *dryRunGuardTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
	default:
		if !strings.Contains(req.URL.Path, "/auth/token") {
			// A synthetic response, not an error: the SDK's retry client treats a
			// transport error (resp == nil) as always retryable, so refusing by
			// error made -n hang through five attempts of backoff before saying
			// anything. A 412 is a status isRetryableWriteStatus will not retry,
			// and it carries the reason in the envelope the transport already
			// knows how to report.
			body := fmt.Sprintf("{\"httpStatus\":412,\"errors\":[{\"code\":\"DRY_RUN\",\"description\":\"refused %s %s: --dry-run is set and this command has no preview mode. Re-run without -n to apply.\"}]}", req.Method, req.URL.Path)
			return &http.Response{
				StatusCode: http.StatusPreconditionFailed,
				Status:     "412 Precondition Failed",
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}
	}
	return t.inner.RoundTrip(req)
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

	// A plain *http.Client, deliberately: the SDK wraps whatever is injected
	// here in its OWN retryablehttp client (WithHTTPClient sets
	// retry.HTTPClient), so a retry client passed in becomes an inner retry
	// loop nested inside the SDK's outer one, and its policy wins on the
	// attempts it makes. That cost two real bugs. retryablehttp's default
	// policy retries a 5xx on any method, so a POST that answered 500 was
	// re-sent — four attempts against an endpoint whose write may already have
	// committed, which is how one command creates N objects. And its default
	// ErrorHandler drains the final response and returns a synthetic
	// "giving up after N attempt(s)", discarding the body: the traceId a 500
	// has to be reported with was gone from the error and from -vv alike.
	// The SDK's own client gets both right (isRetryableWriteStatus refuses to
	// retry POST/PATCH on a 500; PassthroughErrorHandler keeps the real
	// *APIResponseError), so the fix is to stop shadowing it.
	jar, _ := cookiejar.New(nil)
	stdClient := &http.Client{Timeout: 60 * time.Second, Jar: jar}
	stdClient.Transport = &identityEncodingOnWrites{inner: http.DefaultTransport}
	if dryRun {
		stdClient.Transport = &dryRunGuardTransport{inner: stdClient.Transport}
	}
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

// resolveSecurityCloudTenantID returns the Jamf Security Cloud tenant ID for
// the active profile, from JAMFSECURITY_TENANT_ID or the profile's
// security-cloud-tenant-id.
//
// Empty is a valid answer: the SDK then serves Security Cloud paths from the
// client-wide tenant ID, which is right for a tenant whose Pro and Security
// Cloud identifiers match and wrong (403 OWNERSHIP_FORBIDDEN) otherwise. The
// env var wins so CI can point one profile at a different Security Cloud
// tenant without editing config.
func resolveSecurityCloudTenantID(cfg *config.Config, profileName string) string {
	if id := os.Getenv("JAMFSECURITY_TENANT_ID"); id != "" {
		return id
	}
	if cfg == nil {
		return ""
	}
	// GetProfile resolves an empty name to the default profile, which the
	// caller may not have expanded yet.
	p, _, err := config.GetProfile(cfg, profileName)
	if err != nil {
		return ""
	}
	return p.SecurityCloudTenantID
}

// platformSDKClients builds the pair of clients the CLI dispatches through: one
// for the Jamf Pro tenant and one for the Security Cloud tenant.
//
// It is a pair rather than a single client because the scope is a per-client
// X-Tenant-Id header now, not a path segment. One client carries one tenant, and
// Security Cloud is a separate product with its own tenant identifier, so a
// customer holding both cannot be served by one client the way the old
// TenantIDFor-per-namespace lookup managed. The two share credentials and
// therefore the cached token; the second client costs a struct, not a login.
//
// With no Security Cloud tenant configured the same client is returned twice,
// which preserves the documented fallback: Security Cloud paths use the Jamf
// Pro tenant, correct only where the two happen to match.
func platformSDKClients(url, clientID, clientSecret, tenantID, securityCloudTenantID string, showSpinner bool) (platformClient, securityCloudClient *jamfplatform.Client) {
	platformClient = newPlatformSDKClient(url, clientID, clientSecret, tenantID, showSpinner)
	if securityCloudTenantID == "" || securityCloudTenantID == tenantID {
		return platformClient, platformClient
	}
	return platformClient, newPlatformSDKClient(url, clientID, clientSecret, securityCloudTenantID, showSpinner)
}

// securityPlatformSDKClient builds the Platform SDK client that serves the
// gateway-hosted part of Jamf Security Cloud, or returns nil when the profile
// carries no platform credentials. Both returns are nil in that case.
//
// Returning nil is a normal outcome, not an error: a profile configured only
// for Risk/Device Lifecycle/SSE still gets a working `security` tree, and the
// gateway-served subcommands report what to configure via
// platform.RequirePlatformClient when they run.
func securityPlatformSDKClient(cfg *config.Config, profileName string) (platformClient, securityCloudClient *jamfplatform.Client) {
	url := serverURL
	if url == "" {
		url = os.Getenv("JAMF_URL")
	}
	clientID := os.Getenv("JAMF_CLIENT_ID")
	clientSecret := os.Getenv("JAMF_CLIENT_SECRET")
	tenantID := os.Getenv("JAMF_TENANT_ID")

	if p, _, err := config.GetProfile(cfg, profileName); err == nil {
		if url == "" {
			// PlatformURL first: a profile carrying both credential sets keeps
			// the gateway URL there, because URL is the Radar host for the
			// Risk/Lifecycle/SSE client. Falling back to URL covers a plain
			// platform profile, whose URL *is* the gateway.
			url = p.PlatformURL
		}
		if url == "" {
			url = p.URL
		}
		// An id/secret pair is atomic — the same splicing hazard the scoped
		// pairs guard against — so the profile fills both or neither.
		if clientID == "" && clientSecret == "" {
			id, idErr := config.ResolveSecret(p.ClientID)
			secret, secretErr := config.ResolveSecret(p.ClientSecret)
			if idErr == nil && secretErr == nil {
				clientID, clientSecret = id, secret
			}
		}
		if tenantID == "" {
			tenantID = p.TenantID
		}
	}

	securityCloudTenantID := resolveSecurityCloudTenantID(cfg, profileName)
	if url == "" || clientID == "" || clientSecret == "" {
		return nil, nil
	}
	if tenantID == "" && securityCloudTenantID == "" {
		return nil, nil
	}
	return platformSDKClients(url, clientID, clientSecret, tenantID, securityCloudTenantID, shouldShowSpinner())
}
