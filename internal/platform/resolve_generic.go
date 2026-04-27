// Copyright 2026, Jamf Software LLC

package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
)

// ResolveIDByName finds a resource ID by its human-readable name on a Platform
// list endpoint. Walks pages when the response is paginated. Generated commands
// call this when the user supplies --name instead of a positional ID.
//
// The list response can take several shapes:
//   - {"results": [...], "totalCount": N}        — paginated (blueprints, devices)
//   - {"<resource>": [...]}                       — non-paginated single-array (baselines)
//   - [...]                                       — bare array
//
// listPath is the full path including /api/{service}/v{n}/tenant/{tenantId}/<collection>
// with {tenantId} pre-substituted by the caller. Items are matched by checking
// "name", "title", and "displayName" properties in that order. The ID is read
// from "id" (and falls back to "blueprintId", "groupId", "deviceId" for
// resources that use a non-standard ID field).
func ResolveIDByName(ctx context.Context, client *jamfplatform.Client, listPath string, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty name")
	}

	const pageSize = 100
	for page := 0; ; page++ {
		endpoint := listPath
		// Add pagination query params; harmless on endpoints that ignore them.
		q := url.Values{}
		q.Set("page", strconv.Itoa(page))
		q.Set("page-size", strconv.Itoa(pageSize))
		sep := "?"
		if u, err := url.Parse(endpoint); err == nil && u.RawQuery != "" {
			sep = "&"
		}
		endpoint += sep + q.Encode()

		var raw json.RawMessage
		if err := client.Transport().DoExpect(ctx, http.MethodGet, endpoint, nil, http.StatusOK, &raw); err != nil {
			return "", fmt.Errorf("listing %s: %w", listPath, err)
		}

		items, more := extractItems(raw)
		for _, item := range items {
			if matchesName(item, name) {
				if id := extractID(item); id != "" {
					return id, nil
				}
			}
		}
		if !more || len(items) < pageSize {
			break
		}
	}
	return "", fmt.Errorf("%w: no item with name %q", ErrNotFound, name)
}

// extractItems pulls the array of items out of a list response envelope.
// Returns the items and a hint about whether more pages might follow.
func extractItems(raw json.RawMessage) ([]map[string]any, bool) {
	// Try bare array first.
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, false
	}
	// Object envelope: pick the single array property.
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, false
	}
	for _, v := range obj {
		if items, ok := v.([]any); ok {
			out := make([]map[string]any, 0, len(items))
			for _, it := range items {
				if m, ok := it.(map[string]any); ok {
					out = append(out, m)
				}
			}
			// If the envelope also carries totalCount, more pages may follow.
			_, paged := obj["totalCount"]
			return out, paged
		}
	}
	return nil, false
}

func matchesName(item map[string]any, name string) bool {
	for _, key := range []string{"name", "title", "displayName"} {
		if v, ok := item[key].(string); ok && v == name {
			return true
		}
	}
	return false
}

func extractID(item map[string]any) string {
	for _, key := range []string{"id", "blueprintId", "groupId", "deviceId", "benchmarkId", "baselineId"} {
		if v, ok := item[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}
