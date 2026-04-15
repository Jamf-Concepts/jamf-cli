// Copyright 2026, Jamf Software LLC

package protect

import (
	"context"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

// mockProtectClient embeds the interface so only the methods we need must be
// implemented. The zero-value embed will panic if an unimplemented method is
// called, which is the correct behavior for tests.
type mockProtectClient struct {
	registry.ProtectClient

	plans     []jamfprotect.Plan
	analytics []jamfprotect.Analytic
	computers []jamfprotect.Computer

	listPlansCalls     int
	listAnalyticsCalls int
	listComputersCalls int
}

func (m *mockProtectClient) ListPlans(_ context.Context) ([]jamfprotect.Plan, error) {
	m.listPlansCalls++
	return m.plans, nil
}

func (m *mockProtectClient) ListAnalytics(_ context.Context) ([]jamfprotect.Analytic, error) {
	m.listAnalyticsCalls++
	return m.analytics, nil
}

func (m *mockProtectClient) ListComputers(_ context.Context) ([]jamfprotect.Computer, error) {
	m.listComputersCalls++
	return m.computers, nil
}

// ---------------------------------------------------------------------------
// Plans
// ---------------------------------------------------------------------------

func TestResolvePlanID_Found(t *testing.T) {
	mock := &mockProtectClient{
		plans: []jamfprotect.Plan{
			{ID: "plan-001", Name: "Default Plan"},
			{ID: "plan-002", Name: "Custom Plan"},
		},
	}
	r := NewResolver(mock)

	id, err := r.ResolvePlanID(context.Background(), "Custom Plan")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "plan-002" {
		t.Fatalf("got %q, want %q", id, "plan-002")
	}
}

func TestResolvePlanID_NotFound(t *testing.T) {
	mock := &mockProtectClient{
		plans: []jamfprotect.Plan{
			{ID: "plan-001", Name: "Default Plan"},
		},
	}
	r := NewResolver(mock)

	_, err := r.ResolvePlanID(context.Background(), "Nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
		return
	}
	want := `plan "Nonexistent" not found`
	if got := err.Error(); len(got) < len(want) || got[:len(want)] != want {
		t.Fatalf("error should start with %q, got %q", want, got)
	}
}

func TestResolvePlanID_Cached(t *testing.T) {
	mock := &mockProtectClient{
		plans: []jamfprotect.Plan{
			{ID: "plan-001", Name: "Alpha"},
		},
	}
	r := NewResolver(mock)

	// First call populates the cache.
	if _, err := r.ResolvePlanID(context.Background(), "Alpha"); err != nil {
		t.Fatalf("first call: %v", err)
	}
	// Second call should not list again.
	if _, err := r.ResolvePlanID(context.Background(), "Alpha"); err != nil {
		t.Fatalf("second call: %v", err)
	}

	if mock.listPlansCalls != 1 {
		t.Fatalf("expected ListPlans to be called once, got %d", mock.listPlansCalls)
	}
}

// ---------------------------------------------------------------------------
// Analytics
// ---------------------------------------------------------------------------

func TestResolveAnalyticUUID_Found(t *testing.T) {
	mock := &mockProtectClient{
		analytics: []jamfprotect.Analytic{
			{UUID: "uuid-aaa", Name: "SSH Login"},
			{UUID: "uuid-bbb", Name: "Suspicious Process"},
		},
	}
	r := NewResolver(mock)

	id, err := r.ResolveAnalyticUUID(context.Background(), "Suspicious Process")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "uuid-bbb" {
		t.Fatalf("got %q, want %q", id, "uuid-bbb")
	}
}

func TestResolveAnalyticUUID_NotFound(t *testing.T) {
	mock := &mockProtectClient{
		analytics: []jamfprotect.Analytic{
			{UUID: "uuid-aaa", Name: "SSH Login"},
		},
	}
	r := NewResolver(mock)

	_, err := r.ResolveAnalyticUUID(context.Background(), "Missing")
	if err == nil {
		t.Fatal("expected error, got nil")
		return
	}
	want := `analytic "Missing" not found`
	if got := err.Error(); len(got) < len(want) || got[:len(want)] != want {
		t.Fatalf("error should start with %q, got %q", want, got)
	}
}

// ---------------------------------------------------------------------------
// Computers
// ---------------------------------------------------------------------------

func TestResolveComputerUUID_ByHostname(t *testing.T) {
	mock := &mockProtectClient{
		computers: []jamfprotect.Computer{
			{
				UUID:     new("comp-uuid-1"),
				HostName: new("mac-01.local"),
				Serial:   new("C02X1234"),
			},
		},
	}
	r := NewResolver(mock)

	id, err := r.ResolveComputerUUID(context.Background(), "mac-01.local")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "comp-uuid-1" {
		t.Fatalf("got %q, want %q", id, "comp-uuid-1")
	}
}

func TestResolveComputerUUID_BySerial(t *testing.T) {
	mock := &mockProtectClient{
		computers: []jamfprotect.Computer{
			{
				UUID:     new("comp-uuid-2"),
				HostName: new("mac-02.local"),
				Serial:   new("C02Y5678"),
			},
		},
	}
	r := NewResolver(mock)

	id, err := r.ResolveComputerUUID(context.Background(), "C02Y5678")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "comp-uuid-2" {
		t.Fatalf("got %q, want %q", id, "comp-uuid-2")
	}
}

func TestResolveComputerUUID_NotFound(t *testing.T) {
	mock := &mockProtectClient{
		computers: []jamfprotect.Computer{
			{
				UUID:     new("comp-uuid-1"),
				HostName: new("mac-01.local"),
				Serial:   new("C02X1234"),
			},
		},
	}
	r := NewResolver(mock)

	_, err := r.ResolveComputerUUID(context.Background(), "unknown-host")
	if err == nil {
		t.Fatal("expected error, got nil")
		return
	}
	want := `computer "unknown-host" not found`
	if got := err.Error(); len(got) < len(want) || got[:len(want)] != want {
		t.Fatalf("error should start with %q, got %q", want, got)
	}
}
