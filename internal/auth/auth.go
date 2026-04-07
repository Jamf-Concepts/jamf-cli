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
// Linux) so they survive reboots. Returns "" if no user-private cache directory is
// available — callers must check for "" and skip disk caching.
func tokenCachePath(baseURL, clientID string) string {
	h := sha256.Sum256([]byte(baseURL + "|" + clientID))
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(cacheDir, "jamf-cli")
	_ = os.MkdirAll(dir, 0o700)
	return filepath.Join(dir, "token-"+hex.EncodeToString(h[:]))
}

// ClearTokenCache removes the on-disk token cache file for the given URL and
// client ID. Called by setup commands after credential changes to prevent stale
// cached tokens from being used. Errors are silently ignored.
func ClearTokenCache(baseURL, clientID string) {
	if path := tokenCachePath(baseURL, clientID); path != "" {
		_ = os.Remove(path)
	}
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
// The write is atomic: data is written to a temporary file then renamed into place
// so concurrent readers never see a partial file. Failures are silently ignored —
// the cache is best-effort.
func saveTokenCache(path, token string, expiresAt time.Time) {
	data, err := json.Marshal(cachedToken{Token: token, ExpiresAt: expiresAt})
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// cookieJarPath returns the path for the cookie cache file for the given URL and
// client ID. Keyed by the same hash as the token cache so each profile gets its
// own file. Returns "" if no user-private cache directory is available.
func cookieJarPath(baseURL, clientID string) string {
	h := sha256.Sum256([]byte(baseURL + "|" + clientID))
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(cacheDir, "jamf-cli")
	_ = os.MkdirAll(dir, 0o700)
	return filepath.Join(dir, "cookies-"+hex.EncodeToString(h[:]))
}

// ClearCookieCache removes the on-disk cookie cache file for the given URL and
// client ID. Called alongside ClearTokenCache after credential changes.
func ClearCookieCache(baseURL, clientID string) {
	if path := cookieJarPath(baseURL, clientID); path != "" {
		_ = os.Remove(path)
	}
}

// persistedCookieFile is the on-disk format for the cookie cache.
type persistedCookieFile struct {
	Entries []persistedCookieEntry `json:"entries"`
}

type persistedCookieEntry struct {
	URL     string            `json:"url"`
	Cookies []persistedCookie `json:"cookies"`
}

type persistedCookie struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Path     string    `json:"path,omitempty"`
	Domain   string    `json:"domain,omitempty"`
	Expires  time.Time `json:"expires,omitempty"`
	Secure   bool      `json:"secure,omitempty"`
	HttpOnly bool      `json:"http_only,omitempty"`
}

// diskCookieJar is a cookie jar that persists cookies to disk so that
// session-affinity cookies (e.g. APBALANCEID on Jamf Cloud) survive across
// separate CLI invocations. The inner cookiejar.Jar handles all RFC 6265
// semantics; this wrapper intercepts SetCookies to write-through to disk.
type diskCookieJar struct {
	inner *cookiejar.Jar
	path  string
	mu    sync.Mutex
	// seen tracks the latest value of each cookie name per scheme://host key.
	seen map[string]map[string]*http.Cookie
}

// newDiskCookieJar creates a diskCookieJar backed by path. If path is "" it
// behaves as a plain in-memory jar. Any previously persisted cookies are
// loaded immediately so the inner jar starts with the last known state.
func newDiskCookieJar(path string) *diskCookieJar {
	inner, _ := cookiejar.New(nil)
	j := &diskCookieJar{
		inner: inner,
		path:  path,
		seen:  make(map[string]map[string]*http.Cookie),
	}
	if path != "" {
		j.load()
	}
	return j
}

func (j *diskCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.inner.SetCookies(u, cookies)
	if j.path == "" {
		return
	}
	key := u.Scheme + "://" + u.Host
	j.mu.Lock()
	if j.seen[key] == nil {
		j.seen[key] = make(map[string]*http.Cookie)
	}
	for _, c := range cookies {
		j.seen[key][c.Name] = c
	}
	j.mu.Unlock()
	j.save()
}

func (j *diskCookieJar) Cookies(u *url.URL) []*http.Cookie {
	return j.inner.Cookies(u)
}

func (j *diskCookieJar) load() {
	data, err := os.ReadFile(j.path)
	if err != nil {
		return
	}
	var pf persistedCookieFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, entry := range pf.Entries {
		u, err := url.Parse(entry.URL)
		if err != nil {
			continue
		}
		byName := make(map[string]*http.Cookie, len(entry.Cookies))
		var cookies []*http.Cookie
		for _, pc := range entry.Cookies {
			c := &http.Cookie{
				Name:     pc.Name,
				Value:    pc.Value,
				Path:     pc.Path,
				Domain:   pc.Domain,
				Expires:  pc.Expires,
				Secure:   pc.Secure,
				HttpOnly: pc.HttpOnly,
			}
			cookies = append(cookies, c)
			byName[c.Name] = c
		}
		j.inner.SetCookies(u, cookies)
		j.seen[entry.URL] = byName
	}
}

func (j *diskCookieJar) save() {
	j.mu.Lock()
	pf := persistedCookieFile{}
	for rawURL, byName := range j.seen {
		entry := persistedCookieEntry{URL: rawURL}
		for _, c := range byName {
			entry.Cookies = append(entry.Cookies, persistedCookie{
				Name:     c.Name,
				Value:    c.Value,
				Path:     c.Path,
				Domain:   c.Domain,
				Expires:  c.Expires,
				Secure:   c.Secure,
				HttpOnly: c.HttpOnly,
			})
		}
		pf.Entries = append(pf.Entries, entry)
	}
	j.mu.Unlock()

	data, err := json.Marshal(pf)
	if err != nil {
		return
	}
	tmp := j.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, j.path)
}

// OAuth2Provider uses client credentials flow to obtain and cache tokens.
// It proactively refreshes the token before expiry and persists tokens to a
// temp file so repeated CLI invocations skip redundant token exchanges.
type OAuth2Provider struct {
	baseURL      string
	clientID     string
	clientSecret string
	httpClient   *http.Client
	jar          *diskCookieJar

	// cached token state
	mu            sync.Mutex
	token         string
	expiresAt     time.Time
	refreshBuffer time.Duration // how early to refresh before expiry
}

func NewOAuth2Provider(baseURL, clientID, clientSecret string) *OAuth2Provider {
	jar := newDiskCookieJar(cookieJarPath(baseURL, clientID))
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
	if cachePath != "" {
		if tc, ok := loadTokenCache(cachePath); ok && time.Now().Before(tc.ExpiresAt.Add(-p.refreshBuffer)) {
			p.token = tc.Token
			p.expiresAt = tc.ExpiresAt
			return p.token, nil
		}
	}

	// Exchange client credentials for a new token.
	token, expiresIn, err := p.exchangeToken(ctx)
	if err != nil {
		return "", err
	}

	p.token = token
	p.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	if cachePath != "" {
		saveTokenCache(cachePath, p.token, p.expiresAt)
	}
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
	jar          *diskCookieJar

	// cached token state
	mu            sync.Mutex
	token         string
	expiresAt     time.Time
	refreshBuffer time.Duration
}

func NewPlatformOAuth2Provider(baseURL, clientID, clientSecret, tenantID string) *PlatformOAuth2Provider {
	jar := newDiskCookieJar(cookieJarPath(baseURL, clientID))
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
	if cachePath != "" {
		if tc, ok := loadTokenCache(cachePath); ok && time.Now().Before(tc.ExpiresAt.Add(-p.refreshBuffer)) {
			p.token = tc.Token
			p.expiresAt = tc.ExpiresAt
			return p.token, nil
		}
	}

	token, expiresIn, err := p.exchangeToken(ctx)
	if err != nil {
		return "", err
	}

	p.token = token
	p.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	if cachePath != "" {
		saveTokenCache(cachePath, p.token, p.expiresAt)
	}
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
