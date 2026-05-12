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
// the resource's ID and parsed scope. When res.ResolveByList is true it lists
// all records to resolve name→ID first (for resources with no /name/ endpoint).
func FetchScope(ctx context.Context, client registry.HTTPClient, res Resource, name string) (string, *ScopeXML, error) {
	var fetchPath string
	var resolvedID string

	if res.ResolveByList {
		id, err := resolveNameToID(ctx, client, res.APIPath, res.SingularKey, name)
		if err != nil {
			return "", nil, err
		}
		resolvedID = id
		fetchPath = fmt.Sprintf("/JSSResource/%s/id/%s", res.APIPath, url.PathEscape(id))
	} else {
		fetchPath = fmt.Sprintf("/JSSResource/%s/name/%s", res.APIPath, url.PathEscape(name))
	}

	resp, err := client.Do(ctx, "GET", fetchPath, nil)
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

	if res.ResolveByList {
		return resolvedID, &envelope.Scope, nil
	}

	if envelope.General.ID == "" {
		return "", nil, fmt.Errorf("no ID in %s %q", res.SingularKey, name)
	}
	return envelope.General.ID, &envelope.Scope, nil
}

// resolveNameToID lists all records at the resource root and returns the ID
// of the first record whose <name> matches (case-insensitive).
func resolveNameToID(ctx context.Context, client registry.HTTPClient, apiPath, singularKey, name string) (string, error) {
	resp, err := client.Do(ctx, "GET", "/JSSResource/"+apiPath, nil)
	if err != nil {
		return "", fmt.Errorf("listing %s: %w", singularKey, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return "", fmt.Errorf("reading list response: %w", err)
	}

	d := xml.NewDecoder(bytes.NewReader(body))
	depth := 0
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("parsing %s list XML: %w", singularKey, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth == 2 {
				var it struct {
					ID   string `xml:"id"`
					Name string `xml:"name"`
				}
				if decErr := d.DecodeElement(&it, &t); decErr == nil && strings.EqualFold(it.Name, name) {
					return it.ID, nil
				}
				depth-- // DecodeElement consumed the end element
			}
		case xml.EndElement:
			depth--
		}
	}
	return "", fmt.Errorf("%s %q not found", singularKey, name)
}

// PutScope writes an updated scope back to the Classic API. Uses the subset/Scope
// endpoint by default; falls back to a full document PUT when res.NoSubsetPut is set.
func PutScope(ctx context.Context, client registry.HTTPClient, res Resource, id string, s *ScopeXML) error {
	if res.NoSubsetPut {
		return putFullDocument(ctx, client, res, id, s)
	}

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

// putFullDocument fetches the full resource XML, splices in the updated scope,
// and PUTs the whole document back. Used for Classic API resources that do not
// support the /subset/Scope endpoint (e.g. vppassignments).
func putFullDocument(ctx context.Context, client registry.HTTPClient, res Resource, id string, s *ScopeXML) error {
	fetchPath := fmt.Sprintf("/JSSResource/%s/id/%s", res.APIPath, url.PathEscape(id))
	resp, err := client.Do(ctx, "GET", fetchPath, nil)
	if err != nil {
		return fmt.Errorf("fetching %s for update: %w", res.SingularKey, err)
	}
	defer func() { _ = resp.Body.Close() }()

	original, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return fmt.Errorf("reading %s: %w", res.SingularKey, err)
	}

	updated, err := replaceScopeInXML(original, s)
	if err != nil {
		return fmt.Errorf("replacing scope: %w", err)
	}

	putResp, err := client.Do(ctx, "PUT", fetchPath, bytes.NewReader(updated))
	if err != nil {
		return fmt.Errorf("updating scope: %w", err)
	}
	_ = putResp.Body.Close()
	return nil
}

// replaceScopeInXML finds the <scope>...</scope> block in the XML bytes and
// replaces it with the marshalled newScope. Classic API XML is well-formed so
// simple byte search is safe.
func replaceScopeInXML(original []byte, newScope *ScopeXML) ([]byte, error) {
	newScopeXML, err := xml.MarshalIndent(newScope, "  ", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling scope: %w", err)
	}

	scopeOpen := bytes.Index(original, []byte("<scope>"))
	if scopeOpen == -1 {
		return nil, fmt.Errorf("no <scope> element found in resource XML")
	}
	closeTag := []byte("</scope>")
	rel := bytes.Index(original[scopeOpen:], closeTag)
	if rel == -1 {
		return nil, fmt.Errorf("no </scope> closing tag found in resource XML")
	}
	scopeClose := scopeOpen + rel + len(closeTag)

	var buf bytes.Buffer
	buf.Write(original[:scopeOpen])
	buf.Write(newScopeXML)
	buf.Write(original[scopeClose:])
	return buf.Bytes(), nil
}

// VerifyItemInScope refetches scope after a PUT and confirms the given item is
// (or is not) present, depending on `expectPresent`. Catches the silent-drop
// case where the server returns 200/201 but discards a scope element that
// doesn't apply to the resource type (e.g. computer_groups limitation on a
// policy, network_segment on a VPP assignment).
func VerifyItemInScope(ctx context.Context, client registry.HTTPClient, res Resource, name, section, flagName, itemName string, expectPresent bool) error {
	_, s, err := FetchScope(ctx, client, res, name)
	if err != nil {
		return fmt.Errorf("verifying scope: %w", err)
	}

	if isPolicyLimitUserGroup(res.SingularKey, section, flagName) {
		ltuPresent := false
		if s.LimitToUsers != nil {
			for _, g := range s.LimitToUsers.UserGroups.Items {
				if strings.EqualFold(g, itemName) {
					ltuPresent = true
					break
				}
			}
		}
		limPresent := false
		if s.Limitations != nil {
			for _, item := range s.Limitations.UserGroups.Items {
				if strings.EqualFold(item.Name, itemName) {
					limPresent = true
					break
				}
			}
		}
		present := ltuPresent || limPresent
		if present != expectPresent {
			return silentDropError(res.SingularKey, section, flagName, itemName, expectPresent)
		}
		return nil
	}

	items := readScopeItems(s, section, flagName)
	present := false
	if items != nil {
		for _, item := range items.Items {
			if strings.EqualFold(item.Name, itemName) {
				present = true
				break
			}
		}
	}
	if present != expectPresent {
		return silentDropError(res.SingularKey, section, flagName, itemName, expectPresent)
	}
	return nil
}

func silentDropError(singularKey, section, flagName, itemName string, expectedPresent bool) error {
	if expectedPresent {
		return fmt.Errorf("server accepted PUT but did not persist --%s %q in %s scope (resource type %q does not support this scope element)", flagName, itemName, section, singularKey)
	}
	return fmt.Errorf("server accepted PUT but did not remove --%s %q from %s scope (resource type %q may not allow modification of this scope element)", flagName, itemName, section, singularKey)
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
		items.ElemName = resolveElemName(section, flagName)
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
	if s.AllMobileDevices {
		rows = append(rows, map[string]any{"section": "target", "type": "all_mobile_devices", "name": "true"})
	}
	if s.AllJSSUsers {
		rows = append(rows, map[string]any{"section": "target", "type": "all_jss_users", "name": "true"})
	}

	appendNamedRows(&rows, "target", "computer", s.Computers.Items)
	appendNamedRows(&rows, "target", "computer_group", s.ComputerGroups.Items)
	appendNamedRows(&rows, "target", "mobile_device", s.MobileDevices.Items)
	appendNamedRows(&rows, "target", "mobile_device_group", s.MobileDeviceGroups.Items)
	appendNamedRows(&rows, "target", "building", s.Buildings.Items)
	appendNamedRows(&rows, "target", "department", s.Departments.Items)
	appendNamedRows(&rows, "target", "jss_user", s.JSSUsers.Items)
	appendNamedRows(&rows, "target", "jss_user_group", s.JSSUserGroups.Items)
	appendNamedRows(&rows, "target", "class", s.Classes.Items)

	// Policy special case: limit_to_users holds plain strings
	if singularKey == "policy" && s.LimitToUsers != nil {
		for _, g := range s.LimitToUsers.UserGroups.Items {
			rows = append(rows, map[string]any{"section": "limitation", "type": "user_group", "name": g})
		}
	}

	if s.Limitations != nil {
		appendNamedRows(&rows, "limitation", "user", s.Limitations.Users.Items)
		appendNamedRows(&rows, "limitation", "network_segment", s.Limitations.NetworkSegments.Items)
		// For policies, user groups are already emitted from limit_to_users above;
		// limitations/user_groups is a server-side mirror, so skip it to avoid duplicates.
		if singularKey != "policy" {
			appendNamedRows(&rows, "limitation", "user_group", s.Limitations.UserGroups.Items)
		}
		appendNamedRows(&rows, "limitation", "ibeacon", s.Limitations.IBeacons.Items)
	}

	if s.Exclusions != nil {
		appendNamedRows(&rows, "exclusion", "computer", s.Exclusions.Computers.Items)
		appendNamedRows(&rows, "exclusion", "computer_group", s.Exclusions.ComputerGroups.Items)
		appendNamedRows(&rows, "exclusion", "mobile_device", s.Exclusions.MobileDevices.Items)
		appendNamedRows(&rows, "exclusion", "mobile_device_group", s.Exclusions.MobileDeviceGroups.Items)
		appendNamedRows(&rows, "exclusion", "user", s.Exclusions.Users.Items)
		appendNamedRows(&rows, "exclusion", "user_group", s.Exclusions.UserGroups.Items)
		appendNamedRows(&rows, "exclusion", "jss_user", s.Exclusions.JSSUsers.Items)
		appendNamedRows(&rows, "exclusion", "jss_user_group", s.Exclusions.JSSUserGroups.Items)
		appendNamedRows(&rows, "exclusion", "network_segment", s.Exclusions.NetworkSegments.Items)
		appendNamedRows(&rows, "exclusion", "building", s.Exclusions.Buildings.Items)
		appendNamedRows(&rows, "exclusion", "department", s.Exclusions.Departments.Items)
		appendNamedRows(&rows, "exclusion", "ibeacon", s.Exclusions.IBeacons.Items)
	}

	return rows
}

// ValidateScopeCombination checks that the given section/flag combination is valid
// for the resource type. The acceptance rules below reflect live testing of every
// flag against every scope-enabled Classic resource (see docs/solutions/).
//
// Notable rules:
//   - `--user-group` is only valid in limitation/exclusion (LDAP/AD group). Use
//     `--jss-user-group` to target a JSS user group; the previous overload of
//     `--user-group` for target was ambiguous and made wrong-section mistakes
//     undetectable until a GET roundtrip.
//   - Restricted software is computer-only: it accepts no limitations, and only
//     computer-group / building / department for target and exclusion. The
//     server silently drops jss_user_groups / all_jss_users / user_groups for
//     this resource type.
//   - `--computer` is only valid in target/exclusion (no concept of a "computer
//     limitation"). `--mobile-device` same.
//   - `--user` (inventory username, free-text) is only valid in
//     limitation/exclusion.
func ValidateScopeCombination(singularKey, section, flagName string) error {
	isRestricted := singularKey == "restricted_software"

	switch section {
	case "target":
		if isRestricted {
			switch flagName {
			case "computer", "computer-group", "building", "department":
				return nil
			}
			return fmt.Errorf("--%s is not valid as a target for restricted software; use --computer, --computer-group, --building, or --department", flagName)
		}
		switch flagName {
		case "computer", "computer-group", "mobile-device", "mobile-device-group",
			"building", "department", "jss-user-group", "jss-user":
			return nil
		}
		return fmt.Errorf("--%s is not valid as a target; use --computer, --computer-group, --mobile-device, --mobile-device-group, --building, --department, --jss-user-group, or --jss-user", flagName)

	case "limitation":
		if isRestricted {
			return fmt.Errorf("restricted software does not support limitations")
		}
		switch flagName {
		case "network-segment", "user", "user-group":
			return nil
		}
		return fmt.Errorf("--%s is not valid as a limitation; use --network-segment, --user, or --user-group", flagName)

	case "exclusion":
		if isRestricted {
			switch flagName {
			case "computer", "computer-group", "building", "department":
				return nil
			}
			return fmt.Errorf("--%s is not valid as an exclusion for restricted software; use --computer, --computer-group, --building, or --department", flagName)
		}
		switch flagName {
		case "computer", "computer-group", "mobile-device", "mobile-device-group",
			"user", "user-group", "jss-user-group", "jss-user",
			"network-segment", "building", "department":
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
		return ScopeTarget{}, fmt.Errorf("specify one of: --computer, --computer-group, --mobile-device, --mobile-device-group, --building, --department, --network-segment, --user, --user-group, --jss-user-group, --jss-user")
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
	cmd.Flags().String("computer", "", "individual computer (id, name, or UDID); target/exclusion only")
	cmd.Flags().String("computer-group", "", "computer group name")
	cmd.Flags().String("mobile-device", "", "individual mobile device (id, name, or UDID); target/exclusion only")
	cmd.Flags().String("mobile-device-group", "", "mobile device group name")
	cmd.Flags().String("building", "", "building name")
	cmd.Flags().String("department", "", "department name")
	cmd.Flags().String("network-segment", "", "network segment name (limitations/exclusions)")
	cmd.Flags().String("user", "", "inventory username (free-text, limitations/exclusions only)")
	cmd.Flags().String("user-group", "", "LDAP/directory user group name (limitations/exclusions only — use --jss-user-group for target)")
	cmd.Flags().String("jss-user-group", "", "JSS user group name (target/exclusion)")
	cmd.Flags().String("jss-user", "", "JSS user name (target/exclusion)")
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
	case "computer":
		return &s.Computers
	case "computer-group":
		return &s.ComputerGroups
	case "mobile-device":
		return &s.MobileDevices
	case "mobile-device-group":
		return &s.MobileDeviceGroups
	case "building":
		return &s.Buildings
	case "department":
		return &s.Departments
	case "jss-user-group":
		return &s.JSSUserGroups
	case "jss-user":
		return &s.JSSUsers
	}
	return nil
}

func limitationItems(lim *LimitationsXML, flagName string) *ScopeItemSlice {
	switch flagName {
	case "network-segment":
		return &lim.NetworkSegments
	case "user":
		return &lim.Users
	case "user-group":
		return &lim.UserGroups
	}
	return nil
}

func exclusionItems(exc *ExclusionsXML, flagName string) *ScopeItemSlice {
	switch flagName {
	case "computer":
		return &exc.Computers
	case "computer-group":
		return &exc.ComputerGroups
	case "mobile-device":
		return &exc.MobileDevices
	case "mobile-device-group":
		return &exc.MobileDeviceGroups
	case "user":
		return &exc.Users
	case "user-group":
		return &exc.UserGroups
	case "jss-user-group":
		return &exc.JSSUserGroups
	case "jss-user":
		return &exc.JSSUsers
	case "network-segment":
		return &exc.NetworkSegments
	case "building":
		return &exc.Buildings
	case "department":
		return &exc.Departments
	}
	return nil
}

// resolveElemName returns the XML child element name for a new scope item.
// Both --user-group and --jss-user-group route to jss_user_groups whose
// children are <user_group>, so flagToElemName["user-group"] = "user_group"
// is correct for both cases.
func resolveElemName(section, flagName string) string {
	return flagToElemName[flagName]
}

func appendNamedRows(rows *[]map[string]any, section, typeName string, items []NamedItem) {
	for _, item := range items {
		if item.Name != "" {
			*rows = append(*rows, map[string]any{"section": section, "type": typeName, "name": item.Name})
		}
	}
}
