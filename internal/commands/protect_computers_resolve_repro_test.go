// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	jamfprotect "github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
	"github.com/spf13/cobra"
)

type fakeProtectComputer struct {
	UUID, Serial, HostName string
}

type fakeProtectServer struct {
	mu        sync.Mutex
	computers []fakeProtectComputer
	targeted  []string // "<operation> <uuid>" for every per-computer call
}

func (f *fakeProtectServer) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
	})
	gql := func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decoding graphql request: %v", err)
			return
		}
		computerJSON := func(uuid string) map[string]any {
			for _, c := range f.computers {
				if c.UUID == uuid {
					return map[string]any{"uuid": c.UUID, "serial": c.Serial, "hostName": c.HostName}
				}
			}
			return map[string]any{"uuid": uuid}
		}
		data := map[string]any{}
		switch {
		case strings.Contains(req.Query, "query listComputers"):
			items := []any{}
			for _, c := range f.computers {
				items = append(items, computerJSON(c.UUID))
			}
			data["listComputers"] = map[string]any{"items": items, "pageInfo": map[string]any{"next": nil, "total": len(items)}}
		case strings.Contains(req.Query, "query listPlans"):
			data["listPlans"] = map[string]any{
				"items":    []any{map[string]any{"id": "plan-1", "name": "Strict"}},
				"pageInfo": map[string]any{"next": nil, "total": 1},
			}
		default:
			for _, op := range []string{"getComputer", "deleteComputer", "setComputerPlan", "updateComputer"} {
				if strings.Contains(req.Query, "query "+op) || strings.Contains(req.Query, "mutation "+op) {
					uuid, _ := req.Variables["uuid"].(string)
					f.mu.Lock()
					f.targeted = append(f.targeted, op+" "+uuid)
					f.mu.Unlock()
					data[op] = computerJSON(uuid)
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}
	mux.HandleFunc("/app", gql)
	mux.HandleFunc("/graphql", gql)
	return mux
}

type computerCmdCase struct {
	name  string
	op    string
	build func(*registry.CLIContext) *cobra.Command
	args  func(target string) []string
	flags map[string]string
}

func computerCmdCases() []computerCmdCase {
	one := func(target string) []string { return []string{target} }
	return []computerCmdCase{
		{"get", "getComputer", newProtectComputersGetCmd, one, nil},
		{"delete", "deleteComputer", newProtectComputersDeleteCmd, one, map[string]string{"yes": "true"}},
		{"set-plan", "setComputerPlan", newProtectComputersSetPlanCmd, func(target string) []string { return []string{target, "Strict"} }, nil},
		{"update", "updateComputer", newProtectComputersUpdateCmd, one, map[string]string{"label": "x"}},
	}
}

func runComputerCmd(t *testing.T, computers []fakeProtectComputer, tc computerCmdCase, target string) ([]string, error) {
	t.Helper()
	fake := &fakeProtectServer{computers: computers}
	srv := httptest.NewServer(fake.handler(t))
	t.Cleanup(srv.Close)
	pc := jamfprotect.NewClient(srv.URL, "id", "secret", jamfprotect.WithHTTPClient(srv.Client()))
	cliCtx := &registry.CLIContext{Output: &captureOutput{}, ProtectClient: pc}
	cmd := tc.build(cliCtx)
	for k, v := range tc.flags {
		if err := cmd.Flags().Set(k, v); err != nil {
			t.Fatalf("setting --%s: %v", k, err)
		}
	}
	cmd.SetContext(context.Background())
	err := cmd.RunE(cmd, tc.args(target))
	return fake.targeted, err
}

// A device whose endpoint-set hostname equals another device's serial must not
// capture a lookup by that serial.
func TestProtectComputerResolve_SerialIsNotShadowedByHostname(t *testing.T) {
	computers := []fakeProtectComputer{
		{UUID: "uuid-victim", Serial: "C02VICTIM01", HostName: "victim-mac"},
		{UUID: "uuid-attacker", Serial: "C02ATTACK99", HostName: "C02VICTIM01"},
	}
	for _, tc := range computerCmdCases() {
		t.Run(tc.name, func(t *testing.T) {
			targeted, err := runComputerCmd(t, computers, tc, "C02VICTIM01")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want := tc.op + " uuid-victim"
			if len(targeted) != 1 || targeted[0] != want {
				t.Fatalf("%s C02VICTIM01 targeted %v, want [%s]", tc.name, targeted, want)
			}
		})
	}
}

// Two devices sharing a hostname must be refused, naming both candidates, and
// nothing may be sent for either.
func TestProtectComputerResolve_AmbiguousHostnameIsRefused(t *testing.T) {
	computers := []fakeProtectComputer{
		{UUID: "uuid-first", Serial: "C02FIRST001", HostName: "MacBook Pro"},
		{UUID: "uuid-second", Serial: "C02SECOND02", HostName: "MacBook Pro"},
	}
	for _, tc := range computerCmdCases() {
		t.Run(tc.name, func(t *testing.T) {
			targeted, err := runComputerCmd(t, computers, tc, "MacBook Pro")
			if len(targeted) != 0 {
				t.Errorf("%s \"MacBook Pro\" acted on %v; an ambiguous hostname must send nothing", tc.name, targeted)
			}
			if err == nil {
				t.Fatalf("%s \"MacBook Pro\" succeeded; want a refusal naming both candidates", tc.name)
			}
			for _, c := range computers {
				if !strings.Contains(err.Error(), c.UUID) || !strings.Contains(err.Error(), c.Serial) {
					t.Errorf("refusal %q does not name candidate %s (%s)", err, c.UUID, c.Serial)
				}
			}
		})
	}
}

// A UUID is the exact identifier and wins over both serial and hostname.
func TestProtectComputerResolve_UUIDIsAccepted(t *testing.T) {
	computers := []fakeProtectComputer{
		{UUID: "uuid-a", Serial: "C02AAAAAAA1", HostName: "mac-a"},
		{UUID: "uuid-b", Serial: "C02BBBBBBB2", HostName: "uuid-a"},
	}
	for _, tc := range computerCmdCases() {
		t.Run(tc.name, func(t *testing.T) {
			targeted, err := runComputerCmd(t, computers, tc, "uuid-a")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want := tc.op + " uuid-a"
			if len(targeted) != 1 || targeted[0] != want {
				t.Fatalf("%s uuid-a targeted %v, want [%s]", tc.name, targeted, want)
			}
		})
	}
}
