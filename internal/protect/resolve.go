// Copyright 2026, Jamf Software LLC

package protect

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"

	"github.com/Jamf-Concepts/jamf-cli/internal/pickone"
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

	named       map[string][]NamedRef
	computers   []jamfprotect.Computer
	computersOK bool
}

// NewResolver creates a Resolver for the given Protect client.
func NewResolver(client registry.ProtectClient) *Resolver {
	return &Resolver{client: client, named: map[string][]NamedRef{}}
}

// NamedRef is one listed record reduced to the name it is looked up by and
// the identifier the caller acts on.
type NamedRef struct {
	Name string
	ID   string
}

// RefsOf reduces a listing to NamedRefs.
func RefsOf[T any](items []T, name, id func(T) string) []NamedRef {
	refs := make([]NamedRef, len(items))
	for i, it := range items {
		refs[i] = NamedRef{Name: name(it), ID: id(it)}
	}
	return refs
}

// namedKind describes one resource for listing and for the not-found hint.
type namedKind struct {
	noun    string // "analytic set"
	listing string // "analytic sets", as in "listing analytic sets"
	command string // "analytic-sets", as in 'protect analytic-sets list'
	avail   string // "names", as in "to see available names"
}

// PickNamed returns the id of the one ref named exactly name. A name no ref
// carries matches ErrNotFound; a name several refs share is refused with
// every id, since acting on one of them would be a guess.
func PickNamed(refs []NamedRef, name, noun, listCmd, avail string) (string, error) {
	ref, candidates, err := pickone.One(refs, name, pickone.Exact(func(r NamedRef) string { return r.Name }))
	switch {
	case errors.Is(err, pickone.ErrNone):
		return "", notFoundf("%s %q not found; use 'protect %s list' to see available %s", noun, name, listCmd, avail)
	case errors.Is(err, pickone.ErrAmbiguous):
		ids := make([]string, len(candidates))
		for i, c := range candidates {
			ids[i] = c.ID
		}
		return "", fmt.Errorf("%d %ss are named %q (ids %s); rename one so the name is unique", len(candidates), noun, name, strings.Join(ids, ", "))
	}
	return ref.ID, nil
}

func (r *Resolver) resolveNamed(ctx context.Context, k namedKind, name string, list func(context.Context) ([]NamedRef, error)) (string, error) {
	refs, ok := r.named[k.noun]
	if !ok {
		var err error
		refs, err = list(ctx)
		if err != nil {
			return "", fmt.Errorf("listing %s: %w", k.listing, err)
		}
		r.named[k.noun] = refs
	}
	return PickNamed(refs, name, k.noun, k.command, k.avail)
}

// listNamed adapts a client list call and the record's name and id fields.
func listNamed[T any](list func(context.Context) ([]T, error), name, id func(T) string) func(context.Context) ([]NamedRef, error) {
	return func(ctx context.Context) ([]NamedRef, error) {
		items, err := list(ctx)
		if err != nil {
			return nil, err
		}
		return RefsOf(items, name, id), nil
	}
}

// ResolvePlanID returns the ID for a plan given its name.
func (r *Resolver) ResolvePlanID(ctx context.Context, name string) (string, error) {
	return r.resolveNamed(ctx, namedKind{"plan", "plans", "plans", "names"}, name,
		listNamed(r.client.ListPlans, func(p jamfprotect.Plan) string { return p.Name }, func(p jamfprotect.Plan) string { return p.ID }))
}

// ResolveAnalyticUUID returns the UUID for an analytic given its name.
func (r *Resolver) ResolveAnalyticUUID(ctx context.Context, name string) (string, error) {
	return r.resolveNamed(ctx, namedKind{"analytic", "analytics", "analytics", "names"}, name,
		listNamed(r.client.ListAnalytics, func(a jamfprotect.Analytic) string { return a.Name }, func(a jamfprotect.Analytic) string { return a.UUID }))
}

// ResolveAnalyticSetUUID returns the UUID for an analytic set given its name.
func (r *Resolver) ResolveAnalyticSetUUID(ctx context.Context, name string) (string, error) {
	return r.resolveNamed(ctx, namedKind{"analytic set", "analytic sets", "analytic-sets", "names"}, name,
		listNamed(r.client.ListAnalyticSets, func(s jamfprotect.AnalyticSet) string { return s.Name }, func(s jamfprotect.AnalyticSet) string { return s.UUID }))
}

// ResolveExceptionSetUUID returns the UUID for an exception set given its name.
func (r *Resolver) ResolveExceptionSetUUID(ctx context.Context, name string) (string, error) {
	return r.resolveNamed(ctx, namedKind{"exception set", "exception sets", "exception-sets", "names"}, name,
		listNamed(r.client.ListExceptionSets, func(s jamfprotect.ExceptionSetListItem) string { return s.Name }, func(s jamfprotect.ExceptionSetListItem) string { return s.UUID }))
}

// ResolveRemovableStorageControlSetID returns the ID for a removable storage control set.
func (r *Resolver) ResolveRemovableStorageControlSetID(ctx context.Context, name string) (string, error) {
	return r.resolveNamed(ctx, namedKind{"removable storage control set", "removable storage control sets", "removable-storage-control-sets", "names"}, name,
		listNamed(r.client.ListRemovableStorageControlSets, func(s jamfprotect.RemovableStorageControlSet) string { return s.Name }, func(s jamfprotect.RemovableStorageControlSet) string { return s.ID }))
}

// ResolveActionConfigID returns the ID for an action config given its name.
func (r *Resolver) ResolveActionConfigID(ctx context.Context, name string) (string, error) {
	return r.resolveNamed(ctx, namedKind{"action config", "action configs", "action-configs", "names"}, name,
		listNamed(r.client.ListActionConfigs, func(a jamfprotect.ActionConfigListItem) string { return a.Name }, func(a jamfprotect.ActionConfigListItem) string { return a.ID }))
}

// ResolveTelemetryV2ID returns the ID for a telemetry v2 config given its name.
func (r *Resolver) ResolveTelemetryV2ID(ctx context.Context, name string) (string, error) {
	return r.resolveNamed(ctx, namedKind{"telemetry", "telemetry configurations", "telemetry", "names"}, name,
		listNamed(r.client.ListTelemetriesV2, func(t jamfprotect.TelemetryV2) string { return t.Name }, func(t jamfprotect.TelemetryV2) string { return t.ID }))
}

// ResolveCustomPreventListID returns the ID for a custom prevent list given its name.
func (r *Resolver) ResolveCustomPreventListID(ctx context.Context, name string) (string, error) {
	return r.resolveNamed(ctx, namedKind{"custom prevent list", "custom prevent lists", "custom-prevent-lists", "names"}, name,
		listNamed(r.client.ListCustomPreventLists, func(p jamfprotect.CustomPreventList) string { return p.Name }, func(p jamfprotect.CustomPreventList) string { return p.ID }))
}

// ResolveUnifiedLoggingFilterUUID returns the UUID for a unified logging filter.
func (r *Resolver) ResolveUnifiedLoggingFilterUUID(ctx context.Context, name string) (string, error) {
	return r.resolveNamed(ctx, namedKind{"unified logging filter", "unified logging filters", "unified-logging-filters", "names"}, name,
		listNamed(r.client.ListUnifiedLoggingFilters, func(f jamfprotect.UnifiedLoggingFilter) string { return f.Name }, func(f jamfprotect.UnifiedLoggingFilter) string { return f.UUID }))
}

// ResolveUnifiedLoggingFilterSetUUID returns the UUID for a unified logging filter set.
func (r *Resolver) ResolveUnifiedLoggingFilterSetUUID(ctx context.Context, name string) (string, error) {
	return r.resolveNamed(ctx, namedKind{"unified logging filter set", "unified logging filter sets", "unified-logging-filter-sets", "names"}, name,
		listNamed(r.client.ListUnifiedLoggingFilterSets, func(s jamfprotect.UnifiedLoggingFilterSet) string { return s.Name }, func(s jamfprotect.UnifiedLoggingFilterSet) string { return s.UUID }))
}

// ResolveRoleID returns the ID for a role given its name.
func (r *Resolver) ResolveRoleID(ctx context.Context, name string) (string, error) {
	return r.resolveNamed(ctx, namedKind{"role", "roles", "roles", "names"}, name,
		listNamed(r.client.ListRoles, func(role jamfprotect.Role) string { return role.Name }, func(role jamfprotect.Role) string { return role.ID }))
}

// ResolveUserID returns the ID for a user given their email.
func (r *Resolver) ResolveUserID(ctx context.Context, email string) (string, error) {
	return r.resolveNamed(ctx, namedKind{"user", "users", "users", "users"}, email,
		listNamed(r.client.ListUsers, func(u jamfprotect.User) string { return u.Email }, func(u jamfprotect.User) string { return u.ID }))
}

// ResolveGroupID returns the ID for a group given its name.
func (r *Resolver) ResolveGroupID(ctx context.Context, name string) (string, error) {
	return r.resolveNamed(ctx, namedKind{"group", "groups", "groups", "names"}, name,
		listNamed(r.client.ListGroups, func(g jamfprotect.Group) string { return g.Name }, func(g jamfprotect.Group) string { return g.ID }))
}

// ResolveApiClientID returns the client ID for an API client given its name.
func (r *Resolver) ResolveApiClientID(ctx context.Context, name string) (string, error) {
	return r.resolveNamed(ctx, namedKind{"API client", "API clients", "api-clients", "names"}, name,
		listNamed(r.client.ListApiClients, func(a jamfprotect.ApiClient) string { return a.Name }, func(a jamfprotect.ApiClient) string { return a.ClientID }))
}

// ResolveInsightUUID returns the UUID for an insight given its label.
func (r *Resolver) ResolveInsightUUID(ctx context.Context, label string) (string, error) {
	return r.resolveNamed(ctx, namedKind{"insight", "insights", "insights", "labels"}, label,
		listNamed(r.client.ListInsights, func(i jamfprotect.Insight) string { return i.Label }, func(i jamfprotect.Insight) string { return i.UUID }))
}

func derefOr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// DescribeComputer renders a computer as hostname, serial and UUID for
// confirmations and ambiguity refusals.
func DescribeComputer(c jamfprotect.Computer) string {
	return fmt.Sprintf("%s (serial %s, uuid %s)", derefOr(c.HostName), derefOr(c.Serial), derefOr(c.UUID))
}

// ResolveComputer returns the one computer identified by arg, matched as an
// exact UUID, then serial number, then hostname. The first identifier that
// matches decides, so a hostname set to another computer's serial cannot
// capture it, and two computers sharing the deciding value are refused.
func (r *Resolver) ResolveComputer(ctx context.Context, arg string) (jamfprotect.Computer, error) {
	if !r.computersOK {
		items, err := r.client.ListComputers(ctx)
		if err != nil {
			return jamfprotect.Computer{}, fmt.Errorf("listing computers: %w", err)
		}
		r.computers, r.computersOK = items, true
	}
	c, candidates, err := pickone.One(r.computers, arg,
		pickone.Exact(func(c jamfprotect.Computer) string { return derefOr(c.UUID) }),
		pickone.Exact(func(c jamfprotect.Computer) string { return derefOr(c.Serial) }),
		pickone.Exact(func(c jamfprotect.Computer) string { return derefOr(c.HostName) }))
	switch {
	case errors.Is(err, pickone.ErrNone):
		return c, notFoundf("computer %q not found by uuid, serial or hostname; use 'protect computers list' to see available computers", arg)
	case errors.Is(err, pickone.ErrAmbiguous):
		names := make([]string, len(candidates))
		for i, cand := range candidates {
			names[i] = DescribeComputer(cand)
		}
		return c, fmt.Errorf("%q matches %d computers: %s; pass the uuid instead", arg, len(candidates), strings.Join(names, "; "))
	case err != nil:
		return c, err
	case c.UUID == nil:
		return c, fmt.Errorf("computer %s has no uuid", DescribeComputer(c))
	}
	return c, nil
}

// ResolveComputerUUID returns the UUID of the computer ResolveComputer picks.
func (r *Resolver) ResolveComputerUUID(ctx context.Context, arg string) (string, error) {
	c, err := r.ResolveComputer(ctx, arg)
	return derefOr(c.UUID), err
}

// ResolveConnectionID returns the ID for an identity provider connection given
// its name.
func (r *Resolver) ResolveConnectionID(ctx context.Context, name string) (string, error) {
	return r.resolveNamed(ctx, namedKind{"connection", "connections", "connections", "names"}, name,
		listNamed(r.client.ListConnections, func(c jamfprotect.Connection) string { return c.Name }, func(c jamfprotect.Connection) string { return c.ID }))
}
