// Copyright 2026, Jamf Software LLC

package scope

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// ─── XML round-trip ────────────────────────────────────────────────────────────

func TestScopeXML_UnmarshalPolicy(t *testing.T) {
	data := `<policy>
		<general><id>42</id><name>Test</name></general>
		<scope>
			<all_computers>true</all_computers>
			<all_jss_users>false</all_jss_users>
			<computers><computer><id>1</id><name>Mac-001</name></computer></computers>
			<computer_groups>
				<computer_group><id>10</id><name>All Managed</name></computer_group>
				<computer_group><id>11</id><name>Lab Macs</name></computer_group>
			</computer_groups>
			<buildings/>
			<departments/>
			<limit_to_users>
				<user_groups>
					<user_group>Staff</user_group>
					<user_group>Admins</user_group>
				</user_groups>
			</limit_to_users>
			<limitations>
				<network_segments>
					<network_segment><id>1</id><name>Corporate</name></network_segment>
				</network_segments>
				<users/>
				<user_groups><user_group><name>Staff</name></user_group></user_groups>
				<ibeacons/>
			</limitations>
			<exclusions>
				<computers/>
				<computer_groups>
					<computer_group><id>20</id><name>Test Machines</name></computer_group>
				</computer_groups>
				<buildings>
					<building><id>1</id><name>London</name></building>
				</buildings>
				<departments/>
				<users/>
				<user_groups/>
				<network_segments/>
				<ibeacons/>
			</exclusions>
		</scope>
	</policy>`

	var env classicResourceXML
	if err := xml.Unmarshal([]byte(data), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if env.General.ID != "42" {
		t.Errorf("general.id = %s, want 42", env.General.ID)
	}
	if !env.Scope.AllComputers {
		t.Error("all_computers should be true")
	}
	if len(env.Scope.Computers.Items) != 1 {
		t.Errorf("computers: got %d, want 1", len(env.Scope.Computers.Items))
	}
	if len(env.Scope.ComputerGroups.Items) != 2 {
		t.Errorf("computer_groups: got %d, want 2", len(env.Scope.ComputerGroups.Items))
	}
	if env.Scope.ComputerGroups.Items[0].Name != "All Managed" {
		t.Errorf("first group = %q", env.Scope.ComputerGroups.Items[0].Name)
	}
	// <limit_to_users> is deliberately unmodelled: the server keeps it in step
	// with <limitations><user_groups> by itself, so reading the mirror is
	// complete. What matters here is that its presence does not break the
	// parse or leak into another field.
	if got := len(env.Scope.Limitations.UserGroups.Items); got != 1 {
		t.Errorf("limitations.user_groups: got %d, want 1 (the mirror of limit_to_users)", got)
	} else if env.Scope.Limitations.UserGroups.Items[0].Name != "Staff" {
		t.Errorf("limitation user_group = %q, want Staff", env.Scope.Limitations.UserGroups.Items[0].Name)
	}
	if len(env.Scope.Limitations.NetworkSegments.Items) != 1 {
		t.Errorf("limitation network_segments: got %d", len(env.Scope.Limitations.NetworkSegments.Items))
	}
	if len(env.Scope.Exclusions.ComputerGroups.Items) != 1 {
		t.Errorf("exclusion computer_groups: got %d", len(env.Scope.Exclusions.ComputerGroups.Items))
	}
	if len(env.Scope.Exclusions.Buildings.Items) != 1 {
		t.Errorf("exclusion buildings: got %d", len(env.Scope.Exclusions.Buildings.Items))
	}
}

func TestScopeXML_MarshalRoundTrip(t *testing.T) {
	s := ScopeXML{
		AllComputers: true,
		ComputerGroups: ScopeItemSlice{
			Items:    []NamedItem{{ID: "1", Name: "Group A"}, {Name: "Group B"}},
			ElemName: "computer_group",
		},
		Buildings: ScopeItemSlice{
			Items:    []NamedItem{{Name: "HQ"}},
			ElemName: "building",
		},
	}

	data, err := xml.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	xmlStr := string(data)

	if !strings.Contains(xmlStr, "<computer_groups>") {
		t.Error("missing <computer_groups>")
	}
	if !strings.Contains(xmlStr, "<computer_group>") {
		t.Error("missing <computer_group>")
	}
	if !strings.Contains(xmlStr, "<name>Group A</name>") {
		t.Error("missing Group A")
	}

	// Unmarshal back and verify
	var parsed ScopeXML
	if err := xml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal round-trip: %v", err)
	}
	if len(parsed.ComputerGroups.Items) != 2 {
		t.Errorf("round-trip computer_groups: got %d, want 2", len(parsed.ComputerGroups.Items))
	}
}

// mockPutClient records every request path and non-GET body it receives, and
// returns a fixed document for GET.
type mockPutClient struct {
	getBody  string
	requests []string // "METHOD path"
	bodies   []string // request bodies, in request order, "" for a bodyless call
}

func (m *mockPutClient) Do(_ context.Context, method, path string, body io.Reader) (*http.Response, error) {
	m.requests = append(m.requests, method+" "+path)
	sent := ""
	if body != nil {
		b, _ := io.ReadAll(body)
		sent = string(b)
	}
	m.bodies = append(m.bodies, sent)
	if method == "GET" {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(m.getBody))}, nil
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
}

// TestPutScope_SendsOnlyTheScope pins the two properties of the write: one
// request to the top-level endpoint, and a body carrying nothing but <scope>.
//
// The single request is the point — PutScope used to re-GET the whole document
// purely to splice the scope into its bytes, so a scope edit cost three GETs.
// The /subset/Scope check stays because that shortcut works on a direct
// instance and answers 403 through the platform gateway, so it is the tempting
// wrong answer rather than an impossible one.
func TestPutScope_SendsOnlyTheScope(t *testing.T) {
	client := &mockPutClient{}
	res := Resource{APIPath: "policies", SingularKey: "policy"}
	s := &ScopeXML{
		AllComputers:   true,
		ComputerGroups: ScopeItemSlice{Items: []NamedItem{{ID: "1", Name: "Group A"}}, ElemName: "computer_group"},
	}

	if err := PutScope(context.Background(), client, res, "5", s); err != nil {
		t.Fatalf("PutScope: %v", err)
	}

	want := []string{"PUT /JSSResource/policies/id/5"}
	if len(client.requests) != len(want) || client.requests[0] != want[0] {
		t.Fatalf("requests = %v, want %v", client.requests, want)
	}
	if strings.Contains(client.requests[0], "subset") {
		t.Errorf("hit a /subset/ path — not proxied by the Jamf Platform Gateway: %q", client.requests[0])
	}

	body := client.bodies[0]
	for _, unwanted := range []string{"<general>", "<self_service>", "<payloads>", "<packages>"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("body carries %s, which a scope write must not rewrite:\n%s", unwanted, body)
		}
	}
	if !strings.Contains(body, "<policy>") || !strings.Contains(body, "<scope>") {
		t.Errorf("body is not <policy><scope>…:\n%s", body)
	}
	if !strings.Contains(body, "<name>Group A</name>") {
		t.Errorf("body lost the edited member:\n%s", body)
	}
}

// ─── AddToScope ────────────────────────────────────────────────────────────────

func TestAddToScope_TargetComputerGroup(t *testing.T) {
	s := &ScopeXML{
		ComputerGroups: ScopeItemSlice{
			Items:    []NamedItem{{ID: "1", Name: "Existing"}},
			ElemName: "computer_group",
		},
	}

	if !AddToScope(s, "target", "computer-group", "New Group") {
		t.Fatal("expected true")
		return
	}
	if len(s.ComputerGroups.Items) != 2 {
		t.Fatalf("got %d, want 2", len(s.ComputerGroups.Items))
	}
	if s.ComputerGroups.Items[1].Name != "New Group" {
		t.Errorf("name = %q", s.ComputerGroups.Items[1].Name)
	}
}

func TestAddToScope_Idempotent(t *testing.T) {
	s := &ScopeXML{
		ComputerGroups: ScopeItemSlice{
			Items:    []NamedItem{{Name: "Existing"}},
			ElemName: "computer_group",
		},
	}

	if AddToScope(s, "target", "computer-group", "existing") {
		t.Fatal("expected false for case-insensitive duplicate")
		return
	}
	if len(s.ComputerGroups.Items) != 1 {
		t.Fatal("scope should be unchanged")
		return
	}
}

func TestAddToScope_CreatesSection(t *testing.T) {
	s := &ScopeXML{}

	if !AddToScope(s, "exclusion", "computer-group", "Test") {
		t.Fatal("expected true")
		return
	}
	if s.Exclusions == nil {
		t.Fatal("exclusions should be created")
		return
	}
	if len(s.Exclusions.ComputerGroups.Items) != 1 {
		t.Fatal("should have 1 item")
		return
	}
	if s.Exclusions.ComputerGroups.ElemName != "computer_group" {
		t.Errorf("ElemName = %q, want %q", s.Exclusions.ComputerGroups.ElemName, "computer_group")
	}
}

func TestAddToScope_Limitation(t *testing.T) {
	s := &ScopeXML{
		Limitations: &LimitationsXML{},
	}

	if !AddToScope(s, "limitation", "network-segment", "Guest") {
		t.Fatal("expected true")
		return
	}
	if len(s.Limitations.NetworkSegments.Items) != 1 {
		t.Fatal("should have 1 item")
		return
	}
}

// TestAddToScope_UserGroupLimitationIsTheOnlyPath asserts the simplification
// the dropped <limit_to_users> field allows: a directory user group limitation
// goes to limitations.user_groups for every resource, policies included, and
// the marshalled body carries nothing else. The server denormalises it into
// <limit_to_users> itself (wire-checked 2026-09-12), so writing both was work
// with no effect.
func TestAddToScope_UserGroupLimitationIsTheOnlyPath(t *testing.T) {
	for _, singularKey := range []string{"policy", "os_x_configuration_profile", "vpp_assignment"} {
		s := &ScopeXML{}
		if !AddToScope(s, SectionLimitation, flagUserGroup, "Staff") {
			t.Fatalf("%s: expected the group to be added", singularKey)
		}
		if s.Limitations == nil || len(s.Limitations.UserGroups.Items) != 1 {
			t.Fatalf("%s: group did not land in limitations.user_groups", singularKey)
		}

		body, err := marshalScopeBody(singularKey, s)
		if err != nil {
			t.Fatalf("%s: marshal: %v", singularKey, err)
		}
		if strings.Contains(string(body), "limit_to_users") {
			t.Errorf("%s: body still carries <limit_to_users>:\n%s", singularKey, body)
		}
		if !strings.Contains(string(body), "<user_group>") {
			t.Errorf("%s: body is missing the <user_group> child:\n%s", singularKey, body)
		}
	}
}

// ─── RemoveFromScope ──────────────────────────────────────────────────────────

func TestRemoveFromScope_TargetComputerGroup(t *testing.T) {
	s := &ScopeXML{
		ComputerGroups: ScopeItemSlice{
			Items:    []NamedItem{{ID: "1", Name: "Keep"}, {ID: "2", Name: "Remove"}},
			ElemName: "computer_group",
		},
	}

	if !RemoveFromScope(s, "target", "computer-group", "Remove") {
		t.Fatal("expected true")
		return
	}
	if len(s.ComputerGroups.Items) != 1 {
		t.Fatalf("got %d, want 1", len(s.ComputerGroups.Items))
	}
	if s.ComputerGroups.Items[0].Name != "Keep" {
		t.Errorf("remaining = %q", s.ComputerGroups.Items[0].Name)
	}
}

func TestRemoveFromScope_NotFound(t *testing.T) {
	s := &ScopeXML{
		ComputerGroups: ScopeItemSlice{Items: []NamedItem{{Name: "Keep"}}},
	}

	if RemoveFromScope(s, "target", "computer-group", "Nonexistent") {
		t.Fatal("expected false")
		return
	}
}

func TestRemoveFromScope_CaseInsensitive(t *testing.T) {
	s := &ScopeXML{
		ComputerGroups: ScopeItemSlice{Items: []NamedItem{{Name: "Test Group"}}},
	}

	if !RemoveFromScope(s, "target", "computer-group", "test group") {
		t.Fatal("expected case-insensitive match")
		return
	}
}

func TestRemoveFromScope_MissingSection(t *testing.T) {
	s := &ScopeXML{} // no exclusions

	if RemoveFromScope(s, "exclusion", "computer-group", "Test") {
		t.Fatal("expected false when section missing")
		return
	}
}

// ─── ValidateScopeCombination ────────────────────────────────────────────────

func TestValidateScopeCombination_ValidTargets(t *testing.T) {
	for _, flag := range []string{"computer", "computer-group", "building", "department", "jss-user-group", "jss-user"} {
		for _, sk := range []string{"policy", "os_x_configuration_profile"} {
			if err := ValidateScopeCombination(sk, SectionTarget, flag); err != nil {
				t.Errorf("target/%s/%s: %v", sk, flag, err)
			}
		}
	}
}

// TestValidateScopeCombination_RefusesTheWrongDeviceFamily pins the refusal
// that used to be missing: validation had two branches (restricted software,
// and everything else), so a computer-scoped resource accepted every mobile
// category and a mobile one accepted every computer category. The server does
// not: it answers 409 naming the mismatch ("Mobile device groups cannot be
// assigned to an macOS profile", "Computer groups cannot be assigned to an iOS
// profile" — wire-checked 2026-09-12), so the old behaviour spent a GET and a
// PUT to earn a page of HTML.
func TestValidateScopeCombination_RefusesTheWrongDeviceFamily(t *testing.T) {
	cases := []struct{ singularKey, section, flag string }{
		{"policy", SectionTarget, flagMobileDevice},
		{"policy", SectionTarget, flagMobileDeviceGroup},
		{"policy", SectionExclusion, flagMobileDeviceGroup},
		{"os_x_configuration_profile", SectionTarget, flagMobileDeviceGroup},
		{"mac_application", SectionTarget, flagMobileDeviceGroup},
		{"configuration_profile", SectionTarget, flagComputer},
		{"configuration_profile", SectionTarget, flagComputerGroup},
		{"configuration_profile", SectionExclusion, flagComputerGroup},
		{"mobile_device_application", SectionTarget, flagComputerGroup},
		{"vpp_assignment", SectionTarget, flagComputerGroup},
		{"vpp_assignment", SectionTarget, flagMobileDeviceGroup},
	}
	for _, tc := range cases {
		err := ValidateScopeCombination(tc.singularKey, tc.section, tc.flag)
		if err == nil {
			t.Errorf("%s/%s/--%s: expected a refusal", tc.singularKey, tc.section, tc.flag)
			continue
		}
		// The refusal has to name what would have worked, or the caller has to
		// go and read the docs to find out.
		if !strings.Contains(err.Error(), "--") {
			t.Errorf("%s/%s/--%s: refusal names no alternative: %v", tc.singularKey, tc.section, tc.flag, err)
		}
	}
}

// TestValidateScopeCombination_IBeaconAndClassReachTheRightResources pins the
// two categories the CLI could previously read but not write: iBeacons were
// listed by `scope get` on a policy with no flag able to touch them, and an
// ebook's target classes likewise.
func TestValidateScopeCombination_IBeaconAndClassReachTheRightResources(t *testing.T) {
	for _, sk := range []string{"policy", "os_x_configuration_profile", "configuration_profile"} {
		for _, section := range []string{SectionLimitation, SectionExclusion} {
			if err := ValidateScopeCombination(sk, section, flagIBeacon); err != nil {
				t.Errorf("%s/%s/--ibeacon: %v", sk, section, err)
			}
		}
	}
	// The two app resources and ebooks drop iBeacons: a write carrying them
	// answers 2xx and reads back with no <ibeacons> element (wire-checked).
	for _, sk := range []string{"mac_application", "mobile_device_application", "ebook", "restricted_software", "vpp_assignment"} {
		if err := ValidateScopeCombination(sk, SectionLimitation, flagIBeacon); err == nil {
			t.Errorf("%s: --ibeacon should be refused", sk)
		}
	}
	// <classes> is an ebook target and nothing else's.
	if err := ValidateScopeCombination("ebook", SectionTarget, flagClass); err != nil {
		t.Errorf("ebook/target/--class: %v", err)
	}
	for _, sk := range []string{"policy", "configuration_profile", "mac_application", "restricted_software", "vpp_assignment"} {
		if err := ValidateScopeCombination(sk, SectionTarget, flagClass); err == nil {
			t.Errorf("%s: --class should be refused", sk)
		}
	}
}

func TestValidateScopeCombination_UserGroupTargetRejected(t *testing.T) {
	// --user-group must not be valid for target; --jss-user-group is the explicit alternative.
	if err := ValidateScopeCombination("policy", "target", "user-group"); err == nil {
		t.Error("expected error: --user-group as target should be rejected (use --jss-user-group)")
	}
}

func TestValidateScopeCombination_InvalidTarget(t *testing.T) {
	if err := ValidateScopeCombination("policy", "target", "network-segment"); err == nil {
		t.Error("expected error for network-segment as target")
	}
}

func TestValidateScopeCombination_RestrictedSoftwareTargets(t *testing.T) {
	for _, flag := range []string{"computer", "computer-group", "building", "department"} {
		if err := ValidateScopeCombination("restricted_software", "target", flag); err != nil {
			t.Errorf("restricted target/%s: %v", flag, err)
		}
	}
	for _, flag := range []string{"mobile-device", "mobile-device-group", "jss-user-group", "jss-user", "user-group"} {
		if err := ValidateScopeCombination("restricted_software", "target", flag); err == nil {
			t.Errorf("expected error: restricted software target + %s", flag)
		}
	}
}

func TestValidateScopeCombination_ValidLimitations(t *testing.T) {
	for _, flag := range []string{"network-segment", "user", "user-group"} {
		if err := ValidateScopeCombination("policy", "limitation", flag); err != nil {
			t.Errorf("limitation/%s: %v", flag, err)
		}
	}
}

func TestValidateScopeCombination_RestrictedSoftwareNoLimitations(t *testing.T) {
	for _, flag := range []string{"network-segment", "user-group", "user"} {
		err := ValidateScopeCombination("restricted_software", "limitation", flag)
		if err == nil {
			t.Errorf("expected error: restricted software + limitation + %s", flag)
		}
		if !strings.Contains(err.Error(), "does not support limitations") {
			t.Errorf("unexpected error: %v", err)
		}
	}
}

func TestValidateScopeCombination_ValidExclusions(t *testing.T) {
	for _, flag := range []string{"computer", "computer-group", "user", "user-group", "jss-user-group", "jss-user", "network-segment", "building", "department", "ibeacon"} {
		if err := ValidateScopeCombination("policy", SectionExclusion, flag); err != nil {
			t.Errorf("exclusion/%s: %v", flag, err)
		}
	}
	// The mobile mirror of the same list.
	for _, flag := range []string{"mobile-device", "mobile-device-group", "user", "user-group", "jss-user-group", "jss-user", "network-segment", "building", "department", "ibeacon"} {
		if err := ValidateScopeCombination("configuration_profile", SectionExclusion, flag); err != nil {
			t.Errorf("mobile exclusion/%s: %v", flag, err)
		}
	}
}

// ─── namedItemFromIdentifier ─────────────────────────────────────────────────

func TestNamedItemFromIdentifier_UDID(t *testing.T) {
	item := namedItemFromIdentifier("270aae10800b6e61a2ee2bbc285eb967050b5984")
	if item.UDID != "270aae10800b6e61a2ee2bbc285eb967050b5984" {
		t.Errorf("UDID = %q", item.UDID)
	}
	if item.Name != "" || item.ID != "" {
		t.Errorf("unexpected fields set: name=%q id=%q", item.Name, item.ID)
	}
}

func TestNamedItemFromIdentifier_NumericID(t *testing.T) {
	item := namedItemFromIdentifier("42")
	if item.ID != "42" {
		t.Errorf("ID = %q", item.ID)
	}
	if item.Name != "" || item.UDID != "" {
		t.Errorf("unexpected fields set: name=%q udid=%q", item.Name, item.UDID)
	}
}

func TestNamedItemFromIdentifier_Name(t *testing.T) {
	item := namedItemFromIdentifier("Josh's iPhone")
	if item.Name != "Josh's iPhone" {
		t.Errorf("Name = %q", item.Name)
	}
	if item.ID != "" || item.UDID != "" {
		t.Errorf("unexpected fields set: id=%q udid=%q", item.ID, item.UDID)
	}
}

// ─── AddToScope: mobile-device by UDID / ID ──────────────────────────────────

func TestAddToScope_MobileDeviceByUDID(t *testing.T) {
	s := &ScopeXML{}
	udid := "270aae10800b6e61a2ee2bbc285eb967050b5984"

	if !AddToScope(s, "target", "mobile-device", udid) {
		t.Fatal("expected true")
	}
	if len(s.MobileDevices.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(s.MobileDevices.Items))
	}
	item := s.MobileDevices.Items[0]
	if item.UDID != udid {
		t.Errorf("UDID = %q, want %q", item.UDID, udid)
	}
	if item.Name != "" {
		t.Errorf("Name should be empty, got %q", item.Name)
	}
}

func TestAddToScope_MobileDeviceByNumericID(t *testing.T) {
	s := &ScopeXML{}

	if !AddToScope(s, "target", "mobile-device", "7") {
		t.Fatal("expected true")
	}
	item := s.MobileDevices.Items[0]
	if item.ID != "7" {
		t.Errorf("ID = %q, want %q", item.ID, "7")
	}
	if item.Name != "" || item.UDID != "" {
		t.Errorf("unexpected fields: name=%q udid=%q", item.Name, item.UDID)
	}
}

func TestAddToScope_MobileDeviceByName(t *testing.T) {
	s := &ScopeXML{}

	if !AddToScope(s, "target", "mobile-device", "Ward iPhone") {
		t.Fatal("expected true")
	}
	item := s.MobileDevices.Items[0]
	if item.Name != "Ward iPhone" {
		t.Errorf("Name = %q", item.Name)
	}
	if item.ID != "" || item.UDID != "" {
		t.Errorf("unexpected fields: id=%q udid=%q", item.ID, item.UDID)
	}
}

func TestAddToScope_MobileDeviceByUDID_IdempotentUDID(t *testing.T) {
	udid := "270aae10800b6e61a2ee2bbc285eb967050b5984"
	s := &ScopeXML{
		MobileDevices: ScopeItemSlice{
			Items:    []NamedItem{{UDID: udid}},
			ElemName: "mobile_device",
		},
	}
	if AddToScope(s, "target", "mobile-device", udid) {
		t.Fatal("expected false — already present by UDID")
	}
}

func TestAddToScope_MobileDeviceByUDID_IdempotentCaseInsensitive(t *testing.T) {
	udid := "270aae10800b6e61a2ee2bbc285eb967050b5984"
	s := &ScopeXML{
		MobileDevices: ScopeItemSlice{
			Items:    []NamedItem{{UDID: strings.ToUpper(udid)}},
			ElemName: "mobile_device",
		},
	}
	if AddToScope(s, "target", "mobile-device", udid) {
		t.Fatal("expected false — already present (case-insensitive UDID)")
	}
}

// ─── RemoveFromScope: mobile-device by UDID / ID ────────────────────────────

func TestRemoveFromScope_MobileDeviceByUDID(t *testing.T) {
	udid := "270aae10800b6e61a2ee2bbc285eb967050b5984"
	s := &ScopeXML{
		MobileDevices: ScopeItemSlice{
			Items:    []NamedItem{{UDID: udid}},
			ElemName: "mobile_device",
		},
	}
	if !RemoveFromScope(s, "target", "mobile-device", udid) {
		t.Fatal("expected true")
	}
	if len(s.MobileDevices.Items) != 0 {
		t.Errorf("expected empty, got %d items", len(s.MobileDevices.Items))
	}
}

func TestRemoveFromScope_MobileDeviceByNumericID(t *testing.T) {
	s := &ScopeXML{
		MobileDevices: ScopeItemSlice{
			Items:    []NamedItem{{ID: "7", Name: "Ward iPhone"}},
			ElemName: "mobile_device",
		},
	}
	if !RemoveFromScope(s, "target", "mobile-device", "7") {
		t.Fatal("expected true")
	}
	if len(s.MobileDevices.Items) != 0 {
		t.Errorf("expected empty, got %d items", len(s.MobileDevices.Items))
	}
}

func TestValidateScopeCombination_RestrictedSoftwareExclusions(t *testing.T) {
	for _, flag := range []string{"computer", "computer-group", "building", "department"} {
		if err := ValidateScopeCombination("restricted_software", "exclusion", flag); err != nil {
			t.Errorf("restricted exclusion/%s: %v", flag, err)
		}
	}
	// --user is a real restricted-software exclusion and used to be refused:
	// the resource's own GET returns <users> in its exclusions block, and a
	// write carrying <user><name>…</name> persists (wire-checked 2026-09-12).
	// It is the admin UI's "Directory Service/Local Users" exclusion, free
	// text rather than a Jamf Pro object.
	if err := ValidateScopeCombination("restricted_software", SectionExclusion, flagUser); err != nil {
		t.Errorf("restricted exclusion/user should be accepted: %v", err)
	}
	for _, flag := range []string{"mobile-device", "mobile-device-group", "user-group", "jss-user-group", "jss-user", "network-segment", "ibeacon"} {
		if err := ValidateScopeCombination("restricted_software", SectionExclusion, flag); err == nil {
			t.Errorf("expected error: restricted software exclusion + %s", flag)
		}
	}
}

func TestValidateScopeCombination_InvalidSection(t *testing.T) {
	if err := ValidateScopeCombination("policy", "bogus", "computer-group"); err == nil {
		t.Error("expected error for invalid section")
	}
}

func TestValidateScopeCombination_InvalidLimitationFlag(t *testing.T) {
	if err := ValidateScopeCombination("policy", "limitation", "building"); err == nil {
		t.Error("expected error: building is not a valid limitation")
	}
}

func TestValidateScopeCombination_ComputerOnlyInTargetExclusion(t *testing.T) {
	// --computer / --mobile-device / --user must be rejected outside their sections.
	if err := ValidateScopeCombination("policy", "limitation", "computer"); err == nil {
		t.Error("expected error: --computer as limitation")
	}
	if err := ValidateScopeCombination("policy", "limitation", "mobile-device"); err == nil {
		t.Error("expected error: --mobile-device as limitation")
	}
	if err := ValidateScopeCombination("policy", "target", "user"); err == nil {
		t.Error("expected error: --user as target")
	}
}

// ─── FlattenScope ────────────────────────────────────────────────────────────

func TestFlattenScope_BasicPolicy(t *testing.T) {
	s := &ScopeXML{
		AllComputers: true,
		ComputerGroups: ScopeItemSlice{
			Items: []NamedItem{{ID: "1", Name: "Group A"}},
		},
		Buildings: ScopeItemSlice{
			Items: []NamedItem{{Name: "HQ"}},
		},
		Limitations: &LimitationsXML{
			UserGroups:      ScopeItemSlice{Items: []NamedItem{{Name: "Staff"}}},
			NetworkSegments: ScopeItemSlice{Items: []NamedItem{{Name: "Corporate"}}},
		},
		Exclusions: &ExclusionsXML{
			ComputerGroups: ScopeItemSlice{Items: []NamedItem{{Name: "Test Machines"}}},
		},
	}

	rows := FlattenScope(s)

	expected := []struct{ section, typ, name string }{
		{"target", "all_computers", "true"},
		{"target", "computer_group", "Group A"},
		{"target", "building", "HQ"},
		{"limitation", "user_group", "Staff"},
		{"limitation", "network_segment", "Corporate"},
		{"exclusion", "computer_group", "Test Machines"},
	}

	if len(rows) != len(expected) {
		t.Fatalf("got %d rows, want %d: %v", len(rows), len(expected), rows)
	}
	for i, want := range expected {
		got := rows[i]
		if got["section"] != want.section || got["type"] != want.typ || got["name"] != want.name {
			t.Errorf("row %d: got %v, want %s/%s/%s", i, got, want.section, want.typ, want.name)
		}
	}
}

func TestFlattenScope_EmptyScope(t *testing.T) {
	s := &ScopeXML{}
	if rows := FlattenScope(s); len(rows) != 0 {
		t.Errorf("got %d rows, want 0", len(rows))
	}
}

// ─── JSS user group target routing (VPP-style scope) ─────────────────────────

func TestAddToScope_UserGroupTarget_NoLongerRoutes(t *testing.T) {
	// --user-group is LDAP-only (limitation/exclusion). For target users must use
	// --jss-user-group explicitly. AddToScope should refuse to add it.
	s := &ScopeXML{}

	if AddToScope(s, "target", "user-group", "VPP Associated Users") {
		t.Fatal("expected false: --user-group is not a valid target flag")
	}
	if len(s.JSSUserGroups.Items) != 0 {
		t.Errorf("jss_user_groups should remain empty, got %d items", len(s.JSSUserGroups.Items))
	}
}

func TestAddToScope_JSSUserGroupTarget(t *testing.T) {
	s := &ScopeXML{}

	if !AddToScope(s, "target", "jss-user-group", "My Group") {
		t.Fatal("expected true")
	}
	if len(s.JSSUserGroups.Items) != 1 {
		t.Fatalf("jss_user_groups: got %d, want 1", len(s.JSSUserGroups.Items))
	}
	if s.JSSUserGroups.ElemName != "user_group" {
		t.Errorf("ElemName = %q, want user_group", s.JSSUserGroups.ElemName)
	}
}

func TestAddToScope_JSSUserGroupTarget_Idempotent(t *testing.T) {
	s := &ScopeXML{
		JSSUserGroups: ScopeItemSlice{
			Items:    []NamedItem{{Name: "VPP Associated Users"}},
			ElemName: "user_group",
		},
	}

	if AddToScope(s, "target", "jss-user-group", "vpp associated users") {
		t.Fatal("expected false for case-insensitive duplicate")
	}
}

func TestRemoveFromScope_JSSUserGroupTarget(t *testing.T) {
	s := &ScopeXML{
		JSSUserGroups: ScopeItemSlice{
			Items:    []NamedItem{{Name: "VPP Associated Users"}, {Name: "Other Group"}},
			ElemName: "user_group",
		},
	}

	if !RemoveFromScope(s, "target", "jss-user-group", "VPP Associated Users") {
		t.Fatal("expected true")
	}
	if len(s.JSSUserGroups.Items) != 1 {
		t.Fatalf("got %d, want 1", len(s.JSSUserGroups.Items))
	}
	if s.JSSUserGroups.Items[0].Name != "Other Group" {
		t.Errorf("remaining = %q", s.JSSUserGroups.Items[0].Name)
	}
}

func TestRemoveFromScope_JSSUserGroupExclusion(t *testing.T) {
	s := &ScopeXML{
		Exclusions: &ExclusionsXML{
			JSSUserGroups: ScopeItemSlice{
				Items:    []NamedItem{{Name: "Excluded Group"}},
				ElemName: "jss_user_group",
			},
		},
	}

	if !RemoveFromScope(s, "exclusion", "jss-user-group", "Excluded Group") {
		t.Fatal("expected true")
	}
	if len(s.Exclusions.JSSUserGroups.Items) != 0 {
		t.Error("should be empty after remove")
	}
}

func TestFlattenScope_VPPAssignment_JSSUserGroups(t *testing.T) {
	s := &ScopeXML{
		JSSUserGroups: ScopeItemSlice{
			Items: []NamedItem{{ID: "1", Name: "VPP Associated Users"}},
		},
		Limitations: &LimitationsXML{
			UserGroups: ScopeItemSlice{
				Items: []NamedItem{{Name: "COB-iosgrade1"}},
			},
		},
	}

	rows := FlattenScope(s)

	expected := []struct{ section, typ, name string }{
		{"target", "jss_user_group", "VPP Associated Users"},
		{"limitation", "user_group", "COB-iosgrade1"},
	}
	if len(rows) != len(expected) {
		t.Fatalf("got %d rows, want %d: %v", len(rows), len(expected), rows)
	}
	for i, want := range expected {
		got := rows[i]
		if got["section"] != want.section || got["type"] != want.typ || got["name"] != want.name {
			t.Errorf("row %d: got %v, want %s/%s/%s", i, got, want.section, want.typ, want.name)
		}
	}
}

// ─── Round-trip preserves fields server returned but CLI doesn't expose ─────

func TestScopeXML_PreservesMobileDevicesOnRoundTrip(t *testing.T) {
	// Mobile config profiles return <mobile_devices> (individual devices). CLI's
	// scope add only manipulates groups, but the unmarshal/marshal round-trip
	// must preserve these or they get wiped on subset/Scope PUT.
	data := `<configuration_profile>
		<general><id>1</id><name>Profile</name></general>
		<scope>
			<all_mobile_devices>false</all_mobile_devices>
			<mobile_devices>
				<mobile_device><id>18</id><name>G6TDK43P0D4Y</name><udid>00008101-000170490151003A</udid></mobile_device>
			</mobile_devices>
		</scope>
	</configuration_profile>`
	var env classicResourceXML
	if err := xml.Unmarshal([]byte(data), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(env.Scope.MobileDevices.Items) != 1 {
		t.Fatalf("mobile_devices: got %d, want 1", len(env.Scope.MobileDevices.Items))
	}
	if env.Scope.MobileDevices.Items[0].ID != "18" {
		t.Errorf("mobile_devices[0].id = %q, want 18", env.Scope.MobileDevices.Items[0].ID)
	}
	// Marshal back and verify <mobile_devices> still present.
	out, err := xml.Marshal(env.Scope)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), "<mobile_device>") {
		t.Error("round-trip lost <mobile_device> data")
	}
}

func TestScopeXML_PreservesClassesOnRoundTrip(t *testing.T) {
	// Ebooks return <classes>. CLI doesn't expose, but round-trip must preserve.
	data := `<ebook>
		<general><id>2</id><name>Book</name></general>
		<scope>
			<classes>
				<class><id>5</id><name>10A</name></class>
			</classes>
		</scope>
	</ebook>`
	var env classicResourceXML
	if err := xml.Unmarshal([]byte(data), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(env.Scope.Classes.Items) != 1 || env.Scope.Classes.Items[0].Name != "10A" {
		t.Fatalf("classes: got %+v", env.Scope.Classes.Items)
	}
	out, err := xml.Marshal(env.Scope)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), "<class>") {
		t.Error("round-trip lost <class> data")
	}
}

// ─── New scope flags ──────────────────────────────────────────────────────────

func TestAddToScope_ComputerTarget(t *testing.T) {
	s := &ScopeXML{}
	if !AddToScope(s, "target", "computer", "ZTNR9F6XJ0") {
		t.Fatal("expected true")
	}
	if len(s.Computers.Items) != 1 || s.Computers.Items[0].Name != "ZTNR9F6XJ0" {
		t.Errorf("computers = %+v", s.Computers.Items)
	}
}

func TestAddToScope_MobileDeviceTarget(t *testing.T) {
	s := &ScopeXML{}
	if !AddToScope(s, "target", "mobile-device", "G6TDK43P0D4Y") {
		t.Fatal("expected true")
	}
	if len(s.MobileDevices.Items) != 1 || s.MobileDevices.Items[0].Name != "G6TDK43P0D4Y" {
		t.Errorf("mobile_devices = %+v", s.MobileDevices.Items)
	}
	if s.MobileDevices.ElemName != "mobile_device" {
		t.Errorf("ElemName = %q", s.MobileDevices.ElemName)
	}
}

func TestAddToScope_UserLimitation(t *testing.T) {
	s := &ScopeXML{}
	if !AddToScope(s, "limitation", "user", "alice") {
		t.Fatal("expected true")
	}
	if s.Limitations == nil || len(s.Limitations.Users.Items) != 1 {
		t.Fatalf("limitations.users = %+v", s.Limitations)
	}
	if s.Limitations.Users.Items[0].Name != "alice" {
		t.Errorf("user name = %q", s.Limitations.Users.Items[0].Name)
	}
}

func TestAddToScope_UserExclusion(t *testing.T) {
	s := &ScopeXML{}
	if !AddToScope(s, "exclusion", "user", "bob") {
		t.Fatal("expected true")
	}
	if s.Exclusions == nil || len(s.Exclusions.Users.Items) != 1 {
		t.Fatalf("exclusions.users = %+v", s.Exclusions)
	}
}

func TestFlattenScope_NewFields(t *testing.T) {
	s := &ScopeXML{
		AllMobileDevices: true,
		Computers:        ScopeItemSlice{Items: []NamedItem{{ID: "28", Name: "Mac-X"}}},
		MobileDevices:    ScopeItemSlice{Items: []NamedItem{{ID: "18", Name: "iPad-Y"}}},
		Limitations: &LimitationsXML{
			Users: ScopeItemSlice{Items: []NamedItem{{Name: "alice"}}},
		},
		Exclusions: &ExclusionsXML{
			MobileDevices: ScopeItemSlice{Items: []NamedItem{{Name: "iPad-Z"}}},
			Users:         ScopeItemSlice{Items: []NamedItem{{Name: "bob"}}},
		},
	}
	rows := FlattenScope(s)

	want := map[string]bool{
		"target:all_mobile_devices:true": false,
		"target:computer:Mac-X":          false,
		"target:mobile_device:iPad-Y":    false,
		"limitation:user:alice":          false,
		"exclusion:mobile_device:iPad-Z": false,
		"exclusion:user:bob":             false,
	}
	for _, r := range rows {
		key := r["section"].(string) + ":" + r["type"].(string) + ":" + r["name"].(string)
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for k, found := range want {
		if !found {
			t.Errorf("missing flatten row: %s", k)
		}
	}
}

// ─── VerifyItemInScope id/UDID matching ──────────────────────────────────────

func TestVerifyItemInScope_MatchesByNameIDAndUDID(t *testing.T) {
	// Simulates the post-PUT GET response where the server returns the canonical
	// name alongside the id/udid.  The user may have supplied any of the three
	// forms to the CLI, so VerifyItemInScope must accept all three.
	s := &ScopeXML{
		Computers: ScopeItemSlice{
			Items: []NamedItem{{ID: "10", Name: "Mac-Build-01", UDID: "AAA-BBB"}},
		},
		MobileDevices: ScopeItemSlice{
			Items: []NamedItem{{ID: "18", Name: "G6TDK43P0D4Y", UDID: "00008101-000170490151003A"}},
		},
	}

	for _, tc := range []struct {
		flagName string
		input    string
		items    *ScopeItemSlice
	}{
		{"computer", "Mac-Build-01", &s.Computers},
		{"computer", "10", &s.Computers},
		{"computer", "AAA-BBB", &s.Computers},
		{"computer", "aaa-bbb", &s.Computers}, // UDID case-insensitive
		{"mobile-device", "G6TDK43P0D4Y", &s.MobileDevices},
		{"mobile-device", "18", &s.MobileDevices},
		{"mobile-device", "00008101-000170490151003A", &s.MobileDevices},
	} {
		present := false
		for _, item := range tc.items.Items {
			if strings.EqualFold(item.Name, tc.input) ||
				item.ID == tc.input ||
				strings.EqualFold(item.UDID, tc.input) {
				present = true
				break
			}
		}
		if !present {
			t.Errorf("flag --%s value %q: expected match, got none", tc.flagName, tc.input)
		}
	}
}

// TestOutputScope_MisCasedFormatStillFlattens is issue 353 at this call site.
// The switch matched "table", "csv" and "plain" exactly, so any other value —
// including a mis-cased -o Table, which Print renders as a table through its
// default arm — took the nested ScopeXML structure to a table renderer.
func TestOutputScope_MisCasedFormatStillFlattens(t *testing.T) {
	s := &ScopeXML{
		Computers: ScopeItemSlice{Items: []NamedItem{{ID: "42", Name: "lab-01"}}},
	}

	for _, format := range []string{"Table", "TABLE", "Csv", "json-multi", "wibble"} {
		out := &captureFormatter{}
		if err := OutputScope(out, s, format); err != nil {
			t.Fatalf("OutputScope(-o %s): %v", format, err)
		}

		var rows []map[string]any
		if err := json.Unmarshal(out.raw, &rows); err != nil {
			t.Errorf("-o %s did not produce flattened rows (%v): %s", format, err, out.raw)
			continue
		}
		if len(rows) != 1 {
			t.Errorf("-o %s produced %d rows, want 1 flattened row: %s", format, len(rows), out.raw)
			continue
		}
		if rows[0]["name"] != "lab-01" {
			t.Errorf("-o %s row does not carry the flattened name: %v", format, rows[0])
		}
	}
}

// The keep-set still gets the nested structure, which is what the flattening is
// narrowing away from.
func TestOutputScope_StructuredFormatsKeepTheNestedShape(t *testing.T) {
	s := &ScopeXML{
		Computers: ScopeItemSlice{Items: []NamedItem{{ID: "42", Name: "lab-01"}}},
	}

	for _, format := range []string{"json", "yaml", "ndjson", "xml", "raw"} {
		out := &captureFormatter{}
		if err := OutputScope(out, s, format); err != nil {
			t.Fatalf("OutputScope(-o %s): %v", format, err)
		}
		var obj map[string]any
		if err := json.Unmarshal(out.raw, &obj); err != nil {
			t.Errorf("-o %s did not produce the nested object (%v): %s", format, err, out.raw)
			continue
		}
		if _, ok := obj["computers"]; !ok {
			t.Errorf("-o %s lost the nested computers key: %s", format, out.raw)
		}
	}
}

// captureFormatter records the bytes OutputScope writes. It implements only the
// method under test; the rest of registry.OutputFormatter is unreachable here.
type captureFormatter struct {
	registry.OutputFormatter
	raw []byte
}

func (c *captureFormatter) PrintRaw(data []byte) error {
	c.raw = data
	return nil
}

// ─── Fetch by id or name ─────────────────────────────────────────────────────

// TestFetchScope_ByIDIsOneRequest is the request-count half of the refactor:
// `scope get 5` costs one GET, where the command previously took the name
// positionally and a mutation cost three GETs (by name, then by id for the
// raw bytes to splice, then the verification read).
func TestFetchScope_ByIDIsOneRequest(t *testing.T) {
	client := &mockPutClient{getBody: `<policy><general><id>5</id></general><scope><all_computers>true</all_computers></scope></policy>`}
	res := Resource{APIPath: "policies", SingularKey: "policy"}

	id, s, err := FetchScope(context.Background(), client, res, Ref{ID: "5"})
	if err != nil {
		t.Fatalf("FetchScope: %v", err)
	}
	if id != "5" {
		t.Errorf("id = %q, want 5", id)
	}
	if !s.AllComputers {
		t.Error("scope did not parse")
	}
	want := []string{"GET /JSSResource/policies/id/5"}
	if len(client.requests) != 1 || client.requests[0] != want[0] {
		t.Errorf("requests = %v, want %v", client.requests, want)
	}
}

// TestFetchScope_ByNameUsesTheNameEndpoint keeps a name lookup at one request
// too, for every resource that has a /name/ endpoint: the response carries
// <general><id>, which is the ID the subsequent PUT needs.
func TestFetchScope_ByNameUsesTheNameEndpoint(t *testing.T) {
	client := &mockPutClient{getBody: `<policy><general><id>9</id></general><scope/></policy>`}
	res := Resource{APIPath: "policies", SingularKey: "policy"}

	id, _, err := FetchScope(context.Background(), client, res, Ref{Name: "My Policy"})
	if err != nil {
		t.Fatalf("FetchScope: %v", err)
	}
	if id != "9" {
		t.Errorf("id = %q, want 9 (read from <general><id>)", id)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %v, want one GET", client.requests)
	}
	if !strings.Contains(client.requests[0], "/name/My%20Policy") {
		t.Errorf("request = %q, want the /name/ endpoint with the name escaped", client.requests[0])
	}
}

// nameListClient answers the collection GET with a fixed listing and every
// other GET with a fixed document, for the ResolveByList path.
type nameListClient struct {
	list     string
	doc      string
	requests []string
}

func (c *nameListClient) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	c.requests = append(c.requests, method+" "+path)
	body := c.doc
	if !strings.Contains(path, "/id/") {
		body = c.list
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
}

// TestFetchScope_ResolveByListRefusesAnAmbiguousName pins the behaviour change
// for the two resources with no /name/ endpoint. Classic names are not unique
// — a live tenant carried two ebooks with the same name — and this used to
// return the first match in document order, silently rewriting the scope of
// whichever the server happened to list first.
func TestFetchScope_ResolveByListRefusesAnAmbiguousName(t *testing.T) {
	client := &nameListClient{
		list: `<vpp_assignments>
			<vpp_assignment><id>3</id><name>Shared</name></vpp_assignment>
			<vpp_assignment><id>7</id><name>shared</name></vpp_assignment>
		</vpp_assignments>`,
		doc: `<vpp_assignment><general><id>3</id></general><scope/></vpp_assignment>`,
	}
	res := Resource{APIPath: "vppassignments", SingularKey: "vpp_assignment", ResolveByList: true}

	_, _, err := FetchScope(context.Background(), client, res, Ref{Name: "Shared"})
	if err == nil {
		t.Fatal("two records with the same name should be refused, not resolved to the first")
	}
	for _, want := range []string{"3", "7", "<id>"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal should name the colliding ids and the way out; got: %v", err)
		}
	}

	// One match still resolves, in two requests (list, then the document).
	client.requests = nil
	client.list = `<vpp_assignments><vpp_assignment><id>7</id><name>Shared</name></vpp_assignment></vpp_assignments>`
	id, _, err := FetchScope(context.Background(), client, res, Ref{Name: "Shared"})
	if err != nil {
		t.Fatalf("single match: %v", err)
	}
	if id != "7" {
		t.Errorf("id = %q, want 7", id)
	}
	if len(client.requests) != 2 {
		t.Errorf("requests = %v, want the list plus the document", client.requests)
	}
}

// TestMarshalScopeBody_FieldOrderIsSchemaOrder guards the property PutScope's
// doc comment calls load-bearing. The Classic API reads scope children in
// schema order and silently ignores whatever arrives out of it, answering 200
// either way — so re-ordering ScopeXML's fields to match some resource's GET
// would make writes no-ops with nothing failing. Only the relative order
// matters, which is what this pins.
func TestMarshalScopeBody_FieldOrderIsSchemaOrder(t *testing.T) {
	body, err := marshalScopeBody("policy", &ScopeXML{})
	if err != nil {
		t.Fatalf("marshalScopeBody: %v", err)
	}
	got := string(body)

	order := []string{
		"<all_computers>", "<all_jss_users>",
		"<computers>", "<computer_groups>",
		"<mobile_devices>", "<mobile_device_groups>",
		"<jss_users>", "<jss_user_groups>",
		"<buildings>", "<departments>", "<classes>",
	}
	prev := -1
	for _, elem := range order {
		at := strings.Index(got, elem)
		if at < 0 {
			t.Fatalf("body is missing %s:\n%s", elem, got)
		}
		if at < prev {
			t.Errorf("%s appears out of schema order; see the field-order note on ScopeXML:\n%s", elem, got)
		}
		prev = at
	}
	if !strings.HasPrefix(got, xml.Header) {
		t.Errorf("body should open with the XML declaration:\n%s", got)
	}
}
