// Copyright 2026, Jamf Software LLC

package scope

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// udidRe matches a 40-character hex string — the format of Apple device UDIDs.
var udidRe = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// numericRe matches a plain integer string (Jamf Pro Classic API numeric ID).
var numericRe = regexp.MustCompile(`^[0-9]+$`)

// namedItemFromIdentifier builds a NamedItem with the correct field populated
// based on what the caller passed: a 40-char hex UDID, a numeric ID, or a name.
// Used for individual device scope targets where the API accepts any of the three.
func namedItemFromIdentifier(value string) NamedItem {
	switch {
	case udidRe.MatchString(value):
		return NamedItem{UDID: value}
	case numericRe.MatchString(value):
		return NamedItem{ID: value}
	default:
		return NamedItem{Name: value}
	}
}

// isDeviceFlag returns true for flags that target individual devices (not groups),
// where the caller may pass a UDID or numeric ID rather than a name.
func isDeviceFlag(flagName string) bool {
	return flagName == "mobile-device" || flagName == "computer"
}

// Ref identifies which object a scope command addresses: exactly one of an ID
// or a name, matching every other Classic command's `<id>` positional plus
// `--name` flag.
type Ref struct {
	ID   string
	Name string
}

// String renders the reference for an error message, so a failure names what
// the caller actually typed rather than always saying "name".
func (r Ref) String() string {
	if r.ID != "" {
		return "id " + r.ID
	}
	return fmt.Sprintf("%q", r.Name)
}

// NewRef builds a Ref from a scope command's positional and --name flag,
// refusing the two together and the two absent.
//
// Rejecting `<id>` and `--name` together is the Platform convention (see
// CLAUDE.md, Identifier convention) and matters more here than elsewhere:
// there is no correct resolution when they disagree, and preferring one
// silently would mutate the scope of an object the caller did not name.
func NewRef(args []string, flagName string) (Ref, error) {
	var positional string
	if len(args) > 0 {
		positional = args[0]
	}
	switch {
	case positional != "" && flagName != "":
		return Ref{}, exitcode.New(exitcode.Usage, "pass either an <id> argument or --name, not both")
	case positional != "":
		// The positional used to be the NAME on these commands — they were the
		// only ones in the binary that worked that way — so the commonest
		// migration mistake is `scope get "My Policy"`. Sent as an id it costs
		// a request and returns a 404 whose hint points at `list`, saying
		// nothing about --name. Refusing here is what makes the breaking
		// change navigable.
		//
		// The sibling generated commands need no equivalent: their positional
		// has always been an id, so no caller is migrating. Deliberately a
		// refusal rather than a sniff — `blueprints import-profile` guesses
		// between the two for a documented compat reason, and CLAUDE.md says
		// not to copy that without one.
		if !numericRe.MatchString(positional) {
			return Ref{}, exitcode.New(exitcode.Usage,
				fmt.Sprintf("%q is not an id; Classic ids are numeric", positional)).
				WithHint(fmt.Sprintf("pass a name as --name %q — the positional argument used to be the name on these commands and is now the id", positional))
		}
		return Ref{ID: positional}, nil
	case flagName != "":
		return Ref{Name: flagName}, nil
	}
	return Ref{}, exitcode.New(exitcode.Usage, "provide an <id> argument or --name")
}

// FetchScope GETs a Classic API resource and returns its ID and parsed scope.
//
// An ID is one request. A name is one request too for most resources, via the
// Classic /name/{name} endpoint, whose response carries <general><id> — the
// ID needed for the subsequent PUT. Only a resource with no /name/ endpoint
// (res.ResolveByList: the VPP pair) costs two, listing the collection to
// resolve the name first.
func FetchScope(ctx context.Context, client registry.HTTPClient, res Resource, ref Ref) (string, *ScopeXML, error) {
	fetchPath, resolvedID, err := scopeFetchPath(ctx, client, res, ref)
	if err != nil {
		return "", nil, err
	}

	resp, err := client.Do(ctx, "GET", fetchPath, nil)
	if err != nil {
		return "", nil, fmt.Errorf("fetching %s %s: %w", res.SingularKey, ref, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return "", nil, fmt.Errorf("reading response: %w", err)
	}

	var envelope classicResourceXML
	if err := xml.Unmarshal(body, &envelope); err != nil {
		return "", nil, fmt.Errorf("parsing %s XML: %w", res.SingularKey, err)
	}

	if resolvedID != "" {
		return resolvedID, &envelope.Scope, nil
	}
	if envelope.General.ID == "" {
		return "", nil, fmt.Errorf("no ID in %s %s", res.SingularKey, ref)
	}
	return envelope.General.ID, &envelope.Scope, nil
}

// scopeFetchPath picks the GET path for a reference, resolving a name through
// the collection listing only where the resource has no /name/ endpoint.
// Returns the already-known ID when there is one, so FetchScope does not have
// to re-derive it from a response body that may not carry <general><id>.
func scopeFetchPath(ctx context.Context, client registry.HTTPClient, res Resource, ref Ref) (path, resolvedID string, err error) {
	if ref.ID != "" {
		return fmt.Sprintf("/JSSResource/%s/id/%s", res.APIPath, url.PathEscape(ref.ID)), ref.ID, nil
	}
	if res.ResolveByList {
		id, err := resolveNameToID(ctx, client, res.APIPath, res.SingularKey, ref.Name)
		if err != nil {
			return "", "", err
		}
		return fmt.Sprintf("/JSSResource/%s/id/%s", res.APIPath, url.PathEscape(id)), id, nil
	}
	return fmt.Sprintf("/JSSResource/%s/name/%s", res.APIPath, registry.EscapeClassicPathSegment(ref.Name)), "", nil
}

// resolveNameToID lists all records at the resource root and returns the ID of
// the record whose <name> matches case-insensitively.
//
// Two matches is an error, not a coin toss: Classic names are not unique (a
// live tenant carried two ebooks with the same name), and picking the first in
// document order would silently rewrite the scope of whichever one the server
// happened to list first. Only the resources with no /name/ endpoint reach
// this — for the rest the server resolves the name and owns that choice.
func resolveNameToID(ctx context.Context, client registry.HTTPClient, apiPath, singularKey, name string) (string, error) {
	resp, err := client.Do(ctx, "GET", "/JSSResource/"+apiPath, nil)
	if err != nil {
		return "", fmt.Errorf("listing %s: %w", singularKey, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return "", fmt.Errorf("reading list response: %w", err)
	}

	var matches []string
	d := xml.NewDecoder(bytes.NewReader(body))
	depth := 0
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("parsing %s list XML: %w", singularKey, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth == 2 {
				var it struct {
					ID   string `xml:"id"`
					Name string `xml:"name"`
				}
				if decErr := d.DecodeElement(&it, &t); decErr == nil && strings.EqualFold(it.Name, name) {
					matches = append(matches, it.ID)
				}
				depth-- // DecodeElement consumed the end element
			}
		case xml.EndElement:
			depth--
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("%s %q not found", singularKey, name)
	case 1:
		return matches[0], nil
	}
	return "", fmt.Errorf("%d %s records are named %q (ids %s); pass one as the <id> argument instead",
		len(matches), singularKey, name, strings.Join(matches, ", "))
}

// PutScope writes an updated scope back to the Classic API, sending a body
// that carries nothing but the <scope> element.
//
// Two properties of the Classic API make this the right shape, both
// wire-checked 2026-09-12 against Jamf Pro 11.31.1 on a direct instance and
// through the platform gateway:
//
//   - A PUT is a partial update at top-level-section granularity: a body of
//     just <scope> applies the scope and leaves general, self_service,
//     packages, payloads and every other section byte-identical.
//   - <scope> itself is replaced wholesale. A body carrying only some scope
//     categories wipes the rest, so the whole block has to be sent — which is
//     what the caller's ScopeXML, read from the server and edited in place,
//     already is. An empty category element is what clears a category.
//
// The body is marshalled by encoding/xml rather than spliced into the document
// the server returned, and the element ORDER that produces is load-bearing.
// The Classic API's XML binding is sequence-ordered: it reads scope children in
// schema order and silently ignores what arrives out of order, answering 200
// either way. The order a GET returns is not that order — macapplications, for
// one, answers <exclusions> as buildings, departments, mobile_device_groups,
// users, ... — so echoing the server's own bytes back is accepted and applied to
// nothing. ScopeXML's field order is the schema order; keep it that way.
//
// The /subset/Scope shortcut is not used: the Jamf Platform Gateway's Classic
// proxy forwards only top-level Classic paths, so it answers 403 there while
// working on a direct instance (re-probed 2026-09-12). One code path that works
// on both beats two that disagree.
// CLASS TARGETS NEED TWO WRITES. Jamf Pro's Classic API stores <classes> only
// while the stored category is EMPTY. A write made while it already holds a
// member clears it — carrying the identical value or omitting it both clear
// it, and the child's identifier shape makes no difference (wire-checked
// 2026-09-12 on Jamf Pro 11.31.1, 5/5 each way). Since a scope PUT replaces
// <scope> wholesale, no single request can preserve an existing class across
// any other scope change.
//
// So a scope with class members is delivered in two requests: the first
// carries every intended change with <classes> emptied, leaving the category
// empty; the second carries the same scope with the classes populated, which
// the server now accepts. Verified 5/5 — a department added to an ebook that
// already had a class ends up with both.
//
// Three properties make that safe rather than clever:
//
//   - It is never worse than one request. The first PUT already carries the
//     caller's real change, so an interruption between the two leaves exactly
//     what a single PUT would have left: the change applied, the classes gone.
//   - It is keyed on the CATEGORY, not the resource. Only ebooks carry
//     <classes> today, but the rule is a property of the element, so a
//     resource that gains one is handled with no edit here.
//   - A scope with no class members takes the single-request path, so the
//     common case is unchanged and the extra write happens only where it is
//     the difference between working and silently losing data.
func PutScope(ctx context.Context, client registry.HTTPClient, res Resource, id string, s *ScopeXML) error {
	path := fmt.Sprintf("/JSSResource/%s/id/%s", res.APIPath, url.PathEscape(id))

	// A scope carrying class members is delivered in two requests; everything
	// else in one. See the class-target note in this function's doc comment.
	if len(s.Classes.Items) > 0 {
		cleared := *s
		cleared.Classes = ScopeItemSlice{ElemName: s.Classes.ElemName}
		if err := putScopeOnce(ctx, client, res, path, &cleared); err != nil {
			return err
		}
	}
	return putScopeOnce(ctx, client, res, path, s)
}

// putScopeOnce sends one scope-only PUT.
func putScopeOnce(ctx context.Context, client registry.HTTPClient, res Resource, path string, s *ScopeXML) error {
	body, err := marshalScopeBody(res.SingularKey, s)
	if err != nil {
		return err
	}
	resp, err := client.Do(ctx, "PUT", path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("updating scope: %w", err)
	}
	_ = resp.Body.Close()
	return nil
}

// marshalScopeBody renders <singularKey><scope>…</scope></singularKey>, the
// smallest body that updates a Classic resource's scope.
func marshalScopeBody(singularKey string, s *ScopeXML) ([]byte, error) {
	scopeXML, err := xml.MarshalIndent(s, "  ", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling scope: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	buf.WriteString("<" + singularKey + ">\n")
	buf.Write(scopeXML)
	buf.WriteString("\n</" + singularKey + ">\n")
	return buf.Bytes(), nil
}

func silentDropError(singularKey, section, flagName, itemName string, expectedPresent bool) error {
	if expectedPresent {
		return fmt.Errorf("the server accepted the write but %s %q is not in the %s scope of this %s; the likeliest cause is that the identifier names no existing record",
			flagName, itemName, section, singularKey)
	}
	return fmt.Errorf("the server accepted the write but %s %q is still in the %s scope of this %s",
		flagName, itemName, section, singularKey)
}

// AddToScope adds a named item to the given scope section. Returns true if the
// item was added, false if already present (idempotent no-op).
//
// singularKey is no longer read: the policy-only <limit_to_users> branch this
// used to carry is gone, the server keeping that element in step with
// <limitations><user_groups> by itself (see ScopeXML).
func AddToScope(s *ScopeXML, section, flagName, name string) bool {
	items := getOrCreateScopeItems(s, section, flagName)
	if items == nil {
		return false
	}

	for _, item := range items.Items {
		if strings.EqualFold(item.Name, name) ||
			(item.ID != "" && item.ID == name) ||
			(item.UDID != "" && strings.EqualFold(item.UDID, name)) {
			return false
		}
	}

	if items.ElemName == "" {
		items.ElemName = flagToElemName[flagName]
	}
	var item NamedItem
	if isDeviceFlag(flagName) {
		item = namedItemFromIdentifier(name)
	} else {
		item = NamedItem{Name: name}
	}
	items.Items = append(items.Items, item)
	return true
}

// RemoveFromScope removes a named item from the given scope section. Returns
// true if removed, false if not found (idempotent no-op).
func RemoveFromScope(s *ScopeXML, section, flagName, name string) bool {
	return removeNamedItem(readScopeItems(s, section, flagName), name)
}

// OutputScope writes the scope to the output formatter. The column formats get
// the scope flattened into rows; json, yaml, ndjson, xml and raw get the full
// structure.
//
// The keep-set is named and the flattened shape is the default, rather than the
// other way round, because the format string is not normalised: this used to
// match "table", "csv" and "plain" exactly, so any other value — a mis-cased
// -o Table, or the internal json-multi that means JSON on the wire and a table
// on the screen — took the nested structure to a table renderer. See
// output.RendersStructureVerbatim.
func OutputScope(out registry.OutputFormatter, s *ScopeXML, format string) error {
	if output.RendersStructureVerbatim(format) {
		data, err := json.Marshal(s)
		if err != nil {
			return err
		}
		return out.PrintRaw(data)
	}

	rows := FlattenScope(s)
	if len(rows) == 0 {
		fmt.Fprintln(os.Stderr, "Scope is empty")
		return nil
	}
	data, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	return out.PrintRaw(data)
}

// FlattenScope converts a ScopeXML into a flat list of rows for table output.
//
// Every category the XML model carries is emitted, so what `scope get` shows
// is exactly what `scope add`/`remove` can address. The two that used to be
// missing were the ones a caller could see in the admin UI and not here:
// iBeacon limitations and exclusions, and target classes.
func FlattenScope(s *ScopeXML) []map[string]any {
	var rows []map[string]any

	if s.AllComputers {
		rows = append(rows, map[string]any{"section": SectionTarget, "type": elemAllComputers, "name": "true"})
	}
	if s.AllMobileDevices {
		rows = append(rows, map[string]any{"section": SectionTarget, "type": elemAllMobileDevices, "name": "true"})
	}
	if s.AllJSSUsers {
		rows = append(rows, map[string]any{"section": SectionTarget, "type": elemAllJSSUsers, "name": "true"})
	}

	appendNamedRows(&rows, SectionTarget, "computer", s.Computers.Items)
	appendNamedRows(&rows, SectionTarget, "computer_group", s.ComputerGroups.Items)
	appendNamedRows(&rows, SectionTarget, "mobile_device", s.MobileDevices.Items)
	appendNamedRows(&rows, SectionTarget, "mobile_device_group", s.MobileDeviceGroups.Items)
	appendNamedRows(&rows, SectionTarget, "building", s.Buildings.Items)
	appendNamedRows(&rows, SectionTarget, "department", s.Departments.Items)
	appendNamedRows(&rows, SectionTarget, "jss_user", s.JSSUsers.Items)
	appendNamedRows(&rows, SectionTarget, "jss_user_group", s.JSSUserGroups.Items)
	appendNamedRows(&rows, SectionTarget, "class", s.Classes.Items)

	if s.Limitations != nil {
		appendNamedRows(&rows, SectionLimitation, "user", s.Limitations.Users.Items)
		appendNamedRows(&rows, SectionLimitation, "user_group", s.Limitations.UserGroups.Items)
		appendNamedRows(&rows, SectionLimitation, "network_segment", s.Limitations.NetworkSegments.Items)
		appendNamedRows(&rows, SectionLimitation, "ibeacon", s.Limitations.IBeacons.Items)
	}

	if s.Exclusions != nil {
		appendNamedRows(&rows, SectionExclusion, "computer", s.Exclusions.Computers.Items)
		appendNamedRows(&rows, SectionExclusion, "computer_group", s.Exclusions.ComputerGroups.Items)
		appendNamedRows(&rows, SectionExclusion, "mobile_device", s.Exclusions.MobileDevices.Items)
		appendNamedRows(&rows, SectionExclusion, "mobile_device_group", s.Exclusions.MobileDeviceGroups.Items)
		appendNamedRows(&rows, SectionExclusion, "user", s.Exclusions.Users.Items)
		appendNamedRows(&rows, SectionExclusion, "user_group", s.Exclusions.UserGroups.Items)
		appendNamedRows(&rows, SectionExclusion, "jss_user", s.Exclusions.JSSUsers.Items)
		appendNamedRows(&rows, SectionExclusion, "jss_user_group", s.Exclusions.JSSUserGroups.Items)
		appendNamedRows(&rows, SectionExclusion, "network_segment", s.Exclusions.NetworkSegments.Items)
		appendNamedRows(&rows, SectionExclusion, "building", s.Exclusions.Buildings.Items)
		appendNamedRows(&rows, SectionExclusion, "department", s.Exclusions.Departments.Items)
		appendNamedRows(&rows, SectionExclusion, "ibeacon", s.Exclusions.IBeacons.Items)
	}

	return rows
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

func removeNamedItem(items *ScopeItemSlice, name string) bool {
	if items == nil {
		return false
	}
	var keep []NamedItem
	found := false
	for _, item := range items.Items {
		if strings.EqualFold(item.Name, name) ||
			(item.ID != "" && item.ID == name) ||
			(item.UDID != "" && strings.EqualFold(item.UDID, name)) {
			found = true
			continue
		}
		keep = append(keep, item)
	}
	if !found {
		return false
	}
	items.Items = keep
	return true
}

// getOrCreateScopeItems returns the slice a flag addresses in a section,
// materialising the section wrapper when the server omitted it.
func getOrCreateScopeItems(s *ScopeXML, section, flagName string) *ScopeItemSlice {
	switch section {
	case SectionTarget:
		return targetItems(s, flagName)
	case SectionLimitation:
		if s.Limitations == nil {
			s.Limitations = &LimitationsXML{}
		}
		return limitationItems(s.Limitations, flagName)
	case SectionExclusion:
		if s.Exclusions == nil {
			s.Exclusions = &ExclusionsXML{}
		}
		return exclusionItems(s.Exclusions, flagName)
	}
	return nil
}

// readScopeItems is getOrCreateScopeItems without the materialising, for the
// read and remove paths where an absent section means "nothing to find".
func readScopeItems(s *ScopeXML, section, flagName string) *ScopeItemSlice {
	switch section {
	case SectionTarget:
		return targetItems(s, flagName)
	case SectionLimitation:
		if s.Limitations == nil {
			return nil
		}
		return limitationItems(s.Limitations, flagName)
	case SectionExclusion:
		if s.Exclusions == nil {
			return nil
		}
		return exclusionItems(s.Exclusions, flagName)
	}
	return nil
}

// targetItems maps a flag onto its target slice. There is no iBeacon target:
// an iBeacon narrows an audience, it does not select one, so it exists only in
// limitations and exclusions.
func targetItems(s *ScopeXML, flagName string) *ScopeItemSlice {
	switch flagName {
	case flagComputer:
		return &s.Computers
	case flagComputerGroup:
		return &s.ComputerGroups
	case flagMobileDevice:
		return &s.MobileDevices
	case flagMobileDeviceGroup:
		return &s.MobileDeviceGroups
	case flagBuilding:
		return &s.Buildings
	case flagDepartment:
		return &s.Departments
	case flagJSSUserGroup:
		return &s.JSSUserGroups
	case flagJSSUser:
		return &s.JSSUsers
	case flagClass:
		return &s.Classes
	}
	return nil
}

func limitationItems(lim *LimitationsXML, flagName string) *ScopeItemSlice {
	switch flagName {
	case flagNetworkSegment:
		return &lim.NetworkSegments
	case flagUser:
		return &lim.Users
	case flagUserGroup:
		return &lim.UserGroups
	case flagIBeacon:
		return &lim.IBeacons
	}
	return nil
}

func exclusionItems(exc *ExclusionsXML, flagName string) *ScopeItemSlice {
	switch flagName {
	case flagComputer:
		return &exc.Computers
	case flagComputerGroup:
		return &exc.ComputerGroups
	case flagMobileDevice:
		return &exc.MobileDevices
	case flagMobileDeviceGroup:
		return &exc.MobileDeviceGroups
	case flagUser:
		return &exc.Users
	case flagUserGroup:
		return &exc.UserGroups
	case flagJSSUserGroup:
		return &exc.JSSUserGroups
	case flagJSSUser:
		return &exc.JSSUsers
	case flagNetworkSegment:
		return &exc.NetworkSegments
	case flagBuilding:
		return &exc.Buildings
	case flagDepartment:
		return &exc.Departments
	case flagIBeacon:
		return &exc.IBeacons
	}
	return nil
}

func appendNamedRows(rows *[]map[string]any, section, typeName string, items []NamedItem) {
	for _, item := range items {
		if item.Name != "" {
			*rows = append(*rows, map[string]any{"section": section, "type": typeName, "name": item.Name})
		}
	}
}
