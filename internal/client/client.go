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

	"github.com/Jamf-Concepts/jamfpro-cli/internal/auth"
	"github.com/Jamf-Concepts/jamfpro-cli/internal/exitcode"
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

// New creates a new Jamf Pro API client
func New(baseURL string, authProvider auth.Provider, opts ...Option) *Client {
	c := &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
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
	//   /api/v*        → /api/pro/tenant/{id}/v*
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
	req.Header.Set("User-Agent", "jamfpro-cli/1.0 (+https://github.com/Jamf-Concepts/jamfpro-cli)")

	// Classic API endpoints use XML; modern API uses JSON.
	isClassic := strings.HasPrefix(path, "/JSSResource") || strings.HasPrefix(path, "/api/proclassic")
	if isClassic {
		req.Header.Set("Accept", "application/xml")
		if bodyData != nil {
			req.Header.Set("Content-Type", "application/xml")
		}
	} else {
		req.Header.Set("Accept", "application/json")
		if bodyData != nil {
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
			return nil, exitcode.New(exitcode.Authentication, fmt.Sprintf("authentication failed (HTTP 401): %s\nCheck your credentials with: jamfpro-cli config validate", string(body)))
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

func (c *Client) doWithRetry(ctx context.Context, req *http.Request, bodyData []byte) (*http.Response, error) {
	maxRetries := 3
	baseDelay := time.Second

	var lastErr error
	for i := 0; i < maxRetries; i++ {
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
//	/api/v1/accounts              → /api/pro/tenant/{id}/v1/accounts
//	/api/preview/computers        → /api/pro/tenant/{id}/preview/computers
func rewritePathForGateway(path, tenantID string) string {
	if strings.HasPrefix(path, "/JSSResource/") {
		suffix := strings.TrimPrefix(path, "/JSSResource/")
		return "/api/proclassic/tenant/" + tenantID + "/" + suffix
	}
	if strings.HasPrefix(path, "/JSSResource") {
		suffix := strings.TrimPrefix(path, "/JSSResource")
		return "/api/proclassic/tenant/" + tenantID + suffix
	}
	// Modern API: /api/v1/..., /api/v2/..., /api/preview/..., etc.
	if strings.HasPrefix(path, "/api/") {
		suffix := strings.TrimPrefix(path, "/api/")
		return "/api/pro/tenant/" + tenantID + "/" + suffix
	}
	return path
}
