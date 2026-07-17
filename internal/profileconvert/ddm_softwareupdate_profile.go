// Copyright 2026, Jamf Software LLC

package profileconvert

import (
	"encoding/json"
	"fmt"
)

// softwareUpdateProfileKeys maps keys from the legacy com.apple.SoftwareUpdate
// payload to their DDM software-update-settings equivalents. Keys not in this
// map have no DDM equivalent and stay in the profile wrapper.
//
// Source: https://github.com/apple/device-management/blob/release/mdm/profiles/com.apple.SoftwareUpdate.yaml
// Target: https://github.com/apple/device-management/blob/release/declarative/declarations/configurations/softwareupdate.settings.yaml
var softwareUpdateProfileKeys = map[string]bool{
	"restrict-software-update-require-admin-to-install": true,
	"AutomaticDownload":                true,
	"AutomaticallyInstallMacOSUpdates": true,
	"CriticalUpdateInstall":            true,
	"AllowPreReleaseInstallation":      true,
}

// softwareUpdateProfileKeysNoDDM are com.apple.SoftwareUpdate keys with no
// DDM equivalent. They stay in the profile wrapper but we warn about them.
var softwareUpdateProfileKeysNoDDM = map[string]bool{
	"AutomaticCheckEnabled":          true,
	"AutomaticallyInstallAppUpdates": true,
	"ConfigDataInstall":              true,
	"CatalogURL":                     true,
}

func newSoftwareUpdateProfileConverter() *ddmConverter {
	return &ddmConverter{
		componentID:  "com.jamf.ddm.software-update-settings",
		payloadTypes: map[string]bool{"com.apple.SoftwareUpdate": true},
		convert:      convertSoftwareUpdateProfile,
	}
}

// convertSoftwareUpdateProfile converts a legacy com.apple.SoftwareUpdate
// payload to a native com.jamf.ddm.software-update-settings component.
// Keys without a DDM equivalent are returned as remaining for the profile wrapper.
func convertSoftwareUpdateProfile(settings map[string]any) (json.RawMessage, map[string]any, []string, error) {
	remaining := make(map[string]any)
	var warnings []string
	converted := 0

	// Build only the sections we modify. Every converter targeting
	// software-update-settings emits just its own disjoint sections, so they
	// deep-merge without clobbering each other; ensureFullSoftwareUpdateSchema
	// backfills the remaining scaffold sections after all converters run.
	config := make(map[string]any)

	for key, value := range settings {
		if !softwareUpdateProfileKeys[key] {
			if softwareUpdateProfileKeysNoDDM[key] {
				warnings = append(warnings,
					fmt.Sprintf("%q has no DDM software-update equivalent — kept in profile wrapper", key))
			}
			remaining[key] = value
			continue
		}

		switch key {
		case "restrict-software-update-require-admin-to-install":
			// Inverted: restrict=false means standard users CAN install → Enabled: true
			b, ok := value.(bool)
			if !ok {
				remaining[key] = value
				continue
			}
			config["AllowStandardUserOSUpdates"] = map[string]any{
				"Enabled":  !b,
				"Included": true,
			}
			converted++

		case "AutomaticDownload":
			s := boolToAllowed(value)
			if s == "" {
				remaining[key] = value
				continue
			}
			setAutoAction(config, "Download", s)
			converted++

		case "AutomaticallyInstallMacOSUpdates":
			s := boolToAllowed(value)
			if s == "" {
				remaining[key] = value
				continue
			}
			setAutoAction(config, "InstallOSUpdates", s)
			converted++

		case "CriticalUpdateInstall":
			// Critical updates (XProtect, MRT) map to InstallSecurityUpdate
			s := boolToAllowed(value)
			if s == "" {
				remaining[key] = value
				continue
			}
			setAutoAction(config, "InstallSecurityUpdate", s)
			converted++

		case "AllowPreReleaseInstallation":
			b, ok := value.(bool)
			if !ok {
				remaining[key] = value
				continue
			}
			enrollment := "AlwaysOff"
			if b {
				enrollment = "Allowed"
			}
			config["Beta"] = map[string]any{
				"Included": true,
				"Value":    map[string]any{"ProgramEnrollment": enrollment},
			}
			converted++
		}
	}

	if converted == 0 {
		return nil, settings, warnings, nil
	}

	raw, err := marshalConfig(config)
	if err != nil {
		return nil, settings, warnings, err
	}

	if len(remaining) == 0 {
		remaining = nil
	}
	return raw, remaining, warnings, nil
}

// boolToAllowed converts a bool to the DDM "Allowed"/"AlwaysOff" enum.
// The API accepts: Allowed, AlwaysOn, AlwaysOff.
// Returns "" if the value is not a bool.
func boolToAllowed(v any) string {
	b, ok := v.(bool)
	if !ok {
		return ""
	}
	if b {
		return "Allowed"
	}
	return "AlwaysOff"
}

// setAutoAction sets a single AutomaticActions sub-key on the config.
func setAutoAction(config map[string]any, key, value string) {
	actions, _ := config["AutomaticActions"].(map[string]any)
	if actions == nil {
		actions = make(map[string]any)
	}
	actions[key] = map[string]any{"Included": true, "Value": value}
	config["AutomaticActions"] = actions
}
