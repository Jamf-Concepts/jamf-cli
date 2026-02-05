package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ktn-jamf/jamfpro-cli/internal/auth"
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
	// Ensure path has /api prefix (OpenAPI paths omit it)
	if !strings.HasPrefix(path, "/api") {
		path = "/api" + path
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	token, err := c.auth.GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting auth token: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.verbose {
		fmt.Printf("--> %s %s\n", method, req.URL)
	}

	resp, err := c.doWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}

	if c.verbose {
		fmt.Printf("<-- %d %s\n", resp.StatusCode, resp.Status)
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

	return nil, fmt.Errorf("request failed after %d retries: %w", maxRetries, lastErr)
}
