// Copyright 2026, Jamf Software LLC

package platform

import (
	"context"
	"errors"
	"fmt"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// ErrNotFound is returned when a resource name cannot be resolved to an ID.
var ErrNotFound = errors.New("not found")

// IsNotFound reports whether err is or wraps ErrNotFound.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// Resolver maps human-readable names to IDs for Jamf Platform API resources.
// Caches are populated lazily on first use and shared within a single command.
type Resolver struct {
	client     registry.PlatformClient
	blueprints map[string]string // name -> ID
	benchmarks map[string]string // title -> ID
	baselines  map[string]string // title -> ID
	groups     map[string]string // name -> ID
}

// NewResolver creates a Resolver backed by the given Platform client.
func NewResolver(client registry.PlatformClient) *Resolver {
	return &Resolver{client: client}
}

// ResolveBlueprintID returns the ID for a blueprint with the given name.
func (r *Resolver) ResolveBlueprintID(ctx context.Context, name string) (string, error) {
	if r.blueprints == nil {
		bps, err := r.client.ListBlueprints(ctx, nil, "")
		if err != nil {
			return "", fmt.Errorf("listing blueprints: %w", err)
		}
		r.blueprints = make(map[string]string, len(bps))
		for _, bp := range bps {
			r.blueprints[bp.Name] = bp.ID
		}
	}
	id, ok := r.blueprints[name]
	if !ok {
		return "", fmt.Errorf("blueprint %q not found; use 'pro blueprints list' to see available names: %w", name, ErrNotFound)
	}
	return id, nil
}

// ResolveBenchmarkID returns the ID for a benchmark with the given title.
func (r *Resolver) ResolveBenchmarkID(ctx context.Context, title string) (string, error) {
	if r.benchmarks == nil {
		resp, err := r.client.ListBenchmarks(ctx)
		if err != nil {
			return "", fmt.Errorf("listing benchmarks: %w", err)
		}
		r.benchmarks = make(map[string]string, len(resp.Benchmarks))
		for _, bm := range resp.Benchmarks {
			r.benchmarks[bm.Title] = bm.ID
		}
	}
	id, ok := r.benchmarks[title]
	if !ok {
		return "", fmt.Errorf("benchmark %q not found; use 'pro compliance-benchmarks list' to see available titles", title)
	}
	return id, nil
}

// ResolveBaselineID returns the ID for a baseline with the given title.
func (r *Resolver) ResolveBaselineID(ctx context.Context, title string) (string, error) {
	if r.baselines == nil {
		resp, err := r.client.ListBaselines(ctx)
		if err != nil {
			return "", fmt.Errorf("listing baselines: %w", err)
		}
		r.baselines = make(map[string]string, len(resp.Baselines))
		for _, bl := range resp.Baselines {
			r.baselines[bl.Title] = bl.ID
		}
	}
	id, ok := r.baselines[title]
	if !ok {
		return "", fmt.Errorf("baseline %q not found; use 'pro compliance-benchmarks baselines' to see available titles", title)
	}
	return id, nil
}

// ResolveDeviceGroupID returns the ID for a device group with the given name.
func (r *Resolver) ResolveDeviceGroupID(ctx context.Context, name string) (string, error) {
	if r.groups == nil {
		dgs, err := r.client.ListDeviceGroups(ctx, nil, "")
		if err != nil {
			return "", fmt.Errorf("listing device groups: %w", err)
		}
		r.groups = make(map[string]string, len(dgs))
		for _, dg := range dgs {
			r.groups[dg.Name] = dg.ID
		}
	}
	id, ok := r.groups[name]
	if !ok {
		return "", fmt.Errorf("device group %q not found; use 'pro platform-device-groups list' to see available names", name)
	}
	return id, nil
}
