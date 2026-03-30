package commands

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

	if env.General.ID != 42 {
		t.Errorf("general.id = %d, want 42", env.General.ID)
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
	scope := scopeXML{
		AllComputers: true,
		ComputerGroups: scopeItemSlice{
			Items:    []namedItem{{ID: 1, Name: "Group A"}, {Name: "Group B"}},
			elemName: "computer_group",
		},
		Buildings: scopeItemSlice{
			Items:    []namedItem{{Name: "HQ"}},
			elemName: "building",
		},
	}

	data, err := xml.MarshalIndent(scope, "", "  ")
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
	var parsed scopeXML
	if err := xml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal round-trip: %v", err)
	}
	if len(parsed.ComputerGroups.Items) != 2 {
		t.Errorf("round-trip computer_groups: got %d, want 2", len(parsed.ComputerGroups.Items))
	}
}

func TestScopeUpdateXML_Marshal(t *testing.T) {
	scope := scopeXML{
		ComputerGroups: scopeItemSlice{
			Items:    []namedItem{{Name: "Test"}},
			elemName: "computer_group",
		},
	}
	envelope := scopeUpdateXML{
		XMLName: xml.Name{Local: "policy"},
		Scope:   scope,
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

// ─── addToScope ────────────────────────────────────────────────────────────────

func TestAddToScope_TargetComputerGroup(t *testing.T) {
	scope := &scopeXML{
		ComputerGroups: scopeItemSlice{
			Items:    []namedItem{{ID: 1, Name: "Existing"}},
			elemName: "computer_group",
		},
	}

	if !addToScope(scope, "policy", "target", "computer-group", "New Group") {
		t.Fatal("expected true")
	}
	if len(scope.ComputerGroups.Items) != 2 {
		t.Fatalf("got %d, want 2", len(scope.ComputerGroups.Items))
	}
	if scope.ComputerGroups.Items[1].Name != "New Group" {
		t.Errorf("name = %q", scope.ComputerGroups.Items[1].Name)
	}
}

func TestAddToScope_Idempotent(t *testing.T) {
	scope := &scopeXML{
		ComputerGroups: scopeItemSlice{
			Items:    []namedItem{{Name: "Existing"}},
			elemName: "computer_group",
		},
	}

	if addToScope(scope, "policy", "target", "computer-group", "existing") {
		t.Fatal("expected false for case-insensitive duplicate")
	}
	if len(scope.ComputerGroups.Items) != 1 {
		t.Fatal("scope should be unchanged")
	}
}

func TestAddToScope_CreatesSection(t *testing.T) {
	scope := &scopeXML{}

	if !addToScope(scope, "policy", "exclusion", "computer-group", "Test") {
		t.Fatal("expected true")
	}
	if scope.Exclusions == nil {
		t.Fatal("exclusions should be created")
	}
	if len(scope.Exclusions.ComputerGroups.Items) != 1 {
		t.Fatal("should have 1 item")
	}
	if scope.Exclusions.ComputerGroups.elemName != "computer_group" {
		t.Errorf("elemName = %q, want %q", scope.Exclusions.ComputerGroups.elemName, "computer_group")
	}
}

func TestAddToScope_Limitation(t *testing.T) {
	scope := &scopeXML{
		Limitations: &limitationsXML{},
	}

	if !addToScope(scope, "policy", "limitation", "network-segment", "Guest") {
		t.Fatal("expected true")
	}
	if len(scope.Limitations.NetworkSegments.Items) != 1 {
		t.Fatal("should have 1 item")
	}
}

// ─── addToScope: policy limitation user_group special case ─────────────────────

func TestAddToScope_PolicyLimitUserGroup(t *testing.T) {
	scope := &scopeXML{}

	if !addToScope(scope, "policy", "limitation", "user-group", "Staff") {
		t.Fatal("expected true")
	}

	if scope.LimitToUsers == nil {
		t.Fatal("limit_to_users should be created")
	}
	groups := scope.LimitToUsers.UserGroups.Items
	if len(groups) != 1 || groups[0] != "Staff" {
		t.Errorf("got %v, want [Staff]", groups)
	}
}

func TestAddToScope_PolicyLimitUserGroup_Idempotent(t *testing.T) {
	scope := &scopeXML{
		LimitToUsers: &limitToUsersXML{
			UserGroups: scopeStringSlice{Items: []string{"Staff"}, elemName: "user_group"},
		},
	}

	if addToScope(scope, "policy", "limitation", "user-group", "staff") {
		t.Fatal("expected false for case-insensitive duplicate")
	}
}

func TestAddToScope_NonPolicyLimitUserGroup(t *testing.T) {
	scope := &scopeXML{}

	if !addToScope(scope, "os_x_configuration_profile", "limitation", "user-group", "Staff") {
		t.Fatal("expected true")
	}

	// Should go to limitations.user_groups, NOT limit_to_users
	if scope.LimitToUsers != nil {
		t.Error("non-policy should not use limit_to_users")
	}
	if scope.Limitations == nil || len(scope.Limitations.UserGroups.Items) != 1 {
		t.Error("should be in limitations.user_groups")
	}
}

// ─── removeFromScope ───────────────────────────────────────────────────────────

func TestRemoveFromScope_TargetComputerGroup(t *testing.T) {
	scope := &scopeXML{
		ComputerGroups: scopeItemSlice{
			Items:    []namedItem{{ID: 1, Name: "Keep"}, {ID: 2, Name: "Remove"}},
			elemName: "computer_group",
		},
	}

	if !removeFromScope(scope, "policy", "target", "computer-group", "Remove") {
		t.Fatal("expected true")
	}
	if len(scope.ComputerGroups.Items) != 1 {
		t.Fatalf("got %d, want 1", len(scope.ComputerGroups.Items))
	}
	if scope.ComputerGroups.Items[0].Name != "Keep" {
		t.Errorf("remaining = %q", scope.ComputerGroups.Items[0].Name)
	}
}

func TestRemoveFromScope_NotFound(t *testing.T) {
	scope := &scopeXML{
		ComputerGroups: scopeItemSlice{Items: []namedItem{{Name: "Keep"}}},
	}

	if removeFromScope(scope, "policy", "target", "computer-group", "Nonexistent") {
		t.Fatal("expected false")
	}
}

func TestRemoveFromScope_CaseInsensitive(t *testing.T) {
	scope := &scopeXML{
		ComputerGroups: scopeItemSlice{Items: []namedItem{{Name: "Test Group"}}},
	}

	if !removeFromScope(scope, "policy", "target", "computer-group", "test group") {
		t.Fatal("expected case-insensitive match")
	}
}

func TestRemoveFromScope_MissingSection(t *testing.T) {
	scope := &scopeXML{} // no exclusions

	if removeFromScope(scope, "policy", "exclusion", "computer-group", "Test") {
		t.Fatal("expected false when section missing")
	}
}

func TestRemoveFromScope_PolicyLimitUserGroup(t *testing.T) {
	scope := &scopeXML{
		LimitToUsers: &limitToUsersXML{
			UserGroups: scopeStringSlice{Items: []string{"Staff", "Admins"}, elemName: "user_group"},
		},
	}

	if !removeFromScope(scope, "policy", "limitation", "user-group", "staff") {
		t.Fatal("expected true (case-insensitive)")
	}
	if len(scope.LimitToUsers.UserGroups.Items) != 1 {
		t.Fatalf("got %d, want 1", len(scope.LimitToUsers.UserGroups.Items))
	}
	if scope.LimitToUsers.UserGroups.Items[0] != "Admins" {
		t.Errorf("remaining = %q", scope.LimitToUsers.UserGroups.Items[0])
	}
}

func TestRemoveFromScope_PolicyLimitUserGroup_NotFound(t *testing.T) {
	scope := &scopeXML{
		LimitToUsers: &limitToUsersXML{
			UserGroups: scopeStringSlice{Items: []string{"Staff"}},
		},
	}

	if removeFromScope(scope, "policy", "limitation", "user-group", "Nonexistent") {
		t.Fatal("expected false")
	}
}

func TestRemoveFromScope_PolicyLimitUserGroup_InLimitations(t *testing.T) {
	// User group in limitations/user_groups (named items) but NOT in limit_to_users.
	// The remove must still find and remove it — matching the Python script's
	// fallthrough that checks both locations.
	scope := &scopeXML{
		LimitToUsers: &limitToUsersXML{
			UserGroups: scopeStringSlice{Items: []string{}, elemName: "user_group"},
		},
		Limitations: &limitationsXML{
			UserGroups: scopeItemSlice{
				Items:    []namedItem{{ID: 5, Name: "Staff"}},
				elemName: "user_group",
			},
		},
	}

	if !removeFromScope(scope, "policy", "limitation", "user-group", "Staff") {
		t.Fatal("expected true — should find in limitations/user_groups")
	}
	if len(scope.Limitations.UserGroups.Items) != 0 {
		t.Error("should have removed from limitations/user_groups")
	}
}

func TestRemoveFromScope_PolicyLimitUserGroup_InBothLocations(t *testing.T) {
	// User group appears in both limit_to_users AND limitations/user_groups.
	// Both should be removed.
	scope := &scopeXML{
		LimitToUsers: &limitToUsersXML{
			UserGroups: scopeStringSlice{Items: []string{"Staff"}, elemName: "user_group"},
		},
		Limitations: &limitationsXML{
			UserGroups: scopeItemSlice{
				Items:    []namedItem{{Name: "Staff"}},
				elemName: "user_group",
			},
		},
	}

	if !removeFromScope(scope, "policy", "limitation", "user-group", "Staff") {
		t.Fatal("expected true")
	}
	if len(scope.LimitToUsers.UserGroups.Items) != 0 {
		t.Error("should have removed from limit_to_users")
	}
	if len(scope.Limitations.UserGroups.Items) != 0 {
		t.Error("should have removed from limitations/user_groups")
	}
}

// ─── validateScopeCombination ──────────────────────────────────────────────────

func TestValidateScopeCombination_ValidTargets(t *testing.T) {
	for _, flag := range []string{"computer-group", "mobile-device-group", "building", "department"} {
		for _, sk := range []string{"policy", "restricted_software", "os_x_configuration_profile"} {
			if err := validateScopeCombination(sk, "target", flag); err != nil {
				t.Errorf("target/%s/%s: %v", sk, flag, err)
			}
		}
	}
}

func TestValidateScopeCombination_InvalidTarget(t *testing.T) {
	if err := validateScopeCombination("policy", "target", "network-segment"); err == nil {
		t.Error("expected error for network-segment as target")
	}
}

func TestValidateScopeCombination_ValidLimitations(t *testing.T) {
	for _, flag := range []string{"network-segment", "user-group", "computer-group"} {
		if err := validateScopeCombination("policy", "limitation", flag); err != nil {
			t.Errorf("limitation/%s: %v", flag, err)
		}
	}
}

func TestValidateScopeCombination_RestrictedSoftwareNoLimitations(t *testing.T) {
	for _, flag := range []string{"network-segment", "user-group", "computer-group"} {
		err := validateScopeCombination("restricted_software", "limitation", flag)
		if err == nil {
			t.Errorf("expected error: restricted software + limitation + %s", flag)
		}
		if !strings.Contains(err.Error(), "does not support limitations") {
			t.Errorf("unexpected error: %v", err)
		}
	}
}

func TestValidateScopeCombination_ValidExclusions(t *testing.T) {
	for _, flag := range []string{"computer-group", "mobile-device-group", "user-group", "network-segment", "building", "department"} {
		if err := validateScopeCombination("policy", "exclusion", flag); err != nil {
			t.Errorf("exclusion/%s: %v", flag, err)
		}
	}
}

func TestValidateScopeCombination_RestrictedSoftwareExclusions(t *testing.T) {
	for _, flag := range []string{"computer-group", "building", "department"} {
		if err := validateScopeCombination("restricted_software", "exclusion", flag); err != nil {
			t.Errorf("restricted exclusion/%s: %v", flag, err)
		}
	}
	for _, flag := range []string{"mobile-device-group", "user-group", "network-segment"} {
		if err := validateScopeCombination("restricted_software", "exclusion", flag); err == nil {
			t.Errorf("expected error: restricted software exclusion + %s", flag)
		}
	}
}

func TestValidateScopeCombination_InvalidSection(t *testing.T) {
	if err := validateScopeCombination("policy", "bogus", "computer-group"); err == nil {
		t.Error("expected error for invalid section")
	}
}

func TestValidateScopeCombination_InvalidLimitationFlag(t *testing.T) {
	if err := validateScopeCombination("policy", "limitation", "building"); err == nil {
		t.Error("expected error: building is not a valid limitation")
	}
}

// ─── flattenScope ──────────────────────────────────────────────────────────────

func TestFlattenScope_BasicPolicy(t *testing.T) {
	scope := &scopeXML{
		AllComputers: true,
		ComputerGroups: scopeItemSlice{
			Items: []namedItem{{ID: 1, Name: "Group A"}},
		},
		Buildings: scopeItemSlice{
			Items: []namedItem{{Name: "HQ"}},
		},
		LimitToUsers: &limitToUsersXML{
			UserGroups: scopeStringSlice{Items: []string{"Staff"}},
		},
		Limitations: &limitationsXML{
			NetworkSegments: scopeItemSlice{Items: []namedItem{{Name: "Corporate"}}},
		},
		Exclusions: &exclusionsXML{
			ComputerGroups: scopeItemSlice{Items: []namedItem{{Name: "Test Machines"}}},
		},
	}

	rows := flattenScope(scope, "policy")

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
	// The Classic API mirrors policy user groups in both limit_to_users/user_groups
	// and limitations/user_groups. flattenScope should emit only the limit_to_users
	// copy to avoid duplicate rows.
	scope := &scopeXML{
		LimitToUsers: &limitToUsersXML{
			UserGroups: scopeStringSlice{Items: []string{"Staff", "Faculty"}},
		},
		Limitations: &limitationsXML{
			UserGroups: scopeItemSlice{Items: []namedItem{
				{ID: 1, Name: "Staff"},
				{ID: 2, Name: "Faculty"},
			}},
			NetworkSegments: scopeItemSlice{Items: []namedItem{{Name: "Corporate"}}},
		},
	}

	rows := flattenScope(scope, "policy")

	// Count user_group rows — should be exactly 2, not 4.
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
	scope := &scopeXML{}
	if rows := flattenScope(scope, "policy"); len(rows) != 0 {
		t.Errorf("got %d rows, want 0", len(rows))
	}
}

// ─── isPolicyLimitUserGroup ────────────────────────────────────────────────────

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

// ─── Resource lookup ───────────────────────────────────────────────────────────

func TestScopeResourceNames_Sorted(t *testing.T) {
	names := scopeResourceNames()
	if len(names) != len(scopeResources) {
		t.Errorf("got %d names, want %d", len(names), len(scopeResources))
	}
	for i := 1; i < len(names); i++ {
		if names[i] < names[i-1] {
			t.Errorf("not sorted: %q before %q", names[i-1], names[i])
		}
	}
}

func TestLookupScopeResource_Valid(t *testing.T) {
	res, err := lookupScopeResource("policy")
	if err != nil {
		t.Fatal(err)
	}
	if res.apiPath != "policies" {
		t.Errorf("apiPath = %q", res.apiPath)
	}
}

func TestLookupScopeResource_Invalid(t *testing.T) {
	_, err := lookupScopeResource("bogus")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unsupported resource type") {
		t.Errorf("unexpected error: %v", err)
	}
}
