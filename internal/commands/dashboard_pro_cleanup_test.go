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

	ctx := context.Background()
	status := &collectStatus{}
	result, err := collectCleanupAnalysis(ctx, client,
		fetchPolicyDetails(ctx, client), fetchConfigProfileDetails(ctx, client), status)

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
	// Every figure the policy loop derives is withheld from the total, not only
	// the unused-package/script pair: DisabledPolicies and UnscopedPolicies are
	// tallied inside the same loop body the error path skips, so they are a
	// floor as well.
	if result.PolicyCountsReliable() {
		t.Error("PolicyCountsReliable() = true, want false when a policy detail was skipped")
	}
	if got := result.Total(); got != result.UnscopedProfiles {
		t.Errorf("Total() = %d, want %d (only the profile count survives)", got, result.UnscopedProfiles)
	}
	if !strings.Contains(stderrBuf.String(), "policy 2") {
		t.Errorf("stderr warning must name the skipped policy, got: %q", stderrBuf.String())
	}
	// The skip has to reach the tally, or the run exits 0 with a banner that
	// says nothing while two rows read "not available".
	if got := status.failedSections(); len(got) != 1 || got[0] != sectionCleanup {
		t.Errorf("failedSections() = %v, want [%q]", got, sectionCleanup)
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

	ctx := context.Background()
	status := &collectStatus{}
	result, err := collectCleanupAnalysis(ctx, client,
		fetchPolicyDetails(ctx, client), fetchConfigProfileDetails(ctx, client), status)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PoliciesSkipped != 0 {
		t.Errorf("PoliciesSkipped = %d, want 0", result.PoliciesSkipped)
	}
	if !result.PackageScriptUsageReliable() {
		t.Error("PackageScriptUsageReliable() = false, want true when every policy detail was read")
	}
	if !result.Reliable() {
		t.Error("Reliable() = false, want true when every policy and profile detail was read")
	}
	if got := status.failedSections(); len(got) != 0 {
		t.Errorf("failedSections() = %v, want none on a clean run", got)
	}
	// PkgB and ScriptB are unreferenced.
	if result.UnusedPackages != 1 {
		t.Errorf("UnusedPackages = %d, want 1", result.UnusedPackages)
	}
	if result.UnusedScripts != 1 {
		t.Errorf("UnusedScripts = %d, want 1", result.UnusedScripts)
	}
}
