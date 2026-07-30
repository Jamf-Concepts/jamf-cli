// Copyright 2026, Jamf Software LLC

package profileconvert

import (
	"fmt"

	"howett.net/plist"
)

// InjectIdentifiers preserves the PayloadUUID and PayloadIdentifier from an
// existing mobileconfig plist into a new mobileconfig plist. This prevents
// macOS/iOS devices from treating a profile update as a new installation,
// which causes "ghost profiles" where the old profile lingers on devices
// even though the server considers it replaced.
//
// Returns the modified new plist bytes serialised in the original plist
// format. If existingPlist is empty or cannot be parsed, newPlist is
// returned unchanged without error. If newPlist itself cannot be parsed,
// an error is returned.
func InjectIdentifiers(newPlist, existingPlist []byte) ([]byte, error) {
	if len(existingPlist) == 0 {
		return newPlist, nil
	}

	// Extract UUID and identifier from existing plist.
	var existing map[string]any
	if _, err := plist.Unmarshal(existingPlist, &existing); err != nil {
		// Unparseable existing plist — proceed without injection.
		return newPlist, nil
	}

	uuid, _ := existing["PayloadUUID"].(string)
	identifier, _ := existing["PayloadIdentifier"].(string)

	if uuid == "" && identifier == "" {
		return newPlist, nil
	}

	// Parse the new plist so we can overwrite the two identity fields.
	var newProfile map[string]any
	format, err := plist.Unmarshal(newPlist, &newProfile)
	if err != nil {
		return nil, fmt.Errorf("parsing new mobileconfig: %w", err)
	}

	if uuid != "" {
		newProfile["PayloadUUID"] = uuid
	}
	if identifier != "" {
		newProfile["PayloadIdentifier"] = identifier
	}

	// Re-serialise in the original plist format (XMLFormat for mobileconfigs).
	out, err := plist.MarshalIndent(newProfile, format, "\t")
	if err != nil {
		return nil, fmt.Errorf("re-serialising mobileconfig: %w", err)
	}
	return out, nil
}

// NormalizeXML parses a plist (XML or binary) and re-serialises it as XML
// with standard entity escaping. Used to eliminate constructs that cannot
// survive CDATA-wrapping for the Classic API — most notably embedded CDATA
// sections, whose literal "]]>" terminator would end the wrapper early.
// Values round-trip exactly; only the byte representation changes.
func NormalizeXML(data []byte) ([]byte, error) {
	var v any
	if _, err := plist.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("parsing plist: %w", err)
	}
	out, err := plist.MarshalIndent(v, plist.XMLFormat, "\t")
	if err != nil {
		return nil, fmt.Errorf("re-serialising plist: %w", err)
	}
	return out, nil
}

// ExtractProfileIdentifiers extracts the top-level PayloadUUID and
// PayloadIdentifier from a mobileconfig plist. Returns empty strings when
// the fields are absent (not an error condition).
func ExtractProfileIdentifiers(data []byte) (uuid, identifier string, err error) {
	var profile map[string]any
	if _, parseErr := plist.Unmarshal(data, &profile); parseErr != nil {
		return "", "", fmt.Errorf("parsing mobileconfig: %w", parseErr)
	}
	uuid, _ = profile["PayloadUUID"].(string)
	identifier, _ = profile["PayloadIdentifier"].(string)
	return uuid, identifier, nil
}
