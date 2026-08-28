// Copyright 2026, Jamf Software LLC

package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
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
// listPath is the full gateway path, /{service}/v{n}/<collection>. There is no
// /api segment and no tenant segment: the GA gateway mounts each namespace at
// the root, and the scope travels as an X-Tenant-Id or X-Environment-Id header
// set by the transport. Items are matched by checking
// "name", "title", and "displayName" properties in that order. The ID is read
// from "id" (and falls back to "blueprintId", "groupId", "deviceId" for
// resources that use a non-standard ID field).
//
// Returns an error when multiple items share the name (ambiguous match within
// a single page). Use ResolveIDByNameFiltered to narrow the lookup first.
func ResolveIDByName(ctx context.Context, client *jamfplatform.Client, listPath string, name string) (string, error) {
	return ResolveIDByNameFiltered(ctx, client, listPath, name, "")
}

// ResolveIDByNameFiltered is like ResolveIDByName but narrows the server-side
// results with an RSQL filter expression appended as ?filter=<expr> before the
// name walk begins. Pass an empty string for no additional filtering.
//
// Example: ResolveIDByNameFiltered(ctx, c, path, "My Group", `deviceType=="COMPUTER"`)
func ResolveIDByNameFiltered(ctx context.Context, client *jamfplatform.Client, listPath string, name string, filter string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty name")
	}
	if filter != "" {
		q := url.Values{}
		q.Set("filter", filter)
		sep := "?"
		if u, err := url.Parse(listPath); err == nil && u.RawQuery != "" {
			sep = "&"
		}
		listPath = listPath + sep + q.Encode()
	}

	const pageSize = 100

	// First request: no pagination params. Some endpoints return 500 when sent
	// page/page-size params they don't support (e.g. compliance-benchmarks).
	// We detect whether the endpoint is paginated from the response shape and
	// only add params for subsequent pages when totalCount signals more items.
	var raw json.RawMessage
	if err := client.Transport().DoExpect(ctx, http.MethodGet, listPath, nil, http.StatusOK, &raw); err != nil {
		return "", fmt.Errorf("listing %s: %w", listPath, err)
	}
	items, paged := extractItems(raw)
	if id, err := firstMatch(items, name); err != nil {
		return "", err
	} else if id != "" {
		return id, nil
	}

	// Paginate only if the first response signalled more pages.
	if paged && len(items) == pageSize {
		for page := 1; ; page++ {
			q := url.Values{}
			q.Set("page", strconv.Itoa(page))
			q.Set("page-size", strconv.Itoa(pageSize))
			sep := "?"
			if u, err := url.Parse(listPath); err == nil && u.RawQuery != "" {
				sep = "&"
			}
			endpoint := listPath + sep + q.Encode()

			var pageRaw json.RawMessage
			if err := client.Transport().DoExpect(ctx, http.MethodGet, endpoint, nil, http.StatusOK, &pageRaw); err != nil {
				return "", fmt.Errorf("listing %s: %w", listPath, err)
			}
			pageItems, _ := extractItems(pageRaw)
			if id, err := firstMatch(pageItems, name); err != nil {
				return "", err
			} else if id != "" {
				return id, nil
			}
			if len(pageItems) < pageSize {
				break
			}
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
	// Object envelope: pick the first array property in sorted key order for
	// deterministic behaviour when multiple array fields exist (e.g. "rules"
	// + "sources").
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, false
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if items, ok := obj[k].([]any); ok {
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

// firstMatch scans items for entries whose name matches. Returns the ID of the
// first match, or an error when multiple items share the name within this page
// (ambiguous). Returns ("", nil) when no match is found.
func firstMatch(items []map[string]any, name string) (string, error) {
	var matched []string
	for _, item := range items {
		if matchesName(item, name) {
			if id := extractID(item); id != "" {
				matched = append(matched, id)
			}
		}
	}
	switch len(matched) {
	case 0:
		return "", nil
	case 1:
		return matched[0], nil
	default:
		return "", fmt.Errorf("ambiguous match: %d items named %q; pass the positional ID to disambiguate", len(matched), name)
	}
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
