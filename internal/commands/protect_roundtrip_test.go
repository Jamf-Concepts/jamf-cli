// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

// Every export/apply pair in this package has to satisfy one property: the
// document a resource writes is a document the same resource can read back. It
// sounds tautological and it has been false seven times.
//
// Unit tests over the converters do not catch it, because both directions are
// wrong in the same way or the dropped field is not in the fixture. What catches
// it is feeding one command's real output into the other, which found the seventh
// instance — `unified-logging-filters export` writes the community schema, whose
// predicate key is `predicate`, while the SDK input calls the same field `Filter`,
// so every apply was refused with "input → filter: '' should be non-empty". That
// probe was a line of shell and ran nowhere.
//
// This is that probe, offline and in CI. For each entry it drives the resource's
// own protectResources() closures:
//
//	Export (from a fake tenant) → marshal as YAML and as JSON → Restore → capture
//	the input that would have gone to the API → assert the load-bearing field
//	survived.
//
// Both formats matter and disagree: yaml.v3 matches keys case-sensitively against
// the lowercased Go field name, while encoding/json matches case-insensitively
// against the json tag. A shape can round-trip in one and not the other.
//
// Adding a resource to protectResources() with a document it cannot read back
// fails here. See
// docs/solutions/logic-errors/response-shape-is-not-input-shape-2026-08-18.md.

// roundTripCase describes one resource's fixture and what must survive.
type roundTripCase struct {
	// resource is the protectResources() entry name. A name with no entry fails,
	// so a renamed resource cannot silently drop its coverage.
	resource string
	// seed loads the fake tenant with the objects Export will read.
	seed func(*fakeTenant)
	// wants maps a human label to a probe of the captured input. Each probe
	// returns the value that had to survive the round-trip; a zero value fails.
	// These are deliberately the fields the seven real bugs dropped.
	wants map[string]func(captured any) any

	// applyDecode is what the resource's `apply` COMMAND does with a document,
	// which is not always what its restore closure does — and that gap is where
	// the seventh instance lived. ULF restore decodes the community YAML struct
	// and was always correct; ULF apply decoded the SDK input directly and was
	// broken for every filter. Driving only the restore path proves nothing about
	// the documented `export | apply` pipe, so both axes run.
	//
	// The returned value is fed to the same `wants` probes, so a field that
	// survives restore but not apply fails here.
	applyDecode func(data []byte, r *protect.Resolver) (any, error)
}

func protectRoundTripCases() []roundTripCase {
	return []roundTripCase{
		{
			resource: "analytics",
			applyDecode: func(data []byte, _ *protect.Resolver) (any, error) {
				return analyticInputFromDocument(data)
			},
			seed: func(f *fakeTenant) {
				f.analytics = []jamfprotect.Analytic{{
					UUID: "an-1", Name: "zz-analytic", Jamf: false,
					InputType: "GPProcessEvent", Severity: "High",
					Description:     "short",
					LongDescription: "long description",
					Remediation:     "remediate like so",
					Label:           "A Label",
					MatchReason:     "why it matched",
					Startup:         true,
					Filter:          `$event.process.path == "/tmp/x"`,
					Categories:      []string{"Suspicious Behavior"},
					// The community schema calls these `actions` and models them
					// as objects; AnalyticInput.Actions is a []string and the
					// objects live under AnalyticActions. Decoding either as the
					// other is the original bug.
					AnalyticActions: []jamfprotect.AnalyticAction{{Name: "Report", Parameters: "{}"}},
				}}
			},
			wants: map[string]func(any) any{
				"filter":          func(c any) any { return c.(jamfprotect.AnalyticInput).Filter },
				"analyticActions": func(c any) any { return len(c.(jamfprotect.AnalyticInput).AnalyticActions) },
				"longDescription": func(c any) any { return c.(jamfprotect.AnalyticInput).LongDescription },
				"remediation":     func(c any) any { return c.(jamfprotect.AnalyticInput).Remediation },
				"label":           func(c any) any { return c.(jamfprotect.AnalyticInput).Label },
				"matchReason":     func(c any) any { return c.(jamfprotect.AnalyticInput).MatchReason },
				"startup": func(c any) any {
					s := c.(jamfprotect.AnalyticInput).Startup
					return s != nil && *s
				},
			},
		},
		{
			resource: "unified-logging-filters",
			applyDecode: func(data []byte, _ *protect.Resolver) (any, error) {
				return ulfInputFromDocument(data)
			},
			seed: func(f *fakeTenant) {
				f.ulfFilters = []jamfprotect.UnifiedLoggingFilter{{
					UUID: "ulf-1", Name: "zz-filter", Description: "d",
					// The field the seventh instance dropped.
					Filter:  `subsystem == "com.apple.TimeMachine"`,
					Tags:    []string{"zz"},
					Enabled: true,
				}}
			},
			wants: map[string]func(any) any{
				"filter (community `predicate`)": func(c any) any {
					return c.(jamfprotect.UnifiedLoggingFilterInput).Filter
				},
				"tags":    func(c any) any { return len(c.(jamfprotect.UnifiedLoggingFilterInput).Tags) },
				"enabled": func(c any) any { return c.(jamfprotect.UnifiedLoggingFilterInput).Enabled },
			},
		},
		{
			resource: "unified-logging-filter-sets",
			applyDecode: func(data []byte, r *protect.Resolver) (any, error) {
				var e ulfSetExport
				if err := unmarshalInput(data, &e); err != nil {
					return nil, err
				}
				return ulfSetExportToInput(context.Background(), e, r)
			},
			seed: func(f *fakeTenant) {
				f.ulfFilters = []jamfprotect.UnifiedLoggingFilter{{UUID: "ulf-1", Name: "zz-filter"}}
				f.ulfSets = []jamfprotect.UnifiedLoggingFilterSet{{
					UUID: "ulfs-1", Name: "zz-filter-set", Description: "d",
					Filters: []jamfprotect.UnifiedLoggingFilterSetFilter{{UUID: "ulf-1", Name: "zz-filter"}},
				}}
			},
			wants: map[string]func(any) any{
				// Membership travels as names and must resolve back to ids.
				"filter ids": func(c any) any {
					return strings.Join(c.(jamfprotect.UnifiedLoggingFilterSetInput).Filters, ",")
				},
			},
		},
		{
			resource: "analytic-sets",
			applyDecode: func(data []byte, r *protect.Resolver) (any, error) {
				var e analyticSetExport
				if err := unmarshalInput(data, &e); err != nil {
					return nil, err
				}
				return analyticSetExportToInput(context.Background(), e, r)
			},
			seed: func(f *fakeTenant) {
				f.analytics = []jamfprotect.Analytic{{UUID: "an-1", Name: "zz-analytic"}}
				f.analyticSets = []jamfprotect.AnalyticSet{{
					UUID: "as-1", Name: "zz-analytic-set", Description: "d",
					Types:     []string{"Report"},
					Analytics: []jamfprotect.AnalyticSetAnalytic{{UUID: "an-1", Name: "zz-analytic"}},
				}}
			},
			wants: map[string]func(any) any{
				"analytic ids": func(c any) any {
					return strings.Join(c.(jamfprotect.AnalyticSetInput).Analytics, ",")
				},
				"types": func(c any) any { return len(c.(jamfprotect.AnalyticSetInput).Types) },
			},
		},
		{
			resource: "exception-sets",
			applyDecode: func(data []byte, r *protect.Resolver) (any, error) {
				var doc exceptionSetExport
				if err := unmarshalInput(data, &doc); err != nil {
					return nil, err
				}
				return exceptionSetExportToInput(context.Background(), doc, r)
			},
			seed: func(f *fakeTenant) {
				f.analytics = []jamfprotect.Analytic{{UUID: "an-1", Name: "zz-analytic"}}
				f.exceptionSets = []jamfprotect.ExceptionSet{{
					UUID: "es-1", Name: "zz-exception-set", Description: "d",
					Exceptions: []jamfprotect.Exception{{
						Type: "Path", Value: "/tmp/x", IgnoreActivity: "Analytics",
						// Must export as a name and rebind, not carry the uuid.
						Analytic: &jamfprotect.AnalyticRef{UUID: "an-1", Name: "zz-analytic"},
					}},
				}}
			},
			wants: map[string]func(any) any{
				"exception count": func(c any) any { return len(c.(jamfprotect.ExceptionSetInput).Exceptions) },
				"rebound analytic uuid": func(c any) any {
					ex := c.(jamfprotect.ExceptionSetInput).Exceptions
					if len(ex) == 0 || ex[0].AnalyticUUID == nil {
						return ""
					}
					return *ex[0].AnalyticUUID
				},
				"value": func(c any) any {
					ex := c.(jamfprotect.ExceptionSetInput).Exceptions
					if len(ex) == 0 || ex[0].Value == nil {
						return ""
					}
					return *ex[0].Value
				},
			},
		},
		{
			resource: "plans",
			applyDecode: func(data []byte, r *protect.Resolver) (any, error) {
				var e planExport
				if err := unmarshalInput(data, &e); err != nil {
					return nil, err
				}
				return planExportToInput(context.Background(), e, r)
			},
			seed: func(f *fakeTenant) {
				f.analyticSets = []jamfprotect.AnalyticSet{{UUID: "as-1", Name: "zz-analytic-set"}}
				f.plans = []jamfprotect.Plan{{
					ID: "p-1", Name: "zz-plan", Description: "d",
					LogLevel: "INFO", AutoUpdate: true,
					AnalyticSets: []jamfprotect.PlanAnalyticSet{{
						AnalyticSet: jamfprotect.PlanAnalyticSetRef{UUID: "as-1", Name: "zz-analytic-set"},
						Type:        "Report",
					}},
					// The two fields planExport had drifted from PlanInput.
					ThreatPreventionStrategy: "CUSTOM_ENGINES",
					CustomEngineConfig: &jamfprotect.CustomEngineConfig{
						MalwareRiskware:  "PREVENT",
						AdversaryTactics: "REPORT",
						SystemTampering:  "REPORT",
						FilelessThreats:  "REPORT",
						Experimental:     "DISABLED",
					},
				}}
			},
			wants: map[string]func(any) any{
				"threatPreventionStrategy": func(c any) any {
					return c.(jamfprotect.PlanInput).ThreatPreventionStrategy
				},
				"customEngineConfig.MalwareRiskware": func(c any) any {
					cec := c.(jamfprotect.PlanInput).CustomEngineConfig
					if cec == nil {
						return ""
					}
					return cec.MalwareRiskware
				},
				"analytic set ids": func(c any) any {
					return len(c.(jamfprotect.PlanInput).AnalyticSets)
				},
			},
		},
		{
			resource: "roles",
			applyDecode: func(data []byte, _ *protect.Resolver) (any, error) {
				var in jamfprotect.RoleInput
				return in, unmarshalInput(data, &in)
			},
			seed: func(f *fakeTenant) {
				f.roles = []jamfprotect.Role{{
					ID: "r-1", Name: "zz-role",
					// The response nests these under permissions R/W while the
					// input is two flat lists — a shape mismatch of exactly the
					// kind this file guards.
					Permissions: &jamfprotect.RolePermissions{
						Read:  []string{"Alert", "Plan"},
						Write: []string{"Plan"},
					},
				}}
			},
			wants: map[string]func(any) any{
				"readResources":  func(c any) any { return len(c.(jamfprotect.RoleInput).ReadResources) },
				"writeResources": func(c any) any { return len(c.(jamfprotect.RoleInput).WriteResources) },
			},
		},
		{
			resource: "groups",
			applyDecode: func(data []byte, r *protect.Resolver) (any, error) {
				return groupInputFromDocument(context.Background(), data, r)
			},
			seed: func(f *fakeTenant) {
				f.roles = []jamfprotect.Role{{ID: "r-1", Name: "zz-role"}}
				f.groups = []jamfprotect.Group{{
					ID: "g-1", Name: "zz-group",
					// Roles must travel as names: role ids are small sequential
					// integers, so a foreign id binds to the wrong grant.
					AssignedRoles: []jamfprotect.GroupRole{{ID: "r-1", Name: "zz-role"}},
				}}
			},
			wants: map[string]func(any) any{
				"rebound role ids": func(c any) any {
					return strings.Join(c.(jamfprotect.GroupInput).RoleIDs, ",")
				},
			},
		},
		{
			resource: "users",
			applyDecode: func(data []byte, r *protect.Resolver) (any, error) {
				return userInputFromDocument(context.Background(), data, r)
			},
			seed: func(f *fakeTenant) {
				f.roles = []jamfprotect.Role{{ID: "r-1", Name: "zz-role"}}
				f.groups = []jamfprotect.Group{{ID: "g-1", Name: "zz-group"}}
				f.users = []jamfprotect.User{{
					ID: "u-1", Email: "zz@example.com",
					ReceiveEmailAlert:     true,
					EmailAlertMinSeverity: "High",
					AssignedRoles:         []jamfprotect.UserRole{{ID: "r-1", Name: "zz-role"}},
					AssignedGroups:        []jamfprotect.UserGroup{{ID: "g-1", Name: "zz-group"}},
				}}
			},
			wants: map[string]func(any) any{
				"rebound role ids":  func(c any) any { return strings.Join(c.(jamfprotect.UserInput).RoleIDs, ",") },
				"rebound group ids": func(c any) any { return strings.Join(c.(jamfprotect.UserInput).GroupIDs, ",") },
				"emailAlertMinSeverity": func(c any) any {
					return c.(jamfprotect.UserInput).EmailAlertMinSeverity
				},
			},
		},
		{
			resource: "telemetry",
			applyDecode: func(data []byte, _ *protect.Resolver) (any, error) {
				var in jamfprotect.TelemetryV2Input
				return in, unmarshalInput(data, &in)
			},
			seed: func(f *fakeTenant) {
				f.telemetries = []jamfprotect.TelemetryV2{{
					ID: "t-1", Name: "zz-telemetry", Description: "d",
					Events:             []string{"network_connect"},
					PerformanceMetrics: true,
				}}
			},
			wants: map[string]func(any) any{
				"events": func(c any) any { return len(c.(jamfprotect.TelemetryV2Input).Events) },
			},
		},
		{
			resource: "custom-prevent-lists",
			applyDecode: func(data []byte, _ *protect.Resolver) (any, error) {
				var in jamfprotect.CustomPreventListInput
				return in, unmarshalInput(data, &in)
			},
			seed: func(f *fakeTenant) {
				f.preventLists = []jamfprotect.CustomPreventList{{
					ID: "cpl-1", Name: "zz-prevent-list", Type: "FILEHASH",
					List: []string{"abc123"},
				}}
			},
			wants: map[string]func(any) any{
				"type": func(c any) any { return c.(jamfprotect.CustomPreventListInput).Type },
				"list": func(c any) any { return len(c.(jamfprotect.CustomPreventListInput).List) },
			},
		},
		{
			resource: "removable-storage-control-sets",
			applyDecode: func(data []byte, _ *protect.Resolver) (any, error) {
				var in jamfprotect.RemovableStorageControlSetInput
				return in, unmarshalInput(data, &in)
			},
			seed: func(f *fakeTenant) {
				f.rscSets = []jamfprotect.RemovableStorageControlSet{{
					ID: "rscs-1", Name: "zz-usb-set", Description: "d",
					Rules: []jamfprotect.RemovableStorageControlRule{{
						// rscRuleToInput switches on Type and moves the response's
						// flat fields into a per-type nested *RuleInput. Vendor ids
						// must keep their 0x prefix; bare hex is rejected.
						Type: "vendor", MountAction: "READ_ONLY", ApplyTo: "ALL",
						Vendors: []string{"0x0781"},
					}},
				}}
			},
			wants: map[string]func(any) any{
				"rules": func(c any) any { return len(c.(jamfprotect.RemovableStorageControlSetInput).Rules) },
				"vendor rule survives the type switch": func(c any) any {
					rules := c.(jamfprotect.RemovableStorageControlSetInput).Rules
					if len(rules) == 0 || rules[0].VendorRule == nil {
						return ""
					}
					return strings.Join(rules[0].VendorRule.Vendors, ",")
				},
			},
		},
	}
}

func TestProtectExportApplyRoundTrip(t *testing.T) {
	byName := map[string]protectResource{}
	for _, r := range protectResources() {
		byName[r.Name] = r
	}

	for _, tc := range protectRoundTripCases() {
		t.Run(tc.resource, func(t *testing.T) {
			res, ok := byName[tc.resource]
			if !ok {
				t.Fatalf("no protectResources() entry named %q — was it renamed? coverage would be silently lost", tc.resource)
			}
			if res.Restore == nil {
				t.Fatalf("%s has no Restore closure; drop it from the round-trip table with a reason", tc.resource)
			}

			// Both on-disk formats, because their key derivation differs:
			// yaml.v3 lowercases the Go field name and matches case-sensitively,
			// encoding/json uses the json tag and matches case-insensitively.
			for _, format := range []string{"yaml", "json"} {
				t.Run(format, func(t *testing.T) {
					tenant := newFakeTenant()
					tc.seed(tenant)

					entries, err := res.Export(context.Background(), tenant)
					if err != nil {
						t.Fatalf("Export: %v", err)
					}
					if len(entries) != 1 {
						t.Fatalf("Export produced %d entries, want exactly 1 for this fixture", len(entries))
					}

					doc, err := protectMarshal(entries[0].Doc, format)
					if err != nil {
						t.Fatalf("marshalling the exported document: %v", err)
					}

					// Restore reads the document and drives create/update. The
					// fake tenant records the input rather than sending it.
					tenant.captured = nil
					if _, err := res.Restore(context.Background(), tenant, protect.NewResolver(tenant), doc); err != nil {
						t.Fatalf("Restore rejected this resource's own export:\n%v\n\n--- document (%s) ---\n%s",
							err, format, doc)
					}
					if tenant.captured == nil {
						t.Fatalf("Restore made no create/update call; nothing was verified")
					}

					// Axis 1: the backup/restore path.
					for label, probe := range tc.wants {
						got := probe(tenant.captured)
						if isZeroish(got) {
							t.Errorf("restore path: %s did not survive export → %s → restore (got %#v)\n--- document ---\n%s",
								label, format, got, doc)
						}
					}

					// Axis 2: the documented `export | apply` pipe. This is a
					// different decoder for six of these resources, and the gap
					// between the two is where the seventh instance of the bug
					// lived — restore was correct, apply was not.
					if tc.applyDecode == nil {
						t.Fatalf("no applyDecode: the export | apply pipe is unverified for %s", tc.resource)
					}
					decoded, err := tc.applyDecode(doc, protect.NewResolver(tenant))
					if err != nil {
						t.Fatalf("`%s apply` rejected `%s export` output:\n%v\n\n--- document (%s) ---\n%s",
							tc.resource, tc.resource, err, format, doc)
					}
					for label, probe := range tc.wants {
						got := probe(decoded)
						if isZeroish(got) {
							t.Errorf("apply path: %s did not survive export → %s → apply (got %#v)\n--- document ---\n%s",
								label, format, got, doc)
						}
					}
				})
			}
		})
	}
}

// Every resource that can be restored must be in the round-trip table. Without
// this, adding a resource to protectResources() silently adds an unverified
// export/apply pair — which is exactly how the seventh instance shipped.
func TestProtectRoundTripTableCoversEveryRestorableResource(t *testing.T) {
	covered := map[string]bool{}
	for _, tc := range protectRoundTripCases() {
		covered[tc.resource] = true
	}

	// Singletons carry tenant state rather than a name-keyed object document, so
	// there is no export/apply shape pair to round-trip. Each is listed
	// deliberately, with the reason, rather than skipped by a type check.
	exempt := map[string]string{
		"analytic-overrides": "a name-keyed overlay replayed via updateInternalAnalytic, covered by the overrides tests",
		"insights":           "an enabled/disabled split over a Jamf-published catalogue, not an object document",
		"config-freeze":      "a single boolean compared before writing",
		"data-retention":     "a flat settings document compared before writing, covered by TestRestoreDataRetentionSkipsAnUnchangedWrite",
		"action-configs":     "params is an AWSJSON string round-tripped as an opaque blob; covered by TestPruneEmptyValues",
	}

	for _, r := range protectResources() {
		if r.Restore == nil {
			continue // backup-only by design; RestoreSkipReason explains it
		}
		if covered[r.Name] || exempt[r.Name] != "" {
			continue
		}
		t.Errorf("resource %q is restorable but absent from protectRoundTripCases(); "+
			"add a fixture asserting its export survives its own apply, or exempt it with a reason", r.Name)
	}
}

// isZeroish reports whether a probe returned something indistinguishable from
// "the field was dropped". A dropped field is the failure this file exists to
// catch, and it always lands on a zero value.
func isZeroish(v any) bool {
	switch t := v.(type) {
	case string:
		return t == ""
	case int:
		return t == 0
	case bool:
		return !t
	case nil:
		return true
	}
	return false
}

// --- fake tenant ---

// fakeTenant answers the list/get calls Export and the resolver make, and
// records the input Restore would have sent instead of sending it.
//
// It embeds registry.ProtectClient so an unimplemented method panics rather than
// quietly returning a zero value: a resource whose Export reaches for something
// this fake does not serve should fail loudly at the point of the gap.
type fakeTenant struct {
	registry.ProtectClient

	analytics     []jamfprotect.Analytic
	analyticSets  []jamfprotect.AnalyticSet
	exceptionSets []jamfprotect.ExceptionSet
	plans         []jamfprotect.Plan
	roles         []jamfprotect.Role
	groups        []jamfprotect.Group
	users         []jamfprotect.User
	ulfFilters    []jamfprotect.UnifiedLoggingFilter
	ulfSets       []jamfprotect.UnifiedLoggingFilterSet
	telemetries   []jamfprotect.TelemetryV2
	preventLists  []jamfprotect.CustomPreventList
	rscSets       []jamfprotect.RemovableStorageControlSet

	// captured is the input the last create/update received.
	captured any
}

func newFakeTenant() *fakeTenant { return &fakeTenant{} }

func (f *fakeTenant) capture(input any) { f.captured = input }

// notFound makes the fake behave like an empty target tenant, so Restore takes
// its create path. It must match protect.ErrNotFound or upsertByName treats it
// as a real lookup failure and aborts.
func (f *fakeTenant) notFound(what string) error {
	return fmt.Errorf("%s not found in the fake tenant: %w", what, protect.ErrNotFound)
}

// --- list/get: what Export and the resolver read ---

func (f *fakeTenant) ListAnalytics(context.Context) ([]jamfprotect.Analytic, error) {
	return f.analytics, nil
}

func (f *fakeTenant) ListAnalyticSets(context.Context) ([]jamfprotect.AnalyticSet, error) {
	return f.analyticSets, nil
}

func (f *fakeTenant) ListPlans(context.Context) ([]jamfprotect.Plan, error) { return f.plans, nil }

func (f *fakeTenant) ListRoles(context.Context) ([]jamfprotect.Role, error) { return f.roles, nil }

func (f *fakeTenant) ListGroups(context.Context) ([]jamfprotect.Group, error) { return f.groups, nil }

func (f *fakeTenant) ListUsers(context.Context) ([]jamfprotect.User, error) { return f.users, nil }

func (f *fakeTenant) ListUnifiedLoggingFilters(context.Context) ([]jamfprotect.UnifiedLoggingFilter, error) {
	return f.ulfFilters, nil
}

func (f *fakeTenant) ListUnifiedLoggingFilterSets(context.Context) ([]jamfprotect.UnifiedLoggingFilterSet, error) {
	return f.ulfSets, nil
}

func (f *fakeTenant) ListTelemetriesV2(context.Context) ([]jamfprotect.TelemetryV2, error) {
	return f.telemetries, nil
}

func (f *fakeTenant) ListCustomPreventLists(context.Context) ([]jamfprotect.CustomPreventList, error) {
	return f.preventLists, nil
}

func (f *fakeTenant) ListRemovableStorageControlSets(context.Context) ([]jamfprotect.RemovableStorageControlSet, error) {
	return f.rscSets, nil
}

// listExceptionSets returns a summary only, which is why the resource's Export
// fetches each one — a detail this fake has to reproduce or the round-trip would
// pass against membership the real backup never sees.
func (f *fakeTenant) ListExceptionSets(context.Context) ([]jamfprotect.ExceptionSetListItem, error) {
	out := make([]jamfprotect.ExceptionSetListItem, 0, len(f.exceptionSets))
	for _, es := range f.exceptionSets {
		out = append(out, jamfprotect.ExceptionSetListItem{UUID: es.UUID, Name: es.Name, Managed: es.Managed})
	}
	return out, nil
}

func (f *fakeTenant) GetExceptionSet(_ context.Context, uuid string) (*jamfprotect.ExceptionSet, error) {
	for i := range f.exceptionSets {
		if f.exceptionSets[i].UUID == uuid {
			return &f.exceptionSets[i], nil
		}
	}
	return nil, f.notFound("exception set " + uuid)
}

// --- create/update: capture instead of send ---

func (f *fakeTenant) CreateAnalytic(_ context.Context, in jamfprotect.AnalyticInput) (jamfprotect.Analytic, error) {
	f.capture(in)
	return jamfprotect.Analytic{}, nil
}

func (f *fakeTenant) UpdateAnalytic(_ context.Context, _ string, in jamfprotect.AnalyticInput) (jamfprotect.Analytic, error) {
	f.capture(in)
	return jamfprotect.Analytic{}, nil
}

func (f *fakeTenant) CreateAnalyticSet(_ context.Context, in jamfprotect.AnalyticSetInput) (jamfprotect.AnalyticSet, error) {
	f.capture(in)
	return jamfprotect.AnalyticSet{}, nil
}

func (f *fakeTenant) UpdateAnalyticSet(_ context.Context, _ string, in jamfprotect.AnalyticSetInput) (jamfprotect.AnalyticSet, error) {
	f.capture(in)
	return jamfprotect.AnalyticSet{}, nil
}

func (f *fakeTenant) CreateExceptionSet(_ context.Context, in jamfprotect.ExceptionSetInput) (jamfprotect.ExceptionSet, error) {
	f.capture(in)
	return jamfprotect.ExceptionSet{}, nil
}

func (f *fakeTenant) UpdateExceptionSet(_ context.Context, _ string, in jamfprotect.ExceptionSetInput) (jamfprotect.ExceptionSet, error) {
	f.capture(in)
	return jamfprotect.ExceptionSet{}, nil
}

func (f *fakeTenant) CreatePlan(_ context.Context, in jamfprotect.PlanInput) (jamfprotect.Plan, error) {
	f.capture(in)
	return jamfprotect.Plan{}, nil
}

func (f *fakeTenant) UpdatePlan(_ context.Context, _ string, in jamfprotect.PlanInput) (jamfprotect.Plan, error) {
	f.capture(in)
	return jamfprotect.Plan{}, nil
}

func (f *fakeTenant) CreateRole(_ context.Context, in jamfprotect.RoleInput) (jamfprotect.Role, error) {
	f.capture(in)
	return jamfprotect.Role{}, nil
}

func (f *fakeTenant) UpdateRole(_ context.Context, _ string, in jamfprotect.RoleInput) (jamfprotect.Role, error) {
	f.capture(in)
	return jamfprotect.Role{}, nil
}

func (f *fakeTenant) CreateGroup(_ context.Context, in jamfprotect.GroupInput) (jamfprotect.Group, error) {
	f.capture(in)
	return jamfprotect.Group{}, nil
}

func (f *fakeTenant) UpdateGroup(_ context.Context, _ string, in jamfprotect.GroupInput) (jamfprotect.Group, error) {
	f.capture(in)
	return jamfprotect.Group{}, nil
}

func (f *fakeTenant) CreateUser(_ context.Context, in jamfprotect.UserInput) (jamfprotect.User, error) {
	f.capture(in)
	return jamfprotect.User{}, nil
}

func (f *fakeTenant) UpdateUser(_ context.Context, _ string, in jamfprotect.UserInput) (jamfprotect.User, error) {
	f.capture(in)
	return jamfprotect.User{}, nil
}

func (f *fakeTenant) CreateUnifiedLoggingFilter(_ context.Context, in jamfprotect.UnifiedLoggingFilterInput) (jamfprotect.UnifiedLoggingFilter, error) {
	f.capture(in)
	return jamfprotect.UnifiedLoggingFilter{}, nil
}

func (f *fakeTenant) UpdateUnifiedLoggingFilter(_ context.Context, _ string, in jamfprotect.UnifiedLoggingFilterInput) (jamfprotect.UnifiedLoggingFilter, error) {
	f.capture(in)
	return jamfprotect.UnifiedLoggingFilter{}, nil
}

func (f *fakeTenant) CreateUnifiedLoggingFilterSet(_ context.Context, in jamfprotect.UnifiedLoggingFilterSetInput) (jamfprotect.UnifiedLoggingFilterSet, error) {
	f.capture(in)
	return jamfprotect.UnifiedLoggingFilterSet{}, nil
}

func (f *fakeTenant) UpdateUnifiedLoggingFilterSet(_ context.Context, _ string, in jamfprotect.UnifiedLoggingFilterSetInput) (jamfprotect.UnifiedLoggingFilterSet, error) {
	f.capture(in)
	return jamfprotect.UnifiedLoggingFilterSet{}, nil
}

func (f *fakeTenant) CreateTelemetryV2(_ context.Context, in jamfprotect.TelemetryV2Input) (jamfprotect.TelemetryV2, error) {
	f.capture(in)
	return jamfprotect.TelemetryV2{}, nil
}

func (f *fakeTenant) UpdateTelemetryV2(_ context.Context, _ string, in jamfprotect.TelemetryV2Input) (jamfprotect.TelemetryV2, error) {
	f.capture(in)
	return jamfprotect.TelemetryV2{}, nil
}

func (f *fakeTenant) CreateCustomPreventList(_ context.Context, in jamfprotect.CustomPreventListInput) (jamfprotect.CustomPreventList, error) {
	f.capture(in)
	return jamfprotect.CustomPreventList{}, nil
}

func (f *fakeTenant) UpdateCustomPreventList(_ context.Context, _ string, in jamfprotect.CustomPreventListInput) (jamfprotect.CustomPreventList, error) {
	f.capture(in)
	return jamfprotect.CustomPreventList{}, nil
}

func (f *fakeTenant) CreateRemovableStorageControlSet(_ context.Context, in jamfprotect.RemovableStorageControlSetInput) (jamfprotect.RemovableStorageControlSet, error) {
	f.capture(in)
	return jamfprotect.RemovableStorageControlSet{}, nil
}

func (f *fakeTenant) UpdateRemovableStorageControlSet(_ context.Context, _ string, in jamfprotect.RemovableStorageControlSetInput) (jamfprotect.RemovableStorageControlSet, error) {
	f.capture(in)
	return jamfprotect.RemovableStorageControlSet{}, nil
}
