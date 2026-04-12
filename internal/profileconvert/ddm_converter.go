// Copyright 2026, Jamf Software LLC

package profileconvert

import (
	"encoding/json"
	"fmt"

	"howett.net/plist"
)

// DDMComponent represents a native DDM blueprint component produced by converting
// a legacy mobileconfig payload.
type DDMComponent struct {
	Identifier    string          `json:"identifier"`
	Configuration json.RawMessage `json:"configuration"`
}

// DDMConversionResult holds the output of converting a mobileconfig where
// compatible payloads are automatically promoted to native DDM components.
type DDMConversionResult struct {
	// NativeComponents are payloads successfully converted to native DDM.
	NativeComponents []DDMComponent
	// ProfileConfig is the DDMProfileDto wrapping payloads that could not be
	// converted. Nil when every payload was converted to a native component.
	ProfileConfig json.RawMessage
	// DisplayName from the original mobileconfig's PayloadDisplayName.
	DisplayName string
	// Warnings about conversion issues (dropped keys, unsupported types, etc.).
	Warnings []string
	// Conversions describes each successful native DDM conversion
	// (e.g. "com.apple.mobiledevice.passwordpolicy -> com.jamf.ddm.passcode-settings").
	Conversions []string
}

// convertFunc transforms legacy payload settings into a native DDM component
// configuration. It receives settings with Apple metadata already stripped.
//
// Returns:
//   - config: DDM component configuration JSON (nil if no convertible keys)
//   - remaining: settings keys the converter did not handle (nil if all consumed)
//   - warnings: informational messages
//   - err: structural errors only
type convertFunc func(settings map[string]any) (config json.RawMessage, remaining map[string]any, warnings []string, err error)

type ddmConverter struct {
	componentID  string
	payloadTypes map[string]bool
	convert      convertFunc
}

// converters is the ordered registry of DDM payload converters.
// Order matters: for payload types handled by multiple converters (e.g.
// com.apple.applicationaccess), converters run sequentially and each
// receives the remaining keys from the previous one.
var converters []*ddmConverter

func init() {
	converters = append(converters,
		newPasscodeConverter(),
		newSafariConverter(),
		newSoftwareUpdateConverter(),
	)
}

// findConverters returns all converters that handle the given payload type.
func findConverters(payloadType string) []*ddmConverter {
	var matches []*ddmConverter
	for _, c := range converters {
		if c.payloadTypes[payloadType] {
			matches = append(matches, c)
		}
	}
	return matches
}

// ConvertToDDMComponents parses a mobileconfig and converts compatible payloads
// to native DDM components. Payloads without a converter are wrapped in a
// com.jamf.ddm-configuration-profile component. When filterUnsupported is true,
// unsupported payload types without a DDM converter are removed.
func ConvertToDDMComponents(data []byte, filterUnsupported bool) (*DDMConversionResult, error) {
	var profile map[string]any
	if _, err := plist.Unmarshal(data, &profile); err != nil {
		return nil, fmt.Errorf("parsing mobileconfig: %w", err)
	}

	displayName, _ := profile["PayloadDisplayName"].(string)
	if displayName == "" {
		return nil, fmt.Errorf("mobileconfig has no PayloadDisplayName")
	}

	payloadContent, ok := profile["PayloadContent"].([]any)
	if !ok || len(payloadContent) == 0 {
		return nil, fmt.Errorf("mobileconfig has no PayloadContent array")
	}

	result := &DDMConversionResult{DisplayName: displayName}
	var profilePayloads []map[string]any
	seenComponents := make(map[string]bool)
	typeCount := make(map[string]int) // tracks per-type index for unique identifiers

	for i, item := range payloadContent {
		payload, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("PayloadContent[%d] is not a dictionary", i)
		}

		payloadType, _ := payload["PayloadType"].(string)
		if payloadType == "" {
			return nil, fmt.Errorf("PayloadContent[%d] has no PayloadType", i)
		}

		matched := findConverters(payloadType)
		if len(matched) == 0 {
			// No DDM converter — standard profile wrapping path
			if !SupportedPayloadTypes[payloadType] {
				if filterUnsupported {
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("removed unsupported payload type %q", payloadType))
					continue
				}
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("payload type %q may not be supported — see https://github.com/apple/device-management/tree/release/mdm/profiles", payloadType))
			}
			idx := typeCount[payloadType]
			typeCount[payloadType]++
			profilePayloads = append(profilePayloads, buildPayloadEntry(payloadType, payload, idx))
			continue
		}

		// DDM conversion path: run each matching converter sequentially
		remaining := extractSettingsKeys(payload)

		for _, conv := range matched {
			if seenComponents[conv.componentID] {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("skipping duplicate %s conversion (already produced from an earlier payload)", conv.componentID))
				continue
			}

			config, leftover, warnings, err := conv.convert(remaining)
			result.Warnings = append(result.Warnings, warnings...)

			if err != nil {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("DDM conversion failed for %s: %v — skipping this converter", conv.componentID, err))
				continue
			}

			if config != nil {
				seenComponents[conv.componentID] = true
				result.NativeComponents = append(result.NativeComponents, DDMComponent{
					Identifier:    conv.componentID,
					Configuration: config,
				})
				result.Conversions = append(result.Conversions,
					fmt.Sprintf("%s -> %s", payloadType, conv.componentID))
			}

			remaining = leftover
		}

		// Remaining keys go into the configuration-profile wrapper
		if len(remaining) > 0 {
			idx := typeCount[payloadType]
			typeCount[payloadType]++
			entry := map[string]any{
				"payloadType":       payloadType,
				"payloadIdentifier": generatePayloadIdentifier(payloadType, idx),
			}
			for k, v := range remaining {
				entry[k] = v
			}
			profilePayloads = append(profilePayloads, entry)
		}
	}

	// Package leftover payloads as a DDMProfileDto configuration
	if len(profilePayloads) > 0 {
		config := map[string]any{
			"payloadDisplayName": displayName,
			"payloadContent":     profilePayloads,
		}
		raw, err := marshalConfig(config)
		if err != nil {
			return nil, fmt.Errorf("marshalling remaining profile config: %w", err)
		}
		result.ProfileConfig = raw
	}

	if len(result.NativeComponents) == 0 && result.ProfileConfig == nil {
		return nil, fmt.Errorf("no payloads remain after filtering")
	}

	return result, nil
}

// extractSettingsKeys returns preference domain keys from a payload, stripping
// Apple metadata, converting plist values, and removing empty values.
func extractSettingsKeys(payload map[string]any) map[string]any {
	settings := make(map[string]any)
	for k, v := range payload {
		if appleMetadataKeys[k] {
			continue
		}
		converted := convertPlistValue(v)
		if isEmptyValue(converted) {
			continue
		}
		settings[k] = converted
	}
	return settings
}

// wrapIncludedValue wraps a value in the Jamf DDM component format:
// {"Included": true, "Value": <value>}.
func wrapIncludedValue(value any) map[string]any {
	return map[string]any{"Included": true, "Value": value}
}

// toFloat64 coerces numeric types to float64 for comparison and conversion.
func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}

// getIntValue extracts an integer from a map, falling back to defaultVal.
func getIntValue(m map[string]any, key string, defaultVal int) int {
	if v, ok := m[key]; ok {
		if f, ok := toFloat64(v); ok {
			return int(f)
		}
	}
	return defaultVal
}
