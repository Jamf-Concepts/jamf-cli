// Copyright 2026, Jamf Software LLC

package resolve

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/client"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// mockClient implements registry.HTTPClient for testing.
type mockClient struct {
	responses map[string]mockResponse
	calls     []string // recorded as "METHOD /path", query params unescaped
}

type mockResponse struct {
	status int
	body   string
}

func (m *mockClient) Do(ctx context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	key := method + " " + path
	// Also match the unescaped form, so a test pattern can name an RSQL filter
	// (`filter=id=in=(1,2)`) without percent-encoding it. Patterns that spell
	// out the escaped path keep matching against the raw key.
	decoded := key
	if unescaped, err := url.QueryUnescape(key); err == nil {
		decoded = unescaped
	}
	m.calls = append(m.calls, decoded)

	// Longest-match-wins: prefer more specific patterns.
	bestPattern := ""
	var bestResp mockResponse
	for pattern, resp := range m.responses {
		if len(pattern) <= len(bestPattern) {
			continue
		}
		if strings.Contains(key, pattern) || strings.Contains(decoded, pattern) {
			bestPattern = pattern
			bestResp = resp
		}
	}
	if bestPattern == "" {
		bestResp = mockResponse{status: http.StatusNotFound, body: `{"httpStatus":404}`}
	}
	return statusResponse(ctx, method, path, bestResp.status, bestResp.body)
}

// statusResponse answers the way client.Do does: a status of 400 or above is
// an error unless the caller allowed it with registry.WithAllowedStatuses.
func statusResponse(ctx context.Context, method, path string, status int, body string) (*http.Response, error) {
	if status >= 400 && !registry.StatusAllowed(ctx, status) {
		return nil, client.StatusError(status, method, path, []byte(body))
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

const computerV3Response = `{
	"totalCount": 1,
	"results": [{
		"id": "42",
		"udid": "AAAA-BBBB-CCCC",
		"general": {
			"name": "Neil's MacBook",
			"managementId": "73226fb6-61df-4c10-9552-eb9bc353d507"
		},
		"hardware": {
			"serialNumber": "C02X1234"
		}
	}]
}`

const computerV3DetailResponse = `{
	"id": "42",
	"udid": "AAAA-BBBB-CCCC",
	"general": {
		"name": "Neil's MacBook",
		"managementId": "73226fb6-61df-4c10-9552-eb9bc353d507"
	},
	"hardware": {
		"serialNumber": "C02X1234"
	}
}`

const mobileV2Response = `{
	"totalCount": 1,
	"results": [{
		"mobileDeviceId": "99",
		"general": {
			"displayName": "Lab iPad",
			"udid": "MOBILE-UDID",
			"managementId": "mgmt-uuid-mobile"
		},
		"hardware": {
			"serialNumber": "F4GH5678"
		}
	}]
}`

const mobileV2DetailResponse = `{
	"id": "99",
	"managementId": "mgmt-uuid-mobile",
	"udid": "MOBILE-UDID",
	"name": "Lab iPad",
	"serialNumber": "F4GH5678"
}`

func TestResolveComputer_BySerial(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		"v4/computers-inventory?": {200, computerV3Response},
	}}
	d, err := ResolveComputer(context.Background(), client, "C02X1234", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.ID != "42" {
		t.Errorf("ID = %q, want %q", d.ID, "42")
	}
	if d.SerialNumber != "C02X1234" {
		t.Errorf("SerialNumber = %q, want %q", d.SerialNumber, "C02X1234")
	}
	if d.ManagementID != "73226fb6-61df-4c10-9552-eb9bc353d507" {
		t.Errorf("ManagementID = %q, want UUID", d.ManagementID)
	}
	if d.UDID != "AAAA-BBBB-CCCC" {
		t.Errorf("UDID = %q, want %q", d.UDID, "AAAA-BBBB-CCCC")
	}
	if d.Name != "Neil's MacBook" {
		t.Errorf("Name = %q, want %q", d.Name, "Neil's MacBook")
	}
}

func TestResolveComputer_ByName(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		"v4/computers-inventory?": {200, computerV3Response},
	}}
	d, err := ResolveComputer(context.Background(), client, "", "Neil's MacBook", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.ID != "42" {
		t.Errorf("ID = %q, want %q", d.ID, "42")
	}
}

func TestResolveComputer_ByID(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		"v4/computers-inventory/42": {200, computerV3DetailResponse},
	}}
	d, err := ResolveComputer(context.Background(), client, "", "", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.ID != "42" {
		t.Errorf("ID = %q, want %q", d.ID, "42")
	}
	if d.SerialNumber != "C02X1234" {
		t.Errorf("SerialNumber = %q, want %q", d.SerialNumber, "C02X1234")
	}
}

func TestResolveComputer_NotFound(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		"v4/computers-inventory?": {200, `{"totalCount": 0, "results": []}`},
	}}
	_, err := ResolveComputer(context.Background(), client, "NOSUCH", "", "")
	if err == nil {
		t.Fatal("expected error for not found")
		return
	}
	if !strings.Contains(err.Error(), "no computer found") {
		t.Errorf("error = %q, want to contain 'no computer found'", err.Error())
	}
}

func TestResolveComputer_MultipleMatches(t *testing.T) {
	multiResponse := `{
		"totalCount": 3,
		"results": [
			{"id": "1", "udid": "U1", "general": {"name": "Mac", "managementId": "m1"}, "hardware": {"serialNumber": "S1"}},
			{"id": "2", "udid": "U2", "general": {"name": "Mac", "managementId": "m2"}, "hardware": {"serialNumber": "S2"}}
		]
	}`
	client := &mockClient{responses: map[string]mockResponse{
		"v4/computers-inventory?": {200, multiResponse},
	}}
	_, err := ResolveComputer(context.Background(), client, "", "Mac", "")
	if err == nil {
		t.Fatal("expected error for multiple matches")
		return
	}
	if !strings.Contains(err.Error(), "multiple computers found") {
		t.Errorf("error = %q, want to contain 'multiple computers found'", err.Error())
	}
}

func TestResolveComputer_NoFlags(t *testing.T) {
	client := &mockClient{}
	_, err := ResolveComputer(context.Background(), client, "", "", "")
	if err == nil {
		t.Fatal("expected error when no flags provided")
		return
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("error = %q, want to contain 'required'", err.Error())
	}
}

func TestResolveMobileDevice_BySerial(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		"v2/mobile-devices/detail?": {200, mobileV2Response},
	}}
	d, err := ResolveMobileDevice(context.Background(), client, "F4GH5678", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.ID != "99" {
		t.Errorf("ID = %q, want %q", d.ID, "99")
	}
	if d.SerialNumber != "F4GH5678" {
		t.Errorf("SerialNumber = %q, want %q", d.SerialNumber, "F4GH5678")
	}
	if d.ManagementID != "mgmt-uuid-mobile" {
		t.Errorf("ManagementID = %q, want %q", d.ManagementID, "mgmt-uuid-mobile")
	}
}

func TestResolveMobileDevice_ByID(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		"v2/mobile-devices/99": {200, mobileV2DetailResponse},
	}}
	d, err := ResolveMobileDevice(context.Background(), client, "", "", "99")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Name != "Lab iPad" {
		t.Errorf("Name = %q, want %q", d.Name, "Lab iPad")
	}
}

// computerRecord builds a minimal v3 inventory record for the given ID.
func computerRecord(id string) string {
	return fmt.Sprintf(`{"id":%q,"udid":"udid-%s","general":{"name":"mac-%s"},"hardware":{"serialNumber":"SER%s"}}`,
		id, id, id, id)
}

// writeEntriesFile writes a --from-file fixture and returns its path.
func writeEntriesFile(t *testing.T, lines string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "targets.txt")
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	return path
}

func TestResolveComputersFromFile(t *testing.T) {
	path := writeEntriesFile(t, "# Comment\nC02X1234\n\n42\n")

	client := &mockClient{responses: map[string]mockResponse{
		// One batched query per identifier kind, not one per line.
		`filter=hardware.serialNumber=in=("C02X1234")`: {200, computerV3Response},
		`filter=id=in=(42)`:                            {200, computerV3Response},
	}}

	results, skipped, err := ResolveComputersFromFile(context.Background(), client, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0", skipped)
	}
	if len(client.calls) != 2 {
		t.Errorf("made %d requests, want 2 (one per identifier kind): %v", len(client.calls), client.calls)
	}
}

// A file of numeric IDs must not cost one request per line.
func TestResolveComputersFromFile_BatchesIDs(t *testing.T) {
	path := writeEntriesFile(t, "42\n43\n44\n45\n")

	client := &mockClient{responses: map[string]mockResponse{
		`filter=id=in=(42,43,44,45)`: {200, fmt.Sprintf(`{"totalCount":4,"results":[%s,%s,%s,%s]}`,
			computerRecord("42"), computerRecord("43"), computerRecord("44"), computerRecord("45"))},
	}}

	results, skipped, err := ResolveComputersFromFile(context.Background(), client, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 4 || skipped != 0 {
		t.Fatalf("got %d results / %d skipped, want 4 / 0", len(results), skipped)
	}
	if len(client.calls) != 1 {
		t.Errorf("made %d requests for 4 IDs, want 1 batched query: %v", len(client.calls), client.calls)
	}
	// The per-ID GET path must not be used at all.
	for _, c := range client.calls {
		if strings.Contains(c, "/v4/computers-inventory/") {
			t.Errorf("unexpected per-ID lookup: %s", c)
		}
	}
}

// Jamf matches serials case-insensitively, so a file entry that differs only in
// case from the stored serial must still resolve.
func TestResolveComputersFromFile_SerialCaseInsensitive(t *testing.T) {
	path := writeEntriesFile(t, "c02x1234\n")

	client := &mockClient{responses: map[string]mockResponse{
		`filter=hardware.serialNumber=in=("c02x1234")`: {200, computerV3Response}, // stores "C02X1234"
	}}

	results, skipped, err := ResolveComputersFromFile(context.Background(), client, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || skipped != 0 {
		t.Fatalf("got %d results / %d skipped, want 1 / 0", len(results), skipped)
	}
}

// Unresolvable entries are skipped and counted, not fatal.
func TestResolveComputersFromFile_PartialFailureCounted(t *testing.T) {
	path := writeEntriesFile(t, "C02X1234\nNOSUCHSERIAL\n99\n")

	client := &mockClient{responses: map[string]mockResponse{
		// Only C02X1234 comes back; NOSUCHSERIAL and ID 99 are unknown.
		`filter=hardware.serialNumber=in=("C02X1234","NOSUCHSERIAL")`: {200, computerV3Response},
		`filter=id=in=(99)`: {200, `{"totalCount":0,"results":[]}`},
		// A serial nothing matched is tried as a name before it is given up on.
		`filter=general.name=in=("NOSUCHSERIAL")`: {200, `{"totalCount":0,"results":[]}`},
	}}

	results, skipped, err := ResolveComputersFromFile(context.Background(), client, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if skipped != 2 {
		t.Errorf("skipped = %d, want 2", skipped)
	}
}

// A file where nothing resolves must error rather than hand back an empty list
// that reads downstream as a successful no-op batch.
func TestResolveComputersFromFile_AllUnresolvableErrors(t *testing.T) {
	path := writeEntriesFile(t, "NOSUCH1\nNOSUCH2\n")

	client := &mockClient{responses: map[string]mockResponse{
		`filter=hardware.serialNumber=in=`: {200, `{"totalCount":0,"results":[]}`},
		`filter=general.name=in=`:          {200, `{"totalCount":0,"results":[]}`},
	}}

	_, skipped, err := ResolveComputersFromFile(context.Background(), client, path)
	if err == nil {
		t.Fatal("expected an error when no entry resolves")
	}
	if !strings.Contains(err.Error(), "none of the 2 entries") {
		t.Errorf("error = %q, want it to report that no entry resolved", err.Error())
	}
	if skipped != 2 {
		t.Errorf("skipped = %d, want 2", skipped)
	}
}

// An entry carrying a `*` is resolved on its own: inside a shared =in= list
// the wildcard would page in every device it matches.
func TestResolveComputersFromFile_WildcardEntryIsolated(t *testing.T) {
	path := writeEntriesFile(t, "Lab*\n")

	client := &mockClient{responses: map[string]mockResponse{
		`filter=general.name==`: {200, `{"totalCount":0,"results":[]}`},
	}}

	_, _, err := ResolveComputersFromFile(context.Background(), client, path)
	if err == nil {
		t.Fatal("expected an error when the only entry does not resolve")
	}
	if len(client.calls) != 1 {
		t.Fatalf("made %d requests, want 1: %v", len(client.calls), client.calls)
	}
	if strings.Contains(client.calls[0], "=in=") {
		t.Errorf("wildcard entry was packed into an =in= list: %s", client.calls[0])
	}
}

// A name a serial cannot be — a space, quote, comma or paren — shares one
// name-only =in= lookup with the others, escaped, rather than costing a
// request per line. Wire-checked: `displayName=in=("Probe, \"Q\" (x)",…)`
// matches that device, case-insensitively.
func TestResolveComputersFromFile_SpacedNamesBatchByName(t *testing.T) {
	path := writeEntriesFile(t, "Lab Mac 12\n"+`Lab "A", (B)`+"\nlab mac 7\n")

	client := &mockClient{responses: map[string]mockResponse{
		`filter=general.name=in=`: {200, fmt.Sprintf(`{"totalCount":3,"results":[%s,%s,%s]}`,
			computerRecordFull("12", "", "", "Lab Mac 12", "S12"),
			computerRecordFull("13", "", "", `Lab "A", (B)`, "S13"),
			computerRecordFull("7", "", "", "Lab Mac 7", "S7"))},
	}}

	got, skipped, err := ResolveComputersFromFile(context.Background(), client, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skipped != 0 || len(got) != 3 || got[0].ID != "12" || got[1].ID != "13" || got[2].ID != "7" {
		t.Fatalf("got %v / %d skipped, want [12 13 7] / 0", ids(got), skipped)
	}
	if len(client.calls) != 1 {
		t.Fatalf("made %d requests, want 1: %v", len(client.calls), client.calls)
	}
	if want := `general.name=in=("Lab Mac 12","Lab \"A\", (B)","lab mac 7")`; !strings.Contains(client.calls[0], want) {
		t.Errorf("call = %s, want it to carry %s", client.calls[0], want)
	}
	if strings.Contains(client.calls[0], "serialNumber") {
		t.Errorf("a spaced name was also looked up as a serial: %s", client.calls[0])
	}
}

// A chunk closes once its escaped filter would pass the byte cap, not only at
// the entry count, so a list of long identifiers cannot build a URL a proxy
// refuses.
func TestFilterChunks_BoundedByEscapedLength(t *testing.T) {
	var uuids []string
	for i := range batchChunkSize {
		uuids = append(uuids, fmt.Sprintf("96050be1-e53b-454d-9752-%012d", i))
	}
	filter := func(chunk []string) string {
		list := quotedList(chunk)
		return fmt.Sprintf("udid=in=(%s),general.managementId=in=(%s)", list, list)
	}
	chunks := filterChunks(uuids, filter)
	if len(chunks) < 2 {
		t.Fatalf("100 UUIDs fit one chunk of %d escaped bytes; want the byte cap to split them", len(url.QueryEscape(filter(uuids))))
	}
	total := 0
	for _, c := range chunks {
		if n := len(url.QueryEscape(filter(c))); n > batchFilterMaxBytes {
			t.Errorf("chunk of %d entries escapes to %d bytes, over %d", len(c), n, batchFilterMaxBytes)
		}
		total += len(c)
	}
	if total != len(uuids) {
		t.Errorf("chunks hold %d entries, want %d", total, len(uuids))
	}

	// An entry too long for any chunk still gets one of its own.
	long := strings.Repeat("x", batchFilterMaxBytes)
	if got := filterChunks([]string{"a", long, "b"}, filter); len(got) != 3 {
		t.Errorf("got %d chunks, want 3 (the long entry alone)", len(got))
	}
}

// A serial-shaped entry that matched no serial and resolved by name says so,
// since a mistyped serial equal to another device's name would otherwise
// target that device silently. A returned record that cannot be parsed is
// counted rather than reported only as "not found".
func TestResolveEntries_NotesNameFallbackAndUnreadableRecords(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		`filter=hardware.serialNumber=in=`: {200, `{"totalCount":1,"results":[{"general":{"name":"no id"}}]}`},
		`filter=general.name=in=`: {200, fmt.Sprintf(`{"totalCount":1,"results":[%s]}`,
			computerRecordFull("9", "", "", "KIOSK-1", "C02REAL"))},
	}}

	var got []*DeviceIdentifiers
	stderr := captureStderr(t, func() {
		var err error
		got, _, err = ResolveComputerEntries(context.Background(), client, []string{"KIOSK-1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if len(got) != 1 || got[0].ID != "9" {
		t.Fatalf("got %v, want [9]", ids(got))
	}
	if !strings.Contains(stderr, `"KIOSK-1" matched no serial number; resolved by name to computer 9`) {
		t.Errorf("stderr = %q, want the name fallback noted", stderr)
	}
	if !strings.Contains(stderr, "1 computer record(s) returned by the serial number lookup could not be read") {
		t.Errorf("stderr = %q, want the unreadable record counted", stderr)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()
	fn()
	_ = w.Close()
	b, _ := io.ReadAll(r)
	return string(b)
}

// A transport/HTTP failure of the batch lookup is fatal — it must not be
// reported as "these entries don't exist".
func TestResolveComputersFromFile_LookupErrorIsFatal(t *testing.T) {
	path := writeEntriesFile(t, "42\n")

	client := &mockClient{responses: map[string]mockResponse{
		`filter=id=in=(42)`: {500, `{"httpStatus":500}`},
	}}

	_, _, err := ResolveComputersFromFile(context.Background(), client, path)
	if err == nil {
		t.Fatal("expected an error when the batch lookup fails")
	}
	if strings.Contains(err.Error(), "none of the") {
		t.Errorf("HTTP failure was misreported as unresolvable entries: %v", err)
	}
}

// The mobile resolver batches the same way (and against the /detail endpoint,
// the only mobile list path that honors RSQL filters).
func TestResolveMobileDevicesFromFile(t *testing.T) {
	path := writeEntriesFile(t, "F4GH5678\n99\nNOSUCHSERIAL\n")

	client := &mockClient{responses: map[string]mockResponse{
		`filter=serialNumber=in=("F4GH5678","NOSUCHSERIAL")`: {200, mobileV2Response},
		`filter=mobileDeviceId=in=(99)`:                      {200, mobileV2Response},
		`filter=displayName=in=("NOSUCHSERIAL")`:             {200, `{"totalCount":0,"results":[]}`},
	}}

	results, skipped, err := ResolveMobileDevicesFromFile(context.Background(), client, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 || skipped != 1 {
		t.Fatalf("got %d results / %d skipped, want 2 / 1", len(results), skipped)
	}
	// One per identifier kind, plus the name retry for the serial nothing matched.
	if len(client.calls) != 3 {
		t.Errorf("made %d requests, want 3: %v", len(client.calls), client.calls)
	}
	for _, c := range client.calls {
		if !strings.Contains(c, "/v2/mobile-devices/detail") {
			t.Errorf("mobile batch lookup used %s, want the /detail endpoint", c)
		}
	}
}

func TestReadEntriesFromFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, []byte("# only comments\n\n"), 0o644); err != nil {
		t.Fatal(err)
		return
	}
	_, err := readEntriesFromFile(path)
	if err == nil {
		t.Fatal("expected error for empty file")
		return
	}
	if !strings.Contains(err.Error(), "no entries") {
		t.Errorf("error = %q, want to contain 'no entries'", err.Error())
	}
}

func TestIsNumericID(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"42", true},
		{"0", true},
		{"123456", true},
		{"C02X1234", false},
		{"", false},
		{"12abc", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := isNumericID(tt.input); got != tt.want {
				t.Errorf("isNumericID(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatDeviceDesc(t *testing.T) {
	d := &DeviceIdentifiers{
		ID:           "42",
		Name:         "Neil's MacBook",
		SerialNumber: "C02X1234",
	}
	got := FormatDeviceDesc(d)
	want := `"Neil's MacBook" (serial: C02X1234, id: 42)`
	if got != want {
		t.Errorf("FormatDeviceDesc = %q, want %q", got, want)
	}
}

func TestFormatDeviceDesc_NoSerial(t *testing.T) {
	d := &DeviceIdentifiers{ID: "42", Name: "Test"}
	got := FormatDeviceDesc(d)
	if !strings.Contains(got, "id: 42") {
		t.Errorf("FormatDeviceDesc = %q, want to contain 'id: 42'", got)
	}
}

func TestEscapeRSQL(t *testing.T) {
	got := EscapeRSQL(`Neil's "MacBook"`)
	want := `Neil's \"MacBook\"`
	if got != want {
		t.Errorf("EscapeRSQL = %q, want %q", got, want)
	}
}

func TestResolveClassicComputerGroupID(t *testing.T) {
	groupXML := `<?xml version="1.0" encoding="UTF-8"?>
<computer_group>
  <id>7</id>
  <name>Lab Macs</name>
  <is_smart>true</is_smart>
</computer_group>`

	client := &mockClient{responses: map[string]mockResponse{
		"GET /JSSResource/computergroups/name/Lab%20Macs": {200, groupXML},
		"GET /JSSResource/computergroups":                 {200, `<computer_groups><size>1</size><computer_group><id>7</id><name>Lab Macs</name><is_smart>true</is_smart></computer_group></computer_groups>`},
	}}

	id, err := ResolveClassicComputerGroupID(context.Background(), client, "Lab Macs")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "7" {
		t.Errorf("ID = %q, want %q", id, "7")
	}
}

func TestResolveClassicComputerGroupID_NotFound(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		"GET /JSSResource/computergroups/name/NoSuch": {404, `<error><code>404</code></error>`},
	}}

	_, err := ResolveClassicComputerGroupID(context.Background(), client, "NoSuch")
	if err == nil {
		t.Fatal("expected error for 404")
		return
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want to contain 'not found'", err.Error())
	}
}

func TestResolveClassicMobileGroupID(t *testing.T) {
	groupXML := `<?xml version="1.0" encoding="UTF-8"?>
<mobile_device_group>
  <id>12</id>
  <name>Lab iPads</name>
  <is_smart>false</is_smart>
</mobile_device_group>`

	client := &mockClient{responses: map[string]mockResponse{
		"GET /JSSResource/mobiledevicegroups/name/Lab%20iPads": {200, groupXML},
		"GET /JSSResource/mobiledevicegroups":                  {200, `<mobile_device_groups><size>1</size><mobile_device_group><id>12</id><name>Lab iPads</name><is_smart>false</is_smart></mobile_device_group></mobile_device_groups>`},
	}}

	id, err := ResolveClassicMobileGroupID(context.Background(), client, "Lab iPads")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "12" {
		t.Errorf("ID = %q, want %q", id, "12")
	}
}

func TestResolveClassicMobileGroupID_NotFound(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		"GET /JSSResource/mobiledevicegroups/name/NoSuch": {404, `<error><code>404</code></error>`},
	}}

	_, err := ResolveClassicMobileGroupID(context.Background(), client, "NoSuch")
	if err == nil {
		t.Fatal("expected error for 404")
		return
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want to contain 'not found'", err.Error())
	}
}

func TestResolveComputerGroup(t *testing.T) {
	groupListResponse := `{"computer_groups": [{"id": "5", "name": "All Macs"}]}`
	groupDetailResponse := `{"computer_group": {"id": "5", "name": "All Macs", "computers": [{"id": "42", "name": "Mac1"}, {"id": "43", "name": "Mac2"}]}}`

	client := &mockClient{responses: map[string]mockResponse{
		"GET /JSSResource/computergroups":  {200, groupListResponse},
		"/JSSResource/computergroups/id/5": {200, groupDetailResponse},
		"v4/computers-inventory/42":        {200, computerV3DetailResponse},
		"v4/computers-inventory/43":        {200, strings.ReplaceAll(computerV3DetailResponse, `"42"`, `"43"`)},
	}}

	results, err := ResolveComputerGroup(context.Background(), client, "All Macs")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("got %d results, want 2", len(results))
	}
}

func TestParseMobileDevice_DetailEndpoint_GeneralSection(t *testing.T) {
	obj := map[string]any{
		"mobileDeviceId": "99",
		"serialNumber":   "F4GH5678",
		"general": map[string]any{
			"managementId": "mgmt-uuid-from-general",
			"displayName":  "Lab iPad",
		},
	}
	d, err := parseMobileDevice(obj)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.ManagementID != "mgmt-uuid-from-general" {
		t.Errorf("ManagementID = %q, want %q", d.ManagementID, "mgmt-uuid-from-general")
	}
	if d.Name != "Lab iPad" {
		t.Errorf("Name = %q, want %q", d.Name, "Lab iPad")
	}
}
