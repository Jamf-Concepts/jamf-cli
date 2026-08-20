// Copyright 2026, Jamf Software LLC

package protect

import (
	"context"
	"errors"
	"fmt"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// ErrNotFound reports that a lookup completed and the name was absent — as
// distinct from the lookup itself failing.
//
// Callers that create-on-absent must tell the two apart with errors.Is. Treating
// every error as "absent" turns a transient list failure, an expired token or a
// permission problem into an unintended create, and the operator sees whatever
// the server says about the resulting duplicate rather than the real cause. That
// matters most in bulk paths like 'protect restore', where one blip mid-run would
// otherwise mutate the tenant a different way than intended.
var ErrNotFound = errors.New("not found")

// notFoundError carries the CLI's usual "use '<cmd> list'" hint while still
// matching ErrNotFound, so the message the user sees is unchanged.
type notFoundError struct{ msg string }

func (e *notFoundError) Error() string { return e.msg }

func (e *notFoundError) Is(target error) bool { return target == ErrNotFound }

// notFoundf builds an ErrNotFound-matching error with the given message.
func notFoundf(format string, args ...any) error {
	return &notFoundError{msg: fmt.Sprintf(format, args...)}
}

// Resolver maps resource names to IDs/UUIDs. Results are cached per
// resource type to avoid redundant list calls within a single command.
type Resolver struct {
	client registry.ProtectClient

	plans          map[string]string
	analytics      map[string]string
	analyticSets   map[string]string
	exceptionSets  map[string]string
	rscSets        map[string]string
	actionConfigs  map[string]string
	telemetriesV2  map[string]string
	preventLists   map[string]string
	ulfFilters     map[string]string
	ulfSets        map[string]string
	roles          map[string]string
	users          map[string]string
	groups         map[string]string
	apiClients     map[string]string
	computers      map[string]string // hostname -> uuid
	computerSerial map[string]string // serial -> uuid
	insights       map[string]string // label -> uuid
	connections    map[string]string // identity provider name -> id
}

// NewResolver creates a Resolver for the given Protect client.
func NewResolver(client registry.ProtectClient) *Resolver {
	return &Resolver{client: client}
}

// ResolvePlanID returns the ID for a plan given its name.
func (r *Resolver) ResolvePlanID(ctx context.Context, name string) (string, error) {
	if r.plans == nil {
		plans, err := r.client.ListPlans(ctx)
		if err != nil {
			return "", fmt.Errorf("listing plans: %w", err)
		}
		r.plans = make(map[string]string, len(plans))
		for _, p := range plans {
			r.plans[p.Name] = p.ID
		}
	}
	id, ok := r.plans[name]
	if !ok {
		return "", notFoundf("plan %q not found; use 'protect plans list' to see available names", name)
	}
	return id, nil
}

// ResolveAnalyticUUID returns the UUID for an analytic given its name.
func (r *Resolver) ResolveAnalyticUUID(ctx context.Context, name string) (string, error) {
	if r.analytics == nil {
		items, err := r.client.ListAnalytics(ctx)
		if err != nil {
			return "", fmt.Errorf("listing analytics: %w", err)
		}
		r.analytics = make(map[string]string, len(items))
		for _, a := range items {
			r.analytics[a.Name] = a.UUID
		}
	}
	id, ok := r.analytics[name]
	if !ok {
		return "", notFoundf("analytic %q not found; use 'protect analytics list' to see available names", name)
	}
	return id, nil
}

// ResolveAnalyticSetUUID returns the UUID for an analytic set given its name.
func (r *Resolver) ResolveAnalyticSetUUID(ctx context.Context, name string) (string, error) {
	if r.analyticSets == nil {
		items, err := r.client.ListAnalyticSets(ctx)
		if err != nil {
			return "", fmt.Errorf("listing analytic sets: %w", err)
		}
		r.analyticSets = make(map[string]string, len(items))
		for _, s := range items {
			r.analyticSets[s.Name] = s.UUID
		}
	}
	id, ok := r.analyticSets[name]
	if !ok {
		return "", notFoundf("analytic set %q not found; use 'protect analytic-sets list' to see available names", name)
	}
	return id, nil
}

// ResolveExceptionSetUUID returns the UUID for an exception set given its name.
func (r *Resolver) ResolveExceptionSetUUID(ctx context.Context, name string) (string, error) {
	if r.exceptionSets == nil {
		items, err := r.client.ListExceptionSets(ctx)
		if err != nil {
			return "", fmt.Errorf("listing exception sets: %w", err)
		}
		r.exceptionSets = make(map[string]string, len(items))
		for _, s := range items {
			r.exceptionSets[s.Name] = s.UUID
		}
	}
	id, ok := r.exceptionSets[name]
	if !ok {
		return "", notFoundf("exception set %q not found; use 'protect exception-sets list' to see available names", name)
	}
	return id, nil
}

// ResolveRemovableStorageControlSetID returns the ID for a removable storage control set.
func (r *Resolver) ResolveRemovableStorageControlSetID(ctx context.Context, name string) (string, error) {
	if r.rscSets == nil {
		items, err := r.client.ListRemovableStorageControlSets(ctx)
		if err != nil {
			return "", fmt.Errorf("listing removable storage control sets: %w", err)
		}
		r.rscSets = make(map[string]string, len(items))
		for _, s := range items {
			r.rscSets[s.Name] = s.ID
		}
	}
	id, ok := r.rscSets[name]
	if !ok {
		return "", notFoundf("removable storage control set %q not found; use 'protect removable-storage-control-sets list' to see available names", name)
	}
	return id, nil
}

// ResolveActionConfigID returns the ID for an action config given its name.
func (r *Resolver) ResolveActionConfigID(ctx context.Context, name string) (string, error) {
	if r.actionConfigs == nil {
		items, err := r.client.ListActionConfigs(ctx)
		if err != nil {
			return "", fmt.Errorf("listing action configs: %w", err)
		}
		r.actionConfigs = make(map[string]string, len(items))
		for _, a := range items {
			r.actionConfigs[a.Name] = a.ID
		}
	}
	id, ok := r.actionConfigs[name]
	if !ok {
		return "", notFoundf("action config %q not found; use 'protect action-configs list' to see available names", name)
	}
	return id, nil
}

// ResolveTelemetryV2ID returns the ID for a telemetry v2 config given its name.
func (r *Resolver) ResolveTelemetryV2ID(ctx context.Context, name string) (string, error) {
	if r.telemetriesV2 == nil {
		items, err := r.client.ListTelemetriesV2(ctx)
		if err != nil {
			return "", fmt.Errorf("listing telemetry configurations: %w", err)
		}
		r.telemetriesV2 = make(map[string]string, len(items))
		for _, t := range items {
			r.telemetriesV2[t.Name] = t.ID
		}
	}
	id, ok := r.telemetriesV2[name]
	if !ok {
		return "", notFoundf("telemetry %q not found; use 'protect telemetry list' to see available names", name)
	}
	return id, nil
}

// ResolveCustomPreventListID returns the ID for a custom prevent list given its name.
func (r *Resolver) ResolveCustomPreventListID(ctx context.Context, name string) (string, error) {
	if r.preventLists == nil {
		items, err := r.client.ListCustomPreventLists(ctx)
		if err != nil {
			return "", fmt.Errorf("listing custom prevent lists: %w", err)
		}
		r.preventLists = make(map[string]string, len(items))
		for _, p := range items {
			r.preventLists[p.Name] = p.ID
		}
	}
	id, ok := r.preventLists[name]
	if !ok {
		return "", notFoundf("custom prevent list %q not found; use 'protect custom-prevent-lists list' to see available names", name)
	}
	return id, nil
}

// ResolveUnifiedLoggingFilterUUID returns the UUID for a unified logging filter.
func (r *Resolver) ResolveUnifiedLoggingFilterUUID(ctx context.Context, name string) (string, error) {
	if r.ulfFilters == nil {
		items, err := r.client.ListUnifiedLoggingFilters(ctx)
		if err != nil {
			return "", fmt.Errorf("listing unified logging filters: %w", err)
		}
		r.ulfFilters = make(map[string]string, len(items))
		for _, f := range items {
			r.ulfFilters[f.Name] = f.UUID
		}
	}
	id, ok := r.ulfFilters[name]
	if !ok {
		return "", notFoundf("unified logging filter %q not found; use 'protect unified-logging-filters list' to see available names", name)
	}
	return id, nil
}

// ResolveUnifiedLoggingFilterSetUUID returns the UUID for a unified logging filter set.
func (r *Resolver) ResolveUnifiedLoggingFilterSetUUID(ctx context.Context, name string) (string, error) {
	if r.ulfSets == nil {
		items, err := r.client.ListUnifiedLoggingFilterSets(ctx)
		if err != nil {
			return "", fmt.Errorf("listing unified logging filter sets: %w", err)
		}
		r.ulfSets = make(map[string]string, len(items))
		for _, s := range items {
			r.ulfSets[s.Name] = s.UUID
		}
	}
	id, ok := r.ulfSets[name]
	if !ok {
		return "", notFoundf("unified logging filter set %q not found; use 'protect unified-logging-filter-sets list' to see available names", name)
	}
	return id, nil
}

// ResolveRoleID returns the ID for a role given its name.
func (r *Resolver) ResolveRoleID(ctx context.Context, name string) (string, error) {
	if r.roles == nil {
		items, err := r.client.ListRoles(ctx)
		if err != nil {
			return "", fmt.Errorf("listing roles: %w", err)
		}
		r.roles = make(map[string]string, len(items))
		for _, role := range items {
			r.roles[role.Name] = role.ID
		}
	}
	id, ok := r.roles[name]
	if !ok {
		return "", notFoundf("role %q not found; use 'protect roles list' to see available names", name)
	}
	return id, nil
}

// ResolveUserID returns the ID for a user given their email.
func (r *Resolver) ResolveUserID(ctx context.Context, email string) (string, error) {
	if r.users == nil {
		items, err := r.client.ListUsers(ctx)
		if err != nil {
			return "", fmt.Errorf("listing users: %w", err)
		}
		r.users = make(map[string]string, len(items))
		for _, u := range items {
			r.users[u.Email] = u.ID
		}
	}
	id, ok := r.users[email]
	if !ok {
		return "", notFoundf("user %q not found; use 'protect users list' to see available users", email)
	}
	return id, nil
}

// ResolveGroupID returns the ID for a group given its name.
func (r *Resolver) ResolveGroupID(ctx context.Context, name string) (string, error) {
	if r.groups == nil {
		items, err := r.client.ListGroups(ctx)
		if err != nil {
			return "", fmt.Errorf("listing groups: %w", err)
		}
		r.groups = make(map[string]string, len(items))
		for _, g := range items {
			r.groups[g.Name] = g.ID
		}
	}
	id, ok := r.groups[name]
	if !ok {
		return "", notFoundf("group %q not found; use 'protect groups list' to see available names", name)
	}
	return id, nil
}

// ResolveApiClientID returns the client ID for an API client given its name.
func (r *Resolver) ResolveApiClientID(ctx context.Context, name string) (string, error) {
	if r.apiClients == nil {
		items, err := r.client.ListApiClients(ctx)
		if err != nil {
			return "", fmt.Errorf("listing API clients: %w", err)
		}
		r.apiClients = make(map[string]string, len(items))
		for _, a := range items {
			r.apiClients[a.Name] = a.ClientID
		}
	}
	id, ok := r.apiClients[name]
	if !ok {
		return "", notFoundf("API client %q not found; use 'protect api-clients list' to see available names", name)
	}
	return id, nil
}

// ResolveInsightUUID returns the UUID for an insight given its label.
func (r *Resolver) ResolveInsightUUID(ctx context.Context, label string) (string, error) {
	if r.insights == nil {
		items, err := r.client.ListInsights(ctx)
		if err != nil {
			return "", fmt.Errorf("listing insights: %w", err)
		}
		r.insights = make(map[string]string, len(items))
		for _, i := range items {
			r.insights[i.Label] = i.UUID
		}
	}
	id, ok := r.insights[label]
	if !ok {
		return "", notFoundf("insight %q not found; use 'protect insights list' to see available labels", label)
	}
	return id, nil
}

// ResolveComputerUUID returns the UUID for a computer given its hostname or serial number.
func (r *Resolver) ResolveComputerUUID(ctx context.Context, nameOrSerial string) (string, error) {
	if r.computers == nil {
		items, err := r.client.ListComputers(ctx)
		if err != nil {
			return "", fmt.Errorf("listing computers: %w", err)
		}
		r.computers = make(map[string]string, len(items))
		r.computerSerial = make(map[string]string, len(items))
		for _, c := range items {
			if c.HostName != nil {
				r.computers[*c.HostName] = *c.UUID
			}
			if c.Serial != nil {
				r.computerSerial[*c.Serial] = *c.UUID
			}
		}
	}
	if id, ok := r.computers[nameOrSerial]; ok {
		return id, nil
	}
	if id, ok := r.computerSerial[nameOrSerial]; ok {
		return id, nil
	}
	return "", notFoundf("computer %q not found by hostname or serial; use 'protect computers list' to see available computers", nameOrSerial)
}

// ResolveConnectionID returns the ID for an identity provider connection given
// its name.
func (r *Resolver) ResolveConnectionID(ctx context.Context, name string) (string, error) {
	if r.connections == nil {
		items, err := r.client.ListConnections(ctx)
		if err != nil {
			return "", fmt.Errorf("listing connections: %w", err)
		}
		r.connections = make(map[string]string, len(items))
		for _, c := range items {
			r.connections[c.Name] = c.ID
		}
	}
	id, ok := r.connections[name]
	if !ok {
		return "", notFoundf("connection %q not found; use 'protect connections list' to see available names", name)
	}
	return id, nil
}
