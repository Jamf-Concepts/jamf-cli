// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

// Groups, users and API clients reference roles, groups and identity provider
// connections. Those references used to export as raw server IDs — role IDs in
// particular are small sequential integers ("1", "2"), so a document exported
// from one tenant and applied to another resolved to *a* role rather than
// failing: the wrong grant, silently. Every reference therefore exports by name,
// the way analytic sets, ULF sets and plans already do, and apply resolves names
// back to IDs against the target tenant.
//
// The ID-shaped documents are still accepted on input so files written before
// this change keep working; see rbacDocumentUsesIDs.

// groupExport is the portable representation of a group.
type groupExport struct {
	Name        string   `json:"name" yaml:"name"`
	Connection  string   `json:"connection,omitempty" yaml:"connection,omitempty"`
	AccessGroup bool     `json:"accessGroup" yaml:"accessGroup"`
	Roles       []string `json:"roles,omitempty" yaml:"roles,omitempty"`
}

// userExport is the portable representation of a user.
type userExport struct {
	Email                 string   `json:"email" yaml:"email"`
	Connection            string   `json:"connection,omitempty" yaml:"connection,omitempty"`
	Roles                 []string `json:"roles,omitempty" yaml:"roles,omitempty"`
	Groups                []string `json:"groups,omitempty" yaml:"groups,omitempty"`
	ReceiveEmailAlert     bool     `json:"receiveEmailAlert" yaml:"receiveEmailAlert"`
	EmailAlertMinSeverity string   `json:"emailAlertMinSeverity,omitempty" yaml:"emailAlertMinSeverity,omitempty"`
}

// apiClientExport is the portable representation of an API client. The client
// secret is deliberately absent: the server returns it only once at creation
// and regenerates it on any recreate, so it cannot be carried in an export.
type apiClientExport struct {
	Name  string   `json:"name" yaml:"name"`
	Roles []string `json:"roles,omitempty" yaml:"roles,omitempty"`
}

// rbacDocumentUsesIDs reports whether an input document is in the older
// ID-shaped form (RoleIDs/GroupIDs/ConnectionID) rather than the name-based one.
//
// Keys are compared lowercased because the SDK input structs carry no
// json/yaml tags: encoded as JSON their keys are Go field names ("RoleIDs"),
// while yaml.v3 lowercases them ("roleids").
func rbacDocumentUsesIDs(data []byte) bool {
	var probe map[string]any
	if err := unmarshalInput(data, &probe); err != nil {
		return false
	}
	for k := range probe {
		switch strings.ToLower(k) {
		case "roleids", "groupids", "connectionid":
			return true
		}
	}
	return false
}

// --- Groups ---

func groupToExport(g *jamfprotect.Group) groupExport {
	e := groupExport{Name: g.Name, AccessGroup: g.AccessGroup}
	if g.Connection != nil {
		e.Connection = g.Connection.Name
	}
	for _, r := range g.AssignedRoles {
		e.Roles = append(e.Roles, r.Name)
	}
	return e
}

func groupExportToInput(ctx context.Context, e groupExport, r *protect.Resolver) (jamfprotect.GroupInput, error) {
	// The reference lists must marshal as arrays even when empty: the API rejects
	// a null with "input → roleIds: None is not of type 'array'", which a group
	// or user carrying no direct roles would otherwise hit.
	input := jamfprotect.GroupInput{Name: e.Name, AccessGroup: e.AccessGroup, RoleIDs: []string{}}

	if e.Connection != "" {
		id, err := r.ResolveConnectionID(ctx, e.Connection)
		if err != nil {
			return jamfprotect.GroupInput{}, fmt.Errorf("resolving connection %q: %w", e.Connection, err)
		}
		input.ConnectionID = &id
	}
	for _, name := range e.Roles {
		id, err := r.ResolveRoleID(ctx, name)
		if err != nil {
			return jamfprotect.GroupInput{}, fmt.Errorf("resolving role %q: %w", name, err)
		}
		input.RoleIDs = append(input.RoleIDs, id)
	}
	return input, nil
}

// groupInputFromDocument decodes either document shape into a GroupInput.
func groupInputFromDocument(ctx context.Context, data []byte, r *protect.Resolver) (jamfprotect.GroupInput, error) {
	if rbacDocumentUsesIDs(data) {
		var input jamfprotect.GroupInput
		if err := unmarshalInput(data, &input); err != nil {
			return jamfprotect.GroupInput{}, err
		}
		return input, nil
	}
	var e groupExport
	if err := unmarshalInput(data, &e); err != nil {
		return jamfprotect.GroupInput{}, err
	}
	return groupExportToInput(ctx, e, r)
}

// --- Users ---

func userToExport(u *jamfprotect.User) userExport {
	e := userExport{
		Email:                 u.Email,
		ReceiveEmailAlert:     u.ReceiveEmailAlert,
		EmailAlertMinSeverity: u.EmailAlertMinSeverity,
	}
	if u.Connection != nil {
		e.Connection = u.Connection.Name
	}
	for _, r := range u.AssignedRoles {
		e.Roles = append(e.Roles, r.Name)
	}
	for _, g := range u.AssignedGroups {
		e.Groups = append(e.Groups, g.Name)
	}
	return e
}

func userExportToInput(ctx context.Context, e userExport, r *protect.Resolver) (jamfprotect.UserInput, error) {
	input := jamfprotect.UserInput{
		Email:                 e.Email,
		ReceiveEmailAlert:     e.ReceiveEmailAlert,
		EmailAlertMinSeverity: e.EmailAlertMinSeverity,
		RoleIDs:               []string{},
		GroupIDs:              []string{},
	}

	if e.Connection != "" {
		id, err := r.ResolveConnectionID(ctx, e.Connection)
		if err != nil {
			return jamfprotect.UserInput{}, fmt.Errorf("resolving connection %q: %w", e.Connection, err)
		}
		input.ConnectionID = &id
	}
	for _, name := range e.Roles {
		id, err := r.ResolveRoleID(ctx, name)
		if err != nil {
			return jamfprotect.UserInput{}, fmt.Errorf("resolving role %q: %w", name, err)
		}
		input.RoleIDs = append(input.RoleIDs, id)
	}
	for _, name := range e.Groups {
		id, err := r.ResolveGroupID(ctx, name)
		if err != nil {
			return jamfprotect.UserInput{}, fmt.Errorf("resolving group %q: %w", name, err)
		}
		input.GroupIDs = append(input.GroupIDs, id)
	}
	return input, nil
}

// userInputFromDocument decodes either document shape into a UserInput.
func userInputFromDocument(ctx context.Context, data []byte, r *protect.Resolver) (jamfprotect.UserInput, error) {
	if rbacDocumentUsesIDs(data) {
		var input jamfprotect.UserInput
		if err := unmarshalInput(data, &input); err != nil {
			return jamfprotect.UserInput{}, err
		}
		return input, nil
	}
	var e userExport
	if err := unmarshalInput(data, &e); err != nil {
		return jamfprotect.UserInput{}, err
	}
	return userExportToInput(ctx, e, r)
}

// --- API clients ---

func apiClientToExport(a *jamfprotect.ApiClient) apiClientExport {
	e := apiClientExport{Name: a.Name}
	for _, r := range a.AssignedRoles {
		e.Roles = append(e.Roles, r.Name)
	}
	return e
}

func apiClientExportToInput(ctx context.Context, e apiClientExport, r *protect.Resolver) (jamfprotect.ApiClientInput, error) {
	input := jamfprotect.ApiClientInput{Name: e.Name, RoleIDs: []string{}}
	for _, name := range e.Roles {
		id, err := r.ResolveRoleID(ctx, name)
		if err != nil {
			return jamfprotect.ApiClientInput{}, fmt.Errorf("resolving role %q: %w", name, err)
		}
		input.RoleIDs = append(input.RoleIDs, id)
	}
	return input, nil
}

// apiClientInputFromDocument decodes either document shape into an ApiClientInput.
func apiClientInputFromDocument(ctx context.Context, data []byte, r *protect.Resolver) (jamfprotect.ApiClientInput, error) {
	if rbacDocumentUsesIDs(data) {
		var input jamfprotect.ApiClientInput
		if err := unmarshalInput(data, &input); err != nil {
			return jamfprotect.ApiClientInput{}, err
		}
		return input, nil
	}
	var e apiClientExport
	if err := unmarshalInput(data, &e); err != nil {
		return jamfprotect.ApiClientInput{}, err
	}
	return apiClientExportToInput(ctx, e, r)
}
