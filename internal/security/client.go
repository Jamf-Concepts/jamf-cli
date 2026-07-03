// Copyright 2026, Jamf Software LLC

// Package security implements a client for the Jamf Security Cloud APIs
// (Risk, Device Lifecycle, and Shared Signals & Events). Unlike Jamf
// Pro/Protect/Platform, Security Cloud has no product Go SDK and uses its own
// auth model: a Basic-auth login exchange (POST /v1/login with
// base64(clientId:clientSecret)) returns a short-lived (15 minute) JWT bearer
// token — not an OAuth2 client-credentials grant. The Risk and Device
// Lifecycle APIs share one host (api.wandera.com, which also serves
// /v1/login); Shared Signals & Events lives on a separate host
// (sse.jamf.com) per its own OpenID SSE framework discovery document.
package security

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

const (
	// DefaultAPIBaseURL is the shared production host for the Risk and Device
	// Lifecycle APIs, and for the /v1/login token exchange used by all three.
	DefaultAPIBaseURL = "https://api.wandera.com"
	// DefaultSSEBaseURL is the production host for the Shared Signals & Events API.
	DefaultSSEBaseURL = "https://sse.jamf.com"

	// tokenRefreshMargin triggers a re-login this long before the cached
	// JWT's actual expiry (tokens are valid 15 minutes), so a request never
	// races a token that expires mid-flight.
	tokenRefreshMargin = 30 * time.Second
)

// Client is an authenticated HTTP client for the Jamf Security Cloud APIs.
type Client struct {
	httpClient   *http.Client
	userAgent    string
	apiBaseURL   string
	sseBaseURL   string
	clientID     string
	clientSecret string

	mu         sync.Mutex
	token      string
	customerID string
	expiresAt  time.Time
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient overrides the underlying *http.Client (default: http.DefaultClient).
func WithHTTPClient(c *http.Client) Option {
	return func(cl *Client) { cl.httpClient = c }
}

// WithUserAgent sets the User-Agent header sent with every request.
func WithUserAgent(ua string) Option {
	return func(cl *Client) { cl.userAgent = ua }
}

// WithAPIBaseURL overrides the Risk/Device Lifecycle/login host.
func WithAPIBaseURL(url string) Option {
	return func(cl *Client) {
		if url != "" {
			cl.apiBaseURL = url
		}
	}
}

// WithSSEBaseURL overrides the Shared Signals & Events host.
func WithSSEBaseURL(url string) Option {
	return func(cl *Client) {
		if url != "" {
			cl.sseBaseURL = url
		}
	}
}

// NewClient creates a Client for the given application ID and secret. Login
// is deferred until the first request; construction never makes a network call.
func NewClient(clientID, clientSecret string, opts ...Option) *Client {
	c := &Client{
		httpClient:   http.DefaultClient,
		apiBaseURL:   DefaultAPIBaseURL,
		sseBaseURL:   DefaultSSEBaseURL,
		clientID:     clientID,
		clientSecret: clientSecret,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// loginResponse mirrors the Risk/Device Lifecycle API's LoginResponse schema.
type loginResponse struct {
	Token string `json:"token"`
}

// jwtClaims decodes the subset of claims the CLI needs from the login JWT.
type jwtClaims struct {
	CustomerID string `json:"customer_id"`
	ExpiresAt  int64  `json:"exp"`
}

// decodeJWTClaims reads the unverified claims payload out of a JWT. The
// token is only ever presented back to Jamf as a bearer credential over
// TLS — the CLI trusts its own login response and only needs to read the
// expiry and customer_id it already carries, not verify the signature.
func decodeJWTClaims(token string) (jwtClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return jwtClaims{}, fmt.Errorf("malformed JWT: expected 3 segments, got %d", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return jwtClaims{}, fmt.Errorf("decoding JWT payload: %w", err)
	}
	var claims jwtClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return jwtClaims{}, fmt.Errorf("parsing JWT claims: %w", err)
	}
	return claims, nil
}

// login exchanges the client ID/secret for a fresh JWT via POST /v1/login and
// caches it along with its decoded expiry and customer_id. Callers must hold c.mu.
func (c *Client) login(ctx context.Context) error {
	auth := base64.StdEncoding.EncodeToString([]byte(c.clientID + ":" + c.clientSecret))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBaseURL+"/v1/login", nil)
	if err != nil {
		return fmt.Errorf("building login request: %w", err)
	}
	req.Header.Set("authorization", "Basic "+auth)
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("logging in to Jamf Security Cloud: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading login response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var lr loginResponse
	if err := json.Unmarshal(body, &lr); err != nil {
		return fmt.Errorf("parsing login response: %w", err)
	}
	if lr.Token == "" {
		return fmt.Errorf("login response contained no token")
	}

	claims, err := decodeJWTClaims(lr.Token)
	if err != nil {
		return fmt.Errorf("decoding login token: %w", err)
	}

	c.token = lr.Token
	c.customerID = claims.CustomerID
	c.expiresAt = time.Unix(claims.ExpiresAt, 0)
	return nil
}

// ensureToken returns a valid bearer token, logging in if none is cached or
// the cached one is within tokenRefreshMargin of expiring.
func (c *Client) ensureToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token == "" || time.Now().Add(tokenRefreshMargin).After(c.expiresAt) {
		if err := c.login(ctx); err != nil {
			return "", err
		}
	}
	return c.token, nil
}

// CustomerID returns the Jamf Security Cloud customer/account identifier
// embedded in the current JWT, logging in first if needed. Used to fill the
// {customerId} path parameter on Device Lifecycle endpoints without
// requiring the caller to supply it separately.
func (c *Client) CustomerID(ctx context.Context) (string, error) {
	if _, err := c.ensureToken(ctx); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.customerID, nil
}

// Do performs an authenticated request against the Risk / Device Lifecycle
// API host. Satisfies registry.HTTPClient.
func (c *Client) Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	return c.do(ctx, c.apiBaseURL, method, path, body)
}

// DoSSE performs an authenticated request against the Shared Signals &
// Events API host.
func (c *Client) DoSSE(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	return c.do(ctx, c.sseBaseURL, method, path, body)
}

func (c *Client) do(ctx context.Context, baseURL, method, path string, body io.Reader) (*http.Response, error) {
	token, err := c.ensureToken(ctx)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		return nil, httpStatusError(resp.StatusCode, method, path, respBody)
	}
	return resp, nil
}

// httpStatusError maps a non-2xx response to a structured exit code,
// mirroring the mapping internal/client uses for Jamf Pro. Error bodies vary
// across the three APIs (Spring Boot default {status,error,path}, the Risk
// API's {error,message}, the SSE API's {code,message}) — rather than parse
// each shape, the raw body is surfaced verbatim; it's short and human-readable
// in all three cases.
func httpStatusError(status int, method, path string, body []byte) error {
	msg := strings.TrimSpace(string(body))
	switch status {
	case http.StatusUnauthorized:
		return exitcode.New(exitcode.Authentication,
			fmt.Sprintf("authentication failed (HTTP 401): %s", msg)).
			WithHint("the JWT may have expired mid-request, or the client ID/secret lack access to this API; run 'jamf-cli security setup' or check JAMFSECURITY_CLIENT_ID/JAMFSECURITY_CLIENT_SECRET")
	case http.StatusForbidden:
		return exitcode.New(exitcode.PermissionDenied,
			fmt.Sprintf("permission denied (HTTP 403): %s", msg)).
			WithHint("this application ID's Security Integration may not be scoped for this API — check Settings > Security Integrations in the Jamf Security Cloud (Radar) portal")
	case http.StatusNotFound:
		return exitcode.New(exitcode.NotFound,
			fmt.Sprintf("resource not found (HTTP 404): %s %s", method, path))
	case http.StatusTooManyRequests:
		return exitcode.New(exitcode.RateLimited,
			fmt.Sprintf("rate limited (HTTP 429): %s", msg)).
			WithHint("Jamf Security Cloud APIs are limited to 5 req/sec and 10,000 req/day per integration; retry shortly")
	default:
		return exitcode.Wrap(exitcode.General, fmt.Errorf("request failed (HTTP %d): %s", status, msg))
	}
}
