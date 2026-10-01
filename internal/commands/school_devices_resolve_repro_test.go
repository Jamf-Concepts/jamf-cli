// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfschool-go-sdk/jamfschool"
)

type fakeSchoolServer struct {
	mu        sync.Mutex
	mutations []string
}

func newFakeSchoolServer(t *testing.T, devices []jamfschool.Device) (*fakeSchoolServer, *registry.CLIContext) {
	t.Helper()
	f := &fakeSchoolServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/api/devices" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "count": len(devices), "devices": devices})
			return
		}
		f.mu.Lock()
		f.mutations = append(f.mutations, r.Method+" "+r.URL.Path)
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{"code":200}`))
	}))
	t.Cleanup(srv.Close)
	return f, &registry.CLIContext{SchoolClient: jamfschool.NewClient(srv.URL, "1", "test")}
}

func (f *fakeSchoolServer) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.mutations...)
}

type schoolDeviceAction struct {
	name  string
	build func(*registry.CLIContext) *cobra.Command
	flags []string
}

var schoolDeviceActions = []schoolDeviceAction{
	{"erase", newSchoolDevicesEraseCmd, []string{"--yes", "--clear-activation-lock"}},
	{"unenroll", newSchoolDevicesUnenrollCmd, []string{"--yes"}},
	{"trash", newSchoolDevicesTrashCmd, []string{"--yes"}},
	{"clear-activation-lock", newSchoolDevicesClearActivationLockCmd, []string{"--yes"}},
	{"restore", newSchoolDevicesRestoreCmd, nil},
	{"restart", newSchoolDevicesRestartCmd, []string{"--yes"}},
	{"refresh", newSchoolDevicesRefreshCmd, nil},
}

func runSchoolDeviceAction(t *testing.T, a schoolDeviceAction, cliCtx *registry.CLIContext, target string) error {
	t.Helper()
	cmd := a.build(cliCtx)
	cmd.SetArgs(append([]string{target}, a.flags...))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.ExecuteContext(context.Background())
}

func TestSchoolDeviceAction_ExactSerialOrUDIDBeatsAShadowingName(t *testing.T) {
	victim := jamfschool.Device{UDID: "VICTIM-UDID-0001", SerialNumber: "VICTIMSN01", Name: "Retiring iPad"}
	cases := []struct {
		label  string
		target string
	}{
		{"serial", victim.SerialNumber},
		{"udid", victim.UDID},
	}
	for _, a := range schoolDeviceActions {
		for _, tc := range cases {
			t.Run(a.name+"/"+tc.label, func(t *testing.T) {
				attacker := jamfschool.Device{UDID: "ATTACKER-UDID-0002", SerialNumber: "ATTACKSN02", Name: tc.target}
				f, cliCtx := newFakeSchoolServer(t, []jamfschool.Device{victim, attacker})

				if err := runSchoolDeviceAction(t, a, cliCtx, tc.target); err != nil {
					t.Fatalf("%s %s: %v", a.name, tc.target, err)
				}
				sent := f.sent()
				if len(sent) != 1 || !strings.Contains(sent[0], victim.UDID) {
					t.Errorf("%s %s (the %s of %q) sent %v; want exactly one request to %s, not to the device named %q",
						a.name, tc.target, tc.label, victim.Name, sent, victim.UDID, tc.target)
				}
			})
		}
	}
}

func TestSchoolDeviceAction_AmbiguousNameIsRefusedNamingTheCandidates(t *testing.T) {
	devices := []jamfschool.Device{
		{UDID: "CART-UDID-AAAA", SerialNumber: "CARTSNA", Name: "Cart iPad"},
		{UDID: "CART-UDID-BBBB", SerialNumber: "CARTSNB", Name: "Cart iPad"},
	}
	for _, a := range schoolDeviceActions {
		t.Run(a.name, func(t *testing.T) {
			f, cliCtx := newFakeSchoolServer(t, devices)

			err := runSchoolDeviceAction(t, a, cliCtx, "Cart iPad")
			if sent := f.sent(); len(sent) != 0 {
				t.Errorf("%s %q sent %v; an ambiguous name must send nothing", a.name, "Cart iPad", sent)
			}
			if err == nil {
				t.Fatalf("%s %q succeeded; want a refusal naming both candidates", a.name, "Cart iPad")
			}
			for _, d := range devices {
				if !strings.Contains(err.Error(), d.UDID) {
					t.Errorf("refusal %q does not name candidate %s", err, d.UDID)
				}
			}
		})
	}
}
