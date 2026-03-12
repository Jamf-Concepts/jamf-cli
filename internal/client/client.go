package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jamf/jamfpro-cli/internal/auth"
	"github.com/jamf/jamfpro-cli/internal/exitcode"
)

// Client is the HTTP client for Jamf Pro API
type Client struct {
	baseURL    string
	httpClient *http.Client
	auth       auth.Provider
	verbose    bool
}

// Option configures the client
type Option func(*Client)

// WithVerbose enables verbose logging
func WithVerbose(v bool) Option {
	return func(c *Client) {
		c.verbose = v
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
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	token, err := c.auth.GetToken(ctx)
	if err != nil {
		return nil, exitcode.Wrap(exitcode.Authentication, fmt.Errorf("getting auth token: %w", err))
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.verbose {
		fmt.Fprintf(os.Stderr, "--> %s %s\n", method, req.URL)
	}

	resp, err := c.doWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}

	if c.verbose {
		fmt.Fprintf(os.Stderr, "<-- %d %s\n", resp.StatusCode, resp.Status)
	}

	// Map HTTP error status codes to structured exit codes
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
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

func (c *Client) doWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	maxRetries := 3
	baseDelay := time.Second

	var lastErr error
	for i := 0; i < maxRetries; i++ {
		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(baseDelay * time.Duration(1<<i))
			continue
		}

		// Rate limited - respect Retry-After
		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := resp.Header.Get("Retry-After")
			delay := baseDelay * time.Duration(1<<i)
			if retryAfter != "" {
				if d, err := time.ParseDuration(retryAfter + "s"); err == nil {
					delay = d
				}
			}
			resp.Body.Close()
			time.Sleep(delay)
			continue
		}

		return resp, nil
	}

	if lastErr != nil {
		return nil, exitcode.Wrap(exitcode.General, fmt.Errorf("request failed after %d retries: %w", maxRetries, lastErr))
	}
	return nil, exitcode.New(exitcode.RateLimited, fmt.Sprintf("rate limited: request failed after %d retries. The server is throttling requests — wait a moment and try again.", maxRetries))
}
