// Copyright 2026, Jamf Software LLC

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/useragent"
)

// Both token exchanges send jamf-cli's User-Agent. They are built here rather
// than through internal/client, and set none at all until this was added, so a
// gateway or instance log saw Go-http-client for every token jamf-cli fetched.
func TestTokenExchangesSendTheUserAgent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	useragent.Set("9.9.9")
	t.Cleanup(func() { useragent.Set("dev") })
	want := "jamf-cli/9.9.9 (+https://github.com/Jamf-Concepts/jamf-cli)"

	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Path+" "+r.Header.Get("User-Agent"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"t","token_type":"bearer","expires_in":3600}`))
	}))
	defer srv.Close()

	if _, _, err := NewOAuth2Provider(srv.URL, "id", "secret").exchangeToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewPlatformOAuth2Provider(srv.URL, "id", "secret", TenantScope("t")).exchangeToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"/api/oauth/token " + want, "/auth/token " + want} {
		found := false
		for _, g := range got {
			found = found || g == line
		}
		if !found {
			t.Errorf("no request %q; got %q", line, got)
		}
	}
}
