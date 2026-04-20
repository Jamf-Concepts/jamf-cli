// Copyright 2026, Jamf Software LLC

package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
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
	baseURL    string
	httpClient *http.Client
	auth       auth.Provider
	verbose    bool
	tenantID   string // non-empty when using platform gateway auth
}

// Option configures the client
type Option func(*Client)

// WithVerbose enables verbose logging
func WithVerbose(v bool) Option {
	return func(c *Client) {
		c.verbose = v
	}
}

// WithTenantID enables platform gateway mode, where API paths are rewritten
// to include the tenant identifier for routing through the Jamf Platform Gateway.
func WithTenantID(id string) Option {
	return func(c *Client) {
		c.tenantID = id
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

	// Platform gateway mode: rewrite paths to include tenant routing.
	//   /JSSResource/* → /api/proclassic/tenant/{id}/*
	//   /api/v*        → /api/pro/v*/tenant/{id}/*
	if c.tenantID != "" {
		path = rewritePathForGateway(path, c.tenantID)
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

	// Classic API endpoints use XML; modern API uses JSON.
	// An explicit Accept override in the context takes precedence (used by binary download commands).
	isClassic := strings.HasPrefix(path, "/JSSResource") || strings.HasPrefix(path, "/api/proclassic")
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

	if c.verbose {
		fmt.Fprintf(os.Stderr, "--> %s %s\n", method, req.URL)
	}

	resp, err := c.doWithRetry(ctx, req, bodyData)
	if err != nil {
		return nil, err
	}

	if c.verbose {
		fmt.Fprintf(os.Stderr, "<-- %d %s\n", resp.StatusCode, resp.Status)
	}

	// Map HTTP error status codes to structured exit codes
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		_ = resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return nil, exitcode.New(exitcode.Authentication, fmt.Sprintf("authentication failed (HTTP 401): %s\nCheck your credentials with: jamf-cli config validate", string(body)))
		case http.StatusForbidden:
			return nil, exitcode.New(exitcode.PermissionDenied, fmt.Sprintf("permission denied (HTTP 403): %s\nThe authenticated account lacks the required API privileges.", string(body)))
		case http.StatusNotFound:
			return nil, exitcode.New(exitcode.NotFound, fmt.Sprintf("resource not found (HTTP 404): %s %s", method, path))
		case http.StatusTooManyRequests:
			return nil, exitcode.New(exitcode.RateLimited, "rate limited (HTTP 429): server is throttling requests, wait a moment and try again")
		default:
			return nil, exitcode.Wrap(exitcode.General, fmt.Errorf("request failed (HTTP %d): %s", resp.StatusCode, string(body)))
		}
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
	if c.tenantID != "" {
		path = rewritePathForGateway(path, c.tenantID)
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
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Accept", "application/json")
		req.ContentLength = contentLength

		if c.verbose {
			if maxAttempts > 1 {
				fmt.Fprintf(os.Stderr, "--> POST %s (%d bytes, attempt %d/%d)\n", req.URL, contentLength, attempt+1, maxAttempts)
			} else {
				fmt.Fprintf(os.Stderr, "--> POST %s (%d bytes)\n", req.URL, contentLength)
			}
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, exitcode.Wrap(exitcode.General, fmt.Errorf("upload request failed: %w", err))
		}

		// Rate limited and rewindable — sleep, rewind on next iteration, retry.
		if resp.StatusCode == http.StatusTooManyRequests && seeker != nil && attempt+1 < maxAttempts {
			delay := parseRetryAfter(resp.Header.Get("Retry-After"), time.Second*time.Duration(1<<attempt))
			_ = resp.Body.Close()
			if c.verbose {
				fmt.Fprintf(os.Stderr, "<-- 429 Too Many Requests, retrying in %v\n", delay)
			}
			if err := sleepWithContext(ctx, delay); err != nil {
				return nil, err
			}
			continue
		}

		if c.verbose {
			fmt.Fprintf(os.Stderr, "<-- %d %s\n", resp.StatusCode, resp.Status)
		}

		if resp.StatusCode >= 400 {
			respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusTooManyRequests {
				return nil, exitcode.New(exitcode.RateLimited, fmt.Sprintf("upload rate limited (HTTP 429) after %d attempt(s): %s", attempt+1, string(respBody)))
			}
			return nil, exitcode.Wrap(exitcode.General, fmt.Errorf("upload failed (HTTP %d): %s", resp.StatusCode, string(respBody)))
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

// rewritePathForGateway transforms an API path for the Jamf Platform Gateway.
//
//	/JSSResource/computers        → /api/proclassic/tenant/{id}/computers
//	/api/v1/accounts              → /api/pro/v1/tenant/{id}/accounts
//	/api/preview/computers        → /api/pro/preview/tenant/{id}/computers
func rewritePathForGateway(path, tenantID string) string {
	if after, ok := strings.CutPrefix(path, "/JSSResource/"); ok {
		suffix := after
		return "/api/proclassic/tenant/" + tenantID + "/" + suffix
	}
	if after, ok := strings.CutPrefix(path, "/JSSResource"); ok {
		suffix := after
		return "/api/proclassic/tenant/" + tenantID + suffix
	}
	// Modern API: /api/v1/..., /api/v2/..., /api/preview/..., etc.
	// Version segment goes before /tenant/{id} to match the Platform SDK convention:
	//   /api/{namespace}/{version}/tenant/{tenantID}/{resource}
	if after, ok := strings.CutPrefix(path, "/api/"); ok {
		// after = "v1/accounts" → version = "v1", rest = "accounts"
		version, rest, _ := strings.Cut(after, "/")
		if rest == "" {
			return "/api/pro/" + version + "/tenant/" + tenantID
		}
		return "/api/pro/" + version + "/tenant/" + tenantID + "/" + rest
	}
	return path
}
