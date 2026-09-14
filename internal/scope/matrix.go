// Copyright 2026, Jamf Software LLC

package scope

import (
	"fmt"
	"sort"
	"strings"
)

// Scope sections. The Classic API models a scope as one set of targets plus
// two narrowing sets, mirroring the admin UI's Targets / Limitations /
// Exclusions tabs.
const (
	SectionTarget     = "target"
	SectionLimitation = "limitation"
	SectionExclusion  = "exclusion"
)

// Sections is the ordered section vocabulary, shared by --section's help text
// and its shell completion so the two cannot disagree.
var Sections = []string{SectionTarget, SectionLimitation, SectionExclusion}

// shape is one Classic resource's scope contract: which item flags each
// section accepts, and which targets each all-flag forbids.
//
// There are five distinct shapes across the eight scopeable resources, and the
// differences are not derivable from the resource name — a mac application
// takes no iBeacons where an equally computer-scoped policy does, an ebook is
// the union of the computer and mobile shapes plus classes, restricted
// software has no limitations tab at all, and the VPP pair are user-scoped
// with no device categories whatsoever. The sets below were read off the wire
// on Jamf Pro 11.31.1 (2026-09-12) by listing the child elements each
// resource's own GET returns per section, and they agree category-for-category
// with terraform-provider-jamfplatform's independently wire-probed schemas
// (internal/common/scope) with one deliberate exception noted on
// mac_application below.
//
// Sending a category the resource does not carry is not a silent no-op: the
// server answers 409 with a reason ("Mobile device groups cannot be assigned
// to an macOS profile"), so refusing it here turns a wall of HTML into a
// sentence that names the flags that would have worked.
type shape struct {
	// label describes the shape for the "did you mean" half of a refusal.
	label string

	target     []string
	limitation []string
	exclusion  []string

	// allFlagForbids maps an all-flag's XML element name to the target flags
	// it makes unreachable while set. The server accepts such a target with
	// 200 and silently drops it (wire-checked: all_computers=true plus a
	// computer_groups target reads back with the group gone), so the refusal
	// has to be client-side or there is no feedback at all.
	allFlagForbids map[string][]string
}

// Item flag names. These are CLI spellings, not XML element names —
// flagToElemName maps them onto the wire.
const (
	flagComputer          = "computer"
	flagComputerGroup     = "computer-group"
	flagMobileDevice      = "mobile-device"
	flagMobileDeviceGroup = "mobile-device-group"
	flagBuilding          = "building"
	flagDepartment        = "department"
	flagNetworkSegment    = "network-segment"
	flagUser              = "user"
	flagUserGroup         = "user-group"
	flagJSSUser           = "jss-user"
	flagJSSUserGroup      = "jss-user-group"
	flagIBeacon           = "ibeacon"
	flagClass             = "class"
)

// The all-flag element names, used as allFlagForbids keys.
const (
	elemAllComputers     = "all_computers"
	elemAllMobileDevices = "all_mobile_devices"
	elemAllJSSUsers      = "all_jss_users"
)

var (
	// computerTargets and friends are named so the shapes below read as the
	// differences between them rather than as eight opaque lists.
	computerTargets = []string{flagComputer, flagComputerGroup, flagBuilding, flagDepartment, flagJSSUser, flagJSSUserGroup}
	mobileTargets   = []string{flagMobileDevice, flagMobileDeviceGroup, flagBuilding, flagDepartment, flagJSSUser, flagJSSUserGroup}

	directoryNarrowing = []string{flagNetworkSegment, flagUser, flagUserGroup}

	computerExclusions = append(append([]string{}, computerTargets...), flagNetworkSegment, flagUser, flagUserGroup)
	mobileExclusions   = append(append([]string{}, mobileTargets...), flagNetworkSegment, flagUser, flagUserGroup)

	// allJSSUsersForbids is shared: every shape carrying all_jss_users forbids
	// the same two target categories.
	allJSSUsersForbids = []string{flagJSSUser, flagJSSUserGroup}
)

// shapes is keyed on Resource.SingularKey — the XML root key, which is already
// unique per scopeable resource and is what the generator stamps into each
// scope.Resource literal, so a new scopeable resource needs no generator
// change to be covered. TestEveryScopeableResourceHasAShape fails when one
// ships without an entry.
var shapes = map[string]shape{
	// Computer-scoped, iBeacon-bearing.
	"policy": {
		label:      "a computer-scoped resource",
		target:     computerTargets,
		limitation: append(append([]string{}, directoryNarrowing...), flagIBeacon),
		exclusion:  append(append([]string{}, computerExclusions...), flagIBeacon),
		allFlagForbids: map[string][]string{
			elemAllComputers: {flagComputer, flagComputerGroup, flagBuilding, flagDepartment},
			elemAllJSSUsers:  allJSSUsersForbids,
		},
	},
	"os_x_configuration_profile": {
		label:      "a computer-scoped resource",
		target:     computerTargets,
		limitation: append(append([]string{}, directoryNarrowing...), flagIBeacon),
		exclusion:  append(append([]string{}, computerExclusions...), flagIBeacon),
		allFlagForbids: map[string][]string{
			elemAllComputers: {flagComputer, flagComputerGroup, flagBuilding, flagDepartment},
			elemAllJSSUsers:  allJSSUsersForbids,
		},
	},

	// Computer-scoped without iBeacons. macapplications' own GET returns an
	// empty <mobile_device_groups> in both targets and exclusions, and that
	// element is a server artefact rather than a capability: sending a member
	// in it answers 409 "Mobile device groups cannot be assigned to an macOS
	// profile" (wire-checked 2026-09-12). So the flag stays refused, matching
	// the provider, and the GET is not treated as the authority where a write
	// probe disagrees with it.
	"mac_application": {
		label:      "a computer-scoped resource",
		target:     computerTargets,
		limitation: directoryNarrowing,
		exclusion:  computerExclusions,
		allFlagForbids: map[string][]string{
			elemAllComputers: {flagComputer, flagComputerGroup, flagBuilding, flagDepartment},
			elemAllJSSUsers:  allJSSUsersForbids,
		},
	},

	// Mobile-device-scoped, iBeacon-bearing. The XML root key for a mobile
	// device configuration profile is the bare "configuration_profile".
	"configuration_profile": {
		label:      "a mobile-device-scoped resource",
		target:     mobileTargets,
		limitation: append(append([]string{}, directoryNarrowing...), flagIBeacon),
		exclusion:  append(append([]string{}, mobileExclusions...), flagIBeacon),
		allFlagForbids: map[string][]string{
			elemAllMobileDevices: {flagMobileDevice, flagMobileDeviceGroup, flagBuilding, flagDepartment},
			elemAllJSSUsers:      allJSSUsersForbids,
		},
	},

	// Mobile-device-scoped without iBeacons.
	"mobile_device_application": {
		label:      "a mobile-device-scoped resource",
		target:     mobileTargets,
		limitation: directoryNarrowing,
		exclusion:  mobileExclusions,
		allFlagForbids: map[string][]string{
			elemAllMobileDevices: {flagMobileDevice, flagMobileDeviceGroup, flagBuilding, flagDepartment},
			elemAllJSSUsers:      allJSSUsersForbids,
		},
	},

	// The dual-target union, plus classes. An ebook deploys to computers and
	// mobile devices from one scope, and it is the only resource carrying
	// <classes>. It takes no iBeacons: a create carrying them answers 201 and
	// reads back with no <ibeacons> element at all (wire-checked).
	"ebook": {
		label: "an ebook (computers and mobile devices in one scope)",
		target: []string{
			flagComputer, flagComputerGroup, flagMobileDevice, flagMobileDeviceGroup,
			flagBuilding, flagDepartment, flagJSSUser, flagJSSUserGroup, flagClass,
		},
		limitation: directoryNarrowing,
		exclusion: []string{
			flagComputer, flagComputerGroup, flagMobileDevice, flagMobileDeviceGroup,
			flagBuilding, flagDepartment, flagJSSUser, flagJSSUserGroup,
			flagNetworkSegment, flagUser, flagUserGroup,
		},
		allFlagForbids: map[string][]string{
			elemAllComputers:     {flagComputer, flagComputerGroup},
			elemAllMobileDevices: {flagMobileDevice, flagMobileDeviceGroup},
			elemAllJSSUsers:      allJSSUsersForbids,
		},
	},

	// Computer-only and narrower than the computer shape in both directions:
	// no limitations tab, no Jamf Pro user targets, and the one narrowing
	// category it does carry is the free-text directory/local user exclusion.
	"restricted_software": {
		label:      "restricted software (computers only, no limitations)",
		target:     []string{flagComputer, flagComputerGroup, flagBuilding, flagDepartment},
		limitation: nil,
		exclusion:  []string{flagComputer, flagComputerGroup, flagBuilding, flagDepartment, flagUser},
		allFlagForbids: map[string][]string{
			elemAllComputers: {flagComputer, flagComputerGroup, flagBuilding, flagDepartment},
		},
	},

	// User-scoped: Jamf Pro users and user groups, narrowed by directory
	// groups. No device categories at all.
	"vpp_assignment": {
		label:      "a user-scoped VPP resource",
		target:     []string{flagJSSUser, flagJSSUserGroup},
		limitation: []string{flagUserGroup},
		exclusion:  []string{flagJSSUser, flagJSSUserGroup, flagUserGroup},
		allFlagForbids: map[string][]string{
			elemAllJSSUsers: allJSSUsersForbids,
		},
	},
	"vpp_invitation": {
		label:      "a user-scoped VPP resource",
		target:     []string{flagJSSUser, flagJSSUserGroup},
		limitation: []string{flagUserGroup},
		exclusion:  []string{flagJSSUser, flagJSSUserGroup, flagUserGroup},
		allFlagForbids: map[string][]string{
			elemAllJSSUsers: allJSSUsersForbids,
		},
	},
}

// sectionFlags returns the flags one resource accepts in one section, and
// whether the resource is known at all.
func sectionFlags(singularKey, section string) ([]string, bool) {
	sh, ok := shapes[singularKey]
	if !ok {
		return nil, false
	}
	switch section {
	case SectionTarget:
		return sh.target, true
	case SectionLimitation:
		return sh.limitation, true
	case SectionExclusion:
		return sh.exclusion, true
	}
	return nil, true
}

// ValidateScopeCombination checks that a section/flag pair is one the resource
// actually carries, refusing before anything is sent.
//
// The refusal names the flags that would have worked for this resource and
// section, because the two axes a caller gets wrong are "wrong section for a
// valid category" (--computer-group as a limitation) and "wrong category for
// this resource type" (--computer-group on a mobile profile), and a message
// listing one global vocabulary cannot distinguish them.
func ValidateScopeCombination(singularKey, section, flagName string) error {
	if !validSection(section) {
		return fmt.Errorf("invalid section %q; use %s", section, humanList(Sections))
	}

	allowed, known := sectionFlags(singularKey, section)
	if !known {
		// An unmapped resource means a new scopeable resource shipped without
		// a shapes entry. Refusing every flag would be worse than useless, so
		// say what is actually wrong.
		return fmt.Errorf("no scope contract is recorded for resource type %q; this is a jamf-cli bug — please report it", singularKey)
	}

	if len(allowed) == 0 {
		return fmt.Errorf("%s does not support %ss", shapes[singularKey].label, section)
	}
	if contains(allowed, flagName) {
		return nil
	}

	// Distinguish "valid elsewhere on this resource" from "not this resource's
	// shape at all" — the remedy differs.
	if other := otherSectionsAccepting(singularKey, section, flagName); len(other) > 0 {
		return fmt.Errorf("--%s is not a %s category for %s; it is valid in the %s section (--section %s), and the %s categories are %s",
			flagName, section, shapes[singularKey].label, humanList(other), other[0], section, flagList(allowed))
	}
	return fmt.Errorf("--%s is not a scope category of %s at all; the %s categories are %s",
		flagName, shapes[singularKey].label, section, flagList(allowed))
}

// CheckAllFlagConflict refuses a target that the resource's currently-set
// all-flag makes unreachable. The server accepts such a write with 200 and
// drops the member, so without this the only signal is the post-write
// verification failing with a message about the resource type.
func CheckAllFlagConflict(s *ScopeXML, singularKey, section, flagName string) error {
	if section != SectionTarget || s == nil {
		return nil
	}
	sh, ok := shapes[singularKey]
	if !ok {
		return nil
	}
	for elem, forbidden := range sh.allFlagForbids {
		if !allFlagSet(s, elem) || !contains(forbidden, flagName) {
			continue
		}
		return fmt.Errorf("<%s> is true on this %s, so the specific targets it covers (%s) are ignored; clear it first, or add --%s as an exclusion instead — the server accepts this write with 200 and silently drops the member",
			elem, singularKey, flagList(forbidden), flagName)
	}
	return nil
}

// allFlagSet reads one all-flag off a parsed scope by its XML element name.
func allFlagSet(s *ScopeXML, elem string) bool {
	switch elem {
	case elemAllComputers:
		return s.AllComputers
	case elemAllMobileDevices:
		return s.AllMobileDevices
	case elemAllJSSUsers:
		return s.AllJSSUsers
	}
	return false
}

// otherSectionsAccepting lists the sections of the same resource that do
// accept flagName, so a wrong-section mistake can be named as one.
func otherSectionsAccepting(singularKey, exclude, flagName string) []string {
	var out []string
	for _, section := range Sections {
		if section == exclude {
			continue
		}
		if allowed, ok := sectionFlags(singularKey, section); ok && contains(allowed, flagName) {
			out = append(out, section)
		}
	}
	return out
}

// ScopeFlagsFor returns the flags a resource accepts anywhere, so a command
// registers only the flags its own resource can use. Sorted for stable help.
func ScopeFlagsFor(singularKey string) []string {
	sh, ok := shapes[singularKey]
	if !ok {
		// Unknown resource: register the full set rather than none, so the
		// refusal in ValidateScopeCombination is the thing the caller sees.
		return append([]string{}, scopeFlagNames...)
	}
	seen := map[string]bool{}
	for _, group := range [][]string{sh.target, sh.limitation, sh.exclusion} {
		for _, f := range group {
			seen[f] = true
		}
	}
	out := make([]string, 0, len(seen))
	for _, f := range scopeFlagNames { // canonical order, not map order
		if seen[f] {
			out = append(out, f)
		}
	}
	return out
}

// SectionsFor returns the sections a resource actually has, so --section's
// help and completion do not offer a limitations tab to restricted software.
func SectionsFor(singularKey string) []string {
	out := make([]string, 0, len(Sections))
	for _, section := range Sections {
		if allowed, ok := sectionFlags(singularKey, section); ok && len(allowed) > 0 {
			out = append(out, section)
		} else if !ok {
			return append([]string{}, Sections...)
		}
	}
	return out
}

func validSection(section string) bool {
	return contains(Sections, section)
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// flagList renders a flag set as "--a, --b or --c".
func flagList(flags []string) string {
	dashed := make([]string, len(flags))
	for i, f := range flags {
		dashed[i] = "--" + f
	}
	sort.Strings(dashed)
	return humanList(dashed)
}

// humanList joins with commas and a final "or".
func humanList(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " or " + items[1]
	}
	return strings.Join(items[:len(items)-1], ", ") + " or " + items[len(items)-1]
}
