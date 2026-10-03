// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"strings"
	"testing"
)

// The serial search's one result is accepted only when it reports the
// requested serial; another serial means the filter matched another device.
func TestResolveDeviceByIdentifier_SerialResultMustCarryTheSerial(t *testing.T) {
	cases := []struct {
		name   string
		result string
		wantID string
	}{
		{"matching serial", `{"id":"99","general":{"name":"Mac"},"hardware":{"serialNumber":"c02x1234"}}`, "99"},
		{"another serial", `{"id":"77","general":{"name":"Other Mac"},"hardware":{"serialNumber":"ZZZ999"}}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &deviceResolveMockClient{handler: func(_, path string) (int, string, error) {
				switch {
				case strings.HasPrefix(path, "/v4/computers-inventory-detail/"):
					return 404, `{}`, nil
				case strings.Contains(path, "hardware.serialNumber"):
					return 200, `{"totalCount":1,"results":[` + tc.result + `]}`, nil
				default:
					return 200, `{"totalCount":0,"results":[]}`, nil
				}
			}}
			id, _, err := resolveDeviceByIdentifier(context.Background(), client, "C02X1234")
			if tc.wantID != "" {
				if err != nil || id != tc.wantID {
					t.Fatalf("resolveDeviceByIdentifier = %q, %v; want %q", id, err, tc.wantID)
				}
				return
			}
			if err == nil {
				t.Errorf("resolved C02X1234 to device %s, whose serial is ZZZ999", id)
			}
		})
	}
}
