// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

type protectListFailure int

const (
	listAbsent protectListFailure = iota
	listForbidden
	listUnauthorized
	listServerError
	listGraphQLError
	listTimeout
)

func newFakeProtectServer(t *testing.T, mode protectListFailure, creates *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"tok","expires_in":3600,"token_type":"Bearer"}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal(body, &req)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(req.Query, "listPlans("):
			switch mode {
			case listAbsent:
				_, _ = io.WriteString(w, `{"data":{"listPlans":{"items":[],"pageInfo":{"next":null}}}}`)
			case listForbidden:
				w.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(w, `{"message":"Forbidden"}`)
			case listUnauthorized:
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"message":"Unauthorized"}`)
			case listServerError:
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, `{"message":"Internal Server Error"}`)
			case listGraphQLError:
				_, _ = io.WriteString(w, `{"data":null,"errors":[{"message":"Access denied: missing Read permission on Plans"}]}`)
			case listTimeout:
				select {
				case <-r.Context().Done():
				case <-time.After(2 * time.Second):
				}
			}
		case strings.Contains(req.Query, "createPlan("):
			creates.Add(1)
			_, _ = io.WriteString(w, `{"data":{"createPlan":{"id":"999","name":"Baseline"}}}`)
		default:
			t.Errorf("unexpected GraphQL operation: %.80s", req.Query)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Only a lookup that completed and found no plan may create one. Every other
// lookup failure must be returned, with no createPlan sent and no bypass of the
// confirmReplace gate (--no-input without --yes refuses a replace).
func TestProtectPlansApply_OnlyGenuineNotFoundCreates(t *testing.T) {
	oldNoInput, oldDryRun := noInput, dryRun
	noInput, dryRun = true, false
	t.Cleanup(func() { noInput, dryRun = oldNoInput, oldDryRun })

	doc := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(doc, []byte(`{"name":"Baseline","description":"d"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name       string
		mode       protectListFailure
		wantCreate bool
	}{
		{"genuine not found creates", listAbsent, true},
		{"403 on list is returned", listForbidden, false},
		{"401 on list is returned", listUnauthorized, false},
		{"500 on list is returned", listServerError, false},
		{"GraphQL error on list is returned", listGraphQLError, false},
		{"timeout on list is returned", listTimeout, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var creates atomic.Int32
			srv := newFakeProtectServer(t, tc.mode, &creates)
			client := jamfprotect.NewClient(srv.URL, "cid", "secret",
				jamfprotect.WithHTTPClient(&http.Client{Timeout: 300 * time.Millisecond}))
			formatter := output.New("json", true, false)
			formatter.SetWriter(io.Discard)
			cliCtx := &registry.CLIContext{ProtectClient: client, Output: &cliOutput{formatter}}

			cmd := newProtectPlansApplyCmd(cliCtx)
			cmd.SetArgs([]string{"--from-file", doc})
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			err := cmd.Execute()

			if tc.wantCreate {
				if err != nil {
					t.Fatalf("genuine not-found apply failed: %v", err)
				}
				if got := creates.Load(); got != 1 {
					t.Fatalf("createPlan sent %d times, want 1", got)
				}
				return
			}
			if got := creates.Load(); got != 0 {
				t.Errorf("lookup failed but createPlan was sent %d time(s): a failed read became an unconfirmed create", got)
			}
			if err == nil {
				t.Errorf("lookup failure was swallowed: apply returned nil")
			}
		})
	}
}
