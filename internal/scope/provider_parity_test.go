// Copyright 2026, Jamf Software LLC

package scope

import (
	"encoding/xml"
	"reflect"
	"testing"
)

// terraform-provider-jamfplatform's internal/common/scope is the other
// wire-probed model of this same XML, maintained against a live tenant by its
// acceptance tests. The two repos cannot import each other, so the parity is
// asserted here rather than shared — the same arrangement the two copies of
// internal/privileges/catalogue.go are in.
//
// The provider's attribute names are UI-canonical and this package's are the
// wire element names, so the mapping below is the translation. Two rows carry
// the whole reason a mapping is needed at all:
//
//   - the provider's targets/exclusions `user_ids` and `user_group_ids` are
//     the JAMF PRO user categories, wire <jss_users>/<jss_user_groups>;
//   - its `directory_service_or_local_user_names` and
//     `directory_service_user_group_names` are the DIRECTORY ones, wire
//     <users>/<user_groups>.
//
// Read either pair the other way round and a scope write lands in the wrong
// section with no error.
var providerCategories = struct {
	targets, limitations, exclusions map[string]string // provider attribute -> XML element
}{
	targets: map[string]string{
		"all_computers":           "all_computers",
		"all_mobile_devices":      "all_mobile_devices",
		"all_jss_users":           "all_jss_users",
		"computer_ids":            "computers",
		"computer_group_ids":      "computer_groups",
		"mobile_device_ids":       "mobile_devices",
		"mobile_device_group_ids": "mobile_device_groups",
		"building_ids":            "buildings",
		"department_ids":          "departments",
		"user_ids":                "jss_users",
		"user_group_ids":          "jss_user_groups",
		"class_ids":               "classes", // ebook only
	},
	limitations: map[string]string{
		"network_segment_ids":                   "network_segments",
		"ibeacon_ids":                           "ibeacons",
		"directory_service_or_local_user_names": "users",
		"directory_service_user_group_names":    "user_groups",
	},
	exclusions: map[string]string{
		"computer_ids":                          "computers",
		"computer_group_ids":                    "computer_groups",
		"mobile_device_ids":                     "mobile_devices",
		"mobile_device_group_ids":               "mobile_device_groups",
		"building_ids":                          "buildings",
		"department_ids":                        "departments",
		"user_ids":                              "jss_users",
		"user_group_ids":                        "jss_user_groups",
		"network_segment_ids":                   "network_segments",
		"ibeacon_ids":                           "ibeacons",
		"directory_service_or_local_user_names": "users",
		"directory_service_user_group_names":    "user_groups",
	},
}

// xmlElements returns the XML element name of every field on a struct type,
// so the model can be compared against the provider's category set rather
// than against a hand-written second list of its own fields.
func xmlElements(t *testing.T, v any) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	rt := reflect.TypeOf(v)
	for rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.Type == reflect.TypeOf(xml.Name{}) {
			continue
		}
		name, _, _ := cutTag(f.Tag.Get("xml"))
		if name == "" || name == "-" {
			continue
		}
		out[name] = true
	}
	return out
}

// cutTag splits an xml struct tag into its element name and the rest.
func cutTag(tag string) (name, rest string, ok bool) {
	for i := 0; i < len(tag); i++ {
		if tag[i] == ',' {
			return tag[:i], tag[i+1:], true
		}
	}
	return tag, "", false
}

// TestModelCoversEveryProviderCategory is the parity assertion: every scope
// category terraform-provider-jamfplatform models must have a field here, and
// this package must not model a category the provider does not — the second
// half being the one that caught <limit_to_users> and the dead limitations
// computer_groups field.
//
// The sections are compared against the UNION of the provider's shapes, not
// against one of them, because this package deliberately keeps a single union
// struct where the provider keeps eight. That is forced by the write model:
// the Classic API replaces <scope> wholesale, so a read-modify-write has to
// round-trip every category the server returned, including the ones the
// resource in hand does not use. Splitting the struct per shape the way the
// provider does would silently wipe an unmodelled category on write. The
// per-resource knowledge the provider carries in its struct set lives in
// shapes (matrix.go) instead, and TestEveryScopeableResourceHasAShape guards
// that half.
func TestModelCoversEveryProviderCategory(t *testing.T) {
	cases := []struct {
		section  string
		provider map[string]string
		model    map[string]bool
		// extra names the wire carries that the provider has no attribute for,
		// each with the reason it is kept.
		allowed map[string]string
	}{
		{
			section:  "targets",
			provider: providerCategories.targets,
			model:    xmlElements(t, ScopeXML{}),
			allowed: map[string]string{
				// Sub-sections, not categories.
				"limitations": "the limitations sub-section",
				"exclusions":  "the exclusions sub-section",
			},
		},
		{
			section:  "limitations",
			provider: providerCategories.limitations,
			model:    xmlElements(t, LimitationsXML{}),
			allowed:  nil,
		},
		{
			section:  "exclusions",
			provider: providerCategories.exclusions,
			model:    xmlElements(t, ExclusionsXML{}),
			allowed:  nil,
		},
	}

	for _, tc := range cases {
		want := map[string]bool{}
		for attr, elem := range tc.provider {
			want[elem] = true
			if !tc.model[elem] {
				t.Errorf("%s: the provider models %q (wire <%s>) and this package has no field for it",
					tc.section, attr, elem)
			}
		}
		for elem := range tc.model {
			if want[elem] {
				continue
			}
			if reason, ok := tc.allowed[elem]; ok {
				_ = reason
				continue
			}
			t.Errorf("%s: this package models <%s> and the provider has no attribute for it — "+
				"either the provider is missing a category (check its wire probes) or this field is dead weight",
				tc.section, elem)
		}
	}
}

// TestLimitToUsersIsNotModelledEitherSide records the agreement rather than
// just the absence, because "we dropped a field the provider also dropped" is
// the sort of thing that gets re-added by someone reading a raw GET.
//
// Both repos reached it from their own wire probe, 3.5 months apart: the
// provider's 2026-05-24 (policy/model_types.go) and this package's 2026-09-12.
// The server denormalises <limitations><user_groups> into
// <limit_to_users><user_groups> on every write and back on every read, so
// modelling both is a second source of truth for one value — and when the two
// are sent disagreeing, limit_to_users wins, which is how the old
// policy-only special case managed to be correct and pointless at once.
func TestLimitToUsersIsNotModelledEitherSide(t *testing.T) {
	if xmlElements(t, ScopeXML{})["limit_to_users"] {
		t.Error("ScopeXML models <limit_to_users> again; the server mirrors it from limitations.user_groups, " +
			"and terraform-provider-jamfplatform drops it for the same reason")
	}

	// The mirror is what makes dropping it safe, so assert the field it is
	// mirrored from is present and reachable.
	if !xmlElements(t, LimitationsXML{})["user_groups"] {
		t.Fatal("LimitationsXML has no user_groups, so nothing carries the directory user group limitation")
	}
	s := &ScopeXML{}
	if !AddToScope(s, SectionLimitation, flagUserGroup, "Staff") {
		t.Fatal("a directory user group limitation could not be added")
	}
	if len(s.Limitations.UserGroups.Items) != 1 {
		t.Error("the directory user group limitation did not land in limitations.user_groups")
	}
}

// TestNoComputerGroupLimitation pins the other removal. No scopeable resource
// returns a computer_groups element inside <limitations> — a computer group
// selects an audience rather than narrowing one — and the provider models no
// such attribute in any of its three shapes. The field this package used to
// carry was emitted on every scope write, populated by nothing and read by
// nothing.
func TestNoComputerGroupLimitation(t *testing.T) {
	if xmlElements(t, LimitationsXML{})["computer_groups"] {
		t.Error("LimitationsXML models computer_groups again; no resource returns one and the provider has no attribute for it")
	}
	for key := range shapes {
		if allowed, _ := sectionFlags(key, SectionLimitation); contains(allowed, flagComputerGroup) {
			t.Errorf("%s admits --computer-group as a limitation", key)
		}
	}
}
