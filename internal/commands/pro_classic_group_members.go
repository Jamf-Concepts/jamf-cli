// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/resolve"
)

// classicGroupKind describes one family of Classic static group for
// add-members / remove-members.
//
// Both families go through the Classic API rather than the modern one, and
// the choice is not arbitrary. Wire-checked 2026-10-04: PUT
// /v3/computer-groups/static-groups/{id} *replaces* the member list (a group of
// three became a group of one), so an add or a remove through it is a
// read-modify-write that races; PATCH /v2/mobile-device-groups/static-groups
// is incremental but takes numeric IDs only and needs groupName, siteId and
// assignments in every body. The Classic PUT carries additions and deletions
// as deltas, for either family, in one request.
type classicGroupKind struct {
	label      string // "computer" / "mobile device"
	groupLabel string // "computer group" / "mobile device group"
	flag       string // member flag: "computer" / "mobile-device"
	listPath   string // Classic collection, for a --name lookup
	detailPath string // Classic item path, one %s for the group id
	root       string // body root element
	membersKey string // detail key holding the members
	element    string // one member's element
	additions  string // additions wrapper element
	deletions  string // deletions wrapper element

	resolveGroup   func(ctx context.Context, client registry.HTTPClient, name string) (resolve.ClassicGroup, error)
	resolveEntries func(ctx context.Context, client registry.HTTPClient, entries []string) ([]*resolve.DeviceIdentifiers, int, error)
	resolveFile    func(ctx context.Context, client registry.HTTPClient, path string) ([]*resolve.DeviceIdentifiers, int, error)
	sourceGroupIDs func(ctx context.Context, client registry.HTTPClient, name string) ([]string, error)
}

var classicComputerGroupKind = classicGroupKind{
	label:          "computer",
	groupLabel:     "computer group",
	flag:           "computer",
	listPath:       "/JSSResource/computergroups",
	detailPath:     "/JSSResource/computergroups/id/%s",
	root:           "computer_group",
	membersKey:     "computers",
	element:        "computer",
	additions:      "computer_additions",
	deletions:      "computer_deletions",
	resolveGroup:   resolve.ResolveClassicComputerGroup,
	resolveEntries: resolve.ResolveComputerEntries,
	resolveFile:    resolve.ResolveComputersFromFile,
	sourceGroupIDs: resolve.ResolveComputerGroupMemberIDs,
}

var classicMobileGroupKind = classicGroupKind{
	label:          "mobile device",
	groupLabel:     "mobile device group",
	flag:           "mobile-device",
	listPath:       "/JSSResource/mobiledevicegroups",
	detailPath:     "/JSSResource/mobiledevicegroups/id/%s",
	root:           "mobile_device_group",
	membersKey:     "mobile_devices",
	element:        "mobile_device",
	additions:      "mobile_device_additions",
	deletions:      "mobile_device_deletions",
	resolveGroup:   resolve.ResolveClassicMobileGroup,
	resolveEntries: resolve.ResolveMobileDeviceEntries,
	resolveFile:    resolve.ResolveMobileDevicesFromFile,
	sourceGroupIDs: resolve.ResolveMobileDeviceGroupMemberIDs,
}

// groupMembersChunkSize caps the members one PUT carries. The Classic PUT is
// atomic — one entry it cannot match rejects the whole request — so a chunk is
// also the unit a failure costs. Every entry is resolved before it is sent, so
// a chunk fails for a reason about the request rather than one bad line.
const groupMembersChunkSize = 250

func newClassicComputerGroupAddMembersCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return newClassicGroupMembersCmd(cliCtx, classicComputerGroupKind, true)
}

func newClassicComputerGroupRemoveMembersCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return newClassicGroupMembersCmd(cliCtx, classicComputerGroupKind, false)
}

func newClassicMobileGroupAddMembersCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return newClassicGroupMembersCmd(cliCtx, classicMobileGroupKind, true)
}

func newClassicMobileGroupRemoveMembersCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return newClassicGroupMembersCmd(cliCtx, classicMobileGroupKind, false)
}

// groupMembersOpts holds one add-members / remove-members invocation.
type groupMembersOpts struct {
	groupName string
	members   []string
	fromFile  string
	fromGroup string
	yes       bool
}

// isList reports whether the targets came from a list rather than being named
// one by one on the command line, which is what puts a preview between the
// operator and the write.
func (o groupMembersOpts) isList() bool { return o.fromFile != "" || o.fromGroup != "" }

func newClassicGroupMembersCmd(cliCtx *registry.CLIContext, kind classicGroupKind, add bool) *cobra.Command {
	var o groupMembersOpts

	verb, prep, did := "add-members", "to", "Add"
	if !add {
		verb, prep, did = "remove-members", "from", "Remove"
	}
	plural := kind.label + "s"

	cmd := &cobra.Command{
		Use:   verb + " [<id>]",
		Short: fmt.Sprintf("%s %s %s a static %s", did, plural, prep, kind.groupLabel),
		Long: fmt.Sprintf(`%s %s %s a static %s, identified by ID (positional) or --name.

Members come from one of:
  --%s       a %s: ID, serial number, UDID, management ID or name (repeatable)
  --from-file      a file listing one of those per line (# comments ignored)
  --from-group     every member of another %s, smart or static

Every entry is resolved before anything is sent, and one that matches nothing,
or matches more than one %s, is reported and skipped; it counts as a failure
in the summary and exit code (use --allow-partial-failure to tolerate it).
%s

Members go %s the group in batched requests to the Classic API, and the group
is read back afterwards to confirm each change landed. Smart groups are refused:
their membership is computed from criteria.

With --from-file or --from-group the command prints a preview and changes
nothing unless --yes is given. Members named with --%s are written at once,
as one-off edits are; -n/--dry-run previews any form.

Output: the preview table, or one row per member with its result, on stdout;
progress on stderr.`,
			did, plural, prep, kind.groupLabel,
			kind.flag, kind.label, kind.groupLabel, kind.label,
			membershipSkipNote(kind, add), prep, kind.flag),
		Example: fmt.Sprintf(`  jamf-cli pro classic-%ss %s 42 --%s C02X1234 --%s 117
  jamf-cli pro classic-%ss %s --name "Quarantine" --from-file serials.txt --yes
  jamf-cli pro classic-%ss %s --name "Quarantine" --from-group "Lab Macs" -n`,
			strings.ReplaceAll(kind.groupLabel, " ", "-"), verb, kind.flag, kind.flag,
			strings.ReplaceAll(kind.groupLabel, " ", "-"), verb,
			strings.ReplaceAll(kind.groupLabel, " ", "-"), verb),
		Annotations: map[string]string{annotationAPI: apiProClassic},
		Args:        cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClassicGroupMembers(cmd, cliCtx, kind, add, args, o)
		},
	}

	cmd.Flags().StringVar(&o.groupName, "name", "", "group name (instead of the positional ID)")
	cmd.Flags().StringArrayVar(&o.members, kind.flag, nil,
		fmt.Sprintf("%s to %s: ID, serial number, UDID, management ID or name (repeatable)", kind.label, strings.TrimSuffix(verb, "-members")))
	cmd.Flags().StringVar(&o.fromFile, "from-file", "", "file listing one "+kind.label+" identifier per line (# comments ignored)")
	cmd.Flags().StringVar(&o.fromGroup, "from-group", "", "take every member of this "+kind.groupLabel+" (smart or static)")
	cmd.Flags().BoolVar(&o.yes, "yes", false, "apply a --from-file or --from-group change (default: preview only)")
	cmd.MarkFlagsMutuallyExclusive(kind.flag, "from-file", "from-group")
	cmd.MarkFlagsOneRequired(kind.flag, "from-file", "from-group")

	return markGatewayCoverage(cmd, "PUT", strings.Replace(kind.detailPath, "%s", "{id}", 1))
}

// membershipSkipNote says what happens to an entry already in the state asked
// for. The two families differ on the wire — removing a computer that is not a
// member answers 409 and rejects the whole request, the same for a mobile
// device is silently ignored — so the CLI reads the membership first and skips
// both, rather than send what one family refuses.
func membershipSkipNote(kind classicGroupKind, add bool) string {
	if add {
		return fmt.Sprintf("A %s already in the group is reported and left alone.", kind.label)
	}
	return fmt.Sprintf("A %s not in the group is reported and left alone.", kind.label)
}

// classicStaticGroup is a resolved target group and its current members.
type classicStaticGroup struct {
	id      string
	name    string
	members map[string]bool
}

func runClassicGroupMembers(cmd *cobra.Command, cliCtx *registry.CLIContext, kind classicGroupKind, add bool, args []string, o groupMembersOpts) error {
	ctx := cmd.Context()
	client := cliCtx.Client
	stderr := cmd.ErrOrStderr()

	switch {
	case len(args) == 1 && o.groupName != "":
		return fmt.Errorf("pass the group as an <id> or with --name, not both")
	case len(args) == 0 && o.groupName == "":
		return fmt.Errorf("a group is required: pass its <id> or --name")
	}

	group, err := fetchClassicStaticGroup(ctx, client, kind, args, o.groupName)
	if err != nil {
		return err
	}

	devices, unresolved, err := resolveGroupMemberTargets(ctx, client, kind, o)
	if err != nil {
		return err
	}
	if unresolved > 0 {
		_, _ = fmt.Fprintf(stderr, "warning: %d %s identifier(s) could not be resolved and will be skipped\n", unresolved, kind.label)
	}

	plan := planMembershipChange(kind, group, devices, add)
	failures := unresolved + len(plan.refused)
	if len(plan.change) == 0 {
		_, _ = fmt.Fprintf(stderr, "Nothing to change in %s %q: %s%s.\n",
			kind.groupLabel, group.name, plan.noChangeReason(kind, add), unresolvedNote(unresolved))
		if err := printRows(cliCtx, plan.rows); err != nil {
			return err
		}
		return finishBatch(stderr, "entries", len(plan.skipped), failures, nil)
	}

	action := "add"
	if !add {
		action = "remove"
	}
	if dryRun || (o.isList() && !o.yes) {
		tail := ""
		if !dryRun {
			tail = " (use --yes to apply)"
		}
		_, _ = fmt.Fprintf(stderr, "[dry-run] Would %s %d %s(s) %s %s %q (id %s)%s:\n",
			action, len(plan.change), kind.label, directionWord(add), kind.groupLabel, group.name, group.id, tail)
		deviceActionPreviewTable(plan.rows)
		return nil
	}

	_, _ = fmt.Fprintf(stderr, "%sing %d %s(s) %s %s %q...\n",
		capitalize(strings.TrimSuffix(action, "e")), len(plan.change), kind.label, directionWord(add), kind.groupLabel, group.name)

	failed := map[string]error{}
	var firstErr error
	record := func(id string, err error) {
		failed[id] = err
		if firstErr == nil {
			firstErr = err
		}
	}
	currentMembers := func() (map[string]bool, error) {
		g, err := fetchClassicStaticGroup(ctx, client, kind, []string{group.id}, "")
		if err != nil {
			return nil, err
		}
		return g.members, nil
	}
	for chunk := range slices.Chunk(plan.change, groupMembersChunkSize) {
		applyMembershipChunk(ctx, client, kind, group.id, chunk, add, record, currentMembers)
	}

	// Read the group back: a Classic write answering success is not proof it
	// applied (an out-of-order or unrecognised element is accepted and
	// dropped), so each change is confirmed against what the server now holds.
	// A change that cannot be confirmed is not reported as made: it counts as
	// a failure, with a result saying it was sent but not verified.
	after, err := currentMembers()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "warning: could not read %s %q back to confirm the change: %v\n", kind.groupLabel, group.name, err)
	}
	unverified := map[string]bool{}
	for _, d := range plan.change {
		if _, ok := failed[d.ID]; ok {
			continue
		}
		if after == nil {
			unverified[d.ID] = true
			record(d.ID, fmt.Errorf("sent, but the group could not be read back to confirm it: %w", err))
			continue
		}
		if after[d.ID] != add {
			state := "is still a member"
			if add {
				state = "is still not a member"
			}
			record(d.ID, fmt.Errorf("the server answered success but the %s %s", kind.label, state))
		}
	}

	done := "added"
	if !add {
		done = "removed"
	}
	for i, row := range plan.rows {
		id, _ := row["id"].(string)
		if row["result"] != action {
			continue
		}
		if unverified[id] {
			plan.rows[i]["result"] = "unverified: sent, but the group could not be read back"
		} else if err, ok := failed[id]; ok {
			plan.rows[i]["result"] = "failed: " + err.Error()
		} else {
			plan.rows[i]["result"] = done
		}
	}

	changed := len(plan.change) - len(failed)
	failures += len(failed)
	_, _ = fmt.Fprintf(stderr, "%s %d, %d failed, %d unchanged%s.\n",
		capitalize(done), changed, len(failed)+len(plan.refused), len(plan.skipped), unresolvedNote(unresolved))
	if err := printRows(cliCtx, plan.rows); err != nil {
		return err
	}
	return finishBatch(stderr, "entries", changed+len(plan.skipped), failures, firstErr)
}

// unmanagedRefusal matches the Classic API's refusal of unmanaged members,
// which names every offending ID. Wire-checked 2026-10-04 on Jamf Pro: "Error:
// The computers with the following IDs are unmanaged and cannot be added to a
// computer group: 284, 287" (and "The devices with ..." for mobile devices).
var unmanagedRefusal = regexp.MustCompile(`following IDs are unmanaged and cannot be added to a [a-z ]+ group: ([0-9][0-9, ]*)`)

// staleDeletion matches the Classic API's refusal to remove a computer that is
// not a member, which fails the whole request and names only the first such
// ID. Wire-checked 2026-10-04: "Error: Unable to match computer in deletions
// list id= 304". A mobile device group ignores the same deletion instead.
var staleDeletion = regexp.MustCompile(`Unable to match [a-z ]+ in deletions list`)

// applyMembershipChunk sends one chunk and records each failure. The PUT is
// atomic, so one member the server refuses fails every member sent with it;
// two refusals are narrowed and the rest of the chunk is sent once more:
//
//   - an unmanaged member the server names. The managed check in
//     planMembershipChange catches these before anything is sent whenever
//     inventory reports the state; this is for when it did not.
//   - a removal of a computer another writer took out of the group since the
//     snapshot. The group is read again and only its current members are
//     sent; the ones already gone are in the state asked for, which the
//     read-back confirms.
func applyMembershipChunk(ctx context.Context, client registry.HTTPClient, kind classicGroupKind, groupID string,
	chunk []*resolve.DeviceIdentifiers, add bool, record func(id string, err error),
	currentMembers func() (map[string]bool, error),
) {
	err := putGroupMembership(ctx, client, kind, groupID, chunk, add)
	if err == nil {
		return
	}
	if !add && staleDeletion.MatchString(err.Error()) {
		retryStillMembers(ctx, client, kind, groupID, chunk, err, record, currentMembers)
		return
	}
	m := unmanagedRefusal.FindStringSubmatch(err.Error())
	if m == nil {
		for _, d := range chunk {
			record(d.ID, err)
		}
		return
	}
	named := map[string]bool{}
	for _, id := range strings.Split(m[1], ",") {
		named[strings.TrimSpace(id)] = true
	}
	var rest []*resolve.DeviceIdentifiers
	for _, d := range chunk {
		if named[d.ID] {
			record(d.ID, fmt.Errorf("unmanaged: Jamf Pro refuses an unmanaged %s as a static group member", kind.label))
		} else {
			rest = append(rest, d)
		}
	}
	switch {
	case len(rest) == 0:
		return
	case len(rest) == len(chunk):
		// The IDs it named are none of the ones sent: no narrower retry exists.
		for _, d := range rest {
			record(d.ID, err)
		}
		return
	}
	if err := putGroupMembership(ctx, client, kind, groupID, rest, add); err != nil {
		for _, d := range rest {
			record(d.ID, err)
		}
	}
}

// retryStillMembers re-sends a refused removal with only the chunk's members
// the group still holds. When the read fails, or every one is still a member
// (so the refusal is not the stale snapshot), the original error stands.
func retryStillMembers(ctx context.Context, client registry.HTTPClient, kind classicGroupKind, groupID string,
	chunk []*resolve.DeviceIdentifiers, refusal error, record func(id string, err error),
	currentMembers func() (map[string]bool, error),
) {
	members, err := currentMembers()
	if err != nil {
		for _, d := range chunk {
			record(d.ID, refusal)
		}
		return
	}
	var rest []*resolve.DeviceIdentifiers
	for _, d := range chunk {
		if members[d.ID] {
			rest = append(rest, d)
		}
	}
	switch {
	case len(rest) == 0:
		return
	case len(rest) == len(chunk):
		for _, d := range rest {
			record(d.ID, refusal)
		}
		return
	}
	if err := putGroupMembership(ctx, client, kind, groupID, rest, false); err != nil {
		for _, d := range rest {
			record(d.ID, err)
		}
	}
}

// fetchClassicStaticGroup reads the target group by ID, or by name through the
// collision-refusing Classic lookup, and refuses a smart group.
func fetchClassicStaticGroup(ctx context.Context, client registry.HTTPClient, kind classicGroupKind, args []string, name string) (*classicStaticGroup, error) {
	id := ""
	if len(args) == 1 {
		id = strings.TrimSpace(args[0])
		if !resolve.IsNumericID(id) {
			return nil, fmt.Errorf("%q is not a %s ID; pass a name with --name", id, kind.groupLabel)
		}
	} else {
		g, err := kind.resolveGroup(ctx, client, name)
		if err != nil {
			return nil, err
		}
		id = g.ID
	}

	data, err := fetchJSON(ctx, client, fmt.Sprintf(kind.detailPath, url.PathEscape(id)))
	if err != nil {
		return nil, fmt.Errorf("reading %s %s: %w", kind.groupLabel, id, err)
	}
	detail := unwrapClassicDetail(data)
	if smart, _ := detail["is_smart"].(bool); smart {
		gname, _ := detail["name"].(string)
		return nil, fmt.Errorf("%s %q (id %s) is a smart group; its membership is computed from criteria and cannot be edited", kind.groupLabel, gname, id)
	}

	g := &classicStaticGroup{id: id, members: map[string]bool{}}
	g.name, _ = detail["name"].(string)
	for _, m := range extractScopeItems(detail, kind.membersKey, kind.element) {
		if mid := extractID(m); mid != "" {
			g.members[mid] = true
		}
	}
	return g, nil
}

// resolveGroupMemberTargets turns the one member source given into devices.
func resolveGroupMemberTargets(ctx context.Context, client registry.HTTPClient, kind classicGroupKind, o groupMembersOpts) ([]*resolve.DeviceIdentifiers, int, error) {
	switch {
	case o.fromFile != "":
		return kind.resolveFile(ctx, client, o.fromFile)
	case o.fromGroup != "":
		ids, err := kind.sourceGroupIDs(ctx, client, o.fromGroup)
		if err != nil {
			return nil, 0, err
		}
		if len(ids) == 0 {
			return nil, 0, fmt.Errorf("%s %q has no members", kind.groupLabel, o.fromGroup)
		}
		return kind.resolveEntries(ctx, client, ids)
	default:
		return kind.resolveEntries(ctx, client, o.members)
	}
}

// membershipPlan splits resolved devices into the ones to change, the ones
// already in the state asked for, and the ones the server would refuse, with
// one display row per distinct device.
type membershipPlan struct {
	change  []*resolve.DeviceIdentifiers
	skipped []*resolve.DeviceIdentifiers
	refused []*resolve.DeviceIdentifiers
	rows    []map[string]any
}

func planMembershipChange(kind classicGroupKind, group *classicStaticGroup, devices []*resolve.DeviceIdentifiers, add bool) membershipPlan {
	action, skip := "add", "already a member"
	if !add {
		action, skip = "remove", "not a member"
	}
	var p membershipPlan
	seen := map[string]bool{}
	for _, d := range devices {
		if seen[d.ID] {
			continue
		}
		seen[d.ID] = true
		result := action
		switch {
		case group.members[d.ID] == add:
			result = skip
			p.skipped = append(p.skipped, d)
		case add && d.Managed != nil && !*d.Managed:
			// Sending it would fail the whole request, every managed member
			// with it, so it is held back and reported instead.
			result = "refused: unmanaged " + kind.label + "s cannot be static group members"
			p.refused = append(p.refused, d)
		default:
			p.change = append(p.change, d)
		}
		p.rows = append(p.rows, map[string]any{
			"id":     d.ID,
			"name":   d.Name,
			"serial": d.SerialNumber,
			"group":  group.name,
			"result": result,
		})
	}
	return p
}

func (p membershipPlan) noChangeReason(kind classicGroupKind, add bool) string {
	if len(p.refused) > 0 {
		return fmt.Sprintf("%d unmanaged %s(s) refused, the rest already members", len(p.refused), kind.label)
	}
	if add {
		return fmt.Sprintf("every %s named is already a member", kind.label)
	}
	return fmt.Sprintf("no %s named is a member", kind.label)
}

// groupMembershipXML builds the Classic delta body for one chunk: the members
// to add or remove, by numeric ID, and nothing else, so no other field of the
// group is touched.
func groupMembershipXML(kind classicGroupKind, devices []*resolve.DeviceIdentifiers, add bool) string {
	wrapper := kind.additions
	if !add {
		wrapper = kind.deletions
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	fmt.Fprintf(&b, "<%s><%s>", kind.root, wrapper)
	for _, d := range devices {
		fmt.Fprintf(&b, "<%s><id>", kind.element)
		// The ID is server-sourced and numeric; escaping keeps the builder
		// safe on its own terms rather than on its callers'.
		_ = xml.EscapeText(&b, []byte(d.ID))
		fmt.Fprintf(&b, "</id></%s>", kind.element)
	}
	fmt.Fprintf(&b, "</%s></%s>", wrapper, kind.root)
	return b.String()
}

func putGroupMembership(ctx context.Context, client registry.HTTPClient, kind classicGroupKind, groupID string, devices []*resolve.DeviceIdentifiers, add bool) error {
	resp, err := client.Do(ctx, "PUT", fmt.Sprintf(kind.detailPath, url.PathEscape(groupID)),
		strings.NewReader(groupMembershipXML(kind, devices, add)))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
