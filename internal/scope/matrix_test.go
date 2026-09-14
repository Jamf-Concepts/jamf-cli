// Copyright 2026, Jamf Software LLC

package scope

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// TestEveryScopeableResourceHasAShape is the guard that keeps a new scopeable
// Classic resource from shipping with no scope contract.
//
// The list is the SingularKey of every resource the generator stamps a
// scope.Resource for (specs/classic/resources.yaml, `scope: true`). Without an
// entry in shapes, ValidateScopeCombination refuses every flag and
// ScopeFlagsFor registers the whole union — so the command exists and can
// address nothing sensible. Adding a resource means adding a row here and a
// shape there, having read the resource's own GET to see which categories it
// carries per section.
func TestEveryScopeableResourceHasAShape(t *testing.T) {
	scopeable := []string{
		"policy",
		"os_x_configuration_profile",
		"configuration_profile", // mobile device configuration profile
		"mac_application",
		"mobile_device_application",
		"ebook",
		"restricted_software",
		"vpp_assignment",
		"vpp_invitation",
	}
	for _, key := range scopeable {
		sh, ok := shapes[key]
		if !ok {
			t.Errorf("no shape recorded for scopeable resource %q — see the doc comment on shapes", key)
			continue
		}
		if sh.label == "" {
			t.Errorf("%s: shape has no label; it is used in every refusal message", key)
		}
		if len(sh.target) == 0 {
			t.Errorf("%s: shape has no target categories, which no scopeable resource can be", key)
		}
	}
	if len(shapes) != len(scopeable) {
		t.Errorf("shapes has %d entries for %d scopeable resources; a stale entry is as wrong as a missing one", len(shapes), len(scopeable))
	}
}

// TestEveryShapeCategoryIsAddressable fails when a shape admits a flag that no
// accessor can reach — the silent half of a typo in the matrix, where the flag
// is registered, accepted by validation, and then writes to nothing.
func TestEveryShapeCategoryIsAddressable(t *testing.T) {
	for key, sh := range shapes {
		for _, tc := range []struct {
			section string
			flags   []string
		}{
			{SectionTarget, sh.target},
			{SectionLimitation, sh.limitation},
			{SectionExclusion, sh.exclusion},
		} {
			for _, flag := range tc.flags {
				if _, ok := flagToElemName[flag]; !ok {
					t.Errorf("%s/%s/--%s: no flagToElemName entry, so a new member gets no child element name", key, tc.section, flag)
				}
				s := &ScopeXML{}
				if got := getOrCreateScopeItems(s, tc.section, flag); got == nil {
					t.Errorf("%s/%s/--%s: accepted by the matrix but no accessor reaches it", key, tc.section, flag)
				}
			}
		}
	}
}

// TestScopeFlagsForRegistersOnlyTheResourcesOwnCategories pins the help and
// completion surface: a mobile configuration profile must not offer
// --computer-group, and restricted software must not offer a limitations
// section it has no tab for.
func TestScopeFlagsForRegistersOnlyTheResourcesOwnCategories(t *testing.T) {
	if flags := ScopeFlagsFor("configuration_profile"); contains(flags, flagComputerGroup) {
		t.Errorf("mobile configuration profile offers --computer-group: %v", flags)
	}
	if flags := ScopeFlagsFor("policy"); contains(flags, flagMobileDeviceGroup) {
		t.Errorf("policy offers --mobile-device-group: %v", flags)
	}
	if flags := ScopeFlagsFor("vpp_assignment"); contains(flags, flagBuilding) || contains(flags, flagComputer) {
		t.Errorf("user-scoped VPP resource offers device categories: %v", flags)
	}
	if got := SectionsFor("restricted_software"); contains(got, SectionLimitation) {
		t.Errorf("restricted software offers a limitations section: %v", got)
	}
	if got := SectionsFor("policy"); len(got) != 3 {
		t.Errorf("policy sections = %v, want all three", got)
	}
	// An unmapped resource registers the union rather than nothing, so the
	// refusal in ValidateScopeCombination is what the caller reads.
	if got := ScopeFlagsFor("not_a_resource"); len(got) != len(scopeFlagNames) {
		t.Errorf("unmapped resource registered %d flags, want the full %d", len(got), len(scopeFlagNames))
	}
}

// TestValidateScopeCombination_NamesTheRightRemedy checks the two axes a
// caller gets wrong are distinguished. A valid category in the wrong section
// should be told which section takes it; a category the resource does not have
// at all should be told so, because the remedies differ.
func TestValidateScopeCombination_NamesTheRightRemedy(t *testing.T) {
	err := ValidateScopeCombination("policy", SectionTarget, flagNetworkSegment)
	if err == nil {
		t.Fatal("--network-segment as a policy target should be refused")
	}
	if !strings.Contains(err.Error(), "--section limitation") {
		t.Errorf("wrong-section refusal should name the section that works: %v", err)
	}

	err = ValidateScopeCombination("configuration_profile", SectionTarget, flagComputerGroup)
	if err == nil {
		t.Fatal("--computer-group on a mobile profile should be refused")
	}
	if strings.Contains(err.Error(), "--section") {
		t.Errorf("a category the resource does not have must not suggest another section: %v", err)
	}
	if !strings.Contains(err.Error(), "not a scope category") {
		t.Errorf("refusal should say the category does not apply: %v", err)
	}
}

// TestValidateScopeCombination_UnmappedResourceSaysSo keeps the failure legible
// if a new scopeable resource ever reaches a user before its shape does. The
// alternative — refusing every flag with a message about valid categories —
// reads as a broken command rather than a missing table entry.
func TestValidateScopeCombination_UnmappedResourceSaysSo(t *testing.T) {
	err := ValidateScopeCombination("brand_new_resource", SectionTarget, flagComputerGroup)
	if err == nil {
		t.Fatal("expected a refusal for an unmapped resource")
	}
	if !strings.Contains(err.Error(), "jamf-cli bug") {
		t.Errorf("refusal should identify itself as a CLI gap, not a user error: %v", err)
	}
}

// TestCheckAllFlagConflict refuses a target the resource's own all-flag makes
// unreachable. The server accepts such a write with 200 and drops the member
// (wire-checked 2026-09-12: all_computers=true plus a computer_groups target
// reads back with the group gone), so without this the only feedback is the
// post-write verification failing with a message about the resource type.
func TestCheckAllFlagConflict(t *testing.T) {
	allComputers := &ScopeXML{AllComputers: true}
	if err := CheckAllFlagConflict(allComputers, "policy", SectionTarget, flagComputerGroup); err == nil {
		t.Error("all_computers=true should refuse a computer group target")
	} else if !strings.Contains(err.Error(), "all_computers") {
		t.Errorf("refusal should name the flag that is in the way: %v", err)
	}

	// Categories the all-flag does not cover stay reachable.
	if err := CheckAllFlagConflict(allComputers, "policy", SectionTarget, flagJSSUser); err != nil {
		t.Errorf("all_computers must not block a Jamf Pro user target: %v", err)
	}
	// Limitations and exclusions narrow an all-flag scope — that is the point
	// of them — so they are never in conflict.
	if err := CheckAllFlagConflict(allComputers, "policy", SectionExclusion, flagComputerGroup); err != nil {
		t.Errorf("all_computers must not block an exclusion: %v", err)
	}
	// An ebook's all_computers covers only its computer categories; buildings
	// and departments stay addressable, unlike the single-target shapes.
	ebook := &ScopeXML{AllComputers: true}
	if err := CheckAllFlagConflict(ebook, "ebook", SectionTarget, flagBuilding); err != nil {
		t.Errorf("ebook all_computers must not block a building target: %v", err)
	}
	if err := CheckAllFlagConflict(ebook, "ebook", SectionTarget, flagComputerGroup); err == nil {
		t.Error("ebook all_computers should refuse a computer group target")
	}
	// all_jss_users is the one every shape carrying it treats identically.
	allUsers := &ScopeXML{AllJSSUsers: true}
	if err := CheckAllFlagConflict(allUsers, "vpp_assignment", SectionTarget, flagJSSUserGroup); err == nil {
		t.Error("all_jss_users=true should refuse a Jamf Pro user group target")
	}
	// Nothing set: nothing refused.
	if err := CheckAllFlagConflict(&ScopeXML{}, "policy", SectionTarget, flagComputerGroup); err != nil {
		t.Errorf("no all-flag set should refuse nothing: %v", err)
	}
}

// TestNewRef pins the identifier contract: an `<id>` positional or --name, one
// or the other. Accepting both and preferring one would mutate the scope of an
// object the caller did not name when the two disagree.
func TestNewRef(t *testing.T) {
	if ref, err := NewRef([]string{"42"}, ""); err != nil || ref.ID != "42" || ref.Name != "" {
		t.Errorf("positional: ref=%+v err=%v", ref, err)
	}
	if ref, err := NewRef(nil, "My Policy"); err != nil || ref.Name != "My Policy" || ref.ID != "" {
		t.Errorf("--name: ref=%+v err=%v", ref, err)
	}
	if _, err := NewRef([]string{"42"}, "My Policy"); err == nil {
		t.Error("both an <id> and --name should be refused")
	}
	if _, err := NewRef(nil, ""); err == nil {
		t.Error("neither an <id> nor --name should be refused")
	}

	// The migration guard: the positional used to be the NAME on these
	// commands, so `scope get "My Policy"` is the mistake a caller inherits.
	// Sent as an id it costs a request and 404s with a hint pointing at
	// `list`, saying nothing about --name.
	_, err := NewRef([]string{"My Policy"}, "")
	if err == nil {
		t.Fatal("a non-numeric positional should be refused, not sent as an id")
	}
	if !strings.Contains(err.Error(), "not an id") {
		t.Errorf("refusal should say why: %v", err)
	}
	var coded *exitcode.Error
	if !errors.As(err, &coded) || coded.Code != exitcode.Usage {
		t.Errorf("an invocation error must exit %d like every other one: %v", exitcode.Usage, err)
	}
	if !strings.Contains(coded.Hint, "--name") {
		t.Errorf("the hint must name the replacement: %q", coded.Hint)
	}

	// Every refusal here is an invocation error, so all three carry exit 2.
	for _, tc := range []struct {
		name string
		args []string
		flag string
	}{
		{"both", []string{"42"}, "My Policy"},
		{"neither", nil, ""},
	} {
		_, err := NewRef(tc.args, tc.flag)
		var e *exitcode.Error
		if !errors.As(err, &e) || e.Code != exitcode.Usage {
			t.Errorf("%s: want exit %d, got %v", tc.name, exitcode.Usage, err)
		}
	}
	// The rendering feeds every success and failure message, so it has to say
	// which kind of reference the caller used.
	if got := (Ref{ID: "7"}).String(); got != "id 7" {
		t.Errorf("Ref{ID}.String() = %q", got)
	}
	if got := (Ref{Name: "x"}).String(); got != `"x"` {
		t.Errorf("Ref{Name}.String() = %q", got)
	}
}

// TestVerifyWrittenSkipsTheDryRunRead pins the dry-run fix. `dryRunClient`
// suppresses the PUT and returns success, so a verification read afterwards
// finds the scope unchanged and reports "the server accepted the write but …"
// — a dry run failing at exit 1 with a message describing a server fault that
// never happened. `-n` on a scope command previewed the request and then
// always errored.
func TestVerifyWrittenSkipsTheDryRunRead(t *testing.T) {
	client := &mockPutClient{getBody: `<policy><general><id>1</id></general><scope/></policy>`}
	res := Resource{APIPath: "policies", SingularKey: "policy"}
	target := ScopeTarget{FlagName: flagComputerGroup, Name: "Never Written"}
	cmd := &cobra.Command{}

	dry := &registry.CLIContext{Client: client, DryRun: true}
	if err := verifyWritten(cmd, dry, res, "1", SectionTarget, target, &ScopeXML{}, true); err != nil {
		t.Errorf("a dry run must not verify a write it did not make: %v", err)
	}
	if len(client.requests) != 0 {
		t.Errorf("a dry run must not spend a read: %v", client.requests)
	}

	// Without --dry-run the read happens and the absent member is reported.
	wet := &registry.CLIContext{Client: client, DryRun: false}
	err := verifyWritten(cmd, wet, res, "1", SectionTarget, target, &ScopeXML{}, true)
	if err == nil {
		t.Fatal("expected the missing member to be reported")
	}
	if !strings.Contains(err.Error(), "Never Written") {
		t.Errorf("report should name the member: %v", err)
	}
	if len(client.requests) != 1 {
		t.Errorf("requests = %v, want one verification GET", client.requests)
	}
}

// TestScopeExampleIsAWholeInvocation pins the examples as pasteable commands.
// They used to be fragments starting at "scope add", which no --help reader
// could run and which TestNoExampleDocumentsAnUndeclaredPositional could not
// check — 18 of the 33 leaves that test could not read were these.
func TestScopeExampleIsAWholeInvocation(t *testing.T) {
	res := Resource{APIPath: "policies", SingularKey: "policy", CLIName: "classic-policies"}
	got := scopeExample(res, "add")
	if !strings.Contains(got, "jamf-cli pro classic-policies scope add 1 --computer") {
		t.Errorf("example is not a whole invocation:\n%s", got)
	}
	// Only categories the resource has may appear.
	if strings.Contains(got, "--mobile-device") || strings.Contains(got, "--class") {
		t.Errorf("example demonstrates a category this resource refuses:\n%s", got)
	}
	// A resource with no exclusions section gets no exclusions example.
	rs := scopeExample(Resource{SingularKey: "restricted_software", CLIName: "classic-restricted-software"}, "add")
	if !strings.Contains(rs, "--section exclusion") {
		t.Errorf("restricted software has exclusions and should demonstrate them:\n%s", rs)
	}
	// A Resource with no CLIName must not render a double space.
	bare := scopeExample(Resource{SingularKey: "policy"}, "remove")
	if strings.Contains(bare, "pro  scope") {
		t.Errorf("missing CLIName rendered a gap:\n%s", bare)
	}
}
