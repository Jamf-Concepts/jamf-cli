// Copyright 2026, Jamf Software LLC

package commands

import (
	"testing"

	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

func TestProtectAnalyticsOverridesSubcommands(t *testing.T) {
	protect := findProtectCmd(t)
	analytics := findSubcommand(protect, "analytics")
	if analytics == nil {
		t.Fatal("analytics subcommand not found")
	}
	overrides := findSubcommand(analytics, "overrides")
	if overrides == nil {
		t.Fatal("analytics overrides subcommand not found")
	}

	for _, name := range []string{"list", "get", "set", "clear", "export", "apply"} {
		t.Run(name, func(t *testing.T) {
			if findSubcommand(overrides, name) == nil {
				t.Errorf("expected overrides subcommand %q", name)
			}
		})
	}
}

func TestHasOverride(t *testing.T) {
	tests := []struct {
		name     string
		analytic jamfprotect.Analytic
		want     bool
	}{
		{"pristine", jamfprotect.Analytic{Severity: "High"}, false},
		{"severity only", jamfprotect.Analytic{Severity: "High", TenantSeverity: "Low"}, true},
		{"actions only", jamfprotect.Analytic{
			Severity:      "High",
			TenantActions: []jamfprotect.AnalyticAction{{Name: "Report"}},
		}, true},
		{"both", jamfprotect.Analytic{
			Severity:       "High",
			TenantSeverity: "Low",
			TenantActions:  []jamfprotect.AnalyticAction{{Name: "Report"}},
		}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasOverride(tc.analytic); got != tc.want {
				t.Errorf("hasOverride() = %v, want %v", got, tc.want)
			}
		})
	}
}

// The baseline severity must survive into the export unchanged, and only the
// tenant half may appear — an export carrying Jamf's own severity would, on
// apply, pin the analytic to today's baseline and mask future Jamf changes.
func TestOverrideFromAnalyticCarriesOnlyTenantHalf(t *testing.T) {
	a := jamfprotect.Analytic{
		Name:            "BlazingKeylogger",
		Severity:        "High",
		TenantSeverity:  "Low",
		AnalyticActions: []jamfprotect.AnalyticAction{{Name: "Report", Parameters: "{}"}},
		TenantActions:   []jamfprotect.AnalyticAction{{Name: "Alert", Parameters: `{"notify":true}`}},
	}

	got := overrideFromAnalytic(a)

	if got.Analytic != "BlazingKeylogger" {
		t.Errorf("Analytic = %q, want BlazingKeylogger", got.Analytic)
	}
	if got.Severity != "Low" {
		t.Errorf("Severity = %q, want Low (the tenant value, not the baseline)", got.Severity)
	}
	if len(got.Actions) != 1 || got.Actions[0].Name != "Alert" {
		t.Fatalf("Actions = %+v, want the single tenant action Alert", got.Actions)
	}
	if got.Actions[0].Parameters != `{"notify":true}` {
		t.Errorf("Parameters = %q, want the tenant parameters", got.Actions[0].Parameters)
	}
}

func TestOverrideFromAnalyticPristineIsEmpty(t *testing.T) {
	got := overrideFromAnalytic(jamfprotect.Analytic{
		Name:            "Pristine",
		Severity:        "High",
		AnalyticActions: []jamfprotect.AnalyticAction{{Name: "Report", Parameters: "{}"}},
	})
	if got.Severity != "" || len(got.Actions) != 0 {
		t.Errorf("pristine analytic exported as %+v, want empty override", got)
	}
}

// flattenOverride exists because `analytics list` reports Jamf's baseline
// severity even when a tenant override is in force. The flattened view must
// show the effective value, or the table repeats that error.
func TestFlattenOverrideShowsEffectiveValues(t *testing.T) {
	row := flattenOverride(jamfprotect.Analytic{
		Name:            "BlazingKeylogger",
		Severity:        "High",
		TenantSeverity:  "Low",
		AnalyticActions: []jamfprotect.AnalyticAction{{Name: "Report"}},
		TenantActions:   []jamfprotect.AnalyticAction{{Name: "Alert"}},
	})

	if row["baselineSeverity"] != "High" {
		t.Errorf("baselineSeverity = %v, want High", row["baselineSeverity"])
	}
	if row["severity"] != "Low" {
		t.Errorf("severity (effective) = %v, want Low", row["severity"])
	}
	if row["actions"] != "Alert" {
		t.Errorf("actions (effective) = %v, want Alert", row["actions"])
	}
}

func TestFlattenOverrideFallsBackToBaseline(t *testing.T) {
	row := flattenOverride(jamfprotect.Analytic{
		Name:            "Pristine",
		Severity:        "High",
		AnalyticActions: []jamfprotect.AnalyticAction{{Name: "Report"}},
	})
	if row["severity"] != "High" {
		t.Errorf("severity = %v, want the baseline High when no override", row["severity"])
	}
	if row["actions"] != "Report" {
		t.Errorf("actions = %v, want the baseline Report when no override", row["actions"])
	}
	if row["tenantSeverity"] != "" {
		t.Errorf("tenantSeverity = %v, want empty", row["tenantSeverity"])
	}
}

func TestParseActionFlag(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantName   string
		wantParams string
		wantErr    bool
	}{
		{"bare name defaults parameters", "Report", "Report", "{}", false},
		{"name with json", `Alert={"notify":true}`, "Alert", `{"notify":true}`, false},
		{"empty parameters defaults", "Alert=", "Alert", "{}", false},
		{"surrounding space trimmed", "  Report  ", "Report", "{}", false},
		{"empty name rejected", "=x", "", "", true},
		{"empty string rejected", "", "", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseActionFlag(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseActionFlag(%q) = %+v, want error", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseActionFlag(%q) unexpected error: %v", tc.raw, err)
			}
			if got.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tc.wantName)
			}
			if got.Parameters != tc.wantParams {
				t.Errorf("Parameters = %q, want %q", got.Parameters, tc.wantParams)
			}
		})
	}
}

// 'overrides set' must reject a no-op invocation and the set-and-clear
// contradictions before spending an API call.
func TestValidateOverrideSetFlags(t *testing.T) {
	tests := []struct {
		name          string
		severity      string
		actions       []string
		clearSeverity bool
		clearActions  bool
		wantErr       bool
	}{
		{name: "no flags is a no-op", wantErr: true},
		{name: "severity alone", severity: "Low"},
		{name: "action alone", actions: []string{"Report"}},
		{name: "clear severity alone", clearSeverity: true},
		{name: "clear actions alone", clearActions: true},
		{name: "severity plus clear actions", severity: "Low", clearActions: true},
		{name: "severity conflicts with clear-severity", severity: "Low", clearSeverity: true, wantErr: true},
		{name: "action conflicts with clear-actions", actions: []string{"Report"}, clearActions: true, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateOverrideSetFlags(tc.severity, tc.actions, tc.clearSeverity, tc.clearActions)
			if tc.wantErr && err == nil {
				t.Error("expected an error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
