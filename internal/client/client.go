// Copyright 2026, Jamf Software LLC

package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/gateway"
	"github.com/Jamf-Concepts/jamf-cli/internal/httptransport"
	"github.com/Jamf-Concepts/jamf-cli/internal/privileges"
	"github.com/Jamf-Concepts/jamf-cli/internal/redact"
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
		fmt.Fprintf(os.Stderr, "--> %s %s\n", method, redact.URL(req.URL))
	}

	resp, err := c.doWithRetry(ctx, req, bodyData)
	if err != nil {
		return nil, err
	}

	if c.verboseLevel >= 1 {
		fmt.Fprintf(os.Stderr, "<-- %s %s\n", resp.Proto, resp.Status)
	}
	if c.verboseLevel >= 2 {
		logHeaders(os.Stderr, responseWireHeaders(resp), false)
	}

	// Map HTTP error status codes to structured exit codes, unless the caller
	// declared this status a documented result (registry.WithAllowedStatuses) —
	// then the response is returned with its body intact for the caller to render.
	if resp.StatusCode >= 400 && !registry.StatusAllowed(ctx, resp.StatusCode) {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, bodyLogLimit))
		_ = resp.Body.Close()
		if c.verboseLevel >= 3 {
			logBody(os.Stderr, redact.Body(body))
		}
		return nil, httpStatusError(resp.StatusCode, method, path, body)
	}

	if c.verboseLevel >= 3 {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, bodyLogLimit))
		resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(preview), resp.Body))
		logBody(os.Stderr, redact.Body(preview))
	}

	return resp, nil
}

// Upload executes a streaming HTTP request with a caller-specified Content-Type
// and Content-Length. The body is never buffered, so multi-GB files stream
// straight through.
//
// On the platform gateway the request goes out over HTTP/1.1 as a chunked body
// with no declared Content-Length, and contentLength is only reported in
// verbose output. Both halves are measured against the GA gateway, as the
// jamfplatform-go-sdk does for its own uploads (v1.3.0): CloudFront advertises a
// 64 KiB per-stream HTTP/2 window, so one h2 upload is capped near 4 MiB/s where
// HTTP/1.1 runs at the uplink, and a declared length was refused with a 502 at
// about 1.04 GiB where the same 1.43 GiB file uploaded whole without one. A
// direct instance connection is unmeasured and keeps h2 and the declared length.
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

	httpClient := c.httpClient
	wireLength := contentLength
	if c.gateway {
		httpClient, wireLength = c.gatewayUploadClient(), -1
		if httpClient != c.httpClient {
			defer httpClient.CloseIdleConnections()
		}
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
		// -1 declares the length unknown, so net/http chunks without probing
		// the body first.
		req.ContentLength = wireLength

		if c.verboseLevel >= 1 {
			framing := fmt.Sprintf("%d bytes", contentLength)
			if wireLength < 0 {
				framing += ", chunked"
			}
			if maxAttempts > 1 {
				fmt.Fprintf(os.Stderr, "--> POST %s (%s, attempt %d/%d)\n", redact.URL(req.URL), framing, attempt+1, maxAttempts)
			} else {
				fmt.Fprintf(os.Stderr, "--> POST %s (%s)\n", redact.URL(req.URL), framing)
			}
		}
		reqCtx := ctx
		if c.verboseLevel >= 2 {
			reqCtx = traceWireHeaders(ctx, os.Stderr, func() {
				if c.verboseLevel >= 3 {
					fmt.Fprintf(os.Stderr, "    [streaming body: %d bytes, %s]\n", contentLength, contentType)
				}
			})
		}

		resp, err := httpClient.Do(req.WithContext(reqCtx))
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
			fmt.Fprintf(os.Stderr, "<-- %s %s\n", resp.Proto, resp.Status)
		}
		if c.verboseLevel >= 2 {
			logHeaders(os.Stderr, responseWireHeaders(resp), false)
		}

		if resp.StatusCode >= 400 {
			respBody, _ := io.ReadAll(io.LimitReader(resp.Body, bodyLogLimit))
			_ = resp.Body.Close()
			if c.verboseLevel >= 3 {
				logBody(os.Stderr, redact.Body(respBody))
			}
			if resp.StatusCode == http.StatusTooManyRequests {
				return nil, exitcode.New(exitcode.RateLimited, fmt.Sprintf("upload rate limited (HTTP 429) after %d attempt(s): %s", attempt+1, redact.Body(respBody)))
			}
			return nil, exitcode.Wrap(exitcode.General, fmt.Errorf("upload failed (HTTP %d): %s", resp.StatusCode, redact.Body(respBody)))
		}

		if c.verboseLevel >= 3 {
			preview, _ := io.ReadAll(io.LimitReader(resp.Body, bodyLogLimit))
			resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(preview), resp.Body))
			logBody(os.Stderr, redact.Body(preview))
		}

		return resp, nil
	}

	return nil, exitcode.New(exitcode.RateLimited, fmt.Sprintf("upload rate limited: server returned HTTP 429 on all %d attempts", maxAttempts))
}

// gatewayUploadClient returns an HTTP client for one gateway upload: the
// client's own, with its transport pinned to HTTP/1.1 and its cookie jar kept.
// A caller-supplied transport that is not an *http.Transport is left alone,
// since it is not ours to rewrite.
func (c *Client) gatewayUploadClient() *http.Client {
	t, ok := c.httpClient.Transport.(*http.Transport)
	if !ok {
		return c.httpClient
	}
	return &http.Client{
		Transport:     httptransport.HTTP1Only(t),
		Jar:           c.httpClient.Jar,
		CheckRedirect: c.httpClient.CheckRedirect,
		Timeout:       c.httpClient.Timeout,
	}
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

// retrySafeMethods are the methods whose request may be sent again after it
// reached the server. Deliberately narrower than HTTP's idempotent set: a PUT
// that timed out may still be running — a smart group update recalculates
// membership inside the request — and a second one lands on top of it.
var retrySafeMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
}

// doWithRetry sends req, retrying a 429 and a transport failure that is safe
// to retry.
//
// Which transport failures are safe depends on whether the request reached the
// server, which WroteHeaders reports:
//   - never sent (dial, TLS, a dead pooled connection): retried whatever the
//     method, since nothing reached the server.
//   - sent, then timed out waiting for the response: never retried. The server
//     is still working on it, and the identical request will time out the same
//     way — the old policy re-sent it three times, so a 2000-row inventory
//     page cost three full timeouts before the error (issue 392). A
//     paginated walk shrinks its page instead (registry.ShrinkPageSize).
//   - sent, then failed otherwise (connection reset): retried for GET, HEAD
//     and OPTIONS only. A write may already have been applied.
func (c *Client) doWithRetry(ctx context.Context, req *http.Request, bodyData []byte) (*http.Response, error) {
	maxRetries := 3
	baseDelay := time.Second

	var lastErr error
	for i := range maxRetries {
		var sent atomic.Bool
		attemptCtx := httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
			WroteHeaders: func() { sent.Store(true) },
		})
		if c.verboseLevel >= 2 {
			attemptCtx = traceWireHeaders(attemptCtx, os.Stderr, func() {
				if c.verboseLevel >= 3 {
					logBody(os.Stderr, redact.Body(bodyData))
				}
			})
		}
		attempt := req.WithContext(attemptCtx)
		// Reset body for each attempt so retries send the full payload.
		if bodyData != nil {
			attempt.Body = io.NopCloser(bytes.NewReader(bodyData))
			attempt.ContentLength = int64(len(bodyData))
		}

		resp, err := c.httpClient.Do(attempt)
		if err != nil {
			if ctx.Err() != nil {
				return nil, exitcode.Wrap(exitcode.General, err)
			}
			if sent.Load() {
				if isTimeout(err) {
					return nil, serverTimeoutError(req.Method, err)
				}
				if !retrySafeMethods[req.Method] {
					return nil, exitcode.Wrap(exitcode.General, fmt.Errorf("%s request failed after it was sent: %w", req.Method, err)).
						WithHint(mayHaveAppliedHint)
				}
			}
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

// mayHaveAppliedHint is the remedy for a write that failed after it was sent:
// the CLI cannot tell whether the server applied it, and re-running blind is
// how one command creates two objects.
const mayHaveAppliedHint = "the request reached the server and may have been applied — check the object's current state before retrying"

// isTimeout reports whether err is a timeout: the response-header timeout on
// the tuned transport, in either protocol ("net/http: timeout awaiting
// response headers", "http2: timeout awaiting response headers"). Both
// surface as a net.Error inside the *url.Error http.Client.Do returns.
func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// serverTimeoutError reports a request the server did not answer in time,
// wrapping registry.ErrServerTimeout so a paginated walk can recognise it and
// ask for a smaller page.
func serverTimeoutError(method string, cause error) *exitcode.Error {
	e := exitcode.Wrap(exitcode.General, fmt.Errorf("%w: %w", registry.ErrServerTimeout, cause))
	if !retrySafeMethods[method] {
		return e.WithHint(mayHaveAppliedHint)
	}
	return e.WithHint("the request asked for more than the server could assemble in time; for a list, pass a smaller --page-size or narrow it with --section or --filter")
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

// TraceWireHeaders and ResponseWireHeaders are the exported aliases of
// traceWireHeaders and responseWireHeaders, for the same wrapped transports.
func TraceWireHeaders(ctx context.Context, w io.Writer, afterHeaders func()) context.Context {
	return traceWireHeaders(ctx, w, afterHeaders)
}

// ResponseWireHeaders is responseWireHeaders.
func ResponseWireHeaders(resp *http.Response) http.Header {
	return responseWireHeaders(resp)
}

// LogBody is the exported alias of logBody.
func LogBody(w io.Writer, data []byte) {
	logBody(w, data)
}

// RedactTokenBody, RedactCredentialBody and RedactBodyForLog are one rule,
// redact.Body, under the names this package's callers already use.
func RedactTokenBody(data []byte) []byte { return redact.Body(data) }

// RedactCredentialBody is redact.Body.
func RedactCredentialBody(data []byte) []byte { return redact.Body(data) }

// RedactBodyForLog is redact.Body, for transports wrapped outside this package
// (the Platform Gateway client wired through the SDK).
func RedactBodyForLog(data []byte) []byte { return redact.Body(data) }

// logHeaders prints HTTP headers to w in sorted order. A Cookie or Set-Cookie
// value is always replaced with "[redacted]", since a session cookie
// authenticates the same as the token, and so is Authorization when redactAuth
// is true.
func logHeaders(w io.Writer, h http.Header, redactAuth bool) {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		_, _ = fmt.Fprintf(w, "    %s: %s\n", k, headerLogValue(k, strings.Join(h[k], ", "), redactAuth))
	}
}

// headerLogValue is v, or "[redacted]" where k carries a credential.
func headerLogValue(k, v string, redactAuth bool) string {
	if redactAuth && strings.EqualFold(k, "Authorization") || strings.EqualFold(k, "Cookie") || strings.EqualFold(k, "Set-Cookie") {
		return "[redacted]"
	}
	return v
}

// traceWireHeaders returns ctx with a trace that prints every request header as
// the transport writes it, in wire order, then calls afterHeaders.
//
// req.Header is not the wire: net/http adds Host, Content-Length or
// Transfer-Encoding: chunked, and Accept-Encoding itself, and HTTP/2 reports its
// pseudo-headers (:authority, :method, ...), so a dump of the map shows none of
// the framing or protocol a request actually went out with. A trace sees what
// was written. Authorization and cookies are redacted as in logHeaders.
func traceWireHeaders(ctx context.Context, w io.Writer, afterHeaders func()) context.Context {
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		WroteHeaderField: func(key string, value []string) {
			_, _ = fmt.Fprintf(w, "    %s: %s\n", key, headerLogValue(key, strings.Join(value, ", "), true))
		},
		WroteHeaders: afterHeaders,
	})
}

// responseWireHeaders is resp.Header plus the framing net/http lifts out of it:
// a chunked response's Transfer-Encoding is moved to resp.TransferEncoding.
func responseWireHeaders(resp *http.Response) http.Header {
	h := resp.Header.Clone()
	if h == nil {
		h = http.Header{}
	}
	if len(resp.TransferEncoding) > 0 && h.Get("Transfer-Encoding") == "" {
		h.Set("Transfer-Encoding", strings.Join(resp.TransferEncoding, ", "))
	}
	return h
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
			fmt.Sprintf("authentication failed (HTTP 401): %s", redact.Body(body))).
			WithHint("run 'jamf-cli config validate', or check JAMF_TOKEN / client credentials")
	case http.StatusForbidden:
		if note := edgeBlockedNote(body); note != "" {
			return exitcode.New(exitcode.PermissionDenied,
				"request blocked at the Jamf gateway edge (HTTP 403), before it reached Jamf").
				WithHint(note)
		}
		return exitcode.New(exitcode.PermissionDenied,
			fmt.Sprintf("permission denied (HTTP 403): %s", redact.Body(body))).
			WithHint(withGatewayUnservedNote(forbiddenHint(method, path), method, path, body))
	case http.StatusNotFound:
		return exitcode.New(exitcode.NotFound,
			fmt.Sprintf("resource not found (HTTP 404): %s %s", method, path)).
			WithHint(withGatewayUnservedNote(
				"run the matching 'list' command to see valid IDs/names",
				method, path, body))
	case http.StatusTooManyRequests:
		return exitcode.New(exitcode.RateLimited,
			"rate limited (HTTP 429): server is throttling requests").
			WithHint("retry shortly, or lower batch concurrency")
	case http.StatusGatewayTimeout:
		// The edge gave up waiting for Jamf Pro — CloudFront in front of the
		// platform gateway does so at 90s. The body is the edge's own HTML
		// page and says nothing about the request, so it is not repeated.
		return serverTimeoutError(method, fmt.Errorf("gateway timeout (HTTP 504) on %s %s", method, path))
	default:
		if reason := classicHTMLErrorReason(body); reason != "" {
			return exitcode.New(exitcode.General,
				fmt.Sprintf("request failed (HTTP %d): %s", status, reason))
		}
		return exitcode.Wrap(exitcode.General, fmt.Errorf("request failed (HTTP %d): %s", status, redact.Body(body)))
	}
}

// classicHTMLErrorReason pulls the one useful sentence out of a Classic API
// error, or returns "" when the body is not one of its HTML status pages.
//
// The Classic API reports a refused write as an HTML page rather than JSON,
// and the reason it carries is specific and actionable — "Error: Unable to
// match computer group", "Error: Mobile device groups cannot be assigned to an
// macOS profile", "Unable to update the database". Passing the page through
// verbatim buried that in ~400 bytes of markup and inline CSS, which in a JSON
// error envelope arrives as one escaped line; every scope and classic-write
// failure read as a wall of noise with the answer in the middle of it.
//
// The page's shape is stable: a styled <p> carrying the HTTP status name, a
// bare <p> carrying the reason, then a bare <p> of boilerplate links. So the
// attribute-less paragraphs are the candidates, minus the boilerplate. When
// nothing matches, the caller falls back to the raw body rather than swallowing
// a page this does not recognise.
func classicHTMLErrorReason(body []byte) string {
	if !bytes.Contains(body, []byte("<title>Status page</title>")) {
		return ""
	}
	var reasons []string
	for _, m := range bareParagraphRe.FindAllSubmatch(body, -1) {
		text := strings.TrimSpace(stripTagsRe.ReplaceAllString(string(m[1]), ""))
		text = strings.Join(strings.Fields(text), " ")
		if text == "" || strings.Contains(text, "technical details") || strings.Contains(text, "continue your visit") {
			continue
		}
		reasons = append(reasons, text)
	}
	return strings.Join(reasons, "; ")
}

// bareParagraphRe matches a <p> with no attributes — the Classic status page
// styles its status-name paragraph and leaves the reason unstyled.
var bareParagraphRe = regexp.MustCompile(`(?s)<p>(.*?)</p>`)

// stripTagsRe removes the <a> and <br> markup the boilerplate paragraph holds.
var stripTagsRe = regexp.MustCompile(`<[^>]*>`)

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

// isGatewayPath reports whether a path is in gateway form. The rewrite to
// /pro/ or /proclassic/ happens before a request is sent and those prefixes
// exist only in gateway mode, so this is also the answer to "were these
// credentials a platform gateway credential" — no flag needs threading down
// here.
func isGatewayPath(path string) bool {
	return strings.HasPrefix(path, "/pro/") || strings.HasPrefix(path, "/proclassic/")
}

// forbiddenHint names the permission a 403 wanted, in the vocabulary that
// matches the credential that sent the request.
//
// The two are not interchangeable and neither is derivable from the other. A
// Jamf Pro instance enforces API-role privileges ("Read Categories"), granted in
// Jamf Pro; the gateway enforces GA capability permissions (categories:read),
// granted in Jamf Account when the API integration is created, and the GA
// consolidation folded several privileges into one capability. So a gateway 403
// answered with Jamf Pro privilege names sends the operator to a console where
// the grant it names does not exist — which is the same class of wrong answer as
// the unrouted-endpoint 403 that gatewayUnservedNote exists for.
//
// The command's own jamf:privileges annotation is deliberately not used here:
// it is the Jamf Pro vocabulary for a Pro command, and a hand-written command
// fanning out over many endpoints carries one annotation for the whole batch,
// where only the request knows which endpoint was refused.
func forbiddenHint(method, path string) string {
	if !isGatewayPath(path) {
		return "the authenticated account lacks the required API privileges; check its API role"
	}
	if hint := privileges.Hint(gateway.Scopes(method, path)); hint != "" {
		return hint
	}
	return privileges.GatewayFallbackHint()
}

// withGatewayUnservedNote appends an explanation when a failure looks like the
// platform gateway declining to serve a Jamf Pro or Classic namespace it does
// not expose.
//
// The gateway's answer for a path it does not route is 403 BAD_PERMISSIONS or
// Tyk's bare "404 page not found", and neither says anything about the gateway
// — 403 BAD_PERMISSIONS in particular is exactly what a real missing privilege
// looks like, so an operator reads it as "grant me the privilege" and goes
// looking for a role that will never help.
//
// This is the response-side half of the pair. The other half refuses an
// Unserved command before it is sent (checkGatewayCoverage in
// internal/commands/root.go), which is the better error because it names the
// command rather than the URL. This half still matters for two reasons: the
// Undeclared level is deliberately not refused, and a hand-written command that
// fans out over many endpoints — pro overview makes ~41 calls, pro backup and
// pro diff more — carries one command annotation for a whole batch, so only the
// request itself knows which endpoint was refused.
//
// The note is APPENDED rather than substituted, deliberately. Both signals can
// legitimately mean what they normally mean — a 404 for a deployment ID that
// really is gone, a 403 for a role that really is short a privilege — so the
// cost of a false positive has to be one extra sentence, never a confidently
// wrong exclusive answer. The path is already the rewritten gateway one by the
// time this is called, and /pro/ and /proclassic/ only exist in gateway mode, so
// no gateway flag needs threading down here.
func withGatewayUnservedNote(hint, method, path string, body []byte) string {
	if !isGatewayPath(path) {
		return hint
	}
	note := gateway.NoteForRequest(method, path)
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
