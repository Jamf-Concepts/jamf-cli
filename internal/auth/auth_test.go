// Copyright 2026, Jamf Software LLC

package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestOAuth2Provider_GetToken_Success(t *testing.T) {
	// Mock Jamf Pro OAuth2 token endpoint
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request format
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("expected form content type, got %s", ct)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("grant_type") != "client_credentials" {
			t.Errorf("expected grant_type=client_credentials, got %s", r.FormValue("grant_type"))
		}
		if r.FormValue("client_id") != "test-client-id" {
			t.Errorf("expected client_id=test-client-id, got %s", r.FormValue("client_id"))
		}
		if r.FormValue("client_secret") != "test-client-secret" {
			t.Errorf("expected client_secret=test-client-secret, got %s", r.FormValue("client_secret"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-jwt-token",
			"token_type":   "Bearer",
			"expires_in":   300,
		})
	}))
	defer server.Close()

	p := NewOAuth2Provider(server.URL, "test-client-id", "test-client-secret")
	token, err := p.GetToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "test-jwt-token" {
		t.Errorf("expected test-jwt-token, got %s", token)
	}
}

func TestOAuth2Provider_GetToken_CachesToken(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "cached-token",
			"token_type":   "Bearer",
			"expires_in":   300,
		})
	}))
	defer server.Close()

	p := NewOAuth2Provider(server.URL, "id", "secret")

	// Call twice
	token1, _ := p.GetToken(context.Background())
	token2, _ := p.GetToken(context.Background())

	if token1 != token2 {
		t.Error("expected same token from cache")
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected 1 HTTP call (cached), got %d", callCount)
	}
}

func TestOAuth2Provider_GetToken_RefreshesExpiredToken(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "token-" + string(rune('0'+count)),
			"token_type":   "Bearer",
			"expires_in":   1, // 1 second TTL
		})
	}))
	defer server.Close()

	p := NewOAuth2Provider(server.URL, "id", "secret")
	p.refreshBuffer = 0 // No buffer so token expires after 1s exactly

	_, _ = p.GetToken(context.Background())
	time.Sleep(1100 * time.Millisecond) // Wait for expiry
	_, _ = p.GetToken(context.Background())

	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("expected 2 HTTP calls (refresh after expiry), got %d", callCount)
	}
}

func TestOAuth2Provider_GetToken_ZeroExpiresIn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "valid-token",
			"token_type":   "Bearer",
			"expires_in":   0,
		})
	}))
	defer server.Close()

	p := NewOAuth2Provider(server.URL, "id", "secret")
	_, err := p.GetToken(context.Background())
	if err == nil {
		t.Fatal("expected error for zero expires_in")
	}
}

func TestOAuth2Provider_GetToken_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
	}))
	defer server.Close()

	p := NewOAuth2Provider(server.URL, "bad-id", "bad-secret")
	_, err := p.GetToken(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid credentials")
	}
}

func TestOAuth2Provider_Name(t *testing.T) {
	p := NewOAuth2Provider("", "", "")
	if p.Name() != "oauth2" {
		t.Errorf("expected oauth2, got %s", p.Name())
	}
}

func TestTokenProvider_GetToken(t *testing.T) {
	p := NewTokenProvider("my-token")
	token, err := p.GetToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if token != "my-token" {
		t.Errorf("expected my-token, got %s", token)
	}
}

func TestTokenProvider_GetToken_Empty(t *testing.T) {
	p := NewTokenProvider("")
	_, err := p.GetToken(context.Background())
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

// --- PlatformOAuth2Provider tests ---

func TestPlatformOAuth2Provider_GetToken_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Platform gateway uses /auth/token, not /api/oauth/token
		if r.URL.Path != "/auth/token" {
			t.Errorf("expected path /auth/token, got %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("expected form content type, got %s", ct)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("grant_type") != "client_credentials" {
			t.Errorf("expected grant_type=client_credentials, got %s", r.FormValue("grant_type"))
		}
		if r.FormValue("client_id") != "platform-client-id" {
			t.Errorf("expected client_id=platform-client-id, got %s", r.FormValue("client_id"))
		}
		if r.FormValue("client_secret") != "platform-client-secret" {
			t.Errorf("expected client_secret=platform-client-secret, got %s", r.FormValue("client_secret"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "platform-opaque-token",
			"token_type":   "Bearer",
			"expires_in":   1799,
		})
	}))
	defer server.Close()

	p := NewPlatformOAuth2Provider(server.URL, "platform-client-id", "platform-client-secret", "tenant-uuid")
	token, err := p.GetToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "platform-opaque-token" {
		t.Errorf("expected platform-opaque-token, got %s", token)
	}
}

func TestPlatformOAuth2Provider_GetToken_CachesToken(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "cached-platform-token",
			"token_type":   "Bearer",
			"expires_in":   1799,
		})
	}))
	defer server.Close()

	p := NewPlatformOAuth2Provider(server.URL, "id", "secret", "tenant")

	token1, _ := p.GetToken(context.Background())
	token2, _ := p.GetToken(context.Background())

	if token1 != token2 {
		t.Error("expected same token from cache")
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected 1 HTTP call (cached), got %d", callCount)
	}
}

func TestPlatformOAuth2Provider_GetToken_InvalidCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
	}))
	defer server.Close()

	p := NewPlatformOAuth2Provider(server.URL, "bad-id", "bad-secret", "tenant")
	_, err := p.GetToken(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid credentials")
	}
}

func TestPlatformOAuth2Provider_GetToken_EmptyAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "",
			"token_type":   "Bearer",
			"expires_in":   1799,
		})
	}))
	defer server.Close()

	p := NewPlatformOAuth2Provider(server.URL, "id", "secret", "tenant")
	_, err := p.GetToken(context.Background())
	if err == nil {
		t.Fatal("expected error for empty access_token")
	}
}

func TestPlatformOAuth2Provider_GetToken_ZeroExpiresIn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "valid-token",
			"token_type":   "Bearer",
			"expires_in":   0,
		})
	}))
	defer server.Close()

	p := NewPlatformOAuth2Provider(server.URL, "id", "secret", "tenant")
	_, err := p.GetToken(context.Background())
	if err == nil {
		t.Fatal("expected error for zero expires_in")
	}
}

func TestPlatformOAuth2Provider_TenantID(t *testing.T) {
	p := NewPlatformOAuth2Provider("", "", "", "my-tenant-uuid")
	if p.TenantID() != "my-tenant-uuid" {
		t.Errorf("expected my-tenant-uuid, got %s", p.TenantID())
	}
}

func TestPlatformOAuth2Provider_Name(t *testing.T) {
	p := NewPlatformOAuth2Provider("", "", "", "")
	if p.Name() != "platform" {
		t.Errorf("expected platform, got %s", p.Name())
	}
}

// --- Cookie jar tests ---

func TestOAuth2Provider_HasCookieJar(t *testing.T) {
	p := NewOAuth2Provider("https://example.jamfcloud.com", "id", "secret")
	if p.Jar() == nil {
		t.Error("expected non-nil cookie jar")
	}
}

func TestPlatformOAuth2Provider_HasCookieJar(t *testing.T) {
	p := NewPlatformOAuth2Provider("https://us.api.platform.jamf.com", "id", "secret", "tenant")
	if p.Jar() == nil {
		t.Error("expected non-nil cookie jar")
	}
}

// --- Token disk cache tests ---

func TestOAuth2Provider_GetToken_SavesToDiskCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "disk-cache-token",
			"token_type":   "Bearer",
			"expires_in":   300,
		})
	}))
	defer server.Close()

	p := NewOAuth2Provider(server.URL, "disk-id", "disk-secret")
	cachePath := tokenCachePath(server.URL, "disk-id")
	defer func() { _ = os.Remove(cachePath) }()

	token, err := p.GetToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "disk-cache-token" {
		t.Errorf("expected disk-cache-token, got %s", token)
	}

	tc, ok := loadTokenCache(cachePath)
	if !ok {
		t.Fatal("expected cache file to exist after token exchange")
	}
	if tc.Token != "disk-cache-token" {
		t.Errorf("expected disk-cache-token in cache, got %s", tc.Token)
	}
	if tc.ExpiresAt.IsZero() {
		t.Error("expected non-zero expires_at in cache")
	}
}

func TestOAuth2Provider_GetToken_LoadsFromDiskCache(t *testing.T) {
	// No HTTP server — if GetToken makes any network call it will fail.
	// Pre-populate the cache with a valid future token.
	const (
		fakeURL      = "https://fake-instance.jamfcloud.com"
		fakeClientID = "cached-client-id"
		cachedTok    = "pre-cached-token"
	)
	cachePath := tokenCachePath(fakeURL, fakeClientID)
	defer func() { _ = os.Remove(cachePath) }()

	saveTokenCache(cachePath, cachedTok, time.Now().Add(5*time.Minute))

	p := NewOAuth2Provider(fakeURL, fakeClientID, "secret")
	token, err := p.GetToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != cachedTok {
		t.Errorf("expected %s from disk cache, got %s", cachedTok, token)
	}
}

func TestOAuth2Provider_GetToken_IgnoresExpiredDiskCache(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fresh-token",
			"token_type":   "Bearer",
			"expires_in":   300,
		})
	}))
	defer server.Close()

	cachePath := tokenCachePath(server.URL, "exp-id")
	defer func() { _ = os.Remove(cachePath) }()

	// Write an already-expired cache entry.
	saveTokenCache(cachePath, "stale-token", time.Now().Add(-1*time.Minute))

	p := NewOAuth2Provider(server.URL, "exp-id", "secret")
	token, err := p.GetToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "fresh-token" {
		t.Errorf("expected fresh-token after expired cache, got %s", token)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected 1 HTTP call after cache miss, got %d", callCount)
	}
}

func TestOAuth2Provider_GetToken_IgnoresMalformedDiskCache(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fallback-token",
			"token_type":   "Bearer",
			"expires_in":   300,
		})
	}))
	defer server.Close()

	cachePath := tokenCachePath(server.URL, "bad-id")
	defer func() { _ = os.Remove(cachePath) }()

	// Write garbage to the cache file.
	_ = os.WriteFile(cachePath, []byte("not json at all {{{{"), 0o600)

	p := NewOAuth2Provider(server.URL, "bad-id", "secret")
	token, err := p.GetToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "fallback-token" {
		t.Errorf("expected fallback-token after bad cache, got %s", token)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected 1 HTTP call after cache miss, got %d", callCount)
	}
}

// --- Platform OAuth2 disk cache tests ---

func TestPlatformOAuth2Provider_GetToken_SavesToDiskCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "platform-disk-token",
			"token_type":   "Bearer",
			"expires_in":   300,
		})
	}))
	defer server.Close()

	p := NewPlatformOAuth2Provider(server.URL, "plat-disk-id", "plat-secret", "tenant")
	cachePath := tokenCachePath(server.URL, "plat-disk-id")
	defer func() { _ = os.Remove(cachePath) }()

	token, err := p.GetToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "platform-disk-token" {
		t.Errorf("expected platform-disk-token, got %s", token)
	}

	tc, ok := loadTokenCache(cachePath)
	if !ok {
		t.Fatal("expected cache file to exist after token exchange")
	}
	if tc.Token != "platform-disk-token" {
		t.Errorf("expected platform-disk-token in cache, got %s", tc.Token)
	}
	if tc.ExpiresAt.IsZero() {
		t.Error("expected non-zero expires_at in cache")
	}
}

func TestPlatformOAuth2Provider_GetToken_LoadsFromDiskCache(t *testing.T) {
	// No HTTP server — if GetToken makes any network call it will fail.
	const (
		fakeURL      = "https://fake-platform.jamf.com"
		fakeClientID = "plat-cached-id"
		cachedTok    = "plat-pre-cached-token"
	)
	cachePath := tokenCachePath(fakeURL, fakeClientID)
	defer func() { _ = os.Remove(cachePath) }()

	saveTokenCache(cachePath, cachedTok, time.Now().Add(5*time.Minute))

	p := NewPlatformOAuth2Provider(fakeURL, fakeClientID, "secret", "tenant")
	token, err := p.GetToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != cachedTok {
		t.Errorf("expected %s from disk cache, got %s", cachedTok, token)
	}
}

func TestPlatformOAuth2Provider_GetToken_IgnoresExpiredDiskCache(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "plat-fresh-token",
			"token_type":   "Bearer",
			"expires_in":   300,
		})
	}))
	defer server.Close()

	cachePath := tokenCachePath(server.URL, "plat-exp-id")
	defer func() { _ = os.Remove(cachePath) }()

	saveTokenCache(cachePath, "plat-stale-token", time.Now().Add(-1*time.Minute))

	p := NewPlatformOAuth2Provider(server.URL, "plat-exp-id", "secret", "tenant")
	token, err := p.GetToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "plat-fresh-token" {
		t.Errorf("expected plat-fresh-token after expired cache, got %s", token)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected 1 HTTP call after cache miss, got %d", callCount)
	}
}

func TestPlatformOAuth2Provider_GetToken_IgnoresMalformedDiskCache(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "plat-fallback-token",
			"token_type":   "Bearer",
			"expires_in":   300,
		})
	}))
	defer server.Close()

	cachePath := tokenCachePath(server.URL, "plat-bad-id")
	defer func() { _ = os.Remove(cachePath) }()

	_ = os.WriteFile(cachePath, []byte("not json at all {{{{"), 0o600)

	p := NewPlatformOAuth2Provider(server.URL, "plat-bad-id", "secret", "tenant")
	token, err := p.GetToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "plat-fallback-token" {
		t.Errorf("expected plat-fallback-token after bad cache, got %s", token)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected 1 HTTP call after cache miss, got %d", callCount)
	}
}

func TestTokenCachePath_UniquePerProfile(t *testing.T) {
	p1 := tokenCachePath("https://a.jamfcloud.com", "client-1")
	p2 := tokenCachePath("https://b.jamfcloud.com", "client-1")
	p3 := tokenCachePath("https://a.jamfcloud.com", "client-2")

	if p1 == p2 {
		t.Error("different URLs should produce different cache paths")
	}
	if p1 == p3 {
		t.Error("different client IDs should produce different cache paths")
	}
	if p2 == p3 {
		t.Error("different URL+clientID combinations should produce different cache paths")
	}
}
