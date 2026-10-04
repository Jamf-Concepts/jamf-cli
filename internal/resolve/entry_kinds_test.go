// Copyright 2026, Jamf Software LLC

package resolve

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func computerRecordFull(id, udid, mgmt, name, serial string) string {
	return fmt.Sprintf(`{"id":%q,"udid":%q,"general":{"name":%q,"managementId":%q},"hardware":{"serialNumber":%q}}`,
		id, udid, name, mgmt, serial)
}

func TestClassifyEntry(t *testing.T) {
	cases := map[string]entryKind{
		"42":                                       entryID,
		"96050be1-e53b-454d-9752-2306c709f192":     entryUUID,
		"96050BE1-E53B-454D-9752-2306C709F192":     entryUUID,
		"5f3644dc5303edf83f28a8d6070c1e955ba91af1": entryUDID,
		"00008030-001A2D3E0C38802E":                entryUDID,
		"C02X1234":                                 entrySerial,
		"ARMADA-2ACE61":                            entrySerial,
		"Neil's MacBook":                           entryName,
		`Lab "A", (B)`:                             entryName,
		"Lab*":                                     entryUnbatched,
	}
	for in, want := range cases {
		if got := classifyEntry(in); got != want {
			t.Errorf("classifyEntry(%q) = %d, want %d", in, got, want)
		}
	}
}

// A computer UDID and a management ID share the UUID shape, so a UUID entry is
// looked up as either in one request, and resolves whichever it turns out to be.
func TestResolveComputerEntries_UUIDMatchesUDIDOrManagementID(t *testing.T) {
	udid := "96050be1-e53b-454d-9752-2306c709f192"
	mgmt := "d384c00a-d9b1-4c87-9008-de91e1f5084b"
	client := &mockClient{responses: map[string]mockResponse{
		`filter=udid=in=`: {200, fmt.Sprintf(`{"totalCount":2,"results":[%s,%s]}`,
			computerRecordFull("107", udid, "aaaaaaaa-0000-4000-8000-000000000000", "A", "S1"),
			computerRecordFull("88", "bbbbbbbb-0000-4000-8000-000000000000", mgmt, "B", "S2"))},
	}}

	got, skipped, err := ResolveComputerEntries(context.Background(), client, []string{udid, strings.ToUpper(mgmt)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skipped != 0 || len(got) != 2 || got[0].ID != "107" || got[1].ID != "88" {
		t.Fatalf("got %v / %d skipped, want [107 88] / 0", ids(got), skipped)
	}
	if len(client.calls) != 1 {
		t.Fatalf("made %d requests, want 1: %v", len(client.calls), client.calls)
	}
	if !strings.Contains(client.calls[0], "general.managementId=in=") {
		t.Errorf("UUID lookup did not also query the management ID: %s", client.calls[0])
	}
	if strings.Contains(client.calls[0], "serialNumber") {
		t.Errorf("UUID entry was looked up as a serial number: %s", client.calls[0])
	}
}

// A 40-hex mobile UDID is not a serial number, and is looked up by UDID alone.
func TestResolveMobileDeviceEntries_HexUDID(t *testing.T) {
	udid := "5f3644dc5303edf83f28a8d6070c1e955ba91af1"
	client := &mockClient{responses: map[string]mockResponse{
		`filter=udid=in=("` + udid + `")`: {
			200,
			`{"totalCount":1,"results":[{"mobileDeviceId":"64","general":{"displayName":"iPad","udid":"` + udid + `","managementId":"m"},"hardware":{"serialNumber":"GMJR66B185"}}]}`,
		},
	}}
	got, skipped, err := ResolveMobileDeviceEntries(context.Background(), client, []string{udid})
	if err != nil || skipped != 0 || len(got) != 1 || got[0].ID != "64" {
		t.Fatalf("got %v / %d / %v, want [64] / 0 / nil", ids(got), skipped, err)
	}
	if strings.Contains(client.calls[0], "serialNumber") {
		t.Errorf("UDID entry was looked up as a serial number: %s", client.calls[0])
	}
}

// A list-safe entry no serial matched is tried as a name — batched, not one
// request per line.
func TestResolveComputerEntries_SerialMissFallsBackToName(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		`filter=hardware.serialNumber=in=`: {200, `{"totalCount":0,"results":[]}`},
		`filter=general.name=in=("ARMADA-2ACE61","armada-0j6av3")`: {200, fmt.Sprintf(`{"totalCount":2,"results":[%s,%s]}`,
			computerRecordFull("117", "u1", "m1", "ARMADA-2ACE61", "C8AS2ACE61"),
			computerRecordFull("84", "u2", "m2", "ARMADA-0J6AV3", "6TBA0J6AV3"))},
	}}
	got, skipped, err := ResolveComputerEntries(context.Background(), client, []string{"ARMADA-2ACE61", "armada-0j6av3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skipped != 0 || len(got) != 2 || got[0].ID != "117" || got[1].ID != "84" {
		t.Fatalf("got %v / %d skipped, want [117 84] / 0", ids(got), skipped)
	}
	if len(client.calls) != 2 {
		t.Errorf("made %d requests, want 2 (serial batch, name batch): %v", len(client.calls), client.calls)
	}
}

// A serial match wins: the name query is only for what no serial matched.
func TestResolveComputerEntries_SerialMatchSkipsNameQuery(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		`filter=hardware.serialNumber=in=("C02X1234")`: {200, computerV3Response},
	}}
	if _, _, err := ResolveComputerEntries(context.Background(), client, []string{"C02X1234"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, c := range client.calls {
		if strings.Contains(c, "general.name") {
			t.Errorf("name was queried for an entry a serial already matched: %s", c)
		}
	}
}

// Jamf Pro names are not unique. An entry naming two devices is refused with
// both IDs rather than resolved to the first.
func TestResolveComputerEntries_AmbiguousNameRefused(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		`filter=hardware.serialNumber=in=`: {200, `{"totalCount":0,"results":[]}`},
		`filter=general.name=in=`: {200, fmt.Sprintf(`{"totalCount":2,"results":[%s,%s]}`,
			computerRecordFull("1", "u1", "m1", "Lab", "S1"),
			computerRecordFull("2", "u2", "m2", "lab", "S2"))},
		`filter=id=in=(5)`: {200, fmt.Sprintf(`{"totalCount":1,"results":[%s]}`, computerRecordFull("5", "u5", "m5", "Five", "S5"))},
	}}
	got, skipped, err := ResolveComputerEntries(context.Background(), client, []string{"Lab", "5"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skipped != 1 || len(got) != 1 || got[0].ID != "5" {
		t.Fatalf("got %v / %d skipped, want [5] / 1", ids(got), skipped)
	}

	r := pickEntry(computerEntrySpec, "Lab", "name", []*DeviceIdentifiers{{ID: "1"}, {ID: "2"}})
	var ae *AmbiguousEntryError
	if !errors.As(r.err, &ae) || strings.Join(ae.IDs, ",") != "1,2" {
		t.Errorf("pickEntry err = %v, want an AmbiguousEntryError naming ids 1,2", r.err)
	}
}

// Entries in hand obey the same no-silent-no-op rule as a file.
func TestResolveComputerEntries_NoneResolvedErrors(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		`filter=id=in=(999)`: {200, `{"totalCount":0,"results":[]}`},
	}}
	_, skipped, err := ResolveComputerEntries(context.Background(), client, []string{"999"})
	if err == nil || !strings.Contains(err.Error(), "none of the 1 computer identifiers") {
		t.Fatalf("err = %v, want the none-resolved error", err)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
	if _, _, err := ResolveComputerEntries(context.Background(), client, []string{" ", ""}); err == nil {
		t.Error("blank entries were accepted as a target list")
	}
}

func ids(ds []*DeviceIdentifiers) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.ID
	}
	return out
}
