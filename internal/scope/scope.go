// Copyright 2026, Jamf Software LLC

package scope

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// FetchScope performs a GET on a Classic API resource by name and returns
// the resource's ID and parsed scope.
func FetchScope(ctx context.Context, client registry.HTTPClient, res Resource, name string) (string, *ScopeXML, error) {
	path := fmt.Sprintf("/JSSResource/%s/name/%s", res.APIPath, url.PathEscape(name))
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return "", nil, fmt.Errorf("fetching %s %q: %w", res.SingularKey, name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return "", nil, fmt.Errorf("reading response: %w", err)
	}

	var envelope classicResourceXML
	if err := xml.Unmarshal(body, &envelope); err != nil {
		return "", nil, fmt.Errorf("parsing %s XML: %w", res.SingularKey, err)
	}

	if envelope.General.ID == 0 {
		return "", nil, fmt.Errorf("no ID in %s %q", res.SingularKey, name)
	}

	return fmt.Sprintf("%d", envelope.General.ID), &envelope.Scope, nil
}

// PutScope writes an updated scope back to the Classic API via subset PUT.
func PutScope(ctx context.Context, client registry.HTTPClient, res Resource, id string, s *ScopeXML) error {
	envelope := scopeUpdateXML{
		XMLName: xml.Name{Local: res.SingularKey},
		Scope:   *s,
	}

	data, err := xml.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling scope XML: %w", err)
	}
	xmlBody := append([]byte(xml.Header), data...)

	path := fmt.Sprintf("/JSSResource/%s/id/%s/subset/Scope", res.APIPath, url.PathEscape(id))
	resp, err := client.Do(ctx, "PUT", path, bytes.NewReader(xmlBody))
	if err != nil {
		return fmt.Errorf("updating scope: %w", err)
	}
	_ = resp.Body.Close()
	return nil
}

// AddToScope adds a named item to the given scope section. Returns true if the
// item was added, false if already present (idempotent no-op).
func AddToScope(s *ScopeXML, singularKey, section, flagName, name string) bool {
	if isPolicyLimitUserGroup(singularKey, section, flagName) {
		return addPolicyLimitUserGroup(s, name)
	}

	items := getOrCreateScopeItems(s, section, flagName)
	if items == nil {
		return false
	}

	for _, item := range items.Items {
		if strings.EqualFold(item.Name, name) {
			return false
		}
	}

	if items.ElemName == "" {
		items.ElemName = flagToElemName[flagName]
	}
	items.Items = append(items.Items, NamedItem{Name: name})
	return true
}

// RemoveFromScope removes a named item from the given scope section. Returns
// true if removed, false if not found (idempotent no-op).
//
// For the policy limitation user_group case, user groups can live in BOTH
// limit_to_users/user_groups (plain strings) AND limitations/user_groups
// (named items). Both locations are checked.
func RemoveFromScope(s *ScopeXML, singularKey, section, flagName, name string) bool {
	if isPolicyLimitUserGroup(singularKey, section, flagName) {
		a := removePolicyLimitUserGroup(s, name)
		b := removeNamedItem(readScopeItems(s, section, flagName), name)
		return a || b
	}

	return removeNamedItem(readScopeItems(s, section, flagName), name)
}

// OutputScope writes the scope to the output formatter. For table/csv/plain formats
// it flattens the scope into rows; for json/yaml it outputs the full structure.
func OutputScope(out registry.OutputFormatter, s *ScopeXML, singularKey, format string) error {
	switch format {
	case "table", "csv", "plain":
		rows := FlattenScope(s, singularKey)
		if len(rows) == 0 {
			fmt.Fprintln(os.Stderr, "Scope is empty")
			return nil
		}
		data, err := json.Marshal(rows)
		if err != nil {
			return err
		}
		return out.PrintRaw(data)
	default:
		data, err := json.Marshal(s)
		if err != nil {
			return err
		}
		return out.PrintRaw(data)
	}
}

// FlattenScope converts a ScopeXML into a flat list of rows for table output.
func FlattenScope(s *ScopeXML, singularKey string) []map[string]any {
	var rows []map[string]any

	if s.AllComputers {
		rows = append(rows, map[string]any{"section": "target", "type": "all_computers", "name": "true"})
	}
	if s.AllJSSUsers {
		rows = append(rows, map[string]any{"section": "target", "type": "all_jss_users", "name": "true"})
	}

	appendNamedRows(&rows, "target", "computer", s.Computers.Items)
	appendNamedRows(&rows, "target", "computer_group", s.ComputerGroups.Items)
	appendNamedRows(&rows, "target", "mobile_device_group", s.MobileDeviceGroups.Items)
	appendNamedRows(&rows, "target", "building", s.Buildings.Items)
	appendNamedRows(&rows, "target", "department", s.Departments.Items)

	// Policy special case: limit_to_users holds plain strings
	if singularKey == "policy" && s.LimitToUsers != nil {
		for _, g := range s.LimitToUsers.UserGroups.Items {
			rows = append(rows, map[string]any{"section": "limitation", "type": "user_group", "name": g})
		}
	}

	if s.Limitations != nil {
		appendNamedRows(&rows, "limitation", "network_segment", s.Limitations.NetworkSegments.Items)
		// For policies, user groups are already emitted from limit_to_users above;
		// limitations/user_groups is a server-side mirror, so skip it to avoid duplicates.
		if singularKey != "policy" {
			appendNamedRows(&rows, "limitation", "user_group", s.Limitations.UserGroups.Items)
		}
		appendNamedRows(&rows, "limitation", "computer_group", s.Limitations.ComputerGroups.Items)
	}

	if s.Exclusions != nil {
		appendNamedRows(&rows, "exclusion", "computer", s.Exclusions.Computers.Items)
		appendNamedRows(&rows, "exclusion", "computer_group", s.Exclusions.ComputerGroups.Items)
		appendNamedRows(&rows, "exclusion", "mobile_device_group", s.Exclusions.MobileDeviceGroups.Items)
		appendNamedRows(&rows, "exclusion", "user_group", s.Exclusions.UserGroups.Items)
		appendNamedRows(&rows, "exclusion", "network_segment", s.Exclusions.NetworkSegments.Items)
		appendNamedRows(&rows, "exclusion", "building", s.Exclusions.Buildings.Items)
		appendNamedRows(&rows, "exclusion", "department", s.Exclusions.Departments.Items)
	}

	return rows
}

// ValidateScopeCombination checks that the given section/flag combination is valid
// for the resource type.
func ValidateScopeCombination(singularKey, section, flagName string) error {
	isRestricted := singularKey == "restricted_software"

	switch section {
	case "target":
		switch flagName {
		case "computer-group", "mobile-device-group", "building", "department":
			return nil
		}
		return fmt.Errorf("--%s is not valid as a target; use --computer-group, --mobile-device-group, --building, or --department", flagName)

	case "limitation":
		if isRestricted {
			return fmt.Errorf("restricted software does not support limitations")
		}
		switch flagName {
		case "network-segment", "user-group", "computer-group":
			return nil
		}
		return fmt.Errorf("--%s is not valid as a limitation; use --network-segment, --user-group, or --computer-group", flagName)

	case "exclusion":
		if isRestricted {
			switch flagName {
			case "computer-group", "building", "department":
				return nil
			}
			return fmt.Errorf("--%s is not valid as an exclusion for restricted software; use --computer-group, --building, or --department", flagName)
		}
		switch flagName {
		case "computer-group", "mobile-device-group", "user-group", "network-segment", "building", "department":
			return nil
		}
		return fmt.Errorf("--%s is not valid as an exclusion", flagName)
	}

	return fmt.Errorf("invalid section %q; use target, limitation, or exclusion", section)
}

// DetermineScopeTarget inspects the command's flags to find exactly one scope
// target flag that was set. Returns an error if zero or multiple flags are set.
func DetermineScopeTarget(cmd *cobra.Command) (ScopeTarget, error) {
	var found ScopeTarget
	count := 0
	for _, flag := range scopeFlagNames {
		if v, _ := cmd.Flags().GetString(flag); v != "" {
			found = ScopeTarget{FlagName: flag, Name: v}
			count++
		}
	}
	if count == 0 {
		return ScopeTarget{}, fmt.Errorf("specify one of: --computer-group, --mobile-device-group, --building, --department, --network-segment, --user-group")
	}
	if count > 1 {
		return ScopeTarget{}, fmt.Errorf("specify only one scopeable type per invocation")
	}
	return found, nil
}

// AddScopeFlags registers the --section and item flags on a scope add/remove command.
func AddScopeFlags(cmd *cobra.Command, section *string) {
	cmd.Flags().StringVar(section, "section", "target", "scope section: target, limitation, exclusion")
	_ = cmd.RegisterFlagCompletionFunc("section", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"target", "limitation", "exclusion"}, cobra.ShellCompDirectiveNoFileComp
	})
	cmd.Flags().String("computer-group", "", "computer group name")
	cmd.Flags().String("mobile-device-group", "", "mobile device group name")
	cmd.Flags().String("building", "", "building name")
	cmd.Flags().String("department", "", "department name")
	cmd.Flags().String("network-segment", "", "network segment name")
	cmd.Flags().String("user-group", "", "user group name")
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

func removeNamedItem(items *ScopeItemSlice, name string) bool {
	if items == nil {
		return false
	}
	var keep []NamedItem
	found := false
	for _, item := range items.Items {
		if strings.EqualFold(item.Name, name) {
			found = true
			continue
		}
		keep = append(keep, item)
	}
	if !found {
		return false
	}
	items.Items = keep
	return true
}

func addPolicyLimitUserGroup(s *ScopeXML, name string) bool {
	if s.LimitToUsers == nil {
		s.LimitToUsers = &LimitToUsersXML{}
	}
	ltu := s.LimitToUsers
	if ltu.UserGroups.ElemName == "" {
		ltu.UserGroups.ElemName = "user_group"
	}
	for _, g := range ltu.UserGroups.Items {
		if strings.EqualFold(g, name) {
			return false
		}
	}
	ltu.UserGroups.Items = append(ltu.UserGroups.Items, name)
	return true
}

func removePolicyLimitUserGroup(s *ScopeXML, name string) bool {
	if s.LimitToUsers == nil {
		return false
	}
	groups := &s.LimitToUsers.UserGroups
	var keep []string
	found := false
	for _, g := range groups.Items {
		if strings.EqualFold(g, name) {
			found = true
			continue
		}
		keep = append(keep, g)
	}
	if !found {
		return false
	}
	groups.Items = keep
	return true
}

func isPolicyLimitUserGroup(singularKey, section, flagName string) bool {
	return singularKey == "policy" && section == "limitation" && flagName == "user-group"
}

func getOrCreateScopeItems(s *ScopeXML, section, flagName string) *ScopeItemSlice {
	switch section {
	case "target":
		return targetItems(s, flagName)
	case "limitation":
		if s.Limitations == nil {
			s.Limitations = &LimitationsXML{}
		}
		return limitationItems(s.Limitations, flagName)
	case "exclusion":
		if s.Exclusions == nil {
			s.Exclusions = &ExclusionsXML{}
		}
		return exclusionItems(s.Exclusions, flagName)
	}
	return nil
}

func readScopeItems(s *ScopeXML, section, flagName string) *ScopeItemSlice {
	switch section {
	case "target":
		return targetItems(s, flagName)
	case "limitation":
		if s.Limitations == nil {
			return nil
		}
		return limitationItems(s.Limitations, flagName)
	case "exclusion":
		if s.Exclusions == nil {
			return nil
		}
		return exclusionItems(s.Exclusions, flagName)
	}
	return nil
}

func targetItems(s *ScopeXML, flagName string) *ScopeItemSlice {
	switch flagName {
	case "computer-group":
		return &s.ComputerGroups
	case "mobile-device-group":
		return &s.MobileDeviceGroups
	case "building":
		return &s.Buildings
	case "department":
		return &s.Departments
	}
	return nil
}

func limitationItems(lim *LimitationsXML, flagName string) *ScopeItemSlice {
	switch flagName {
	case "network-segment":
		return &lim.NetworkSegments
	case "user-group":
		return &lim.UserGroups
	case "computer-group":
		return &lim.ComputerGroups
	}
	return nil
}

func exclusionItems(exc *ExclusionsXML, flagName string) *ScopeItemSlice {
	switch flagName {
	case "computer-group":
		return &exc.ComputerGroups
	case "mobile-device-group":
		return &exc.MobileDeviceGroups
	case "user-group":
		return &exc.UserGroups
	case "network-segment":
		return &exc.NetworkSegments
	case "building":
		return &exc.Buildings
	case "department":
		return &exc.Departments
	}
	return nil
}

func appendNamedRows(rows *[]map[string]any, section, typeName string, items []NamedItem) {
	for _, item := range items {
		if item.Name != "" {
			*rows = append(*rows, map[string]any{"section": section, "type": typeName, "name": item.Name})
		}
	}
}
