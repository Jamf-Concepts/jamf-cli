package commands

import "github.com/spf13/cobra"

// Group ID constants control display order in help output.
const (
	groupCore       = "core"
	groupComputers  = "computers"
	groupMobile     = "mobile"
	groupEnrollment = "enrollment"
	groupInventory  = "inventory"
	groupOrg        = "org"
	groupUsers      = "users"
	groupContent    = "content"
	groupMDM        = "mdm"
	groupServer     = "server"

	// Classic API groups
	groupClassicComputers = "classic-computers"
	groupClassicMobile    = "classic-mobile"
	groupClassicConfig    = "classic-config"
	groupClassicAdmin     = "classic-admin"
	groupClassicPatch     = "classic-patch"
)

// commandGroups defines the groups in display order for --help output.
var commandGroups = []*cobra.Group{
	{ID: groupCore, Title: "Core Commands:"},
	{ID: groupComputers, Title: "Computer Management:"},
	{ID: groupMobile, Title: "Mobile Device Management:"},
	{ID: groupEnrollment, Title: "Enrollment:"},
	{ID: groupInventory, Title: "Inventory & Search:"},
	{ID: groupOrg, Title: "Organization:"},
	{ID: groupUsers, Title: "Users & Security:"},
	{ID: groupContent, Title: "Content & Configuration:"},
	{ID: groupMDM, Title: "MDM & Certificates:"},
	{ID: groupServer, Title: "Server Administration:"},

	// Classic API groups
	{ID: groupClassicComputers, Title: "Classic - Computers:"},
	{ID: groupClassicMobile, Title: "Classic - Mobile Devices:"},
	{ID: groupClassicConfig, Title: "Classic - Configuration:"},
	{ID: groupClassicAdmin, Title: "Classic - Administration:"},
	{ID: groupClassicPatch, Title: "Classic - Patch Management:"},
}

// commandGroupMap maps each command's Use name to its group ID.
var commandGroupMap = map[string]string{
	// Core
	"version":    groupCore,
	"config":     groupCore,
	"completion": groupCore,
	"commands":   groupCore,
	"overview":   groupCore,

	// Computer Management
	"computers":                   groupComputers,
	"computer-groups":             groupComputers,
	"computer-smart-groups":       groupComputers,
	"computer-prestages-v-3s":     groupComputers,
	"computer-prestage-scope-v-2s": groupComputers,
	"erase-device-computers":      groupComputers,
	"remove-computer-mdm-profiles": groupComputers,

	// Mobile Device Management
	"mobile-devices":                        groupMobile,
	"mobile-device-groups":                  groupMobile,
	"mobile-device-smart-groups":            groupMobile,
	"mobile-device-prestages-v-3s":          groupMobile,
	"mobile-device-prestages-v-2s":          groupMobile,
	"mobile-device-prestage-scope-v-2s":     groupMobile,
	"mobile-device-prestage-sync-state-v-2s": groupMobile,
	"mobile-device-apps":                    groupMobile,
	"mobile-device-enrollment-profiles":     groupMobile,
	"mobile-device-extension-attributes":    groupMobile,
	"mobile-device-inventory-details":       groupMobile,
	"erase-device-mobiles":                  groupMobile,
	"remove-mobile-device-mdm-profiles":     groupMobile,

	// Enrollment
	"enrollment-settings":              groupEnrollment,
	"enrollment-languages":             groupEnrollment,
	"enrollment-customization-panels":  groupEnrollment,
	"device-enrollment-instances":      groupEnrollment,
	"device-enrollment-instance-sync-states": groupEnrollment,
	"account-driven-user-enrollment-session-token-settings": groupEnrollment,
	"reenrollments": groupEnrollment,
	"onboardings":   groupEnrollment,

	// Inventory & Search
	"inventory-informations":          groupInventory,
	"inventory-preloads":              groupInventory,
	"inventory-preload-v-2s":          groupInventory,
	"advanced-mobile-device-searches": groupInventory,
	"advanced-user-content-searches":  groupInventory,

	// Organization
	"buildings":   groupOrg,
	"departments": groupOrg,
	"categories":  groupOrg,
	"sites":       groupOrg,

	// Users & Security
	"users":            groupUsers,
	"user-preferences": groupUsers,
	"user-smart-groups": groupUsers,
	"static-user-groups": groupUsers,
	"authentications":  groupUsers,
	"access-managements": groupUsers,
	"ldap-rs":          groupUsers,

	// Content & Configuration
	"scripts":                          groupContent,
	"ebooks":                           groupContent,
	"package-deployments":              groupContent,
	"policy-properties":                groupContent,
	"jcds":                             groupContent,
	"self-service-settings":            groupContent,
	"self-service-brandings":           groupContent,
	"return-to-service-configurations": groupContent,
	"supervision-identities":           groupContent,
	"app-requests":                     groupContent,

	// MDM & Certificates
	"mdm-renewals":                  groupMDM,
	"renew-mdm-profiles":            groupMDM,
	"certificate-authorities":       groupMDM,
	"device-communication-settings": groupMDM,
	"local-admin-password-v-2s":     groupMDM,
	"client-check-ins":              groupMDM,

	// Server Administration
	"servers":              groupServer,
	"systems":              groupServer,
	"caches":               groupServer,
	"database-connections": groupServer,
	"notifications":        groupServer,
	"jamf-pro-informations": groupServer,
	"jamf-pro-versions":    groupServer,
	"jamf-pro-server-urls": groupServer,
	"csas":                 groupServer,
	"slasas":               groupServer,
	"activation-codes":     groupServer,
	"service-discoveries":  groupServer,
	"venafis":              groupServer,
	"country-codes":        groupServer,
	"locales":              groupServer,
	"time-zones":           groupServer,

	// Classic - Computers
	"classic-policies":                   groupClassicComputers,
	"classic-macos-config-profiles":      groupClassicComputers,
	"classic-computer-commands":          groupClassicComputers,
	"classic-computer-ext-attrs":         groupClassicComputers,
	"classic-computer-configs":           groupClassicComputers,
	"classic-computer-apps":              groupClassicComputers,
	"classic-computer-app-usage":         groupClassicComputers,
	"classic-computer-history":           groupClassicComputers,
	"classic-computer-invitations":       groupClassicComputers,
	"classic-advanced-computer-searches": groupClassicComputers,

	// Classic - Mobile Devices
	"classic-mobile-commands":              groupClassicMobile,
	"classic-mobile-config-profiles":       groupClassicMobile,
	"classic-mobile-provisioning-profiles": groupClassicMobile,
	"classic-mobile-history":               groupClassicMobile,
	"classic-mobile-invitations":           groupClassicMobile,

	// Classic - Configuration
	"classic-packages":              groupClassicConfig,
	"classic-printers":              groupClassicConfig,
	"classic-network-segments":      groupClassicConfig,
	"classic-dock-items":            groupClassicConfig,
	"classic-directory-bindings":    groupClassicConfig,
	"classic-disk-encryption-configs": groupClassicConfig,
	"classic-restricted-software":   groupClassicConfig,
	"classic-allowed-file-extensions": groupClassicConfig,
	"classic-mac-apps":              groupClassicConfig,
	"classic-ibeacons":              groupClassicConfig,
	"classic-classes":               groupClassicConfig,

	// Classic - Administration
	"classic-accounts":               groupClassicAdmin,
	"classic-webhooks":               groupClassicAdmin,
	"classic-smtp-server":            groupClassicAdmin,
	"classic-distribution-points":    groupClassicAdmin,
	"classic-software-update-servers": groupClassicAdmin,
	"classic-licensed-software":      groupClassicAdmin,
	"classic-user-ext-attrs":         groupClassicAdmin,
	"classic-vpp-accounts":           groupClassicAdmin,
	"classic-vpp-assignments":        groupClassicAdmin,
	"classic-vpp-invitations":        groupClassicAdmin,
	"classic-gsx-connection":         groupClassicAdmin,
	"classic-jwt-configs":            groupClassicAdmin,

	// Classic - Patch Management
	"classic-patch-titles":           groupClassicPatch,
	"classic-patch-policies":         groupClassicPatch,
	"classic-patch-external-sources": groupClassicPatch,
	"classic-patch-internal-sources": groupClassicPatch,
	"classic-patch-available-titles": groupClassicPatch,
}

// applyGroups registers command groups on the root command and assigns each
// subcommand to its group. Commands not in commandGroupMap are left ungrouped
// and appear under Cobra's "Additional Commands:" heading.
func applyGroups(root *cobra.Command) {
	root.AddGroup(commandGroups...)
	root.SetHelpCommandGroupID(groupCore)

	for _, cmd := range root.Commands() {
		if gid, ok := commandGroupMap[cmd.Name()]; ok {
			cmd.GroupID = gid
		}
	}
}
