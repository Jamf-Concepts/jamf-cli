// Copyright 2026, Jamf Software LLC

package school

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Jamf-Concepts/jamfschool-go-sdk/jamfschool"

	"github.com/Jamf-Concepts/jamf-cli/internal/pickone"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// ErrNotFound indicates a resource was not found by name during resolution.
type ErrNotFound struct {
	ResourceType string
	Name         string
	Hint         string // e.g. "use 'school devices list' to see available devices"
}

func (e *ErrNotFound) Error() string {
	if e.Hint != "" {
		return fmt.Sprintf("%s %q not found; %s", e.ResourceType, e.Name, e.Hint)
	}
	return fmt.Sprintf("%s %q not found", e.ResourceType, e.Name)
}

// listing caches one resource type's list response for the Resolver's lifetime.
type listing[T any] struct {
	items  []T
	loaded bool
}

func (l *listing[T]) load(ctx context.Context, what string, list func(context.Context) ([]T, error)) ([]T, error) {
	if !l.loaded {
		items, err := list(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing %s: %w", what, err)
		}
		l.items, l.loaded = items, true
	}
	return l.items, nil
}

// Resolver maps resource names to IDs/UUIDs. Results are cached per
// resource type to avoid redundant list calls within a single command.
type Resolver struct {
	client registry.SchoolClient

	devices      listing[jamfschool.Device]
	users        listing[jamfschool.User]
	profiles     listing[jamfschool.Profile]
	apps         listing[jamfschool.App]
	classes      listing[jamfschool.Class]
	groups       listing[jamfschool.Group]
	deviceGroups listing[jamfschool.DeviceGroup]
	locations    listing[jamfschool.Location]
	ibeacons     listing[jamfschool.IBeacon]
}

// NewResolver creates a Resolver for the given School client.
func NewResolver(client registry.SchoolClient) *Resolver {
	return &Resolver{client: client}
}

// pick resolves arg to exactly one item. ErrNone becomes *ErrNotFound, so
// create-on-absent callers keep working; an ambiguity names every candidate.
func pick[T any](items []T, arg, kind, hint string, describe func(T) string, tiers ...pickone.Tier[T]) (T, error) {
	item, candidates, err := pickone.One(items, arg, tiers...)
	switch {
	case errors.Is(err, pickone.ErrNone):
		return item, &ErrNotFound{kind, arg, hint}
	case errors.Is(err, pickone.ErrAmbiguous):
		names := make([]string, len(candidates))
		for i, c := range candidates {
			names[i] = describe(c)
		}
		return item, fmt.Errorf("%q matches %d %ss: %s; refusing to pick one", arg, len(candidates), kind, strings.Join(names, "; "))
	}
	return item, err
}

// DescribeDevice renders a device as name, serial and UDID for confirmations
// and ambiguity refusals.
func DescribeDevice(d jamfschool.Device) string {
	return fmt.Sprintf("%s (serial %s, UDID %s)", d.Name, d.SerialNumber, d.UDID)
}

// ResolveDevice returns the one device identified by arg, matched as an exact
// UDID, then serial number, then name. The first identifier that matches
// decides, so a device named after another device's serial cannot capture it.
func (r *Resolver) ResolveDevice(ctx context.Context, arg string) (jamfschool.Device, error) {
	devices, err := r.devices.load(ctx, "devices", r.client.GetDevices)
	if err != nil {
		return jamfschool.Device{}, err
	}
	return pick(devices, arg, "device", "use 'school devices list' to see available devices", DescribeDevice,
		pickone.Exact(func(d jamfschool.Device) string { return d.UDID }),
		pickone.Exact(func(d jamfschool.Device) string { return d.SerialNumber }),
		pickone.Exact(func(d jamfschool.Device) string { return d.Name }))
}

// ResolveDeviceUDID returns the UDID of the device ResolveDevice picks.
func (r *Resolver) ResolveDeviceUDID(ctx context.Context, arg string) (string, error) {
	d, err := r.ResolveDevice(ctx, arg)
	return d.UDID, err
}

// ResolveUserID returns the ID for a user given their username or email.
func (r *Resolver) ResolveUserID(ctx context.Context, nameOrEmail string) (int64, error) {
	users, err := r.users.load(ctx, "users", r.client.GetUsers)
	if err != nil {
		return 0, err
	}
	u, err := pick(users, nameOrEmail, "user", "use 'school users list' to see available users",
		func(u jamfschool.User) string { return fmt.Sprintf("%q (email %s, ID %d)", u.Username, u.Email, u.ID) },
		pickone.Exact(func(u jamfschool.User) string { return u.Username }),
		pickone.Exact(func(u jamfschool.User) string { return u.Email }))
	return u.ID, err
}

// byName resolves a name-keyed resource and returns its ID.
func byName[T any, ID any](items []T, name, kind, hint string, nameOf func(T) string, idOf func(T) ID) (ID, error) {
	item, err := pick(items, name, kind, hint,
		func(t T) string { return fmt.Sprintf("%q (ID %v)", nameOf(t), idOf(t)) },
		pickone.Exact(nameOf))
	if err != nil {
		var zero ID
		return zero, err
	}
	return idOf(item), nil
}

// ResolveProfileID returns the ID for a profile given its name.
func (r *Resolver) ResolveProfileID(ctx context.Context, name string) (int64, error) {
	items, err := r.profiles.load(ctx, "profiles", r.client.GetProfiles)
	if err != nil {
		return 0, err
	}
	return byName(items, name, "profile", "use 'school profiles list' to see available names",
		func(p jamfschool.Profile) string { return p.Name }, func(p jamfschool.Profile) int64 { return p.ID })
}

// ResolveAppID returns the ID for an app given its name.
func (r *Resolver) ResolveAppID(ctx context.Context, name string) (int64, error) {
	items, err := r.apps.load(ctx, "apps", r.client.GetApps)
	if err != nil {
		return 0, err
	}
	return byName(items, name, "app", "use 'school apps list' to see available names",
		func(a jamfschool.App) string { return a.Name }, func(a jamfschool.App) int64 { return a.ID })
}

// ResolveClassUUID returns the UUID for a class given its name.
func (r *Resolver) ResolveClassUUID(ctx context.Context, name string) (string, error) {
	items, err := r.classes.load(ctx, "classes", r.client.GetClasses)
	if err != nil {
		return "", err
	}
	return byName(items, name, "class", "use 'school classes list' to see available names",
		func(c jamfschool.Class) string { return c.Name }, func(c jamfschool.Class) string { return c.UUID })
}

// ResolveGroupID returns the ID for a user group given its name.
func (r *Resolver) ResolveGroupID(ctx context.Context, name string) (int64, error) {
	items, err := r.groups.load(ctx, "groups", r.client.GetGroups)
	if err != nil {
		return 0, err
	}
	return byName(items, name, "group", "use 'school groups list' to see available names",
		func(g jamfschool.Group) string { return g.Name }, func(g jamfschool.Group) int64 { return g.ID })
}

// ResolveDeviceGroupID returns the ID for a device group given its name.
func (r *Resolver) ResolveDeviceGroupID(ctx context.Context, name string) (int64, error) {
	items, err := r.deviceGroups.load(ctx, "device groups", r.client.GetDeviceGroups)
	if err != nil {
		return 0, err
	}
	return byName(items, name, "device group", "use 'school device-groups list' to see available names",
		func(g jamfschool.DeviceGroup) string { return g.Name }, func(g jamfschool.DeviceGroup) int64 { return g.ID })
}

// ResolveLocationID returns the ID for a location given its name.
func (r *Resolver) ResolveLocationID(ctx context.Context, name string) (int64, error) {
	items, err := r.locations.load(ctx, "locations", r.client.GetLocations)
	if err != nil {
		return 0, err
	}
	return byName(items, name, "location", "use 'school locations list' to see available names",
		func(l jamfschool.Location) string { return l.Name }, func(l jamfschool.Location) int64 { return l.ID })
}

// ResolveIBeaconID returns the ID for an iBeacon given its name.
func (r *Resolver) ResolveIBeaconID(ctx context.Context, name string) (int64, error) {
	items, err := r.ibeacons.load(ctx, "ibeacons", r.client.GetIBeacons)
	if err != nil {
		return 0, err
	}
	return byName(items, name, "ibeacon", "use 'school ibeacons list' to see available names",
		func(b jamfschool.IBeacon) string { return b.Name }, func(b jamfschool.IBeacon) int64 { return b.ID })
}
