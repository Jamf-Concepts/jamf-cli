// Copyright 2026, Jamf Software LLC

package gateway

import "testing"

// A re-derivation that was not told which SDK it came from must keep what the
// manifest already recorded. Blanking it made verify-gateway-coverage report a
// stale manifest that was byte-identical apart from the field it had just
// erased — and, worse, a rewritten manifest nobody could trace to an SDK.
func TestCarryForwardProvenanceKeepsTheRecordedSDKRevision(t *testing.T) {
	fresh := &Coverage{}
	prev := &Coverage{Sources: Sources{SDKCommit: "adb8d7b"}}
	CarryForwardProvenance(fresh, prev)
	if fresh.Sources.SDKCommit != "adb8d7b" {
		t.Errorf("SDKCommit: got %q, want the previously recorded revision", fresh.Sources.SDKCommit)
	}
}

// A run that WAS told must win: that is the whole point of passing it.
func TestCarryForwardProvenanceDoesNotOverwriteAKnownRevision(t *testing.T) {
	fresh := &Coverage{Sources: Sources{SDKCommit: "new1234"}}
	prev := &Coverage{Sources: Sources{SDKCommit: "adb8d7b"}}
	CarryForwardProvenance(fresh, prev)
	if fresh.Sources.SDKCommit != "new1234" {
		t.Errorf("SDKCommit: got %q, want the revision this run was given", fresh.Sources.SDKCommit)
	}
}

// A first run has nothing to carry forward, and must not crash reaching for it.
func TestCarryForwardProvenanceToleratesNoPreviousManifest(t *testing.T) {
	fresh := &Coverage{Sources: Sources{SDKCommit: "new1234"}}
	CarryForwardProvenance(fresh, nil)
	if fresh.Sources.SDKCommit != "new1234" {
		t.Errorf("SDKCommit: got %q, want it left alone", fresh.Sources.SDKCommit)
	}
}
