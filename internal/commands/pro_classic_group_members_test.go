// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/client"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// fakeDevice is one inventory record the fake Jamf Pro serves.
type fakeDevice struct {
	id, name, serial, udid, mgmt string
	managed                      *bool
}

// fakeGroupServer is a stateful Jamf Pro for one Classic static group: it
// serves the group, applies the additions and deletions a PUT carries the way
// the server was observed to (atomic; an unknown or unmanaged member rejects
// the whole request and names nothing / names the unmanaged IDs), and answers
// any inventory query with every device, since the resolver indexes the
// results by the identifier it asked for.
type fakeGroupServer struct {
	kind    classicGroupKind
	groupID string
	name    string
	smart   bool
	members map[string]bool
	devices []fakeDevice
	// ignoreWrites answers a PUT with success and applies nothing.
	ignoreWrites bool
	puts         []string
	calls        []string
	putErr       error
}

func newFakeGroupServer(kind classicGroupKind, members ...string) *fakeGroupServer {
	f := &fakeGroupServer{kind: kind, groupID: "200", name: "Quarantine", members: map[string]bool{}}
	for _, m := range members {
		f.members[m] = true
	}
	return f
}

func (f *fakeGroupServer) device(id string) *fakeDevice {
	for i := range f.devices {
		if f.devices[i].id == id {
			return &f.devices[i]
		}
	}
	return nil
}

func (f *fakeGroupServer) Do(_ context.Context, method, path string, body io.Reader) (*http.Response, error) {
	decoded, _ := url.QueryUnescape(path)
	f.calls = append(f.calls, method+" "+decoded)
	bare, _, _ := strings.Cut(path, "?")
	detail := fmt.Sprintf(f.kind.detailPath, f.groupID)

	switch {
	case method == "GET" && bare == f.kind.listPath:
		key := strings.TrimPrefix(f.kind.listPath, "/JSSResource/")
		key = map[string]string{"computergroups": "computer_groups", "mobiledevicegroups": "mobile_device_groups"}[key]
		return jsonResp(200, map[string]any{key: []any{map[string]any{"id": f.groupID, "name": f.name}}}), nil
	case method == "GET" && bare == detail:
		var ms []any
		ids := make([]string, 0, len(f.members))
		for id := range f.members {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			ms = append(ms, map[string]any{"id": id})
		}
		return jsonResp(200, map[string]any{f.kind.root: map[string]any{
			"id": f.groupID, "name": f.name, "is_smart": f.smart, f.kind.membersKey: ms,
		}}), nil
	case method == "PUT" && bare == detail:
		raw, _ := io.ReadAll(body)
		f.puts = append(f.puts, string(raw))
		if f.putErr != nil {
			return nil, f.putErr
		}
		return f.applyPut(string(raw))
	case method == "GET" && (strings.HasPrefix(bare, "/v4/computers-inventory") || strings.HasPrefix(bare, "/v2/mobile-devices/detail")):
		var results []any
		for _, d := range f.devices {
			results = append(results, f.inventoryRecord(d))
		}
		return jsonResp(200, map[string]any{"totalCount": len(results), "results": results}), nil
	}
	return nil, fmt.Errorf("fake group server: no route for %s %s", method, path)
}

var fakeMemberID = regexp.MustCompile(`<id>([0-9]+)</id>`)

func (f *fakeGroupServer) applyPut(body string) (*http.Response, error) {
	add := strings.Contains(body, "<"+f.kind.additions+">")
	var ids []string
	for _, m := range fakeMemberID.FindAllStringSubmatch(body, -1) {
		ids = append(ids, m[1])
	}
	var unmanaged []string
	for _, id := range ids {
		d := f.device(id)
		if d == nil {
			return nil, client.StatusError(409, "PUT", "", []byte("Error: Unable to match "+f.kind.element))
		}
		if add && d.managed != nil && !*d.managed {
			unmanaged = append(unmanaged, id)
		}
		if !add && !f.members[id] && f.kind.element == "computer" {
			return nil, client.StatusError(409, "PUT", "", []byte("Error: Unable to match computer in deletions list id= "+id))
		}
	}
	if len(unmanaged) > 0 {
		return nil, client.StatusError(409, "PUT", "", []byte(fmt.Sprintf(
			"Error: The computers with the following IDs are unmanaged and cannot be added to a computer group: %s",
			strings.Join(unmanaged, ", "))))
	}
	if !f.ignoreWrites {
		for _, id := range ids {
			if add {
				f.members[id] = true
			} else {
				delete(f.members, id)
			}
		}
	}
	return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader("<ok/>")), Header: http.Header{}}, nil
}

func (f *fakeGroupServer) inventoryRecord(d fakeDevice) map[string]any {
	if f.kind.element == "computer" {
		general := map[string]any{"name": d.name, "managementId": d.mgmt}
		if d.managed != nil {
			general["remoteManagement"] = map[string]any{"managed": *d.managed}
		}
		return map[string]any{"id": d.id, "udid": d.udid, "general": general, "hardware": map[string]any{"serialNumber": d.serial}}
	}
	general := map[string]any{"displayName": d.name, "udid": d.udid, "managementId": d.mgmt}
	if d.managed != nil {
		general["managed"] = *d.managed
	}
	return map[string]any{"mobileDeviceId": d.id, "general": general, "hardware": map[string]any{"serialNumber": d.serial}}
}

func (f *fakeGroupServer) putIDs(i int) []string {
	var ids []string
	for _, m := range fakeMemberID.FindAllStringSubmatch(f.puts[i], -1) {
		ids = append(ids, m[1])
	}
	return ids
}

func jsonResp(status int, v any) *http.Response {
	b, _ := json.Marshal(v)
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(b))), Header: http.Header{}}
}

func boolPtr(b bool) *bool { return &b }

func standardDevices() []fakeDevice {
	return []fakeDevice{
		{id: "1", name: "Mac-01", serial: "SER001", udid: "11111111-1111-4111-8111-111111111111", mgmt: "aaaaaaaa-0000-4000-8000-000000000001", managed: boolPtr(true)},
		{id: "2", name: "Mac-02", serial: "SER002", udid: "22222222-2222-4222-8222-222222222222", mgmt: "aaaaaaaa-0000-4000-8000-000000000002", managed: boolPtr(true)},
		{id: "3", name: "Mac-03", serial: "SER003", udid: "33333333-3333-4333-8333-333333333333", mgmt: "aaaaaaaa-0000-4000-8000-000000000003", managed: boolPtr(true)},
	}
}

func runGroupMembers(t *testing.T, f *fakeGroupServer, add bool, args ...string) (string, error) {
	t.Helper()
	cliCtx := &registry.CLIContext{Client: f}
	cmd := newClassicGroupMembersCmd(cliCtx, f.kind, add)
	_, stderr, err := runCobraCmd(t, cmd, args...)
	return stderr, err
}

// Every kind of identifier resolves, and the members go in one request.
func TestAddMembers_MixedIdentifiersOnePUT(t *testing.T) {
	f := newFakeGroupServer(classicComputerGroupKind)
	f.devices = standardDevices()

	_, err := runGroupMembers(t, f, true, "200",
		"--computer", "1", "--computer", "SER002", "--computer", "aaaaaaaa-0000-4000-8000-000000000003")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.puts) != 1 {
		t.Fatalf("sent %d PUTs, want 1: %v", len(f.puts), f.puts)
	}
	if got := strings.Join(f.putIDs(0), ","); got != "1,2,3" {
		t.Errorf("PUT carried ids %s, want 1,2,3", got)
	}
	if !strings.Contains(f.puts[0], "<computer_group><computer_additions><computer><id>1</id></computer>") {
		t.Errorf("PUT body is not a bare additions delta: %s", f.puts[0])
	}
	for _, id := range []string{"1", "2", "3"} {
		if !f.members[id] {
			t.Errorf("computer %s is not a member after the add", id)
		}
	}
}

// Re-adding a member is idempotent on the wire, but sending it buys nothing;
// removing a non-member answers 409 for computers and rejects the whole
// request. Both are read off the membership first and left out.
func TestGroupMembers_AlreadyInStateIsNotSent(t *testing.T) {
	f := newFakeGroupServer(classicComputerGroupKind, "1")
	f.devices = standardDevices()

	if _, err := runGroupMembers(t, f, true, "200", "--computer", "1", "--computer", "2"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if got := strings.Join(f.putIDs(0), ","); got != "2" {
		t.Errorf("add PUT carried %s, want only the non-member 2", got)
	}

	f.puts = nil
	if _, err := runGroupMembers(t, f, false, "200", "--computer", "2", "--computer", "3"); err != nil {
		t.Fatalf("remove with a non-member in the list: %v", err)
	}
	if got := strings.Join(f.putIDs(0), ","); got != "2" {
		t.Errorf("remove PUT carried %s, want only the member 2", got)
	}

	f.puts = nil
	stderr, err := runGroupMembers(t, f, false, "200", "--computer", "3")
	if err != nil {
		t.Fatalf("remove of a non-member only: %v", err)
	}
	if len(f.puts) != 0 {
		t.Errorf("sent a PUT although nothing changes: %v", f.puts)
	}
	if !strings.Contains(stderr, "Nothing to change") {
		t.Errorf("stderr = %q, want a nothing-to-change note", stderr)
	}
}

// A list source previews without --yes; -n previews even with it.
func TestGroupMembers_ListSourcePreviewsUntilYes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serials.txt")
	if err := os.WriteFile(path, []byte("# lab\nSER001\nSER002\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	f := newFakeGroupServer(classicComputerGroupKind)
	f.devices = standardDevices()
	stderr, err := runGroupMembers(t, f, true, "--name", "Quarantine", "--from-file", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.puts) != 0 {
		t.Errorf("--from-file without --yes sent %d PUTs", len(f.puts))
	}
	if !strings.Contains(stderr, "[dry-run]") || !strings.Contains(stderr, "--yes") {
		t.Errorf("stderr = %q, want a preview naming --yes", stderr)
	}

	t.Cleanup(func() { dryRun = false })
	dryRun = true
	if _, err := runGroupMembers(t, f, true, "200", "--from-file", path, "--yes"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := runGroupMembers(t, f, true, "200", "--computer", "1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.puts) != 0 {
		t.Errorf("-n sent %d PUTs", len(f.puts))
	}

	dryRun = false
	if _, err := runGroupMembers(t, f, true, "200", "--from-file", path, "--yes"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.puts) != 1 {
		t.Errorf("--yes sent %d PUTs, want 1", len(f.puts))
	}
}

func TestGroupMembers_SmartGroupRefused(t *testing.T) {
	f := newFakeGroupServer(classicComputerGroupKind)
	f.smart = true
	f.devices = standardDevices()
	_, err := runGroupMembers(t, f, true, "200", "--computer", "1")
	if err == nil || !strings.Contains(err.Error(), "smart group") {
		t.Fatalf("err = %v, want a smart-group refusal", err)
	}
	if len(f.puts) != 0 {
		t.Error("a smart group was written to")
	}
}

// An unmanaged device would fail the whole request, every managed member with
// it, so one inventory reports as unmanaged is held back before anything is
// sent and the rest still land.
func TestAddMembers_UnmanagedHeldBack(t *testing.T) {
	f := newFakeGroupServer(classicComputerGroupKind)
	f.devices = standardDevices()
	f.devices[1].managed = boolPtr(false)

	_, err := runGroupMembers(t, f, true, "200", "--computer", "1", "--computer", "2", "--computer", "3")
	if exitcode.CodeFrom(err) != exitcode.PartialFailure {
		t.Fatalf("err = %v (code %d), want a partial failure", err, exitcode.CodeFrom(err))
	}
	if len(f.puts) != 1 || strings.Join(f.putIDs(0), ",") != "1,3" {
		t.Fatalf("PUTs = %v, want one carrying 1,3", f.puts)
	}
	if !f.members["1"] || !f.members["3"] || f.members["2"] {
		t.Errorf("members = %v, want 1 and 3", f.members)
	}
}

// When inventory does not say, the server's refusal names the unmanaged IDs;
// they are recorded and the rest of the chunk is sent again.
func TestAddMembers_ServerNamedUnmanagedRetried(t *testing.T) {
	f := newFakeGroupServer(classicComputerGroupKind)
	f.devices = standardDevices()
	for i := range f.devices {
		f.devices[i].managed = nil // inventory reports no managed state
	}
	f2 := &refusingGroupServer{fakeGroupServer: f, unmanaged: map[string]bool{"2": true}}
	cliCtx := &registry.CLIContext{Client: f2}
	_, _, err := runCobraCmd(t, newClassicGroupMembersCmd(cliCtx, f.kind, true), "200",
		"--computer", "1", "--computer", "2", "--computer", "3")
	if exitcode.CodeFrom(err) != exitcode.PartialFailure {
		t.Fatalf("err = %v, want a partial failure", err)
	}
	if len(f.puts) != 2 {
		t.Fatalf("sent %d PUTs, want the refused one and its retry: %v", len(f.puts), f.puts)
	}
	if got := strings.Join(f.putIDs(1), ","); got != "1,3" {
		t.Errorf("retry carried %s, want 1,3", got)
	}
	if !f.members["1"] || !f.members["3"] || f.members["2"] {
		t.Errorf("members = %v, want 1 and 3", f.members)
	}
}

// refusingGroupServer refuses the IDs it names as unmanaged, the way the
// server does when inventory does not report the managed state.
type refusingGroupServer struct {
	*fakeGroupServer
	unmanaged map[string]bool
}

func (r *refusingGroupServer) Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	if method == "PUT" {
		raw, _ := io.ReadAll(body)
		var named []string
		for _, m := range fakeMemberID.FindAllStringSubmatch(string(raw), -1) {
			if r.unmanaged[m[1]] {
				named = append(named, m[1])
			}
		}
		if len(named) > 0 {
			r.puts = append(r.puts, string(raw))
			return nil, client.StatusError(409, "PUT", path, []byte(
				"Error: The computers with the following IDs are unmanaged and cannot be added to a computer group: "+strings.Join(named, ", ")))
		}
		body = strings.NewReader(string(raw))
	}
	return r.fakeGroupServer.Do(ctx, method, path, body)
}

// A write the server answers with success but does not apply is caught by the
// read-back rather than reported as done.
func TestAddMembers_ReadBackCatchesAnIgnoredWrite(t *testing.T) {
	f := newFakeGroupServer(classicComputerGroupKind)
	f.devices = standardDevices()
	f.ignoreWrites = true
	_, err := runGroupMembers(t, f, true, "200", "--computer", "1")
	if err == nil {
		t.Fatal("an ignored write was reported as success")
	}
	if !strings.Contains(err.Error(), "still not a member") {
		t.Errorf("err = %v, want the read-back to name the member that did not land", err)
	}
}

// A failed request fails every member it carried, and only those.
func TestAddMembers_ChunkedAndFailurePerChunk(t *testing.T) {
	f := newFakeGroupServer(classicComputerGroupKind)
	var flags []string
	for i := 1; i <= groupMembersChunkSize+10; i++ {
		id := fmt.Sprint(i)
		f.devices = append(f.devices, fakeDevice{id: id, name: "Mac-" + id, serial: "S" + id, managed: boolPtr(true)})
		flags = append(flags, "--computer", id)
	}
	_, err := runGroupMembers(t, f, true, append([]string{"200"}, flags...)...)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.puts) != 2 {
		t.Fatalf("sent %d PUTs for %d members, want 2", len(f.puts), groupMembersChunkSize+10)
	}
	if n := len(f.putIDs(0)); n != groupMembersChunkSize {
		t.Errorf("first chunk carried %d, want %d", n, groupMembersChunkSize)
	}

	g := newFakeGroupServer(classicComputerGroupKind)
	g.devices = standardDevices()
	g.putErr = errors.New("connection reset")
	_, err = runGroupMembers(t, g, true, "200", "--computer", "1", "--computer", "2")
	if err == nil || exitcode.CodeFrom(err) == exitcode.PartialFailure {
		t.Errorf("err = %v, want a total failure when the only request fails", err)
	}
}

// The mobile family writes its own element names.
func TestAddMembers_MobileElementNames(t *testing.T) {
	f := newFakeGroupServer(classicMobileGroupKind)
	f.devices = []fakeDevice{{id: "64", name: "iPad", serial: "GMJR66B185", udid: "5f3644dc5303edf83f28a8d6070c1e955ba91af1", mgmt: "m", managed: boolPtr(true)}}
	if _, err := runGroupMembers(t, f, true, "200", "--mobile-device", "5f3644dc5303edf83f28a8d6070c1e955ba91af1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var doc struct {
		XMLName xml.Name `xml:"mobile_device_group"`
		IDs     []string `xml:"mobile_device_additions>mobile_device>id"`
	}
	body := strings.TrimPrefix(f.puts[0], `<?xml version="1.0" encoding="UTF-8"?>`)
	if err := xml.Unmarshal([]byte(body), &doc); err != nil || strings.Join(doc.IDs, ",") != "64" {
		t.Errorf("PUT body %s does not parse as a mobile additions delta for 64 (%v)", f.puts[0], err)
	}
}

func TestGroupMembers_GroupIdentityForms(t *testing.T) {
	f := newFakeGroupServer(classicComputerGroupKind)
	f.devices = standardDevices()
	if _, err := runGroupMembers(t, f, true, "200", "--name", "Quarantine", "--computer", "1"); err == nil {
		t.Error("an <id> and --name together were accepted")
	}
	if _, err := runGroupMembers(t, f, true, "--computer", "1"); err == nil {
		t.Error("no group was accepted")
	}
	if _, err := runGroupMembers(t, f, true, "Quarantine", "--computer", "1"); err == nil {
		t.Error("a name as the positional ID was accepted")
	}
	if _, err := runGroupMembers(t, f, true, "200"); err == nil {
		t.Error("no member source was accepted")
	}
}
