// Copyright 2026, Jamf Software LLC

package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestVerifyOAuth2Credentials_IgnoresTheTokenCache pins the reason
// VerifyOAuth2Credentials calls exchangeToken rather than GetToken.
//
// The on-disk token cache is keyed on (baseURL, clientID) and not on the
// secret, so a cache entry left by a working pair would answer for a later
// wrong secret carrying the same client ID — reporting a credential that
// cannot authenticate as verified, which is the one thing verification exists
// to catch.
func TestVerifyOAuth2Credentials_IgnoresTheTokenCache(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // keep cache writes out of the real one
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	var exchanges int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/oauth/token") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			return
		}
		exchanges++
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parsing form: %v", err)
		}
		if r.PostForm.Get("client_secret") != "right" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 300})
	}))
	defer server.Close()

	ctx := context.Background()
	if err := VerifyOAuth2Credentials(ctx, server.URL, "cid", "right"); err != nil {
		t.Fatalf("verifying the good pair: %v", err)
	}
	if err := VerifyOAuth2Credentials(ctx, server.URL, "cid", "wrong"); err == nil {
		t.Fatal("a wrong secret was accepted for a client ID verified moments earlier")
	}
	if exchanges != 2 {
		t.Errorf("token exchanges = %d, want 2 — a cached token answered for the second pair", exchanges)
	}
}

func TestVerifyPlatformCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/auth/token") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parsing form: %v", err)
		}
		if r.PostForm.Get("client_secret") != "right" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 300})
	}))
	defer server.Close()

	ctx := context.Background()
	if err := VerifyPlatformCredentials(ctx, server.URL, "cid", "right", EnvironmentScope("env-1")); err != nil {
		t.Fatalf("verifying the good pair: %v", err)
	}
	if err := VerifyPlatformCredentials(ctx, server.URL, "cid", "wrong", EnvironmentScope("env-1")); err == nil {
		t.Fatal("a wrong secret was accepted")
	}
}
