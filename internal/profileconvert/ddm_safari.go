// Copyright 2026, Jamf Software LLC

package profileconvert

import (
	"encoding/json"
	"fmt"
)

// safariKeyMapping defines how a legacy Safari restriction key maps to a DDM key.
type safariKeyMapping struct {
	DDMKey    string
	Invert    bool
	Transform func(any) any // custom value transformation (overrides Invert)
}

// safariKeyMap maps Safari-specific keys from the legacy
// com.apple.applicationaccess payload to their
// com.apple.configuration.safari.settings equivalents (wrapped by
// com.jamf.ddm.safari-settings).
//
// Source: https://github.com/apple/device-management/blob/release/mdm/profiles/com.apple.applicationaccess.yaml
// Target: https://github.com/apple/device-management/blob/release/declarative/declarations/configurations/safari.settings.yaml
var safariKeyMap = map[string]safariKeyMapping{
	"safariAcceptCookies":        {DDMKey: "AcceptCookies", Transform: convertCookiePolicy},
	"safariForceFraudWarning":    {DDMKey: "AllowDisablingFraudWarning", Invert: true},
	"safariAllowPopups":          {DDMKey: "AllowPopups"},
	"safariAllowJavaScript":      {DDMKey: "AllowJavaScript"},
	"allowSafariPrivateBrowsing": {DDMKey: "AllowPrivateBrowsing"},
	"allowSafariHistoryClearing": {DDMKey: "AllowHistoryClearing"},
	"allowSafariSummary":         {DDMKey: "AllowSummary"},
}

// safariKeysNoDDM are Safari-related keys that have no DDM equivalent.
// They stay in the profile wrapper but we warn about them.
var safariKeysNoDDM = map[string]bool{
	"safariAllowAutoFill": true,
	"allowSafari":         true,
}

func newSafariConverter() *ddmConverter {
	return &ddmConverter{
		componentID:  "com.jamf.ddm.safari-settings",
		payloadTypes: map[string]bool{"com.apple.applicationaccess": true},
		convert:      convertSafari,
	}
}

// convertSafari extracts Safari-specific keys from a com.apple.applicationaccess
// payload and converts them to a com.jamf.ddm.safari-settings component.
// Non-Safari keys are returned as remaining for the profile wrapper.
func convertSafari(settings map[string]any) (json.RawMessage, map[string]any, []string, error) {
	config := make(map[string]any)
	remaining := make(map[string]any)
	var warnings []string
	converted := 0

	for key, value := range settings {
		mapping, hasDDM := safariKeyMap[key]

		if hasDDM {
			var ddmValue any
			if mapping.Transform != nil {
				ddmValue = mapping.Transform(value)
			} else if mapping.Invert {
				if b, ok := value.(bool); ok {
					ddmValue = !b
				} else {
					ddmValue = value
				}
			} else {
				ddmValue = value
			}
			config[mapping.DDMKey] = wrapIncludedValue(ddmValue)
			converted++
		} else if safariKeysNoDDM[key] {
			remaining[key] = value
			warnings = append(warnings,
				fmt.Sprintf("%q has no DDM safari equivalent — kept in profile wrapper", key))
		} else {
			remaining[key] = value
		}
	}

	if converted == 0 {
		return nil, settings, warnings, nil
	}

	warnings = append(warnings,
		"safari-settings DDM component requires macOS 26+ / iOS 26+ — verify target devices are compatible")

	raw, err := marshalConfig(config)
	if err != nil {
		return nil, settings, warnings, err
	}

	if len(remaining) == 0 {
		remaining = nil
	}
	return raw, remaining, warnings, nil
}

// convertCookiePolicy maps the legacy numeric safariAcceptCookies value to the
// DDM string enum. Legacy values: 0=block all, 1/1.5=prevent cross-site,
// 2=allow all (default).
func convertCookiePolicy(v any) any {
	f, ok := toFloat64(v)
	if !ok {
		return "Always"
	}
	switch {
	case f < 0.5:
		return "Never"
	case f < 1.25:
		return "CurrentWebsite"
	case f < 1.75:
		return "VisitedWebsites"
	default:
		return "Always"
	}
}
