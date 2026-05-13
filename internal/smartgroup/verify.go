// Copyright 2026, Jamf Software LLC

package smartgroup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
)

// VerifyOutcome is the per-template result of a verify-templates pass.
type VerifyOutcome int

const (
	VerifyOK VerifyOutcome = iota
	VerifyZeroMatch
	VerifyError
)

func (o VerifyOutcome) String() string {
	switch o {
	case VerifyOK:
		return "OK"
	case VerifyZeroMatch:
		return "ZERO_MATCH"
	case VerifyError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// VerifyResult captures one template's verification outcome.
type VerifyResult struct {
	Slug        string
	Outcome     VerifyOutcome
	MemberCount int
	Error       string
}

// defaultVerifyOpts supplies sensible required-param values for verification.
// Update when adding new required-param templates.
var defaultVerifyOpts = map[string]map[string]any{
	"updates/os-version-below":       {"below-version": "15.0"},
	"updates/major-version-behind":   {"major-below": 15},
	"lifecycle/jamf-binary-outdated": {"below-version": "11.0.0"},
}

// RunOneVerification creates a temporary smart group from the template,
// recalculates membership, captures the count, and (if cleanup) deletes the
// temporary group.
func RunOneVerification(ctx context.Context, client HTTPDoer, tmpl Template, cleanup bool) VerifyResult {
	opts, err := tmpl.ResolveOpts(defaultVerifyOpts[tmpl.Slug])
	if err != nil {
		return VerifyResult{Slug: tmpl.Slug, Outcome: VerifyError, Error: fmt.Sprintf("ResolveOpts: %v", err)}
	}
	req, err := tmpl.Build(opts)
	if err != nil {
		return VerifyResult{Slug: tmpl.Slug, Outcome: VerifyError, Error: fmt.Sprintf("Build: %v", err)}
	}
	req.Name = fmt.Sprintf("__verify_%s_%06d", sanitizeSlug(tmpl.Slug), rand.Intn(1000000))

	id, err := createTempGroup(ctx, client, req)
	if err != nil {
		return VerifyResult{Slug: tmpl.Slug, Outcome: VerifyError, Error: err.Error()}
	}

	_ = recalcGroup(ctx, client, id) // recalc failure is non-fatal

	count, err := CountMembers(ctx, client, id)
	if err != nil {
		if cleanup {
			_ = deleteGroup(ctx, client, id)
		}
		return VerifyResult{Slug: tmpl.Slug, Outcome: VerifyError, Error: err.Error()}
	}

	if cleanup {
		_ = deleteGroup(ctx, client, id)
	}

	outcome := VerifyOK
	if count == 0 {
		outcome = VerifyZeroMatch
	}
	return VerifyResult{Slug: tmpl.Slug, Outcome: outcome, MemberCount: count}
}

func sanitizeSlug(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
			out = append(out, c)
		case c >= '0' && c <= '9':
			out = append(out, c)
		case c == '-' || c == '_':
			out = append(out, c)
		case c == '/':
			out = append(out, '_')
		}
	}
	return string(out)
}

func createTempGroup(ctx context.Context, client HTTPDoer, req SmartGroupRequest) (string, error) {
	body, _ := json.Marshal(req)
	resp, err := client.Do(ctx, http.MethodPost, "/v2/computer-groups/smart-groups", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(buf))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func recalcGroup(ctx context.Context, client HTTPDoer, id string) error {
	resp, err := client.Do(ctx, http.MethodPost, "/v1/smart-computer-groups/"+id+"/recalculate", nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("recalculate: HTTP %d", resp.StatusCode)
	}
	return nil
}

func deleteGroup(ctx context.Context, client HTTPDoer, id string) error {
	resp, err := client.Do(ctx, http.MethodDelete, "/v2/computer-groups/smart-groups/"+id, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}
