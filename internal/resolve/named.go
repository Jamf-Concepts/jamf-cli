// Copyright 2026, Jamf Software LLC

package resolve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/listfile"
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// Target is one --list-file entry resolved to the record it names.
type Target struct {
	// ID is the record's identifier, ready for the request path.
	ID string
	// Label names the record in a preview or result line, already safe for a
	// terminal, and never quotes a hidden entry's value.
	Label string
	// Entry is the line the target came from; its zero value for a device,
	// whose label comes from the server's record instead.
	Entry listfile.Entry
}

// Hide returns err, or for a target from a hidden entry an error that keeps
// only err's exit code and hint. See listfile.Entry.Hide.
func (t Target) Hide(err error) error { return t.Entry.Hide(err) }

// NamedRecord is one record a name lookup returned.
type NamedRecord struct {
	ID, Name string
}

// NameLookup returns the records whose name may be one of names. It may
// return more — a collection that ignores the filter returns every record —
// because ResolveNamedEntries matches each name itself.
type NameLookup func(ctx context.Context, names []string) ([]NamedRecord, error)

// NamedEntrySpec describes a resource whose --list-file entries are IDs or
// names.
type NamedEntrySpec struct {
	// Label is the singular noun used in messages, e.g. "building".
	Label string
	// Lookup finds records by name. Nil means the resource takes IDs only,
	// and a line that is not one is reported and skipped.
	Lookup NameLookup
	// FoldCase matches a name case-insensitively and narrows to the one
	// exact-case match when there is exactly one, which is the Classic API's
	// own rule (classicNarrowToExactName). Off, a name must match exactly,
	// which is the Pro API's (filterResultsByName).
	FoldCase bool
	// IsID reports whether an entry is an ID rather than a name. Nil means
	// IsNumericID, so an all-digit line is always an ID and never a name.
	IsID func(string) bool
}

// ResolveNamedFile reads path (or stdin, for "-") and resolves its entries
// with ResolveNamedEntries.
func ResolveNamedFile(ctx context.Context, spec NamedEntrySpec, path string) ([]Target, int, error) {
	entries, err := listfile.Read(path)
	if err != nil {
		return nil, 0, fmt.Errorf("reading --%s: %w", listfile.FlagName, err)
	}
	if len(entries) == 0 {
		return nil, 0, fmt.Errorf("%s contains no entries", listfile.SourceName(path))
	}
	targets, skipped, err := ResolveNamedEntries(ctx, spec, entries)
	if errors.Is(err, errNothingResolved) {
		// The CLI's own words, naming no entry: safe in an MCP child as is.
		return nil, skipped, fmt.Errorf("none of the %d entries in %s could be resolved", len(entries), listfile.SourceName(path))
	}
	if err != nil {
		// A failed lookup's text can carry the names in its request path.
		if registry.InMCPChild() {
			return nil, 0, listfile.Opaque(fmt.Sprintf("looking up the %ss in %s", spec.Label, listfile.SourceName(path)), err)
		}
		return nil, 0, err
	}
	return targets, skipped, nil
}

// errNothingResolved is ResolveNamedEntries' answer to a list in which no
// entry resolved.
var errNothingResolved = errors.New("no entry could be resolved")

// ResolveNamedEntries resolves entries to records: an ID passes through, and
// every name is looked up with one call to spec.Lookup, which batches as its
// API allows, rather than one request per line.
//
// The policy is the device resolver's (resolveEntries): an entry that matches
// no record, or more than one, warns to stderr and is skipped, and the number
// skipped is returned so the caller can fold it into its tally and exit code.
// Only a failed lookup is an error, and so is a list in which nothing
// resolved, so a wholly stale file never reads as a successful no-op. ID 0 is
// skipped too: Jamf Pro uses it as a sentinel. Targets keep input order, and
// an entry that resolves to a record already targeted is dropped.
func ResolveNamedEntries(ctx context.Context, spec NamedEntrySpec, entries []listfile.Entry) ([]Target, int, error) {
	isID := spec.IsID
	if isID == nil {
		isID = IsNumericID
	}

	var names []string
	seenName := map[string]bool{}
	for _, e := range entries {
		if !isID(e.Value) && !seenName[e.Value] {
			seenName[e.Value] = true
			names = append(names, e.Value)
		}
	}

	byName := map[string][]NamedRecord{}
	if len(names) > 0 && spec.Lookup != nil {
		records, err := spec.Lookup(ctx, names)
		if err != nil {
			return nil, 0, fmt.Errorf("looking up %ss by name: %w", spec.Label, err)
		}
		for _, r := range records {
			if r.ID == "" {
				continue
			}
			k := r.Name
			if spec.FoldCase {
				k = strings.ToLower(k)
			}
			byName[k] = append(byName[k], r)
		}
	}

	targets := make([]Target, 0, len(entries))
	seenID := map[string]bool{}
	skipped := 0
	for _, e := range entries {
		id, err := spec.pick(e, isID, byName)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "  warning: could not resolve %s %s: %s\n", spec.Label, e.Label(), spec.failure(e, err))
			skipped++
			continue
		}
		if seenID[id] {
			continue
		}
		seenID[id] = true
		targets = append(targets, Target{ID: id, Label: e.Target(output.SanitizeTerminal(id)), Entry: e})
	}
	if len(targets) == 0 {
		return nil, skipped, fmt.Errorf("none of the %d %s entries could be resolved: %w", len(entries), spec.Label, errNothingResolved)
	}
	return targets, skipped, nil
}

// errNoNameLookup is the refusal for a name given to a resource that is
// addressed by ID alone.
var errNoNameLookup = errors.New("not an ID, and this resource cannot be looked up by name")

func (s NamedEntrySpec) pick(e listfile.Entry, isID func(string) bool, byName map[string][]NamedRecord) (string, error) {
	if isID(e.Value) {
		if e.Value == "0" {
			return "", errors.New("ID 0 is not valid (Jamf Pro uses 0 as a sentinel value)")
		}
		return e.Value, nil
	}
	if s.Lookup == nil {
		return "", errNoNameLookup
	}
	var matches []NamedRecord
	if s.FoldCase {
		matches = byName[strings.ToLower(e.Value)]
		var exact []NamedRecord
		for _, m := range matches {
			if m.Name == e.Value {
				exact = append(exact, m)
			}
		}
		if len(exact) == 1 {
			matches = exact
		}
	} else {
		matches = byName[e.Value]
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no %s named %q", s.Label, e.Value)
	case 1:
		return matches[0].ID, nil
	}
	ids := make([]string, len(matches))
	for i, m := range matches {
		ids[i] = m.ID
	}
	return "", &AmbiguousEntryError{Label: s.Label, Value: e.Value, IDs: ids}
}

func (s NamedEntrySpec) failure(e listfile.Entry, err error) string {
	switch {
	case !e.Hidden():
		return output.SanitizeTerminal(err.Error())
	case errors.Is(err, ErrAmbiguousDevice):
		return fmt.Sprintf("it matches more than one %s; give its numeric ID", s.Label)
	case errors.Is(err, errNoNameLookup):
		return err.Error()
	default:
		return fmt.Sprintf("no %s matches it", s.Label)
	}
}

// ComputerTargetsFromFile resolves a --list-file of computers (ID, serial
// number, UDID, management ID or name) to targets. See
// ResolveComputersFromFile for the lookup and the failure policy.
func ComputerTargetsFromFile(ctx context.Context, client registry.HTTPClient, path string) ([]Target, int, error) {
	return deviceTargets(ResolveComputersFromFile(ctx, client, path))
}

// MobileDeviceTargetsFromFile is ComputerTargetsFromFile for mobile devices.
func MobileDeviceTargetsFromFile(ctx context.Context, client registry.HTTPClient, path string) ([]Target, int, error) {
	return deviceTargets(ResolveMobileDevicesFromFile(ctx, client, path))
}

// deviceTargets turns resolved devices into targets, dropping a device that an
// earlier line already named: the device resolver keeps one result per line.
func deviceTargets(devices []*DeviceIdentifiers, skipped int, err error) ([]Target, int, error) {
	if err != nil {
		return nil, skipped, err
	}
	targets := make([]Target, 0, len(devices))
	seen := map[string]bool{}
	for _, d := range devices {
		if seen[d.ID] {
			continue
		}
		seen[d.ID] = true
		targets = append(targets, Target{ID: d.ID, Label: FormatDeviceDesc(d)})
	}
	return targets, skipped, nil
}

// FilterChunks splits values into the chunks a batched RSQL `=in=` lookup
// sends: at most 100 each, and closed early once the query-escaped filter
// would pass 4 KB. See filterChunks.
func FilterChunks(values []string, filter func(chunk []string) string) [][]string {
	return filterChunks(values, filter)
}

// QuotedList renders values as the quoted, escaped members of an RSQL `=in=`
// list.
func QuotedList(values []string) string { return quotedList(values) }

// ProNameLookup looks names up in a Jamf Pro API collection: chunked RSQL
// `nameField=in=(…)` filters, each paged through to its totalCount, so N names
// cost about N/100 requests rather than N. listPath may carry a query string
// (a section the name field lives in). idField and nameField may be dotted
// paths into each record.
//
// A collection that answers 400 to `=in=` is asked again with the names OR'd
// as `==` clauses, and one that refuses that too is listed whole; either way
// ResolveNamedEntries matches each name itself, so a collection that ignores
// the filter (the prestages do) still resolves correctly, only more slowly.
func ProNameLookup(client registry.HTTPClient, listPath, nameField, idField string) NameLookup {
	return func(ctx context.Context, names []string) ([]NamedRecord, error) {
		ctx = registry.WithAllowedStatuses(ctx, http.StatusBadRequest)
		var out []NamedRecord
		inList := func(chunk []string) string { return fmt.Sprintf("%s=in=(%s)", nameField, QuotedList(chunk)) }
		orEq := func(chunk []string) string {
			clauses := make([]string, len(chunk))
			for i, n := range chunk {
				clauses[i] = fmt.Sprintf(`%s=="%s"`, nameField, EscapeRSQL(n))
			}
			return strings.Join(clauses, ",")
		}
		for _, chunk := range FilterChunks(names, inList) {
			records, err := fetchRecords(ctx, client, withFilter(listPath, inList(chunk)))
			if isBadRequest(err) {
				records, err = fetchRecords(ctx, client, withFilter(listPath, orEq(chunk)))
			}
			if isBadRequest(err) {
				records, err = fetchRecords(ctx, client, listPath)
				if err == nil {
					// The whole collection is in hand; no other chunk needs a
					// request of its own.
					return namedRecords(records, nameField, idField), nil
				}
			}
			if err != nil {
				return nil, err
			}
			out = append(out, namedRecords(records, nameField, idField)...)
		}
		return out, nil
	}
}

func withFilter(listPath, filter string) string {
	sep := "?"
	if strings.Contains(listPath, "?") {
		sep = "&"
	}
	return listPath + sep + "filter=" + url.QueryEscape(filter)
}

func isBadRequest(err error) bool {
	var hse *httpStatusError
	return errors.As(err, &hse) && hse.code == http.StatusBadRequest
}

// fetchRecords pages through a filtered Pro collection, or reads a collection
// that answers a plain array in one request.
func fetchRecords(ctx context.Context, client registry.HTTPClient, path string) ([]map[string]any, error) {
	records, err := fetchAllPages(ctx, client, path)
	if _, ok := errors.AsType[*pageShapeError](err); !ok {
		return records, err
	}
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, &httpStatusError{code: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	var arr []map[string]any
	if err := json.Unmarshal(body, &arr); err != nil {
		return nil, fmt.Errorf("parsing lookup response: %w", err)
	}
	return arr, nil
}

func namedRecords(records []map[string]any, nameField, idField string) []NamedRecord {
	out := make([]NamedRecord, 0, len(records))
	for _, r := range records {
		out = append(out, NamedRecord{ID: dottedString(r, idField), Name: dottedString(r, nameField)})
	}
	return out
}

// dottedString reads a dotted path (general.name) out of a record as a
// string, rendering a numeric ID without a fraction.
func dottedString(m map[string]any, path string) string {
	parts := strings.Split(path, ".")
	for _, p := range parts[:len(parts)-1] {
		inner, ok := m[p].(map[string]any)
		if !ok {
			return ""
		}
		m = inner
	}
	return jsonString(m, parts[len(parts)-1])
}

// ResolveOne resolves one --list-file line for a resource that has no batched
// lookup, returning the record's ID and a label naming it.
type ResolveOne func(ctx context.Context, value string) (id, label string, err error)

// ResolveEachFile resolves path's entries one line at a time, for a hand-written
// command whose resource resolves an identifier through its own resolver
// (Protect's computers, School's devices, the Platform devices). It keeps the
// policy of ResolveNamedEntries — a line that fails to resolve is warned about
// and skipped, an entry resolving to a record already targeted is dropped, and
// a list in which nothing resolves is an error — except that a failure the
// next line would meet too stops the whole list at once rather than printing
// one warning per line: an authentication, permission, rate-limit or
// unsupported refusal.
func ResolveEachFile(ctx context.Context, label, path string, resolveOne ResolveOne) ([]Target, int, error) {
	entries, err := listfile.Read(path)
	if err != nil {
		return nil, 0, fmt.Errorf("reading --%s: %w", listfile.FlagName, err)
	}
	if len(entries) == 0 {
		return nil, 0, fmt.Errorf("%s contains no entries", listfile.SourceName(path))
	}
	targets := make([]Target, 0, len(entries))
	seen := map[string]bool{}
	skipped := 0
	for _, e := range entries {
		id, desc, err := resolveOne(ctx, e.Value)
		if err != nil {
			if StopsTheList(err) {
				return nil, 0, e.Wrap(fmt.Sprintf("resolving %s %s", label, e.Label()), err)
			}
			_, _ = fmt.Fprintf(os.Stderr, "  warning: could not resolve %s %s: %s\n", label, e.Label(), output.SanitizeTerminal(e.Hide(err).Error()))
			skipped++
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		if desc == "" || e.Hidden() {
			desc = e.Target(output.SanitizeTerminal(id))
		} else {
			desc = output.SanitizeTerminal(desc)
		}
		targets = append(targets, Target{ID: id, Label: desc, Entry: e})
	}
	if len(targets) == 0 {
		return nil, skipped, fmt.Errorf("none of the %d entries in %s could be resolved", len(entries), listfile.SourceName(path))
	}
	return targets, skipped, nil
}

// StopsTheList reports whether a failure is about the request rather than the
// line (authentication, permission, rate limit, unsupported), so every later
// line of a --list-file batch would fail the same way. Resolution and the
// delete and read loops all stop on it instead of sending the rest.
func StopsTheList(err error) bool {
	switch exitcode.CodeFrom(err) {
	case exitcode.Authentication, exitcode.PermissionDenied, exitcode.RateLimited, exitcode.Unsupported:
		return true
	}
	return false
}

// AbortBatch ends a --list-file batch that StopsTheList says cannot go on. It
// reports how many lines were never attempted and returns err itself, wrapped
// so the process keeps err's own exit code: not the partial-failure 7 and not
// the 0 --allow-partial-failure would give, either of which would hide a
// revoked token or a missing privilege behind a tally.
func AbortBatch(err error, done, notAttempted int, noun string) error {
	_, _ = fmt.Fprintf(os.Stderr, "stopping: %d %s not attempted after %d done, because every later request would fail the same way\n", notAttempted, noun, done)
	return fmt.Errorf("%s batch stopped: %w", noun, err)
}
