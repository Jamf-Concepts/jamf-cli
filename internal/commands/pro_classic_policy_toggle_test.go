// Copyright 2026, Jamf Software LLC

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

// policyToggleFixture serves three policies — two sharing a name — and
// accepts their PUTs.
func policyToggleFixture() *bulkMockClient {
	return &bulkMockClient{responses: map[string]overviewMockResponse{
		"GET /JSSResource/policies": {200, `{"policies":[
			{"id":1,"name":"Deploy Chrome"},
			{"id":2,"name":"Lab Reset"},
			{"id":3,"name":"lab reset"}
		]}`},
		"GET /JSSResource/policies/id/1": {200, policyDetailJSON(1, "Deploy Chrome", true, "Apps", "Lab Macs")},
		"GET /JSSResource/policies/id/2": {200, policyDetailJSON(2, "Lab Reset", false, "Lab", "Lab Macs")},
		"GET /JSSResource/policies/id/3": {200, policyDetailJSON(3, "lab reset", true, "Lab", "Other")},
		"PUT /JSSResource/policies/id/1": {201, `<policy><id>1</id></policy>`},
		"PUT /JSSResource/policies/id/2": {201, `<policy><id>2</id></policy>`},
		"PUT /JSSResource/policies/id/3": {201, `<policy><id>3</id></policy>`},
	}}
}

func runPolicyToggle(t *testing.T, mock *bulkMockClient, enable bool, args ...string) (string, error) {
	t.Helper()
	_, stderr, err := runCobraCmd(t, newClassicPolicyToggleCmd(newBulkCLIContext(mock), enable), args...)
	return stderr, err
}

// One named policy runs without --yes, as `update --set` does, and the body
// carries <general><enabled> and nothing else.
func TestPolicyToggle_SingleTargetWritesMinimalBody(t *testing.T) {
	mock := policyToggleFixture()
	if _, err := runPolicyToggle(t, mock, false, "1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	bodies := mock.bodiesMatching("PUT /JSSResource/policies/id/1")
	if len(bodies) != 1 {
		t.Fatalf("sent %d PUTs, want 1", len(bodies))
	}
	want := `<policy><general><enabled>false</enabled></general></policy>`
	if !strings.HasSuffix(bodies[0], want) {
		t.Errorf("body = %s, want it to end %s", bodies[0], want)
	}
}

// A unique exact name wins over a case-insensitive twin; a name only a fold
// matches, and that matches two, is refused rather than resolved to either.
func TestPolicyToggle_NameResolution(t *testing.T) {
	mock := policyToggleFixture()
	if _, err := runPolicyToggle(t, mock, true, "--name", "Lab Reset"); err != nil {
		t.Fatalf("exact name: %v", err)
	}
	if got := mock.callsMatching("PUT "); len(got) != 1 || !strings.Contains(got[0], "/id/2") {
		t.Errorf("exact name wrote %v, want policy 2 only", got)
	}

	mock = policyToggleFixture()
	stderr, err := runPolicyToggle(t, mock, true, "--name", "LAB RESET")
	if err == nil {
		t.Fatal("a name matching two policies case-insensitively was resolved")
	}
	if !strings.Contains(stderr, "names 2 policies") || mock.hasMutatingCall() {
		t.Errorf("stderr = %q, mutated = %v; want the collision named and nothing written", stderr, mock.hasMutatingCall())
	}
}

// A file previews without --yes, and an unknown line counts against the exit
// code once applied.
func TestPolicyToggle_FromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policies.txt")
	if err := os.WriteFile(path, []byte("# list\n1\nDeploy Chrome\nNo Such Policy\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	mock := policyToggleFixture()
	stderr, err := runPolicyToggle(t, mock, false, "--from-file", path)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if mock.hasMutatingCall() || !strings.Contains(stderr, "[dry-run]") {
		t.Errorf("preview wrote or did not say so: %q", stderr)
	}

	mock = policyToggleFixture()
	_, err = runPolicyToggle(t, mock, false, "--from-file", path, "--yes")
	if exitcode.CodeFrom(err) != exitcode.PartialFailure {
		t.Fatalf("err = %v, want a partial failure for the unknown line", err)
	}
	// "1" and "Deploy Chrome" are the same policy: one write.
	if got := mock.callsMatching("PUT "); len(got) != 1 {
		t.Errorf("wrote %v, want policy 1 once", got)
	}
}

// Filters select, preview without --yes, and skip a policy already in state.
func TestPolicyToggle_Filters(t *testing.T) {
	mock := policyToggleFixture()
	if _, err := runPolicyToggle(t, mock, false, "--category", "Lab"); err != nil {
		t.Fatalf("preview: %v", err)
	}
	if mock.hasMutatingCall() {
		t.Error("filtered preview wrote")
	}

	mock = policyToggleFixture()
	if _, err := runPolicyToggle(t, mock, false, "--category", "Lab", "--yes"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	// 2 is already disabled; only 3 changes.
	if got := mock.callsMatching("PUT "); len(got) != 1 || !strings.Contains(got[0], "/id/3") {
		t.Errorf("wrote %v, want policy 3 only", got)
	}
}

func TestPolicyToggle_DryRunBeatsYes(t *testing.T) {
	t.Cleanup(func() { dryRun = false })
	dryRun = true
	mock := policyToggleFixture()
	if _, err := runPolicyToggle(t, mock, false, "1", "--yes"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.hasMutatingCall() {
		t.Error("-n with --yes wrote a policy")
	}
}

func TestPolicyToggle_OneTargetForm(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"1", "--name", "Deploy Chrome"},
		{"1", "--category", "Lab"},
		{"--from-file", "x", "--scope-group", "Lab Macs"},
		{"Deploy Chrome"},
	} {
		if _, err := runPolicyToggle(t, policyToggleFixture(), true, args...); err == nil {
			t.Errorf("args %q were accepted", args)
		}
	}
}

// A policy already in the requested state is left alone and is not a failure.
func TestPolicyToggle_AlreadyInState(t *testing.T) {
	mock := policyToggleFixture()
	stderr, err := runPolicyToggle(t, mock, true, "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.hasMutatingCall() || !strings.Contains(stderr, "already enabled") {
		t.Errorf("stderr = %q, mutated = %v; want it left alone", stderr, mock.hasMutatingCall())
	}
}
