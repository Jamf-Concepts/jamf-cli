// Copyright 2026, Jamf Software LLC

package school

import (
	"context"
	"fmt"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// Resolver maps resource names to IDs/UUIDs. Results are cached per
// resource type to avoid redundant list calls within a single command.
type Resolver struct {
	client registry.SchoolClient

	devices      map[string]string // name -> UDID
	deviceSerial map[string]string // serial -> UDID
	users        map[string]int64  // username -> ID
	userEmail    map[string]int64  // email -> ID
	profiles     map[string]int64  // name -> ID
	apps         map[string]int64  // name -> ID
	classes      map[string]string // name -> UUID
	groups       map[string]int64  // name -> ID
	deviceGroups map[string]int64  // name -> ID
	locations    map[string]int64  // name -> ID
	ibeacons     map[string]int64  // name -> ID
}

// NewResolver creates a Resolver for the given School client.
func NewResolver(client registry.SchoolClient) *Resolver {
	return &Resolver{client: client}
}

// ResolveDeviceUDID returns the UDID for a device given its name or serial number.
func (r *Resolver) ResolveDeviceUDID(ctx context.Context, nameOrSerial string) (string, error) {
	if r.devices == nil {
		devices, err := r.client.GetDevices(ctx)
		if err != nil {
			return "", fmt.Errorf("listing devices: %w", err)
		}
		r.devices = make(map[string]string, len(devices))
		r.deviceSerial = make(map[string]string, len(devices))
		for _, d := range devices {
			r.devices[d.Name] = d.UDID
			if d.SerialNumber != "" {
				r.deviceSerial[d.SerialNumber] = d.UDID
			}
		}
	}
	if udid, ok := r.devices[nameOrSerial]; ok {
		return udid, nil
	}
	if udid, ok := r.deviceSerial[nameOrSerial]; ok {
		return udid, nil
	}
	return "", fmt.Errorf("device %q not found by name or serial; use 'school devices list' to see available devices", nameOrSerial)
}

// ResolveUserID returns the ID for a user given their username or email.
func (r *Resolver) ResolveUserID(ctx context.Context, nameOrEmail string) (int64, error) {
	if r.users == nil {
		users, err := r.client.GetUsers(ctx)
		if err != nil {
			return 0, fmt.Errorf("listing users: %w", err)
		}
		r.users = make(map[string]int64, len(users))
		r.userEmail = make(map[string]int64, len(users))
		for _, u := range users {
			if u.Username != "" {
				r.users[u.Username] = u.ID
			}
			if u.Email != "" {
				r.userEmail[u.Email] = u.ID
			}
		}
	}
	if id, ok := r.users[nameOrEmail]; ok {
		return id, nil
	}
	if id, ok := r.userEmail[nameOrEmail]; ok {
		return id, nil
	}
	return 0, fmt.Errorf("user %q not found by username or email; use 'school users list' to see available users", nameOrEmail)
}

// ResolveProfileID returns the ID for a profile given its name.
func (r *Resolver) ResolveProfileID(ctx context.Context, name string) (int64, error) {
	if r.profiles == nil {
		items, err := r.client.GetProfiles(ctx)
		if err != nil {
			return 0, fmt.Errorf("listing profiles: %w", err)
		}
		r.profiles = make(map[string]int64, len(items))
		for _, p := range items {
			r.profiles[p.Name] = p.ID
		}
	}
	id, ok := r.profiles[name]
	if !ok {
		return 0, fmt.Errorf("profile %q not found; use 'school profiles list' to see available names", name)
	}
	return id, nil
}

// ResolveAppID returns the ID for an app given its name.
func (r *Resolver) ResolveAppID(ctx context.Context, name string) (int64, error) {
	if r.apps == nil {
		items, err := r.client.GetApps(ctx)
		if err != nil {
			return 0, fmt.Errorf("listing apps: %w", err)
		}
		r.apps = make(map[string]int64, len(items))
		for _, a := range items {
			r.apps[a.Name] = a.ID
		}
	}
	id, ok := r.apps[name]
	if !ok {
		return 0, fmt.Errorf("app %q not found; use 'school apps list' to see available names", name)
	}
	return id, nil
}

// ResolveClassUUID returns the UUID for a class given its name.
func (r *Resolver) ResolveClassUUID(ctx context.Context, name string) (string, error) {
	if r.classes == nil {
		items, err := r.client.GetClasses(ctx)
		if err != nil {
			return "", fmt.Errorf("listing classes: %w", err)
		}
		r.classes = make(map[string]string, len(items))
		for _, c := range items {
			r.classes[c.Name] = c.UUID
		}
	}
	id, ok := r.classes[name]
	if !ok {
		return "", fmt.Errorf("class %q not found; use 'school classes list' to see available names", name)
	}
	return id, nil
}

// ResolveGroupID returns the ID for a user group given its name.
func (r *Resolver) ResolveGroupID(ctx context.Context, name string) (int64, error) {
	if r.groups == nil {
		items, err := r.client.GetGroups(ctx)
		if err != nil {
			return 0, fmt.Errorf("listing groups: %w", err)
		}
		r.groups = make(map[string]int64, len(items))
		for _, g := range items {
			r.groups[g.Name] = g.ID
		}
	}
	id, ok := r.groups[name]
	if !ok {
		return 0, fmt.Errorf("group %q not found; use 'school groups list' to see available names", name)
	}
	return id, nil
}

// ResolveDeviceGroupID returns the ID for a device group given its name.
func (r *Resolver) ResolveDeviceGroupID(ctx context.Context, name string) (int64, error) {
	if r.deviceGroups == nil {
		items, err := r.client.GetDeviceGroups(ctx)
		if err != nil {
			return 0, fmt.Errorf("listing device groups: %w", err)
		}
		r.deviceGroups = make(map[string]int64, len(items))
		for _, g := range items {
			r.deviceGroups[g.Name] = g.ID
		}
	}
	id, ok := r.deviceGroups[name]
	if !ok {
		return 0, fmt.Errorf("device group %q not found; use 'school device-groups list' to see available names", name)
	}
	return id, nil
}

// ResolveLocationID returns the ID for a location given its name.
func (r *Resolver) ResolveLocationID(ctx context.Context, name string) (int64, error) {
	if r.locations == nil {
		items, err := r.client.GetLocations(ctx)
		if err != nil {
			return 0, fmt.Errorf("listing locations: %w", err)
		}
		r.locations = make(map[string]int64, len(items))
		for _, l := range items {
			r.locations[l.Name] = l.ID
		}
	}
	id, ok := r.locations[name]
	if !ok {
		return 0, fmt.Errorf("location %q not found; use 'school locations list' to see available names", name)
	}
	return id, nil
}

// ResolveIBeaconID returns the ID for an iBeacon given its name.
func (r *Resolver) ResolveIBeaconID(ctx context.Context, name string) (int64, error) {
	if r.ibeacons == nil {
		items, err := r.client.GetIBeacons(ctx)
		if err != nil {
			return 0, fmt.Errorf("listing ibeacons: %w", err)
		}
		r.ibeacons = make(map[string]int64, len(items))
		for _, b := range items {
			r.ibeacons[b.Name] = b.ID
		}
	}
	id, ok := r.ibeacons[name]
	if !ok {
		return 0, fmt.Errorf("ibeacon %q not found; use 'school ibeacons list' to see available names", name)
	}
	return id, nil
}
