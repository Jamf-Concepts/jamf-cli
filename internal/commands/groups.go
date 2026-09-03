// Copyright 2026, Jamf Software LLC

package commands

import (
	"strings"

	"github.com/spf13/cobra"
)

// ─── Root-level groups ──────────────────────────────────────────────────────

var rootGroups = []*cobra.Group{
	{ID: "core", Title: "Core Commands:"},
	{ID: "products", Title: "Products:"},
}

var rootGroupMap = map[string]string{
	"version":       "core",
	"config":        "core",
	"completion":    "core",
	"commands":      "core",
	"multi":         "core",
	"doctor":        "core",
	"mcp":           "core",
	"agent-context": "core",
	"pro":           "products",
	"protect":       "products",
	"school":        "products",
	"security":      "products",
	"platform":      "products",
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

	// Software & content delivery (split out from the old "Content & Configuration"
	// catch-all so each user intent — install an app, push a script, run Self
	// Service, manage distribution — has a focused home).
	groupAppsPatching    = "apps-patching"
	groupDistribution    = "distribution"
	groupScriptsPolicies = "scripts-policies"
	groupSelfService     = "self-service"
	groupIntegrations    = "integrations"

	groupInventory = "inventory"
	groupOrg       = "org"

	// Identity & access — five buckets:
	//   groupUsers          → end-user records (people assigned to devices)
	//   groupAdminAccounts  → Jamf Pro admin accounts, prefs, password/login state
	//   groupIdentityEndUser→ end-user identity providers (Cloud LDAP, Cloud IdP, Cloud Azure)
	//   groupAdminSSO       → admin SSO into the Pro UI: SAML and OIDC IdP config
	//                         (the same OIDC SSO is what Jamf Account requires before
	//                         Blueprints and Compliance Benchmarks become available,
	//                         but it lives here because it's the admin-login mechanism
	//                         itself, not a Platform-only setting)
	//   groupAPIAccess      → modern OAuth2 client-credentials API auth
	groupUsers           = "users"
	groupAdminAccounts   = "admin-accounts"
	groupIdentityEndUser = "identity-end-user"
	groupAdminSSO        = "admin-sso"
	groupAPIAccess       = "api-access"

	// Infrastructure / device-side operations. MDM & Certificates is the
	// MDM transport itself; OS Updates is its own Apple-defined orchestration
	// surface (per "Managing Apple OS Updates" in the official docs);
	// Security covers local-admin-password rotation (LAPS) and per-device
	// compliance posture — not MDM, but operationally adjacent.
	groupMDM       = "mdm"
	groupOSUpdates = "os-updates"
	groupSecurity  = "security"

	// Server Administration was a 33-resource grab bag; split into core
	// health/health-related state vs the 14 third-party / hardware / locale
	// system-integration resources that belong together.
	groupServer             = "server"
	groupSystemIntegrations = "system-integrations"

	// Platform API groups
	groupPlatformConfig     = "platform-config"
	groupPlatformCompliance = "platform-compliance"
	groupPlatformDevices    = "platform-devices"

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

	// Software & content delivery
	{ID: groupAppsPatching, Title: "Apps & Patching:"},
	{ID: groupDistribution, Title: "Distribution & JCDS:"},
	{ID: groupScriptsPolicies, Title: "Scripts & Policies:"},
	{ID: groupSelfService, Title: "Self Service:"},
	{ID: groupIntegrations, Title: "Jamf App Integrations:"},

	{ID: groupInventory, Title: "Inventory & Search:"},
	{ID: groupOrg, Title: "Organization:"},

	// Identity & access
	{ID: groupUsers, Title: "Users & Groups:"},
	{ID: groupAdminAccounts, Title: "Admin Accounts:"},
	{ID: groupIdentityEndUser, Title: "Identity Providers:"},
	{ID: groupAdminSSO, Title: "Admin SSO:"},
	{ID: groupAPIAccess, Title: "API Access:"},

	{ID: groupMDM, Title: "MDM & Certificates:"},
	{ID: groupOSUpdates, Title: "OS Updates:"},
	{ID: groupSecurity, Title: "Security:"},
	{ID: groupServer, Title: "Server Health:"},
	{ID: groupSystemIntegrations, Title: "System Integrations:"},

	// Platform API groups
	{ID: groupPlatformConfig, Title: "Platform - Configuration:"},
	{ID: groupPlatformCompliance, Title: "Platform - Compliance:"},
	{ID: groupPlatformDevices, Title: "Platform - Devices & Users:"},

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
	"auth":     groupCore,
	"device":   groupCore,

	// Power Commands
	"backup":      groupPower,
	"audit":       groupPower,
	"bulk":        groupPower,
	"report":      groupPower,
	"diff":        groupPower,
	"group-tools": groupPower,
	// Computer Management
	"computers":                              groupComputers,
	"computer-groups":                        groupComputers,
	"computer-smart-groups":                  groupComputers,
	"computer-prestages":                     groupComputers,
	"computer-prestage-scopes":               groupComputers,
	"computer-extension-attributes":          groupComputers,
	"computer-inventory-collection-settings": groupComputers,
	"static-computer-groups":                 groupComputers,
	"computer-groups-smart-groups":           groupComputers,
	"computer-groups-static-groups":          groupComputers,
	"computers-inventory":                    groupComputers,
	// macOS-only Dock customization (Pro: Settings > Computer management > Dock items)
	"dock-items": groupComputers,

	// Mobile Device Management
	"mobile-devices":                     groupMobile,
	"mobile-device-groups":               groupMobile,
	"mobile-device-groups-smart-groups":  groupMobile,
	"mobile-device-groups-static-groups": groupMobile,
	"mobile-device-smart-groups":         groupMobile,
	"mobile-device-prestages":            groupMobile,
	"mobile-device-prestage-scopes":      groupMobile,
	"mobile-device-prestage-sync-states": groupMobile,
	"mobile-device-apps":                 groupMobile,
	"mobile-device-enrollment-profiles":  groupMobile,
	"mobile-device-extension-attributes": groupMobile,
	"mobile-device-inventory-details":    groupMobile,
	"groups":                             groupMobile,
	"device-extension-attributes":        groupMobile,
	// Mobile-only "Settings and Security Management for Mobile Devices" features
	"return-to-service-configurations": groupMobile,

	// Enrollment
	"enrollment-settings":                                   groupEnrollment,
	"enrollment-languages":                                  groupEnrollment,
	"enrollment-customization-panels":                       groupEnrollment,
	"device-enrollment-instances":                           groupEnrollment,
	"device-enrollment-instance-sync-states":                groupEnrollment,
	"account-driven-user-enrollment-session-token-settings": groupEnrollment,
	"reenrollment":                                          groupEnrollment,
	"onboardings":                                           groupEnrollment,
	"onboarding-configuration":                              groupEnrollment,
	"enrollment-customizations":                             groupEnrollment,
	// Supervision identities are applied via PreStage enrollment, so they live
	// in the enrollment workflow rather than in policy/script settings.
	"supervision-identities": groupEnrollment,

	// Inventory & Search
	"inventory-informations":          groupInventory,
	"inventory-preloads":              groupInventory,
	"advanced-mobile-device-searches": groupInventory,
	"advanced-user-content-searches":  groupInventory,

	// Organization
	"buildings":   groupOrg,
	"departments": groupOrg,
	"categories":  groupOrg,
	"sites":       groupOrg,

	// Apps & Patching — third-party app delivery (App Installers + VPP) plus
	// the patch-management workflow that updates them. Ebooks are licensed
	// through Apple/VPP just like apps, so they belong here too.
	"app-installers":                      groupAppsPatching,
	"app-installer-titles":                groupAppsPatching,
	"app-installer-deployments":           groupAppsPatching,
	"app-installer-global-settings":       groupAppsPatching,
	"app-requests":                        groupAppsPatching,
	"vpp-subscriptions":                   groupAppsPatching,
	"vpp-locations":                       groupAppsPatching,
	"ebooks":                              groupAppsPatching,
	"patch-titles":                        groupAppsPatching,
	"patch-policies":                      groupAppsPatching,
	"patch-policy-logs":                   groupAppsPatching,
	"patch-software-title-configurations": groupAppsPatching,

	// Distribution & JCDS — package storage / cloud distribution point /
	// in-house file delivery infrastructure.
	"jcds":                      groupDistribution,
	"packages":                  groupDistribution,
	"jamf-packages":             groupDistribution,
	"cloud-distribution-points": groupDistribution,
	"distribution-points":       groupDistribution,
	"dss-proxies":               groupDistribution,

	// Scripts & Policies — generic policy primitives that aren't device- or
	// app-specific (scripts, modern package deployments, global policy config,
	// the shared icon library).
	"scripts":             groupScriptsPolicies,
	"package-deployments": groupScriptsPolicies,
	"policy-properties":   groupScriptsPolicies,
	"icons":               groupScriptsPolicies,

	// Self Service — the unified end-user app (Self Service+ replaces Self
	// Service classic, Jamf Connect menubar, and surfaces Jamf Protect status).
	"self-service-settings":        groupSelfService,
	"self-service-branding-macos":  groupSelfService,
	"self-service-branding-ios":    groupSelfService,
	"self-service-branding-images": groupSelfService,
	"self-service-plus":            groupSelfService,
	"login-customization":          groupSelfService,

	// Jamf App Integrations — every Jamf-branded app/product that integrates
	// with Pro (Connect, Protect, Parent, Teacher) lives under "Jamf
	// Application Integrations" in the official docs and groups together
	// here for the same reason.
	"jamf-connects":                 groupIntegrations,
	"jamf-connect-deployment-tasks": groupIntegrations,
	"jamf-protect":                  groupIntegrations,
	"jamf-protect-plans":            groupIntegrations,
	"jamf-protect-deployment-tasks": groupIntegrations,
	"parent-app":                    groupIntegrations,
	"teacher-settings":              groupIntegrations,

	// Users & Groups — *end-user* records: people assigned to managed devices,
	// their session state, and the smart/static groups admins build to scope
	// policies. Distinct from Jamf Pro admin accounts (Admin Accounts group).
	"users":              groupUsers,
	"user-sessions":      groupUsers,
	"user-preferences":   groupUsers,
	"user-smart-groups":  groupUsers,
	"static-user-groups": groupUsers,

	// Admin Accounts — Jamf Pro admin user accounts and their settings,
	// surfaced under "Settings > System > User accounts and groups" in the
	// Pro UI. Per docs: "Jamf Pro user accounts and groups allow you to grant
	// different privileges and levels of access to each user."
	"user-accounts":       groupAdminAccounts,
	"account-groups":      groupAdminAccounts,
	"account-preferences": groupAdminAccounts,
	"change-passwords":    groupAdminAccounts,
	"last-logins":         groupAdminAccounts,
	"authentications":     groupAdminAccounts,
	"access-managements":  groupAdminAccounts,

	// Identity Providers — end-user identity surfaces (Cloud LDAP bridge to
	// Google Workspace, Microsoft Entra ID via Cloud Azure, classic LDAP for
	// on-prem directories). Distinct from admin SSO and API auth below.
	"cloud-id-p-histories":      groupIdentityEndUser,
	"cloud-id-p-configurations": groupIdentityEndUser,
	"cloud-id-p-test-searches":  groupIdentityEndUser,
	"cloud-ldaps":               groupIdentityEndUser,
	"cloud-ldap-connections":    groupIdentityEndUser,
	"cloud-ldap-defaults":       groupIdentityEndUser,
	"cloud-ldap-key-stores":     groupIdentityEndUser,
	"cloud-ldap-mappings":       groupIdentityEndUser,
	"classic-ldaps":             groupIdentityEndUser,
	"classic-ldap-servers":      groupIdentityEndUser,
	"ldap-rs":                   groupIdentityEndUser,
	"cloud-azures":              groupIdentityEndUser,
	"cloud-azure-defaults":      groupIdentityEndUser,

	// Admin SSO — single sign-on into the Pro UI for administrators. SAML
	// lives here (`sso-*`) and so does OIDC (`oidcs`); both are IdP-side
	// admin-login config. Enabling OIDC SSO via Jamf Account is the
	// prerequisite that gates Blueprints and Compliance Benchmarks, but the
	// config itself is an Identity & Access concern, not a Platform one.
	"sso-failovers":     groupAdminSSO,
	"sso-settings-cert": groupAdminSSO,
	"sso-settings":      groupAdminSSO,
	"oidcs":             groupAdminSSO,

	// API Access — modern OAuth2 client-credentials API auth (replaces the
	// classic API user model).
	"api-roles":            groupAPIAccess,
	"api-integrations":     groupAPIAccess,
	"api-roles-privileges": groupAPIAccess,
	"oauth-token-sessions": groupAPIAccess,
	"m2m":                  groupAPIAccess,

	// MDM & Certificates — the MDM transport itself: command queue, profile
	// renewal, certificate plumbing, push notifications, and the DDM bridge
	// (DDM declarations are delivered over the MDM channel).
	"mdm-renewals":                  groupMDM,
	"mdm-commands":                  groupMDM,
	"certificate-authorities":       groupMDM,
	"device-communication-settings": groupMDM,
	"client-check-in":               groupMDM,
	"apns-client-push-status":       groupMDM,
	"ddm-status":                    groupMDM,
	"ddm-syncs":                     groupMDM,

	// OS Updates — Apple's "Managing Apple OS Updates" surface (managed
	// software update plans for macOS/iOS/iPadOS/tvOS, including the
	// declarative install action).
	"managed-software-updates":        groupOSUpdates,
	"managed-software-updates-plans":  groupOSUpdates,
	"mac-os-managed-software-updates": groupOSUpdates,

	// Security — endpoint security primitives that aren't MDM transport:
	// LAPS for managed local-admin password rotation, and per-device
	// compliance/posture data.
	"local-admin-passwords":          groupSecurity,
	"device-compliance-informations": groupSecurity,

	// Server Health — the live state of the Jamf Pro instance itself.
	"servers":               groupServer,
	"systems":               groupServer,
	"cache":                 groupServer,
	"database-connections":  groupServer,
	"notifications":         groupServer,
	"dashboards":            groupServer,
	"jamf-pro-informations": groupServer,
	"jamf-pro-versions":     groupServer,
	"jamf-pro-server-url":   groupServer,
	"activation-codes":      groupServer,
	"service-discovery":     groupServer,
	"cloud-informations":    groupServer,
	"health-checks":         groupServer,
	"log-flushings":         groupServer,
	"schedulers":            groupServer,
	"startup-status":        groupServer,
	"environment-type":      groupServer,

	// System Integrations — third-party / hardware / locale config that
	// connects Jamf Pro to the surrounding world.
	"smtp-server":                          groupSystemIntegrations,
	"remote-administration-configurations": groupSystemIntegrations,
	"team-viewer-remote-administrations":   groupSystemIntegrations,
	"jamf-remote-assist-session-histories": groupSystemIntegrations,
	"csas":                                 groupSystemIntegrations,
	"slasas":                               groupSystemIntegrations,
	"venafis":                              groupSystemIntegrations,
	"country-codes":                        groupSystemIntegrations,
	"locales":                              groupSystemIntegrations,
	"time-zones":                           groupSystemIntegrations,
	"gsx-connection":                       groupSystemIntegrations,
	"adcs-settings":                        groupSystemIntegrations,
	"digi-cert-settings":                   groupSystemIntegrations,
	"impact-alert-notification-settings":   groupSystemIntegrations,

	// Platform - Configuration
	"blueprints":           groupPlatformConfig,
	"blueprint-components": groupPlatformConfig,
	"ddm-reports":          groupPlatformConfig,

	// Platform - Compliance
	"compliance-benchmarks": groupPlatformCompliance,
	"baselines":             groupPlatformCompliance,
	"benchmark-reports":     groupPlatformCompliance,
	"rules":                 groupPlatformCompliance,

	// Platform - Devices & Users
	"platform-devices":       groupPlatformDevices,
	"platform-device-groups": groupPlatformDevices,
	"device-actions":         groupPlatformDevices,
	"platform-users":         groupPlatformDevices,

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
	"classic-computer-groups":            groupClassicComputers,

	// Classic - Mobile Devices
	"classic-advanced-mobile-device-searches": groupClassicMobile,
	"classic-mobile-devices":                  groupClassicMobile,
	"classic-mobile-device-groups":            groupClassicMobile,
	"classic-mobile-commands":                 groupClassicMobile,
	"classic-mobile-config-profiles":          groupClassicMobile,
	"classic-mobile-provisioning-profiles":    groupClassicMobile,
	"classic-mobile-history":                  groupClassicMobile,
	"classic-mobile-invitations":              groupClassicMobile,

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
	"classic-ebooks":                  groupClassicConfig,
	"classic-mobile-apps":             groupClassicMobile,
	"classic-ibeacons":                groupClassicConfig,
	"classic-classes":                 groupClassicConfig,
	"classic-removable-mac-addresses": groupClassicConfig,

	// Classic - Administration
	"classic-accounts":                groupClassicAdmin,
	"classic-account-groups":          groupClassicAdmin,
	"classic-account-users":           groupClassicAdmin,
	"classic-webhooks":                groupClassicAdmin,
	"classic-smtp-server":             groupClassicAdmin,
	"classic-distribution-points":     groupClassicAdmin,
	"classic-software-update-servers": groupClassicAdmin,
	"classic-licensed-software":       groupClassicAdmin,
	"classic-user-groups":             groupClassicAdmin,
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
	"classic-patch-reports":          groupClassicPatch,
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
	// "Operations" (was "Organization") — covers data-forwarding, data-retention,
	// downloads, config-freeze, connections, audit-logs. Renamed to avoid
	// colliding with Pro's "Organization" concept (buildings/departments) and
	// to read as the Infrastructure-pillar bucket it actually is.
	{ID: groupProtectOrg, Title: "Operations:"},
	{ID: groupProtectAccess, Title: "Access & Identity:"},
}

var protectGroupMap = map[string]string{
	"setup":    groupProtectCore,
	"overview": groupProtectCore,
	"auth":     groupProtectCore,
	"backup":   groupProtectCore,
	"restore":  groupProtectCore,

	"plans":                          groupProtectSecurity,
	"analytics":                      groupProtectSecurity,
	"analytic-sets":                  groupProtectSecurity,
	"exception-sets":                 groupProtectSecurity,
	"removable-storage-control-sets": groupProtectSecurity,
	"action-configs":                 groupProtectSecurity,
	"telemetry":                      groupProtectSecurity,
	"custom-prevent-lists":           groupProtectSecurity,
	"unified-logging-filters":        groupProtectSecurity,
	"unified-logging-filter-sets":    groupProtectSecurity,
	"insights":                       groupProtectSecurity,

	"computers": groupProtectEndpoint,
	"alerts":    groupProtectEndpoint,

	"data-forwarding": groupProtectOrg,
	"data-retention":  groupProtectOrg,
	"downloads":       groupProtectOrg,
	"config-freeze":   groupProtectOrg,
	"connections":     groupProtectOrg,
	"audit-logs":      groupProtectOrg,

	"roles":       groupProtectAccess,
	"users":       groupProtectAccess,
	"groups":      groupProtectAccess,
	"api-clients": groupProtectAccess,
	"permissions": groupProtectAccess,
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

// ─── Jamf School groups (children of the "school" command) ──────────────────

const (
	groupSchoolCore     = "school-core"
	groupSchoolDevices  = "school-devices"
	groupSchoolUsers    = "school-users"
	groupSchoolContent  = "school-content"
	groupSchoolInfra    = "school-infra"
	groupSchoolPlatform = "school-platform"
)

var schoolGroups = []*cobra.Group{
	{ID: groupSchoolCore, Title: "Core Commands:"},
	{ID: groupSchoolDevices, Title: "Devices:"},
	{ID: groupSchoolUsers, Title: "Users & Organization:"},
	{ID: groupSchoolContent, Title: "Content:"},
	{ID: groupSchoolInfra, Title: "Infrastructure:"},
	{ID: groupSchoolPlatform, Title: "Platform:"},
}

var schoolGroupMap = map[string]string{
	"setup":    groupSchoolCore,
	"overview": groupSchoolCore,

	"devices":       groupSchoolDevices,
	"device-groups": groupSchoolDevices,

	"users":   groupSchoolUsers,
	"groups":  groupSchoolUsers,
	"classes": groupSchoolUsers,

	"profiles": groupSchoolContent,
	"apps":     groupSchoolContent,

	"locations":   groupSchoolInfra,
	"ibeacons":    groupSchoolInfra,
	"dep-devices": groupSchoolInfra,

	"blueprints":  groupSchoolPlatform,
	"ddm-reports": groupSchoolPlatform,
}

func applySchoolGroups(school *cobra.Command) {
	school.AddGroup(schoolGroups...)
	school.SetHelpCommandGroupID(groupSchoolCore)

	for _, cmd := range school.Commands() {
		if gid, ok := schoolGroupMap[cmd.Name()]; ok {
			cmd.GroupID = gid
		}
	}
}

// ─── Jamf Security Cloud groups (children of the "security" command) ───────

const (
	groupSecurityCore    = "security-core"
	groupSecurityRisk    = "security-risk"
	groupSecuritySSE     = "security-sse"
	groupSecurityNetwork = "security-network"
	groupSecurityZTNA    = "security-ztna"
	groupSecurityDevices = "security-devices"
	groupSecurityUEM     = "security-uem"
	groupSecurityEnroll  = "security-enrollment"
)

var securityGroups = []*cobra.Group{
	{ID: groupSecurityCore, Title: "Core Commands:"},
	{ID: groupSecurityRisk, Title: "Device Risk & Lifecycle:"},
	{ID: groupSecuritySSE, Title: "Shared Signals & Events:"},
	{ID: groupSecurityNetwork, Title: "DNS & Content Filtering:"},
	{ID: groupSecurityZTNA, Title: "Zero Trust Network Access:"},
	{ID: groupSecurityDevices, Title: "Device Groups:"},
	{ID: groupSecurityUEM, Title: "UEM Connect:"},
	{ID: groupSecurityEnroll, Title: "Enrollment:"},
}

var securityGroupMap = map[string]string{
	"setup": groupSecurityCore,

	"risk":             groupSecurityRisk,
	"device-lifecycle": groupSecurityRisk,

	"stream":       groupSecuritySSE,
	"status":       groupSecuritySSE,
	"verification": groupSecuritySSE,
	"jwks":         groupSecuritySSE,
	"well-known":   groupSecuritySSE,

	// Served on the platform gateway (/api/securitycloud) rather than
	// api.wandera.com — see the wiring comment in security.go.
	"dns-zones":                    groupSecurityNetwork,
	"dns-search-domains":           groupSecurityNetwork,
	"dns-custom-hostname-mappings": groupSecurityNetwork,
	"content-categories":           groupSecurityNetwork,

	"ztna-apps":             groupSecurityZTNA,
	"ztna-gateways":         groupSecurityZTNA,
	"ztna-grouped-gateways": groupSecurityZTNA,
	"ztna-shared-gateways":  groupSecurityZTNA,
	"ztna-predefined-apps":  groupSecurityZTNA,

	"device-groups": groupSecurityDevices,

	"uem-connectors":           groupSecurityUEM,
	"uem-connector-enablement": groupSecurityUEM,
	"uem-sync-settings":        groupSecurityUEM,
	"uem-sync":                 groupSecurityUEM,
	"uem-activation-profiles":  groupSecurityUEM,

	"enrollment-activation-profiles": groupSecurityEnroll,
}

func applySecurityGroups(security *cobra.Command) {
	security.AddGroup(securityGroups...)
	security.SetHelpCommandGroupID(groupSecurityCore)

	for _, cmd := range security.Commands() {
		if gid, ok := securityGroupMap[cmd.Name()]; ok {
			cmd.GroupID = gid
		}
	}
}

// ─── Jamf Platform groups (children of the "platform" command) ──────────────

const (
	groupPlatformCore    = "platform-core"
	groupPlatformAI      = "platform-ai"
	groupPlatformAccount = "platform-account"
	groupPlatformAudit   = "platform-audit"
)

var platformGroups = []*cobra.Group{
	{ID: groupPlatformCore, Title: "Core Commands:"},
	{ID: groupPlatformAI, Title: "AI Governance:"},
	{ID: groupPlatformAccount, Title: "Jamf Account (US-only):"},
	{ID: groupPlatformAudit, Title: "Audit:"},
}

var platformGroupMap = map[string]string{
	"setup":       groupPlatformCore,
	"auth":        groupPlatformCore,
	"ai-policies": groupPlatformAI,
	"ai-tools":    groupPlatformAI,

	// Jamf Account. Grouped together, and the group title carries the US-only
	// constraint so `platform --help` says it without the reader opening a
	// subcommand.
	"account-licenses":            groupPlatformAccount,
	"deal-registrations":          groupPlatformAccount,
	"distributor-configuration":   groupPlatformAccount,
	"distributor-purchase-orders": groupPlatformAccount,
	"distributor-quotes":          groupPlatformAccount,
	"sso-connections":             groupPlatformAccount,
	"sso-domains":                 groupPlatformAccount,

	"audit": groupPlatformAudit,
}

func applyPlatformGroups(platform *cobra.Command) {
	platform.AddGroup(platformGroups...)
	platform.SetHelpCommandGroupID(groupPlatformCore)

	for _, cmd := range platform.Commands() {
		if gid, ok := platformGroupMap[cmd.Name()]; ok {
			cmd.GroupID = gid
		}
	}
}

// groupTitleMap is a cached lookup from group ID to display title, built once on first use.
var groupTitleMap map[string]string

func groupTitle(id string) string {
	if groupTitleMap == nil {
		groupTitleMap = make(map[string]string)
		for _, groups := range [][]*cobra.Group{rootGroups, proGroups, protectGroups, schoolGroups, securityGroups, platformGroups} {
			for _, g := range groups {
				groupTitleMap[g.ID] = strings.TrimSuffix(g.Title, ":")
			}
		}
	}
	return groupTitleMap[id]
}
