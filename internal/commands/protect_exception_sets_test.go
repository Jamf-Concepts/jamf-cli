// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

func exceptionSetMock() *mockProtectClient {
	return &mockProtectClient{
		analytics: []jamfprotect.Analytic{
			// A Jamf-published analytic: the same uuid in every tenant.
			{UUID: "da360eb3-jamf", Name: "BlazingKeylogger", Jamf: true},
			// A custom analytic: this tenant's own uuid, different elsewhere.
			{UUID: "target-side-uuid", Name: "zz-cli-analytic-alpha"},
		},
	}
}

func strp(s string) *string { return &s }

// The export must carry the analytic's *name*. A uuid alone is portable only for
// Jamf-published analytics; for a custom one it names an analytic the target does
// not have, and the server refuses the whole set with "Action blocked due to
// dependencies on this resource" — an error naming neither the analytic nor the
// uuid nor the reason.
func TestExceptionSetToExport_CarriesAnalyticName(t *testing.T) {
	got := exceptionSetToExport(&jamfprotect.ExceptionSet{
		Name:        "zz-cli-exceptions",
		Description: "fixture",
		Exceptions: []jamfprotect.Exception{
			{
				Type:           "Path",
				Value:          "/usr/local/bin/thing",
				IgnoreActivity: "Analytics",
				Analytic:       &jamfprotect.AnalyticRef{Name: "zz-cli-analytic-alpha", UUID: "source-side-uuid"},
			},
			{
				Type:           "TeamId",
				Value:          "ABCDE12345",
				IgnoreActivity: "Telemetry",
				AppSigningInfo: &jamfprotect.AppSigningInfo{AppID: "com.example.app", TeamID: "ABCDE12345"},
			},
		},
	})

	if len(got.Exceptions) != 2 {
		t.Fatalf("got %d exceptions, want 2", len(got.Exceptions))
	}
	if got.Exceptions[0].Analytic != "zz-cli-analytic-alpha" {
		t.Errorf("Analytic = %q, want the analytic's name", got.Exceptions[0].Analytic)
	}
	// The uuid is still written so the document stays readable by an older CLI.
	if got.Exceptions[0].AnalyticUUID == nil || *got.Exceptions[0].AnalyticUUID != "source-side-uuid" {
		t.Errorf("AnalyticUUID = %v, want it retained alongside the name", got.Exceptions[0].AnalyticUUID)
	}
	// An exception with no analytic target must not gain one.
	if got.Exceptions[1].Analytic != "" || got.Exceptions[1].AnalyticUUID != nil {
		t.Errorf("exception 2 gained an analytic reference: %+v", got.Exceptions[1])
	}
	if got.Exceptions[1].AppSigningInfo == nil || got.Exceptions[1].AppSigningInfo.TeamID != "ABCDE12345" {
		t.Error("AppSigningInfo must survive the projection")
	}
	// Non-nil empty rather than nil, so a set with no ES exceptions still sends [].
	if got.EsExceptions == nil {
		t.Error("EsExceptions must marshal as [] rather than null")
	}
}

// This is the whole point: the name rebinds to the *target's* uuid, so the
// exception suppresses the right analytic after a restore into another tenant.
func TestExceptionSetExportToInput_RebindsNameToTargetUUID(t *testing.T) {
	e := exceptionSetExport{
		Name: "zz-cli-exceptions",
		Exceptions: []exceptionExport{{
			Type:           "Path",
			Value:          strp("/usr/local/bin/thing"),
			IgnoreActivity: "Analytics",
			Analytic:       "zz-cli-analytic-alpha",
			AnalyticUUID:   strp("source-side-uuid"),
		}},
	}

	got, err := exceptionSetExportToInput(context.Background(), e, protect.NewResolver(exceptionSetMock()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Exceptions[0].AnalyticUUID == nil {
		t.Fatal("AnalyticUUID is nil")
	}
	if *got.Exceptions[0].AnalyticUUID != "target-side-uuid" {
		t.Errorf("AnalyticUUID = %q, want the target tenant's uuid — the name must win over the stale uuid",
			*got.Exceptions[0].AnalyticUUID)
	}
}

// A named analytic the target does not have must fail with something the operator
// can act on. Passing the source uuid through instead gets "Action blocked due to
// dependencies on this resource" from the server, which names nothing.
func TestExceptionSetExportToInput_UnresolvableAnalyticFails(t *testing.T) {
	e := exceptionSetExport{
		Name:       "zz-cli-exceptions",
		Exceptions: []exceptionExport{{Type: "Path", Analytic: "not-in-this-tenant"}},
	}

	_, err := exceptionSetExportToInput(context.Background(), e, protect.NewResolver(exceptionSetMock()))
	if err == nil {
		t.Fatal("expected an error naming the analytic, not the server's opaque dependency message")
	}
	for _, want := range []string{"not-in-this-tenant", "zz-cli-exceptions"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should name %q", err, want)
		}
	}
}

// Documents written before the name field existed carry only a uuid. Those keep
// working: Jamf-published uuids are stable across tenants, which is the case that
// was never broken.
func TestExceptionSetExportToInput_LegacyUUIDOnlyPassesThrough(t *testing.T) {
	e := exceptionSetExport{
		Name:       "legacy",
		Exceptions: []exceptionExport{{Type: "Path", AnalyticUUID: strp("da360eb3-jamf")}},
	}

	got, err := exceptionSetExportToInput(context.Background(), e, protect.NewResolver(exceptionSetMock()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Exceptions[0].AnalyticUUID == nil || *got.Exceptions[0].AnalyticUUID != "da360eb3-jamf" {
		t.Errorf("AnalyticUUID = %v, want the document's own uuid", got.Exceptions[0].AnalyticUUID)
	}
}

// The document keys are a field-for-field mirror of the SDK input types, so a
// file written by the previous version decodes without losing its exceptions.
// yaml.v3 matches keys case-sensitively against the lowercased Go field name, so
// this is the check that the mirror is exact rather than approximately right.
func TestExceptionSetExportDecodesPreviousYAMLShape(t *testing.T) {
	// Exactly what yaml.Marshal(jamfprotect.ExceptionSetInput{...}) produced.
	legacy := []byte(`name: legacy set
description: written by the previous version
exceptions:
    - type: Path
      value: /usr/local/bin/thing
      appsigninginfo: null
      ignoreactivity: Analytics
      analyticuuid: da360eb3-jamf
      analytictypes: []
esexceptions: []
`)

	var got exceptionSetExport
	if err := unmarshalInput(legacy, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "legacy set" {
		t.Errorf("Name = %q", got.Name)
	}
	if len(got.Exceptions) != 1 {
		t.Fatalf("got %d exceptions, want 1 — the key casing no longer matches", len(got.Exceptions))
	}
	ex := got.Exceptions[0]
	if ex.Type != "Path" {
		t.Errorf("Type = %q", ex.Type)
	}
	if ex.Value == nil || *ex.Value != "/usr/local/bin/thing" {
		t.Errorf("Value = %v", ex.Value)
	}
	if ex.IgnoreActivity != "Analytics" {
		t.Errorf("IgnoreActivity = %q, want the legacy key to still bind", ex.IgnoreActivity)
	}
	if ex.AnalyticUUID == nil || *ex.AnalyticUUID != "da360eb3-jamf" {
		t.Errorf("AnalyticUUID = %v, want the legacy key to still bind", ex.AnalyticUUID)
	}
}

// The SDK input type carried no json tags on its top-level fields, so JSON
// documents written by the previous version used "Name"/"Exceptions".
// encoding/json matches keys case-insensitively, so they still apply — this is
// the assertion that says so rather than assuming it.
func TestExceptionSetExportDecodesPreviousJSONShape(t *testing.T) {
	legacy := []byte(`{
  "Name": "legacy set",
  "Description": "written by the previous version",
  "Exceptions": [
    {
      "type": "Path",
      "value": "/tmp/thing",
      "ignoreActivity": "Analytics",
      "analyticUuid": "da360eb3-jamf",
      "analyticTypes": ["GPFSEvent"]
    }
  ],
  "EsExceptions": []
}`)

	var got exceptionSetExport
	if err := unmarshalInput(legacy, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "legacy set" {
		t.Errorf("Name = %q, want the PascalCase key to still bind", got.Name)
	}
	if len(got.Exceptions) != 1 {
		t.Fatalf("got %d exceptions, want 1", len(got.Exceptions))
	}
	if got.Exceptions[0].AnalyticUUID == nil || *got.Exceptions[0].AnalyticUUID != "da360eb3-jamf" {
		t.Errorf("AnalyticUUID = %v", got.Exceptions[0].AnalyticUUID)
	}
	if len(got.Exceptions[0].AnalyticTypes) != 1 {
		t.Errorf("AnalyticTypes = %v", got.Exceptions[0].AnalyticTypes)
	}
}
