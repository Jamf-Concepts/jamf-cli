// Copyright 2026, Jamf Software LLC

package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

const missingPermsBody = `{"errors":[{"code":"MISSING_PERMISSION","description":"certificate:issue"}]}`

// A 403 is normally mapped to exitcode.PermissionDenied with a hint about the
// caller's own API role. Endpoints whose 403 is a documented result opt out via
// registry.WithAllowedStatuses, and must then receive the response with its body
// intact so they can render what the server actually said.
func TestDo_AllowedStatusReturnsResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(missingPermsBody))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))
	ctx := registry.WithAllowedStatuses(context.Background(), http.StatusForbidden)

	resp, err := c.Do(ctx, "GET", "/v1/pki/digicert/trust-lifecycle-manager/1/privilege-check", nil)
	if err != nil {
		t.Fatalf("Do() error = %v, want the 403 response instead", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if string(body) != missingPermsBody {
		t.Errorf("body = %q, want the server's 403 payload intact", string(body))
	}
}

// Only the statuses the caller named are let through; everything else still maps
// to a structured exit code.
func TestDo_UnlistedStatusStillErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"httpStatus":404}`))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))
	// 403 allowed, 404 not.
	ctx := registry.WithAllowedStatuses(context.Background(), http.StatusForbidden)

	_, err := c.Do(ctx, "GET", "/v1/pki/digicert/trust-lifecycle-manager/1/privilege-check", nil)
	if err == nil {
		t.Fatal("expected an error for an unlisted status")
	}
	if got := exitcode.CodeFrom(err); got != exitcode.NotFound {
		t.Errorf("exit code = %d, want %d (not found)", got, exitcode.NotFound)
	}
}

// Without the opt-out, a 403 keeps its existing exit code and hint.
func TestDo_ForbiddenWithoutOptOutStillPermissionDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(missingPermsBody))
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))

	_, err := c.Do(context.Background(), "GET", "/v1/buildings", nil)
	if err == nil {
		t.Fatal("expected a permission-denied error")
	}
	if got := exitcode.CodeFrom(err); got != exitcode.PermissionDenied {
		t.Errorf("exit code = %d, want %d (permission denied)", got, exitcode.PermissionDenied)
	}
}
