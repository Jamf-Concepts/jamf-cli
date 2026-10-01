// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

type insightsClient struct {
	registry.ProtectClient
	items   []jamfprotect.Insight
	updated []string
}

func (c *insightsClient) ListInsights(context.Context) ([]jamfprotect.Insight, error) {
	return c.items, nil
}

func (c *insightsClient) UpdateInsightStatus(_ context.Context, uuid string, enabled bool) (jamfprotect.Insight, error) {
	c.updated = append(c.updated, uuid)
	return jamfprotect.Insight{UUID: uuid, Enabled: enabled}, nil
}

func restoreInsights(t *testing.T, c registry.ProtectClient, doc string) (string, error) {
	t.Helper()
	for _, r := range protectResources() {
		if r.Name == "insights" {
			return r.Restore(context.Background(), c, nil, []byte(doc))
		}
	}
	t.Fatal("no insights resource")
	return "", nil
}

// Restoring insight state by label refuses a label two insights share rather
// than toggling whichever was listed last.
func TestProtectRestoreInsights_SharedLabelIsRefused(t *testing.T) {
	c := &insightsClient{items: []jamfprotect.Insight{
		{UUID: "u-1", Label: "FileVault", Enabled: true},
		{UUID: "u-2", Label: "FileVault", Enabled: true},
	}}
	_, err := restoreInsights(t, c, "enabled: []\ndisabled: [FileVault]\n")
	if err == nil || !strings.Contains(err.Error(), "u-1") || !strings.Contains(err.Error(), "u-2") {
		t.Errorf("restore of a shared label: err = %v; want a refusal naming u-1 and u-2", err)
	}
	if len(c.updated) != 0 {
		t.Errorf("updated %v; want nothing changed", c.updated)
	}
}

func TestProtectRestoreInsights_UniqueLabelIsUpdated(t *testing.T) {
	c := &insightsClient{items: []jamfprotect.Insight{
		{UUID: "u-1", Label: "FileVault", Enabled: true},
		{UUID: "u-2", Label: "Gatekeeper", Enabled: true},
	}}
	if _, err := restoreInsights(t, c, "enabled: []\ndisabled: [FileVault, Absent]\n"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if len(c.updated) != 1 || c.updated[0] != "u-1" {
		t.Errorf("updated %v; want [u-1]", c.updated)
	}
}
