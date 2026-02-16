package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ktn-jamf/jamfpro-cli/internal/auth"
)

func TestDo_ModernAPIPathPrefix(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))

	_, err := c.Do(context.Background(), "GET", "/v1/buildings", nil)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	if gotPath != "/api/v1/buildings" {
		t.Errorf("path = %q, want %q", gotPath, "/api/v1/buildings")
	}
}

func TestDo_ClassicAPIPathNoPrefix(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))

	_, err := c.Do(context.Background(), "GET", "/JSSResource/policies", nil)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	if gotPath != "/JSSResource/policies" {
		t.Errorf("path = %q, want %q", gotPath, "/JSSResource/policies")
	}
}

func TestDo_ExplicitAPIPrefix(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))

	_, err := c.Do(context.Background(), "GET", "/api/v1/buildings", nil)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	if gotPath != "/api/v1/buildings" {
		t.Errorf("path = %q, want %q", gotPath, "/api/v1/buildings")
	}
}

func TestDo_SetsJSONHeaders(t *testing.T) {
	var gotAccept, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))

	body := strings.NewReader(`{"name":"test"}`)
	_, err := c.Do(context.Background(), "POST", "/JSSResource/policies/id/0", body)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want %q", gotAccept, "application/json")
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", gotContentType, "application/json")
	}
}

func TestDo_SetsBearerAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("my-secret-token"))

	_, err := c.Do(context.Background(), "GET", "/JSSResource/policies", nil)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	if gotAuth != "Bearer my-secret-token" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer my-secret-token")
	}
}

func TestDo_ClassicAPIWithBody(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id": 1}`))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))

	input := `{"policy":{"name":"Test Policy"}}`
	resp, err := c.Do(context.Background(), "POST", "/JSSResource/policies/id/0", strings.NewReader(input))
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()

	if gotBody != input {
		t.Errorf("body = %q, want %q", gotBody, input)
	}
}
