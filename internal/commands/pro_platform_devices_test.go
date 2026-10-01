// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/platform"
	"github.com/jamf/jamfplatform-go-sdk/jamfplatform/devices"
)

// serveDevices answers the device list with rows and records each filter sent.
func serveDevices(mux *http.ServeMux, rows []devices.DeviceListReadRepresentationV1) *[]string {
	var filters []string
	mux.HandleFunc("/devices/v1/devices", func(w http.ResponseWriter, r *http.Request) {
		filters = append(filters, r.URL.Query().Get("filter"))
		writeJSON(w, map[string]any{"results": rows, "totalCount": len(rows)})
	})
	return &filters
}

func TestResolveDeviceIDDirect_ResolvesExactlyOneSerial(t *testing.T) {
	cases := []struct {
		name       string
		arg        string
		rows       []devices.DeviceListReadRepresentationV1
		wantFilter string
		wantID     string
		wantErr    string
	}{
		{
			name:       "wildcard matching several is refused",
			arg:        "C02*",
			rows:       []devices.DeviceListReadRepresentationV1{{ID: "d-1", SerialNumber: "C02AAA"}, {ID: "d-2", SerialNumber: "C02BBB"}},
			wantFilter: `serialNumber=="C02*"`,
			wantErr:    "not found",
		},
		{
			name:       "two devices sharing the serial are refused",
			arg:        "DUP1",
			rows:       []devices.DeviceListReadRepresentationV1{{ID: "d-1", SerialNumber: "DUP1"}, {ID: "d-2", SerialNumber: "dup1"}},
			wantFilter: `serialNumber=="DUP1"`,
			wantErr:    "d-1, d-2",
		},
		{
			name:       "quote and backslash are RSQL-escaped, not Go-quoted",
			arg:        "AB\"C\\D\tE",
			rows:       nil,
			wantFilter: "serialNumber==\"AB\\\"C\\\\D\tE\"",
			wantErr:    "not found",
		},
		{
			name:       "one case-insensitive match resolves",
			arg:        "c02xyz",
			rows:       []devices.DeviceListReadRepresentationV1{{ID: "d-9", Name: "Lab Mac", SerialNumber: "C02XYZ"}},
			wantFilter: `serialNumber=="c02xyz"`,
			wantID:     "d-9",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, mux := newTestPlatformSDK(t)
			filters := serveDevices(mux, tc.rows)

			id, label, err := resolveDeviceIDDirect(context.Background(), sdk, tc.arg)
			if len(*filters) != 1 || (*filters)[0] != tc.wantFilter {
				t.Errorf("filters sent = %q, want [%q]", *filters, tc.wantFilter)
			}
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want it to contain %q (resolved %q)", err, tc.wantErr, id)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id != tc.wantID || !strings.Contains(label, "Lab Mac") || !strings.Contains(label, "C02XYZ") {
				t.Errorf("resolved (%q, %q), want %s labelled with its name and serial", id, label, tc.wantID)
			}
		})
	}
}

func TestResolveDeviceIDDirect_NotFoundIsErrNotFound(t *testing.T) {
	sdk, mux := newTestPlatformSDK(t)
	serveDevices(mux, nil)
	if _, _, err := resolveDeviceIDDirect(context.Background(), sdk, "NOPE"); !errors.Is(err, platform.ErrNotFound) {
		t.Errorf("err = %v, want platform.ErrNotFound", err)
	}
}

func TestPlatformDevicesDelete_ConfirmationNamesTheResolvedDevice(t *testing.T) {
	resetGlobals()
	dryRun = true
	defer func() { dryRun = false }()

	cliCtx, mux, _ := newTestPlatformContext(t)
	serveDevices(mux, []devices.DeviceListReadRepresentationV1{{ID: "d-9", Name: "Lab Mac", SerialNumber: "C02XYZ"}})
	mux.HandleFunc("/devices/v1/devices/d-9", func(w http.ResponseWriter, _ *http.Request) {
		t.Error("dry run sent the delete")
		w.WriteHeader(http.StatusNoContent)
	})

	cmd := newPlatformDevicesDeleteCmd(cliCtx)
	cmd.SetArgs([]string{"c02xyz"})
	var runErr error
	stderr := captureStderr(t, func() { runErr = cmd.Execute() })
	if runErr != nil {
		t.Fatalf("delete: %v", runErr)
	}
	if !strings.Contains(stderr, "Lab Mac (serial C02XYZ, id d-9)") {
		t.Errorf("confirmation = %q, want it to name the resolved device", stderr)
	}
}
