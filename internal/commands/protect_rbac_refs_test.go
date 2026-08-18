// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

func rbacMock() *mockProtectClient {
	return &mockProtectClient{
		roles: []jamfprotect.Role{
			{ID: "1", Name: "Read Only"},
			{ID: "2", Name: "Full Admin"},
		},
		groups:      []jamfprotect.Group{{ID: "10", Name: "Default"}},
		connections: []jamfprotect.Connection{{ID: "conn-1", Name: "jamf-id-db"}},
	}
}

// --- Export shape ---

func TestGroupToExport_UsesNames(t *testing.T) {
	got := groupToExport(&jamfprotect.Group{
		Name:          "Default",
		AccessGroup:   true,
		Connection:    &jamfprotect.GroupConnection{ID: "conn-1", Name: "jamf-id-db"},
		AssignedRoles: []jamfprotect.GroupRole{{ID: "2", Name: "Full Admin"}},
	})

	if got.Connection != "jamf-id-db" {
		t.Errorf("Connection = %q, want the connection name", got.Connection)
	}
	if len(got.Roles) != 1 || got.Roles[0] != "Full Admin" {
		t.Errorf("Roles = %v, want [Full Admin]", got.Roles)
	}
	if !got.AccessGroup {
		t.Error("AccessGroup = false, want true")
	}
}

func TestApiClientToExport_OmitsSecret(t *testing.T) {
	got := apiClientToExport(&jamfprotect.ApiClient{
		Name:          "jamf-cli",
		ClientID:      "abc123",
		Password:      "super-secret",
		AssignedRoles: []jamfprotect.ApiClientRole{{ID: "1", Name: "Read Only"}},
	})

	if len(got.Roles) != 1 || got.Roles[0] != "Read Only" {
		t.Errorf("Roles = %v, want [Read Only]", got.Roles)
	}
	// The struct has no secret field at all; assert via the rendered document
	// so a future field addition trips this test.
	rendered, err := renderExportForTest(got)
	if err != nil {
		t.Fatalf("rendering export: %v", err)
	}
	for _, leak := range []string{"super-secret", "abc123"} {
		if strings.Contains(rendered, leak) {
			t.Errorf("export leaked %q:\n%s", leak, rendered)
		}
	}
}

func TestUserToExport_UsesNames(t *testing.T) {
	got := userToExport(&jamfprotect.User{
		Email:                 "someone@example.com",
		Connection:            &jamfprotect.UserConnection{ID: "conn-1", Name: "jamf-id-db"},
		AssignedRoles:         []jamfprotect.UserRole{{ID: "2", Name: "Full Admin"}},
		AssignedGroups:        []jamfprotect.UserGroup{{ID: "10", Name: "Default"}},
		ReceiveEmailAlert:     true,
		EmailAlertMinSeverity: "Low",
	})

	if got.Connection != "jamf-id-db" {
		t.Errorf("Connection = %q, want jamf-id-db", got.Connection)
	}
	if len(got.Roles) != 1 || got.Roles[0] != "Full Admin" {
		t.Errorf("Roles = %v, want [Full Admin]", got.Roles)
	}
	if len(got.Groups) != 1 || got.Groups[0] != "Default" {
		t.Errorf("Groups = %v, want [Default]", got.Groups)
	}
}

// --- Name resolution on apply ---

func TestGroupInputFromDocument_ResolvesNames(t *testing.T) {
	doc := []byte("name: Default\nconnection: jamf-id-db\naccessGroup: true\nroles:\n  - Full Admin\n  - Read Only\n")

	got, err := groupInputFromDocument(context.Background(), doc, protect.NewResolver(rbacMock()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ConnectionID == nil || *got.ConnectionID != "conn-1" {
		t.Errorf("ConnectionID = %v, want conn-1", got.ConnectionID)
	}
	want := []string{"2", "1"}
	if len(got.RoleIDs) != len(want) {
		t.Fatalf("RoleIDs = %v, want %v", got.RoleIDs, want)
	}
	for i, w := range want {
		if got.RoleIDs[i] != w {
			t.Errorf("RoleIDs[%d] = %q, want %q (order must follow the document)", i, got.RoleIDs[i], w)
		}
	}
}

func TestUserInputFromDocument_ResolvesNames(t *testing.T) {
	doc := []byte("email: someone@example.com\nconnection: jamf-id-db\nroles:\n  - Read Only\ngroups:\n  - Default\n")

	got, err := userInputFromDocument(context.Background(), doc, protect.NewResolver(rbacMock()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.RoleIDs) != 1 || got.RoleIDs[0] != "1" {
		t.Errorf("RoleIDs = %v, want [1]", got.RoleIDs)
	}
	if len(got.GroupIDs) != 1 || got.GroupIDs[0] != "10" {
		t.Errorf("GroupIDs = %v, want [10]", got.GroupIDs)
	}
}

func TestApiClientInputFromDocument_ResolvesNames(t *testing.T) {
	doc := []byte(`{"name":"jamf-cli","roles":["Full Admin"]}`)

	got, err := apiClientInputFromDocument(context.Background(), doc, protect.NewResolver(rbacMock()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.RoleIDs) != 1 || got.RoleIDs[0] != "2" {
		t.Errorf("RoleIDs = %v, want [2]", got.RoleIDs)
	}
}

// The whole point of the change: a role name absent from the target tenant must
// fail loudly. The old ID-based export bound to whatever role held that integer.
func TestGroupInputFromDocument_UnresolvableRoleFails(t *testing.T) {
	doc := []byte("name: Default\nroles:\n  - No Such Role\n")

	_, err := groupInputFromDocument(context.Background(), doc, protect.NewResolver(rbacMock()))
	if err == nil {
		t.Fatal("expected an error for an unresolvable role name")
	}
	if !strings.Contains(err.Error(), "No Such Role") {
		t.Errorf("error = %q, want it to name the unresolvable role", err.Error())
	}
}

// --- Back-compat with ID-shaped documents ---

func TestRbacDocumentUsesIDs(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want bool
	}{
		{"legacy group json with capitalised go field names", `{"Name":"Default","RoleIDs":["2"]}`, true},
		{"legacy yaml with lowercased field names", "name: Default\nroleids:\n  - \"2\"\n", true},
		{"legacy user with GroupIDs", `{"Email":"a@b.c","GroupIDs":["10"]}`, true},
		{"legacy connection id", `{"Name":"Default","ConnectionID":"conn-1"}`, true},
		{"name-based group", "name: Default\nroles:\n  - Full Admin\n", false},
		{"name-based user", `{"email":"a@b.c","groups":["Default"]}`, false},
		{"bare name only", `{"name":"Default"}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := rbacDocumentUsesIDs([]byte(tc.doc)); got != tc.want {
				t.Errorf("rbacDocumentUsesIDs() = %v, want %v", got, tc.want)
			}
		})
	}
}

// A document written before the name-based change must still apply, and must
// pass its IDs through untouched rather than being read as role *names*.
func TestGroupInputFromDocument_LegacyIDsPassThrough(t *testing.T) {
	doc := []byte(`{"Name":"Default","AccessGroup":true,"RoleIDs":["2"]}`)

	got, err := groupInputFromDocument(context.Background(), doc, protect.NewResolver(rbacMock()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.RoleIDs) != 1 || got.RoleIDs[0] != "2" {
		t.Errorf("RoleIDs = %v, want the literal [2]", got.RoleIDs)
	}
	if !got.AccessGroup {
		t.Error("AccessGroup = false, want true")
	}
}

// renderExportForTest marshals a value the way printExport would, so leak
// assertions cover the rendered document rather than the struct.
func renderExportForTest(v any) (string, error) {
	data, err := yaml.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
