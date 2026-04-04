// Copyright 2026, Jamf Software LLC

package commands

import "github.com/spf13/cobra"

// ─── Root-level groups ──────────────────────────────────────────────────────

var rootGroups = []*cobra.Group{
	{ID: "core", Title: "Core Commands:"},
	{ID: "products", Title: "Products:"},
}

var rootGroupMap = map[string]string{
	"version":    "core",
	"config":     "core",
	"completion": "core",
	"commands":   "core",
	"multi":      "core",
	"pro":        "products",
	"protect":    "products",
}

// applyRootGroups registers groups on the root command and assigns each
// direct child to its group.
func applyRootGroups(root *cobra.Command) {
	root.AddGroup(rootGroups...)
	root.SetHelpCommandGroupID("core")

	for _, cmd := range root.Commands() {
		if gid, ok := rootGroupMap[cmd.Name()]; ok {
			cmd.GroupID = gid
		}
	}
}

// ─── Jamf Pro groups (children of the "pro" command) ────────────────────────

// Group ID constants control display order in help output.
const (
	groupCore       = "core"
	groupPower      = "power"
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

// proGroups defines the groups in display order for `jamf-cli pro --help`.
var proGroups = []*cobra.Group{
	{ID: groupCore, Title: "Core Commands:"},
	{ID: groupPower, Title: "Power Commands:"},
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

// proGroupMap maps each Jamf Pro command's Use name to its group ID.
var proGroupMap = map[string]string{
	// Core
	"setup":    groupCore,
	"overview": groupCore,
	"device":   groupCore,

	// Power Commands
	"backup":         groupPower,
	"audit":          groupPower,
	"bulk":           groupPower,
	"report":         groupPower,
	"diff":           groupPower,
	"group-tools":    groupPower,
	"policy-execute": groupPower,

	// Computer Management
	"computers":                              groupComputers,
	"computer-groups":                        groupComputers,
	"computer-smart-groups":                  groupComputers,
	"computer-prestages-v-3s":                groupComputers,
	"computer-prestage-scope-v-2s":           groupComputers,
	"computer-extension-attributes":          groupComputers,
	"computer-inventory-collection-settings": groupComputers,
	"smart-computer-groups":                  groupComputers,
	"static-computer-groups":                 groupComputers,
	"redeploy-jamf-management-frameworks":    groupComputers,
	"erase-device-computers":                 groupComputers,
	"remove-computer-mdm-profiles":           groupComputers,

	// Mobile Device Management
	"mobile-devices":                         groupMobile,
	"mobile-device-groups":                   groupMobile,
	"mobile-device-smart-groups":             groupMobile,
	"mobile-device-prestages-v-3s":           groupMobile,
	"mobile-device-prestages-v-2s":           groupMobile,
	"mobile-device-prestage-scope-v-2s":      groupMobile,
	"mobile-device-prestage-sync-state-v-2s": groupMobile,
	"mobile-device-apps":                     groupMobile,
	"mobile-device-enrollment-profiles":      groupMobile,
	"mobile-device-extension-attributes":     groupMobile,
	"mobile-device-inventory-details":        groupMobile,
	"erase-device-mobiles":                   groupMobile,
	"remove-mobile-device-mdm-profiles":      groupMobile,
	"groups":                                 groupMobile,
	"device-extension-attributes":            groupMobile,

	// Enrollment
	"enrollment-settings":                                   groupEnrollment,
	"enrollment-languages":                                  groupEnrollment,
	"enrollment-customization-panels":                       groupEnrollment,
	"device-enrollment-instances":                           groupEnrollment,
	"device-enrollment-instance-sync-states":                groupEnrollment,
	"account-driven-user-enrollment-session-token-settings": groupEnrollment,
	"reenrollments":                                         groupEnrollment,
	"onboardings":                                           groupEnrollment,
	"onboarding-configurations":                             groupEnrollment,

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
	"users":                groupUsers,
	"user-preferences":     groupUsers,
	"user-smart-groups":    groupUsers,
	"static-user-groups":   groupUsers,
	"authentications":      groupUsers,
	"access-managements":   groupUsers,
	"ldap-rs":              groupUsers,
	"account-preferences":  groupUsers,
	"change-passwords":     groupUsers,
	"api-roles":            groupUsers,
	"api-integrations":     groupUsers,
	"api-roles-privileges": groupUsers,
	"oauth-token-sessions": groupUsers,
	"oidcs":                groupUsers,

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
	"packages":                         groupContent,
	"jamf-packages":                    groupContent,
	"jamf-connects":                    groupContent,
	"jamf-protects":                    groupContent,
	"jamf-protect-plans":               groupContent,
	"jamf-protect-deployment-tasks":    groupContent,
	"self-service-pluss":               groupContent,
	"parent-apps":                      groupContent,
	"login-customizations":             groupContent,
	"patch-v-2s":                       groupContent,
	"patch-policy-v-2s":                groupContent,
	"patch-policy-logs-v-2s":           groupContent,

	// MDM & Certificates
	"mdm-renewals":                            groupMDM,
	"renew-mdm-profiles":                      groupMDM,
	"certificate-authorities":                 groupMDM,
	"device-communication-settings":           groupMDM,
	"local-admin-password-v-2s":               groupMDM,
	"client-check-ins":                        groupMDM,
	"mac-os-managed-software-updates":         groupMDM,
	"managed-software-updates":                groupMDM,
	"managed-software-updates-plans":          groupMDM,
	"ddm-statuss":                             groupMDM,
	"ddm-syncs":                               groupMDM,
	"device-compliance-informations":          groupMDM,
	"jamf-remote-assist-session-histories":    groupMDM,
	"jamf-remote-assist-session-history-v-2s": groupMDM,

	// Server Administration
	"servers":                            groupServer,
	"systems":                            groupServer,
	"caches":                             groupServer,
	"database-connections":               groupServer,
	"notifications":                      groupServer,
	"jamf-pro-informations":              groupServer,
	"jamf-pro-versions":                  groupServer,
	"jamf-pro-server-urls":               groupServer,
	"csas":                               groupServer,
	"slasas":                             groupServer,
	"activation-codes":                   groupServer,
	"service-discoveries":                groupServer,
	"venafis":                            groupServer,
	"country-codes":                      groupServer,
	"locales":                            groupServer,
	"time-zones":                         groupServer,
	"cloud-distribution-points":          groupServer,
	"cloud-informations":                 groupServer,
	"distribution-points":                groupServer,
	"dss-proxies":                        groupServer,
	"gsx-connections":                    groupServer,
	"health-checks":                      groupServer,
	"log-flushings":                      groupServer,
	"schedulers":                         groupServer,
	"startup-statuss":                    groupServer,
	"adcs-settings":                      groupServer,
	"digi-cert-settings":                 groupServer,
	"impact-alert-notification-settings": groupServer,
	"apns-client-push-statuss":           groupServer,
	"vpp-locations":                      groupServer,

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
	"classic-packages":                groupClassicConfig,
	"classic-printers":                groupClassicConfig,
	"classic-network-segments":        groupClassicConfig,
	"classic-dock-items":              groupClassicConfig,
	"classic-directory-bindings":      groupClassicConfig,
	"classic-disk-encryption-configs": groupClassicConfig,
	"classic-restricted-software":     groupClassicConfig,
	"classic-allowed-file-extensions": groupClassicConfig,
	"classic-mac-apps":                groupClassicConfig,
	"classic-mobile-apps":             groupClassicMobile,
	"classic-ibeacons":                groupClassicConfig,
	"classic-classes":                 groupClassicConfig,

	// Classic - Administration
	"classic-accounts":                groupClassicAdmin,
	"classic-webhooks":                groupClassicAdmin,
	"classic-smtp-server":             groupClassicAdmin,
	"classic-distribution-points":     groupClassicAdmin,
	"classic-software-update-servers": groupClassicAdmin,
	"classic-licensed-software":       groupClassicAdmin,
	"classic-user-ext-attrs":          groupClassicAdmin,
	"classic-vpp-accounts":            groupClassicAdmin,
	"classic-vpp-assignments":         groupClassicAdmin,
	"classic-vpp-invitations":         groupClassicAdmin,
	"classic-gsx-connection":          groupClassicAdmin,
	"classic-jwt-configs":             groupClassicAdmin,

	// Classic - Patch Management
	"classic-patch-titles":           groupClassicPatch,
	"classic-patch-policies":         groupClassicPatch,
	"classic-patch-external-sources": groupClassicPatch,
	"classic-patch-internal-sources": groupClassicPatch,
	"classic-patch-available-titles": groupClassicPatch,
}

// applyProGroups registers command groups on the pro command and assigns each
// subcommand to its group. Commands not in proGroupMap are left ungrouped
// and appear under Cobra's "Additional Commands:" heading.
func applyProGroups(pro *cobra.Command) {
	pro.AddGroup(proGroups...)
	pro.SetHelpCommandGroupID(groupCore)

	for _, cmd := range pro.Commands() {
		if gid, ok := proGroupMap[cmd.Name()]; ok {
			cmd.GroupID = gid
		}
	}
}

// ─── Jamf Protect groups (children of the "protect" command) ───────────────

const (
	groupProtectCore     = "protect-core"
	groupProtectSecurity = "protect-security"
	groupProtectEndpoint = "protect-endpoints"
	groupProtectOrg      = "protect-org"
	groupProtectAccess   = "protect-access"
)

var protectGroups = []*cobra.Group{
	{ID: groupProtectCore, Title: "Core Commands:"},
	{ID: groupProtectSecurity, Title: "Security Configuration:"},
	{ID: groupProtectEndpoint, Title: "Endpoints:"},
	{ID: groupProtectOrg, Title: "Organization:"},
	{ID: groupProtectAccess, Title: "Access & Identity:"},
}

var protectGroupMap = map[string]string{
	"setup":    groupProtectCore,
	"overview": groupProtectCore,
	"auth":     groupProtectCore,

	"plans":                          groupProtectSecurity,
	"analytics":                      groupProtectSecurity,
	"analytic-sets":                  groupProtectSecurity,
	"exception-sets":                 groupProtectSecurity,
	"removable-storage-control-sets": groupProtectSecurity,
	"action-configs":                 groupProtectSecurity,
	"telemetry":                      groupProtectSecurity,
	"custom-prevent-lists":           groupProtectSecurity,
	"unified-logging-filters":        groupProtectSecurity,

	"computers": groupProtectEndpoint,

	"data-forwarding": groupProtectOrg,
	"data-retention":  groupProtectOrg,
	"downloads":       groupProtectOrg,
	"config-freeze":   groupProtectOrg,
	"connections":     groupProtectOrg,

	"roles":       groupProtectAccess,
	"users":       groupProtectAccess,
	"groups":      groupProtectAccess,
	"api-clients": groupProtectAccess,
}

func applyProtectGroups(protect *cobra.Command) {
	protect.AddGroup(protectGroups...)
	protect.SetHelpCommandGroupID(groupProtectCore)

	for _, cmd := range protect.Commands() {
		if gid, ok := protectGroupMap[cmd.Name()]; ok {
			cmd.GroupID = gid
		}
	}
}
