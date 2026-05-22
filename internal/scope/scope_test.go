// Copyright 2026, Jamf Software LLC

package scope

import (
	"encoding/xml"
	"strings"
	"testing"
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
				<user_groups/>
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
	if env.Scope.LimitToUsers == nil {
		t.Fatal("limit_to_users is nil")
		return
	}
	if len(env.Scope.LimitToUsers.UserGroups.Items) != 2 {
		t.Errorf("limit_to_users.user_groups: got %d, want 2", len(env.Scope.LimitToUsers.UserGroups.Items))
	}
	if env.Scope.LimitToUsers.UserGroups.Items[0] != "Staff" {
		t.Errorf("first limit user_group = %q", env.Scope.LimitToUsers.UserGroups.Items[0])
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

func TestScopeUpdateXML_Marshal(t *testing.T) {
	s := ScopeXML{
		ComputerGroups: ScopeItemSlice{
			Items:    []NamedItem{{Name: "Test"}},
			ElemName: "computer_group",
		},
	}
	envelope := scopeUpdateXML{
		XMLName: xml.Name{Local: "policy"},
		Scope:   s,
	}

	data, err := xml.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	xmlStr := string(data)

	if !strings.Contains(xmlStr, "<policy>") {
		t.Error("missing <policy> wrapper")
	}
	if !strings.Contains(xmlStr, "<scope>") {
		t.Error("missing <scope>")
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

	if !AddToScope(s, "policy", "target", "computer-group", "New Group") {
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

	if AddToScope(s, "policy", "target", "computer-group", "existing") {
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

	if !AddToScope(s, "policy", "exclusion", "computer-group", "Test") {
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

	if !AddToScope(s, "policy", "limitation", "network-segment", "Guest") {
		t.Fatal("expected true")
		return
	}
	if len(s.Limitations.NetworkSegments.Items) != 1 {
		t.Fatal("should have 1 item")
		return
	}
}

// ─── AddToScope: policy limitation user_group special case ───────────────────

func TestAddToScope_PolicyLimitUserGroup(t *testing.T) {
	s := &ScopeXML{}

	if !AddToScope(s, "policy", "limitation", "user-group", "Staff") {
		t.Fatal("expected true")
		return
	}

	if s.LimitToUsers == nil {
		t.Fatal("limit_to_users should be created")
		return
	}
	groups := s.LimitToUsers.UserGroups.Items
	if len(groups) != 1 || groups[0] != "Staff" {
		t.Errorf("got %v, want [Staff]", groups)
	}
}

func TestAddToScope_PolicyLimitUserGroup_Idempotent(t *testing.T) {
	s := &ScopeXML{
		LimitToUsers: &LimitToUsersXML{
			UserGroups: ScopeStringSlice{Items: []string{"Staff"}, ElemName: "user_group"},
		},
	}

	if AddToScope(s, "policy", "limitation", "user-group", "staff") {
		t.Fatal("expected false for case-insensitive duplicate")
		return
	}
}

func TestAddToScope_NonPolicyLimitUserGroup(t *testing.T) {
	s := &ScopeXML{}

	if !AddToScope(s, "os_x_configuration_profile", "limitation", "user-group", "Staff") {
		t.Fatal("expected true")
		return
	}

	// Should go to limitations.user_groups, NOT limit_to_users
	if s.LimitToUsers != nil {
		t.Error("non-policy should not use limit_to_users")
	}
	if s.Limitations == nil || len(s.Limitations.UserGroups.Items) != 1 {
		t.Error("should be in limitations.user_groups")
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

	if !RemoveFromScope(s, "policy", "target", "computer-group", "Remove") {
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

	if RemoveFromScope(s, "policy", "target", "computer-group", "Nonexistent") {
		t.Fatal("expected false")
		return
	}
}

func TestRemoveFromScope_CaseInsensitive(t *testing.T) {
	s := &ScopeXML{
		ComputerGroups: ScopeItemSlice{Items: []NamedItem{{Name: "Test Group"}}},
	}

	if !RemoveFromScope(s, "policy", "target", "computer-group", "test group") {
		t.Fatal("expected case-insensitive match")
		return
	}
}

func TestRemoveFromScope_MissingSection(t *testing.T) {
	s := &ScopeXML{} // no exclusions

	if RemoveFromScope(s, "policy", "exclusion", "computer-group", "Test") {
		t.Fatal("expected false when section missing")
		return
	}
}

func TestRemoveFromScope_PolicyLimitUserGroup(t *testing.T) {
	s := &ScopeXML{
		LimitToUsers: &LimitToUsersXML{
			UserGroups: ScopeStringSlice{Items: []string{"Staff", "Admins"}, ElemName: "user_group"},
		},
	}

	if !RemoveFromScope(s, "policy", "limitation", "user-group", "staff") {
		t.Fatal("expected true (case-insensitive)")
		return
	}
	if len(s.LimitToUsers.UserGroups.Items) != 1 {
		t.Fatalf("got %d, want 1", len(s.LimitToUsers.UserGroups.Items))
	}
	if s.LimitToUsers.UserGroups.Items[0] != "Admins" {
		t.Errorf("remaining = %q", s.LimitToUsers.UserGroups.Items[0])
	}
}

func TestRemoveFromScope_PolicyLimitUserGroup_NotFound(t *testing.T) {
	s := &ScopeXML{
		LimitToUsers: &LimitToUsersXML{
			UserGroups: ScopeStringSlice{Items: []string{"Staff"}},
		},
	}

	if RemoveFromScope(s, "policy", "limitation", "user-group", "Nonexistent") {
		t.Fatal("expected false")
		return
	}
}

func TestRemoveFromScope_PolicyLimitUserGroup_InLimitations(t *testing.T) {
	s := &ScopeXML{
		LimitToUsers: &LimitToUsersXML{
			UserGroups: ScopeStringSlice{Items: []string{}, ElemName: "user_group"},
		},
		Limitations: &LimitationsXML{
			UserGroups: ScopeItemSlice{
				Items:    []NamedItem{{ID: "5", Name: "Staff"}},
				ElemName: "user_group",
			},
		},
	}

	if !RemoveFromScope(s, "policy", "limitation", "user-group", "Staff") {
		t.Fatal("expected true — should find in limitations/user_groups")
		return
	}
	if len(s.Limitations.UserGroups.Items) != 0 {
		t.Error("should have removed from limitations/user_groups")
	}
}

func TestRemoveFromScope_PolicyLimitUserGroup_InBothLocations(t *testing.T) {
	s := &ScopeXML{
		LimitToUsers: &LimitToUsersXML{
			UserGroups: ScopeStringSlice{Items: []string{"Staff"}, ElemName: "user_group"},
		},
		Limitations: &LimitationsXML{
			UserGroups: ScopeItemSlice{
				Items:    []NamedItem{{Name: "Staff"}},
				ElemName: "user_group",
			},
		},
	}

	if !RemoveFromScope(s, "policy", "limitation", "user-group", "Staff") {
		t.Fatal("expected true")
		return
	}
	if len(s.LimitToUsers.UserGroups.Items) != 0 {
		t.Error("should have removed from limit_to_users")
	}
	if len(s.Limitations.UserGroups.Items) != 0 {
		t.Error("should have removed from limitations/user_groups")
	}
}

// ─── ValidateScopeCombination ────────────────────────────────────────────────

func TestValidateScopeCombination_ValidTargets(t *testing.T) {
	for _, flag := range []string{"computer", "computer-group", "mobile-device", "mobile-device-group", "building", "department", "jss-user-group", "jss-user"} {
		for _, sk := range []string{"policy", "os_x_configuration_profile"} {
			if err := ValidateScopeCombination(sk, "target", flag); err != nil {
				t.Errorf("target/%s/%s: %v", sk, flag, err)
			}
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
	for _, flag := range []string{"computer", "computer-group", "mobile-device", "mobile-device-group", "user", "user-group", "jss-user-group", "jss-user", "network-segment", "building", "department"} {
		if err := ValidateScopeCombination("policy", "exclusion", flag); err != nil {
			t.Errorf("exclusion/%s: %v", flag, err)
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

	if !AddToScope(s, "configuration_profile", "target", "mobile-device", udid) {
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

	if !AddToScope(s, "configuration_profile", "target", "mobile-device", "7") {
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

	if !AddToScope(s, "configuration_profile", "target", "mobile-device", "Ward iPhone") {
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
	if AddToScope(s, "configuration_profile", "target", "mobile-device", udid) {
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
	if AddToScope(s, "configuration_profile", "target", "mobile-device", udid) {
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
	if !RemoveFromScope(s, "configuration_profile", "target", "mobile-device", udid) {
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
	if !RemoveFromScope(s, "configuration_profile", "target", "mobile-device", "7") {
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
	for _, flag := range []string{"mobile-device", "mobile-device-group", "user", "user-group", "jss-user-group", "jss-user", "network-segment"} {
		if err := ValidateScopeCombination("restricted_software", "exclusion", flag); err == nil {
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
		LimitToUsers: &LimitToUsersXML{
			UserGroups: ScopeStringSlice{Items: []string{"Staff"}},
		},
		Limitations: &LimitationsXML{
			NetworkSegments: ScopeItemSlice{Items: []NamedItem{{Name: "Corporate"}}},
		},
		Exclusions: &ExclusionsXML{
			ComputerGroups: ScopeItemSlice{Items: []NamedItem{{Name: "Test Machines"}}},
		},
	}

	rows := FlattenScope(s, "policy")

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

func TestFlattenScope_PolicyUserGroupNoDuplicates(t *testing.T) {
	s := &ScopeXML{
		LimitToUsers: &LimitToUsersXML{
			UserGroups: ScopeStringSlice{Items: []string{"Staff", "Faculty"}},
		},
		Limitations: &LimitationsXML{
			UserGroups: ScopeItemSlice{Items: []NamedItem{
				{ID: "1", Name: "Staff"},
				{ID: "2", Name: "Faculty"},
			}},
			NetworkSegments: ScopeItemSlice{Items: []NamedItem{{Name: "Corporate"}}},
		},
	}

	rows := FlattenScope(s, "policy")

	var ugCount int
	for _, r := range rows {
		if r["type"] == "user_group" {
			ugCount++
		}
	}
	if ugCount != 2 {
		t.Errorf("got %d user_group rows, want 2 (no duplicates): %v", ugCount, rows)
	}

	expected := []struct{ section, typ, name string }{
		{"limitation", "user_group", "Staff"},
		{"limitation", "user_group", "Faculty"},
		{"limitation", "network_segment", "Corporate"},
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
	if rows := FlattenScope(s, "policy"); len(rows) != 0 {
		t.Errorf("got %d rows, want 0", len(rows))
	}
}

// ─── isPolicyLimitUserGroup ──────────────────────────────────────────────────

func TestIsPolicyLimitUserGroup(t *testing.T) {
	tests := []struct {
		singularKey, section, flagName string
		want                           bool
	}{
		{"policy", "limitation", "user-group", true},
		{"policy", "exclusion", "user-group", false},
		{"policy", "limitation", "computer-group", false},
		{"os_x_configuration_profile", "limitation", "user-group", false},
	}
	for _, tt := range tests {
		if got := isPolicyLimitUserGroup(tt.singularKey, tt.section, tt.flagName); got != tt.want {
			t.Errorf("isPolicyLimitUserGroup(%q,%q,%q) = %v, want %v",
				tt.singularKey, tt.section, tt.flagName, got, tt.want)
		}
	}
}

// ─── JSS user group target routing (VPP-style scope) ─────────────────────────

func TestAddToScope_UserGroupTarget_NoLongerRoutes(t *testing.T) {
	// --user-group is LDAP-only (limitation/exclusion). For target users must use
	// --jss-user-group explicitly. AddToScope should refuse to add it.
	s := &ScopeXML{}

	if AddToScope(s, "vpp_assignment", "target", "user-group", "VPP Associated Users") {
		t.Fatal("expected false: --user-group is not a valid target flag")
	}
	if len(s.JSSUserGroups.Items) != 0 {
		t.Errorf("jss_user_groups should remain empty, got %d items", len(s.JSSUserGroups.Items))
	}
}

func TestAddToScope_JSSUserGroupTarget(t *testing.T) {
	s := &ScopeXML{}

	if !AddToScope(s, "vpp_assignment", "target", "jss-user-group", "My Group") {
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

	if AddToScope(s, "vpp_assignment", "target", "jss-user-group", "vpp associated users") {
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

	if !RemoveFromScope(s, "vpp_assignment", "target", "jss-user-group", "VPP Associated Users") {
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

	if !RemoveFromScope(s, "vpp_assignment", "exclusion", "jss-user-group", "Excluded Group") {
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

	rows := FlattenScope(s, "vpp_assignment")

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

func TestResolveElemName(t *testing.T) {
	tests := []struct {
		section, flag, want string
	}{
		{"target", "jss-user-group", "user_group"},
		{"target", "computer", "computer"},
		{"target", "computer-group", "computer_group"},
		{"target", "mobile-device", "mobile_device"},
		{"target", "mobile-device-group", "mobile_device_group"},
		{"limitation", "user", "user"},
		{"limitation", "user-group", "user_group"},
		{"exclusion", "computer", "computer"},
		{"exclusion", "mobile-device", "mobile_device"},
		{"exclusion", "user", "user"},
		{"exclusion", "user-group", "user_group"},
		{"exclusion", "jss-user-group", "user_group"},
	}
	for _, tt := range tests {
		if got := resolveElemName(tt.section, tt.flag); got != tt.want {
			t.Errorf("resolveElemName(%q,%q) = %q, want %q", tt.section, tt.flag, got, tt.want)
		}
	}
}

func TestReplaceScopeInXML(t *testing.T) {
	original := []byte(`<?xml version="1.0" encoding="UTF-8"?><vpp_assignment><general><id>11</id></general><scope><all_jss_users>false</all_jss_users><jss_user_groups><user_group><id>1</id><name>Old Group</name></user_group></jss_user_groups></scope></vpp_assignment>`)

	newScope := &ScopeXML{
		JSSUserGroups: ScopeItemSlice{
			Items:    []NamedItem{{ID: "2", Name: "New Group"}},
			ElemName: "user_group",
		},
	}

	updated, err := replaceScopeInXML(original, newScope)
	if err != nil {
		t.Fatalf("replaceScopeInXML: %v", err)
	}

	s := string(updated)
	if strings.Contains(s, "Old Group") {
		t.Error("old scope content should be replaced")
	}
	if !strings.Contains(s, "New Group") {
		t.Error("new scope content should be present")
	}
	if !strings.Contains(s, "<general>") {
		t.Error("non-scope content should be preserved")
	}
}

func TestReplaceScopeInXML_MissingScope(t *testing.T) {
	original := []byte(`<vpp_assignment><general><id>1</id></general></vpp_assignment>`)
	_, err := replaceScopeInXML(original, &ScopeXML{})
	if err == nil {
		t.Error("expected error for missing <scope>")
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
	if !AddToScope(s, "policy", "target", "computer", "ZTNR9F6XJ0") {
		t.Fatal("expected true")
	}
	if len(s.Computers.Items) != 1 || s.Computers.Items[0].Name != "ZTNR9F6XJ0" {
		t.Errorf("computers = %+v", s.Computers.Items)
	}
}

func TestAddToScope_MobileDeviceTarget(t *testing.T) {
	s := &ScopeXML{}
	if !AddToScope(s, "configuration_profile", "target", "mobile-device", "G6TDK43P0D4Y") {
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
	if !AddToScope(s, "policy", "limitation", "user", "alice") {
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
	if !AddToScope(s, "policy", "exclusion", "user", "bob") {
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
	rows := FlattenScope(s, "configuration_profile")

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
