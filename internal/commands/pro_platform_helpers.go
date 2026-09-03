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

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	jamfclient "github.com/Jamf-Concepts/jamf-cli/internal/client"
	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
)

// platformVerboseTransport wraps an http.RoundTripper to mirror the verbose
// logging the Pro HTTP client (internal/client) emits — request/response
// lines at -v, headers at -vv, bodies at -vvv. Plumbed into the SDK via
// WithHTTPClient so spec-generated commands log the same way as hand-written
// Pro commands.
//
// It also labels retries, which nothing else can. This transport sits *below*
// retryablehttp, so it already sees every attempt — but a retry sequence came
// out as N identical request/response pairs with nothing saying they were
// retries and no timing, so a bounded 15s of backoff still read as the CLI
// looping or hanging. The SDK's own RequestLogHook does not help here: it emits
// through the SDK's Logger interface, whose LogRequest carries only method, URL
// and body, so the attempt number and the wait cannot travel through it — the
// hook exists for consumers that log *only* via that interface (the Terraform
// provider's tflog, which is not a RoundTripper) and would merely duplicate
// every line for this CLI.
type platformVerboseTransport struct {
	inner http.RoundTripper
	level int

	// lastKey, lastFailed and attempt track consecutive identical requests so a
	// retry can be named as one. Two conditions, both needed:
	//
	//   - Consecutive. retryablehttp re-issues the same method and URL with
	//     nothing interleaved, so a different request in between resets it.
	//   - The previous attempt failed. retryablehttp only retries a failure, so
	//     a repeat after a 2xx is a fresh call however identical it looks —
	//     labelling it a retry is a confident lie about what the gateway did.
	//     Without this, a command that legitimately re-issues one request (a
	//     poll, or a --name lookup followed by a list of the same collection)
	//     reported "(retry 1, waited 0s)". Cheap to get right, because this
	//     transport sees the response the retry decision is made on.
	//
	// Not concurrency-guarded: the SDK drives one request at a time per
	// transport, and a racing command would only mislabel a log line.
	lastKey    string
	lastFailed bool
	attempt    int
	lastSent   time.Time
}

func (t *platformVerboseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.level >= 1 {
		key := req.Method + " " + req.URL.String()
		if key == t.lastKey && t.lastFailed {
			t.attempt++
			fmt.Fprintf(os.Stderr, "--> %s %s (retry %d, waited %s)\n",
				req.Method, req.URL, t.attempt, time.Since(t.lastSent).Round(100*time.Millisecond))
		} else {
			t.lastKey, t.attempt = key, 0
			fmt.Fprintf(os.Stderr, "--> %s %s\n", req.Method, req.URL)
		}
		t.lastSent = time.Now()
	}
	if t.level >= 2 {
		jamfclient.LogHeaders(os.Stderr, req.Header, true)
	}
	if t.level >= 3 && req.Body != nil && req.Body != http.NoBody {
		raw, err := io.ReadAll(io.LimitReader(req.Body, jamfclient.BodyLogLimit))
		_ = req.Body.Close()
		if err == nil {
			// Redacted, unlike before: the response side was already redacted
			// and the request side was not, which is backwards for the one
			// body guaranteed to carry a secret. The SDK's token exchange runs
			// through this transport, and clientcredentials.Config's
			// auto-detect AuthStyle retries with AuthStyleInParams after any
			// first-attempt error — so a rotated secret, the WAF 403 or a 5xx
			// is followed by an attempt whose form body carries client_secret
			// in plaintext.
			jamfclient.LogBody(os.Stderr, jamfclient.RedactBodyForLog(raw))
			req.Body = io.NopCloser(bytes.NewReader(raw))
			req.ContentLength = int64(len(raw))
		}
	}

	resp, err := t.inner.RoundTrip(req)
	if err != nil {
		// A transport-level error is retryable, so the next identical request
		// is a retry.
		t.lastFailed = true
		return resp, err
	}
	t.lastFailed = resp.StatusCode >= 400

	if t.level >= 1 {
		fmt.Fprintf(os.Stderr, "<-- %d %s\n", resp.StatusCode, resp.Status)
	}
	if t.level >= 2 {
		jamfclient.LogHeaders(os.Stderr, resp.Header, false)
	}
	if t.level >= 3 && resp.Body != nil {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, jamfclient.BodyLogLimit))
		_ = resp.Body.Close()
		jamfclient.LogBody(os.Stderr, jamfclient.RedactBodyForLog(preview))
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
//
// The retired-host refusal lives here, in the constructor, rather than at each
// caller: it is the one place every path that builds a platform client must
// pass through, and two of those paths did not have it. ResolveAuthForProfile
// checked it and the `security` and `school` resolvers did not, because
// PersistentPreRunE returns early for both products — so a profile still
// naming `{region}.apigw.jamf.com` reached the gateway-served Security Cloud
// commands and `school blueprints` and failed inside the token exchange, which
// is exactly the symptom the refusal exists to replace. A guard on a
// constructor cannot be forgotten by the next caller; a guard beside one
// caller can.
func newPlatformSDKClient(url, clientID, clientSecret string, scope auth.Scope, showSpinner bool) (*jamfplatform.Client, error) {
	if err := refuseRetiredGatewayURL(url); err != nil {
		return nil, err
	}
	opts := []jamfplatform.Option{
		jamfplatform.WithUserAgent("jamf-cli/" + cliVersion),
	}
	// Exactly one scope option, or none: the SDK treats both-set as a mistake
	// and lets environment win, and an organization-scoped credential wants
	// neither — the gateway reads its scope from the access token.
	switch scope.Kind {
	case auth.ScopeEnvironment:
		if scope.ID != "" {
			opts = append(opts, jamfplatform.WithEnvironmentID(scope.ID))
		}
	case auth.ScopeTenant:
		if scope.ID != "" {
			opts = append(opts, jamfplatform.WithTenantID(scope.ID))
		}
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

	return jamfplatform.NewClient(url, clientID, clientSecret, opts...), nil
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

// resolveScope returns the level the active profile's integration is scoped to,
// and the identifier naming it.
//
// The three levels are mutually exclusive — an integration is created at one of
// them in Jamf Account, and the credential carries that choice — so this
// resolves to exactly one. Environment is the level to prefer; tenant is what
// Jamf Account calls the legacy method for integrations without a platform
// environment; organization-scoped credentials name neither, and the gateway
// resolves them from the access token.
//
// Precedence is flag, then env var, then the profile — the same ladder
// resolveAuth applies to every other credential input, so `--tenant-id B`
// against a profile holding tenant A retargets it the way `JAMF_TENANT_ID=B`
// already did. Reading only the env vars was worse than not honouring the flag
// at all: the `security` product never reaches ResolveAuthForProfile, so
// nothing on that path read the flag, and `--tenant-id B security device-groups
// delete <id>` deleted in the profile's tenant A and exited 0 with nothing in
// the output naming the tenant it used. --url is honoured here, so the two
// documented ways to override a profile's scope disagreed and only the failing
// one was silent.
//
// Environment wins over tenant when both are somehow present, matching the
// SDK's own precedence; a source naming both is rejected earlier, by
// checkScopeConflict, since the pair is a configuration mistake rather than a
// combination worth resolving silently.
func resolveScope(cfg *config.Config, profileName string) auth.Scope {
	if environmentID != "" {
		return auth.EnvironmentScope(environmentID)
	}
	if tenantID != "" {
		return auth.TenantScope(tenantID)
	}
	if id := os.Getenv("JAMF_ENVIRONMENT_ID"); id != "" {
		return auth.EnvironmentScope(id)
	}
	if id := os.Getenv("JAMF_TENANT_ID"); id != "" {
		return auth.TenantScope(id)
	}
	if cfg == nil {
		return auth.Scope{}
	}
	// GetProfile resolves an empty name to the default profile, which the
	// caller may not have expanded yet.
	p, _, err := config.GetProfile(cfg, profileName)
	if err != nil {
		return auth.Scope{}
	}
	if p.EnvironmentID != "" {
		return auth.EnvironmentScope(p.EnvironmentID)
	}
	return auth.TenantScope(p.TenantID)
}

// checkScopeConflict rejects a profile that names two levels at once.
//
// A credential is minted against one level, and the gateway refuses the other
// level's header with 403 OWNERSHIP_FORBIDDEN even when both IDs belong to the
// same customer. So there is no reading of "environment-id and tenant-id" that
// works: one of them is guaranteed to be wrong for this credential, and picking
// by precedence would report that as a permission error from whichever half
// lost.
func checkScopeConflict(cfg *config.Config, profileName string) error {
	// Flags first, then env vars: both override the profile, so a pair supplied
	// at either level is the same mistake one level up. Checking only the
	// profile let `--tenant-id … --environment-id …` and the JAMF_* pair
	// through to a request built from whichever won.
	if tenantID != "" && environmentID != "" {
		return exitcode.New(exitcode.Usage, fmt.Sprintf(
			"--environment-id (%s) and --tenant-id (%s) are both set\n\n"+
				"An API integration is created at one level — organization, platform environment, or\n"+
				"tenant — and its credential only works with that level's header. Pass only the one\n"+
				"this credential was created for.",
			environmentID, tenantID))
	}
	// A flag that names one level settles it, whatever the environment or the
	// profile says.
	if tenantID != "" || environmentID != "" {
		return nil
	}
	envTenant, envEnvironment := os.Getenv("JAMF_TENANT_ID"), os.Getenv("JAMF_ENVIRONMENT_ID")
	if envTenant != "" && envEnvironment != "" {
		return exitcode.New(exitcode.Usage, fmt.Sprintf(
			"JAMF_ENVIRONMENT_ID (%s) and JAMF_TENANT_ID (%s) are both set\n\n"+
				"An API integration is created at one level — organization, platform environment, or\n"+
				"tenant — and its credential only works with that level's header. Unset whichever one\n"+
				"this credential was not created for.",
			envEnvironment, envTenant))
	}
	// An env var that names one level settles it, whatever the profile says.
	if envTenant != "" || envEnvironment != "" {
		return nil
	}
	if cfg == nil {
		return nil
	}
	p, resolved, err := config.GetProfile(cfg, profileName)
	if err != nil || p == nil {
		return nil
	}
	if p.EnvironmentID == "" || p.TenantID == "" {
		return nil
	}
	return exitcode.New(exitcode.Usage, fmt.Sprintf(
		"profile %q sets both environment-id (%s) and tenant-id (%s)\n\n"+
			"An API integration is created at one level — organization, platform environment, or\n"+
			"tenant — and its credential only works with that level's header. Keep the one this\n"+
			"integration was created for and remove the other, or run 'jamf-cli platform setup'\n"+
			"again for %q.",
		resolved, p.EnvironmentID, p.TenantID, resolved))
}

// securityPlatformSDKClient builds the Platform SDK client that serves the
// gateway-hosted part of Jamf Security Cloud, or returns nil when the profile
// carries no platform credentials.
//
// A nil client with a nil error is a normal outcome: a profile configured only
// for Risk/Device Lifecycle/SSE still gets a working `security` tree, and the
// gateway-served subcommands report what to configure via
// platform.RequirePlatformClient when they run. An error means the credentials
// are present but unusable — today, a profile still naming the retired gateway
// host — which has to be reported rather than degraded into "no credentials
// configured".
func securityPlatformSDKClient(cfg *config.Config, profileName string) (*jamfplatform.Client, error) {
	url := serverURL
	if url == "" {
		url = os.Getenv("JAMF_URL")
	}
	clientID := os.Getenv("JAMF_CLIENT_ID")
	clientSecret := os.Getenv("JAMF_CLIENT_SECRET")

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
		//
		// A failed resolve is reported, not swallowed. Discarding it turned a
		// deleted keychain item, a locked login keychain or a moved file: path
		// into "no Jamf Security Cloud credentials configured", which sends the
		// operator to re-enter credentials that were already stored. Both
		// sibling copies — ResolveAuthForProfile and resolveSecurityClient's
		// fillPair — wrap and return; this one is no exception.
		if clientID == "" && clientSecret == "" {
			id, err := config.ResolveSecret(p.ClientID)
			if err != nil {
				return nil, fmt.Errorf("resolving client-id for profile %q: %w", profileName, err)
			}
			secret, err := config.ResolveSecret(p.ClientSecret)
			if err != nil {
				return nil, fmt.Errorf("resolving client-secret for profile %q: %w", profileName, err)
			}
			clientID, clientSecret = id, secret
		}
	}

	if url == "" || clientID == "" || clientSecret == "" {
		return nil, nil
	}
	// A scope is not required: an organization-scoped integration carries its
	// scope in the token. What is required is credentials.
	return newPlatformSDKClient(url, clientID, clientSecret, resolveScope(cfg, profileName), shouldShowSpinner())
}
