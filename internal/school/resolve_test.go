// Copyright 2026, Jamf Software LLC

package school

import (
	"context"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfschool-go-sdk/jamfschool"
)

// mockSchoolClient embeds the interface so only the methods we need must be
// implemented. The zero-value embed will panic if an unimplemented method is
// called, which is the correct behavior for tests.
type mockSchoolClient struct {
	registry.SchoolClient

	devices      []jamfschool.Device
	users        []jamfschool.User
	profiles     []jamfschool.Profile
	apps         []jamfschool.App
	classes      []jamfschool.Class
	groups       []jamfschool.Group
	deviceGroups []jamfschool.DeviceGroup
	locations    []jamfschool.Location
	ibeacons     []jamfschool.IBeacon

	getDevicesCalls      int
	getUsersCalls        int
	getProfilesCalls     int
	getAppsCalls         int
	getClassesCalls      int
	getGroupsCalls       int
	getDeviceGroupsCalls int
	getLocationsCalls    int
	getIBeaconsCalls     int
}

func (m *mockSchoolClient) GetDevices(_ context.Context) ([]jamfschool.Device, error) {
	m.getDevicesCalls++
	return m.devices, nil
}

func (m *mockSchoolClient) GetUsers(_ context.Context) ([]jamfschool.User, error) {
	m.getUsersCalls++
	return m.users, nil
}

func (m *mockSchoolClient) GetProfiles(_ context.Context) ([]jamfschool.Profile, error) {
	m.getProfilesCalls++
	return m.profiles, nil
}

func (m *mockSchoolClient) GetApps(_ context.Context) ([]jamfschool.App, error) {
	m.getAppsCalls++
	return m.apps, nil
}

func (m *mockSchoolClient) GetClasses(_ context.Context) ([]jamfschool.Class, error) {
	m.getClassesCalls++
	return m.classes, nil
}

func (m *mockSchoolClient) GetGroups(_ context.Context) ([]jamfschool.Group, error) {
	m.getGroupsCalls++
	return m.groups, nil
}

func (m *mockSchoolClient) GetDeviceGroups(_ context.Context) ([]jamfschool.DeviceGroup, error) {
	m.getDeviceGroupsCalls++
	return m.deviceGroups, nil
}

func (m *mockSchoolClient) GetLocations(_ context.Context) ([]jamfschool.Location, error) {
	m.getLocationsCalls++
	return m.locations, nil
}

func (m *mockSchoolClient) GetIBeacons(_ context.Context) ([]jamfschool.IBeacon, error) {
	m.getIBeaconsCalls++
	return m.ibeacons, nil
}

// ---------------------------------------------------------------------------
// Devices
// ---------------------------------------------------------------------------

func TestResolveDeviceUDID_ByName(t *testing.T) {
	mock := &mockSchoolClient{
		devices: []jamfschool.Device{
			{UDID: "udid-001", Name: "iPad Lab 1", SerialNumber: "C02X1234"},
			{UDID: "udid-002", Name: "iPad Lab 2", SerialNumber: "C02X5678"},
		},
	}
	r := NewResolver(mock)

	udid, err := r.ResolveDeviceUDID(context.Background(), "iPad Lab 2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if udid != "udid-002" {
		t.Fatalf("got %q, want %q", udid, "udid-002")
	}
}

func TestResolveDeviceUDID_BySerial(t *testing.T) {
	mock := &mockSchoolClient{
		devices: []jamfschool.Device{
			{UDID: "udid-001", Name: "iPad Lab 1", SerialNumber: "C02X1234"},
		},
	}
	r := NewResolver(mock)

	udid, err := r.ResolveDeviceUDID(context.Background(), "C02X1234")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if udid != "udid-001" {
		t.Fatalf("got %q, want %q", udid, "udid-001")
	}
}

func TestResolveDeviceUDID_NotFound(t *testing.T) {
	mock := &mockSchoolClient{
		devices: []jamfschool.Device{
			{UDID: "udid-001", Name: "iPad Lab 1", SerialNumber: "C02X1234"},
		},
	}
	r := NewResolver(mock)

	_, err := r.ResolveDeviceUDID(context.Background(), "unknown")
	if err == nil {
		t.Fatal("expected error, got nil")
		return
	}
}

func TestResolveDeviceUDID_Cached(t *testing.T) {
	mock := &mockSchoolClient{
		devices: []jamfschool.Device{
			{UDID: "udid-001", Name: "iPad", SerialNumber: "C02X"},
		},
	}
	r := NewResolver(mock)

	if _, err := r.ResolveDeviceUDID(context.Background(), "iPad"); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := r.ResolveDeviceUDID(context.Background(), "iPad"); err != nil {
		t.Fatalf("second call: %v", err)
	}

	if mock.getDevicesCalls != 1 {
		t.Fatalf("expected GetDevices called once, got %d", mock.getDevicesCalls)
	}
}

// ---------------------------------------------------------------------------
// Users
// ---------------------------------------------------------------------------

func TestResolveUserID_ByUsername(t *testing.T) {
	mock := &mockSchoolClient{
		users: []jamfschool.User{
			{ID: 1, Username: "jdoe", Email: "jdoe@school.edu"},
			{ID: 2, Username: "jsmith", Email: "jsmith@school.edu"},
		},
	}
	r := NewResolver(mock)

	id, err := r.ResolveUserID(context.Background(), "jsmith")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 2 {
		t.Fatalf("got %d, want 2", id)
	}
}

func TestResolveUserID_ByEmail(t *testing.T) {
	mock := &mockSchoolClient{
		users: []jamfschool.User{
			{ID: 1, Username: "jdoe", Email: "jdoe@school.edu"},
		},
	}
	r := NewResolver(mock)

	id, err := r.ResolveUserID(context.Background(), "jdoe@school.edu")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 1 {
		t.Fatalf("got %d, want 1", id)
	}
}

func TestResolveUserID_NotFound(t *testing.T) {
	mock := &mockSchoolClient{
		users: []jamfschool.User{
			{ID: 1, Username: "jdoe", Email: "jdoe@school.edu"},
		},
	}
	r := NewResolver(mock)

	_, err := r.ResolveUserID(context.Background(), "nobody")
	if err == nil {
		t.Fatal("expected error, got nil")
		return
	}
}

func TestResolveUserID_Cached(t *testing.T) {
	mock := &mockSchoolClient{
		users: []jamfschool.User{
			{ID: 1, Username: "jdoe", Email: "jdoe@school.edu"},
		},
	}
	r := NewResolver(mock)

	if _, err := r.ResolveUserID(context.Background(), "jdoe"); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := r.ResolveUserID(context.Background(), "jdoe"); err != nil {
		t.Fatalf("second call: %v", err)
	}

	if mock.getUsersCalls != 1 {
		t.Fatalf("expected GetUsers called once, got %d", mock.getUsersCalls)
	}
}

// ---------------------------------------------------------------------------
// Profiles
// ---------------------------------------------------------------------------

func TestResolveProfileID_Found(t *testing.T) {
	mock := &mockSchoolClient{
		profiles: []jamfschool.Profile{
			{ID: 10, Name: "WiFi Profile"},
			{ID: 20, Name: "VPN Profile"},
		},
	}
	r := NewResolver(mock)

	id, err := r.ResolveProfileID(context.Background(), "VPN Profile")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 20 {
		t.Fatalf("got %d, want 20", id)
	}
}

func TestResolveProfileID_NotFound(t *testing.T) {
	mock := &mockSchoolClient{
		profiles: []jamfschool.Profile{
			{ID: 10, Name: "WiFi Profile"},
		},
	}
	r := NewResolver(mock)

	_, err := r.ResolveProfileID(context.Background(), "Missing")
	if err == nil {
		t.Fatal("expected error, got nil")
		return
	}
}

// ---------------------------------------------------------------------------
// Apps
// ---------------------------------------------------------------------------

func TestResolveAppID_Found(t *testing.T) {
	mock := &mockSchoolClient{
		apps: []jamfschool.App{
			{ID: 1, Name: "Pages"},
			{ID: 2, Name: "Keynote"},
		},
	}
	r := NewResolver(mock)

	id, err := r.ResolveAppID(context.Background(), "Keynote")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 2 {
		t.Fatalf("got %d, want 2", id)
	}
}

func TestResolveAppID_NotFound(t *testing.T) {
	mock := &mockSchoolClient{
		apps: []jamfschool.App{{ID: 1, Name: "Pages"}},
	}
	r := NewResolver(mock)

	_, err := r.ResolveAppID(context.Background(), "Numbers")
	if err == nil {
		t.Fatal("expected error, got nil")
		return
	}
}

// ---------------------------------------------------------------------------
// Classes
// ---------------------------------------------------------------------------

func TestResolveClassUUID_Found(t *testing.T) {
	mock := &mockSchoolClient{
		classes: []jamfschool.Class{
			{UUID: "uuid-math", Name: "Math 101"},
			{UUID: "uuid-sci", Name: "Science 201"},
		},
	}
	r := NewResolver(mock)

	uuid, err := r.ResolveClassUUID(context.Background(), "Science 201")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uuid != "uuid-sci" {
		t.Fatalf("got %q, want %q", uuid, "uuid-sci")
	}
}

func TestResolveClassUUID_NotFound(t *testing.T) {
	mock := &mockSchoolClient{
		classes: []jamfschool.Class{{UUID: "uuid-math", Name: "Math 101"}},
	}
	r := NewResolver(mock)

	_, err := r.ResolveClassUUID(context.Background(), "History")
	if err == nil {
		t.Fatal("expected error, got nil")
		return
	}
}

// ---------------------------------------------------------------------------
// Groups
// ---------------------------------------------------------------------------

func TestResolveGroupID_Found(t *testing.T) {
	mock := &mockSchoolClient{
		groups: []jamfschool.Group{
			{ID: 1, Name: "Teachers"},
			{ID: 2, Name: "Students"},
		},
	}
	r := NewResolver(mock)

	id, err := r.ResolveGroupID(context.Background(), "Students")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 2 {
		t.Fatalf("got %d, want 2", id)
	}
}

func TestResolveGroupID_NotFound(t *testing.T) {
	mock := &mockSchoolClient{
		groups: []jamfschool.Group{{ID: 1, Name: "Teachers"}},
	}
	r := NewResolver(mock)

	_, err := r.ResolveGroupID(context.Background(), "Admins")
	if err == nil {
		t.Fatal("expected error, got nil")
		return
	}
}

// ---------------------------------------------------------------------------
// Device Groups
// ---------------------------------------------------------------------------

func TestResolveDeviceGroupID_Found(t *testing.T) {
	mock := &mockSchoolClient{
		deviceGroups: []jamfschool.DeviceGroup{
			{ID: 10, Name: "Lab iPads"},
			{ID: 20, Name: "Cart MacBooks"},
		},
	}
	r := NewResolver(mock)

	id, err := r.ResolveDeviceGroupID(context.Background(), "Cart MacBooks")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 20 {
		t.Fatalf("got %d, want 20", id)
	}
}

func TestResolveDeviceGroupID_NotFound(t *testing.T) {
	mock := &mockSchoolClient{
		deviceGroups: []jamfschool.DeviceGroup{{ID: 10, Name: "Lab iPads"}},
	}
	r := NewResolver(mock)

	_, err := r.ResolveDeviceGroupID(context.Background(), "Missing")
	if err == nil {
		t.Fatal("expected error, got nil")
		return
	}
}

// ---------------------------------------------------------------------------
// Locations
// ---------------------------------------------------------------------------

func TestResolveLocationID_Found(t *testing.T) {
	mock := &mockSchoolClient{
		locations: []jamfschool.Location{
			{ID: 1, Name: "Main Campus"},
			{ID: 2, Name: "Satellite Office"},
		},
	}
	r := NewResolver(mock)

	id, err := r.ResolveLocationID(context.Background(), "Satellite Office")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 2 {
		t.Fatalf("got %d, want 2", id)
	}
}

func TestResolveLocationID_NotFound(t *testing.T) {
	mock := &mockSchoolClient{
		locations: []jamfschool.Location{{ID: 1, Name: "Main Campus"}},
	}
	r := NewResolver(mock)

	_, err := r.ResolveLocationID(context.Background(), "Nowhere")
	if err == nil {
		t.Fatal("expected error, got nil")
		return
	}
}

// ---------------------------------------------------------------------------
// iBeacons
// ---------------------------------------------------------------------------

func TestResolveIBeaconID_Found(t *testing.T) {
	mock := &mockSchoolClient{
		ibeacons: []jamfschool.IBeacon{
			{ID: 1, Name: "Library"},
			{ID: 2, Name: "Gym"},
		},
	}
	r := NewResolver(mock)

	id, err := r.ResolveIBeaconID(context.Background(), "Gym")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 2 {
		t.Fatalf("got %d, want 2", id)
	}
}

func TestResolveIBeaconID_NotFound(t *testing.T) {
	mock := &mockSchoolClient{
		ibeacons: []jamfschool.IBeacon{{ID: 1, Name: "Library"}},
	}
	r := NewResolver(mock)

	_, err := r.ResolveIBeaconID(context.Background(), "Cafeteria")
	if err == nil {
		t.Fatal("expected error, got nil")
		return
	}
}
