// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"net/http"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// registerDevicePlatformFixtures registers the shared device-resolution and
// device-groups endpoints used by all fetchDevicePlatformSections tests.
// Device grp-a is always returned as the device's group membership.
func registerDevicePlatformFixtures(mux *http.ServeMux) {
	mux.HandleFunc("/devices/v1/devices", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"results":    []map[string]any{{"id": "dev-1", "serialNumber": "SN001"}},
			"totalCount": 1,
		})
	})
	mux.HandleFunc("/device-groups/v1/devices/dev-1/device-groups", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"results":    []map[string]any{{"groupId": "grp-a"}},
			"totalCount": 1,
		})
	})
	mux.HandleFunc("/ddm/report/v1/devices/dev-1", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"channels": []any{}})
	})
}

// TestFetchDevicePlatformSections_NilBlueprintScope verifies that a blueprint
// whose scope field is absent from the API response does not panic, and that
// it is excluded from the output (nil scope never overlaps).
func TestFetchDevicePlatformSections_NilBlueprintScope(t *testing.T) {
	sdk, mux := newTestPlatformSDK(t)
	registerDevicePlatformFixtures(mux)

	mux.HandleFunc("/blueprints/v1/blueprints", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"results":    []map[string]any{{"id": "bp-1", "name": "Blueprint A"}},
			"totalCount": 1,
		})
	})
	// scope field absent — SDK deserialises Scope to nil *BlueprintScope
	mux.HandleFunc("/blueprints/v1/blueprints/bp-1", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"id": "bp-1", "name": "Blueprint A"})
	})
	mux.HandleFunc("/compliance-benchmarks/v1/benchmarks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"benchmarks": []any{}})
	})

	sections := fetchDevicePlatformSections(context.Background(), &registry.CLIContext{PlatformSDKClient: sdk}, "SN001")

	bpSection := findPlatformSection(sections, "Platform Blueprints")
	if bpSection == nil {
		t.Fatal("Platform Blueprints section missing")
	}
	for _, item := range bpSection.Items {
		if item.Resource == "Blueprint A" {
			t.Error("blueprint with nil scope should be excluded from output")
		}
	}
}

// TestFetchDevicePlatformSections_NilDeploymentState verifies that a blueprint
// with a scope that matches the device's group but an absent deploymentState
// does not panic, and appears with an empty state string.
func TestFetchDevicePlatformSections_NilDeploymentState(t *testing.T) {
	sdk, mux := newTestPlatformSDK(t)
	registerDevicePlatformFixtures(mux)

	mux.HandleFunc("/blueprints/v1/blueprints", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"results":    []map[string]any{{"id": "bp-2", "name": "Blueprint B"}},
			"totalCount": 1,
		})
	})
	// scope includes grp-a (device's group); deploymentState absent → nil *DeploymentState
	mux.HandleFunc("/blueprints/v1/blueprints/bp-2", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"id":    "bp-2",
			"name":  "Blueprint B",
			"scope": map[string]any{"deviceGroups": []string{"grp-a"}},
		})
	})
	mux.HandleFunc("/compliance-benchmarks/v1/benchmarks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"benchmarks": []any{}})
	})

	sections := fetchDevicePlatformSections(context.Background(), &registry.CLIContext{PlatformSDKClient: sdk}, "SN001")

	bpSection := findPlatformSection(sections, "Platform Blueprints")
	if bpSection == nil {
		t.Fatal("Platform Blueprints section missing")
	}
	for _, item := range bpSection.Items {
		if item.Resource == "Blueprint B" {
			if item.Value != "" {
				t.Errorf("nil deploymentState: state = %q, want empty string", item.Value)
			}
			return
		}
	}
	t.Error("Blueprint B not found in Platform Blueprints section")
}

// TestFetchDevicePlatformSections_NilBenchmarkTarget verifies that a benchmark
// whose target field is absent from the API response does not panic, and that
// it is excluded from the output (nil target never overlaps).
func TestFetchDevicePlatformSections_NilBenchmarkTarget(t *testing.T) {
	sdk, mux := newTestPlatformSDK(t)
	registerDevicePlatformFixtures(mux)

	mux.HandleFunc("/blueprints/v1/blueprints", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"results": []any{}, "totalCount": 0})
	})
	mux.HandleFunc("/compliance-benchmarks/v1/benchmarks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"benchmarks": []map[string]any{{"id": "cb-1", "title": "Benchmark X"}},
		})
	})
	// target field absent — SDK deserialises Target to nil *TargetV2
	mux.HandleFunc("/compliance-benchmarks/v1/benchmarks/cb-1", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"id": "cb-1", "title": "Benchmark X"})
	})

	sections := fetchDevicePlatformSections(context.Background(), &registry.CLIContext{PlatformSDKClient: sdk}, "SN001")

	cbSection := findPlatformSection(sections, "Platform Compliance")
	if cbSection == nil {
		t.Fatal("Compliance Benchmarks section missing")
	}
	for _, item := range cbSection.Items {
		if item.Resource == "Benchmark X" {
			t.Error("benchmark with nil target should be excluded from output")
		}
	}
}

func findPlatformSection(sections []overviewSection, name string) *overviewSection {
	for i := range sections {
		if sections[i].Name == name {
			return &sections[i]
		}
	}
	return nil
}
