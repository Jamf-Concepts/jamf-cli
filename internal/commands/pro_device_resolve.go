// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// escapeRSQL escapes characters that have special meaning in RSQL filter values.
func escapeRSQL(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}

// Security status constants used across multiple commands.
const (
	statusFVAllEncrypted  = "ALL_ENCRYPTED"
	statusFVBootEncrypted = "BOOT_ENCRYPTED"
	statusGKDisabled      = "DISABLED"
	statusGKDisabledAlt   = "Disabled" // Some API versions use mixed case
	statusSIPEnabled      = "ENABLED"
	statusSIPEnabledAlt   = "Enabled"
)

// resolveDeviceByIdentifier takes a free-form identifier (Jamf ID, serial
// number, or computer name) and returns the device's Jamf ID and display name.
//
// Resolution order:
//  1. Try identifier as Jamf ID via the detail endpoint.
//  2. If 404, try as serial number via RSQL filter on hardware.serialNumber.
//  3. If 0 results, try as name via RSQL filter on general.name.
//
// Returns an error if zero matches or multiple matches are found.
func resolveDeviceByIdentifier(ctx context.Context, client registry.HTTPClient, identifier string) (string, string, error) {
	// 1. Try as Jamf ID — direct lookup.
	id, name, err := tryDeviceByID(ctx, client, identifier)
	if err == nil {
		return id, name, nil
	}

	// 2. Try as serial number.
	filter := fmt.Sprintf(`hardware.serialNumber=="%s"`, escapeRSQL(identifier))
	serialPath := "/v1/computers-inventory?section=GENERAL&page-size=2&filter=" + url.QueryEscape(filter)
	count, id, name, err := searchInventoryForDevice(ctx, client, serialPath)
	if err != nil {
		return "", "", fmt.Errorf("searching by serial number: %w", err)
	}
	if count == 1 {
		return id, name, nil
	}
	if count > 1 {
		return "", "", fmt.Errorf("multiple devices match serial number %q (%d found)", identifier, count)
	}

	// 3. Try as name.
	filter = fmt.Sprintf(`general.name=="%s"`, escapeRSQL(identifier))
	namePath := "/v1/computers-inventory?section=GENERAL&page-size=5&filter=" + url.QueryEscape(filter)
	count, id, name, err = searchInventoryForDevice(ctx, client, namePath)
	if err != nil {
		return "", "", fmt.Errorf("searching by name: %w", err)
	}
	if count == 1 {
		return id, name, nil
	}
	if count > 1 {
		return "", "", fmt.Errorf("multiple devices match name %q (%d found)", identifier, count)
	}

	return "", "", fmt.Errorf("no device found matching %q", identifier)
}

// tryDeviceByID attempts to fetch a device directly by its Jamf ID.
// Returns id, name, error. A non-200 response is treated as a miss (returns error).
func tryDeviceByID(ctx context.Context, client registry.HTTPClient, id string) (string, string, error) {
	obj, err := fetchJSON(ctx, client, "/v1/computers-inventory-detail/"+id)
	if err != nil {
		return "", "", err
	}
	return extractField(obj, "id"), extractDeviceName(obj), nil
}

// searchInventoryForDevice executes a GET against the given inventory path
// (which should include RSQL filter params) and returns the match count,
// the first result's id and name, and any error.
func searchInventoryForDevice(ctx context.Context, client registry.HTTPClient, path string) (int, string, string, error) {
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return 0, "", "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return 0, "", "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return 0, "", "", err
	}

	var data struct {
		TotalCount int              `json:"totalCount"`
		Results    []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return 0, "", "", err
	}

	if data.TotalCount == 0 || len(data.Results) == 0 {
		return 0, "", "", nil
	}

	first := data.Results[0]
	id := extractField(first, "id")
	name := extractDeviceName(first)
	return data.TotalCount, id, name, nil
}

// extractDeviceName pulls the computer name from a computers-inventory object.
// The name lives at general.name in the API response.
func extractDeviceName(obj map[string]any) string {
	if general, ok := obj["general"].(map[string]any); ok {
		if name, ok := general["name"].(string); ok {
			return name
		}
	}
	return ""
}
