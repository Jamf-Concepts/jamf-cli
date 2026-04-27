// Copyright 2026, Jamf Software LLC

package blueprintcomponents

import (
	"encoding/json"
	"sort"

	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/blueprints"
)

// Scaffolds maps blueprint component identifiers to example JSON configurations.
// Populated from SDK-typed structs at init time; components without SDK types are
// omitted and will be added when the SDK gains support.
var Scaffolds map[string]string

// ShortNames maps short CLI names to full component identifiers.
var ShortNames = map[string]string{
	// SDK-typed
	"ddm-configuration-profile": "com.jamf.ddm-configuration-profile",
	"audio-accessory-settings":  "com.jamf.ddm.audio-accessory-settings",
	"disk-management":           "com.jamf.ddm.disk-management",
	"math-settings":             "com.jamf.ddm.math-settings",
	"passcode-settings":         "com.jamf.ddm.passcode-settings",
	"safari-bookmarks":          "com.jamf.ddm.safari-bookmarks",
	"safari-extensions":         "com.jamf.ddm.safari-extensions",
	"safari-settings":           "com.jamf.ddm.safari-settings",
	"software-update-settings":  "com.jamf.ddm.software-update-settings",
	"sw-updates":                "com.jamf.ddm.sw-updates",
	// Raw JSON fallback — migrate to SDK types when blueprints package gains support
	"app-managed":                 "com.jamf.ddm.app-managed",
	"custom-declarations":         "com.jamf.ddm.custom-declarations",
	"service-background-tasks":    "com.jamf.ddm.service-background-tasks",
	"service-configuration-files": "com.jamf.ddm.service-configuration-files",
}

// Identifiers returns all known component identifiers in sorted order.
func Identifiers() []string {
	ids := make([]string, 0, len(Scaffolds))
	for id := range Scaffolds {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func init() {
	examples := map[string]any{
		"com.jamf.ddm-configuration-profile":    exampleConfigurationProfile(),
		"com.jamf.ddm.audio-accessory-settings": exampleAudioAccessorySettings(),
		"com.jamf.ddm.disk-management":          exampleDiskManagement(),
		"com.jamf.ddm.math-settings":            exampleMathSettings(),
		"com.jamf.ddm.passcode-settings":        examplePasscodeSettings(),
		"com.jamf.ddm.safari-bookmarks":         exampleSafariBookmarks(),
		"com.jamf.ddm.safari-extensions":        exampleSafariExtensions(),
		"com.jamf.ddm.safari-settings":          exampleSafariSettings(),
		"com.jamf.ddm.software-update-settings": exampleSoftwareUpdateSettings(),
		"com.jamf.ddm.sw-updates":               exampleSwUpdates(),
	}
	Scaffolds = make(map[string]string, len(examples)+len(rawScaffolds))
	for id, v := range examples {
		b, _ := json.MarshalIndent(v, "", "  ")
		Scaffolds[id] = string(b)
	}
	// Raw JSON fallback for components not yet typed in the SDK.
	// Remove each entry once the blueprints package gains the corresponding type.
	for id, v := range rawScaffolds {
		Scaffolds[id] = v
	}
}

// rawScaffolds holds example configurations for components not yet represented
// in the jamfplatform-go-sdk blueprints package.
var rawScaffolds = map[string]string{
	"com.jamf.ddm.app-managed": `{}`,
	"com.jamf.ddm.custom-declarations": `{
  "declarations": [
    {
      "kind": "CONFIGURATION",
      "payload": {},
      "type": ""
    }
  ]
}`,
	"com.jamf.ddm.service-background-tasks": `{
  "backgroundTasks": [
    {
      "ExecutableAssetReference": {
        "Authentication": {"Type": "MDM"},
        "Reference": {"ContentType": "", "DataURL": "", "Hash-SHA-256": "", "Size": 0}
      },
      "LaunchdConfigurations": [
        {
          "Context": "daemon",
          "FileAssetReference": {
            "Authentication": {"Type": "MDM"},
            "Reference": {"ContentType": "", "DataURL": "", "Hash-SHA-256": "", "Size": 0}
          }
        }
      ],
      "TaskDescription": "",
      "TaskType": ""
    }
  ]
}`,
	"com.jamf.ddm.service-configuration-files": `{
  "serviceConfigFiles": [
    {
      "DataAssetReference": {
        "Authentication": {"Type": "MDM"},
        "Reference": {"ContentType": "", "DataURL": "", "Hash-SHA-256": "", "Size": 0}
      },
      "ServiceType": ""
    }
  ]
}`,
}

func boolPtr(b bool) *bool    { return &b }
func intPtr(i int) *int       { return &i }
func strPtr(s string) *string { return &s }

func exampleConfigurationProfile() blueprints.ConfigurationProfileConfiguration {
	return blueprints.ConfigurationProfileConfiguration{
		PayloadDisplayName: "Example Profile",
		PayloadContent: []json.RawMessage{
			json.RawMessage(`{"payloadType":"com.apple.domains","WebDomains":["*.example.com"]}`),
		},
	}
}

func exampleAudioAccessorySettings() blueprints.AudioAccessorySettingsConfiguration {
	return blueprints.AudioAccessorySettingsConfiguration{
		TemporaryPairing: &blueprints.TemporaryPairing{
			Included: boolPtr(true),
			Disabled: boolPtr(false),
			Configuration: &blueprints.TemporaryPairingConfig{
				UnpairingTime: blueprints.UnpairingTime{
					Policy: "None",
					Hour:   intPtr(0),
				},
			},
		},
	}
}

func exampleDiskManagement() blueprints.DiskManagementSettingsConfiguration {
	return blueprints.DiskManagementSettingsConfiguration{
		Version: 2,
		Restrictions: &blueprints.Restrictions{
			ExternalStorage: &blueprints.StorageMode{Included: boolPtr(true), Value: "Allowed"},
			NetworkStorage:  &blueprints.StorageMode{Included: boolPtr(true), Value: "Allowed"},
		},
	}
}

func exampleMathSettings() blueprints.MathSettingsConfiguration {
	return blueprints.MathSettingsConfiguration{
		Calculator: &blueprints.Calculator{
			BasicMode:      &blueprints.BasicMode{Included: boolPtr(true), AddSquareRoot: false},
			InputModes:     &blueprints.InputModes{Included: boolPtr(true), RPN: false, UnitConversion: false},
			MathNotesMode:  &blueprints.MathNotesMode{Included: boolPtr(true), Enabled: false},
			ProgrammerMode: &blueprints.ProgrammerMode{Included: boolPtr(true), Enabled: false},
			ScientificMode: &blueprints.ScientificMode{Included: boolPtr(true), Enabled: false},
		},
		SystemBehavior: &blueprints.SystemBehavior{
			Included:            boolPtr(true),
			KeyboardSuggestions: false,
			MathNotes:           false,
		},
	}
}

func examplePasscodeSettings() blueprints.PasscodeSettingsConfiguration {
	return blueprints.PasscodeSettingsConfiguration{
		Version:                      2,
		ChangeAtNextAuth:             &blueprints.ChangeAtNextAuth{Included: boolPtr(false), Value: boolPtr(false)},
		CustomRegex:                  &blueprints.CustomRegex{Included: boolPtr(false), Regex: strPtr("[0-9]{16}"), Description: &map[string]string{"<key>": ""}},
		FailedAttemptsResetInMinutes: &blueprints.FailedAttemptsResetInMinutes{Included: boolPtr(false), Value: intPtr(5)},
		MaximumFailedAttempts:        &blueprints.MaximumFailedAttempts{Included: boolPtr(false), Value: intPtr(10)},
		MaximumGracePeriodInMinutes:  &blueprints.MaximumGracePeriodInMinutes{Included: boolPtr(false), Value: intPtr(2)},
		MaximumInactivityInMinutes:   &blueprints.MaximumInactivityInMinutes{Included: boolPtr(false), Value: intPtr(10)},
		MaximumPasscodeAgeInDays:     &blueprints.MaximumPasscodeAgeInDays{Included: boolPtr(false), Value: intPtr(14)},
		MinimumComplexCharacters:     &blueprints.MinimumComplexCharacters{Included: boolPtr(false), Value: intPtr(1)},
		MinimumLength:                &blueprints.MinimumLength{Included: boolPtr(false), Value: intPtr(8)},
		PasscodeReuseLimit:           &blueprints.PasscodeReuseLimit{Included: boolPtr(false), Value: intPtr(10)},
		RequireAlphanumericPasscode:  &blueprints.RequireAlphanumericPasscode{Included: boolPtr(false), Value: boolPtr(false)},
		RequireComplexPasscode:       &blueprints.RequireComplexPasscode{Included: boolPtr(false), Value: boolPtr(false)},
		RequirePasscode:              &blueprints.RequirePasscode{Included: boolPtr(false), Value: boolPtr(false)},
	}
}

func exampleSafariBookmarks() blueprints.SafariBookmarksConfiguration {
	return blueprints.SafariBookmarksConfiguration{
		ManagedBookmarks: []blueprints.BookmarkGroup{
			{
				GroupIdentifier: "group-1",
				Title:           "Example Group",
				Bookmarks: []blueprints.BookmarkItem{
					{Type: "BOOKMARK", BOOKMARK: &blueprints.URLBookmarkItem{Type: "BOOKMARK", Title: "Example", URL: "https://example.com"}},
				},
			},
		},
	}
}

func exampleSafariExtensions() blueprints.SafariExtensionsConfiguration {
	return blueprints.SafariExtensionsConfiguration{
		ManagedExtensions: map[string]blueprints.ManagedExtension{
			"<extension-bundle-id>": {
				State:           strPtr("Allowed"),
				PrivateBrowsing: strPtr("Allowed"),
				AllowedDomains:  &[]blueprints.ManagedExtensionDomain{{Domain: "example.com"}},
				DeniedDomains:   &[]blueprints.ManagedExtensionDomain{{Domain: ""}},
			},
		},
	}
}

func exampleSafariSettings() blueprints.SafariSettingsConfiguration {
	return blueprints.SafariSettingsConfiguration{
		AcceptCookies:              &blueprints.AcceptCookies{Included: boolPtr(false), Value: strPtr("Never")},
		AllowDisablingFraudWarning: &blueprints.AllowDisablingFraudWarning{Included: boolPtr(false), Value: boolPtr(false)},
		AllowHistoryClearing:       &blueprints.AllowHistoryClearing{Included: boolPtr(false), Value: boolPtr(false)},
		AllowJavaScript:            &blueprints.AllowJavaScript{Included: boolPtr(false), Value: boolPtr(false)},
		AllowPopups:                &blueprints.AllowPopups{Included: boolPtr(false), Value: boolPtr(false)},
		AllowPrivateBrowsing:       &blueprints.AllowPrivateBrowsing{Included: boolPtr(false), Value: boolPtr(false)},
		AllowSummary:               &blueprints.AllowSummary{Included: boolPtr(false), Value: boolPtr(false)},
		NewTabStartPage: &blueprints.NewTabStartPage{
			Included:            boolPtr(false),
			PageType:            strPtr("Start"),
			HomepageURL:         strPtr("https://example.com"),
			ExtensionIdentifier: strPtr("com.example.extension (ABC1234567)"),
		},
	}
}

func exampleSoftwareUpdateSettings() blueprints.SoftwareUpdateSettingsConfiguration {
	return blueprints.SoftwareUpdateSettingsConfiguration{
		AllowStandardUserOSUpdates: &blueprints.OptionallyEnabled{Included: boolPtr(true), Enabled: false},
		AutomaticActions: &blueprints.AutomaticActions{
			Download:              &blueprints.AutomaticAction{Included: boolPtr(true), Value: "Allowed"},
			InstallOSUpdates:      &blueprints.AutomaticAction{Included: boolPtr(true), Value: "Allowed"},
			InstallSecurityUpdate: &blueprints.AutomaticAction{Included: boolPtr(true), Value: "Allowed"},
		},
		Beta: &blueprints.Beta{
			Included: boolPtr(true),
			Value:    &blueprints.BetaSettings{ProgramEnrollment: "Allowed"},
		},
		Deferrals: &blueprints.Deferrals{
			CombinedPeriodInDays: &blueprints.OptionalPeriodInDays{Included: boolPtr(true), Value: intPtr(1)},
			MajorPeriodInDays:    &blueprints.OptionalPeriodInDays{Included: boolPtr(true), Value: intPtr(1)},
			MinorPeriodInDays:    &blueprints.OptionalPeriodInDays{Included: boolPtr(true), Value: intPtr(1)},
			SystemPeriodInDays:   &blueprints.OptionalPeriodInDays{Included: boolPtr(true), Value: intPtr(1)},
		},
		Notifications: &blueprints.OptionallyEnabled{Included: boolPtr(true), Enabled: false},
		RapidSecurityResponse: &blueprints.RapidSecurityResponse{
			Enable:         &blueprints.OptionallyEnabled{Included: boolPtr(true), Enabled: false},
			EnableRollback: &blueprints.OptionallyEnabled{Included: boolPtr(true), Enabled: false},
		},
		RecommendedCadence: &blueprints.RecommendedCadence{Included: boolPtr(true), Value: "All"},
	}
}

func exampleSwUpdates() blueprints.SwUpdateLatestConfiguration {
	return blueprints.SwUpdateLatestConfiguration{
		EnforcementType:  "AUTOMATIC",
		Strategy:         "LATEST",
		DeploymentTime:   "16:00",
		EnforceAfterDays: 14,
		DetailsURL:       &blueprints.DetailsURL{Included: boolPtr(false), Value: strPtr("")},
	}
}
