// Copyright 2026, Jamf Software LLC

package profileconvert

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/smallstep/pkcs7"
	"howett.net/plist"
)

// IsSignedProfile reports whether data looks like a CMS/DER-signed
// mobileconfig (ASN.1 SEQUENCE header) rather than plist XML or a binary
// plist. A false positive is harmless — ExtractSignedProfile fails cleanly
// and callers fall back to the original bytes.
func IsSignedProfile(data []byte) bool {
	if len(data) < 2 || data[0] != 0x30 {
		return false
	}
	switch data[1] {
	case 0x80, 0x81, 0x82, 0x83, 0x84:
		return true
	}
	return false
}

// ExtractSignedProfile returns the inner plist of a CMS-signed (DER/BER)
// mobileconfig. The signature cannot survive a Classic API upload anyway —
// the server re-serialises the plist on ingest — so callers extract and
// send the content, surfacing a note to the user.
func ExtractSignedProfile(data []byte) ([]byte, error) {
	p7, err := pkcs7.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parsing CMS envelope: %w", err)
	}
	if len(p7.Content) == 0 {
		return nil, fmt.Errorf("CMS envelope has no content")
	}
	return p7.Content, nil
}

// maskedPayloadMetaKeys are profile-metadata keys the Classic API rewrites
// or injects on ingest (identifiers, org branding, enablement flags, display
// names of payloads it re-renders). Differences there are expected server
// behaviour, not content corruption, and are excluded from verification.
var maskedPayloadMetaKeys = map[string]bool{
	"PayloadUUID":              true,
	"PayloadIdentifier":        true,
	"PayloadOrganization":      true,
	"PayloadDisplayName":       true,
	"PayloadDescription":       true,
	"PayloadEnabled":           true,
	"PayloadRemovalDisallowed": true,
	"PayloadScope":             true,
	"PayloadVersion":           true,
}

// DiffPayloadValues compares an intended mobileconfig plist against the
// server-stored form and returns the dotted paths of setting values the
// server did not store faithfully. Metadata keys the server rewrites by
// design are masked (maskedPayloadMetaKeys); string comparison ignores
// leading/trailing whitespace because the server trims value edges on
// ingest. Keys present only in the stored form (server-injected defaults)
// are ignored — only the caller's intent is checked.
func DiffPayloadValues(intended, stored []byte) ([]string, error) {
	var want, got map[string]any
	if _, err := plist.Unmarshal(intended, &want); err != nil {
		return nil, fmt.Errorf("parsing intended plist: %w", err)
	}
	if _, err := plist.Unmarshal(stored, &got); err != nil {
		return nil, fmt.Errorf("parsing stored plist: %w", err)
	}
	var diffs []string
	diffPayloadValues(want, got, "", &diffs)
	return diffs, nil
}

func diffPayloadValues(intended, stored any, path string, diffs *[]string) {
	switch want := intended.(type) {
	case map[string]any:
		got, ok := stored.(map[string]any)
		if !ok {
			*diffs = append(*diffs, path)
			return
		}
		for k, v := range want {
			if maskedPayloadMetaKeys[k] {
				continue
			}
			p := k
			if path != "" {
				p = path + "." + k
			}
			s, present := got[k]
			if !present {
				// A missing empty container is a harmless server omission.
				if isEmptyContainer(v) {
					continue
				}
				*diffs = append(*diffs, p)
				continue
			}
			diffPayloadValues(v, s, p, diffs)
		}
	case []any:
		got, ok := stored.([]any)
		if !ok || len(got) != len(want) {
			*diffs = append(*diffs, path)
			return
		}
		for i := range want {
			diffPayloadValues(want[i], got[i], fmt.Sprintf("%s[%d]", path, i), diffs)
		}
	case string:
		got, ok := stored.(string)
		if !ok || strings.TrimSpace(got) != strings.TrimSpace(want) {
			*diffs = append(*diffs, path)
		}
	case []byte:
		got, ok := stored.([]byte)
		if !ok || !bytes.Equal(got, want) {
			*diffs = append(*diffs, path)
		}
	default:
		// Numbers, bools, dates: same parser both sides, but be lenient on
		// integer width differences via the printed form.
		if fmt.Sprint(intended) != fmt.Sprint(stored) {
			*diffs = append(*diffs, path)
		}
	}
}

func isEmptyContainer(v any) bool {
	switch t := v.(type) {
	case map[string]any:
		return len(t) == 0
	case []any:
		return len(t) == 0
	case string:
		return t == ""
	}
	return false
}
