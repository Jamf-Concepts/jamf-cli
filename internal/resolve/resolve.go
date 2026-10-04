// Copyright 2026, Jamf Software LLC

// Package resolve provides device identifier resolution for Jamf Pro.
// It translates serial numbers, device names, and numeric IDs into the
// full set of identifiers (ID, managementId, UDID) required by various
// API endpoints.
package resolve

import (
	"bufio"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/Jamf-Concepts/jamf-cli/internal/pickone"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/xmlconv"
)

// DeviceIdentifiers holds all ID forms needed by different action endpoints.
type DeviceIdentifiers struct {
	ID           string // numeric ID (string form, e.g. "42")
	ManagementID string // UUID for blank-push, ddm-sync
	UDID         string // device UDID for renew-mdm
	Name         string // display name for confirmation messages
	SerialNumber string // serial number for confirmation messages
	// Managed is whether Jamf Pro manages the device, or nil when the record
	// read did not say. A static group refuses an unmanaged member.
	Managed *bool
}

// deviceQuery is a lookup by one identifier field: the RSQL field the server
// filters on, the requested value, how it is described in errors, and how the
// same value is read back off a returned record.
type deviceQuery struct {
	field string
	value string
	label string
	of    func(*DeviceIdentifiers) string
}

func (q deviceQuery) filter() string { return fmt.Sprintf(`%s=="%s"`, q.field, EscapeRSQL(q.value)) }

func (q deviceQuery) desc() string { return fmt.Sprintf("%s %q", q.label, q.value) }

func bySerial(field, v string) deviceQuery {
	return deviceQuery{field, v, "serial number", func(d *DeviceIdentifiers) string { return d.SerialNumber }}
}

func byName(field, v string) deviceQuery {
	return deviceQuery{field, v, "name", func(d *DeviceIdentifiers) string { return d.Name }}
}

// ResolveComputer looks up a computer by serial, name, or ID using the
// v4 computers-inventory API and returns all identifier forms.
// Exactly one of serial, name, or id must be non-empty.
func ResolveComputer(ctx context.Context, client registry.HTTPClient, serial, name, id string) (*DeviceIdentifiers, error) {
	switch {
	case serial != "":
		return resolveComputerByFilter(ctx, client, bySerial("hardware.serialNumber", serial))
	case name != "":
		return resolveComputerByFilter(ctx, client, byName("general.name", name))
	case id != "":
		return resolveComputerByID(ctx, client, id)
	default:
		return nil, fmt.Errorf("one of --serial, --name, or --id is required")
	}
}

// ResolveComputerByManagementID looks up a computer by its MDM management ID (UUID).
func ResolveComputerByManagementID(ctx context.Context, client registry.HTTPClient, managementID string) (*DeviceIdentifiers, error) {
	return resolveComputerByFilter(ctx, client, deviceQuery{
		field: "general.managementId", value: managementID, label: "management ID",
		of: func(d *DeviceIdentifiers) string { return d.ManagementID },
	})
}

// ResolveComputerByUDID looks up a computer by its UDID.
func ResolveComputerByUDID(ctx context.Context, client registry.HTTPClient, udid string) (*DeviceIdentifiers, error) {
	return resolveComputerByFilter(ctx, client, deviceQuery{
		field: "udid", value: udid, label: "UDID",
		of: func(d *DeviceIdentifiers) string { return d.UDID },
	})
}

// ResolveMobileDevice looks up a mobile device by serial, name, or ID using
// the v2 mobile-devices API and returns all identifier forms.
func ResolveMobileDevice(ctx context.Context, client registry.HTTPClient, serial, name, id string) (*DeviceIdentifiers, error) {
	switch {
	case serial != "":
		return resolveMobileByFilter(ctx, client, bySerial("serialNumber", serial))
	case name != "":
		return resolveMobileByFilter(ctx, client, byName("displayName", name))
	case id != "":
		return resolveMobileByID(ctx, client, id)
	default:
		return nil, fmt.Errorf("one of --serial, --name, or --id is required")
	}
}

// ResolveComputerGroup resolves all members of a computer group by name.
// Uses the Classic API to list group members, then batch-resolves each
// via the v3 inventory API to get managementId/UDID.
func ResolveComputerGroup(ctx context.Context, client registry.HTTPClient, groupName string) ([]*DeviceIdentifiers, error) {
	memberIDs, err := ResolveComputerGroupMemberIDs(ctx, client, groupName)
	if err != nil {
		return nil, err
	}
	return batchResolveComputers(ctx, client, memberIDs)
}

// ResolveComputerGroupMemberIDs returns the numeric IDs of a smart or static
// computer group's members, for a caller that resolves them itself.
func ResolveComputerGroupMemberIDs(ctx context.Context, client registry.HTTPClient, groupName string) ([]string, error) {
	// Try smart group first (modern API), fall back to static group.
	memberIDs, err := fetchSmartComputerGroupMemberIDs(ctx, client, groupName)
	if errors.Is(err, errGroupNotFound) {
		// Static groups have no modern membership endpoint, so they are read
		// through the Classic API.
		staticIDs, staticErr := fetchClassicGroupMemberIDs(ctx, client,
			"/JSSResource/computergroups", "computer_groups", "computers", groupName)
		if staticErr != nil {
			return nil, fmt.Errorf("group %q not found as smart group (%v) or static group (%w)", groupName, err, staticErr)
		}
		memberIDs, err = staticIDs, nil
	}
	return memberIDs, err
}

// ResolveMobileDeviceGroup resolves all members of a mobile device group by name.
func ResolveMobileDeviceGroup(ctx context.Context, client registry.HTTPClient, groupName string) ([]*DeviceIdentifiers, error) {
	memberIDs, err := ResolveMobileDeviceGroupMemberIDs(ctx, client, groupName)
	if err != nil {
		return nil, err
	}
	return batchResolveMobileDevices(ctx, client, memberIDs)
}

// ResolveMobileDeviceGroupMemberIDs is ResolveComputerGroupMemberIDs for
// mobile device groups.
func ResolveMobileDeviceGroupMemberIDs(ctx context.Context, client registry.HTTPClient, groupName string) ([]string, error) {
	// Try smart group first (modern API), fall back to Classic for static
	// (no modern static mobile device group API exists yet).
	memberIDs, err := fetchSmartMobileGroupMemberIDs(ctx, client, groupName)
	if errors.Is(err, errGroupNotFound) {
		staticIDs, staticErr := fetchClassicGroupMemberIDs(ctx, client,
			"/JSSResource/mobiledevicegroups", "mobile_device_groups", "mobile_devices", groupName)
		if staticErr != nil {
			return nil, fmt.Errorf("group %q not found as smart group (%v) or static group (%w)", groupName, err, staticErr)
		}
		memberIDs, err = staticIDs, nil
	}
	return memberIDs, err
}

// ResolveComputersFromFile reads computer identifiers from a file (one per
// line) and resolves them to full identifiers. Blank lines and #-comments are
// skipped. An entry may be a numeric ID, a serial number, a UDID, a management
// ID or a name — see resolveEntries for how each is told apart.
//
// Returns the resolved devices and the number of entries that could not be
// resolved. Each unresolvable entry warns to stderr and is skipped — matching
// the per-member soft-fail of the --group path (batchResolveComputers) so one
// bad line doesn't abort the whole batch — and the count is returned so callers
// can fold it into their success/failure tally and exit code. If no entry at
// all resolves, an error is returned rather than an empty list: a wholly stale
// file must not read as a successful no-op batch.
//
// Entries are looked up in chunked RSQL `=in=` queries, so an N-line file costs
// O(N/batchChunkSize) requests rather than one per line.
func ResolveComputersFromFile(ctx context.Context, client registry.HTTPClient, path string) ([]*DeviceIdentifiers, int, error) {
	return resolveEntriesFromFile(ctx, client, path, computerEntrySpec)
}

// ResolveMobileDevicesFromFile reads mobile device identifiers from a file and
// resolves them. Same contract as ResolveComputersFromFile — see there for the
// failure policy.
func ResolveMobileDevicesFromFile(ctx context.Context, client registry.HTTPClient, path string) ([]*DeviceIdentifiers, int, error) {
	return resolveEntriesFromFile(ctx, client, path, mobileEntrySpec)
}

// ResolveComputerEntries resolves identifiers already in hand — repeated flag
// values, say — with the same batching and failure policy as
// ResolveComputersFromFile.
func ResolveComputerEntries(ctx context.Context, client registry.HTTPClient, entries []string) ([]*DeviceIdentifiers, int, error) {
	return resolveEntryList(ctx, client, entries, computerEntrySpec)
}

// ResolveMobileDeviceEntries is ResolveComputerEntries for mobile devices.
func ResolveMobileDeviceEntries(ctx context.Context, client registry.HTTPClient, entries []string) ([]*DeviceIdentifiers, int, error) {
	return resolveEntryList(ctx, client, entries, mobileEntrySpec)
}

// batchChunkSize caps how many identifiers are packed into one RSQL `=in=`
// list. Jamf Pro accepts considerably more (a 200-value list was verified
// against 11.30), but 100 keeps the request URL comfortably short.
const batchChunkSize = 100

// batchFilterMaxBytes caps one chunk's query-escaped filter, since a count cap
// alone does not bound the URL: 100 UUIDs OR'd across two fields escape to
// about 9 KB, past the 8 KB request line common proxies enforce, and a name
// can be any length.
const batchFilterMaxBytes = 4096

// fileEntrySpec describes how to batch-resolve --from-file entries for one
// device type.
type fileEntrySpec struct {
	label    string // "computer" / "mobile device", used in messages
	basePath string // inventory list path, including any section params
	idField  string // RSQL field holding the numeric ID
	// RSQL fields holding the serial number, UDID, management ID and name.
	serialField, udidField, managementIDField, nameField string
	parse                                                func(map[string]any) (*DeviceIdentifiers, error)
	// identifier resolves one entry that cannot be packed into a shared `=in=`
	// list, by every identifier at once.
	identifier identifierSpec
}

var computerEntrySpec = fileEntrySpec{
	label:             "computer",
	basePath:          "/v4/computers-inventory?section=GENERAL&section=HARDWARE",
	idField:           "id",
	serialField:       "hardware.serialNumber",
	udidField:         "udid",
	managementIDField: "general.managementId",
	nameField:         "general.name",
	parse:             parseComputerInventory,
	identifier:        computerIdentifierSpec,
}

var mobileEntrySpec = fileEntrySpec{
	label:             "mobile device",
	basePath:          mobileDetailPath,
	idField:           "mobileDeviceId",
	serialField:       "serialNumber",
	udidField:         "udid",
	managementIDField: "managementId",
	nameField:         "displayName",
	parse:             parseMobileDevice,
	identifier:        mobileIdentifierSpec,
}

func resolveEntriesFromFile(ctx context.Context, client registry.HTTPClient, path string, spec fileEntrySpec) ([]*DeviceIdentifiers, int, error) {
	entries, err := readEntriesFromFile(path)
	if err != nil {
		return nil, 0, err
	}
	devices, skipped, err := resolveEntries(ctx, client, spec, entries)
	if err != nil {
		return nil, 0, err
	}
	if len(devices) == 0 {
		return nil, skipped, fmt.Errorf("none of the %d entries in %s could be resolved", len(entries), path)
	}
	return devices, skipped, nil
}

func resolveEntryList(ctx context.Context, client registry.HTTPClient, entries []string, spec fileEntrySpec) ([]*DeviceIdentifiers, int, error) {
	cleaned := make([]string, 0, len(entries))
	for _, e := range entries {
		if e = strings.TrimSpace(e); e != "" {
			cleaned = append(cleaned, e)
		}
	}
	if len(cleaned) == 0 {
		return nil, 0, fmt.Errorf("no %s identifiers given", spec.label)
	}
	devices, skipped, err := resolveEntries(ctx, client, spec, cleaned)
	if err != nil {
		return nil, 0, err
	}
	if len(devices) == 0 {
		return nil, skipped, fmt.Errorf("none of the %d %s identifiers could be resolved", len(cleaned), spec.label)
	}
	return devices, skipped, nil
}

// entryKind is how resolveEntries reads one entry, decided by its shape alone.
type entryKind int

const (
	entryID        entryKind = iota // all digits: a Jamf Pro ID, never a name
	entryUUID                       // 8-4-4-4-12 hex: a computer UDID or either family's management ID
	entryUDID                       // 40 hex, or 8-16 hex: a mobile device UDID
	entrySerial                     // list-safe: a serial number, or failing that a name
	entryName                       // quotable but not list-safe (a space, say): only ever a name
	entryUnbatched                  // carries a `*`: one identifier lookup of its own
)

var (
	uuidShape = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)
	udidShape = regexp.MustCompile(`^(?:[0-9A-Fa-f]{40}|[0-9A-Fa-f]{8}-[0-9A-Fa-f]{16})$`)
)

// classifyEntry reads an entry's kind from its shape. A computer's UDID and
// its management ID are both UUIDs, so a UUID is looked up as either; nothing
// else about either shape can be a serial number, which Apple caps at 12
// characters.
func classifyEntry(entry string) entryKind {
	switch {
	case isNumericID(entry):
		return entryID
	case uuidShape.MatchString(entry):
		return entryUUID
	case udidShape.MatchString(entry):
		return entryUDID
	case isRSQLListSafe(entry):
		return entrySerial
	case !strings.Contains(entry, "*"):
		return entryName
	default:
		return entryUnbatched
	}
}

// entryResult is one entry's outcome: a device, or why there is none.
type entryResult struct {
	device *DeviceIdentifiers
	err    error
}

// resolveEntries resolves entries to devices, preserving input order and
// duplicates (one target per entry, as before). The returned error covers only
// transport/HTTP failures of the batch lookups — an entry the server simply
// doesn't know warns to stderr and counts towards the skipped total.
//
// Each kind batches into `=in=` queries of its own: IDs by ID; UUIDs by UDID
// or management ID in one OR'd filter; UDIDs by UDID; list-safe entries by
// serial number and then, for the ones no serial matched, by name; any other
// entry by name alone, since a space, quote, comma or paren cannot be part of
// a serial number or UDID. An entry matching more than one device is refused
// rather than resolved to the first, since Jamf Pro names are not unique. An
// entry carrying a `*` is resolved on its own: inside a shared list the
// wildcard would page in every device it matches.
func resolveEntries(ctx context.Context, client registry.HTTPClient, spec fileEntrySpec, entries []string) ([]*DeviceIdentifiers, int, error) {
	byKind := map[entryKind][]string{}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if seen[entry] {
			continue
		}
		seen[entry] = true
		k := classifyEntry(entry)
		byKind[k] = append(byKind[k], entry)
	}

	results := make(map[string]entryResult, len(seen))

	byID := map[string][]*DeviceIdentifiers{}
	if err := spec.batch(ctx, client, byKind[entryID], "ID", false,
		func(chunk []string) string { return fmt.Sprintf("%s=in=(%s)", spec.idField, strings.Join(chunk, ",")) },
		func(d *DeviceIdentifiers) []string { return []string{d.ID} }, byID); err != nil {
		return nil, 0, err
	}
	for _, e := range byKind[entryID] {
		results[e] = pickEntry(spec, e, "ID", byID[e])
	}

	byUUID := map[string][]*DeviceIdentifiers{}
	if err := spec.batch(ctx, client, byKind[entryUUID], "UDID or management ID", true,
		func(chunk []string) string {
			list := quotedList(chunk)
			return fmt.Sprintf("%s=in=(%s),%s=in=(%s)", spec.udidField, list, spec.managementIDField, list)
		},
		func(d *DeviceIdentifiers) []string { return []string{d.UDID, d.ManagementID} }, byUUID); err != nil {
		return nil, 0, err
	}
	for _, e := range byKind[entryUUID] {
		results[e] = pickEntry(spec, e, "UDID or management ID", byUUID[strings.ToLower(e)])
	}

	byUDID := map[string][]*DeviceIdentifiers{}
	if err := spec.batch(ctx, client, byKind[entryUDID], "UDID", true,
		func(chunk []string) string { return fmt.Sprintf("%s=in=(%s)", spec.udidField, quotedList(chunk)) },
		func(d *DeviceIdentifiers) []string { return []string{d.UDID} }, byUDID); err != nil {
		return nil, 0, err
	}
	for _, e := range byKind[entryUDID] {
		results[e] = pickEntry(spec, e, "UDID", byUDID[strings.ToLower(e)])
	}

	// Jamf matches serials and names case-insensitively, so both are keyed
	// case-folded to keep an entry that differs only in case matchable.
	bySerial := map[string][]*DeviceIdentifiers{}
	if err := spec.batch(ctx, client, byKind[entrySerial], "serial number", true,
		func(chunk []string) string { return fmt.Sprintf("%s=in=(%s)", spec.serialField, quotedList(chunk)) },
		func(d *DeviceIdentifiers) []string { return []string{d.SerialNumber} }, bySerial); err != nil {
		return nil, 0, err
	}
	var unmatchedSerials []string
	for _, e := range byKind[entrySerial] {
		if len(bySerial[strings.ToLower(e)]) == 0 {
			unmatchedSerials = append(unmatchedSerials, e)
			continue
		}
		results[e] = pickEntry(spec, e, "serial number", bySerial[strings.ToLower(e)])
	}
	byName := map[string][]*DeviceIdentifiers{}
	if err := spec.batch(ctx, client, slices.Concat(unmatchedSerials, byKind[entryName]), "name", true,
		func(chunk []string) string { return fmt.Sprintf("%s=in=(%s)", spec.nameField, quotedList(chunk)) },
		func(d *DeviceIdentifiers) []string { return []string{d.Name} }, byName); err != nil {
		return nil, 0, err
	}
	for _, e := range unmatchedSerials {
		r := pickEntry(spec, e, "serial number or name", byName[strings.ToLower(e)])
		if r.device != nil {
			// A mistyped serial that happens to be another device's name
			// would otherwise target that device without a word.
			_, _ = fmt.Fprintf(os.Stderr, "  note: %q matched no serial number; resolved by name to %s %s\n", e, spec.label, r.device.ID)
		}
		results[e] = r
	}
	for _, e := range byKind[entryName] {
		results[e] = pickEntry(spec, e, "name", byName[strings.ToLower(e)])
	}

	for _, e := range byKind[entryUnbatched] {
		d, err := resolveIdentifier(ctx, client, spec.identifier, e)
		if err != nil && !errors.Is(err, ErrNoDeviceMatch) && !errors.Is(err, ErrAmbiguousDevice) {
			return nil, 0, err
		}
		results[e] = entryResult{device: d, err: err}
	}

	devices := make([]*DeviceIdentifiers, 0, len(entries))
	skipped := 0
	for _, entry := range entries {
		r := results[entry]
		if r.err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "  warning: could not resolve %s %q: %v\n", spec.label, entry, r.err)
			skipped++
			continue
		}
		devices = append(devices, r.device)
	}
	return devices, skipped, nil
}

// batch runs one identifier kind's chunked `=in=` lookups and indexes every
// returned device under each key keysOf yields, case-folded when fold is set.
// A device is indexed once per key however many queries return it. A returned
// record that cannot be parsed is counted on stderr, so a device the server
// did return is not reported only as "no device found".
func (s fileEntrySpec) batch(ctx context.Context, client registry.HTTPClient, entries []string, label string, fold bool,
	filter func(chunk []string) string, keysOf func(*DeviceIdentifiers) []string, into map[string][]*DeviceIdentifiers,
) error {
	unreadable := 0
	for _, chunk := range filterChunks(entries, filter) {
		records, err := s.fetchFiltered(ctx, client, filter(chunk))
		if err != nil {
			return fmt.Errorf("looking up %ss by %s: %w", s.label, label, err)
		}
		for _, record := range records {
			d, err := s.parse(record)
			if err != nil {
				unreadable++
				continue
			}
			for _, k := range keysOf(d) {
				if k == "" {
					continue
				}
				if fold {
					k = strings.ToLower(k)
				}
				if !slices.ContainsFunc(into[k], func(o *DeviceIdentifiers) bool { return o.ID == d.ID }) {
					into[k] = append(into[k], d)
				}
			}
		}
	}
	if unreadable > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "  warning: %d %s record(s) returned by the %s lookup could not be read and were ignored\n", unreadable, s.label, label)
	}
	return nil
}

// filterChunks splits entries into the chunks batch queries: at most
// batchChunkSize each, and closed early once the query-escaped filter would
// pass batchFilterMaxBytes. An entry too long to share a chunk gets one alone.
func filterChunks(entries []string, filter func(chunk []string) string) [][]string {
	var chunks [][]string
	var cur []string
	for _, e := range entries {
		next := append(slices.Clip(cur), e)
		if len(cur) > 0 && (len(next) > batchChunkSize || len(url.QueryEscape(filter(next))) > batchFilterMaxBytes) {
			chunks = append(chunks, cur)
			next = []string{e}
		}
		cur = next
	}
	if len(cur) > 0 {
		chunks = append(chunks, cur)
	}
	return chunks
}

// pickEntry turns an entry's matches into its result: one device, or an error
// that says there were none or names every device it matched.
func pickEntry(spec fileEntrySpec, entry, label string, matches []*DeviceIdentifiers) entryResult {
	switch len(matches) {
	case 0:
		return entryResult{err: fmt.Errorf("no %s found with %s %q", spec.label, label, entry)}
	case 1:
		return entryResult{device: matches[0]}
	}
	ids := make([]string, len(matches))
	for i, d := range matches {
		ids[i] = d.ID
	}
	return entryResult{err: &AmbiguousEntryError{Label: spec.label, Value: entry, IDs: ids}}
}

// AmbiguousEntryError reports an identifier that matched more than one device.
type AmbiguousEntryError struct {
	Label string
	Value string
	IDs   []string
}

func (e *AmbiguousEntryError) Error() string {
	return fmt.Sprintf("%q matches %d %ss (ids %s); use the numeric ID of the one you mean",
		e.Value, len(e.IDs), e.Label, strings.Join(e.IDs, ", "))
}

// Is makes errors.Is(err, ErrAmbiguousDevice) hold.
func (e *AmbiguousEntryError) Is(target error) bool { return target == ErrAmbiguousDevice }

// quotedList renders entries as the quoted members of an RSQL `=in=` list.
func quotedList(entries []string) string {
	quoted := make([]string, len(entries))
	for i, s := range entries {
		quoted[i] = `"` + EscapeRSQL(s) + `"`
	}
	return strings.Join(quoted, ",")
}

// fetchFiltered runs an RSQL filter against the spec's inventory endpoint,
// paging through all matches.
func (s fileEntrySpec) fetchFiltered(ctx context.Context, client registry.HTTPClient, filter string) ([]map[string]any, error) {
	sep := "?"
	if strings.Contains(s.basePath, "?") {
		sep = "&"
	}
	return fetchAllPages(ctx, client, fmt.Sprintf("%s%sfilter=%s", s.basePath, sep, url.QueryEscape(filter)))
}

// --- Computer resolution helpers ---

func resolveComputerByFilter(ctx context.Context, client registry.HTTPClient, q deviceQuery) (*DeviceIdentifiers, error) {
	// Use page-size=2 to detect ambiguity (multiple matches).
	path := fmt.Sprintf("/v4/computers-inventory?section=GENERAL&section=HARDWARE&page-size=2&filter=%s",
		url.QueryEscape(q.filter()))

	results, total, err := fetchInventoryPage(ctx, client, path)
	if err != nil {
		return nil, fmt.Errorf("looking up computer by %s: %w", q.desc(), err)
	}
	if total == 0 || len(results) == 0 {
		return nil, fmt.Errorf("no computer found with %s", q.desc())
	}
	if total > 1 {
		return nil, fmt.Errorf("multiple computers found with %s (%d matches); use --serial or --id to disambiguate", q.desc(), total)
	}
	d, err := parseComputerInventory(results[0])
	if err != nil {
		return nil, err
	}
	return q.confirm(d, "computer")
}

// confirm accepts a returned record only when it carries the requested value,
// since an unescaped `*` in the value makes the server's == a wildcard match.
func (q deviceQuery) confirm(d *DeviceIdentifiers, noun string) (*DeviceIdentifiers, error) {
	if !strings.EqualFold(q.of(d), q.value) {
		return nil, fmt.Errorf("no %s found with %s (the server returned %s, whose %s is %q)", noun, q.desc(), d.ID, q.label, q.of(d))
	}
	return d, nil
}

func resolveComputerByID(ctx context.Context, client registry.HTTPClient, id string) (*DeviceIdentifiers, error) {
	path := fmt.Sprintf("/v4/computers-inventory/%s?section=GENERAL&section=HARDWARE", url.PathEscape(id))
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("looking up computer ID %s: %w", id, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no computer found with ID %s", id)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("looking up computer ID %s: HTTP %d", id, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, fmt.Errorf("parsing computer response: %w", err)
	}
	return parseComputerInventory(obj)
}

func parseComputerInventory(obj map[string]any) (*DeviceIdentifiers, error) {
	d := &DeviceIdentifiers{}
	d.ID = jsonString(obj, "id")
	d.UDID = jsonString(obj, "udid")

	if general, ok := obj["general"].(map[string]any); ok {
		d.Name = jsonString(general, "name")
		d.ManagementID = jsonString(general, "managementId")
		if rm, ok := general["remoteManagement"].(map[string]any); ok {
			d.Managed = boolField(rm, "managed")
		}
	}
	if hardware, ok := obj["hardware"].(map[string]any); ok {
		d.SerialNumber = jsonString(hardware, "serialNumber")
	}

	if d.ID == "" {
		return nil, fmt.Errorf("computer response missing id field")
	}
	return d, nil
}

// --- Mobile device resolution helpers ---

// mobileDetailPath is the one mobile list path that honors RSQL filters
// (/v2/mobile-devices ignores them). It answers "hardware": null, and so no
// serial number, unless HARDWARE is requested.
const mobileDetailPath = "/v2/mobile-devices/detail?section=GENERAL&section=HARDWARE"

func resolveMobileByFilter(ctx context.Context, client registry.HTTPClient, q deviceQuery) (*DeviceIdentifiers, error) {
	path := fmt.Sprintf("%s&page-size=2&filter=%s", mobileDetailPath, url.QueryEscape(q.filter()))

	results, total, err := fetchInventoryPage(ctx, client, path)
	if err != nil {
		return nil, fmt.Errorf("looking up mobile device by %s: %w", q.desc(), err)
	}
	if total == 0 || len(results) == 0 {
		return nil, fmt.Errorf("no mobile device found with %s", q.desc())
	}
	if total > 1 {
		return nil, fmt.Errorf("multiple mobile devices found with %s (%d matches); use --serial or --id to disambiguate", q.desc(), total)
	}
	d, err := parseMobileDevice(results[0])
	if err != nil {
		return nil, err
	}
	return q.confirm(d, "mobile device")
}

func resolveMobileByID(ctx context.Context, client registry.HTTPClient, id string) (*DeviceIdentifiers, error) {
	path := fmt.Sprintf("/v2/mobile-devices/%s", url.PathEscape(id))
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("looking up mobile device ID %s: %w", id, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no mobile device found with ID %s", id)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("looking up mobile device ID %s: HTTP %d", id, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, fmt.Errorf("parsing mobile device response: %w", err)
	}
	return parseMobileDevice(obj)
}

func parseMobileDevice(obj map[string]any) (*DeviceIdentifiers, error) {
	d := &DeviceIdentifiers{
		ID:           jsonString(obj, "id"),
		ManagementID: jsonString(obj, "managementId"),
		UDID:         jsonString(obj, "udid"),
		Name:         jsonString(obj, "name"),
		SerialNumber: jsonString(obj, "serialNumber"),
		Managed:      boolField(obj, "managed"),
	}
	// /v2/mobile-devices/detail returns "mobileDeviceId" instead of "id",
	// nests managementId/udid/displayName inside "general", and the serial
	// number inside "hardware".
	if d.ID == "" {
		d.ID = jsonString(obj, "mobileDeviceId")
	}
	if general, ok := obj["general"].(map[string]any); ok {
		if d.Managed == nil {
			d.Managed = boolField(general, "managed")
		}
		if d.ManagementID == "" {
			d.ManagementID = jsonString(general, "managementId")
		}
		if d.UDID == "" {
			d.UDID = jsonString(general, "udid")
		}
		if d.Name == "" {
			d.Name = jsonString(general, "displayName")
		}
	}
	// /detail nests the serial under "hardware", populated only with
	// section=HARDWARE.
	if hardware, ok := obj["hardware"].(map[string]any); ok && d.SerialNumber == "" {
		d.SerialNumber = jsonString(hardware, "serialNumber")
	}
	if d.Name == "" {
		d.Name = jsonString(obj, "displayName")
	}
	if d.ID == "" {
		return nil, fmt.Errorf("mobile device response missing id field")
	}
	return d, nil
}

// --- Smart group resolution (modern API) ---

// fetchSmartComputerGroupMemberIDs finds a smart computer group by name via
// the v3 API and returns its member IDs. v3 rather than v2 because the gateway
// publishes only v3; the search-result and membership schemas are the same on
// both, so this is a version bump and not a shape change.
func fetchSmartComputerGroupMemberIDs(ctx context.Context, client registry.HTTPClient, groupName string) ([]string, error) {
	// Look up group by name with RSQL filter.
	groupID, err := resolveGroupIDByName(ctx, client,
		"/v3/computer-groups/smart-groups", "name", groupName)
	if err != nil {
		return nil, err
	}

	// Fetch membership: returns {"members": [1, 2, 3]}.
	path := fmt.Sprintf("/v3/computer-groups/smart-group-membership/%s", url.PathEscape(groupID))
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching smart group membership: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching smart group membership: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, err
	}

	var membership struct {
		Members []int `json:"members"`
	}
	if err := json.Unmarshal(body, &membership); err != nil {
		return nil, fmt.Errorf("parsing smart group membership: %w", err)
	}

	ids := make([]string, len(membership.Members))
	for i, m := range membership.Members {
		ids[i] = fmt.Sprintf("%d", m)
	}
	return ids, nil
}

// fetchSmartMobileGroupMemberIDs finds a smart mobile device group by name via
// the v2 API and returns its member IDs. v2 rather than v1 for the same reason
// as the computer groups above: v1 is withdrawn from the gateway's published
// surface and the GET response schemas are identical.
func fetchSmartMobileGroupMemberIDs(ctx context.Context, client registry.HTTPClient, groupName string) ([]string, error) {
	groupID, err := resolveGroupIDByName(ctx, client,
		"/v2/mobile-device-groups/smart-groups", "groupName", groupName)
	if err != nil {
		return nil, err
	}

	// Fetch membership: paginated response with device details.
	path := fmt.Sprintf("/v2/mobile-device-groups/smart-group-membership/%s", url.PathEscape(groupID))
	results, err := fetchAllPages(ctx, client, path)
	if err != nil {
		return nil, fmt.Errorf("fetching smart mobile group membership: %w", err)
	}

	ids := make([]string, 0, len(results))
	for _, r := range results {
		id := jsonString(r, "id")
		if id == "" {
			id = jsonString(r, "mobileDeviceId")
		}
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// errGroupNotFound reports that a group lookup completed and found no group
// of that name. Only this error lets a smart-group miss fall back to the
// static-group lookup; an ambiguity or a failed request is returned as is.
var errGroupNotFound = errors.New("group not found")

// resolveGroupIDByName uses RSQL filtering on a group list endpoint to find
// a group by name and return its ID. A 404 from the search endpoint means it
// does not exist on this server, which is read as not found.
func resolveGroupIDByName(ctx context.Context, client registry.HTTPClient, listPath, nameField, groupName string) (string, error) {
	filter := fmt.Sprintf(`%s=="%s"`, nameField, EscapeRSQL(groupName))
	path := fmt.Sprintf("%s?page-size=2&filter=%s", listPath, url.QueryEscape(filter))

	results, total, err := fetchInventoryPage(registry.WithAllowedStatuses(ctx, http.StatusNotFound), client, path)
	var status *httpStatusError
	if errors.As(err, &status) && status.code == http.StatusNotFound {
		return "", fmt.Errorf("group %q: %w (search answered HTTP 404)", groupName, errGroupNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("searching for group %q: %w", groupName, err)
	}
	if total == 0 || len(results) == 0 {
		return "", fmt.Errorf("group %q: %w", groupName, errGroupNotFound)
	}
	if total > 1 {
		ids := make([]string, len(results))
		for i, r := range results {
			ids[i] = groupResultID(r)
		}
		return "", fmt.Errorf("multiple groups found with name %q (%d matches, ids %s); rename one so the name is unique", groupName, total, strings.Join(ids, ", "))
	}
	if got := jsonString(results[0], nameField); !strings.EqualFold(got, groupName) {
		return "", fmt.Errorf("group %q: %w (the search returned group %s, named %q)", groupName, errGroupNotFound, groupResultID(results[0]), got)
	}
	id := groupResultID(results[0])
	if id == "" {
		return "", fmt.Errorf("group %q found but missing id field", groupName)
	}
	return id, nil
}

func groupResultID(r map[string]any) string {
	if id := jsonString(r, "id"); id != "" {
		return id
	}
	return jsonString(r, "groupId")
}

// --- Classic API group fallback (static groups) ---

func fetchClassicGroupMemberIDs(ctx context.Context, client registry.HTTPClient, listPath, listKey, membersKey, groupName string) ([]string, error) {
	groupID, err := pickClassicGroupID(ctx, client, listPath, listKey, "static group", groupName)
	if err != nil {
		return nil, err
	}

	// Fetch group detail to get member IDs.
	detailPath := fmt.Sprintf("%s/id/%s", listPath, url.PathEscape(groupID))
	resp2, err := client.Do(ctx, "GET", detailPath, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching group %q: %w", groupName, err)
	}
	defer func() { _ = resp2.Body.Close() }()

	body2, err := io.ReadAll(io.LimitReader(resp2.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	detail, err := unmarshalClassic(body2)
	if err != nil {
		return nil, fmt.Errorf("parsing group detail: %w", err)
	}

	// Unwrap Classic API detail envelope (e.g., {"computer_group": {...}}).
	for _, v := range detail {
		if inner, ok := v.(map[string]any); ok {
			detail = inner
			break
		}
	}

	members, _ := detail[membersKey].([]any)
	var ids []string
	for _, m := range members {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		id := jsonString(mm, "id")
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// pickClassicGroupID lists a Classic group collection and resolves groupName
// to exactly one id: a unique exact match wins, then a unique case-insensitive
// one, and a name more than one group shares is refused with every id.
func pickClassicGroupID(ctx context.Context, client registry.HTTPClient, listPath, listKey, label, groupName string) (string, error) {
	g, err := pickClassicGroup(ctx, client, listPath, listKey, label, groupName)
	return g.ID, err
}

func pickClassicGroup(ctx context.Context, client registry.HTTPClient, listPath, listKey, label, groupName string) (ClassicGroup, error) {
	resp, err := client.Do(ctx, "GET", listPath, nil)
	if err != nil {
		return ClassicGroup{}, fmt.Errorf("listing %ss: %w", label, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return ClassicGroup{}, fmt.Errorf("%s %q not found (listing %ss answered HTTP 404)", label, groupName, label)
	}
	if resp.StatusCode != http.StatusOK {
		return ClassicGroup{}, fmt.Errorf("listing %ss: HTTP %d", label, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return ClassicGroup{}, err
	}
	groups, err := parseClassicGroupList(body, listKey)
	if err != nil {
		return ClassicGroup{}, fmt.Errorf("parsing %s list: %w", label, err)
	}
	group, candidates, err := pickone.One(groups, groupName,
		pickone.Exact(func(g classicGroupRef) string { return g.Name }),
		pickone.Fold(func(g classicGroupRef) string { return g.Name }))
	switch {
	case errors.Is(err, pickone.ErrNone):
		return ClassicGroup{}, fmt.Errorf("%s %q not found", label, groupName)
	case errors.Is(err, pickone.ErrAmbiguous):
		ids := make([]string, len(candidates))
		for i, c := range candidates {
			ids[i] = fmt.Sprintf("%q (id %s)", c.Name, c.ID)
		}
		return ClassicGroup{}, fmt.Errorf("group %q matches %d %ss: %s; rename one or pass the exact name", groupName, len(candidates), label, strings.Join(ids, ", "))
	}
	if group.ID == "" {
		return ClassicGroup{}, fmt.Errorf("%s %q found but missing id field", label, groupName)
	}
	return ClassicGroup(group), nil
}

// ClassicGroup is a resolved Classic group: its id and its name as stored.
type ClassicGroup struct {
	ID   string
	Name string
}

// classicGroupRef is one entry of a Classic group listing, its name kept as
// the literal text the server sent.
type classicGroupRef struct {
	ID   string `xml:"id"`
	Name string `xml:"name"`
}

// parseClassicGroupList reads a Classic group listing without coercing names.
// xmlconv turns numeric-looking text into numbers, so a group named "14.2"
// would otherwise compare equal to a request for "14".
func parseClassicGroupList(body []byte, listKey string) ([]classicGroupRef, error) {
	if xmlconv.IsXML(body) {
		var list struct {
			Groups []classicGroupRef `xml:",any"`
		}
		if err := xml.Unmarshal(body, &list); err != nil {
			return nil, err
		}
		return list.Groups, nil
	}
	var listData map[string]any
	if err := json.Unmarshal(body, &listData); err != nil {
		return nil, err
	}
	raw, _ := listData[listKey].([]any)
	groups := make([]classicGroupRef, 0, len(raw))
	for _, g := range raw {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		name, _ := gm["name"].(string)
		groups = append(groups, classicGroupRef{ID: jsonString(gm, "id"), Name: name})
	}
	return groups, nil
}

func batchResolveComputers(ctx context.Context, client registry.HTTPClient, ids []string) ([]*DeviceIdentifiers, error) {
	results := make([]*DeviceIdentifiers, 0, len(ids))
	for _, id := range ids {
		d, err := resolveComputerByID(ctx, client, id)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "  warning: could not resolve computer ID %s: %v\n", id, err)
			continue
		}
		results = append(results, d)
	}
	return results, nil
}

func batchResolveMobileDevices(ctx context.Context, client registry.HTTPClient, ids []string) ([]*DeviceIdentifiers, error) {
	results := make([]*DeviceIdentifiers, 0, len(ids))
	for _, id := range ids {
		d, err := resolveMobileByID(ctx, client, id)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "  warning: could not resolve mobile device ID %s: %v\n", id, err)
			continue
		}
		results = append(results, d)
	}
	return results, nil
}

// --- Shared helpers ---

// unmarshalClassic parses a Classic API response that may be XML or JSON.
func unmarshalClassic(data []byte) (map[string]any, error) {
	if xmlconv.IsXML(data) {
		return xmlconv.ToMap(data)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// fetchAllPages fetches all pages from a paginated endpoint that returns
// {"totalCount": N, "results": [...]}. The basePath should include any
// filters but NOT page/page-size params (they are appended automatically).
func fetchAllPages(ctx context.Context, client registry.HTTPClient, basePath string) ([]map[string]any, error) {
	const pageSize = 200
	var allResults []map[string]any

	sep := "?"
	if strings.Contains(basePath, "?") {
		sep = "&"
	}

	for page := 0; ; page++ {
		pagePath := fmt.Sprintf("%s%spage=%d&page-size=%d", basePath, sep, page, pageSize)
		results, total, err := fetchInventoryPage(ctx, client, pagePath)
		if err != nil {
			return nil, err
		}
		allResults = append(allResults, results...)
		if len(allResults) >= total || len(results) == 0 {
			break
		}
	}
	return allResults, nil
}

// fetchInventoryPage makes a GET request and parses a paginated response
// with {"totalCount": N, "results": [...]}.
func fetchInventoryPage(ctx context.Context, client registry.HTTPClient, path string) ([]map[string]any, int, error) {
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, &httpStatusError{code: resp.StatusCode}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, 0, err
	}

	var page struct {
		TotalCount int              `json:"totalCount"`
		Results    []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, 0, fmt.Errorf("parsing paginated response: %w", err)
	}
	return page.Results, page.TotalCount, nil
}

type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string { return fmt.Sprintf("HTTP %d", e.code) }

// jsonString extracts a string field from a map, handling both string and
// numeric ID values (the Jamf API sometimes returns IDs as numbers).
func jsonString(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return fmt.Sprintf("%d", int(val))
	default:
		return fmt.Sprintf("%v", val)
	}
}

// boolField reads a boolean field, or nil when it is absent or not a boolean.
func boolField(m map[string]any, key string) *bool {
	if b, ok := m[key].(bool); ok {
		return &b
	}
	return nil
}

var rsqlEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// EscapeRSQL escapes a value for use inside one double-quoted RSQL literal,
// so the value cannot close the quote. A `*` stays a wildcard, so a caller
// acting on the result compares the returned record's value itself.
func EscapeRSQL(s string) string {
	return rsqlEscaper.Replace(s)
}

func isNumericID(s string) bool { return IsNumericID(s) }

// IsNumericID reports whether s contains only digits, so it can be a Jamf Pro
// ID rather than a serial number or a name.
func IsNumericID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// isRSQLListSafe reports whether s can be a serial number: alphanumeric plus
// `-`, `_` and `.`. An entry carrying anything else (a quote, comma, paren, or
// whitespace) can only be a name, and is looked up by name alone.
func isRSQLListSafe(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

// readEntriesFromFile reads lines from a file, skipping blanks and #-comments.
func readEntriesFromFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening file %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var entries []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		entries = append(entries, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading file %s: %w", path, err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("file %s contains no entries", path)
	}
	return entries, nil
}

// ResolveClassicComputerGroup resolves a computer group name to exactly one
// Classic group. Works for both smart and static computer groups.
func ResolveClassicComputerGroup(ctx context.Context, client registry.HTTPClient, groupName string) (ClassicGroup, error) {
	return pickClassicGroup(ctx, client, "/JSSResource/computergroups", "computer_groups", "computer group", groupName)
}

// ResolveClassicMobileGroup resolves a mobile device group name to exactly one
// Classic group. Works for both smart and static mobile device groups.
func ResolveClassicMobileGroup(ctx context.Context, client registry.HTTPClient, groupName string) (ClassicGroup, error) {
	return pickClassicGroup(ctx, client, "/JSSResource/mobiledevicegroups", "mobile_device_groups", "mobile device group", groupName)
}

// ResolveClassicComputerGroupID resolves a computer group name to its Classic API
// numeric ID. Works for both smart and static computer groups.
func ResolveClassicComputerGroupID(ctx context.Context, client registry.HTTPClient, groupName string) (string, error) {
	return pickClassicGroupID(ctx, client, "/JSSResource/computergroups", "computer_groups", "computer group", groupName)
}

// ResolveClassicMobileGroupID resolves a mobile device group name to its Classic API
// numeric ID. Works for both smart and static mobile device groups.
func ResolveClassicMobileGroupID(ctx context.Context, client registry.HTTPClient, groupName string) (string, error) {
	return pickClassicGroupID(ctx, client, "/JSSResource/mobiledevicegroups", "mobile_device_groups", "mobile device group", groupName)
}

// FormatDeviceDesc returns a human-readable device description for confirmation messages.
// Example: "Neil's MacBook" (serial: C02X1234, id: 42)
func FormatDeviceDesc(d *DeviceIdentifiers) string {
	parts := []string{}
	if d.SerialNumber != "" {
		parts = append(parts, "serial: "+d.SerialNumber)
	}
	parts = append(parts, "id: "+d.ID)

	name := d.Name
	if name == "" {
		name = d.ID
	}
	return fmt.Sprintf("%q (%s)", name, strings.Join(parts, ", "))
}
