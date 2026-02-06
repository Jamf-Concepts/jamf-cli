package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		json.NewEncoder(w).Encode(map[string]interface{}{
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
		json.NewEncoder(w).Encode(map[string]interface{}{
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
		json.NewEncoder(w).Encode(map[string]interface{}{
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
		json.NewEncoder(w).Encode(map[string]interface{}{
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
		w.Write([]byte(`{"error":"invalid_client"}`))
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

func TestBasicProvider_GetToken_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"token":   "basic-bearer-token",
			"expires": "2026-01-01T00:00:00Z",
		})
	}))
	defer server.Close()

	p := NewBasicProvider(server.URL, "admin", "secret")
	token, err := p.GetToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "basic-bearer-token" {
		t.Errorf("expected basic-bearer-token, got %s", token)
	}
}

func TestBasicProvider_GetToken_CachesToken(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"token":   "cached-basic-token",
			"expires": "2026-01-01T00:00:00Z",
		})
	}))
	defer server.Close()

	p := NewBasicProvider(server.URL, "admin", "secret")

	token1, _ := p.GetToken(context.Background())
	token2, _ := p.GetToken(context.Background())

	if token1 != token2 {
		t.Error("expected same token from cache")
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected 1 HTTP call (cached), got %d", callCount)
	}
}

func TestBasicProvider_GetToken_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	p := NewBasicProvider(server.URL, "bad", "creds")
	_, err := p.GetToken(context.Background())
	if err == nil {
		t.Fatal("expected error for unauthorized")
	}
}

func TestBasicProvider_Name(t *testing.T) {
	p := NewBasicProvider("", "", "")
	if p.Name() != "basic" {
		t.Errorf("expected basic, got %s", p.Name())
	}
}

func TestBasicAuthExchange_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/auth/token" {
			t.Errorf("expected /api/v1/auth/token, got %s", r.URL.Path)
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"token":   "bearer-token-123",
			"expires": "2026-01-01T00:00:00Z",
		})
	}))
	defer server.Close()

	token, err := BasicAuthExchange(context.Background(), server.URL, "admin", "secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "bearer-token-123" {
		t.Errorf("expected bearer-token-123, got %s", token)
	}
}

func TestBasicAuthExchange_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := BasicAuthExchange(context.Background(), server.URL, "bad", "creds")
	if err == nil {
		t.Fatal("expected error for unauthorized")
	}
	if err.Error() != "invalid username or password" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBasicAuthExchange_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	_, err := BasicAuthExchange(context.Background(), server.URL, "admin", "pass")
	if err == nil {
		t.Fatal("expected error for server error")
	}
}

func TestBasicAuthExchange_EmptyToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"token":   "",
			"expires": "2026-01-01T00:00:00Z",
		})
	}))
	defer server.Close()

	_, err := BasicAuthExchange(context.Background(), server.URL, "admin", "pass")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}
