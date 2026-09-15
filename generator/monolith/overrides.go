// Copyright 2026, Jamf Software LLC

package monolith

// AppInstallerSubtree is the path subtree ExtractSubtree derives the App
// Installer spec from, and AppInstallerSpecs routes it into one file.
//
// One file, because a filename no longer names anything. It used to be four:
// one tag covers all 23 operations upstream, so tag routing could not separate
// the families and the four filenames were how `app-installers`,
// `app-installer-titles`, `app-installer-deployments` and
// `app-installer-global-settings` got their names. Resource identity now comes
// from the paths, which separate the four families on their own — and better,
// since `/v1/app-installers/titles` says so more plainly than a filename does.
//
// This subtree is here at all because App Installers sits under hiddenapi/ in
// jamf/jss: no consolidated /api/schema/ document carries it, so the gateway's
// published Pro API spec is the only thing that describes it.
const AppInstallerSubtree = "/v1/app-installers"

var AppInstallerSpecs = []SubtreeSpec{
	{
		Prefix:      "/v1/app-installers",
		Filename:    "AppInstallers.yaml",
		Title:       "Jamf Pro API - App Installers",
		Description: "App Installers: the feature probe, the Jamf App Catalog of available titles and their versions, deployments to computers with their per-computer installation state, and the global settings controlling end-user notifications and deployment process controls.",
	},
}
