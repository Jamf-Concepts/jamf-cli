// Copyright 2026, Jamf Software LLC

package security

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

func makeJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	return header + "." + body + ".sig"
}

func TestDecodeJWTClaims_Valid(t *testing.T) {
	exp := time.Now().Add(15 * time.Minute).Unix()
	tok := makeJWT(t, map[string]any{"customer_id": "cust-123", "exp": exp})

	claims, err := decodeJWTClaims(tok)
	if err != nil {
		t.Fatalf("decodeJWTClaims() error = %v", err)
	}
	if claims.CustomerID != "cust-123" {
		t.Errorf("CustomerID = %q, want %q", claims.CustomerID, "cust-123")
	}
	if claims.ExpiresAt != exp {
		t.Errorf("ExpiresAt = %d, want %d", claims.ExpiresAt, exp)
	}
}

func TestDecodeJWTClaims_MissingExp(t *testing.T) {
	tok := makeJWT(t, map[string]any{"customer_id": "cust-123"})
	if _, err := decodeJWTClaims(tok); err == nil {
		t.Fatal("decodeJWTClaims() error = nil, want error for missing exp")
	}
}

func TestDecodeJWTClaims_MalformedSegments(t *testing.T) {
	if _, err := decodeJWTClaims("only.two"); err == nil {
		t.Fatal("decodeJWTClaims() error = nil, want error")
	}
}

func TestDecodeJWTClaims_BadBase64(t *testing.T) {
	if _, err := decodeJWTClaims("header.not-valid-base64!!!.sig"); err == nil {
		t.Fatal("decodeJWTClaims() error = nil, want error")
	}
}

func TestDecodeJWTClaims_BadJSON(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte("not json"))
	tok := "header." + payload + ".sig"
	if _, err := decodeJWTClaims(tok); err == nil {
		t.Fatal("decodeJWTClaims() error = nil, want error")
	}
}

func TestHTTPStatusError_Mapping(t *testing.T) {
	cases := []struct {
		status int
		code   int
	}{
		{http.StatusUnauthorized, exitcode.Authentication},
		{http.StatusForbidden, exitcode.PermissionDenied},
		{http.StatusNotFound, exitcode.NotFound},
		{http.StatusTooManyRequests, exitcode.RateLimited},
		{http.StatusInternalServerError, exitcode.General},
	}
	for _, c := range cases {
		err := httpStatusError(c.status, "GET", "/x", []byte("boom"))
		var e *exitcode.Error
		if !errors.As(err, &e) {
			t.Fatalf("status %d: not an *exitcode.Error: %v", c.status, err)
		}
		if e.Code != c.code {
			t.Errorf("status %d: code = %d, want %d", c.status, e.Code, c.code)
		}
		// 404 intentionally omits the response body (see httpStatusError).
		if c.status != http.StatusNotFound && !strings.Contains(err.Error(), "boom") {
			t.Errorf("status %d: error %q missing body", c.status, err.Error())
		}
	}
}

// TestLoginAndDoExpect_RoundTrip exercises login() and doExpect() together
// against a fake server standing in for api.wandera.com, using the
// WithHTTPClient/WithAPIBaseURL seams.
func TestLoginAndDoExpect_RoundTrip(t *testing.T) {
	exp := time.Now().Add(15 * time.Minute).Unix()
	tok := makeJWT(t, map[string]any{"customer_id": "cust-456", "exp": exp})

	var loginCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/login":
			loginCalls++
			auth := r.Header.Get("authorization")
			if !strings.HasPrefix(auth, "Basic ") {
				t.Errorf("login request missing Basic auth header, got %q", auth)
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"token": tok})
		case r.Method == http.MethodGet && r.URL.Path == "/risk/v2/devices":
			bearer := r.Header.Get("authorization")
			if bearer != "Bearer "+tok {
				t.Errorf("request bearer = %q, want %q", bearer, "Bearer "+tok)
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"devices": []any{}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient(
		WithHTTPClient(srv.Client()),
		WithAPIBaseURL(srv.URL),
		WithRiskCredentials("id", "secret"),
	)

	var result map[string]any
	if err := c.DoExpectRisk(context.Background(), "GET", "/risk/v2/devices", nil, &result); err != nil {
		t.Fatalf("DoExpectRisk() error = %v", err)
	}
	if loginCalls != 1 {
		t.Errorf("loginCalls = %d, want 1", loginCalls)
	}

	// A second call within the token's lifetime must reuse the cached token,
	// not log in again.
	if err := c.DoExpectRisk(context.Background(), "GET", "/risk/v2/devices", nil, &result); err != nil {
		t.Fatalf("DoExpectRisk() second call error = %v", err)
	}
	if loginCalls != 1 {
		t.Errorf("loginCalls after second request = %d, want 1 (token should be cached)", loginCalls)
	}
}

func TestDoExpect_UnconfiguredScopeErrorsWithoutNetworkCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request to %s %s for an unconfigured scope", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	c := NewClient(WithHTTPClient(srv.Client()), WithAPIBaseURL(srv.URL))

	var result map[string]any
	err := c.DoExpectRisk(context.Background(), "GET", "/risk/v2/devices", nil, &result)
	if err == nil {
		t.Fatal("DoExpectRisk() error = nil, want error for unconfigured credentials")
	}
	if !strings.Contains(err.Error(), "security setup") {
		t.Errorf("error %q missing setup hint", err.Error())
	}
}

func TestLogin_NonOKStatusSurfacesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("bad credentials"))
	}))
	defer srv.Close()

	c := NewClient(
		WithHTTPClient(srv.Client()),
		WithAPIBaseURL(srv.URL),
		WithLifecycleCredentials("id", "secret"),
	)

	_, err := c.LifecycleCustomerID(context.Background())
	if err == nil {
		t.Fatal("LifecycleCustomerID() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "bad credentials") {
		t.Errorf("error %q missing response body", err.Error())
	}
}

func TestLifecycleCustomerID_EmptyClaimErrors(t *testing.T) {
	exp := time.Now().Add(15 * time.Minute).Unix()
	tok := makeJWT(t, map[string]any{"exp": exp}) // no customer_id

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"token": tok})
	}))
	defer srv.Close()

	c := NewClient(
		WithHTTPClient(srv.Client()),
		WithAPIBaseURL(srv.URL),
		WithLifecycleCredentials("id", "secret"),
	)

	if _, err := c.LifecycleCustomerID(context.Background()); err == nil {
		t.Fatal("LifecycleCustomerID() error = nil, want error for empty customer_id")
	}
}
