// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

// protectBackupMetaFile and protectBackupFailuresFile mirror the Jamf Pro
// backup layout so the two products' output is navigable the same way.
const (
	protectBackupMetaFile     = "_meta"
	protectBackupFailuresFile = "_failures"
)

// protectRedacted replaces a secret that the API returns but a backup must not
// carry. It is a visible placeholder rather than an omission so a reader can see
// that the tenant has the value set.
const protectRedacted = "<redacted>"

// protectBackupMeta records what a backup captured and from where, so a restore
// can report the provenance of what it is about to write.
type protectBackupMeta struct {
	Tool      string         `json:"tool" yaml:"tool"`
	Version   string         `json:"version" yaml:"version"`
	Product   string         `json:"product" yaml:"product"`
	CreatedAt string         `json:"created_at" yaml:"created_at"`
	TenantURL string         `json:"tenant_url,omitempty" yaml:"tenant_url,omitempty"`
	Resources []string       `json:"resources" yaml:"resources"`
	Counts    map[string]int `json:"counts" yaml:"counts"`
}

// protectExportEntry is one object destined for one file.
type protectExportEntry struct {
	// Name is the object's identity in its tenant and becomes the file name.
	Name string
	// Doc is the portable document written to disk.
	Doc any
}

// protectResource is one entry in the backup/restore set.
//
// Order encodes the restore dependency chain rather than a preference: members
// must exist before the sets that name them, every set before the plans that
// bind them, roles before the groups that grant them, and groups before the
// users that join them. Resources sharing an Order are independent of each
// other.
type protectResource struct {
	Name  string
	Order int

	// Export lists every object and returns one entry per object. Whether it
	// needs a per-object fetch is a property of the resource: most SDK list
	// queries return the full field set, but listExceptionSets and
	// listActionConfigs return only a summary, so those two must fetch each
	// object individually or the backup silently records empty membership.
	Export func(ctx context.Context, c registry.ProtectClient) ([]protectExportEntry, error)

	// Restore applies one document and returns the object's name.
	Restore func(ctx context.Context, c registry.ProtectClient, r *protect.Resolver, data []byte) (string, error)

	// Singleton marks an org-wide setting: one document, no collection.
	Singleton bool

	// RestoreSkipReason, when non-empty, excludes the resource from restore and
	// explains why. Backup still captures it — a record you cannot replay is
	// still worth having.
	RestoreSkipReason string

	// SensitiveReason, when non-empty, marks a resource whose documents can
	// carry a third-party credential that the API hands back in full. Those
	// files are written 0600 instead of 0644 and the reason is reported, because
	// the documented workflow for a backup directory is to commit it to git and
	// nothing else in the output warns you what you are about to commit.
	SensitiveReason string
}

// upsertByName resolves an object by name and creates it when absent, updates it
// when present. Every Protect resource follows this shape; the generic keeps the
// table from repeating it fifteen times.
//
// Only protect.ErrNotFound means "absent". Any other error is the lookup itself
// failing — a transient list call, an expired token, a permission problem — and
// must abort this object rather than be read as "so create it", which would
// mutate the tenant a different way than the backup describes and report the
// resulting duplicate-name error instead of the real cause.
func upsertByName[I any, R any](
	ctx context.Context,
	name string,
	input I,
	resolve func(context.Context, string) (string, error),
	create func(context.Context, I) (R, error),
	update func(context.Context, string, I) (R, error),
) (string, error) {
	id, err := resolve(ctx, name)
	if err != nil {
		if !errors.Is(err, protect.ErrNotFound) {
			return "", fmt.Errorf("looking up %q: %w", name, err)
		}
		if _, err := create(ctx, input); err != nil {
			return "", err
		}
		return name, nil
	}
	if _, err := update(ctx, id, input); err != nil {
		return "", err
	}
	return name, nil
}

// restoreGroup applies one group. It cannot use upsertByName because the update
// half needs the existing group, not just its ID: createGroup and updateGroup
// disagree about accessGroup on a connection-less local group, and
// protectGroupUpdateSatisfied (protect_groups.go) holds the wire facts and decides
// what an update may carry. 'groups apply' runs the same guard.
func restoreGroup(ctx context.Context, c registry.ProtectClient, input jamfprotect.GroupInput) (string, error) {
	groups, err := c.ListGroups(ctx)
	if err != nil {
		return "", fmt.Errorf("listing groups: %w", err)
	}
	var existing *jamfprotect.Group
	for i := range groups {
		if groups[i].Name == input.Name {
			existing = &groups[i]
			break
		}
	}

	if existing == nil {
		if _, err := c.CreateGroup(ctx, input); err != nil {
			return "", err
		}
		return input.Name, nil
	}

	satisfied, err := protectGroupUpdateSatisfied(existing, input)
	if err != nil {
		return "", err
	}
	if satisfied {
		return fmt.Sprintf("%s (already an access group; updateGroup cannot re-send that flag for a local group)", input.Name), nil
	}

	if _, err := c.UpdateGroup(ctx, existing.ID, input); err != nil {
		return "", err
	}
	return input.Name, nil
}

// decode unmarshals a backup document into T.
func decode[T any](data []byte) (T, error) {
	var v T
	if err := unmarshalInput(data, &v); err != nil {
		return v, err
	}
	return v, nil
}

// protectResources is the backup/restore set, in restore order.
func protectResources() []protectResource {
	return []protectResource{
		// --- Order 10: leaf objects nothing else depends on ---
		{
			Name:  "analytics",
			Order: 10,
			Export: func(ctx context.Context, c registry.ProtectClient) ([]protectExportEntry, error) {
				items, err := c.ListAnalytics(ctx)
				if err != nil {
					return nil, err
				}
				var out []protectExportEntry
				for _, a := range items {
					// Jamf-managed definitions are published centrally and are
					// identical in every tenant; the server refuses to write
					// them. Only the custom ones are a tenant's own data. What a
					// tenant changed about a Jamf analytic is captured by
					// analytic-overrides below.
					if a.Jamf {
						continue
					}
					out = append(out, protectExportEntry{Name: a.Name, Doc: analyticToYAML(a)})
				}
				return out, nil
			},
			Restore: func(ctx context.Context, c registry.ProtectClient, r *protect.Resolver, data []byte) (string, error) {
				input, err := analyticInputFromDocument(data)
				if err != nil {
					return "", err
				}
				return upsertByName(ctx, input.Name, input, r.ResolveAnalyticUUID, c.CreateAnalytic, c.UpdateAnalytic)
			},
		},
		{
			Name:  "unified-logging-filters",
			Order: 10,
			Export: func(ctx context.Context, c registry.ProtectClient) ([]protectExportEntry, error) {
				items, err := c.ListUnifiedLoggingFilters(ctx)
				if err != nil {
					return nil, err
				}
				var out []protectExportEntry
				for _, f := range items {
					out = append(out, protectExportEntry{Name: f.Name, Doc: ulfToYAML(f)})
				}
				return out, nil
			},
			Restore: func(ctx context.Context, c registry.ProtectClient, r *protect.Resolver, data []byte) (string, error) {
				y, err := decode[unifiedLoggingFilterYAML](data)
				if err != nil {
					return "", err
				}
				input := ulfYAMLToInput(y)
				return upsertByName(ctx, input.Name, input, r.ResolveUnifiedLoggingFilterUUID, c.CreateUnifiedLoggingFilter, c.UpdateUnifiedLoggingFilter)
			},
		},
		{
			Name:  "removable-storage-control-sets",
			Order: 10,
			Export: func(ctx context.Context, c registry.ProtectClient) ([]protectExportEntry, error) {
				items, err := c.ListRemovableStorageControlSets(ctx)
				if err != nil {
					return nil, err
				}
				var out []protectExportEntry
				for i := range items {
					out = append(out, protectExportEntry{Name: items[i].Name, Doc: rebuildRSCSInput(&items[i])})
				}
				return out, nil
			},
			Restore: func(ctx context.Context, c registry.ProtectClient, r *protect.Resolver, data []byte) (string, error) {
				input, err := decode[jamfprotect.RemovableStorageControlSetInput](data)
				if err != nil {
					return "", err
				}
				return upsertByName(ctx, input.Name, input, r.ResolveRemovableStorageControlSetID, c.CreateRemovableStorageControlSet, c.UpdateRemovableStorageControlSet)
			},
		},
		{
			Name:  "telemetry",
			Order: 10,
			Export: func(ctx context.Context, c registry.ProtectClient) ([]protectExportEntry, error) {
				items, err := c.ListTelemetriesV2(ctx)
				if err != nil {
					return nil, err
				}
				var out []protectExportEntry
				for i := range items {
					out = append(out, protectExportEntry{Name: items[i].Name, Doc: telemetryToInput(&items[i])})
				}
				return out, nil
			},
			Restore: func(ctx context.Context, c registry.ProtectClient, r *protect.Resolver, data []byte) (string, error) {
				input, err := decode[jamfprotect.TelemetryV2Input](data)
				if err != nil {
					return "", err
				}
				return upsertByName(ctx, input.Name, input, r.ResolveTelemetryV2ID, c.CreateTelemetryV2, c.UpdateTelemetryV2)
			},
		},
		{
			Name:  "custom-prevent-lists",
			Order: 10,
			Export: func(ctx context.Context, c registry.ProtectClient) ([]protectExportEntry, error) {
				items, err := c.ListCustomPreventLists(ctx)
				if err != nil {
					return nil, err
				}
				var out []protectExportEntry
				for i := range items {
					out = append(out, protectExportEntry{Name: items[i].Name, Doc: preventListToInput(&items[i])})
				}
				return out, nil
			},
			Restore: func(ctx context.Context, c registry.ProtectClient, r *protect.Resolver, data []byte) (string, error) {
				input, err := decode[jamfprotect.CustomPreventListInput](data)
				if err != nil {
					return "", err
				}
				return upsertByName(ctx, input.Name, input, r.ResolveCustomPreventListID, c.CreateCustomPreventList, c.UpdateCustomPreventList)
			},
		},
		{
			Name:  "action-configs",
			Order: 10,
			// An HTTP report client's params carry its request headers, and the
			// SDK's query selects `headers { header value }` in full — so an
			// "Authorization: Bearer …" lands in the document verbatim. The
			// values cannot be redacted here because restore needs them to
			// reproduce a working client.
			SensitiveReason: "an HTTP report client's request headers are captured verbatim, " +
				"so a document may contain a bearer token or API key",
			Export: func(ctx context.Context, c registry.ProtectClient) ([]protectExportEntry, error) {
				// listActionConfigs returns id/name/description only — the
				// clients and alertConfig that make up the object need a fetch.
				items, err := c.ListActionConfigs(ctx)
				if err != nil {
					return nil, err
				}
				var out []protectExportEntry
				for _, li := range items {
					full, err := c.GetActionConfig(ctx, li.ID)
					if err != nil {
						return nil, fmt.Errorf("fetching action config %q: %w", li.Name, err)
					}
					doc, err := actionConfigToInput(full)
					if err != nil {
						return nil, err
					}
					out = append(out, protectExportEntry{Name: full.Name, Doc: doc})
				}
				return out, nil
			},
			Restore: func(ctx context.Context, c registry.ProtectClient, r *protect.Resolver, data []byte) (string, error) {
				input, err := decode[jamfprotect.ActionConfigInput](data)
				if err != nil {
					return "", err
				}
				return upsertByName(ctx, input.Name, input, r.ResolveActionConfigID, c.CreateActionConfig, c.UpdateActionConfig)
			},
		},

		// --- Order 15: the tenant overlay on Jamf-managed analytics ---
		{
			Name:  "analytic-overrides",
			Order: 15,
			Export: func(ctx context.Context, c registry.ProtectClient) ([]protectExportEntry, error) {
				items, err := c.ListAnalytics(ctx)
				if err != nil {
					return nil, err
				}
				doc := analyticOverridesDoc{Overrides: []analyticOverride{}}
				for _, a := range items {
					if hasOverride(a) {
						doc.Overrides = append(doc.Overrides, overrideFromAnalytic(a))
					}
				}
				sort.Slice(doc.Overrides, func(i, j int) bool {
					return doc.Overrides[i].Analytic < doc.Overrides[j].Analytic
				})
				if len(doc.Overrides) == 0 {
					return nil, nil
				}
				return []protectExportEntry{{Name: "analytic-overrides", Doc: doc}}, nil
			},
			Singleton: true,
			Restore: func(ctx context.Context, c registry.ProtectClient, _ *protect.Resolver, data []byte) (string, error) {
				doc, err := decode[analyticOverridesDoc](data)
				if err != nil {
					return "", err
				}
				byName, err := listAnalyticsByName(ctx, c)
				if err != nil {
					return "", err
				}
				// Report and continue, the way 'overrides apply' does and the way every
				// other resource in this table does at the object level. Aborting at the
				// first refused entry left the earlier ones written and unreported, the
				// later ones never attempted, and the whole singleton shown as one
				// FAILED line — so a retry was unsafe to reason about.
				var applied, failed int
				for _, o := range doc.Overrides {
					a, ok := byName[o.Analytic]
					if !ok || !a.Jamf {
						fmt.Fprintf(os.Stderr, "  skipped override for %q: absent or not Jamf-managed\n", o.Analytic)
						continue
					}
					input := jamfprotect.InternalAnalyticInput{
						TenantSeverityNull: o.Severity == "",
						TenantActionsNull:  len(o.Actions) == 0,
					}
					if o.Severity != "" {
						sev := o.Severity
						input.TenantSeverity = &sev
					}
					for _, act := range o.Actions {
						params := act.Parameters
						if params == "" {
							params = "{}"
						}
						input.TenantActions = append(input.TenantActions, jamfprotect.AnalyticActionInput{Name: act.Name, Parameters: params})
					}
					if _, err := c.UpdateInternalAnalytic(ctx, a.UUID, input); err != nil {
						fmt.Fprintf(os.Stderr, "  FAILED override for %q: %v\n", o.Analytic, err)
						failed++
						continue
					}
					applied++
				}
				if failed > 0 {
					// Still an error so the resource counts as failed and the run exits
					// non-zero, but the counts say what actually landed.
					return "", fmt.Errorf("%d of %d override(s) failed (%d applied)", failed, len(doc.Overrides), applied)
				}
				return fmt.Sprintf("%d override(s)", applied), nil
			},
		},

		// --- Order 20: sets that name the objects above ---
		{
			// Order 20 rather than alongside the analytics: an exception may
			// target a specific analytic by name, and the name only resolves
			// once that analytic exists in the target.
			Name:  "exception-sets",
			Order: 20,
			Export: func(ctx context.Context, c registry.ProtectClient) ([]protectExportEntry, error) {
				// listExceptionSets returns uuid/name/managed only.
				items, err := c.ListExceptionSets(ctx)
				if err != nil {
					return nil, err
				}
				var out []protectExportEntry
				for _, li := range items {
					if li.Managed {
						continue
					}
					full, err := c.GetExceptionSet(ctx, li.UUID)
					if err != nil {
						return nil, fmt.Errorf("fetching exception set %q: %w", li.Name, err)
					}
					out = append(out, protectExportEntry{Name: full.Name, Doc: exceptionSetToExport(full)})
				}
				return out, nil
			},
			Restore: func(ctx context.Context, c registry.ProtectClient, r *protect.Resolver, data []byte) (string, error) {
				e, err := decode[exceptionSetExport](data)
				if err != nil {
					return "", err
				}
				input, err := exceptionSetExportToInput(ctx, e, r)
				if err != nil {
					return "", err
				}
				return upsertByName(ctx, input.Name, input, r.ResolveExceptionSetUUID, c.CreateExceptionSet, c.UpdateExceptionSet)
			},
		},
		{
			Name:  "analytic-sets",
			Order: 20,
			Export: func(ctx context.Context, c registry.ProtectClient) ([]protectExportEntry, error) {
				items, err := c.ListAnalyticSets(ctx)
				if err != nil {
					return nil, err
				}
				var out []protectExportEntry
				for i := range items {
					if items[i].Managed {
						continue
					}
					out = append(out, protectExportEntry{Name: items[i].Name, Doc: analyticSetToExport(&items[i])})
				}
				return out, nil
			},
			Restore: func(ctx context.Context, c registry.ProtectClient, r *protect.Resolver, data []byte) (string, error) {
				e, err := decode[analyticSetExport](data)
				if err != nil {
					return "", err
				}
				input, err := analyticSetExportToInput(ctx, e, r)
				if err != nil {
					return "", err
				}
				return upsertByName(ctx, input.Name, input, r.ResolveAnalyticSetUUID, c.CreateAnalyticSet, c.UpdateAnalyticSet)
			},
		},
		{
			Name:  "unified-logging-filter-sets",
			Order: 20,
			Export: func(ctx context.Context, c registry.ProtectClient) ([]protectExportEntry, error) {
				items, err := c.ListUnifiedLoggingFilterSets(ctx)
				if err != nil {
					return nil, err
				}
				var out []protectExportEntry
				for i := range items {
					out = append(out, protectExportEntry{Name: items[i].Name, Doc: ulfSetToExport(&items[i])})
				}
				return out, nil
			},
			Restore: func(ctx context.Context, c registry.ProtectClient, r *protect.Resolver, data []byte) (string, error) {
				e, err := decode[ulfSetExport](data)
				if err != nil {
					return "", err
				}
				input, err := ulfSetExportToInput(ctx, e, r)
				if err != nil {
					return "", err
				}
				return upsertByName(ctx, input.Name, input, r.ResolveUnifiedLoggingFilterSetUUID, c.CreateUnifiedLoggingFilterSet, c.UpdateUnifiedLoggingFilterSet)
			},
		},

		// --- Order 30: plans bind every set above ---
		{
			Name:  "plans",
			Order: 30,
			Export: func(ctx context.Context, c registry.ProtectClient) ([]protectExportEntry, error) {
				items, err := c.ListPlans(ctx)
				if err != nil {
					return nil, err
				}
				var out []protectExportEntry
				for i := range items {
					out = append(out, protectExportEntry{Name: items[i].Name, Doc: planToExport(&items[i])})
				}
				return out, nil
			},
			Restore: func(ctx context.Context, c registry.ProtectClient, r *protect.Resolver, data []byte) (string, error) {
				e, err := decode[planExport](data)
				if err != nil {
					return "", err
				}
				input, err := planExportToInput(ctx, e, r, true)
				if err != nil {
					return "", err
				}
				return upsertByName(ctx, input.Name, input, r.ResolvePlanID, c.CreatePlan, c.UpdatePlan)
			},
		},

		// --- Order 40+: access and identity ---
		{
			Name:  "roles",
			Order: 40,
			Export: func(ctx context.Context, c registry.ProtectClient) ([]protectExportEntry, error) {
				items, err := c.ListRoles(ctx)
				if err != nil {
					return nil, err
				}
				var out []protectExportEntry
				for i := range items {
					out = append(out, protectExportEntry{Name: items[i].Name, Doc: roleToInput(&items[i])})
				}
				return out, nil
			},
			Restore: func(ctx context.Context, c registry.ProtectClient, r *protect.Resolver, data []byte) (string, error) {
				input, err := decode[jamfprotect.RoleInput](data)
				if err != nil {
					return "", err
				}
				return upsertByName(ctx, input.Name, input, r.ResolveRoleID, c.CreateRole, c.UpdateRole)
			},
		},
		{
			Name:  "groups",
			Order: 50,
			Export: func(ctx context.Context, c registry.ProtectClient) ([]protectExportEntry, error) {
				items, err := c.ListGroups(ctx)
				if err != nil {
					return nil, err
				}
				var out []protectExportEntry
				for i := range items {
					out = append(out, protectExportEntry{Name: items[i].Name, Doc: groupToExport(&items[i])})
				}
				return out, nil
			},
			Restore: func(ctx context.Context, c registry.ProtectClient, r *protect.Resolver, data []byte) (string, error) {
				input, err := groupInputFromDocument(ctx, data, r)
				if err != nil {
					return "", err
				}
				return restoreGroup(ctx, c, input)
			},
		},
		{
			Name:  "api-clients",
			Order: 60,
			Export: func(ctx context.Context, c registry.ProtectClient) ([]protectExportEntry, error) {
				items, err := c.ListApiClients(ctx)
				if err != nil {
					return nil, err
				}
				var out []protectExportEntry
				for i := range items {
					out = append(out, protectExportEntry{Name: items[i].Name, Doc: apiClientToExport(&items[i])})
				}
				return out, nil
			},
			RestoreSkipReason: "the server issues a new client secret on create and never returns an existing one, " +
				"so a restored client cannot reuse the original credentials — recreate them deliberately with 'protect api-clients apply'",
		},
		{
			Name:  "users",
			Order: 60,
			Export: func(ctx context.Context, c registry.ProtectClient) ([]protectExportEntry, error) {
				items, err := c.ListUsers(ctx)
				if err != nil {
					return nil, err
				}
				var out []protectExportEntry
				for i := range items {
					out = append(out, protectExportEntry{Name: items[i].Email, Doc: userToExport(&items[i])})
				}
				return out, nil
			},
			Restore: func(ctx context.Context, c registry.ProtectClient, r *protect.Resolver, data []byte) (string, error) {
				input, err := userInputFromDocument(ctx, data, r)
				if err != nil {
					return "", err
				}
				return upsertByName(ctx, input.Email, input, r.ResolveUserID, c.CreateUser, c.UpdateUser)
			},
		},

		// --- Order 65: compliance posture ---
		{
			// Insights are a fixed Jamf-published catalogue; a tenant only
			// chooses which are enabled. So the document carries the enabled
			// labels rather than the insights themselves, and restore flips the
			// target's own copies to match.
			Name:      "insights",
			Order:     65,
			Singleton: true,
			Export: func(ctx context.Context, c registry.ProtectClient) ([]protectExportEntry, error) {
				items, err := c.ListInsights(ctx)
				if err != nil {
					return nil, err
				}
				doc := insightStateDoc{Enabled: []string{}, Disabled: []string{}}
				for _, i := range items {
					if i.Enabled {
						doc.Enabled = append(doc.Enabled, i.Label)
					} else {
						doc.Disabled = append(doc.Disabled, i.Label)
					}
				}
				sort.Strings(doc.Enabled)
				sort.Strings(doc.Disabled)
				return []protectExportEntry{{Name: "insights", Doc: doc}}, nil
			},
			Restore: func(ctx context.Context, c registry.ProtectClient, _ *protect.Resolver, data []byte) (string, error) {
				doc, err := decode[insightStateDoc](data)
				if err != nil {
					return "", err
				}
				items, err := c.ListInsights(ctx)
				if err != nil {
					return "", err
				}
				byLabel := make(map[string]jamfprotect.Insight, len(items))
				for _, i := range items {
					byLabel[i.Label] = i
				}
				want := make(map[string]bool, len(doc.Enabled)+len(doc.Disabled))
				for _, l := range doc.Enabled {
					want[l] = true
				}
				for _, l := range doc.Disabled {
					want[l] = false
				}

				// Sorted, because iterating the map made both the order of live
				// mutations and the order of the log lines vary run to run — against the
				// determinism the rest of restore is built on.
				labels := make([]string, 0, len(want))
				for l := range want {
					labels = append(labels, l)
				}
				sort.Strings(labels)

				var changed int
				for _, label := range labels {
					enabled := want[label]
					insight, ok := byLabel[label]
					if !ok {
						fmt.Fprintf(os.Stderr, "  skipped insight %q: not present in this tenant\n", label)
						continue
					}
					// Only write the ones that differ. The catalogue runs to
					// hundreds, and every call is a mutation on a live tenant.
					if insight.Enabled == enabled {
						continue
					}
					if _, err := c.UpdateInsightStatus(ctx, insight.UUID, enabled); err != nil {
						return "", fmt.Errorf("setting insight %q: %w", label, err)
					}
					changed++
				}
				return fmt.Sprintf("%d insight(s) changed", changed), nil
			},
		},

		// --- Order 70: org-wide settings ---
		{
			Name:      "config-freeze",
			Order:     70,
			Singleton: true,
			Export: func(ctx context.Context, c registry.ProtectClient) ([]protectExportEntry, error) {
				got, err := c.GetConfigFreeze(ctx)
				if err != nil {
					return nil, err
				}
				return []protectExportEntry{{Name: "config-freeze", Doc: got}}, nil
			},
			Restore: func(ctx context.Context, c registry.ProtectClient, _ *protect.Resolver, data []byte) (string, error) {
				cfg, err := decode[jamfprotect.ChangeManagementConfig](data)
				if err != nil {
					return "", err
				}
				// Writing the value the tenant already holds is refused —
				// disabling a freeze that is not on answers "Tenant '...' is not
				// in a change freeze". So compare first and only write a real
				// change, which is what makes replaying a backup idempotent.
				current, err := c.GetConfigFreeze(ctx)
				if err != nil {
					return "", err
				}
				if current.ConfigFreeze == cfg.ConfigFreeze {
					return fmt.Sprintf("config-freeze already %t", cfg.ConfigFreeze), nil
				}
				if _, err := c.UpdateOrganizationConfigFreeze(ctx, cfg.ConfigFreeze); err != nil {
					return "", err
				}
				return fmt.Sprintf("config-freeze=%t", cfg.ConfigFreeze), nil
			},
		},
		{
			// Identity provider connections have no create mutation, so they can
			// be captured but never replayed. Worth capturing anyway: a user or
			// group naming a connection restores only where that name exists, and
			// connection names are tenant-specific.
			Name:      "connections",
			Order:     70,
			Singleton: true,
			Export: func(ctx context.Context, c registry.ProtectClient) ([]protectExportEntry, error) {
				items, err := c.ListConnections(ctx)
				if err != nil {
					return nil, err
				}
				names := make([]string, 0, len(items))
				for _, cn := range items {
					names = append(names, cn.Name)
				}
				sort.Strings(names)
				return []protectExportEntry{{Name: "connections", Doc: map[string][]string{"connections": names}}}, nil
			},
			RestoreSkipReason: "identity provider connections have no create API — configure them in the target first, " +
				"then restore the users and groups that reference them by name",
		},
		{
			Name:      "data-retention",
			Order:     70,
			Singleton: true,
			Export: func(ctx context.Context, c registry.ProtectClient) ([]protectExportEntry, error) {
				got, err := c.GetDataRetention(ctx)
				if err != nil {
					return nil, err
				}
				return []protectExportEntry{{Name: "data-retention", Doc: dataRetentionToInput(got)}}, nil
			},
			Restore: func(ctx context.Context, c registry.ProtectClient, _ *protect.Resolver, data []byte) (string, error) {
				input, err := decode[jamfprotect.DataRetentionInput](data)
				if err != nil {
					return "", err
				}
				// Retention updates are rate-limited to once per 24 hours, so an
				// unconditional write made a re-run report a failure for a
				// resource already in the desired state. Compare first, the same
				// way config-freeze and insights do.
				current, err := c.GetDataRetention(ctx)
				if err != nil {
					return "", err
				}
				if dataRetentionToInput(current) == input {
					return "data-retention already matches", nil
				}
				if _, err := c.UpdateDataRetention(ctx, input); err != nil {
					return "", err
				}
				return "data-retention", nil
			},
		},
		{
			Name:      "data-forwarding",
			Order:     70,
			Singleton: true,
			Export: func(ctx context.Context, c registry.ProtectClient) ([]protectExportEntry, error) {
				got, err := c.GetDataForwarding(ctx)
				if err != nil {
					return nil, err
				}
				// Legacy Sentinel returns its sharedKey in cleartext (only
				// SentinelV2 uses the secretExists boolean). This resource is
				// never replayed, so redacting costs nothing and keeps the
				// secret out of the backup.
				return []protectExportEntry{{Name: "data-forwarding", Doc: redactDataForwarding(got)}}, nil
			},
			SensitiveReason: "the S3 cloudformation blob embeds a tenant-specific IAM ExternalId " +
				"(the Sentinel shared key is redacted)",
			RestoreSkipReason: "the forwarding settings response is not the update input shape and carries " +
				"third-party credentials the API never returns — reapply with 'protect data-forwarding update'",
		},
	}
}

// protectDefaultObjects names objects every Jamf Protect tenant is provisioned
// with. They are skipped on restore by default.
//
// Skipping them is safe precisely because references resolve by name: a restored
// group naming "Full Admin" binds to the target's own built-in role, so the role
// document itself never needs replaying. Replaying it would at best be a no-op
// and at worst overwrite a product-versioned default with an older tenant's copy.
//
// Built-in roles are identifiable beyond their names — "Read Only" is id 1 and
// "Full Admin" id 2 in every tenant observed, while custom roles get high
// sequential ids — but restore only ever sees the document, and the exports
// strip ids by design, so the check here is by name. --include-defaults replays
// them anyway.
var protectDefaultObjects = map[string]map[string]bool{
	"roles": {
		"Full Admin": true,
		"Read Only":  true,
	},
	"groups": {
		"Default": true,
	},
	"analytic-sets": {
		// Every tenant's catch-all set. Its membership is the full Jamf-published
		// analytic list, which the target already has.
		"Default Analytic Set": true,
	},
}

// isProtectDefaultObject reports whether an object is a tenant default.
func isProtectDefaultObject(resource, name string) bool {
	return protectDefaultObjects[resource][name]
}

// insightStateDoc records which compliance insights a tenant has enabled. Both
// lists are written so a restore is explicit in each direction rather than
// treating "absent" as "disabled".
type insightStateDoc struct {
	Enabled  []string `json:"enabled" yaml:"enabled"`
	Disabled []string `json:"disabled" yaml:"disabled"`
}

// protectFileNameSafe makes an object name usable as a file name. Protect names
// are free text and routinely contain slashes and colons.
var protectFileNameUnsafe = regexp.MustCompile(`[^\w.@-]+`)

func protectFileNameSafe(name string) string {
	safe := protectFileNameUnsafe.ReplaceAllString(name, "_")
	safe = strings.Trim(safe, "_")
	if safe == "" {
		safe = "unnamed"
	}
	return safe
}

// protectNameAllocator hands out a distinct file name per object.
//
// protectFileNameSafe is lossy in two directions, and both collide silently
// without this: it collapses every run of illegal characters to one "_", so
// "Alert: High" and "Alert - High" arrive at the same name, and a
// case-insensitive filesystem — the macOS default — folds "MyPlan" and "myplan"
// together too. os.WriteFile truncates, so the loser of a collision is a
// backed-up object that simply is not in the backup.
//
// The discriminator is derived from the object's own name so it is stable across
// runs: a backup directory under version control keeps diffing cleanly.
type protectNameAllocator struct {
	used map[string]string // lowercased file name -> object name that claimed it
}

func newProtectNameAllocator() *protectNameAllocator {
	return &protectNameAllocator{used: map[string]string{}}
}

// allocate returns the file name (without extension) to use for objectName, and
// whether it had to be disambiguated.
func (a *protectNameAllocator) allocate(objectName string) (string, bool) {
	base := protectFileNameSafe(objectName)
	if owner, taken := a.used[strings.ToLower(base)]; !taken || owner == objectName {
		a.used[strings.ToLower(base)] = objectName
		return base, false
	}
	sum := sha256.Sum256([]byte(objectName))
	candidate := base + "-" + hex.EncodeToString(sum[:])[:8]
	// A second collision needs two identical object names in one resource, which
	// the tenant cannot produce, but fall through deterministically rather than
	// overwrite if it ever happens.
	for i := 0; ; i++ {
		key := strings.ToLower(candidate)
		if owner, taken := a.used[key]; !taken || owner == objectName {
			a.used[key] = objectName
			return candidate, true
		}
		candidate = fmt.Sprintf("%s-%s-%d", base, hex.EncodeToString(sum[:])[:8], i)
	}
}

// protectSplitTokens parses a comma-separated flag value.
func protectSplitTokens(v string) []string {
	var out []string
	for _, tok := range strings.Split(v, ",") {
		if tok = strings.TrimSpace(tok); tok != "" {
			out = append(out, tok)
		}
	}
	return out
}

// protectObjectNameFromFile reads an object's own name out of a backup document.
//
// The file name cannot be used: protectFileNameSafe rewrites characters that are
// illegal in a path, so "Full Admin" is stored as "Full_Admin.yaml" and the
// original is only recoverable from the document body.
func protectObjectNameFromFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var probe map[string]any
	if err := unmarshalInput(data, &probe); err != nil {
		return ""
	}
	for _, key := range []string{"name", "Name", "email", "Email"} {
		if v, ok := probe[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// protectSelectResources filters the set by an include list and an exclude list,
// returning it sorted into restore order. include is an allowlist ("only
// these"); exclude removes from whatever include produced, so the two compose.
func protectSelectResources(filter, exclude string) ([]protectResource, error) {
	all := protectResources()
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Order != all[j].Order {
			return all[i].Order < all[j].Order
		}
		return all[i].Name < all[j].Name
	})

	known := make([]string, 0, len(all))
	valid := map[string]bool{}
	for _, r := range all {
		known = append(known, r.Name)
		valid[r.Name] = true
	}
	unknownErr := func(tokens []string, flag string) error {
		var unknown []string
		for _, t := range tokens {
			if !valid[t] {
				unknown = append(unknown, t)
			}
		}
		if len(unknown) == 0 {
			return nil
		}
		sort.Strings(unknown)
		return fmt.Errorf("unknown resource(s) in --%s: %s\nknown resources: %s",
			flag, strings.Join(unknown, ", "), strings.Join(known, ", "))
	}

	includes := protectSplitTokens(filter)
	if err := unknownErr(includes, "resources"); err != nil {
		return nil, err
	}
	excludes := protectSplitTokens(exclude)
	if err := unknownErr(excludes, "exclude"); err != nil {
		return nil, err
	}

	included := map[string]bool{}
	for _, t := range includes {
		included[t] = true
	}
	excluded := map[string]bool{}
	for _, t := range excludes {
		excluded[t] = true
	}

	var out []protectResource
	for _, r := range all {
		if len(included) > 0 && !included[r.Name] {
			continue
		}
		if excluded[r.Name] {
			continue
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no resources selected: --resources and --exclude leave nothing to do")
	}
	return out, nil
}

func protectMarshal(v any, format string) ([]byte, error) {
	if format == "json" {
		return json.MarshalIndent(v, "", "  ")
	}
	// Two-space indent to match printExport, so a backup file and the output of
	// the matching `export` command are byte-identical for the same object and
	// can be diffed against each other. yaml.Marshal's default is four.
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// protectResourceListHelp renders the resource vocabulary for --help.
//
// It is generated from protectResources() rather than written out, because the
// set changes and a hand-maintained list in help text drifts silently. Without
// it the only ways to discover the vocabulary are shell completion and passing a
// wrong value to read the error — neither of which is `--help`.
//
// forRestore marks the resources backup captures but restore will not replay, so
// the reader can see why asking for one had no effect.
func protectResourceListHelp(forRestore bool) string {
	all := protectResources()
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })

	var b strings.Builder
	b.WriteString("Resources accepted by --resources and --exclude:\n")
	for _, r := range all {
		switch {
		case forRestore && r.RestoreSkipReason != "":
			fmt.Fprintf(&b, "  %-32s (captured by backup, never replayed)\n", r.Name)
		case r.Singleton:
			fmt.Fprintf(&b, "  %-32s (one document, not a collection)\n", r.Name)
		default:
			fmt.Fprintf(&b, "  %s\n", r.Name)
		}
	}
	return b.String()
}

// protectRestoreExts are the extensions restore will pick up. Pruning considers
// exactly this set, because it is precisely what a later restore would apply.
var protectRestoreExts = []string{".yaml", ".yml", ".json"}

// readProtectBackupMeta reads the _meta document an earlier run left in dir, in
// whichever of the two formats it was written. The bool reports whether one was
// found and parsed — a missing or unreadable manifest is not an error, it just
// means the directory's provenance is unknown.
func readProtectBackupMeta(dir string) (protectBackupMeta, bool) {
	for _, ext := range protectRestoreExts {
		data, err := os.ReadFile(filepath.Join(dir, protectBackupMetaFile+ext))
		if err != nil {
			continue
		}
		var meta protectBackupMeta
		if err := unmarshalInput(data, &meta); err != nil {
			continue
		}
		return meta, true
	}
	return protectBackupMeta{}, false
}

// protectPruneStale removes backup documents this run did not write.
//
// Without it a backup directory is the union of every run that ever wrote to it,
// and because restore applies whatever it finds, an object deleted from the tenant
// is silently recreated by the next restore. Switching --format has the same
// effect in reverse: the old .yaml files stay beside the new .json ones and both
// get applied.
//
// 'pro backup' does not do this. The difference is deliberate rather than drift:
// Pro has no restore, so a stale file there only misleads a diff, while here it
// resurrects an object.
//
// The rails matter more than the feature. Only files this command could itself
// have written are ever considered — inside a directory named after one of its own
// resources, or for a singleton the one file that resource owns — and only with an
// extension restore would read. A resource whose export failed is never pruned:
// the true object set is unknown, and deleting on a failed read is exactly how a
// backup tool loses the data it exists to protect.
func protectPruneStale(dir string, res protectResource, kept map[string]bool) ([]string, error) {
	var candidates []string

	if res.Singleton {
		// A singleton lives at the root next to _meta and every other
		// singleton, so only the file this resource owns is a candidate.
		for _, ext := range protectRestoreExts {
			candidates = append(candidates, res.Name+ext)
		}
	} else {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			for _, want := range protectRestoreExts {
				if ext == want {
					candidates = append(candidates, e.Name())
					break
				}
			}
		}
	}

	var pruned []string
	for _, name := range candidates {
		if kept[name] {
			continue
		}
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			continue // never existed; only relevant for the singleton guesses
		}
		if err := os.Remove(path); err != nil {
			return pruned, fmt.Errorf("removing stale %s: %w", path, err)
		}
		// A singleton's file sits at the tree root, not under a directory named
		// after the resource, so reporting insights/insights.json would name a
		// path that does not exist.
		if res.Singleton {
			pruned = append(pruned, name)
		} else {
			pruned = append(pruned, filepath.Join(res.Name, name))
		}
	}
	sort.Strings(pruned)
	return pruned, nil
}

// --- backup ---

func newProtectBackupCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		outputDir string
		format    string
		resources string
		exclude   string
		noPrune   bool
	)

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Export all Jamf Protect configuration to a local directory",
		Long: `Export configuration from a Jamf Protect tenant to a local directory.

Each object is written as its own YAML or JSON file under a per-resource
subdirectory, in the same portable form the matching 'export' command produces —
cross-resource references are names, not IDs, so a backup can be restored into a
different tenant.

Jamf-managed content is skipped: those definitions are published centrally, are
identical in every tenant, and the server refuses to write them. What a tenant
changed *about* them is captured separately as analytic-overrides.

Partial failures are tolerated. A resource that fails to export is recorded in
_failures.yaml and the run continues, so one broken resource cannot cost you the
rest of the backup — but the command exits non-zero so a scheduled job can tell
an incomplete backup from a good one. The global --allow-partial-failure exits 0
instead, the same way it does for 'pro backup'.

Documents that can carry a third-party credential — an HTTP action config's
request headers, the data forwarding settings — are written 0600 and reported, so
you know what is in the tree before committing it to version control.

Note that --output is this command's destination directory, so the global -o output
format flag is unavailable here (--format picks the file encoding instead). This
matches 'pro backup'.

Documents left by an earlier run that no longer match the tenant are removed, and
each removal is reported. The run refuses to prune a directory whose _meta names a
different tenant, because pruning is keyed on this tenant's object set and would
otherwise delete the other tenant's documents; --no-prune writes alongside them. Without that a backup directory is the union of every run
that ever wrote to it, and because 'protect restore' applies whatever it finds, an
object you deleted from the tenant would be silently recreated. Only files this
command could have written are ever considered, and a resource that failed to
export is never pruned. --no-prune keeps them.

` + protectResourceListHelp(false),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runProtectBackup(cmd.Context(), cliCtx, outputDir, format, resources, exclude, noPrune)
		},
	}

	cmd.Flags().StringVar(&outputDir, "output", "", "destination directory (required)")
	cmd.Flags().StringVar(&format, "format", "yaml", "file format: yaml or json")
	cmd.Flags().StringVar(&resources, "resources", "", "comma-separated allowlist of resources to capture (default: all)")
	cmd.Flags().StringVar(&exclude, "exclude", "", "comma-separated resources to skip (e.g. users,api-clients)")
	cmd.Flags().BoolVar(&noPrune, "no-prune", false, "keep documents from earlier runs that no longer match the tenant (they would be re-applied by 'protect restore')")
	_ = cmd.MarkFlagRequired("output")
	_ = cmd.RegisterFlagCompletionFunc("format", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"yaml", "json"}, cobra.ShellCompDirectiveNoFileComp
	})
	// Both flags take the same vocabulary and compose, so both complete.
	_ = cmd.RegisterFlagCompletionFunc("resources", protectResourceCompletion)
	_ = cmd.RegisterFlagCompletionFunc("exclude", protectResourceCompletion)

	return cmd
}

func protectResourceCompletion(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	all := protectResources()
	names := make([]string, 0, len(all))
	for _, r := range all {
		names = append(names, r.Name)
	}
	sort.Strings(names)
	return names, cobra.ShellCompDirectiveNoFileComp
}

// allowPartialFailure is the root persistent flag, read directly the way
// pro_backup.go does. Declaring a local flag of the same name shadowed it, so
// passing it in the global position was silently ignored.
func runProtectBackup(ctx context.Context, cliCtx *registry.CLIContext, outputDir, format, filter, exclude string, noPrune bool) error {
	if format != "yaml" && format != "json" {
		return fmt.Errorf("invalid --format %q: must be yaml or json", format)
	}
	selected, err := protectSelectResources(filter, exclude)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	// Pruning is destructive and keyed on "the object set of the tenant being
	// backed up right now", so pointing the command at a directory that holds a
	// different tenant's backup would delete that tenant's documents for every
	// object this one does not have. The run records its tenant in _meta; refuse
	// when an existing manifest names a different one. Nothing else about the
	// backup depends on the directory's history, so --no-prune makes it safe.
	if !noPrune {
		if prior, ok := readProtectBackupMeta(outputDir); ok &&
			prior.TenantURL != "" && cliCtx.ProtectURL != "" && prior.TenantURL != cliCtx.ProtectURL {
			return fmt.Errorf("%s holds a backup of %s, not %s — pruning would delete that tenant's documents; "+
				"use a different --output, or pass --no-prune to write alongside them",
				outputDir, prior.TenantURL, cliCtx.ProtectURL)
		}
	}

	ext := "." + format
	failures := map[string]string{}
	counts := map[string]int{}
	captured := make([]string, 0, len(selected))
	var sensitive []string
	var pruned []string

	for _, res := range selected {
		entries, err := res.Export(ctx, cliCtx.ProtectClient)
		if err != nil {
			failures[res.Name] = err.Error()
			fmt.Fprintf(os.Stderr, "WARNING: %s failed: %v\n", res.Name, err)
			continue
		}
		// Report empty resources too. A silently absent line reads as "not
		// checked" when it actually means "checked, nothing there".
		if len(entries) == 0 {
			counts[res.Name] = 0
			captured = append(captured, res.Name)
			// A resource that used to hold objects and now holds none still has
			// its files on disk, and restore would re-create every one of them.
			if !noPrune {
				dir := outputDir
				if !res.Singleton {
					dir = filepath.Join(outputDir, res.Name)
				}
				gone, err := protectPruneStale(dir, res, nil)
				if err != nil {
					return err
				}
				pruned = append(pruned, gone...)
			}
			if !quiet {
				fmt.Fprintf(os.Stderr, "%-32s %d\n", res.Name, 0)
			}
			continue
		}

		// A document that can carry a credential is not world-readable, and the
		// operator is told which resource that was before they commit the tree.
		mode := os.FileMode(0o644)
		if res.SensitiveReason != "" {
			mode = 0o600
			sensitive = append(sensitive, fmt.Sprintf("%s: %s", res.Name, res.SensitiveReason))
		}

		dir := outputDir
		if !res.Singleton {
			dir = filepath.Join(outputDir, res.Name)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("creating %s directory: %w", res.Name, err)
			}
		}

		written := 0
		names := newProtectNameAllocator()
		kept := map[string]bool{}
		marshalFailed := false
		for _, e := range entries {
			data, err := protectMarshal(e.Doc, format)
			if err != nil {
				failures[res.Name+"/"+e.Name] = err.Error()
				marshalFailed = true
				continue
			}
			fileName, disambiguated := names.allocate(e.Name)
			path := filepath.Join(dir, fileName+ext)
			if err := os.WriteFile(path, data, mode); err != nil {
				return fmt.Errorf("writing %s: %w", path, err)
			}
			// os.WriteFile passes perm to OpenFile(O_CREATE|O_TRUNC), so it only
			// applies when the file is created. A re-run into an existing tree —
			// a git clone of a backup repo hands every file back 0644, since git
			// records no non-exec permissions — would otherwise keep the loose
			// mode while the summary below reports 0600.
			if mode != 0o644 {
				if err := os.Chmod(path, mode); err != nil {
					return fmt.Errorf("tightening permissions on %s: %w", path, err)
				}
			}
			kept[fileName+ext] = true
			if disambiguated && !quiet {
				fmt.Fprintf(os.Stderr, "  note: %q shares a file name with another object; written as %s%s\n", e.Name, fileName, ext)
			}
			written++
		}
		counts[res.Name] = written

		// Only prune when every object in the resource was written. If one failed
		// to marshal, its existing file on disk is the better record and deleting
		// it would turn a partial failure into data loss.
		if !noPrune && !marshalFailed {
			gone, err := protectPruneStale(dir, res, kept)
			if err != nil {
				return err
			}
			pruned = append(pruned, gone...)
		}
		captured = append(captured, res.Name)
		if !quiet {
			fmt.Fprintf(os.Stderr, "%-32s %d\n", res.Name, written)
		}
	}

	meta := protectBackupMeta{
		Tool:      "jamf-cli",
		Version:   cliVersion,
		Product:   "protect",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		TenantURL: cliCtx.ProtectURL,
		Resources: captured,
		Counts:    counts,
	}
	metaData, err := protectMarshal(meta, format)
	if err != nil {
		return fmt.Errorf("marshalling metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, protectBackupMetaFile+ext), metaData, 0o644); err != nil {
		return fmt.Errorf("writing metadata: %w", err)
	}

	total := 0
	for _, n := range counts {
		total += n
	}

	if len(pruned) > 0 && !quiet {
		fmt.Fprintf(os.Stderr, "\nPruned %d document(s) that no longer match the tenant:\n", len(pruned))
		for _, f := range pruned {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		fmt.Fprintf(os.Stderr, "Use --no-prune to keep them (note that 'protect restore' would re-apply them)\n")
	}

	if len(sensitive) > 0 {
		fmt.Fprintf(os.Stderr, "\nWARNING: %d resource(s) can carry credentials and were written 0600:\n", len(sensitive))
		for _, sr := range sensitive {
			fmt.Fprintf(os.Stderr, "  %s\n", sr)
		}
		fmt.Fprintf(os.Stderr, "Review these before committing %s to version control, "+
			"or re-run with --exclude action-configs,data-forwarding\n", outputDir)
	}

	if len(failures) > 0 {
		// The next line tells the operator to read this file, so a failure to
		// write it cannot be swallowed — that would send them to a file that is
		// missing or holds an earlier run's failures.
		failData, err := protectMarshal(failures, format)
		if err != nil {
			return fmt.Errorf("marshalling failure manifest: %w", err)
		}
		if err := os.WriteFile(filepath.Join(outputDir, protectBackupFailuresFile+ext), failData, 0o644); err != nil {
			return fmt.Errorf("writing failure manifest: %w", err)
		}
		fmt.Fprintf(os.Stderr, "\nBacked up %d object(s) to %s, %d failure(s) — see %s%s\n",
			total, outputDir, len(failures), protectBackupFailuresFile, ext)
		if allowPartialFailure && total > 0 {
			fmt.Fprintf(os.Stderr, "warning: backup completed with %d failure(s); continuing (--allow-partial-failure)\n", len(failures))
			return nil
		}
		// A backup that exits 0 with a resource missing is indistinguishable from
		// a good one to the scheduled job that runs it, which is the whole point
		// of having an exit code. 'pro backup' makes the same call.
		msg := fmt.Sprintf("backup completed with %d failure(s) (see %s%s)", len(failures), protectBackupFailuresFile, ext)
		return exitcode.PartialOrPropagate(total, len(failures), nil, msg)
	}

	fmt.Fprintf(os.Stderr, "\nBacked up %d object(s) to %s\n", total, outputDir)
	return nil
}

// --- restore ---

func newProtectRestoreCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		inputDir        string
		resources       string
		exclude         string
		includeDefaults bool
		yes             bool
	)

	cmd := &cobra.Command{
		Use:         "restore",
		Short:       "Apply a Jamf Protect backup directory to a tenant",
		Annotations: map[string]string{"jamf:destructive": "true"},
		Long: `Apply a directory produced by 'protect backup' to a Jamf Protect tenant.

Objects are applied in dependency order — filters and analytics before the sets
that name them, every set before the plans that bind them, roles before groups
before users — because each reference is resolved by name against the target
tenant as it is applied.

Existing objects of the same name are updated, absent ones created. Nothing is
ever deleted: an object present in the tenant but absent from the backup is left
alone.

Two resources are captured by backup but not replayed here, because replaying
them cannot reproduce the original: API clients (the server issues a new secret
on create) and data forwarding (its settings carry third-party credentials the
API never returns). Both are reported when skipped.

The global -n, --dry-run reports what would be applied without calling the API.

` + protectResourceListHelp(true),
		RunE: func(cmd *cobra.Command, _ []string) error {
			// dryRun is the root persistent flag, read directly the way
			// allowPartialFailure is. Declaring a local flag of the same name made
			// Cobra drop the inherited one, taking -n with it, so 'restore -n'
			// failed with "unknown shorthand flag".
			return runProtectRestore(cmd.Context(), cliCtx, inputDir, resources, exclude, includeDefaults, dryRun, yes)
		},
	}

	cmd.Flags().StringVar(&inputDir, "input", "", "backup directory to restore from (required)")
	cmd.Flags().StringVar(&resources, "resources", "", "comma-separated allowlist of resources to apply (default: all)")
	cmd.Flags().StringVar(&exclude, "exclude", "", "comma-separated resources to skip (e.g. users,roles)")
	cmd.Flags().BoolVar(&includeDefaults, "include-defaults", false, "also apply objects every tenant is provisioned with (built-in roles, the Default group, the Default Analytic Set)")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	_ = cmd.MarkFlagRequired("input")
	_ = cmd.RegisterFlagCompletionFunc("resources", protectResourceCompletion)
	_ = cmd.RegisterFlagCompletionFunc("exclude", protectResourceCompletion)

	return cmd
}

// protectRestoreFile is one document found on disk, paired with its resource.
type protectRestoreFile struct {
	Resource protectResource
	Path     string
}

// collectProtectRestoreFiles walks the backup directory in restore order.
func collectProtectRestoreFiles(inputDir string, selected []protectResource, includeDefaults bool) ([]protectRestoreFile, []string, error) {
	var files []protectRestoreFile
	var skipped []string

	for _, res := range selected {
		if res.RestoreSkipReason != "" {
			skipped = append(skipped, fmt.Sprintf("%s: %s", res.Name, res.RestoreSkipReason))
			continue
		}

		if res.Singleton {
			// Same extension set as pruning, so backup and restore agree on what a
			// restorable document is. Accepting only .yaml/.json here made a
			// hand-written insights.yml one that backup deletes and restore ignores.
			for _, ext := range protectRestoreExts {
				path := filepath.Join(inputDir, res.Name+ext)
				if _, err := os.Stat(path); err == nil {
					files = append(files, protectRestoreFile{Resource: res, Path: path})
					break
				}
			}
			continue
		}

		dir := filepath.Join(inputDir, res.Name)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, nil, fmt.Errorf("reading %s: %w", dir, err)
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if slices.Contains(protectRestoreExts, ext) {
				names = append(names, e.Name())
			}
		}
		// Deterministic order so a restore is reproducible and its log diffable.
		sort.Strings(names)
		for _, n := range names {
			objectName := protectObjectNameFromFile(filepath.Join(dir, n))
			if !includeDefaults && isProtectDefaultObject(res.Name, objectName) {
				skipped = append(skipped, fmt.Sprintf("%s/%s: a tenant default, already present in the target (--include-defaults to apply anyway)", res.Name, objectName))
				continue
			}
			files = append(files, protectRestoreFile{Resource: res, Path: filepath.Join(dir, n)})
		}
	}
	return files, skipped, nil
}

func runProtectRestore(ctx context.Context, cliCtx *registry.CLIContext, inputDir, filter, exclude string, includeDefaults, dryRun, yes bool) error {
	info, err := os.Stat(inputDir)
	if err != nil {
		return fmt.Errorf("reading backup directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", inputDir)
	}

	selected, err := protectSelectResources(filter, exclude)
	if err != nil {
		return err
	}
	files, skipped, err := collectProtectRestoreFiles(inputDir, selected, includeDefaults)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no restorable documents found under %s", inputDir)
	}

	for _, s := range skipped {
		fmt.Fprintf(os.Stderr, "Not restored — %s\n", s)
	}

	if dryRun {
		fmt.Fprintf(os.Stderr, "\nWould apply %d document(s) in this order:\n", len(files))
		for _, f := range files {
			fmt.Fprintf(os.Stderr, "  %-32s %s\n", f.Resource.Name, filepath.Base(f.Path))
		}
		return nil
	}

	proceed, err := confirmAction(fmt.Sprintf("restore %d document(s) into", len(files)), "this tenant", yes)
	if err != nil {
		return err
	}
	if !proceed {
		return nil
	}

	resolver := protect.NewResolver(cliCtx.ProtectClient)
	var applied, failed int
	// Names resolved during restore are cached by the resolver, so an object
	// created in an earlier stage must invalidate that cache before a later
	// stage looks for it. A fresh resolver per resource is the simplest way to
	// guarantee that ordering actually pays off.
	currentResource := ""

	for _, f := range files {
		if f.Resource.Name != currentResource {
			currentResource = f.Resource.Name
			resolver = protect.NewResolver(cliCtx.ProtectClient)
			fmt.Fprintf(os.Stderr, "\n%s\n", currentResource)
		}

		data, err := os.ReadFile(f.Path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  FAILED %s: %v\n", filepath.Base(f.Path), err)
			failed++
			continue
		}
		name, err := f.Resource.Restore(ctx, cliCtx.ProtectClient, resolver, data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  FAILED %s: %v\n", filepath.Base(f.Path), err)
			failed++
			continue
		}
		fmt.Fprintf(os.Stderr, "  applied %s\n", name)
		applied++
	}

	fmt.Fprintf(os.Stderr, "\nApplied %d document(s), %d failed\n", applied, failed)
	if failed > 0 {
		// PartialFailure rather than General, and honouring --allow-partial-failure,
		// so the two halves of the round trip are observable the same way by the
		// job that runs them.
		msg := fmt.Sprintf("%d document(s) failed to restore", failed)
		if allowPartialFailure && applied > 0 {
			fmt.Fprintf(os.Stderr, "warning: restore completed with %d failure(s); continuing (--allow-partial-failure)\n", failed)
			return nil
		}
		return exitcode.PartialOrPropagate(applied, failed, nil, msg)
	}
	return nil
}
