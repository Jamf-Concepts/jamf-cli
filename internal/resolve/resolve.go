// Copyright 2026, Jamf Software LLC

// Package resolve provides device identifier resolution for Jamf Pro.
// It translates serial numbers, device names, and numeric IDs into the
// full set of identifiers (ID, managementId, UDID) required by various
// API endpoints.
package resolve

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"unicode"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/xmlconv"
)

// DeviceIdentifiers holds all ID forms needed by different action endpoints.
type DeviceIdentifiers struct {
	ID           string // numeric ID (string form, e.g. "42")
	ManagementID string // UUID for blank-push, ddm-sync
	UDID         string // device UDID for renew-mdm
	Name         string // display name for confirmation messages
	SerialNumber string // serial number for confirmation messages
}

// ResolveComputer looks up a computer by serial, name, or ID using the
// v3 computers-inventory API and returns all identifier forms.
// Exactly one of serial, name, or id must be non-empty.
func ResolveComputer(ctx context.Context, client registry.HTTPClient, serial, name, id string) (*DeviceIdentifiers, error) {
	switch {
	case serial != "":
		return resolveComputerByFilter(ctx, client,
			fmt.Sprintf(`hardware.serialNumber=="%s"`, EscapeRSQL(serial)),
			fmt.Sprintf("serial number %q", serial))
	case name != "":
		return resolveComputerByFilter(ctx, client,
			fmt.Sprintf(`general.name=="%s"`, EscapeRSQL(name)),
			fmt.Sprintf("name %q", name))
	case id != "":
		return resolveComputerByID(ctx, client, id)
	default:
		return nil, fmt.Errorf("one of --serial, --name, or --id is required")
	}
}

// ResolveMobileDevice looks up a mobile device by serial, name, or ID using
// the v2 mobile-devices API and returns all identifier forms.
func ResolveMobileDevice(ctx context.Context, client registry.HTTPClient, serial, name, id string) (*DeviceIdentifiers, error) {
	switch {
	case serial != "":
		return resolveMobileByFilter(ctx, client,
			fmt.Sprintf(`serialNumber=="%s"`, EscapeRSQL(serial)),
			fmt.Sprintf("serial number %q", serial))
	case name != "":
		return resolveMobileByFilter(ctx, client,
			fmt.Sprintf(`displayName=="%s"`, EscapeRSQL(name)),
			fmt.Sprintf("name %q", name))
	case id != "":
		return resolveMobileByID(ctx, client, id)
	default:
		return nil, fmt.Errorf("one of --serial, --name, or --id is required")
	}
}

// ResolveComputerGroup resolves all members of a computer group by name.
// Uses the Classic API to list group members, then batch-resolves each
// via the v3 inventory API to get managementId/UDID.
func ResolveComputerGroup(ctx context.Context, client registry.HTTPClient, groupName string) ([]*DeviceIdentifiers, error) {
	// Try smart group first (modern API), fall back to static group.
	memberIDs, err := fetchSmartComputerGroupMemberIDs(ctx, client, groupName)
	if err != nil {
		// Not found as smart group — try static group (Classic API, no modern
		// membership endpoint exists for static computer groups).
		staticIDs, staticErr := fetchClassicGroupMemberIDs(ctx, client,
			"/JSSResource/computergroups", "computer_groups", "computers", groupName)
		if staticErr != nil {
			return nil, fmt.Errorf("group %q not found as smart group (%v) or static group (%v)", groupName, err, staticErr)
		}
		memberIDs = staticIDs
	}
	return batchResolveComputers(ctx, client, memberIDs)
}

// ResolveMobileDeviceGroup resolves all members of a mobile device group by name.
func ResolveMobileDeviceGroup(ctx context.Context, client registry.HTTPClient, groupName string) ([]*DeviceIdentifiers, error) {
	// Try smart group first (modern API), fall back to Classic for static
	// (no modern static mobile device group API exists yet).
	memberIDs, err := fetchSmartMobileGroupMemberIDs(ctx, client, groupName)
	if err != nil {
		staticIDs, staticErr := fetchClassicGroupMemberIDs(ctx, client,
			"/JSSResource/mobiledevicegroups", "mobile_device_groups", "mobile_devices", groupName)
		if staticErr != nil {
			return nil, fmt.Errorf("group %q not found as smart group (%v) or static group (%v)", groupName, err, staticErr)
		}
		memberIDs = staticIDs
	}
	return batchResolveMobileDevices(ctx, client, memberIDs)
}

// ResolveComputersFromFile reads serials or IDs from a file (one per line)
// and resolves each to full identifiers. Blank lines and #-comments are skipped.
func ResolveComputersFromFile(ctx context.Context, client registry.HTTPClient, path string) ([]*DeviceIdentifiers, error) {
	entries, err := readEntriesFromFile(path)
	if err != nil {
		return nil, err
	}
	var results []*DeviceIdentifiers
	for _, entry := range entries {
		var d *DeviceIdentifiers
		if isNumericID(entry) {
			d, err = ResolveComputer(ctx, client, "", "", entry)
		} else {
			d, err = ResolveComputer(ctx, client, entry, "", "")
		}
		if err != nil {
			return nil, fmt.Errorf("resolving %q: %w", entry, err)
		}
		results = append(results, d)
	}
	return results, nil
}

// ResolveMobileDevicesFromFile reads serials or IDs from a file and resolves each.
func ResolveMobileDevicesFromFile(ctx context.Context, client registry.HTTPClient, path string) ([]*DeviceIdentifiers, error) {
	entries, err := readEntriesFromFile(path)
	if err != nil {
		return nil, err
	}
	var results []*DeviceIdentifiers
	for _, entry := range entries {
		var d *DeviceIdentifiers
		if isNumericID(entry) {
			d, err = ResolveMobileDevice(ctx, client, "", "", entry)
		} else {
			d, err = ResolveMobileDevice(ctx, client, entry, "", "")
		}
		if err != nil {
			return nil, fmt.Errorf("resolving %q: %w", entry, err)
		}
		results = append(results, d)
	}
	return results, nil
}

// --- Computer resolution helpers ---

func resolveComputerByFilter(ctx context.Context, client registry.HTTPClient, filter, desc string) (*DeviceIdentifiers, error) {
	// Use page-size=2 to detect ambiguity (multiple matches).
	path := fmt.Sprintf("/v3/computers-inventory?section=GENERAL&section=HARDWARE&page-size=2&filter=%s",
		url.QueryEscape(filter))

	results, total, err := fetchInventoryPage(ctx, client, path)
	if err != nil {
		return nil, fmt.Errorf("looking up computer by %s: %w", desc, err)
	}
	if total == 0 || len(results) == 0 {
		return nil, fmt.Errorf("no computer found with %s", desc)
	}
	if total > 1 {
		return nil, fmt.Errorf("multiple computers found with %s (%d matches); use --serial or --id to disambiguate", desc, total)
	}
	return parseComputerInventory(results[0])
}

func resolveComputerByID(ctx context.Context, client registry.HTTPClient, id string) (*DeviceIdentifiers, error) {
	path := fmt.Sprintf("/v3/computers-inventory/%s?section=GENERAL&section=HARDWARE", url.PathEscape(id))
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("looking up computer ID %s: %w", id, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no computer found with ID %s", id)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("looking up computer ID %s: HTTP %d", id, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, fmt.Errorf("parsing computer response: %w", err)
	}
	return parseComputerInventory(obj)
}

func parseComputerInventory(obj map[string]any) (*DeviceIdentifiers, error) {
	d := &DeviceIdentifiers{}
	d.ID = jsonString(obj, "id")
	d.UDID = jsonString(obj, "udid")

	if general, ok := obj["general"].(map[string]any); ok {
		d.Name = jsonString(general, "name")
		d.ManagementID = jsonString(general, "managementId")
	}
	if hardware, ok := obj["hardware"].(map[string]any); ok {
		d.SerialNumber = jsonString(hardware, "serialNumber")
	}

	if d.ID == "" {
		return nil, fmt.Errorf("computer response missing id field")
	}
	return d, nil
}

// --- Mobile device resolution helpers ---

func resolveMobileByFilter(ctx context.Context, client registry.HTTPClient, filter, desc string) (*DeviceIdentifiers, error) {
	// Use /v2/mobile-devices/detail because /v2/mobile-devices ignores RSQL filters.
	path := fmt.Sprintf("/v2/mobile-devices/detail?page-size=2&filter=%s",
		url.QueryEscape(filter))

	results, total, err := fetchInventoryPage(ctx, client, path)
	if err != nil {
		return nil, fmt.Errorf("looking up mobile device by %s: %w", desc, err)
	}
	if total == 0 || len(results) == 0 {
		return nil, fmt.Errorf("no mobile device found with %s", desc)
	}
	if total > 1 {
		return nil, fmt.Errorf("multiple mobile devices found with %s (%d matches); use --serial or --id to disambiguate", desc, total)
	}
	return parseMobileDevice(results[0])
}

func resolveMobileByID(ctx context.Context, client registry.HTTPClient, id string) (*DeviceIdentifiers, error) {
	path := fmt.Sprintf("/v2/mobile-devices/%s", url.PathEscape(id))
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("looking up mobile device ID %s: %w", id, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no mobile device found with ID %s", id)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("looking up mobile device ID %s: HTTP %d", id, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, fmt.Errorf("parsing mobile device response: %w", err)
	}
	return parseMobileDevice(obj)
}

func parseMobileDevice(obj map[string]any) (*DeviceIdentifiers, error) {
	d := &DeviceIdentifiers{
		ID:           jsonString(obj, "id"),
		ManagementID: jsonString(obj, "managementId"),
		UDID:         jsonString(obj, "udid"),
		Name:         jsonString(obj, "name"),
		SerialNumber: jsonString(obj, "serialNumber"),
	}
	// /v2/mobile-devices/detail returns "mobileDeviceId" instead of "id".
	if d.ID == "" {
		d.ID = jsonString(obj, "mobileDeviceId")
	}
	if d.Name == "" {
		d.Name = jsonString(obj, "displayName")
	}
	if d.ID == "" {
		return nil, fmt.Errorf("mobile device response missing id field")
	}
	return d, nil
}

// --- Smart group resolution (modern API) ---

// fetchSmartComputerGroupMemberIDs finds a smart computer group by name via
// the v2 API and returns its member IDs.
func fetchSmartComputerGroupMemberIDs(ctx context.Context, client registry.HTTPClient, groupName string) ([]string, error) {
	// Look up group by name with RSQL filter.
	groupID, err := resolveGroupIDByName(ctx, client,
		"/v2/computer-groups/smart-groups", "name", groupName)
	if err != nil {
		return nil, err
	}

	// Fetch membership: returns {"members": [1, 2, 3]}.
	path := fmt.Sprintf("/v2/computer-groups/smart-group-membership/%s", url.PathEscape(groupID))
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching smart group membership: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching smart group membership: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, err
	}

	var membership struct {
		Members []int `json:"members"`
	}
	if err := json.Unmarshal(body, &membership); err != nil {
		return nil, fmt.Errorf("parsing smart group membership: %w", err)
	}

	ids := make([]string, len(membership.Members))
	for i, m := range membership.Members {
		ids[i] = fmt.Sprintf("%d", m)
	}
	return ids, nil
}

// fetchSmartMobileGroupMemberIDs finds a smart mobile device group by name via
// the v1 API and returns its member IDs.
func fetchSmartMobileGroupMemberIDs(ctx context.Context, client registry.HTTPClient, groupName string) ([]string, error) {
	groupID, err := resolveGroupIDByName(ctx, client,
		"/v1/mobile-device-groups/smart-groups", "groupName", groupName)
	if err != nil {
		return nil, err
	}

	// Fetch membership: paginated response with device details.
	path := fmt.Sprintf("/v1/mobile-device-groups/smart-group-membership/%s", url.PathEscape(groupID))
	results, err := fetchAllPages(ctx, client, path)
	if err != nil {
		return nil, fmt.Errorf("fetching smart mobile group membership: %w", err)
	}

	ids := make([]string, 0, len(results))
	for _, r := range results {
		id := jsonString(r, "id")
		if id == "" {
			id = jsonString(r, "mobileDeviceId")
		}
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// resolveGroupIDByName uses RSQL filtering on a group list endpoint to find
// a group by name and return its ID.
func resolveGroupIDByName(ctx context.Context, client registry.HTTPClient, listPath, nameField, groupName string) (string, error) {
	filter := fmt.Sprintf(`%s=="%s"`, nameField, EscapeRSQL(groupName))
	path := fmt.Sprintf("%s?page-size=2&filter=%s", listPath, url.QueryEscape(filter))

	results, total, err := fetchInventoryPage(ctx, client, path)
	if err != nil {
		return "", fmt.Errorf("searching for group %q: %w", groupName, err)
	}
	if total == 0 || len(results) == 0 {
		return "", fmt.Errorf("group %q not found", groupName)
	}
	if total > 1 {
		return "", fmt.Errorf("multiple groups found with name %q (%d matches)", groupName, total)
	}
	id := jsonString(results[0], "id")
	if id == "" {
		id = jsonString(results[0], "groupId")
	}
	if id == "" {
		return "", fmt.Errorf("group %q found but missing id field", groupName)
	}
	return id, nil
}

// --- Classic API group fallback (static groups) ---

func fetchClassicGroupMemberIDs(ctx context.Context, client registry.HTTPClient, listPath, listKey, membersKey, groupName string) ([]string, error) {
	// List all groups to find the ID by name.
	resp, err := client.Do(ctx, "GET", listPath, nil)
	if err != nil {
		return nil, fmt.Errorf("listing groups: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	listData, err := unmarshalClassic(body)
	if err != nil {
		return nil, fmt.Errorf("parsing group list: %w", err)
	}

	groups, _ := listData[listKey].([]any)
	var groupID string
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		name := jsonString(gm, "name")
		if strings.EqualFold(name, groupName) {
			groupID = jsonString(gm, "id")
			break
		}
	}
	if groupID == "" {
		return nil, fmt.Errorf("group %q not found", groupName)
	}

	// Fetch group detail to get member IDs.
	detailPath := fmt.Sprintf("%s/id/%s", listPath, url.PathEscape(groupID))
	resp2, err := client.Do(ctx, "GET", detailPath, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching group %q: %w", groupName, err)
	}
	defer func() { _ = resp2.Body.Close() }()

	body2, err := io.ReadAll(io.LimitReader(resp2.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	detail, err := unmarshalClassic(body2)
	if err != nil {
		return nil, fmt.Errorf("parsing group detail: %w", err)
	}

	// Unwrap Classic API detail envelope (e.g., {"computer_group": {...}}).
	for _, v := range detail {
		if inner, ok := v.(map[string]any); ok {
			detail = inner
			break
		}
	}

	members, _ := detail[membersKey].([]any)
	var ids []string
	for _, m := range members {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		id := jsonString(mm, "id")
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func batchResolveComputers(ctx context.Context, client registry.HTTPClient, ids []string) ([]*DeviceIdentifiers, error) {
	results := make([]*DeviceIdentifiers, 0, len(ids))
	for _, id := range ids {
		d, err := resolveComputerByID(ctx, client, id)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "  warning: could not resolve computer ID %s: %v\n", id, err)
			continue
		}
		results = append(results, d)
	}
	return results, nil
}

func batchResolveMobileDevices(ctx context.Context, client registry.HTTPClient, ids []string) ([]*DeviceIdentifiers, error) {
	results := make([]*DeviceIdentifiers, 0, len(ids))
	for _, id := range ids {
		d, err := resolveMobileByID(ctx, client, id)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "  warning: could not resolve mobile device ID %s: %v\n", id, err)
			continue
		}
		results = append(results, d)
	}
	return results, nil
}

// --- Shared helpers ---

// unmarshalClassic parses a Classic API response that may be XML or JSON.
func unmarshalClassic(data []byte) (map[string]any, error) {
	if xmlconv.IsXML(data) {
		return xmlconv.ToMap(data)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// fetchAllPages fetches all pages from a paginated endpoint that returns
// {"totalCount": N, "results": [...]}. The basePath should include any
// filters but NOT page/page-size params (they are appended automatically).
func fetchAllPages(ctx context.Context, client registry.HTTPClient, basePath string) ([]map[string]any, error) {
	const pageSize = 200
	var allResults []map[string]any

	sep := "?"
	if strings.Contains(basePath, "?") {
		sep = "&"
	}

	for page := 0; ; page++ {
		pagePath := fmt.Sprintf("%s%spage=%d&page-size=%d", basePath, sep, page, pageSize)
		results, total, err := fetchInventoryPage(ctx, client, pagePath)
		if err != nil {
			return nil, err
		}
		allResults = append(allResults, results...)
		if len(allResults) >= total || len(results) == 0 {
			break
		}
	}
	return allResults, nil
}

// fetchInventoryPage makes a GET request and parses a paginated response
// with {"totalCount": N, "results": [...]}.
func fetchInventoryPage(ctx context.Context, client registry.HTTPClient, path string) ([]map[string]any, int, error) {
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, 0, err
	}

	var page struct {
		TotalCount int              `json:"totalCount"`
		Results    []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, 0, fmt.Errorf("parsing paginated response: %w", err)
	}
	return page.Results, page.TotalCount, nil
}

// jsonString extracts a string field from a map, handling both string and
// numeric ID values (the Jamf API sometimes returns IDs as numbers).
func jsonString(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return fmt.Sprintf("%d", int(val))
	default:
		return fmt.Sprintf("%v", val)
	}
}

// EscapeRSQL escapes double quotes in a value for use in RSQL filter expressions.
func EscapeRSQL(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}

// isNumericID returns true if s contains only digits (i.e., it's a Jamf Pro ID,
// not a serial number).
func isNumericID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// readEntriesFromFile reads lines from a file, skipping blanks and #-comments.
func readEntriesFromFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening file %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var entries []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		entries = append(entries, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading file %s: %w", path, err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("file %s contains no entries", path)
	}
	return entries, nil
}

// resolveClassicGroupID is the shared implementation for Classic API group ID
// lookups. pathSegment is the JSSResource collection name (e.g. "computergroups"),
// label is the human-readable type used in error messages (e.g. "computer group").
func resolveClassicGroupID(ctx context.Context, client registry.HTTPClient, pathSegment, label, groupName string) (string, error) {
	path := fmt.Sprintf("/JSSResource/%s/name/%s", pathSegment, url.PathEscape(groupName))
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return "", fmt.Errorf("looking up %s %q: %w", label, groupName, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("%s %q not found", label, groupName)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("looking up %s %q: HTTP %d", label, groupName, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	detail, err := unmarshalClassic(body)
	if err != nil {
		return "", fmt.Errorf("parsing %s response: %w", label, err)
	}
	for _, v := range detail {
		if inner, ok := v.(map[string]any); ok {
			if id := jsonString(inner, "id"); id != "" {
				return id, nil
			}
		}
	}
	return "", fmt.Errorf("%s %q: id not found in response", label, groupName)
}

// ResolveClassicComputerGroupID resolves a computer group name to its Classic API
// numeric ID. Works for both smart and static computer groups.
func ResolveClassicComputerGroupID(ctx context.Context, client registry.HTTPClient, groupName string) (string, error) {
	return resolveClassicGroupID(ctx, client, "computergroups", "computer group", groupName)
}

// ResolveClassicMobileGroupID resolves a mobile device group name to its Classic API
// numeric ID. Works for both smart and static mobile device groups.
func ResolveClassicMobileGroupID(ctx context.Context, client registry.HTTPClient, groupName string) (string, error) {
	return resolveClassicGroupID(ctx, client, "mobiledevicegroups", "mobile device group", groupName)
}

// FormatDeviceDesc returns a human-readable device description for confirmation messages.
// Example: "Neil's MacBook" (serial: C02X1234, id: 42)
func FormatDeviceDesc(d *DeviceIdentifiers) string {
	parts := []string{}
	if d.SerialNumber != "" {
		parts = append(parts, "serial: "+d.SerialNumber)
	}
	parts = append(parts, "id: "+d.ID)

	name := d.Name
	if name == "" {
		name = d.ID
	}
	return fmt.Sprintf("%q (%s)", name, strings.Join(parts, ", "))
}
