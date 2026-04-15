// Copyright 2026, Jamf Software LLC

package profileconvert

import (
	"encoding/json"
)

// rsrKeys are the Rapid Security Response keys this converter claims from
// com.apple.applicationaccess.
//
// Source: https://github.com/apple/device-management/blob/release/mdm/profiles/com.apple.applicationaccess.yaml
// Target: https://github.com/apple/device-management/blob/release/declarative/declarations/configurations/softwareupdate.settings.yaml
var rsrKeys = map[string]bool{
	"allowRapidSecurityResponseInstallation": true,
	"allowRapidSecurityResponseRemoval":      true,
}

func newRSRConverter() *ddmConverter {
	return &ddmConverter{
		componentID:  "com.jamf.ddm.software-update-settings",
		payloadTypes: map[string]bool{"com.apple.applicationaccess": true},
		convert:      convertRSR,
	}
}

// convertRSR extracts Rapid Security Response keys from a
// com.apple.applicationaccess payload and converts them to the
// RapidSecurityResponse section of com.jamf.ddm.software-update-settings.
// Non-RSR keys are returned as remaining for other converters or the
// profile wrapper.
func convertRSR(settings map[string]any) (json.RawMessage, map[string]any, []string, error) {
	remaining := make(map[string]any)

	var rsrInstall, rsrRemoval any
	hasInstall, hasRemoval := false, false

	for key, value := range settings {
		if rsrKeys[key] {
			switch key {
			case "allowRapidSecurityResponseInstallation":
				rsrInstall = value
				hasInstall = true
			case "allowRapidSecurityResponseRemoval":
				rsrRemoval = value
				hasRemoval = true
			}
		} else {
			remaining[key] = value
		}
	}

	if !hasInstall && !hasRemoval {
		return nil, settings, nil, nil
	}

	rsr := make(map[string]any)

	if hasInstall {
		b, ok := rsrInstall.(bool)
		if ok {
			rsr["Enable"] = map[string]any{"Enabled": b, "Included": true}
		}
	}

	if hasRemoval {
		b, ok := rsrRemoval.(bool)
		if ok {
			rsr["EnableRollback"] = map[string]any{"Enabled": b, "Included": true}
		}
	}

	if len(rsr) == 0 {
		return nil, settings, nil, nil
	}

	config := map[string]any{"RapidSecurityResponse": rsr}

	raw, err := marshalConfig(config)
	if err != nil {
		return nil, settings, nil, err
	}

	if len(remaining) == 0 {
		remaining = nil
	}
	return raw, remaining, nil, nil
}
