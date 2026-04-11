// Copyright 2026, Jamf Software LLC

package commands

// ResourceDef describes an API resource for backup/diff operations.
type ResourceDef struct {
	Name       string // CLI display name: "policies"
	ListPath   string // API list endpoint: "/JSSResource/policies" or "/v1/scripts"
	GetPath    string // API detail endpoint with {id} placeholder (empty when ListOnly is true)
	WrapperKey string // Classic API JSON wrapper key (empty for modern)
	IsClassic  bool
	SubDir     string // backup output subdirectory
	// ListOnly is true when there is no GET-by-id; each list row is the full export
	// (e.g. GET /v1/sites returns complete V1Site objects; GET /v1/sites/{id} does not exist).
	ListOnly bool
}

// BackupResources lists all resource types that the backup command exports.
// Ordered by logical grouping: policies/profiles first, then supporting objects.
var BackupResources = []ResourceDef{
	// Policies
	{
		Name:       "policies",
		ListPath:   "/JSSResource/policies",
		GetPath:    "/JSSResource/policies/id/{id}",
		WrapperKey: "policies",
		IsClassic:  true,
		SubDir:     "policies",
	},
	// Configuration Profiles - macOS
	{
		Name:       "profiles",
		ListPath:   "/JSSResource/osxconfigurationprofiles",
		GetPath:    "/JSSResource/osxconfigurationprofiles/id/{id}",
		WrapperKey: "os_x_configuration_profiles",
		IsClassic:  true,
		SubDir:     "profiles/macos",
	},
	// Configuration Profiles - iOS
	{
		Name:       "profiles",
		ListPath:   "/JSSResource/mobiledeviceconfigurationprofiles",
		GetPath:    "/JSSResource/mobiledeviceconfigurationprofiles/id/{id}",
		WrapperKey: "configuration_profiles",
		IsClassic:  true,
		SubDir:     "profiles/ios",
	},
	// Scripts
	{
		Name:     "scripts",
		ListPath: "/v1/scripts",
		GetPath:  "/v1/scripts/{id}",
		SubDir:   "scripts",
	},
	// Extension Attributes - Computer
	{
		Name:       "extension-attributes",
		ListPath:   "/JSSResource/computerextensionattributes",
		GetPath:    "/JSSResource/computerextensionattributes/id/{id}",
		WrapperKey: "computer_extension_attributes",
		IsClassic:  true,
		SubDir:     "extension-attributes/computer",
	},
	// Extension Attributes - Mobile
	{
		Name:       "extension-attributes",
		ListPath:   "/JSSResource/mobiledeviceextensionattributes",
		GetPath:    "/JSSResource/mobiledeviceextensionattributes/id/{id}",
		WrapperKey: "mobile_device_extension_attributes",
		IsClassic:  true,
		SubDir:     "extension-attributes/mobile",
	},
	// Smart Groups - Computer
	{
		Name:     "smart-groups",
		ListPath: "/v2/computer-groups/smart-groups",
		GetPath:  "/v2/computer-groups/smart-groups/{id}",
		SubDir:   "smart-groups/computers",
	},
	// Smart Groups - Mobile
	{
		Name:     "smart-groups",
		ListPath: "/v1/mobile-device-groups/smart-groups",
		GetPath:  "/v1/mobile-device-groups/{id}",
		SubDir:   "smart-groups/mobile",
	},
	// Categories
	{
		Name:     "categories",
		ListPath: "/v1/categories",
		GetPath:  "/v1/categories/{id}",
		SubDir:   "categories",
	},
	// Buildings
	{
		Name:     "buildings",
		ListPath: "/v1/buildings",
		GetPath:  "/v1/buildings/{id}",
		SubDir:   "buildings",
	},
	// Departments
	{
		Name:     "departments",
		ListPath: "/v1/departments",
		GetPath:  "/v1/departments/{id}",
		SubDir:   "departments",
	},
	// Sites
	{
		Name:     "sites",
		ListPath: "/v1/sites",
		GetPath:  "/v1/sites/{id}",
		SubDir:   "sites",
	},
	// Packages
	{
		Name:       "packages",
		ListPath:   "/JSSResource/packages",
		GetPath:    "/JSSResource/packages/id/{id}",
		WrapperKey: "packages",
		IsClassic:  true,
		SubDir:     "packages",
	},
	// Printers
	{
		Name:       "printers",
		ListPath:   "/JSSResource/printers",
		GetPath:    "/JSSResource/printers/id/{id}",
		WrapperKey: "printers",
		IsClassic:  true,
		SubDir:     "printers",
	},
	// Dock Items
	{
		Name:       "dock-items",
		ListPath:   "/JSSResource/dockitems",
		GetPath:    "/JSSResource/dockitems/id/{id}",
		WrapperKey: "dock_items",
		IsClassic:  true,
		SubDir:     "dock-items",
	},
	// Network Segments
	{
		Name:       "network-segments",
		ListPath:   "/JSSResource/networksegments",
		GetPath:    "/JSSResource/networksegments/id/{id}",
		WrapperKey: "network_segments",
		IsClassic:  true,
		SubDir:     "network-segments",
	},
	// Restricted Software
	{
		Name:       "restricted-software",
		ListPath:   "/JSSResource/restrictedsoftware",
		GetPath:    "/JSSResource/restrictedsoftware/id/{id}",
		WrapperKey: "restricted_software",
		IsClassic:  true,
		SubDir:     "restricted-software",
	},
	// Disk Encryption Configs
	{
		Name:       "disk-encryption",
		ListPath:   "/JSSResource/diskencryptionconfigurations",
		GetPath:    "/JSSResource/diskencryptionconfigurations/id/{id}",
		WrapperKey: "disk_encryption_configurations",
		IsClassic:  true,
		SubDir:     "disk-encryption",
	},
	// Patch Software Titles
	{
		Name:       "patch-titles",
		ListPath:   "/JSSResource/patchsoftwaretitles",
		GetPath:    "/JSSResource/patchsoftwaretitles/id/{id}",
		WrapperKey: "patch_software_titles",
		IsClassic:  true,
		SubDir:     "patch-titles",
	},
	// Static Groups - Computer
	{
		Name:     "static-groups",
		ListPath: "/v2/computer-groups/static-groups",
		GetPath:  "/v2/computer-groups/static-groups/{id}",
		SubDir:   "static-groups/computers",
	},
}

// FilterResources returns only resources whose Name matches one of the given names.
func FilterResources(resources []ResourceDef, names []string) []ResourceDef {
	if len(names) == 0 {
		return resources
	}
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}
	var filtered []ResourceDef
	for _, r := range resources {
		if nameSet[r.Name] {
			filtered = append(filtered, r)
		}
	}
	return filtered
}
