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

// ResolveIDByNameInField is ResolveIDByName with one extra property consulted
// ahead of the standard three.
//
// Some resources carry their human-readable identifier under a field of their
// own: an SSO domain's is "domain", so a --name lookup against the domains
// collection matched nothing and reported "no item with name …" — indis-
// tinguishable from a typo, on a resource whose only other handle is an opaque
// integer ID. The extra field is named per resource by the generator's
// platformNameLookupFields table rather than added to defaultNameFields,
// because a global "domain" match would let a resource that happens to carry an
// unrelated domain property resolve on it.
func ResolveIDByNameInField(ctx context.Context, client *jamfplatform.Client, listPath, name, nameField string) (string, error) {
	return resolveIDByName(ctx, client, listPath, name, "", nameField)
}

// ResolveIDByNameFiltered is like ResolveIDByName but narrows the server-side
// results with an RSQL filter expression appended as ?filter=<expr> before the
// name walk begins. Pass an empty string for no additional filtering.
//
// Example: ResolveIDByNameFiltered(ctx, c, path, "My Group", `deviceType=="COMPUTER"`)
func ResolveIDByNameFiltered(ctx context.Context, client *jamfplatform.Client, listPath string, name string, filter string) (string, error) {
	return resolveIDByName(ctx, client, listPath, name, filter, "")
}

func resolveIDByName(ctx context.Context, client *jamfplatform.Client, listPath, name, filter, nameField string) (string, error) {
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
	// Matches accumulate across every page rather than being decided per page.
	// Deciding per page returned as soon as one page held a single match, so a
	// name repeated either side of a 100-item boundary read as unique and the
	// caller upserted the page-1 item with no ambiguity error — the ambiguity
	// check firing only when both copies happened to land on the same page.
	matched, nameless := collectMatches(nil, items, name, nameField)

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
			var pageNameless int
			matched, pageNameless = collectMatches(matched, pageItems, name, nameField)
			nameless += pageNameless
			if len(pageItems) < pageSize {
				break
			}
		}
	}

	switch {
	case len(matched) == 1:
		return matched[0], nil
	case len(matched) > 1:
		return "", fmt.Errorf("ambiguous match: %d items named %q; identify it by ID instead", len(matched), name)
	case nameless > 0:
		// The name matched and the list gave nothing to address it by. Distinct
		// from ErrNotFound on purpose: a caller that treats absence as "create
		// it" (apply does) would otherwise create a second copy of something
		// that already exists, every run, silently. Security Cloud's device
		// groups are the live case — the implicit "Default Group" is returned
		// with a name and no id — and a nameless-item report is what CLAUDE.md
		// records as the fix for the same gap on ZTNA's predefined-derived
		// apps, where a null name made --name read as a typo.
		return "", fmt.Errorf("found %d item(s) named %q in %s, but the list returns no ID for them; identify the item by ID instead", nameless, name, listPath)
	}

	return "", fmt.Errorf("%w: no item with name %q", ErrNotFound, name)
}

// collectMatches appends the IDs of items whose name matches to matched, and
// returns the number of matching items the list gave no usable ID for.
func collectMatches(matched []string, items []map[string]any, name, nameField string) ([]string, int) {
	nameless := 0
	for _, item := range items {
		if !matchesNameIn(item, name, nameField) {
			continue
		}
		if id := extractID(item); id != "" {
			matched = append(matched, id)
			continue
		}
		nameless++
	}
	return matched, nameless
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

// defaultNameFields are the keys a resource's human-readable name is found
// under across the platform surface.
var defaultNameFields = []string{"name", "title", "displayName"}

// extraNameField, when non-empty, is consulted in addition to
// defaultNameFields. Set per call by ResolveIDByNameInField for resources whose
// name lives somewhere else — an SSO domain's is "domain".
func matchesNameIn(item map[string]any, name, extraNameField string) bool {
	keys := defaultNameFields
	if extraNameField != "" {
		keys = append([]string{extraNameField}, defaultNameFields...)
	}
	for _, key := range keys {
		if v, ok := item[key].(string); ok && v == name {
			return true
		}
	}
	return false
}

func extractID(item map[string]any) string {
	for _, key := range []string{"id", "blueprintId", "groupId", "deviceId", "benchmarkId", "baselineId"} {
		if v := idString(item[key]); v != "" {
			return v
		}
	}
	return ""
}

// idString renders an ID that may not be a string on the wire.
//
// Every platform resource but one keys on a UUID, so this only ever had to
// handle strings — and then an SSO domain's ID turned out to be a small
// integer. The failure was silent and read as the wrong thing entirely: the
// name matched, extractID's type assertion did not, so firstMatch counted zero
// matches and --name reported `no item with name "example.com"`, which looks
// like a typo rather than an ID it could not read.
//
// json.Number keeps an integer exact where float64 would not; the encoder
// leaves numbers as float64 unless UseNumber is set, so both are handled. A
// non-integral float is formatted without a trailing ".0" and is not an ID
// anyway — nothing on this surface has one.
func idString(v any) string {
	switch id := v.(type) {
	case string:
		return id
	case json.Number:
		return id.String()
	case float64:
		return strconv.FormatFloat(id, 'f', -1, 64)
	default:
		return ""
	}
}
