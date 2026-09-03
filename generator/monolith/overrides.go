// Copyright 2026, Jamf Software LLC

package monolith

// TagFilenameOverrides maps an OpenAPI tag to the spec filename that should
// be produced for it when a matching path is NOT already present in the
// existing specs/ layout. This only fires for **new** tags/paths that the
// path-based layout cannot place automatically.
//
// Keyed by tag name (kebab form, as it appears in the monolith spec).
// Value is the basename (with .yaml extension) that the splitter should write.
//
// Leave empty until an upstream rename or genuinely new tag requires an
// explicit mapping. Most tags map cleanly to a PascalCase-singular filename
// derived from the tag itself.
var TagFilenameOverrides = map[string]string{
	// Example:
	// "some-renamed-tag": "PreservedOldName.yaml",
}

// DroppedTags lists tags whose paths must never be emitted, even if present
// in the monolith. Used for legacy/preview endpoints that would collide with
// canonical resources.
var DroppedTags = map[string]bool{
	// Upstream ships a stripped-down preview endpoint at /devices/extensionAttributes
	// that only returns names. The real CRUD lives under
	// /v1/mobile-device-extension-attributes and is tagged separately. Mirrors
	// the legacy preview drop in the Makefile sync-specs target.
	"mobile-device-extension-attributes-preview": true,
	// "devices" owns only /v1/devices/{id}/groups — a sub-lookup endpoint with
	// no sibling CRUD on the base collection. Emitting it produces a lonely
	// `pro devices groups <id>` command with no context. Membership is already
	// reachable via the canonical computer-groups / mobile-device-groups
	// resources, so drop the tag rather than surface a half-resource.
	"devices": true,
	// "user-session-preview" covers two legacy session-token endpoints
	// (GET /user, POST /user/updateSession) unrelated to the canonical
	// user-sessions resource (/v1/user-sessions/*). Keeping them forces a
	// mixed-tag UserSession.yaml and surfaces stale preview endpoints as
	// top-level commands. Drop it.
	"user-session-preview": true,
}

// PreservedSpecs lists spec filenames (relative to specs/) that the splitter
// must not touch. Their paths are also treated as invisible to the splitter:
// any matching path in the monolith is dropped in favour of the preserved
// file's hand-maintained definition.
//
// Use this for specs sourced outside the public monolith (e.g. private or
// preview endpoints that only ship in internal documentation).
var PreservedSpecs = map[string]bool{
	// App Installers is published only on the gateway — the endpoints sit under
	// hiddenapi/ in jamf/jss, so neither the jss bundle nor the instance's own
	// /api/schema/ monolith carries them. These four files are derived from the
	// SDK's api/pro_api.json by ExtractSubtree (see AppInstallerSpecs below), so
	// the monolith splitter must leave them alone rather than delete a resource
	// the public monolith cannot describe.
	"AppInstallers.yaml":              true,
	"AppInstallerDeployments.yaml":    true,
	"AppInstallerGlobalSettings.yaml": true,
	"AppInstallerTitles.yaml":         true,
}

// AppInstallerSubtree is the path subtree ExtractSubtree derives the App
// Installer specs from, and AppInstallerSpecs routes its four families into one
// spec file each.
//
// One tag covers all 23 operations upstream ("app-installers"), so tag-based
// routing cannot separate them, and the parser's own splitByPathFamilies is no
// help either: it fires only on sibling collection paths that each have a
// /{param} child, which titles and deployments have and global-settings does
// not — it would emit two resources named after their paths and drop the other
// two families' operations entirely. Hence an explicit route per family, whose
// filenames are the command names this CLI has shipped since the endpoints were
// reverse-engineered.
//
// A path under the subtree that no route owns is an error, not a warning: a new
// family is a spec file whose name is a judgement call, and dropping it would
// lose the endpoint with nothing to notice.
const AppInstallerSubtree = "/v1/app-installers"

var AppInstallerSpecs = []SubtreeSpec{
	{
		Prefix:      "/v1/app-installers",
		Filename:    "AppInstallers.yaml",
		Title:       "Jamf Pro API - App Installers",
		Description: "Reports whether the App Installers feature is available on this instance, and which App Installer features the Cloud Services Connection enables.",
	},
	{
		Prefix:      "/v1/app-installers/titles",
		Filename:    "AppInstallerTitles.yaml",
		Title:       "Jamf Pro API - App Installer Titles",
		Description: "Browse the Jamf App Catalog of available App Installer titles and their versions. Titles are read-only catalog entries provided by Jamf.",
	},
	{
		Prefix:      "/v1/app-installers/deployments",
		Filename:    "AppInstallerDeployments.yaml",
		Title:       "Jamf Pro API - App Installer Deployments",
		Description: "Deploy App Installer titles to computers, manage each deployment's version and update behaviour, and read per-computer installation state.",
	},
	{
		Prefix:      "/v1/app-installers/global-settings",
		Filename:    "AppInstallerGlobalSettings.yaml",
		Title:       "Jamf Pro API - App Installer Global Settings",
		Description: "Global settings for App Installer deployments, controlling end-user experience notifications and deployment process controls.",
	},
}
