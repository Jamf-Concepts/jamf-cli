// Copyright 2026, Jamf Software LLC

package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/httptransport"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// Client is the HTTP client for Jamf Pro API
type Client struct {
	baseURL      string
	httpClient   *http.Client
	auth         auth.Provider
	verboseLevel int // 1 = request/response lines, 2 = +headers
	// gateway is set when requests route through the Jamf Platform Gateway:
	// paths are mapped into their gateway namespace and the scope travels as a
	// header. It is separate from scope because an organization-scoped
	// credential is gateway auth with no header at all, so the presence of an
	// ID cannot stand in for "this is the gateway".
	gateway bool
	scope   auth.Scope
}

// Option configures the client
type Option func(*Client)

// WithVerbose sets the verbosity level (1 = request/response lines, 2 = +headers).
func WithVerbose(level int) Option {
	return func(c *Client) {
		c.verboseLevel = level
	}
}

// WithGatewayScope enables platform gateway mode: paths are rewritten into their
// gateway namespace (/api/v1/x -> /pro/v1/x, /JSSResource/x ->
// /proclassic/x) and the scope travels as a request header —
// X-Environment-Id or X-Tenant-Id, or nothing for an organization-scoped
// credential, which the gateway resolves from the access token.
func WithGatewayScope(scope auth.Scope) Option {
	return func(c *Client) {
		c.gateway = true
		c.scope = scope
	}
}

// WithCookieJar sets the cookie jar on the HTTP client. Sharing a jar with the
// auth provider enables sticky session affinity cookies (e.g. APBALANCEID on
// Jamf Cloud) to persist from the token exchange through all API calls.
func WithCookieJar(jar http.CookieJar) Option {
	return func(c *Client) {
		c.httpClient.Jar = jar
	}
}

// New creates a new Jamf Pro API client
func New(baseURL string, authProvider auth.Provider, opts ...Option) *Client {
	c := &Client{
		baseURL: baseURL,
		// No Client.Timeout — bound by ctx; per-phase timeouts live on
		// the tuned Transport. See NewTunedTransport for rationale.
		httpClient: &http.Client{
			Transport: httptransport.New(),
		},
		auth: authProvider,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Do executes an HTTP request with authentication and retry logic
func (c *Client) Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	// Ensure path has /api prefix (OpenAPI paths omit it).
	// Classic API paths start with /JSSResource and bypass the /api prefix.
	if !strings.HasPrefix(path, "/api") && !strings.HasPrefix(path, "/JSSResource") {
		path = "/api" + path
	}

	// Platform gateway mode: map the path into its gateway namespace. The
	// tenant is sent as a header, not a path segment — see setScopeHeader.
	//   /JSSResource/* → /proclassic/*
	//   /api/v*        → /pro/v*
	if c.gateway {
		path = rewritePathForGateway(path)
	}

	// Buffer the request body so it can be replayed on retries.
	var bodyData []byte
	if body != nil {
		var err error
		bodyData, err = io.ReadAll(io.LimitReader(body, 100<<20)) // 100 MB limit
		if err != nil {
			return nil, fmt.Errorf("reading request body: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	token, err := c.auth.GetToken(ctx)
	if err != nil {
		return nil, exitcode.Wrap(exitcode.Authentication, fmt.Errorf("getting auth token: %w", err))
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "jamf-cli/1.0 (+https://github.com/Jamf-Concepts/jamf-cli)")
	c.setScopeHeader(req)

	// Classic API endpoints use XML; modern API uses JSON.
	// An explicit Accept override in the context takes precedence (used by binary download commands).
	isClassic := strings.HasPrefix(path, "/JSSResource") || strings.HasPrefix(path, "/proclassic")
	if override := registry.AcceptFromContext(ctx); override != "" {
		req.Header.Set("Accept", override)
	} else if isClassic {
		req.Header.Set("Accept", "application/xml")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	if bodyData != nil {
		if override := registry.ContentTypeFromContext(ctx); override != "" {
			req.Header.Set("Content-Type", override)
		} else if isClassic {
			req.Header.Set("Content-Type", "application/xml")
		} else {
			req.Header.Set("Content-Type", "application/json")
		}
	}

	if c.verboseLevel >= 1 {
		fmt.Fprintf(os.Stderr, "--> %s %s\n", method, req.URL)
	}
	if c.verboseLevel >= 2 {
		logHeaders(os.Stderr, req.Header, true)
	}
	if c.verboseLevel >= 3 {
		logBody(os.Stderr, bodyData)
	}

	resp, err := c.doWithRetry(ctx, req, bodyData)
	if err != nil {
		return nil, err
	}

	if c.verboseLevel >= 1 {
		fmt.Fprintf(os.Stderr, "<-- %d %s\n", resp.StatusCode, resp.Status)
	}
	if c.verboseLevel >= 2 {
		logHeaders(os.Stderr, resp.Header, false)
	}

	// Map HTTP error status codes to structured exit codes, unless the caller
	// declared this status a documented result (registry.WithAllowedStatuses) —
	// then the response is returned with its body intact for the caller to render.
	if resp.StatusCode >= 400 && !registry.StatusAllowed(ctx, resp.StatusCode) {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, bodyLogLimit))
		_ = resp.Body.Close()
		if c.verboseLevel >= 3 {
			logBody(os.Stderr, body)
		}
		return nil, httpStatusError(resp.StatusCode, method, path, body)
	}

	if c.verboseLevel >= 3 {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, bodyLogLimit))
		resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(preview), resp.Body))
		logBody(os.Stderr, preview)
	}

	return resp, nil
}

// Upload executes a streaming HTTP request with a caller-specified Content-Type
// and Content-Length. The body is never buffered, so multi-GB files stream
// straight through.
//
// 429 retry: when body implements io.Seeker (e.g. *os.File, *bytes.Reader, or
// the seekable multipart body from NewMultipartFileUpload), Upload retries up
// to 3 times on HTTP 429, honoring Retry-After. Non-seekable bodies surface
// 429 to the caller immediately — retrying would corrupt the upload.
func (c *Client) Upload(ctx context.Context, path string, body io.Reader, contentType string, contentLength int64) (*http.Response, error) {
	if !strings.HasPrefix(path, "/api") && !strings.HasPrefix(path, "/JSSResource") {
		path = "/api" + path
	}
	if c.gateway {
		path = rewritePathForGateway(path)
	}

	seeker, _ := body.(io.Seeker)
	maxAttempts := 1
	if seeker != nil {
		maxAttempts = 3
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			if _, err := seeker.Seek(0, io.SeekStart); err != nil {
				return nil, exitcode.Wrap(exitcode.General, fmt.Errorf("rewinding upload body for retry: %w", err))
			}
		}

		req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+path, body)
		if err != nil {
			return nil, fmt.Errorf("creating upload request: %w", err)
		}

		token, err := c.auth.GetToken(ctx)
		if err != nil {
			return nil, exitcode.Wrap(exitcode.Authentication, fmt.Errorf("getting auth token: %w", err))
		}

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("User-Agent", "jamf-cli/1.0 (+https://github.com/Jamf-Concepts/jamf-cli)")
		c.setScopeHeader(req)
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Accept", "application/json")
		req.ContentLength = contentLength

		if c.verboseLevel >= 1 {
			if maxAttempts > 1 {
				fmt.Fprintf(os.Stderr, "--> POST %s (%d bytes, attempt %d/%d)\n", req.URL, contentLength, attempt+1, maxAttempts)
			} else {
				fmt.Fprintf(os.Stderr, "--> POST %s (%d bytes)\n", req.URL, contentLength)
			}
		}
		if c.verboseLevel >= 2 {
			logHeaders(os.Stderr, req.Header, true)
		}
		if c.verboseLevel >= 3 {
			fmt.Fprintf(os.Stderr, "    [streaming body: %d bytes, %s]\n", contentLength, contentType)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, exitcode.Wrap(exitcode.General, fmt.Errorf("upload request failed: %w", err))
		}

		// Rate limited and rewindable — sleep, rewind on next iteration, retry.
		if resp.StatusCode == http.StatusTooManyRequests && seeker != nil && attempt+1 < maxAttempts {
			delay := parseRetryAfter(resp.Header.Get("Retry-After"), time.Second*time.Duration(1<<attempt))
			_ = resp.Body.Close()
			if c.verboseLevel >= 1 {
				fmt.Fprintf(os.Stderr, "<-- 429 Too Many Requests, retrying in %v\n", delay)
			}
			if err := sleepWithContext(ctx, delay); err != nil {
				return nil, err
			}
			continue
		}

		if c.verboseLevel >= 1 {
			fmt.Fprintf(os.Stderr, "<-- %d %s\n", resp.StatusCode, resp.Status)
		}
		if c.verboseLevel >= 2 {
			logHeaders(os.Stderr, resp.Header, false)
		}

		if resp.StatusCode >= 400 {
			respBody, _ := io.ReadAll(io.LimitReader(resp.Body, bodyLogLimit))
			_ = resp.Body.Close()
			if c.verboseLevel >= 3 {
				logBody(os.Stderr, respBody)
			}
			if resp.StatusCode == http.StatusTooManyRequests {
				return nil, exitcode.New(exitcode.RateLimited, fmt.Sprintf("upload rate limited (HTTP 429) after %d attempt(s): %s", attempt+1, string(respBody)))
			}
			return nil, exitcode.Wrap(exitcode.General, fmt.Errorf("upload failed (HTTP %d): %s", resp.StatusCode, string(respBody)))
		}

		if c.verboseLevel >= 3 {
			preview, _ := io.ReadAll(io.LimitReader(resp.Body, bodyLogLimit))
			resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(preview), resp.Body))
			logBody(os.Stderr, preview)
		}

		return resp, nil
	}

	return nil, exitcode.New(exitcode.RateLimited, fmt.Sprintf("upload rate limited: server returned HTTP 429 on all %d attempts", maxAttempts))
}

// parseRetryAfter returns the delay from a Retry-After header value.
// Supports the delta-seconds form only (Jamf uses integers). Falls back to
// the caller-supplied default when the header is missing or malformed.
func parseRetryAfter(header string, fallback time.Duration) time.Duration {
	if header == "" {
		return fallback
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	return fallback
}

func (c *Client) doWithRetry(ctx context.Context, req *http.Request, bodyData []byte) (*http.Response, error) {
	maxRetries := 3
	baseDelay := time.Second

	var lastErr error
	for i := range maxRetries {
		// Reset body for each attempt so retries send the full payload.
		if bodyData != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyData))
			req.ContentLength = int64(len(bodyData))
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if sleepErr := sleepWithContext(ctx, baseDelay*time.Duration(1<<i)); sleepErr != nil {
				return nil, sleepErr
			}
			continue
		}

		// Rate limited - respect Retry-After
		if resp.StatusCode == http.StatusTooManyRequests {
			delay := baseDelay * time.Duration(1<<i)
			if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
				if secs, err := strconv.Atoi(retryAfter); err == nil {
					delay = time.Duration(secs) * time.Second
				}
			}
			_ = resp.Body.Close()
			if sleepErr := sleepWithContext(ctx, delay); sleepErr != nil {
				return nil, sleepErr
			}
			continue
		}

		return resp, nil
	}

	if lastErr != nil {
		return nil, exitcode.Wrap(exitcode.General, fmt.Errorf("request failed after %d retries: %w", maxRetries, lastErr))
	}
	return nil, exitcode.New(exitcode.RateLimited, fmt.Sprintf("rate limited: request failed after %d retries. The server is throttling requests — wait a moment and try again.", maxRetries))
}

// ReadResponseBody reads the full body from an HTTP response with a 10 MB limit.
func ReadResponseBody(resp *http.Response) ([]byte, error) {
	return io.ReadAll(io.LimitReader(resp.Body, 10<<20))
}

// sleepWithContext blocks for the given duration or until the context is cancelled.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// bodyLogLimit is the maximum number of body bytes written to stderr at -vvv.
const bodyLogLimit = 64 << 10 // 64 KB

// BodyLogLimit is the truncation cap used by LogBody.
const BodyLogLimit = bodyLogLimit

// LogHeaders is the exported alias of logHeaders, callable from other packages
// that wrap HTTP transports (e.g. the Platform Gateway client wired through
// the SDK).
func LogHeaders(w io.Writer, h http.Header, redactAuth bool) {
	logHeaders(w, h, redactAuth)
}

// LogBody is the exported alias of logBody.
func LogBody(w io.Writer, data []byte) {
	logBody(w, data)
}

// tokenFieldRe matches JSON string values for sensitive auth fields so they can
// be replaced with "[REDACTED]" before logging. Handles access_token,
// refresh_token, and id_token — the fields returned by OAuth2 token endpoints.
var tokenFieldRe = regexp.MustCompile(`("(?:access_token|refresh_token|id_token)"\s*:\s*)"[^"]*"`)

// RedactTokenBody replaces OAuth2 token values in raw JSON with "[REDACTED]"
// so response bodies are safe to log at -vvv. Non-JSON data is returned as-is.
func RedactTokenBody(data []byte) []byte {
	if !bytes.Contains(data, []byte("access_token")) &&
		!bytes.Contains(data, []byte("refresh_token")) &&
		!bytes.Contains(data, []byte("id_token")) {
		return data
	}
	return tokenFieldRe.ReplaceAll(data, []byte(`$1"[REDACTED]"`))
}

// logHeaders prints HTTP headers to w in sorted order. When redactAuth is true,
// the Authorization header value is replaced with "[redacted]".
func logHeaders(w io.Writer, h http.Header, redactAuth bool) {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := strings.Join(h[k], ", ")
		if redactAuth && strings.EqualFold(k, "Authorization") {
			v = "[redacted]"
		}
		_, _ = fmt.Fprintf(w, "    %s: %s\n", k, v)
	}
}

// StatusError maps an HTTP error response to the same structured exit error Do
// would have returned. For callers that opted a status out of that mapping with
// registry.WithAllowedStatuses and then decided the response was a genuine
// failure after all — so the error text and hint stay in one place.
func StatusError(status int, method, path string, body []byte) error {
	return httpStatusError(status, method, path, body)
}

// httpStatusError maps an HTTP error response to a structured exit error with a
// short message and a separate remediation Hint (surfaced on stderr and in the
// JSON error envelope).
func httpStatusError(status int, method, path string, body []byte) error {
	switch status {
	case http.StatusUnauthorized:
		return exitcode.New(exitcode.Authentication,
			fmt.Sprintf("authentication failed (HTTP 401): %s", string(body))).
			WithHint("run 'jamf-cli config validate', or check JAMF_TOKEN / client credentials")
	case http.StatusForbidden:
		if note := edgeBlockedNote(body); note != "" {
			return exitcode.New(exitcode.PermissionDenied,
				"request blocked at the Jamf gateway edge (HTTP 403), before it reached Jamf").
				WithHint(note)
		}
		return exitcode.New(exitcode.PermissionDenied,
			fmt.Sprintf("permission denied (HTTP 403): %s", string(body))).
			WithHint(withGatewayUnservedNote(
				"the authenticated account lacks the required API privileges; check its API role",
				path, body))
	case http.StatusNotFound:
		return exitcode.New(exitcode.NotFound,
			fmt.Sprintf("resource not found (HTTP 404): %s %s", method, path)).
			WithHint(withGatewayUnservedNote(
				"run the matching 'list' command to see valid IDs/names",
				path, body))
	case http.StatusTooManyRequests:
		return exitcode.New(exitcode.RateLimited,
			"rate limited (HTTP 429): server is throttling requests").
			WithHint("retry shortly, or lower batch concurrency")
	default:
		return exitcode.Wrap(exitcode.General, fmt.Errorf("request failed (HTTP %d): %s", status, string(body)))
	}
}

// edgeBlockedNote recognises a CloudFront/WAF refusal and returns a hint for it,
// or "" when the body is not one.
//
// The GA gateway sits behind CloudFront, and its WAF refuses some requests before
// Jamf ever sees them. Left alone this surfaces as "permission denied (HTTP 403)"
// with a full HTML page dumped into the message and a hint telling the operator
// to check their API role — wrong twice over: the credential is fine, and no role
// change will help.
//
// The tell is the response body: an HTML error page carrying CloudFront's own
// wording. There is also a "Server: CloudFront" header, but it is not available
// here and the body is unambiguous, which keeps this a pure function.
//
// Known triggers, wire-established 2026-08-28 against EU:
//
//   - "file://" anywhere in the request body. Verified as a 2x2 where that
//     substring was the only variable. A legitimate value in Jamf Classic
//     payloads — a dock item's path is exactly where it belongs — so this
//     refuses real requests.
//   - .pkg upload content, matched inside the xar table of contents.
//   - A burst of writes, seemingly rate- or volume-based: 13 Classic creates
//     fired straight after ~440 requests were all refused, and none of them
//     reproduces in isolation.
//
// The hint names all of them and does NOT claim which one fired, because the
// response cannot say: the same page comes back for a content match and for a
// volume block, with no traceId and nothing identifying the rule. An earlier
// version of this asserted a specific trigger inferred from probes run inside a
// volume block, and the correlation was spurious.
//
// Deliberately no client-side workaround. Rewriting a caller-supplied body to
// dodge a WAF rule would be silent, lossy on a path where the content is
// meaningful, and would go on happening after the rule was fixed.
func edgeBlockedNote(body []byte) string {
	if !bytes.Contains(body, []byte("Request blocked")) &&
		!bytes.Contains(body, []byte("The request could not be satisfied")) {
		return ""
	}
	// The response is CloudFront's page, so it cannot say which rule fired —
	// name every known trigger and let the caller match on what they sent.
	return "This is the gateway's CDN/WAF, not Jamf and not your API privileges, so no role change will help. " +
		"Known triggers: \"file://\" anywhere in the request body (a legitimate value in some Classic payloads), .pkg upload content, and a burst of writes. " +
		"The response cannot say which one fired. There is no client-side fix — retry a single request cold, and report it to Jamf."
}

// withGatewayUnservedNote appends an explanation when a failure looks like the
// platform gateway declining to serve a Jamf Pro namespace it does not expose.
//
// App installers are the case this exists for: the surface is reachable only
// against a Jamf Pro instance directly, not through the gateway. The gateway's
// answer for a path it does not route is 403 BAD_PERMISSIONS or Tyk's bare
// "404 page not found", and neither says anything about the gateway — 403
// BAD_PERMISSIONS in particular is exactly what a real missing privilege looks
// like, so an operator reads it as "grant me the privilege" and goes looking for
// a role that will never help.
//
// The note is APPENDED rather than substituted, deliberately. Both signals can
// legitimately mean what they normally mean — a 404 for a deployment ID that
// really is gone, a 403 for a role that really is short a privilege — so the
// cost of a false positive has to be one extra sentence, never a confidently
// wrong exclusive answer. The path is already the rewritten gateway one by the
// time this is called, and "/pro/..." only exists in gateway mode, so no gateway
// flag needs threading down here.
func withGatewayUnservedNote(hint, path string, body []byte) string {
	if !strings.HasPrefix(path, "/pro/") {
		return hint
	}
	note := gatewayUnservedNote(path)
	if note == "" {
		return hint
	}
	// Tyk's unrouted 404 is a plain-text page, not a JSON envelope; a JSON body
	// means the request reached Jamf Pro and the 404 is about the resource.
	if bytes.Contains(body, []byte("404 page not found")) ||
		bytes.Contains(body, []byte("BAD_PERMISSIONS")) ||
		len(bytes.TrimSpace(body)) == 0 {
		return strings.TrimRight(hint, ". ") + ". " + note
	}
	return hint
}

// gatewayUnservedNote names the Jamf Pro namespaces the platform gateway does
// not expose, and what to use instead. Keyed on the gateway path prefix.
func gatewayUnservedNote(path string) string {
	if strings.HasPrefix(path, "/pro/v1/app-installers") {
		return "App installers are not exposed on the Jamf Platform gateway — they are reachable only against a Jamf Pro instance directly, so run `pro app-installer-*` commands with a profile whose url is your instance and whose auth-method is oauth2 or token."
	}
	return ""
}

// logBody prints body bytes to w indented by four spaces. Truncates at bodyLogLimit
// and notes when truncation occurs.
func logBody(w io.Writer, data []byte) {
	if len(data) == 0 {
		return
	}
	truncated := len(data) >= bodyLogLimit
	_, _ = fmt.Fprintf(w, "    %s\n", strings.ReplaceAll(strings.TrimRight(string(data), "\n"), "\n", "\n    "))
	if truncated {
		_, _ = fmt.Fprintf(w, "    [body truncated at %d bytes]\n", bodyLogLimit)
	}
}

// rewritePathForGateway maps an instance API path onto its Jamf Platform
// Gateway namespace.
//
//	/JSSResource/computers        → /proclassic/computers
//	/api/v1/accounts              → /pro/v1/accounts
//	/api/preview/computers        → /pro/preview/computers
//
// There is NO /api segment on the gateway. The GA gateway at
// {region}.api.jamfcloud.com mounts each namespace at the root and answers
// 404 "page not found" — the unknown-namespace tell — for anything under /api;
// the retired {region}.apigw.jamf.com required it. Wire-verified 2026-08-28
// against EU: /pro/v1/categories and /proclassic/categories both answered 200
// on the same credential in the same run where their /api forms answered 404.
// Dropped outright rather than selected per host, the same call as the tenant
// path→header migration below: a second code path nothing exercises is how
// that URL-shape bug went unnoticed for weeks.
//
// The tenant is NOT in the path. Until 2026-08-25 every gateway URL embedded it
// — /api/pro/{version}/tenant/{tenantID}/{resource} — and Tyk resolved the
// request context from `path`; `header` became an allowed source in prod on that
// date (tyk-gateway-management 0793131b, "JSC-73421 Enable header context
// support - Prod") and the published specs dropped the segment in GitOps build
// v1495 in favour of a required X-Tenant-Id header. Both forms answer during the
// transition window, so this follows the platform SDK onto headers only rather
// than keeping a second code path nothing exercises.
func rewritePathForGateway(path string) string {
	if after, ok := strings.CutPrefix(path, "/JSSResource/"); ok {
		return "/proclassic/" + after
	}
	if after, ok := strings.CutPrefix(path, "/JSSResource"); ok {
		return "/proclassic" + after
	}
	// Modern API: /api/v1/..., /api/v2/..., /api/preview/..., etc. The /api the
	// caller-facing path carries is the instance's, not the gateway's — it is
	// replaced by the namespace here, never prefixed onto it.
	if after, ok := strings.CutPrefix(path, "/api/"); ok {
		return "/pro/" + after
	}
	return path
}

// setScopeHeader stamps the gateway scope header on a request. Only in gateway
// mode, and only when there is one to send: a direct-to-instance request is not
// scoped at all, and an organization-scoped gateway credential carries its scope
// in the token rather than a header.
func (c *Client) setScopeHeader(req *http.Request) {
	if !c.gateway {
		return
	}
	if name, value := c.scope.Header(); name != "" {
		req.Header.Set(name, value)
	}
}
