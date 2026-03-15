package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
		return "", fmt.Errorf("no token configured: provide one via --token, JAMF_TOKEN env var, or a config profile")
	}
	return p.token, nil
}

func (p *TokenProvider) Name() string {
	return "token"
}

// OAuth2Provider uses client credentials flow to obtain and cache tokens.
// It proactively refreshes the token before expiry.
type OAuth2Provider struct {
	baseURL      string
	clientID     string
	clientSecret string
	httpClient   *http.Client

	// cached token state
	mu            sync.Mutex
	token         string
	expiresAt     time.Time
	refreshBuffer time.Duration // how early to refresh before expiry
}

func NewOAuth2Provider(baseURL, clientID, clientSecret string) *OAuth2Provider {
	return &OAuth2Provider{
		baseURL:      baseURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		refreshBuffer: 10 * time.Second,
	}
}

func (p *OAuth2Provider) GetToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Return cached token if still valid
	if p.token != "" && time.Now().Before(p.expiresAt.Add(-p.refreshBuffer)) {
		return p.token, nil
	}

	// Exchange client credentials for a new token
	token, expiresIn, err := p.exchangeToken(ctx)
	if err != nil {
		return "", err
	}

	p.token = token
	p.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
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

