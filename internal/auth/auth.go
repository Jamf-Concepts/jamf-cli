// Copyright 2026, Jamf Software LLC

package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Provider defines the interface for authentication providers
type Provider interface {
	// GetToken returns a valid authentication token
	GetToken(ctx context.Context) (string, error)
	// Name returns the provider name for logging
	Name() string
}

// TokenProvider uses a pre-existing bearer token
type TokenProvider struct {
	token string
}

func NewTokenProvider(token string) *TokenProvider {
	return &TokenProvider{token: token}
}

func (p *TokenProvider) GetToken(ctx context.Context) (string, error) {
	if p.token == "" {
		return "", fmt.Errorf("no token configured: provide one via JAMF_TOKEN env var or a config profile")
	}
	return p.token, nil
}

func (p *TokenProvider) Name() string {
	return "token"
}

// cachedToken is the on-disk representation of a persisted OAuth2 token.
type cachedToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// tokenCachePath returns the path of the token cache file for the given URL and
// client ID. The path is keyed by a sha256 hash so each profile gets a unique file.
// Files are stored under os.UserCacheDir() (~/Library/Caches on macOS, ~/.cache on
// Linux) so they survive reboots, with os.TempDir() as a fallback.
func tokenCachePath(baseURL, clientID string) string {
	h := sha256.Sum256([]byte(baseURL + "|" + clientID))
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}
	dir := filepath.Join(cacheDir, "jamf-cli")
	_ = os.MkdirAll(dir, 0o700)
	return filepath.Join(dir, "token-"+hex.EncodeToString(h[:]))
}

// loadTokenCache reads the cache file at path and returns the token and its expiry.
// Returns false if the file does not exist, cannot be parsed, or the token is empty.
func loadTokenCache(path string) (cachedToken, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return cachedToken{}, false
	}
	var tc cachedToken
	if err := json.Unmarshal(data, &tc); err != nil || tc.Token == "" {
		return cachedToken{}, false
	}
	return tc, true
}

// saveTokenCache persists a token and its expiry to the cache file with mode 0600.
// Failures are silently ignored — the cache is best-effort.
func saveTokenCache(path, token string, expiresAt time.Time) {
	data, err := json.Marshal(cachedToken{Token: token, ExpiresAt: expiresAt})
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

// OAuth2Provider uses client credentials flow to obtain and cache tokens.
// It proactively refreshes the token before expiry and persists tokens to a
// temp file so repeated CLI invocations skip redundant token exchanges.
type OAuth2Provider struct {
	baseURL      string
	clientID     string
	clientSecret string
	httpClient   *http.Client
	jar          *cookiejar.Jar

	// cached token state
	mu            sync.Mutex
	token         string
	expiresAt     time.Time
	refreshBuffer time.Duration // how early to refresh before expiry
}

func NewOAuth2Provider(baseURL, clientID, clientSecret string) *OAuth2Provider {
	jar, _ := cookiejar.New(nil)
	return &OAuth2Provider{
		baseURL:      baseURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		jar:          jar,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Jar:     jar,
		},
		refreshBuffer: 10 * time.Second,
	}
}

// Jar returns the cookie jar used by this provider's HTTP client.
// Sharing this jar with the API client enables sticky session affinity
// cookies (e.g. APBALANCEID on Jamf Cloud) to persist across requests.
func (p *OAuth2Provider) Jar() http.CookieJar {
	return p.jar
}

func (p *OAuth2Provider) GetToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Return in-memory cached token if still valid.
	if p.token != "" && time.Now().Before(p.expiresAt.Add(-p.refreshBuffer)) {
		return p.token, nil
	}

	// Try disk cache before making a token exchange request.
	// This avoids a redundant OAuth2 round-trip on every CLI invocation.
	cachePath := tokenCachePath(p.baseURL, p.clientID)
	if tc, ok := loadTokenCache(cachePath); ok && time.Now().Before(tc.ExpiresAt.Add(-p.refreshBuffer)) {
		p.token = tc.Token
		p.expiresAt = tc.ExpiresAt
		return p.token, nil
	}

	// Exchange client credentials for a new token.
	token, expiresIn, err := p.exchangeToken(ctx)
	if err != nil {
		return "", err
	}

	p.token = token
	p.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	saveTokenCache(cachePath, p.token, p.expiresAt)
	return p.token, nil
}

func (p *OAuth2Provider) exchangeToken(ctx context.Context) (string, int, error) {
	data := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {p.clientID},
		"client_secret": {p.clientSecret},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/oauth/token",
		strings.NewReader(data.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("creating token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("token exchange request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", 0, fmt.Errorf("OAuth2 token exchange failed: invalid client credentials, verify your client-id and client-secret are correct")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", 0, fmt.Errorf("OAuth2 token exchange failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", 0, fmt.Errorf("parsing token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return "", 0, fmt.Errorf("OAuth2 token exchange returned empty access_token, check that the API integration is enabled in Jamf Pro")
	}

	if tokenResp.ExpiresIn <= 0 {
		return "", 0, fmt.Errorf("token exchange returned invalid expires_in: %d", tokenResp.ExpiresIn)
	}

	return tokenResp.AccessToken, tokenResp.ExpiresIn, nil
}

func (p *OAuth2Provider) Name() string {
	return "oauth2"
}

// PlatformOAuth2Provider uses the Jamf Platform Gateway for authentication.
// Instead of authenticating directly against a Jamf Pro instance, it obtains
// tokens from a regional platform gateway (e.g., https://us.api.platform.jamf.com)
// and routes all API requests through that gateway using tenant-scoped URL paths.
type PlatformOAuth2Provider struct {
	baseURL      string
	clientID     string
	clientSecret string
	tenantID     string
	httpClient   *http.Client
	jar          *cookiejar.Jar

	// cached token state
	mu            sync.Mutex
	token         string
	expiresAt     time.Time
	refreshBuffer time.Duration
}

func NewPlatformOAuth2Provider(baseURL, clientID, clientSecret, tenantID string) *PlatformOAuth2Provider {
	jar, _ := cookiejar.New(nil)
	return &PlatformOAuth2Provider{
		baseURL:      baseURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		tenantID:     tenantID,
		jar:          jar,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Jar:     jar,
		},
		refreshBuffer: 10 * time.Second,
	}
}

// Jar returns the cookie jar used by this provider's HTTP client.
// Sharing this jar with the API client enables sticky session affinity
// cookies (e.g. APBALANCEID on Jamf Cloud) to persist across requests.
func (p *PlatformOAuth2Provider) Jar() http.CookieJar {
	return p.jar
}

func (p *PlatformOAuth2Provider) GetToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Return in-memory cached token if still valid.
	if p.token != "" && time.Now().Before(p.expiresAt.Add(-p.refreshBuffer)) {
		return p.token, nil
	}

	// Try disk cache before making a token exchange request.
	cachePath := tokenCachePath(p.baseURL, p.clientID)
	if tc, ok := loadTokenCache(cachePath); ok && time.Now().Before(tc.ExpiresAt.Add(-p.refreshBuffer)) {
		p.token = tc.Token
		p.expiresAt = tc.ExpiresAt
		return p.token, nil
	}

	token, expiresIn, err := p.exchangeToken(ctx)
	if err != nil {
		return "", err
	}

	p.token = token
	p.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	saveTokenCache(cachePath, p.token, p.expiresAt)
	return p.token, nil
}

func (p *PlatformOAuth2Provider) exchangeToken(ctx context.Context) (string, int, error) {
	data := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {p.clientID},
		"client_secret": {p.clientSecret},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/auth/token",
		strings.NewReader(data.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("creating platform token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("platform token exchange request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", 0, fmt.Errorf("platform token exchange failed: invalid client credentials, verify your client-id and client-secret are correct")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", 0, fmt.Errorf("platform token exchange failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", 0, fmt.Errorf("parsing platform token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return "", 0, fmt.Errorf("platform token exchange returned empty access_token")
	}

	if tokenResp.ExpiresIn <= 0 {
		return "", 0, fmt.Errorf("platform token exchange returned invalid expires_in: %d", tokenResp.ExpiresIn)
	}

	return tokenResp.AccessToken, tokenResp.ExpiresIn, nil
}

// TenantID returns the tenant identifier used for gateway URL path rewriting.
func (p *PlatformOAuth2Provider) TenantID() string {
	return p.tenantID
}

func (p *PlatformOAuth2Provider) Name() string {
	return "platform"
}
