// Copyright 2026, Jamf Software LLC

package resolve

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// ErrNoDeviceMatch is what a NoDeviceMatchError answers errors.Is with, so a
// caller can tell "nothing is called that" from a failed lookup.
var ErrNoDeviceMatch = errors.New("no matching device")

// NoDeviceMatchError reports a value that names no device.
type NoDeviceMatchError struct {
	Label string // "computer" / "mobile device"
	Value string
}

func (e *NoDeviceMatchError) Error() string {
	return fmt.Sprintf("no %s found with ID, name, UDID or serial number %q", e.Label, e.Value)
}

// Is makes errors.Is(err, ErrNoDeviceMatch) hold.
func (e *NoDeviceMatchError) Is(target error) bool { return target == ErrNoDeviceMatch }

// identifierSpec describes one device family's inventory endpoint for
// ResolveComputerIdentifier / ResolveMobileDeviceIdentifier.
type identifierSpec struct {
	label   string // "computer" / "mobile device"
	path    string // inventory list path, with the sections parse needs
	idField string // RSQL field holding the numeric ID
	// fields are the RSQL fields holding the name, UDID and serial number.
	nameField, udidField, serialField string
	parse                             func(map[string]any) (*DeviceIdentifiers, error)
	// readPrivilege is the Jamf Pro privilege the lookup needs, named in the
	// hint on a 401 or 403.
	readPrivilege string
}

// Both endpoints are published on the platform gateway; the per-ID inventory
// paths the other resolvers use are not all, so the ID goes through the filter
// as well.
var computerIdentifierSpec = identifierSpec{
	label:         "computer",
	path:          "/v4/computers-inventory?section=GENERAL&section=HARDWARE",
	idField:       "id",
	nameField:     "general.name",
	udidField:     "udid",
	serialField:   "hardware.serialNumber",
	parse:         parseComputerInventory,
	readPrivilege: "Read Computers",
}

var mobileIdentifierSpec = identifierSpec{
	label:         "mobile device",
	path:          "/v2/mobile-devices/detail?section=GENERAL&section=HARDWARE",
	idField:       "mobileDeviceId",
	nameField:     "displayName",
	udidField:     "udid",
	serialField:   "serialNumber",
	parse:         parseMobileDevice,
	readPrivilege: "Read Mobile Devices",
}

// identifierPageSize bounds the lookup. One exact identifier names a handful of
// records at most (names are not unique); anything past this is reported as
// ambiguous all the same.
const identifierPageSize = 20

// ResolveComputerIdentifier resolves a value that may be a computer's Jamf Pro
// ID, name, UDID or serial number to one computer, in a single request.
//
// See resolveIdentifier for how a value matching more than one record is
// handled.
func ResolveComputerIdentifier(ctx context.Context, client registry.HTTPClient, value string) (*DeviceIdentifiers, error) {
	return resolveIdentifier(ctx, client, computerIdentifierSpec, value)
}

// ResolveMobileDeviceIdentifier is ResolveComputerIdentifier for mobile devices.
func ResolveMobileDeviceIdentifier(ctx context.Context, client registry.HTTPClient, value string) (*DeviceIdentifiers, error) {
	return resolveIdentifier(ctx, client, mobileIdentifierSpec, value)
}

// resolveIdentifier ORs the value across every identifier field and then
// re-checks each result exactly, since RSQL `==` treats `*` as a wildcard.
//
// A numeric value that is some record's ID resolves to that record even when
// another record carries the same digits as its name, because the numeric-ID
// reading is the CLI's documented contract and the one the Classic API itself
// applies first. Any other value must name exactly one record: Classic names
// are not unique, and the Classic API's own name matching would silently pick
// one of the duplicates.
func resolveIdentifier(ctx context.Context, client registry.HTTPClient, spec identifierSpec, value string) (*DeviceIdentifiers, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("empty %s identifier", spec.label)
	}

	quoted := `"` + EscapeRSQL(value) + `"`
	clauses := []string{
		spec.nameField + "==" + quoted,
		spec.udidField + "==" + quoted,
		spec.serialField + "==" + quoted,
	}
	if isNumericID(value) {
		clauses = append([]string{spec.idField + "==" + value}, clauses...)
	}
	path := fmt.Sprintf("%s&page-size=%d&filter=%s", spec.path, identifierPageSize,
		url.QueryEscape(strings.Join(clauses, ",")))

	records, _, err := fetchInventoryPage(ctx, client, path)
	if err != nil {
		return nil, lookupError(spec, value, err)
	}

	var matches []*DeviceIdentifiers
	for _, record := range records {
		d, err := spec.parse(record)
		if err != nil {
			continue
		}
		if isNumericID(value) && d.ID == value {
			return d, nil
		}
		if matchedBy(d, value) != "" {
			matches = append(matches, d)
		}
	}

	switch len(matches) {
	case 0:
		return nil, &NoDeviceMatchError{Label: spec.label, Value: value}
	case 1:
		return matches[0], nil
	}
	described := make([]string, len(matches))
	for i, d := range matches {
		described[i] = fmt.Sprintf("id %s (%s)", d.ID, matchedBy(d, value))
	}
	return nil, fmt.Errorf("%q matches %d %ss: %s; pass the numeric ID of the one you mean",
		value, len(matches), spec.label, strings.Join(described, ", "))
}

// lookupError wraps a failed inventory lookup. A 401 or 403 gets a hint naming
// the read privilege. Resolving a device is a new inventory read, so an API
// client that could scope a computer by numeric ID before now fails here, and
// the bare status does not say which privilege is missing.
func lookupError(spec identifierSpec, value string, err error) error {
	msg := fmt.Sprintf("looking up %s %q", spec.label, value)
	var ee *exitcode.Error
	if !errors.As(err, &ee) || (ee.Code != exitcode.PermissionDenied && ee.Code != exitcode.Authentication) {
		return fmt.Errorf("%s: %w", msg, err)
	}
	hint := fmt.Sprintf("resolving a %s to its ID reads inventory, so the API client needs %s", spec.label, spec.readPrivilege)
	if ee.Hint != "" {
		hint += "; " + ee.Hint
	}
	return &exitcode.Error{Code: ee.Code, Message: msg, Err: err, Hint: hint, Details: ee.Details}
}

// matchedBy names the identifier a record matched value on, or "" for none.
func matchedBy(d *DeviceIdentifiers, value string) string {
	switch {
	case strings.EqualFold(d.Name, value):
		return "name"
	case strings.EqualFold(d.UDID, value):
		return "UDID"
	case strings.EqualFold(d.SerialNumber, value):
		return "serial number"
	}
	return ""
}
