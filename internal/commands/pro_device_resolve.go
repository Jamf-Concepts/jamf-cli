// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/resolve"
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
	if !errors.Is(err, errNoDeviceWithID) {
		return "", "", fmt.Errorf("looking up %q as a device ID: %w", identifier, err)
	}

	// 2. Try as serial number.
	filter := fmt.Sprintf(`hardware.serialNumber=="%s"`, resolve.EscapeRSQL(identifier))
	serialPath := "/v4/computers-inventory?section=GENERAL&section=HARDWARE&page-size=2&filter=" + url.QueryEscape(filter)
	count, first, err := searchInventoryForDevice(ctx, client, serialPath)
	if err != nil {
		return "", "", fmt.Errorf("searching by serial number: %w", err)
	}
	// A result that reports no serial is accepted; one reporting another serial
	// is the filter matching some other device.
	if serial := extractNestedString(first, "hardware", "serialNumber"); count == 1 && (serial == "" || strings.EqualFold(serial, identifier)) {
		return extractField(first, "id"), extractDeviceName(first), nil
	}
	if count > 1 {
		return "", "", fmt.Errorf("multiple devices match serial number %q (%d found)", identifier, count)
	}

	// 3. Try as name.
	filter = fmt.Sprintf(`general.name=="%s"`, resolve.EscapeRSQL(identifier))
	namePath := "/v4/computers-inventory?section=GENERAL&page-size=5&filter=" + url.QueryEscape(filter)
	count, first, err = searchInventoryForDevice(ctx, client, namePath)
	if err != nil {
		return "", "", fmt.Errorf("searching by name: %w", err)
	}
	if name := extractDeviceName(first); count == 1 && strings.EqualFold(name, identifier) {
		return extractField(first, "id"), name, nil
	}
	if count > 1 {
		return "", "", fmt.Errorf("multiple devices match name %q (%d found)", identifier, count)
	}

	return "", "", fmt.Errorf("no device found matching %q", identifier)
}

// errNoDeviceWithID is the one ID-lookup failure that lets resolution move on
// to the serial and name searches.
var errNoDeviceWithID = errors.New("no device with that ID")

// tryDeviceByID attempts to fetch a device directly by its Jamf ID. A 404
// returns errNoDeviceWithID; any other failure is returned as is.
func tryDeviceByID(ctx context.Context, client registry.HTTPClient, id string) (string, string, error) {
	resp, err := client.Do(ctx, "GET", "/v4/computers-inventory-detail/"+url.PathEscape(id), nil)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, resp.Body)
		return "", "", errNoDeviceWithID
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return "", "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return "", "", err
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return "", "", fmt.Errorf("parsing device %s: %w", id, err)
	}
	return extractField(obj, "id"), extractDeviceName(obj), nil
}

// searchInventoryForDevice executes a GET against the given inventory path
// (which should include RSQL filter params) and returns the match count and
// the first result.
func searchInventoryForDevice(ctx context.Context, client registry.HTTPClient, path string) (int, map[string]any, error) {
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return 0, nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return 0, nil, err
	}

	var data struct {
		TotalCount int              `json:"totalCount"`
		Results    []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return 0, nil, err
	}

	if data.TotalCount == 0 || len(data.Results) == 0 {
		return 0, nil, nil
	}
	return data.TotalCount, data.Results[0], nil
}

// extractNestedString reads obj[section][key] as a string, or "".
func extractNestedString(obj map[string]any, section, key string) string {
	inner, _ := obj[section].(map[string]any)
	v, _ := inner[key].(string)
	return v
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
