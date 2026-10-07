// Copyright 2026, Jamf Software LLC

package profileconvert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"howett.net/plist"
)

// TakeoverReport says whether a blueprint built from a Classic profile can take
// over the copy of that profile already installed on a device, and if not, why.
//
// Takeover is Apple's legacy-profile declaration (com.apple.configuration.legacy):
// a DDM profile adopts an MDM-installed one, without reinstalling it, only when
// the top-level PayloadIdentifier and PayloadUUID match, the payload count
// matches, and every payload has the same PayloadType, PayloadIdentifier and
// PayloadUUID in the same order. A profile that fails any of that is not adopted.
// Worse, if the identity is preserved but a type changed, the declaration is
// reported invalid and nothing is applied at all (wire-checked 2026-10-06: a
// 14-payload profile with seven MCX-wrapped payloads reported failed:1 on the
// device). So identity is only ever preserved when every condition holds.
type TakeoverReport struct {
	// Supported is true when the blueprint carries the profile's identity and
	// deploying it adopts the installed profile.
	Supported bool
	// Reasons lists why takeover is not supported. Empty when Supported.
	Reasons []string
}

// ApplyTakeoverIdentity decides whether the converted configuration can take over
// the profile it came from and, when it can, stamps the profile's own identity
// onto it: the top-level payloadIdentifier/payloadUUID and each payload's.
//
// It judges the converted output against the original profile rather than
// predicting from the input, so every way the converter can change the shape of
// a profile (wrapping a payload as MCX, skipping a disabled type, unwrapping an
// MCX payload, dropping an empty payload, rewriting a type's spelling, promoting
// payloads to native components) shows up as a mismatch.
//
// nativeComponents is the number of native DDM components the conversion
// produced alongside the configuration-profile component; any at all means the
// profile has been split and cannot be adopted.
//
// config is returned unchanged when takeover is not supported: preserving
// identity there would make the declaration invalid instead of installing
// alongside the Classic profile.
func ApplyTakeoverIdentity(config json.RawMessage, original []byte, nativeComponents int) (json.RawMessage, TakeoverReport, error) {
	var profile map[string]any
	if _, err := plist.Unmarshal(original, &profile); err != nil {
		return config, TakeoverReport{}, fmt.Errorf("parsing mobileconfig: %w", err)
	}

	var reasons []string

	if nativeComponents > 0 {
		reasons = append(reasons, nativeConversionReason(nativeComponents))
	}

	topID, _ := profile["PayloadIdentifier"].(string)
	topUUID, _ := profile["PayloadUUID"].(string)
	if topID == "" || topUUID == "" {
		reasons = append(reasons, "the profile has no top-level PayloadIdentifier and PayloadUUID to carry over")
	}

	origContent, _ := profile["PayloadContent"].([]any)

	parsed, produced, err := decodeConfig(config)
	if err != nil {
		return config, TakeoverReport{}, err
	}

	type identity struct{ id, uuid string }
	ids := make([]identity, 0, len(origContent))

	if len(produced) != len(origContent) {
		reasons = append(reasons, fmt.Sprintf(
			"the blueprint carries %d payload(s) but the installed profile has %d (payloads were skipped, removed as empty or unwrapped)",
			len(produced), len(origContent)))
	} else {
		for i, item := range origContent {
			payload, _ := item.(map[string]any)
			origType, _ := payload["PayloadType"].(string)
			id, _ := payload["PayloadIdentifier"].(string)
			uuid, _ := payload["PayloadUUID"].(string)
			ids = append(ids, identity{id, uuid})

			entry, _ := produced[i].(map[string]any)
			gotType, _ := entry["payloadType"].(string)

			switch {
			case gotType != origType:
				reasons = append(reasons, fmt.Sprintf("payload %d (%s) is delivered as %s", i+1, origType, gotType))
			case DisabledPayloadTypes[origType]:
				reasons = append(reasons, fmt.Sprintf("payload %d (%s) is disabled by blueprints", i+1, origType))
			case id == "" || uuid == "":
				reasons = append(reasons, fmt.Sprintf("payload %d (%s) has no PayloadIdentifier and PayloadUUID to carry over", i+1, origType))
			case id != uuid:
				// The blueprints API rewrites a payload's UUID to equal its identifier,
				// so a payload whose two differ can never match the installed copy.
				reasons = append(reasons, fmt.Sprintf("payload %d (%s) has a PayloadUUID that differs from its PayloadIdentifier, and blueprints force them to match", i+1, origType))
			}
		}
	}

	if len(reasons) > 0 {
		return config, TakeoverReport{Reasons: reasons}, nil
	}

	parsed["payloadIdentifier"] = topID
	parsed["payloadUUID"] = topUUID
	for i := range produced {
		entry, _ := produced[i].(map[string]any)
		entry["payloadIdentifier"] = ids[i].id
		entry["payloadUUID"] = ids[i].uuid
	}
	stamped, err := marshalConfig(parsed)
	if err != nil {
		return config, TakeoverReport{}, err
	}
	return stamped, TakeoverReport{Supported: true}, nil
}

func nativeConversionReason(n int) string {
	return fmt.Sprintf("%d payload type(s) were converted to native DDM components, which splits the profile", n)
}

// NativeConversionReport is the takeover verdict for a profile every payload of
// which was promoted to a native DDM component, leaving no legacy wrapper to
// stamp identity onto.
func NativeConversionReport(nativeComponents int) TakeoverReport {
	return TakeoverReport{Reasons: []string{nativeConversionReason(nativeComponents)}}
}

// decodeConfig unmarshals a configuration keeping numbers exact, so stamping
// identity onto it does not rewrite an integer setting into a float.
func decodeConfig(config json.RawMessage) (map[string]any, []any, error) {
	dec := json.NewDecoder(bytes.NewReader(config))
	dec.UseNumber()
	var parsed map[string]any
	if err := dec.Decode(&parsed); err != nil {
		return nil, nil, fmt.Errorf("parsing converted configuration: %w", err)
	}
	content, ok := parsed["payloadContent"].([]any)
	if !ok {
		return nil, nil, fmt.Errorf("converted configuration has no payloadContent")
	}
	return parsed, content, nil
}

// maxDescriptionLength is the blueprints API limit on a description.
const maxDescriptionLength = 2000

// ImportDescription is the blueprint description import-profile writes, saying
// where the blueprint came from and whether it can take over the installed
// profile. It stays one short line: the full reasons are in the command's own
// warning, and a description is read in a list.
func ImportDescription(profileKind, profileName, profileID string, report TakeoverReport) string {
	kind := "computer profile"
	if profileKind == "mobile" {
		kind = "mobile device profile"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Imported from %s %q (ID %s) by jamf-cli. ", kind, profileName, profileID)
	if report.Supported {
		b.WriteString("Takeover supported.")
		return clipDescription(b.String())
	}
	b.WriteString("Takeover not supported")
	if len(report.Reasons) > 0 {
		b.WriteString(": ")
		b.WriteString(shortReason(report.Reasons[0]))
		if more := len(report.Reasons) - 1; more > 0 {
			fmt.Fprintf(&b, " (+%d more)", more)
		}
	}
	if s := b.String(); !strings.HasSuffix(s, "…") {
		b.WriteString(".")
	}
	return clipDescription(b.String())
}

var (
	reasonTrailingParen = regexp.MustCompile(`\s*\([^()]*\)$`)
	reasonTail          = regexp.MustCompile(`,\s+(and|which)\s.*$`)
)

// shortReason trims a takeover reason to its first clause: the explanatory tail
// (", and blueprints force them to match", "(payloads were skipped, ...)") is for
// the warning on stderr, not for a description.
func shortReason(r string) string {
	r = reasonTrailingParen.ReplaceAllString(r, "")
	r = reasonTail.ReplaceAllString(r, "")
	const limit = 110
	if len(r) > limit {
		cut := strings.LastIndexByte(r[:limit], ' ')
		if cut <= 0 {
			cut = limit
		}
		for cut > 0 && !utf8.RuneStart(r[cut]) {
			cut--
		}
		r = r[:cut] + "…"
	}
	return r
}

// clipDescription shortens s to the API limit in bytes, which is stricter than
// the limit in characters, cutting on a rune boundary so a profile name with
// multi-byte characters is never split.
func clipDescription(s string) string {
	const ellipsis = "..."
	if len(s) <= maxDescriptionLength {
		return s
	}
	cut := maxDescriptionLength - len(ellipsis)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + ellipsis
}

// DroppedKeys compares the configuration sent to the blueprints API with the one
// it stored and lists, per payload, the keys the API discarded. The API silently
// drops any key Apple's schema does not define for that payload type, and once a
// blueprint has taken over a profile those keys are no longer enforced on the
// device (wire-checked: InterfaceLevel on com.apple.finder).
//
// Only top-level keys of each payload are compared. Payloads are matched by
// position, which holds because the API preserves their order.
func DroppedKeys(sent, stored json.RawMessage) []string {
	_, sentContent, err := decodeConfig(sent)
	if err != nil {
		return nil
	}
	_, storedContent, err := decodeConfig(stored)
	if err != nil || len(storedContent) != len(sentContent) {
		return nil
	}
	var out []string
	for i := range sentContent {
		s, _ := sentContent[i].(map[string]any)
		g, _ := storedContent[i].(map[string]any)
		var lost []string
		for k := range s {
			if _, ok := g[k]; !ok {
				lost = append(lost, k)
			}
		}
		if len(lost) == 0 {
			continue
		}
		sort.Strings(lost)
		t, _ := s["payloadType"].(string)
		out = append(out, fmt.Sprintf("payload %d (%s): %s", i+1, t, strings.Join(lost, ", ")))
	}
	return out
}
