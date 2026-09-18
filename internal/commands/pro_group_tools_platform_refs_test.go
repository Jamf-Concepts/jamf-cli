// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"net/http"
	"testing"
)

// addPlatformReferencedGroups walks two Platform collections whose scope field
// is a POINTER with omitempty — BlueprintDetail.Scope and BenchmarkV2.Target —
// and dereferenced both without a nil check. A blueprint or benchmark that
// targets nothing therefore crashed `pro group-tools analyze --unused` with a
// nil pointer dereference, taking the whole command down after it had already
// spent six sweeps of the instance. Reproduced live against a gateway tenant
// holding one unscoped blueprint: the operator has no way around it from the
// command line, and the panic names an SDK type rather than the blueprint.
//
// Both halves are covered here because the second one had not fired yet purely
// because that tenant's benchmarks all happened to be targeted, and a fix to
// one loop invites the reasoning that the other is different.
func TestAddPlatformReferencedGroups_ToleratesAnUnscopedBlueprintAndBenchmark(t *testing.T) {
	sdk, mux := newTestPlatformSDK(t)

	mux.HandleFunc("/device-groups/v1/device-groups", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"results": []any{
				map[string]any{"id": "dg-1", "name": "Scoped Group"},
				map[string]any{"id": "dg-2", "name": "Benchmark Group"},
			},
			"totalCount": 2,
		})
	})

	mux.HandleFunc("/blueprints/v1/blueprints", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"results": []any{
				map[string]any{"id": "bp-unscoped", "name": "Targets nothing"},
				map[string]any{"id": "bp-scoped", "name": "Targets dg-1"},
			},
			"totalCount": 2,
		})
	})
	// No "scope" key at all — which is what the API sends for a blueprint that
	// targets nothing, and what omitempty turns into a nil pointer.
	mux.HandleFunc("/blueprints/v1/blueprints/bp-unscoped", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"id": "bp-unscoped", "name": "Targets nothing"})
	})
	mux.HandleFunc("/blueprints/v1/blueprints/bp-scoped", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"id":    "bp-scoped",
			"name":  "Targets dg-1",
			"scope": map[string]any{"deviceGroups": []any{"dg-1"}},
		})
	})

	mux.HandleFunc("/compliance-benchmarks/v1/benchmarks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"benchmarks": []any{
				map[string]any{"id": "cb-untargeted", "title": "Targets nothing"},
				map[string]any{
					"id":     "cb-targeted",
					"title":  "Targets dg-2",
					"target": map[string]any{"deviceGroups": []any{"dg-2"}},
				},
			},
		})
	})

	referenced := map[string]bool{}
	addPlatformReferencedGroups(context.Background(), sdk, referenced)

	// The unscoped pair must be skipped rather than fatal, and — the part a
	// bare recover() would not give — the scoped pair on the far side of it
	// must still be read. A guard that returned instead of continuing would
	// pass a crash test and silently stop marking groups as referenced, which
	// is worse than the panic: `--unused` would then offer a group that a
	// blueprint does scope as a removal candidate.
	for _, want := range []string{"Scoped Group", "Benchmark Group"} {
		if !referenced[want] {
			t.Errorf("group %q is referenced by a scoped blueprint or benchmark but was not marked; the walk stopped at the unscoped one", want)
		}
	}
	if len(referenced) != 2 {
		t.Errorf("referenced = %v, want exactly the two scoped groups", referenced)
	}
}
