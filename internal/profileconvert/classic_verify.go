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

// normalizeLineEndings folds CRLF and lone CR to LF. A carriage return is the
// only whitespace character Jamf Pro keeps inside a string value, so line
// breaks have to be submitted as "&#13;" — but they never read back as CR:
// MCX and mobile fragments convert them to LF on store, and a verbatim-stored
// CR comes back as a raw CR byte that our own plist parse normalises. Without
// this fold, every profile that uses the only working line break (including
// every profile authored by Jamf Pro's own UI) would look unfaithfully stored.
//
// U+2028/U+2029/U+0085 are deliberately untouched — they round-trip byte-exact,
// so comparing them strictly keeps real corruption detectable.
func normalizeLineEndings(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// PayloadDiff pairs the path of a value the server did not store faithfully
// with a short explanation of how it differs.
type PayloadDiff struct {
	Path   string
	Reason string
}

// DiffPayloadValuesDetailed compares an intended mobileconfig plist against
// the server-stored form and classifies each difference, so callers can
// surface the remedy that actually applies instead of blaming the PI-827
// entity layer for every divergence — the line-break deletion class is far
// more common and has a workaround. Metadata keys the server rewrites by
// design are masked (maskedPayloadMetaKeys); string comparison ignores
// leading/trailing whitespace because the server trims value edges on
// ingest, and CR/LF differences (see normalizeLineEndings). Keys present
// only in the stored form (server-injected defaults) are ignored — only the
// caller's intent is checked.
func DiffPayloadValuesDetailed(intended, stored []byte) ([]PayloadDiff, error) {
	var want, got map[string]any
	if _, err := plist.Unmarshal(intended, &want); err != nil {
		return nil, fmt.Errorf("parsing intended plist: %w", err)
	}
	if _, err := plist.Unmarshal(stored, &got); err != nil {
		return nil, fmt.Errorf("parsing stored plist: %w", err)
	}
	var diffs []PayloadDiff
	diffPayloadValues(want, got, "", &diffs)
	return diffs, nil
}

func diffPayloadValues(intended, stored any, path string, diffs *[]PayloadDiff) {
	switch want := intended.(type) {
	case map[string]any:
		got, ok := stored.(map[string]any)
		if !ok {
			*diffs = append(*diffs, PayloadDiff{path, "server did not store a dictionary here"})
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
				*diffs = append(*diffs, PayloadDiff{p, "not stored at all"})
				continue
			}
			diffPayloadValues(v, s, p, diffs)
		}
	case []any:
		got, ok := stored.([]any)
		if !ok || len(got) != len(want) {
			*diffs = append(*diffs, PayloadDiff{path, "server stored a different number of entries"})
			return
		}
		for i := range want {
			diffPayloadValues(want[i], got[i], fmt.Sprintf("%s[%d]", path, i), diffs)
		}
	case string:
		got, ok := stored.(string)
		if !ok {
			*diffs = append(*diffs, PayloadDiff{path, "server did not store a string here"})
			return
		}
		w, g := normalizeLineEndings(strings.TrimSpace(want)), normalizeLineEndings(strings.TrimSpace(got))
		if w != g {
			*diffs = append(*diffs, PayloadDiff{path, stringDiffReason(w, g)})
		}
	case []byte:
		got, ok := stored.([]byte)
		if !ok || !bytes.Equal(got, want) {
			*diffs = append(*diffs, PayloadDiff{path, "binary data differs"})
		}
	default:
		// Numbers, bools, dates: same parser both sides, but be lenient on
		// integer width differences via the printed form.
		if fmt.Sprint(intended) != fmt.Sprint(stored) {
			*diffs = append(*diffs, PayloadDiff{path, "value differs"})
		}
	}
}

// stringDiffReason classifies how a stored string diverged from what was sent.
// The classes are the wire behaviours probed against Jamf Pro 11.30.x; each
// carries the remedy that applies to it, if any.
func stringDiffReason(want, got string) string {
	switch {
	case strings.NewReplacer("\n", "", "\t", "").Replace(want) == got:
		return "line breaks/tabs removed — Jamf Pro deletes literal line feeds and tabs here; " +
			"write a line break as the character reference &#13; (the form Jamf Pro's own UI writes) " +
			"or move the value into an Application & Custom Settings payload, which keeps them"
	case strings.NewReplacer("&", "&amp;", "<", "&lt;").Replace(want) == got:
		return "extra & / < entity layer added by the server (Jamf PI-827) — no wire format stores this payload type faithfully"
	case strings.Contains(got, "�") && hasNonBMP(want):
		return "non-BMP characters (e.g. emoji) replaced by the server — a Jamf-side limitation; macOS itself handles them"
	}
	return "value differs"
}

// hasNonBMP reports whether s contains a character outside the Basic
// Multilingual Plane (emoji and similar), which Jamf Pro cannot store.
func hasNonBMP(s string) bool {
	for _, r := range s {
		if r > 0xFFFF {
			return true
		}
	}
	return false
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
