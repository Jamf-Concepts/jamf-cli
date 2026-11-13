// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

type threatPreventionVersionsTestClient struct {
	*stubProtectClientBase
	versions []jamfprotect.ThreatPreventionVersion
	err      error
}

func (c *threatPreventionVersionsTestClient) ListThreatPreventionVersions(_ context.Context) ([]jamfprotect.ThreatPreventionVersion, error) {
	return c.versions, c.err
}

func runProtectThreatPreventionVersionsList(t *testing.T, pc registry.ProtectClient) (*captureOutput, error) {
	t.Helper()
	out := &captureOutput{}
	cliCtx := &registry.CLIContext{Output: out, ProtectClient: pc}
	cmd := newProtectThreatPreventionVersionsListCmd(cliCtx)
	cmd.SetContext(context.Background())
	return out, cmd.RunE(cmd, nil)
}

func TestProtectThreatPreventionVersionsSubcommands(t *testing.T) {
	protect := findProtectCmd(t)
	versions := findSubcommand(protect, "threat-prevention-versions")
	if versions == nil {
		t.Fatal("threat-prevention-versions subcommand not found")
		return
	}
	if findSubcommand(versions, "list") == nil {
		t.Error("expected threat-prevention-versions subcommand \"list\"")
	}
}

func TestProtectThreatPreventionVersionsList_Output(t *testing.T) {
	pc := &threatPreventionVersionsTestClient{
		stubProtectClientBase: &stubProtectClientBase{},
		versions: []jamfprotect.ThreatPreventionVersion{
			{ID: "1002", Created: "2026-11-10T09:00:00Z"},
			{ID: "1001", Created: "2026-11-03T09:00:00Z"},
		},
	}
	out, err := runProtectThreatPreventionVersionsList(t, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var rows []map[string]any
	if err := json.Unmarshal(out.rawData, &rows); err != nil {
		t.Fatalf("output is not a JSON list: %v\n%s", err, out.rawData)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0]["id"] != "1002" || rows[0]["created"] != "2026-11-10T09:00:00Z" {
		t.Errorf("first row = %v, want id 1002 created 2026-11-10T09:00:00Z", rows[0])
	}
}

func TestProtectThreatPreventionVersionsList_Error(t *testing.T) {
	want := errors.New("boom")
	pc := &threatPreventionVersionsTestClient{
		stubProtectClientBase: &stubProtectClientBase{},
		err:                   want,
	}
	if _, err := runProtectThreatPreventionVersionsList(t, pc); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}
