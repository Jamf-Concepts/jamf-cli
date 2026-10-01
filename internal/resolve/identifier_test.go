// Copyright 2026, Jamf Software LLC

package resolve

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func inventoryPage(records ...string) string {
	return fmt.Sprintf(`{"totalCount":%d,"results":[%s]}`, len(records), strings.Join(records, ","))
}

// mobileDetailRecord is the /v2/mobile-devices/detail shape as the wire sends
// it with section=GENERAL&section=HARDWARE: udid and displayName under
// general, serialNumber under hardware, nothing at the top level but the id.
func mobileDetailRecord(id, name, udid, serial string) string {
	return fmt.Sprintf(`{"mobileDeviceId":%q,"general":{"displayName":%q,"udid":%q,"managementId":"m-%s"},"hardware":{"serialNumber":%q}}`,
		id, name, udid, id, serial)
}

func TestResolveComputerIdentifier_EachIdentifierKind(t *testing.T) {
	rec := `{"id":"107","udid":"96050be1-e53b-454d-9752-2306c709f192","general":{"name":"ARMADA-058JG5"},"hardware":{"serialNumber":"FWWT058JG5"}}`
	for _, value := range []string{"107", "armada-058jg5", "96050BE1-E53B-454D-9752-2306C709F192", "fwwt058jg5"} {
		client := &mockClient{responses: map[string]mockResponse{"v4/computers-inventory?": {200, inventoryPage(rec)}}}
		d, err := ResolveComputerIdentifier(context.Background(), client, value)
		if err != nil {
			t.Fatalf("%s: %v", value, err)
		}
		if d.ID != "107" {
			t.Errorf("%s: ID = %q, want 107", value, d.ID)
		}
		if len(client.calls) != 1 {
			t.Errorf("%s: want one request, got %v", value, client.calls)
		}
	}
}

func TestResolveComputerIdentifier_FilterORsEveryField(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{"v4/computers-inventory?": {200, inventoryPage(computerRecord("7"))}}}
	if _, err := ResolveComputerIdentifier(context.Background(), client, "7"); err != nil {
		t.Fatal(err)
	}
	want := `filter=id==7,general.name=="7",udid=="7",hardware.serialNumber=="7"`
	if !strings.Contains(client.calls[0], want) {
		t.Errorf("request %q does not carry %s", client.calls[0], want)
	}

	client = &mockClient{responses: map[string]mockResponse{"v4/computers-inventory?": {200, inventoryPage(computerRecord("7"))}}}
	_, _ = ResolveComputerIdentifier(context.Background(), client, "SER7")
	if strings.Contains(client.calls[0], "filter=id==") {
		t.Errorf("a non-numeric value must not be compared against the numeric id field: %s", client.calls[0])
	}
}

// A numeric value that is a record's ID wins over another record that carries
// the same digits as its name: the numeric reading is the documented contract.
func TestResolveComputerIdentifier_IDWinsOverNumericName(t *testing.T) {
	named := `{"id":"9","udid":"u9","general":{"name":"42"},"hardware":{"serialNumber":"S9"}}`
	client := &mockClient{responses: map[string]mockResponse{"v4/computers-inventory?": {200, inventoryPage(named, computerRecord("42"))}}}
	d, err := ResolveComputerIdentifier(context.Background(), client, "42")
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != "42" {
		t.Errorf("ID = %q, want 42", d.ID)
	}
}

func TestResolveComputerIdentifier_DuplicateNameIsRefused(t *testing.T) {
	a := `{"id":"4","udid":"u4","general":{"name":"FVFZCAK0LYWH"},"hardware":{"serialNumber":"FVFZCAK0LYWH"}}`
	b := `{"id":"31","udid":"u31","general":{"name":"FVFZCAK0LYWH"},"hardware":{"serialNumber":"OTHER"}}`
	client := &mockClient{responses: map[string]mockResponse{"v4/computers-inventory?": {200, inventoryPage(a, b)}}}
	_, err := ResolveComputerIdentifier(context.Background(), client, "FVFZCAK0LYWH")
	if err == nil {
		t.Fatal("expected an ambiguity error")
	}
	for _, want := range []string{"id 4", "id 31", "numeric ID"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if errors.Is(err, ErrNoDeviceMatch) {
		t.Error("an ambiguous value is not a missing one")
	}
}

// RSQL == treats * as a wildcard, so a server-side match is re-checked exactly.
func TestResolveComputerIdentifier_WildcardMatchIsNotExact(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{"v4/computers-inventory?": {200, inventoryPage(computerRecord("1"), computerRecord("2"))}}}
	_, err := ResolveComputerIdentifier(context.Background(), client, "mac-*")
	if !errors.Is(err, ErrNoDeviceMatch) {
		t.Fatalf("want ErrNoDeviceMatch, got %v", err)
	}
}

func TestResolveComputerIdentifier_NotFoundAndLookupFailure(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{"v4/computers-inventory?": {200, inventoryPage()}}}
	if _, err := ResolveComputerIdentifier(context.Background(), client, "nope"); !errors.Is(err, ErrNoDeviceMatch) {
		t.Errorf("want ErrNoDeviceMatch, got %v", err)
	}
	client = &mockClient{responses: map[string]mockResponse{"v4/computers-inventory?": {403, `{}`}}}
	_, err := ResolveComputerIdentifier(context.Background(), client, "nope")
	if err == nil || errors.Is(err, ErrNoDeviceMatch) {
		t.Errorf("a failed lookup must not read as no match: %v", err)
	}
}

func TestResolveMobileDeviceIdentifier_DetailShape(t *testing.T) {
	rec := mobileDetailRecord("64", "ARMADA-66B185", "5f3644dc5303edf83f28a8d6070c1e955ba91af1", "GMJR66B185")
	for _, value := range []string{"64", "armada-66b185", "5F3644DC5303EDF83F28A8D6070C1E955BA91AF1", "gmjr66b185"} {
		client := &mockClient{responses: map[string]mockResponse{"v2/mobile-devices/detail?": {200, inventoryPage(rec)}}}
		d, err := ResolveMobileDeviceIdentifier(context.Background(), client, value)
		if err != nil {
			t.Fatalf("%s: %v", value, err)
		}
		if d.ID != "64" || d.UDID == "" || d.SerialNumber == "" {
			t.Errorf("%s: got %+v", value, d)
		}
		if !strings.Contains(client.calls[0], "section=HARDWARE") {
			t.Errorf("%s: the serial is only populated with section=HARDWARE: %s", value, client.calls[0])
		}
	}
}
