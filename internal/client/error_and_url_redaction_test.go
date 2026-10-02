// Copyright 2026, Jamf Software LLC

package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
)

// TestStatusErrorRedactsTheBodyItQuotes covers the error envelope: a refused
// write's response body is quoted into the message, and a Classic or JSON body
// can echo the credential the request carried.
func TestStatusErrorRedactsTheBodyItQuotes(t *testing.T) {
	body := []byte(`{"httpStatus":400,"errors":[{"field":"password"}],"password":"SENT-echoed","name":"svc"}`)
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusBadRequest, http.StatusConflict} {
		msg := StatusError(status, "POST", "/v1/x", body).Error()
		if strings.Contains(msg, "SENT-echoed") {
			t.Errorf("HTTP %d: credential quoted into the error: %s", status, msg)
		}
		if !strings.Contains(msg, `"name":"svc"`) || !strings.Contains(msg, "[REDACTED]") {
			t.Errorf("HTTP %d: want the body redacted, not dropped: %s", status, msg)
		}
	}
}

func TestVerboseRequestLineRedactsCredentialQueryParameters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := New(srv.URL, auth.NewTokenProvider("test-token"), WithVerbose(1))
	out := captureStderrConcurrently(t, func() {
		resp, err := c.Do(context.Background(), http.MethodGet, "/v1/x?page=2&access_token=SENT-query-token", nil)
		if err != nil {
			t.Errorf("Do: %v", err)
			return
		}
		_, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
	})
	if strings.Contains(out, "SENT-query-token") {
		t.Errorf("credential query parameter logged at -v: %s", out)
	}
	if !strings.Contains(out, "page=2") || !strings.Contains(out, "access_token=[REDACTED]") {
		t.Errorf("want the request line with only the credential redacted: %s", out)
	}
}
