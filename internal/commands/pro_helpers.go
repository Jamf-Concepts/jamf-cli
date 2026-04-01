package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/xmlconv"
)

// FetchJSON performs a GET request and returns the parsed JSON object.
// Exported version of the overview.go fetchJSON helper.
func FetchJSON(ctx context.Context, client registry.HTTPClient, path string) (map[string]any, error) {
	return fetchJSON(ctx, client, path)
}

// FetchAllPaginated fetches all items from a modern API endpoint.
// It auto-detects the response format:
//   - Paginated: `{"totalCount": N, "results": [...]}` — fetches all pages
//   - Array: `[{...}, {...}]` — returns the full array directly
//
// Some Jamf Pro endpoints (e.g. /v1/sites, /v1/computer-groups,
// /v2/patch-software-title-configurations) return plain arrays even when
// pagination params are provided. This function handles both transparently.
func FetchAllPaginated(ctx context.Context, client registry.HTTPClient, basePath string, pageSize int) ([]map[string]any, error) {
	if pageSize <= 0 {
		pageSize = 100
	}

	var all []map[string]any
	page := 0

	for {
		sep := "?"
		if strings.Contains(basePath, "?") {
			sep = "&"
		}
		path := fmt.Sprintf("%s%spage=%d&page-size=%d", basePath, sep, page, pageSize)

		resp, err := client.Do(ctx, "GET", path, nil)
		if err != nil {
			return all, fmt.Errorf("fetching page %d: %w", page, err)
		}

		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return all, fmt.Errorf("reading page %d body: %w", page, err)
		}

		if resp.StatusCode != http.StatusOK {
			return all, fmt.Errorf("fetching page %d: HTTP %d", page, resp.StatusCode)
		}

		// Auto-detect: try array first, then paginated object.
		var arr []any
		if json.Unmarshal(body, &arr) == nil {
			// Plain array endpoint — return everything, no pagination.
			for _, item := range arr {
				if m, ok := item.(map[string]any); ok {
					all = append(all, m)
				}
			}
			return all, nil
		}

		var data map[string]any
		if err := json.Unmarshal(body, &data); err != nil {
			return all, fmt.Errorf("parsing page %d: %w", page, err)
		}

		results, _ := data["results"].([]any)
		for _, r := range results {
			if m, ok := r.(map[string]any); ok {
				all = append(all, m)
			}
		}

		totalCount, _ := data["totalCount"].(float64)
		if len(all) >= int(totalCount) || len(results) == 0 {
			break
		}
		page++
	}

	return all, nil
}

// FetchClassicList performs a GET on a Classic API list endpoint and returns
// the unwrapped array. Classic API returns XML; JSON is handled as a fallback.
func FetchClassicList(ctx context.Context, client registry.HTTPClient, path, wrapperKey string) ([]any, error) {
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if xmlconv.IsXML(body) {
		items, err := xmlconv.ExtractListItems(body)
		if err != nil {
			return nil, err
		}
		// Convert []map[string]any to []any for interface compatibility.
		result := make([]any, len(items))
		for i, item := range items {
			result[i] = item
		}
		return result, nil
	}

	// JSON fallback
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, err
	}
	inner, ok := wrapper[wrapperKey]
	if !ok {
		return nil, nil
	}
	var arr []any
	if err := json.Unmarshal(inner, &arr); err != nil {
		return nil, err
	}
	return arr, nil
}

// FetchResult holds either a result or an error from a parallel fetch.
type FetchResult[R any] struct {
	Value R
	Err   error
}

// BoundedParallelFetch runs fn for each item with bounded concurrency.
// Returns all results (in input order) and any errors collected.
func BoundedParallelFetch[T any, R any](ctx context.Context, items []T, concurrency int, fn func(context.Context, T) (R, error)) ([]R, []error) {
	if concurrency <= 0 {
		concurrency = 10
	}

	results := make([]FetchResult[R], len(items))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, item := range items {
		wg.Add(1)
		go func(idx int, it T) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[idx] = FetchResult[R]{Err: ctx.Err()}
				return
			}

			val, err := fn(ctx, it)
			results[idx] = FetchResult[R]{Value: val, Err: err}
		}(i, item)
	}

	wg.Wait()

	values := make([]R, 0, len(items))
	var errs []error
	for _, r := range results {
		if r.Err != nil {
			errs = append(errs, r.Err)
		} else {
			values = append(values, r.Value)
		}
	}
	return values, errs
}

// extractField extracts a string or numeric field from a JSON object by key.
func extractField(m map[string]any, key string) string {
	switch v := m[key].(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%d", int(v))
	default:
		return ""
	}
}

// extractDefinitionID extracts the "definitionId" field from a JSON object.
func extractDefinitionID(m map[string]any) string {
	return extractField(m, "definitionId")
}

// slugifyRegex matches non-alphanumeric characters for replacement.
var slugifyRegex = regexp.MustCompile(`[^a-z0-9]+`)

// SlugifyName converts a display name to a filesystem-safe slug.
// "Deploy Chrome - v1.2" → "deploy-chrome-v1-2"
func SlugifyName(name string) string {
	lower := strings.ToLower(name)

	// Replace non-alphanumeric with hyphens
	slug := slugifyRegex.ReplaceAllString(lower, "-")

	// Trim leading/trailing hyphens
	slug = strings.Trim(slug, "-")

	if slug == "" {
		return "unnamed"
	}
	return slug
}

// StripServerFields removes server-generated fields from a JSON object
// for clean diffing. Removes id, timestamps, and other server-set fields.
func StripServerFields(obj map[string]any) map[string]any {
	stripKeys := map[string]bool{
		"id":                 true,
		"href":               true,
		"createdDate":        true,
		"updatedDate":        true,
		"dateCreated":        true,
		"dateLastModified":   true,
		"lastModified":       true,
		"createdTimestamp":   true,
		"updatedTimestamp":   true,
		"date_created_epoch": true,
		"date_created_utc":   true,
	}

	result := make(map[string]any, len(obj))
	for k, v := range obj {
		if stripKeys[k] {
			continue
		}
		// Strip keys ending in common timestamp suffixes
		lower := strings.ToLower(k)
		if strings.HasSuffix(lower, "_epoch") || strings.HasSuffix(lower, "_utc") {
			continue
		}
		// Recursively strip nested objects and arrays
		switch val := v.(type) {
		case map[string]any:
			result[k] = StripServerFields(val)
		case []any:
			stripped := make([]any, len(val))
			for i, elem := range val {
				if m, ok := elem.(map[string]any); ok {
					stripped[i] = StripServerFields(m)
				} else {
					stripped[i] = elem
				}
			}
			result[k] = stripped
		default:
			result[k] = v
		}
	}
	return result
}

// DeduplicateSlug appends a numeric suffix if slug already exists in the set.
// Returns the unique slug and adds it to the set.
func DeduplicateSlug(slug string, seen map[string]bool) string {
	if !seen[slug] {
		seen[slug] = true
		return slug
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", slug, i)
		if !seen[candidate] {
			seen[candidate] = true
			return candidate
		}
	}
}
