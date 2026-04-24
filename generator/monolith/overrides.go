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
	// App Installer deployments and titles are not exposed in the public
	// /api/schema/ monolith; their specs are maintained from a private source.
	"AppInstallerDeployments.yaml": true,
	"AppInstallerTitles.yaml":      true,
}
