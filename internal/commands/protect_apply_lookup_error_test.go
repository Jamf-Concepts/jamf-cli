// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

type protectListFailure int

const (
	listAbsent protectListFailure = iota
	listDuplicate
	listForbidden
	listUnauthorized
	listServerError
	listGraphQLError
	listTimeout
)

type protectListMode struct {
	name string
	mode protectListFailure
}

var protectListFailures = []protectListMode{
	{"403", listForbidden},
	{"401", listUnauthorized},
	{"500", listServerError},
	{"GraphQL error", listGraphQLError},
	{"timeout", listTimeout},
}

// protectOps names the GraphQL list query a lookup sends and the mutation a
// create sends.
type protectOps struct{ list, create string }

// protectRecord carries every identifier field the SDK's list types decode, so
// one item shape serves every resource.
func protectRecord(id string) string {
	return fmt.Sprintf(`{"id":%q,"uuid":%q,"clientId":%q,"name":"Baseline","email":"Baseline"}`, id, id, id)
}

// newFakeProtectServer answers ops.list according to mode and counts ops.create.
func newFakeProtectServer(t *testing.T, ops protectOps, mode protectListFailure, creates *atomic.Int32) *httptest.Server {
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
		case namesOperation(req.Query, "query", ops.list):
			switch mode {
			case listAbsent:
				_, _ = fmt.Fprintf(w, `{"data":{%q:{"items":[],"pageInfo":{"next":null}}}}`, ops.list)
			case listDuplicate:
				_, _ = fmt.Fprintf(w, `{"data":{%q:{"items":[%s,%s],"pageInfo":{"next":null}}}}`, ops.list, protectRecord("dup-1"), protectRecord("dup-2"))
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
				_, _ = io.WriteString(w, `{"data":null,"errors":[{"message":"Access denied: missing Read permission"}]}`)
			case listTimeout:
				select {
				case <-r.Context().Done():
				case <-time.After(2 * time.Second):
				}
			}
		case ops.create != "" && namesOperation(req.Query, "mutation", ops.create):
			creates.Add(1)
			_, _ = fmt.Fprintf(w, `{"data":{%q:%s}}`, ops.create, protectRecord("999"))
		default:
			t.Errorf("unexpected GraphQL operation: %.80s", req.Query)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// namesOperation reports whether a GraphQL document declares the operation
// kind name, matched as a whole word so createUnifiedLoggingFilter does not
// match createUnifiedLoggingFilterSet.
func namesOperation(query, kind, name string) bool {
	return regexp.MustCompile(`\b` + kind + `\s+` + regexp.QuoteMeta(name) + `\b`).MatchString(query)
}

func newFakeProtectContext(t *testing.T, ops protectOps, mode protectListFailure, creates *atomic.Int32) *registry.CLIContext {
	t.Helper()
	srv := newFakeProtectServer(t, ops, mode, creates)
	client := jamfprotect.NewClient(srv.URL, "cid", "secret",
		jamfprotect.WithHTTPClient(&http.Client{Timeout: 300 * time.Millisecond}))
	formatter := output.New("json", true, false)
	formatter.SetWriter(io.Discard)
	return &registry.CLIContext{ProtectClient: client, Output: &cliOutput{formatter}}
}

var protectApplies = []struct {
	name string
	ctor func(*registry.CLIContext) *cobra.Command
	ops  protectOps
}{
	{"action-configs", newProtectActionConfigsApplyCmd, protectOps{"listActionConfigs", "createActionConfigs"}},
	{"analytic-sets", newProtectAnalyticSetsApplyCmd, protectOps{"listAnalyticSets", "createAnalyticSet"}},
	{"api-clients", newProtectApiClientsApplyCmd, protectOps{"listApiClients", "createApiClient"}},
	{"exception-sets", newProtectExceptionSetsApplyCmd, protectOps{"listExceptionSets", "createExceptionSet"}},
	{"groups", newProtectGroupsApplyCmd, protectOps{"listGroups", "createGroup"}},
	{"plans", newProtectPlansApplyCmd, protectOps{"listPlans", "createPlan"}},
	{"custom-prevent-lists", newProtectPreventListsApplyCmd, protectOps{"listPreventLists", "createPreventList"}},
	{"removable-storage-control-sets", newProtectRSCSApplyCmd, protectOps{"listUSBControlSets", "createUSBControlSet"}},
	{"roles", newProtectRolesApplyCmd, protectOps{"listRoles", "createRole"}},
	{"telemetry", newProtectTelemetryApplyCmd, protectOps{"listTelemetriesV2", "createTelemetryV2"}},
	{"unified-logging-filters", newProtectULFApplyCmd, protectOps{"listUnifiedLoggingFilters", "createUnifiedLoggingFilter"}},
	{"unified-logging-filter-sets", newProtectULFSetsApplyCmd, protectOps{"listUnifiedLoggingFilterSets", "createUnifiedLoggingFilterSet"}},
	{"users", newProtectUsersApplyCmd, protectOps{"listUsers", "createUser"}},
}

func runProtectApply(t *testing.T, ctor func(*registry.CLIContext) *cobra.Command, cliCtx *registry.CLIContext) error {
	t.Helper()
	oldNoInput, oldDryRun := noInput, dryRun
	noInput, dryRun = true, false
	t.Cleanup(func() { noInput, dryRun = oldNoInput, oldDryRun })

	doc := filepath.Join(t.TempDir(), "doc.json")
	if err := os.WriteFile(doc, []byte(`{"name":"Baseline","email":"Baseline","description":"d"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := ctor(cliCtx)
	cmd.SetArgs([]string{"--from-file", doc})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd.Execute()
}

// Only a lookup that completed and found no plan may create one. Every other
// lookup failure must be returned, with no createPlan sent and no bypass of the
// confirmReplace gate (--no-input without --yes refuses a replace).
func TestProtectPlansApply_OnlyGenuineNotFoundCreates(t *testing.T) {
	var creates atomic.Int32
	cliCtx := newFakeProtectContext(t, protectOps{"listPlans", "createPlan"}, listAbsent, &creates)
	if err := runProtectApply(t, newProtectPlansApplyCmd, cliCtx); err != nil {
		t.Fatalf("genuine not-found apply failed: %v", err)
	}
	if got := creates.Load(); got != 1 {
		t.Fatalf("createPlan sent %d times, want 1", got)
	}
}

// Every Protect apply creates only when the lookup completed and found no
// record. A failed lookup, or a name two records share, is returned with no
// create sent.
func TestProtectApply_LookupFailureCreatesNothing(t *testing.T) {
	modes := append([]protectListMode{{"duplicate name", listDuplicate}}, protectListFailures...)
	for _, a := range protectApplies {
		for _, m := range modes {
			t.Run(a.name+"/"+m.name, func(t *testing.T) {
				var creates atomic.Int32
				err := runProtectApply(t, a.ctor, newFakeProtectContext(t, a.ops, m.mode, &creates))
				if got := creates.Load(); got != 0 {
					t.Errorf("%s sent %d time(s) after a %s lookup: a failed read became a create", a.ops.create, got, m.name)
				}
				if err == nil {
					t.Errorf("a %s lookup was swallowed: apply returned nil", m.name)
				}
			})
		}
	}
}

// Every name resolver tells "absent" from "the lookup failed": an empty list
// matches protect.ErrNotFound, a failed list does not, and a name two records
// share is refused with both ids and does not match it either.
func TestProtectResolver_NotFoundOnlyWhenTheListCompletes(t *testing.T) {
	resolvers := []struct {
		list    string
		resolve func(*protect.Resolver, context.Context, string) (string, error)
	}{
		{"listPlans", (*protect.Resolver).ResolvePlanID},
		{"listAnalytics", (*protect.Resolver).ResolveAnalyticUUID},
		{"listAnalyticSets", (*protect.Resolver).ResolveAnalyticSetUUID},
		{"listExceptionSets", (*protect.Resolver).ResolveExceptionSetUUID},
		{"listUSBControlSets", (*protect.Resolver).ResolveRemovableStorageControlSetID},
		{"listActionConfigs", (*protect.Resolver).ResolveActionConfigID},
		{"listTelemetriesV2", (*protect.Resolver).ResolveTelemetryV2ID},
		{"listPreventLists", (*protect.Resolver).ResolveCustomPreventListID},
		{"listUnifiedLoggingFilters", (*protect.Resolver).ResolveUnifiedLoggingFilterUUID},
		{"listUnifiedLoggingFilterSets", (*protect.Resolver).ResolveUnifiedLoggingFilterSetUUID},
		{"listRoles", (*protect.Resolver).ResolveRoleID},
		{"listUsers", (*protect.Resolver).ResolveUserID},
		{"listGroups", (*protect.Resolver).ResolveGroupID},
		{"listApiClients", (*protect.Resolver).ResolveApiClientID},
	}
	for _, rv := range resolvers {
		t.Run(rv.list+"/empty", func(t *testing.T) {
			var creates atomic.Int32
			cliCtx := newFakeProtectContext(t, protectOps{list: rv.list}, listAbsent, &creates)
			_, err := rv.resolve(protect.NewResolver(cliCtx.ProtectClient), context.Background(), "Baseline")
			if !errors.Is(err, protect.ErrNotFound) {
				t.Errorf("empty list: err = %v; want one matching protect.ErrNotFound", err)
			}
		})
		t.Run(rv.list+"/duplicate", func(t *testing.T) {
			var creates atomic.Int32
			cliCtx := newFakeProtectContext(t, protectOps{list: rv.list}, listDuplicate, &creates)
			id, err := rv.resolve(protect.NewResolver(cliCtx.ProtectClient), context.Background(), "Baseline")
			if err == nil || errors.Is(err, protect.ErrNotFound) {
				t.Fatalf("two records named Baseline: id %q, err %v; want a refusal that is not ErrNotFound", id, err)
			}
			for _, want := range []string{"dup-1", "dup-2"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal %q does not name %s", err, want)
				}
			}
		})
		for _, m := range protectListFailures {
			t.Run(rv.list+"/"+m.name, func(t *testing.T) {
				var creates atomic.Int32
				cliCtx := newFakeProtectContext(t, protectOps{list: rv.list}, m.mode, &creates)
				_, err := rv.resolve(protect.NewResolver(cliCtx.ProtectClient), context.Background(), "Baseline")
				if err == nil || errors.Is(err, protect.ErrNotFound) {
					t.Errorf("%s on list: err = %v; want an error that is not ErrNotFound", m.name, err)
				}
			})
		}
	}
}

// An import upserts by name, so a name two records share is refused before
// anything is created or updated.
func TestProtectImport_DuplicateNameIsRefused(t *testing.T) {
	imports := []struct {
		name string
		ctor func(*registry.CLIContext) *cobra.Command
		list string
	}{
		{"analytics", newProtectAnalyticsImportCmd, "listAnalytics"},
		{"unified-logging-filters", newProtectULFImportCmd, "listUnifiedLoggingFilters"},
	}
	for _, im := range imports {
		t.Run(im.name, func(t *testing.T) {
			var creates atomic.Int32
			cliCtx := newFakeProtectContext(t, protectOps{list: im.list}, listDuplicate, &creates)
			doc := filepath.Join(t.TempDir(), "doc.yaml")
			if err := os.WriteFile(doc, []byte("name: Baseline\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := im.ctor(cliCtx)
			cmd.SetArgs([]string{"--file", doc})
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "dup-1") || !strings.Contains(err.Error(), "dup-2") {
				t.Errorf("import of a name two records share: err = %v; want a refusal naming dup-1 and dup-2", err)
			}
		})
	}
}
