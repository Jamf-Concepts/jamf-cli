// Copyright 2026, Jamf Software LLC

package profileconvert

import (
	"encoding/json"
	"fmt"
	"maps"

	"github.com/Jamf-Concepts/jamf-cli/internal/blueprintcomponents"
)

// softwareUpdateDeferralKeys are the keys this converter claims from
// com.apple.applicationaccess. All relate to software update deferral
// policies that map to the DDM software-update-settings Deferrals section.
//
// Source: https://github.com/apple/device-management/blob/release/mdm/profiles/com.apple.applicationaccess.yaml
// Target: https://github.com/apple/device-management/blob/release/declarative/declarations/configurations/softwareupdate.settings.yaml
var softwareUpdateDeferralKeys = map[string]bool{
	"forceDelayedSoftwareUpdates":                       true,
	"forceDelayedMajorSoftwareUpdates":                  true,
	"forceDelayedAppSoftwareUpdates":                    true,
	"enforcedSoftwareUpdateDelay":                       true,
	"enforcedSoftwareUpdateMajorOSDeferredInstallDelay": true,
	"enforcedSoftwareUpdateMinorOSDeferredInstallDelay": true,
	"enforcedSoftwareUpdateNonOSDeferredInstallDelay":   true,
}

func newSoftwareUpdateConverter() *ddmConverter {
	return &ddmConverter{
		componentID:  "com.jamf.ddm.software-update-settings",
		payloadTypes: map[string]bool{"com.apple.applicationaccess": true},
		convert:      convertSoftwareUpdate,
	}
}

// convertSoftwareUpdate extracts software update deferral keys from a
// com.apple.applicationaccess payload and converts them to
// com.jamf.ddm.software-update-settings Deferrals. Only active deferrals
// (forceDelayed* = true) produce DDM configuration. Non-deferral keys
// are returned as remaining for the profile wrapper.
func convertSoftwareUpdate(settings map[string]any) (json.RawMessage, map[string]any, []string, error) {
	remaining := make(map[string]any)
	var warnings []string

	// Separate deferral keys from the rest
	deferralKeys := make(map[string]any)
	for key, value := range settings {
		if softwareUpdateDeferralKeys[key] {
			deferralKeys[key] = value
		} else {
			remaining[key] = value
		}
	}

	if len(deferralKeys) == 0 {
		return nil, settings, nil, nil
	}

	// Only convert when deferrals are actually enabled
	forceDelayed, _ := deferralKeys["forceDelayedSoftwareUpdates"].(bool)
	forceMajor, _ := deferralKeys["forceDelayedMajorSoftwareUpdates"].(bool)
	forceApp, _ := deferralKeys["forceDelayedAppSoftwareUpdates"].(bool)

	if !forceDelayed && !forceMajor && !forceApp {
		// All force flags are false — nothing active to convert.
		// Return keys to remaining so they stay in the profile wrapper.
		maps.Copy(remaining, deferralKeys)
		return nil, remaining, nil, nil
	}

	deferrals := make(map[string]any)

	if forceDelayed {
		// Use the minor-specific delay, falling back to the shared delay, then 30
		delay := getIntValue(deferralKeys, "enforcedSoftwareUpdateMinorOSDeferredInstallDelay",
			getIntValue(deferralKeys, "enforcedSoftwareUpdateDelay", 30))

		// CombinedPeriodInDays covers iOS/tvOS/visionOS; MinorPeriodInDays covers macOS
		deferrals["CombinedPeriodInDays"] = wrapIncludedValue(delay)
		deferrals["MinorPeriodInDays"] = wrapIncludedValue(delay)
	}

	if forceMajor {
		delay := getIntValue(deferralKeys, "enforcedSoftwareUpdateMajorOSDeferredInstallDelay", 30)
		deferrals["MajorPeriodInDays"] = wrapIncludedValue(delay)
	}

	if forceApp {
		// App update deferrals have no direct DDM equivalent — keep in profile wrapper
		warnings = append(warnings,
			"forceDelayedAppSoftwareUpdates has no DDM equivalent — kept in profile wrapper")
		remaining["forceDelayedAppSoftwareUpdates"] = true
		if v, ok := deferralKeys["enforcedSoftwareUpdateNonOSDeferredInstallDelay"]; ok {
			remaining["enforcedSoftwareUpdateNonOSDeferredInstallDelay"] = v
		}
		// The shared delay may be needed by the app deferral in the profile wrapper
		if v, ok := deferralKeys["enforcedSoftwareUpdateDelay"]; ok {
			remaining["enforcedSoftwareUpdateDelay"] = v
		}
	}

	if len(deferrals) == 0 {
		return nil, remaining, warnings, nil
	}

	// Emit ONLY the Deferrals section (kept complete, from the scaffold, with the
	// values we set marked Included:true and the rest Included:false). Emitting the
	// whole scaffold here — every section present with Included:false — would
	// clobber sections another converter set to Included:true (e.g. AutomaticActions
	// from com.apple.SoftwareUpdate) when the two configs are deep-merged into one
	// component. ensureFullSoftwareUpdateSchema backfills the remaining top-level
	// sections after all converters run.
	base, err := softwareUpdateBaseConfig()
	if err != nil {
		return nil, settings, warnings, fmt.Errorf("loading software-update scaffold: %w", err)
	}
	baseDeferrals, ok := base["Deferrals"].(map[string]any)
	if !ok {
		return nil, settings, warnings, fmt.Errorf("software-update scaffold missing Deferrals section")
	}
	config := map[string]any{"Deferrals": mergeDeferrals(baseDeferrals, deferrals)}

	raw, err := marshalConfig(config)
	if err != nil {
		return nil, settings, warnings, err
	}

	if len(remaining) == 0 {
		remaining = nil
	}
	return raw, remaining, warnings, nil
}

// softwareUpdateBaseConfig returns the full component schema from
// blueprintcomponents.Scaffolds. The Jamf UI requires every section to be
// present — omitting sections causes the panel to render blank. All Included
// flags are set to false so non-converted sections are visible but not
// actively managed.
func softwareUpdateBaseConfig() (map[string]any, error) {
	raw := blueprintcomponents.Scaffolds["com.jamf.ddm.software-update-settings"]
	if raw == "" {
		return nil, fmt.Errorf("scaffold not found for com.jamf.ddm.software-update-settings")
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return nil, fmt.Errorf("parsing software-update scaffold: %w", err)
	}
	clearIncluded(config)
	// The scaffold contains placeholder values (empty strings, example arrays)
	// that fail API validation. Sanitize Beta to its minimal valid form.
	config["Beta"] = map[string]any{
		"Included": false,
		"Value":    map[string]any{"ProgramEnrollment": "Allowed"},
	}
	return config, nil
}

// clearIncluded recursively sets all "Included" fields to false in a
// component configuration map.
func clearIncluded(m map[string]any) {
	for k, v := range m {
		if k == "Included" {
			m[k] = false
			continue
		}
		if sub, ok := v.(map[string]any); ok {
			clearIncluded(sub)
		}
	}
}

// mergeDeferrals overlays converted deferral values onto the base defaults.
func mergeDeferrals(base, converted map[string]any) map[string]any {
	maps.Copy(base, converted)
	return base
}
