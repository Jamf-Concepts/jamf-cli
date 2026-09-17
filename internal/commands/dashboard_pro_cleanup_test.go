// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// When a policy detail fetch fails, its packages/scripts never enter the
// reference sets, so every package/script it used looks unused. The report
// must not publish that under-referenced tally as fact: the collector records
// the skip, the reliability predicate flips, and the derived rows render as
// "not available" instead of a misleading number. A warning naming the skipped
// policy reaches stderr so the gap is visible rather than silent.
func TestCollectCleanupAnalysis_WithholdsUsageWhenAPolicyDetailFails(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/policies": {200, `{"policy": [{"id": 1}, {"id": 2}]}`},
			"/JSSResource/policies/id/1": {200, `{"policy": {
				"general": {"enabled": true},
				"scope": {"all_computers": true},
				"package_configuration": {"packages": [{"name": "PkgA"}]},
				"scripts": {"script": [{"name": "ScriptA"}]}
			}}`},
			// Policy 2's detail is unreadable — the failure the test is about.
			"/JSSResource/policies/id/2":            {500, ``},
			"/JSSResource/osxconfigurationprofiles": {200, `{"configuration_profile": []}`},
			"/JSSResource/packages":                 {200, `{"package": [{"name": "PkgA"}, {"name": "PkgB"}]}`},
			"/JSSResource/scripts":                  {200, `{"script": [{"name": "ScriptA"}, {"name": "ScriptB"}]}`},
		},
	}

	// Capture stderr so the warning can be asserted.
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = origStderr }()

	result, err := collectCleanupAnalysis(context.Background(), client)

	_ = w.Close()
	os.Stderr = origStderr
	var stderrBuf bytes.Buffer
	_, _ = stderrBuf.ReadFrom(r)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PoliciesSkipped != 1 {
		t.Errorf("PoliciesSkipped = %d, want 1", result.PoliciesSkipped)
	}
	if result.PackageScriptUsageReliable() {
		t.Error("PackageScriptUsageReliable() = true, want false when a policy detail was skipped")
	}
	// The derived counts must not feed the headline total when unreliable.
	if got, want := result.Total(), result.DisabledPolicies+result.UnscopedPolicies+result.UnscopedProfiles; got != want {
		t.Errorf("Total() = %d, want %d (derived counts excluded when unreliable)", got, want)
	}
	if !strings.Contains(stderrBuf.String(), "policy 2") {
		t.Errorf("stderr warning must name the skipped policy, got: %q", stderrBuf.String())
	}

	// The rendered report withholds the numbers and says why.
	var htmlBuf bytes.Buffer
	data := &DashboardData{
		Title:       "Cleanup Reliability",
		GeneratedAt: time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC),
		CLIVersion:  "1.0.0",
		Cleanup:     result,
	}
	if err := renderDashboard(&htmlBuf, data); err != nil {
		t.Fatalf("renderDashboard error: %v", err)
	}
	html := htmlBuf.String()
	if !strings.Contains(html, "not available") {
		t.Error("HTML must render 'not available' for the derived rows when usage is unreliable")
	}
}

// The complementary case: every policy detail read, so the derived counts are
// trustworthy and render as numbers.
func TestCollectCleanupAnalysis_PublishesUsageWhenEveryPolicyRead(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/policies": {200, `{"policy": [{"id": 1}]}`},
			"/JSSResource/policies/id/1": {200, `{"policy": {
				"general": {"enabled": true},
				"scope": {"all_computers": true},
				"package_configuration": {"packages": [{"name": "PkgA"}]},
				"scripts": {"script": [{"name": "ScriptA"}]}
			}}`},
			"/JSSResource/osxconfigurationprofiles": {200, `{"configuration_profile": []}`},
			"/JSSResource/packages":                 {200, `{"package": [{"name": "PkgA"}, {"name": "PkgB"}]}`},
			"/JSSResource/scripts":                  {200, `{"script": [{"name": "ScriptA"}, {"name": "ScriptB"}]}`},
		},
	}

	result, err := collectCleanupAnalysis(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PoliciesSkipped != 0 {
		t.Errorf("PoliciesSkipped = %d, want 0", result.PoliciesSkipped)
	}
	if !result.PackageScriptUsageReliable() {
		t.Error("PackageScriptUsageReliable() = false, want true when every policy detail was read")
	}
	// PkgB and ScriptB are unreferenced.
	if result.UnusedPackages != 1 {
		t.Errorf("UnusedPackages = %d, want 1", result.UnusedPackages)
	}
	if result.UnusedScripts != 1 {
		t.Errorf("UnusedScripts = %d, want 1", result.UnusedScripts)
	}
}
