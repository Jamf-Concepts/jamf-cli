// Copyright 2026, Jamf Software LLC

// Package security implements a client for the Jamf Security Cloud APIs
// (Risk, Device Lifecycle, and Shared Signals & Events). Unlike Jamf
// Pro/Protect/Platform, Security Cloud has no product Go SDK and uses its own
// auth model: a Basic-auth login exchange (POST /v1/login with
// base64(clientId:clientSecret)) returns a short-lived (15 minute) JWT bearer
// token — not an OAuth2 client-credentials grant. Critically, each of the
// three APIs is provisioned as its own "Security Integration" in the Radar
// portal with its own application ID/secret pair, and the resulting JWT is
// scoped to exactly one API via its `aud` claim (RISK_API,
// DEVICE_LIFECYCLE_API, ...) — a Risk credential cannot mint a token that
// Device Lifecycle or SSE will accept. So this client tracks three
// independent credential pairs and token caches, one per scope, any subset
// of which may be configured. The Risk and Device Lifecycle APIs share one
// host (api.wandera.com, which also serves /v1/login); Shared Signals &
// Events lives on a separate host (sse.jamf.com) per its own OpenID SSE
// framework discovery document.
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

// tokenSource holds one scope's credentials and cached JWT. A zero-value
// tokenSource (empty clientID/clientSecret) means that scope isn't configured.
type tokenSource struct {
	label        string // human-readable name for error messages, e.g. "Risk"
	clientID     string
	clientSecret string

	mu         sync.Mutex
	token      string
	customerID string
	expiresAt  time.Time
}

func (t *tokenSource) configured() bool {
	return t.clientID != "" && t.clientSecret != ""
}

// Client is an authenticated HTTP client for the Jamf Security Cloud APIs.
type Client struct {
	httpClient *http.Client
	userAgent  string
	apiBaseURL string
	sseBaseURL string

	risk      tokenSource
	lifecycle tokenSource
	sse       tokenSource
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

// WithRiskCredentials configures the Risk API application ID/secret. Omit
// (or pass empty strings) to leave the Risk API unconfigured — Risk calls
// then fail with a clear "not configured" error instead of a login failure.
func WithRiskCredentials(clientID, clientSecret string) Option {
	return func(cl *Client) {
		cl.risk.clientID = clientID
		cl.risk.clientSecret = clientSecret
	}
}

// WithLifecycleCredentials configures the Device Lifecycle API application ID/secret.
func WithLifecycleCredentials(clientID, clientSecret string) Option {
	return func(cl *Client) {
		cl.lifecycle.clientID = clientID
		cl.lifecycle.clientSecret = clientSecret
	}
}

// WithSSECredentials configures the Shared Signals & Events application ID/secret.
func WithSSECredentials(clientID, clientSecret string) Option {
	return func(cl *Client) {
		cl.sse.clientID = clientID
		cl.sse.clientSecret = clientSecret
	}
}

// NewClient creates a Client. Any of the three scopes' credentials may be
// left unconfigured via the With*Credentials options; login is deferred
// until the first request for that scope, so construction never makes a
// network call.
func NewClient(opts ...Option) *Client {
	c := &Client{
		httpClient: http.DefaultClient,
		apiBaseURL: DefaultAPIBaseURL,
		sseBaseURL: DefaultSSEBaseURL,
	}
	c.risk.label = "Risk"
	c.lifecycle.label = "Device Lifecycle"
	c.sse.label = "Shared Signals & Events"
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// HasRiskCredentials reports whether Risk API credentials were configured.
func (c *Client) HasRiskCredentials() bool { return c.risk.configured() }

// HasLifecycleCredentials reports whether Device Lifecycle API credentials were configured.
func (c *Client) HasLifecycleCredentials() bool { return c.lifecycle.configured() }

// HasSSECredentials reports whether Shared Signals & Events credentials were configured.
func (c *Client) HasSSECredentials() bool { return c.sse.configured() }

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

// login exchanges ts's client ID/secret for a fresh JWT via POST /v1/login
// and caches it along with its decoded expiry and customer_id. The login
// endpoint always lives on the main API host, even for the SSE scope (SSE's
// data-plane host is sse.jamf.com, but there is no separate SSE login
// endpoint in the spec). Callers must hold ts.mu.
func (c *Client) login(ctx context.Context, ts *tokenSource) error {
	auth := base64.StdEncoding.EncodeToString([]byte(ts.clientID + ":" + ts.clientSecret))
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
		return fmt.Errorf("logging in to Jamf Security Cloud (%s): %w", ts.label, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading login response (%s): %w", ts.label, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s API login failed: HTTP %d: %s", ts.label, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var lr loginResponse
	if err := json.Unmarshal(body, &lr); err != nil {
		return fmt.Errorf("parsing login response (%s): %w", ts.label, err)
	}
	if lr.Token == "" {
		return fmt.Errorf("%s API login response contained no token", ts.label)
	}

	claims, err := decodeJWTClaims(lr.Token)
	if err != nil {
		return fmt.Errorf("decoding %s API login token: %w", ts.label, err)
	}

	ts.token = lr.Token
	ts.customerID = claims.CustomerID
	ts.expiresAt = time.Unix(claims.ExpiresAt, 0)
	return nil
}

// ensureToken returns a valid bearer token for ts, logging in if none is
// cached or the cached one is within tokenRefreshMargin of expiring.
func (c *Client) ensureToken(ctx context.Context, ts *tokenSource) (string, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if !ts.configured() {
		return "", fmt.Errorf("%s API credentials are not configured; run 'jamf-cli security setup'", ts.label)
	}
	if ts.token == "" || time.Now().Add(tokenRefreshMargin).After(ts.expiresAt) {
		if err := c.login(ctx, ts); err != nil {
			return "", err
		}
	}
	return ts.token, nil
}

// LifecycleCustomerID returns the Jamf Security Cloud customer/account
// identifier embedded in the Device Lifecycle JWT, logging in first if
// needed. Used to fill the {customerId} path parameter on Device Lifecycle
// endpoints without requiring the caller to supply it separately.
func (c *Client) LifecycleCustomerID(ctx context.Context) (string, error) {
	if _, err := c.ensureToken(ctx, &c.lifecycle); err != nil {
		return "", err
	}
	c.lifecycle.mu.Lock()
	defer c.lifecycle.mu.Unlock()
	return c.lifecycle.customerID, nil
}

// DoExpectRisk performs an authenticated JSON request against the Risk API.
// Mirrors jamfplatform-go-sdk's Transport.DoExpect signature so generated
// commands (generator/security) can share the same template shape as the
// Platform generator. body is marshalled to JSON when non-nil; result is
// unmarshalled from the response body when non-nil. Returns an error if the
// response status doesn't match expectedStatus.
func (c *Client) DoExpectRisk(ctx context.Context, method, path string, body any, expectedStatus int, result any) error {
	return c.doExpect(ctx, c.apiBaseURL, &c.risk, method, path, body, expectedStatus, result)
}

// DoExpectLifecycle performs an authenticated JSON request against the Device Lifecycle API.
func (c *Client) DoExpectLifecycle(ctx context.Context, method, path string, body any, expectedStatus int, result any) error {
	return c.doExpect(ctx, c.apiBaseURL, &c.lifecycle, method, path, body, expectedStatus, result)
}

// DoExpectSSE performs an authenticated JSON request against the Shared Signals & Events API.
func (c *Client) DoExpectSSE(ctx context.Context, method, path string, body any, expectedStatus int, result any) error {
	return c.doExpect(ctx, c.sseBaseURL, &c.sse, method, path, body, expectedStatus, result)
}

func (c *Client) doExpect(ctx context.Context, baseURL string, ts *tokenSource, method, path string, body any, expectedStatus int, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshalling request body: %w", err)
		}
		bodyReader = strings.NewReader(string(data))
	}

	resp, err := c.do(ctx, baseURL, ts, method, path, bodyReader)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 100<<20))
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}
	if resp.StatusCode != expectedStatus {
		return fmt.Errorf("%s %s: expected HTTP %d, got %d: %s", method, path, expectedStatus, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if result == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, result); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}
	return nil
}

func (c *Client) do(ctx context.Context, baseURL string, ts *tokenSource, method, path string, body io.Reader) (*http.Response, error) {
	token, err := c.ensureToken(ctx, ts)
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
			WithHint("the JWT may have expired mid-request, or this application ID isn't scoped for this API; run 'jamf-cli security setup'")
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
