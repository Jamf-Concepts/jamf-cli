// Copyright 2026, Jamf Software LLC

package resolve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
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

// statusErrClient answers every request the way client.Do answers a non-2xx:
// with an *exitcode.Error and no response.
type statusErrClient struct{ err error }

func (c statusErrClient) Do(context.Context, string, string, io.Reader) (*http.Response, error) {
	return nil, c.err
}

// A 401 or 403 on the lookup names the read privilege the resolution needs,
// keeps the exit code, and keeps the hint the client already attached.
func TestResolveDeviceIdentifier_PermissionFailureNamesThePrivilege(t *testing.T) {
	cases := []struct {
		resolve   func(context.Context, registry.HTTPClient, string) (*DeviceIdentifiers, error)
		code      int
		privilege string
	}{
		{ResolveComputerIdentifier, exitcode.PermissionDenied, "Read Computers"},
		{ResolveMobileDeviceIdentifier, exitcode.PermissionDenied, "Read Mobile Devices"},
		{ResolveComputerIdentifier, exitcode.Authentication, "Read Computers"},
	}
	for _, tc := range cases {
		upstream := exitcode.New(tc.code, "permission denied (HTTP 403)").WithHint("upstream hint")
		_, err := tc.resolve(context.Background(), statusErrClient{upstream}, "5")
		var ee *exitcode.Error
		if !errors.As(err, &ee) {
			t.Fatalf("want an *exitcode.Error, got %T %v", err, err)
		}
		if ee.Code != tc.code {
			t.Errorf("exit code = %d, want %d", ee.Code, tc.code)
		}
		if !strings.Contains(ee.Hint, tc.privilege) || !strings.Contains(ee.Hint, "upstream hint") {
			t.Errorf("hint %q should name %s and keep the upstream hint", ee.Hint, tc.privilege)
		}
		if !strings.Contains(err.Error(), `looking up`) || !strings.Contains(err.Error(), "HTTP 403") {
			t.Errorf("message should say what was looked up and keep the status: %v", err)
		}
		if errors.Is(err, ErrNoDeviceMatch) {
			t.Error("a permission failure must not read as no match")
		}
	}

	// Any other failure is wrapped as before, with no privilege hint.
	other := exitcode.New(exitcode.General, "server error (HTTP 500)")
	_, err := ResolveComputerIdentifier(context.Background(), statusErrClient{other}, "5")
	var ee *exitcode.Error
	if errors.As(err, &ee) && strings.Contains(ee.Hint, "Read Computers") {
		t.Errorf("a 5xx must not be blamed on a missing privilege: %q", ee.Hint)
	}
}
