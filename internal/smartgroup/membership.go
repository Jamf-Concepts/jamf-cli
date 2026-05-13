// Copyright 2026, Jamf Software LLC

package smartgroup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// HTTPDoer is the minimal HTTP interface required by membership/verify helpers.
// Matches registry.HTTPClient's Do signature so the same value can be passed.
type HTTPDoer interface {
	Do(ctx context.Context, method, url string, body io.Reader) (*http.Response, error)
}

// CountMembers calls GET /v2/computer-groups/smart-group-membership/{id} and
// returns the length of the members array.
func CountMembers(ctx context.Context, client HTTPDoer, groupID string) (int, error) {
	url := fmt.Sprintf("/v2/computer-groups/smart-group-membership/%s", groupID)
	resp, err := client.Do(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("smart-group membership: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("smart-group membership: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		Members []int `json:"members"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, fmt.Errorf("smart-group membership: decode: %w", err)
	}
	return len(out.Members), nil
}
