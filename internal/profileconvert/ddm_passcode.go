// Copyright 2026, Jamf Software LLC

package profileconvert

import (
	"encoding/json"
	"fmt"
)

// passcodeKeyMapping defines how a legacy passcode key maps to a DDM key.
type passcodeKeyMapping struct {
	DDMKey string
	Invert bool // true → negate the boolean value
}

// passcodeKeyMap maps legacy com.apple.mobiledevice.passwordpolicy keys to
// their com.apple.configuration.passcode.settings equivalents (wrapped by
// com.jamf.ddm.passcode-settings).
//
// Source: https://github.com/apple/device-management/blob/release/mdm/profiles/com.apple.mobiledevice.passwordpolicy.yaml
// Target: https://github.com/apple/device-management/blob/release/declarative/declarations/configurations/passcode.settings.yaml
var passcodeKeyMap = map[string]passcodeKeyMapping{
	"forcePIN":                     {DDMKey: "RequirePasscode"},
	"allowSimple":                  {DDMKey: "RequireComplexPasscode", Invert: true},
	"requireAlphanumeric":          {DDMKey: "RequireAlphanumericPasscode"},
	"minLength":                    {DDMKey: "MinimumLength"},
	"minComplexChars":              {DDMKey: "MinimumComplexCharacters"},
	"maxFailedAttempts":            {DDMKey: "MaximumFailedAttempts"},
	"maxInactivity":                {DDMKey: "MaximumInactivityInMinutes"},
	"maxPINAgeInDays":              {DDMKey: "MaximumPasscodeAgeInDays"},
	"maxGracePeriod":               {DDMKey: "MaximumGracePeriodInMinutes"},
	"pinHistory":                   {DDMKey: "PasscodeReuseLimit"},
	"minutesUntilFailedLoginReset": {DDMKey: "FailedAttemptsResetInMinutes"},
	"changeAtNextAuth":             {DDMKey: "ChangeAtNextAuth"},
}

func newPasscodeConverter() *ddmConverter {
	return &ddmConverter{
		componentID:  "com.jamf.ddm.passcode-settings",
		payloadTypes: map[string]bool{"com.apple.mobiledevice.passwordpolicy": true},
		convert:      convertPasscode,
	}
}

// convertPasscode converts a legacy passcode policy payload to a native
// com.jamf.ddm.passcode-settings component configuration. All keys are
// consumed — the entire payload is replaced by the DDM component.
func convertPasscode(settings map[string]any) (json.RawMessage, map[string]any, []string, error) {
	config := make(map[string]any)
	var warnings []string
	converted := 0

	for key, value := range settings {
		// Handle customRegex: nested dict with different key names in DDM
		if key == "customRegex" {
			dict, ok := value.(map[string]any)
			if !ok {
				warnings = append(warnings, "passcode customRegex is not a dictionary — skipped")
				continue
			}
			cr := map[string]any{"Included": true}
			if regex, ok := dict["passwordContentRegex"]; ok {
				cr["Regex"] = regex
			}
			if desc, ok := dict["passwordContentDescription"]; ok {
				cr["Description"] = desc
			}
			config["CustomRegex"] = cr
			converted++
			continue
		}

		mapping, ok := passcodeKeyMap[key]
		if !ok {
			warnings = append(warnings, fmt.Sprintf("passcode key %q has no DDM mapping — skipped", key))
			continue
		}

		ddmValue := value
		if mapping.Invert {
			if b, ok := value.(bool); ok {
				ddmValue = !b
			}
		}

		config[mapping.DDMKey] = wrapIncludedValue(ddmValue)
		converted++
	}

	if converted == 0 {
		return nil, settings, warnings, nil
	}

	// The passcode-settings component requires a version field
	config["version"] = "2"

	raw, err := marshalConfig(config)
	if err != nil {
		return nil, settings, warnings, err
	}
	return raw, nil, warnings, nil
}
